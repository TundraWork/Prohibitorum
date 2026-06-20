package oidc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	// authorized controls IsAccountAuthorizedForOIDCClient.
	authorized bool
	authzErr   error
	// acct is returned by GetAccountByID when acctErr is nil.
	acct    db.Account
	acctErr error
	// groups is returned by ListExposedGroupSlugsByAccount.
	groups []string
}

func (f *fakeFAQueries) GetForwardAuthClientByHost(_ context.Context, _ pgtype.Text) (db.GetForwardAuthClientByHostRow, error) {
	if f.faClientErr != nil {
		return db.GetForwardAuthClientByHostRow{}, f.faClientErr
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
