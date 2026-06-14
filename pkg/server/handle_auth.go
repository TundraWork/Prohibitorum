package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/jackc/pgx/v5"
	"github.com/sirupsen/logrus"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/avatar"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
	webauthnauth "prohibitorum/pkg/credential/webauthn"
	"prohibitorum/pkg/logx"
	sessstore "prohibitorum/pkg/session"
)

// ceremonyTTL is how long a /begin's KV stash survives before /complete must claim it.
const ceremonyTTL = 5 * time.Minute

// newCeremonyToken returns a URL-safe random token for the ceremony cookie.
func newCeremonyToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("ceremony token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// sessionView projects a db.Account into the response shape of GET /me and
// POST /auth/login/complete. origin is s.config.PublicOrigins[0] (or "").
func (s *Server) sessionView(a *db.Account) contract.SessionView {
	v := contract.SessionView{
		ID:          a.ID,
		Username:    a.Username,
		DisplayName: a.DisplayName,
		Role:        a.Role,
		Attributes:  decodeAttributes(a.Attributes),
	}
	origin := ""
	if s.config != nil && len(s.config.PublicOrigins) > 0 {
		origin = s.config.PublicOrigins[0]
	}
	if u := avatar.AccountURL(*a, origin); u != "" {
		v.AvatarURL = &u
	}
	if a.AvatarSource.Valid {
		src := a.AvatarSource.String
		v.AvatarSource = &src
	}
	if origin != "" {
		// avatarSourceUrls is best-effort: a failed list read degrades gracefully
		// (the field is simply omitted) rather than failing the whole /me response.
		if rows, lerr := s.avatarQ().ListAvatarSourcesByAccount(context.Background(), a.ID); lerr == nil && len(rows) > 0 {
			urls := make(map[string]string, len(rows))
			for _, row := range rows {
				if u := avatar.SourceURL(a.OidcSubject.String(), row.Source, row.Etag.String, origin); u != "" {
					urls[row.Source] = u
				}
			}
			if len(urls) > 0 {
				v.AvatarSourceUrls = urls
			}
		}
	}
	return v
}

// writeAuthErr serializes an *authn.AuthError onto a raw http.ResponseWriter
// using the project's error envelope. When the AuthError carries a
// RetryAfter duration (rate-limit, factor lockout), the header is emitted
// as integer seconds, rounded up so a sub-second remainder still nudges the
// client past the lockout boundary.
func writeAuthErr(w http.ResponseWriter, err error) {
	ae := authn.AsAuthError(err)
	if ae == nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if ae.RetryAfter > 0 {
		secs := int((ae.RetryAfter + time.Second - 1) / time.Second)
		if secs < 1 {
			secs = 1
		}
		w.Header().Set("Retry-After", fmt.Sprintf("%d", secs))
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(ae.Status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"message": ae.Message,
		"code":    ae.Code,
		"details": []string{},
	})
}

// matchCredentialRowID finds the db.WebauthnCredential row whose CredentialID
// equals the raw credential ID returned by the authenticator. Returns 0 if not
// found (should never happen — FinishPasskeyLogin already verified membership).
func matchCredentialRowID(creds []db.WebauthnCredential, rawID []byte) int32 {
	for _, c := range creds {
		if bytes.Equal(c.CredentialID, rawID) {
			return c.ID
		}
	}
	return 0
}

// ----- GET /auth/status (typed) --------------------------------------------

type authStatusOut struct {
	Body contract.AuthStatus
}

func (s *Server) handleAuthStatus(ctx context.Context, _ *struct{}) (*authStatusOut, error) {
	bootstrapped, err := s.queries.HasAnyActiveAdmin(ctx)
	if err != nil {
		return nil, fmt.Errorf("auth status: %w", err)
	}
	return &authStatusOut{Body: contract.AuthStatus{Bootstrapped: bootstrapped}}, nil
}

// ----- POST /auth/login/begin (raw chi) ------------------------------------

func (s *Server) handleLoginBeginHTTP(w http.ResponseWriter, r *http.Request) {
	// Per-IP cap on the unauthenticated login ceremony — bounds ceremony-spam
	// (KV writes) and WebAuthn-verification cost (audit SESS-3).
	if s.rateLimit(w, r, "login:ip:"+sessstore.ClientIP(r, s.config.TrustProxy), loginIPLimit, authIPWindow) {
		return
	}

	bootstrapped, err := s.queries.HasAnyActiveAdmin(r.Context())
	if err != nil {
		writeAuthErr(w, fmt.Errorf("login/begin: %w", err))
		return
	}
	if !bootstrapped {
		writeAuthErr(w, authn.ErrNotBootstrapped())
		return
	}

	assertion, sessionData, err := s.webauthn.BeginDiscoverableLogin(webauthnauth.LoginOptions()...)
	if err != nil {
		writeAuthErr(w, webauthnauth.MapLoginCeremonyError(r.Context(), err))
		return
	}

	token, err := newCeremonyToken()
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	payload, err := json.Marshal(sessionData)
	if err != nil {
		writeAuthErr(w, fmt.Errorf("login/begin marshal: %w", err))
		return
	}
	if err := s.kvStore.SetEx(r.Context(), "webauthn_ceremony:login:"+token, string(payload), ceremonyTTL); err != nil {
		writeAuthErr(w, fmt.Errorf("login/begin setex: %w", err))
		return
	}

	http.SetCookie(w, sessstore.CeremonyCookie(s.config, r, token))
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(assertion.Response)
}

// ----- POST /auth/login/complete (raw chi) ---------------------------------

func (s *Server) handleLoginCompleteHTTP(w http.ResponseWriter, r *http.Request) {
	// Per-IP cap on the login ceremony — shares the begin budget so a ceremony
	// (begin+complete) counts together; bounds WebAuthn-verify + DB cost on the
	// unauthenticated surface (audit SESS-3).
	if s.rateLimit(w, r, "login:ip:"+sessstore.ClientIP(r, s.config.TrustProxy), loginIPLimit, authIPWindow) {
		return
	}

	cer, err := r.Cookie(sessstore.CeremonyCookieName)
	if err != nil || cer.Value == "" {
		writeAuthErr(w, authn.ErrCeremonyMissing())
		return
	}
	// Pop the ceremony stash atomically: single-use prevents an attacker
	// who has captured a WebAuthn assertion (e.g. via a proxy / replay)
	// from completing the login twice against the same challenge state.
	// The pre-bundle Get-then-Del race let two concurrent calls both
	// observe the value; one would issue a session and the other would
	// retry FinishPasskeyLogin with the same SessionData (which would
	// likely fail authenticator-side checks but constituted a wider
	// attack surface than necessary). On a race the loser sees
	// ErrKeyNotFound → ceremony_expired, same UX as a TTL miss.
	raw, err := s.kvStore.Pop(r.Context(), "webauthn_ceremony:login:"+cer.Value)
	if err != nil {
		// kv.ErrKeyNotFound or wrapped — both surface as expired ceremony.
		logx.WithContext(r.Context()).WithFields(logrus.Fields{
			"event":     "auth.login_failure",
			"reason":    "ceremony_state_missing",
			"client_ip": sessstore.ClientIP(r, s.config.TrustProxy),
		}).Warn("auth")
		writeAuthErr(w, authn.ErrCeremonyExpired())
		return
	}
	var sessionData webauthn.SessionData
	if err := json.Unmarshal([]byte(raw), &sessionData); err != nil {
		logx.WithContext(r.Context()).WithFields(logrus.Fields{
			"event":     "auth.login_failure",
			"reason":    "ceremony_state_corrupt",
			"client_ip": sessstore.ClientIP(r, s.config.TrustProxy),
		}).Warn("auth")
		writeAuthErr(w, authn.ErrCeremonyState())
		return
	}

	// FinishPasskeyLogin resolves the user via our handler and verifies the
	// assertion. The handler is called with (rawID, userHandle); we look up
	// the account by webauthn_user_handle and return a WebAuthnAccount adapter.
	var resolvedAccount db.Account
	var resolvedCreds []db.WebauthnCredential
	handler := func(_ /*rawID*/, userHandle []byte) (webauthn.User, error) {
		a, err := s.queries.GetAccountByWebauthnUserHandle(r.Context(), userHandle)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, fmt.Errorf("unknown user handle")
			}
			return nil, err
		}
		creds, err := s.queries.ListCredentialsByAccount(r.Context(), a.ID)
		if err != nil {
			return nil, err
		}
		resolvedAccount = a
		resolvedCreds = creds
		return &webauthnauth.WebAuthnAccount{Account: &a, Credentials: creds}, nil
	}

	_, credential, err := s.webauthn.FinishPasskeyLogin(handler, sessionData, r)
	if err != nil {
		writeAuthErr(w, webauthnauth.MapLoginCeremonyError(r.Context(), err))
		return
	}
	if resolvedAccount.ID == 0 {
		logx.WithContext(r.Context()).WithFields(logrus.Fields{
			"event":     "auth.login_failure",
			"reason":    "no_account",
			"client_ip": sessstore.ClientIP(r, s.config.TrustProxy),
		}).Warn("auth")
		writeAuthErr(w, authn.ErrLoginAccountNotFound())
		return
	}
	if resolvedAccount.Disabled {
		logx.WithContext(r.Context()).WithFields(logrus.Fields{
			"event":      "auth.login_failure",
			"reason":     "account_disabled",
			"account_id": resolvedAccount.ID,
			"client_ip":  sessstore.ClientIP(r, s.config.TrustProxy),
		}).Warn("auth")
		writeAuthErr(w, authn.ErrAccountDisabled())
		return
	}

	// Update credential usage (sign_count + last_used_at), then issue session.
	// Check for sign-count regression (potential cloned authenticator) before
	// updating — stamp clone_warning_at on the credential row if detected.
	// Do not reject the login; the stamp is for admin forensics.
	credRowID := matchCredentialRowID(resolvedCreds, credential.ID)
	if credRowID != 0 {
		// Find the old sign count from the resolved creds list.
		newCount := int64(credential.Authenticator.SignCount)
		for _, c := range resolvedCreds {
			if c.ID == credRowID && newCount < c.SignCount {
				_ = s.queries.SetCredentialCloneWarning(r.Context(), credRowID)
				logx.WithContext(r.Context()).WithFields(logrus.Fields{
					"event":         "auth.clone_warning",
					"account_id":    resolvedAccount.ID,
					"credential_id": credRowID,
					"old_count":     c.SignCount,
					"new_count":     newCount,
				}).Warn("auth")
				break
			}
		}
		_ = s.queries.UpdateCredentialUsage(r.Context(), db.UpdateCredentialUsageParams{
			ID:        credRowID,
			AccountID: resolvedAccount.ID,
			SignCount:  newCount,
		})
	}
	// Ceremony stash was Popped atomically above; no Del needed here.
	http.SetCookie(w, sessstore.ClearedCeremonyCookie(s.config, r))

	ip := sessstore.ClientIP(r, s.config.TrustProxy)
	token, _, err := s.sessionStore.Issue(r.Context(), resolvedAccount.ID, ip, r.UserAgent(), []string{"hwk"}, nil)
	if err != nil {
		writeAuthErr(w, fmt.Errorf("session issue: %w", err))
		return
	}
	http.SetCookie(w, sessstore.FreshSessionCookie(s.config, r, resolvedAccount.ID, token, s.config.SessionTTL))

	logx.WithContext(r.Context()).WithFields(logrus.Fields{
		"event":      "auth.login_success",
		"account_id": resolvedAccount.ID,
		"username":   resolvedAccount.Username,
		"client_ip":  sessstore.ClientIP(r, s.config.TrustProxy),
	}).Info("auth")

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.sessionView(&resolvedAccount))
}

// ----- POST /auth/logout (raw chi) -----------------------------------------

func (s *Server) handleLogoutHTTP(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessstore.SessionCookieNameFor(s.config)); err == nil && c.Value != "" {
		if id, tok, ok := sessstore.ParseCookieValue(c.Value); ok {
			_ = s.sessionStore.Revoke(r.Context(), id, tok)
			logx.WithContext(r.Context()).WithFields(logrus.Fields{
				"event":      "auth.logout",
				"account_id": id,
				"client_ip":  sessstore.ClientIP(r, s.config.TrustProxy),
			}).Info("auth")
		}
	}
	http.SetCookie(w, sessstore.ClearedSessionCookie(s.config, r))
	w.WriteHeader(http.StatusNoContent)
}
