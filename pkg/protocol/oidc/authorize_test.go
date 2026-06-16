package oidc

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/kv"
)

const testIssuer = "https://auth.example.com"

// fakeAuthzQueries embeds db.Querier so every method exists (and panics if an
// unexpected one is called), overriding only the two the authorize handler
// touches.
type fakeAuthzQueries struct {
	db.Querier
	client     db.OidcClient
	clientErr  error
	session    db.Session
	sessionErr error
	granted    []string
	grantedErr error
	// denied inverts the per-app access predicate: zero-value (false) means the
	// account IS authorized, so every existing test keeps passing untouched. Set
	// true to exercise the RBAC denial path. authzErr forces a predicate error
	// (fail-closed → server_error).
	denied   bool
	authzErr error
}

// IsAccountAuthorizedForOIDCClient backs the RBAC per-app access gate. Default
// (denied=false, authzErr=nil) → authorized=true so pre-RBAC tests are
// unaffected.
func (f *fakeAuthzQueries) IsAccountAuthorizedForOIDCClient(_ context.Context, _ db.IsAccountAuthorizedForOIDCClientParams) (pgtype.Bool, error) {
	if f.authzErr != nil {
		return pgtype.Bool{}, f.authzErr
	}
	return pgtype.Bool{Bool: !f.denied, Valid: true}, nil
}

func (f *fakeAuthzQueries) GetOIDCClient(ctx context.Context, id string) (db.OidcClient, error) {
	if f.clientErr != nil {
		return db.OidcClient{}, f.clientErr
	}
	return f.client, nil
}

func (f *fakeAuthzQueries) GetSession(ctx context.Context, id string) (db.Session, error) {
	if f.sessionErr != nil {
		return db.Session{}, f.sessionErr
	}
	return f.session, nil
}

func (f *fakeAuthzQueries) GetConsent(ctx context.Context, arg db.GetConsentParams) ([]string, error) {
	if f.grantedErr != nil {
		return nil, f.grantedErr
	}
	return f.granted, nil
}

// recordingAudit captures every Record call for assertions.
type recordingAudit struct{ records []audit.Record }

func (a *recordingAudit) Record(ctx context.Context, r audit.Record) error {
	a.records = append(a.records, r)
	return nil
}

// validClient is the baseline registered client used across the tests.
func validClient() db.OidcClient {
	return db.OidcClient{
		ClientID:                    "rp-1",
		RedirectUris:                []string{"https://rp.example.com/callback"},
		AllowedScopes:               []string{"openid", "profile", "offline_access"},
		RequirePkce:                 true,
		AllowedCodeChallengeMethods: []string{"S256"},
	}
}

// validSession is the GetSession row returned for the authenticated path.
func validSession() db.Session {
	return db.Session{
		ID:        "sid-1",
		AccountID: 42,
		AuthTime:  pgtype.Timestamptz{Valid: true},
		Amr:       []string{"pwd"},
		Acr:       pgtype.Text{String: "urn:acr:1", Valid: true},
	}
}

// baseParams is a fully-valid authorize request query.
func baseParams() url.Values {
	v := url.Values{}
	v.Set("client_id", "rp-1")
	v.Set("redirect_uri", "https://rp.example.com/callback")
	v.Set("response_type", "code")
	v.Set("scope", "openid profile")
	v.Set("state", "xyz-state")
	v.Set("nonce", "n-123")
	v.Set("code_challenge", "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM")
	v.Set("code_challenge_method", "S256")
	return v
}

func newProvider(q db.Querier, a audit.Writer) *Provider {
	return &Provider{
		cfg:     &configx.Config{OIDC: configx.OIDCConfig{Issuer: testIssuer}},
		queries: q,
		kv:      kv.NewMemoryStore(),
		audit:   a,
		rl:      authn.NewRateLimiter(),
	}
}

// authedReq builds a GET /oauth/authorize request with a session on context.
func authedReq(v url.Values) *http.Request {
	req := httptest.NewRequest("GET", "/oauth/authorize?"+v.Encode(), nil)
	return req.WithContext(authn.WithSession(req.Context(), &authn.Session{
		Account: &db.Account{ID: 42},
		Data:    &authn.SessionData{SessionID: "sid-1", AccountID: 42},
	}))
}

func TestAuthorize_UnknownClient_DirectError(t *testing.T) {
	q := &fakeAuthzQueries{clientErr: pgx.ErrNoRows}
	p := newProvider(q, &recordingAudit{})

	rec := httptest.NewRecorder()
	p.HandleAuthorize(rec, authedReq(baseParams()))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
	if loc := rec.Result().Header.Get("Location"); loc != "" {
		t.Fatalf("unknown client must not redirect; got Location=%q", loc)
	}
}

func TestAuthorize_BadRedirectURI_DirectError(t *testing.T) {
	q := &fakeAuthzQueries{client: validClient()}
	p := newProvider(q, &recordingAudit{})

	v := baseParams()
	v.Set("redirect_uri", "https://attacker.example.com/steal")
	rec := httptest.NewRecorder()
	p.HandleAuthorize(rec, authedReq(v))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
	if loc := rec.Result().Header.Get("Location"); loc != "" {
		t.Fatalf("non-matching redirect_uri must not redirect; got Location=%q", loc)
	}
}

// redirectQuery drives the handler and returns the parsed Location query.
func redirectQuery(t *testing.T, rec *httptest.ResponseRecorder) url.Values {
	t.Helper()
	if rec.Code != http.StatusFound {
		t.Fatalf("want 302, got %d", rec.Code)
	}
	loc := rec.Result().Header.Get("Location")
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("parse Location %q: %v", loc, err)
	}
	return u.Query()
}

func TestAuthorize_UnsupportedResponseType_Redirect(t *testing.T) {
	q := &fakeAuthzQueries{client: validClient(), session: validSession()}
	p := newProvider(q, &recordingAudit{})

	v := baseParams()
	v.Set("response_type", "token")
	rec := httptest.NewRecorder()
	p.HandleAuthorize(rec, authedReq(v))

	if got := redirectQuery(t, rec).Get("error"); got != errCodeUnsupportedResponseType {
		t.Fatalf("want error=%s, got %q", errCodeUnsupportedResponseType, got)
	}
}

func TestAuthorize_MissingOpenidScope_Redirect(t *testing.T) {
	q := &fakeAuthzQueries{client: validClient(), session: validSession()}
	p := newProvider(q, &recordingAudit{})

	v := baseParams()
	v.Set("scope", "profile")
	rec := httptest.NewRecorder()
	p.HandleAuthorize(rec, authedReq(v))

	if got := redirectQuery(t, rec).Get("error"); got != errCodeInvalidScope {
		t.Fatalf("want error=%s, got %q", errCodeInvalidScope, got)
	}
}

func TestAuthorize_ScopeNotSubset_Redirect(t *testing.T) {
	q := &fakeAuthzQueries{client: validClient(), session: validSession()}
	p := newProvider(q, &recordingAudit{})

	v := baseParams()
	v.Set("scope", "openid admin") // admin not in AllowedScopes
	rec := httptest.NewRecorder()
	p.HandleAuthorize(rec, authedReq(v))

	if got := redirectQuery(t, rec).Get("error"); got != errCodeInvalidScope {
		t.Fatalf("want error=%s, got %q", errCodeInvalidScope, got)
	}
}

func TestAuthorize_MissingCodeChallenge_Redirect(t *testing.T) {
	q := &fakeAuthzQueries{client: validClient(), session: validSession()}
	p := newProvider(q, &recordingAudit{})

	v := baseParams()
	v.Del("code_challenge")
	rec := httptest.NewRecorder()
	p.HandleAuthorize(rec, authedReq(v))

	if got := redirectQuery(t, rec).Get("error"); got != errCodeInvalidRequest {
		t.Fatalf("want error=%s, got %q", errCodeInvalidRequest, got)
	}
}

func TestAuthorize_PlainPKCEMethod_Redirect(t *testing.T) {
	q := &fakeAuthzQueries{client: validClient(), session: validSession()}
	p := newProvider(q, &recordingAudit{})

	v := baseParams()
	v.Set("code_challenge_method", "plain")
	rec := httptest.NewRecorder()
	p.HandleAuthorize(rec, authedReq(v))

	if got := redirectQuery(t, rec).Get("error"); got != errCodeInvalidRequest {
		t.Fatalf("want error=%s, got %q", errCodeInvalidRequest, got)
	}
}

// TestAuthorize_RequirePkceFalse_NoChallengeProceeds verifies the require_pkce
// gate: a client with RequirePkce=false may omit code_challenge entirely and
// still reach the (authenticated) happy path that issues a code.
func TestAuthorize_RequirePkceFalse_NoChallengeProceeds(t *testing.T) {
	c := validClient()
	c.RequirePkce = false
	q := &fakeAuthzQueries{client: c, session: validSession()}
	p := newProvider(q, &recordingAudit{})

	v := baseParams()
	v.Del("code_challenge")
	v.Del("code_challenge_method")
	rec := httptest.NewRecorder()
	p.HandleAuthorize(rec, authedReq(v))

	// A successful authorize 302-redirects with a `code` param, no `error`.
	loc := redirectQuery(t, rec)
	if e := loc.Get("error"); e != "" {
		t.Fatalf("unexpected error=%q (loc query %v)", e, loc)
	}
	if loc.Get("code") == "" {
		t.Fatalf("expected an authorization code in redirect, got %v", loc)
	}
}

func TestAuthorize_NoSession_RedirectsToLogin(t *testing.T) {
	q := &fakeAuthzQueries{client: validClient()}
	p := newProvider(q, &recordingAudit{})

	// No session on context — use a bare request.
	v := baseParams()
	req := httptest.NewRequest("GET", "/oauth/authorize?"+v.Encode(), nil)
	rec := httptest.NewRecorder()
	p.HandleAuthorize(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("want 302, got %d", rec.Code)
	}
	loc := rec.Result().Header.Get("Location")
	wantPrefix := testIssuer + "/login?return_to="
	if !strings.HasPrefix(loc, wantPrefix) {
		t.Fatalf("want Location prefix %q, got %q", wantPrefix, loc)
	}
	// return_to must round-trip to the full authorize URL.
	u, _ := url.Parse(loc)
	rt := u.Query().Get("return_to")
	if !strings.HasPrefix(rt, testIssuer+"/oauth/authorize?") {
		t.Fatalf("return_to should be the full authorize URL, got %q", rt)
	}
}

func TestAuthorize_NoSessionPromptNone_LoginRequired(t *testing.T) {
	q := &fakeAuthzQueries{client: validClient()}
	p := newProvider(q, &recordingAudit{})

	v := baseParams()
	v.Set("prompt", "none")
	req := httptest.NewRequest("GET", "/oauth/authorize?"+v.Encode(), nil)
	rec := httptest.NewRecorder()
	p.HandleAuthorize(rec, req)

	gotQ := redirectQuery(t, rec)
	if got := gotQ.Get("error"); got != errCodeLoginRequired {
		t.Fatalf("want error=%s, got %q", errCodeLoginRequired, got)
	}
	// Must redirect to the RP, not the login page.
	loc := rec.Result().Header.Get("Location")
	if !strings.HasPrefix(loc, "https://rp.example.com/callback") {
		t.Fatalf("login_required must go to the RP redirect_uri, got %q", loc)
	}
}

// disabledSentinelReq builds a GET /oauth/authorize request carrying the
// disabled-mid-session sentinel that LoadSession attaches when an account is
// disabled mid-session: a non-nil Session whose Data is nil. The bare
// /oauth/authorize route skips authn.Check, so the handler must treat this as
// "not authenticated" rather than dereferencing the nil Data.
func disabledSentinelReq(v url.Values) *http.Request {
	req := httptest.NewRequest("GET", "/oauth/authorize?"+v.Encode(), nil)
	return req.WithContext(authn.WithSession(req.Context(), &authn.Session{
		Account: &db.Account{ID: 42, Disabled: true},
		Data:    nil,
	}))
}

func TestAuthorize_DisabledSentinelSession_RedirectsToLogin(t *testing.T) {
	q := &fakeAuthzQueries{client: validClient()}
	p := newProvider(q, &recordingAudit{})

	rec := httptest.NewRecorder()
	// Must not panic on the nil sess.Data; must take the no-session bounce.
	p.HandleAuthorize(rec, disabledSentinelReq(baseParams()))

	if rec.Code != http.StatusFound {
		t.Fatalf("want 302 login bounce, got %d", rec.Code)
	}
	loc := rec.Result().Header.Get("Location")
	wantPrefix := testIssuer + "/login?return_to="
	if !strings.HasPrefix(loc, wantPrefix) {
		t.Fatalf("want Location prefix %q, got %q", wantPrefix, loc)
	}
}

func TestAuthorize_DisabledSentinelSessionPromptNone_LoginRequired(t *testing.T) {
	q := &fakeAuthzQueries{client: validClient()}
	p := newProvider(q, &recordingAudit{})

	v := baseParams()
	v.Set("prompt", "none")
	rec := httptest.NewRecorder()
	// Must not panic; with prompt=none the disabled sentinel takes the
	// login_required RP redirect rather than the interactive login bounce.
	p.HandleAuthorize(rec, disabledSentinelReq(v))

	gotQ := redirectQuery(t, rec)
	if got := gotQ.Get("error"); got != errCodeLoginRequired {
		t.Fatalf("want error=%s, got %q", errCodeLoginRequired, got)
	}
	loc := rec.Result().Header.Get("Location")
	if !strings.HasPrefix(loc, "https://rp.example.com/callback") {
		t.Fatalf("login_required must go to the RP redirect_uri, got %q", loc)
	}
}

func TestAuthorize_HappyPath_IssuesCode(t *testing.T) {
	q := &fakeAuthzQueries{client: validClient(), session: validSession()}
	ra := &recordingAudit{}
	p := newProvider(q, ra)

	rec := httptest.NewRecorder()
	p.HandleAuthorize(rec, authedReq(baseParams()))

	gotQ := redirectQuery(t, rec)

	// Redirect target must be the registered RP callback.
	loc := rec.Result().Header.Get("Location")
	if !strings.HasPrefix(loc, "https://rp.example.com/callback?") {
		t.Fatalf("want redirect to RP callback, got %q", loc)
	}

	code := gotQ.Get("code")
	if code == "" {
		t.Fatal("expected a non-empty code param")
	}
	if got := gotQ.Get("state"); got != "xyz-state" {
		t.Fatalf("state should be echoed; got %q", got)
	}
	if got := gotQ.Get("iss"); got != testIssuer {
		t.Fatalf("iss should be the issuer; got %q", got)
	}
	if gotQ.Get("error") != "" {
		t.Fatalf("happy path must not carry an error: %q", gotQ.Get("error"))
	}

	// The code must be retrievable from KV (single-use consume succeeds).
	ac, err := consumeCode(context.Background(), p.kv, code)
	if err != nil {
		t.Fatalf("consumeCode: %v", err)
	}
	if ac.ClientID != "rp-1" || ac.AccountID != 42 || ac.SessionID != "sid-1" {
		t.Fatalf("unexpected authCode contents: %+v", ac)
	}
	if ac.Nonce != "n-123" {
		t.Fatalf("nonce not carried: %q", ac.Nonce)
	}
	if len(ac.Scope) != 2 || ac.Scope[0] != "openid" {
		t.Fatalf("scope not carried: %v", ac.Scope)
	}
	if ac.ACR != "urn:acr:1" || len(ac.AMR) != 1 || ac.AMR[0] != "pwd" {
		t.Fatalf("auth context not carried: acr=%q amr=%v", ac.ACR, ac.AMR)
	}

	// A success audit record with the oidc_client factor must be emitted.
	if len(ra.records) != 1 {
		t.Fatalf("want 1 audit record, got %d", len(ra.records))
	}
	r0 := ra.records[0]
	if r0.Factor != audit.FactorOIDCClient {
		t.Fatalf("want factor %s, got %s", audit.FactorOIDCClient, r0.Factor)
	}
	if r0.AccountID == nil || *r0.AccountID != 42 {
		t.Fatalf("audit AccountID should be 42, got %v", r0.AccountID)
	}
	if r0.Detail["reason"] != "authorize" {
		t.Fatalf("audit detail reason should be authorize, got %v", r0.Detail["reason"])
	}
}

func TestAuthorize_NoStateOmitted(t *testing.T) {
	q := &fakeAuthzQueries{client: validClient(), session: validSession()}
	p := newProvider(q, &recordingAudit{})

	v := baseParams()
	v.Del("state")
	rec := httptest.NewRecorder()
	p.HandleAuthorize(rec, authedReq(v))

	gotQ := redirectQuery(t, rec)
	if _, ok := gotQ["state"]; ok {
		t.Fatalf("state must be omitted when absent from request, got %q", gotQ.Get("state"))
	}
	if gotQ.Get("code") == "" {
		t.Fatal("expected a code even without state")
	}
}

func TestAuthorize_RateLimited_429(t *testing.T) {
	q := &fakeAuthzQueries{client: validClient(), session: validSession()}
	p := newProvider(q, &recordingAudit{})

	// The per-account limit is authorizeRateMax (60) per window. Drive the
	// authenticated happy path until the limiter trips; assert one call returns
	// 429 with a Retry-After header.
	var saw429 bool
	for i := 0; i < authorizeRateMax+1; i++ {
		rec := httptest.NewRecorder()
		p.HandleAuthorize(rec, authedReq(baseParams()))
		if rec.Code == http.StatusTooManyRequests {
			if ra := rec.Result().Header.Get("Retry-After"); ra == "" {
				t.Fatalf("429 response must carry a Retry-After header")
			}
			saw429 = true
			break
		}
		if rec.Code != http.StatusFound {
			t.Fatalf("iteration %d: want 302 or 429, got %d", i, rec.Code)
		}
	}
	if !saw429 {
		t.Fatalf("expected a 429 within %d requests, never saw one", authorizeRateMax+1)
	}
}

// sessionWithAuthTime returns the baseline session with a specific auth_time.
func sessionWithAuthTime(at time.Time) db.Session {
	s := validSession()
	s.AuthTime = pgtype.Timestamptz{Time: at, Valid: true}
	return s
}

// TestAuthorize_PromptValidation guards T2.3: unknown prompt tokens are
// rejected with invalid_request (not silently ignored), and "none" must not be
// combined with any other value.
func TestAuthorize_PromptValidation(t *testing.T) {
	for _, prompt := range []string{"create", "select_account create", "none login", "none consent"} {
		q := &fakeAuthzQueries{client: validClient(), session: sessionWithAuthTime(time.Now())}
		p := newProvider(q, &recordingAudit{})
		v := baseParams()
		v.Set("prompt", prompt)
		rec := httptest.NewRecorder()
		p.HandleAuthorize(rec, authedReq(v))
		if rec.Code != http.StatusFound {
			t.Fatalf("prompt=%q: want 302 error redirect, got %d", prompt, rec.Code)
		}
		loc, _ := url.Parse(rec.Result().Header.Get("Location"))
		if got := loc.Query().Get("error"); got != "invalid_request" {
			t.Errorf("prompt=%q: want error=invalid_request, got %q", prompt, got)
		}
		// RFC 9207: error redirects carry iss.
		if got := loc.Query().Get("iss"); got != testIssuer {
			t.Errorf("prompt=%q: error redirect missing iss (got %q)", prompt, got)
		}
	}
}

// (a) prompt=login, valid session, NO reauth param → 302 bounce to /login with
// a reauth nonce; must NOT mint a code.
func TestAuthorize_PromptLogin_BouncesToLogin(t *testing.T) {
	q := &fakeAuthzQueries{client: validClient(), session: sessionWithAuthTime(time.Now())}
	p := newProvider(q, &recordingAudit{})

	v := baseParams()
	v.Set("prompt", "login")
	rec := httptest.NewRecorder()
	p.HandleAuthorize(rec, authedReq(v))

	if rec.Code != http.StatusFound {
		t.Fatalf("want 302, got %d", rec.Code)
	}
	loc := rec.Result().Header.Get("Location")
	if !strings.HasPrefix(loc, testIssuer+"/login?return_to=") {
		t.Fatalf("prompt=login must bounce to the login page, got %q", loc)
	}
	u, _ := url.Parse(loc)
	rt := u.Query().Get("return_to")
	rtu, err := url.Parse(rt)
	if err != nil {
		t.Fatalf("parse return_to %q: %v", rt, err)
	}
	if got := rtu.Query().Get("reauth"); got == "" {
		t.Fatalf("return_to must carry a reauth nonce; return_to=%q", rt)
	}
	// A bounce must not have minted a code in the RP query.
	if got := u.Query().Get("code"); got != "" {
		t.Fatalf("bounce must not issue a code, got code=%q", got)
	}
}

// (b) prompt=login with a STALE session (auth_time BEFORE the demand marker),
// returning WITH that nonce → must re-bounce (stale session does not satisfy).
func TestAuthorize_PromptLogin_StaleSessionRebounces(t *testing.T) {
	p := newProvider(&fakeAuthzQueries{client: validClient()}, &recordingAudit{})

	// Demand a marker NOW; the session's auth_time predates it → stale.
	nonce, err := authn.DemandReauth(context.Background(), p.kv, "oidc:reauth:", 42)
	if err != nil {
		t.Fatalf("DemandReauth: %v", err)
	}
	staleAuth := time.Now().Add(-time.Hour)
	p.queries = &fakeAuthzQueries{client: validClient(), session: sessionWithAuthTime(staleAuth)}

	v := baseParams()
	v.Set("prompt", "login")
	v.Set("reauth", nonce)
	rec := httptest.NewRecorder()
	p.HandleAuthorize(rec, authedReq(v))

	if rec.Code != http.StatusFound {
		t.Fatalf("want 302, got %d", rec.Code)
	}
	loc := rec.Result().Header.Get("Location")
	if !strings.HasPrefix(loc, testIssuer+"/login?return_to=") {
		t.Fatalf("stale session must re-bounce to login, got %q", loc)
	}
	u, _ := url.Parse(loc)
	if got := u.Query().Get("code"); got != "" {
		t.Fatalf("stale session must not issue a code, got code=%q", got)
	}
}

// (c) prompt=login with a FRESH session (auth_time AFTER the demand marker) and
// the valid nonce → issues a code (302 to the RP).
func TestAuthorize_PromptLogin_FreshSessionIssuesCode(t *testing.T) {
	p := newProvider(&fakeAuthzQueries{client: validClient()}, &recordingAudit{})

	nonce, err := authn.DemandReauth(context.Background(), p.kv, "oidc:reauth:", 42)
	if err != nil {
		t.Fatalf("DemandReauth: %v", err)
	}
	// Fresh: auth_time strictly after the marker just written.
	freshAuth := time.Now().Add(time.Second)
	p.queries = &fakeAuthzQueries{client: validClient(), session: sessionWithAuthTime(freshAuth)}

	v := baseParams()
	v.Set("prompt", "login")
	v.Set("reauth", nonce)
	rec := httptest.NewRecorder()
	p.HandleAuthorize(rec, authedReq(v))

	gotQ := redirectQuery(t, rec)
	loc := rec.Result().Header.Get("Location")
	if !strings.HasPrefix(loc, "https://rp.example.com/callback?") {
		t.Fatalf("fresh session must issue a code to the RP, got %q", loc)
	}
	if gotQ.Get("code") == "" {
		t.Fatal("fresh session + valid nonce must issue a code")
	}
	if gotQ.Get("error") != "" {
		t.Fatalf("must not carry an error: %q", gotQ.Get("error"))
	}
}

// (d) max_age=0 → always demand → bounce (no reauth nonce present).
func TestAuthorize_MaxAgeZero_AlwaysBounces(t *testing.T) {
	q := &fakeAuthzQueries{client: validClient(), session: sessionWithAuthTime(time.Now())}
	p := newProvider(q, &recordingAudit{})

	v := baseParams()
	v.Set("max_age", "0")
	rec := httptest.NewRecorder()
	p.HandleAuthorize(rec, authedReq(v))

	if rec.Code != http.StatusFound {
		t.Fatalf("want 302, got %d", rec.Code)
	}
	loc := rec.Result().Header.Get("Location")
	if !strings.HasPrefix(loc, testIssuer+"/login?return_to=") {
		t.Fatalf("max_age=0 must always bounce to login, got %q", loc)
	}
}

// (e) max_age=3600 with a ~10-minute-old session → no bounce; issues a code.
func TestAuthorize_MaxAgeFresh_IssuesCode(t *testing.T) {
	q := &fakeAuthzQueries{client: validClient(), session: sessionWithAuthTime(time.Now().Add(-10 * time.Minute))}
	p := newProvider(q, &recordingAudit{})

	v := baseParams()
	v.Set("max_age", "3600")
	rec := httptest.NewRecorder()
	p.HandleAuthorize(rec, authedReq(v))

	gotQ := redirectQuery(t, rec)
	loc := rec.Result().Header.Get("Location")
	if !strings.HasPrefix(loc, "https://rp.example.com/callback?") {
		t.Fatalf("max_age=3600 with a fresh session must issue a code, got %q", loc)
	}
	if gotQ.Get("code") == "" {
		t.Fatal("expected a code")
	}
}

// (f) prompt=none + a demand (max_age=0) → login_required to the RP.
func TestAuthorize_PromptNone_Demand_LoginRequired(t *testing.T) {
	q := &fakeAuthzQueries{client: validClient(), session: sessionWithAuthTime(time.Now())}
	p := newProvider(q, &recordingAudit{})

	v := baseParams()
	v.Set("prompt", "none")
	v.Set("max_age", "0")
	rec := httptest.NewRecorder()
	p.HandleAuthorize(rec, authedReq(v))

	gotQ := redirectQuery(t, rec)
	if got := gotQ.Get("error"); got != errCodeLoginRequired {
		t.Fatalf("want error=%s, got %q", errCodeLoginRequired, got)
	}
	loc := rec.Result().Header.Get("Location")
	if !strings.HasPrefix(loc, "https://rp.example.com/callback") {
		t.Fatalf("login_required must go to the RP, got %q", loc)
	}
}

// (g) prompt=login none (both) → invalid_request redirected back to the RP.
// This guard fires AFTER redirect_uri validation, so it is on the redirectError
// side of the open-redirect boundary, not a direct 400.
func TestAuthorize_PromptLoginAndNone_RedirectInvalidRequest(t *testing.T) {
	q := &fakeAuthzQueries{client: validClient(), session: validSession()}
	p := newProvider(q, &recordingAudit{})

	v := baseParams()
	v.Set("prompt", "login none")
	rec := httptest.NewRecorder()
	p.HandleAuthorize(rec, authedReq(v))

	gotQ := redirectQuery(t, rec)
	if got := gotQ.Get("error"); got != errCodeInvalidRequest {
		t.Fatalf("want error=%s, got %q", errCodeInvalidRequest, got)
	}
	if got := gotQ.Get("state"); got != "xyz-state" {
		t.Fatalf("want state=xyz-state echoed, got %q", got)
	}
	loc := rec.Result().Header.Get("Location")
	if !strings.HasPrefix(loc, "https://rp.example.com/callback") {
		t.Fatalf("prompt=login+none must redirect to the RP, got %q", loc)
	}
}

// invalid max_age (non-int) → redirect error invalid_request to the RP.
func TestAuthorize_InvalidMaxAge_RedirectInvalidRequest(t *testing.T) {
	q := &fakeAuthzQueries{client: validClient(), session: validSession()}
	p := newProvider(q, &recordingAudit{})

	v := baseParams()
	v.Set("max_age", "abc")
	rec := httptest.NewRecorder()
	p.HandleAuthorize(rec, authedReq(v))

	gotQ := redirectQuery(t, rec)
	if got := gotQ.Get("error"); got != errCodeInvalidRequest {
		t.Fatalf("want error=%s, got %q", errCodeInvalidRequest, got)
	}
	loc := rec.Result().Header.Get("Location")
	if !strings.HasPrefix(loc, "https://rp.example.com/callback") {
		t.Fatalf("invalid max_age must redirect to the RP, got %q", loc)
	}
}

// ── per-app access gate (RBAC, Task 7) ───────────────────────────────────────

// TestAuthorize_AppAccessDenied_Interactive verifies an authenticated user who
// is not authorized for a restricted client is 302-redirected to the IdP's OWN
// /error page (reason=app_access_denied + the client display name), NOT to the
// RP, and that the denial is audited with EventAccessDenied.
func TestAuthorize_AppAccessDenied_Interactive(t *testing.T) {
	c := validClient()
	c.DisplayName = "Acme Console"
	q := &fakeAuthzQueries{client: c, session: validSession(), denied: true}
	ra := &recordingAudit{}
	p := newProvider(q, ra)

	rec := httptest.NewRecorder()
	p.HandleAuthorize(rec, authedReq(baseParams()))

	if rec.Code != http.StatusFound {
		t.Fatalf("want 302, got %d", rec.Code)
	}
	loc := rec.Result().Header.Get("Location")
	wantPrefix := testIssuer + "/error?reason=app_access_denied"
	if !strings.HasPrefix(loc, wantPrefix) {
		t.Fatalf("denied user must go to the IdP /error page, got %q", loc)
	}
	// The app display name must round-trip in the app= param.
	u, _ := url.Parse(loc)
	if got := u.Query().Get("app"); got != "Acme Console" {
		t.Fatalf("error page app= should be the client display name, got %q", got)
	}
	// Must NOT have minted a code or redirected to the RP.
	if strings.HasPrefix(loc, "https://rp.example.com/callback") {
		t.Fatalf("denial must not redirect to the RP, got %q", loc)
	}

	// A denial audit record (oidc_client / access_denied / account 42) is emitted.
	if len(ra.records) != 1 {
		t.Fatalf("want 1 audit record, got %d", len(ra.records))
	}
	r0 := ra.records[0]
	if r0.Factor != audit.FactorOIDCClient || r0.Event != audit.EventAccessDenied {
		t.Fatalf("want %s/%s, got %s/%s", audit.FactorOIDCClient, audit.EventAccessDenied, r0.Factor, r0.Event)
	}
	if r0.AccountID == nil || *r0.AccountID != 42 {
		t.Fatalf("denial audit AccountID should be 42, got %v", r0.AccountID)
	}
	if r0.Detail["reason"] != "app_access_denied" {
		t.Fatalf("denial audit reason should be app_access_denied, got %v", r0.Detail["reason"])
	}
}

// TestAuthorize_AppAccessDenied_PromptNone verifies that a denied user whose RP
// forbade interactive UI (prompt=none) gets the protocol-native access_denied
// redirected to the RP's redirect_uri — NOT the /error page.
func TestAuthorize_AppAccessDenied_PromptNone(t *testing.T) {
	q := &fakeAuthzQueries{client: validClient(), session: validSession(), denied: true}
	p := newProvider(q, &recordingAudit{})

	v := baseParams()
	v.Set("prompt", "none")
	rec := httptest.NewRecorder()
	p.HandleAuthorize(rec, authedReq(v))

	gotQ := redirectQuery(t, rec)
	if got := gotQ.Get("error"); got != errCodeAccessDenied {
		t.Fatalf("want error=%s, got %q", errCodeAccessDenied, got)
	}
	loc := rec.Result().Header.Get("Location")
	if !strings.HasPrefix(loc, "https://rp.example.com/callback") {
		t.Fatalf("prompt=none denial must go to the RP redirect_uri, got %q", loc)
	}
	// state must be echoed back to the RP.
	if got := gotQ.Get("state"); got != "xyz-state" {
		t.Fatalf("state should be echoed, got %q", got)
	}
}

// TestAuthorize_AppAccessAllowed_IssuesCode verifies the gate is non-blocking
// for an authorized account (the explicit positive case alongside the existing
// happy path, which also runs with the default authorized predicate).
func TestAuthorize_AppAccessAllowed_IssuesCode(t *testing.T) {
	q := &fakeAuthzQueries{client: validClient(), session: validSession()} // denied=false
	p := newProvider(q, &recordingAudit{})

	rec := httptest.NewRecorder()
	p.HandleAuthorize(rec, authedReq(baseParams()))

	gotQ := redirectQuery(t, rec)
	if gotQ.Get("code") == "" {
		t.Fatalf("authorized account must receive a code, got %v", gotQ)
	}
	if gotQ.Get("error") != "" {
		t.Fatalf("authorized account must not carry an error, got %q", gotQ.Get("error"))
	}
}

// TestAuthorize_AppAccessPredicateError_ServerError verifies the gate fails
// CLOSED: a predicate evaluation error denies and surfaces server_error to the
// RP (never fail-open, never access_denied since we make no authz claim).
func TestAuthorize_AppAccessPredicateError_ServerError(t *testing.T) {
	q := &fakeAuthzQueries{client: validClient(), session: validSession(), authzErr: errors.New("db down")}
	p := newProvider(q, &recordingAudit{})

	rec := httptest.NewRecorder()
	p.HandleAuthorize(rec, authedReq(baseParams()))

	gotQ := redirectQuery(t, rec)
	if got := gotQ.Get("error"); got != errCodeServerError {
		t.Fatalf("want error=%s (fail closed), got %q", errCodeServerError, got)
	}
	if gotQ.Get("code") != "" {
		t.Fatalf("predicate error must not issue a code, got %q", gotQ.Get("code"))
	}
}

func TestAuthorize_GetSessionError_RedirectsServerError(t *testing.T) {
	q := &fakeAuthzQueries{
		client:     validClient(),
		session:    validSession(),
		sessionErr: errors.New("db down"),
	}
	p := newProvider(q, &recordingAudit{})

	rec := httptest.NewRecorder()
	p.HandleAuthorize(rec, authedReq(baseParams()))

	// GetSession fails AFTER redirect_uri validation, so the error travels back
	// to the RP via redirectError carrying error=server_error.
	gotQ := redirectQuery(t, rec)
	if got := gotQ.Get("error"); got != errCodeServerError {
		t.Fatalf("want error=%s, got %q", errCodeServerError, got)
	}
	loc := rec.Result().Header.Get("Location")
	if !strings.HasPrefix(loc, "https://rp.example.com/callback") {
		t.Fatalf("server_error must go to the RP redirect_uri, got %q", loc)
	}
}
