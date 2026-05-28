package oidc_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/db"
	federationoidc "prohibitorum/pkg/federation/oidc"
)

// fakeModesQueries is the test-only subset of db.Querier the mode
// policies touch. We do NOT embed db.Querier — modes.go uses the narrow
// ModesQueries interface, so the fake only needs those seven methods.
// If a future change to modes.go reaches for a query we didn't fake,
// the compiler will surface it the moment we update the interface.
type fakeModesQueries struct {
	mu sync.Mutex

	identityResult db.AccountIdentity
	identityErr    error

	accountByIDResults map[int32]db.Account
	accountByIDErr     error

	accountByUsername    map[string]db.Account
	accountByUsernameErr error // override for "not found" tests, etc.

	nextAccountID    int32
	insertedAccount  db.InsertAccountParams
	insertedAccounts []db.Account
	insertAccountErr error

	nextIdentityID    int64
	insertedIdentity  db.InsertAccountIdentityParams
	insertIdentityErr error

	displayNameCalls []db.UpdateAccountDisplayNameParams
	emailCalls       []db.UpdateAccountIdentityEmailParams
}

func newFakeModesQueries() *fakeModesQueries {
	return &fakeModesQueries{
		accountByIDResults: map[int32]db.Account{},
		accountByUsername:  map[string]db.Account{},
		nextAccountID:      100,
		nextIdentityID:     200,
		// default: identity lookup returns ErrNoRows (first-sight path)
		identityErr: pgx.ErrNoRows,
		// default: username lookup returns ErrNoRows (no collision)
		accountByUsernameErr: pgx.ErrNoRows,
	}
}

func (f *fakeModesQueries) GetAccountIdentityByIssuerSub(_ context.Context, _ db.GetAccountIdentityByIssuerSubParams) (db.AccountIdentity, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.identityResult, f.identityErr
}

func (f *fakeModesQueries) GetAccountByID(_ context.Context, id int32) (db.Account, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.accountByIDErr != nil {
		return db.Account{}, f.accountByIDErr
	}
	if a, ok := f.accountByIDResults[id]; ok {
		return a, nil
	}
	return db.Account{}, pgx.ErrNoRows
}

func (f *fakeModesQueries) GetAccountByUsername(_ context.Context, u string) (db.Account, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if a, ok := f.accountByUsername[u]; ok {
		return a, nil
	}
	return db.Account{}, f.accountByUsernameErr
}

func (f *fakeModesQueries) InsertAccount(_ context.Context, arg db.InsertAccountParams) (db.Account, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.insertAccountErr != nil {
		return db.Account{}, f.insertAccountErr
	}
	f.insertedAccount = arg
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
	// Index by ID so post-insert GetAccountByID (e.g. federation's
	// post-resolve disabled-account check) finds the row.
	f.accountByIDResults[id] = acct
	return acct, nil
}

func (f *fakeModesQueries) InsertAccountIdentity(_ context.Context, arg db.InsertAccountIdentityParams) (db.AccountIdentity, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.insertIdentityErr != nil {
		return db.AccountIdentity{}, f.insertIdentityErr
	}
	f.insertedIdentity = arg
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

func (f *fakeModesQueries) UpdateAccountDisplayName(_ context.Context, arg db.UpdateAccountDisplayNameParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.displayNameCalls = append(f.displayNameCalls, arg)
	return nil
}

func (f *fakeModesQueries) UpdateAccountIdentityEmail(_ context.Context, arg db.UpdateAccountIdentityEmailParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.emailCalls = append(f.emailCalls, arg)
	return nil
}

// recordingAudit captures every audit.Record so tests can assert on
// counts, events, and detail contents.
type recordingAudit struct {
	mu      sync.Mutex
	records []audit.Record
}

func (r *recordingAudit) Record(_ context.Context, rec audit.Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, rec)
	return nil
}

func (r *recordingAudit) snapshot() []audit.Record {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]audit.Record, len(r.records))
	copy(out, r.records)
	return out
}

// helpers --------------------------------------------------------------

func newIDP(mode string) *db.UpstreamIdp {
	return &db.UpstreamIdp{
		ID:                   42,
		Slug:                 "test-idp",
		IssuerUrl:            "https://issuer.example/",
		Mode:                 mode,
		RequireVerifiedEmail: true,
	}
}

func goodTokens() *federationoidc.Tokens {
	return &federationoidc.Tokens{
		Issuer:            "https://issuer.example/",
		Subject:           "sub-1",
		Email:             "alice@example.com",
		EmailVerified:     true,
		PreferredUsername: "alice",
		Name:              "Alice Example",
	}
}

func findEvent(records []audit.Record, event string) *audit.Record {
	for i := range records {
		if records[i].Event == event {
			return &records[i]
		}
	}
	return nil
}

// tests ----------------------------------------------------------------

func TestApplyAutoProvision_HappyPath(t *testing.T) {
	q := newFakeModesQueries()
	a := &recordingAudit{}
	idp := newIDP(federationoidc.ModeAutoProvision)
	tok := goodTokens()

	id, isNew, err := federationoidc.Resolve(context.Background(), q, a, idp, tok)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !isNew {
		t.Fatalf("want isNew=true, got false")
	}
	if id != 100 {
		t.Fatalf("want accountID=100, got %d", id)
	}

	if q.insertedAccount.Username != "alice" ||
		q.insertedAccount.DisplayName != "Alice Example" ||
		q.insertedAccount.Role != "user" ||
		q.insertedAccount.Disabled {
		t.Fatalf("InsertAccount args wrong: %+v", q.insertedAccount)
	}
	if string(q.insertedAccount.Attributes) != "{}" {
		t.Fatalf("Attributes want '{}', got %q", string(q.insertedAccount.Attributes))
	}
	// webauthn_user_handle is NOT NULL UNIQUE in the schema; auto-provision
	// MUST mint a handle (via acctpkg.GenerateUserHandle) before insert.
	if len(q.insertedAccount.WebauthnUserHandle) == 0 {
		t.Fatalf("WebauthnUserHandle must be non-empty (NOT NULL in schema); got %v", q.insertedAccount.WebauthnUserHandle)
	}

	if q.insertedIdentity.AccountID != 100 ||
		q.insertedIdentity.UpstreamIdpID != idp.ID ||
		q.insertedIdentity.UpstreamIss != tok.Issuer ||
		q.insertedIdentity.UpstreamSub != tok.Subject ||
		!q.insertedIdentity.UpstreamEmail.Valid ||
		q.insertedIdentity.UpstreamEmail.String != tok.Email {
		t.Fatalf("InsertAccountIdentity args wrong: %+v", q.insertedIdentity)
	}

	recs := a.snapshot()
	if len(recs) != 2 {
		t.Fatalf("want 2 audit records, got %d (%+v)", len(recs), recs)
	}
	if recs[0].Event != audit.EventRegister || recs[1].Event != audit.EventUse {
		t.Fatalf("want register+use, got %s+%s", recs[0].Event, recs[1].Event)
	}
	for _, r := range recs {
		if r.Factor != audit.FactorFederationOIDC {
			t.Fatalf("factor: want federation_oidc, got %q", r.Factor)
		}
		if r.AccountID == nil || *r.AccountID != 100 {
			t.Fatalf("account_id: want 100, got %v", r.AccountID)
		}
		if d := r.Detail; d["idp_slug"] != "test-idp" || d["iss"] != tok.Issuer || d["sub"] != tok.Subject {
			t.Fatalf("detail missing iss/sub/slug: %+v", d)
		}
	}
}

func TestApplyAutoProvision_EmailNotVerifiedRejected(t *testing.T) {
	q := newFakeModesQueries()
	a := &recordingAudit{}
	idp := newIDP(federationoidc.ModeAutoProvision)
	tok := goodTokens()
	tok.EmailVerified = false

	_, _, err := federationoidc.Resolve(context.Background(), q, a, idp, tok)
	if ae := authn.AsAuthError(err); ae == nil || ae.Code != "email_not_verified" {
		t.Fatalf("want email_not_verified, got %v", err)
	}

	recs := a.snapshot()
	if len(recs) != 1 || recs[0].Event != audit.EventFail {
		t.Fatalf("want 1 fail row, got %+v", recs)
	}
	if recs[0].Detail["reason"] != "email_not_verified" {
		t.Fatalf("reason: want email_not_verified, got %v", recs[0].Detail["reason"])
	}
	if len(q.insertedAccounts) != 0 {
		t.Fatalf("no account should have been inserted")
	}
}

func TestApplyAutoProvision_DomainNotAllowedRejected(t *testing.T) {
	q := newFakeModesQueries()
	a := &recordingAudit{}
	idp := newIDP(federationoidc.ModeAutoProvision)
	idp.AllowedDomains = []string{"corp.example", "other.example"}
	tok := goodTokens() // alice@example.com

	_, _, err := federationoidc.Resolve(context.Background(), q, a, idp, tok)
	if ae := authn.AsAuthError(err); ae == nil || ae.Code != "invite_required" {
		t.Fatalf("want invite_required, got %v", err)
	}

	recs := a.snapshot()
	if len(recs) != 1 || recs[0].Event != audit.EventFail {
		t.Fatalf("want 1 fail row, got %+v", recs)
	}
	if recs[0].Detail["reason"] != "domain_not_allowed" {
		t.Fatalf("reason: want domain_not_allowed, got %v", recs[0].Detail["reason"])
	}
}

func TestApplyAutoProvision_DomainAllowedCaseInsensitive(t *testing.T) {
	q := newFakeModesQueries()
	a := &recordingAudit{}
	idp := newIDP(federationoidc.ModeAutoProvision)
	idp.AllowedDomains = []string{"EXAMPLE.com"}
	tok := goodTokens() // alice@example.com

	if _, _, err := federationoidc.Resolve(context.Background(), q, a, idp, tok); err != nil {
		t.Fatalf("expected provision to succeed (case-insensitive domain match), got %v", err)
	}
}

func TestApplyAutoProvision_UsernameCollisionRejected(t *testing.T) {
	q := newFakeModesQueries()
	q.accountByUsername["alice"] = db.Account{ID: 7, Username: "alice"}
	a := &recordingAudit{}
	idp := newIDP(federationoidc.ModeAutoProvision)
	tok := goodTokens()

	_, _, err := federationoidc.Resolve(context.Background(), q, a, idp, tok)
	if ae := authn.AsAuthError(err); ae == nil || ae.Code != "username_collision" {
		t.Fatalf("want username_collision, got %v", err)
	}

	recs := a.snapshot()
	if len(recs) != 1 || recs[0].Event != audit.EventFail {
		t.Fatalf("want 1 fail row, got %+v", recs)
	}
	if recs[0].Detail["reason"] != "username_collision" {
		t.Fatalf("reason: want username_collision, got %v", recs[0].Detail["reason"])
	}
	if recs[0].Detail["username"] != "alice" {
		t.Fatalf("username in detail: want alice, got %v", recs[0].Detail["username"])
	}
}

func TestApplyAutoProvision_MissingPreferredUsername(t *testing.T) {
	q := newFakeModesQueries()
	a := &recordingAudit{}
	idp := newIDP(federationoidc.ModeAutoProvision)
	tok := goodTokens()
	tok.PreferredUsername = ""

	_, _, err := federationoidc.Resolve(context.Background(), q, a, idp, tok)
	if err == nil {
		t.Fatal("want error for missing preferred_username")
	}
	if ae := authn.AsAuthError(err); ae != nil {
		t.Fatalf("missing preferred_username should not be an AuthError (it's a config bug -> 500), got %v", ae)
	}
}

func TestApplyInviteOnly_AlwaysRejects(t *testing.T) {
	q := newFakeModesQueries()
	a := &recordingAudit{}
	idp := newIDP(federationoidc.ModeInviteOnly)
	tok := goodTokens()

	_, _, err := federationoidc.Resolve(context.Background(), q, a, idp, tok)
	if ae := authn.AsAuthError(err); ae == nil || ae.Code != "invite_required" {
		t.Fatalf("want invite_required, got %v", err)
	}

	recs := a.snapshot()
	if len(recs) != 1 || recs[0].Event != audit.EventFail {
		t.Fatalf("want 1 fail row, got %+v", recs)
	}
	if recs[0].Detail["reason"] != "invite_only_not_implemented" {
		t.Fatalf("reason: want invite_only_not_implemented, got %v", recs[0].Detail["reason"])
	}
}

func TestApplyLinkOnly_NoExistingIdentity_Rejects(t *testing.T) {
	q := newFakeModesQueries()
	a := &recordingAudit{}
	idp := newIDP(federationoidc.ModeLinkOnly)
	tok := goodTokens()

	_, _, err := federationoidc.Resolve(context.Background(), q, a, idp, tok)
	if ae := authn.AsAuthError(err); ae == nil || ae.Code != "link_required" {
		t.Fatalf("want link_required, got %v", err)
	}

	recs := a.snapshot()
	if len(recs) != 1 || recs[0].Event != audit.EventFail {
		t.Fatalf("want 1 fail row, got %+v", recs)
	}
	if recs[0].Detail["reason"] != "link_required" {
		t.Fatalf("reason: want link_required, got %v", recs[0].Detail["reason"])
	}
}

func TestResolve_ExistingIdentitySyncsClaims(t *testing.T) {
	q := newFakeModesQueries()
	// Identity exists, with stale display_name + upstream_email.
	q.identityErr = nil
	q.identityResult = db.AccountIdentity{
		ID:            300,
		AccountID:     50,
		UpstreamIdpID: 42,
		UpstreamIss:   "https://issuer.example/",
		UpstreamSub:   "sub-1",
		UpstreamEmail: pgtype.Text{String: "alice-old@example.com", Valid: true},
	}
	q.accountByIDResults[50] = db.Account{ID: 50, Username: "alice", DisplayName: "Alice Old"}

	a := &recordingAudit{}
	idp := newIDP(federationoidc.ModeAutoProvision)
	tok := goodTokens() // Name: "Alice Example", Email: "alice@example.com"

	id, isNew, err := federationoidc.Resolve(context.Background(), q, a, idp, tok)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if isNew {
		t.Fatalf("want isNew=false, got true")
	}
	if id != 50 {
		t.Fatalf("want accountID=50, got %d", id)
	}

	if len(q.displayNameCalls) != 1 {
		t.Fatalf("want 1 UpdateAccountDisplayName call, got %d", len(q.displayNameCalls))
	}
	if q.displayNameCalls[0].ID != 50 || q.displayNameCalls[0].DisplayName != "Alice Example" {
		t.Fatalf("display-name args wrong: %+v", q.displayNameCalls[0])
	}
	if len(q.emailCalls) != 1 {
		t.Fatalf("want 1 UpdateAccountIdentityEmail call, got %d", len(q.emailCalls))
	}
	if q.emailCalls[0].ID != 300 ||
		!q.emailCalls[0].UpstreamEmail.Valid ||
		q.emailCalls[0].UpstreamEmail.String != "alice@example.com" {
		t.Fatalf("email-update args wrong: %+v", q.emailCalls[0])
	}

	recs := a.snapshot()
	if len(recs) != 1 || recs[0].Event != audit.EventUse {
		t.Fatalf("want 1 use row, got %+v", recs)
	}
	if recs[0].AccountID == nil || *recs[0].AccountID != 50 {
		t.Fatalf("audit account_id: want 50, got %v", recs[0].AccountID)
	}
}

func TestResolve_ExistingIdentityNoChangesSkipsSync(t *testing.T) {
	q := newFakeModesQueries()
	q.identityErr = nil
	q.identityResult = db.AccountIdentity{
		ID:            300,
		AccountID:     50,
		UpstreamIdpID: 42,
		UpstreamIss:   "https://issuer.example/",
		UpstreamSub:   "sub-1",
		UpstreamEmail: pgtype.Text{String: "alice@example.com", Valid: true},
	}
	q.accountByIDResults[50] = db.Account{ID: 50, Username: "alice", DisplayName: "Alice Example"}

	a := &recordingAudit{}
	idp := newIDP(federationoidc.ModeAutoProvision)
	tok := goodTokens()

	if _, _, err := federationoidc.Resolve(context.Background(), q, a, idp, tok); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if len(q.displayNameCalls) != 0 {
		t.Fatalf("want 0 display-name updates (no diff), got %d", len(q.displayNameCalls))
	}
	if len(q.emailCalls) != 0 {
		t.Fatalf("want 0 email updates (no diff), got %d", len(q.emailCalls))
	}
}

func TestResolve_NilArgs(t *testing.T) {
	if _, _, err := federationoidc.Resolve(context.Background(), newFakeModesQueries(), &recordingAudit{}, nil, goodTokens()); err == nil {
		t.Fatal("want error for nil idp")
	}
	if _, _, err := federationoidc.Resolve(context.Background(), newFakeModesQueries(), &recordingAudit{}, newIDP(federationoidc.ModeAutoProvision), nil); err == nil {
		t.Fatal("want error for nil tokens")
	}
}

// Compile-time guard: db.Queries (which satisfies db.Querier) must
// satisfy the narrow ModesQueries interface. If sqlc regenerates a
// signature out of sync with the interface, this fails the build.
var _ federationoidc.ModesQueries = (*db.Queries)(nil)

// Sanity check the error wrapping convention used by Resolve for the
// pgx.ErrNoRows fall-through: pgx.ErrNoRows is consumed (treated as the
// "first-sight" branch), other lookup errors propagate.
func TestResolve_PropagatesUnknownLookupError(t *testing.T) {
	q := newFakeModesQueries()
	q.identityErr = errors.New("db down")
	a := &recordingAudit{}
	if _, _, err := federationoidc.Resolve(context.Background(), q, a, newIDP(federationoidc.ModeAutoProvision), goodTokens()); err == nil {
		t.Fatal("want error to propagate")
	}
}
