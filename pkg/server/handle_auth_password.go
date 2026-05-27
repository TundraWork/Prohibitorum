// Package server — handle_auth_password.go
//
// Two-step Password+TOTP login. Step 1 (/auth/password/begin) verifies the
// password and issues an opaque partial-session token in KV (single-use,
// 5-min TTL, no IP/UA binding per spec D1). Step 2 (/auth/totp/verify or
// /auth/recovery-code/verify) atomically consumes the token (kv.Pop:
// single concurrent winner, single-use enforced by the store) and issues a
// real session on success.
//
// Username-enumeration / lockout-oracle defense (Bundle 1 / Fix 6 +
// spec D3): /auth/password/begin returns the same 401 bad_credentials
// AND burns the same argon2id cost for ALL four failure modes:
//   1. username unknown               — burn dummy, 401
//   2. account exists but disabled    — burn dummy, 401
//   3. account locked by throttle     — burn dummy, 401
//   4. password incorrect             — argon2id ran in Verify, 401
// The pre-bundle handler emitted 429 + Retry-After on the locked case,
// which let an attacker probe "is THIS account currently in a throttle
// lockout?" (an enumeration oracle for accounts that exist + are
// actively under attack). The throttle is still enforced server-side;
// only the response is collapsed. Step-2 (/auth/totp/verify and
// /auth/recovery-code/verify) keeps the 429 behaviour because the
// partial-session token already proved account existence in step 1.

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
		// Bundle 1 / Fix 6: collapse every failure mode here into 401
		// bad_credentials. The pre-bundle handler emitted 429 +
		// Retry-After when Verify returned factor_locked, which let an
		// attacker probe lockout state per-username. We also burn an
		// argon2id dummy when the underlying error short-circuited
		// before argon2 ran (ErrPasswordNotSet, factor_locked) so the
		// wall-clock cost remains constant across all four failure
		// modes. Lockout is still enforced server-side; only the
		// response is collapsed.
		ae := authn.AsAuthError(err)
		isFactorLocked := ae != nil && ae.Code == "factor_locked"
		if errors.Is(err, password.ErrPasswordNotSet) || isFactorLocked {
			s.passwordStore.VerifyAgainstDummy(r.Context(), body.Password)
		}
		// password.ErrPasswordIncorrect already ran argon2id in Verify, so
		// no dummy burn needed. Any other *authn.AuthError (rate_limited
		// from CheckLocked's row scan error path, etc.) — likewise no
		// extra burn; fall through to bad_credentials.
		writeAuthErr(w, authn.ErrBadCredentials())
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
	// Re-check account state after consuming the partial-session token. An
	// admin disabling the account between step-1 (/begin) and step-2
	// (/verify) must prevent session issuance. Pre-bundle we trusted the
	// /begin disabled check; that left a window in which a disabled account
	// could still complete login. partial_session_invalid (not
	// account_disabled) — the partial token's underlying state changed, and
	// the spec D3 enumeration guard at /begin doesn't apply here (the
	// caller already proved password possession in step 1).
	if acct, err := s.accountLookupQ().GetAccountByID(r.Context(), partial.AccountID); err != nil || acct.Disabled {
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
	// Re-check account state after consuming the partial-session token —
	// see comment in handleTOTPVerifyHTTP. Disabled-mid-flow must NOT
	// receive a session.
	if acct, err := s.accountLookupQ().GetAccountByID(r.Context(), partial.AccountID); err != nil || acct.Disabled {
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

// consumePartialSession atomically retrieves and removes the KV entry via
// the store's Pop primitive. Single-use is enforced by the store itself —
// the pre-bundle Get-then-Del raced under concurrency, letting two callers
// observe the same value before either Del fired. Pop closes that gap:
// only one concurrent caller sees the value; the rest get ErrKeyNotFound.
// Returns the deserialized partial-session payload or an error if the
// token is missing / expired / already consumed.
func (s *Server) consumePartialSession(ctx context.Context, token string) (*partialSession, error) {
	raw, err := s.kvStore.Pop(ctx, partialSessionKey(token))
	if err != nil {
		return nil, err
	}
	var ps partialSession
	if err := json.Unmarshal([]byte(raw), &ps); err != nil {
		return nil, err
	}
	return &ps, nil
}

// accountLookupQ returns the query surface for the post-partial-session
// disabled re-check. Falls back to s.queries when no test override is
// installed (production path).
func (s *Server) accountLookupQ() accountLookupQueries {
	if s.accountLookup != nil {
		return s.accountLookup
	}
	return s.queries
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
