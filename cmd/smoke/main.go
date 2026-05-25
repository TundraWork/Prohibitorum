// Command smoke drives an end-to-end Prohibitorum WebAuthn ceremony against a
// running dev server using an in-process virtual authenticator. No browser
// required. Intended for v0.1.1 smoke testing per STATUS.md.
//
// Flow:
//
//  1. Run `enroll-admin` to mint a bootstrap enrollment URL.
//  2. POST /enrollments/{token}/register/begin → CreationOptions.
//  3. Build an ECDSA-P256 (ES256) virtual authenticator credential.
//  4. POST /enrollments/{token}/register/complete → session cookie.
//  5. GET /me → assert username/displayName round-trip.
//  6. POST /auth/logout.
//  7. POST /auth/login/begin → AssertionOptions.
//  8. Sign the assertion challenge with the virtual authenticator's key.
//  9. POST /auth/login/complete → new session cookie.
// 10. GET /me again → assert same account.
//
// Failure at any step prints a diagnostic and exits non-zero.
package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
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
	"strings"
	"time"

	"github.com/fxamacker/cbor/v2"
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

	step("step 1/10 — minting enrollment URL via enroll-admin")
	token, err := mintEnrollmentToken(*baseURL, *skipNew)
	if err != nil {
		log.Fatalf("enroll-admin: %v", err)
	}
	log.Printf("  token: %s…", token[:12])

	step("step 2/10 — POST /enrollments/{token}/register/begin")
	creation, err := c.beginEnrollment(token, *username, *display, "smoke-laptop")
	if err != nil {
		log.Fatalf("register/begin: %v", err)
	}
	log.Printf("  challenge len=%d rpId=%s userId len=%d", len(creation.Challenge), creation.RP.ID, len(creation.User.ID))

	step("step 3/10 — building virtual authenticator credential")
	auth, err := newAuthenticator(creation.RP.ID)
	if err != nil {
		log.Fatalf("authenticator: %v", err)
	}
	attestation, err := auth.attestCredential(creation.Challenge, creation.User.ID, *baseURL)
	if err != nil {
		log.Fatalf("attest: %v", err)
	}
	log.Printf("  credentialId len=%d cose_alg=-7 (ES256)", len(auth.credentialID))

	step("step 4/10 — POST /enrollments/{token}/register/complete")
	if err := c.completeEnrollment(token, auth, attestation); err != nil {
		log.Fatalf("register/complete: %v", err)
	}
	log.Printf("  session cookie set (have %d cookies)", len(c.cookies()))

	step("step 5/10 — GET /me")
	me1, err := c.getMe()
	if err != nil {
		log.Fatalf("get me: %v", err)
	}
	if me1.Username != *username {
		log.Fatalf("/me username mismatch: got %q want %q", me1.Username, *username)
	}
	log.Printf("  username=%s displayName=%s role=%s", me1.Username, me1.DisplayName, me1.Role)

	step("step 6/10 — POST /auth/logout")
	if err := c.logout(); err != nil {
		log.Fatalf("logout: %v", err)
	}
	if _, err := c.getMe(); err == nil {
		log.Fatalf("post-logout /me succeeded; expected 401")
	}
	log.Printf("  session revoked; /me returns 401 as expected")

	step("step 7/10 — POST /auth/login/begin")
	assertion, err := c.beginLogin()
	if err != nil {
		log.Fatalf("login/begin: %v", err)
	}
	log.Printf("  challenge len=%d rpId=%s", len(assertion.Challenge), assertion.RPID)

	step("step 8/10 — signing assertion with virtual authenticator")
	signed, err := auth.signAssertion(assertion.Challenge, *baseURL)
	if err != nil {
		log.Fatalf("sign assertion: %v", err)
	}
	log.Printf("  signature len=%d (ASN.1 DER)", len(signed.signature))

	step("step 9/10 — POST /auth/login/complete")
	if err := c.completeLogin(auth, signed); err != nil {
		log.Fatalf("login/complete: %v", err)
	}
	log.Printf("  session cookie restored")

	step("step 10/10 — GET /me (post-login)")
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

	fmt.Println()
	fmt.Println("✓ smoke OK — enrollment, /me, logout, login all round-tripped against",
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
