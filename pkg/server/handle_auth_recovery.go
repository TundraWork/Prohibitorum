// Package server — handle_auth_recovery.go
//
// Recovery ceremony: the only path from a redeemed recovery code back to a
// real session. Replaces the pre-2026-05-28 behaviour where
// /auth/recovery-code/verify itself issued a session (which left
// recovery_code wired in as a continuous-elevation primitive — a stolen
// session + leaked recovery code could pivot to a full takeover).
//
// Flow (after /auth/password/begin + /auth/recovery-code/verify):
//
//	POST /auth/recovery/totp/begin   {recovery_session_token}
//	  → 200 {secret_base32, otpauth_uri}
//	  Wipes the old totp_credential row, inserts a fresh unconfirmed
//	  enrollment. Recovery codes are PRESERVED so the user can retry
//	  recovery with a different code if they abandon mid-ceremony.
//
//	POST /auth/recovery/totp/verify  {recovery_session_token, code}
//	  → 200 {recovery_codes: [...10 new codes]} + session cookie
//	  Atomically consumes the recovery_session_token (single-use),
//	  confirms the new TOTP, wipes the remaining old recovery codes,
//	  mints 10 fresh codes, and issues a real session with
//	  amr=["pwd","otp","mfa"].
//
// recovery_session_token semantics:
//   - 128-bit URL-safe random (shares newCeremonyToken with partial-session
//     and webauthn ceremony tokens), separate KV namespace
//     (recovery_session:<tok>). Single-use + 10-min TTL puts 128 bits
//     comfortably above the OWASP session-token floor.
//   - 10-minute TTL (longer than partial-session — user is mid-ceremony).
//   - Atomic single-use via kv.Store.Pop at /verify.
//   - /begin uses Get (non-destructive) so the user can retry /begin if they
//     fail to scan the QR; only /verify consumes the token.
//   - Scope: ONLY these two endpoints. Not a session — no /me, no sudo.

package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/credential/totp"
	sessstore "prohibitorum/pkg/session"
)

// recoverySessionTTL bounds how long the user has between burning a recovery
// code and completing the new-TOTP enrollment. Long enough for an ordinary
// "scan the QR, type the code" interaction; short enough that a leaked
// token doesn't sit around indefinitely.
const recoverySessionTTL = 10 * time.Minute

// recoverySession is the JSON payload stashed under recovery_session:<token>.
// account_id is the only state the ceremony needs; issued_at is recorded for
// audit / future expiry-tightening but isn't read by the current handlers.
// A `phase` field is intentionally omitted — the v0.2.2 ceremony only does
// TOTP recovery. A future passkey-recovery variant would add `phase: "totp"`
// vs `phase: "webauthn"` to discriminate at /begin.
type recoverySession struct {
	AccountID int32     `json:"account_id"`
	IssuedAt  time.Time `json:"issued_at"`
}

func recoverySessionKey(token string) string { return "recovery_session:" + token }

// loadRecoverySession resolves a recovery_session_token to its payload. Used
// by /begin (non-destructive lookup) — /verify uses popRecoverySession for
// atomic single-use semantics. Takes the raw token (not the prefixed key);
// the helper applies recoverySessionKey internally.
func (s *Server) loadRecoverySession(ctx context.Context, token string) (*recoverySession, error) {
	raw, err := s.kvStore.Get(ctx, recoverySessionKey(token))
	if err != nil {
		return nil, err
	}
	var rs recoverySession
	if err := json.Unmarshal([]byte(raw), &rs); err != nil {
		return nil, err
	}
	return &rs, nil
}

// popRecoverySession atomically consumes the token. Two parallel /verify
// calls with the same token: exactly one observes the value, the loser sees
// ErrKeyNotFound. This is the design's atomicity guarantee — paired with the
// fact that a failed /verify does NOT re-stash the token, so the user must
// restart from /auth/password/begin if the TOTP code was wrong.
func (s *Server) popRecoverySession(ctx context.Context, token string) (*recoverySession, error) {
	raw, err := s.kvStore.Pop(ctx, recoverySessionKey(token))
	if err != nil {
		return nil, err
	}
	var rs recoverySession
	if err := json.Unmarshal([]byte(raw), &rs); err != nil {
		return nil, err
	}
	return &rs, nil
}

// POST /api/prohibitorum/auth/recovery/totp/begin
//
// Body: {recovery_session_token}
// Response: 200 {secret_base32, otpauth_uri}
//
// Failure modes (all 401 recovery_session_invalid):
//   - missing / expired token
//   - account disabled between recovery-code redeem and now
//
// The TOTP store reset is unconditional once we reach the Begin call —
// the previous TOTP credential is wiped (audit: totp/revoke reason=recovery),
// recovery codes preserved.
func (s *Server) handleAuthRecoveryTOTPBeginHTTP(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RecoverySessionToken string `json:"recovery_session_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.RecoverySessionToken == "" {
		writeAuthErr(w, authn.ErrRecoverySessionInvalid())
		return
	}
	rs, err := s.loadRecoverySession(r.Context(), body.RecoverySessionToken)
	if err != nil {
		writeAuthErr(w, authn.ErrRecoverySessionInvalid())
		return
	}
	// Disabled-account re-check: admin may have disabled the account between
	// /auth/recovery-code/verify and /begin. Reject by collapsing to the
	// generic recovery_session_invalid (same pattern as the step-2 verifies
	// in handle_auth_password.go) — the user has already proved possession of
	// a recovery code, but the account state is what gates access.
	acct, err := s.accountLookupQ().GetAccountByID(r.Context(), rs.AccountID)
	if err != nil || acct.Disabled {
		writeAuthErr(w, authn.ErrRecoverySessionInvalid())
		return
	}

	enr, err := s.totpStore.BeginPreservingRecovery(r.Context(), acct.ID, acct.Username)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"secret_base32": enr.SecretBase32,
		"otpauth_uri":   enr.ProvisioningURI,
	})
}

// POST /api/prohibitorum/auth/recovery/totp/verify
//
// Body: {recovery_session_token, code}
// Response: 200 {recovery_codes: [...]} + Set-Cookie session
//
// On success: atomic-consume the recovery_session_token, confirm the new
// TOTP, wipe old recovery codes + mint 10 new ones inside the same tx, issue
// a real session with amr=["pwd","otp","mfa"].
//
// On TOTP failure: 401 bad_credentials. The recovery_session_token is
// ALREADY consumed (Pop above) — the user must restart from
// /auth/password/begin. This is intentional: keeping the token live for
// retry would require either a re-stash on failure (atomicity hazard) or
// a separate "verify but don't consume" path. The v0.2.2 design picks
// "single-use, restart on failure" for simplicity. The harsher UX is
// documented; see docs/superpowers/specs/2026-05-27-recovery-ceremony-design.md.
func (s *Server) handleAuthRecoveryTOTPVerifyHTTP(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RecoverySessionToken string `json:"recovery_session_token"`
		Code                 string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.RecoverySessionToken == "" || body.Code == "" {
		writeAuthErr(w, authn.ErrRecoverySessionInvalid())
		return
	}
	rs, err := s.popRecoverySession(r.Context(), body.RecoverySessionToken)
	if err != nil {
		writeAuthErr(w, authn.ErrRecoverySessionInvalid())
		return
	}
	acct, err := s.accountLookupQ().GetAccountByID(r.Context(), rs.AccountID)
	if err != nil || acct.Disabled {
		writeAuthErr(w, authn.ErrRecoverySessionInvalid())
		return
	}

	codes, err := s.totpStore.VerifyAndCommitRecovery(r.Context(), acct.ID, body.Code)
	if err != nil {
		// factor_locked / rate-limited surface through with their own status.
		// Sentinels (invalid/replay/not-set/corrupt) collapse to bad_credentials
		// — same pattern as handle_me_totp.go to avoid factor-state oracles.
		if errors.Is(err, totp.ErrTOTPCorrupt) {
			writeAuthErr(w, authn.ErrBadCredentials())
			return
		}
		if ae := authn.AsAuthError(err); ae != nil {
			writeAuthErr(w, ae)
			return
		}
		writeAuthErr(w, authn.ErrBadCredentials())
		return
	}

	// Issue a normal session. amr matches what /auth/totp/verify would have
	// emitted on a normal Password+TOTP login: the user has just proven
	// possession of (a recovery code → freshly-enrolled TOTP), which by
	// design carries the same authority as a normal MFA login. The fact
	// that the FIRST step was a recovery code is captured in the audit
	// trail (recovery_code:use + the recovery_complete revoke chain), not
	// here.
	ip := sessstore.ClientIP(r, s.config.TrustProxy)
	ua := r.UserAgent()
	token, _, err := s.sessionStore.Issue(r.Context(), acct.ID, ip, ua, []string{"pwd", "otp", "mfa"})
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	http.SetCookie(w, sessstore.FreshSessionCookie(s.config, r, acct.ID, token, s.config.SessionTTL))
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"recovery_codes": codes})
}
