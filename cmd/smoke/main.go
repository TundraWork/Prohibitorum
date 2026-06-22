// Command smoke drives an end-to-end Prohibitorum auth surface against a
// running dev server using an in-process virtual WebAuthn authenticator plus
// an RFC 6238 TOTP code generator. No browser required.
//
// It exercises the WebAuthn ceremony, session table writes, and
// multi-credential management, plus the
// password + TOTP + recovery-code surface, sudo step-up via all three
// methods, throttle observation, and the destructive
// /me/auth/revoke-password-totp endpoint. Per
// feedback_always_verify_fixes.md, this binary IS the verification
// gate — every endpoint mounted in pkg/server.registerOperations
// must be exercised against the live DB before a change can be
// considered done.
//
// Failure at any step prints a diagnostic and exits non-zero.
package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/png"
	"io"
	"log"
	"math/big"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"

	crewjam "github.com/crewjam/saml"
	"github.com/fxamacker/cbor/v2"
	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/jackc/pgx/v5"

	"prohibitorum/cmd/smoke/mockop"
	totppkg "prohibitorum/pkg/credential/totp"
	fedoidc "prohibitorum/pkg/federation/oidc"
)

// Per-group step totals for the progress headers. Each arc numbers its own
// steps locally (e.g. "oidc 1/18"); a local denominator stays correct when a
// later arc is added, unlike a single global counter.
const (
	nCore       = 51
	nFederation = 26
	nOIDC       = 18
	nSAML       = 12
	nHardening  = 12
	nConsent    = 2
	nAdmin      = 8
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

	// federation: bring up an in-process mock OIDC OP for use by the
	// federation steps appended after the core surface. Started early so a
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

	step(fmt.Sprintf("core %d/%d — minting enrollment URL via enroll-admin", 1, nCore))
	token, err := mintEnrollmentToken(*baseURL, *skipNew)
	if err != nil {
		log.Fatalf("enroll-admin: %v", err)
	}
	log.Printf("  token: %s…", token[:12])

	step(fmt.Sprintf("core %d/%d — POST /enrollments/{token}/register/begin", 2, nCore))
	creation, err := c.beginEnrollment(token, *username, *display, "smoke-laptop")
	if err != nil {
		log.Fatalf("register/begin: %v", err)
	}
	log.Printf("  challenge len=%d rpId=%s userId len=%d", len(creation.Challenge), creation.RP.ID, len(creation.User.ID))

	step(fmt.Sprintf("core %d/%d — building virtual authenticator credential", 3, nCore))
	auth, err := newAuthenticator(creation.RP.ID)
	if err != nil {
		log.Fatalf("authenticator: %v", err)
	}
	attestation, err := auth.attestCredential(creation.Challenge, creation.User.ID, *baseURL)
	if err != nil {
		log.Fatalf("attest: %v", err)
	}
	log.Printf("  credentialId len=%d cose_alg=-7 (ES256)", len(auth.credentialID))

	step(fmt.Sprintf("core %d/%d — POST /enrollments/{token}/register/complete", 4, nCore))
	if err := c.completeEnrollment(token, auth, attestation); err != nil {
		log.Fatalf("register/complete: %v", err)
	}
	log.Printf("  session cookie set (have %d cookies)", len(c.cookies()))

	step(fmt.Sprintf("core %d/%d — GET /me", 5, nCore))
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
	step(fmt.Sprintf("core %d/%d — SPA shell served for dashboard routes", 6, nCore))
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

	step(fmt.Sprintf("core %d/%d — POST /auth/logout", 7, nCore))
	if err := c.logout(); err != nil {
		log.Fatalf("logout: %v", err)
	}
	if _, err := c.getMe(); err == nil {
		log.Fatalf("post-logout /me succeeded; expected 401")
	}
	log.Printf("  session revoked; /me returns 401 as expected")

	step(fmt.Sprintf("core %d/%d — POST /auth/login/begin", 8, nCore))
	assertion, err := c.beginLogin()
	if err != nil {
		log.Fatalf("login/begin: %v", err)
	}
	log.Printf("  challenge len=%d rpId=%s", len(assertion.Challenge), assertion.RPID)

	step(fmt.Sprintf("core %d/%d — signing assertion with virtual authenticator", 9, nCore))
	signed, err := auth.signAssertion(assertion.Challenge, *baseURL)
	if err != nil {
		log.Fatalf("sign assertion: %v", err)
	}
	log.Printf("  signature len=%d (ASN.1 DER)", len(signed.signature))

	step(fmt.Sprintf("core %d/%d — POST /auth/login/complete", 10, nCore))
	if err := c.completeLogin(auth, signed); err != nil {
		log.Fatalf("login/complete: %v", err)
	}
	log.Printf("  session cookie restored")

	step(fmt.Sprintf("core %d/%d — GET /me (post-login)", 11, nCore))
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

	step(fmt.Sprintf("core %d/%d — second client B begins login with the same authenticator", 12, nCore))
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

	step(fmt.Sprintf("core %d/%d — B completes login; both A and B now hold sessions", 13, nCore))
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

	step(fmt.Sprintf("core %d/%d — A lists /me/sessions; expect 2 (current + B's)", 14, nCore))
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

	step(fmt.Sprintf("core %d/%d — A revokes B's session via /me/sessions/revoke", 15, nCore))
	if err := c.revokeSession(otherID); err != nil {
		log.Fatalf("revoke session: %v", err)
	}
	log.Printf("  revoke succeeded")

	step(fmt.Sprintf("core %d/%d — B's /me should now return 401", 16, nCore))
	if _, err := cB.getMe(); err == nil {
		log.Fatalf("B /me succeeded after revocation; expected 401")
	}
	log.Printf("  B is denied, RevokeBySessionID confirmed")

	step(fmt.Sprintf("core %d/%d — A adds a second passkey via /me/credentials/register/{begin,complete}", 17, nCore))
	// Adding a passkey is fresh-sudo gated; prime the sudo window first —
	// /register/begin consumes a slot within the window, /complete then
	// rides the ceremony stash. Without this the begin returns 401 sudo_required.
	if err := sudoWebAuthn(c, auth, *baseURL); err != nil {
		log.Fatalf("sudo webauthn (pre add-passkey): %v", err)
	}
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

	step(fmt.Sprintf("core %d/%d — A lists /me/credentials; expect 2", 18, nCore))
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
	// core surface: password + TOTP + recovery codes + sudo (3 methods) +
	// throttle observation + destructive revoke.
	//
	// Pre-condition: A is logged in with a fresh WebAuthn session (core 10).
	// =========================================================================

	const password = "smoke-pw-correct-horse-battery-staple"

	step(fmt.Sprintf("core %d/%d — sudo via webauthn (prime SudoUntil for /me/password/set)", 19, nCore))
	if err := sudoWebAuthn(c, auth, *baseURL); err != nil {
		log.Fatalf("sudo webauthn (pre password/set): %v", err)
	}
	log.Printf("  sudo grant acquired (webauthn)")

	step(fmt.Sprintf("core %d/%d — POST /me/password/set", 20, nCore))
	if err := c.postJSON("/api/prohibitorum/me/password/set",
		map[string]string{"password": password}, nil); err != nil {
		log.Fatalf("password/set: %v", err)
	}
	log.Printf("  password set (204)")

	step(fmt.Sprintf("core %d/%d — DB assert: password_credential row exists w/ argon2id hash", 21, nCore))
	if err := verifyPasswordCredential(me2.ID); err != nil {
		log.Fatalf("password DB assert: %v", err)
	}

	step(fmt.Sprintf("core %d/%d — POST /me/totp/begin (first enrollment; no sudo required)", 22, nCore))
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

	step(fmt.Sprintf("core %d/%d — POST /me/totp/verify {current code}", 23, nCore))
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

	step(fmt.Sprintf("core %d/%d — DB assert: totp_credential.confirmed_at IS NOT NULL + 10 recovery rows", 24, nCore))
	if err := verifyTOTPConfirmed(me2.ID); err != nil {
		log.Fatalf("totp DB assert: %v", err)
	}

	step(fmt.Sprintf("core %d/%d — POST /auth/logout (drop A's webauthn session)", 25, nCore))
	if err := c.logout(); err != nil {
		log.Fatalf("logout pre-password-login: %v", err)
	}
	log.Printf("  logged out")

	step(fmt.Sprintf("core %d/%d — POST /auth/password/begin {username, password}", 26, nCore))
	partialToken, err := c.passwordBegin(*username, password)
	if err != nil {
		log.Fatalf("password/begin: %v", err)
	}
	log.Printf("  partial_session_token len=%d", len(partialToken))

	step(fmt.Sprintf("core %d/%d — POST /auth/totp/verify {partial_session_token, current code}", 27, nCore))
	// RFC 6238 §5.2 replay protection: last_step from the earlier TOTP verify is still set, so
	// wait across the period boundary before the next successful TOTP verify.
	totpStep = waitForNextTOTPStep(totpStep)
	totpCode := totppkg.ComputeCodeForTesting(secret, time.Now().Unix(), 6)
	if err := c.totpStepTwoVerify(partialToken, totpCode); err != nil {
		log.Fatalf("auth/totp/verify: %v", err)
	}
	log.Printf("  session cookie issued via password+TOTP (step=%d)", totpStep)

	step(fmt.Sprintf("core %d/%d — GET /me round-trips post-password+TOTP login", 28, nCore))
	mePT, err := c.getMe()
	if err != nil {
		log.Fatalf("GET /me post-pwd+totp: %v", err)
	}
	if mePT.ID != me2.ID {
		log.Fatalf("/me id drift after pwd+totp: got %d want %d", mePT.ID, me2.ID)
	}
	log.Printf("  /me id=%d (same account)", mePT.ID)

	step(fmt.Sprintf("core %d/%d — POST /auth/logout (drop pwd+totp session)", 29, nCore))
	if err := c.logout(); err != nil {
		log.Fatalf("logout pre-recovery-ceremony: %v", err)
	}

	// --- Recovery ceremony (2026-05-28 hardening) -----------------------------
	// /auth/recovery-code/verify no longer issues a session. It hands back a
	// recovery_session_token that the user must redeem at
	// /auth/recovery/totp/{begin,verify} to enroll a fresh TOTP and regain
	// account access. recovery_code is no longer a sudo method (former
	// these steps were dropped); the user re-proves possession of TOTP every time.

	step(fmt.Sprintf("core %d/%d — POST /auth/password/begin (fresh partial token for recovery ceremony)", 30, nCore))
	partialToken2, err := c.passwordBegin(*username, password)
	if err != nil {
		log.Fatalf("password/begin 2: %v", err)
	}
	log.Printf("  partial_session_token len=%d", len(partialToken2))

	step(fmt.Sprintf("core %d/%d — POST /auth/recovery-code/verify {recovery_codes[0]} → recovery_session_token", 31, nCore))
	recoveryToken, err := c.recoveryCodeVerify(partialToken2, recoveryCodes[0])
	if err != nil {
		log.Fatalf("auth/recovery-code/verify: %v", err)
	}
	if recoveryToken == "" {
		log.Fatalf("auth/recovery-code/verify returned empty recovery_session_token")
	}
	log.Printf("  recovery_session_token len=%d (no session cookie yet)", len(recoveryToken))

	step(fmt.Sprintf("core %d/%d — DB assert: recovery_codes[0].used_at IS NOT NULL (consumed by redeem)", 32, nCore))
	if err := verifyRecoveryCodeUsed(me2.ID, 1, 0); err != nil {
		log.Fatalf("recovery code used_at DB assert: %v", err)
	}

	step(fmt.Sprintf("core %d/%d — POST /auth/recovery/totp/begin {recovery_session_token}", 33, nCore))
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

	step(fmt.Sprintf("core %d/%d — DB assert: TOTP unconfirmed; 9 recovery codes still present", 34, nCore))
	if err := verifyTOTPUnconfirmedAndRecoveryCount(me2.ID, 9); err != nil {
		log.Fatalf("post-recovery-begin DB assert: %v", err)
	}

	step(fmt.Sprintf("core %d/%d — wait next TOTP step + POST /auth/recovery/totp/verify {token, code}", 35, nCore))
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

	step(fmt.Sprintf("core %d/%d — DB assert: new TOTP confirmed; exactly 10 recovery codes", 36, nCore))
	if err := verifyTOTPConfirmed(me2.ID); err != nil {
		log.Fatalf("post-recovery-verify DB assert: %v", err)
	}

	step(fmt.Sprintf("core %d/%d — GET /me round-trips post-recovery-ceremony", 37, nCore))
	mePT2, err := c.getMe()
	if err != nil {
		log.Fatalf("GET /me post-recovery: %v", err)
	}
	if mePT2.ID != me2.ID {
		log.Fatalf("/me id drift after recovery: got %d want %d", mePT2.ID, me2.ID)
	}
	log.Printf("  /me id=%d (account intact post-recovery)", mePT2.ID)

	step(fmt.Sprintf("core %d/%d — POST /auth/logout (drop recovery-ceremony session)", 38, nCore))
	if err := c.logout(); err != nil {
		log.Fatalf("logout post-recovery: %v", err)
	}

	step(fmt.Sprintf("core %d/%d — re-login via webauthn for the throttle observation phase", 39, nCore))
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

	step(fmt.Sprintf("core %d/%d — drive wrong TOTP codes via /me/sudo password_totp until 429", 40, nCore))
	attempts, retryAfter, err := driveTOTPLockout(c, password)
	if err != nil {
		log.Fatalf("drive totp lockout: %v", err)
	}
	log.Printf("  observed 429 after %d wrong attempts; Retry-After=%s",
		attempts, retryAfter)

	step(fmt.Sprintf("core %d/%d — DB assert: auth_throttle row for (account, 'totp') failed_attempts>=3, locked", 41, nCore))
	if err := verifyThrottleLocked(me2.ID, "totp"); err != nil {
		log.Fatalf("throttle DB assert: %v", err)
	}

	step(fmt.Sprintf("core %d/%d — HARNESS ONLY: DELETE auth_throttle row + fresh login to reset per-session sudo rate limit", 42, nCore))
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

	step(fmt.Sprintf("core %d/%d — sudo via password_totp (/me/sudo/begin + /me/sudo/complete)", 43, nCore))
	// Wait past the period boundary from the earlier successful TOTP verify so the
	// next code we send hasn't been seen by last_step.
	totpStep = waitForNextTOTPStep(totpStep)
	if err := sudoPasswordTOTP(c, password, secret); err != nil {
		log.Fatalf("sudo password_totp: %v", err)
	}
	log.Printf("  sudo grant acquired (password_totp; step=%d)", totpStep)

	step(fmt.Sprintf("core %d/%d — POST /me/recovery-codes/regenerate (consumes sudo, mints fresh codes)", 44, nCore))
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

	step(fmt.Sprintf("core %d/%d — POST /me/sudo/methods (recovery_code must NOT appear post-hardening)", 45, nCore))
	if err := verifySudoMethodsNoRecoveryCode(c); err != nil {
		log.Fatalf("sudo methods invariant: %v", err)
	}
	log.Printf("  /me/sudo/methods correctly omits recovery_code")

	step(fmt.Sprintf("core %d/%d — POST /me/sudo/begin {method:recovery_code} must 400 sudo_method_unavailable", 46, nCore))
	if err := verifySudoBeginRejectsRecoveryCode(c); err != nil {
		log.Fatalf("sudo begin recovery_code rejection: %v", err)
	}
	log.Printf("  /me/sudo/begin rejects recovery_code with sudo_method_unavailable")

	step(fmt.Sprintf("core %d/%d — sudo via webauthn (priming the destructive revoke)", 47, nCore))
	if err := sudoWebAuthn(c, auth, *baseURL); err != nil {
		log.Fatalf("sudo webauthn (pre revoke): %v", err)
	}
	log.Printf("  sudo grant acquired (webauthn)")

	step(fmt.Sprintf("core %d/%d — POST /me/auth/revoke-password-totp (destructive)", 48, nCore))
	if err := c.postJSON("/api/prohibitorum/me/auth/revoke-password-totp",
		map[string]any{}, nil); err != nil {
		log.Fatalf("revoke-password-totp: %v", err)
	}
	log.Printf("  fallback factors revoked (204)")

	step(fmt.Sprintf("core %d/%d — DB assert: password_credential / totp_credential / recovery_code all empty", 49, nCore))
	if err := verifyFactorsEmpty(me2.ID); err != nil {
		log.Fatalf("post-revoke DB assert: %v", err)
	}

	step(fmt.Sprintf("core %d/%d — logout then POST /auth/password/begin must now 401", 50, nCore))
	if err := c.logout(); err != nil {
		log.Fatalf("logout post-revoke: %v", err)
	}
	if _, err := c.passwordBegin(*username, password); err == nil {
		log.Fatalf("password/begin succeeded after revoke; expected 401")
	}
	log.Printf("  /auth/password/begin returns 401 as expected")

	step(fmt.Sprintf("core %d/%d — DB assert: credential_event covers the credential lifecycle", 51, nCore))
	if err := verifyCoreAuditEvents(me2.ID); err != nil {
		log.Fatalf("audit DB assert: %v", err)
	}

	// =========================================================================
	// federation surface: upstream OIDC federation — login + callback drive against an
	// in-process mock OP, then negative paths (email_not_verified,
	// username_collision, invalid_return_to, upstream_error), link_only mode,
	// self-service link from the smoke-admin session, and unlink. The mock
	// OP is the one started at main() entry.
	// =========================================================================

	step(fmt.Sprintf("federation %d/%d — seed upstream_idp 'mockop' (auto_provision)", 1, nFederation))
	dek := loadDEK()
	mockopIDPID, err := seedUpstreamIDP(dek, "mockop", "Mock OP", opTS.URL,
		"test-client", "test-client-secret", "auto_provision",
		[]string{"example.com"}, true)
	if err != nil {
		log.Fatalf("seed upstream_idp 'mockop': %v", err)
	}
	log.Printf("  upstream_idp id=%d slug=mockop issuer=%s", mockopIDPID, opTS.URL)

	step(fmt.Sprintf("federation %d/%d — happy-path /login → upstream /authorize", 2, nFederation))
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

	step(fmt.Sprintf("federation %d/%d — follow /authorize → /callback (code+state+iss)", 3, nFederation))
	callbackURL, err := followMockOPAuthorize(authorizeURL)
	if err != nil {
		log.Fatalf("mock OP /authorize: %v", err)
	}
	if !strings.Contains(callbackURL, "/api/prohibitorum/auth/federation/mockop/callback") {
		log.Fatalf("mock OP did not redirect to /callback: %q", callbackURL)
	}
	log.Printf("  302 to RP /callback (with code, state, iss)")

	step(fmt.Sprintf("federation %d/%d — RP /callback (first login) → 302 /welcome, NO session", 4, nFederation))
	// First-time federated identity is UNCONFIRMED: the callback withholds the
	// durable session and parks the browser on /welcome with a fed-state
	// confirmation-grant cookie. There must be no session yet.
	if loc, err := extClient.getRedirectAbs(callbackURL); err != nil {
		log.Fatalf("federation/callback: %v", err)
	} else if loc != "/welcome" {
		log.Fatalf("federation/callback: want redirect to /welcome (unconfirmed first login), got %q", loc)
	}
	if _, err := extClient.getMe(); err == nil {
		log.Fatalf("federation/callback: session issued before confirmation; /me should 401 pre-confirm")
	}
	log.Printf("  302 to /welcome; no session cookie yet (post-callback /me 401) ✓")

	step(fmt.Sprintf("federation %d/%d — GET /auth/federation/confirm → pending identity (grant-scoped)", 5, nFederation))
	view, err := extClient.confirmGet()
	if err != nil {
		log.Fatalf("federation/confirm GET: %v", err)
	}
	if view.Username != "extuser" {
		log.Fatalf("confirm GET username: got %q want %q", view.Username, "extuser")
	}
	if view.DisplayName != "Ext User" {
		log.Fatalf("confirm GET displayName: got %q want %q", view.DisplayName, "Ext User")
	}
	log.Printf("  confirm GET: idp=%q username=%s displayName=%s avatarPending=%v ✓",
		view.IDPDisplayName, view.Username, view.DisplayName, view.AvatarPending)

	step(fmt.Sprintf("federation %d/%d — POST /auth/federation/confirm → {redirect:/me} + session", 6, nFederation))
	if redirect, err := extClient.confirmPost(); err != nil {
		log.Fatalf("federation/confirm POST: %v", err)
	} else if redirect != "/me" {
		log.Fatalf("federation/confirm POST: want redirect /me, got %q", redirect)
	}
	// The session cookie is Path=/, so the jar sends it to every endpoint.
	// Verify session presence by hitting an API endpoint — if the cookie
	// weren't set, /me would 401.
	if _, err := extClient.getMe(); err != nil {
		log.Fatalf("federation/confirm: no session cookie post-confirm (/me failed: %v)", err)
	}
	log.Printf("  confirm POST → /me; session cookie issued (verified via /me) ✓")

	step(fmt.Sprintf("federation %d/%d — GET /me as federated user", 7, nFederation))
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

	step(fmt.Sprintf("federation %d/%d — DB assert: account_identity + credential_event for ext-user-1", 8, nFederation))
	if err := verifyFederatedIdentityCreated(extMe.ID, "ext-user-1", mockopIDPID); err != nil {
		log.Fatalf("identity DB assert: %v", err)
	}

	step(fmt.Sprintf("federation %d/%d — claim sync on re-login (display_name drift)", 9, nFederation))
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

	step(fmt.Sprintf("federation %d/%d — negative: email_not_verified", 10, nFederation))
	if err := extClient.logout(); err != nil {
		log.Fatalf("ext logout pre-neg: %v", err)
	}
	negClient1, _ := newFederationClient(*baseURL)
	opSrv.SetClaims("ext-user-99", "ext99@example.com", false, "extuser99", "Ext 99")
	if err := expectFederationCallbackError(negClient1, *baseURL, "mockop",
		"email_not_verified"); err != nil {
		log.Fatalf("negative email_not_verified: %v", err)
	}
	log.Printf("  /callback → 302 /error?error=email_not_verified ✓")

	step(fmt.Sprintf("federation %d/%d — negative: username_collision", 11, nFederation))
	negClient2, _ := newFederationClient(*baseURL)
	// Collide on smoke-admin's username (auto_provision tries to create
	// a new account with that name; existing local account wins).
	opSrv.SetClaims("ext-collide-1", "collide@example.com", true, *username, "Collider")
	if err := expectFederationCallbackError(negClient2, *baseURL, "mockop",
		"username_collision"); err != nil {
		log.Fatalf("negative username_collision: %v", err)
	}
	log.Printf("  /callback → 302 /error?error=username_collision ✓")

	step(fmt.Sprintf("federation %d/%d — negative: invalid_return_to", 12, nFederation))
	negClient3, _ := newFederationClient(*baseURL)
	// /login with an off-origin return_to now 302-redirects to /error (not 400).
	resp55, err := negClient3.getRaw("/api/prohibitorum/auth/federation/mockop/login?return_to=https://evil.example.com")
	if err != nil {
		log.Fatalf("negative invalid_return_to: %v", err)
	}
	resp55body, _ := io.ReadAll(resp55.Body)
	_ = resp55.Body.Close()
	if resp55.StatusCode != http.StatusFound {
		log.Fatalf("negative invalid_return_to: want 302, got %d (body=%s)", resp55.StatusCode, string(resp55body))
	}
	if loc55 := resp55.Header.Get("Location"); !strings.HasPrefix(loc55, "/error?error=invalid_return_to") {
		log.Fatalf("negative invalid_return_to: Location want prefix /error?error=invalid_return_to, got %q", loc55)
	}
	log.Printf("  /login?return_to=https://evil… → 302 /error?error=invalid_return_to ✓")

	step(fmt.Sprintf("federation %d/%d — negative: upstream_error (access_denied)", 13, nFederation))
	negClient4, _ := newFederationClient(*baseURL)
	opSrv.FailWithError("access_denied", "user denied")
	if err := expectFederationCallbackError(negClient4, *baseURL, "mockop",
		"upstream_error"); err != nil {
		log.Fatalf("negative upstream_error: %v", err)
	}
	log.Printf("  /callback → 302 /error?error=upstream_error ✓")

	step(fmt.Sprintf("federation %d/%d — GET /me/identities (as federated user)", 14, nFederation))
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

	step(fmt.Sprintf("federation %d/%d — seed upstream_idp 'mockop-link' (link_only mode)", 15, nFederation))
	linkIDPID, err := seedUpstreamIDP(dek, "mockop-link", "Mock OP (link-only)", opTS.URL,
		"test-client", "test-client-secret", "link_only",
		[]string{"example.com"}, true)
	if err != nil {
		log.Fatalf("seed mockop-link: %v", err)
	}
	log.Printf("  upstream_idp id=%d slug=mockop-link mode=link_only", linkIDPID)

	step(fmt.Sprintf("federation %d/%d — link_only refuses unknown sub (302 /error?error=link_required)", 16, nFederation))
	negClient5, _ := newFederationClient(*baseURL)
	opSrv.SetClaims("ext-unknown-9", "unknown@example.com", true, "extuser-unknown", "Unknown")
	if err := expectFederationCallbackError(negClient5, *baseURL, "mockop-link",
		"link_required"); err != nil {
		log.Fatalf("negative link_required: %v", err)
	}
	log.Printf("  link_only /callback → 302 /error?error=link_required ✓")

	// --- Self-service link from smoke-admin (with sudo) ---------------------

	step(fmt.Sprintf("federation %d/%d — re-login as smoke-admin via webauthn for link/unlink", 17, nFederation))
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

	step(fmt.Sprintf("federation %d/%d — sudo (webauthn) + /me/identities/link/mockop/begin → /callback", 18, nFederation))
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

	step(fmt.Sprintf("federation %d/%d — DB assert: account_identity for admin-link-1 owned by smoke-admin", 19, nFederation))
	if err := verifyFederatedIdentityCreated(adminMe.ID, "admin-link-1", mockopIDPID); err != nil {
		log.Fatalf("link DB assert: %v", err)
	}

	step(fmt.Sprintf("federation %d/%d — GET /me/identities (as smoke-admin)", 20, nFederation))
	adminIdentities, err := c.listMyIdentities()
	if err != nil {
		log.Fatalf("listMyIdentities admin: %v", err)
	}
	if len(adminIdentities) != 1 || adminIdentities[0].IdpSlug != "mockop" {
		log.Fatalf("expected 1 identity for smoke-admin, got %+v", adminIdentities)
	}
	log.Printf("  smoke-admin has 1 federated identity (id=%d)", adminIdentities[0].ID)

	step(fmt.Sprintf("federation %d/%d — sudo (webauthn) + POST /me/identities/{id}/unlink", 21, nFederation))
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

	step(fmt.Sprintf("federation %d/%d — seed invite enrollment + drive /enrollments/{token}/start-federation", 22, nFederation))
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

	step(fmt.Sprintf("federation %d/%d — DB assert: invite consumed + account + account_identity + register audit", 23, nFederation))
	if err := verifyInviteOnlyRedemption(inviteToken, inviteUsername, inviteSub, mockopIDPID); err != nil {
		log.Fatalf("invite redemption DB assert: %v", err)
	}

	step(fmt.Sprintf("federation %d/%d — negative: consumed token rejected → 302 /error?error=invite_required", 24, nFederation))
	// Reuse the now-consumed token; the federator's BeginInviteRedemption
	// must reject before any upstream hop. Fresh client (no cookies).
	negInvite1, _ := newFederationClient(*baseURL)
	if err := expectInviteStartFederationError(negInvite1, *baseURL, inviteToken,
		"invite_required"); err != nil {
		log.Fatalf("negative consumed-token: %v", err)
	}
	log.Printf("  /start-federation consumed → 302 /error?error=invite_required ✓")

	step(fmt.Sprintf("federation %d/%d — negative: expired token rejected → 302 /error?error=invite_required", 25, nFederation))
	const expiredToken = "invite-token-smoke-expired-001"
	// Seed a NEW enrollment that's already past expires_at (1 second in the
	// past). BeginInviteRedemption checks enr.ExpiresAt.After(time.Now()).
	if err := seedInviteEnrollment(expiredToken, "invite-redeemer-expired", "Expired Redeemer", "user", "mockop", "-1 second"); err != nil {
		log.Fatalf("seed expired invite: %v", err)
	}
	negInvite2, _ := newFederationClient(*baseURL)
	if err := expectInviteStartFederationError(negInvite2, *baseURL, expiredToken,
		"invite_required"); err != nil {
		log.Fatalf("negative expired-token: %v", err)
	}
	log.Printf("  /start-federation expired → 302 /error?error=invite_required ✓")

	step(fmt.Sprintf("federation %d/%d — DB assert: credential_event covers federation lifecycle", 26, nFederation))
	if err := verifyFederationAuditEvents(); err != nil {
		log.Fatalf("federation audit DB assert: %v", err)
	}

	// =========================================================================
	// Upstream avatar inheritance over federation (Tasks 1–10): a first-time
	// federated login now inherits the upstream `picture` into the account
	// avatar via a BACKGROUND goroutine, unless the user uploaded their own.
	// Three cases, all against the auto_provision `mockop` IdP seeded earlier:
	//
	//   avatar-fed 1 — picture in the id_token → inherited (poll until stored).
	//   avatar-fed 2 — no-clobber: a user upload survives an upstream re-login.
	//   avatar-fed 4 — dual-source selection: with BOTH a user upload AND an
	//                  upstream row stored, switch the active pointer between
	//                  user/upstream/none and verify ?source previews + active GET.
	//   avatar-fed 3 — userinfo fallback: picture only in /userinfo → inherited.
	//
	// The avatar fetch hits the mockop /avatar.png image endpoint (Part B) and
	// is SSRF-allowed because mockop is on 127.0.0.1 and the server runs with
	// PROHIBITORUM_FEDERATION_ALLOW_PRIVATE_NETWORK=true.
	// =========================================================================

	step("avatar-fed 1/4 — federated first login inherits upstream id_token picture")
	{
		const avSub = "ext-avatar-1"
		opSrv.SetClaims(avSub, "ext-avatar-1@example.com", true, "extavatar1", "Ext Avatar One")
		opSrv.SetPicture(opSrv.PictureURL()) // picture in id_token, pointing at the mockop image
		avClient, err := newFederationClient(*baseURL)
		if err != nil {
			log.Fatalf("avatar-fed: client: %v", err)
		}
		// First login → /welcome → confirm → session (the confirm flow from C0).
		if err := driveFederationToWelcome(avClient, *baseURL, "mockop"); err != nil {
			log.Fatalf("avatar-fed: drive to /welcome: %v", err)
		}
		if _, err := avClient.confirmGet(); err != nil {
			log.Fatalf("avatar-fed: confirm GET: %v", err)
		}
		if redirect, err := avClient.confirmPost(); err != nil {
			log.Fatalf("avatar-fed: confirm POST: %v", err)
		} else if redirect != "/me" {
			log.Fatalf("avatar-fed: confirm redirect want /me, got %q", redirect)
		}
		avMe, err := avClient.getMe()
		if err != nil {
			log.Fatalf("avatar-fed: /me: %v", err)
		}
		avSubject, err := getOIDCSubject(avMe.ID)
		if err != nil {
			log.Fatalf("avatar-fed: oidc_subject: %v", err)
		}
		// The avatar fetch is a background goroutine — poll the public endpoint
		// until the WebP appears (never assume it's instant).
		n, etag, err := pollAvatarInherited(*baseURL, avSubject, 10*time.Second)
		if err != nil {
			log.Fatalf("avatar-fed: %v", err)
		}
		log.Printf("  GET /avatar/%s → 200 image/webp etag=%s body=%d bytes (inherited from upstream) ✓",
			avSubject, etag, n)
		// Bonus: /me should now surface a non-null avatarUrl.
		avMe2, err := avClient.getMe()
		if err != nil {
			log.Fatalf("avatar-fed: /me post-inherit: %v", err)
		}
		if avMe2.AvatarURL == nil {
			log.Fatalf("avatar-fed: /me.avatarUrl is null after upstream inherit")
		}
		log.Printf("  /me.avatarUrl=%q ✓", *avMe2.AvatarURL)

		// avatar-fed 2/4 — no-clobber: a user upload must survive an upstream
		// re-login with a different picture. Re-use this confirmed account.
		step("avatar-fed 2/4 — user upload survives an upstream re-login (no clobber)")
		var pngBuf bytes.Buffer
		{
			img := image.NewRGBA(image.Rect(0, 0, 8, 8))
			if err := png.Encode(&pngBuf, img); err != nil {
				log.Fatalf("avatar-fed: encode user PNG: %v", err)
			}
		}
		{
			req, err := http.NewRequest(http.MethodPut,
				*baseURL+"/api/prohibitorum/me/avatar", bytes.NewReader(pngBuf.Bytes()))
			if err != nil {
				log.Fatalf("avatar-fed: build PUT: %v", err)
			}
			req.Header.Set("Content-Type", "image/png")
			for _, ck := range avClient.cookies() {
				req.AddCookie(ck)
			}
			resp, err := avClient.hc.Do(req)
			if err != nil {
				log.Fatalf("avatar-fed: PUT /me/avatar: %v", err)
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != http.StatusNoContent {
				log.Fatalf("avatar-fed: PUT /me/avatar: want 204, got %d (%s)", resp.StatusCode, body)
			}
		}
		// Snapshot avatar_source/etag after the user upload.
		srcAfterUpload, etagAfterUpload, err := getAvatarSourceEtag(avMe.ID)
		if err != nil {
			log.Fatalf("avatar-fed: read source/etag post-upload: %v", err)
		}
		if srcAfterUpload != "user" {
			log.Fatalf("avatar-fed: avatar_source after user upload want 'user', got %q", srcAfterUpload)
		}
		log.Printf("  user upload set avatar_source=user etag=%s", etagAfterUpload)

		// Log out, change the upstream picture, re-login (confirmed → straight to
		// /me, no /welcome), then give the background job a chance to run.
		if err := avClient.logout(); err != nil {
			log.Fatalf("avatar-fed: logout pre-reclaim: %v", err)
		}
		opSrv.SetClaims(avSub, "ext-avatar-1@example.com", true, "extavatar1", "Ext Avatar One")
		opSrv.SetPicture(opSrv.PictureURL())
		if err := driveFederationLogin(avClient, *baseURL, "mockop", "/me"); err != nil {
			log.Fatalf("avatar-fed: re-login (confirmed) failed: %v", err)
		}
		// Poll the avatar status until the background job (if it ran) settles, then
		// assert the user avatar is UNCHANGED.
		for i := 0; i < 25; i++ {
			var st struct {
				Pending bool `json:"pending"`
			}
			if err := avClient.get("/api/prohibitorum/me/avatar/status", &st); err == nil && !st.Pending {
				break
			}
			time.Sleep(200 * time.Millisecond)
		}
		// A short settle so any (incorrect) clobber would have landed.
		time.Sleep(500 * time.Millisecond)
		srcAfter, etagAfter, err := getAvatarSourceEtag(avMe.ID)
		if err != nil {
			log.Fatalf("avatar-fed: read source/etag post-relogin: %v", err)
		}
		if srcAfter != "user" {
			log.Fatalf("avatar-fed: NO-CLOBBER VIOLATED — avatar_source changed to %q after upstream re-login", srcAfter)
		}
		if etagAfter != etagAfterUpload {
			log.Fatalf("avatar-fed: NO-CLOBBER VIOLATED — avatar_etag changed (%q → %q) after upstream re-login",
				etagAfterUpload, etagAfter)
		}
		log.Printf("  avatar_source still 'user', etag unchanged (%s) after upstream re-login ✓", etagAfter)

		// avatar-fed 4/4 — dual-source selection. After avatar-fed 2 this account
		// is in the IDEAL dual-source state: avatar_source='user' (active) AND an
		// 'upstream' row also stored (Task 3 upserts upstream on every federated
		// login without activating it). Exercise the active-pointer switch across
		// user/upstream/none and the per-source ?source= previews.
		//
		// Race note: avatar-fed 2's re-login may have left a background inherit
		// goroutine in flight. It cannot corrupt these assertions: runAvatarInherit
		// re-reads avatar_source from the DB after its slow I/O and only calls
		// SetActiveAvatar when that fresh read is NULL or 'upstream' — so it can
		// neither override the explicit 'user'/'none' we set below nor write a value
		// different from the 'upstream' a switch-to-upstream already wrote.
		step("avatar-fed 4/4 — dual-source selection + ?source previews")
		const selPath = "/api/prohibitorum/me/avatar/selection"

		// (a) /me reports the active source AND both per-source preview URLs.
		dsMe, err := avClient.getMe()
		if err != nil {
			log.Fatalf("avatar-fed dual: /me: %v", err)
		}
		if dsMe.AvatarSource == nil || *dsMe.AvatarSource != "user" {
			log.Fatalf("avatar-fed dual: /me.avatarSource want 'user', got %v", dsMe.AvatarSource)
		}
		if _, ok := dsMe.AvatarSourceUrls["user"]; !ok {
			log.Fatalf("avatar-fed dual: /me.avatarSourceUrls missing 'user' key (got %v)", dsMe.AvatarSourceUrls)
		}
		if _, ok := dsMe.AvatarSourceUrls["upstream:mockop"]; !ok {
			log.Fatalf("avatar-fed dual: /me.avatarSourceUrls missing 'upstream:mockop' key — coexistence not proven (got %v)", dsMe.AvatarSourceUrls)
		}
		// The per-upstream source must be labeled with the IdP display name
		// (exercises the live LEFT JOIN account_avatar→upstream_idp end-to-end).
		if dsMe.AvatarSourceLabels["upstream:mockop"] != "Mock OP" {
			log.Fatalf("avatar-fed dual: /me.avatarSourceLabels['upstream:mockop'] want 'Mock OP', got %q (labels=%v)",
				dsMe.AvatarSourceLabels["upstream:mockop"], dsMe.AvatarSourceLabels)
		}
		log.Printf("  /me.avatarSource=%q avatarSourceUrls has user+upstream:mockop keys; label='Mock OP' ✓", *dsMe.AvatarSource)

		// (b) ?source previews both resolve to a non-empty image/webp regardless of
		// which source is active (a 200 carrying a JSON error envelope would be a bug).
		for _, src := range []string{"upstream:mockop", "user"} {
			resp, err := avClient.getRaw("/avatar/" + avSubject + "?source=" + src)
			if err != nil {
				log.Fatalf("avatar-fed dual: GET ?source=%s: %v", src, err)
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				log.Fatalf("avatar-fed dual: ?source=%s want 200, got %d", src, resp.StatusCode)
			}
			if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "image/webp") {
				log.Fatalf("avatar-fed dual: ?source=%s content-type want image/webp, got %q", src, ct)
			}
			if len(body) == 0 {
				log.Fatalf("avatar-fed dual: ?source=%s returned empty body", src)
			}
			log.Printf("  ?source=%s → 200 image/webp %d bytes ✓", src, len(body))
		}

		// (c) Switch active → upstream. 204 No Content; DB pointer flips; active GET 200.
		if resp, err := avClient.putJSONRaw(selPath, map[string]string{"source": "upstream:mockop"}); err != nil {
			log.Fatalf("avatar-fed dual: PUT selection upstream: %v", err)
		} else {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != http.StatusNoContent {
				log.Fatalf("avatar-fed dual: PUT selection upstream: want 204, got %d (%s)", resp.StatusCode, body)
			}
		}
		if src, _, err := getAvatarSourceEtag(avMe.ID); err != nil {
			log.Fatalf("avatar-fed dual: read source post-upstream: %v", err)
		} else if src != "upstream:mockop" {
			log.Fatalf("avatar-fed dual: avatar_source after switch want 'upstream:mockop', got %q", src)
		}
		if resp, err := avClient.getRaw("/avatar/" + avSubject); err != nil {
			log.Fatalf("avatar-fed dual: active GET post-upstream: %v", err)
		} else {
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				log.Fatalf("avatar-fed dual: active GET post-upstream want 200, got %d", resp.StatusCode)
			}
		}
		log.Printf("  selection→upstream: 204, avatar_source='upstream:mockop', active GET 200 ✓")

		// (d) Switch active → none. 204; pointer='none'; active GET 404; /me.avatarUrl nil.
		if resp, err := avClient.putJSONRaw(selPath, map[string]string{"source": "none"}); err != nil {
			log.Fatalf("avatar-fed dual: PUT selection none: %v", err)
		} else {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != http.StatusNoContent {
				log.Fatalf("avatar-fed dual: PUT selection none: want 204, got %d (%s)", resp.StatusCode, body)
			}
		}
		if src, _, err := getAvatarSourceEtag(avMe.ID); err != nil {
			log.Fatalf("avatar-fed dual: read source post-none: %v", err)
		} else if src != "none" {
			log.Fatalf("avatar-fed dual: avatar_source after switch want 'none', got %q", src)
		}
		if resp, err := avClient.getRaw("/avatar/" + avSubject); err != nil {
			log.Fatalf("avatar-fed dual: active GET post-none: %v", err)
		} else {
			resp.Body.Close()
			if resp.StatusCode != http.StatusNotFound {
				log.Fatalf("avatar-fed dual: active GET post-none want 404, got %d", resp.StatusCode)
			}
		}
		if noneMe, err := avClient.getMe(); err != nil {
			log.Fatalf("avatar-fed dual: /me post-none: %v", err)
		} else if noneMe.AvatarURL != nil {
			log.Fatalf("avatar-fed dual: /me.avatarUrl want nil after selection=none, got %q", *noneMe.AvatarURL)
		}
		log.Printf("  selection→none: 204, avatar_source='none', active GET 404, /me.avatarUrl nil ✓")

		// (e) Switch active → user. 204; pointer='user'; active GET 200 (sane final state).
		if resp, err := avClient.putJSONRaw(selPath, map[string]string{"source": "user"}); err != nil {
			log.Fatalf("avatar-fed dual: PUT selection user: %v", err)
		} else {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != http.StatusNoContent {
				log.Fatalf("avatar-fed dual: PUT selection user: want 204, got %d (%s)", resp.StatusCode, body)
			}
		}
		if src, _, err := getAvatarSourceEtag(avMe.ID); err != nil {
			log.Fatalf("avatar-fed dual: read source post-user: %v", err)
		} else if src != "user" {
			log.Fatalf("avatar-fed dual: avatar_source after switch-back want 'user', got %q", src)
		}
		if resp, err := avClient.getRaw("/avatar/" + avSubject); err != nil {
			log.Fatalf("avatar-fed dual: active GET post-user: %v", err)
		} else {
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				log.Fatalf("avatar-fed dual: active GET post-user want 200, got %d", resp.StatusCode)
			}
		}
		log.Printf("  selection→user: 204, avatar_source='user', active GET 200 (final state restored) ✓")
	}

	step("avatar-fed 3/4 — userinfo fallback: picture only in /userinfo is still inherited")
	{
		const avSub = "ext-avatar-ui-1"
		opSrv.SetClaims(avSub, "ext-avatar-ui-1@example.com", true, "extavatarui1", "Ext Avatar UI")
		// Picture appears ONLY in /userinfo — the id_token omits it, forcing the
		// RP's avatar-inherit path down the UserInfo fallback branch.
		opSrv.SetPictureUserInfoOnly(opSrv.PictureURL())
		uiClient, err := newFederationClient(*baseURL)
		if err != nil {
			log.Fatalf("avatar-fed ui: client: %v", err)
		}
		if err := driveFederationToWelcome(uiClient, *baseURL, "mockop"); err != nil {
			log.Fatalf("avatar-fed ui: drive to /welcome: %v", err)
		}
		if _, err := uiClient.confirmGet(); err != nil {
			log.Fatalf("avatar-fed ui: confirm GET: %v", err)
		}
		if redirect, err := uiClient.confirmPost(); err != nil {
			log.Fatalf("avatar-fed ui: confirm POST: %v", err)
		} else if redirect != "/me" {
			log.Fatalf("avatar-fed ui: confirm redirect want /me, got %q", redirect)
		}
		uiMe, err := uiClient.getMe()
		if err != nil {
			log.Fatalf("avatar-fed ui: /me: %v", err)
		}
		uiSubject, err := getOIDCSubject(uiMe.ID)
		if err != nil {
			log.Fatalf("avatar-fed ui: oidc_subject: %v", err)
		}
		n, etag, err := pollAvatarInherited(*baseURL, uiSubject, 10*time.Second)
		if err != nil {
			log.Fatalf("avatar-fed ui: %v (proves the UserInfo fallback did NOT run)", err)
		}
		log.Printf("  GET /avatar/%s → 200 image/webp etag=%s body=%d bytes (inherited via UserInfo fallback) ✓",
			uiSubject, etag, n)

		// Negative: this account has ONLY an upstream row (no user upload), so
		// switching the active source to 'user' must be rejected with a 400
		// avatar_source_unavailable error envelope.
		if resp, err := uiClient.putJSONRaw("/api/prohibitorum/me/avatar/selection",
			map[string]string{"source": "user"}); err != nil {
			log.Fatalf("avatar-fed ui: PUT selection user (negative): %v", err)
		} else {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				log.Fatalf("avatar-fed ui: selection→user with no user row: want 400, got %d (%s)",
					resp.StatusCode, body)
			}
			var env struct {
				Code string `json:"code"`
			}
			if err := json.Unmarshal(body, &env); err != nil {
				log.Fatalf("avatar-fed ui: decode error envelope: %v (body=%s)", err, body)
			}
			if env.Code != "avatar_source_unavailable" {
				log.Fatalf("avatar-fed ui: selection→user error code want 'avatar_source_unavailable', got %q (body=%s)",
					env.Code, body)
			}
			log.Printf("  selection→user with no stored user row → 400 avatar_source_unavailable ✓")
		}

		// Reset the picture knobs so they don't bleed into later mockop uses.
		opSrv.SetPicture("")
	}

	// =========================================================================
	// oidc surface: downstream OIDC OP — a mock relying party drives the full
	// authorization-code + PKCE flow against the Prohibitorum OP, then exercises
	// /userinfo, /introspect, refresh-token rotation + reuse detection, /revoke,
	// and RP-initiated logout.
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
	// Pre-condition: c holds a live webauthn session (re-established during the link/unlink re-login
	// and never logged out since — the link/unlink steps only touched non-session-killing
	// endpoints). The OP issuer == *baseURL (configx defaults OIDC.Issuer to the
	// first public origin, which the dev server sets to *baseURL).

	// Refresh c's /me to make sure the session is live and to capture the
	// account id (the OP projects oidc_subject for this account into id_token.sub).
	oidcMe, err := c.getMe()
	if err != nil {
		log.Fatalf("oidc: c has no live session at start of OIDC OP steps: %v", err)
	}
	issuer := *baseURL
	const rpClientID = "smoke-rp"
	rpRedirectURI := *baseURL + "/rp/callback"
	rpPostLogout := *baseURL + "/rp/post-logout"

	step(fmt.Sprintf("oidc %d/%d — GET /oauth/jwks → exactly 1 auto-provisioned signing key", 1, nOIDC))
	// The server auto-provisions an active OIDC signing key on first boot
	// (server.ensureActiveSigningKey), so the OP is signable out of the box with
	// no manual `signing-key generate`. Assert that boot key is the sole
	// publishable key — the single-key invariant the rotation arc (admin 4)
	// relies on. (A second `generate` here would add a pending key and break it.)
	jwks, err := fetchJWKS(*baseURL)
	if err != nil {
		log.Fatalf("fetch jwks: %v", err)
	}
	if len(jwks.Keys) != 1 {
		log.Fatalf("jwks: want exactly 1 key, got %d", len(jwks.Keys))
	}
	signingKID := jwks.Keys[0].KeyID
	if signingKID == "" {
		log.Fatalf("jwks: auto-provisioned key has empty kid")
	}
	log.Printf("  auto-provisioned signing key kid=%s; /oauth/jwks has 1 key ✓", signingKID)

	step(fmt.Sprintf("oidc %d/%d — oidc-client create (confidential, openid+profile+offline_access)", 2, nOIDC))
	rpSecret, err := createOIDCClient(*baseURL, rpClientID, rpRedirectURI, rpPostLogout,
		[]string{"openid", "profile", "offline_access"})
	if err != nil {
		log.Fatalf("oidc-client create: %v", err)
	}
	if rpSecret == "" {
		log.Fatalf("oidc-client create: empty client secret parsed from CLI output")
	}
	log.Printf("  client %q registered; secret len=%d", rpClientID, len(rpSecret))

	step(fmt.Sprintf("oidc %d/%d — GET /oauth/authorize (PKCE S256) → 302 with code+state+iss", 3, nOIDC))
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

	step(fmt.Sprintf("oidc %d/%d — POST /oauth/token (authorization_code, Basic auth)", 4, nOIDC))
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

	step(fmt.Sprintf("oidc %d/%d — GET /oauth/userinfo (Bearer access token)", 5, nOIDC))
	userinfo, err := fetchUserinfo(*baseURL, accessToken)
	if err != nil {
		log.Fatalf("/oauth/userinfo: %v", err)
	}
	if got := str(userinfo["sub"]); got != idSub {
		log.Fatalf("userinfo sub: want %q (matching id_token), got %q", idSub, got)
	}
	if str(userinfo["username"]) != oidcMe.Username {
		log.Fatalf("userinfo username: want %q, got %q", oidcMe.Username, str(userinfo["username"]))
	}
	if str(userinfo["displayName"]) == "" {
		log.Fatalf("userinfo missing displayName (profile scope granted)")
	}
	log.Printf("  userinfo sub matches id_token; username=%s displayName=%s ✓",
		str(userinfo["username"]), str(userinfo["displayName"]))

	step(fmt.Sprintf("oidc %d/%d — POST /oauth/introspect (access token, Basic auth) → active", 6, nOIDC))
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

	step(fmt.Sprintf("oidc %d/%d — POST /oauth/token (refresh_token rotation, Basic auth)", 7, nOIDC))
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

	step(fmt.Sprintf("oidc %d/%d — refresh idempotency window + reuse detection", 8, nOIDC))
	// Refresh rotation carries a short previous-token idempotency window so a
	// benign client double-submit / network retry of the JUST-rotated token
	// returns the SAME successor instead of falsely tripping reuse and locking
	// the client out. A genuinely superseded token (older than the single
	// previous generation, or beyond the window) is still reuse → invalid_grant
	// + family revocation.
	//
	// (a) Replay the immediately-previous token within the window → SAME
	// successor (no second mint, family intact).
	idem, err := tokenExchange(*baseURL, rpClientID, rpSecret, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {oldRefreshToken},
	})
	if err != nil {
		log.Fatalf("refresh idempotent replay: %v", err)
	}
	if idem.RefreshToken != refreshToken {
		log.Fatalf("idempotent replay: want same successor %q, got %q", refreshToken, idem.RefreshToken)
	}
	// (b) Rotate again so the original token is now TWO generations old (beyond
	// the single-previous-token window) — no time-wait needed.
	refreshed2, err := tokenExchange(*baseURL, rpClientID, rpSecret, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	})
	if err != nil {
		log.Fatalf("/oauth/token second rotation: %v", err)
	}
	if refreshed2.RefreshToken == "" || refreshed2.RefreshToken == refreshToken {
		log.Fatalf("second rotation did not rotate")
	}
	refreshToken = refreshed2.RefreshToken
	// (c) Replay the two-generations-old token → genuine reuse → 400 + revoke.
	if err := tokenExpectError(*baseURL, rpClientID, rpSecret, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {oldRefreshToken},
	}, http.StatusBadRequest, "invalid_grant"); err != nil {
		log.Fatalf("refresh reuse (two-generations-old): %v", err)
	}
	log.Printf("  idempotent replay → same successor; two-gen-old replay → 400 reuse ✓")

	step(fmt.Sprintf("oidc %d/%d — reuse detection revoked the whole family (current token now dead)", 9, nOIDC))
	// The earlier reuse trip revoked the family, so the current (twice-rotated)
	// token must now also fail with invalid_grant.
	if err := tokenExpectError(*baseURL, rpClientID, rpSecret, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	}, http.StatusBadRequest, "invalid_grant"); err != nil {
		log.Fatalf("post-reuse family revocation: %v", err)
	}
	log.Printf("  current refresh_token also dead post-reuse (family revoked) ✓")

	step(fmt.Sprintf("oidc %d/%d — fresh authorize+token, then /oauth/revoke the refresh token", 10, nOIDC))
	// The family was revoked by the reuse trip; mint a fresh code → token to get a
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

	step(fmt.Sprintf("oidc %d/%d — /oauth/revoke an access token → revoked_jti row", 11, nOIDC))
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

	step(fmt.Sprintf("oidc %d/%d — negative — unregistered redirect_uri never sends browser to bad URI", 12, nOIDC))
	if err := authorizeExpectDirectError(c, *baseURL, rpClientID,
		*baseURL+"/rp/UNREGISTERED-callback", issuer); err != nil {
		log.Fatalf("negative unregistered redirect_uri: %v", err)
	}
	log.Printf("  /authorize with bad redirect_uri → 302 /error?error=invalid_request (no Location to the bad URI) ✓")

	step(fmt.Sprintf("oidc %d/%d — negative — PKCE mismatch at /token → invalid_grant", 13, nOIDC))
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

	step(fmt.Sprintf("oidc %d/%d — negative — bad client secret at /token → invalid_client (401)", 14, nOIDC))
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

	step(fmt.Sprintf("oidc %d/%d — GET /oidc/logout (id_token_hint + post_logout_redirect_uri)", 15, nOIDC))
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

	step(fmt.Sprintf("oidc %d/%d — logout revoked c's IdP session (the id_token's sid) → /me 401", 16, nOIDC))
	if _, err := c.getMe(); err == nil {
		log.Fatalf("/me succeeded after /oidc/logout; expected 401 (session sid should be revoked)")
	}
	log.Printf("  c's /me now 401 — logout revoked the id_token's sid session ✓")

	step(fmt.Sprintf("oidc %d/%d — DB assert — revoked_jti row for the revoked access token", 17, nOIDC))
	if err := verifyRevokedJTI(revokedJTI); err != nil {
		log.Fatalf("revoked_jti DB assert: %v", err)
	}

	step(fmt.Sprintf("oidc %d/%d — DB assert — credential_event (factor=oidc_client) lifecycle", 18, nOIDC))
	if err := verifyOIDCAuditEvents(); err != nil {
		log.Fatalf("oidc audit DB assert: %v", err)
	}

	// =========================================================================
	// saml surface: SAML IdP — an in-process mock SP drives the full SP-initiated
	// Web Browser SSO profile against the Prohibitorum IdP, verifies the auto-
	// POSTed SAMLResponse with crewjam ServiceProvider, asserts NameID stability
	// across a second SSO, then exercises Single Logout (revoking exactly the
	// bound IdP session), the require_signed / bad-ACS / replay negatives, and the
	// DB-state asserts (saml_subject_id stability, saml_session rows,
	// credential_event factor=saml_sp).
	//
	// The SAME auto-provisioned signing_key signs the SAML assertions — no new
	// key. The mock SP is registered with --kind ghes, which forces
	// require_signed_authn_request=true (needed for the unsigned-AuthnRequest
	// negative).
	//
	// Pre-condition: c's session was revoked by RP-initiated logout (/oidc/logout). We must
	// re-establish a fresh webauthn session before the SSO steps.

	step(fmt.Sprintf("saml %d/%d — re-login via webauthn (c's session was revoked by /oidc/logout)", 1, nSAML))
	{
		relogin, err := c.beginLogin()
		if err != nil {
			log.Fatalf("saml relogin/begin: %v", err)
		}
		signed, err := auth.signAssertion(relogin.Challenge, *baseURL)
		if err != nil {
			log.Fatalf("saml relogin sign: %v", err)
		}
		if err := c.completeLogin(auth, signed); err != nil {
			log.Fatalf("saml relogin/complete: %v", err)
		}
	}
	samlMe, err := c.getMe()
	if err != nil {
		log.Fatalf("saml: c has no live session at start of SAML steps: %v", err)
	}
	log.Printf("  smoke-admin id=%d back in session for SAML steps", samlMe.ID)

	step(fmt.Sprintf("saml %d/%d — GET /saml/metadata → EntityDescriptor with ≥1 signing KeyDescriptor", 2, nSAML))
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

	step(fmt.Sprintf("saml %d/%d — saml-sp create --kind ghes --metadata-file <mock SP metadata>", 3, nSAML))
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

	step(fmt.Sprintf("saml %d/%d — signed AuthnRequest → /saml/sso → verify SAMLResponse + GHES attrs", 4, nSAML))
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
		if username != samlMe.Username {
			log.Fatalf("SAMLResponse USERNAME: want %q, got %q", samlMe.Username, username)
		}
		// crewjam already enforced Destination/Recipient==ACS and Audience==entityID
		// during ParseXMLResponse (it rejects otherwise); assert NameID + USERNAME here.
		log.Printf("  SAMLResponse verified: NameID=%.16s… USERNAME=%s; Destination/Recipient/Audience enforced by ParseXMLResponse ✓",
			stableNameID, username)
	}

	step(fmt.Sprintf("saml %d/%d — second SSO (same account+SP) → NameID identical (stability)", 5, nSAML))
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

	step(fmt.Sprintf("saml %d/%d — DB assert — saml_subject_id stable (1 row, same name_id) + ≥1 saml_session row", 6, nSAML))
	if err := verifySAMLSubjectStable(samlMe.ID, stableNameID); err != nil {
		log.Fatalf("saml_subject_id DB assert: %v", err)
	}
	// Steps 91+92 were two SSOs from the SAME session (client c) to the SAME SP.
	// Post Fix C2 (UNIQUE (session_id, sp_id, session_index) + upsert), those
	// collapse to ONE row (the second SSO refreshes not_on_or_after rather than
	// duplicating). So the correct expectation here is exactly 1, not 2.
	if err := verifySAMLSessionCount(samlMe.ID, 1); err != nil {
		log.Fatalf("saml_session DB assert: %v", err)
	}

	step(fmt.Sprintf("saml %d/%d — SLO — drive a DEDICATED session's SSO, then sign a LogoutRequest targeting it", 7, nSAML))
	// SLO revokes the IdP session bound to the saml_session (sessionIndex = the
	// session's ID). To avoid breaking c (needed for the replay negative below),
	// drive the SSO that we will SLO from a SEPARATE client cSLO whose own login
	// session is the one we then assert-revoked. We target that exact session by
	// passing its session ID as the LogoutRequest SessionIndex.
	cSLO, err := newClient(*baseURL)
	if err != nil {
		log.Fatalf("saml SLO client: %v", err)
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

	step(fmt.Sprintf("saml %d/%d — signed LogoutRequest → signed LogoutResponse + bound session revoked", 8, nSAML))
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

	step(fmt.Sprintf("saml %d/%d — negative — UNSIGNED AuthnRequest to require_signed GHES SP → rejected", 9, nSAML))
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

	step(fmt.Sprintf("saml %d/%d — negative — AuthnRequest with bad/unregistered ACS URL → rejected", 10, nSAML))
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

	step(fmt.Sprintf("saml %d/%d — negative — replayed AuthnRequest ID (same request twice) → 2nd rejected", 11, nSAML))
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

	step(fmt.Sprintf("saml %d/%d — DB assert — credential_event (factor=saml_sp) sso(use) + slo(session_end)", 12, nSAML))
	if err := verifySAMLAuditEvents(); err != nil {
		log.Fatalf("saml audit DB assert: %v", err)
	}

	// =========================================================================
	// hardening surface: protocol-completeness gate. Forced re-authentication
	// (OIDC prompt=login / max_age, SAML ForceAuthn), prompt=none + stale,
	// PKCE method rejection, public-client introspection refusal, SAML
	// NameIDPolicy mismatch, ForceAuthn+IsPassive NoPassive, POST-binding
	// AuthnRequest intake, signed IdP metadata, and IdP-initiated SSO.
	//
	// Pre-condition: c holds a live webauthn session (re-logged in during the saml
	// arc; the SSO/SLO steps only drove OTHER clients or touched c's
	// session non-destructively). The saml mock
	// SP `sp` + its verifier `spProvider`, ssoURL, mockSPACSURL, and the
	// auto-provisioned signing key are all still in scope and reused here.

	// freshLogin re-runs the WebAuthn login ceremony on c, minting a NEW
	// session (fresh auth_time) on the cookie jar — the move that satisfies a
	// prompt=login / ForceAuthn re-auth demand whose nonce predates it.
	freshLogin := func() {
		lo, err := c.beginLogin()
		if err != nil {
			log.Fatalf("hardening fresh login/begin: %v", err)
		}
		signed, err := auth.signAssertion(lo.Challenge, *baseURL)
		if err != nil {
			log.Fatalf("hardening fresh login sign: %v", err)
		}
		if err := c.completeLogin(auth, signed); err != nil {
			log.Fatalf("hardening fresh login/complete: %v", err)
		}
	}

	hardeningMe, err := c.getMe()
	if err != nil {
		log.Fatalf("hardening: c has no live session at start of hardening steps: %v", err)
	}
	_ = hardeningMe

	// ---- OIDC forced re-auth + policy steps (reuse the step-71 confidential
	// client `smoke-rp` + `rpRedirectURI`/`issuer`). ----

	step(fmt.Sprintf("hardening %d/%d — OIDC prompt=login bounces (stale session), then a fresh login + reauth nonce issues a code", 1, nHardening))
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

	step(fmt.Sprintf("hardening %d/%d — OIDC max_age=0 bounces; max_age=3600 issues a code", 2, nHardening))
	{
		// max_age=0 demands re-auth regardless of how recent the session is →
		// a bounce to /login (the freshly minted session still cannot
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

	step(fmt.Sprintf("hardening %d/%d — OIDC prompt=none + stale → redirect with error=login_required (no /login bounce)", 3, nHardening))
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

	step(fmt.Sprintf("hardening %d/%d — OIDC code_challenge_method=plain → redirect error=invalid_request", 4, nHardening))
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

	step(fmt.Sprintf("hardening %d/%d — public OIDC client — introspect → invalid_client (401); confidential still works; public revoke OK", 5, nHardening))
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
	// Reuses the saml mock SP `sp`, verifier `spProvider`, ssoURL, mockSPACSURL.
	// c is freshly logged-in (the re-auth steps minted recent sessions).

	step(fmt.Sprintf("hardening %d/%d — SAML ForceAuthn bounces (stale session), then a fresh login + reauth nonce issues an assertion", 6, nHardening))
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

	step(fmt.Sprintf("hardening %d/%d — SAML ForceAuthn + IsPassive → NoPassive status Response (no assertion)", 7, nHardening))
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

	step(fmt.Sprintf("hardening %d/%d — SAML NameIDPolicy Format=emailAddress (≠ persistent) → InvalidNameIDPolicy", 8, nHardening))
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

	step(fmt.Sprintf("hardening %d/%d — SAML POST-binding (enveloped-signed) AuthnRequest → assertion", 9, nHardening))
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

	step(fmt.Sprintf("hardening %d/%d — SAML /saml/metadata is SIGNED, verifies against its own cert, validUntil is future", 10, nHardening))
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

	step(fmt.Sprintf("hardening %d/%d — SAML IdP-initiated SSO — opted-in SP gets an unsolicited Response (RelayState echoed); the non-opted-in SP without the flag → 302 /error", 11, nHardening))
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

		// The prior SP did NOT opt in → 302 /error?error=saml_idp_init_disabled.
		// ssoInit returns (status, body, err) but not the Location header. Use a
		// non-following client directly to capture the redirect target.
		{
			q302 := url.Values{"sp": {mockSPEntityID}}
			req302, _ := http.NewRequest(http.MethodGet, c.base+"/saml/sso/init?"+q302.Encode(), nil)
			hcNF := &http.Client{
				Jar:     c.jar,
				Timeout: 10 * time.Second,
				CheckRedirect: func(*http.Request, []*http.Request) error {
					return http.ErrUseLastResponse
				},
			}
			resp302, err302 := hcNF.Do(req302)
			if err302 != nil {
				log.Fatalf("/saml/sso/init (no opt-in): %v", err302)
			}
			body302, _ := io.ReadAll(resp302.Body)
			_ = resp302.Body.Close()
			if resp302.StatusCode != http.StatusFound {
				log.Fatalf("/saml/sso/init for an SP without --allow-idp-initiated: want 302, got %d (body=%s)",
					resp302.StatusCode, firstN(string(body302), 200))
			}
			loc302 := resp302.Header.Get("Location")
			if !strings.HasPrefix(loc302, "/error?error=saml_idp_init_disabled") {
				log.Fatalf("/saml/sso/init (no opt-in): Location want /error?error=saml_idp_init_disabled prefix, got %q", loc302)
			}
			log.Printf("  /saml/sso/init for the prior SP (no opt-in) → 302 %s ✓", loc302)
		}
	}

	step(fmt.Sprintf("hardening %d/%d — DB assert — credential_event covers the SAML re-auth/idp-initiated lifecycle", 12, nHardening))
	if err := verifyHardeningSAMLAuditEvents(); err != nil {
		log.Fatalf("hardening SAML audit DB assert: %v", err)
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

	step(fmt.Sprintf("consent %d/%d — UI: consent flow (require-consent client) — bounce, context, approve, remember, prompt=consent, deny", 1, nConsent))
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

		// (3) POST approve → {redirect}. The OP validates return_to server-side
		// (validateReturnTo) and returns the same-origin RELATIVE path (the
		// absolute issuer authorize URL normalised to path+query); the SPA
		// hardRedirects it. Assert that validated relative form.
		var res1 struct {
			Redirect string `json:"redirect"`
		}
		if err := c.postJSON("/api/prohibitorum/consent?return_to="+url.QueryEscape(returnTo1),
			map[string]string{"ticket": ticket1, "decision": "approve"}, &res1); err != nil {
			log.Fatalf("POST /consent approve: %v", err)
		}
		// retryPath is the validated relative form of return_to — it doubles as
		// the expected approve redirect and the path we re-drive authorize on.
		retryPath, err := pathQueryOf(returnTo1)
		if err != nil {
			log.Fatalf("consent approve parse return_to: %v", err)
		}
		if res1.Redirect != retryPath {
			log.Fatalf("consent approve redirect: want %q (validated relative), got %q", retryPath, res1.Redirect)
		}
		log.Printf("  POST approve → redirect == validated return_to (relative) ✓ (grant stored)")

		// (4) Re-drive authorize on the return_to → now issues a code.
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

	step(fmt.Sprintf("consent %d/%d — UI: GET /api/prohibitorum/auth/federation → 200 JSON array incl. seeded slugs", 2, nConsent))
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

	// =========================================================================
	// Admin Management API arc (steps 114–121) — Task 10 integration capstone.
	//
	// Runs LAST, while c still holds a live admin session (the consent arc drove
	// /oauth/authorize and got codes, not /login bounces). Every 🔐 mutation is
	// preceded by a fresh sudoWebAuthn; the multi-use sudo window means one
	// elevation covers subsequent gated actions until expiry, but we re-assert
	// before each mutation for test isolation.
	//
	// The signing-key sub-arc adds a 2nd key; this is safe because nothing after
	// this arc depends on JWKS having exactly 1 key (oidc 1's "exactly 1 key"
	// assertion ran far earlier, before any key was added here).
	// =========================================================================

	step(fmt.Sprintf("admin %d/%d — admin: POST /oidc-clients reveals secret ONCE; GET list/one never expose it", 1, nAdmin))
	const adminClientID = "smoke-admin-rp"
	var createdClientSecret string
	{
		if err := sudoWebAuthn(c, auth, *baseURL); err != nil {
			log.Fatalf("admin oidc-client create: sudo: %v", err)
		}
		var created struct {
			ClientID                string   `json:"clientId"`
			DisplayName             string   `json:"displayName"`
			RedirectURIs            []string `json:"redirectUris"`
			TokenEndpointAuthMethod string   `json:"tokenEndpointAuthMethod"`
			Secret                  string   `json:"secret"`
		}
		if err := c.postJSON("/api/prohibitorum/oidc-applications", map[string]any{
			"clientId":     adminClientID,
			"displayName":  "Smoke Admin RP",
			"redirectUris": []string{*baseURL + "/admin-rp/callback"},
			"scopes":       []string{"openid", "profile"},
			"public":       false,
		}, &created); err != nil {
			log.Fatalf("POST /oidc-clients: %v", err)
		}
		if created.ClientID != adminClientID {
			log.Fatalf("create: clientId want %q got %q", adminClientID, created.ClientID)
		}
		if created.Secret == "" {
			log.Fatalf("create: confidential client response must reveal a secret exactly once, got empty")
		}
		if created.TokenEndpointAuthMethod == "" {
			log.Fatalf("create: tokenEndpointAuthMethod must be set for a confidential client")
		}
		createdClientSecret = created.Secret

		// GET list → secret/hash NEVER present (verify on the raw bytes too).
		listRaw, err := c.getBytes("/api/prohibitorum/oidc-applications")
		if err != nil {
			log.Fatalf("GET /oidc-clients: %v", err)
		}
		assertNoSecretLeak("GET /oidc-clients", listRaw)
		// And the created client appears in the list.
		var list []struct {
			ClientID string `json:"clientId"`
		}
		if err := json.Unmarshal(listRaw, &list); err != nil {
			log.Fatalf("GET /oidc-clients decode: %v", err)
		}
		var found bool
		for _, c := range list {
			if c.ClientID == adminClientID {
				found = true
			}
		}
		if !found {
			log.Fatalf("GET /oidc-clients: created client %q not in list", adminClientID)
		}

		// GET one → secret/hash NEVER present.
		oneRaw, err := c.getBytes("/api/prohibitorum/oidc-applications/" + url.PathEscape(adminClientID))
		if err != nil {
			log.Fatalf("GET /oidc-clients/%s: %v", adminClientID, err)
		}
		assertNoSecretLeak("GET /oidc-clients/{clientId}", oneRaw)
		log.Printf("  created %q (secret len=%d, revealed once); list+get expose NO secret/hash ✓", adminClientID, len(createdClientSecret))
	}

	step(fmt.Sprintf("admin %d/%d — admin: PUT /oidc-clients/{clientId} changes config; reflected on GET", 2, nAdmin))
	const updatedDisplayName = "Smoke Admin RP (renamed)"
	updatedRedirectURI := *baseURL + "/admin-rp/callback2"
	{
		if err := sudoWebAuthn(c, auth, *baseURL); err != nil {
			log.Fatalf("admin oidc-client update: sudo: %v", err)
		}
		var updated struct {
			ClientID     string   `json:"clientId"`
			DisplayName  string   `json:"displayName"`
			RedirectURIs []string `json:"redirectUris"`
		}
		if err := c.putJSON("/api/prohibitorum/oidc-applications/"+url.PathEscape(adminClientID), map[string]any{
			"displayName":    updatedDisplayName,
			"redirectUris":   []string{updatedRedirectURI},
			"allowedScopes":  []string{"openid", "profile"},
			"requireConsent": false,
			"disabled":       false,
		}, &updated); err != nil {
			log.Fatalf("PUT /oidc-clients/%s: %v", adminClientID, err)
		}
		// Re-GET and assert the change is reflected.
		var got struct {
			DisplayName  string   `json:"displayName"`
			RedirectURIs []string `json:"redirectUris"`
		}
		if err := c.get("/api/prohibitorum/oidc-applications/"+url.PathEscape(adminClientID), &got); err != nil {
			log.Fatalf("GET (post-update) /oidc-clients/%s: %v", adminClientID, err)
		}
		if got.DisplayName != updatedDisplayName {
			log.Fatalf("update not reflected: displayName want %q got %q", updatedDisplayName, got.DisplayName)
		}
		if !slices.Contains(got.RedirectURIs, updatedRedirectURI) {
			log.Fatalf("update not reflected: redirectUris %v missing %q", got.RedirectURIs, updatedRedirectURI)
		}
		log.Printf("  PUT changed displayName→%q + redirect_uri; GET reflects both ✓", updatedDisplayName)
	}

	step(fmt.Sprintf("admin %d/%d — admin: POST /oidc-clients/rotate-secret returns a NEW secret (≠ create secret)", 3, nAdmin))
	{
		if err := sudoWebAuthn(c, auth, *baseURL); err != nil {
			log.Fatalf("admin oidc-client rotate-secret: sudo: %v", err)
		}
		var rotated struct {
			ClientID string `json:"clientId"`
			Secret   string `json:"secret"`
		}
		if err := c.postJSON("/api/prohibitorum/oidc-applications/rotate-secret",
			map[string]any{"clientId": adminClientID}, &rotated); err != nil {
			log.Fatalf("POST /oidc-clients/rotate-secret: %v", err)
		}
		if rotated.Secret == "" {
			log.Fatalf("rotate-secret: empty secret")
		}
		if rotated.Secret == createdClientSecret {
			log.Fatalf("rotate-secret: returned the SAME secret as create — rotation did not change it")
		}
		log.Printf("  rotate-secret → new secret (len=%d) ≠ create secret ✓", len(rotated.Secret))
	}

	step(fmt.Sprintf("admin %d/%d — admin: snapshot JWKS (1 active key=oldKID) + mint priorKeyToken under oldKID", 4, nAdmin))
	var oldKID string
	var priorKeyToken string
	{
		jwksBefore, err := fetchJWKS(*baseURL)
		if err != nil {
			log.Fatalf("fetch jwks (snapshot): %v", err)
		}
		if len(jwksBefore.Keys) != 1 {
			log.Fatalf("jwks snapshot: want exactly 1 key at arc start, got %d", len(jwksBefore.Keys))
		}
		oldKID = jwksBefore.Keys[0].KeyID
		if oldKID == "" {
			log.Fatalf("jwks snapshot: empty kid")
		}
		// Mint a token under the current active key via the existing smoke-rp
		// confidential client + c's live session.
		v, code := freshAuthorizeCode(c, *baseURL, rpClientID, rpRedirectURI, issuer)
		ptok, err := tokenExchange(*baseURL, rpClientID, rpSecret, url.Values{
			"grant_type":    {"authorization_code"},
			"code":          {code},
			"redirect_uri":  {rpRedirectURI},
			"code_verifier": {v},
		})
		if err != nil {
			log.Fatalf("mint priorKeyToken: token exchange: %v", err)
		}
		priorKeyToken = ptok.IDToken
		if priorKeyToken == "" {
			log.Fatalf("mint priorKeyToken: empty id_token")
		}
		kid, err := tokenKID(priorKeyToken)
		if err != nil {
			log.Fatalf("priorKeyToken kid: %v", err)
		}
		if kid != oldKID {
			log.Fatalf("priorKeyToken signed by kid=%q, want active oldKID=%q", kid, oldKID)
		}
		if _, err := verifyIDToken(*baseURL, priorKeyToken); err != nil {
			log.Fatalf("priorKeyToken must verify now: %v", err)
		}
		log.Printf("  JWKS=1 key oldKID=%s; priorKeyToken signed by oldKID and verifies now ✓", oldKID)
	}

	step(fmt.Sprintf("admin %d/%d — admin: POST /signing-keys/generate → newKID PENDING; JWKS publishes BOTH; oldKID still signs", 5, nAdmin))
	var newKID string
	{
		if err := sudoWebAuthn(c, auth, *baseURL); err != nil {
			log.Fatalf("admin signing-key generate: sudo: %v", err)
		}
		var gen struct {
			Kid    string `json:"kid"`
			Status string `json:"status"`
		}
		if err := c.postJSON("/api/prohibitorum/signing-keys/generate", map[string]any{}, &gen); err != nil {
			log.Fatalf("POST /signing-keys/generate: %v", err)
		}
		newKID = gen.Kid
		if newKID == "" || newKID == oldKID {
			log.Fatalf("generate: newKID=%q invalid (oldKID=%q)", newKID, oldKID)
		}
		if gen.Status != "pending" {
			log.Fatalf("generate: new key status want pending, got %q", gen.Status)
		}
		// Sealed at rest: the new key's private material is DEK-encrypted
		// (private_pem_enc populated) — there is no plaintext column.
		if dburl := os.Getenv("PROHIBITORUM_DATABASE_URL"); dburl != "" {
			sealed, err := dbScalar(dburl, fmt.Sprintf(
				"SELECT (private_pem_enc IS NOT NULL)::text FROM signing_key WHERE kid = '%s'", newKID))
			if err != nil {
				log.Fatalf("generate: query sealed-at-rest state: %v", err)
			}
			if len(sealed) != 1 || sealed[0] != "true" {
				log.Fatalf("generate: newKID=%q must be sealed at rest (private_pem_enc set); got %v", newKID, sealed)
			}
			log.Printf("  newKID private key sealed at rest (private_pem_enc set) ✓")
		}
		// JWKS now publishes BOTH keys.
		jwks2, err := fetchJWKS(*baseURL)
		if err != nil {
			log.Fatalf("fetch jwks (post-generate): %v", err)
		}
		if len(jwks2.Keys) != 2 {
			log.Fatalf("jwks post-generate: want 2 keys, got %d", len(jwks2.Keys))
		}
		if len(jwks2.Key(oldKID)) == 0 || len(jwks2.Key(newKID)) == 0 {
			log.Fatalf("jwks post-generate must publish both oldKID=%q and newKID=%q; got kids=%v", oldKID, newKID, jwksKIDs(jwks2))
		}
		// oldKID is still the signer: mint a token, assert kid == oldKID.
		v, code := freshAuthorizeCode(c, *baseURL, rpClientID, rpRedirectURI, issuer)
		t2, err := tokenExchange(*baseURL, rpClientID, rpSecret, url.Values{
			"grant_type":    {"authorization_code"},
			"code":          {code},
			"redirect_uri":  {rpRedirectURI},
			"code_verifier": {v},
		})
		if err != nil {
			log.Fatalf("post-generate token exchange: %v", err)
		}
		kid, err := tokenKID(t2.IDToken)
		if err != nil {
			log.Fatalf("post-generate token kid: %v", err)
		}
		if kid != oldKID {
			log.Fatalf("post-generate: signer should still be oldKID=%q, got %q (pending key must NOT sign)", oldKID, kid)
		}
		log.Printf("  newKID=%s pending; JWKS=2 (oldKID active + newKID pending); oldKID still signs ✓", newKID)
	}

	step(fmt.Sprintf("admin %d/%d — admin: POST /signing-keys/{newKID}/activate → newKID signs; oldKID decommissioning; both in JWKS; priorKeyToken STILL verifies", 6, nAdmin))
	{
		if err := sudoWebAuthn(c, auth, *baseURL); err != nil {
			log.Fatalf("admin signing-key activate: sudo: %v", err)
		}
		var act struct {
			Kid    string `json:"kid"`
			Status string `json:"status"`
		}
		if err := c.postJSON("/api/prohibitorum/signing-keys/"+url.PathEscape(newKID)+"/activate", map[string]any{}, &act); err != nil {
			log.Fatalf("POST /signing-keys/%s/activate: %v", newKID, err)
		}
		if act.Status != "active" {
			log.Fatalf("activate: newKID status want active, got %q", act.Status)
		}
		// (a) signing now uses newKID — mint a FRESH token, assert kid == newKID.
		v, code := freshAuthorizeCode(c, *baseURL, rpClientID, rpRedirectURI, issuer)
		t3, err := tokenExchange(*baseURL, rpClientID, rpSecret, url.Values{
			"grant_type":    {"authorization_code"},
			"code":          {code},
			"redirect_uri":  {rpRedirectURI},
			"code_verifier": {v},
		})
		if err != nil {
			log.Fatalf("post-activate token exchange: %v", err)
		}
		freshKID, err := tokenKID(t3.IDToken)
		if err != nil {
			log.Fatalf("post-activate token kid: %v", err)
		}
		if freshKID != newKID {
			log.Fatalf("post-activate: signer should be newKID=%q, got %q", newKID, freshKID)
		}
		if _, err := verifyIDToken(*baseURL, t3.IDToken); err != nil {
			log.Fatalf("post-activate fresh token must verify: %v", err)
		}

		// (b) GET /signing-keys shows oldKID=decommissioning, newKID=active.
		keysRaw, err := c.getBytes("/api/prohibitorum/signing-keys")
		if err != nil {
			log.Fatalf("GET /signing-keys: %v", err)
		}
		// (e) no private_pem-like field present in the JSON.
		assertNoSecretLeak("GET /signing-keys", keysRaw)
		var keys []struct {
			Kid    string `json:"kid"`
			Status string `json:"status"`
		}
		if err := json.Unmarshal(keysRaw, &keys); err != nil {
			log.Fatalf("GET /signing-keys decode: %v", err)
		}
		statusByKID := map[string]string{}
		for _, k := range keys {
			statusByKID[k.Kid] = k.Status
		}
		if statusByKID[oldKID] != "decommissioning" {
			log.Fatalf("post-activate: oldKID=%q status want decommissioning, got %q (all=%v)", oldKID, statusByKID[oldKID], statusByKID)
		}
		if statusByKID[newKID] != "active" {
			log.Fatalf("post-activate: newKID=%q status want active, got %q (all=%v)", newKID, statusByKID[newKID], statusByKID)
		}

		// (c) JWKS still publishes BOTH newKID (active) and oldKID (decommissioning).
		jwks3, err := fetchJWKS(*baseURL)
		if err != nil {
			log.Fatalf("fetch jwks (post-activate): %v", err)
		}
		if len(jwks3.Key(oldKID)) == 0 || len(jwks3.Key(newKID)) == 0 {
			log.Fatalf("jwks post-activate must still publish both oldKID=%q and newKID=%q; got kids=%v", oldKID, newKID, jwksKIDs(jwks3))
		}

		// (d) priorKeyToken (signed by oldKID) STILL VERIFIES — grace-window proof.
		if _, err := verifyIDToken(*baseURL, priorKeyToken); err != nil {
			log.Fatalf("GRACE-WINDOW FAIL: priorKeyToken (signed by decommissioning oldKID=%q) must still verify, got: %v", oldKID, err)
		}
		log.Printf("  newKID=%s now SIGNS; GET /signing-keys: oldKID=decommissioning newKID=active; JWKS still publishes BOTH; priorKeyToken (oldKID) STILL verifies in grace ✓", newKID)
	}

	step(fmt.Sprintf("admin %d/%d — admin: GET /audit-events?factor=oidc_client|signing_key shows the mutations; light redaction spot-check", 7, nAdmin))
	{
		var oidcEvents []contractAuditEvent
		if err := c.get("/api/prohibitorum/audit-events?factor=oidc_client&limit=200", &oidcEvents); err != nil {
			log.Fatalf("GET /audit-events?factor=oidc_client: %v", err)
		}
		var sawClientRegister bool
		for _, e := range oidcEvents {
			if e.Factor != "oidc_client" {
				log.Fatalf("audit filter leaked a non-oidc_client factor: %q", e.Factor)
			}
			if e.Event == "register" {
				if cid, _ := e.Detail["client_id"].(string); cid == adminClientID {
					sawClientRegister = true
				}
			}
			assertAuditDetailNoSecret("oidc_client", e.Event, e.Detail)
		}
		if !sawClientRegister {
			log.Fatalf("audit: no oidc_client register event for %q found in %d events", adminClientID, len(oidcEvents))
		}

		var keyEvents []contractAuditEvent
		if err := c.get("/api/prohibitorum/audit-events?factor=signing_key&limit=200", &keyEvents); err != nil {
			log.Fatalf("GET /audit-events?factor=signing_key: %v", err)
		}
		var sawKeyMutation bool
		for _, e := range keyEvents {
			if e.Factor != "signing_key" {
				log.Fatalf("audit filter leaked a non-signing_key factor: %q", e.Factor)
			}
			// generate emits register; activate emits update — either proves it.
			if e.Event == "register" || e.Event == "update" {
				if kid, _ := e.Detail["kid"].(string); kid == newKID {
					sawKeyMutation = true
				}
			}
			assertAuditDetailNoSecret("signing_key", e.Event, e.Detail)
		}
		if !sawKeyMutation {
			log.Fatalf("audit: no signing_key register/update event for newKID=%q found in %d events", newKID, len(keyEvents))
		}
		log.Printf("  audit-events: oidc_client register(%q) + signing_key mutation(newKID) present; no secret in any detail ✓", adminClientID)
	}

	step(fmt.Sprintf("admin %d/%d — admin: GET /accounts/{id}/credentials lists passkey(s) w/ 4-char suffix only; force-revoke succeeds under the active sudo window (gate-deny covered by admin_route_policy_test)", 8, nAdmin))
	{
		me, err := c.getMe()
		if err != nil {
			log.Fatalf("admin credentials: getMe: %v", err)
		}
		credsRaw, err := c.getBytes(fmt.Sprintf("/api/prohibitorum/accounts/%d/credentials", me.ID))
		if err != nil {
			log.Fatalf("GET /accounts/%d/credentials: %v", me.ID, err)
		}
		var creds []struct {
			ID                 int32  `json:"id"`
			CredentialIDSuffix string `json:"credentialIdSuffix"`
		}
		if err := json.Unmarshal(credsRaw, &creds); err != nil {
			log.Fatalf("GET /accounts/{id}/credentials decode: %v (body=%s)", err, credsRaw)
		}
		if len(creds) == 0 {
			log.Fatalf("admin credentials: account %d should have >=1 registered passkey, got 0", me.ID)
		}
		for _, cr := range creds {
			if len(cr.CredentialIDSuffix) != 4 {
				log.Fatalf("admin credentials: credentialIdSuffix must be exactly 4 chars, got %q (len=%d)", cr.CredentialIDSuffix, len(cr.CredentialIDSuffix))
			}
		}
		// The full credential id must NOT appear anywhere in the response (only
		// the 4-char suffix is forensic-safe).
		if strings.Contains(string(credsRaw), "credentialId\"") {
			log.Fatalf("admin credentials: response exposes a full credentialId field: %s", credsRaw)
		}

		// Force-revoke is sudo-gated. Gate enforcement is covered by
		// admin_route_policy_test.go (unit test). Here we verify the happy
		// path: the sudo window from the activate-signing-key step is still
		// valid (multi-use, 15 min TTL), so a force-revoke call succeeds
		// without a new explicit step-up. Credentials are returned ORDER BY
		// created_at DESC, so creds[0] is the most recently added passkey
		// (the step-16 second credential / auth2). We revoke that one to keep
		// the original passkey (auth, creds[len-1]) intact for later logins.
		revokeTarget := creds[0]
		resp, err := c.postJSONRaw("/api/prohibitorum/accounts/credentials/delete",
			map[string]any{"accountId": me.ID, "credentialId": revokeTarget.ID})
		if err != nil {
			log.Fatalf("admin credentials: POST delete (force-revoke): %v", err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
			log.Fatalf("admin credentials: force-revoke WITH sudo must succeed (204/200), got %d (body=%s)", resp.StatusCode, body)
		}
		log.Printf("  /accounts/%d/credentials: %d passkey(s), 4-char suffix only, no full id; force-revoke (sudo window from the activate step) → %d ✓", me.ID, len(creds), resp.StatusCode)
	}

	// =========================================================================
	// Tier-1 self-service + admin reads (steps appended after the existing 121).
	//
	// Pre-condition: c holds a live webauthn session (confirmed by the admin arc's
	// c.getMe() success). Password+TOTP were revoked by the destructive-revoke step and never
	// restored; all assertions below are written accordingly.
	// me2.ID is the smoke-admin's account id (set during enrollment, never changes).
	// =========================================================================

	step("Tier-1 1/4 — PUT /me round-trip (displayName rename + revert)")
	{
		// Capture current displayName.
		before, err := c.getMe()
		if err != nil {
			log.Fatalf("Tier-1 1/4: getMe before: %v", err)
		}
		originalName := before.DisplayName

		// Rename.
		var renamed meResponse
		if err := c.putJSON("/api/prohibitorum/me",
			map[string]string{"displayName": "Smoke Renamed"}, &renamed); err != nil {
			log.Fatalf("Tier-1 1/4: PUT /me (rename): %v", err)
		}
		// Re-GET and assert.
		after, err := c.getMe()
		if err != nil {
			log.Fatalf("Tier-1 1/4: getMe after rename: %v", err)
		}
		if after.DisplayName != "Smoke Renamed" {
			log.Fatalf("Tier-1 1/4: displayName after rename: want %q, got %q", "Smoke Renamed", after.DisplayName)
		}
		if after.Username != before.Username {
			log.Fatalf("Tier-1 1/4: username changed across rename: was %q, now %q", before.Username, after.Username)
		}
		if after.Role != before.Role {
			log.Fatalf("Tier-1 1/4: role changed across rename: was %q, now %q", before.Role, after.Role)
		}
		log.Printf("  displayName → %q (rename confirmed, username=%s role=%s)", after.DisplayName, after.Username, after.Role)

		// Revert.
		var reverted meResponse
		if err := c.putJSON("/api/prohibitorum/me",
			map[string]string{"displayName": originalName}, &reverted); err != nil {
			log.Fatalf("Tier-1 1/4: PUT /me (revert): %v", err)
		}
		check, err := c.getMe()
		if err != nil {
			log.Fatalf("Tier-1 1/4: getMe after revert: %v", err)
		}
		if check.DisplayName != originalName {
			log.Fatalf("Tier-1 1/4: displayName after revert: want %q, got %q", originalName, check.DisplayName)
		}
		log.Printf("  displayName reverted to %q ✓", check.DisplayName)
	}

	step("Tier-1 2/4 — GET /me/factors (passkey count >= 1; password+TOTP revoked earlier)")
	{
		var factors struct {
			PasswordSet            bool `json:"passwordSet"`
			TOTPEnrolled           bool `json:"totpEnrolled"`
			RecoveryCodesRemaining int  `json:"recoveryCodesRemaining"`
			PasskeyCount           int  `json:"passkeyCount"`
		}
		if err := c.get("/api/prohibitorum/me/factors", &factors); err != nil {
			log.Fatalf("Tier-1 2/4: GET /me/factors: %v", err)
		}
		log.Printf("  factors: passwordSet=%v totpEnrolled=%v recoveryCodesRemaining=%d passkeyCount=%d",
			factors.PasswordSet, factors.TOTPEnrolled, factors.RecoveryCodesRemaining, factors.PasskeyCount)
		if factors.PasswordSet {
			log.Fatalf("Tier-1 2/4: passwordSet=true but password was revoked by the destructive-revoke step")
		}
		if factors.TOTPEnrolled {
			log.Fatalf("Tier-1 2/4: totpEnrolled=true but TOTP was revoked by the destructive-revoke step")
		}
		if factors.PasskeyCount < 1 {
			log.Fatalf("Tier-1 2/4: passkeyCount=%d, want >=1", factors.PasskeyCount)
		}
		log.Printf("  /me/factors: passwordSet=false totpEnrolled=false recoveryCodesRemaining=%d passkeyCount=%d ✓",
			factors.RecoveryCodesRemaining, factors.PasskeyCount)
	}

	step("Tier-1 3/4 — admin GET /accounts/{id}/sessions: len>=1, all isCurrent==false")
	{
		var sessions []struct {
			ID         string `json:"id"`
			IsCurrent  bool   `json:"isCurrent"`
			IssuedAt   string `json:"issuedAt"`
			ExpiresAt  string `json:"expiresAt"`
			LastSeenIP string `json:"lastSeenIp"`
			UserAgent  string `json:"userAgent"`
		}
		path := fmt.Sprintf("/api/prohibitorum/accounts/%d/sessions", me2.ID)
		if err := c.get(path, &sessions); err != nil {
			log.Fatalf("Tier-1 3/4: GET %s: %v", path, err)
		}
		if len(sessions) < 1 {
			log.Fatalf("Tier-1 3/4: admin sessions: want >=1, got 0 for account %d", me2.ID)
		}
		for _, s := range sessions {
			if s.IsCurrent {
				log.Fatalf("Tier-1 3/4: admin sessions: isCurrent=true for session %q — admin route must always return false", s.ID)
			}
		}
		log.Printf("  /accounts/%d/sessions → %d session(s), all isCurrent=false ✓", me2.ID, len(sessions))
	}

	step("Tier-1 4/4 — SAML provider PUT attr_map round-trip")
	{
		// Find the mock SP (entityId == mockSPEntityID == "https://mock-sp.smoke.test")
		// via the admin list endpoint, then GET the full record, PUT back with
		// attributeMap added, and assert it round-trips on GET.
		type samlProviderItem struct {
			ID                        int64           `json:"id"`
			EntityID                  string          `json:"entityId"`
			DisplayName               string          `json:"displayName"`
			NameIDFormat              string          `json:"nameIdFormat"`
			AttributeMap              json.RawMessage `json:"attributeMap"`
			RequireSignedAuthnRequest bool            `json:"requireSignedAuthnRequest"`
			AllowIdpInitiated         bool            `json:"allowIdpInitiated"`
		}

		// List all SAML providers to find the mock SP's id.
		var providers []samlProviderItem
		if err := c.get("/api/prohibitorum/saml-applications", &providers); err != nil {
			log.Fatalf("Tier-1 4/4: GET /saml-providers: %v", err)
		}
		var spID int64
		for _, p := range providers {
			if p.EntityID == mockSPEntityID {
				spID = p.ID
				break
			}
		}
		if spID == 0 {
			log.Fatalf("Tier-1 4/4: mock SP %q not found in /saml-providers list (%d providers)", mockSPEntityID, len(providers))
		}
		log.Printf("  mock SP id=%d found in list", spID)

		// GET the full provider record to capture current required fields.
		var current samlProviderItem
		if err := c.get(fmt.Sprintf("/api/prohibitorum/saml-applications/%d", spID), &current); err != nil {
			log.Fatalf("Tier-1 4/4: GET /saml-providers/%d: %v", spID, err)
		}

		// Build the attribute map entry to add.
		type attrEntry struct {
			Name       string `json:"name"`
			NameFormat string `json:"name_format"`
			Source     string `json:"source"`
			Multi      bool   `json:"multi"`
		}
		newAttrs := []attrEntry{
			{
				Name:       "EMAIL",
				NameFormat: "urn:oasis:names:tc:SAML:2.0:attrname-format:basic",
				Source:     "email",
				Multi:      false,
			},
		}

		// sudo is required for PUT /saml-providers/{id} (registerSudoOpHTTP).
		// Log out and back in to start a fresh recent-auth window (new IssuedAt)
		// before the sudo call; the previous window was issued during the admin arc.
		if err := c.logout(); err != nil {
			log.Fatalf("Tier-1 4/4: logout pre-SAML-PUT: %v", err)
		}
		{
			lo, err := c.beginLogin()
			if err != nil {
				log.Fatalf("Tier-1 4/4: relogin/begin: %v", err)
			}
			signed, err := auth.signAssertion(lo.Challenge, *baseURL)
			if err != nil {
				log.Fatalf("Tier-1 4/4: relogin sign: %v", err)
			}
			if err := c.completeLogin(auth, signed); err != nil {
				log.Fatalf("Tier-1 4/4: relogin/complete: %v", err)
			}
		}
		if err := sudoWebAuthn(c, auth, *baseURL); err != nil {
			log.Fatalf("Tier-1 4/4: sudo webauthn (pre SAML PUT): %v", err)
		}

		// displayName must not be empty (PUT handler rejects empty string).
		// Use the stored value if present, otherwise fall back to the entityId.
		displayNameForPUT := current.DisplayName
		if displayNameForPUT == "" {
			displayNameForPUT = current.EntityID
		}

		// PUT back the current fields with attributeMap added.
		putBody := map[string]any{
			"displayName":               displayNameForPUT,
			"nameIdFormat":              current.NameIDFormat,
			"attributeMap":              newAttrs,
			"requireSignedAuthnRequest": current.RequireSignedAuthnRequest,
			"allowIdpInitiated":         current.AllowIdpInitiated,
		}
		var putResp samlProviderItem
		if err := c.putJSON(fmt.Sprintf("/api/prohibitorum/saml-applications/%d", spID), putBody, &putResp); err != nil {
			log.Fatalf("Tier-1 4/4: PUT /saml-providers/%d: %v", spID, err)
		}

		// Re-GET to confirm round-trip.
		var updated samlProviderItem
		if err := c.get(fmt.Sprintf("/api/prohibitorum/saml-applications/%d", spID), &updated); err != nil {
			log.Fatalf("Tier-1 4/4: GET /saml-providers/%d (post-PUT): %v", spID, err)
		}
		// Decode the returned attributeMap and check for the EMAIL entry.
		var gotAttrs []attrEntry
		if err := json.Unmarshal(updated.AttributeMap, &gotAttrs); err != nil {
			log.Fatalf("Tier-1 4/4: attributeMap decode: %v (raw=%s)", err, updated.AttributeMap)
		}
		var foundEmail bool
		for _, a := range gotAttrs {
			if a.Name == "EMAIL" {
				foundEmail = true
				break
			}
		}
		if !foundEmail {
			log.Fatalf("Tier-1 4/4: attributeMap round-trip: EMAIL entry missing (got %+v)", gotAttrs)
		}
		log.Printf("  PUT /saml-providers/%d: attributeMap has EMAIL entry ✓", spID)
	}

	// =========================================================================
	// Multi-use sudo window — verifies that a single elevation (from Tier-1 4/4's
	// sudoWebAuthn call above) covers MULTIPLE gated actions until the window
	// expires. We call the same sudo-gated endpoint twice without a new step-up;
	// both must succeed under the existing window (not one-shot).
	// =========================================================================
	{
		step("sudo-multiuse 1/2 — first gated action succeeds under existing sudo window (from Tier-1 4/4)")
		resp1, err := c.postJSONRaw("/api/prohibitorum/me/credentials/register/begin", map[string]any{})
		if err != nil {
			log.Fatalf("sudo-multiuse: first register/begin: %v", err)
		}
		status1 := resp1.StatusCode
		_ = resp1.Body.Close()
		if status1 != http.StatusOK {
			log.Fatalf("sudo-multiuse: first register/begin: want 200, got %d", status1)
		}
		log.Printf("  first register/begin → 200 ✓ (sudo window from Tier-1 4/4 still active)")

		step("sudo-multiuse 2/2 — second gated action within same window also succeeds (multi-use)")
		resp2, err := c.postJSONRaw("/api/prohibitorum/me/credentials/register/begin", map[string]any{})
		if err != nil {
			log.Fatalf("sudo-multiuse: second register/begin: %v", err)
		}
		status2 := resp2.StatusCode
		body2, _ := io.ReadAll(resp2.Body)
		_ = resp2.Body.Close()
		if status2 != http.StatusOK {
			log.Fatalf("sudo-multiuse: second register/begin: want 200 (multi-use window still active), got %d (body=%s)",
				status2, body2)
		}
		log.Printf("  second register/begin → 200 ✓ (multi-use window confirmed; elevation not one-shot)")
	}

	// =========================================================================
	// Avatar round-trip: upload → public GET → OIDC picture claim assertion.
	//
	// Pre-condition: c holds a live webauthn session. idSub (the smoke-admin's
	// OIDC subject UUID) and rpClientID/rpSecret/rpRedirectURI/issuer are all
	// still in scope from the oidc block.
	// =========================================================================

	step("avatar 1/4 — PUT /me/avatar with a tiny 8×8 PNG (upload round-trip)")
	var pngBuf bytes.Buffer
	{
		img := image.NewRGBA(image.Rect(0, 0, 8, 8))
		if err := png.Encode(&pngBuf, img); err != nil {
			log.Fatalf("avatar: encode PNG: %v", err)
		}
	}
	{
		req, err := http.NewRequest(http.MethodPut,
			*baseURL+"/api/prohibitorum/me/avatar",
			bytes.NewReader(pngBuf.Bytes()))
		if err != nil {
			log.Fatalf("avatar: build PUT request: %v", err)
		}
		req.Header.Set("Content-Type", "image/png")
		// Attach the session cookies from c's jar so the server recognises the authed request.
		for _, ck := range c.cookies() {
			req.AddCookie(ck)
		}
		resp, err := c.hc.Do(req)
		if err != nil {
			log.Fatalf("avatar: PUT /me/avatar: %v", err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			log.Fatalf("avatar: PUT /me/avatar: want 204, got %d (body=%s)", resp.StatusCode, body)
		}
		log.Printf("  PUT /me/avatar → 204 ✓")
	}

	step("avatar 2/4 — GET /avatar/{subject} (public, unauthenticated) → 200 image/webp + ETag")
	{
		resp, err := http.Get(*baseURL + "/avatar/" + idSub)
		if err != nil {
			log.Fatalf("avatar: GET /avatar/%s: %v", idSub, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			log.Fatalf("avatar: GET /avatar/%s: want 200, got %d (body=%s)", idSub, resp.StatusCode, body)
		}
		ct := resp.Header.Get("Content-Type")
		if !strings.HasPrefix(ct, "image/webp") {
			log.Fatalf("avatar: GET /avatar/%s: want Content-Type image/webp, got %q", idSub, ct)
		}
		if len(body) == 0 {
			log.Fatalf("avatar: GET /avatar/%s: body is empty", idSub)
		}
		etag := resp.Header.Get("ETag")
		if etag == "" {
			log.Fatalf("avatar: GET /avatar/%s: ETag header missing", idSub)
		}
		log.Printf("  GET /avatar/%s → 200 Content-Type=%s ETag=%s body=%d bytes ✓", idSub, ct, etag, len(body))
	}

	step("avatar 3/4 — GET /me reflects non-null avatarUrl with prefix /avatar/{subject}?v=")
	{
		meAvatar, err := c.getMe()
		if err != nil {
			log.Fatalf("avatar: getMe post-upload: %v", err)
		}
		if meAvatar.AvatarURL == nil {
			log.Fatalf("avatar: /me.avatarUrl is null after upload (expected non-null)")
		}
		wantPrefix := *baseURL + "/avatar/" + idSub + "?v="
		if !strings.HasPrefix(*meAvatar.AvatarURL, wantPrefix) {
			log.Fatalf("avatar: /me.avatarUrl=%q does not have expected prefix %q", *meAvatar.AvatarURL, wantPrefix)
		}
		log.Printf("  /me.avatarUrl=%q (prefix OK) ✓", *meAvatar.AvatarURL)
	}

	step("avatar 4/4 — GET /oauth/userinfo (fresh token, profile scope) has picture claim matching /avatar/{subject}?v=")
	{
		v, code := freshAuthorizeCode(c, *baseURL, rpClientID, rpRedirectURI, issuer)
		tok, err := tokenExchange(*baseURL, rpClientID, rpSecret, url.Values{
			"grant_type":    {"authorization_code"},
			"code":          {code},
			"redirect_uri":  {rpRedirectURI},
			"code_verifier": {v},
		})
		if err != nil {
			log.Fatalf("avatar: token exchange for userinfo: %v", err)
		}
		ui, err := fetchUserinfo(*baseURL, tok.AccessToken)
		if err != nil {
			log.Fatalf("avatar: GET /oauth/userinfo: %v", err)
		}
		pic := str(ui["picture"])
		wantPrefix := *baseURL + "/avatar/" + idSub + "?v="
		if !strings.HasPrefix(pic, wantPrefix) {
			log.Fatalf("avatar: userinfo.picture=%q does not have expected prefix %q", pic, wantPrefix)
		}
		log.Printf("  userinfo.picture=%q (picture claim present, prefix OK) ✓", pic)
	}

	// =========================================================================
	// RBAC end-to-end arc (Task 11): per-app access gate + OIDC groups claim.
	//
	// Reuses the bootstrap smoke-admin (account id == me2.ID, set during enrollment).
	// A fresh restricted OIDC client is created, then:
	//   deny  — the admin (NOT yet granted) drives an interactive authorize →
	//           302 to <issuer>/error?reason=app_access_denied, NO code.
	//   grant — a group is created, the admin is added as a member, and access is
	//           granted to that GROUP (exercising the via-group path).
	//   allow — the admin re-drives authorize (scope "openid groups") → a code;
	//           the code is exchanged; the id_token AND /userinfo both carry a
	//           groups claim containing the group slug.
	//
	// Pre-condition: c holds a live webauthn session (avatar 4/4 above drove
	// freshAuthorizeCode(c, …) successfully — that requires a live session).
	// Every admin mutation is sudo-gated; each is preceded by a fresh
	// sudoWebAuthn (multi-use window, re-asserted before each mutation for
	// test isolation) exactly as the admin arc (steps 114–121) does.
	// rpRedirectURI shape + issuer reused from the oidc block. The group is
	// created exposedToDownstream:true so its slug
	// surfaces in the groups claim (ListExposedGroupSlugsByAccount).
	// =========================================================================
	{
		const rbacGroupSlug = "smoke-rbac-team"
		const rbacClientID = "smoke-rbac-rp"
		rbacRedirectURI := *baseURL + "/rbac-rp/callback"

		step("rbac 1/7 — create exposed group {slug:smoke-rbac-team} (sudo)")
		var rbacGroupID int32
		{
			if err := sudoWebAuthn(c, auth, *baseURL); err != nil {
				log.Fatalf("rbac: sudo (pre group create): %v", err)
			}
			var created struct {
				ID                  int32  `json:"id"`
				Slug                string `json:"slug"`
				ExposedToDownstream bool   `json:"exposedToDownstream"`
			}
			if err := c.postJSON("/api/prohibitorum/groups", map[string]any{
				"slug":                rbacGroupSlug,
				"displayName":         "Smoke RBAC Team",
				"exposedToDownstream": true,
			}, &created); err != nil {
				log.Fatalf("rbac: POST /groups: %v", err)
			}
			if created.ID == 0 {
				log.Fatalf("rbac: POST /groups returned id=0")
			}
			if created.Slug != rbacGroupSlug {
				log.Fatalf("rbac: group slug: want %q, got %q", rbacGroupSlug, created.Slug)
			}
			if !created.ExposedToDownstream {
				log.Fatalf("rbac: group exposedToDownstream must be true (groups claim depends on it)")
			}
			rbacGroupID = created.ID
			log.Printf("  group id=%d slug=%s exposedToDownstream=true created ✓", rbacGroupID, rbacGroupSlug)
		}

		step("rbac 2/7 — add the smoke-admin as a group member (sudo)")
		{
			// The admin's account id is me2.ID (the smoke already fetched /me at
			// enrollment; me2.ID never changes across the run).
			if err := sudoWebAuthn(c, auth, *baseURL); err != nil {
				log.Fatalf("rbac: sudo (pre member add): %v", err)
			}
			memberPath := fmt.Sprintf("/api/prohibitorum/groups/%d/members", rbacGroupID)
			if err := c.postJSON(memberPath, map[string]any{"accountId": me2.ID}, nil); err != nil {
				log.Fatalf("rbac: POST %s: %v", memberPath, err)
			}
			log.Printf("  added account id=%d as a member of group id=%d ✓", me2.ID, rbacGroupID)
		}

		step("rbac 3/7 — oidc-client create (confidential, scopes openid+profile+groups)")
		// allowed_scopes must include "groups" or the authorize would reject the
		// requested scope with invalid_scope before the access gate is reached.
		rbacSecret, err := createOIDCClient(*baseURL, rbacClientID, rbacRedirectURI, rbacRedirectURI,
			[]string{"openid", "profile", "groups"})
		if err != nil {
			log.Fatalf("rbac: oidc-client create: %v", err)
		}
		if rbacSecret == "" {
			log.Fatalf("rbac: oidc-client create: empty client secret parsed from CLI output")
		}
		log.Printf("  client %q registered (scopes openid+profile+groups); secret len=%d ✓", rbacClientID, len(rbacSecret))

		step("rbac 4/7 — mark the client restricted (sudo)")
		{
			if err := sudoWebAuthn(c, auth, *baseURL); err != nil {
				log.Fatalf("rbac: sudo (pre set-restricted): %v", err)
			}
			restrictPath := "/api/prohibitorum/oidc-applications/" + url.PathEscape(rbacClientID) + "/access/set-restricted"
			if err := c.postJSON(restrictPath, map[string]any{"restricted": true}, nil); err != nil {
				log.Fatalf("rbac: POST %s: %v", restrictPath, err)
			}
			log.Printf("  client %q access_restricted=true ✓", rbacClientID)
		}

		step("rbac 5/7 — DENY: authorize as the not-yet-granted admin → 302 /error?reason=app_access_denied (no code)")
		{
			// Build an interactive authorize (NOT prompt=none) so the denial lands on
			// the IdP's own /error page rather than an RP error redirect.
			_, challenge := genPKCE()
			state := randState()
			denyAuthz := fmt.Sprintf(
				"/oauth/authorize?response_type=code&client_id=%s&redirect_uri=%s&scope=%s&state=%s&nonce=%s&code_challenge=%s&code_challenge_method=S256",
				url.QueryEscape(rbacClientID),
				url.QueryEscape(rbacRedirectURI),
				url.QueryEscape("openid groups"),
				url.QueryEscape(state),
				url.QueryEscape(randState()),
				url.QueryEscape(challenge),
			)
			loc, err := authorizeRaw(c, denyAuthz)
			if err != nil {
				log.Fatalf("rbac: deny authorize: %v", err)
			}
			// The denial must NOT redirect to the RP redirect_uri with a code.
			if strings.HasPrefix(loc, rbacRedirectURI) {
				log.Fatalf("rbac: deny authorize redirected to the RP redirect_uri (no denial enforced): %q", loc)
			}
			u, perr := url.Parse(loc)
			if perr != nil {
				log.Fatalf("rbac: deny authorize parse Location %q: %v", loc, perr)
			}
			if u.Path != "/error" {
				log.Fatalf("rbac: deny authorize: want a bounce to %s/error, got path %q (loc=%q)", issuer, u.Path, loc)
			}
			if u.Query().Get("reason") != "app_access_denied" {
				log.Fatalf("rbac: deny authorize: want reason=app_access_denied, got %q (loc=%q)", u.Query().Get("reason"), loc)
			}
			// No authorization code may be issued on the denial path.
			if u.Query().Get("code") != "" {
				log.Fatalf("rbac: deny authorize issued a code despite the access denial: %q", loc)
			}
			log.Printf("  not-yet-granted admin → 302 %s/error?reason=app_access_denied (no code) ✓", issuer)
		}

		step("rbac 6/7 — GRANT access to the group the admin belongs to (via-group path, sudo)")
		{
			if err := sudoWebAuthn(c, auth, *baseURL); err != nil {
				log.Fatalf("rbac: sudo (pre grant): %v", err)
			}
			grantPath := "/api/prohibitorum/oidc-applications/" + url.PathEscape(rbacClientID) + "/access/grant"
			if err := c.postJSON(grantPath, map[string]any{
				"principalKind": "group",
				"principalId":   rbacGroupID,
			}, nil); err != nil {
				log.Fatalf("rbac: POST %s: %v", grantPath, err)
			}
			log.Printf("  granted group id=%d access to client %q (admin is a member) ✓", rbacGroupID, rbacClientID)
		}

		step("rbac 7/7 — ALLOW: authorize (openid groups) → code → token; id_token + /userinfo carry groups claim incl. the slug")
		{
			// Drive a fresh interactive authorize; the via-group grant now satisfies
			// the access gate, so a code is issued.
			verifier, challenge := genPKCE()
			state := randState()
			nonce := randState()
			allowAuthz := fmt.Sprintf(
				"/oauth/authorize?response_type=code&client_id=%s&redirect_uri=%s&scope=%s&state=%s&nonce=%s&code_challenge=%s&code_challenge_method=S256",
				url.QueryEscape(rbacClientID),
				url.QueryEscape(rbacRedirectURI),
				url.QueryEscape("openid groups"),
				url.QueryEscape(state),
				url.QueryEscape(nonce),
				url.QueryEscape(challenge),
			)
			loc, err := authorizeWithSession(c, allowAuthz)
			if err != nil {
				log.Fatalf("rbac: allow authorize: %v", err)
			}
			code, err := parseAuthorizeRedirect(loc, rbacRedirectURI, state, issuer)
			if err != nil {
				log.Fatalf("rbac: allow authorize redirect: %v", err)
			}
			log.Printf("  granted admin → 302 to redirect_uri with code (len=%d) ✓", len(code))

			tok, err := tokenExchange(*baseURL, rbacClientID, rbacSecret, url.Values{
				"grant_type":    {"authorization_code"},
				"code":          {code},
				"redirect_uri":  {rbacRedirectURI},
				"code_verifier": {verifier},
			})
			if err != nil {
				log.Fatalf("rbac: token exchange: %v", err)
			}
			if tok.IDToken == "" || tok.AccessToken == "" {
				log.Fatalf("rbac: token response missing id_token or access_token")
			}

			// id_token must carry a groups claim that includes the group slug.
			idClaims, err := verifyIDToken(*baseURL, tok.IDToken)
			if err != nil {
				log.Fatalf("rbac: verify id_token: %v", err)
			}
			if !groupsClaimContains(idClaims["groups"], rbacGroupSlug) {
				log.Fatalf("rbac: id_token groups claim does not contain %q (got %v)", rbacGroupSlug, idClaims["groups"])
			}
			log.Printf("  id_token.groups contains %q ✓", rbacGroupSlug)

			// /userinfo (Bearer access token) must carry the same groups claim.
			ui, err := fetchUserinfo(*baseURL, tok.AccessToken)
			if err != nil {
				log.Fatalf("rbac: GET /oauth/userinfo: %v", err)
			}
			if !groupsClaimContains(ui["groups"], rbacGroupSlug) {
				log.Fatalf("rbac: userinfo groups claim does not contain %q (got %v)", rbacGroupSlug, ui["groups"])
			}
			log.Printf("  userinfo.groups contains %q ✓ (per-app gate + groups claim proven via-group)", rbacGroupSlug)
		}
	}

	// SAML RBAC arc intentionally SKIPPED: the per-app access gate also covers
	// SAML SPs, but adding a SAML restrict→deny→grant→allow check would be scope
	// creep here — the SAML SSO steps (88–99) drive a require_signed GHES SP and
	// asserting the denial would need a fresh restricted SP + its own verifier.
	// The OIDC arc above fully exercises the gate (deny non-member + via-group
	// grant) and the groups claim, which is the contract under test.

	// noFollow is a one-off HTTP client that does NOT follow redirects so we
	// can assert 302 Location headers on browser-navigated error paths.
	noFollow := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	step("error-redirect 1/2 — federation callback ?error=access_denied → 302 /error?error=upstream_error")
	{
		req, err := http.NewRequest(http.MethodGet,
			*baseURL+"/api/prohibitorum/auth/federation/mockop/callback?error=access_denied&error_description=nope",
			nil)
		if err != nil {
			log.Fatalf("error-redirect 1: build request: %v", err)
		}
		resp, err := noFollow.Do(req)
		if err != nil {
			log.Fatalf("error-redirect 1: GET /callback: %v", err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusFound {
			log.Fatalf("error-redirect 1: want 302, got %d", resp.StatusCode)
		}
		loc := resp.Header.Get("Location")
		if !strings.HasPrefix(loc, "/error?error=upstream_error") {
			log.Fatalf("error-redirect 1: Location want prefix /error?error=upstream_error, got %q", loc)
		}
		log.Printf("  federation callback access_denied → 302 %s ✓", loc)
	}

	step("error-redirect 2/2 — SAML malformed SAMLRequest → 302 /error?error=saml_request_invalid")
	{
		req, err := http.NewRequest(http.MethodGet,
			*baseURL+"/saml/sso?SAMLRequest=not-base64",
			nil)
		if err != nil {
			log.Fatalf("error-redirect 2: build request: %v", err)
		}
		resp, err := noFollow.Do(req)
		if err != nil {
			log.Fatalf("error-redirect 2: GET /saml/sso: %v", err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusFound {
			log.Fatalf("error-redirect 2: want 302, got %d", resp.StatusCode)
		}
		loc := resp.Header.Get("Location")
		if !strings.HasPrefix(loc, "/error?error=saml_request_invalid") {
			log.Fatalf("error-redirect 2: Location want prefix /error?error=saml_request_invalid, got %q", loc)
		}
		log.Printf("  SAML malformed SAMLRequest → 302 %s ✓", loc)
	}

	fmt.Println()
	fmt.Println("✓ smoke OK — core (webauthn enroll/login + password/TOTP/recovery + sudo + throttle + destructive revoke) + federation (upstream OIDC login/link/unlink incl. invite_only) + oidc (OIDC OP code+PKCE flow: userinfo/introspect/refresh-rotation+reuse/revoke/logout) + saml (SAML IdP SSO/SLO + signed metadata + require_signed/bad-ACS/replay negatives) + hardening (forced re-auth / PKCE+introspect policy / NameIDPolicy / POST AuthnRequest / signed metadata / IdP-initiated) + consent (Login+Consent UI backend: consent ticket round-trip + federation-providers list) + admin (OIDC client CRUD reveal-once + signing-key generate→activate JWKS grace lifecycle + audit-events viewer + admin credential listing) + Tier-1 (PUT /me round-trip, GET /me/factors, admin sessions, SAML attr_map round-trip) + sudo-multiuse (single elevation covers multiple gated actions until expiry) + avatar (PUT /me/avatar upload, public GET /avatar/{sub} image/webp+ETag, /me.avatarUrl, userinfo.picture claim) + avatar-fed (federated first-login inherit + no-clobber on re-login + UserInfo fallback + dual-source selection/previews + avatar_source_unavailable negative) + rbac (per-app access gate + OIDC groups claim: DENY then grant-via-group → ALLOW with groups in id_token+userinfo) + error-redirect (federation access_denied + SAML malformed request → 302 /error) + DB-state assertions passed against",
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
		1:  2,  // kty: EC2
		3:  -7, // alg: ES256
		-1: 1,  // crv: P-256
		-2: x,  // x coordinate (32 bytes, big-endian)
		-3: y,  // y coordinate (32 bytes, big-endian)
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
	flags := byte(flagUP|flagUV) | flagsExtra
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
	ID                 int32             `json:"id"`
	Username           string            `json:"username"`
	DisplayName        string            `json:"displayName"`
	Role               string            `json:"role"`
	AvatarURL          *string           `json:"avatarUrl,omitempty"`
	AvatarSource       *string           `json:"avatarSource,omitempty"`
	AvatarSourceUrls   map[string]string `json:"avatarSourceUrls,omitempty"`
	AvatarSourceLabels map[string]string `json:"avatarSourceLabels,omitempty"`
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
		"clientExtensionResults":  map[string]any{},
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
//   - RevokeSession at /me/sessions/revoke)
//   - every session row has amr = '{hwk}' for WebAuthn
func verifyDB(accountID int32) error {
	dburl := os.Getenv("PROHIBITORUM_DATABASE_URL")
	if dburl == "" {
		return errors.New("PROHIBITORUM_DATABASE_URL not set; cannot verify DB state")
	}
	algs, err := dbScalar(dburl,
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

	sessRows, err := dbScalar(dburl,
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

// dbScalar runs a read-only query against the smoke database (via pgx — the
// module's Postgres driver, so no external `psql` client is needed) and returns
// one string per row. Multi-column rows are joined with "|" and NULLs render as
// "", matching the `psql -At` output the callers were originally written
// against (most queries select a single text/`::text` column).
func dbScalar(dburl, query string) ([]string, error) {
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dburl)
	if err != nil {
		return nil, fmt.Errorf("pgx connect: %w", err)
	}
	defer conn.Close(ctx)
	rows, err := conn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		cols := make([]string, len(vals))
		for i, v := range vals {
			cols[i] = fmtSQLVal(v)
		}
		out = append(out, strings.Join(cols, "|"))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}
	return out, nil
}

// fmtSQLVal renders a decoded SQL value the way `psql -At` would: NULL → "",
// bytes/text verbatim, everything else via fmt's default formatting.
func fmtSQLVal(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case []byte:
		return string(x)
	case string:
		return x
	default:
		return fmt.Sprintf("%v", x)
	}
}

// =========================================================================
// core helpers
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

// putJSONRaw is the PUT analogue of postJSONRaw: it issues PUT path with a JSON
// body and returns the raw *http.Response without c.do's >=400 error wrap, so
// callers can assert non-2xx status codes (e.g. the 400 avatar_source_unavailable
// envelope) directly.
func (c *client) putJSONRaw(path string, body any) (*http.Response, error) {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequest(http.MethodPut, c.base+path, &buf)
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
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dburl)
	if err != nil {
		return fmt.Errorf("pgx connect: %w", err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx,
		"DELETE FROM auth_throttle WHERE account_id=$1 AND factor=$2",
		accountID, factor); err != nil {
		return fmt.Errorf("delete throttle: %w", err)
	}
	return nil
}

// ---- core DB assertions ----------------------------------------------------

func verifyPasswordCredential(accountID int32) error {
	dburl := os.Getenv("PROHIBITORUM_DATABASE_URL")
	rows, err := dbScalar(dburl,
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
	rows, err := dbScalar(dburl, fmt.Sprintf(
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
	codes, err := dbScalar(dburl, fmt.Sprintf(
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
	confirmed, err := dbScalar(dburl, fmt.Sprintf(
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
	codes, err := dbScalar(dburl, fmt.Sprintf(
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
	rows, err := dbScalar(dburl, fmt.Sprintf(
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
	rows, err := dbScalar(dburl, fmt.Sprintf(
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
		rows, err := dbScalar(dburl, fmt.Sprintf(
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

// verifyCoreAuditEvents checks credential_event for the union of (factor, event)
// pairs the core surface is supposed to emit during this smoke run. Counts
// are lower bounds — the underlying writers may emit more events than the
// minimum (e.g. sudo_granted fires once per /me/sudo/complete).
func verifyCoreAuditEvents(accountID int32) error {
	dburl := os.Getenv("PROHIBITORUM_DATABASE_URL")
	rows, err := dbScalar(dburl, fmt.Sprintf(
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
	log.Printf("  credential_event covers the credential lifecycle (counts=%v)", counts)
	return nil
}

func firstN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// =========================================================================
// federation helpers
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

// getBytes issues GET path with c's session jar and returns the raw response
// body bytes (so callers can both decode AND scan the wire JSON for leaked
// secret material). Errors on any >= 400 status.
func (c *client) getBytes(path string) ([]byte, error) {
	resp, err := c.getRaw(path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("GET %s: %d %s — %s", path, resp.StatusCode, resp.Status, body)
	}
	return body, nil
}

// putJSON issues PUT path with a JSON body and decodes the response into out
// (out may be nil). Mirrors postJSON. Used by the admin oidc-client update arc.
func (c *client) putJSON(path string, body any, out any) error {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return err
		}
	}
	req, err := http.NewRequest(http.MethodPut, c.base+path, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, out)
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
// asserts the RP /callback responds 302 to /error?error=<wantCode>.
// The backend now redirects browser-navigated error paths to the SPA /error
// page instead of returning a JSON body, so we use a non-following client
// to observe the 302 + Location header directly.
// On success returns nil; on any divergence, returns a descriptive error.
func expectFederationCallbackError(c *client, baseURL, slug string, wantCode string) error {
	loginPath := fmt.Sprintf("/api/prohibitorum/auth/federation/%s/login?return_to=/me", slug)
	authorizeURL, err := c.getRedirect(loginPath)
	if err != nil {
		return fmt.Errorf("login: %w", err)
	}
	callbackURL, err := followMockOPAuthorize(authorizeURL)
	if err != nil {
		return fmt.Errorf("authorize: %w", err)
	}
	// Issue the callback request with a non-following client to capture the
	// 302 Location header (the error redirect to /error?error=<code>).
	noFollow := &http.Client{
		Jar:     c.jar,
		Timeout: 10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, err := http.NewRequest(http.MethodGet, callbackURL, nil)
	if err != nil {
		return err
	}
	resp, err := noFollow.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusFound {
		return fmt.Errorf("/callback status: want 302, got %d (body=%s)", resp.StatusCode, string(body))
	}
	loc := resp.Header.Get("Location")
	wantPrefix := "/error?error=" + wantCode
	if !strings.HasPrefix(loc, wantPrefix) {
		return fmt.Errorf("/callback Location: want prefix %q, got %q", wantPrefix, loc)
	}
	return nil
}

// federationConfirmView mirrors contract.FederationConfirmView — the projection
// the /welcome confirm GET returns for a pending first-time federated identity.
type federationConfirmView struct {
	IDPDisplayName string  `json:"idpDisplayName"`
	DisplayName    string  `json:"displayName"`
	Username       string  `json:"username"`
	Email          string  `json:"email"`
	AvatarURL      *string `json:"avatarUrl,omitempty"`
	AvatarPending  bool    `json:"avatarPending"`
}

// confirmGet drives GET /api/prohibitorum/auth/federation/confirm — the
// grant-scoped peek the /welcome page makes to render the pending identity.
// The fed-state (confirmation-grant) cookie must already be in c's jar (set by
// the callback 302). No session is required or issued.
func (c *client) confirmGet() (*federationConfirmView, error) {
	var v federationConfirmView
	if err := c.get("/api/prohibitorum/auth/federation/confirm", &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// confirmPost drives POST /api/prohibitorum/auth/federation/confirm — the YES
// click: single-use-consumes the grant, stamps confirmed_at, and issues the
// durable session cookie. Returns the {redirect} the server hands back.
func (c *client) confirmPost() (string, error) {
	var out struct {
		Redirect string `json:"redirect"`
	}
	if err := c.postJSON("/api/prohibitorum/auth/federation/confirm", map[string]any{}, &out); err != nil {
		return "", err
	}
	return out.Redirect, nil
}

// driveFederationToWelcome runs /federation/{slug}/login → upstream /authorize →
// RP /callback for a FIRST-TIME (unconfirmed) federated identity and asserts the
// callback 302s to /welcome with NO session issued (post-callback getMe must
// fail). The fed-state confirmation-grant cookie is left in c's jar so a
// subsequent confirmGet/confirmPost can complete the ceremony. The mockop
// *server* claims must already be set by the caller.
func driveFederationToWelcome(c *client, baseURL, slug string) error {
	loginPath := fmt.Sprintf("/api/prohibitorum/auth/federation/%s/login?return_to=/me", slug)
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
	if loc != "/welcome" {
		return fmt.Errorf("callback: want redirect to /welcome (unconfirmed first login), got %q", loc)
	}
	// The callback must NOT have issued a session for an unconfirmed identity.
	if _, err := c.getMe(); err == nil {
		return errors.New("callback issued a session before confirmation; /me should 401 pre-confirm")
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
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dburl)
	if err != nil {
		return 0, fmt.Errorf("pgx connect: %w", err)
	}
	defer conn.Close(ctx)
	// DELETE any prior row with this slug so re-running the smoke is idempotent
	// (cascades to account_identity rows).
	if _, err := conn.Exec(ctx, "DELETE FROM upstream_idp WHERE slug=$1", slug); err != nil {
		return 0, fmt.Errorf("delete prior: %w", err)
	}
	if allowedDomains == nil {
		allowedDomains = []string{}
	}
	// Insert with placeholder secret bytes; the real ciphertext is written by the
	// UPDATE below, once we know the row id (the AAD binds the ciphertext to it).
	// pgx binds Go values natively (text[] from []string, bytea from []byte), so
	// no hand-built ARRAY[…]::text[] / E'\\x…' SQL literals are needed.
	var id int64
	if err := conn.QueryRow(ctx, `INSERT INTO upstream_idp
		(slug, display_name, issuer_url, client_id, client_secret_enc, secret_nonce,
		 key_version, scopes, mode, allowed_domains, username_claim, display_name_claim,
		 email_claim, require_verified_email)
		VALUES ($1, $2, $3, $4, $5, $6, 1,
		  ARRAY['openid','profile','email']::text[], $7, $8,
		  'preferred_username', 'name', 'email', $9)
		RETURNING id`,
		slug, displayName, issuer, clientID, []byte{0}, []byte{0},
		mode, allowedDomains, requireVerifiedEmail).Scan(&id); err != nil {
		return 0, fmt.Errorf("insert: %w", err)
	}
	ct, nonce, err := fedoidc.EncryptClientSecret(dek, []byte(clientSecret), id, 1)
	if err != nil {
		return 0, fmt.Errorf("EncryptClientSecret: %w", err)
	}
	if _, err := conn.Exec(ctx,
		"UPDATE upstream_idp SET client_secret_enc=$1, secret_nonce=$2 WHERE id=$3",
		ct, nonce, id); err != nil {
		return 0, fmt.Errorf("update secret: %w", err)
	}
	return id, nil
}

// verifyFederatedIdentityCreated asserts an account_identity row exists for
// (accountID, upstreamSub) and is owned by the given upstream_idp_id. Also
// confirms credential_event has a federation_oidc/register row.
func verifyFederatedIdentityCreated(accountID int32, upstreamSub string, idpID int64) error {
	dburl := os.Getenv("PROHIBITORUM_DATABASE_URL")
	rows, err := dbScalar(dburl, fmt.Sprintf(
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
	rows, err := dbScalar(dburl, fmt.Sprintf(
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

// getOIDCSubject reads the account.oidc_subject UUID for accountID. This is the
// path segment the public avatar endpoint GET /avatar/{subject} keys on, so the
// avatar smoke steps use it to poll for the inherited image.
func getOIDCSubject(accountID int32) (string, error) {
	dburl := os.Getenv("PROHIBITORUM_DATABASE_URL")
	rows, err := dbScalar(dburl, fmt.Sprintf(
		"SELECT oidc_subject::text FROM account WHERE id=%d", accountID))
	if err != nil {
		return "", err
	}
	if len(rows) != 1 || rows[0] == "" {
		return "", fmt.Errorf("oidc_subject for account %d: got %v", accountID, rows)
	}
	return rows[0], nil
}

// getAvatarSourceEtag reads (avatar_source, avatar_etag) for accountID. Used by
// the no-clobber assertion to prove a user-uploaded avatar survives an upstream
// re-login. NULL columns come back as empty strings.
func getAvatarSourceEtag(accountID int32) (source, etag string, err error) {
	dburl := os.Getenv("PROHIBITORUM_DATABASE_URL")
	rows, err := dbScalar(dburl, fmt.Sprintf(
		"SELECT coalesce(avatar_source,'') || '|' || coalesce(avatar_etag,'') "+
			"FROM account WHERE id=%d", accountID))
	if err != nil {
		return "", "", err
	}
	if len(rows) != 1 {
		return "", "", fmt.Errorf("avatar_source/etag for account %d: got %v", accountID, rows)
	}
	parts := strings.SplitN(rows[0], "|", 2)
	source = parts[0]
	if len(parts) == 2 {
		etag = parts[1]
	}
	return source, etag, nil
}

// pollAvatarInherited polls GET /avatar/{subject} until it returns 200 with a
// Content-Type of image/webp (the background avatar-inherit goroutine has
// stored the upstream image) or the timeout elapses. Returns the response body
// length + ETag on success.
func pollAvatarInherited(baseURL, subject string, timeout time.Duration) (int, string, error) {
	deadline := time.Now().Add(timeout)
	var lastStatus int
	var lastCT string
	for time.Now().Before(deadline) {
		resp, err := http.Get(baseURL + "/avatar/" + subject)
		if err != nil {
			return 0, "", err
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		lastStatus = resp.StatusCode
		lastCT = resp.Header.Get("Content-Type")
		if resp.StatusCode == http.StatusOK && strings.HasPrefix(lastCT, "image/webp") && len(body) > 0 {
			return len(body), resp.Header.Get("ETag"), nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return 0, "", fmt.Errorf("avatar never appeared for subject %s within %s (last status=%d ct=%q)",
		subject, timeout, lastStatus, lastCT)
}

// verifyFederationAuditEvents asserts credential_event has lower-bound
// counts for the federation_oidc surface this smoke exercises. Lower bounds
// only — server-side handlers may emit additional events under variants we
// don't differentiate at the wire layer.
func verifyFederationAuditEvents() error {
	dburl := os.Getenv("PROHIBITORUM_DATABASE_URL")
	rows, err := dbScalar(dburl,
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
	log.Printf("  credential_event covers federation lifecycle (counts=%v)", counts)
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
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dburl)
	if err != nil {
		return fmt.Errorf("pgx connect: %w", err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, "DELETE FROM enrollment WHERE token=$1", token); err != nil {
		return fmt.Errorf("delete prior invite: %w", err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO enrollment (
		token, intent, expires_at,
		template_username, template_display_name, template_role,
		expected_upstream_idp_slug
	) VALUES (
		$1, 'invite', now() + $2::interval,
		$3, $4, $5,
		$6
	)`, token, expiresOffset, templateUsername, templateDisplayName, templateRole, expectedSlug); err != nil {
		return fmt.Errorf("insert invite enrollment: %w", err)
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
	consumed, err := dbScalar(dburl, fmt.Sprintf(
		"SELECT (consumed_at IS NOT NULL)::text FROM enrollment WHERE token='%s'", token))
	if err != nil {
		return err
	}
	if len(consumed) != 1 || consumed[0] != "true" {
		return fmt.Errorf("enrollment.consumed_at NOT NULL: want 1 row 'true', got %v", consumed)
	}
	log.Printf("  enrollment[%s].consumed_at IS NOT NULL ✓", token)

	// 2) account row exists with the template username.
	accIDs, err := dbScalar(dburl, fmt.Sprintf(
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
	idents, err := dbScalar(dburl, fmt.Sprintf(
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
	regs, err := dbScalar(dburl, fmt.Sprintf(
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
// response is 302 to /error?error=<wantCode>. The backend now redirects
// browser-navigated error paths to the SPA /error page instead of returning
// a JSON body. Used for the invite_only negative cases (consumed/expired
// token) where BeginInviteRedemption rejects before any upstream hop.
func expectInviteStartFederationError(c *client, baseURL, token string, wantCode string) error {
	path := fmt.Sprintf("/api/prohibitorum/enrollments/%s/start-federation?return_to=/me", token)
	req, err := http.NewRequest(http.MethodGet, c.base+path, nil)
	if err != nil {
		return fmt.Errorf("start-federation: build request: %w", err)
	}
	noFollow := &http.Client{
		Jar:     c.jar,
		Timeout: 10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := noFollow.Do(req)
	if err != nil {
		return fmt.Errorf("start-federation: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusFound {
		return fmt.Errorf("/start-federation status: want 302, got %d (body=%s)", resp.StatusCode, string(body))
	}
	loc := resp.Header.Get("Location")
	wantPrefix := "/error?error=" + wantCode
	if !strings.HasPrefix(loc, wantPrefix) {
		return fmt.Errorf("/start-federation Location: want prefix %q, got %q", wantPrefix, loc)
	}
	return nil
}

// =========================================================================
// OIDC OP helpers — mock relying party
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

// groupsClaimContains reports whether a decoded JSON "groups" claim (a
// []any of strings, as json.Unmarshal produces for a JSON string array)
// contains want. Used by the RBAC arc to assert the group slug surfaces in the
// id_token and /userinfo groups claim.
func groupsClaimContains(v any, want string) bool {
	arr, ok := v.([]any)
	if !ok {
		return false
	}
	for _, e := range arr {
		if s, ok := e.(string); ok && s == want {
			return true
		}
	}
	return false
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
// --require-consent so the Login+Consent UI backend consent steps (consent 1) can drive the /consent bounce.
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

// tokenKID parses an RS256 JWS and returns the `kid` from its JOSE header,
// WITHOUT verifying the signature. Used by the signing-key-lifecycle arc to
// prove which key signed a freshly minted token.
func tokenKID(token string) (string, error) {
	parsed, err := jwt.ParseSigned(token, []jose.SignatureAlgorithm{jose.RS256})
	if err != nil {
		return "", fmt.Errorf("parse jws: %w", err)
	}
	if len(parsed.Headers) != 1 {
		return "", fmt.Errorf("unexpected JOSE header count: %d", len(parsed.Headers))
	}
	return parsed.Headers[0].KeyID, nil
}

// jwksKIDs returns the kids in a JWKS, for diagnostics.
func jwksKIDs(set *jose.JSONWebKeySet) []string {
	out := make([]string, 0, len(set.Keys))
	for _, k := range set.Keys {
		out = append(out, k.KeyID)
	}
	return out
}

// contractAuditEvent mirrors the wire shape of contract.AuditEventView for the
// audit-events viewer arc.
type contractAuditEvent struct {
	ID     int64          `json:"id"`
	Factor string         `json:"factor"`
	Event  string         `json:"event"`
	Detail map[string]any `json:"detail"`
}

// secretLeakNeedles are substrings that must NEVER appear in a wire-safe admin
// read response (signing-key list, oidc-client list/get). A hit means a secret-
// bearing field (private key PEM, client secret/hash) leaked through the view.
var secretLeakNeedles = []string{
	"private_pem", "privatePem", "privateKey", "private_key",
	"-----BEGIN", // PEM header (private OR encrypted key blocks)
	"client_secret_hash", "clientSecretHash", "secret_hash", "secretHash",
}

// assertNoSecretLeak fails the smoke if the raw response JSON contains any
// secret-bearing field name or PEM material. The create/rotate responses
// legitimately carry a top-level "secret" — those are checked separately and do
// NOT pass through this scanner.
func assertNoSecretLeak(label string, body []byte) {
	s := string(body)
	for _, needle := range secretLeakNeedles {
		if strings.Contains(s, needle) {
			log.Fatalf("%s: response leaks secret material (matched %q): %s", label, needle, s)
		}
	}
}

// publicAuditDetailKeys are detail fields whose values are PUBLIC identifiers
// (key ids, client ids, etc.) and so are legitimately opaque/base64-shaped. They
// are exempt from the base64-secret length heuristic — but the key-name and PEM
// checks still apply to every field.
var publicAuditDetailKeys = map[string]bool{
	"kid": true, "client_id": true, "slug": true, "sp_id": true,
	"entity_id": true, "status": true, "action": true, "mode": true,
	"public": true, "reason": true,
}

// assertAuditDetailNoSecret spot-checks an audit-event detail map for obvious
// secret material: no key named like a secret, no PEM block in any value, and no
// long opaque base64 value in a field that is NOT a known public identifier.
func assertAuditDetailNoSecret(factor, event string, detail map[string]any) {
	for k, v := range detail {
		lk := strings.ToLower(k)
		if strings.Contains(lk, "private") || strings.Contains(lk, "secret") || strings.Contains(lk, "password") {
			log.Fatalf("audit detail (factor=%s event=%s) has a secret-like key %q: %v", factor, event, k, detail)
		}
		s, ok := v.(string)
		if !ok {
			continue
		}
		if strings.Contains(s, "-----BEGIN") {
			log.Fatalf("audit detail (factor=%s event=%s) value for %q contains a PEM block", factor, event, k)
		}
		// A long all-base64url value in a NON-public field is a redaction smell.
		// Public identifiers (kid, client_id, …) are legitimately base64-shaped.
		if !publicAuditDetailKeys[lk] && len(s) >= 32 && isLikelyBase64Secret(s) {
			log.Fatalf("audit detail (factor=%s event=%s) value for %q looks like a raw secret: %q", factor, event, k, s)
		}
	}
}

// isLikelyBase64Secret reports whether s is composed only of base64url/base64
// alphabet characters (a heuristic for an unredacted secret/token).
func isLikelyBase64Secret(s string) bool {
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z':
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '+' || r == '/' || r == '-' || r == '_' || r == '=':
		default:
			return false
		}
	}
	return true
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
// hardening helpers — forced re-auth, public-client token/introspect/revoke,
// SAML status-Response decode, and the re-auth audit assert.
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

// verifyHardeningSAMLAuditEvents asserts credential_event picked up the re-auth SAML
// surface: the ForceAuthn re-auth assertion + POST-binding assertion add to the
// use/sso count, and the IdP-initiated SSO emits use with reason=idp_initiated.
func verifyHardeningSAMLAuditEvents() error {
	dburl := os.Getenv("PROHIBITORUM_DATABASE_URL")
	if dburl == "" {
		return errors.New("PROHIBITORUM_DATABASE_URL not set")
	}
	rows, err := dbScalar(dburl,
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
	// idp_initiated reason must be present (hardening 11). sso reason count grew vs
	// the saml baseline (ForceAuthn retry + POST-binding) — lower
	// bound 5 keeps us safe (the saml arc alone already asserts >=3).
	if counts["use:idp_initiated"] < 1 {
		return fmt.Errorf("credential_event saml_sp use:idp_initiated: want >=1, got %d (full counts=%v)",
			counts["use:idp_initiated"], counts)
	}
	if counts["use:sso"] < 5 {
		return fmt.Errorf("credential_event saml_sp use:sso: want >=5 after the re-auth arc, got %d (full counts=%v)",
			counts["use:sso"], counts)
	}
	log.Printf("  credential_event covers SAML re-auth lifecycle (counts=%v)", counts)
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
	loc := resp.Header.Get("Location")
	// The critical security property: never redirect to the untrusted URI.
	if strings.Contains(loc, badRedirectURI) {
		return fmt.Errorf("response Location leaks the bad redirect_uri: %q", loc)
	}
	// The backend now 302-redirects to the SPA /error page (same-origin, so
	// no open-redirect risk) rather than returning a raw 400 JSON body.
	if resp.StatusCode == http.StatusFound {
		if !strings.HasPrefix(loc, "/error?error=invalid_request") {
			return fmt.Errorf("bad redirect_uri: 302 Location want /error?error=invalid_request prefix, got %q", loc)
		}
		return nil
	}
	if resp.StatusCode != http.StatusBadRequest {
		return fmt.Errorf("status: want 302 or 400, got %d (body=%s)", resp.StatusCode, body)
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
	rows, err := dbScalar(dburl, fmt.Sprintf(
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

// verifyOIDCAuditEvents asserts credential_event has lower-bound counts for
// the oidc_client factor across this smoke run. The audit `event` column holds
// the abstract verb (use/fail/revoke); the concrete reason lives in
// detail->>'reason'. We assert on (event, reason) pairs so a regression that
// drops a specific audit (e.g. refresh_reuse) is caught.
//
// Expected reasons emitted by the handlers:
//   - use/authorize        — every /oauth/authorize success (≥5: steps 72,79,80,82,83 + reruns)
//   - use/token_issued     — every authorization_code grant (≥4: steps 73,79,80 + the negatives consume a code but the bad-secret/PKCE-mismatch ones FAIL before token_issued)
//   - use/refresh_rotated  — the successful refresh (oidc 7) ≥1
//   - use/logout           — RP-initiated logout (oidc 15) ≥1
//   - fail/refresh_reuse   — the reuse replay (oidc 8) ≥1
//   - revoke/revoked       — /oauth/revoke (steps 79 refresh + 80 access) ≥2
func verifyOIDCAuditEvents() error {
	dburl := os.Getenv("PROHIBITORUM_DATABASE_URL")
	if dburl == "" {
		return errors.New("PROHIBITORUM_DATABASE_URL not set")
	}
	rows, err := dbScalar(dburl,
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
	log.Printf("  credential_event covers OIDC OP lifecycle (counts=%v)", counts)
	return nil
}
