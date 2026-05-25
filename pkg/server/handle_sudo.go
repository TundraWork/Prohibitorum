// Package server — handle_sudo.go
//
// Sudo mode: re-prove possession of an enrolled factor (webauthn,
// password+TOTP, or a recovery code) to elevate the current session for a
// short window. Sensitive /me actions require the gate.
//
// Threat model: a session cookie stolen via XSS or a malicious browser
// extension grants attacker-controlled access to /me. Without sudo the
// attacker could approve their own pairing, add a backup credential, or
// rotate factors. Sudo forces a fresh proof — biometric-bound passkey,
// password+TOTP, or single-use recovery code — for each elevation window.
// Cookie theft alone no longer suffices for the gated actions.
//
// v0.2 extends the v0.1 webauthn-only flow to three methods so password+TOTP
// accounts (which the v0.1 flow excluded entirely) can also elevate. The
// chosen method is stashed at /begin in `sudo_intent:<session_id>` (5-min
// TTL) and read at /complete to dispatch the verification.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/sirupsen/logrus"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	webauthnauth "prohibitorum/pkg/credential/webauthn"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/logx"
	sessstore "prohibitorum/pkg/session"
)

// sudoIntent is the JSON payload stashed at /begin so /complete knows which
// verification path to take and (eventually) can enforce time limits beyond
// the KV TTL if needed.
type sudoIntent struct {
	Method   string    `json:"method"`
	IssuedAt time.Time `json:"issued_at"`
}

func sudoIntentKey(sessionID string) string { return "sudo_intent:" + sessionID }

// sudoFlowQueries is the narrow query surface /me/sudo/methods needs:
// AvailableMethods (via authn.FlowQueries) + recovery code listing.
// Declared here so tests can stub it without standing up the full
// sqlc-generated *db.Queries. NewServer wires it to s.queries.
type sudoFlowQueries interface {
	authn.FlowQueries
	ListRecoveryCodesByAccount(ctx context.Context, accountID int32) ([]db.RecoveryCode, error)
}

// ----- GET /me/sudo/methods ----------------------------------------------

func (s *Server) handleSudoMethodsHTTP(w http.ResponseWriter, r *http.Request) {
	sess := authn.SessionFromContext(r.Context())
	methods := s.availableSudoMethods(r.Context(), sess.Account.ID)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"methods": methods})
}

// availableSudoMethods returns the elevation methods enrolled for the
// account, in priority order: webauthn, password_totp, recovery_code.
// Always returns a non-nil slice (empty means admin-recovery-only).
func (s *Server) availableSudoMethods(ctx context.Context, accountID int32) []string {
	out := []string{}
	q := s.sudoFlowQ()
	methods, err := authn.AvailableMethods(ctx, q, accountID)
	if err != nil && !errors.Is(err, authn.ErrNoUsableMethod) {
		logx.WithContext(ctx).WithError(err).Warn("sudo: AvailableMethods")
	}
	for _, m := range methods {
		// Federation isn't a sudo factor — it doesn't re-prove possession of
		// anything held by the user. Skip it here even if AvailableMethods
		// surfaces it for the login UI.
		if m == authn.MethodWebAuthn || m == authn.MethodPasswordTOTP {
			out = append(out, string(m))
		}
	}
	if rows, err := q.ListRecoveryCodesByAccount(ctx, accountID); err == nil && len(rows) > 0 {
		out = append(out, "recovery_code")
	}
	return out
}

// sudoFlowQ returns the query surface for the /me/sudo/methods computation.
// Defaults to s.queries; tests inject a fake via s.sudoFlowOverride.
func (s *Server) sudoFlowQ() sudoFlowQueries {
	if s.sudoFlowOverride != nil {
		return s.sudoFlowOverride
	}
	return s.queries
}

// ----- POST /me/sudo/begin -----------------------------------------------

func (s *Server) handleSudoBeginHTTP(w http.ResponseWriter, r *http.Request) {
	sess := authn.SessionFromContext(r.Context())
	if s.rateLimit(w, r, "sudo:acct:"+sess.Data.SessionID, 10, time.Minute) {
		return
	}

	var body struct {
		Method string `json:"method"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Method == "" {
		writeAuthErr(w, authn.ErrSudoMethodUnavailable())
		return
	}

	available := s.availableSudoMethods(r.Context(), sess.Account.ID)
	if !slices.Contains(available, body.Method) {
		writeAuthErr(w, authn.ErrSudoMethodUnavailable())
		return
	}

	intent := sudoIntent{Method: body.Method, IssuedAt: time.Now().UTC()}
	payload, err := json.Marshal(intent)
	if err != nil {
		writeAuthErr(w, fmt.Errorf("sudo/begin: marshal intent: %w", err))
		return
	}
	if err := s.kvStore.SetEx(r.Context(), sudoIntentKey(sess.Data.SessionID), string(payload), 5*time.Minute); err != nil {
		writeAuthErr(w, fmt.Errorf("sudo/begin: setex intent: %w", err))
		return
	}

	switch body.Method {
	case string(authn.MethodWebAuthn):
		s.beginSudoWebAuthn(w, r, sess)
	case string(authn.MethodPasswordTOTP), "recovery_code":
		// No challenge — client submits credentials directly at /complete.
		w.WriteHeader(http.StatusNoContent)
	default:
		// Defensive: availableSudoMethods would have rejected this, but
		// keep the surface narrow.
		writeAuthErr(w, authn.ErrSudoMethodUnavailable())
	}
}

// beginSudoWebAuthn runs the v0.1 WebAuthn assertion-challenge ceremony. The
// resulting SessionData is stashed under `webauthn_ceremony:sudo:<sid>`
// alongside the method intent so /complete can pick the right verifier.
func (s *Server) beginSudoWebAuthn(w http.ResponseWriter, r *http.Request, sess *authn.Session) {
	// Require the caller to assert with one of their own existing
	// credentials — narrower than discoverable login so a different
	// account's authenticator can't satisfy the gate.
	creds, err := s.queries.ListCredentialsByAccount(r.Context(), sess.Account.ID)
	if err != nil {
		writeAuthErr(w, fmt.Errorf("sudo/begin: list creds: %w", err))
		return
	}
	if len(creds) == 0 {
		// availableSudoMethods would normally prevent this, but tolerate a
		// race (admin revoked the credential between methods-check and
		// begin) by failing the same way v0.1 did.
		writeAuthErr(w, authn.ErrSudoRequired())
		return
	}
	wu := &webauthnauth.WebAuthnAccount{Account: sess.Account, Credentials: creds}
	assertion, sessionData, err := s.webauthn.BeginLogin(wu)
	if err != nil {
		writeAuthErr(w, webauthnauth.MapLoginCeremonyError(r.Context(), err))
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
	sess := authn.SessionFromContext(r.Context())
	if s.rateLimit(w, r, "sudo:acct:"+sess.Data.SessionID, 10, time.Minute) {
		return
	}

	intentRaw, err := s.kvStore.Get(r.Context(), sudoIntentKey(sess.Data.SessionID))
	if err != nil {
		writeAuthErr(w, authn.ErrCeremonyExpired())
		return
	}
	var intent sudoIntent
	if err := json.Unmarshal([]byte(intentRaw), &intent); err != nil {
		writeAuthErr(w, authn.ErrCeremonyState())
		return
	}

	switch intent.Method {
	case string(authn.MethodWebAuthn):
		s.completeSudoWebAuthn(w, r, sess)
	case string(authn.MethodPasswordTOTP):
		s.completeSudoPasswordTOTP(w, r, sess)
	case "recovery_code":
		s.completeSudoRecoveryCode(w, r, sess)
	default:
		writeAuthErr(w, authn.ErrSudoMethodUnavailable())
	}
}

// completeSudoWebAuthn is the v0.1 sudo-finish path: FinishLogin against the
// stashed assertion state, refresh sign-count, stamp SudoUntil, audit.
func (s *Server) completeSudoWebAuthn(w http.ResponseWriter, r *http.Request, sess *authn.Session) {
	raw, err := s.kvStore.Get(r.Context(), sudoStashKey(sess.Data.SessionID))
	if err != nil {
		writeAuthErr(w, authn.ErrCeremonyExpired())
		return
	}
	var sessionData webauthn.SessionData
	if err := json.Unmarshal([]byte(raw), &sessionData); err != nil {
		writeAuthErr(w, authn.ErrCeremonyState())
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
	wu := &webauthnauth.WebAuthnAccount{Account: sess.Account, Credentials: creds}
	credential, err := s.webauthn.FinishLogin(wu, sessionData, r)
	if err != nil {
		writeAuthErr(w, webauthnauth.MapLoginCeremonyError(r.Context(), err))
		return
	}

	// Persist the new sign count, mirroring /auth/login/complete's behaviour
	// so clone-authenticator detection works across both surfaces.
	credRowID := matchCredentialRowID(creds, credential.ID)
	if credRowID != 0 {
		newCount := int64(credential.Authenticator.SignCount)
		for _, c := range creds {
			if c.ID == credRowID && newCount < c.SignCount {
				_ = s.queries.SetCredentialCloneWarning(r.Context(), credRowID)
				logx.WithContext(r.Context()).WithFields(logrus.Fields{
					"event":         "auth.clone_warning",
					"account_id":    sess.Account.ID,
					"credential_id": credRowID,
					"old_count":     c.SignCount,
					"new_count":     newCount,
				}).Warn("auth")
				break
			}
		}
		_ = s.queries.UpdateCredentialUsage(r.Context(), db.UpdateCredentialUsageParams{
			ID:        credRowID,
			AccountID: sess.Account.ID,
			SignCount: newCount,
		})
	}

	s.stampSudoUntil(w, r, sess, string(authn.MethodWebAuthn))
}

// completeSudoPasswordTOTP verifies password first, then TOTP. Password
// failure short-circuits the TOTP check (per spec D6) so the password
// throttle counts but the TOTP throttle stays clean.
func (s *Server) completeSudoPasswordTOTP(w http.ResponseWriter, r *http.Request, sess *authn.Session) {
	var body struct {
		CurrentPassword string `json:"current_password"`
		TOTPCode        string `json:"totp_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.CurrentPassword == "" || body.TOTPCode == "" {
		writeAuthErr(w, authn.ErrBadCredentials())
		return
	}

	if err := s.passwordStore.Verify(r.Context(), sess.Account.ID, body.CurrentPassword); err != nil {
		// factor_locked / rate-limited surface their own status (429).
		if ae := authn.AsAuthError(err); ae != nil {
			writeAuthErr(w, ae)
			return
		}
		// Sentinel collapse: ErrPasswordIncorrect / ErrPasswordNotSet →
		// generic 401 so /complete doesn't leak which factor missed.
		writeAuthErr(w, authn.ErrBadCredentials())
		return
	}

	if _, err := s.totpStore.Verify(r.Context(), sess.Account.ID, body.TOTPCode); err != nil {
		if ae := authn.AsAuthError(err); ae != nil {
			writeAuthErr(w, ae)
			return
		}
		writeAuthErr(w, authn.ErrBadCredentials())
		return
	}

	s.stampSudoUntil(w, r, sess, string(authn.MethodPasswordTOTP))
}

// completeSudoRecoveryCode consumes a single recovery code. Same single-use
// semantics as /auth/recovery-code/verify: the code is marked used_at
// regardless of subsequent failures in this request.
func (s *Server) completeSudoRecoveryCode(w http.ResponseWriter, r *http.Request, sess *authn.Session) {
	var body struct {
		RecoveryCode string `json:"recovery_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.RecoveryCode == "" {
		writeAuthErr(w, authn.ErrBadCredentials())
		return
	}
	ip := sessstore.ClientIP(r, s.config.TrustProxy)
	if err := s.totpStore.VerifyRecoveryCode(r.Context(), sess.Account.ID, body.RecoveryCode, sess.Data.SessionID, ip); err != nil {
		if ae := authn.AsAuthError(err); ae != nil {
			writeAuthErr(w, ae)
			return
		}
		writeAuthErr(w, authn.ErrBadCredentials())
		return
	}
	s.stampSudoUntil(w, r, sess, "recovery_code")
}

// stampSudoUntil writes SudoUntil = now + Auth.SudoTTL onto the live session,
// clears any KV ceremony state for this session, and emits the
// sudo_granted audit record.
func (s *Server) stampSudoUntil(w http.ResponseWriter, r *http.Request, sess *authn.Session, method string) {
	current, _, err := s.sessionStore.Load(r.Context(), sess.Account.ID, sess.Token, sessstore.ClientIP(r, s.config.TrustProxy), r.UserAgent())
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	current.SudoUntil = time.Now().Add(s.config.Auth.SudoTTL)
	if err := s.sessionStore.Save(r.Context(), sess.Account.ID, sess.Token, current); err != nil {
		writeAuthErr(w, fmt.Errorf("sudo/complete: save: %w", err))
		return
	}

	_ = s.kvStore.Del(r.Context(), sudoIntentKey(sess.Data.SessionID))
	_ = s.kvStore.Del(r.Context(), sudoStashKey(sess.Data.SessionID))

	if s.Audit != nil {
		accountID := sess.Account.ID
		_ = s.Audit.Record(r.Context(), audit.Record{
			AccountID: &accountID,
			Factor:    audit.FactorSession,
			Event:     "sudo_granted",
			IP:        audit.ParseIPOrNil(sessstore.ClientIP(r, s.config.TrustProxy)),
			UserAgent: r.UserAgent(),
			Detail:    map[string]any{"method": method},
		})
	}

	logx.WithContext(r.Context()).WithFields(logrus.Fields{
		"event":      "auth.sudo_granted",
		"account_id": sess.Account.ID,
		"session_id": sess.Data.SessionID,
		"method":     method,
		"client_ip":  sessstore.ClientIP(r, s.config.TrustProxy),
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
func (s *Server) requireFreshSudo(ctx context.Context, w http.ResponseWriter, sess *authn.Session) bool {
	if sess == nil || sess.Data == nil || !sess.Data.HasFreshSudo() {
		writeAuthErr(w, authn.ErrSudoRequired())
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
