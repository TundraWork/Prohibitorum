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
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/cmd/smoke/mockop"
	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
	fedoidc "prohibitorum/pkg/federation"
	federationoidc "prohibitorum/pkg/federation/providers/oidc"
	federationsteam "prohibitorum/pkg/federation/providers/steam"
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
	idpSlugErr error

	// account_identity (auto-provision: ErrNoRows on first lookup)
	identityResult db.AccountIdentity
	identityErr    error

	// account
	accountByIDResults map[int32]db.Account
	accountByUsername  map[string]db.Account
	usernameErr        error
	nextAccountID      int32
	insertedAccounts   []db.Account

	// account_identity inserts
	nextIdentityID  int64
	insertIdentitys []db.InsertAccountIdentityParams

	// confirmedIdentityID records the id passed to ConfirmAccountIdentity
	// (0 = never called). Invite redemption auto-confirms the just-inserted
	// identity in-tx via the federation modes layer.
	confirmedIdentityID int64

	// session
	sessions []db.Session

	// audit
	events []db.InsertCredentialEventParams

	// enrollment (populated by handle_invite_federation_test.go via seedEnrollment;
	// declared here so the field lives on the shared fake type).
	enrollmentByToken map[string]db.Enrollment

	// listIDPs is used by fakeFedQueries.ListUpstreamIDPs (see below).
	listIDPs    []db.UpstreamIdp
	listIDPsErr error

	// iconEtags is used by fakeFedQueries.ListEntityIconEtags (see below).
	iconEtags []db.ListEntityIconEtagsRow
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
	if f.idpSlugErr != nil {
		return db.UpstreamIdp{}, f.idpSlugErr
	}
	if v, ok := f.idpBySlug[slug]; ok {
		return v, nil
	}
	return db.UpstreamIdp{}, pgx.ErrNoRows
}

func (f *fakeFedQueries) GetUpstreamIDPBySlugAny(ctx context.Context, slug string) (db.UpstreamIdp, error) {
	return f.GetUpstreamIDPBySlug(ctx, slug)
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

func (f *fakeFedQueries) ConfirmAccountIdentity(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.confirmedIdentityID = id
	return nil
}

func (f *fakeFedQueries) UpdateAccountDisplayName(_ context.Context, _ db.UpdateAccountDisplayNameParams) error {
	return nil
}

func (f *fakeFedQueries) UpdateAccountIdentityEmail(_ context.Context, _ db.UpdateAccountIdentityEmailParams) error {
	return nil
}

func (f *fakeFedQueries) UpdateAccountEmail(_ context.Context, _ db.UpdateAccountEmailParams) error {
	return nil
}

func (f *fakeFedQueries) InsertSession(_ context.Context, arg db.InsertSessionParams) (db.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	row := db.Session{
		ID:            arg.ID,
		AccountID:     arg.AccountID,
		AuthTime:      arg.AuthTime,
		Amr:           arg.Amr,
		UpstreamIdpID: arg.UpstreamIdpID,
	}
	f.sessions = append(f.sessions, row)
	return row, nil
}

func (f *fakeFedQueries) RevokeSession(_ context.Context, _ string) error             { return nil }
func (f *fakeFedQueries) RevokeAllSessionsByAccount(_ context.Context, _ int32) error { return nil }

func (f *fakeFedQueries) InsertCredentialEvent(_ context.Context, arg db.InsertCredentialEventParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, arg)
	return nil
}

func (f *fakeFedQueries) ListUpstreamIDPs(_ context.Context) ([]db.UpstreamIdp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.listIDPs, f.listIDPsErr
}

func (f *fakeFedQueries) ListEntityIconEtags(_ context.Context, _ string) ([]db.ListEntityIconEtagsRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.iconEtags, nil
}


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

	// client carries a cookie jar so the federation anti-forgery cookie set at
	// /login (N4) is replayed on the /callback hop, mimicking a real browser.
	// CheckRedirect is disabled so each 302 stays observable.
	client *http.Client

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
	sealed, err := fedoidc.SealProviderSecret(fedTestDEK, []byte("test-secret"), idpID, keyVersion)
	if err != nil {
		t.Fatalf("EncryptClientSecret: %v", err)
	}
	idp := db.UpstreamIdp{
		ID:                   idpID,
		Slug:                 "mockop",
		DisplayName:          "Mock OP",
		IssuerUrl:            opTS.URL,
		ClientID:             "test-client",
		ClientSecretEnc:      sealed.Ciphertext,
		SecretNonce:          sealed.Nonce,
		KeyVersion:           keyVersion,
		Scopes:               []string{"openid", "profile", "email"},
		Mode:                 fedoidc.ModeAutoProvision,
		Protocol:             federationoidc.Protocol,
		PictureClaim:         "picture",
		RequireVerifiedEmail: true,
		// Schema defaults from migration 004 — DB-side DEFAULTs don't apply
		// to in-memory fakes, so seed them explicitly so modes.go can
		// resolve the per-IdP claim names via ClaimString(...).
		UsernameClaim:    "preferred_username",
		DisplayNameClaim: "name",
		EmailClaim:       "email",
		AllowPrivateNetwork: true, // mock OP is on loopback
	}
	q := newFakeFedQueries()
	q.idpBySlug[idp.Slug] = idp

	// 3. KV + audit + sessionStore.
	kvStore := kv.NewMemoryStore()
	t.Cleanup(func() { _ = kvStore.Close() })
	auditWriter := audit.NewWriter(q)

	cfg := &configx.Config{
		SessionTTL: time.Hour,
	}
	sessionStore := sessstore.NewSessionStore(kvStore, q, cfg.SessionTTL)

	fedCfg := configx.FederationConfig{StateTTL: 5 * time.Minute}
	deks := map[int][]byte{1: fedTestDEK}

	// 5. Mount handlers behind a chi router served by httptest.
	s := &Server{
		config:       cfg,
		kvStore:      kvStore,
		sessionStore: sessionStore,
		rateLimiter:  authn.NewRateLimiter(),
		Audit:        auditWriter,
	}
	// The confirm endpoints reach through s.queries via confirmFedQ(); the fake
	// satisfies that narrow surface (GetAccountByID/GetUpstreamIDPBySlug/
	// ConfirmAccountIdentity), so point the override at it.
	s.confirmFedOverride = q
	r := chi.NewRouter()
	r.Get("/api/prohibitorum/auth/federation/{slug}/login", s.handleFederationLoginHTTP)
	r.Get("/api/prohibitorum/auth/federation/{slug}/callback", s.handleFederationCallbackHTTP)
	r.Get("/api/prohibitorum/auth/federation/confirm", s.handleFederationConfirmGet)
	r.Post("/api/prohibitorum/auth/federation/confirm", s.handleFederationConfirmPost)
	r.Post("/api/prohibitorum/auth/federation/confirm/decline", s.handleFederationConfirmDecline)
	srvTS := httptest.NewServer(r)
	t.Cleanup(srvTS.Close)

	// Now we know our own origin — federator can build redirect_uri's that
	// the test http.Client can dial back into.
	s.federationService = newTestFederationService(t, q, kvStore, auditWriter, deks, srvTS.URL, fedCfg.StateTTL)
	cfg.PublicOrigins = []string{srvTS.URL}

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	client := &http.Client{
		Jar: jar,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	return &fedTestHarness{
		t:      t,
		op:     op,
		opTS:   opTS,
		idp:    idp,
		q:      q,
		s:      s,
		srvTS:  srvTS,
		origin: srvTS.URL,
		client: client,
	}
}

type serverSteamAdapter struct{}

func (serverSteamAdapter) Protocol() string {
	return federationsteam.Protocol
}

func (serverSteamAdapter) Begin(_ context.Context, _ fedoidc.Provider, begin fedoidc.BeginContext) (json.RawMessage, fedoidc.NextAction, error) {
	state := json.RawMessage(`{"step":"steam"}`)
	action := fedoidc.NextAction{
		Kind: fedoidc.ActionRedirect,
		URL:  "https://steam.test/openid?state=" + url.QueryEscape(begin.FlowID),
	}
	return state, action, nil
}

func (serverSteamAdapter) Advance(context.Context, fedoidc.Provider, json.RawMessage, fedoidc.ActionInput) (fedoidc.AdvanceResult, error) {
	return fedoidc.AdvanceResult{Identity: &fedoidc.VerifiedIdentity{
		Issuer: federationsteam.Issuer, Subject: "76561198000000000",
		Username: "steam_76561198000000000", DisplayName: "Steam User",
		AMR: []string{"steam"}, AvatarURL: "https://cdn.test/steam-avatar.jpg",
	}}, nil
}

type serverAvatarRecorder struct {
	calls    int
	account  int32
	provider fedoidc.Provider
	url      string
}

func (r *serverAvatarRecorder) Inherit(accountID int32, provider fedoidc.Provider, avatarURL string) {
	r.calls++
	r.account = accountID
	r.provider = provider
	r.url = avatarURL
}

func (*serverAvatarRecorder) Pending(context.Context, int32) bool {
	return false
}

func newTestFederationService(t *testing.T, q *fakeFedQueries, store kv.Store, writer audit.Writer, deks map[int][]byte, origin string, ttl time.Duration) *fedoidc.Service {
	t.Helper()
	registry := fedoidc.NewRegistry()
	adapter := federationoidc.NewAdapter(fedoidc.NewSecretStore(deks))
	if err := registry.RegisterDefinition(federationoidc.Definition{}); err != nil { t.Fatal(err) }
	if err := registry.RegisterAdapter(adapter); err != nil { t.Fatal(err) }
	if err := registry.RegisterDefinition(federationsteam.Definition{}); err != nil { t.Fatal(err) }
	if err := registry.RegisterAdapter(serverSteamAdapter{}); err != nil { t.Fatal(err) }
	service := fedoidc.NewService(registry, fedoidc.NewProviderStore(q), store, fedoidc.NewResolver(q, writer, nil), fedoidc.ServiceConfig{StateTTL: ttl, PublicOrigin: origin, Audit: writer})
	service.SetAvatarManager(fedoidc.NewAvatarManager(q, store))
	return service
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
	resp, err := h.client.Get(u)
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
	resp, err := h.client.Get(u)
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
	if _, err := h.s.kvStore.Get(context.Background(), fedoidc.FlowKey(state)); err != nil {
		t.Errorf("state not stashed under LoginKey: %v", err)
	}
}

func TestFederationOIDCStateRetainsSecurityBindings(t *testing.T) {
	h := newFederationTestServer(t)
	loc, resp := h.driveLogin(t, "mockop", "/me")
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}
	authorizeURL, err := url.Parse(loc)
	if err != nil {
		t.Fatal(err)
	}
	stateToken := authorizeURL.Query().Get("state")
	raw, err := h.s.kvStore.Get(context.Background(), fedoidc.FlowKey(stateToken))
	if err != nil {
		t.Fatal(err)
	}
	state, err := fedoidc.DecodeFlowState(raw)
	if err != nil {
		t.Fatal(err)
	}
	var private struct {
		CallbackURL  string `json:"callbackUrl"`
		ExpectedIss  string `json:"expectedIssuer"`
		TokenURL     string `json:"tokenEndpoint"`
		Nonce        string `json:"nonce"`
		CodeVerifier string `json:"codeVerifier"`
	}
	if err := json.Unmarshal(state.AdapterState, &private); err != nil {
		t.Fatal(err)
	}
	challengeDigest := sha256.Sum256([]byte(private.CodeVerifier))
	wantChallenge := base64.RawURLEncoding.EncodeToString(challengeDigest[:])
	if private.CallbackURL != h.srvTS.URL+"/api/prohibitorum/auth/federation/mockop/callback" ||
		private.ExpectedIss != h.opTS.URL || private.TokenURL != h.opTS.URL+"/token" ||
		private.Nonce == "" || private.Nonce != authorizeURL.Query().Get("nonce") ||
		private.CodeVerifier == "" || wantChallenge != authorizeURL.Query().Get("code_challenge") {
		t.Fatalf("OIDC private state/action mismatch: state=%+v authorize=%v", private, authorizeURL.Query())
	}
	var browserToken string
	for _, cookie := range resp.Cookies() {
		if cookie.Name == sessstore.FedStateCookieName {
			browserToken = cookie.Value
			break
		}
	}
	if !fedoidc.BrowserBindingOK(state.BrowserDigest, browserToken) ||
		state.CurrentAction.Kind != fedoidc.ActionRedirect || state.CurrentAction.URL != loc {
		t.Fatalf("browser/action binding mismatch: state=%+v cookieSet=%v", state, browserToken != "")
	}
}


func TestFederationLogin_InvalidReturnTo(t *testing.T) {
	h := newFederationTestServer(t)

	_, resp := h.driveLogin(t, "mockop", "https://evil.example.com/")
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status: want 302, got %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, "/error?error=invalid_return_to&ref=") {
		t.Errorf("Location: want /error?error=invalid_return_to&ref=…, got %q", loc)
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
	blob, err := h.s.kvStore.Get(context.Background(), fedoidc.FlowKey(state))
	if err != nil {
		t.Fatalf("state not in KV: %v", err)
	}
	fs, err := fedoidc.DecodeFlowState(blob)
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
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status: want 302, got %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, "/error?error=federation_state_invalid&ref=") {
		t.Errorf("Location: want /error?error=federation_state_invalid&ref=…, got %q", loc)
	}
}

func TestFederationLogin_LookupFailureReturnsStateInvalid(t *testing.T) {
	h := newFederationTestServer(t)
	h.q.idpSlugErr = errors.New("database unavailable")

	_, resp := h.driveLogin(t, "mockop", "/me")
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status: want 302, got %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, "/error?error=federation_state_invalid&ref=") {
		t.Fatalf("Location = %q, want opaque federation_state_invalid redirect", loc)
	}
	if strings.Contains(loc, "server_error") {
		t.Fatalf("Location exposed provider-store failure as server_error: %q", loc)
	}
}

// seedConfirmedIdentity makes the harness's upstream subject ("sub-1") resolve
// to an EXISTING, confirmed local account — exercising the re-login path that
// issues a durable session immediately (Confirmed=true). Without this, a fresh
// auto-provision yields Confirmed=false and the callback parks the user on
// /welcome (see TestFederationCallback_UnconfirmedRedirectsToWelcome).
func seedSteamProvider(t *testing.T, h *fedTestHarness) db.UpstreamIdp {
	t.Helper()
	const providerID int64 = 99
	sealed, err := fedoidc.SealProviderSecret(fedTestDEK, []byte("steam-api-key"), providerID, 1)
	if err != nil {
		t.Fatal(err)
	}
	provider := db.UpstreamIdp{
		ID: providerID, Slug: "steam", DisplayName: "Steam",
		Protocol: federationsteam.Protocol, Mode: fedoidc.ModeAutoProvision,
		ClientSecretEnc: sealed.Ciphertext, SecretNonce: sealed.Nonce, KeyVersion: 1,
		UsernameClaim: "preferred_username", DisplayNameClaim: "name",
		EmailClaim: "email", PictureClaim: "picture",
	}
	h.q.idpBySlug[provider.Slug] = provider
	return provider
}

func seedConfirmedIdentity(h *fedTestHarness) {
	const acctID int32 = 777
	h.q.accountByIDResults[acctID] = db.Account{
		ID: acctID, Username: "alice", DisplayName: "Alice Example",
	}
	h.q.identityErr = nil
	h.q.identityResult = db.AccountIdentity{
		ID:            555,
		AccountID:     acctID,
		UpstreamIdpID: h.idp.ID,
		UpstreamIss:   h.opTS.URL,
		UpstreamSub:   "sub-1",
		ConfirmedAt:   pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}
}

func TestFederationCallback_HappyPath(t *testing.T) {
	h := newFederationTestServer(t)
	seedConfirmedIdentity(h)

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

	// Re-login of a confirmed identity inserts NOTHING (no account, no identity);
	// it issues a session directly.
	if len(h.q.insertedAccounts) != 0 {
		t.Errorf("accounts inserted: want 0 (re-login), got %d", len(h.q.insertedAccounts))
	}
	if len(h.q.insertIdentitys) != 0 {
		t.Errorf("identities inserted: want 0 (re-login), got %d", len(h.q.insertIdentitys))
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

func TestFederationCallback_DisabledExistingAccountRejectsWithoutSession(t *testing.T) {
	h := newFederationTestServer(t)
	seedConfirmedIdentity(h)
	account := h.q.accountByIDResults[777]
	account.Disabled = true
	h.q.accountByIDResults[777] = account

	loc, resp := h.driveLogin(t, "mockop", "/me")
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("login status = %d, want 302", resp.StatusCode)
	}
	code, state, iss := driveAuthorize(t, loc)
	q := url.Values{}
	q.Set("code", code)
	q.Set("state", state)
	q.Set("iss", iss)
	resp = h.hitCallback(t, "mockop", q)

	if resp.StatusCode != http.StatusFound ||
		!strings.HasPrefix(resp.Header.Get("Location"), "/error?error=bad_credentials&ref=") {
		t.Fatalf("callback status/location = %d %q, want bad_credentials redirect", resp.StatusCode, resp.Header.Get("Location"))
	}
	if len(h.q.sessions) != 0 {
		t.Fatalf("disabled account inserted %d sessions", len(h.q.sessions))
	}
	var disabledAudit bool
	for _, event := range h.q.events {
		if event.Event != audit.EventFail {
			continue
		}
		var detail map[string]any
		if err := json.Unmarshal(event.Detail, &detail); err != nil {
			t.Fatal(err)
		}
		if detail["reason"] == "account_disabled" {
			disabledAudit = true
			break
		}
	}
	if !disabledAudit {
		t.Fatal("disabled-account failure audit missing")
	}
}

func TestFederationCallback_AllowsMissingAuthorizationResponseIssuer(t *testing.T) {
	h := newFederationTestServer(t)
	seedConfirmedIdentity(h)
	loc, resp := h.driveLogin(t, "mockop", "/me")
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("login status = %d, want 302", resp.StatusCode)
	}
	code, state, _ := driveAuthorize(t, loc)
	q := url.Values{}
	q.Set("code", code)
	q.Set("state", state)

	resp = h.hitCallback(t, "mockop", q)
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/me" {
		t.Fatalf("callback status/location = %d %q, want 302 /me", resp.StatusCode, resp.Header.Get("Location"))
	}
	if len(h.q.sessions) != 1 {
		t.Fatalf("sessions inserted = %d, want 1", len(h.q.sessions))
	}
}


// TestFederationCallback_PersistsUpstreamIdpID guards H1-sch: the federation
// callback must stamp the upstream IdP's id onto the session row so the OIDC
// OP can later surface a "federated" discriminator in id_token claims.
func TestFederationCallback_PersistsUpstreamIdpID(t *testing.T) {
	h := newFederationTestServer(t)
	seedConfirmedIdentity(h)

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
		t.Fatalf("callback: want 302, got %d (body=%s)", resp.StatusCode, body)
	}
	if len(h.q.sessions) != 1 {
		t.Fatalf("sessions inserted: want 1, got %d", len(h.q.sessions))
	}
	got := h.q.sessions[0].UpstreamIdpID
	if got == nil {
		t.Fatalf("session.UpstreamIdpID: want non-nil, got nil")
	}
	if *got != h.idp.ID {
		t.Errorf("session.UpstreamIdpID: want %d, got %d", h.idp.ID, *got)
	}
}

func TestFederationCallback_BackfillsAMRWhenUpstreamOmits(t *testing.T) {
	h := newFederationTestServer(t)
	seedConfirmedIdentity(h)
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

	// Browser-navigated error path now redirects to SPA /error page.
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status: want 302, got %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, "/error?error=upstream_error&ref=") {
		t.Errorf("Location: want /error?error=upstream_error&ref=…, got %q", loc)
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

func TestFederationCallback_InvalidAuthorizationCodeIsStateInvalid(t *testing.T) {
	h := newFederationTestServer(t)
	loc, resp := h.driveLogin(t, "mockop", "/me")
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("login status = %d, want 302", resp.StatusCode)
	}
	authorizeURL, err := url.Parse(loc)
	if err != nil {
		t.Fatal(err)
	}
	state := authorizeURL.Query().Get("state")

	q := url.Values{}
	q.Set("code", "not-issued-by-the-op")
	q.Set("state", state)
	resp = h.hitCallback(t, "mockop", q)

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("callback status = %d, want 302", resp.StatusCode)
	}
	if got := resp.Header.Get("Location"); !strings.HasPrefix(got, "/error?error=federation_state_invalid&ref=") {
		t.Fatalf("Location = %q, want federation_state_invalid redirect", got)
	}
	if _, err := h.s.kvStore.Get(context.Background(), fedoidc.FlowKey(state)); err != nil {
		t.Fatalf("failed callback did not restore flow: %v", err)
	}
}

func TestFederationCallback_MissingStateOrCode(t *testing.T) {
	h := newFederationTestServer(t)

	q := url.Values{}
	q.Set("state", "")
	q.Set("code", "abc")
	resp := h.hitCallback(t, "mockop", q)

	// Browser-navigated error path now redirects to SPA /error page.
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status: want 302, got %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, "/error?error=federation_state_invalid&ref=") {
		t.Errorf("Location: want /error?error=federation_state_invalid&ref=…, got %q", loc)
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
	// Upstream returns email_verified=false; require_verified_email=true → /error redirect.
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

	// Browser-navigated error path now redirects to SPA /error page.
	if resp.StatusCode != http.StatusFound {
		body, _ := readAll(resp.Body)
		t.Fatalf("status: want 302, got %d (body=%s)", resp.StatusCode, body)
	}
	callbackLoc := resp.Header.Get("Location")
	if !strings.HasPrefix(callbackLoc, "/error?error=email_not_verified&ref=") {
		t.Errorf("Location: want /error?error=email_not_verified&ref=…, got %q", callbackLoc)
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

	// Browser-navigated error path now redirects to SPA /error page.
	if resp.StatusCode != http.StatusFound {
		body, _ := readAll(resp.Body)
		t.Fatalf("status: want 302, got %d (body=%s)", resp.StatusCode, body)
	}
	callbackLoc := resp.Header.Get("Location")
	if !strings.HasPrefix(callbackLoc, "/error?error=username_collision&ref=") {
		t.Errorf("Location: want /error?error=username_collision&ref=…, got %q", callbackLoc)
	}
}

func TestValidateFederationReturnTo(t *testing.T) {
	s := &Server{}
	cases := []struct {
		in      string
		want    string
		wantErr bool
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
		// Normalization: the returned path is built from parsed components, not
		// returned verbatim — raw whitespace in the path is percent-encoded.
		{"/path with space", "/path%20with%20space", false},
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

// --- handleListFederationProvidersHTTP tests ----------------------------------

// newListFedServer builds a minimal Server with a fakeFedQueries whose
// ListUpstreamIDPs is pre-configured. No federator, KV, or session store
// needed — the list handler only calls listFedQ().
func newListFedServer(q *fakeFedQueries) *Server {
	return &Server{
		listFedOverride: q,
	}
}

func TestListFederationProviders_TwoIDPs(t *testing.T) {
	q := newFakeFedQueries()
	q.listIDPs = []db.UpstreamIdp{
		{Slug: "google", DisplayName: "Google"},
		{Slug: "github", DisplayName: "GitHub"},
	}

	s := newListFedServer(q)
	req := httptest.NewRequest(http.MethodGet, "/api/prohibitorum/auth/federation", nil)
	w := httptest.NewRecorder()
	s.handleListFederationProvidersHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: want 200, got %d", resp.StatusCode)
	}

	var out []contract.FederationProvider
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("providers: want 2, got %d", len(out))
	}
	// Verify slug + displayName pass through correctly.
	for _, want := range []struct{ slug, name string }{
		{"google", "Google"},
		{"github", "GitHub"},
	} {
		found := false
		for _, p := range out {
			if p.Slug == want.slug && p.DisplayName == want.name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("provider {%s %s} not found in %+v", want.slug, want.name, out)
		}
	}
}

func TestListFederationProviders_OmitsInviteOnly(t *testing.T) {
	// invite_only IdPs are reachable only via an invite link, so they must not
	// appear as generic "sign in with" buttons. auto_provision + link_only do.
	q := newFakeFedQueries()
	q.listIDPs = []db.UpstreamIdp{
		{Slug: "google", DisplayName: "Google", Mode: fedoidc.ModeAutoProvision},
		{Slug: "okta", DisplayName: "Okta", Mode: fedoidc.ModeLinkOnly},
		{Slug: "invite", DisplayName: "Invite Co", Mode: fedoidc.ModeInviteOnly},
	}

	s := newListFedServer(q)
	req := httptest.NewRequest(http.MethodGet, "/api/prohibitorum/auth/federation", nil)
	w := httptest.NewRecorder()
	s.handleListFederationProvidersHTTP(w, req)

	var out []contract.FederationProvider
	if err := json.NewDecoder(w.Result().Body).Decode(&out); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("providers: want 2 (auto_provision + link_only), got %d: %+v", len(out), out)
	}
	for _, p := range out {
		if p.Slug == "invite" {
			t.Errorf("invite_only IdP must not appear as a sign-in button: %+v", out)
		}
	}
}

func TestListFederationProviders_ZeroIDPs(t *testing.T) {
	q := newFakeFedQueries()
	// listIDPs is nil by default; handler must emit [] not null.

	s := newListFedServer(q)
	req := httptest.NewRequest(http.MethodGet, "/api/prohibitorum/auth/federation", nil)
	w := httptest.NewRecorder()
	s.handleListFederationProvidersHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: want 200, got %d", resp.StatusCode)
	}

	rawBody, _ := io.ReadAll(resp.Body)
	// Trim trailing newline added by json.Encoder.
	trimmed := strings.TrimSpace(string(rawBody))
	if trimmed != "[]" {
		t.Errorf("body: want %q, got %q (must be [] not null)", "[]", trimmed)
	}

	// Also verify the decoded slice has length 0 (not nil-decoded).
	var out []contract.FederationProvider
	if err := json.Unmarshal([]byte(trimmed), &out); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("providers: want 0, got %d", len(out))
	}
}

func TestListFederationProviders_QueryError(t *testing.T) {
	q := newFakeFedQueries()
	q.listIDPsErr = fmt.Errorf("db unavailable")

	s := newListFedServer(q)
	req := httptest.NewRequest(http.MethodGet, "/api/prohibitorum/auth/federation", nil)
	w := httptest.NewRecorder()
	s.handleListFederationProvidersHTTP(w, req)

	resp := w.Result()
	// writeAuthErr maps a plain fmt.Errorf (non-*authn.AuthError) to 500.
	if resp.StatusCode != http.StatusInternalServerError {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: want 500, got %d (body=%s)", resp.StatusCode, body)
	}
}

func TestListFederationProviders_IconURL(t *testing.T) {
	q := newFakeFedQueries()
	q.listIDPs = []db.UpstreamIdp{
		{Slug: "google", DisplayName: "Google"},
		{Slug: "github", DisplayName: "GitHub"},
	}
	// Only google has an icon etag.
	q.iconEtags = []db.ListEntityIconEtagsRow{
		{OwnerID: "google", Etag: "abc12345"},
	}

	s := newListFedServer(q)
	req := httptest.NewRequest(http.MethodGet, "/api/prohibitorum/auth/federation", nil)
	w := httptest.NewRecorder()
	s.handleListFederationProvidersHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: want 200, got %d", resp.StatusCode)
	}

	var out []contract.FederationProvider
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("providers: want 2, got %d", len(out))
	}

	for _, p := range out {
		switch p.Slug {
		case "google":
			if p.IconURL == nil {
				t.Errorf("google: IconURL should be non-nil (icon exists)")
			} else if !strings.Contains(*p.IconURL, "/icon/upstream_idp/google") {
				t.Errorf("google: IconURL %q does not contain expected path", *p.IconURL)
			} else if !strings.Contains(*p.IconURL, "?v=abc12345") {
				t.Errorf("google: IconURL %q does not contain expected cache-bust param ?v=abc12345", *p.IconURL)
			}
		case "github":
			if p.IconURL != nil {
				t.Errorf("github: IconURL should be nil (no icon), got %q", *p.IconURL)
			}
		default:
			t.Errorf("unexpected provider slug %q", p.Slug)
		}
	}
}

// --- shared utilities ------------------------------------------------------

// readAll is a thin alias used by failure-path messages.
func readAll(r io.Reader) ([]byte, error) { return io.ReadAll(r) }
