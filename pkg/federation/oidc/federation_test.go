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
}

func newFakeFederatorQueries() *fakeFederatorQueries {
	return &fakeFederatorQueries{
		fakeModesQueries:    newFakeModesQueries(),
		idpBySlug:           map[string]db.UpstreamIdp{},
		identitiesByAccount: map[int32][]db.ListAccountIdentitiesByAccountRow{},
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
		StateTTL:      5 * time.Minute,
		DefaultScopes: []string{"openid", "profile", "email"},
	}
	deks := map[int][]byte{1: testDEK}
	origin := "https://idp.example.test"

	fd := federationoidc.NewFederator(q, kvm, au, cfg, deks, origin)

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

	result, err := fx.f.HandleCallback(context.Background(), state, code, iss)
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

	result, err := fx.f.HandleCallback(context.Background(), state, code, iss)
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
	_, err = fx.f.HandleCallback(context.Background(), state, code, "https://attacker.example/")
	if ae := authn.AsAuthError(err); ae == nil || ae.Code != "federation_state_invalid" {
		t.Fatalf("want federation_state_invalid, got %v", err)
	}
	if r := auditReason(fx.au.snapshot(), audit.EventFail); r != "iss_mismatch_callback" {
		t.Fatalf("audit reason = %q, want iss_mismatch_callback", r)
	}
}

func TestFederator_HandleCallback_RejectsMissingState(t *testing.T) {
	fx := newFixture(t, federationoidc.ModeAutoProvision)

	// Token that never went into KV.
	_, err := fx.f.HandleCallback(context.Background(), "totally-bogus-state", "any-code", "")
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

	if _, err := fx.f.HandleCallback(context.Background(), state, code, iss); err != nil {
		t.Fatalf("first HandleCallback: %v", err)
	}

	// Second call MUST fail — state was Pop'd on the first call.
	_, err = fx.f.HandleCallback(context.Background(), state, code, iss)
	if ae := authn.AsAuthError(err); ae == nil || ae.Code != "federation_state_invalid" {
		t.Fatalf("want federation_state_invalid on replay, got %v", err)
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
	_, err = fx.f.HandleCallback(context.Background(), state, "definitely-not-a-real-code", iss)
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

	_, err = fx.f.HandleCallback(context.Background(), state, code, iss)
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
