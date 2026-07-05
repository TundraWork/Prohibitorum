package oidc_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
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
	"prohibitorum/pkg/avatar"
	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/db"
	federationoidc "prohibitorum/pkg/federation/oidc"
	steamoidc "prohibitorum/pkg/federation/steam"
	"prohibitorum/pkg/kv"
)

// fakeFederatorQueries extends fakeModesQueries (modes_test.go) with the extra
// methods Federator needs: GetUpstreamIDPBySlug, ListAccountIdentitiesByAccount,
// UpsertAvatarSource, SetActiveAvatar, and ListAvatarSourcesByAccount.
type fakeFederatorQueries struct {
	*fakeModesQueries

	mu         sync.Mutex
	idpBySlug  map[string]db.UpstreamIdp
	idpSlugErr error

	identitiesByAccount    map[int32][]db.ListAccountIdentitiesByAccountRow
	identitiesByAccountErr error

	enrollmentByToken    map[string]db.Enrollment
	enrollmentByTokenErr error

	// Avatar-inherit recorders: the stored WebP bytes per account and the
	// upstream meta etag per account, so RunAvatarInheritForTest assertions can
	// confirm a store happened (or didn't). upsertedSources stores the most-recently
	// upserted row per (accountID, source) so ListAvatarSourcesByAccount can serve
	// the right etag back for the etag-skip logic.
	upsertedAvatar  map[int32][]byte
	setMetaUpstream map[int32]string
	upsertedSources map[int32]map[string]db.UpsertAvatarSourceParams // accountID → source → params

	// setActiveAvatarCalls records every SetActiveAvatar call for assertions.
	setActiveAvatarCalls []db.SetActiveAvatarParams
}

func newFakeFederatorQueries() *fakeFederatorQueries {
	return &fakeFederatorQueries{
		fakeModesQueries:    newFakeModesQueries(),
		idpBySlug:           map[string]db.UpstreamIdp{},
		identitiesByAccount: map[int32][]db.ListAccountIdentitiesByAccountRow{},
		enrollmentByToken:   map[string]db.Enrollment{},
		upsertedSources:     map[int32]map[string]db.UpsertAvatarSourceParams{},
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

func (f *fakeFederatorQueries) UpsertAvatarSource(_ context.Context, arg db.UpsertAvatarSourceParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.upsertedAvatar == nil {
		f.upsertedAvatar = map[int32][]byte{}
	}
	f.upsertedAvatar[arg.AccountID] = arg.Bytes
	if f.setMetaUpstream == nil {
		f.setMetaUpstream = map[int32]string{}
	}
	f.setMetaUpstream[arg.AccountID] = arg.Etag.String
	// Also store into upsertedSources so ListAvatarSourcesByAccount can serve
	// the correct etag back for subsequent etag-skip checks.
	if f.upsertedSources == nil {
		f.upsertedSources = map[int32]map[string]db.UpsertAvatarSourceParams{}
	}
	if f.upsertedSources[arg.AccountID] == nil {
		f.upsertedSources[arg.AccountID] = map[string]db.UpsertAvatarSourceParams{}
	}
	f.upsertedSources[arg.AccountID][arg.Source] = arg
	return nil
}

func (f *fakeFederatorQueries) SetActiveAvatar(_ context.Context, arg db.SetActiveAvatarParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setActiveAvatarCalls = append(f.setActiveAvatarCalls, arg)
	return nil
}

// ListAvatarSourcesByAccount returns rows built from any previously upserted
// avatar sources plus any rows pre-seeded via seedUpstreamRow.
func (f *fakeFederatorQueries) ListAvatarSourcesByAccount(_ context.Context, accountID int32) ([]db.ListAvatarSourcesByAccountRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var rows []db.ListAvatarSourcesByAccountRow
	for _, p := range f.upsertedSources[accountID] {
		rows = append(rows, db.ListAvatarSourcesByAccountRow{
			Source: p.Source,
			Etag:   p.Etag,
		})
	}
	return rows, nil
}

// seedUpstreamRow pre-seeds an 'upstream' avatar row (simulating a prior run)
// so ListAvatarSourcesByAccount returns a row with the given etag.
func (f *fakeFederatorQueries) seedUpstreamRow(accountID int32, etag string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.upsertedSources == nil {
		f.upsertedSources = map[int32]map[string]db.UpsertAvatarSourceParams{}
	}
	if f.upsertedSources[accountID] == nil {
		f.upsertedSources[accountID] = map[string]db.UpsertAvatarSourceParams{}
	}
	f.upsertedSources[accountID][avTestSource] = db.UpsertAvatarSourceParams{
		AccountID: accountID,
		Source:    avTestSource,
		Etag:      pgtype.Text{String: etag, Valid: true},
	}
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

	result, err := fx.f.HandleCallback(context.Background(), state, code, iss, req.AntiForgeryToken, nil)
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

	// Seed an existing CONFIRMED identity row for (iss=ts.URL, sub=sub-1):
	// a normal returning user whose confirmed_at is set, so the re-login
	// emits Use and issues a session directly (no /welcome gate).
	fx.q.identityErr = nil
	fx.q.identityResult = db.AccountIdentity{
		ID:            300,
		AccountID:     50,
		UpstreamIdpID: fx.idp.ID,
		UpstreamIss:   fx.ts.URL,
		UpstreamSub:   "sub-1",
		UpstreamEmail: pgtype.Text{String: "alice@example.com", Valid: true},
		ConfirmedAt:   pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}
	fx.q.accountByIDResults[50] = db.Account{ID: 50, Username: "alice", DisplayName: "Alice Example"}

	req, err := fx.f.BeginLogin(context.Background(), "mockop", "/me")
	if err != nil {
		t.Fatalf("BeginLogin: %v", err)
	}
	code, state, iss := driveAuthorizeFed(t, req.AuthorizeURL)

	result, err := fx.f.HandleCallback(context.Background(), state, code, iss, req.AntiForgeryToken, nil)
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
	_, err = fx.f.HandleCallback(context.Background(), state, code, "https://attacker.example/", req.AntiForgeryToken, nil)
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

	_, err = fx.f.HandleCallback(context.Background(), stateToken, code, "", req.AntiForgeryToken, nil)
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
	_, err := fx.f.HandleCallback(context.Background(), "totally-bogus-state", "any-code", "", "", nil)
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

	if _, err := fx.f.HandleCallback(context.Background(), state, code, iss, req.AntiForgeryToken, nil); err != nil {
		t.Fatalf("first HandleCallback: %v", err)
	}

	// Second call MUST fail — state was Pop'd on the first call.
	_, err = fx.f.HandleCallback(context.Background(), state, code, iss, req.AntiForgeryToken, nil)
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
	_, err = fx.f.HandleCallback(context.Background(), state, code, iss, "wrong-anti-forgery-token", nil)
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
	_, err = fx.f.HandleCallback(context.Background(), state2, code2, iss2, "", nil)
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

	_, err = fx.f.HandleCallback(context.Background(), state, code, iss, req.AntiForgeryToken, nil)
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
	_, err = fx.f.HandleCallback(context.Background(), state, "definitely-not-a-real-code", iss, req.AntiForgeryToken, nil)
	if ae := authn.AsAuthError(err); ae == nil || ae.Code != "federation_state_invalid" {
		t.Fatalf("want federation_state_invalid on bad code, got %v", err)
	}
	if r := auditReason(fx.au.snapshot(), audit.EventFail); r != "code_exchange_failed" {
		t.Fatalf("audit reason = %q, want code_exchange_failed", r)
	}
}

func TestFederator_HandleCallback_RejectsDisabledAccount(t *testing.T) {
	fx := newFixture(t, federationoidc.ModeAutoProvision)

	// Seed an existing CONFIRMED identity that points at a disabled account:
	// Resolve emits Use, then HandleCallback's post-resolve disabled check
	// rejects with bad_credentials + a separate account_disabled fail.
	fx.q.identityErr = nil
	fx.q.identityResult = db.AccountIdentity{
		ID:            300,
		AccountID:     77,
		UpstreamIdpID: fx.idp.ID,
		UpstreamIss:   fx.ts.URL,
		UpstreamSub:   "sub-1",
		UpstreamEmail: pgtype.Text{String: "alice@example.com", Valid: true},
		ConfirmedAt:   pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}
	fx.q.accountByIDResults[77] = db.Account{ID: 77, Username: "alice", DisplayName: "Alice", Disabled: true}

	req, err := fx.f.BeginLogin(context.Background(), "mockop", "/me")
	if err != nil {
		t.Fatalf("BeginLogin: %v", err)
	}
	code, state, iss := driveAuthorizeFed(t, req.AuthorizeURL)

	_, err = fx.f.HandleCallback(context.Background(), state, code, iss, req.AntiForgeryToken, nil)
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

	result, err := fx.f.LinkCallback(context.Background(), state, code, iss, acctID, url.Values{})
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
	_, err = fx.f.LinkCallback(context.Background(), state, code, iss, completingAs, url.Values{})
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

	_, err = fx.f.LinkCallback(context.Background(), state, code, iss, acctID, url.Values{})
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

	_, err = fx.f.LinkCallback(context.Background(), state, code, iss, acctID, url.Values{})
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

	_, err = fx.f.LinkCallback(context.Background(), state, code, iss, acctID, url.Values{})
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
	_, err = fx.f.LinkCallback(context.Background(), req.StateKey, "any-code", "", 99, url.Values{})
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

func TestFederator_BeginLogin_InviteOnly_RejectsPreAuth(t *testing.T) {
	// A plain sign-in on an invite_only IdP can never succeed (invites arrive
	// via BeginInviteRedemption), so begin() rejects BEFORE minting an authorize
	// URL rather than sending the user through the upstream dance to fail at the
	// callback.
	fx := newFixture(t, federationoidc.ModeInviteOnly)

	_, err := fx.f.BeginLogin(context.Background(), "mockop", "/me")
	if ae := authn.AsAuthError(err); ae == nil || ae.Code != "invite_required" {
		t.Fatalf("want invite_required AuthError, got %v", err)
	}
	if r := auditReason(fx.au.snapshot(), audit.EventFail); r != "invite_required_pre_auth" {
		t.Fatalf("audit fail reason = %q, want invite_required_pre_auth", r)
	}
}

func TestFederator_BeginLogin_LinkOnly_Proceeds(t *testing.T) {
	// link_only is NOT pre-gated: an already-linked identity re-logs-in via the
	// existing-identity path at the callback, so BeginLogin must still mint the
	// authorize URL (the upstream identity is unknown until the callback).
	fx := newFixture(t, federationoidc.ModeLinkOnly)

	req, err := fx.f.BeginLogin(context.Background(), "mockop", "/me")
	if err != nil {
		t.Fatalf("BeginLogin on link_only should proceed, got %v", err)
	}
	if req == nil || req.AuthorizeURL == "" {
		t.Fatal("expected an authorize URL")
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

	result, err := fx.f.HandleCallback(context.Background(), stateTok, code, iss, req.AntiForgeryToken, nil)
	if err != nil {
		t.Fatalf("HandleCallback: %v", err)
	}
	if !result.IsNew {
		t.Error("IsNew = false, want true (invite-minted fresh account)")
	}
	// The invite IS the authorization: HandleCallback threads Confirmed=true
	// so the HTTP layer issues a session immediately (no /welcome gate).
	if !result.Confirmed {
		t.Error("Confirmed = false, want true (invite auto-confirms)")
	}
	if result.IdentityID == 0 {
		t.Error("IdentityID = 0, want the inserted identity id")
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

// --- avatar-inherit tests -------------------------------------------------

// validPNG encodes a small opaque PNG that avatar.Process can decode + resize.
// A fully-transparent image is fine for the pipeline (it only needs a decodable
// raster); the bytes are deterministic so avatar.Process yields a stable etag.
func validPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return buf.Bytes()
}

// avatarFetchTokens builds a *Tokens carrying the upstream picture URL under the
// "picture" claim, so runAvatarInherit resolves it from id_token Raw and never
// needs to call UserInfo (we pass a nil client).
func avatarFetchTokens() *federationoidc.Tokens {
	return &federationoidc.Tokens{
		Raw: map[string]any{"picture": "https://pic.example/x.png"},
	}
}

// Per-upstream avatar test fixtures: inherited avatars are now keyed
// "upstream:<slug>" and the dedup key is per-(account, idp).
const (
	avTestSlug         = "mockop"
	avTestIDPID  int64 = 1
)

const avTestSource = "upstream:" + avTestSlug

// runInherit is a shorthand to drive the job synchronously with the test PNG,
// for the default test upstream (slug avTestSlug / id avTestIDPID).
func runInherit(t *testing.T, fx *fixtureFederator, pngBytes []byte, accountID int32) {
	t.Helper()
	federationoidc.SetAvatarFetchForTest(fx.f, func(_ context.Context, _ string, _ bool) ([]byte, error) {
		return pngBytes, nil
	})
	federationoidc.RunAvatarInheritForTest(
		fx.f, context.Background(), nil,
		db.UpstreamIdp{Slug: avTestSlug, ID: avTestIDPID, PictureClaim: "picture"}, avatarFetchTokens(), accountID,
	)
}

// TestAvatarInherit_FreshNullSource: active=NULL (never chosen) → upsert
// 'upstream' + SetActiveAvatar('upstream').
func TestAvatarInherit_FreshNullSource(t *testing.T) {
	fx := newFixture(t, federationoidc.ModeAutoProvision)
	// Account 7: no avatar row, avatar_source NULL.
	fx.q.accountByIDResults[7] = db.Account{ID: 7}

	pngBytes := validPNG(t)
	runInherit(t, fx, pngBytes, 7)

	if _, ok := fx.q.upsertedAvatar[7]; !ok {
		t.Fatal("want UpsertAvatarSource for account 7")
	}
	_, wantEtag, err := avatar.Process(pngBytes)
	if err != nil {
		t.Fatalf("avatar.Process: %v", err)
	}
	if got := fx.q.setMetaUpstream[7]; got != wantEtag {
		t.Errorf("stored etag = %q, want %q", got, wantEtag)
	}
	// SetActiveAvatar must have been called (active was NULL).
	fx.q.mu.Lock()
	calls := fx.q.setActiveAvatarCalls
	fx.q.mu.Unlock()
	if len(calls) == 0 {
		t.Fatal("want SetActiveAvatar called (active was NULL)")
	}
	if calls[0].Source != avTestSource || calls[0].AccountID != 7 {
		t.Errorf("SetActiveAvatar call = %+v, want {avTestSource 7}", calls[0])
	}
	// Status key cleared on completion.
	if _, err := fx.kvm.Get(context.Background(), federationoidc.AvatarFetchKey(7, avTestIDPID)); err == nil {
		t.Error("status key should be cleared after the job completes")
	}
}

// TestAvatarInherit_ActiveUser: active='user' → upsert 'upstream' (fetch NOT
// skipped), but NO SetActiveAvatar (deliberate user choice is preserved).
func TestAvatarInherit_ActiveUser(t *testing.T) {
	fx := newFixture(t, federationoidc.ModeAutoProvision)
	fx.q.accountByIDResults[7] = db.Account{
		ID:           7,
		AvatarSource: pgtype.Text{String: "user", Valid: true},
	}

	pngBytes := validPNG(t)
	runInherit(t, fx, pngBytes, 7)

	// Upstream row MUST have been stored (fetch not skipped).
	if _, ok := fx.q.upsertedAvatar[7]; !ok {
		t.Fatal("want UpsertAvatarSource for account 7 even when active='user'")
	}
	// SetActiveAvatar must NOT have been called.
	fx.q.mu.Lock()
	calls := fx.q.setActiveAvatarCalls
	fx.q.mu.Unlock()
	if len(calls) != 0 {
		t.Fatalf("must NOT call SetActiveAvatar when active='user'; got %d call(s)", len(calls))
	}
	// Status key cleared.
	if _, err := fx.kvm.Get(context.Background(), federationoidc.AvatarFetchKey(7, avTestIDPID)); err == nil {
		t.Error("status key should be cleared")
	}
}

// TestAvatarInherit_ActiveNone: active='none' → upsert 'upstream', NO
// SetActiveAvatar (explicit "no avatar" choice is preserved).
func TestAvatarInherit_ActiveNone(t *testing.T) {
	fx := newFixture(t, federationoidc.ModeAutoProvision)
	fx.q.accountByIDResults[7] = db.Account{
		ID:           7,
		AvatarSource: pgtype.Text{String: "none", Valid: true},
	}

	pngBytes := validPNG(t)
	runInherit(t, fx, pngBytes, 7)

	// Upstream row stored.
	if _, ok := fx.q.upsertedAvatar[7]; !ok {
		t.Fatal("want UpsertAvatarSource for account 7 even when active='none'")
	}
	// SetActiveAvatar must NOT have been called.
	fx.q.mu.Lock()
	calls := fx.q.setActiveAvatarCalls
	fx.q.mu.Unlock()
	if len(calls) != 0 {
		t.Fatalf("must NOT call SetActiveAvatar when active='none'; got %d call(s)", len(calls))
	}
	// Status key cleared.
	if _, err := fx.kvm.Get(context.Background(), federationoidc.AvatarFetchKey(7, avTestIDPID)); err == nil {
		t.Error("status key should be cleared")
	}
}

// TestAvatarInherit_ActiveUpstreamChanged: active='upstream', existing upstream
// row has a DIFFERENT etag → upsert (changed) + SetActiveAvatar('upstream') to
// refresh the denormalized active etag cache.
func TestAvatarInherit_ActiveUpstreamChanged(t *testing.T) {
	fx := newFixture(t, federationoidc.ModeAutoProvision)
	fx.q.accountByIDResults[7] = db.Account{
		ID:           7,
		AvatarSource: pgtype.Text{String: avTestSource, Valid: true},
		AvatarEtag:   pgtype.Text{String: "old-etag", Valid: true},
	}
	// Seed a prior upstream row with a different etag.
	fx.q.seedUpstreamRow(7, "old-etag")

	pngBytes := validPNG(t)
	runInherit(t, fx, pngBytes, 7)

	// Upstream row must be re-upserted (etag changed).
	if _, ok := fx.q.upsertedAvatar[7]; !ok {
		t.Fatal("want UpsertAvatarSource when upstream etag changed")
	}
	// SetActiveAvatar must have been called (active='upstream' + bytes changed).
	fx.q.mu.Lock()
	calls := fx.q.setActiveAvatarCalls
	fx.q.mu.Unlock()
	if len(calls) == 0 {
		t.Fatal("want SetActiveAvatar when active='upstream' and etag changed")
	}
	if calls[0].Source != avTestSource || calls[0].AccountID != 7 {
		t.Errorf("SetActiveAvatar call = %+v, want {avTestSource 7}", calls[0])
	}
	// Status key cleared.
	if _, err := fx.kvm.Get(context.Background(), federationoidc.AvatarFetchKey(7, avTestIDPID)); err == nil {
		t.Error("status key should be cleared")
	}
}

// TestAvatarInherit_UnchangedEtag: existing upstream row etag == processed etag
// → NO upsert, NO SetActiveAvatar.
func TestAvatarInherit_UnchangedEtag(t *testing.T) {
	fx := newFixture(t, federationoidc.ModeAutoProvision)

	pngBytes := validPNG(t)
	_, etag, err := avatar.Process(pngBytes)
	if err != nil {
		t.Fatalf("avatar.Process: %v", err)
	}
	// Seed account 7 with active='upstream' and the same etag the fetch will produce.
	fx.q.accountByIDResults[7] = db.Account{
		ID:           7,
		AvatarSource: pgtype.Text{String: avTestSource, Valid: true},
		AvatarEtag:   pgtype.Text{String: etag, Valid: true},
	}
	// Seed the upstream row with the same etag so ListAvatarSourcesByAccount returns it.
	fx.q.seedUpstreamRow(7, etag)

	runInherit(t, fx, pngBytes, 7)

	// No upsert when etag is unchanged.
	if _, ok := fx.q.upsertedAvatar[7]; ok {
		t.Fatal("must NOT re-upsert bytes when the upstream etag is unchanged")
	}
	// No SetActiveAvatar when nothing changed.
	fx.q.mu.Lock()
	calls := fx.q.setActiveAvatarCalls
	fx.q.mu.Unlock()
	if len(calls) != 0 {
		t.Fatalf("must NOT call SetActiveAvatar when etag unchanged; got %d call(s)", len(calls))
	}
	// Status key cleared.
	if _, err := fx.kvm.Get(context.Background(), federationoidc.AvatarFetchKey(7, avTestIDPID)); err == nil {
		t.Error("status key should be cleared after the unchanged-etag no-op")
	}
}

// TestAvatarInherit_DedupesViaSetNX verifies concurrent dedup: a second
// invocation while the first holds the KV key exits without doing any work.
func TestAvatarInherit_DedupesViaSetNX(t *testing.T) {
	fx := newFixture(t, federationoidc.ModeAutoProvision)
	fx.q.accountByIDResults[7] = db.Account{ID: 7}

	// Simulate a concurrent run already in flight: pre-set the status key so the
	// SetNX inside runAvatarInherit fails and the second run exits immediately.
	ok, err := fx.kvm.SetNX(context.Background(), federationoidc.AvatarFetchKey(7, avTestIDPID), "1", time.Minute)
	if err != nil || !ok {
		t.Fatalf("seed SetNX: ok=%v err=%v", ok, err)
	}

	called := false
	federationoidc.SetAvatarFetchForTest(fx.f, func(_ context.Context, _ string, _ bool) ([]byte, error) {
		called = true
		return validPNG(t), nil
	})

	federationoidc.RunAvatarInheritForTest(
		fx.f, context.Background(), nil,
		db.UpstreamIdp{Slug: avTestSlug, ID: avTestIDPID, PictureClaim: "picture"}, avatarFetchTokens(), 7,
	)

	if called {
		t.Fatal("second run must not fetch — SetNX dedup should make it exit immediately")
	}
	if _, ok := fx.q.setMetaUpstream[7]; ok {
		t.Fatal("second run must not store while another run holds the key")
	}
	// The deduped run must NOT delete the key it never acquired (the in-flight
	// run owns it).
	if _, err := fx.kvm.Get(context.Background(), federationoidc.AvatarFetchKey(7, avTestIDPID)); err != nil {
		t.Errorf("deduped run must leave the in-flight key intact, got %v", err)
	}
}

// TestAvatarInherit_SecondUpstreamDoesNotStealActive: a user already showing one
// upstream's avatar logs in via a DIFFERENT upstream. The second avatar is
// stored, but it must NOT steal the active selection — the user keeps the first
// (and can switch in the picker). This is the core per-upstream guarantee.
func TestAvatarInherit_SecondUpstreamDoesNotStealActive(t *testing.T) {
	fx := newFixture(t, federationoidc.ModeAutoProvision)
	// Active = the FIRST upstream's source.
	fx.q.accountByIDResults[7] = db.Account{
		ID:           7,
		AvatarSource: pgtype.Text{String: avTestSource, Valid: true},
	}

	pngBytes := validPNG(t)
	federationoidc.SetAvatarFetchForTest(fx.f, func(_ context.Context, _ string, _ bool) ([]byte, error) {
		return pngBytes, nil
	})
	// Inherit from a SECOND, different upstream (slug "other", id 2).
	federationoidc.RunAvatarInheritForTest(
		fx.f, context.Background(), nil,
		db.UpstreamIdp{Slug: "other", ID: 2, PictureClaim: "picture"}, avatarFetchTokens(), 7,
	)

	// The second upstream's avatar row must be stored.
	if _, ok := fx.q.upsertedAvatar[7]; !ok {
		t.Fatal("want UpsertAvatarSource for the second upstream")
	}
	// But it must NOT activate — the first upstream stays active.
	fx.q.mu.Lock()
	calls := fx.q.setActiveAvatarCalls
	fx.q.mu.Unlock()
	if len(calls) != 0 {
		t.Fatalf("must NOT call SetActiveAvatar when a different upstream is already active; got %d call(s)", len(calls))
	}
}

// --- Steam OpenID 2.0 branch tests ----------------------------------------

// newSteamFixture builds a fixtureFederator wired with a Steam-protocol IdP
// (no mock OP, no OIDC client). The Steam seams are NOT yet stubbed — callers
// must call SetSteamSeamsForTest before driving the callback.
func newSteamFixture(t *testing.T, mode string) *fixtureFederator {
	t.Helper()

	const idpID int64 = 99
	const keyVersion int32 = 1

	// The Steam API key lives in the same ClientSecretEnc field as the OIDC
	// client secret. Encrypt a known value so steamTokens can decrypt it.
	ct, nonce, err := federationoidc.EncryptClientSecret(testDEK, []byte("steam-api-key"), idpID, keyVersion)
	if err != nil {
		t.Fatalf("EncryptClientSecret: %v", err)
	}

	idp := db.UpstreamIdp{
		ID:               idpID,
		Slug:             "steam",
		DisplayName:      "Steam",
		Protocol:         "steam",
		IssuerUrl:        "https://steamcommunity.com/openid", // informational only
		ClientID:         "",                                   // unused for Steam
		ClientSecretEnc:  ct,
		SecretNonce:      nonce,
		KeyVersion:       keyVersion,
		Mode:             mode,
		// Claim fields unused on steam path but populated for completeness.
		UsernameClaim:    "preferred_username",
		DisplayNameClaim: "name",
		EmailClaim:       "email",
		PictureClaim:     "picture",
	}

	q := newFakeFederatorQueries()
	q.idpBySlug[idp.Slug] = idp

	kvm := kv.NewMemoryStore()
	t.Cleanup(func() { _ = kvm.Close() })

	au := &recordingAudit{}

	cfg := configx.FederationConfig{
		StateTTL:      5 * time.Minute,
		DefaultScopes: []string{},
	}
	deks := map[int][]byte{1: testDEK}
	origin := "https://idp.example.test"

	fd := federationoidc.NewFederator(q, kvm, au, cfg, deks, nil, origin)

	return &fixtureFederator{
		t: t, idp: idp, q: q, kvm: kvm, au: au,
		f: fd, cfg: cfg, origin: origin,
	}
}

// TestFederator_BeginSteam_BuildsCorrectURL verifies that BeginLogin on a
// Steam-protocol IdP returns a Steam OpenID 2.0 checkid_setup URL (not an
// OIDC /authorize URL), stashes the FedState under LoginKey with Protocol-
// agnostic fields (IDPID, IDPSlug, ReturnTo, BrowserBinding), and leaves
// OIDC-only fields (ExpectedIss, ExpectedTokenEndpoint, Nonce, CodeVerifier)
// empty so HandleCallback can skip the OIDC drift check.
func TestFederator_BeginSteam_BuildsCorrectURL(t *testing.T) {
	fx := newSteamFixture(t, federationoidc.ModeAutoProvision)

	req, err := fx.f.BeginLogin(context.Background(), "steam", "/dashboard")
	if err != nil {
		t.Fatalf("BeginLogin (steam): %v", err)
	}
	if req.StateKey == "" {
		t.Fatal("StateKey empty")
	}
	if req.AntiForgeryToken == "" {
		t.Fatal("AntiForgeryToken empty (login flow should set it)")
	}

	// AuthorizeURL should point at Steam's checkid_setup endpoint.
	if !strings.HasPrefix(req.AuthorizeURL, "https://steamcommunity.com/openid/login") {
		t.Errorf("AuthorizeURL should be Steam login URL, got %s", req.AuthorizeURL)
	}
	u, err := url.Parse(req.AuthorizeURL)
	if err != nil {
		t.Fatalf("parse AuthorizeURL: %v", err)
	}
	if u.Query().Get("openid.mode") != "checkid_setup" {
		t.Errorf("openid.mode = %q, want checkid_setup", u.Query().Get("openid.mode"))
	}

	// The return_to should carry our state token.
	returnTo := u.Query().Get("openid.return_to")
	if !strings.Contains(returnTo, req.StateKey) {
		t.Errorf("openid.return_to %q should contain state token %q", returnTo, req.StateKey)
	}

	// State must live under LoginKey.
	blob, err := fx.kvm.Get(context.Background(), federationoidc.LoginKey(req.StateKey))
	if err != nil {
		t.Fatalf("state missing from LoginKey: %v", err)
	}
	state, err := federationoidc.DecodeFedState(blob)
	if err != nil {
		t.Fatalf("DecodeFedState: %v", err)
	}
	if state.IDPSlug != "steam" {
		t.Errorf("IDPSlug = %q, want steam", state.IDPSlug)
	}
	if state.ReturnTo != "/dashboard" {
		t.Errorf("ReturnTo = %q, want /dashboard", state.ReturnTo)
	}
	// OIDC-only fields should be empty for Steam.
	if state.ExpectedIss != "" {
		t.Errorf("ExpectedIss should be empty for Steam, got %q", state.ExpectedIss)
	}
	if state.ExpectedTokenEndpoint != "" {
		t.Errorf("ExpectedTokenEndpoint should be empty for Steam, got %q", state.ExpectedTokenEndpoint)
	}
	if state.Nonce != "" {
		t.Errorf("Nonce should be empty for Steam, got %q", state.Nonce)
	}
	if state.CodeVerifier != "" {
		t.Errorf("CodeVerifier should be empty for Steam, got %q", state.CodeVerifier)
	}
	if state.BrowserBinding == "" {
		t.Error("BrowserBinding should be set for login flow")
	}
}

// TestFederator_HandleCallback_Steam_HappyPath drives the full Steam callback
// path: stub steamVerify (returns a fixed SteamID64) + steamSummary (returns
// persona name + avatar URL), pre-stash the LoginKey state (so PopState works
// without going through BeginLogin), and assert that an account is auto-
// provisioned with username "steam_<id>" + the persona name as display name.
func TestFederator_HandleCallback_Steam_HappyPath(t *testing.T) {
	const steamID = "76561198000000001"
	const personaName = "ProGamer"
	const avatarURL = "https://cdn.steamstatic.com/test.jpg"

	fx := newSteamFixture(t, federationoidc.ModeAutoProvision)

	// Stub the Steam seams: verify always succeeds; summary returns fixed data.
	federationoidc.SetSteamSeamsForTest(fx.f,
		func(_ context.Context, _ url.Values, _ string) (string, error) {
			return steamID, nil
		},
		func(_ context.Context, _, _ string) (steamoidc.Summary, error) {
			return steamoidc.Summary{PersonaName: personaName, AvatarURL: avatarURL}, nil
		},
	)

	// Use BeginLogin to stash the FedState (so we get a valid stateToken +
	// anti-forgery token). BeginLogin on a steam IdP goes through beginSteam
	// and populates LoginKey, which HandleCallback will Pop.
	req, err := fx.f.BeginLogin(context.Background(), "steam", "/me")
	if err != nil {
		t.Fatalf("BeginLogin (steam): %v", err)
	}

	// Construct synthetic callback params (what Steam would send back).
	// The openid.mode must be "id_res" for steam.Verify; the stub ignores the
	// actual params so we only need a plausible set.
	callbackParams := url.Values{
		"openid.mode":      {"id_res"},
		"openid.ns":        {"http://specs.openid.net/auth/2.0"},
		"openid.claimed_id": {"https://steamcommunity.com/openid/id/" + steamID},
		"openid.identity":  {"https://steamcommunity.com/openid/id/" + steamID},
		"openid.return_to": {fx.origin + "/api/prohibitorum/auth/federation/steam/callback?state=" + req.StateKey},
		"state":            {req.StateKey},
	}

	// On Steam flow, code and iss are empty (Steam doesn't use them).
	result, err := fx.f.HandleCallback(context.Background(), req.StateKey, "", "", req.AntiForgeryToken, callbackParams)
	if err != nil {
		t.Fatalf("HandleCallback (steam): %v", err)
	}

	// A new account must have been provisioned.
	if !result.IsNew {
		t.Error("IsNew = false, want true (new Steam user)")
	}
	if result.IDPSlug != "steam" {
		t.Errorf("IDPSlug = %q, want steam", result.IDPSlug)
	}
	if result.ReturnTo != "/me" {
		t.Errorf("ReturnTo = %q, want /me", result.ReturnTo)
	}

	// AMR must carry ["steam"].
	if len(result.AMR) != 1 || result.AMR[0] != "steam" {
		t.Errorf("AMR = %v, want [steam]", result.AMR)
	}

	// Account row must use the "steam_<id>" username and persona display name.
	if len(fx.q.insertedAccounts) != 1 {
		t.Fatalf("want 1 inserted account, got %d", len(fx.q.insertedAccounts))
	}
	wantUsername := "steam_" + steamID
	if fx.q.insertedAccount.Username != wantUsername {
		t.Errorf("username = %q, want %q", fx.q.insertedAccount.Username, wantUsername)
	}
	if fx.q.insertedAccount.DisplayName != personaName {
		t.Errorf("display_name = %q, want %q", fx.q.insertedAccount.DisplayName, personaName)
	}

	// Identity row must use the Steam issuer + SteamID as subject.
	if fx.q.insertedIdentity.UpstreamIss != "https://steamcommunity.com/openid" {
		t.Errorf("identity upstream_iss = %q, want https://steamcommunity.com/openid", fx.q.insertedIdentity.UpstreamIss)
	}
	if fx.q.insertedIdentity.UpstreamSub != steamID {
		t.Errorf("identity upstream_sub = %q, want %q", fx.q.insertedIdentity.UpstreamSub, steamID)
	}

	// Audit: Register + Use.
	recs := fx.au.snapshot()
	if findEvent(recs, audit.EventRegister) == nil {
		t.Error("missing audit Register")
	}
	if findEvent(recs, audit.EventUse) == nil {
		t.Error("missing audit Use")
	}
}

// TestFederator_LinkCallback_Steam_HappyPath drives the Steam branch of
// LinkCallback: an authenticated user links a Steam identity to their existing
// account. Stubs the Steam seams, seeds a link-flow KV state via LinkBegin,
// then calls LinkCallback with openid.* callback params. Asserts that an
// account_identity row is inserted + confirmed for the existing account (no new
// account created), and that EventLink is emitted with the correct account_id.
func TestFederator_LinkCallback_Steam_HappyPath(t *testing.T) {
	const steamID = "76561198000000002"
	const personaName = "LinkTestGamer"
	const avatarURL = "https://cdn.steamstatic.com/link-test.jpg"
	const acctID int32 = 42

	fx := newSteamFixture(t, federationoidc.ModeAutoProvision)

	// Stub the Steam seams: verify always succeeds; summary returns fixed data.
	federationoidc.SetSteamSeamsForTest(fx.f,
		func(_ context.Context, _ url.Values, _ string) (string, error) {
			return steamID, nil
		},
		func(_ context.Context, _, _ string) (steamoidc.Summary, error) {
			return steamoidc.Summary{PersonaName: personaName, AvatarURL: avatarURL}, nil
		},
	)

	// Start the link flow for acctID. LinkBegin on a steam IdP goes through
	// beginSteam and stashes FedState under LinkKey with LinkingAccountID=acctID.
	req, err := fx.f.LinkBegin(context.Background(), acctID, "steam", "/me/identities")
	if err != nil {
		t.Fatalf("LinkBegin (steam): %v", err)
	}

	// Construct synthetic callback params matching what Steam sends back.
	// The stub ignores the actual params; we need openid.mode=id_res so the
	// steam path doesn't short-circuit on a cancelled flow.
	callbackParams := url.Values{
		"openid.mode":       {"id_res"},
		"openid.ns":         {"http://specs.openid.net/auth/2.0"},
		"openid.claimed_id": {"https://steamcommunity.com/openid/id/" + steamID},
		"openid.identity":   {"https://steamcommunity.com/openid/id/" + steamID},
		"openid.return_to":  {fx.origin + "/api/prohibitorum/me/identities/link/steam/callback?state=" + req.StateKey},
		"state":             {req.StateKey},
	}

	// Steam link: code and iss are unused.
	result, err := fx.f.LinkCallback(context.Background(), req.StateKey, "", "", acctID, callbackParams)
	if err != nil {
		t.Fatalf("LinkCallback (steam): %v", err)
	}
	if result.IDPSlug != "steam" {
		t.Errorf("IDPSlug = %q, want steam", result.IDPSlug)
	}
	if result.ReturnTo != "/me/identities" {
		t.Errorf("ReturnTo = %q, want /me/identities", result.ReturnTo)
	}

	// Identity row must be bound to the existing account (not a new one).
	if fx.q.insertedIdentity.AccountID != acctID {
		t.Errorf("inserted identity AccountID = %d, want %d", fx.q.insertedIdentity.AccountID, acctID)
	}
	// No new account must have been created.
	if len(fx.q.insertedAccounts) != 0 {
		t.Errorf("link MUST NOT insert a new account; got %d", len(fx.q.insertedAccounts))
	}
	// Steam issuer + SteamID as subject.
	if fx.q.insertedIdentity.UpstreamIss != "https://steamcommunity.com/openid" {
		t.Errorf("identity upstream_iss = %q, want https://steamcommunity.com/openid", fx.q.insertedIdentity.UpstreamIss)
	}
	if fx.q.insertedIdentity.UpstreamSub != steamID {
		t.Errorf("identity upstream_sub = %q, want %q", fx.q.insertedIdentity.UpstreamSub, steamID)
	}

	// Audit: EventLink emitted with the linking account ID.
	recs := fx.au.snapshot()
	link := findEvent(recs, audit.EventLink)
	if link == nil {
		t.Fatal("missing audit EventLink")
	}
	if link.AccountID == nil || *link.AccountID != acctID {
		t.Errorf("Link account = %v, want %d", link.AccountID, acctID)
	}
	if link.Detail["idp_slug"] != "steam" {
		t.Errorf("Link detail idp_slug = %v, want steam", link.Detail["idp_slug"])
	}
}
