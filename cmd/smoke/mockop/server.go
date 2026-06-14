// Package mockop implements a minimal in-process OIDC OpenID Provider used by
// the smoke runner (cmd/smoke) and the upstream-OIDC RP / HTTP-handler tests.
//
// It is NOT a general-purpose OP. It exists to give downstream tests a real
// HTTP target that speaks Discovery + Authorize + Token + JWKS with ES256-
// signed ID tokens, plus a small set of test hooks for injecting claims,
// errors, and an overridden issuer (used by the mix-up rejection test in
// Task 5).
//
// Usage:
//
//	s, _ := mockop.New("")
//	ts := httptest.NewServer(s.Routes())
//	defer ts.Close()
//	s.SetBase(ts.URL) // late-bind: discovery + authorize iss + id_token iss
//	s.SetClaims("sub-1", "user@example.com", true, "user", "User One")
//	// drive /authorize and /token against ts.URL ...
//
// New accepts an empty base; the caller is required to call SetBase before
// the server is exercised so that the discovery document, /authorize iss
// query param, and id_token iss claim all reference the real listener URL.
package mockop

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"math/big"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// Claims is the snapshot of OIDC user claims the mock OP will include in the
// next minted ID token. Set via SetClaims; captured into per-code state at
// /authorize time so that concurrent /authorize calls with different claims
// snapshots do not race.
type Claims struct {
	Sub               string
	Email             string
	EmailVerified     bool
	PreferredUsername string
	Name              string
	Picture           string
}

type errorParams struct {
	Code        string
	Description string
}

type codeState struct {
	clientID        string
	redirectURI     string
	nonce           string
	codeChallenge   string
	challengeMethod string
	claims          Claims
	amr             []string
	authTime        time.Time // zero means omit auth_time from id_token
	iss             string    // captured at authorize time (base or override)
	expiresAt       time.Time
}

// Server is the mock OP. All exported methods are safe for concurrent use.
type Server struct {
	mu         sync.Mutex
	base       string
	signingKey *ecdsa.PrivateKey
	kid        string

	// Test hooks. nextClaims/nextAMR/issuerOverride persist across calls;
	// nextError is single-shot and cleared after the next /authorize.
	nextClaims     Claims
	nextAMR        []string
	nextAuthTime   time.Time // zero means omit auth_time from id_token
	nextError      *errorParams
	issuerOverride string

	// userinfoPicture, when non-empty, is returned ONLY from /userinfo (never
	// in the id_token). SetPictureUserInfoOnly sets it AND clears the id_token
	// picture so the RP must fall back to UserInfo to discover the avatar.
	userinfoPicture string

	// Per-code state, keyed by the authorization code.
	codes map[string]codeState
}

// New constructs a Server with a freshly generated ES256 (P-256) signing
// key. The base argument may be empty; callers MUST call SetBase before
// serving traffic so that the discovery document and id_token iss claim
// match the real listener URL. For tests this is typically done with:
//
//	s, _ := New("")
//	ts := httptest.NewServer(s.Routes())
//	s.SetBase(ts.URL)
func New(base string) (*Server, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	return &Server{
		base:       base,
		signingKey: key,
		kid:        "mockop-1",
		codes:      map[string]codeState{},
	}, nil
}

// Routes returns the HTTP handler that serves discovery, authorize, token,
// and jwks. The mux is constructed fresh on each call but is cheap.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", s.handleDiscovery)
	mux.HandleFunc("/authorize", s.handleAuthorize)
	mux.HandleFunc("/token", s.handleToken)
	mux.HandleFunc("/jwks", s.handleJWKS)
	mux.HandleFunc("/userinfo", s.handleUserinfo)
	mux.HandleFunc("/avatar.png", s.handleAvatarPNG)
	return mux
}

// --- Test hooks ---------------------------------------------------------

// SetBase late-binds the public base URL used by discovery, the /authorize
// iss redirect parameter, and the id_token iss claim. Callers must invoke
// this after determining the real listener URL (e.g. from httptest.Server)
// and before any client traffic reaches the server.
func (s *Server) SetBase(base string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.base = base
}

// SetClaims sets the user claims the next ID token will include. Persists
// across calls (i.e. a single SetClaims survives multiple /authorize +
// /token round trips) until replaced.
func (s *Server) SetClaims(sub, email string, emailVerified bool, preferredUsername, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextClaims = Claims{
		Sub:               sub,
		Email:             email,
		EmailVerified:     emailVerified,
		PreferredUsername: preferredUsername,
		Name:              name,
	}
}

// SetPicture sets the picture claim to include in the next ID token. Persists
// across calls until replaced. Pass "" to clear (no picture claim emitted).
// Also clears any userinfo-only picture so the two knobs don't fight.
func (s *Server) SetPicture(picture string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextClaims.Picture = picture
	s.userinfoPicture = ""
}

// SetPictureUserInfoOnly arranges for the picture claim to appear ONLY in the
// /userinfo response, never in the id_token. This forces the RP's avatar-inherit
// path down the UserInfo fallback branch. Clears the id_token picture. Pass ""
// to clear the userinfo picture too.
func (s *Server) SetPictureUserInfoOnly(picture string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextClaims.Picture = ""
	s.userinfoPicture = picture
}

// PictureURL returns the absolute URL of the small PNG this mock OP serves at
// /avatar.png. Use it as the value passed to SetPicture / SetPictureUserInfoOnly
// so the RP fetches a real image during avatar inheritance.
func (s *Server) PictureURL() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.base + "/avatar.png"
}

// SetAMR sets the amr claim added to the next ID token. Passing nil clears
// it (no amr claim emitted). Persists across calls.
func (s *Server) SetAMR(amr []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextAMR = append([]string(nil), amr...)
}

// FailWithError installs a single-shot error redirect: the next /authorize
// call will redirect to redirect_uri with error/error_description/state
// query params instead of issuing a code. Cleared after firing.
func (s *Server) FailWithError(code, description string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextError = &errorParams{Code: code, Description: description}
}

// SetAuthTime sets the auth_time value to include in the next ID token.
// Passing the zero value clears the hook (auth_time is omitted). Persists
// across calls.
func (s *Server) SetAuthTime(t time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextAuthTime = t
}

// OverrideIssuer forces the iss redirect parameter and the id_token iss
// claim to the given value (instead of the base URL). Used to fixture the
// upstream mix-up rejection test. Persists across calls until cleared by
// passing an empty string.
func (s *Server) OverrideIssuer(iss string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.issuerOverride = iss
}

// --- Handlers -----------------------------------------------------------

func (s *Server) handleDiscovery(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	base := s.base
	s.mu.Unlock()

	doc := map[string]any{
		"issuer":                                base,
		"authorization_endpoint":                base + "/authorize",
		"token_endpoint":                        base + "/token",
		"jwks_uri":                              base + "/jwks",
		"userinfo_endpoint":                     base + "/userinfo",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"ES256"},
		"scopes_supported":                      []string{"openid", "profile", "email"},
		"code_challenge_methods_supported":      []string{"S256"},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(doc)
}

func (s *Server) handleJWKS(w http.ResponseWriter, r *http.Request) {
	pub := s.signingKey.PublicKey
	jwks := map[string]any{
		"keys": []map[string]any{{
			"kty": "EC",
			"crv": "P-256",
			"use": "sig",
			"alg": "ES256",
			"kid": s.kid,
			"x":   base64URLBigInt(pub.X),
			"y":   base64URLBigInt(pub.Y),
		}},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(jwks)
}

// handleUserinfo returns the current active claims as an OIDC UserInfo document.
// The mock OP issues opaque (non-introspectable) access tokens, so this endpoint
// does not validate the Bearer token; it simply mirrors the latest SetClaims
// snapshot. The "sub" MUST equal the id_token subject (the RP's UserInfo client
// rejects a sub mismatch), which holds because the avatar-inherit fetch races
// ahead of the next SetClaims call. The "picture" field reflects whichever
// picture knob is active: SetPicture (also in id_token) or SetPictureUserInfoOnly
// (userinfo only).
func (s *Server) handleUserinfo(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	cl := s.nextClaims
	uiPic := s.userinfoPicture
	s.mu.Unlock()

	doc := map[string]any{
		"sub":                cl.Sub,
		"name":               cl.Name,
		"email":              cl.Email,
		"email_verified":     cl.EmailVerified,
		"preferred_username": cl.PreferredUsername,
	}
	switch {
	case uiPic != "":
		doc["picture"] = uiPic
	case cl.Picture != "":
		doc["picture"] = cl.Picture
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(doc)
}

// handleAvatarPNG serves a small, valid PNG so the RP's avatar-inherit fetch has
// a real image to normalize. A 16×16 solid-colour RGBA bitmap is plenty for
// pkg/avatar.Process (which re-encodes to a 512 WebP) — the exact pixels don't
// matter, only that the bytes decode as a real image.
func (s *Server) handleAvatarPNG(w http.ResponseWriter, r *http.Request) {
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			img.Set(x, y, color.RGBA{R: 0x33, G: 0x88, B: 0xcc, A: 0xff})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		http.Error(w, "encode", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	_, _ = w.Write(buf.Bytes())
}

func (s *Server) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	clientID := q.Get("client_id")
	redirectURI := q.Get("redirect_uri")
	state := q.Get("state")
	nonce := q.Get("nonce")
	codeChallenge := q.Get("code_challenge")
	challengeMethod := q.Get("code_challenge_method")

	// Basic param presence validation. This isn't a security boundary
	// (this is a test fixture, not a real OP) — but missing params would
	// produce garbage redirects, so reject them up front.
	if clientID == "" || redirectURI == "" || state == "" || nonce == "" ||
		codeChallenge == "" || challengeMethod != "S256" {
		http.Error(w, `{"error":"invalid_request"}`, http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	// Single-shot error path: if FailWithError was called, redirect with
	// error params and clear the flag.
	if s.nextError != nil {
		errCode, desc := s.nextError.Code, s.nextError.Description
		s.nextError = nil
		s.mu.Unlock()

		u, err := url.Parse(redirectURI)
		if err != nil {
			http.Error(w, `{"error":"invalid_request"}`, http.StatusBadRequest)
			return
		}
		rq := u.Query()
		rq.Set("error", errCode)
		rq.Set("error_description", desc)
		rq.Set("state", state)
		u.RawQuery = rq.Encode()
		http.Redirect(w, r, u.String(), http.StatusFound)
		return
	}

	code := genRand(16)
	iss := s.base
	if s.issuerOverride != "" {
		iss = s.issuerOverride
	}
	s.codes[code] = codeState{
		clientID:        clientID,
		redirectURI:     redirectURI,
		nonce:           nonce,
		codeChallenge:   codeChallenge,
		challengeMethod: challengeMethod,
		claims:          s.nextClaims,
		amr:             s.nextAMR,
		authTime:        s.nextAuthTime,
		iss:             iss,
		expiresAt:       time.Now().Add(5 * time.Minute),
	}
	s.mu.Unlock()

	u, err := url.Parse(redirectURI)
	if err != nil {
		http.Error(w, `{"error":"invalid_request"}`, http.StatusBadRequest)
		return
	}
	rq := u.Query()
	rq.Set("code", code)
	rq.Set("state", state)
	rq.Set("iss", iss)
	u.RawQuery = rq.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

func (s *Server) handleToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, `{"error":"invalid_request"}`, http.StatusBadRequest)
		return
	}
	code := r.Form.Get("code")
	codeVerifier := r.Form.Get("code_verifier")

	s.mu.Lock()
	st, ok := s.codes[code]
	if ok {
		delete(s.codes, code) // single-use
	}
	s.mu.Unlock()

	if !ok || time.Now().After(st.expiresAt) {
		http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
		return
	}

	// Verify PKCE. We only support S256.
	if st.challengeMethod != "S256" {
		http.Error(w, `{"error":"invalid_request"}`, http.StatusBadRequest)
		return
	}
	h := sha256.Sum256([]byte(codeVerifier))
	if base64.RawURLEncoding.EncodeToString(h[:]) != st.codeChallenge {
		http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
		return
	}

	// Mint ID token. Claims are snapshotted from codeState (captured at
	// authorize time) so concurrent /authorize calls with different
	// SetClaims values don't cross-contaminate.
	now := time.Now()
	idClaims := map[string]any{
		"iss":                st.iss,
		"sub":                st.claims.Sub,
		"aud":                st.clientID,
		"exp":                now.Add(5 * time.Minute).Unix(),
		"iat":                now.Unix(),
		"nonce":              st.nonce,
		"email":              st.claims.Email,
		"email_verified":     st.claims.EmailVerified,
		"preferred_username": st.claims.PreferredUsername,
		"name":               st.claims.Name,
	}
	if st.claims.Picture != "" {
		idClaims["picture"] = st.claims.Picture
	}
	if len(st.amr) > 0 {
		idClaims["amr"] = st.amr
	}
	if !st.authTime.IsZero() {
		idClaims["auth_time"] = st.authTime.Unix()
	}
	idToken, err := s.signES256(idClaims)
	if err != nil {
		http.Error(w, `{"error":"server_error"}`, http.StatusInternalServerError)
		return
	}

	resp := map[string]any{
		"access_token": genRand(24),
		"token_type":   "Bearer",
		"expires_in":   300,
		"id_token":     idToken,
		"scope":        "openid profile email",
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// --- Helpers ------------------------------------------------------------

// signES256 produces a JWS in compact serialization with header
// {"alg":"ES256","typ":"JWT","kid":<kid>}. The signature is r||s, each
// left-padded to 32 bytes per RFC 7518 §3.4.
func (s *Server) signES256(claims map[string]any) (string, error) {
	header := map[string]any{"alg": "ES256", "typ": "JWT", "kid": s.kid}
	h, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	c, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signingInput := base64.RawURLEncoding.EncodeToString(h) + "." +
		base64.RawURLEncoding.EncodeToString(c)
	digest := sha256.Sum256([]byte(signingInput))
	r, sv, err := ecdsa.Sign(rand.Reader, s.signingKey, digest[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 64)
	rBytes := r.Bytes()
	sBytes := sv.Bytes()
	copy(sig[32-len(rBytes):32], rBytes)
	copy(sig[64-len(sBytes):], sBytes)
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func genRand(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// base64URLBigInt encodes a big.Int's big-endian byte representation,
// left-padded to 32 bytes (P-256 coordinate size), as base64url without
// padding — the form required by RFC 7518 §6.2.1.2 for JWK EC keys.
func base64URLBigInt(n *big.Int) string {
	b := n.Bytes()
	if len(b) < 32 {
		padded := make([]byte, 32)
		copy(padded[32-len(b):], b)
		b = padded
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
