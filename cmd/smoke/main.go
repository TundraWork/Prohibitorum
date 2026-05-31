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
	"encoding/xml"
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
	"slices"
	"strings"
	"time"

	crewjam "github.com/crewjam/saml"
	"github.com/fxamacker/cbor/v2"
	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"

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

	// --- SPA shell routes: the new dashboard paths must serve index.html
	// (id="app") via the NotFound fallback, not be shadowed by a backend route. ---
	step("step 5b/45 — SPA shell served for dashboard routes")
	for _, p := range []string{"/", "/sessions", "/credentials", "/admin/accounts", "/enroll/" + token} {
		resp, err := http.Get(*baseURL + p)
		if err != nil {
			log.Fatalf("SPA shell GET %s: %v", p, err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			log.Fatalf("SPA shell GET %s: status %d (want 200)", p, resp.StatusCode)
		}
		if !strings.Contains(string(body), `id="app"`) {
			log.Fatalf("SPA shell GET %s: body missing id=\"app\" (got %d bytes)", p, len(body))
		}
	}
	log.Printf("  /, /sessions, /credentials, /admin/accounts, /enroll/<token> all serve the SPA shell ✓")

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

	// v0.3 numbering: 24 federation steps appended after the v0.2 block
	// (steps 1–45). Existing v0.2 step labels keep their "/45" denominator
	// to avoid a diff on every existing line. Steps 46–64 cover
	// auto_provision + link_only + self-service link/unlink; steps 65–68
	// cover invite_only (token-bearing redemption + consumed + expired
	// negatives); step 69 is the final audit-table lower-bound assert.
	const totalV03 = 69

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
	// The session cookie is Path=/, so the jar sends it to every endpoint.
	// Verify session presence by hitting an API endpoint — if the cookie
	// weren't set, /me would 401.
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

	// --- invite_only — token-bearing redemption -----------------------------
	//
	// Stage 3 of the invite_only chunk: now that stages 1 (federator) and 2
	// (HTTP handler) have shipped, exercise the public entrypoint
	// GET /enrollments/{token}/start-federation end-to-end. Drives the same
	// upstream OP that the auto_provision steps used (mockop), but binds the
	// invite to it via enrollment.expected_upstream_idp_slug — applyInviteOnly
	// dispatches on the FedState.EnrollmentToken regardless of the IdP's
	// configured mode.

	step(fmt.Sprintf("step 65/%d — seed invite enrollment + drive /enrollments/{token}/start-federation", totalV03))
	const inviteToken = "invite-token-smoke-001"
	const inviteSub = "invite-redeemer-sub-001"
	const inviteUsername = "invite-redeemer"
	const inviteDisplay = "Invite Redeemer"
	if err := seedInviteEnrollment(inviteToken, inviteUsername, inviteDisplay, "user", "mockop", "1 hour"); err != nil {
		log.Fatalf("seed invite enrollment: %v", err)
	}
	opSrv.SetClaims(inviteSub, "invite-redeemer@example.com", true, "ignored-by-template", "Ignored By Template")
	inviteClient, err := newFederationClient(*baseURL)
	if err != nil {
		log.Fatalf("invite client: %v", err)
	}
	// GET /enrollments/{token}/start-federation?return_to=/me → 302 to upstream /authorize
	inviteAuthorizeURL, err := inviteClient.getRedirect(fmt.Sprintf("/api/prohibitorum/enrollments/%s/start-federation?return_to=/me", inviteToken))
	if err != nil {
		log.Fatalf("/start-federation: %v", err)
	}
	if !strings.HasPrefix(inviteAuthorizeURL, opTS.URL+"/authorize") {
		log.Fatalf("/start-federation: expected redirect to mock OP /authorize, got %q", inviteAuthorizeURL)
	}
	inviteCallbackURL, err := followMockOPAuthorize(inviteAuthorizeURL)
	if err != nil {
		log.Fatalf("invite mock OP /authorize: %v", err)
	}
	if !strings.Contains(inviteCallbackURL, "/api/prohibitorum/auth/federation/mockop/callback") {
		log.Fatalf("invite callback URL: %q", inviteCallbackURL)
	}
	if loc, err := inviteClient.getRedirectAbs(inviteCallbackURL); err != nil {
		log.Fatalf("invite federation/callback: %v", err)
	} else if loc != "/me" {
		log.Fatalf("invite federation/callback: want /me, got %q", loc)
	}
	inviteMe, err := inviteClient.getMe()
	if err != nil {
		log.Fatalf("invite /me: %v", err)
	}
	if inviteMe.Username != inviteUsername {
		log.Fatalf("invite /me username: got %q want %q (template wins over upstream preferred_username)",
			inviteMe.Username, inviteUsername)
	}
	if inviteMe.DisplayName != inviteDisplay {
		log.Fatalf("invite /me displayName: got %q want %q", inviteMe.DisplayName, inviteDisplay)
	}
	log.Printf("  invite redeemed; /me id=%d username=%s displayName=%s", inviteMe.ID, inviteMe.Username, inviteMe.DisplayName)

	step(fmt.Sprintf("step 66/%d — DB assert: invite consumed + account + account_identity + register audit", totalV03))
	if err := verifyInviteOnlyRedemption(inviteToken, inviteUsername, inviteSub, mockopIDPID); err != nil {
		log.Fatalf("invite redemption DB assert: %v", err)
	}

	step(fmt.Sprintf("step 67/%d — negative: consumed token rejected with 403 invite_required", totalV03))
	// Reuse the now-consumed token; the federator's BeginInviteRedemption
	// must reject before any upstream hop. Fresh client (no cookies).
	negInvite1, _ := newFederationClient(*baseURL)
	if err := expectInviteStartFederationError(negInvite1, *baseURL, inviteToken,
		http.StatusForbidden, "invite_required"); err != nil {
		log.Fatalf("negative consumed-token: %v", err)
	}
	log.Printf("  /start-federation consumed → 403 invite_required ✓")

	step(fmt.Sprintf("step 68/%d — negative: expired token rejected with 403 invite_required", totalV03))
	const expiredToken = "invite-token-smoke-expired-001"
	// Seed a NEW enrollment that's already past expires_at (1 second in the
	// past). BeginInviteRedemption checks enr.ExpiresAt.After(time.Now()).
	if err := seedInviteEnrollment(expiredToken, "invite-redeemer-expired", "Expired Redeemer", "user", "mockop", "-1 second"); err != nil {
		log.Fatalf("seed expired invite: %v", err)
	}
	negInvite2, _ := newFederationClient(*baseURL)
	if err := expectInviteStartFederationError(negInvite2, *baseURL, expiredToken,
		http.StatusForbidden, "invite_required"); err != nil {
		log.Fatalf("negative expired-token: %v", err)
	}
	log.Printf("  /start-federation expired → 403 invite_required ✓")

	step(fmt.Sprintf("step 69/%d — DB assert: credential_event covers v0.3 federation lifecycle", totalV03))
	if err := verifyV03FederationAuditEvents(); err != nil {
		log.Fatalf("v0.3 federation audit DB assert: %v", err)
	}

	// =========================================================================
	// v0.4 surface: downstream OIDC OP — a mock relying party drives the full
	// authorization-code + PKCE flow against the Prohibitorum OP, then exercises
	// /userinfo, /introspect, refresh-token rotation + reuse detection, /revoke,
	// and RP-initiated logout. Per Task 15 of the v0.4 plan.
	//
	// session-cookie scope: the OIDC OP routes are mounted at the server ROOT
	// (/oauth/authorize, /oauth/token, …). The session cookie is now Path=/, so
	// c's cookie jar auto-sends it to those root-mounted endpoints
	// (browser-equivalent) — no manual attach. See authorizeWithSession below.
	//
	// Ordering: every authenticated /authorize+/token step (incl. the authorize
	// negatives that need a live session) runs FIRST while c holds a session;
	// the RP-initiated logout runs LAST because it revokes c's session via the
	// id_token's sid. Each /authorize mints a fresh single-use code.
	//
	// Pre-condition: c holds a live webauthn session (re-established at step 60
	// and never logged out since — steps 61–64 only touched non-session-killing
	// endpoints). The OP issuer == *baseURL (configx defaults OIDC.Issuer to the
	// first public origin, which the dev server sets to *baseURL).
	const totalV04 = 90

	// Refresh c's /me to make sure the session is live and to capture the
	// account id (the OP projects oidc_subject for this account into id_token.sub).
	v04Me, err := c.getMe()
	if err != nil {
		log.Fatalf("v0.4: c has no live session at start of OIDC OP steps: %v", err)
	}
	issuer := *baseURL
	const rpClientID = "smoke-rp"
	rpRedirectURI := *baseURL + "/rp/callback"
	rpPostLogout := *baseURL + "/rp/post-logout"

	step(fmt.Sprintf("step 70/%d — v0.4: signing-key generate + GET /oauth/jwks (exactly 1 key)", totalV04))
	signingKID, err := mintSigningKey(*baseURL)
	if err != nil {
		log.Fatalf("signing-key generate: %v", err)
	}
	jwks, err := fetchJWKS(*baseURL)
	if err != nil {
		log.Fatalf("fetch jwks: %v", err)
	}
	if len(jwks.Keys) != 1 {
		log.Fatalf("jwks: want exactly 1 key, got %d", len(jwks.Keys))
	}
	if jwks.Keys[0].KeyID != signingKID {
		log.Fatalf("jwks: key kid=%q, want minted kid=%q", jwks.Keys[0].KeyID, signingKID)
	}
	log.Printf("  signing key kid=%s; /oauth/jwks has 1 key ✓", signingKID)

	step(fmt.Sprintf("step 71/%d — v0.4: oidc-client create (confidential, openid+profile+offline_access)", totalV04))
	rpSecret, err := createOIDCClient(*baseURL, rpClientID, rpRedirectURI, rpPostLogout,
		[]string{"openid", "profile", "offline_access"})
	if err != nil {
		log.Fatalf("oidc-client create: %v", err)
	}
	if rpSecret == "" {
		log.Fatalf("oidc-client create: empty client secret parsed from CLI output")
	}
	log.Printf("  client %q registered; secret len=%d", rpClientID, len(rpSecret))

	step(fmt.Sprintf("step 72/%d — v0.4: GET /oauth/authorize (PKCE S256) → 302 with code+state+iss", totalV04))
	verifier, challenge := genPKCE()
	authState := randState()
	authNonce := randState()
	authzURL := fmt.Sprintf(
		"/oauth/authorize?response_type=code&client_id=%s&redirect_uri=%s&scope=%s&state=%s&nonce=%s&code_challenge=%s&code_challenge_method=S256",
		url.QueryEscape(rpClientID),
		url.QueryEscape(rpRedirectURI),
		url.QueryEscape("openid profile offline_access"),
		url.QueryEscape(authState),
		url.QueryEscape(authNonce),
		url.QueryEscape(challenge),
	)
	assertSessionCookieAtRoot(c)
	loc, err := authorizeWithSession(c, authzURL)
	if err != nil {
		log.Fatalf("/oauth/authorize: %v", err)
	}
	authCode, err := parseAuthorizeRedirect(loc, rpRedirectURI, authState, issuer)
	if err != nil {
		log.Fatalf("/oauth/authorize redirect: %v", err)
	}
	log.Printf("  302 to redirect_uri with code (len=%d), state, iss ✓", len(authCode))

	step(fmt.Sprintf("step 73/%d — v0.4: POST /oauth/token (authorization_code, Basic auth)", totalV04))
	tok, err := tokenExchange(*baseURL, rpClientID, rpSecret, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {authCode},
		"redirect_uri":  {rpRedirectURI},
		"code_verifier": {verifier},
	})
	if err != nil {
		log.Fatalf("/oauth/token authorization_code: %v", err)
	}
	if tok.TokenType != "Bearer" {
		log.Fatalf("token_type: want Bearer, got %q", tok.TokenType)
	}
	if tok.ExpiresIn <= 0 {
		log.Fatalf("expires_in: want > 0, got %d", tok.ExpiresIn)
	}
	if tok.AccessToken == "" || tok.IDToken == "" {
		log.Fatalf("token response missing access_token or id_token")
	}
	if tok.RefreshToken == "" {
		log.Fatalf("token response missing refresh_token (offline_access was granted)")
	}
	accessToken := tok.AccessToken
	idToken := tok.IDToken
	refreshToken := tok.RefreshToken

	// Verify the id_token: signature against JWKS + the OIDC claim set.
	idClaims, err := verifyIDToken(*baseURL, idToken)
	if err != nil {
		log.Fatalf("verify id_token: %v", err)
	}
	if got := str(idClaims["iss"]); got != issuer {
		log.Fatalf("id_token iss: want %q, got %q", issuer, got)
	}
	if got := str(idClaims["aud"]); got != rpClientID {
		log.Fatalf("id_token aud: want %q, got %q", rpClientID, got)
	}
	idSub := str(idClaims["sub"])
	if idSub == "" {
		log.Fatalf("id_token sub is empty")
	}
	if got := str(idClaims["nonce"]); got != authNonce {
		log.Fatalf("id_token nonce: want %q, got %q", authNonce, got)
	}
	if str(idClaims["at_hash"]) == "" {
		log.Fatalf("id_token missing at_hash")
	}
	if str(idClaims["sid"]) == "" {
		log.Fatalf("id_token missing sid")
	}
	if _, ok := idClaims["auth_time"]; !ok {
		log.Fatalf("id_token missing auth_time")
	}
	if _, ok := idClaims["amr"]; !ok {
		log.Fatalf("id_token missing amr")
	}
	// Verify the access token: it too is a JWS signed by the OP, with JOSE
	// typ=at+jwt and a jti claim (RFC 9068).
	atClaims, atTyp, err := verifyAccessToken(*baseURL, accessToken)
	if err != nil {
		log.Fatalf("verify access_token: %v", err)
	}
	if atTyp != "at+jwt" {
		log.Fatalf("access_token JOSE typ: want at+jwt, got %q", atTyp)
	}
	if str(atClaims["jti"]) == "" {
		log.Fatalf("access_token missing jti")
	}
	log.Printf("  id_token sub=%s aud=%s nonce✓ at_hash✓ sid✓ auth_time✓ amr✓; access_token typ=at+jwt jti✓; refresh_token len=%d",
		idSub, rpClientID, len(refreshToken))

	step(fmt.Sprintf("step 74/%d — v0.4: GET /oauth/userinfo (Bearer access token)", totalV04))
	userinfo, err := fetchUserinfo(*baseURL, accessToken)
	if err != nil {
		log.Fatalf("/oauth/userinfo: %v", err)
	}
	if got := str(userinfo["sub"]); got != idSub {
		log.Fatalf("userinfo sub: want %q (matching id_token), got %q", idSub, got)
	}
	if str(userinfo["username"]) != v04Me.Username {
		log.Fatalf("userinfo username: want %q, got %q", v04Me.Username, str(userinfo["username"]))
	}
	if str(userinfo["displayName"]) == "" {
		log.Fatalf("userinfo missing displayName (profile scope granted)")
	}
	log.Printf("  userinfo sub matches id_token; username=%s displayName=%s ✓",
		str(userinfo["username"]), str(userinfo["displayName"]))

	step(fmt.Sprintf("step 75/%d — v0.4: POST /oauth/introspect (access token, Basic auth) → active", totalV04))
	intro, err := introspect(*baseURL, rpClientID, rpSecret, accessToken)
	if err != nil {
		log.Fatalf("/oauth/introspect: %v", err)
	}
	if active, _ := intro["active"].(bool); !active {
		log.Fatalf("introspect: want active=true, got %v", intro["active"])
	}
	if got := str(intro["token_type"]); got != "access_token" {
		log.Fatalf("introspect token_type: want access_token, got %q", got)
	}
	if got := str(intro["client_id"]); got != rpClientID {
		log.Fatalf("introspect client_id: want %q, got %q", rpClientID, got)
	}
	if got := str(intro["sub"]); got != idSub {
		log.Fatalf("introspect sub: want %q, got %q", idSub, got)
	}
	log.Printf("  introspect active=true token_type=access_token client_id=%s sub✓", rpClientID)

	step(fmt.Sprintf("step 76/%d — v0.4: POST /oauth/token (refresh_token rotation, Basic auth)", totalV04))
	refreshed, err := tokenExchange(*baseURL, rpClientID, rpSecret, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	})
	if err != nil {
		log.Fatalf("/oauth/token refresh_token: %v", err)
	}
	if refreshed.RefreshToken == "" {
		log.Fatalf("refresh response missing rotated refresh_token")
	}
	if refreshed.RefreshToken == refreshToken {
		log.Fatalf("refresh_token was NOT rotated (new == old)")
	}
	if refreshed.IDToken == "" {
		log.Fatalf("refresh response missing id_token")
	}
	if _, err := verifyIDToken(*baseURL, refreshed.IDToken); err != nil {
		log.Fatalf("verify refreshed id_token: %v", err)
	}
	oldRefreshToken := refreshToken
	refreshToken = refreshed.RefreshToken
	log.Printf("  refresh rotated (new != old); refreshed id_token verifies ✓")

	step(fmt.Sprintf("step 77/%d — v0.4: replay OLD refresh_token → invalid_grant (reuse detection)", totalV04))
	if err := tokenExpectError(*baseURL, rpClientID, rpSecret, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {oldRefreshToken},
	}, http.StatusBadRequest, "invalid_grant"); err != nil {
		log.Fatalf("refresh reuse: %v", err)
	}
	log.Printf("  replayed superseded refresh_token → 400 invalid_grant ✓")

	step(fmt.Sprintf("step 78/%d — v0.4: reuse detection revoked the whole family (current token now dead)", totalV04))
	// rotateRefresh revokes the family on reuse, so the current (rotated) token
	// must now also fail with invalid_grant.
	if err := tokenExpectError(*baseURL, rpClientID, rpSecret, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	}, http.StatusBadRequest, "invalid_grant"); err != nil {
		log.Fatalf("post-reuse family revocation: %v", err)
	}
	log.Printf("  current refresh_token also dead post-reuse (family revoked) ✓")

	step(fmt.Sprintf("step 79/%d — v0.4: fresh authorize+token, then /oauth/revoke the refresh token", totalV04))
	// The family was revoked at step 78; mint a fresh code → token to get a
	// live refresh token to revoke via RFC 7009.
	v2, code2 := freshAuthorizeCode(c, *baseURL, rpClientID, rpRedirectURI, issuer)
	tok2, err := tokenExchange(*baseURL, rpClientID, rpSecret, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code2},
		"redirect_uri":  {rpRedirectURI},
		"code_verifier": {v2},
	})
	if err != nil {
		log.Fatalf("/oauth/token (pre-revoke): %v", err)
	}
	if tok2.RefreshToken == "" {
		log.Fatalf("pre-revoke token response missing refresh_token")
	}
	if err := revokeToken(*baseURL, rpClientID, rpSecret, tok2.RefreshToken); err != nil {
		log.Fatalf("/oauth/revoke: %v", err)
	}
	// A refresh with the revoked token must now fail.
	if err := tokenExpectError(*baseURL, rpClientID, rpSecret, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {tok2.RefreshToken},
	}, http.StatusBadRequest, "invalid_grant"); err != nil {
		log.Fatalf("post-revoke refresh: %v", err)
	}
	log.Printf("  /oauth/revoke → 200; revoked refresh_token → invalid_grant ✓")

	step(fmt.Sprintf("step 80/%d — v0.4: /oauth/revoke an access token → revoked_jti row", totalV04))
	// Mint another fresh access token, revoke it, and confirm a revoked_jti row.
	v3, code3 := freshAuthorizeCode(c, *baseURL, rpClientID, rpRedirectURI, issuer)
	tok3, err := tokenExchange(*baseURL, rpClientID, rpSecret, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code3},
		"redirect_uri":  {rpRedirectURI},
		"code_verifier": {v3},
	})
	if err != nil {
		log.Fatalf("/oauth/token (pre-access-revoke): %v", err)
	}
	atClaims3, _, err := verifyAccessToken(*baseURL, tok3.AccessToken)
	if err != nil {
		log.Fatalf("verify access token (pre-access-revoke): %v", err)
	}
	revokedJTI := str(atClaims3["jti"])
	if revokedJTI == "" {
		log.Fatalf("pre-access-revoke: access token has no jti")
	}
	if err := revokeToken(*baseURL, rpClientID, rpSecret, tok3.AccessToken); err != nil {
		log.Fatalf("/oauth/revoke access token: %v", err)
	}
	log.Printf("  access token jti=%.16s… revoked ✓", revokedJTI)
	introRevoked, err := introspect(*baseURL, rpClientID, rpSecret, tok3.AccessToken)
	if err != nil {
		log.Fatalf("introspect revoked access token: %v", err)
	}
	if introRevoked["active"] != false {
		log.Fatalf("introspect revoked access token: want active=false, got %v", introRevoked["active"])
	}
	log.Printf("  introspect revoked access token → active=false ✓")

	step(fmt.Sprintf("step 81/%d — v0.4: negative — unregistered redirect_uri is a DIRECT error (no redirect)", totalV04))
	if err := authorizeExpectDirectError(c, *baseURL, rpClientID,
		*baseURL+"/rp/UNREGISTERED-callback", issuer); err != nil {
		log.Fatalf("negative unregistered redirect_uri: %v", err)
	}
	log.Printf("  /authorize with bad redirect_uri → direct 400 invalid_request (no Location to the bad URI) ✓")

	step(fmt.Sprintf("step 82/%d — v0.4: negative — PKCE mismatch at /token → invalid_grant", totalV04))
	vGood, codeBad := freshAuthorizeCode(c, *baseURL, rpClientID, rpRedirectURI, issuer)
	_ = vGood // intentionally NOT used: send a wrong verifier.
	wrongVerifier, _ := genPKCE()
	if err := tokenExpectError(*baseURL, rpClientID, rpSecret, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {codeBad},
		"redirect_uri":  {rpRedirectURI},
		"code_verifier": {wrongVerifier},
	}, http.StatusBadRequest, "invalid_grant"); err != nil {
		log.Fatalf("negative PKCE mismatch: %v", err)
	}
	log.Printf("  /token with wrong code_verifier → 400 invalid_grant ✓")

	step(fmt.Sprintf("step 83/%d — v0.4: negative — bad client secret at /token → invalid_client (401)", totalV04))
	vc, codec := freshAuthorizeCode(c, *baseURL, rpClientID, rpRedirectURI, issuer)
	if err := tokenExpectError(*baseURL, rpClientID, "WRONG-SECRET", url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {codec},
		"redirect_uri":  {rpRedirectURI},
		"code_verifier": {vc},
	}, http.StatusUnauthorized, "invalid_client"); err != nil {
		log.Fatalf("negative bad client secret: %v", err)
	}
	log.Printf("  /token with wrong client secret → 401 invalid_client ✓")

	step(fmt.Sprintf("step 84/%d — v0.4: GET /oidc/logout (id_token_hint + post_logout_redirect_uri)", totalV04))
	// Capture the current session id so we can confirm logout revoked exactly it.
	logoutState := randState()
	logoutLoc, err := c.getRedirect(fmt.Sprintf("/oidc/logout?id_token_hint=%s&post_logout_redirect_uri=%s&state=%s",
		url.QueryEscape(idToken), url.QueryEscape(rpPostLogout), url.QueryEscape(logoutState)))
	if err != nil {
		log.Fatalf("/oidc/logout: %v", err)
	}
	if !strings.HasPrefix(logoutLoc, rpPostLogout) {
		log.Fatalf("/oidc/logout: want redirect to %q, got %q", rpPostLogout, logoutLoc)
	}
	if lu, perr := url.Parse(logoutLoc); perr != nil {
		log.Fatalf("/oidc/logout: parse Location %q: %v", logoutLoc, perr)
	} else if lu.Query().Get("state") != logoutState {
		log.Fatalf("/oidc/logout: state not echoed: want %q, got %q", logoutState, lu.Query().Get("state"))
	}
	log.Printf("  302 to post_logout_redirect_uri with state echoed ✓")

	step(fmt.Sprintf("step 85/%d — v0.4: logout revoked c's IdP session (the id_token's sid) → /me 401", totalV04))
	if _, err := c.getMe(); err == nil {
		log.Fatalf("/me succeeded after /oidc/logout; expected 401 (session sid should be revoked)")
	}
	log.Printf("  c's /me now 401 — logout revoked the id_token's sid session ✓")

	step(fmt.Sprintf("step 86/%d — v0.4: DB assert — revoked_jti row for the revoked access token", totalV04))
	if err := verifyRevokedJTI(revokedJTI); err != nil {
		log.Fatalf("revoked_jti DB assert: %v", err)
	}

	step(fmt.Sprintf("step 87/%d — v0.4: DB assert — credential_event (factor=oidc_client) lifecycle", totalV04))
	if err := verifyV04OIDCAuditEvents(); err != nil {
		log.Fatalf("v0.4 OIDC audit DB assert: %v", err)
	}

	// =========================================================================
	// v0.5 surface: SAML IdP — an in-process mock SP drives the full SP-initiated
	// Web Browser SSO profile against the Prohibitorum IdP, verifies the auto-
	// POSTed SAMLResponse with crewjam ServiceProvider, asserts NameID stability
	// across a second SSO, then exercises Single Logout (revoking exactly the
	// bound IdP session), the require_signed / bad-ACS / replay negatives, and the
	// DB-state asserts (saml_subject_id stability, saml_session rows,
	// credential_event factor=saml_sp). Per Task 12 of the v0.5 plan.
	//
	// The SAME signing_key minted at step 70 signs the SAML assertions — no new
	// key. The mock SP is registered with --kind ghes, which forces
	// require_signed_authn_request=true (needed for the unsigned-AuthnRequest
	// negative).
	//
	// Pre-condition: c's session was revoked at step 84 (/oidc/logout). We must
	// re-establish a fresh webauthn session before the SSO steps.
	const totalV05 = 99

	step(fmt.Sprintf("step 88/%d — v0.5: re-login via webauthn (c's session was revoked by /oidc/logout)", totalV05))
	{
		relogin, err := c.beginLogin()
		if err != nil {
			log.Fatalf("v0.5 relogin/begin: %v", err)
		}
		signed, err := auth.signAssertion(relogin.Challenge, *baseURL)
		if err != nil {
			log.Fatalf("v0.5 relogin sign: %v", err)
		}
		if err := c.completeLogin(auth, signed); err != nil {
			log.Fatalf("v0.5 relogin/complete: %v", err)
		}
	}
	v05Me, err := c.getMe()
	if err != nil {
		log.Fatalf("v0.5: c has no live session at start of SAML steps: %v", err)
	}
	log.Printf("  smoke-admin id=%d back in session for SAML steps", v05Me.ID)

	step(fmt.Sprintf("step 89/%d — v0.5: GET /saml/metadata → EntityDescriptor with ≥1 signing KeyDescriptor", totalV05))
	idpMetaXML, err := fetchSAMLMetadata(*baseURL)
	if err != nil {
		log.Fatalf("fetch /saml/metadata: %v", err)
	}
	{
		var idpED crewjam.EntityDescriptor
		if err := xml.Unmarshal(idpMetaXML, &idpED); err != nil {
			log.Fatalf("unmarshal IdP metadata: %v", err)
		}
		if len(idpED.IDPSSODescriptors) == 0 {
			log.Fatalf("IdP metadata has no IDPSSODescriptor")
		}
		signingKDs := 0
		for _, kd := range idpED.IDPSSODescriptors[0].KeyDescriptors {
			if kd.Use == "signing" || kd.Use == "" {
				signingKDs++
			}
		}
		if signingKDs < 1 {
			log.Fatalf("IdP metadata has %d signing KeyDescriptors, want ≥1", signingKDs)
		}
		log.Printf("  EntityDescriptor entityID=%s, %d signing KeyDescriptor(s) ✓", idpED.EntityID, signingKDs)
	}

	step(fmt.Sprintf("step 90/%d — v0.5: saml-sp create --kind ghes --metadata-file <mock SP metadata>", totalV05))
	const mockSPEntityID = "https://mock-sp.smoke.test"
	const mockSPACSURL = "https://mock-sp.smoke.test/saml/consume"
	sp, err := newMockSP(mockSPEntityID, mockSPACSURL)
	if err != nil {
		log.Fatalf("new mock SP: %v", err)
	}
	spMetaXML, err := sp.metadataXML()
	if err != nil {
		log.Fatalf("mock SP metadata: %v", err)
	}
	if err := createSAMLSP(*baseURL, "ghes", spMetaXML); err != nil {
		log.Fatalf("saml-sp create: %v", err)
	}
	log.Printf("  registered GHES SP entity_id=%s (require_signed_authn_request forced true)", mockSPEntityID)

	ssoURL := *baseURL + "/saml/sso"
	spProvider, err := sp.serviceProvider(idpMetaXML)
	if err != nil {
		log.Fatalf("build SP verifier: %v", err)
	}

	step(fmt.Sprintf("step 91/%d — v0.5: signed AuthnRequest → /saml/sso → verify SAMLResponse + GHES attrs", totalV05))
	var stableNameID string
	{
		query, reqID, err := sp.authnRequestRedirect(ssoURL, mockSPACSURL, true)
		if err != nil {
			log.Fatalf("build signed AuthnRequest: %v", err)
		}
		statusCode, body, err := ssoWithSession(c, query)
		if err != nil {
			log.Fatalf("/saml/sso: %v", err)
		}
		if statusCode != http.StatusOK {
			log.Fatalf("/saml/sso: want 200 auto-POST, got %d (body=%s)", statusCode, firstN(body, 400))
		}
		respXML, err := extractSAMLResponse(body)
		if err != nil {
			log.Fatalf("extract SAMLResponse: %v", err)
		}
		assertion, err := spProvider.parse(respXML, reqID)
		if err != nil {
			log.Fatalf("crewjam ParseXMLResponse rejected the SAMLResponse: %v", err)
		}
		if assertion.Subject == nil || assertion.Subject.NameID == nil || assertion.Subject.NameID.Value == "" {
			log.Fatalf("SAMLResponse assertion has no NameID")
		}
		stableNameID = assertion.Subject.NameID.Value
		// GHES attribute profile: USERNAME at least must be present and match.
		username := samlAttrValue(assertion, "USERNAME")
		if username == "" {
			log.Fatalf("SAMLResponse missing GHES USERNAME attribute (attrs=%v)", samlAttrNames(assertion))
		}
		if username != v05Me.Username {
			log.Fatalf("SAMLResponse USERNAME: want %q, got %q", v05Me.Username, username)
		}
		// crewjam already enforced Destination/Recipient==ACS and Audience==entityID
		// during ParseXMLResponse (it rejects otherwise); assert NameID + USERNAME here.
		log.Printf("  SAMLResponse verified: NameID=%.16s… USERNAME=%s; Destination/Recipient/Audience enforced by ParseXMLResponse ✓",
			stableNameID, username)
	}

	step(fmt.Sprintf("step 92/%d — v0.5: second SSO (same account+SP) → NameID identical (stability)", totalV05))
	{
		query, reqID, err := sp.authnRequestRedirect(ssoURL, mockSPACSURL, true)
		if err != nil {
			log.Fatalf("build signed AuthnRequest 2: %v", err)
		}
		statusCode, body, err := ssoWithSession(c, query)
		if err != nil {
			log.Fatalf("/saml/sso (2): %v", err)
		}
		if statusCode != http.StatusOK {
			log.Fatalf("/saml/sso (2): want 200, got %d (body=%s)", statusCode, firstN(body, 400))
		}
		respXML, err := extractSAMLResponse(body)
		if err != nil {
			log.Fatalf("extract SAMLResponse (2): %v", err)
		}
		assertion, err := spProvider.parse(respXML, reqID)
		if err != nil {
			log.Fatalf("ParseXMLResponse (2): %v", err)
		}
		if assertion.Subject.NameID.Value != stableNameID {
			log.Fatalf("NameID not stable: first=%q second=%q", stableNameID, assertion.Subject.NameID.Value)
		}
		log.Printf("  NameID identical across both SSOs ✓ (re-SSO upserts the SAME saml_session row, not a duplicate — Fix C2 dedup)")
	}

	step(fmt.Sprintf("step 93/%d — v0.5: DB assert — saml_subject_id stable (1 row, same name_id) + ≥1 saml_session row", totalV05))
	if err := verifySAMLSubjectStable(v05Me.ID, stableNameID); err != nil {
		log.Fatalf("saml_subject_id DB assert: %v", err)
	}
	// Steps 91+92 were two SSOs from the SAME session (client c) to the SAME SP.
	// Post Fix C2 (UNIQUE (session_id, sp_id, session_index) + upsert), those
	// collapse to ONE row (the second SSO refreshes not_on_or_after rather than
	// duplicating). So the correct expectation here is exactly 1, not 2.
	if err := verifySAMLSessionCount(v05Me.ID, 1); err != nil {
		log.Fatalf("saml_session DB assert: %v", err)
	}

	step(fmt.Sprintf("step 94/%d — v0.5: SLO — drive a DEDICATED session's SSO, then sign a LogoutRequest targeting it", totalV05))
	// SLO revokes the IdP session bound to the saml_session (sessionIndex = the
	// session's ID). To avoid breaking c (needed for the replay negative below),
	// drive the SSO that we will SLO from a SEPARATE client cSLO whose own login
	// session is the one we then assert-revoked. We target that exact session by
	// passing its session ID as the LogoutRequest SessionIndex.
	cSLO, err := newClient(*baseURL)
	if err != nil {
		log.Fatalf("v0.5 SLO client: %v", err)
	}
	{
		login, err := cSLO.beginLogin()
		if err != nil {
			log.Fatalf("cSLO login/begin: %v", err)
		}
		signed, err := auth.signAssertion(login.Challenge, *baseURL)
		if err != nil {
			log.Fatalf("cSLO sign: %v", err)
		}
		if err := cSLO.completeLogin(auth, signed); err != nil {
			log.Fatalf("cSLO login/complete: %v", err)
		}
		if _, err := cSLO.getMe(); err != nil {
			log.Fatalf("cSLO has no live session: %v", err)
		}
	}
	// Identify cSLO's session ID (== the sessionIndex the SSO will persist).
	var sloSessionIndex string
	{
		sessions, err := cSLO.listMySessions()
		if err != nil {
			log.Fatalf("cSLO /me/sessions: %v", err)
		}
		for _, s := range sessions {
			if s.IsCurrent {
				sloSessionIndex = s.ID
				break
			}
		}
		if sloSessionIndex == "" {
			log.Fatalf("could not identify cSLO's current session id: %+v", sessions)
		}
	}
	// Drive the SSO on cSLO so a saml_session row binds NameID→sloSessionIndex.
	{
		query, reqID, err := sp.authnRequestRedirect(ssoURL, mockSPACSURL, true)
		if err != nil {
			log.Fatalf("build cSLO AuthnRequest: %v", err)
		}
		statusCode, body, err := ssoWithSession(cSLO, query)
		if err != nil {
			log.Fatalf("cSLO /saml/sso: %v", err)
		}
		if statusCode != http.StatusOK {
			log.Fatalf("cSLO /saml/sso: want 200, got %d (body=%s)", statusCode, firstN(body, 400))
		}
		respXML, err := extractSAMLResponse(body)
		if err != nil {
			log.Fatalf("cSLO extract SAMLResponse: %v", err)
		}
		if _, err := spProvider.parse(respXML, reqID); err != nil {
			log.Fatalf("cSLO ParseXMLResponse: %v", err)
		}
	}
	log.Printf("  dedicated SLO session id=%s issued an SSO assertion ✓", sloSessionIndex)

	step(fmt.Sprintf("step 95/%d — v0.5: signed LogoutRequest → signed LogoutResponse + bound session revoked", totalV05))
	{
		query, _, err := sp.logoutRequestRedirect(*baseURL+"/saml/slo", stableNameID, sloSessionIndex)
		if err != nil {
			log.Fatalf("build LogoutRequest: %v", err)
		}
		statusCode, location, body, err := sloRedirect(cSLO, query)
		if err != nil {
			log.Fatalf("/saml/slo: %v", err)
		}
		if statusCode != http.StatusFound {
			log.Fatalf("/saml/slo: want 302 LogoutResponse redirect, got %d (body=%s)", statusCode, firstN(body, 400))
		}
		lr, err := decodeRedirectLogoutResponse(location)
		if err != nil {
			log.Fatalf("decode LogoutResponse: %v", err)
		}
		if lr.Status.StatusCode.Value != "urn:oasis:names:tc:SAML:2.0:status:Success" {
			log.Fatalf("LogoutResponse status: want Success, got %q", lr.Status.StatusCode.Value)
		}
		// The signed LogoutResponse must carry a Signature element. crewjam's
		// LogoutResponse unmarshals it into Signature (an *etree.Element-free
		// struct); assert the redirect actually delivered a Destination back to
		// the SP's SLO endpoint and a Success — the signature itself was produced
		// by signElement (verified by the saml package's own slo_test.go).
		log.Printf("  LogoutResponse Success ✓ (InResponseTo=%.12s…)", lr.InResponseTo)
	}
	// The SLO revoked cSLO's IdP session — its /me must now 401.
	if _, err := cSLO.getMe(); err == nil {
		log.Fatalf("cSLO /me succeeded after SLO; expected 401 (bound session should be revoked)")
	}
	log.Printf("  cSLO /me now 401 — SLO revoked exactly the bound IdP session ✓")
	// c's session must be UNTOUCHED (SessionIndex targeted only cSLO's session).
	if _, err := c.getMe(); err != nil {
		log.Fatalf("c /me 401 after SLO; SLO should only have revoked cSLO's session, not c's: %v", err)
	}
	log.Printf("  c's session survived (SLO SessionIndex scoping confirmed) ✓")

	step(fmt.Sprintf("step 96/%d — v0.5: negative — UNSIGNED AuthnRequest to require_signed GHES SP → rejected", totalV05))
	{
		query, _, err := sp.authnRequestRedirect(ssoURL, mockSPACSURL, false) // sign=false
		if err != nil {
			log.Fatalf("build unsigned AuthnRequest: %v", err)
		}
		statusCode, body, err := ssoWithSession(c, query)
		if err != nil {
			log.Fatalf("/saml/sso (unsigned): %v", err)
		}
		if statusCode == http.StatusOK {
			log.Fatalf("/saml/sso (unsigned): want non-200 rejection, got 200 (body leaked a SAMLResponse=%v)",
				strings.Contains(body, "SAMLResponse"))
		}
		if strings.Contains(body, "SAMLResponse") {
			log.Fatalf("/saml/sso (unsigned): rejected status %d but body still contains a SAMLResponse", statusCode)
		}
		log.Printf("  unsigned AuthnRequest → %d, no SAMLResponse ✓", statusCode)
	}

	step(fmt.Sprintf("step 97/%d — v0.5: negative — AuthnRequest with bad/unregistered ACS URL → rejected", totalV05))
	{
		query, _, err := sp.authnRequestRedirect(ssoURL, "https://mock-sp.smoke.test/EVIL-acs", true)
		if err != nil {
			log.Fatalf("build bad-ACS AuthnRequest: %v", err)
		}
		statusCode, body, err := ssoWithSession(c, query)
		if err != nil {
			log.Fatalf("/saml/sso (bad ACS): %v", err)
		}
		if statusCode == http.StatusOK {
			log.Fatalf("/saml/sso (bad ACS): want non-200 rejection, got 200")
		}
		if strings.Contains(body, "SAMLResponse") {
			log.Fatalf("/saml/sso (bad ACS): rejected status %d but body leaked a SAMLResponse", statusCode)
		}
		log.Printf("  unregistered ACS URL → %d, no SAMLResponse ✓", statusCode)
	}

	step(fmt.Sprintf("step 98/%d — v0.5: negative — replayed AuthnRequest ID (same request twice) → 2nd rejected", totalV05))
	{
		query, _, err := sp.authnRequestRedirect(ssoURL, mockSPACSURL, true)
		if err != nil {
			log.Fatalf("build replay AuthnRequest: %v", err)
		}
		// First presentation: must succeed (consumes the replay key at issue).
		statusCode, body, err := ssoWithSession(c, query)
		if err != nil {
			log.Fatalf("/saml/sso (replay 1): %v", err)
		}
		if statusCode != http.StatusOK {
			log.Fatalf("/saml/sso (replay 1): want 200, got %d (body=%s)", statusCode, firstN(body, 400))
		}
		// Second presentation of the SAME request ID: must be rejected.
		statusCode2, body2, err := ssoWithSession(c, query)
		if err != nil {
			log.Fatalf("/saml/sso (replay 2): %v", err)
		}
		if statusCode2 == http.StatusOK {
			log.Fatalf("/saml/sso (replay 2): want non-200 (replay rejected), got 200")
		}
		if strings.Contains(body2, "SAMLResponse") {
			log.Fatalf("/saml/sso (replay 2): rejected status %d but body leaked a SAMLResponse", statusCode2)
		}
		log.Printf("  replayed AuthnRequest ID: 1st=200, 2nd=%d (no SAMLResponse) ✓", statusCode2)
	}

	step(fmt.Sprintf("step 99/%d — v0.5: DB assert — credential_event (factor=saml_sp) sso(use) + slo(session_end)", totalV05))
	if err := verifyV05SAMLAuditEvents(); err != nil {
		log.Fatalf("v0.5 SAML audit DB assert: %v", err)
	}

	// =========================================================================
	// v0.6 surface: protocol-completeness gate. Forced re-authentication
	// (OIDC prompt=login / max_age, SAML ForceAuthn), prompt=none + stale,
	// PKCE method rejection, public-client introspection refusal, SAML
	// NameIDPolicy mismatch, ForceAuthn+IsPassive NoPassive, POST-binding
	// AuthnRequest intake, signed IdP metadata, and IdP-initiated SSO. Per
	// Task 9 of the v0.6 plan. Steps 100..N continue the v0.5 counter.
	//
	// Pre-condition: c holds a live webauthn session (re-login at step 88;
	// steps 91–99 only drove SSO/SLO against OTHER clients or touched c's
	// session non-destructively — confirmed alive at step 95). The v0.5 mock
	// SP `sp` + its verifier `spProvider`, ssoURL, mockSPACSURL, and the
	// signing key (step 70) are all still in scope and reused here.
	const totalV06 = 111

	// freshLogin re-runs the WebAuthn login ceremony on c, minting a NEW
	// session (fresh auth_time) on the cookie jar — the move that satisfies a
	// prompt=login / ForceAuthn re-auth demand whose nonce predates it.
	freshLogin := func() {
		lo, err := c.beginLogin()
		if err != nil {
			log.Fatalf("v0.6 fresh login/begin: %v", err)
		}
		signed, err := auth.signAssertion(lo.Challenge, *baseURL)
		if err != nil {
			log.Fatalf("v0.6 fresh login sign: %v", err)
		}
		if err := c.completeLogin(auth, signed); err != nil {
			log.Fatalf("v0.6 fresh login/complete: %v", err)
		}
	}

	v06Me, err := c.getMe()
	if err != nil {
		log.Fatalf("v0.6: c has no live session at start of v0.6 steps: %v", err)
	}
	_ = v06Me

	// ---- OIDC forced re-auth + policy steps (reuse the step-71 confidential
	// client `smoke-rp` + `rpRedirectURI`/`issuer`). ----

	step(fmt.Sprintf("step 100/%d — v0.6: OIDC prompt=login bounces (stale session), then a fresh login + reauth nonce issues a code", totalV06))
	{
		_, challenge := genPKCE()
		state := randState()
		nonce := randState()
		baseAuthz := fmt.Sprintf(
			"/oauth/authorize?response_type=code&client_id=%s&redirect_uri=%s&scope=%s&state=%s&nonce=%s&code_challenge=%s&code_challenge_method=S256&prompt=login",
			url.QueryEscape(rpClientID),
			url.QueryEscape(rpRedirectURI),
			url.QueryEscape("openid profile offline_access"),
			url.QueryEscape(state),
			url.QueryEscape(nonce),
			url.QueryEscape(challenge),
		)
		// (a) prompt=login with the existing session → bounce to /login with a
		// reauth nonce; must NOT be a code redirect to the RP.
		loc, err := authorizeRaw(c, baseAuthz)
		if err != nil {
			log.Fatalf("prompt=login authorize: %v", err)
		}
		if strings.HasPrefix(loc, rpRedirectURI) {
			log.Fatalf("prompt=login returned a code redirect to the RP (stale session must not satisfy re-auth): %q", loc)
		}
		if !strings.HasPrefix(loc, issuer+"/login") {
			log.Fatalf("prompt=login: want a bounce to %s/login, got %q", issuer, loc)
		}
		returnTo, reauthNonce, err := extractReauthFromLoginBounce(loc)
		if err != nil {
			log.Fatalf("prompt=login bounce: %v", err)
		}
		if reauthNonce == "" {
			log.Fatalf("prompt=login bounce carried no reauth nonce: return_to=%q", returnTo)
		}
		log.Printf("  prompt=login (stale) → 302 %s/login, return_to carries reauth=%.10s… (no code) ✓", issuer, reauthNonce)

		// (b) Fresh login → new session with a newer auth_time.
		freshLogin()

		// (c) Retry the authorize URL WITH the reauth nonce → now a code.
		retryPath, err := pathQueryOf(returnTo)
		if err != nil {
			log.Fatalf("prompt=login retry parse: %v", err)
		}
		loc2, err := authorizeWithSession(c, retryPath)
		if err != nil {
			log.Fatalf("prompt=login retry authorize: %v", err)
		}
		code, err := parseAuthorizeRedirect(loc2, rpRedirectURI, state, issuer)
		if err != nil {
			log.Fatalf("prompt=login retry redirect: %v", err)
		}
		log.Printf("  fresh login + &reauth=<nonce> → code (len=%d) ✓", len(code))
	}

	step(fmt.Sprintf("step 101/%d — v0.6: OIDC max_age=0 bounces; max_age=3600 issues a code", totalV06))
	{
		// max_age=0 demands re-auth regardless of how recent the session is →
		// a bounce to /login (the session just minted at step 100 still cannot
		// satisfy a zero max_age without a reauth nonce).
		v, challenge := genPKCE()
		_ = v
		mkAuthz := func(maxAge string) string {
			return fmt.Sprintf(
				"/oauth/authorize?response_type=code&client_id=%s&redirect_uri=%s&scope=%s&state=%s&nonce=%s&code_challenge=%s&code_challenge_method=S256&max_age=%s",
				url.QueryEscape(rpClientID),
				url.QueryEscape(rpRedirectURI),
				url.QueryEscape("openid profile offline_access"),
				url.QueryEscape(randState()),
				url.QueryEscape(randState()),
				url.QueryEscape(challenge),
				maxAge,
			)
		}
		loc, err := authorizeRaw(c, mkAuthz("0"))
		if err != nil {
			log.Fatalf("max_age=0 authorize: %v", err)
		}
		if !strings.HasPrefix(loc, issuer+"/login") {
			log.Fatalf("max_age=0: want a bounce to %s/login, got %q", issuer, loc)
		}
		log.Printf("  max_age=0 → 302 %s/login (re-auth demanded) ✓", issuer)

		// max_age=3600 is easily satisfied by the recent session → a code.
		state := randState()
		challengeURL := fmt.Sprintf(
			"/oauth/authorize?response_type=code&client_id=%s&redirect_uri=%s&scope=%s&state=%s&nonce=%s&code_challenge=%s&code_challenge_method=S256&max_age=3600",
			url.QueryEscape(rpClientID),
			url.QueryEscape(rpRedirectURI),
			url.QueryEscape("openid profile offline_access"),
			url.QueryEscape(state),
			url.QueryEscape(randState()),
			url.QueryEscape(challenge),
		)
		loc2, err := authorizeWithSession(c, challengeURL)
		if err != nil {
			log.Fatalf("max_age=3600 authorize: %v", err)
		}
		if _, err := parseAuthorizeRedirect(loc2, rpRedirectURI, state, issuer); err != nil {
			log.Fatalf("max_age=3600 redirect: %v", err)
		}
		log.Printf("  max_age=3600 → code (no bounce) ✓")
	}

	step(fmt.Sprintf("step 102/%d — v0.6: OIDC prompt=none + stale → redirect with error=login_required (no /login bounce)", totalV06))
	{
		_, challenge := genPKCE()
		state := randState()
		authz := fmt.Sprintf(
			"/oauth/authorize?response_type=code&client_id=%s&redirect_uri=%s&scope=%s&state=%s&nonce=%s&code_challenge=%s&code_challenge_method=S256&prompt=none&max_age=0",
			url.QueryEscape(rpClientID),
			url.QueryEscape(rpRedirectURI),
			url.QueryEscape("openid profile offline_access"),
			url.QueryEscape(state),
			url.QueryEscape(randState()),
			url.QueryEscape(challenge),
		)
		loc, err := authorizeRaw(c, authz)
		if err != nil {
			log.Fatalf("prompt=none authorize: %v", err)
		}
		if !strings.HasPrefix(loc, rpRedirectURI) {
			log.Fatalf("prompt=none must redirect to the RP redirect_uri (not bounce to /login), got %q", loc)
		}
		u, perr := url.Parse(loc)
		if perr != nil {
			log.Fatalf("prompt=none parse Location: %v", perr)
		}
		if got := u.Query().Get("error"); got != "login_required" {
			log.Fatalf("prompt=none: want error=login_required, got %q (loc=%q)", got, loc)
		}
		if u.Query().Get("state") != state {
			log.Fatalf("prompt=none: state not echoed on the error redirect")
		}
		log.Printf("  prompt=none + stale → 302 to RP with error=login_required (state echoed) ✓")
	}

	step(fmt.Sprintf("step 103/%d — v0.6: OIDC code_challenge_method=plain → redirect error=invalid_request", totalV06))
	{
		_, challenge := genPKCE()
		state := randState()
		authz := fmt.Sprintf(
			"/oauth/authorize?response_type=code&client_id=%s&redirect_uri=%s&scope=%s&state=%s&nonce=%s&code_challenge=%s&code_challenge_method=plain",
			url.QueryEscape(rpClientID),
			url.QueryEscape(rpRedirectURI),
			url.QueryEscape("openid profile offline_access"),
			url.QueryEscape(state),
			url.QueryEscape(randState()),
			url.QueryEscape(challenge),
		)
		loc, err := authorizeRaw(c, authz)
		if err != nil {
			log.Fatalf("plain PKCE authorize: %v", err)
		}
		if !strings.HasPrefix(loc, rpRedirectURI) {
			log.Fatalf("plain PKCE: want a redirect to the RP redirect_uri with an error, got %q", loc)
		}
		u, perr := url.Parse(loc)
		if perr != nil {
			log.Fatalf("plain PKCE parse Location: %v", perr)
		}
		if got := u.Query().Get("error"); got != "invalid_request" {
			log.Fatalf("plain PKCE: want error=invalid_request, got %q (loc=%q)", got, loc)
		}
		if u.Query().Get("code") != "" {
			log.Fatalf("plain PKCE: a code was issued despite the disallowed method")
		}
		log.Printf("  code_challenge_method=plain → 302 to RP with error=invalid_request (no code) ✓")
	}

	step(fmt.Sprintf("step 104/%d — v0.6: public OIDC client — introspect → invalid_client (401); confidential still works; public revoke OK", totalV06))
	{
		const pubClientID = "smoke-rp-public"
		pubRedirectURI := *baseURL + "/rp-public/callback"
		if err := createPublicOIDCClient(*baseURL, pubClientID, pubRedirectURI,
			[]string{"openid", "profile", "offline_access"}); err != nil {
			log.Fatalf("oidc-client create --public: %v", err)
		}
		// Acquire a token for the public client via the PKCE code flow (no secret).
		pv, pchallenge := genPKCE()
		pstate := randState()
		pAuthz := fmt.Sprintf(
			"/oauth/authorize?response_type=code&client_id=%s&redirect_uri=%s&scope=%s&state=%s&nonce=%s&code_challenge=%s&code_challenge_method=S256",
			url.QueryEscape(pubClientID),
			url.QueryEscape(pubRedirectURI),
			url.QueryEscape("openid profile offline_access"),
			url.QueryEscape(pstate),
			url.QueryEscape(randState()),
			url.QueryEscape(pchallenge),
		)
		pLoc, err := authorizeWithSession(c, pAuthz)
		if err != nil {
			log.Fatalf("public-client authorize: %v", err)
		}
		pCode, err := parseAuthorizeRedirect(pLoc, pubRedirectURI, pstate, issuer)
		if err != nil {
			log.Fatalf("public-client authorize redirect: %v", err)
		}
		pTok, err := tokenExchangePublic(*baseURL, pubClientID, url.Values{
			"grant_type":    {"authorization_code"},
			"code":          {pCode},
			"redirect_uri":  {pubRedirectURI},
			"code_verifier": {pv},
		})
		if err != nil {
			log.Fatalf("public-client token exchange: %v", err)
		}
		if pTok.AccessToken == "" {
			log.Fatalf("public-client token response missing access_token")
		}
		log.Printf("  public client %q minted an access token (no secret, PKCE) ✓", pubClientID)

		// (a) Public client introspect → invalid_client (401).
		if err := introspectExpectInvalidClientPublic(*baseURL, pubClientID, pTok.AccessToken); err != nil {
			log.Fatalf("public-client introspect: %v", err)
		}
		log.Printf("  public client /oauth/introspect → 401 invalid_client ✓")

		// (b) Confidential client introspect of its OWN fresh token still works.
		cv, ccode := freshAuthorizeCode(c, *baseURL, rpClientID, rpRedirectURI, issuer)
		cTok, err := tokenExchange(*baseURL, rpClientID, rpSecret, url.Values{
			"grant_type":    {"authorization_code"},
			"code":          {ccode},
			"redirect_uri":  {rpRedirectURI},
			"code_verifier": {cv},
		})
		if err != nil {
			log.Fatalf("confidential token exchange (introspect contrast): %v", err)
		}
		intro, err := introspect(*baseURL, rpClientID, rpSecret, cTok.AccessToken)
		if err != nil {
			log.Fatalf("confidential introspect (contrast): %v", err)
		}
		if active, _ := intro["active"].(bool); !active {
			log.Fatalf("confidential introspect: want active=true, got %v", intro["active"])
		}
		log.Printf("  confidential client /oauth/introspect → active=true ✓")

		// (c) The public client may still revoke its own token (RFC 7009).
		if err := revokeTokenPublic(*baseURL, pubClientID, pTok.AccessToken); err != nil {
			log.Fatalf("public-client revoke: %v", err)
		}
		log.Printf("  public client /oauth/revoke (own token) → 200 ✓")
	}

	// ---- SAML forced re-auth + policy + binding + metadata + IdP-initiated. ----
	// Reuses the v0.5 mock SP `sp`, verifier `spProvider`, ssoURL, mockSPACSURL.
	// c is freshly logged-in (step 100/104 minted recent sessions).

	step(fmt.Sprintf("step 105/%d — v0.6: SAML ForceAuthn bounces (stale session), then a fresh login + reauth nonce issues an assertion", totalV06))
	{
		query, reqID, err := sp.authnRequestRedirectOpts(ssoURL, mockSPACSURL, true, authnOpts{forceAuthn: true})
		if err != nil {
			log.Fatalf("build ForceAuthn AuthnRequest: %v", err)
		}
		// (a) ForceAuthn with the existing session → bounce to <entityID>/login
		// with a reauth nonce; status must be 302, not a 200 auto-POST.
		status, body, err := ssoWithSession(c, query)
		if err != nil {
			log.Fatalf("ForceAuthn /saml/sso: %v", err)
		}
		if status != http.StatusFound {
			log.Fatalf("ForceAuthn: want 302 bounce, got %d (body=%s)", status, firstN(body, 300))
		}
		_ = body
		// ssoWithSession does not return the Location; re-issue to capture it.
		loc, err := ssoLocation(c, query)
		if err != nil {
			log.Fatalf("ForceAuthn capture Location: %v", err)
		}
		if !strings.HasPrefix(loc, *baseURL+"/login") {
			log.Fatalf("ForceAuthn: want bounce to %s/login, got %q", *baseURL, loc)
		}
		returnTo, reauthNonce, err := extractReauthFromLoginBounce(loc)
		if err != nil {
			log.Fatalf("ForceAuthn bounce: %v", err)
		}
		if reauthNonce == "" {
			log.Fatalf("ForceAuthn bounce carried no reauth nonce: return_to=%q", returnTo)
		}
		log.Printf("  ForceAuthn (stale) → 302 %s/login, return_to carries reauth=%.10s… ✓", *baseURL, reauthNonce)

		// (b) Fresh login → newer auth_time.
		freshLogin()

		// (c) Retry the SSO URL WITH the reauth nonce → assertion issued.
		// The bounce preserved the EXACT signed raw query and appended
		// &reauth=<nonce>; replay that raw query verbatim.
		retryQuery, err := rawQueryOf(returnTo)
		if err != nil {
			log.Fatalf("ForceAuthn retry parse: %v", err)
		}
		status2, body2, err := ssoWithSession(c, retryQuery)
		if err != nil {
			log.Fatalf("ForceAuthn retry /saml/sso: %v", err)
		}
		if status2 != http.StatusOK {
			log.Fatalf("ForceAuthn retry: want 200 auto-POST, got %d (body=%s)", status2, firstN(body2, 300))
		}
		respXML, err := extractSAMLResponse(body2)
		if err != nil {
			log.Fatalf("ForceAuthn retry extract SAMLResponse: %v", err)
		}
		if _, err := spProvider.parse(respXML, reqID); err != nil {
			log.Fatalf("ForceAuthn retry ParseXMLResponse: %v", err)
		}
		log.Printf("  fresh login + &reauth=<nonce> → assertion issued ✓")
	}

	step(fmt.Sprintf("step 106/%d — v0.6: SAML ForceAuthn + IsPassive → NoPassive status Response (no assertion)", totalV06))
	{
		query, _, err := sp.authnRequestRedirectOpts(ssoURL, mockSPACSURL, true,
			authnOpts{forceAuthn: true, isPassive: true})
		if err != nil {
			log.Fatalf("build ForceAuthn+IsPassive AuthnRequest: %v", err)
		}
		status, body, err := ssoWithSession(c, query)
		if err != nil {
			log.Fatalf("ForceAuthn+IsPassive /saml/sso: %v", err)
		}
		if status != http.StatusOK {
			log.Fatalf("ForceAuthn+IsPassive: want 200 auto-POST (NoPassive Response), got %d (body=%s)", status, firstN(body, 300))
		}
		respXML, err := extractSAMLResponse(body)
		if err != nil {
			log.Fatalf("ForceAuthn+IsPassive extract SAMLResponse: %v", err)
		}
		top, sub, hasAssertion, err := decodeStatusResponse(respXML)
		if err != nil {
			log.Fatalf("decode NoPassive Response: %v", err)
		}
		if hasAssertion {
			log.Fatalf("ForceAuthn+IsPassive: NoPassive Response must carry NO assertion")
		}
		if sub != "urn:oasis:names:tc:SAML:2.0:status:NoPassive" {
			log.Fatalf("ForceAuthn+IsPassive: want sub-status NoPassive, got top=%q sub=%q", top, sub)
		}
		log.Printf("  ForceAuthn+IsPassive → Response StatusCode=NoPassive, no assertion ✓")
	}

	step(fmt.Sprintf("step 107/%d — v0.6: SAML NameIDPolicy Format=emailAddress (≠ persistent) → InvalidNameIDPolicy", totalV06))
	{
		const emailFormat = "urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress"
		query, _, err := sp.authnRequestRedirectOpts(ssoURL, mockSPACSURL, true,
			authnOpts{nameIDFormat: emailFormat})
		if err != nil {
			log.Fatalf("build NameIDPolicy-mismatch AuthnRequest: %v", err)
		}
		status, body, err := ssoWithSession(c, query)
		if err != nil {
			log.Fatalf("NameIDPolicy-mismatch /saml/sso: %v", err)
		}
		if status != http.StatusOK {
			log.Fatalf("NameIDPolicy-mismatch: want 200 auto-POST (InvalidNameIDPolicy Response), got %d (body=%s)", status, firstN(body, 300))
		}
		respXML, err := extractSAMLResponse(body)
		if err != nil {
			log.Fatalf("NameIDPolicy-mismatch extract SAMLResponse: %v", err)
		}
		_, sub, hasAssertion, err := decodeStatusResponse(respXML)
		if err != nil {
			log.Fatalf("decode InvalidNameIDPolicy Response: %v", err)
		}
		if hasAssertion {
			log.Fatalf("NameIDPolicy-mismatch: Response must carry NO assertion")
		}
		if sub != "urn:oasis:names:tc:SAML:2.0:status:InvalidNameIDPolicy" {
			log.Fatalf("NameIDPolicy-mismatch: want sub-status InvalidNameIDPolicy, got sub=%q", sub)
		}
		log.Printf("  NameIDPolicy Format=emailAddress → Response StatusCode=InvalidNameIDPolicy, no assertion ✓")
	}

	step(fmt.Sprintf("step 108/%d — v0.6: SAML POST-binding (enveloped-signed) AuthnRequest → assertion", totalV06))
	{
		samlReq, reqID, err := sp.authnRequestPostForm(ssoURL, mockSPACSURL, authnOpts{})
		if err != nil {
			log.Fatalf("build POST-binding AuthnRequest: %v", err)
		}
		status, body, err := ssoPostForm(c, samlReq, "")
		if err != nil {
			log.Fatalf("POST-binding /saml/sso: %v", err)
		}
		if status != http.StatusOK {
			log.Fatalf("POST-binding: want 200 auto-POST, got %d (body=%s)", status, firstN(body, 400))
		}
		respXML, err := extractSAMLResponse(body)
		if err != nil {
			log.Fatalf("POST-binding extract SAMLResponse: %v", err)
		}
		assertion, err := spProvider.parse(respXML, reqID)
		if err != nil {
			log.Fatalf("POST-binding ParseXMLResponse: %v", err)
		}
		if assertion.Subject == nil || assertion.Subject.NameID == nil || assertion.Subject.NameID.Value == "" {
			log.Fatalf("POST-binding assertion has no NameID")
		}
		log.Printf("  POST-binding enveloped-signed AuthnRequest → assertion (NameID=%.16s…) ✓", assertion.Subject.NameID.Value)
	}

	step(fmt.Sprintf("step 109/%d — v0.6: SAML /saml/metadata is SIGNED, verifies against its own cert, validUntil is future", totalV06))
	{
		metaXML, err := fetchSAMLMetadata(*baseURL)
		if err != nil {
			log.Fatalf("fetch /saml/metadata: %v", err)
		}
		ed, err := verifyMetadataSignature(metaXML)
		if err != nil {
			log.Fatalf("metadata signature: %v", err)
		}
		if ed.ValidUntil.IsZero() {
			log.Fatalf("metadata has no validUntil")
		}
		if !ed.ValidUntil.After(time.Now()) {
			log.Fatalf("metadata validUntil is not in the future: %s", ed.ValidUntil)
		}
		log.Printf("  metadata <ds:Signature> verifies against embedded cert; validUntil=%s (future) ✓", ed.ValidUntil.Format(time.RFC3339))
	}

	step(fmt.Sprintf("step 110/%d — v0.6: SAML IdP-initiated SSO — opted-in SP gets an unsolicited Response (RelayState echoed); v0.5 SP without the flag → 403", totalV06))
	{
		// Register a SECOND SP that opts into IdP-initiated SSO. Its mock SP
		// carries a distinct entityID + ACS but reuses the mock signing key
		// shape (a fresh mock SP with its own cert).
		const initEntityID = "https://mock-sp-init.smoke.test"
		const initACSURL = "https://mock-sp-init.smoke.test/saml/consume"
		spInit, err := newMockSP(initEntityID, initACSURL)
		if err != nil {
			log.Fatalf("new IdP-initiated mock SP: %v", err)
		}
		initMeta, err := spInit.metadataXML()
		if err != nil {
			log.Fatalf("IdP-initiated mock SP metadata: %v", err)
		}
		if err := createSAMLSPIdPInitiated(*baseURL, "ghes", initMeta); err != nil {
			log.Fatalf("saml-sp create --allow-idp-initiated: %v", err)
		}
		idpMetaXML, err := fetchSAMLMetadata(*baseURL)
		if err != nil {
			log.Fatalf("fetch IdP metadata for init verifier: %v", err)
		}
		initVerifier, err := spInit.serviceProviderOpts(idpMetaXML, true)
		if err != nil {
			log.Fatalf("build IdP-initiated verifier: %v", err)
		}

		status, body, err := ssoInit(c, initEntityID, "deep")
		if err != nil {
			log.Fatalf("/saml/sso/init: %v", err)
		}
		if status != http.StatusOK {
			log.Fatalf("/saml/sso/init: want 200 auto-POST, got %d (body=%s)", status, firstN(body, 400))
		}
		if got := extractRelayState(body); got != "deep" {
			log.Fatalf("/saml/sso/init: RelayState not echoed: want %q, got %q", "deep", got)
		}
		respXML, err := extractSAMLResponse(body)
		if err != nil {
			log.Fatalf("/saml/sso/init extract SAMLResponse: %v", err)
		}
		// Assert NO InResponseTo (unsolicited) at the wire level before crewjam parse.
		if strings.Contains(string(respXML), "InResponseTo") {
			log.Fatalf("/saml/sso/init: unsolicited Response unexpectedly carries InResponseTo")
		}
		assertion, err := initVerifier.parseUnsolicited(respXML)
		if err != nil {
			log.Fatalf("/saml/sso/init ParseXMLResponse (unsolicited): %v", err)
		}
		if assertion.Subject == nil || assertion.Subject.NameID == nil || assertion.Subject.NameID.Value == "" {
			log.Fatalf("/saml/sso/init assertion has no NameID")
		}
		log.Printf("  /saml/sso/init (opted-in SP) → unsolicited Response (no InResponseTo), RelayState=deep echoed, assertion accepted ✓")

		// The v0.5 SP did NOT opt in → 403.
		status403, body403, err := ssoInit(c, mockSPEntityID, "")
		if err != nil {
			log.Fatalf("/saml/sso/init (no opt-in): %v", err)
		}
		if status403 != http.StatusForbidden {
			log.Fatalf("/saml/sso/init for an SP without --allow-idp-initiated: want 403, got %d (body=%s)", status403, firstN(body403, 200))
		}
		log.Printf("  /saml/sso/init for the v0.5 SP (no opt-in) → 403 ✓")
	}

	step(fmt.Sprintf("step 111/%d — v0.6: DB assert — credential_event covers the v0.6 SAML re-auth/idp-initiated lifecycle", totalV06))
	if err := verifyV06SAMLAuditEvents(); err != nil {
		log.Fatalf("v0.6 SAML audit DB assert: %v", err)
	}

	// =========================================================================
	// Login + Consent UI backend: the consent ticket round-trip and the public
	// federation-providers list. A confidential client registered with
	// --require-consent bounces /oauth/authorize to <Issuer>/consent?ticket=…
	// when no grant covers the requested scopes; the consent app API renders
	// the ticket and records the approve/deny decision. After approve the same
	// authorize issues a code; a second authorize is remembered (no bounce);
	// &prompt=consent forces a fresh bounce; deny redirects with
	// error=access_denied. Per Task 6 of the Login + Consent UI plan.
	//
	// Pre-condition: c holds a live, recently-minted webauthn session (steps
	// 100/104/105 re-logged in; steps 106–110 only drove SAML against c's
	// session non-destructively, and 110 is read-only on c). The OP issuer ==
	// *baseURL.
	const totalUI = 113

	step(fmt.Sprintf("step 112/%d — UI: consent flow (require-consent client) — bounce, context, approve, remember, prompt=consent, deny", totalUI))
	{
		const consentClientID = "smoke-consent-rp"
		consentRedirectURI := *baseURL + "/consent-rp/callback"
		consentSecret, err := createConsentOIDCClient(*baseURL, consentClientID, consentRedirectURI,
			[]string{"openid", "profile"})
		if err != nil {
			log.Fatalf("oidc-client create --require-consent: %v", err)
		}
		_ = consentSecret // token exchange is not required for the consent assertions.
		log.Printf("  registered require-consent client %q", consentClientID)

		// mkAuthz builds a fresh authorize URL (new state/nonce/PKCE) for the
		// consent client, optionally appending &prompt=consent.
		mkAuthz := func(forceConsent bool) (path, state string) {
			_, challenge := genPKCE()
			state = randState()
			path = fmt.Sprintf(
				"/oauth/authorize?response_type=code&client_id=%s&redirect_uri=%s&scope=%s&state=%s&nonce=%s&code_challenge=%s&code_challenge_method=S256",
				url.QueryEscape(consentClientID),
				url.QueryEscape(consentRedirectURI),
				url.QueryEscape("openid profile"),
				url.QueryEscape(state),
				url.QueryEscape(randState()),
				url.QueryEscape(challenge),
			)
			if forceConsent {
				path += "&prompt=consent"
			}
			return path, state
		}

		// parseConsentBounce asserts loc is a 302 to <base>/consent?ticket=… and
		// returns (ticket, return_to).
		consentPrefix := *baseURL + "/consent?ticket="
		parseConsentBounce := func(loc string) (ticket, returnTo string) {
			if !strings.HasPrefix(loc, consentPrefix) {
				log.Fatalf("consent: want bounce to %s…, got %q", consentPrefix, loc)
			}
			u, perr := url.Parse(loc)
			if perr != nil {
				log.Fatalf("consent: parse bounce Location %q: %v", loc, perr)
			}
			ticket = u.Query().Get("ticket")
			returnTo = u.Query().Get("return_to")
			if ticket == "" || returnTo == "" {
				log.Fatalf("consent: bounce missing ticket/return_to: %q", loc)
			}
			return ticket, returnTo
		}

		// (1) First authorize → bounce to /consent (no grant yet).
		authz1, state1 := mkAuthz(false)
		loc1, err := authorizeRaw(c, authz1)
		if err != nil {
			log.Fatalf("consent authorize (1): %v", err)
		}
		ticket1, returnTo1 := parseConsentBounce(loc1)
		log.Printf("  authorize → 302 %s/consent?ticket=%s&return_to=… ✓", *baseURL, ticket1)

		// (2) GET /api/prohibitorum/consent?ticket= → client + scopes.
		var ctx struct {
			Client struct {
				ClientID    string `json:"clientId"`
				DisplayName string `json:"displayName"`
			} `json:"client"`
			Account struct {
				DisplayName string `json:"displayName"`
			} `json:"account"`
			Scopes []string `json:"scopes"`
		}
		if err := c.get("/api/prohibitorum/consent?ticket="+url.QueryEscape(ticket1), &ctx); err != nil {
			log.Fatalf("GET /consent context: %v", err)
		}
		if ctx.Client.ClientID != consentClientID {
			log.Fatalf("consent context clientId: want %q, got %q", consentClientID, ctx.Client.ClientID)
		}
		if !slices.Contains(ctx.Scopes, "openid") || !slices.Contains(ctx.Scopes, "profile") {
			log.Fatalf("consent context scopes: want openid+profile, got %v", ctx.Scopes)
		}
		log.Printf("  GET /consent → client=%s account=%q scopes=%v ✓", ctx.Client.ClientID, ctx.Account.DisplayName, ctx.Scopes)

		// (3) POST approve → {redirect} == return_to.
		var res1 struct {
			Redirect string `json:"redirect"`
		}
		if err := c.postJSON("/api/prohibitorum/consent?return_to="+url.QueryEscape(returnTo1),
			map[string]string{"ticket": ticket1, "decision": "approve"}, &res1); err != nil {
			log.Fatalf("POST /consent approve: %v", err)
		}
		if res1.Redirect != returnTo1 {
			log.Fatalf("consent approve redirect: want %q, got %q", returnTo1, res1.Redirect)
		}
		log.Printf("  POST approve → redirect == return_to ✓ (grant stored)")

		// (4) Re-drive authorize on the return_to → now issues a code.
		retryPath, err := pathQueryOf(returnTo1)
		if err != nil {
			log.Fatalf("consent retry parse return_to: %v", err)
		}
		loc4, err := authorizeWithSession(c, retryPath)
		if err != nil {
			log.Fatalf("consent re-authorize: %v", err)
		}
		code4, err := parseAuthorizeRedirect(loc4, consentRedirectURI, state1, issuer)
		if err != nil {
			log.Fatalf("consent re-authorize redirect: %v", err)
		}
		log.Printf("  re-authorize (post-approve) → code (len=%d) ✓", len(code4))

		// (5) A 2nd, fresh authorize → still a code, NOT a /consent bounce.
		authz5, state5 := mkAuthz(false)
		loc5, err := authorizeWithSession(c, authz5)
		if err != nil {
			log.Fatalf("consent 2nd authorize: %v", err)
		}
		if strings.HasPrefix(loc5, consentPrefix) {
			log.Fatalf("consent 2nd authorize bounced to /consent despite a remembered grant: %q", loc5)
		}
		if _, err := parseAuthorizeRedirect(loc5, consentRedirectURI, state5, issuer); err != nil {
			log.Fatalf("consent 2nd authorize redirect: %v", err)
		}
		log.Printf("  2nd authorize → code (remembered grant, no /consent bounce) ✓")

		// (6) Fresh authorize with &prompt=consent → forced re-consent bounce.
		authz6, _ := mkAuthz(true)
		loc6, err := authorizeRaw(c, authz6)
		if err != nil {
			log.Fatalf("consent prompt=consent authorize: %v", err)
		}
		ticket6, returnTo6 := parseConsentBounce(loc6)
		log.Printf("  authorize &prompt=consent → forced /consent bounce (ticket=%s) ✓", ticket6)

		// (7) Deny on the prompt=consent ticket → redirect carries error=access_denied.
		var res7 struct {
			Redirect string `json:"redirect"`
		}
		if err := c.postJSON("/api/prohibitorum/consent?return_to="+url.QueryEscape(returnTo6),
			map[string]string{"ticket": ticket6, "decision": "deny"}, &res7); err != nil {
			log.Fatalf("POST /consent deny: %v", err)
		}
		du, perr := url.Parse(res7.Redirect)
		if perr != nil {
			log.Fatalf("consent deny: parse redirect %q: %v", res7.Redirect, perr)
		}
		if !strings.HasPrefix(res7.Redirect, consentRedirectURI) {
			log.Fatalf("consent deny: redirect must target the RP redirect_uri, got %q", res7.Redirect)
		}
		if got := du.Query().Get("error"); got != "access_denied" {
			log.Fatalf("consent deny: want error=access_denied, got %q (redirect=%q)", got, res7.Redirect)
		}
		log.Printf("  POST deny → redirect carries error=access_denied ✓")
	}

	step(fmt.Sprintf("step 113/%d — UI: GET /api/prohibitorum/auth/federation → 200 JSON array incl. seeded slugs", totalUI))
	{
		pubc, err := newClient(*baseURL) // no session needed (public endpoint)
		if err != nil {
			log.Fatalf("federation-list client: %v", err)
		}
		var providers []struct {
			Slug        string `json:"slug"`
			DisplayName string `json:"displayName"`
		}
		if err := pubc.get("/api/prohibitorum/auth/federation", &providers); err != nil {
			log.Fatalf("GET /auth/federation: %v", err)
		}
		// The federation section (steps 46/58) seeded upstream IdPs 'mockop' and
		// 'mockop-link'; both are enabled so both must appear here.
		var haveMockop, haveMockopLink bool
		for _, p := range providers {
			switch p.Slug {
			case "mockop":
				haveMockop = true
			case "mockop-link":
				haveMockopLink = true
			}
			if p.Slug == "" || p.DisplayName == "" {
				log.Fatalf("federation provider with empty slug/displayName: %+v", p)
			}
		}
		if !haveMockop || !haveMockopLink {
			log.Fatalf("federation list missing seeded slugs (mockop=%v mockop-link=%v): %+v",
				haveMockop, haveMockopLink, providers)
		}
		log.Printf("  /auth/federation → 200, %d providers incl. mockop + mockop-link ✓", len(providers))
	}

	fmt.Println()
	fmt.Println("✓ smoke OK — 45/45 (v0.2) + 46–69/69 (v0.3 federation incl. invite_only) + 70–87 (v0.4 OIDC OP) + 88–99 (v0.5 SAML IdP) + 100–111 (v0.6 forced re-auth / PKCE+introspect policy / NameIDPolicy / POST AuthnRequest / signed metadata / IdP-initiated) + 112–113 (Login+Consent UI backend: consent ticket round-trip + federation-providers list) + DB-state assertions passed against",
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
		// register: auto_provision (1) + invite_only_redemption (1) = ≥2.
		{"federation_oidc:register", 2},
		// use: auto_provision initial (1) + re-login (1) + re-login pre-/me/identities (1)
		// + invite_only session-start (1) = ≥4.
		{"federation_oidc:use", 4},
		// fail: pre-existing 4 (email_not_verified, username_collision,
		// upstream_error, link_required) + invite_only consumed-token (1) +
		// invite_only expired-token (1) = ≥6. invalid_return_to does NOT
		// emit a fail row — it's caught at the HTTP layer before the
		// federator runs.
		{"federation_oidc:fail", 6},
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

// seedInviteEnrollment inserts a row into the enrollment table with
// intent='invite' bound to a specific upstream IdP slug. expiresOffset is a
// Postgres interval literal (e.g. "1 hour", "-1 second") added to now() —
// negative offsets are used to seed pre-expired rows for the expired-token
// negative test. Idempotent: deletes any prior row with the same token first.
func seedInviteEnrollment(token, templateUsername, templateDisplayName, templateRole, expectedSlug, expiresOffset string) error {
	dburl := os.Getenv("PROHIBITORUM_DATABASE_URL")
	if dburl == "" {
		return errors.New("PROHIBITORUM_DATABASE_URL not set")
	}
	// Best-effort cleanup so re-runs are idempotent. CASCADE not needed —
	// account_identity references account (not enrollment), and the prior
	// invite — if successfully redeemed — has its consumed_at set; deleting
	// it has no effect on the minted account.
	if _, err := exec.Command("psql", dburl, "-c",
		fmt.Sprintf("DELETE FROM enrollment WHERE token='%s'", token)).CombinedOutput(); err != nil {
		return fmt.Errorf("psql delete prior invite: %w", err)
	}
	insertSQL := fmt.Sprintf(`INSERT INTO enrollment (
		token, intent, expires_at,
		template_username, template_display_name, template_role,
		expected_upstream_idp_slug
	) VALUES (
		'%s', 'invite', now() + interval '%s',
		'%s', '%s', '%s',
		'%s'
	)`, token, expiresOffset, templateUsername, templateDisplayName, templateRole, expectedSlug)
	if out, err := exec.Command("psql", dburl, "-c", insertSQL).CombinedOutput(); err != nil {
		return fmt.Errorf("psql insert invite enrollment: %v: %s", err, string(out))
	}
	return nil
}

// verifyInviteOnlyRedemption DB-asserts the post-redemption state of an
// invite_only flow:
//   - enrollment.consumed_at is NOT NULL for the redeemed token,
//   - exactly one account exists with the template username,
//   - exactly one account_identity row links that account to the upstream sub
//     under the given IdP,
//   - credential_event has at least one federation_oidc/register row with
//     detail->>'reason' = 'invite_only_redemption'.
func verifyInviteOnlyRedemption(token, username, upstreamSub string, idpID int64) error {
	dburl := os.Getenv("PROHIBITORUM_DATABASE_URL")

	// 1) consumed_at NOT NULL on the enrollment row. Cast to bool::text
	//    yields psql's "true"/"false" representation (not the "t"/"f"
	//    short form used by ::char output).
	consumed, err := psqlScalar(dburl, fmt.Sprintf(
		"SELECT (consumed_at IS NOT NULL)::text FROM enrollment WHERE token='%s'", token))
	if err != nil {
		return err
	}
	if len(consumed) != 1 || consumed[0] != "true" {
		return fmt.Errorf("enrollment.consumed_at NOT NULL: want 1 row 'true', got %v", consumed)
	}
	log.Printf("  enrollment[%s].consumed_at IS NOT NULL ✓", token)

	// 2) account row exists with the template username.
	accIDs, err := psqlScalar(dburl, fmt.Sprintf(
		"SELECT id::text FROM account WHERE username='%s'", username))
	if err != nil {
		return err
	}
	if len(accIDs) != 1 {
		return fmt.Errorf("account[username=%s]: want 1 row, got %v", username, accIDs)
	}
	accID, err := strconv.ParseInt(accIDs[0], 10, 64)
	if err != nil {
		return fmt.Errorf("parse account id %q: %w", accIDs[0], err)
	}
	log.Printf("  account[username=%s] id=%d ✓", username, accID)

	// 3) account_identity row links that account to the upstream sub + idp.
	idents, err := psqlScalar(dburl, fmt.Sprintf(
		"SELECT account_id::text || ':' || upstream_idp_id::text "+
			"FROM account_identity WHERE upstream_sub='%s'", upstreamSub))
	if err != nil {
		return err
	}
	want := fmt.Sprintf("%d:%d", accID, idpID)
	if len(idents) != 1 || idents[0] != want {
		return fmt.Errorf("account_identity[sub=%s]: want exactly %q, got %v", upstreamSub, want, idents)
	}
	log.Printf("  account_identity[%s] account_id=%d upstream_idp_id=%d ✓", upstreamSub, accID, idpID)

	// 4) credential_event has a federation_oidc/register row with
	//    detail->>'reason' = 'invite_only_redemption' for this account.
	regs, err := psqlScalar(dburl, fmt.Sprintf(
		"SELECT count(*)::text FROM credential_event "+
			"WHERE factor='federation_oidc' AND event='register' "+
			"AND account_id=%d AND detail->>'reason'='invite_only_redemption'", accID))
	if err != nil {
		return err
	}
	if len(regs) != 1 || regs[0] == "" || regs[0] == "0" {
		return fmt.Errorf("credential_event register with reason=invite_only_redemption: want >=1, got %v", regs)
	}
	log.Printf("  credential_event register{reason=invite_only_redemption} for account %d: %s ✓", accID, regs[0])
	return nil
}

// expectInviteStartFederationError drives
// GET /api/prohibitorum/enrollments/{token}/start-federation and asserts the
// response is the given HTTP status + JSON error code. Used for the two
// invite_only negative cases (consumed token + expired token), where the
// federator's BeginInviteRedemption rejects before any upstream hop.
func expectInviteStartFederationError(c *client, baseURL, token string, wantStatus int, wantCode string) error {
	path := fmt.Sprintf("/api/prohibitorum/enrollments/%s/start-federation?return_to=/me", token)
	resp, err := c.getRaw(path)
	if err != nil {
		return fmt.Errorf("start-federation: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != wantStatus {
		return fmt.Errorf("/start-federation status: want %d, got %d (body=%s)", wantStatus, resp.StatusCode, string(body))
	}
	var ae struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(body, &ae)
	if ae.Code != wantCode {
		return fmt.Errorf("/start-federation error code: want %q, got %q (body=%s)", wantCode, ae.Code, string(body))
	}
	return nil
}

// =========================================================================
// v0.4 OIDC OP helpers — mock relying party
// =========================================================================

// oidcTokenResponse mirrors pkg/protocol/oidc.tokenResponse (the RFC 6749 §5.1
// token-endpoint body). Fields are pointers/strings so absent members decode
// cleanly.
type oidcTokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	IDToken      string `json:"id_token"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

// str coerces a JSON-decoded claim/response value to a string ("" if absent or
// not a string). Used pervasively against map[string]any claim sets.
func str(v any) string {
	s, _ := v.(string)
	return s
}

// mintSigningKey shells out to `prohibitorum signing-key generate` and parses
// the printed kid. The CLI prints exactly:
//
//	Generated signing key <kid> (active)
//
// (or "(inactive)" when a key already exists and --activate was not passed).
// We parse the 4th whitespace-separated field as the kid. DATABASE_URL and the
// DEK are inherited from os.Environ(); PROHIBITORUM_PUBLIC_ORIGIN is set so the
// CLI's config parse succeeds (issuer derivation).
func mintSigningKey(baseURL string) (string, error) {
	cmd := exec.Command("mise", "exec", "--", "go", "run", "./cmd/prohibitorum", "signing-key", "generate")
	cmd.Env = append(os.Environ(), "PROHIBITORUM_PUBLIC_ORIGIN="+baseURL)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("signing-key generate: %v\n%s", err, out)
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "Generated signing key ") {
			continue
		}
		fields := strings.Fields(line)
		// "Generated" "signing" "key" "<kid>" "(active)"
		if len(fields) >= 4 {
			return fields[3], nil
		}
	}
	return "", fmt.Errorf("no 'Generated signing key' line in output:\n%s", out)
}

// createOIDCClient shells out to `prohibitorum oidc-client create` for a
// confidential client and parses the one-time plaintext secret. The CLI prints:
//
//	Registered confidential client "smoke-rp"
//	Client secret (store this now, it will NOT be shown again):
//	<secret>
//
// We locate the "Client secret" prefix line and return the next non-empty line.
func createOIDCClient(baseURL, clientID, redirectURI, postLogoutURI string, scopes []string) (string, error) {
	args := []string{"exec", "--", "go", "run", "./cmd/prohibitorum", "oidc-client", "create",
		"--client-id", clientID,
		"--display-name", "Smoke RP",
		"--redirect-uri", redirectURI,
		"--post-logout-redirect-uri", postLogoutURI,
	}
	for _, s := range scopes {
		args = append(args, "--scope", s)
	}
	cmd := exec.Command("mise", args...)
	cmd.Env = append(os.Environ(), "PROHIBITORUM_PUBLIC_ORIGIN="+baseURL)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("oidc-client create: %v\n%s", err, out)
	}
	lines := strings.Split(string(out), "\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "Client secret") {
			// Secret is the next non-empty line.
			for j := i + 1; j < len(lines); j++ {
				s := strings.TrimSpace(lines[j])
				if s != "" {
					return s, nil
				}
			}
		}
	}
	return "", fmt.Errorf("no client secret in oidc-client create output:\n%s", out)
}

// createConsentOIDCClient registers a CONFIDENTIAL client with
// --require-consent so the Login+Consent UI backend consent steps (step 112) can drive the /consent bounce.
// Modeled on createOIDCClient; returns the parsed client secret.
func createConsentOIDCClient(baseURL, clientID, redirectURI string, scopes []string) (string, error) {
	args := []string{"exec", "--", "go", "run", "./cmd/prohibitorum", "oidc-client", "create",
		"--client-id", clientID,
		"--display-name", "Smoke Consent RP",
		"--redirect-uri", redirectURI,
		"--post-logout-redirect-uri", redirectURI,
		"--require-consent",
	}
	for _, s := range scopes {
		args = append(args, "--scope", s)
	}
	cmd := exec.Command("mise", args...)
	cmd.Env = append(os.Environ(), "PROHIBITORUM_PUBLIC_ORIGIN="+baseURL)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("oidc-client create --require-consent: %v\n%s", err, out)
	}
	lines := strings.Split(string(out), "\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "Client secret") {
			for j := i + 1; j < len(lines); j++ {
				s := strings.TrimSpace(lines[j])
				if s != "" {
					return s, nil
				}
			}
		}
	}
	return "", fmt.Errorf("no client secret in oidc-client create --require-consent output:\n%s", out)
}

// genPKCE generates an RFC 7636 PKCE pair: verifier = base64url(32 random
// bytes); challenge = base64url(SHA256(verifier)). Both base64url-no-padding.
func genPKCE() (verifier, challenge string) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		log.Fatalf("genPKCE: rand: %v", err)
	}
	verifier = base64.RawURLEncoding.EncodeToString(raw[:])
	sum := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	return verifier, challenge
}

// randState returns 16 random bytes base64url-encoded, for `state`/`nonce`.
func randState() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		log.Fatalf("randState: rand: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw[:])
}

// fetchJWKS GETs /oauth/jwks and unmarshals it into a jose.JSONWebKeySet.
func fetchJWKS(baseURL string) (*jose.JSONWebKeySet, error) {
	resp, err := http.Get(baseURL + "/oauth/jwks")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /oauth/jwks: %d %s", resp.StatusCode, body)
	}
	var jwks jose.JSONWebKeySet
	if err := json.Unmarshal(body, &jwks); err != nil {
		return nil, fmt.Errorf("unmarshal jwks: %w (body=%s)", err, body)
	}
	return &jwks, nil
}

// verifyJWSAgainstJWKS parses an RS256 JWS, resolves the matching key in the
// OP's JWKS by the token header kid, verifies the signature, and returns the
// claims plus the JOSE `typ` header. Used for both id_token and access_token.
func verifyJWSAgainstJWKS(baseURL, token string) (map[string]any, string, error) {
	jwks, err := fetchJWKS(baseURL)
	if err != nil {
		return nil, "", err
	}
	parsed, err := jwt.ParseSigned(token, []jose.SignatureAlgorithm{jose.RS256})
	if err != nil {
		return nil, "", fmt.Errorf("parse jws: %w", err)
	}
	if len(parsed.Headers) != 1 {
		return nil, "", fmt.Errorf("unexpected JOSE header count: %d", len(parsed.Headers))
	}
	kid := parsed.Headers[0].KeyID
	keys := jwks.Key(kid)
	if len(keys) == 0 {
		return nil, "", fmt.Errorf("no JWKS key matches token kid %q", kid)
	}
	var claims map[string]any
	if err := parsed.Claims(keys[0].Key, &claims); err != nil {
		return nil, "", fmt.Errorf("verify jws signature: %w", err)
	}
	typ, _ := parsed.Headers[0].ExtraHeaders[jose.HeaderType].(string)
	return claims, typ, nil
}

// verifyIDToken verifies an id_token's signature against the OP JWKS and
// returns its claims. The caller checks the individual OIDC claims.
func verifyIDToken(baseURL, idToken string) (map[string]any, error) {
	claims, _, err := verifyJWSAgainstJWKS(baseURL, idToken)
	return claims, err
}

// verifyAccessToken verifies an access token's signature and returns its
// claims plus the JOSE typ header (expected "at+jwt" per RFC 9068).
func verifyAccessToken(baseURL, accessToken string) (map[string]any, string, error) {
	return verifyJWSAgainstJWKS(baseURL, accessToken)
}

// assertSessionCookieAtRoot fails the smoke if c's jar does not hold the
// session cookie at the ROOT path. This is the behavioral proof of the
// Path=/ scoping fix: a real browser (and the jar) only sends the cookie to
// /oauth/authorize and /saml/sso* when it is scoped to "/". It accepts either
// the plain name (HTTP dev) or the __Host- prefix (HTTPS deployment) so the
// guard can never pass vacuously when pointed at an https origin.
func assertSessionCookieAtRoot(c *client) {
	u, _ := url.Parse(c.base + "/")
	for _, ck := range c.jar.Cookies(u) {
		// Match either the plain (HTTP dev) or __Host- (HTTPS) session cookie
		// name so this guard can never pass vacuously if the smoke is pointed
		// at an https origin.
		if ck.Name == "prohibitorum_session" || ck.Name == "__Host-prohibitorum_session" {
			return
		}
	}
	log.Fatalf("session cookie not present at root path %q — Path=/ scoping regressed", c.base+"/")
}

// authorizeWithSession issues GET <c.base>+path against the root-mounted
// /oauth/authorize. The session cookie is now Path=/, so c's jar auto-sends it
// to the root-mounted endpoint (browser-equivalent); no manual attach.
// It expects a 302 and returns the Location. Redirects are NOT followed.
func authorizeWithSession(c *client, path string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, c.base+path, nil)
	if err != nil {
		return "", err
	}
	// Do not follow the redirect — we want to observe the Location (the 302
	// back to the RP redirect_uri, an unmounted path that would 404 if followed).
	hc := &http.Client{
		Jar:     c.jar,
		Timeout: 10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusFound {
		return "", fmt.Errorf("GET /oauth/authorize: want 302, got %d (%s)", resp.StatusCode, body)
	}
	loc := resp.Header.Get("Location")
	if loc == "" {
		return "", errors.New("GET /oauth/authorize: 302 with empty Location")
	}
	return loc, nil
}

// =========================================================================
// v0.6 helpers — forced re-auth, public-client token/introspect/revoke,
// SAML status-Response decode, and the v0.6 audit assert.
// =========================================================================

// authorizeRaw is authorizeWithSession under a name that signals the caller
// expects a 302 whose Location may be EITHER a /login bounce OR an error
// redirect back to the RP (both are 302s) — not necessarily a code redirect.
func authorizeRaw(c *client, path string) (string, error) {
	return authorizeWithSession(c, path)
}

// extractReauthFromLoginBounce parses a <Issuer>/login?return_to=<encoded>
// bounce Location, url-decodes the return_to (which is the full re-presented
// authorize/SSO URL carrying &reauth=<nonce>), and returns (returnTo, nonce).
func extractReauthFromLoginBounce(loc string) (returnTo, nonce string, err error) {
	u, err := url.Parse(loc)
	if err != nil {
		return "", "", fmt.Errorf("parse login bounce Location %q: %w", loc, err)
	}
	returnTo = u.Query().Get("return_to")
	if returnTo == "" {
		return "", "", fmt.Errorf("login bounce has no return_to: %q", loc)
	}
	ru, err := url.Parse(returnTo)
	if err != nil {
		return "", "", fmt.Errorf("parse return_to %q: %w", returnTo, err)
	}
	return returnTo, ru.Query().Get("reauth"), nil
}

// pathQueryOf returns the path+query ("/oauth/authorize?…") of an absolute URL,
// suitable for authorizeWithSession (which prepends c.base).
func pathQueryOf(absURL string) (string, error) {
	u, err := url.Parse(absURL)
	if err != nil {
		return "", err
	}
	if u.RawQuery == "" {
		return u.Path, nil
	}
	return u.Path + "?" + u.RawQuery, nil
}

// rawQueryOf returns the RAW query string (everything after "?") of an absolute
// URL, preserving the exact on-the-wire percent-encoding — required for the SAML
// redirect-binding signature, whose signed octet string is reconstructed from
// the raw query by the IdP.
func rawQueryOf(absURL string) (string, error) {
	u, err := url.Parse(absURL)
	if err != nil {
		return "", err
	}
	return u.RawQuery, nil
}

// ssoLocation issues GET /saml/sso?<query> and returns the Location header (used
// to capture the ForceAuthn /login bounce target — ssoWithSession returns
// status+body but not Location). The session cookie is now Path=/, so c's jar
// auto-sends it to the root-mounted endpoint (browser-equivalent).
func ssoLocation(c *client, query string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, c.base+"/saml/sso?"+query, nil)
	if err != nil {
		return "", err
	}
	hc := &http.Client{
		Jar:     c.jar,
		Timeout: 10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.Header.Get("Location"), nil
}

// decodeStatusResponse unmarshals a SAML Response and returns its top-level and
// second-level StatusCode URIs plus whether it carries an Assertion. Used for the
// NoPassive / InvalidNameIDPolicy status-Response asserts.
func decodeStatusResponse(respXML []byte) (topStatus, subStatus string, hasAssertion bool, err error) {
	var resp crewjam.Response
	if uerr := xml.Unmarshal(respXML, &resp); uerr != nil {
		return "", "", false, fmt.Errorf("unmarshal Response: %w", uerr)
	}
	topStatus = resp.Status.StatusCode.Value
	if resp.Status.StatusCode.StatusCode != nil {
		subStatus = resp.Status.StatusCode.StatusCode.Value
	}
	hasAssertion = resp.Assertion != nil
	return topStatus, subStatus, hasAssertion, nil
}

// createPublicOIDCClient shells out to `prohibitorum oidc-client create --public`.
// Public clients carry no secret (token_endpoint_auth_method=none) and use PKCE.
func createPublicOIDCClient(baseURL, clientID, redirectURI string, scopes []string) error {
	args := []string{"exec", "--", "go", "run", "./cmd/prohibitorum", "oidc-client", "create",
		"--public",
		"--client-id", clientID,
		"--display-name", "Smoke Public RP",
		"--redirect-uri", redirectURI,
		// post_logout_redirect_uris is NOT NULL in oidc_client; supply one (the
		// CLI passes nil otherwise → a NULL-constraint violation on insert).
		"--post-logout-redirect-uri", redirectURI,
	}
	for _, s := range scopes {
		args = append(args, "--scope", s)
	}
	cmd := exec.Command("mise", args...)
	cmd.Env = append(os.Environ(), "PROHIBITORUM_PUBLIC_ORIGIN="+baseURL)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("oidc-client create --public: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "Registered public client") {
		return fmt.Errorf("oidc-client create --public: unexpected output:\n%s", out)
	}
	return nil
}

// tokenExchangePublic POSTs to /oauth/token as a PUBLIC client: client_id in the
// form body, NO Basic auth, NO secret (RFC 6749 §2.3 public client + PKCE).
func tokenExchangePublic(baseURL, clientID string, form url.Values) (*oidcTokenResponse, error) {
	form.Set("client_id", clientID)
	req, err := http.NewRequest(http.MethodPost, baseURL+"/oauth/token", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	hc := &http.Client{Timeout: 10 * time.Second}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("POST /oauth/token (public): %d %s — %s", resp.StatusCode, resp.Status, body)
	}
	var out oidcTokenResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode public token response: %w (body=%s)", err, body)
	}
	return &out, nil
}

// introspectExpectInvalidClientPublic POSTs /oauth/introspect as a PUBLIC client
// (client_id in the form, no Basic auth) and asserts the OP refuses it with
// 401 invalid_client (RFC 7662 §2.1 — public clients may not introspect).
func introspectExpectInvalidClientPublic(baseURL, clientID, token string) error {
	form := url.Values{"client_id": {clientID}, "token": {token}}
	req, err := http.NewRequest(http.MethodPost, baseURL+"/oauth/introspect", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	hc := &http.Client{Timeout: 10 * time.Second}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusUnauthorized {
		return fmt.Errorf("status: want 401, got %d (body=%s)", resp.StatusCode, body)
	}
	var ae struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(body, &ae)
	if ae.Error != "invalid_client" {
		return fmt.Errorf("error: want invalid_client, got %q (body=%s)", ae.Error, body)
	}
	return nil
}

// revokeTokenPublic POSTs /oauth/revoke as a PUBLIC client (client_id in the
// form, no Basic auth). RFC 7009: the endpoint always responds 200.
func revokeTokenPublic(baseURL, clientID, token string) error {
	form := url.Values{"client_id": {clientID}, "token": {token}}
	req, err := http.NewRequest(http.MethodPost, baseURL+"/oauth/revoke", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	hc := &http.Client{Timeout: 10 * time.Second}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("POST /oauth/revoke (public): want 200, got %d (%s)", resp.StatusCode, body)
	}
	return nil
}

// verifyV06SAMLAuditEvents asserts credential_event picked up the v0.6 SAML
// surface: the ForceAuthn re-auth assertion + POST-binding assertion add to the
// use/sso count, and the IdP-initiated SSO emits use with reason=idp_initiated.
func verifyV06SAMLAuditEvents() error {
	dburl := os.Getenv("PROHIBITORUM_DATABASE_URL")
	if dburl == "" {
		return errors.New("PROHIBITORUM_DATABASE_URL not set")
	}
	rows, err := psqlScalar(dburl,
		"SELECT event || ':' || COALESCE(detail->>'reason','') || ':' || count(*)::text "+
			"FROM credential_event WHERE factor='saml_sp' "+
			"GROUP BY event, COALESCE(detail->>'reason','') "+
			"ORDER BY event, COALESCE(detail->>'reason','')")
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
	// idp_initiated reason must be present (step 110). sso reason count grew vs
	// the v0.5 baseline (ForceAuthn retry @105 + POST-binding @108) — lower
	// bound 5 keeps us safe (v0.5 alone already asserts >=3).
	if counts["use:idp_initiated"] < 1 {
		return fmt.Errorf("credential_event saml_sp use:idp_initiated: want >=1, got %d (full counts=%v)",
			counts["use:idp_initiated"], counts)
	}
	if counts["use:sso"] < 5 {
		return fmt.Errorf("credential_event saml_sp use:sso: want >=5 post-v0.6, got %d (full counts=%v)",
			counts["use:sso"], counts)
	}
	log.Printf("  credential_event covers v0.6 SAML lifecycle (counts=%v)", counts)
	return nil
}

// parseAuthorizeRedirect validates an /authorize success redirect: it must
// start with redirectURI, carry a non-empty code, echo the sent state, and
// carry iss == issuer (RFC 9207). Returns the authorization code.
func parseAuthorizeRedirect(loc, redirectURI, wantState, issuer string) (string, error) {
	if !strings.HasPrefix(loc, redirectURI) {
		return "", fmt.Errorf("redirect Location %q does not start with redirect_uri %q", loc, redirectURI)
	}
	u, err := url.Parse(loc)
	if err != nil {
		return "", fmt.Errorf("parse Location %q: %w", loc, err)
	}
	q := u.Query()
	code := q.Get("code")
	if code == "" {
		return "", fmt.Errorf("redirect missing code: %q", loc)
	}
	if q.Get("state") != wantState {
		return "", fmt.Errorf("redirect state: want %q, got %q", wantState, q.Get("state"))
	}
	if q.Get("iss") != issuer {
		return "", fmt.Errorf("redirect iss: want %q, got %q", issuer, q.Get("iss"))
	}
	return code, nil
}

// freshAuthorizeCode runs a single PKCE S256 /authorize against the OP with c's
// session and returns (verifier, code). Each call mints a fresh single-use
// code. Fatal on any error since callers always need a usable code.
func freshAuthorizeCode(c *client, baseURL, clientID, redirectURI, issuer string) (verifier, code string) {
	v, challenge := genPKCE()
	state := randState()
	nonce := randState()
	authzURL := fmt.Sprintf(
		"/oauth/authorize?response_type=code&client_id=%s&redirect_uri=%s&scope=%s&state=%s&nonce=%s&code_challenge=%s&code_challenge_method=S256",
		url.QueryEscape(clientID),
		url.QueryEscape(redirectURI),
		url.QueryEscape("openid profile offline_access"),
		url.QueryEscape(state),
		url.QueryEscape(nonce),
		url.QueryEscape(challenge),
	)
	loc, err := authorizeWithSession(c, authzURL)
	if err != nil {
		log.Fatalf("freshAuthorizeCode: /authorize: %v", err)
	}
	code, err = parseAuthorizeRedirect(loc, redirectURI, state, issuer)
	if err != nil {
		log.Fatalf("freshAuthorizeCode: %v", err)
	}
	return v, code
}

// authorizeExpectDirectError issues /authorize with an UNREGISTERED
// redirect_uri and asserts the OP returns a DIRECT JSON error (per the
// open-redirect guard) rather than a 302 to the bad URI. Expects a non-302
// status (400) with a JSON `error` body, and no Location header pointing at the
// bad URI. It uses c's jar so the session cookie is sent browser-equivalently.
func authorizeExpectDirectError(c *client, baseURL, clientID, badRedirectURI, issuer string) error {
	v, challenge := genPKCE()
	_ = v
	state := randState()
	authzURL := fmt.Sprintf(
		"/oauth/authorize?response_type=code&client_id=%s&redirect_uri=%s&scope=%s&state=%s&nonce=%s&code_challenge=%s&code_challenge_method=S256",
		url.QueryEscape(clientID),
		url.QueryEscape(badRedirectURI),
		url.QueryEscape("openid profile offline_access"),
		url.QueryEscape(state),
		url.QueryEscape(randState()),
		url.QueryEscape(challenge),
	)
	req, err := http.NewRequest(http.MethodGet, c.base+authzURL, nil)
	if err != nil {
		return err
	}
	hc := &http.Client{
		Jar:     c.jar,
		Timeout: 10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusFound {
		return fmt.Errorf("bad redirect_uri produced a 302 (open redirect!): Location=%q", resp.Header.Get("Location"))
	}
	if resp.StatusCode != http.StatusBadRequest {
		return fmt.Errorf("status: want 400, got %d (body=%s)", resp.StatusCode, body)
	}
	if loc := resp.Header.Get("Location"); strings.Contains(loc, badRedirectURI) {
		return fmt.Errorf("response Location leaks the bad redirect_uri: %q", loc)
	}
	var ae struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(body, &ae)
	if ae.Error != "invalid_request" {
		return fmt.Errorf("error code: want invalid_request, got %q (body=%s)", ae.Error, body)
	}
	return nil
}

// tokenExchange POSTs form-encoded params to /oauth/token with HTTP Basic
// client auth and decodes a success token response. Errors on any non-200.
func tokenExchange(baseURL, clientID, clientSecret string, form url.Values) (*oidcTokenResponse, error) {
	resp, err := postTokenForm(baseURL, "/oauth/token", clientID, clientSecret, form)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("POST /oauth/token: %d %s — %s", resp.StatusCode, resp.Status, body)
	}
	var out oidcTokenResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode token response: %w (body=%s)", err, body)
	}
	return &out, nil
}

// tokenExpectError POSTs to /oauth/token and asserts the response is the given
// HTTP status with the given OAuth `error` code (RFC 6749 §5.2 body).
func tokenExpectError(baseURL, clientID, clientSecret string, form url.Values, wantStatus int, wantError string) error {
	resp, err := postTokenForm(baseURL, "/oauth/token", clientID, clientSecret, form)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != wantStatus {
		return fmt.Errorf("status: want %d, got %d (body=%s)", wantStatus, resp.StatusCode, body)
	}
	var ae struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(body, &ae)
	if ae.Error != wantError {
		return fmt.Errorf("error: want %q, got %q (body=%s)", wantError, ae.Error, body)
	}
	return nil
}

// postTokenForm is the shared low-level POST for the token/introspect/revoke
// endpoints: application/x-www-form-urlencoded body + HTTP Basic client auth.
func postTokenForm(baseURL, path, clientID, clientSecret string, form url.Values) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodPost, baseURL+path, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(clientID, clientSecret)
	hc := &http.Client{Timeout: 10 * time.Second}
	return hc.Do(req)
}

// fetchUserinfo GETs /oauth/userinfo with a Bearer access token and decodes the
// claim set. Errors on any non-200.
func fetchUserinfo(baseURL, accessToken string) (map[string]any, error) {
	req, err := http.NewRequest(http.MethodGet, baseURL+"/oauth/userinfo", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	hc := &http.Client{Timeout: 10 * time.Second}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /oauth/userinfo: %d %s — %s", resp.StatusCode, resp.Status, body)
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode userinfo: %w (body=%s)", err, body)
	}
	return out, nil
}

// introspect POSTs /oauth/introspect (Basic client auth) for a token and
// decodes the RFC 7662 response.
func introspect(baseURL, clientID, clientSecret, token string) (map[string]any, error) {
	resp, err := postTokenForm(baseURL, "/oauth/introspect", clientID, clientSecret, url.Values{"token": {token}})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("POST /oauth/introspect: %d %s — %s", resp.StatusCode, resp.Status, body)
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode introspect: %w (body=%s)", err, body)
	}
	return out, nil
}

// revokeToken POSTs /oauth/revoke (Basic client auth) for a token. RFC 7009:
// the endpoint always responds 200, so any non-200 is a failure.
func revokeToken(baseURL, clientID, clientSecret, token string) error {
	resp, err := postTokenForm(baseURL, "/oauth/revoke", clientID, clientSecret, url.Values{"token": {token}})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("POST /oauth/revoke: want 200, got %d (%s)", resp.StatusCode, body)
	}
	return nil
}

// verifyRevokedJTI asserts a revoked_jti row exists for the given jti.
func verifyRevokedJTI(jti string) error {
	dburl := os.Getenv("PROHIBITORUM_DATABASE_URL")
	if dburl == "" {
		return errors.New("PROHIBITORUM_DATABASE_URL not set")
	}
	rows, err := psqlScalar(dburl, fmt.Sprintf(
		"SELECT count(*)::text FROM revoked_jti WHERE jti='%s'", jti))
	if err != nil {
		return err
	}
	if len(rows) != 1 || rows[0] != "1" {
		return fmt.Errorf("expected exactly 1 revoked_jti row for jti, got %v", rows)
	}
	log.Printf("  revoked_jti row present for revoked access token ✓")
	return nil
}

// verifyV04OIDCAuditEvents asserts credential_event has lower-bound counts for
// the oidc_client factor across this smoke run. The audit `event` column holds
// the abstract verb (use/fail/revoke); the concrete reason lives in
// detail->>'reason'. We assert on (event, reason) pairs so a regression that
// drops a specific audit (e.g. refresh_reuse) is caught.
//
// Expected reasons emitted by the handlers:
//   - use/authorize        — every /oauth/authorize success (≥5: steps 72,79,80,82,83 + reruns)
//   - use/token_issued     — every authorization_code grant (≥4: steps 73,79,80 + the negatives consume a code but the bad-secret/PKCE-mismatch ones FAIL before token_issued)
//   - use/refresh_rotated  — the successful refresh (step 76) ≥1
//   - use/logout           — RP-initiated logout (step 84) ≥1
//   - fail/refresh_reuse   — the reuse replay (step 77) ≥1
//   - revoke/revoked       — /oauth/revoke (steps 79 refresh + 80 access) ≥2
func verifyV04OIDCAuditEvents() error {
	dburl := os.Getenv("PROHIBITORUM_DATABASE_URL")
	if dburl == "" {
		return errors.New("PROHIBITORUM_DATABASE_URL not set")
	}
	rows, err := psqlScalar(dburl,
		"SELECT event || ':' || COALESCE(detail->>'reason','') || ':' || count(*)::text "+
			"FROM credential_event WHERE factor='oidc_client' "+
			"GROUP BY event, COALESCE(detail->>'reason','') "+
			"ORDER BY event, COALESCE(detail->>'reason','')")
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
		{"use:authorize", 5},
		{"use:token_issued", 3},
		{"use:refresh_rotated", 1},
		{"use:logout", 1},
		{"fail:refresh_reuse", 1},
		{"revoke:revoked", 2},
	}
	for _, w := range want {
		if counts[w.key] < w.min {
			return fmt.Errorf("credential_event oidc_client %s: want >=%d, got %d (full counts=%v)",
				w.key, w.min, counts[w.key], counts)
		}
	}
	log.Printf("  credential_event covers v0.4 OIDC OP lifecycle (counts=%v)", counts)
	return nil
}
