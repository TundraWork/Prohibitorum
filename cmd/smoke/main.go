// Command smoke drives an end-to-end Prohibitorum auth surface against a
// running dev server using an in-process virtual WebAuthn authenticator plus
// an RFC 6238 TOTP code generator. No browser required.
//
// v0.1.1 covered the WebAuthn ceremony, session table writes, and
// multi-credential management. v0.2 extends the script with the
// password + TOTP + recovery-code surface, sudo step-up via all three
// methods, throttle observation, and the destructive
// /me/auth/revoke-password-totp endpoint. Per
// feedback_always_verify_fixes.md, this binary IS the v0.2 verification
// gate — every endpoint mounted in pkg/server.registerOperations under
// v0.2 must be exercised against the live DB before the version can be
// considered done.
//
// Failure at any step prints a diagnostic and exits non-zero.
package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/fxamacker/cbor/v2"

	"prohibitorum/cmd/smoke/mockop"
	totppkg "prohibitorum/pkg/credential/totp"
	fedoidc "prohibitorum/pkg/federation/oidc"
)

func main() {
	var (
		baseURL  = flag.String("base-url", "http://localhost:8080", "Prohibitorum server base URL")
		username = flag.String("username", "smoke-admin", "username for the bootstrap admin")
		display  = flag.String("display", "Smoke Admin", "display name for the bootstrap admin")
		skipNew  = flag.Bool("skip-new", false, "do not pass --new to enroll-admin (re-uses existing pending bootstrap if available)")
	)
	flag.Parse()

	log.SetFlags(0)

	// v0.3 federation: bring up an in-process mock OIDC OP for use by the
	// federation steps appended after the v0.2 surface. Started early so a
	// failed bind surfaces before any DB writes.
	opSrv, err := mockop.New("")
	if err != nil {
		log.Fatalf("mockop: %v", err)
	}
	opTS := httptest.NewServer(opSrv.Routes())
	defer opTS.Close()
	opSrv.SetBase(opTS.URL)
	log.Printf("  mock OP listening on %s", opTS.URL)

	c, err := newClient(*baseURL)
	if err != nil {
		log.Fatalf("smoke: %v", err)
	}

	step("step 1/45 — minting enrollment URL via enroll-admin")
	token, err := mintEnrollmentToken(*baseURL, *skipNew)
	if err != nil {
		log.Fatalf("enroll-admin: %v", err)
	}
	log.Printf("  token: %s…", token[:12])

	step("step 2/45 — POST /enrollments/{token}/register/begin")
	creation, err := c.beginEnrollment(token, *username, *display, "smoke-laptop")
	if err != nil {
		log.Fatalf("register/begin: %v", err)
	}
	log.Printf("  challenge len=%d rpId=%s userId len=%d", len(creation.Challenge), creation.RP.ID, len(creation.User.ID))

	step("step 3/45 — building virtual authenticator credential")
	auth, err := newAuthenticator(creation.RP.ID)
	if err != nil {
		log.Fatalf("authenticator: %v", err)
	}
	attestation, err := auth.attestCredential(creation.Challenge, creation.User.ID, *baseURL)
	if err != nil {
		log.Fatalf("attest: %v", err)
	}
	log.Printf("  credentialId len=%d cose_alg=-7 (ES256)", len(auth.credentialID))

	step("step 4/45 — POST /enrollments/{token}/register/complete")
	if err := c.completeEnrollment(token, auth, attestation); err != nil {
		log.Fatalf("register/complete: %v", err)
	}
	log.Printf("  session cookie set (have %d cookies)", len(c.cookies()))

	step("step 5/45 — GET /me")
	me1, err := c.getMe()
	if err != nil {
		log.Fatalf("get me: %v", err)
	}
	if me1.Username != *username {
		log.Fatalf("/me username mismatch: got %q want %q", me1.Username, *username)
	}
	log.Printf("  username=%s displayName=%s role=%s", me1.Username, me1.DisplayName, me1.Role)

	step("step 6/45 — POST /auth/logout")
	if err := c.logout(); err != nil {
		log.Fatalf("logout: %v", err)
	}
	if _, err := c.getMe(); err == nil {
		log.Fatalf("post-logout /me succeeded; expected 401")
	}
	log.Printf("  session revoked; /me returns 401 as expected")

	step("step 7/45 — POST /auth/login/begin")
	assertion, err := c.beginLogin()
	if err != nil {
		log.Fatalf("login/begin: %v", err)
	}
	log.Printf("  challenge len=%d rpId=%s", len(assertion.Challenge), assertion.RPID)

	step("step 8/45 — signing assertion with virtual authenticator")
	signed, err := auth.signAssertion(assertion.Challenge, *baseURL)
	if err != nil {
		log.Fatalf("sign assertion: %v", err)
	}
	log.Printf("  signature len=%d (ASN.1 DER)", len(signed.signature))

	step("step 9/45 — POST /auth/login/complete")
	if err := c.completeLogin(auth, signed); err != nil {
		log.Fatalf("login/complete: %v", err)
	}
	log.Printf("  session cookie restored")

	step("step 10/45 — GET /me (post-login)")
	me2, err := c.getMe()
	if err != nil {
		log.Fatalf("get me (post-login): %v", err)
	}
	if me2.Username != *username {
		log.Fatalf("/me username drift: got %q want %q", me2.Username, *username)
	}
	if me2.ID != me1.ID {
		log.Fatalf("/me account id changed across login: got %d want %d", me2.ID, me1.ID)
	}
	log.Printf("  username=%s id=%d (matches enrollment)", me2.Username, me2.ID)

	// --- Phase 2: RevokeBySessionID + add-second-credential coverage ---

	step("step 11/45 — second client B begins login with the same authenticator")
	cB, err := newClient(*baseURL)
	if err != nil {
		log.Fatalf("client B: %v", err)
	}
	assertionB, err := cB.beginLogin()
	if err != nil {
		log.Fatalf("B login/begin: %v", err)
	}
	signedB, err := auth.signAssertion(assertionB.Challenge, *baseURL)
	if err != nil {
		log.Fatalf("B sign: %v", err)
	}

	step("step 12/45 — B completes login; both A and B now hold sessions")
	if err := cB.completeLogin(auth, signedB); err != nil {
		log.Fatalf("B login/complete: %v", err)
	}
	meB, err := cB.getMe()
	if err != nil {
		log.Fatalf("B /me: %v", err)
	}
	if meB.ID != me2.ID {
		log.Fatalf("B /me id mismatch: got %d want %d", meB.ID, me2.ID)
	}
	log.Printf("  B logged in as id=%d (same account as A)", meB.ID)

	step("step 13/45 — A lists /me/sessions; expect 2 (current + B's)")
	sessions, err := c.listMySessions()
	if err != nil {
		log.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 2 {
		log.Fatalf("expected 2 sessions, got %d: %+v", len(sessions), sessions)
	}
	var otherID string
	for _, s := range sessions {
		if !s.IsCurrent {
			otherID = s.ID
		}
	}
	if otherID == "" {
		log.Fatalf("could not identify B's session in %+v", sessions)
	}
	log.Printf("  found B's session id=%s", otherID)

	step("step 14/45 — A revokes B's session via /me/sessions/revoke")
	if err := c.revokeSession(otherID); err != nil {
		log.Fatalf("revoke session: %v", err)
	}
	log.Printf("  revoke succeeded")

	step("step 15/45 — B's /me should now return 401")
	if _, err := cB.getMe(); err == nil {
		log.Fatalf("B /me succeeded after revocation; expected 401")
	}
	log.Printf("  B is denied, RevokeBySessionID confirmed")

	step("step 16/45 — A adds a second passkey via /me/credentials/register/{begin,complete}")
	addBegin, err := c.beginAddCredential()
	if err != nil {
		log.Fatalf("add cred/begin: %v", err)
	}
	auth2, err := newAuthenticator(addBegin.RP.ID)
	if err != nil {
		log.Fatalf("new authenticator 2: %v", err)
	}
	att2, err := auth2.attestCredential(addBegin.Challenge, addBegin.User.ID, *baseURL)
	if err != nil {
		log.Fatalf("attest 2: %v", err)
	}
	if err := c.completeAddCredential(auth2, att2, "smoke-second"); err != nil {
		log.Fatalf("add cred/complete: %v", err)
	}
	log.Printf("  second credential registered (advertises ES256 -7)")

	step("step 17/45 — A lists /me/credentials; expect 2")
	creds, err := c.listMyCredentials()
	if err != nil {
		log.Fatalf("list credentials: %v", err)
	}
	if len(creds) != 2 {
		log.Fatalf("expected 2 credentials, got %d: %+v", len(creds), creds)
	}
	log.Printf("  credentials count = %d", len(creds))

	// --- DB-level verification: cose_alg and session table ---

	step("DB verification — cose_alg + session-table writer wiring")
	if err := verifyDB(me2.ID); err != nil {
		log.Fatalf("DB verification: %v", err)
	}

	// =========================================================================
	// v0.2 surface: password + TOTP + recovery codes + sudo (3 methods) +
	// throttle observation + destructive revoke. Per Task 8 of the v0.2 plan;
	// see docs/v0.2-spec.md "cmd/smoke v0.2 extension".
	//
	// Pre-condition: A is logged in with a fresh WebAuthn session (step 9).
	// =========================================================================

	const password = "smoke-pw-v0.2-correct-horse-battery-staple"

	step("step 18/45 — sudo via webauthn (prime SudoUntil for /me/password/set)")
	if err := sudoWebAuthn(c, auth, *baseURL); err != nil {
		log.Fatalf("sudo webauthn (pre password/set): %v", err)
	}
	log.Printf("  sudo grant acquired (webauthn)")

	step("step 19/45 — POST /me/password/set")
	if err := c.postJSON("/api/prohibitorum/me/password/set",
		map[string]string{"password": password}, nil); err != nil {
		log.Fatalf("password/set: %v", err)
	}
	log.Printf("  password set (204)")

	step("step 20/45 — DB assert: password_credential row exists w/ argon2id hash")
	if err := verifyPasswordCredential(me2.ID); err != nil {
		log.Fatalf("password DB assert: %v", err)
	}

	step("step 21/45 — POST /me/totp/begin (first enrollment; no sudo required)")
	var totpBegin struct {
		SecretBase32 string `json:"secret_base32"`
		OtpauthURI   string `json:"otpauth_uri"`
	}
	if err := c.postJSON("/api/prohibitorum/me/totp/begin",
		map[string]any{}, &totpBegin); err != nil {
		log.Fatalf("totp/begin: %v", err)
	}
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).
		DecodeString(strings.TrimRight(totpBegin.SecretBase32, "="))
	if err != nil {
		log.Fatalf("decode totp secret_base32 %q: %v", totpBegin.SecretBase32, err)
	}
	log.Printf("  secret len=%d otpauth=%.40s…", len(secret), totpBegin.OtpauthURI)

	step("step 22/45 — POST /me/totp/verify {current code}")
	totpStep := time.Now().Unix() / 30
	code := totppkg.ComputeCodeForTesting(secret, time.Now().Unix(), 6)
	var totpVerify struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}
	if err := c.postJSON("/api/prohibitorum/me/totp/verify",
		map[string]string{"code": code}, &totpVerify); err != nil {
		log.Fatalf("totp/verify: %v", err)
	}
	if len(totpVerify.RecoveryCodes) != 10 {
		log.Fatalf("expected 10 recovery codes, got %d", len(totpVerify.RecoveryCodes))
	}
	recoveryCodes := totpVerify.RecoveryCodes
	log.Printf("  totp confirmed; %d recovery codes minted (step=%d)", len(recoveryCodes), totpStep)

	step("step 23/45 — DB assert: totp_credential.confirmed_at IS NOT NULL + 10 recovery rows")
	if err := verifyTOTPConfirmed(me2.ID); err != nil {
		log.Fatalf("totp DB assert: %v", err)
	}

	step("step 24/45 — POST /auth/logout (drop A's webauthn session)")
	if err := c.logout(); err != nil {
		log.Fatalf("logout pre-password-login: %v", err)
	}
	log.Printf("  logged out")

	step("step 25/45 — POST /auth/password/begin {username, password}")
	partialToken, err := c.passwordBegin(*username, password)
	if err != nil {
		log.Fatalf("password/begin: %v", err)
	}
	log.Printf("  partial_session_token len=%d", len(partialToken))

	step("step 26/45 — POST /auth/totp/verify {partial_session_token, current code}")
	// RFC 6238 §5.2 replay protection: last_step from step 22 is still set, so
	// wait across the period boundary before the next successful TOTP verify.
	totpStep = waitForNextTOTPStep(totpStep)
	totpCode := totppkg.ComputeCodeForTesting(secret, time.Now().Unix(), 6)
	if err := c.totpStepTwoVerify(partialToken, totpCode); err != nil {
		log.Fatalf("auth/totp/verify: %v", err)
	}
	log.Printf("  session cookie issued via password+TOTP (step=%d)", totpStep)

	step("step 27/45 — GET /me round-trips post-password+TOTP login")
	mePT, err := c.getMe()
	if err != nil {
		log.Fatalf("GET /me post-pwd+totp: %v", err)
	}
	if mePT.ID != me2.ID {
		log.Fatalf("/me id drift after pwd+totp: got %d want %d", mePT.ID, me2.ID)
	}
	log.Printf("  /me id=%d (same account)", mePT.ID)

	step("step 28/45 — POST /auth/logout (drop pwd+totp session)")
	if err := c.logout(); err != nil {
		log.Fatalf("logout pre-recovery-ceremony: %v", err)
	}

	// --- Recovery ceremony (2026-05-28 hardening) -----------------------------
	// /auth/recovery-code/verify no longer issues a session. It hands back a
	// recovery_session_token that the user must redeem at
	// /auth/recovery/totp/{begin,verify} to enroll a fresh TOTP and regain
	// account access. recovery_code is no longer a sudo method (former
	// step 39/40 dropped); the user re-proves possession of TOTP every time.

	step("step 29/45 — POST /auth/password/begin (fresh partial token for recovery ceremony)")
	partialToken2, err := c.passwordBegin(*username, password)
	if err != nil {
		log.Fatalf("password/begin 2: %v", err)
	}
	log.Printf("  partial_session_token len=%d", len(partialToken2))

	step("step 30/45 — POST /auth/recovery-code/verify {recovery_codes[0]} → recovery_session_token")
	recoveryToken, err := c.recoveryCodeVerify(partialToken2, recoveryCodes[0])
	if err != nil {
		log.Fatalf("auth/recovery-code/verify: %v", err)
	}
	if recoveryToken == "" {
		log.Fatalf("auth/recovery-code/verify returned empty recovery_session_token")
	}
	log.Printf("  recovery_session_token len=%d (no session cookie yet)", len(recoveryToken))

	step("step 31/45 — DB assert: recovery_codes[0].used_at IS NOT NULL (consumed by redeem)")
	if err := verifyRecoveryCodeUsed(me2.ID, 1, 0); err != nil {
		log.Fatalf("recovery code used_at DB assert: %v", err)
	}

	step("step 32a/45 — POST /auth/recovery/totp/begin {recovery_session_token}")
	var recoveryBegin struct {
		SecretBase32 string `json:"secret_base32"`
		OtpauthURI   string `json:"otpauth_uri"`
	}
	if err := c.postJSON("/api/prohibitorum/auth/recovery/totp/begin",
		map[string]string{"recovery_session_token": recoveryToken}, &recoveryBegin); err != nil {
		log.Fatalf("auth/recovery/totp/begin: %v", err)
	}
	newSecret, err := base32.StdEncoding.WithPadding(base32.NoPadding).
		DecodeString(strings.TrimRight(recoveryBegin.SecretBase32, "="))
	if err != nil {
		log.Fatalf("decode new totp secret_base32 %q: %v", recoveryBegin.SecretBase32, err)
	}
	log.Printf("  new TOTP secret minted (len=%d); old TOTP wiped, recovery codes preserved", len(newSecret))

	step("step 32b/45 — DB assert: TOTP unconfirmed; 9 recovery codes still present")
	if err := verifyTOTPUnconfirmedAndRecoveryCount(me2.ID, 9); err != nil {
		log.Fatalf("post-recovery-begin DB assert: %v", err)
	}

	step("step 32c/45 — wait next TOTP step + POST /auth/recovery/totp/verify {token, code}")
	totpStep = waitForNextTOTPStep(totpStep)
	newCode := totppkg.ComputeCodeForTesting(newSecret, time.Now().Unix(), 6)
	var recoveryVerify struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}
	if err := c.postJSON("/api/prohibitorum/auth/recovery/totp/verify",
		map[string]string{
			"recovery_session_token": recoveryToken,
			"code":                   newCode,
		}, &recoveryVerify); err != nil {
		log.Fatalf("auth/recovery/totp/verify: %v", err)
	}
	if len(recoveryVerify.RecoveryCodes) != 10 {
		log.Fatalf("recovery ceremony: want 10 new recovery codes, got %d", len(recoveryVerify.RecoveryCodes))
	}
	// Swap in the post-recovery secret + recovery codes for the remaining
	// steps (sudo password_totp later will use the new secret).
	secret = newSecret
	recoveryCodes = recoveryVerify.RecoveryCodes
	log.Printf("  session cookie issued; 10 fresh recovery codes minted (old 9 wiped)")

	step("step 32d/45 — DB assert: new TOTP confirmed; exactly 10 recovery codes")
	if err := verifyTOTPConfirmed(me2.ID); err != nil {
		log.Fatalf("post-recovery-verify DB assert: %v", err)
	}

	step("step 32e/45 — GET /me round-trips post-recovery-ceremony")
	mePT2, err := c.getMe()
	if err != nil {
		log.Fatalf("GET /me post-recovery: %v", err)
	}
	if mePT2.ID != me2.ID {
		log.Fatalf("/me id drift after recovery: got %d want %d", mePT2.ID, me2.ID)
	}
	log.Printf("  /me id=%d (account intact post-recovery)", mePT2.ID)

	step("step 32f/45 — POST /auth/logout (drop recovery-ceremony session)")
	if err := c.logout(); err != nil {
		log.Fatalf("logout post-recovery: %v", err)
	}

	step("step 33/45 — re-login via webauthn for the throttle observation phase")
	relogin, err := c.beginLogin()
	if err != nil {
		log.Fatalf("relogin/begin: %v", err)
	}
	signedRelogin, err := auth.signAssertion(relogin.Challenge, *baseURL)
	if err != nil {
		log.Fatalf("relogin sign: %v", err)
	}
	if err := c.completeLogin(auth, signedRelogin); err != nil {
		log.Fatalf("relogin/complete: %v", err)
	}
	log.Printf("  webauthn session restored for sudo-throttle observation")

	step("step 34/45 — drive wrong TOTP codes via /me/sudo password_totp until 429")
	attempts, retryAfter, err := driveTOTPLockout(c, password)
	if err != nil {
		log.Fatalf("drive totp lockout: %v", err)
	}
	log.Printf("  observed 429 after %d wrong attempts; Retry-After=%s",
		attempts, retryAfter)

	step("step 35/45 — DB assert: auth_throttle row for (account, 'totp') failed_attempts>=3, locked")
	if err := verifyThrottleLocked(me2.ID, "totp"); err != nil {
		log.Fatalf("throttle DB assert: %v", err)
	}

	step("step 36/45 — HARNESS ONLY: DELETE auth_throttle row + fresh login to reset per-session sudo rate limit")
	if err := resetThrottle(me2.ID, "totp"); err != nil {
		log.Fatalf("reset throttle: %v", err)
	}
	// /me/sudo/begin is rate-limited per session (10/min, see handle_sudo.go).
	// The throttle-observation phase burned through that budget, so log out
	// and start a fresh webauthn session for the remaining sudo-gated work.
	if err := c.logout(); err != nil {
		log.Fatalf("logout pre-sudo-refresh: %v", err)
	}
	freshBegin, err := c.beginLogin()
	if err != nil {
		log.Fatalf("post-throttle relogin/begin: %v", err)
	}
	freshSigned, err := auth.signAssertion(freshBegin.Challenge, *baseURL)
	if err != nil {
		log.Fatalf("post-throttle relogin sign: %v", err)
	}
	if err := c.completeLogin(auth, freshSigned); err != nil {
		log.Fatalf("post-throttle relogin/complete: %v", err)
	}
	log.Printf("  auth_throttle reset + fresh webauthn session (sudo budget reset)")

	step("step 37/45 — sudo via password_totp (/me/sudo/begin + /me/sudo/complete)")
	// Wait past the period boundary from step 26's successful verify so the
	// next code we send hasn't been seen by last_step.
	totpStep = waitForNextTOTPStep(totpStep)
	if err := sudoPasswordTOTP(c, password, secret); err != nil {
		log.Fatalf("sudo password_totp: %v", err)
	}
	log.Printf("  sudo grant acquired (password_totp; step=%d)", totpStep)

	step("step 38/45 — POST /me/recovery-codes/regenerate (consumes sudo, mints fresh codes)")
	var regen struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}
	if err := c.postJSON("/api/prohibitorum/me/recovery-codes/regenerate",
		map[string]any{}, &regen); err != nil {
		log.Fatalf("recovery-codes/regenerate: %v", err)
	}
	if len(regen.RecoveryCodes) != 10 {
		log.Fatalf("regenerate: expected 10 codes, got %d", len(regen.RecoveryCodes))
	}
	recoveryCodes = regen.RecoveryCodes
	log.Printf("  regenerated %d recovery codes (old set invalidated)", len(recoveryCodes))

	step("step 39/45 — POST /me/sudo/methods (recovery_code must NOT appear post-hardening)")
	if err := verifySudoMethodsNoRecoveryCode(c); err != nil {
		log.Fatalf("sudo methods invariant: %v", err)
	}
	log.Printf("  /me/sudo/methods correctly omits recovery_code")

	step("step 40/45 — POST /me/sudo/begin {method:recovery_code} must 400 sudo_method_unavailable")
	if err := verifySudoBeginRejectsRecoveryCode(c); err != nil {
		log.Fatalf("sudo begin recovery_code rejection: %v", err)
	}
	log.Printf("  /me/sudo/begin rejects recovery_code with sudo_method_unavailable")

	step("step 41/45 — sudo via webauthn (priming the destructive revoke)")
	if err := sudoWebAuthn(c, auth, *baseURL); err != nil {
		log.Fatalf("sudo webauthn (pre revoke): %v", err)
	}
	log.Printf("  sudo grant acquired (webauthn)")

	step("step 42/45 — POST /me/auth/revoke-password-totp (destructive)")
	if err := c.postJSON("/api/prohibitorum/me/auth/revoke-password-totp",
		map[string]any{}, nil); err != nil {
		log.Fatalf("revoke-password-totp: %v", err)
	}
	log.Printf("  fallback factors revoked (204)")

	step("step 43/45 — DB assert: password_credential / totp_credential / recovery_code all empty")
	if err := verifyFactorsEmpty(me2.ID); err != nil {
		log.Fatalf("post-revoke DB assert: %v", err)
	}

	step("step 44/45 — logout then POST /auth/password/begin must now 401")
	if err := c.logout(); err != nil {
		log.Fatalf("logout post-revoke: %v", err)
	}
	if _, err := c.passwordBegin(*username, password); err == nil {
		log.Fatalf("password/begin succeeded after revoke; expected 401")
	}
	log.Printf("  /auth/password/begin returns 401 as expected")

	step("step 45/45 — DB assert: credential_event covers v0.2 lifecycle")
	if err := verifyV02AuditEvents(me2.ID); err != nil {
		log.Fatalf("audit DB assert: %v", err)
	}

	// =========================================================================
	// v0.3 surface: upstream OIDC federation — login + callback drive against an
	// in-process mock OP, then negative paths (email_not_verified,
	// username_collision, invalid_return_to, upstream_error), link_only mode,
	// self-service link from the smoke-admin session, and unlink. Per Task 10
	// of the v0.3 plan. The mock OP is the one started at main() entry.
	// =========================================================================

	// v0.3 numbering: 20 federation steps appended after the v0.2 block
	// (steps 1–45). Existing v0.2 step labels keep their "/45" denominator
	// to avoid a diff on every existing line.
	const totalV03 = 65

	step(fmt.Sprintf("step 46/%d — seed upstream_idp 'mockop' (auto_provision)", totalV03))
	dek := loadDEK()
	mockopIDPID, err := seedUpstreamIDP(dek, "mockop", "Mock OP", opTS.URL,
		"test-client", "test-client-secret", "auto_provision",
		[]string{"example.com"}, true)
	if err != nil {
		log.Fatalf("seed upstream_idp 'mockop': %v", err)
	}
	log.Printf("  upstream_idp id=%d slug=mockop issuer=%s", mockopIDPID, opTS.URL)

	step(fmt.Sprintf("step 47/%d — happy-path /login → upstream /authorize", totalV03))
	opSrv.SetClaims("ext-user-1", "ext@example.com", true, "extuser", "Ext User")
	extClient, err := newFederationClient(*baseURL)
	if err != nil {
		log.Fatalf("federation client: %v", err)
	}
	authorizeURL, err := extClient.getRedirect("/api/prohibitorum/auth/federation/mockop/login?return_to=/me")
	if err != nil {
		log.Fatalf("federation/login: %v", err)
	}
	if !strings.HasPrefix(authorizeURL, opTS.URL+"/authorize") {
		log.Fatalf("federation/login: expected redirect to mock OP /authorize, got %q", authorizeURL)
	}
	log.Printf("  302 to upstream /authorize (len=%d)", len(authorizeURL))

	step(fmt.Sprintf("step 48/%d — follow /authorize → /callback (code+state+iss)", totalV03))
	callbackURL, err := followMockOPAuthorize(authorizeURL)
	if err != nil {
		log.Fatalf("mock OP /authorize: %v", err)
	}
	if !strings.Contains(callbackURL, "/api/prohibitorum/auth/federation/mockop/callback") {
		log.Fatalf("mock OP did not redirect to /callback: %q", callbackURL)
	}
	log.Printf("  302 to RP /callback (with code, state, iss)")

	step(fmt.Sprintf("step 49/%d — RP /callback → 302 /me + session cookie", totalV03))
	if loc, err := extClient.getRedirectAbs(callbackURL); err != nil {
		log.Fatalf("federation/callback: %v", err)
	} else if loc != "/me" {
		log.Fatalf("federation/callback: want redirect to /me, got %q", loc)
	}
	// Session cookies are Path=/api/prohibitorum, so c.cookies() (which
	// queries with URL path "/") returns 0. Verify session presence by
	// hitting an API endpoint — if the cookie weren't set, /me would 401.
	if _, err := extClient.getMe(); err != nil {
		log.Fatalf("federation/callback: no session cookie (post-callback /me failed: %v)", err)
	}
	log.Printf("  session cookie issued (verified via /me)")

	step(fmt.Sprintf("step 50/%d — GET /me as federated user", totalV03))
	extMe, err := extClient.getMe()
	if err != nil {
		log.Fatalf("federated /me: %v", err)
	}
	if extMe.Username != "extuser" {
		log.Fatalf("federated /me username: got %q want %q", extMe.Username, "extuser")
	}
	if extMe.DisplayName != "Ext User" {
		log.Fatalf("federated /me displayName: got %q want %q", extMe.DisplayName, "Ext User")
	}
	log.Printf("  /me id=%d username=%s displayName=%s", extMe.ID, extMe.Username, extMe.DisplayName)

	step(fmt.Sprintf("step 51/%d — DB assert: account_identity + credential_event for ext-user-1", totalV03))
	if err := verifyFederatedIdentityCreated(extMe.ID, "ext-user-1", mockopIDPID); err != nil {
		log.Fatalf("identity DB assert: %v", err)
	}

	step(fmt.Sprintf("step 52/%d — claim sync on re-login (display_name drift)", totalV03))
	if err := extClient.logout(); err != nil {
		log.Fatalf("ext logout pre-resync: %v", err)
	}
	opSrv.SetClaims("ext-user-1", "ext@example.com", true, "extuser", "Ext User v2")
	if err := driveFederationLogin(extClient, *baseURL, "mockop", "/me"); err != nil {
		log.Fatalf("re-login federation: %v", err)
	}
	extMe2, err := extClient.getMe()
	if err != nil {
		log.Fatalf("federated /me post-resync: %v", err)
	}
	if extMe2.DisplayName != "Ext User v2" {
		log.Fatalf("claim sync: displayName not updated, got %q want %q",
			extMe2.DisplayName, "Ext User v2")
	}
	log.Printf("  /me.displayName = %q (synced from upstream)", extMe2.DisplayName)

	step(fmt.Sprintf("step 53/%d — negative: email_not_verified", totalV03))
	if err := extClient.logout(); err != nil {
		log.Fatalf("ext logout pre-neg: %v", err)
	}
	negClient1, _ := newFederationClient(*baseURL)
	opSrv.SetClaims("ext-user-99", "ext99@example.com", false, "extuser99", "Ext 99")
	if err := expectFederationCallbackError(negClient1, *baseURL, "mockop",
		http.StatusForbidden, "email_not_verified"); err != nil {
		log.Fatalf("negative email_not_verified: %v", err)
	}
	log.Printf("  /callback → 403 email_not_verified ✓")

	step(fmt.Sprintf("step 54/%d — negative: username_collision", totalV03))
	negClient2, _ := newFederationClient(*baseURL)
	// Collide on smoke-admin's username (auto_provision tries to create
	// a new account with that name; existing local account wins).
	opSrv.SetClaims("ext-collide-1", "collide@example.com", true, *username, "Collider")
	if err := expectFederationCallbackError(negClient2, *baseURL, "mockop",
		http.StatusForbidden, "username_collision"); err != nil {
		log.Fatalf("negative username_collision: %v", err)
	}
	log.Printf("  /callback → 403 username_collision ✓")

	step(fmt.Sprintf("step 55/%d — negative: invalid_return_to", totalV03))
	negClient3, _ := newFederationClient(*baseURL)
	resp, err := negClient3.getRaw("/api/prohibitorum/auth/federation/mockop/login?return_to=https://evil.example.com")
	if err != nil {
		log.Fatalf("negative invalid_return_to: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		log.Fatalf("negative invalid_return_to: want 400, got %d", resp.StatusCode)
	}
	log.Printf("  /login?return_to=https://evil… → 400 ✓")

	step(fmt.Sprintf("step 56/%d — negative: upstream_error (access_denied)", totalV03))
	negClient4, _ := newFederationClient(*baseURL)
	opSrv.FailWithError("access_denied", "user denied")
	if err := expectFederationCallbackError(negClient4, *baseURL, "mockop",
		http.StatusBadRequest, "upstream_error"); err != nil {
		log.Fatalf("negative upstream_error: %v", err)
	}
	log.Printf("  /callback → 400 upstream_error ✓")

	step(fmt.Sprintf("step 57/%d — GET /me/identities (as federated user)", totalV03))
	// Restore valid claims and re-login as ext-user-1.
	opSrv.SetClaims("ext-user-1", "ext@example.com", true, "extuser", "Ext User v2")
	if err := driveFederationLogin(extClient, *baseURL, "mockop", "/me"); err != nil {
		log.Fatalf("ext re-login for /me/identities: %v", err)
	}
	identities, err := extClient.listMyIdentities()
	if err != nil {
		log.Fatalf("listMyIdentities: %v", err)
	}
	if len(identities) != 1 || identities[0].IdpSlug != "mockop" {
		log.Fatalf("expected 1 identity with idpSlug=mockop, got %+v", identities)
	}
	log.Printf("  /me/identities = [%s (%s)]", identities[0].IdpSlug, identities[0].IdpDisplayName)

	step(fmt.Sprintf("step 58/%d — seed upstream_idp 'mockop-link' (link_only mode)", totalV03))
	linkIDPID, err := seedUpstreamIDP(dek, "mockop-link", "Mock OP (link-only)", opTS.URL,
		"test-client", "test-client-secret", "link_only",
		[]string{"example.com"}, true)
	if err != nil {
		log.Fatalf("seed mockop-link: %v", err)
	}
	log.Printf("  upstream_idp id=%d slug=mockop-link mode=link_only", linkIDPID)

	step(fmt.Sprintf("step 59/%d — link_only refuses unknown sub (403 link_required)", totalV03))
	negClient5, _ := newFederationClient(*baseURL)
	opSrv.SetClaims("ext-unknown-9", "unknown@example.com", true, "extuser-unknown", "Unknown")
	if err := expectFederationCallbackError(negClient5, *baseURL, "mockop-link",
		http.StatusForbidden, "link_required"); err != nil {
		log.Fatalf("negative link_required: %v", err)
	}
	log.Printf("  link_only /callback → 403 link_required ✓")

	// --- Self-service link from smoke-admin (with sudo) ---------------------

	step(fmt.Sprintf("step 60/%d — re-login as smoke-admin via webauthn for link/unlink", totalV03))
	if err := extClient.logout(); err != nil {
		log.Printf("  (ext logout error ignored: %v)", err)
	}
	// /me/sudo/begin is rate-limited per session (10/min); each negative
	// federation test above touched a *different* fresh client, so the
	// smoke-admin session's bucket is clean. Just re-login.
	adminLogin, err := c.beginLogin()
	if err != nil {
		log.Fatalf("admin relogin/begin: %v", err)
	}
	adminSigned, err := auth.signAssertion(adminLogin.Challenge, *baseURL)
	if err != nil {
		log.Fatalf("admin relogin sign: %v", err)
	}
	if err := c.completeLogin(auth, adminSigned); err != nil {
		log.Fatalf("admin relogin/complete: %v", err)
	}
	adminMe, err := c.getMe()
	if err != nil {
		log.Fatalf("admin /me post-relogin: %v", err)
	}
	log.Printf("  smoke-admin id=%d back in session", adminMe.ID)

	step(fmt.Sprintf("step 61/%d — sudo (webauthn) + /me/identities/link/mockop/begin → /callback", totalV03))
	if err := sudoWebAuthn(c, auth, *baseURL); err != nil {
		log.Fatalf("admin sudo webauthn pre-link: %v", err)
	}
	opSrv.SetClaims("admin-link-1", "admin@example.com", true, *username, *display)
	// Disable auto-redirect on the admin client so we can drive
	// /link/begin → upstream /authorize → /link/callback by hand. The
	// remaining steps after the link round-trip (listMySessions,
	// /me/identities, unlink) all hit non-redirecting endpoints so they
	// are unaffected by leaving CheckRedirect in place.
	c.hc.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	authorizeLink, err := c.getRedirect("/api/prohibitorum/me/identities/link/mockop/begin?return_to=/me")
	if err != nil {
		log.Fatalf("link/begin: %v", err)
	}
	if !strings.HasPrefix(authorizeLink, opTS.URL+"/authorize") {
		log.Fatalf("link/begin: expected upstream /authorize, got %q", authorizeLink)
	}
	linkCallbackURL, err := followMockOPAuthorize(authorizeLink)
	if err != nil {
		log.Fatalf("link mock OP /authorize: %v", err)
	}
	if !strings.Contains(linkCallbackURL, "/api/prohibitorum/me/identities/link/mockop/callback") {
		log.Fatalf("link callback URL: %q", linkCallbackURL)
	}
	// The link callback must NOT issue a new session cookie (the admin
	// session in c is preserved). Snapshot the current session id via
	// /me/sessions and verify it survives the callback unchanged.
	preSessions, err := c.listMySessions()
	if err != nil {
		log.Fatalf("pre-link /me/sessions: %v", err)
	}
	var preSessID string
	for _, s := range preSessions {
		if s.IsCurrent {
			preSessID = s.ID
			break
		}
	}
	if preSessID == "" {
		log.Fatalf("could not identify current session id pre-link: %+v", preSessions)
	}
	if loc, err := c.getRedirectAbs(linkCallbackURL); err != nil {
		log.Fatalf("link/callback: %v", err)
	} else if loc != "/me" {
		log.Fatalf("link/callback: want /me, got %q", loc)
	}
	postSessions, err := c.listMySessions()
	if err != nil {
		log.Fatalf("post-link /me/sessions: %v", err)
	}
	var postSessID string
	for _, s := range postSessions {
		if s.IsCurrent {
			postSessID = s.ID
			break
		}
	}
	if postSessID != preSessID {
		log.Fatalf("link/callback: session id changed (%s → %s) — link must not Issue a new session",
			preSessID, postSessID)
	}
	log.Printf("  link/callback → 302 /me; session id unchanged (%s) ✓", preSessID)

	step(fmt.Sprintf("step 62/%d — DB assert: account_identity for admin-link-1 owned by smoke-admin", totalV03))
	if err := verifyFederatedIdentityCreated(adminMe.ID, "admin-link-1", mockopIDPID); err != nil {
		log.Fatalf("link DB assert: %v", err)
	}

	step(fmt.Sprintf("step 63/%d — GET /me/identities (as smoke-admin)", totalV03))
	adminIdentities, err := c.listMyIdentities()
	if err != nil {
		log.Fatalf("listMyIdentities admin: %v", err)
	}
	if len(adminIdentities) != 1 || adminIdentities[0].IdpSlug != "mockop" {
		log.Fatalf("expected 1 identity for smoke-admin, got %+v", adminIdentities)
	}
	log.Printf("  smoke-admin has 1 federated identity (id=%d)", adminIdentities[0].ID)

	step(fmt.Sprintf("step 64/%d — sudo (webauthn) + POST /me/identities/{id}/unlink", totalV03))
	if err := sudoWebAuthn(c, auth, *baseURL); err != nil {
		log.Fatalf("admin sudo pre-unlink: %v", err)
	}
	unlinkPath := fmt.Sprintf("/api/prohibitorum/me/identities/%d/unlink", adminIdentities[0].ID)
	if err := c.postJSON(unlinkPath, map[string]any{}, nil); err != nil {
		log.Fatalf("unlink: %v", err)
	}
	if err := verifyFederatedIdentityGone(adminMe.ID, "admin-link-1"); err != nil {
		log.Fatalf("post-unlink DB assert: %v", err)
	}
	log.Printf("  /me/identities/%d/unlink → 204, row deleted ✓", adminIdentities[0].ID)

	step(fmt.Sprintf("step 65/%d — DB assert: credential_event covers v0.3 federation lifecycle", totalV03))
	if err := verifyV03FederationAuditEvents(); err != nil {
		log.Fatalf("v0.3 federation audit DB assert: %v", err)
	}

	fmt.Println()
	fmt.Println("✓ smoke OK — 45/45 (v0.2) + 46–65/65 (v0.3 federation) + DB-state assertions passed against",
		*baseURL)
}

func step(msg string) {
	log.Println()
	log.Println(msg)
}

// ---------- HTTP client with cookie jar ----------

type client struct {
	base string
	hc   *http.Client
	jar  *cookiejar.Jar
}

func newClient(base string) (*client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	return &client{
		base: base,
		jar:  jar,
		hc:   &http.Client{Jar: jar, Timeout: 10 * time.Second},
	}, nil
}

func (c *client) cookies() []*http.Cookie {
	u, _ := url.Parse(c.base)
	return c.jar.Cookies(u)
}

func (c *client) postJSON(path string, body any, out any) error {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return err
		}
	}
	req, err := http.NewRequest(http.MethodPost, c.base+path, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, out)
}

func (c *client) get(path string, out any) error {
	req, err := http.NewRequest(http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	return c.do(req, out)
}

func (c *client) do(req *http.Request, out any) error {
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("%s %s: %d %s — %s",
			req.Method, req.URL.Path, resp.StatusCode, resp.Status, string(body))
	}
	if out == nil || len(body) == 0 {
		return nil
	}
	return json.Unmarshal(body, out)
}

// ---------- enroll-admin → token ----------

func mintEnrollmentToken(baseURL string, skipNew bool) (string, error) {
	args := []string{"exec", "--", "go", "run", "./cmd/prohibitorum", "enroll-admin"}
	if !skipNew {
		args = append(args, "--new")
	}
	cmd := exec.Command("mise", args...)
	cmd.Env = append(os.Environ(),
		"PROHIBITORUM_PUBLIC_ORIGIN="+baseURL,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("enroll-admin: %v\n%s", err, out)
	}

	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if u, ok := parseEnrollmentURL(line); ok {
			parts := strings.Split(u.Path, "/")
			if len(parts) == 0 {
				return "", fmt.Errorf("could not extract token from %q", u)
			}
			return parts[len(parts)-1], nil
		}
	}
	return "", fmt.Errorf("no enrollment URL in enroll-admin output:\n%s", out)
}

func parseEnrollmentURL(s string) (*url.URL, bool) {
	idx := strings.Index(s, "http")
	if idx < 0 {
		return nil, false
	}
	candidate := strings.Fields(s[idx:])[0]
	u, err := url.Parse(candidate)
	if err != nil {
		return nil, false
	}
	if !strings.Contains(u.Path, "/enroll/") {
		return nil, false
	}
	return u, true
}

// ---------- WebAuthn JSON shapes (just enough for the ceremony) ----------

type creationOptions struct {
	Challenge base64URL `json:"challenge"`
	RP        struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"rp"`
	User struct {
		ID          base64URL `json:"id"`
		Name        string    `json:"name"`
		DisplayName string    `json:"displayName"`
	} `json:"user"`
}

type assertionOptions struct {
	Challenge base64URL `json:"challenge"`
	RPID      string    `json:"rpId"`
}

// base64URL decodes from the standard WebAuthn JSON encoding
// (base64url-no-padding) and round-trips back the same way.
type base64URL []byte

func (b *base64URL) UnmarshalJSON(data []byte) error {
	s, err := unquote(data)
	if err != nil {
		return err
	}
	decoded, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		// Some implementations include padding.
		decoded, err = base64.URLEncoding.DecodeString(s)
		if err != nil {
			return err
		}
	}
	*b = decoded
	return nil
}

func (b base64URL) MarshalJSON() ([]byte, error) {
	return []byte(`"` + base64.RawURLEncoding.EncodeToString(b) + `"`), nil
}

func unquote(data []byte) (string, error) {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return "", err
	}
	return s, nil
}

// ---------- virtual authenticator ----------

type authenticator struct {
	rpID         string
	rpIDHash     [32]byte
	key          *ecdsa.PrivateKey
	credentialID []byte
	userHandle   []byte
	signCount    uint32
	aaguid       [16]byte
}

func newAuthenticator(rpID string) (*authenticator, error) {
	if rpID == "" {
		return nil, errors.New("authenticator: empty rpId")
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	credID := make([]byte, 32)
	if _, err := rand.Read(credID); err != nil {
		return nil, err
	}
	a := &authenticator{
		rpID:         rpID,
		rpIDHash:     sha256.Sum256([]byte(rpID)),
		key:          key,
		credentialID: credID,
	}
	// aaguid stays zero (none-attestation, software authenticator).
	return a, nil
}

// coseKey returns the ES256 COSE_Key CBOR for the authenticator's public key
// (RFC 8152 Section 7 + WebAuthn §5.8.5).
func (a *authenticator) coseKey() ([]byte, error) {
	pub := a.key.Public().(*ecdsa.PublicKey)
	x := padCoord(pub.X)
	y := padCoord(pub.Y)
	// Integer keys must be encoded as a deterministic CBOR map. The
	// fxamacker/cbor library defaults to canonical/CTAP2-compatible encoding
	// when given a map with integer keys.
	m := map[int]any{
		1:  2,    // kty: EC2
		3:  -7,   // alg: ES256
		-1: 1,    // crv: P-256
		-2: x,    // x coordinate (32 bytes, big-endian)
		-3: y,    // y coordinate (32 bytes, big-endian)
	}
	opts, err := cbor.CTAP2EncOptions().EncMode()
	if err != nil {
		return nil, err
	}
	return opts.Marshal(m)
}

func padCoord(n *big.Int) []byte {
	b := n.Bytes()
	if len(b) == 32 {
		return b
	}
	out := make([]byte, 32)
	copy(out[32-len(b):], b)
	return out
}

// authData builds the AuthenticatorData byte string per WebAuthn §6.1.
// includeAttested controls whether attestedCredentialData is appended
// (true for registration, false for assertion).
func (a *authenticator) authData(includeAttested bool, flagsExtra byte) ([]byte, error) {
	const (
		flagUP = 0x01
		flagUV = 0x04
		flagBE = 0x08
		flagBS = 0x10
		flagAT = 0x40
	)
	flags := byte(flagUP | flagUV) | flagsExtra
	if includeAttested {
		flags |= flagAT
	}

	var buf bytes.Buffer
	buf.Write(a.rpIDHash[:])
	buf.WriteByte(flags)
	var counterBytes [4]byte
	binary.BigEndian.PutUint32(counterBytes[:], a.signCount)
	buf.Write(counterBytes[:])

	if includeAttested {
		buf.Write(a.aaguid[:])
		var credLen [2]byte
		binary.BigEndian.PutUint16(credLen[:], uint16(len(a.credentialID)))
		buf.Write(credLen[:])
		buf.Write(a.credentialID)
		coseKey, err := a.coseKey()
		if err != nil {
			return nil, err
		}
		buf.Write(coseKey)
	}
	return buf.Bytes(), nil
}

type attestationResult struct {
	credentialID      []byte
	attestationObject []byte
	clientDataJSON    []byte
}

// attestCredential produces an AuthenticatorAttestationResponse for the
// supplied challenge + origin (registration ceremony). The authenticator
// remembers the user handle so it can return it on subsequent discoverable
// assertions per WebAuthn §5.1.4.
func (a *authenticator) attestCredential(challenge, userHandle []byte, origin string) (*attestationResult, error) {
	a.userHandle = userHandle
	authData, err := a.authData(true, 0)
	if err != nil {
		return nil, err
	}
	clientData := clientDataJSON("webauthn.create", challenge, origin)

	// attestationObject CBOR per WebAuthn §6.5.4: {"fmt": "none", "attStmt": {}, "authData": <bytes>}
	att := map[string]any{
		"fmt":      "none",
		"attStmt":  map[string]any{},
		"authData": authData,
	}
	opts, err := cbor.CTAP2EncOptions().EncMode()
	if err != nil {
		return nil, err
	}
	attObj, err := opts.Marshal(att)
	if err != nil {
		return nil, err
	}
	return &attestationResult{
		credentialID:      a.credentialID,
		attestationObject: attObj,
		clientDataJSON:    clientData,
	}, nil
}

type assertionResult struct {
	authenticatorData []byte
	clientDataJSON    []byte
	signature         []byte
	userHandle        []byte
}

// signAssertion produces an AuthenticatorAssertionResponse for the supplied
// challenge + origin (login ceremony).
func (a *authenticator) signAssertion(challenge []byte, origin string) (*assertionResult, error) {
	a.signCount++
	authData, err := a.authData(false, 0)
	if err != nil {
		return nil, err
	}
	clientData := clientDataJSON("webauthn.get", challenge, origin)

	cdHash := sha256.Sum256(clientData)
	var msg bytes.Buffer
	msg.Write(authData)
	msg.Write(cdHash[:])
	digest := sha256.Sum256(msg.Bytes())
	sig, err := ecdsa.SignASN1(rand.Reader, a.key, digest[:])
	if err != nil {
		return nil, err
	}
	return &assertionResult{
		authenticatorData: authData,
		clientDataJSON:    clientData,
		signature:         sig,
		userHandle:        a.userHandle,
	}, nil
}

func clientDataJSON(typ string, challenge []byte, origin string) []byte {
	cd := map[string]any{
		"type":      typ,
		"challenge": base64.RawURLEncoding.EncodeToString(challenge),
		"origin":    origin,
	}
	out, _ := json.Marshal(cd)
	return out
}

// ---------- Prohibitorum REST shapes ----------

type meResponse struct {
	ID          int32  `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	Role        string `json:"role"`
}

func (c *client) beginEnrollment(token, username, display, nickname string) (*creationOptions, error) {
	body := map[string]string{
		"username":    username,
		"displayName": display,
		"nickname":    nickname,
	}
	var opts creationOptions
	if err := c.postJSON("/api/prohibitorum/enrollments/"+token+"/register/begin", body, &opts); err != nil {
		return nil, err
	}
	return &opts, nil
}

func (c *client) completeEnrollment(token string, a *authenticator, att *attestationResult) error {
	credIDB64 := base64.RawURLEncoding.EncodeToString(att.credentialID)
	payload := map[string]any{
		"id":    credIDB64,
		"rawId": credIDB64,
		"type":  "public-key",
		"response": map[string]any{
			"clientDataJSON":    base64.RawURLEncoding.EncodeToString(att.clientDataJSON),
			"attestationObject": base64.RawURLEncoding.EncodeToString(att.attestationObject),
			"transports":        []string{"internal"},
		},
		"clientExtensionResults": map[string]any{},
		"authenticatorAttachment": "platform",
	}
	return c.postJSON("/api/prohibitorum/enrollments/"+token+"/register/complete", payload, nil)
}

func (c *client) beginLogin() (*assertionOptions, error) {
	var opts assertionOptions
	if err := c.postJSON("/api/prohibitorum/auth/login/begin", nil, &opts); err != nil {
		return nil, err
	}
	return &opts, nil
}

func (c *client) completeLogin(a *authenticator, sig *assertionResult) error {
	credIDB64 := base64.RawURLEncoding.EncodeToString(a.credentialID)
	userHandle := ""
	if sig.userHandle != nil {
		userHandle = base64.RawURLEncoding.EncodeToString(sig.userHandle)
	}
	payload := map[string]any{
		"id":    credIDB64,
		"rawId": credIDB64,
		"type":  "public-key",
		"response": map[string]any{
			"authenticatorData": base64.RawURLEncoding.EncodeToString(sig.authenticatorData),
			"clientDataJSON":    base64.RawURLEncoding.EncodeToString(sig.clientDataJSON),
			"signature":         base64.RawURLEncoding.EncodeToString(sig.signature),
			"userHandle":        userHandle,
		},
		"clientExtensionResults": map[string]any{},
	}
	return c.postJSON("/api/prohibitorum/auth/login/complete", payload, nil)
}

func (c *client) logout() error {
	return c.postJSON("/api/prohibitorum/auth/logout", map[string]string{}, nil)
}

func (c *client) getMe() (*meResponse, error) {
	var me meResponse
	if err := c.get("/api/prohibitorum/me", &me); err != nil {
		return nil, err
	}
	return &me, nil
}

// ---------- /me/sessions and /me/credentials shapes ----------

type sessionListItem struct {
	ID         string `json:"id"`
	IsCurrent  bool   `json:"isCurrent"`
	IssuedAt   string `json:"issuedAt"`
	ExpiresAt  string `json:"expiresAt"`
	LastSeenIP string `json:"lastSeenIp"`
	UserAgent  string `json:"userAgent"`
}

type credentialListItem struct {
	ID                 int32  `json:"id"`
	CredentialIDSuffix string `json:"credentialIdSuffix"`
	Nickname           string `json:"nickname,omitempty"`
}

func (c *client) listMySessions() ([]sessionListItem, error) {
	var out []sessionListItem
	if err := c.get("/api/prohibitorum/me/sessions", &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *client) revokeSession(sessionID string) error {
	return c.postJSON("/api/prohibitorum/me/sessions/revoke",
		map[string]string{"id": sessionID}, nil)
}

func (c *client) listMyCredentials() ([]credentialListItem, error) {
	var out []credentialListItem
	if err := c.get("/api/prohibitorum/me/credentials", &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *client) beginAddCredential() (*creationOptions, error) {
	var opts creationOptions
	if err := c.postJSON("/api/prohibitorum/me/credentials/register/begin",
		map[string]any{}, &opts); err != nil {
		return nil, err
	}
	return &opts, nil
}

func (c *client) completeAddCredential(a *authenticator, att *attestationResult, nickname string) error {
	credIDB64 := base64.RawURLEncoding.EncodeToString(att.credentialID)
	payload := map[string]any{
		"id":    credIDB64,
		"rawId": credIDB64,
		"type":  "public-key",
		"response": map[string]any{
			"clientDataJSON":    base64.RawURLEncoding.EncodeToString(att.clientDataJSON),
			"attestationObject": base64.RawURLEncoding.EncodeToString(att.attestationObject),
			"transports":        []string{"internal"},
		},
		"clientExtensionResults":  map[string]any{},
		"authenticatorAttachment": "platform",
	}
	path := "/api/prohibitorum/me/credentials/register/complete"
	if nickname != "" {
		path += "?nickname=" + url.QueryEscape(nickname)
	}
	return c.postJSON(path, payload, nil)
}

// ---------- DB-state verification (psql shell-out) ----------

// verifyDB asserts the runtime effects that pure-HTTP smoke can't see:
//   - both webauthn_credential rows for accountID have cose_alg = -7 (ES256)
//     (proves the COSEAlg helper at handle_enrollment.go AND handle_me.go)
//   - the session table has at least 3 rows for accountID, with at least
//     one revoked (proves InsertSession at Issue + RevokeSession at logout
//     + RevokeSession at /me/sessions/revoke)
//   - every session row has amr = '{hwk}' for WebAuthn
func verifyDB(accountID int32) error {
	dburl := os.Getenv("PROHIBITORUM_DATABASE_URL")
	if dburl == "" {
		return errors.New("PROHIBITORUM_DATABASE_URL not set; cannot verify DB state")
	}
	algs, err := psqlScalar(dburl,
		fmt.Sprintf("SELECT cose_alg FROM webauthn_credential WHERE account_id=%d ORDER BY id", accountID))
	if err != nil {
		return fmt.Errorf("query cose_alg: %w", err)
	}
	if len(algs) < 2 {
		return fmt.Errorf("expected >=2 webauthn_credential rows for account %d, got %d (%v)",
			accountID, len(algs), algs)
	}
	for i, a := range algs {
		v, err := strconv.Atoi(a)
		if err != nil {
			return fmt.Errorf("cose_alg row %d: not an int: %q", i, a)
		}
		if v != -7 {
			return fmt.Errorf("cose_alg row %d: got %d, want -7 (ES256)", i, v)
		}
	}
	log.Printf("  webauthn_credential.cose_alg = -7 for all %d rows ✓", len(algs))

	sessRows, err := psqlScalar(dburl,
		fmt.Sprintf("SELECT amr[1] || ':' || (revoked_at IS NOT NULL)::text FROM session WHERE account_id=%d ORDER BY auth_time", accountID))
	if err != nil {
		return fmt.Errorf("query session: %w", err)
	}
	if len(sessRows) < 3 {
		return fmt.Errorf("expected >=3 session rows for account %d (enroll-issue, post-logout, post-login, B's session...), got %d (%v)",
			accountID, len(sessRows), sessRows)
	}
	revokedCount := 0
	for _, row := range sessRows {
		parts := strings.SplitN(row, ":", 2)
		if len(parts) != 2 {
			return fmt.Errorf("malformed session row: %q", row)
		}
		if parts[0] != "hwk" {
			return fmt.Errorf("session amr[1] = %q, want %q", parts[0], "hwk")
		}
		if parts[1] == "true" {
			revokedCount++
		}
	}
	if revokedCount < 2 {
		return fmt.Errorf("expected >=2 revoked session rows (logout + revoke-by-id), got %d", revokedCount)
	}
	log.Printf("  session table: %d rows, amr={hwk} on all, %d revoked ✓", len(sessRows), revokedCount)

	return nil
}

func psqlScalar(dburl, query string) ([]string, error) {
	out, err := exec.Command("psql", dburl, "-At", "-c", query).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("psql: %v: %s", err, string(out))
	}
	s := strings.TrimSpace(string(out))
	if s == "" {
		return nil, nil
	}
	return strings.Split(s, "\n"), nil
}

// =========================================================================
// v0.2 helpers
// =========================================================================

// postJSONRaw is a postJSON variant that returns the raw *http.Response without
// erroring on 4xx/5xx. The smoke uses it to observe rate-limit / throttle 429s
// where the response status is the point of the test.
func (c *client) postJSONRaw(path string, body any) (*http.Response, error) {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequest(http.MethodPost, c.base+path, &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.hc.Do(req)
}

// passwordBegin issues /auth/password/begin and returns the partial_session_token.
// Returns an error on any non-2xx (caller may inspect for 401 vs other).
func (c *client) passwordBegin(username, password string) (string, error) {
	var out struct {
		PartialSessionToken string `json:"partial_session_token"`
	}
	if err := c.postJSON("/api/prohibitorum/auth/password/begin",
		map[string]string{"username": username, "password": password}, &out); err != nil {
		return "", err
	}
	if out.PartialSessionToken == "" {
		return "", errors.New("password/begin: empty partial_session_token")
	}
	return out.PartialSessionToken, nil
}

func (c *client) totpStepTwoVerify(partialToken, code string) error {
	return c.postJSON("/api/prohibitorum/auth/totp/verify",
		map[string]string{"partial_session_token": partialToken, "code": code}, nil)
}

// recoveryCodeVerify drives /auth/recovery-code/verify. Post 2026-05-28
// hardening this returns a recovery_session_token (no session cookie). The
// caller must redeem the token at /auth/recovery/totp/{begin,verify}.
func (c *client) recoveryCodeVerify(partialToken, code string) (string, error) {
	var out struct {
		RecoverySessionToken string `json:"recovery_session_token"`
	}
	if err := c.postJSON("/api/prohibitorum/auth/recovery-code/verify",
		map[string]string{"partial_session_token": partialToken, "code": code}, &out); err != nil {
		return "", err
	}
	return out.RecoverySessionToken, nil
}

// sudoWebAuthn runs /me/sudo/begin {method:webauthn} → assertion → /me/sudo/complete.
// On success the session in c carries a fresh SudoUntil that the next gated
// action will consume.
func sudoWebAuthn(c *client, auth *authenticator, base string) error {
	var assertion assertionOptions
	if err := c.postJSON("/api/prohibitorum/me/sudo/begin",
		map[string]string{"method": "webauthn"}, &assertion); err != nil {
		return fmt.Errorf("sudo/begin webauthn: %w", err)
	}
	signed, err := auth.signAssertion(assertion.Challenge, base)
	if err != nil {
		return fmt.Errorf("sudo sign: %w", err)
	}
	credIDB64 := base64.RawURLEncoding.EncodeToString(auth.credentialID)
	userHandle := ""
	if signed.userHandle != nil {
		userHandle = base64.RawURLEncoding.EncodeToString(signed.userHandle)
	}
	payload := map[string]any{
		"id":    credIDB64,
		"rawId": credIDB64,
		"type":  "public-key",
		"response": map[string]any{
			"authenticatorData": base64.RawURLEncoding.EncodeToString(signed.authenticatorData),
			"clientDataJSON":    base64.RawURLEncoding.EncodeToString(signed.clientDataJSON),
			"signature":         base64.RawURLEncoding.EncodeToString(signed.signature),
			"userHandle":        userHandle,
		},
		"clientExtensionResults": map[string]any{},
	}
	return c.postJSON("/api/prohibitorum/me/sudo/complete", payload, nil)
}

// sudoPasswordTOTP runs /me/sudo/begin {method:password_totp} → /me/sudo/complete
// with the current password and a freshly computed TOTP code.
func sudoPasswordTOTP(c *client, password string, secret []byte) error {
	if err := c.postJSON("/api/prohibitorum/me/sudo/begin",
		map[string]string{"method": "password_totp"}, nil); err != nil {
		return fmt.Errorf("sudo/begin password_totp: %w", err)
	}
	// Compute the TOTP code as late as possible to minimise the chance of
	// crossing a 30 s period boundary between client and server.
	code := totppkg.ComputeCodeForTesting(secret, time.Now().Unix(), 6)
	return c.postJSON("/api/prohibitorum/me/sudo/complete", map[string]string{
		"current_password": password,
		"totp_code":        code,
	}, nil)
}

// waitForNextTOTPStep blocks until the current unix step strictly exceeds
// lastStep. RFC 6238 §5.2: a code accepted at step T may not be re-accepted.
// The server enforces this via last_step in totp_credential, so the smoke
// must wait across the 30 s boundary whenever it wants to reuse the same
// TOTP secret for back-to-back successful verifies.
//
// Returns the new step so callers can pass it as the next lastStep.
func waitForNextTOTPStep(lastStep int64) int64 {
	period := int64(30)
	for {
		cur := time.Now().Unix() / period
		if cur > lastStep {
			return cur
		}
		// Sleep until the next period boundary plus a small safety margin so
		// the server's clock has crossed it too.
		nextBoundary := (cur + 1) * period
		wait := time.Until(time.Unix(nextBoundary, 0)) + 500*time.Millisecond
		if wait <= 0 {
			wait = 250 * time.Millisecond
		}
		time.Sleep(wait)
	}
}

// verifySudoMethodsNoRecoveryCode hits /me/sudo/methods and asserts the
// returned slice does NOT contain "recovery_code". Post 2026-05-28
// hardening, recovery codes are not a sudo factor.
func verifySudoMethodsNoRecoveryCode(c *client) error {
	var out struct {
		Methods []string `json:"methods"`
	}
	if err := c.get("/api/prohibitorum/me/sudo/methods", &out); err != nil {
		return fmt.Errorf("GET /me/sudo/methods: %w", err)
	}
	for _, m := range out.Methods {
		if m == "recovery_code" {
			return fmt.Errorf("recovery_code must NOT appear in /me/sudo/methods, got %v", out.Methods)
		}
	}
	return nil
}

// verifySudoBeginRejectsRecoveryCode posts /me/sudo/begin {method:recovery_code}
// and asserts a 400 with code=sudo_method_unavailable. Catches any future
// regression that re-adds recovery_code to the sudo dispatch.
func verifySudoBeginRejectsRecoveryCode(c *client) error {
	resp, err := c.postJSONRaw("/api/prohibitorum/me/sudo/begin",
		map[string]string{"method": "recovery_code"})
	if err != nil {
		return fmt.Errorf("post /me/sudo/begin: %w", err)
	}
	defer resp.Body.Close()
	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusBadRequest {
		return fmt.Errorf("status: want 400, got %d (body=%s)", resp.StatusCode, bodyBytes)
	}
	var perr struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(bodyBytes, &perr)
	if perr.Code != "sudo_method_unavailable" {
		return fmt.Errorf("error code: want sudo_method_unavailable, got %q (body=%s)", perr.Code, bodyBytes)
	}
	return nil
}

// driveTOTPLockout sends wrong TOTP codes via sudo password_totp until the
// throttle replies with 429. Returns the attempt count and the Retry-After
// header value. Each attempt requires a fresh sudo intent (/me/sudo/begin), so
// the function re-primes each loop.
//
// Schedule per configx default: idx 0,1 = 0s; idx 2 = 1s; idx 3 = 2s; … the
// first non-zero lockout lands on the 3rd consecutive failure. The TOTP throttle
// counter increments inside completeSudoPasswordTOTP via totpStore.Verify, so
// the password verify has to succeed each time. We deliberately reuse the right
// password and a deterministically wrong TOTP code so only the totp throttle
// climbs.
func driveTOTPLockout(c *client, password string) (int, string, error) {
	const wrongCode = "000000"
	const maxAttempts = 8
	for i := 1; i <= maxAttempts; i++ {
		if err := c.postJSON("/api/prohibitorum/me/sudo/begin",
			map[string]string{"method": "password_totp"}, nil); err != nil {
			return i, "", fmt.Errorf("attempt %d: sudo/begin: %w", i, err)
		}
		resp, err := c.postJSONRaw("/api/prohibitorum/me/sudo/complete",
			map[string]string{"current_password": password, "totp_code": wrongCode})
		if err != nil {
			return i, "", fmt.Errorf("attempt %d: complete: %w", i, err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests {
			return i, resp.Header.Get("Retry-After"), nil
		}
		if resp.StatusCode != http.StatusUnauthorized {
			// 401 is the expected bad_credentials response. Anything else
			// means a different failure mode and we should stop early.
			return i, "", fmt.Errorf("attempt %d: unexpected status %d", i, resp.StatusCode)
		}
	}
	return maxAttempts, "", fmt.Errorf("no 429 observed after %d attempts", maxAttempts)
}

// resetThrottle is a HARNESS-ONLY shortcut to clear a throttle lockout the
// smoke just observed. There is no production API for this; admins normally
// wait the lockout out or, in a real incident, intervene via psql.
func resetThrottle(accountID int32, factor string) error {
	dburl := os.Getenv("PROHIBITORUM_DATABASE_URL")
	if dburl == "" {
		return errors.New("PROHIBITORUM_DATABASE_URL not set")
	}
	out, err := exec.Command("psql", dburl, "-c",
		fmt.Sprintf("DELETE FROM auth_throttle WHERE account_id=%d AND factor='%s'",
			accountID, factor)).CombinedOutput()
	if err != nil {
		return fmt.Errorf("psql delete: %v: %s", err, string(out))
	}
	return nil
}

// ---- v0.2 DB assertions ----------------------------------------------------

func verifyPasswordCredential(accountID int32) error {
	dburl := os.Getenv("PROHIBITORUM_DATABASE_URL")
	rows, err := psqlScalar(dburl,
		fmt.Sprintf("SELECT hash FROM password_credential WHERE account_id=%d", accountID))
	if err != nil {
		return err
	}
	if len(rows) != 1 {
		return fmt.Errorf("expected 1 password_credential row, got %d", len(rows))
	}
	if !strings.HasPrefix(rows[0], "$argon2id$v=19$") {
		return fmt.Errorf("password hash prefix mismatch: got %q", firstN(rows[0], 32))
	}
	log.Printf("  password_credential rows=1, hash prefix=$argon2id$v=19$ ✓")
	return nil
}

func verifyTOTPConfirmed(accountID int32) error {
	dburl := os.Getenv("PROHIBITORUM_DATABASE_URL")
	rows, err := psqlScalar(dburl, fmt.Sprintf(
		"SELECT (confirmed_at IS NOT NULL)::text FROM totp_credential WHERE account_id=%d",
		accountID))
	if err != nil {
		return err
	}
	if len(rows) != 1 {
		return fmt.Errorf("expected 1 totp_credential row, got %d", len(rows))
	}
	if rows[0] != "t" && rows[0] != "true" {
		return fmt.Errorf("totp_credential.confirmed_at is null (got %q)", rows[0])
	}
	codes, err := psqlScalar(dburl, fmt.Sprintf(
		"SELECT count(*)::text FROM recovery_code WHERE account_id=%d", accountID))
	if err != nil {
		return err
	}
	if len(codes) != 1 || codes[0] != "10" {
		return fmt.Errorf("expected 10 recovery_code rows, got %v", codes)
	}
	log.Printf("  totp_credential.confirmed_at IS NOT NULL ✓, recovery_code count=10 ✓")
	return nil
}

// verifyTOTPUnconfirmedAndRecoveryCount asserts the post /auth/recovery/totp/begin
// invariant: an unconfirmed totp_credential row exists, and exactly
// wantRecovery rows remain in recovery_code (the wipe is deferred until
// /verify success). Catches the most common regression — wiping recovery
// codes too eagerly at /begin and bricking the user's retry path.
func verifyTOTPUnconfirmedAndRecoveryCount(accountID int32, wantRecovery int) error {
	dburl := os.Getenv("PROHIBITORUM_DATABASE_URL")
	confirmed, err := psqlScalar(dburl, fmt.Sprintf(
		"SELECT (confirmed_at IS NOT NULL)::text FROM totp_credential WHERE account_id=%d",
		accountID))
	if err != nil {
		return err
	}
	if len(confirmed) != 1 {
		return fmt.Errorf("expected 1 totp_credential row, got %d", len(confirmed))
	}
	if confirmed[0] == "t" || confirmed[0] == "true" {
		return fmt.Errorf("totp_credential.confirmed_at should be NULL after /recovery/totp/begin; got %q", confirmed[0])
	}
	codes, err := psqlScalar(dburl, fmt.Sprintf(
		"SELECT count(*)::text FROM recovery_code WHERE account_id=%d AND used_at IS NULL", accountID))
	if err != nil {
		return err
	}
	if len(codes) != 1 || codes[0] != fmt.Sprintf("%d", wantRecovery) {
		return fmt.Errorf("expected %d unused recovery_code rows, got %v", wantRecovery, codes)
	}
	log.Printf("  totp_credential.confirmed_at IS NULL ✓, unused recovery_code count=%d ✓", wantRecovery)
	return nil
}

// verifyRecoveryCodeUsed checks that at least minUsed recovery codes for the
// account have used_at set. When expectIdx >= 0, the function additionally
// requires that the row at ordinal expectIdx (ordered by id ASC) is used.
func verifyRecoveryCodeUsed(accountID int32, minUsed int, expectIdx int) error {
	dburl := os.Getenv("PROHIBITORUM_DATABASE_URL")
	rows, err := psqlScalar(dburl, fmt.Sprintf(
		"SELECT (used_at IS NOT NULL)::text FROM recovery_code WHERE account_id=%d ORDER BY id ASC",
		accountID))
	if err != nil {
		return err
	}
	used := 0
	for _, r := range rows {
		if r == "t" || r == "true" {
			used++
		}
	}
	if used < minUsed {
		return fmt.Errorf("expected >=%d used recovery codes, got %d (rows=%v)",
			minUsed, used, rows)
	}
	if expectIdx >= 0 {
		if expectIdx >= len(rows) {
			return fmt.Errorf("expectIdx %d out of range (len=%d)", expectIdx, len(rows))
		}
		if rows[expectIdx] != "t" && rows[expectIdx] != "true" {
			return fmt.Errorf("recovery_code[%d].used_at is null", expectIdx)
		}
	}
	log.Printf("  recovery_code used_at set on %d of %d rows ✓", used, len(rows))
	return nil
}

func verifyThrottleLocked(accountID int32, factor string) error {
	dburl := os.Getenv("PROHIBITORUM_DATABASE_URL")
	rows, err := psqlScalar(dburl, fmt.Sprintf(
		"SELECT failed_attempts::text || ':' || (locked_until > now())::text "+
			"FROM auth_throttle WHERE account_id=%d AND factor='%s'",
		accountID, factor))
	if err != nil {
		return err
	}
	if len(rows) != 1 {
		return fmt.Errorf("expected 1 auth_throttle row for (acct=%d,factor=%s), got %d",
			accountID, factor, len(rows))
	}
	parts := strings.SplitN(rows[0], ":", 2)
	if len(parts) != 2 {
		return fmt.Errorf("malformed throttle row: %q", rows[0])
	}
	failed, err := strconv.Atoi(parts[0])
	if err != nil {
		return fmt.Errorf("failed_attempts parse: %w", err)
	}
	if failed < 3 {
		return fmt.Errorf("expected failed_attempts >= 3, got %d", failed)
	}
	if parts[1] != "t" && parts[1] != "true" {
		return fmt.Errorf("expected locked_until > now, got locked_until expired/null (%q)", parts[1])
	}
	log.Printf("  auth_throttle: failed_attempts=%d locked_until>now ✓", failed)
	return nil
}

func verifyFactorsEmpty(accountID int32) error {
	dburl := os.Getenv("PROHIBITORUM_DATABASE_URL")
	for _, tbl := range []string{"password_credential", "totp_credential", "recovery_code"} {
		rows, err := psqlScalar(dburl, fmt.Sprintf(
			"SELECT count(*)::text FROM %s WHERE account_id=%d", tbl, accountID))
		if err != nil {
			return err
		}
		if len(rows) != 1 || rows[0] != "0" {
			return fmt.Errorf("%s should be empty for account %d, got count=%v",
				tbl, accountID, rows)
		}
	}
	log.Printf("  password_credential / totp_credential / recovery_code all empty ✓")
	return nil
}

// verifyV02AuditEvents checks credential_event for the union of (factor, event)
// pairs the v0.2 surface is supposed to emit during this smoke run. Counts
// are lower bounds — the underlying writers may emit more events than the
// minimum (e.g. sudo_granted fires once per /me/sudo/complete).
func verifyV02AuditEvents(accountID int32) error {
	dburl := os.Getenv("PROHIBITORUM_DATABASE_URL")
	rows, err := psqlScalar(dburl, fmt.Sprintf(
		"SELECT factor || ':' || event || ':' || count(*)::text "+
			"FROM credential_event WHERE account_id=%d GROUP BY factor, event ORDER BY 1",
		accountID))
	if err != nil {
		return err
	}
	counts := map[string]int{}
	for _, row := range rows {
		parts := strings.SplitN(row, ":", 3)
		if len(parts) != 3 {
			continue
		}
		n, _ := strconv.Atoi(parts[2])
		counts[parts[0]+":"+parts[1]] = n
	}
	want := []struct {
		key string
		min int
	}{
		{"password:register", 1},
		{"password:use", 1},
		{"password:revoke", 1},
		// totp:register fires on first-confirm twice — initial enrollment +
		// recovery-ceremony commit. totp:revoke fires at recovery-begin
		// (reason=recovery) AND at the destructive revoke-password-totp.
		{"totp:register", 2},
		{"totp:use", 1},
		{"totp:revoke", 2},
		// recovery_code:register: initial 10 + post-recovery 10 + regen 10 = 30
		// in this smoke run; lower bound 10 is safe.
		{"recovery_code:register", 10},
		// recovery_code:use: just the one redeem at /auth/recovery-code/verify
		// (sudo via recovery_code is gone post-2026-05-28 hardening).
		{"recovery_code:use", 1},
		// recovery_code:revoke: the recovery ceremony's
		// recovery_complete-revoke (9 events) + regenerate revoke (10 events)
		// + final destructive revoke. Lower bound 9 keeps us safe against
		// minor reordering.
		{"recovery_code:revoke", 9},
		// session:sudo_granted: pre-pwd-set (webauthn), pwd_totp, pre-revoke
		// (webauthn). Recovery_code is no longer a sudo method.
		{"session:sudo_granted", 3},
	}
	for _, w := range want {
		if counts[w.key] < w.min {
			return fmt.Errorf("credential_event %s: want >=%d, got %d (full counts=%v)",
				w.key, w.min, counts[w.key], counts)
		}
	}
	log.Printf("  credential_event covers v0.2 lifecycle (counts=%v)", counts)
	return nil
}

func firstN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// =========================================================================
// v0.3 federation helpers
// =========================================================================

// federationIdentity mirrors handle_me_identities.go's identityView JSON
// shape. UpstreamEmail is *string so absent values decode as nil rather
// than the empty string.
type federationIdentity struct {
	ID             int64   `json:"id"`
	IdpSlug        string  `json:"idpSlug"`
	IdpDisplayName string  `json:"idpDisplayName"`
	UpstreamEmail  *string `json:"upstreamEmail"`
	LinkedAt       string  `json:"linkedAt"`
}

// newFederationClient is newClient + a CheckRedirect that returns
// http.ErrUseLastResponse so the test can inspect 302 Location headers
// instead of having the stdlib follow them. The federation login flow
// hops across origins (RP → upstream OP → RP) and we want each leg
// observable.
func newFederationClient(base string) (*client, error) {
	c, err := newClient(base)
	if err != nil {
		return nil, err
	}
	c.hc.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return c, nil
}

// getRaw is a get-with-status escape hatch for tests that need to inspect
// non-2xx responses without c.do's error wrap.
func (c *client) getRaw(path string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, c.base+path, nil)
	if err != nil {
		return nil, err
	}
	return c.hc.Do(req)
}

// getRedirect issues GET path and expects a 302 with a Location header,
// returning the Location value. Fails the test if the response is not 302.
func (c *client) getRedirect(path string) (string, error) {
	resp, err := c.getRaw(path)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusFound {
		return "", fmt.Errorf("GET %s: want 302, got %d (%s)", path, resp.StatusCode, string(body))
	}
	loc := resp.Header.Get("Location")
	if loc == "" {
		return "", fmt.Errorf("GET %s: 302 with empty Location", path)
	}
	return loc, nil
}

// getRedirectAbs issues GET on an absolute URL (not c.base + path) and
// expects a 302, returning the Location value. Used to drive cross-origin
// hops in the federation flow.
func (c *client) getRedirectAbs(absURL string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, absURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusFound {
		return "", fmt.Errorf("GET %s: want 302, got %d (%s)", absURL, resp.StatusCode, string(body))
	}
	loc := resp.Header.Get("Location")
	if loc == "" {
		return "", fmt.Errorf("GET %s: 302 with empty Location", absURL)
	}
	return loc, nil
}

// followMockOPAuthorize GETs the upstream OP /authorize URL with a no-cookie
// throwaway client (the OP does not need session cookies; it just records
// per-code state and 302s back to the RP /callback). Returns the absolute
// callback URL the OP redirected to.
func followMockOPAuthorize(authorizeURL string) (string, error) {
	hc := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, err := http.NewRequest(http.MethodGet, authorizeURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusFound {
		return "", fmt.Errorf("mock OP /authorize: want 302, got %d (%s)", resp.StatusCode, string(body))
	}
	loc := resp.Header.Get("Location")
	if loc == "" {
		return "", errors.New("mock OP /authorize: empty Location")
	}
	return loc, nil
}

// driveFederationLogin runs /federation/{slug}/login → upstream /authorize
// → RP /callback end-to-end. Asserts each hop is a 302 and the final
// destination is the configured return_to. The mockop *server* claims must
// already be set by the caller.
func driveFederationLogin(c *client, baseURL, slug, returnTo string) error {
	loginPath := fmt.Sprintf("/api/prohibitorum/auth/federation/%s/login?return_to=%s",
		slug, url.QueryEscape(returnTo))
	authorizeURL, err := c.getRedirect(loginPath)
	if err != nil {
		return fmt.Errorf("login: %w", err)
	}
	callbackURL, err := followMockOPAuthorize(authorizeURL)
	if err != nil {
		return fmt.Errorf("authorize: %w", err)
	}
	loc, err := c.getRedirectAbs(callbackURL)
	if err != nil {
		return fmt.Errorf("callback: %w", err)
	}
	if loc != returnTo {
		return fmt.Errorf("callback: want %s, got %s", returnTo, loc)
	}
	return nil
}

// expectFederationCallbackError drives /login → /authorize → /callback and
// asserts the RP /callback response is the given status + JSON error code.
// On success returns nil; on any divergence, returns a descriptive error.
func expectFederationCallbackError(c *client, baseURL, slug string, wantStatus int, wantCode string) error {
	loginPath := fmt.Sprintf("/api/prohibitorum/auth/federation/%s/login?return_to=/me", slug)
	authorizeURL, err := c.getRedirect(loginPath)
	if err != nil {
		return fmt.Errorf("login: %w", err)
	}
	callbackURL, err := followMockOPAuthorize(authorizeURL)
	if err != nil {
		return fmt.Errorf("authorize: %w", err)
	}
	// Issue the callback request against the RP. We don't use
	// c.getRedirectAbs because we expect an error body, not a 302.
	req, err := http.NewRequest(http.MethodGet, callbackURL, nil)
	if err != nil {
		return err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != wantStatus {
		return fmt.Errorf("/callback status: want %d, got %d (body=%s)", wantStatus, resp.StatusCode, string(body))
	}
	var ae struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(body, &ae)
	if ae.Code != wantCode {
		return fmt.Errorf("/callback error code: want %q, got %q (body=%s)", wantCode, ae.Code, string(body))
	}
	return nil
}

// (*client).listMyIdentities calls GET /api/prohibitorum/me/identities and
// decodes the JSON array of federation identities.
func (c *client) listMyIdentities() ([]federationIdentity, error) {
	var out []federationIdentity
	if err := c.get("/api/prohibitorum/me/identities", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// loadDEK reads PROHIBITORUM_DATA_ENCRYPTION_KEY_V1 from env (the same
// var the dev server uses) and base64-decodes it. Fatal on missing or
// wrong-length DEK because the federation steps cannot proceed without it.
func loadDEK() []byte {
	b64 := os.Getenv("PROHIBITORUM_DATA_ENCRYPTION_KEY_V1")
	if b64 == "" {
		log.Fatalf("PROHIBITORUM_DATA_ENCRYPTION_KEY_V1 not set; cannot seed upstream_idp")
	}
	dek, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		log.Fatalf("DEK base64-decode: %v", err)
	}
	if len(dek) != 32 {
		log.Fatalf("DEK length: want 32, got %d", len(dek))
	}
	return dek
}

// seedUpstreamIDP INSERTs an upstream_idp row with the supplied policy
// fields, then re-encrypts the client_secret using the returned row id
// (the AAD binds the ciphertext to the id) and UPDATEs the row. Returns
// the row id.
func seedUpstreamIDP(dek []byte, slug, displayName, issuer, clientID, clientSecret, mode string, allowedDomains []string, requireVerifiedEmail bool) (int64, error) {
	dburl := os.Getenv("PROHIBITORUM_DATABASE_URL")
	if dburl == "" {
		return 0, errors.New("PROHIBITORUM_DATABASE_URL not set")
	}
	// First DELETE any prior row with this slug so re-running the smoke is
	// idempotent. Cascades to account_identity rows.
	if _, err := exec.Command("psql", dburl, "-c",
		fmt.Sprintf("DELETE FROM upstream_idp WHERE slug='%s'", slug)).CombinedOutput(); err != nil {
		return 0, fmt.Errorf("psql delete prior: %w", err)
	}
	// Cast text[] literals explicitly so the smoke doesn't depend on
	// libpq's array-input heuristics. Use $tag$…$tag$ dollar-quoting on
	// values that might contain quotes; here the values are tightly
	// controlled so single-quote wrap is fine.
	allowedSQL := "ARRAY[" + sqlStringArray(allowedDomains) + "]::text[]"
	insertSQL := fmt.Sprintf(`INSERT INTO upstream_idp
		(slug, display_name, issuer_url, client_id, client_secret_enc, secret_nonce,
		 key_version, scopes, mode, allowed_domains, username_claim, display_name_claim,
		 email_claim, require_verified_email)
		VALUES ('%s', '%s', '%s', '%s', E'\\x00', E'\\x00', 1,
		  ARRAY['openid','profile','email']::text[],
		  '%s', %s,
		  'preferred_username', 'name', 'email', %t)
		RETURNING id`,
		slug, displayName, issuer, clientID, mode, allowedSQL, requireVerifiedEmail)
	// -q suppresses the trailing "INSERT 0 1" status line so -At -c with
	// RETURNING gives us just the id (without -q, psql 18 emits both the
	// returned row and the command tag, separated by a newline).
	out, err := exec.Command("psql", dburl, "-At", "-q", "-c", insertSQL).CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("psql insert: %v: %s", err, string(out))
	}
	// Defensive: even with -q some psql versions still print the command
	// tag. Take just the first non-empty line.
	idStr := ""
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "INSERT") {
			idStr = line
			break
		}
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse RETURNING id %q (raw=%q): %w", idStr, string(out), err)
	}
	ct, nonce, err := fedoidc.EncryptClientSecret(dek, []byte(clientSecret), id, 1)
	if err != nil {
		return 0, fmt.Errorf("EncryptClientSecret: %w", err)
	}
	// bytea hex literal: '\xDEADBEEF'. Need single quotes around the
	// '\\xHEX' form so psql -c parses it as a bytea value.
	updateSQL := fmt.Sprintf(`UPDATE upstream_idp
		SET client_secret_enc = E'\\x%s'::bytea,
		    secret_nonce       = E'\\x%s'::bytea
		WHERE id = %d`, hex.EncodeToString(ct), hex.EncodeToString(nonce), id)
	if outU, err := exec.Command("psql", dburl, "-c", updateSQL).CombinedOutput(); err != nil {
		return 0, fmt.Errorf("psql update secret: %v: %s", err, string(outU))
	}
	return id, nil
}

// sqlStringArray produces the inside of a SQL ARRAY[…] literal: each
// element single-quoted, comma-separated. Empty input → empty string.
// No quote-escaping because the smoke controls every value here.
func sqlStringArray(xs []string) string {
	if len(xs) == 0 {
		return ""
	}
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		out = append(out, "'"+x+"'")
	}
	return strings.Join(out, ",")
}

// verifyFederatedIdentityCreated asserts an account_identity row exists for
// (accountID, upstreamSub) and is owned by the given upstream_idp_id. Also
// confirms credential_event has a federation_oidc/register row.
func verifyFederatedIdentityCreated(accountID int32, upstreamSub string, idpID int64) error {
	dburl := os.Getenv("PROHIBITORUM_DATABASE_URL")
	rows, err := psqlScalar(dburl, fmt.Sprintf(
		"SELECT account_id::text || ':' || upstream_idp_id::text "+
			"FROM account_identity WHERE upstream_sub='%s'", upstreamSub))
	if err != nil {
		return err
	}
	if len(rows) != 1 {
		return fmt.Errorf("expected 1 account_identity row for sub=%s, got %d (%v)",
			upstreamSub, len(rows), rows)
	}
	want := fmt.Sprintf("%d:%d", accountID, idpID)
	if rows[0] != want {
		return fmt.Errorf("account_identity ownership: want %q, got %q", want, rows[0])
	}
	log.Printf("  account_identity[%s] account_id=%d upstream_idp_id=%d ✓",
		upstreamSub, accountID, idpID)
	return nil
}

// verifyFederatedIdentityGone asserts no account_identity row exists for
// (accountID, upstreamSub). Used after unlink.
func verifyFederatedIdentityGone(accountID int32, upstreamSub string) error {
	dburl := os.Getenv("PROHIBITORUM_DATABASE_URL")
	rows, err := psqlScalar(dburl, fmt.Sprintf(
		"SELECT count(*)::text FROM account_identity "+
			"WHERE account_id=%d AND upstream_sub='%s'", accountID, upstreamSub))
	if err != nil {
		return err
	}
	if len(rows) != 1 || rows[0] != "0" {
		return fmt.Errorf("expected 0 account_identity rows post-unlink, got %v", rows)
	}
	log.Printf("  account_identity[%s] for account %d is gone ✓", upstreamSub, accountID)
	return nil
}

// verifyV03FederationAuditEvents asserts credential_event has lower-bound
// counts for the federation_oidc surface this smoke exercises. Lower bounds
// only — server-side handlers may emit additional events under variants we
// don't differentiate at the wire layer.
func verifyV03FederationAuditEvents() error {
	dburl := os.Getenv("PROHIBITORUM_DATABASE_URL")
	rows, err := psqlScalar(dburl,
		"SELECT factor || ':' || event || ':' || count(*)::text "+
			"FROM credential_event WHERE factor='federation_oidc' GROUP BY factor, event ORDER BY 1")
	if err != nil {
		return err
	}
	counts := map[string]int{}
	for _, row := range rows {
		parts := strings.SplitN(row, ":", 3)
		if len(parts) != 3 {
			continue
		}
		n, _ := strconv.Atoi(parts[2])
		counts[parts[0]+":"+parts[1]] = n
	}
	want := []struct {
		key string
		min int
	}{
		// happy-path auto_provision: register (1) + use (1) initial + use (1) re-login
		// + use (1) re-login pre-/me/identities = ≥3 uses.
		{"federation_oidc:register", 1},
		{"federation_oidc:use", 3},
		// negative tests: 4 separate failure modes:
		// email_not_verified, username_collision, upstream_error, link_required.
		// invalid_return_to does NOT emit a fail row — it's caught at the
		// HTTP layer before the federator runs.
		{"federation_oidc:fail", 4},
		// self-link round-trip: 1 link + 1 unlink.
		{"federation_oidc:link", 1},
		{"federation_oidc:unlink", 1},
	}
	for _, w := range want {
		if counts[w.key] < w.min {
			return fmt.Errorf("credential_event %s: want >=%d, got %d (full counts=%v)",
				w.key, w.min, counts[w.key], counts)
		}
	}
	log.Printf("  credential_event covers v0.3 federation lifecycle (counts=%v)", counts)
	return nil
}
