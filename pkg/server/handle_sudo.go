// Package server — handle_sudo.go
//
// Sudo mode: re-prove possession of an enrolled factor (webauthn or
// password+TOTP) to elevate the current session for a short window.
// Sensitive /me actions require the gate.
//
// Threat model: a session cookie stolen via XSS or a malicious browser
// extension grants attacker-controlled access to /me. Without sudo the
// attacker could approve their own pairing, add a backup credential, or
// rotate factors. Sudo forces a fresh proof — biometric-bound passkey or
// password+TOTP — for each elevation window. Cookie theft alone no longer
// suffices for the gated actions.
//
// v0.2 extends the v0.1 webauthn-only flow to two methods so password+TOTP
// accounts (which the v0.1 flow excluded entirely) can also elevate. The
// chosen method is stashed at /begin in `sudo_intent:<session_id>` (5-min
// TTL) and read at /complete to dispatch the verification.
//
// recovery_code is INTENTIONALLY EXCLUDED from sudo (recovery ceremony
// hardening, 2026-05-28). NIST SP 800-63B-4 §5.2 cautions against using a
// knowledge factor for reauthentication, and a stolen session + a single
// leaked recovery code would otherwise let an attacker escalate to password
// change / revoke-password-totp. The recovery-code login path now mints a
// narrow-scope recovery_session_token and routes the user through a forced
// TOTP re-enrollment ceremony at /auth/recovery/totp/{begin,verify}.
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
// AvailableMethods (via authn.FlowQueries). Recovery codes are no longer a
// sudo factor (see package-doc rationale), so ListRecoveryCodesByAccount is
// not part of this interface — but we keep recovery-code listing in the
// embedded surface as an unused method via authn.FlowQueries so existing
// fakes can satisfy this contract without churn.
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
// account, in priority order: webauthn, password_totp. Always returns a
// non-nil slice (empty means admin-recovery-only).
//
// recovery_code is intentionally excluded — see package-doc. Recovery
// codes redeem at /auth/recovery-code/verify and route through the
// dedicated re-enrollment ceremony, not the sudo gate.
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
	case string(authn.MethodPasswordTOTP):
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
	// UV=Required (webauthnauth.LoginOptions): the sudo step-up must verify the
	// asserted user-verification flag, not just user-presence — without it a
	// UV-bound passkey could elevate with presence only (audit WACER-1).
	assertion, sessionData, err := s.webauthn.BeginLogin(wu, webauthnauth.LoginOptions()...)
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

	// Pop the intent atomically — single-use prevents a race where two
	// /complete calls in flight could both dispatch the same intent and
	// each issue a sudo grant. The webauthn-stash consume below uses Pop
	// for the same reason. On a race the loser sees ErrKeyNotFound (mapped
	// to ErrCeremonyExpired), which is the same UX as a slow user whose
	// intent already TTL-expired.
	intentRaw, err := s.kvStore.Pop(r.Context(), sudoIntentKey(sess.Data.SessionID))
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
	default:
		writeAuthErr(w, authn.ErrSudoMethodUnavailable())
	}
}

// completeSudoWebAuthn is the v0.1 sudo-finish path: FinishLogin against the
// stashed assertion state, refresh sign-count, stamp SudoUntil, audit.
func (s *Server) completeSudoWebAuthn(w http.ResponseWriter, r *http.Request, sess *authn.Session) {
	// Pop atomically: single-use webauthn assertion. Two parallel /complete
	// calls cannot both replay the same assertion-challenge — the loser
	// sees ErrKeyNotFound (→ ceremony_expired).
	raw, err := s.kvStore.Pop(r.Context(), sudoStashKey(sess.Data.SessionID))
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

	// Intent and webauthn stash were already Popped atomically by the
	// dispatcher / completeSudoWebAuthn. Best-effort Del of the stash
	// covers the orphan case where /begin used webauthn (writing a stash)
	// but /complete dispatched a different method (e.g. user changed
	// their mind, second /begin overrode the intent but left the stash).
	// Del on an absent key is a no-op.
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

// consumeFreshSudo reports whether sess holds a fresh sudo grant, consuming it
// (one-shot) when present. It writes NOTHING — the caller renders the
// sudo_required error in its transport's idiom (writeAuthErr for raw HTTP,
// huma.WriteErr for typed ops). This is THE single chokepoint for the fresh-sudo
// gate: both the raw-HTTP withFreshSudo path (registerSudoOpHTTP) and the typed
// Huma registerSudoOp path route through it, so the policy can't drift between
// the two registration styles.
func (s *Server) consumeFreshSudo(ctx context.Context, sess *authn.Session) bool {
	if sess == nil || sess.Data == nil || !sess.Data.HasFreshSudo() {
		return false
	}
	// One-shot: a single sudo elevation covers a single gated action; the user
	// re-asserts for the next one. Shrinks the stolen-cookie window further, and
	// the user already had to bio-unlock once.
	sess.Data.SudoUntil = time.Time{}
	if err := s.sessionStore.Save(ctx, sess.Account.ID, sess.Token, sess.Data); err != nil {
		// Fail CLOSED: every request re-reads SudoUntil from KV, so if the
		// cleared value did not persist the future SudoUntil keeps satisfying
		// the gate — turning the one-shot grant into "every gated action for the
		// rest of the TTL window". Deny this action rather than authorize on an
		// un-persisted clear (audit SESS-1).
		logx.WithContext(ctx).WithError(err).Warn("sudo: clear failed — denying gated action")
		return false
	}
	return true
}

// requireFreshSudo is the raw-HTTP fresh-sudo gate: it consumes the grant via
// consumeFreshSudo and, on absence, writes ErrSudoRequired (401) and returns
// true so the caller returns immediately. False means satisfied — proceed.
func (s *Server) requireFreshSudo(ctx context.Context, w http.ResponseWriter, sess *authn.Session) bool {
	if !s.consumeFreshSudo(ctx, sess) {
		writeAuthErr(w, authn.ErrSudoRequired())
		return true
	}
	return false
}
