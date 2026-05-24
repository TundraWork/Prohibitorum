// Package server — handle_sudo.go
//
// Sudo mode: re-prove possession of the account's passkey to elevate the
// current session for a short window. Sensitive actions (currently only
// /me/devices/pair/approve) require the gate.
//
// Threat model: a session cookie stolen via XSS or a malicious browser
// extension grants attacker-controlled access to /me. Without sudo the
// attacker could approve their own pairing and persist a backup credential.
// Sudo forces a fresh WebAuthn assertion — which requires the user's
// authenticator + biometric — for each elevation window. Cookie theft alone
// no longer suffices for the gated actions.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/sirupsen/logrus"

	"prohibitorum/pkg/auth"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/logx"
)

// ----- POST /me/sudo/begin -----------------------------------------------

func (s *Server) handleSudoBeginHTTP(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	if sess == nil {
		writeAuthErr(w, auth.ErrNoSession())
		return
	}
	if s.rateLimit(w, r, "sudo:acct:"+sess.Data.SessionID, 10, time.Minute) {
		return
	}

	// Require the caller to assert with one of their own existing
	// credentials — narrower than discoverable login so a different
	// account's authenticator can't satisfy the gate.
	creds, err := s.queries.ListCredentialsByAccount(r.Context(), sess.Account.ID)
	if err != nil {
		writeAuthErr(w, fmt.Errorf("sudo/begin: list creds: %w", err))
		return
	}
	if len(creds) == 0 {
		// No credentials — nothing to assert against. Surface as a state
		// error so the dashboard can prompt registration via the standard
		// /me/credentials/register flow.
		writeAuthErr(w, auth.ErrSudoRequired())
		return
	}
	wu := &auth.WebAuthnAccount{Account: sess.Account, Credentials: creds}
	assertion, sessionData, err := s.webauthn.BeginLogin(wu)
	if err != nil {
		writeAuthErr(w, auth.MapLoginCeremonyError(r.Context(), err))
		return
	}
	payload, err := json.Marshal(sessionData)
	if err != nil {
		writeAuthErr(w, fmt.Errorf("sudo/begin: marshal: %w", err))
		return
	}
	// Per-session ceremony stash; key on SessionID so two browser tabs of
	// the same account don't clobber each other.
	if err := s.kvStore.SetEx(r.Context(), sudoStashKey(sess.Data.SessionID), string(payload), 5*time.Minute); err != nil {
		writeAuthErr(w, fmt.Errorf("sudo/begin: setex: %w", err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(assertion.Response)
}

// ----- POST /me/sudo/complete --------------------------------------------

func (s *Server) handleSudoCompleteHTTP(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	if sess == nil {
		writeAuthErr(w, auth.ErrNoSession())
		return
	}
	if s.rateLimit(w, r, "sudo:acct:"+sess.Data.SessionID, 10, time.Minute) {
		return
	}

	raw, err := s.kvStore.Get(r.Context(), sudoStashKey(sess.Data.SessionID))
	if err != nil {
		writeAuthErr(w, auth.ErrCeremonyExpired())
		return
	}
	var sessionData webauthn.SessionData
	if err := json.Unmarshal([]byte(raw), &sessionData); err != nil {
		writeAuthErr(w, auth.ErrCeremonyState())
		return
	}

	// Fetch creds again — between begin and complete, deletions/renames
	// could have happened (very unlikely within the 5-min window, but
	// cheap to refresh).
	creds, err := s.queries.ListCredentialsByAccount(r.Context(), sess.Account.ID)
	if err != nil {
		writeAuthErr(w, fmt.Errorf("sudo/complete: list creds: %w", err))
		return
	}
	wu := &auth.WebAuthnAccount{Account: sess.Account, Credentials: creds}
	credential, err := s.webauthn.FinishLogin(wu, sessionData, r)
	if err != nil {
		writeAuthErr(w, auth.MapLoginCeremonyError(r.Context(), err))
		return
	}

	// Persist the new sign count, mirroring /auth/login/complete's behaviour
	// so clone-authenticator detection works across both surfaces.
	credRowID := matchCredentialRowID(creds, credential.ID)
	if credRowID != 0 {
		_ = s.queries.UpdateCredentialUsage(r.Context(), db.UpdateCredentialUsageParams{
			ID:        credRowID,
			AccountID: sess.Account.ID,
			SignCount: int64(credential.Authenticator.SignCount),
		})
	}

	// Stamp SudoUntil on the live session. Reload from KV to avoid
	// stamping over a stale snapshot if another in-flight request mutated
	// the entry.
	now := time.Now()
	current, _, err := s.sessionStore.Load(r.Context(), sess.Account.ID, sess.Token, auth.ClientIP(r, s.config.TrustProxy), r.UserAgent())
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	current.SudoUntil = now.Add(auth.SudoTTL)
	if err := s.sessionStore.Save(r.Context(), sess.Account.ID, sess.Token, current); err != nil {
		writeAuthErr(w, fmt.Errorf("sudo/complete: save: %w", err))
		return
	}

	_ = s.kvStore.Del(r.Context(), sudoStashKey(sess.Data.SessionID))

	logx.WithContext(r.Context()).WithFields(logrus.Fields{
		"event":      "auth.sudo_granted",
		"account_id": sess.Account.ID,
		"session_id": sess.Data.SessionID,
		"client_ip":  auth.ClientIP(r, s.config.TrustProxy),
	}).Info("auth")

	w.WriteHeader(http.StatusNoContent)
}

func sudoStashKey(sessionID string) string {
	return "webauthn_ceremony:sudo:" + sessionID
}

// requireFreshSudo writes an ErrSudoRequired (401) when the session is
// missing or its SudoUntil has lapsed. Returns true on FAIL — caller
// should return immediately. False means the gate is satisfied and the
// caller may proceed; the grant is consumed (one gated action per grant).
func (s *Server) requireFreshSudo(ctx context.Context, w http.ResponseWriter, sess *auth.Session) bool {
	if sess == nil || sess.Data == nil || !sess.Data.HasFreshSudo() {
		writeAuthErr(w, auth.ErrSudoRequired())
		return true
	}
	// One-shot: consume the grant. A single sudo elevation covers a single
	// gated action; the user re-asserts for the next one. Cheaper for the
	// attacker model (stolen cookie windows shrink further) and the user
	// already had to bio-unlock once.
	sess.Data.SudoUntil = time.Time{}
	if err := s.sessionStore.Save(ctx, sess.Account.ID, sess.Token, sess.Data); err != nil {
		// Best-effort — failing to clear means the user gets one extra
		// gated action this window. Not a security regression.
		logx.WithContext(ctx).WithError(err).Warn("sudo: clear failed")
	}
	return false
}
