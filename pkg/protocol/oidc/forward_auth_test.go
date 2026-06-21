package oidc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/kv"
)

func TestForwardAuth_PKCE_S256(t *testing.T) {
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	want := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	if got := pkceChallengeS256(verifier); got != want {
		t.Fatalf("pkceChallengeS256 = %q, want %q", got, want)
	}
	if !verifyPKCE(verifier, want) {
		t.Fatal("verifyPKCE should accept the matching verifier")
	}
	if verifyPKCE("wrong", want) {
		t.Fatal("verifyPKCE should reject a wrong verifier")
	}
}

func TestForwardAuth_Session_RoundTrip(t *testing.T) {
	ctx := context.Background()
	store := kv.NewMemoryStore()
	tok, err := mintFASession(ctx, store, faSession{AccountID: 42, ClientID: "svc"}, time.Hour)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	got := loadFASession(ctx, store, tok)
	if got == nil || got.AccountID != 42 || got.ClientID != "svc" {
		t.Fatalf("load = %+v", got)
	}
	if loadFASession(ctx, store, "nonexistent") != nil {
		t.Fatal("missing token should load nil")
	}
}

func TestForwardAuth_State_SingleUse(t *testing.T) {
	ctx := context.Background()
	store := kv.NewMemoryStore()
	id, err := mintFAState(ctx, store, faState{OriginalURL: "https://app.acme.io/foo", ClientID: "svc", Verifier: "v"}, 5*time.Minute)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	st := popFAState(ctx, store, id)
	if st == nil || st.OriginalURL != "https://app.acme.io/foo" {
		t.Fatalf("pop = %+v", st)
	}
	if popFAState(ctx, store, id) != nil {
		t.Fatal("state must be single-use")
	}
}

func TestForwardAuth_Cookie_HostOnly(t *testing.T) {
	c := faCookie(true, "tok")
	if c.Name != "__Host-"+forwardAuthCookieBase || !c.Secure || !c.HttpOnly || c.Path != "/" || c.Domain != "" {
		t.Fatalf("secure cookie wrong: %+v", c)
	}
	if c2 := faCookie(false, "tok"); c2.Name != forwardAuthCookieBase || c2.Secure {
		t.Fatalf("insecure cookie wrong: %+v", c2)
	}
}

func TestForwardAuth_IdentityHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	writeIdentityHeaders(rec, "alice", "Alice A", "alice@example.com", []string{"admins", "staff"})
	h := rec.Header()
	if h.Get("Remote-User") != "alice" || h.Get("Remote-Name") != "Alice A" ||
		h.Get("Remote-Email") != "alice@example.com" || h.Get("Remote-Groups") != "admins,staff" {
		t.Fatalf("headers: %v", h)
	}
	rec2 := httptest.NewRecorder()
	writeIdentityHeaders(rec2, "bob", "Bob", "", nil)
	if _, ok := rec2.Header()["Remote-Email"]; ok {
		t.Fatal("empty email must be omitted")
	}
}

// ---------------------------------------------------------------------------
// Fake querier for HandleForwardAuthVerify tests
// ---------------------------------------------------------------------------

// fakeFAQueries embeds db.Querier (panics on unimplemented) and overrides only
// the methods HandleForwardAuthVerify calls.
type fakeFAQueries struct {
	db.Querier
	// faClient is returned for any host lookup when faClientErr is nil.
	faClient    db.GetForwardAuthClientByHostRow
	faClientErr error
	// knownHost, when set, restricts GetForwardAuthClientByHost to that host.
	knownHost string
	// authorized controls IsAccountAuthorizedForOIDCClient.
	authorized bool
	authzErr   error
	// acct is returned by GetAccountByID when acctErr is nil.
	acct    db.Account
	acctErr error
	// groups is returned by ListExposedGroupSlugsByAccount.
	groups []string
	// Captured params for RegisterForwardAuthApp tests.
	insertParams   *db.InsertOIDCClientParams
	faConfigParams *db.SetForwardAuthConfigParams
}

func (f *fakeFAQueries) GetForwardAuthClientByHost(_ context.Context, host pgtype.Text) (db.GetForwardAuthClientByHostRow, error) {
	if f.faClientErr != nil {
		return db.GetForwardAuthClientByHostRow{}, f.faClientErr
	}
	if f.knownHost != "" && host.String != f.knownHost {
		return db.GetForwardAuthClientByHostRow{}, pgx.ErrNoRows
	}
	return f.faClient, nil
}

func (f *fakeFAQueries) IsAccountAuthorizedForOIDCClient(_ context.Context, _ db.IsAccountAuthorizedForOIDCClientParams) (pgtype.Bool, error) {
	if f.authzErr != nil {
		return pgtype.Bool{}, f.authzErr
	}
	return pgtype.Bool{Bool: f.authorized, Valid: true}, nil
}

func (f *fakeFAQueries) GetAccountByID(_ context.Context, _ int32) (db.Account, error) {
	if f.acctErr != nil {
		return db.Account{}, f.acctErr
	}
	return f.acct, nil
}

func (f *fakeFAQueries) ListExposedGroupSlugsByAccount(_ context.Context, _ int32) ([]string, error) {
	return f.groups, nil
}

// Capture fields for RegisterForwardAuthApp tests.
func (f *fakeFAQueries) InsertOIDCClient(_ context.Context, p db.InsertOIDCClientParams) (db.OidcClient, error) {
	f.insertParams = &p
	return db.OidcClient{ClientID: p.ClientID, DisplayName: p.DisplayName}, nil
}

func (f *fakeFAQueries) SetForwardAuthConfig(_ context.Context, p db.SetForwardAuthConfigParams) error {
	f.faConfigParams = &p
	return nil
}

// newFAProvider builds a Provider for forward-auth tests backed by the given
// querier and a fresh memory KV, returning both so tests can pre-seed the KV.
func newFAProvider(q db.Querier) (*Provider, kv.Store) {
	store := kv.NewMemoryStore()
	p := &Provider{
		cfg:     &configx.Config{OIDC: configx.OIDCConfig{Issuer: testIssuer}},
		queries: q,
		kv:      store,
		audit:   &recordingAudit{},
		rl:      authn.NewRateLimiter(),
	}
	return p, store
}

// faRequest builds a ForwardAuth request with the supplied X-Forwarded-* headers
// and an optional cookie.
func faRequest(proto, host, uri string, cookie *http.Cookie) *http.Request {
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Forwarded-Proto", proto)
	req.Header.Set("X-Forwarded-Host", host)
	req.Header.Set("X-Forwarded-Uri", uri)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	return req
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestForwardAuthVerify_UnknownHost_403(t *testing.T) {
	q := &fakeFAQueries{faClientErr: pgx.ErrNoRows}
	p, _ := newFAProvider(q)

	rec := httptest.NewRecorder()
	p.HandleForwardAuthVerify(rec, faRequest("https", "nope.example", "/", nil))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d", rec.Code)
	}
}

func TestForwardAuthVerify_NoCookie_RedirectsToAuthorize(t *testing.T) {
	q := &fakeFAQueries{
		faClient: db.GetForwardAuthClientByHostRow{ClientID: "svc", Disabled: false},
	}
	p, _ := newFAProvider(q)

	rec := httptest.NewRecorder()
	p.HandleForwardAuthVerify(rec, faRequest("https", "app.acme.io", "/dashboard", nil))

	if rec.Code != http.StatusFound {
		t.Fatalf("want 302, got %d", rec.Code)
	}
	loc := rec.Result().Header.Get("Location")
	if !strings.Contains(loc, "/oauth/authorize?") {
		t.Fatalf("Location should contain /oauth/authorize, got %q", loc)
	}
	parsed, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("invalid Location URL: %v", err)
	}
	qs := parsed.Query()
	if qs.Get("client_id") != "svc" {
		t.Errorf("client_id: want svc, got %q", qs.Get("client_id"))
	}
	wantRedirect := "https://app.acme.io" + ForwardAuthPathPrefix + "/callback"
	if qs.Get("redirect_uri") != wantRedirect {
		t.Errorf("redirect_uri: want %q, got %q", wantRedirect, qs.Get("redirect_uri"))
	}
	if qs.Get("response_type") != "code" {
		t.Errorf("response_type: want code, got %q", qs.Get("response_type"))
	}
	if qs.Get("scope") != "openid email groups" {
		t.Errorf("scope: want 'openid email groups', got %q", qs.Get("scope"))
	}
	if qs.Get("code_challenge_method") != "S256" {
		t.Errorf("code_challenge_method: want S256, got %q", qs.Get("code_challenge_method"))
	}
	if qs.Get("code_challenge") == "" {
		t.Error("code_challenge must be non-empty")
	}
	if qs.Get("state") == "" {
		t.Error("state must be non-empty")
	}
}

func TestForwardAuthVerify_ValidCookie_200WithHeaders(t *testing.T) {
	ctx := context.Background()
	q := &fakeFAQueries{
		faClient:   db.GetForwardAuthClientByHostRow{ClientID: "svc", Disabled: false},
		authorized: true,
		acct: db.Account{
			ID:          42,
			Username:    "alice",
			DisplayName: "Alice",
			Email:       pgtype.Text{String: "alice@x", Valid: true},
		},
		groups: []string{"admins"},
	}
	p, store := newFAProvider(q)

	// Pre-seed a forward-auth session in the provider's KV store.
	token, err := mintFASession(ctx, store, faSession{AccountID: 42, ClientID: "svc"}, time.Hour)
	if err != nil {
		t.Fatalf("mintFASession: %v", err)
	}
	cookie := faCookie(true, token)

	rec := httptest.NewRecorder()
	p.HandleForwardAuthVerify(rec, faRequest("https", "app.acme.io", "/dashboard", cookie))

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	h := rec.Header()
	if h.Get("Remote-User") != "alice" {
		t.Errorf("Remote-User: want alice, got %q", h.Get("Remote-User"))
	}
	if h.Get("Remote-Groups") != "admins" {
		t.Errorf("Remote-Groups: want admins, got %q", h.Get("Remote-Groups"))
	}
}

func TestForwardAuthVerify_RevokedAccess_Redirects(t *testing.T) {
	ctx := context.Background()
	q := &fakeFAQueries{
		faClient:   db.GetForwardAuthClientByHostRow{ClientID: "svc", Disabled: false},
		authorized: false, // access revoked
		acct: db.Account{
			ID:       42,
			Username: "alice",
		},
	}
	p, store := newFAProvider(q)

	token, err := mintFASession(ctx, store, faSession{AccountID: 42, ClientID: "svc"}, time.Hour)
	if err != nil {
		t.Fatalf("mintFASession: %v", err)
	}
	cookie := faCookie(true, token)

	rec := httptest.NewRecorder()
	p.HandleForwardAuthVerify(rec, faRequest("https", "app.acme.io", "/dashboard", cookie))

	if rec.Code != http.StatusFound {
		t.Fatalf("want 302, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// HandleForwardAuthCallback tests
// ---------------------------------------------------------------------------

// newFACallbackRequest builds a GET request to the callback URL on the protected
// domain with X-Forwarded-* headers set.
func newFACallbackRequest(proto, host, code, stateID string) *http.Request {
	u := proto + "://" + host + ForwardAuthPathPrefix + "/callback?code=" + code + "&state=" + stateID
	req := httptest.NewRequest(http.MethodGet, u, nil)
	req.Header.Set("X-Forwarded-Host", host)
	req.Header.Set("X-Forwarded-Proto", proto)
	return req
}

// hasFACookie returns the value of the forward-auth cookie (with or without
// __Host- prefix) from rec, or "" if absent.
func hasFACookie(rec *httptest.ResponseRecorder, secure bool) string {
	name := faCookieName(secure)
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c.Value
		}
	}
	return ""
}

func TestForwardAuthCallback_Success(t *testing.T) {
	ctx := context.Background()
	p, store := newFAProvider(&fakeFAQueries{})
	p.cfg.ForwardAuth.SessionTTL = time.Hour

	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	stateID, err := mintFAState(ctx, store, faState{
		OriginalURL: "https://app.acme.io/foo",
		ClientID:    "svc",
		Verifier:    verifier,
	}, 5*time.Minute)
	if err != nil {
		t.Fatalf("mintFAState: %v", err)
	}
	code, err := mintCode(ctx, store, authCode{
		ClientID:            "svc",
		AccountID:           42,
		RedirectURI:         "https://app.acme.io" + ForwardAuthPathPrefix + "/callback",
		CodeChallenge:       pkceChallengeS256(verifier),
		CodeChallengeMethod: "S256",
	}, 5*time.Minute)
	if err != nil {
		t.Fatalf("mintCode: %v", err)
	}

	req := newFACallbackRequest("https", "app.acme.io", code, stateID)
	rec := httptest.NewRecorder()
	p.HandleForwardAuthCallback(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status=%d want 302; body=%s", rec.Code, rec.Body.String())
	}
	loc, _ := rec.Result().Location()
	if loc == nil || loc.String() != "https://app.acme.io/foo" {
		t.Fatalf("location=%v", loc)
	}
	val := hasFACookie(rec, true)
	if val == "" {
		t.Fatal("forward-auth cookie not set")
	}
	sess := loadFASession(ctx, store, val)
	if sess == nil || sess.AccountID != 42 || sess.ClientID != "svc" {
		t.Fatalf("fa-session = %+v", sess)
	}
}

func TestForwardAuthCallback_PKCEMismatch_Rejected(t *testing.T) {
	ctx := context.Background()
	p, store := newFAProvider(&fakeFAQueries{})
	p.cfg.ForwardAuth.SessionTTL = time.Hour

	// state.Verifier="wrong" but code carries challenge for "right"
	stateID, err := mintFAState(ctx, store, faState{
		OriginalURL: "https://app.acme.io/foo",
		ClientID:    "svc",
		Verifier:    "wrong-verifier",
	}, 5*time.Minute)
	if err != nil {
		t.Fatalf("mintFAState: %v", err)
	}
	code, err := mintCode(ctx, store, authCode{
		ClientID:            "svc",
		AccountID:           42,
		RedirectURI:         "https://app.acme.io" + ForwardAuthPathPrefix + "/callback",
		CodeChallenge:       pkceChallengeS256("right-verifier"),
		CodeChallengeMethod: "S256",
	}, 5*time.Minute)
	if err != nil {
		t.Fatalf("mintCode: %v", err)
	}

	req := newFACallbackRequest("https", "app.acme.io", code, stateID)
	rec := httptest.NewRecorder()
	p.HandleForwardAuthCallback(rec, req)

	// Should redirect to error page, not OriginalURL
	loc, _ := rec.Result().Location()
	if loc != nil && loc.String() == "https://app.acme.io/foo" {
		t.Fatal("PKCE mismatch must NOT redirect to OriginalURL")
	}
	if hasFACookie(rec, true) != "" {
		t.Fatal("PKCE mismatch must not set fa cookie")
	}
}

func TestForwardAuthCallback_ClientMismatch_Rejected(t *testing.T) {
	ctx := context.Background()
	p, store := newFAProvider(&fakeFAQueries{})
	p.cfg.ForwardAuth.SessionTTL = time.Hour

	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	// state says client "svc" but code carries "other-client"
	stateID, err := mintFAState(ctx, store, faState{
		OriginalURL: "https://app.acme.io/foo",
		ClientID:    "svc",
		Verifier:    verifier,
	}, 5*time.Minute)
	if err != nil {
		t.Fatalf("mintFAState: %v", err)
	}
	code, err := mintCode(ctx, store, authCode{
		ClientID:            "other-client",
		AccountID:           42,
		RedirectURI:         "https://app.acme.io" + ForwardAuthPathPrefix + "/callback",
		CodeChallenge:       pkceChallengeS256(verifier),
		CodeChallengeMethod: "S256",
	}, 5*time.Minute)
	if err != nil {
		t.Fatalf("mintCode: %v", err)
	}

	req := newFACallbackRequest("https", "app.acme.io", code, stateID)
	rec := httptest.NewRecorder()
	p.HandleForwardAuthCallback(rec, req)

	loc, _ := rec.Result().Location()
	if loc != nil && loc.String() == "https://app.acme.io/foo" {
		t.Fatal("client mismatch must NOT redirect to OriginalURL")
	}
	if hasFACookie(rec, true) != "" {
		t.Fatal("client mismatch must not set fa cookie")
	}
}

func TestForwardAuthCallback_RedirectURIMismatch_Rejected(t *testing.T) {
	ctx := context.Background()
	p, store := newFAProvider(&fakeFAQueries{})
	p.cfg.ForwardAuth.SessionTTL = time.Hour

	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	stateID, err := mintFAState(ctx, store, faState{
		OriginalURL: "https://app.acme.io/foo",
		ClientID:    "svc",
		Verifier:    verifier,
	}, 5*time.Minute)
	if err != nil {
		t.Fatalf("mintFAState: %v", err)
	}
	// RedirectURI points to a different host
	code, err := mintCode(ctx, store, authCode{
		ClientID:            "svc",
		AccountID:           42,
		RedirectURI:         "https://evil.example.com" + ForwardAuthPathPrefix + "/callback",
		CodeChallenge:       pkceChallengeS256(verifier),
		CodeChallengeMethod: "S256",
	}, 5*time.Minute)
	if err != nil {
		t.Fatalf("mintCode: %v", err)
	}

	req := newFACallbackRequest("https", "app.acme.io", code, stateID)
	rec := httptest.NewRecorder()
	p.HandleForwardAuthCallback(rec, req)

	loc, _ := rec.Result().Location()
	if loc != nil && loc.String() == "https://app.acme.io/foo" {
		t.Fatal("redirect_uri mismatch must NOT redirect to OriginalURL")
	}
	if hasFACookie(rec, true) != "" {
		t.Fatal("redirect_uri mismatch must not set fa cookie")
	}
}

func TestForwardAuthCallback_UsedState_Rejected(t *testing.T) {
	ctx := context.Background()
	p, store := newFAProvider(&fakeFAQueries{})
	p.cfg.ForwardAuth.SessionTTL = time.Hour

	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	stateID, err := mintFAState(ctx, store, faState{
		OriginalURL: "https://app.acme.io/foo",
		ClientID:    "svc",
		Verifier:    verifier,
	}, 5*time.Minute)
	if err != nil {
		t.Fatalf("mintFAState: %v", err)
	}
	// Consume the state once (simulating a completed or replayed flow)
	popFAState(ctx, store, stateID)

	code, err := mintCode(ctx, store, authCode{
		ClientID:            "svc",
		AccountID:           42,
		RedirectURI:         "https://app.acme.io" + ForwardAuthPathPrefix + "/callback",
		CodeChallenge:       pkceChallengeS256(verifier),
		CodeChallengeMethod: "S256",
	}, 5*time.Minute)
	if err != nil {
		t.Fatalf("mintCode: %v", err)
	}

	req := newFACallbackRequest("https", "app.acme.io", code, stateID)
	rec := httptest.NewRecorder()
	p.HandleForwardAuthCallback(rec, req)

	loc, _ := rec.Result().Location()
	if loc != nil && loc.String() == "https://app.acme.io/foo" {
		t.Fatal("used state must NOT redirect to OriginalURL")
	}
	if hasFACookie(rec, true) != "" {
		t.Fatal("used state must not set fa cookie")
	}
}

func TestForwardAuthCallback_UsedCode_Rejected(t *testing.T) {
	ctx := context.Background()
	p, store := newFAProvider(&fakeFAQueries{})
	p.cfg.ForwardAuth.SessionTTL = time.Hour

	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"

	mintAndConsume := func() (string, string) {
		stateID, err := mintFAState(ctx, store, faState{
			OriginalURL: "https://app.acme.io/foo",
			ClientID:    "svc",
			Verifier:    verifier,
		}, 5*time.Minute)
		if err != nil {
			t.Fatalf("mintFAState: %v", err)
		}
		code, err := mintCode(ctx, store, authCode{
			ClientID:            "svc",
			AccountID:           42,
			RedirectURI:         "https://app.acme.io" + ForwardAuthPathPrefix + "/callback",
			CodeChallenge:       pkceChallengeS256(verifier),
			CodeChallengeMethod: "S256",
		}, 5*time.Minute)
		if err != nil {
			t.Fatalf("mintCode: %v", err)
		}
		return stateID, code
	}

	// First call: succeeds and consumes both state + code
	stateID1, code1 := mintAndConsume()
	rec1 := httptest.NewRecorder()
	p.HandleForwardAuthCallback(rec1, newFACallbackRequest("https", "app.acme.io", code1, stateID1))
	if rec1.Code != http.StatusFound {
		t.Fatalf("first call should succeed, got %d", rec1.Code)
	}

	// Second call: same code, fresh state — consumeCode must reject the used code
	stateID2, _ := mintFAState(ctx, store, faState{
		OriginalURL: "https://app.acme.io/foo",
		ClientID:    "svc",
		Verifier:    verifier,
	}, 5*time.Minute)
	rec2 := httptest.NewRecorder()
	p.HandleForwardAuthCallback(rec2, newFACallbackRequest("https", "app.acme.io", code1, stateID2))

	loc, _ := rec2.Result().Location()
	if loc != nil && loc.String() == "https://app.acme.io/foo" {
		t.Fatal("used code must NOT redirect to OriginalURL")
	}
	if hasFACookie(rec2, true) != "" {
		t.Fatal("used code must not set fa cookie")
	}
}

func TestForwardAuthCallback_MissingCode_Rejected(t *testing.T) {
	ctx := context.Background()
	p, store := newFAProvider(&fakeFAQueries{})
	p.cfg.ForwardAuth.SessionTTL = time.Hour

	stateID, err := mintFAState(ctx, store, faState{
		OriginalURL: "https://app.acme.io/foo",
		ClientID:    "svc",
		Verifier:    "v",
	}, 5*time.Minute)
	if err != nil {
		t.Fatalf("mintFAState: %v", err)
	}

	// No code parameter
	req := newFACallbackRequest("https", "app.acme.io", "", stateID)
	rec := httptest.NewRecorder()
	p.HandleForwardAuthCallback(rec, req)

	loc, _ := rec.Result().Location()
	if loc != nil && loc.String() == "https://app.acme.io/foo" {
		t.Fatal("missing code must NOT redirect to OriginalURL")
	}
	if hasFACookie(rec, true) != "" {
		t.Fatal("missing code must not set fa cookie")
	}
}

func TestForwardAuthCallback_MissingState_Rejected(t *testing.T) {
	ctx := context.Background()
	p, store := newFAProvider(&fakeFAQueries{})
	p.cfg.ForwardAuth.SessionTTL = time.Hour

	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	code, err := mintCode(ctx, store, authCode{
		ClientID:            "svc",
		AccountID:           42,
		RedirectURI:         "https://app.acme.io" + ForwardAuthPathPrefix + "/callback",
		CodeChallenge:       pkceChallengeS256(verifier),
		CodeChallengeMethod: "S256",
	}, 5*time.Minute)
	if err != nil {
		t.Fatalf("mintCode: %v", err)
	}

	// No state parameter
	req := newFACallbackRequest("https", "app.acme.io", code, "")
	rec := httptest.NewRecorder()
	p.HandleForwardAuthCallback(rec, req)

	loc, _ := rec.Result().Location()
	if loc != nil && loc.String() == "https://app.acme.io/foo" {
		t.Fatal("missing state must NOT redirect to OriginalURL")
	}
	if hasFACookie(rec, true) != "" {
		t.Fatal("missing state must not set fa cookie")
	}
}

func TestRegisterForwardAuthApp_BuildsPublicPKCEClient(t *testing.T) {
	f := &fakeFAQueries{}
	_, err := RegisterForwardAuthApp(context.Background(), f, "fa-client", "app.example.test", "App")
	if err != nil {
		t.Fatalf("RegisterForwardAuthApp: %v", err)
	}
	if f.insertParams == nil || f.faConfigParams == nil {
		t.Fatal("expected InsertOIDCClient and SetForwardAuthConfig to be called")
	}
	if got := f.insertParams.TokenEndpointAuthMethod; got != "none" {
		t.Errorf("token_endpoint_auth_method = %q, want \"none\" (public)", got)
	}
	if f.insertParams.RequireConsent {
		t.Error("require_consent must be false")
	}
	wantURI := "https://app.example.test/.prohibitorum-forward-auth/callback"
	if len(f.insertParams.RedirectUris) != 1 || f.insertParams.RedirectUris[0] != wantURI {
		t.Errorf("redirect_uris = %v, want [%q]", f.insertParams.RedirectUris, wantURI)
	}
	if !f.faConfigParams.ForwardAuthEnabled {
		t.Error("forward_auth_enabled must be true")
	}
	if f.faConfigParams.ForwardAuthHost.String != "app.example.test" {
		t.Errorf("forward_auth_host = %q", f.faConfigParams.ForwardAuthHost.String)
	}
	wantScopes := []string{"openid", "email", "groups"}
	if !slices.Equal(f.insertParams.AllowedScopes, wantScopes) {
		t.Errorf("allowed_scopes = %v, want %v", f.insertParams.AllowedScopes, wantScopes)
	}
}

// ---------------------------------------------------------------------------
// HandleForwardAuthSignOut + ValidatedForwardAuthReturnURL tests
// ---------------------------------------------------------------------------

func TestHandleForwardAuthSignOut_ClearsSessionAndRedirects(t *testing.T) {
	p, store := newFAProvider(&fakeFAQueries{})
	// Seed a per-domain session keyed by the cookie token.
	const tok = "tok-123"
	if err := store.SetEx(context.Background(), faSessionKey(tok), `{"account_id":1,"client_id":"fa"}`, time.Hour); err != nil {
		t.Fatalf("seed: %v", err)
	}
	req := faRequest("https", "app.example.test", "/", &http.Cookie{Name: faCookieName(true), Value: tok})
	rec := httptest.NewRecorder()
	p.HandleForwardAuthSignOut(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, testIssuer+"/api/prohibitorum/forward-auth/sso-logout?") {
		t.Errorf("Location = %q, want sso-logout on issuer", loc)
	}
	if !strings.Contains(loc, "rd=https%3A%2F%2Fapp.example.test%2F") {
		t.Errorf("Location missing rd=app host: %q", loc)
	}
	// KV session is gone.
	if v, _ := store.Get(context.Background(), faSessionKey(tok)); v != "" {
		t.Error("expected fa:session to be deleted")
	}
	// Cookie cleared (MaxAge<0).
	cleared := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == faCookieName(true) && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Error("expected cleared forward-auth cookie")
	}
}

func TestValidatedForwardAuthReturnURL(t *testing.T) {
	q := &fakeFAQueries{knownHost: "app.example.test"}
	cases := []struct {
		name string
		rd   string
		want bool
	}{
		{"registered host", "https://app.example.test/foo", true},
		{"unregistered host", "https://evil.example.com/", false},
		{"empty", "", false},
		{"non-http scheme", "javascript:alert(1)", false},
		{"garbage", "://nope", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := ValidatedForwardAuthReturnURL(context.Background(), q, c.rd)
			if ok != c.want {
				t.Fatalf("ok = %v, want %v (rd=%q)", ok, c.want, c.rd)
			}
			if ok && got != c.rd {
				t.Errorf("dest = %q, want %q", got, c.rd)
			}
		})
	}
}
