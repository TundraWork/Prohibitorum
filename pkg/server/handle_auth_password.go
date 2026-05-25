// Package server — handle_auth_password.go
//
// Two-step Password+TOTP login. Step 1 (/auth/password/begin) verifies the
// password and issues an opaque partial-session token in KV (single-use,
// 5-min TTL, no IP/UA binding per spec D1). Step 2 (/auth/totp/verify or
// /auth/recovery-code/verify) atomically consumes the token (Get then Del,
// regardless of factor outcome) and issues a real session on success.
//
// Username-enumeration defense (spec D3): step 1 always runs an argon2id
// verify even when no account row or no password_credential row exists, so
// the wall-clock cost equalises across the three failure modes.

package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/credential/password"
	sessstore "prohibitorum/pkg/session"
)

type partialSession struct {
	AccountID       int32     `json:"account_id"`
	FactorCompleted string    `json:"factor_completed"`
	IssuedAt        time.Time `json:"issued_at"`
}

func partialSessionKey(token string) string { return "partial_session:" + token }

// POST /api/prohibitorum/auth/password/begin
func (s *Server) handlePasswordBeginHTTP(w http.ResponseWriter, r *http.Request) {
	if s.rateLimit(w, r, "login:ip:"+sessstore.ClientIP(r, s.config.TrustProxy), 30, time.Minute) {
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Username == "" || body.Password == "" {
		writeAuthErr(w, authn.ErrBadCredentials())
		return
	}

	acct, err := s.queries.GetAccountByUsername(r.Context(), body.Username)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			s.passwordStore.VerifyAgainstDummy(r.Context(), body.Password)
			writeAuthErr(w, authn.ErrBadCredentials())
			return
		}
		writeAuthErr(w, err)
		return
	}

	// Reject disabled accounts before argon2id verify, but burn equivalent
	// CPU against a dummy hash so the wall-clock cost matches the active-
	// account path (timing side-channel: leaking which usernames map to
	// disabled-but-existing accounts would defeat spec D3 enumeration
	// defense). We return ErrBadCredentials rather than ErrAccountDisabled
	// because at /begin we haven't proved possession of anything — the
	// WebAuthn path in handle_auth.go discloses disabled-state only AFTER
	// the assertion verifies. Exercised by cmd/smoke (Task 8).
	if acct.Disabled {
		s.passwordStore.VerifyAgainstDummy(r.Context(), body.Password)
		writeAuthErr(w, authn.ErrBadCredentials())
		return
	}

	if err := s.passwordStore.Verify(r.Context(), acct.ID, body.Password); err != nil {
		if errors.Is(err, password.ErrPasswordNotSet) {
			s.passwordStore.VerifyAgainstDummy(r.Context(), body.Password)
			writeAuthErr(w, authn.ErrBadCredentials())
			return
		}
		if errors.Is(err, password.ErrPasswordIncorrect) {
			writeAuthErr(w, authn.ErrBadCredentials())
			return
		}
		// *authn.AuthError (factor_locked) — writeAuthErr handles status + Retry-After.
		if ae := authn.AsAuthError(err); ae != nil {
			writeAuthErr(w, ae)
			return
		}
		writeAuthErr(w, err)
		return
	}

	token, err := newCeremonyToken()
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	payload, _ := json.Marshal(partialSession{
		AccountID:       acct.ID,
		FactorCompleted: "password",
		IssuedAt:        time.Now().UTC(),
	})
	if err := s.kvStore.SetEx(r.Context(), partialSessionKey(token), string(payload), s.config.Auth.PartialSessionTTL); err != nil {
		writeAuthErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"partial_session_token": token})
}

// POST /api/prohibitorum/auth/totp/verify
func (s *Server) handleTOTPVerifyHTTP(w http.ResponseWriter, r *http.Request) {
	if s.rateLimit(w, r, "login:ip:"+sessstore.ClientIP(r, s.config.TrustProxy), 30, time.Minute) {
		return
	}
	var body struct {
		PartialSessionToken string `json:"partial_session_token"`
		Code                string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.PartialSessionToken == "" || body.Code == "" {
		writeAuthErr(w, authn.ErrPartialSessionInvalid())
		return
	}
	partial, err := s.consumePartialSession(r.Context(), body.PartialSessionToken)
	if err != nil {
		writeAuthErr(w, authn.ErrPartialSessionInvalid())
		return
	}
	if _, err := s.totpStore.Verify(r.Context(), partial.AccountID, body.Code); err != nil {
		if ae := authn.AsAuthError(err); ae != nil {
			writeAuthErr(w, ae)
			return
		}
		// Sentinel errors (ErrTOTPInvalidCode, ErrTOTPReplay, ErrTOTPNotSet) →
		// collapse to bad_credentials so step-2 doesn't leak factor state.
		writeAuthErr(w, authn.ErrBadCredentials())
		return
	}
	s.issueSessionAndSetCookie(w, r, partial.AccountID, []string{"pwd", "otp", "mfa"})
}

// POST /api/prohibitorum/auth/recovery-code/verify
func (s *Server) handleRecoveryCodeVerifyHTTP(w http.ResponseWriter, r *http.Request) {
	if s.rateLimit(w, r, "login:ip:"+sessstore.ClientIP(r, s.config.TrustProxy), 30, time.Minute) {
		return
	}
	var body struct {
		PartialSessionToken string `json:"partial_session_token"`
		Code                string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.PartialSessionToken == "" || body.Code == "" {
		writeAuthErr(w, authn.ErrPartialSessionInvalid())
		return
	}
	partial, err := s.consumePartialSession(r.Context(), body.PartialSessionToken)
	if err != nil {
		writeAuthErr(w, authn.ErrPartialSessionInvalid())
		return
	}
	ip := sessstore.ClientIP(r, s.config.TrustProxy)
	if err := s.totpStore.VerifyRecoveryCode(r.Context(), partial.AccountID, body.Code, "", ip); err != nil {
		if ae := authn.AsAuthError(err); ae != nil {
			writeAuthErr(w, ae)
			return
		}
		writeAuthErr(w, authn.ErrBadCredentials())
		return
	}
	s.issueSessionAndSetCookie(w, r, partial.AccountID, []string{"pwd", "recovery_code", "mfa"})
}

// consumePartialSession atomically Get-then-Del the KV entry. The Del fires
// even on JSON corruption — single-use is single-use regardless of payload
// state. Returns the deserialized partial-session payload or an error if
// the token is missing/expired.
func (s *Server) consumePartialSession(ctx context.Context, token string) (*partialSession, error) {
	key := partialSessionKey(token)
	raw, err := s.kvStore.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	_ = s.kvStore.Del(ctx, key)
	var ps partialSession
	if err := json.Unmarshal([]byte(raw), &ps); err != nil {
		return nil, err
	}
	return &ps, nil
}

func (s *Server) issueSessionAndSetCookie(w http.ResponseWriter, r *http.Request, accountID int32, amr []string) {
	ip := sessstore.ClientIP(r, s.config.TrustProxy)
	ua := r.UserAgent()
	token, _, err := s.sessionStore.Issue(r.Context(), accountID, ip, ua, amr)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	http.SetCookie(w, sessstore.FreshSessionCookie(s.config, r, accountID, token, s.config.SessionTTL))
	w.WriteHeader(http.StatusNoContent)
}
