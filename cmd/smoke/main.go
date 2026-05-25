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
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/fxamacker/cbor/v2"

	totppkg "prohibitorum/pkg/credential/totp"
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
		log.Fatalf("logout pre-recovery-login: %v", err)
	}

	step("step 29/45 — POST /auth/password/begin (fresh partial token for recovery-code login)")
	partialToken2, err := c.passwordBegin(*username, password)
	if err != nil {
		log.Fatalf("password/begin 2: %v", err)
	}
	log.Printf("  partial_session_token len=%d", len(partialToken2))

	step("step 30/45 — POST /auth/recovery-code/verify {recovery_codes[0]}")
	if err := c.recoveryCodeVerify(partialToken2, recoveryCodes[0]); err != nil {
		log.Fatalf("auth/recovery-code/verify: %v", err)
	}
	log.Printf("  session cookie issued via password+recovery_code")

	step("step 31/45 — DB assert: recovery_codes[0].used_at IS NOT NULL")
	if err := verifyRecoveryCodeUsed(me2.ID, 1, 0); err != nil {
		log.Fatalf("recovery code used_at DB assert: %v", err)
	}

	step("step 32/45 — POST /auth/logout (drop recovery-login session)")
	if err := c.logout(); err != nil {
		log.Fatalf("logout pre-webauthn-relogin: %v", err)
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

	step("step 39/45 — sudo via recovery_code (/me/sudo/begin + /me/sudo/complete)")
	if err := sudoRecoveryCode(c, recoveryCodes[0]); err != nil {
		log.Fatalf("sudo recovery_code: %v", err)
	}
	log.Printf("  sudo grant acquired (recovery_code)")

	step("step 40/45 — DB assert: at least one used recovery code (consumed by sudo)")
	if err := verifyRecoveryCodeUsed(me2.ID, 1, -1); err != nil {
		log.Fatalf("post-sudo recovery DB assert: %v", err)
	}

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

	fmt.Println()
	fmt.Println("✓ smoke OK — 45/45 + DB-state assertions passed against",
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

func (c *client) recoveryCodeVerify(partialToken, code string) error {
	return c.postJSON("/api/prohibitorum/auth/recovery-code/verify",
		map[string]string{"partial_session_token": partialToken, "code": code}, nil)
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

func sudoRecoveryCode(c *client, code string) error {
	if err := c.postJSON("/api/prohibitorum/me/sudo/begin",
		map[string]string{"method": "recovery_code"}, nil); err != nil {
		return fmt.Errorf("sudo/begin recovery_code: %w", err)
	}
	return c.postJSON("/api/prohibitorum/me/sudo/complete",
		map[string]string{"recovery_code": code}, nil)
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
		{"totp:register", 1},
		{"totp:use", 1},
		{"totp:revoke", 1},
		{"recovery_code:register", 10}, // initial 10 + 10 regenerated = 20, but lower bound 10
		{"recovery_code:use", 2},       // login + sudo
		{"recovery_code:revoke", 1},
		{"session:sudo_granted", 3}, // pre-pwd-set, pwd_totp, recovery, webauthn-pre-revoke — >=3
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
