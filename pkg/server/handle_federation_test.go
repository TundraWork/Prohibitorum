// Package server — handle_federation_test.go
//
// Handler-level tests for the federation login/callback endpoints (Task 7).
//
// Scaffolding decision: copy the modes_test.go / federation_test.go fake
// pattern into this package (tests are easier to read when self-contained).
// We instantiate a real fedoidc.Federator against the mock OP and a memory KV
// — no interface seam on s.federator, matching the brief.
//
// The fake querier here satisfies every interface the handlers chain through:
//   - fedoidc.FederatorQueries (Federator → Resolve → audit insert)
//   - sessstore.SessionQueries (SessionStore.Issue)
//   - audit dbWriter via db.Querier (audit.NewWriter)
//
// We embed db.Querier so the audit writer (which needs the full sqlc surface)
// type-checks. The embedded nil is fine because we override the one method
// (InsertCredentialEvent) that audit actually calls; any other method dispatch
// would NPE and signal a missing method on the fake.

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"prohibitorum/cmd/smoke/mockop"
	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/db"
	fedoidc "prohibitorum/pkg/federation/oidc"
	"prohibitorum/pkg/kv"
	sessstore "prohibitorum/pkg/session"
)

// --- fake querier ---------------------------------------------------------

// fakeFedQueries combines the surfaces the federator + session store + audit
// writer all need. Embedding db.Querier lets it pass to audit.NewWriter (which
// takes db.Querier); we override every method actually invoked by the code
// paths under test. Any unexpected dispatch nil-panics, which is the loudest
// possible signal that the fake is incomplete.
type fakeFedQueries struct {
	db.Querier

	mu sync.Mutex

	// upstream_idp
	idpBySlug map[string]db.UpstreamIdp

	// account_identity (auto-provision: ErrNoRows on first lookup)
	identityResult db.AccountIdentity
	identityErr    error

	// account
	accountByIDResults  map[int32]db.Account
	accountByUsername   map[string]db.Account
	usernameErr         error
	nextAccountID       int32
	insertedAccounts    []db.Account

	// account_identity inserts
	nextIdentityID  int64
	insertIdentitys []db.InsertAccountIdentityParams

	// session
	sessions []db.Session

	// audit
	events []db.InsertCredentialEventParams
}

func newFakeFedQueries() *fakeFedQueries {
	return &fakeFedQueries{
		idpBySlug:          map[string]db.UpstreamIdp{},
		identityErr:        pgx.ErrNoRows,
		accountByIDResults: map[int32]db.Account{},
		accountByUsername:  map[string]db.Account{},
		usernameErr:        pgx.ErrNoRows,
		nextAccountID:      100,
		nextIdentityID:     200,
	}
}

func (f *fakeFedQueries) GetUpstreamIDPBySlug(_ context.Context, slug string) (db.UpstreamIdp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if v, ok := f.idpBySlug[slug]; ok {
		return v, nil
	}
	return db.UpstreamIdp{}, pgx.ErrNoRows
}

func (f *fakeFedQueries) ListAccountIdentitiesByAccount(_ context.Context, _ int32) ([]db.ListAccountIdentitiesByAccountRow, error) {
	return nil, nil
}

func (f *fakeFedQueries) GetAccountIdentityByIssuerSub(_ context.Context, _ db.GetAccountIdentityByIssuerSubParams) (db.AccountIdentity, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.identityResult, f.identityErr
}

func (f *fakeFedQueries) GetAccountByID(_ context.Context, id int32) (db.Account, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if a, ok := f.accountByIDResults[id]; ok {
		return a, nil
	}
	return db.Account{}, pgx.ErrNoRows
}

func (f *fakeFedQueries) GetAccountByUsername(_ context.Context, u string) (db.Account, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if a, ok := f.accountByUsername[u]; ok {
		return a, nil
	}
	return db.Account{}, f.usernameErr
}

func (f *fakeFedQueries) InsertAccount(_ context.Context, arg db.InsertAccountParams) (db.Account, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := f.nextAccountID
	f.nextAccountID++
	acct := db.Account{
		ID:          id,
		Username:    arg.Username,
		DisplayName: arg.DisplayName,
		Role:        arg.Role,
		Attributes:  arg.Attributes,
		Disabled:    arg.Disabled,
	}
	f.insertedAccounts = append(f.insertedAccounts, acct)
	f.accountByIDResults[id] = acct
	return acct, nil
}

func (f *fakeFedQueries) InsertAccountIdentity(_ context.Context, arg db.InsertAccountIdentityParams) (db.AccountIdentity, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.insertIdentitys = append(f.insertIdentitys, arg)
	id := f.nextIdentityID
	f.nextIdentityID++
	return db.AccountIdentity{
		ID:            id,
		AccountID:     arg.AccountID,
		UpstreamIdpID: arg.UpstreamIdpID,
		UpstreamIss:   arg.UpstreamIss,
		UpstreamSub:   arg.UpstreamSub,
		UpstreamEmail: arg.UpstreamEmail,
	}, nil
}

func (f *fakeFedQueries) UpdateAccountDisplayName(_ context.Context, _ db.UpdateAccountDisplayNameParams) error {
	return nil
}

func (f *fakeFedQueries) UpdateAccountIdentityEmail(_ context.Context, _ db.UpdateAccountIdentityEmailParams) error {
	return nil
}

func (f *fakeFedQueries) InsertSession(_ context.Context, arg db.InsertSessionParams) (db.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	row := db.Session{ID: arg.ID, AccountID: arg.AccountID, AuthTime: arg.AuthTime, Amr: arg.Amr}
	f.sessions = append(f.sessions, row)
	return row, nil
}

func (f *fakeFedQueries) RevokeSession(_ context.Context, _ string) error              { return nil }
func (f *fakeFedQueries) RevokeAllSessionsByAccount(_ context.Context, _ int32) error  { return nil }

func (f *fakeFedQueries) InsertCredentialEvent(_ context.Context, arg db.InsertCredentialEventParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, arg)
	return nil
}

// Compile-time guard: must satisfy the federator's query surface.
var _ fedoidc.FederatorQueries = (*fakeFedQueries)(nil)

// --- harness --------------------------------------------------------------

type fedTestHarness struct {
	t      *testing.T
	op     *mockop.Server
	opTS   *httptest.Server
	idp    db.UpstreamIdp
	q      *fakeFedQueries
	s      *Server
	srvTS  *httptest.Server
	origin string

	// linkAccountID / linkToken are populated by newLinkTestHarness (Task 8
	// tests). The plain federation tests in this file leave them zero.
	linkAccountID int32
	linkToken     string
}

// fedTestDEK is a deterministic 32-byte AES-256 key for the test ciphertexts.
var fedTestDEK = bytes.Repeat([]byte{0x22}, 32)

// newFederationTestServer spins up the mock OP, builds a fake querier with a
// single upstream_idp row, constructs a real Federator, and mounts the two
// handlers under a chi router served by httptest.Server. The Server's
// publicOrigin is the test server's URL — so the redirect_uri the federator
// generates matches the callback URL the test will hit.
func newFederationTestServer(t *testing.T) *fedTestHarness {
	t.Helper()

	// 1. Mock OP.
	op, err := mockop.New("")
	if err != nil {
		t.Fatalf("mockop.New: %v", err)
	}
	opTS := httptest.NewServer(op.Routes())
	op.SetBase(opTS.URL)
	t.Cleanup(opTS.Close)
	op.SetClaims("sub-1", "alice@example.com", true, "alice", "Alice Example")
	op.SetAMR([]string{"pwd", "mfa"})

	// 2. Fake querier seeded with one IdP row.
	const idpID int64 = 42
	const keyVersion int32 = 1
	ct, nonce, err := fedoidc.EncryptClientSecret(fedTestDEK, []byte("test-secret"), idpID, keyVersion)
	if err != nil {
		t.Fatalf("EncryptClientSecret: %v", err)
	}
	idp := db.UpstreamIdp{
		ID:                   idpID,
		Slug:                 "mockop",
		DisplayName:          "Mock OP",
		IssuerUrl:            opTS.URL,
		ClientID:             "test-client",
		ClientSecretEnc:      ct,
		SecretNonce:          nonce,
		KeyVersion:           keyVersion,
		Scopes:               []string{"openid", "profile", "email"},
		Mode:                 fedoidc.ModeAutoProvision,
		RequireVerifiedEmail: true,
		// Schema defaults from migration 004 — DB-side DEFAULTs don't apply
		// to in-memory fakes, so seed them explicitly so modes.go can
		// resolve the per-IdP claim names via ClaimString(...).
		UsernameClaim:    "preferred_username",
		DisplayNameClaim: "name",
		EmailClaim:       "email",
	}
	q := newFakeFedQueries()
	q.idpBySlug[idp.Slug] = idp

	// 3. KV + audit + sessionStore.
	kvStore := kv.NewMemoryStore()
	t.Cleanup(func() { _ = kvStore.Close() })
	auditWriter := audit.NewWriter(q)

	cfg := &configx.Config{
		SessionTTL: time.Hour,
		TrustProxy: false,
	}
	sessionStore := sessstore.NewSessionStore(kvStore, q, cfg.SessionTTL)

	// 4. Federator. publicOrigin is filled in below after we start srvTS.
	fedCfg := configx.FederationConfig{
		StateTTL:      5 * time.Minute,
		DefaultScopes: []string{"openid", "profile", "email"},
	}
	deks := map[int][]byte{1: fedTestDEK}

	// 5. Mount handlers behind a chi router served by httptest.
	s := &Server{
		config:       cfg,
		kvStore:      kvStore,
		sessionStore: sessionStore,
		rateLimiter:  authn.NewRateLimiter(),
		Audit:        auditWriter,
	}
	r := chi.NewRouter()
	r.Get("/api/prohibitorum/auth/federation/{slug}/login", s.handleFederationLoginHTTP)
	r.Get("/api/prohibitorum/auth/federation/{slug}/callback", s.handleFederationCallbackHTTP)
	srvTS := httptest.NewServer(r)
	t.Cleanup(srvTS.Close)

	// Now we know our own origin — federator can build redirect_uri's that
	// the test http.Client can dial back into.
	s.federator = fedoidc.NewFederator(q, kvStore, auditWriter, fedCfg, deks, srvTS.URL)
	cfg.PublicOrigins = []string{srvTS.URL}

	return &fedTestHarness{
		t:      t,
		op:     op,
		opTS:   opTS,
		idp:    idp,
		q:      q,
		s:      s,
		srvTS:  srvTS,
		origin: srvTS.URL,
	}
}

// noFollow returns an http.Client that captures every 302 instead of chasing it.
// This is the entire driver mechanism — we step manually through login → authorize
// → callback so each hop is observable.
func noFollow() *http.Client {
	return &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// driveLogin hits /login and returns (authorizeURL, status).
func (h *fedTestHarness) driveLogin(t *testing.T, slug, returnTo string) (string, *http.Response) {
	t.Helper()
	u := h.srvTS.URL + "/api/prohibitorum/auth/federation/" + slug + "/login"
	if returnTo != "" {
		u += "?return_to=" + url.QueryEscape(returnTo)
	}
	resp, err := noFollow().Get(u)
	if err != nil {
		t.Fatalf("GET %s: %v", u, err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusFound {
		return "", resp
	}
	return resp.Header.Get("Location"), resp
}

// driveAuthorize hits the upstream /authorize URL (no-follow) and extracts
// (code, state, iss) from the resulting 302 Location.
func driveAuthorize(t *testing.T, authorizeURL string) (code, state, iss string) {
	t.Helper()
	resp, err := noFollow().Get(authorizeURL)
	if err != nil {
		t.Fatalf("GET /authorize: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("authorize: status %d, want 302", resp.StatusCode)
	}
	loc, err := resp.Location()
	if err != nil {
		t.Fatalf("authorize Location: %v", err)
	}
	q := loc.Query()
	return q.Get("code"), q.Get("state"), q.Get("iss")
}

// hitCallback hits /callback with the given params and returns the response.
func (h *fedTestHarness) hitCallback(t *testing.T, slug string, q url.Values) *http.Response {
	t.Helper()
	u := h.srvTS.URL + "/api/prohibitorum/auth/federation/" + slug + "/callback?" + q.Encode()
	resp, err := noFollow().Get(u)
	if err != nil {
		t.Fatalf("GET /callback: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

// decodeErrBody extracts the auth error envelope from a non-2xx response.
type errBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func decodeErrBody(t *testing.T, resp *http.Response) errBody {
	t.Helper()
	var out errBody
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	return out
}

// --- tests ----------------------------------------------------------------

func TestFederationLogin_RedirectsToUpstream(t *testing.T) {
	h := newFederationTestServer(t)

	loc, resp := h.driveLogin(t, "mockop", "/me")
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status: want 302, got %d", resp.StatusCode)
	}
	if !strings.HasPrefix(loc, h.opTS.URL+"/authorize") {
		t.Errorf("Location: want prefix %q, got %q", h.opTS.URL+"/authorize", loc)
	}
	// state must round-trip into KV under LoginKey.
	u, _ := url.Parse(loc)
	state := u.Query().Get("state")
	if state == "" {
		t.Fatal("state missing from authorize URL")
	}
	if _, err := h.s.kvStore.Get(context.Background(), fedoidc.LoginKey(state)); err != nil {
		t.Errorf("state not stashed under LoginKey: %v", err)
	}
}

func TestFederationLogin_InvalidReturnTo(t *testing.T) {
	h := newFederationTestServer(t)

	_, resp := h.driveLogin(t, "mockop", "https://evil.example.com/")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d", resp.StatusCode)
	}
	body := decodeErrBody(t, resp)
	if body.Code != "invalid_return_to" {
		t.Errorf("code: want invalid_return_to, got %q", body.Code)
	}
}

func TestFederationLogin_EmptyReturnToDefaultsToSlash(t *testing.T) {
	h := newFederationTestServer(t)

	loc, resp := h.driveLogin(t, "mockop", "")
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status: want 302, got %d (Location=%s)", resp.StatusCode, loc)
	}
	u, _ := url.Parse(loc)
	state := u.Query().Get("state")
	blob, err := h.s.kvStore.Get(context.Background(), fedoidc.LoginKey(state))
	if err != nil {
		t.Fatalf("state not in KV: %v", err)
	}
	fs, err := fedoidc.DecodeFedState(blob)
	if err != nil {
		t.Fatalf("DecodeFedState: %v", err)
	}
	if fs.ReturnTo != "/" {
		t.Errorf("ReturnTo: want %q, got %q", "/", fs.ReturnTo)
	}
}

func TestFederationLogin_UnknownSlugReturnsStateInvalid(t *testing.T) {
	h := newFederationTestServer(t)

	_, resp := h.driveLogin(t, "no-such-idp", "/me")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status: want 401, got %d", resp.StatusCode)
	}
	body := decodeErrBody(t, resp)
	if body.Code != "federation_state_invalid" {
		t.Errorf("code: want federation_state_invalid, got %q", body.Code)
	}
}

func TestFederationCallback_HappyPath(t *testing.T) {
	h := newFederationTestServer(t)

	loc, resp := h.driveLogin(t, "mockop", "/me")
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("login status: want 302, got %d", resp.StatusCode)
	}
	code, state, iss := driveAuthorize(t, loc)

	q := url.Values{}
	q.Set("code", code)
	q.Set("state", state)
	q.Set("iss", iss)
	resp = h.hitCallback(t, "mockop", q)

	if resp.StatusCode != http.StatusFound {
		body, _ := readAll(resp.Body)
		t.Fatalf("callback: want 302, got %d (body=%s)", resp.StatusCode, body)
	}
	if got := resp.Header.Get("Location"); got != "/me" {
		t.Errorf("Location: want /me, got %q", got)
	}

	// Session cookie should be set.
	var sessCookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == sessstore.SessionCookieName {
			sessCookie = c
			break
		}
	}
	if sessCookie == nil {
		t.Fatal("session cookie not set")
	}
	if sessCookie.Value == "" {
		t.Fatal("session cookie value empty")
	}

	// One inserted account + one inserted identity row.
	if len(h.q.insertedAccounts) != 1 {
		t.Errorf("accounts inserted: want 1, got %d", len(h.q.insertedAccounts))
	}
	if len(h.q.insertIdentitys) != 1 {
		t.Errorf("identities inserted: want 1, got %d", len(h.q.insertIdentitys))
	}
	if len(h.q.sessions) != 1 {
		t.Errorf("sessions inserted: want 1, got %d", len(h.q.sessions))
	}
	// AMR pass-through: upstream sent [pwd mfa].
	if len(h.q.sessions) > 0 {
		amr := h.q.sessions[0].Amr
		if len(amr) != 2 || amr[0] != "pwd" || amr[1] != "mfa" {
			t.Errorf("session amr: want [pwd mfa], got %v", amr)
		}
	}
}

func TestFederationCallback_BackfillsAMRWhenUpstreamOmits(t *testing.T) {
	h := newFederationTestServer(t)
	// Upstream omits the amr claim entirely. The handler must backfill
	// ["federated"] (RFC 8176) so the session row's amr is non-empty —
	// otherwise pkg/session.Issue would reject and surface a 500.
	h.op.SetAMR(nil)

	loc, resp := h.driveLogin(t, "mockop", "/me")
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("login: want 302, got %d", resp.StatusCode)
	}
	code, state, iss := driveAuthorize(t, loc)

	q := url.Values{}
	q.Set("code", code)
	q.Set("state", state)
	q.Set("iss", iss)
	resp = h.hitCallback(t, "mockop", q)

	if resp.StatusCode != http.StatusFound {
		body, _ := readAll(resp.Body)
		t.Fatalf("callback: want 302 (no 500), got %d (body=%s)", resp.StatusCode, body)
	}

	// Session cookie must be set.
	var sessCookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == sessstore.SessionCookieName {
			sessCookie = c
			break
		}
	}
	if sessCookie == nil || sessCookie.Value == "" {
		t.Fatal("session cookie not set")
	}

	// Session row's amr must be exactly ["federated"].
	if len(h.q.sessions) != 1 {
		t.Fatalf("sessions inserted: want 1, got %d", len(h.q.sessions))
	}
	amr := h.q.sessions[0].Amr
	if len(amr) != 1 || amr[0] != "federated" {
		t.Errorf("session amr: want [federated], got %v", amr)
	}
}

func TestFederationCallback_UpstreamError(t *testing.T) {
	h := newFederationTestServer(t)

	q := url.Values{}
	q.Set("error", "access_denied")
	q.Set("error_description", "user denied consent")
	q.Set("state", "anything")
	q.Set("code", "")
	resp := h.hitCallback(t, "mockop", q)

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d", resp.StatusCode)
	}
	body := decodeErrBody(t, resp)
	if body.Code != "upstream_error" {
		t.Errorf("code: want upstream_error, got %q", body.Code)
	}
	if !strings.Contains(body.Message, "access_denied") {
		t.Errorf("message should embed upstream code; got %q", body.Message)
	}

	// Audit row emitted with reason=upstream_error.
	if len(h.q.events) == 0 {
		t.Fatal("no audit event recorded")
	}
	ev := h.q.events[len(h.q.events)-1]
	if ev.Factor != string(audit.FactorFederationOIDC) || ev.Event != audit.EventFail {
		t.Errorf("audit factor/event: got %s/%s", ev.Factor, ev.Event)
	}
	var detail map[string]any
	if err := json.Unmarshal(ev.Detail, &detail); err != nil {
		t.Fatalf("decode audit detail: %v", err)
	}
	if detail["reason"] != "upstream_error" {
		t.Errorf("audit reason: got %v", detail["reason"])
	}
	if detail["upstream_code"] != "access_denied" {
		t.Errorf("audit upstream_code: got %v", detail["upstream_code"])
	}
}

func TestFederationCallback_MissingStateOrCode(t *testing.T) {
	h := newFederationTestServer(t)

	q := url.Values{}
	q.Set("state", "")
	q.Set("code", "abc")
	resp := h.hitCallback(t, "mockop", q)

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status: want 401, got %d", resp.StatusCode)
	}
	body := decodeErrBody(t, resp)
	if body.Code != "federation_state_invalid" {
		t.Errorf("code: want federation_state_invalid, got %q", body.Code)
	}
	// No audit row — stray browser hits must not flood the log.
	for _, ev := range h.q.events {
		if ev.Event == audit.EventFail {
			t.Errorf("unexpected fail audit row on missing-state/code: %+v", ev)
		}
	}
}

func TestFederationCallback_EmailNotVerifiedRejected(t *testing.T) {
	h := newFederationTestServer(t)
	// Upstream returns email_verified=false; require_verified_email=true → 403.
	h.op.SetClaims("sub-1", "alice@example.com", false, "alice", "Alice Example")

	loc, resp := h.driveLogin(t, "mockop", "/me")
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("login: want 302, got %d", resp.StatusCode)
	}
	code, state, iss := driveAuthorize(t, loc)
	q := url.Values{}
	q.Set("code", code)
	q.Set("state", state)
	q.Set("iss", iss)
	resp = h.hitCallback(t, "mockop", q)

	if resp.StatusCode != http.StatusForbidden {
		body, _ := readAll(resp.Body)
		t.Fatalf("status: want 403, got %d (body=%s)", resp.StatusCode, body)
	}
	body := decodeErrBody(t, resp)
	if body.Code != "email_not_verified" {
		t.Errorf("code: want email_not_verified, got %q", body.Code)
	}
	if len(h.q.sessions) != 0 {
		t.Errorf("sessions: want 0 on email-not-verified rejection, got %d", len(h.q.sessions))
	}
}

func TestFederationCallback_UsernameCollision(t *testing.T) {
	h := newFederationTestServer(t)
	// Pre-seed a local account with the same username the upstream returns.
	// The federator's auto_provision mode looks up by username before insert
	// and bails on a collision.
	h.q.accountByUsername["alice"] = db.Account{
		ID: 999, Username: "alice", DisplayName: "Local Alice",
	}
	h.q.usernameErr = nil

	loc, resp := h.driveLogin(t, "mockop", "/me")
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("login: want 302, got %d", resp.StatusCode)
	}
	code, state, iss := driveAuthorize(t, loc)
	q := url.Values{}
	q.Set("code", code)
	q.Set("state", state)
	q.Set("iss", iss)
	resp = h.hitCallback(t, "mockop", q)

	if resp.StatusCode != http.StatusForbidden {
		body, _ := readAll(resp.Body)
		t.Fatalf("status: want 403, got %d (body=%s)", resp.StatusCode, body)
	}
	body := decodeErrBody(t, resp)
	if body.Code != "username_collision" {
		t.Errorf("code: want username_collision, got %q", body.Code)
	}
}

func TestFederationLogin_RateLimited(t *testing.T) {
	h := newFederationTestServer(t)

	// Use a directly-invoked handler with a stable RemoteAddr so all hits
	// share the rate-limit bucket. The bucket is 30/min per IP; the 31st
	// must 429.
	for i := 0; i < 30; i++ {
		req := httptest.NewRequest(http.MethodGet,
			"/api/prohibitorum/auth/federation/mockop/login", nil)
		req.RemoteAddr = "10.1.2.3:5555"
		// chi.URLParam needs a route context; populate it directly.
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("slug", "mockop")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()
		h.s.handleFederationLoginHTTP(w, req)
		if w.Code == http.StatusTooManyRequests {
			t.Fatalf("hit %d returned 429 prematurely (want >= 30 allowed)", i+1)
		}
	}

	// 31st request: should be rate-limited.
	req := httptest.NewRequest(http.MethodGet,
		"/api/prohibitorum/auth/federation/mockop/login", nil)
	req.RemoteAddr = "10.1.2.3:5555"
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("slug", "mockop")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	h.s.handleFederationLoginHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status: want 429, got %d (body=%s)", w.Code, w.Body.String())
	}
	var body errBody
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Code != "rate_limited" {
		t.Errorf("code: want rate_limited, got %q", body.Code)
	}
}

func TestValidateFederationReturnTo(t *testing.T) {
	s := &Server{}
	cases := []struct {
		in       string
		want     string
		wantErr  bool
	}{
		{"", "/", false},
		{"/", "/", false},
		{"/me", "/me", false},
		{"/me/identities?ok=1", "/me/identities?ok=1", false},
		{"//evil.example.com", "", true},
		{"https://evil.example.com/", "", true},
		{"http://evil.example.com/", "", true},
		{"javascript:alert(1)", "", true},
		{"evil", "", true},
	}
	for _, c := range cases {
		got, err := s.validateFederationReturnTo(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("validateFederationReturnTo(%q): want error, got %q", c.in, got)
				continue
			}
			if ae := authn.AsAuthError(err); ae == nil || ae.Code != "invalid_return_to" {
				t.Errorf("validateFederationReturnTo(%q): want invalid_return_to, got %v", c.in, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("validateFederationReturnTo(%q): unexpected error %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("validateFederationReturnTo(%q): want %q, got %q", c.in, c.want, got)
		}
	}
}

// --- shared utilities ------------------------------------------------------

// readAll is a thin alias used by failure-path messages.
func readAll(r io.Reader) ([]byte, error) { return io.ReadAll(r) }
