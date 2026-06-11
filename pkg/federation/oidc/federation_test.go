package oidc_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/cmd/smoke/mockop"
	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/db"
	federationoidc "prohibitorum/pkg/federation/oidc"
	"prohibitorum/pkg/kv"
)

// fakeFederatorQueries extends fakeModesQueries (modes_test.go) with the two
// extra methods Federator needs: GetUpstreamIDPBySlug + ListAccountIdentitiesByAccount.
// The login-flow callbacks re-look-up the idp by slug after Pop'ing state.
type fakeFederatorQueries struct {
	*fakeModesQueries

	mu         sync.Mutex
	idpBySlug  map[string]db.UpstreamIdp
	idpSlugErr error

	identitiesByAccount    map[int32][]db.ListAccountIdentitiesByAccountRow
	identitiesByAccountErr error

	enrollmentByToken    map[string]db.Enrollment
	enrollmentByTokenErr error
}

func newFakeFederatorQueries() *fakeFederatorQueries {
	return &fakeFederatorQueries{
		fakeModesQueries:    newFakeModesQueries(),
		idpBySlug:           map[string]db.UpstreamIdp{},
		identitiesByAccount: map[int32][]db.ListAccountIdentitiesByAccountRow{},
		enrollmentByToken:   map[string]db.Enrollment{},
	}
}

func (f *fakeFederatorQueries) GetUpstreamIDPBySlug(_ context.Context, slug string) (db.UpstreamIdp, error) {
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

func (f *fakeFederatorQueries) ListAccountIdentitiesByAccount(_ context.Context, id int32) ([]db.ListAccountIdentitiesByAccountRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.identitiesByAccountErr != nil {
		return nil, f.identitiesByAccountErr
	}
	return f.identitiesByAccount[id], nil
}

func (f *fakeFederatorQueries) GetEnrollmentByToken(_ context.Context, token string) (db.Enrollment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.enrollmentByTokenErr != nil {
		return db.Enrollment{}, f.enrollmentByTokenErr
	}
	if e, ok := f.enrollmentByToken[token]; ok {
		return e, nil
	}
	return db.Enrollment{}, pgx.ErrNoRows
}

// Compile-time guard: the fake must satisfy the interface the Federator wants.
var _ federationoidc.FederatorQueries = (*fakeFederatorQueries)(nil)

// --- fixture helpers -------------------------------------------------------

// testDEK is a deterministic 32-byte AES-256 key for the test ciphertexts.
var testDEK = bytes.Repeat([]byte{0x11}, 32)

// fixtureFederator wires a fresh Federator against a mock OP, a memory KV,
// a recording audit, and a fake querier seeded with one upstream_idp row.
type fixtureFederator struct {
	t      *testing.T
	op     *mockop.Server
	ts     *httptest.Server
	idp    db.UpstreamIdp
	q      *fakeFederatorQueries
	kvm    *kv.MemoryStore
	au     *recordingAudit
	f      *federationoidc.Federator
	cfg    configx.FederationConfig
	origin string
}

// newFixture builds the standard test environment. `mode` selects the
// upstream_idp.mode policy. The IdP slug is always "mockop".
func newFixture(t *testing.T, mode string) *fixtureFederator {
	t.Helper()

	op, err := mockop.New("")
	if err != nil {
		t.Fatalf("mockop.New: %v", err)
	}
	ts := httptest.NewServer(op.Routes())
	op.SetBase(ts.URL)
	t.Cleanup(ts.Close)

	// Encrypt a known client-secret with the test DEK at key version 1.
	const idpID int64 = 42
	const keyVersion int32 = 1
	ct, nonce, err := federationoidc.EncryptClientSecret(testDEK, []byte("test-secret"), idpID, keyVersion)
	if err != nil {
		t.Fatalf("EncryptClientSecret: %v", err)
	}

	idp := db.UpstreamIdp{
		ID:                   idpID,
		Slug:                 "mockop",
		DisplayName:          "Mock OP",
		IssuerUrl:            ts.URL,
		ClientID:             "test-client",
		ClientSecretEnc:      ct,
		SecretNonce:          nonce,
		KeyVersion:           keyVersion,
		Scopes:               []string{"openid", "profile", "email"},
		Mode:                 mode,
		RequireVerifiedEmail: true,
		// Schema defaults from migration 004 — the fixture builds the row
		// in-memory so the DB-side DEFAULT clauses don't apply; pass them
		// explicitly so modes.go / LinkCallback can extract via ClaimString.
		UsernameClaim:    "preferred_username",
		DisplayNameClaim: "name",
		EmailClaim:       "email",
	}

	q := newFakeFederatorQueries()
	q.idpBySlug[idp.Slug] = idp

	kvm := kv.NewMemoryStore()
	t.Cleanup(func() { _ = kvm.Close() })

	au := &recordingAudit{}

	cfg := configx.FederationConfig{
		StateTTL:            5 * time.Minute,
		DefaultScopes:       []string{"openid", "profile", "email"},
		AllowPrivateNetwork: true, // mock OP is on loopback
	}
	deks := map[int][]byte{1: testDEK}
	origin := "https://idp.example.test"

	fd := federationoidc.NewFederator(q, kvm, au, cfg, deks, nil, origin)

	// Default claims the mock OP will mint into the next id_token.
	op.SetClaims("sub-1", "alice@example.com", true, "alice", "Alice Example")
	op.SetAMR([]string{"pwd", "mfa"})

	return &fixtureFederator{
		t: t, op: op, ts: ts, idp: idp, q: q, kvm: kvm, au: au,
		f: fd, cfg: cfg, origin: origin,
	}
}

// noRedirectClient: same as the one in client_test.go; duplicated here so
// federation_test.go is self-contained.
func noRedirectClientFed() *http.Client {
	return &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// driveAuthorize hits the upstream /authorize URL and extracts (code, state, iss)
// from the 302 Location. Uses noRedirectClient so the test can inspect the
// redirect instead of following it (the redirect target is a fake URL).
func driveAuthorizeFed(t *testing.T, authorizeURL string) (code, state, iss string) {
	t.Helper()
	resp, err := noRedirectClientFed().Get(authorizeURL)
	if err != nil {
		t.Fatalf("GET /authorize: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("authorize status = %d, want 302; body=%s", resp.StatusCode, body)
	}
	loc, err := resp.Location()
	if err != nil {
		t.Fatalf("authorize Location: %v", err)
	}
	q := loc.Query()
	return q.Get("code"), q.Get("state"), q.Get("iss")
}

// auditReason returns the reason on the first matching event, or "" if not found.
func auditReason(recs []audit.Record, event string) string {
	for _, r := range recs {
		if r.Event == event {
			if s, ok := r.Detail["reason"].(string); ok {
				return s
			}
		}
	}
	return ""
}

// --- tests -----------------------------------------------------------------

func TestFederator_BeginLogin_UnknownSlug(t *testing.T) {
	fx := newFixture(t, federationoidc.ModeAutoProvision)
	_, err := fx.f.BeginLogin(context.Background(), "no-such-slug", "/me")
	if !errors.Is(err, federationoidc.ErrUnknownIDP) {
		t.Fatalf("want ErrUnknownIDP, got %v", err)
	}
}

func TestFederator_BeginLogin_StashesStateUnderLoginKey(t *testing.T) {
	fx := newFixture(t, federationoidc.ModeAutoProvision)

	req, err := fx.f.BeginLogin(context.Background(), "mockop", "/me")
	if err != nil {
		t.Fatalf("BeginLogin: %v", err)
	}
	if req.StateKey == "" {
		t.Fatal("StateKey empty")
	}
	if req.AuthorizeURL == "" {
		t.Fatal("AuthorizeURL empty")
	}
	u, err := url.Parse(req.AuthorizeURL)
	if err != nil {
		t.Fatalf("parse AuthorizeURL: %v", err)
	}
	if !strings.HasPrefix(req.AuthorizeURL, fx.ts.URL+"/authorize") {
		t.Errorf("AuthorizeURL should point at mock OP /authorize, got %s", req.AuthorizeURL)
	}
	q := u.Query()
	if q.Get("code_challenge_method") != "S256" {
		t.Errorf("want S256, got %q", q.Get("code_challenge_method"))
	}
	if q.Get("code_challenge") == "" {
		t.Error("code_challenge empty")
	}
	if q.Get("state") != req.StateKey {
		t.Errorf("state in URL (%q) != returned StateKey (%q)", q.Get("state"), req.StateKey)
	}
	if q.Get("nonce") == "" {
		t.Error("nonce empty")
	}

	// State MUST live under LoginKey, NOT LinkKey.
	if _, err := fx.kvm.Get(context.Background(), federationoidc.LoginKey(req.StateKey)); err != nil {
		t.Fatalf("state missing from LoginKey: %v", err)
	}
	if _, err := fx.kvm.Get(context.Background(), federationoidc.LinkKey(req.StateKey)); err == nil {
		t.Fatal("state should NOT be present under LinkKey")
	}

	// Verify the FedState blob.
	blob, _ := fx.kvm.Get(context.Background(), federationoidc.LoginKey(req.StateKey))
	state, err := federationoidc.DecodeFedState(blob)
	if err != nil {
		t.Fatalf("DecodeFedState: %v", err)
	}
	if state.IDPSlug != "mockop" {
		t.Errorf("IDPSlug = %q, want mockop", state.IDPSlug)
	}
	if state.ExpectedIss != fx.ts.URL {
		t.Errorf("ExpectedIss = %q, want %q", state.ExpectedIss, fx.ts.URL)
	}
	if state.LinkingAccountID != nil {
		t.Errorf("LinkingAccountID should be nil for login flow, got %v", *state.LinkingAccountID)
	}
}

// TestFederator_LinkBegin_NoBrowserBinding pins OIDCFED-1: the link flow's
// handler never sets the anti-forgery cookie and its callback never checks it,
// so begin() must leave BrowserBinding EMPTY for a link flow rather than persist
// a value no code path can satisfy. The login flow keeps a non-empty binding.
func TestFederator_LinkBegin_NoBrowserBinding(t *testing.T) {
	fx := newFixture(t, federationoidc.ModeAutoProvision)

	lreq, err := fx.f.BeginLogin(context.Background(), "mockop", "/me")
	if err != nil {
		t.Fatalf("BeginLogin: %v", err)
	}
	lblob, _ := fx.kvm.Get(context.Background(), federationoidc.LoginKey(lreq.StateKey))
	lstate, err := federationoidc.DecodeFedState(lblob)
	if err != nil {
		t.Fatalf("DecodeFedState (login): %v", err)
	}
	if lstate.BrowserBinding == "" {
		t.Error("login flow: BrowserBinding empty, want non-empty")
	}

	req, err := fx.f.LinkBegin(context.Background(), 7, "mockop", "/connected")
	if err != nil {
		t.Fatalf("LinkBegin: %v", err)
	}
	blob, err := fx.kvm.Get(context.Background(), federationoidc.LinkKey(req.StateKey))
	if err != nil {
		t.Fatalf("link state missing from LinkKey: %v", err)
	}
	state, err := federationoidc.DecodeFedState(blob)
	if err != nil {
		t.Fatalf("DecodeFedState (link): %v", err)
	}
	if state.BrowserBinding != "" {
		t.Errorf("link flow: BrowserBinding = %q, want empty", state.BrowserBinding)
	}
	if state.LinkingAccountID == nil || *state.LinkingAccountID != 7 {
		t.Errorf("link flow: LinkingAccountID = %v, want 7", state.LinkingAccountID)
	}
}

func TestFederator_HandleCallback_HappyPath_AutoProvision(t *testing.T) {
	fx := newFixture(t, federationoidc.ModeAutoProvision)

	req, err := fx.f.BeginLogin(context.Background(), "mockop", "/me")
	if err != nil {
		t.Fatalf("BeginLogin: %v", err)
	}
	code, state, iss := driveAuthorizeFed(t, req.AuthorizeURL)
	if state != req.StateKey {
		t.Fatalf("state from authorize (%q) != BeginLogin (%q)", state, req.StateKey)
	}
	if iss != fx.ts.URL {
		t.Fatalf("iss from authorize = %q, want %q", iss, fx.ts.URL)
	}

	result, err := fx.f.HandleCallback(context.Background(), state, code, iss, req.AntiForgeryToken)
	if err != nil {
		t.Fatalf("HandleCallback: %v", err)
	}
	if !result.IsNew {
		t.Error("IsNew = false, want true")
	}
	if result.IDPSlug != "mockop" {
		t.Errorf("IDPSlug = %q", result.IDPSlug)
	}
	if result.ReturnTo != "/me" {
		t.Errorf("ReturnTo = %q", result.ReturnTo)
	}
	if len(result.AMR) != 2 || result.AMR[0] != "pwd" || result.AMR[1] != "mfa" {
		t.Errorf("AMR = %v, want [pwd mfa]", result.AMR)
	}

	// Account row + identity row created.
	if len(fx.q.insertedAccounts) != 1 {
		t.Fatalf("want 1 inserted account, got %d", len(fx.q.insertedAccounts))
	}
	if fx.q.insertedAccount.Username != "alice" {
		t.Errorf("username = %q, want alice", fx.q.insertedAccount.Username)
	}
	if fx.q.insertedIdentity.AccountID != result.AccountID {
		t.Errorf("identity account_id = %d, want %d", fx.q.insertedIdentity.AccountID, result.AccountID)
	}

	// Resolve emits Register + Use.
	recs := fx.au.snapshot()
	if findEvent(recs, audit.EventRegister) == nil {
		t.Error("missing audit Register")
	}
	if findEvent(recs, audit.EventUse) == nil {
		t.Error("missing audit Use")
	}
}

func TestFederator_HandleCallback_HappyPath_ExistingIdentity(t *testing.T) {
	fx := newFixture(t, federationoidc.ModeAutoProvision)

	// Seed an existing identity row for (iss=ts.URL, sub=sub-1).
	fx.q.identityErr = nil
	fx.q.identityResult = db.AccountIdentity{
		ID:            300,
		AccountID:     50,
		UpstreamIdpID: fx.idp.ID,
		UpstreamIss:   fx.ts.URL,
		UpstreamSub:   "sub-1",
		UpstreamEmail: pgtype.Text{String: "alice@example.com", Valid: true},
	}
	fx.q.accountByIDResults[50] = db.Account{ID: 50, Username: "alice", DisplayName: "Alice Example"}

	req, err := fx.f.BeginLogin(context.Background(), "mockop", "/me")
	if err != nil {
		t.Fatalf("BeginLogin: %v", err)
	}
	code, state, iss := driveAuthorizeFed(t, req.AuthorizeURL)

	result, err := fx.f.HandleCallback(context.Background(), state, code, iss, req.AntiForgeryToken)
	if err != nil {
		t.Fatalf("HandleCallback: %v", err)
	}
	if result.IsNew {
		t.Error("IsNew = true, want false (existing identity)")
	}
	if result.AccountID != 50 {
		t.Errorf("AccountID = %d, want 50", result.AccountID)
	}
	if len(fx.q.insertedAccounts) != 0 {
		t.Errorf("should not have inserted a new account; got %d", len(fx.q.insertedAccounts))
	}

	// Only Use, no Register.
	recs := fx.au.snapshot()
	if findEvent(recs, audit.EventRegister) != nil {
		t.Error("unexpected Register on re-login")
	}
	if findEvent(recs, audit.EventUse) == nil {
		t.Error("missing audit Use on re-login")
	}
}

func TestFederator_HandleCallback_RejectsIssMismatch(t *testing.T) {
	fx := newFixture(t, federationoidc.ModeAutoProvision)
	req, err := fx.f.BeginLogin(context.Background(), "mockop", "/me")
	if err != nil {
		t.Fatalf("BeginLogin: %v", err)
	}
	code, state, _ := driveAuthorizeFed(t, req.AuthorizeURL)

	// Pretend the OP shipped a different iss in the callback param.
	_, err = fx.f.HandleCallback(context.Background(), state, code, "https://attacker.example/", req.AntiForgeryToken)
	if ae := authn.AsAuthError(err); ae == nil || ae.Code != "federation_state_invalid" {
		t.Fatalf("want federation_state_invalid, got %v", err)
	}
	if r := auditReason(fx.au.snapshot(), audit.EventFail); r != "iss_mismatch_callback" {
		t.Fatalf("audit reason = %q, want iss_mismatch_callback", r)
	}
}

// TestFederator_HandleCallback_RejectsTokenEndpointDrift verifies the
// RFC 9700 §4.4.2.1 mix-up defense: ExpectedTokenEndpoint snapshotted at
// BeginLogin must still match the discovered token_endpoint at callback.
// A mismatch implies the upstream's discovery doc changed mid-flow
// (admin edit or attacker swap) and the code exchange would otherwise
// be sent to the wrong OP. Audit finding H3-sch.
func TestFederator_HandleCallback_RejectsTokenEndpointDrift(t *testing.T) {
	fx := newFixture(t, federationoidc.ModeAutoProvision)
	req, err := fx.f.BeginLogin(context.Background(), "mockop", "/me")
	if err != nil {
		t.Fatalf("BeginLogin: %v", err)
	}
	code, stateToken, _ := driveAuthorizeFed(t, req.AuthorizeURL)

	// Corrupt the snapshotted token_endpoint to simulate discovery drift.
	key := federationoidc.LoginKey(stateToken)
	blob, err := fx.kvm.Get(context.Background(), key)
	if err != nil {
		t.Fatalf("Get FedState: %v", err)
	}
	fs, err := federationoidc.DecodeFedState(blob)
	if err != nil {
		t.Fatalf("DecodeFedState: %v", err)
	}
	fs.ExpectedTokenEndpoint = "https://attacker.example/token"
	corrupted, err := fs.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if err := fx.kvm.SetEx(context.Background(), key, corrupted, time.Minute); err != nil {
		t.Fatalf("SetEx: %v", err)
	}

	_, err = fx.f.HandleCallback(context.Background(), stateToken, code, "", req.AntiForgeryToken)
	if ae := authn.AsAuthError(err); ae == nil || ae.Code != "federation_state_invalid" {
		t.Fatalf("want federation_state_invalid, got %v", err)
	}
	if r := auditReason(fx.au.snapshot(), audit.EventFail); r != "token_endpoint_drift" {
		t.Fatalf("audit reason = %q, want token_endpoint_drift", r)
	}
}

func TestFederator_HandleCallback_RejectsMissingState(t *testing.T) {
	fx := newFixture(t, federationoidc.ModeAutoProvision)

	// Token that never went into KV.
	_, err := fx.f.HandleCallback(context.Background(), "totally-bogus-state", "any-code", "", "")
	if ae := authn.AsAuthError(err); ae == nil || ae.Code != "federation_state_invalid" {
		t.Fatalf("want federation_state_invalid, got %v", err)
	}
	if r := auditReason(fx.au.snapshot(), audit.EventFail); r != "state_invalid" {
		t.Fatalf("audit reason = %q, want state_invalid", r)
	}
}

func TestFederator_HandleCallback_SingleUseStateConsumed(t *testing.T) {
	fx := newFixture(t, federationoidc.ModeAutoProvision)
	req, err := fx.f.BeginLogin(context.Background(), "mockop", "/me")
	if err != nil {
		t.Fatalf("BeginLogin: %v", err)
	}
	code, state, iss := driveAuthorizeFed(t, req.AuthorizeURL)

	if _, err := fx.f.HandleCallback(context.Background(), state, code, iss, req.AntiForgeryToken); err != nil {
		t.Fatalf("first HandleCallback: %v", err)
	}

	// Second call MUST fail — state was Pop'd on the first call.
	_, err = fx.f.HandleCallback(context.Background(), state, code, iss, req.AntiForgeryToken)
	if ae := authn.AsAuthError(err); ae == nil || ae.Code != "federation_state_invalid" {
		t.Fatalf("want federation_state_invalid on replay, got %v", err)
	}
}

// TestFederator_HandleCallback_RejectsBrowserBindingMismatch guards N4: a
// callback whose anti-forgery cookie is absent or does not match the binding
// captured at BeginLogin is rejected (login-CSRF / session-fixation defense),
// before any code exchange or account resolution.
func TestFederator_HandleCallback_RejectsBrowserBindingMismatch(t *testing.T) {
	fx := newFixture(t, federationoidc.ModeAutoProvision)
	req, err := fx.f.BeginLogin(context.Background(), "mockop", "/me")
	if err != nil {
		t.Fatalf("BeginLogin: %v", err)
	}
	code, state, iss := driveAuthorizeFed(t, req.AuthorizeURL)

	// Wrong token (attacker fed the victim a code/state from the attacker's own
	// upstream dance; the victim's browser has a different / no binding cookie).
	_, err = fx.f.HandleCallback(context.Background(), state, code, iss, "wrong-anti-forgery-token")
	if ae := authn.AsAuthError(err); ae == nil || ae.Code != "federation_state_invalid" {
		t.Fatalf("want federation_state_invalid on binding mismatch, got %v", err)
	}
	if r := auditReason(fx.au.snapshot(), audit.EventFail); r != "browser_binding_mismatch" {
		t.Fatalf("audit reason = %q, want browser_binding_mismatch", r)
	}

	// Absent cookie is likewise refused. A fresh flow (the prior state was
	// Pop'd) so the binding check — not the missing-state path — is exercised.
	req2, err := fx.f.BeginLogin(context.Background(), "mockop", "/me")
	if err != nil {
		t.Fatalf("BeginLogin #2: %v", err)
	}
	code2, state2, iss2 := driveAuthorizeFed(t, req2.AuthorizeURL)
	_, err = fx.f.HandleCallback(context.Background(), state2, code2, iss2, "")
	if ae := authn.AsAuthError(err); ae == nil || ae.Code != "federation_state_invalid" {
		t.Fatalf("want federation_state_invalid on absent cookie, got %v", err)
	}
}

// TestFederator_HandleCallback_DisabledMidFlowCleanError guards T3.1: if the
// upstream IdP is disabled/deleted between BeginLogin and the callback, the
// re-lookup must collapse to a clean federation_state_invalid + audit row (the
// same shape begin() produces) rather than a wrapped HTTP 500.
func TestFederator_HandleCallback_DisabledMidFlowCleanError(t *testing.T) {
	fx := newFixture(t, federationoidc.ModeAutoProvision)
	req, err := fx.f.BeginLogin(context.Background(), "mockop", "/me")
	if err != nil {
		t.Fatalf("BeginLogin: %v", err)
	}
	code, state, iss := driveAuthorizeFed(t, req.AuthorizeURL)

	// Disable the IdP mid-flow (GetUpstreamIDPBySlug now misses → ErrNoRows).
	delete(fx.q.idpBySlug, "mockop")

	_, err = fx.f.HandleCallback(context.Background(), state, code, iss, req.AntiForgeryToken)
	if ae := authn.AsAuthError(err); ae == nil || ae.Code != "federation_state_invalid" {
		t.Fatalf("want clean federation_state_invalid, got %v", err)
	}
	if r := auditReason(fx.au.snapshot(), audit.EventFail); r != "idp_disabled_or_deleted" {
		t.Fatalf("audit reason = %q, want idp_disabled_or_deleted", r)
	}
}

func TestFederator_HandleCallback_RejectsCodeExchangeFailure(t *testing.T) {
	fx := newFixture(t, federationoidc.ModeAutoProvision)
	req, err := fx.f.BeginLogin(context.Background(), "mockop", "/me")
	if err != nil {
		t.Fatalf("BeginLogin: %v", err)
	}
	_, state, iss := driveAuthorizeFed(t, req.AuthorizeURL)

	// Submit a code that does not match anything the OP recorded.
	_, err = fx.f.HandleCallback(context.Background(), state, "definitely-not-a-real-code", iss, req.AntiForgeryToken)
	if ae := authn.AsAuthError(err); ae == nil || ae.Code != "federation_state_invalid" {
		t.Fatalf("want federation_state_invalid on bad code, got %v", err)
	}
	if r := auditReason(fx.au.snapshot(), audit.EventFail); r != "code_exchange_failed" {
		t.Fatalf("audit reason = %q, want code_exchange_failed", r)
	}
}

func TestFederator_HandleCallback_RejectsDisabledAccount(t *testing.T) {
	fx := newFixture(t, federationoidc.ModeAutoProvision)

	// Seed an existing identity that points at a disabled account.
	fx.q.identityErr = nil
	fx.q.identityResult = db.AccountIdentity{
		ID:            300,
		AccountID:     77,
		UpstreamIdpID: fx.idp.ID,
		UpstreamIss:   fx.ts.URL,
		UpstreamSub:   "sub-1",
		UpstreamEmail: pgtype.Text{String: "alice@example.com", Valid: true},
	}
	fx.q.accountByIDResults[77] = db.Account{ID: 77, Username: "alice", DisplayName: "Alice", Disabled: true}

	req, err := fx.f.BeginLogin(context.Background(), "mockop", "/me")
	if err != nil {
		t.Fatalf("BeginLogin: %v", err)
	}
	code, state, iss := driveAuthorizeFed(t, req.AuthorizeURL)

	_, err = fx.f.HandleCallback(context.Background(), state, code, iss, req.AntiForgeryToken)
	if ae := authn.AsAuthError(err); ae == nil || ae.Code != "bad_credentials" {
		t.Fatalf("want bad_credentials for disabled account, got %v", err)
	}

	recs := fx.au.snapshot()
	// We expect Resolve's Use event AND a separate fail for account_disabled.
	if r := auditReason(recs, audit.EventFail); r != "account_disabled" {
		t.Fatalf("audit reason = %q, want account_disabled", r)
	}
}

func TestFederator_LinkBegin_StashesAtLinkKey(t *testing.T) {
	fx := newFixture(t, federationoidc.ModeAutoProvision)
	const acctID int32 = 99

	req, err := fx.f.LinkBegin(context.Background(), acctID, "mockop", "/me/identities")
	if err != nil {
		t.Fatalf("LinkBegin: %v", err)
	}

	// State must live under LinkKey, not LoginKey.
	if _, err := fx.kvm.Get(context.Background(), federationoidc.LinkKey(req.StateKey)); err != nil {
		t.Fatalf("state missing from LinkKey: %v", err)
	}
	if _, err := fx.kvm.Get(context.Background(), federationoidc.LoginKey(req.StateKey)); err == nil {
		t.Fatal("state should NOT be present under LoginKey")
	}

	blob, _ := fx.kvm.Get(context.Background(), federationoidc.LinkKey(req.StateKey))
	state, err := federationoidc.DecodeFedState(blob)
	if err != nil {
		t.Fatalf("DecodeFedState: %v", err)
	}
	if state.LinkingAccountID == nil || *state.LinkingAccountID != acctID {
		t.Errorf("LinkingAccountID = %v, want %d", state.LinkingAccountID, acctID)
	}

	// Authorize URL should point at the link-flavored callback.
	u, _ := url.Parse(req.AuthorizeURL)
	got := u.Query().Get("redirect_uri")
	want := "https://idp.example.test/api/prohibitorum/me/identities/link/mockop/callback"
	if got != want {
		t.Errorf("redirect_uri = %q, want %q", got, want)
	}
}

func TestFederator_LinkCallback_HappyPath(t *testing.T) {
	fx := newFixture(t, federationoidc.ModeAutoProvision)
	const acctID int32 = 99
	// Seed the account row so re-fetch works (though LinkCallback doesn't
	// actually call GetAccountByID — set up for parallel structure).
	fx.q.accountByIDResults[acctID] = db.Account{ID: acctID, Username: "alice"}

	req, err := fx.f.LinkBegin(context.Background(), acctID, "mockop", "/me/identities")
	if err != nil {
		t.Fatalf("LinkBegin: %v", err)
	}
	code, state, iss := driveAuthorizeFed(t, req.AuthorizeURL)

	result, err := fx.f.LinkCallback(context.Background(), state, code, iss, acctID)
	if err != nil {
		t.Fatalf("LinkCallback: %v", err)
	}
	if result.IDPSlug != "mockop" {
		t.Errorf("IDPSlug = %q", result.IDPSlug)
	}
	if result.ReturnTo != "/me/identities" {
		t.Errorf("ReturnTo = %q", result.ReturnTo)
	}

	// Identity row inserted directly (NOT via Resolve — no Account row created).
	if fx.q.insertedIdentity.AccountID != acctID {
		t.Errorf("inserted identity AccountID = %d, want %d", fx.q.insertedIdentity.AccountID, acctID)
	}
	if fx.q.insertedIdentity.UpstreamIss != fx.ts.URL {
		t.Errorf("inserted UpstreamIss = %q, want %q", fx.q.insertedIdentity.UpstreamIss, fx.ts.URL)
	}
	if len(fx.q.insertedAccounts) != 0 {
		t.Errorf("link MUST NOT insert a new account; got %d", len(fx.q.insertedAccounts))
	}

	// Audit Link event with the right account.
	link := findEvent(fx.au.snapshot(), audit.EventLink)
	if link == nil {
		t.Fatal("missing audit EventLink")
	}
	if link.AccountID == nil || *link.AccountID != acctID {
		t.Errorf("Link account = %v, want %d", link.AccountID, acctID)
	}
	if link.Detail["idp_slug"] != "mockop" {
		t.Errorf("Link detail idp_slug = %v", link.Detail["idp_slug"])
	}
}

func TestFederator_LinkCallback_RejectsSessionSwap(t *testing.T) {
	fx := newFixture(t, federationoidc.ModeAutoProvision)
	const startedBy int32 = 99
	const completingAs int32 = 100

	req, err := fx.f.LinkBegin(context.Background(), startedBy, "mockop", "/me/identities")
	if err != nil {
		t.Fatalf("LinkBegin: %v", err)
	}
	code, state, iss := driveAuthorizeFed(t, req.AuthorizeURL)

	// Different account completes the link flow.
	_, err = fx.f.LinkCallback(context.Background(), state, code, iss, completingAs)
	if ae := authn.AsAuthError(err); ae == nil || ae.Code != "federation_state_invalid" {
		t.Fatalf("want federation_state_invalid, got %v", err)
	}
	if r := auditReason(fx.au.snapshot(), audit.EventFail); r != "session_swap" {
		t.Fatalf("audit reason = %q, want session_swap", r)
	}
	// MUST NOT have inserted an identity row.
	if fx.q.insertedIdentity.AccountID != 0 {
		t.Errorf("identity row should not be inserted; got %+v", fx.q.insertedIdentity)
	}
}

func TestFederator_LinkCallback_RejectsLinkConflict(t *testing.T) {
	fx := newFixture(t, federationoidc.ModeAutoProvision)
	const acctID int32 = 99

	// Force the insert to return a unique-violation (the (iss, sub) is
	// already bound to another account).
	fx.q.insertIdentityErr = &pgconn.PgError{Code: "23505", ConstraintName: "account_identity_iss_sub_uniq"}

	req, err := fx.f.LinkBegin(context.Background(), acctID, "mockop", "/me/identities")
	if err != nil {
		t.Fatalf("LinkBegin: %v", err)
	}
	code, state, iss := driveAuthorizeFed(t, req.AuthorizeURL)

	_, err = fx.f.LinkCallback(context.Background(), state, code, iss, acctID)
	if ae := authn.AsAuthError(err); ae == nil || ae.Code != "federation_state_invalid" {
		t.Fatalf("want federation_state_invalid, got %v", err)
	}
	if r := auditReason(fx.au.snapshot(), audit.EventFail); r != "link_conflict" {
		t.Fatalf("audit reason = %q, want link_conflict", r)
	}
}

func TestFederator_LinkCallback_RejectsEmailNotVerified(t *testing.T) {
	fx := newFixture(t, federationoidc.ModeAutoProvision)
	const acctID int32 = 99

	// Mock OP ships email_verified=false; idp.RequireVerifiedEmail defaults
	// to true in newFixture, so this MUST be rejected.
	fx.op.SetClaims("sub-1", "alice@example.com", false, "alice", "Alice Example")

	req, err := fx.f.LinkBegin(context.Background(), acctID, "mockop", "/me/identities")
	if err != nil {
		t.Fatalf("LinkBegin: %v", err)
	}
	code, state, iss := driveAuthorizeFed(t, req.AuthorizeURL)

	_, err = fx.f.LinkCallback(context.Background(), state, code, iss, acctID)
	if ae := authn.AsAuthError(err); ae == nil || ae.Code != "email_not_verified" {
		t.Fatalf("want email_not_verified, got %v", err)
	}

	recs := fx.au.snapshot()
	fail := findEvent(recs, audit.EventFail)
	if fail == nil {
		t.Fatal("missing audit EventFail")
	}
	if r, _ := fail.Detail["reason"].(string); r != "email_not_verified" {
		t.Errorf("audit reason = %q, want email_not_verified", r)
	}
	if s, _ := fail.Detail["idp_slug"].(string); s != "mockop" {
		t.Errorf("audit idp_slug = %q, want mockop", s)
	}
	if fail.AccountID == nil || *fail.AccountID != acctID {
		t.Errorf("audit AccountID = %v, want %d", fail.AccountID, acctID)
	}

	// MUST NOT have inserted an identity row.
	if fx.q.insertedIdentity.AccountID != 0 {
		t.Errorf("identity row should not be inserted; got %+v", fx.q.insertedIdentity)
	}
}

func TestFederator_LinkCallback_RejectsDomainNotAllowed(t *testing.T) {
	fx := newFixture(t, federationoidc.ModeAutoProvision)
	const acctID int32 = 99

	// Restrict to example.com; mock OP returns badcorp.com.
	idp := fx.idp
	idp.AllowedDomains = []string{"example.com"}
	fx.q.idpBySlug["mockop"] = idp

	fx.op.SetClaims("sub-1", "evil@badcorp.com", true, "evil", "Evil")

	req, err := fx.f.LinkBegin(context.Background(), acctID, "mockop", "/me/identities")
	if err != nil {
		t.Fatalf("LinkBegin: %v", err)
	}
	code, state, iss := driveAuthorizeFed(t, req.AuthorizeURL)

	_, err = fx.f.LinkCallback(context.Background(), state, code, iss, acctID)
	if ae := authn.AsAuthError(err); ae == nil || ae.Code != "invite_required" {
		t.Fatalf("want invite_required, got %v", err)
	}

	recs := fx.au.snapshot()
	fail := findEvent(recs, audit.EventFail)
	if fail == nil {
		t.Fatal("missing audit EventFail")
	}
	if r, _ := fail.Detail["reason"].(string); r != "domain_not_allowed" {
		t.Errorf("audit reason = %q, want domain_not_allowed", r)
	}
	if s, _ := fail.Detail["idp_slug"].(string); s != "mockop" {
		t.Errorf("audit idp_slug = %q, want mockop", s)
	}
	if fail.AccountID == nil || *fail.AccountID != acctID {
		t.Errorf("audit AccountID = %v, want %d", fail.AccountID, acctID)
	}

	// MUST NOT have inserted an identity row.
	if fx.q.insertedIdentity.AccountID != 0 {
		t.Errorf("identity row should not be inserted; got %+v", fx.q.insertedIdentity)
	}
}

func TestFederator_LinkCallback_RejectsLoginStateToken(t *testing.T) {
	// Cross-namespace defense: a token minted by BeginLogin lives under
	// LoginKey; calling LinkCallback with it must fail because LinkCallback
	// only Pop's LinkKey.
	fx := newFixture(t, federationoidc.ModeAutoProvision)

	req, err := fx.f.BeginLogin(context.Background(), "mockop", "/me")
	if err != nil {
		t.Fatalf("BeginLogin: %v", err)
	}
	_, err = fx.f.LinkCallback(context.Background(), req.StateKey, "any-code", "", 99)
	if ae := authn.AsAuthError(err); ae == nil || ae.Code != "federation_state_invalid" {
		t.Fatalf("want federation_state_invalid, got %v", err)
	}
}

func TestFederator_BeginLogin_MissingDEKVersion(t *testing.T) {
	fx := newFixture(t, federationoidc.ModeAutoProvision)

	// Mutate the row to claim a key version that's not in the deks map.
	bad := fx.idp
	bad.KeyVersion = 99
	fx.q.idpBySlug["mockop"] = bad

	_, err := fx.f.BeginLogin(context.Background(), "mockop", "/me")
	if err == nil || !strings.Contains(err.Error(), "missing DEK version 99") {
		t.Fatalf("want error mentioning missing DEK version 99, got %v", err)
	}
}

// seedInviteEnrollment seeds a valid pending invite enrollment row bound
// to the fixture's IdP slug. Tokens for ConsumeEnrollment AND
// GetEnrollmentByToken share the same map, so BeginInviteRedemption and
// HandleCallback both find the row.
func (fx *fixtureFederator) seedInviteEnrollment(token, username string) {
	enr := db.Enrollment{
		Token:                   token,
		Intent:                  "invite",
		ExpectedUpstreamIdpSlug: pgtype.Text{String: fx.idp.Slug, Valid: true},
		TemplateUsername:        pgtype.Text{String: username, Valid: true},
		TemplateDisplayName:     pgtype.Text{String: "Invited " + username, Valid: true},
		TemplateRole:            pgtype.Text{String: "user", Valid: true},
		TemplateAttributes:      []byte("{}"),
		// GetEnrollmentByToken checks ExpiresAt > now() via .After(time.Now());
		// ConsumeEnrollment in the fake doesn't check, so seed a future ts here
		// for the begin-side validation.
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
	}
	fx.q.enrollmentByToken[token] = enr
	fx.q.consumeEnrollmentResult = enr
}

func TestFederator_HandleCallback_InviteRedemption_HappyPath(t *testing.T) {
	// The IdP's mode is invite_only here, but the mode-decoupling means we
	// could just as well use auto_provision; the EnrollmentToken on the
	// FedState is what routes us through applyInviteOnly.
	fx := newFixture(t, federationoidc.ModeInviteOnly)
	fx.seedInviteEnrollment("the-invite-token", "newuser")

	req, err := fx.f.BeginInviteRedemption(context.Background(), "the-invite-token", "/me")
	if err != nil {
		t.Fatalf("BeginInviteRedemption: %v", err)
	}
	if req.StateKey == "" {
		t.Fatal("StateKey empty")
	}

	// The state must live under LoginKey (same namespace as BeginLogin,
	// not LinkKey) and carry the EnrollmentToken so HandleCallback can
	// dispatch.
	blob, err := fx.kvm.Get(context.Background(), federationoidc.LoginKey(req.StateKey))
	if err != nil {
		t.Fatalf("state missing from LoginKey: %v", err)
	}
	state, err := federationoidc.DecodeFedState(blob)
	if err != nil {
		t.Fatalf("DecodeFedState: %v", err)
	}
	if state.EnrollmentToken != "the-invite-token" {
		t.Errorf("FedState.EnrollmentToken = %q, want the-invite-token", state.EnrollmentToken)
	}
	if state.LinkingAccountID != nil {
		t.Errorf("LinkingAccountID should be nil for invite flow, got %v", *state.LinkingAccountID)
	}

	code, stateTok, iss := driveAuthorizeFed(t, req.AuthorizeURL)

	result, err := fx.f.HandleCallback(context.Background(), stateTok, code, iss, req.AntiForgeryToken)
	if err != nil {
		t.Fatalf("HandleCallback: %v", err)
	}
	if !result.IsNew {
		t.Error("IsNew = false, want true (invite-minted fresh account)")
	}
	if result.ReturnTo != "/me" {
		t.Errorf("ReturnTo = %q", result.ReturnTo)
	}

	// Account row created with template's username (NOT the upstream's
	// "alice" preferred_username — the invite is the authoritative source).
	if len(fx.q.insertedAccounts) != 1 {
		t.Fatalf("want 1 inserted account, got %d", len(fx.q.insertedAccounts))
	}
	if fx.q.insertedAccount.Username != "newuser" {
		t.Errorf("username = %q, want newuser (from template)", fx.q.insertedAccount.Username)
	}
	if fx.q.insertedAccount.DisplayName != "Invited newuser" {
		t.Errorf("display_name = %q, want %q", fx.q.insertedAccount.DisplayName, "Invited newuser")
	}
	if fx.q.insertedIdentity.AccountID != result.AccountID {
		t.Errorf("identity account_id = %d, want %d", fx.q.insertedIdentity.AccountID, result.AccountID)
	}

	// ConsumeEnrollment must have fired exactly once with our token.
	if len(fx.q.consumedTokens) != 1 || fx.q.consumedTokens[0] != "the-invite-token" {
		t.Errorf("consumedTokens = %v, want [the-invite-token]", fx.q.consumedTokens)
	}

	// Audit Register with invite_only_redemption reason + Use.
	recs := fx.au.snapshot()
	reg := findEvent(recs, audit.EventRegister)
	if reg == nil {
		t.Fatal("missing audit Register")
	}
	if reg.Detail["reason"] != "invite_only_redemption" {
		t.Errorf("Register reason = %v, want invite_only_redemption", reg.Detail["reason"])
	}
	if findEvent(recs, audit.EventUse) == nil {
		t.Fatal("missing audit Use")
	}
}

func TestFederator_BeginInviteRedemption_UnknownToken(t *testing.T) {
	fx := newFixture(t, federationoidc.ModeInviteOnly)
	_, err := fx.f.BeginInviteRedemption(context.Background(), "nope-not-a-token", "/me")
	if ae := authn.AsAuthError(err); ae == nil || ae.Code != "invite_required" {
		t.Fatalf("want invite_required, got %v", err)
	}
}

// --- discovery-cache tests (audit finding H2-sch) -------------------------

// newCachingFixture is a variant of newFixture that wraps the mock OP behind
// a counter-bearing handler so tests can assert how many times the upstream
// /.well-known/openid-configuration endpoint was actually fetched. We don't
// modify mockop itself — a thin http.Handler wrapper around op.Routes()
// keeps the instrumentation local to the test file.
func newCachingFixture(t *testing.T) (fx *fixtureFederator, discoveryHits *int32) {
	t.Helper()

	op, err := mockop.New("")
	if err != nil {
		t.Fatalf("mockop.New: %v", err)
	}
	var hits int32
	inner := op.Routes()
	counting := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/openid-configuration" {
			atomic.AddInt32(&hits, 1)
		}
		inner.ServeHTTP(w, r)
	})
	ts := httptest.NewServer(counting)
	op.SetBase(ts.URL)
	t.Cleanup(ts.Close)

	const idpID int64 = 42
	const keyVersion int32 = 1
	ct, nonce, err := federationoidc.EncryptClientSecret(testDEK, []byte("test-secret"), idpID, keyVersion)
	if err != nil {
		t.Fatalf("EncryptClientSecret: %v", err)
	}

	idp := db.UpstreamIdp{
		ID:                   idpID,
		Slug:                 "mockop",
		DisplayName:          "Mock OP",
		IssuerUrl:            ts.URL,
		ClientID:             "test-client",
		ClientSecretEnc:      ct,
		SecretNonce:          nonce,
		KeyVersion:           keyVersion,
		Scopes:               []string{"openid", "profile", "email"},
		Mode:                 federationoidc.ModeAutoProvision,
		RequireVerifiedEmail: true,
		UsernameClaim:        "preferred_username",
		DisplayNameClaim:     "name",
		EmailClaim:           "email",
	}

	q := newFakeFederatorQueries()
	q.idpBySlug[idp.Slug] = idp

	kvm := kv.NewMemoryStore()
	t.Cleanup(func() { _ = kvm.Close() })

	au := &recordingAudit{}
	cfg := configx.FederationConfig{
		StateTTL:            5 * time.Minute,
		DefaultScopes:       []string{"openid", "profile", "email"},
		AllowPrivateNetwork: true, // mock OP is on loopback
	}
	deks := map[int][]byte{1: testDEK}
	origin := "https://idp.example.test"
	fd := federationoidc.NewFederator(q, kvm, au, cfg, deks, nil, origin)

	op.SetClaims("sub-1", "alice@example.com", true, "alice", "Alice Example")
	op.SetAMR([]string{"pwd", "mfa"})

	return &fixtureFederator{
		t: t, op: op, ts: ts, idp: idp, q: q, kvm: kvm, au: au,
		f: fd, cfg: cfg, origin: origin,
	}, &hits
}

// TestFederator_BuildClient_CachesAcrossBeginLogin verifies that two
// successive BeginLogin calls against the same IdP reuse one *Client and
// therefore hit upstream OIDC discovery exactly once. Without the cache,
// every federation request runs full discovery — see audit finding H2-sch.
func TestFederator_BuildClient_CachesAcrossBeginLogin(t *testing.T) {
	fx, hits := newCachingFixture(t)

	if _, err := fx.f.BeginLogin(context.Background(), "mockop", "/me"); err != nil {
		t.Fatalf("first BeginLogin: %v", err)
	}
	if _, err := fx.f.BeginLogin(context.Background(), "mockop", "/me"); err != nil {
		t.Fatalf("second BeginLogin: %v", err)
	}

	if got := atomic.LoadInt32(hits); got != 1 {
		t.Fatalf("discovery hits = %d, want 1 (cache miss + cache hit)", got)
	}
	if n := federationoidc.ClientCacheLenForTest(fx.f); n != 1 {
		t.Errorf("cache len = %d, want 1", n)
	}
}

// TestFederator_BuildClient_RebuildsOnKeyVersionChange verifies that bumping
// upstream_idp.key_version (the natural cache-invalidation lever for DEK
// rotation) produces a fresh cache key and therefore a fresh discovery
// fetch. We construct a federator whose DEK map carries the same key under
// versions 1 and 2 so we can re-encrypt the row under v2 without touching
// production crypto paths.
func TestFederator_BuildClient_RebuildsOnKeyVersionChange(t *testing.T) {
	fx, hits := newCachingFixture(t)

	// Build a second federator with both v1 and v2 DEKs available, sharing
	// every other collaborator (queries, kv, audit) and — crucially — the
	// same upstream OP so the discovery counter still ticks. The fixture's
	// own fx.f only knows v1; this fresh federator knows both.
	deks := map[int][]byte{1: testDEK, 2: testDEK}
	fd := federationoidc.NewFederator(fx.q, fx.kvm, fx.au, fx.cfg, deks, nil, fx.origin)

	// First call — v1, cache miss → 1 discovery hit.
	if _, err := fd.BeginLogin(context.Background(), "mockop", "/me"); err != nil {
		t.Fatalf("first BeginLogin (key v1): %v", err)
	}
	if got := atomic.LoadInt32(hits); got != 1 {
		t.Fatalf("discovery hits after v1 call = %d, want 1", got)
	}

	// Bump key_version to 2 on the IdP row (re-encrypt under v2).
	ct2, nonce2, err := federationoidc.EncryptClientSecret(testDEK, []byte("test-secret"), fx.idp.ID, 2)
	if err != nil {
		t.Fatalf("EncryptClientSecret v2: %v", err)
	}
	idp2 := fx.idp
	idp2.KeyVersion = 2
	idp2.ClientSecretEnc = ct2
	idp2.SecretNonce = nonce2
	fx.q.idpBySlug["mockop"] = idp2

	// Second call — v2 → cache key differs → fresh discovery hit.
	if _, err := fd.BeginLogin(context.Background(), "mockop", "/me"); err != nil {
		t.Fatalf("second BeginLogin (key v2): %v", err)
	}
	if got := atomic.LoadInt32(hits); got != 2 {
		t.Fatalf("discovery hits after v2 call = %d, want 2 (key_version bump invalidates cache)", got)
	}

	// Sanity: a repeat v2 call hits the cache (no new discovery).
	if _, err := fd.BeginLogin(context.Background(), "mockop", "/me"); err != nil {
		t.Fatalf("third BeginLogin (key v2 repeat): %v", err)
	}
	if got := atomic.LoadInt32(hits); got != 2 {
		t.Fatalf("discovery hits after v2 repeat = %d, want 2 (cache should hit)", got)
	}

	// Cache should hold separate entries for v1 and v2.
	if n := federationoidc.ClientCacheLenForTest(fd); n != 2 {
		t.Errorf("cache len = %d, want 2 (v1 + v2 entries)", n)
	}
}

// TestFederator_BuildClient_RebuildsOnTTLExpiry verifies that once the TTL
// window elapses, the next buildClient call re-runs discovery instead of
// returning the stale cached *Client. We force the condition by shrinking
// the TTL to a negative duration before any call: every Store writes an
// already-expired expiresAt, so every Load sees expiry and rebuilds.
func TestFederator_BuildClient_RebuildsOnTTLExpiry(t *testing.T) {
	fx, hits := newCachingFixture(t)

	// Negative TTL: each cached entry is stored already expired.
	federationoidc.SetClientCacheTTLForTest(fx.f, -time.Second)

	// Three back-to-back BeginLogin calls — each must re-run discovery
	// because the previous cache entry's expiresAt is already in the past.
	for i := 1; i <= 3; i++ {
		if _, err := fx.f.BeginLogin(context.Background(), "mockop", "/me"); err != nil {
			t.Fatalf("BeginLogin #%d: %v", i, err)
		}
	}
	if got := atomic.LoadInt32(hits); got != 3 {
		t.Fatalf("discovery hits = %d, want 3 (each call sees expired entry)", got)
	}

	// Sanity: restore the normal TTL and verify the cache settles into
	// "hit" mode again.
	federationoidc.ClearClientCacheForTest(fx.f)
	federationoidc.SetClientCacheTTLForTest(fx.f, 15*time.Minute)

	if _, err := fx.f.BeginLogin(context.Background(), "mockop", "/me"); err != nil {
		t.Fatalf("post-reset BeginLogin: %v", err)
	}
	if _, err := fx.f.BeginLogin(context.Background(), "mockop", "/me"); err != nil {
		t.Fatalf("post-reset BeginLogin (cached): %v", err)
	}
	if got := atomic.LoadInt32(hits); got != 4 {
		t.Fatalf("discovery hits after TTL reset = %d, want 4 (one new miss + one hit)", got)
	}
}

func TestFederator_BeginInviteRedemption_ExpiredToken(t *testing.T) {
	fx := newFixture(t, federationoidc.ModeInviteOnly)
	// Seed an enrollment that has already expired.
	fx.q.enrollmentByToken["expired"] = db.Enrollment{
		Token:                   "expired",
		Intent:                  "invite",
		ExpectedUpstreamIdpSlug: pgtype.Text{String: fx.idp.Slug, Valid: true},
		TemplateUsername:        pgtype.Text{String: "x", Valid: true},
		TemplateRole:            pgtype.Text{String: "user", Valid: true},
		ExpiresAt:               pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), Valid: true},
	}
	_, err := fx.f.BeginInviteRedemption(context.Background(), "expired", "/me")
	if ae := authn.AsAuthError(err); ae == nil || ae.Code != "invite_required" {
		t.Fatalf("want invite_required, got %v", err)
	}
	if r := auditReason(fx.au.snapshot(), audit.EventFail); r != "invite_expired" {
		t.Errorf("audit reason = %q, want invite_expired", r)
	}
}

// --- step-up / auth_time tests -------------------------------------------

// TestClient_AuthURLStepUpOptions verifies that passing StepUpAuthOptions()
// to AuthURL produces a URL with prompt=login and max_age=0. The base URL
// (without step-up) must NOT contain those params.
func TestClient_AuthURLStepUpOptions(t *testing.T) {
	ts, _ := newMockOP(t)
	c := newClient(t, ts)

	_, challenge := pkceVerifierAndChallenge()
	state := "step-state"
	nonce := "step-nonce"

	// Baseline: no extra options — must not inject step-up params.
	baseURL := c.AuthURL(state, nonce, challenge)
	bu, err := url.Parse(baseURL)
	if err != nil {
		t.Fatalf("parse base AuthURL: %v", err)
	}
	bq := bu.Query()
	if bq.Get("prompt") != "" {
		t.Errorf("baseline URL has prompt=%q, want absent", bq.Get("prompt"))
	}
	if bq.Get("max_age") != "" {
		t.Errorf("baseline URL has max_age=%q, want absent", bq.Get("max_age"))
	}

	// Step-up: must inject prompt=login and max_age=0.
	stepURL := c.AuthURL(state, nonce, challenge, federationoidc.StepUpAuthOptions()...)
	su, err := url.Parse(stepURL)
	if err != nil {
		t.Fatalf("parse step-up AuthURL: %v", err)
	}
	sq := su.Query()
	if got := sq.Get("prompt"); got != "login" {
		t.Errorf("step-up URL prompt=%q, want login", got)
	}
	if got := sq.Get("max_age"); got != "0" {
		t.Errorf("step-up URL max_age=%q, want 0", got)
	}
}

// TestClient_ExchangePopulatesAuthTime verifies that after a code exchange,
// Tokens.AuthTime is populated from the id_token auth_time claim. When the
// mock OP does not emit auth_time the field must be zero.
func TestClient_ExchangePopulatesAuthTime(t *testing.T) {
	ts, op := newMockOP(t)
	op.SetClaims("sub-at", "at@example.test", true, "at", "Auth Time Test")

	// Phase 1: mock OP does not emit auth_time — Tokens.AuthTime must be zero.
	c := newClient(t, ts)
	verifier, challenge := pkceVerifierAndChallenge()
	code := driveAuthorize(t, c.AuthURL("s1", "n1", challenge))
	toks, err := c.Exchange(context.Background(), code, verifier, ts.URL, "n1")
	if err != nil {
		t.Fatalf("Exchange (no auth_time): %v", err)
	}
	if !toks.AuthTime.IsZero() {
		t.Errorf("AuthTime = %v, want zero (no auth_time in token)", toks.AuthTime)
	}

	// Phase 2: mock OP emits auth_time — Tokens.AuthTime must be non-zero
	// and within a reasonable window of now.
	beforeAuth := time.Now().Add(-time.Second)
	op.SetAuthTime(time.Now())
	code2 := driveAuthorize(t, c.AuthURL("s2", "n2", challenge))
	toks2, err := c.Exchange(context.Background(), code2, verifier, ts.URL, "n2")
	if err != nil {
		t.Fatalf("Exchange (with auth_time): %v", err)
	}
	if toks2.AuthTime.IsZero() {
		t.Error("AuthTime is zero; want non-zero (auth_time was in token)")
	}
	if toks2.AuthTime.Before(beforeAuth) {
		t.Errorf("AuthTime %v is before expected window start %v", toks2.AuthTime, beforeAuth)
	}
}

// TestFederator_SudoBegin_BindsStateAndForcesReauth verifies the sudo step-up
// flow stashes state under the distinct SudoKey namespace, captures the linked
// identity's subject as ExpectedSub, carries a browser binding (sudo flows set
// the cookie), and forces re-auth via prompt=login + max_age=0.
func TestFederator_SudoBegin_BindsStateAndForcesReauth(t *testing.T) {
	fx := newFixture(t, federationoidc.ModeAutoProvision)
	fx.q.identitiesByAccount[7] = []db.ListAccountIdentitiesByAccountRow{
		{AccountID: 7, UpstreamIdpID: fx.idp.ID, UpstreamSub: "sub-1", IdpSlug: "mockop", IdpDisplayName: "Mock OP"},
	}
	req, err := fx.f.SudoBegin(context.Background(), 7, "mockop", "/security")
	if err != nil {
		t.Fatalf("SudoBegin: %v", err)
	}

	blob, err := fx.kvm.Get(context.Background(), federationoidc.SudoKey(req.StateKey))
	if err != nil {
		t.Fatalf("sudo state missing from SudoKey: %v", err)
	}
	st, err := federationoidc.DecodeFedState(blob)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if st.SudoAccountID == nil || *st.SudoAccountID != 7 {
		t.Errorf("SudoAccountID = %v, want 7", st.SudoAccountID)
	}
	if st.ExpectedSub != "sub-1" {
		t.Errorf("ExpectedSub = %q, want sub-1", st.ExpectedSub)
	}
	if st.BrowserBinding == "" {
		t.Error("BrowserBinding empty, want non-empty (sudo flow carries the cookie)")
	}
	if req.AntiForgeryToken == "" {
		t.Error("AntiForgeryToken empty, want non-empty (sudo flow sets the cookie)")
	}
	u, err := url.Parse(req.AuthorizeURL)
	if err != nil {
		t.Fatalf("parse AuthorizeURL: %v", err)
	}
	if u.Query().Get("prompt") != "login" || u.Query().Get("max_age") != "0" {
		t.Errorf("authorize URL missing step-up params: %s", req.AuthorizeURL)
	}
	// not present under Login/Link keys
	if _, err := fx.kvm.Get(context.Background(), federationoidc.LoginKey(req.StateKey)); err == nil {
		t.Error("sudo state should NOT be under LoginKey")
	}
	if _, err := fx.kvm.Get(context.Background(), federationoidc.LinkKey(req.StateKey)); err == nil {
		t.Error("sudo state should NOT be under LinkKey")
	}
}

// TestFederator_SudoBegin_RejectsUnlinkedSlug verifies an account with no linked
// identity at the slug cannot begin a sudo step-up (would have no subject to
// re-verify against in the callback).
func TestFederator_SudoBegin_RejectsUnlinkedSlug(t *testing.T) {
	fx := newFixture(t, federationoidc.ModeAutoProvision)
	// account 7 has NO linked identity
	_, err := fx.f.SudoBegin(context.Background(), 7, "mockop", "/security")
	if err == nil {
		t.Fatal("want error for unlinked slug, got nil")
	}
}

// TestFederator_SudoBegin_RejectsUnknownSlug verifies a disabled/unknown slug
// (GetUpstreamIDPBySlug excludes disabled) yields the provider-not-found path.
func TestFederator_SudoBegin_RejectsUnknownSlug(t *testing.T) {
	fx := newFixture(t, federationoidc.ModeAutoProvision)
	fx.q.identitiesByAccount[7] = []db.ListAccountIdentitiesByAccountRow{
		{AccountID: 7, UpstreamIdpID: fx.idp.ID, UpstreamSub: "sub-1", IdpSlug: "mockop", IdpDisplayName: "Mock OP"},
	}
	_, err := fx.f.SudoBegin(context.Background(), 7, "no-such-slug", "/security")
	if err == nil {
		t.Fatal("want error for unknown slug, got nil")
	}
}
