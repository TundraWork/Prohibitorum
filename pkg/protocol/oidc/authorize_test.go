package oidc

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

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

// recordingAudit captures every Record call for assertions.
type recordingAudit struct{ records []audit.Record }

func (a *recordingAudit) Record(ctx context.Context, r audit.Record) error {
	a.records = append(a.records, r)
	return nil
}

// validClient is the baseline registered client used across the tests.
func validClient() db.OidcClient {
	return db.OidcClient{
		ClientID:      "rp-1",
		RedirectUris:  []string{"https://rp.example.com/callback"},
		AllowedScopes: []string{"openid", "profile", "offline_access"},
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

func TestAuthorize_RequireConsent_Redirect(t *testing.T) {
	c := validClient()
	c.RequireConsent = true
	q := &fakeAuthzQueries{client: c, session: validSession()}
	p := newProvider(q, &recordingAudit{})

	rec := httptest.NewRecorder()
	p.HandleAuthorize(rec, authedReq(baseParams()))

	if got := redirectQuery(t, rec).Get("error"); got != errCodeConsentRequired {
		t.Fatalf("want error=%s, got %q", errCodeConsentRequired, got)
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
