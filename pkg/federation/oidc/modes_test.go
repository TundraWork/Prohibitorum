package oidc_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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

	displayNameCalls  []db.UpdateAccountDisplayNameParams
	emailCalls        []db.UpdateAccountIdentityEmailParams
	accountEmailCalls []db.UpdateAccountEmailParams

	// Enrollment state for invite_only tests. consumeEnrollmentResult is
	// returned on every ConsumeEnrollment call when consumeEnrollmentErr
	// is nil; consumedTokens records the tokens actually consumed so
	// happy-path tests can assert the right one was hit.
	consumeEnrollmentResult db.Enrollment
	consumeEnrollmentErr    error
	consumedTokens          []string
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

func (f *fakeModesQueries) UpdateAccountEmail(_ context.Context, arg db.UpdateAccountEmailParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.accountEmailCalls = append(f.accountEmailCalls, arg)
	return nil
}

func (f *fakeModesQueries) ConsumeEnrollment(_ context.Context, token string) (db.Enrollment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.consumedTokens = append(f.consumedTokens, token)
	if f.consumeEnrollmentErr != nil {
		return db.Enrollment{}, f.consumeEnrollmentErr
	}
	return f.consumeEnrollmentResult, nil
}

// ConsumeInviteEnrollment delegates to ConsumeEnrollment here — the SQL intent
// restriction (intent='invite') is exercised against real PG by the smoke's
// invite_only federation arc, not by this canned fake (audit OIDCFED-2).
func (f *fakeModesQueries) ConsumeInviteEnrollment(ctx context.Context, token string) (db.Enrollment, error) {
	return f.ConsumeEnrollment(ctx, token)
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

// hasFail reports whether the recording contains an EventFail row whose
// Detail["reason"] equals reason. Used by the 23505-race-mapping tests
// to assert structured audit emission.
func (r *recordingAudit) hasFail(reason string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, rec := range r.records {
		if rec.Event != audit.EventFail {
			continue
		}
		if s, ok := rec.Detail["reason"].(string); ok && s == reason {
			return true
		}
	}
	return false
}

// helpers --------------------------------------------------------------

func newIDP(mode string) *db.UpstreamIdp {
	return &db.UpstreamIdp{
		ID:                   42,
		Slug:                 "test-idp",
		IssuerUrl:            "https://issuer.example/",
		Mode:                 mode,
		RequireVerifiedEmail: true,
		// Schema defaults (migration 004): pass them explicitly because the
		// fake row construction here doesn't run the DB-side defaults.
		UsernameClaim:    "preferred_username",
		DisplayNameClaim: "name",
		EmailClaim:       "email",
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
		// Raw mirrors what client.Exchange would build for an OIDC-default
		// OP: typed standard claims hoisted under their JSON-tag keys.
		Raw: map[string]any{
			"sub":                "sub-1",
			"iss":                "https://issuer.example/",
			"preferred_username": "alice",
			"name":               "Alice Example",
			"email":              "alice@example.com",
		},
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

	id, isNew, err := federationoidc.Resolve(context.Background(), q, a, idp, tok, nil)
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

	_, _, err := federationoidc.Resolve(context.Background(), q, a, idp, tok, nil)
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

	_, _, err := federationoidc.Resolve(context.Background(), q, a, idp, tok, nil)
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

	if _, _, err := federationoidc.Resolve(context.Background(), q, a, idp, tok, nil); err != nil {
		t.Fatalf("expected provision to succeed (case-insensitive domain match), got %v", err)
	}
}

func TestApplyAutoProvision_UsernameCollisionRejected(t *testing.T) {
	q := newFakeModesQueries()
	q.accountByUsername["alice"] = db.Account{ID: 7, Username: "alice"}
	a := &recordingAudit{}
	idp := newIDP(federationoidc.ModeAutoProvision)
	tok := goodTokens()

	_, _, err := federationoidc.Resolve(context.Background(), q, a, idp, tok, nil)
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
	// modes.go reads via ClaimString(tokens.Raw, idp.UsernameClaim) now;
	// clear the Raw entry too so the "claim genuinely absent" branch fires.
	delete(tok.Raw, "preferred_username")

	_, _, err := federationoidc.Resolve(context.Background(), q, a, idp, tok, nil)
	if err == nil {
		t.Fatal("want error for missing preferred_username")
	}
	if ae := authn.AsAuthError(err); ae != nil {
		t.Fatalf("missing preferred_username should not be an AuthError (it's a config bug -> 500), got %v", ae)
	}
}

// pgUniqueViolation returns a *pgconn.PgError carrying SQLSTATE 23505 so
// tests can simulate a lost race against a concurrent insert. Used by the
// race-mapping tests below — exercises the isUniqueViolation branches in
// applyAutoProvision / applyInviteOnly.
func pgUniqueViolation(constraint string) error {
	return &pgconn.PgError{Code: "23505", ConstraintName: constraint}
}

// TestApplyAutoProvision_UsernameRaceMapsToCollisionError covers the
// READ COMMITTED race window: the collision check passes (no row with
// this username yet), but InsertAccount returns 23505 because another
// concurrent tx claimed the username between our check and our insert.
// Must surface as ErrUsernameCollision (clean 4xx + audit) instead of a
// wrapped 500.
func TestApplyAutoProvision_UsernameRaceMapsToCollisionError(t *testing.T) {
	q := newFakeModesQueries()
	q.insertAccountErr = pgUniqueViolation("account_username_key")
	a := &recordingAudit{}
	idp := newIDP(federationoidc.ModeAutoProvision)

	_, _, err := federationoidc.Resolve(context.Background(), q, a, idp, goodTokens(), nil)
	if ae := authn.AsAuthError(err); ae == nil || ae.Code != "username_collision" {
		t.Fatalf("want username_collision AuthError, got %v", err)
	}
	if !a.hasFail("username_collision") {
		t.Errorf("want audit fail reason=username_collision; got %+v", a.records)
	}
}

// TestApplyAutoProvision_IdentityConflictRaceMapsToInviteRequired covers
// the (upstream_iss, upstream_sub) UNIQUE race: InsertAccount succeeds,
// but InsertAccountIdentity loses to a concurrent callback that already
// bound the same upstream sub to a different account. Collapse onto
// ErrInviteRequired (matches LinkCallback link_conflict pattern) so we
// don't enumerate "which other account owns this identity".
func TestApplyAutoProvision_IdentityConflictRaceMapsToInviteRequired(t *testing.T) {
	q := newFakeModesQueries()
	q.insertIdentityErr = pgUniqueViolation("account_identity_upstream_iss_sub_key")
	a := &recordingAudit{}
	idp := newIDP(federationoidc.ModeAutoProvision)

	_, _, err := federationoidc.Resolve(context.Background(), q, a, idp, goodTokens(), nil)
	if ae := authn.AsAuthError(err); ae == nil || ae.Code != "invite_required" {
		t.Fatalf("want invite_required AuthError, got %v", err)
	}
	if !a.hasFail("identity_conflict") {
		t.Errorf("want audit fail reason=identity_conflict; got %+v", a.records)
	}
}

// TestApplyInviteOnly_UsernameRaceMapsToCollisionError mirrors the
// auto_provision race test but for the invite path. Same READ COMMITTED
// window; same expected mapping.
func TestApplyInviteOnly_UsernameRaceMapsToCollisionError(t *testing.T) {
	q := newFakeModesQueries()
	q.consumeEnrollmentResult = makeInviteEnrollment("test-idp", "alice", "Alice", "user", nil)
	q.insertAccountErr = pgUniqueViolation("account_username_key")
	a := &recordingAudit{}
	idp := newIDP(federationoidc.ModeInviteOnly)

	_, _, err := federationoidc.ApplyInviteOnlyForTest(context.Background(), q, a, idp, goodTokens(), "invite-token-xyz", nil)
	if ae := authn.AsAuthError(err); ae == nil || ae.Code != "username_collision" {
		t.Fatalf("want username_collision AuthError, got %v", err)
	}
	if !a.hasFail("username_collision") {
		t.Errorf("want audit fail reason=username_collision; got %+v", a.records)
	}
}

// TestApplyInviteOnly_IdentityConflictRaceMapsToInviteRequired mirrors
// the auto_provision identity-conflict race for the invite path.
func TestApplyInviteOnly_IdentityConflictRaceMapsToInviteRequired(t *testing.T) {
	q := newFakeModesQueries()
	q.consumeEnrollmentResult = makeInviteEnrollment("test-idp", "alice", "Alice", "user", nil)
	q.insertIdentityErr = pgUniqueViolation("account_identity_upstream_iss_sub_key")
	a := &recordingAudit{}
	idp := newIDP(federationoidc.ModeInviteOnly)

	_, _, err := federationoidc.ApplyInviteOnlyForTest(context.Background(), q, a, idp, goodTokens(), "invite-token-xyz", nil)
	if ae := authn.AsAuthError(err); ae == nil || ae.Code != "invite_required" {
		t.Fatalf("want invite_required AuthError, got %v", err)
	}
	if !a.hasFail("identity_conflict") {
		t.Errorf("want audit fail reason=identity_conflict; got %+v", a.records)
	}
}

// makeInviteEnrollment builds a valid db.Enrollment row for happy-path
// tests. slug should match the IdP being passed into applyInviteOnly.
func makeInviteEnrollment(slug, username, displayName, role string, attrs []byte) db.Enrollment {
	return db.Enrollment{
		Token:                   "invite-token-xyz",
		Intent:                  "invite",
		ExpectedUpstreamIdpSlug: pgtype.Text{String: slug, Valid: true},
		TemplateUsername:        pgtype.Text{String: username, Valid: true},
		TemplateDisplayName:     pgtype.Text{String: displayName, Valid: displayName != ""},
		TemplateRole:            pgtype.Text{String: role, Valid: true},
		TemplateAttributes:      attrs,
	}
}

func TestApplyInviteOnly_NoTokenRejects(t *testing.T) {
	// Driving via Resolve with mode=invite_only and no FedState invite token:
	// this is what happens when someone hits /federation/{slug}/login directly
	// on an invite_only IdP. The empty-token branch at the top of
	// applyInviteOnly emits invite_required_no_token and rejects.
	q := newFakeModesQueries()
	a := &recordingAudit{}
	idp := newIDP(federationoidc.ModeInviteOnly)
	tok := goodTokens()

	_, _, err := federationoidc.Resolve(context.Background(), q, a, idp, tok, nil)
	if ae := authn.AsAuthError(err); ae == nil || ae.Code != "invite_required" {
		t.Fatalf("want invite_required, got %v", err)
	}
	recs := a.snapshot()
	if len(recs) != 1 || recs[0].Event != audit.EventFail {
		t.Fatalf("want 1 fail row, got %+v", recs)
	}
	if recs[0].Detail["reason"] != "invite_required_no_token" {
		t.Fatalf("reason: want invite_required_no_token, got %v", recs[0].Detail["reason"])
	}
	if len(q.consumedTokens) != 0 {
		t.Fatalf("ConsumeEnrollment must not be called when token is empty; got %v", q.consumedTokens)
	}
}

func TestApplyInviteOnly_HappyPath(t *testing.T) {
	q := newFakeModesQueries()
	q.consumeEnrollmentResult = makeInviteEnrollment(
		"test-idp", "alice", "Alice Inv", "user", []byte(`{"key":"val"}`),
	)
	a := &recordingAudit{}
	idp := newIDP(federationoidc.ModeInviteOnly)
	tok := goodTokens()

	id, isNew, err := federationoidc.ApplyInviteOnlyForTest(
		context.Background(), q, a, idp, tok, "invite-token-xyz", nil,
	)
	if err != nil {
		t.Fatalf("applyInviteOnly: %v", err)
	}
	if !isNew {
		t.Fatalf("want isNew=true, got false")
	}
	if id != 100 {
		t.Fatalf("want accountID=100, got %d", id)
	}

	if len(q.consumedTokens) != 1 || q.consumedTokens[0] != "invite-token-xyz" {
		t.Fatalf("ConsumeEnrollment tokens = %v, want [invite-token-xyz]", q.consumedTokens)
	}
	if q.insertedAccount.Username != "alice" ||
		q.insertedAccount.DisplayName != "Alice Inv" ||
		q.insertedAccount.Role != "user" {
		t.Fatalf("InsertAccount args wrong: %+v", q.insertedAccount)
	}
	if string(q.insertedAccount.Attributes) != `{"key":"val"}` {
		t.Errorf("Attributes = %q, want template JSON", string(q.insertedAccount.Attributes))
	}
	if len(q.insertedAccount.WebauthnUserHandle) == 0 {
		t.Errorf("WebauthnUserHandle must be non-empty")
	}
	if q.insertedIdentity.AccountID != 100 ||
		q.insertedIdentity.UpstreamIss != tok.Issuer ||
		q.insertedIdentity.UpstreamSub != tok.Subject {
		t.Fatalf("InsertAccountIdentity args wrong: %+v", q.insertedIdentity)
	}

	recs := a.snapshot()
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

func TestApplyInviteOnly_ConsumedOrExpired(t *testing.T) {
	q := newFakeModesQueries()
	q.consumeEnrollmentErr = pgx.ErrNoRows
	a := &recordingAudit{}
	idp := newIDP(federationoidc.ModeInviteOnly)
	tok := goodTokens()

	_, _, err := federationoidc.ApplyInviteOnlyForTest(
		context.Background(), q, a, idp, tok, "stale-token", nil,
	)
	if ae := authn.AsAuthError(err); ae == nil || ae.Code != "invite_required" {
		t.Fatalf("want invite_required, got %v", err)
	}
	recs := a.snapshot()
	if r := recs[0].Detail["reason"]; r != "invite_consumed_or_expired" {
		t.Errorf("reason = %v, want invite_consumed_or_expired", r)
	}
	if len(q.insertedAccounts) != 0 {
		t.Errorf("no account should have been inserted, got %d", len(q.insertedAccounts))
	}
}

func TestApplyInviteOnly_SlugMismatch(t *testing.T) {
	q := newFakeModesQueries()
	// Enrollment was minted for "other-idp"; we're driving against idp.Slug="test-idp".
	q.consumeEnrollmentResult = makeInviteEnrollment(
		"other-idp", "alice", "Alice", "user", nil,
	)
	a := &recordingAudit{}
	idp := newIDP(federationoidc.ModeInviteOnly)
	tok := goodTokens()

	_, _, err := federationoidc.ApplyInviteOnlyForTest(
		context.Background(), q, a, idp, tok, "invite-token-xyz", nil,
	)
	if ae := authn.AsAuthError(err); ae == nil || ae.Code != "invite_required" {
		t.Fatalf("want invite_required, got %v", err)
	}
	recs := a.snapshot()
	if r := recs[0].Detail["reason"]; r != "invite_slug_mismatch" {
		t.Errorf("reason = %v, want invite_slug_mismatch", r)
	}
	if len(q.insertedAccounts) != 0 {
		t.Errorf("no account should have been inserted, got %d", len(q.insertedAccounts))
	}
}

func TestApplyInviteOnly_UsernameCollision(t *testing.T) {
	q := newFakeModesQueries()
	q.consumeEnrollmentResult = makeInviteEnrollment(
		"test-idp", "alice", "Alice", "user", nil,
	)
	// Existing local account already owns "alice".
	q.accountByUsername["alice"] = db.Account{ID: 7, Username: "alice"}
	a := &recordingAudit{}
	idp := newIDP(federationoidc.ModeInviteOnly)
	tok := goodTokens()

	_, _, err := federationoidc.ApplyInviteOnlyForTest(
		context.Background(), q, a, idp, tok, "invite-token-xyz", nil,
	)
	if ae := authn.AsAuthError(err); ae == nil || ae.Code != "username_collision" {
		t.Fatalf("want username_collision, got %v", err)
	}
	recs := a.snapshot()
	if r := recs[0].Detail["reason"]; r != "username_collision" {
		t.Errorf("reason = %v, want username_collision", r)
	}
	if len(q.insertedAccounts) != 0 {
		t.Errorf("no account should have been inserted (collision fired first); got %d", len(q.insertedAccounts))
	}
}

func TestApplyInviteOnly_DisplayNameFallsBackToUsername(t *testing.T) {
	q := newFakeModesQueries()
	q.consumeEnrollmentResult = makeInviteEnrollment(
		"test-idp", "bob", "" /* empty display_name */, "user", nil,
	)
	a := &recordingAudit{}
	idp := newIDP(federationoidc.ModeInviteOnly)
	tok := goodTokens()

	if _, _, err := federationoidc.ApplyInviteOnlyForTest(
		context.Background(), q, a, idp, tok, "invite-token-xyz", nil,
	); err != nil {
		t.Fatalf("applyInviteOnly: %v", err)
	}
	if q.insertedAccount.DisplayName != "bob" {
		t.Errorf("DisplayName = %q, want bob (fallback to username)", q.insertedAccount.DisplayName)
	}
}

func TestApplyLinkOnly_NoExistingIdentity_Rejects(t *testing.T) {
	q := newFakeModesQueries()
	a := &recordingAudit{}
	idp := newIDP(federationoidc.ModeLinkOnly)
	tok := goodTokens()

	_, _, err := federationoidc.Resolve(context.Background(), q, a, idp, tok, nil)
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

	id, isNew, err := federationoidc.Resolve(context.Background(), q, a, idp, tok, nil)
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

	if _, _, err := federationoidc.Resolve(context.Background(), q, a, idp, tok, nil); err != nil {
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
	if _, _, err := federationoidc.Resolve(context.Background(), newFakeModesQueries(), &recordingAudit{}, nil, goodTokens(), nil); err == nil {
		t.Fatal("want error for nil idp")
	}
	if _, _, err := federationoidc.Resolve(context.Background(), newFakeModesQueries(), &recordingAudit{}, newIDP(federationoidc.ModeAutoProvision), nil, nil); err == nil {
		t.Fatal("want error for nil tokens")
	}
}

// Compile-time guard: db.Queries (which satisfies db.Querier) must
// satisfy the narrow ModesQueries interface. If sqlc regenerates a
// signature out of sync with the interface, this fails the build.
var _ federationoidc.ModesQueries = (*db.Queries)(nil)

// TestApplyAutoProvision_HonorsUsernameClaimOverride exercises an
// Entra-style upstream: no preferred_username, the admin configured
// username_claim="upn" instead. The new code must read raw["upn"] and
// insert that as the local Username.
func TestApplyAutoProvision_HonorsUsernameClaimOverride(t *testing.T) {
	q := newFakeModesQueries()
	a := &recordingAudit{}
	idp := newIDP(federationoidc.ModeAutoProvision)
	idp.UsernameClaim = "upn"

	tok := goodTokens()
	// Simulate Entra: no preferred_username in raw, but upn is shipped.
	delete(tok.Raw, "preferred_username")
	tok.PreferredUsername = ""
	tok.Raw["upn"] = "alice-upn"

	id, isNew, err := federationoidc.Resolve(context.Background(), q, a, idp, tok, nil)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !isNew || id != 100 {
		t.Fatalf("want new id=100, got id=%d isNew=%v", id, isNew)
	}
	if q.insertedAccount.Username != "alice-upn" {
		t.Errorf("InsertAccount.Username = %q, want alice-upn (from raw[upn])", q.insertedAccount.Username)
	}
}

// TestApplyAutoProvision_HonorsEmailClaimOverride exercises an upstream
// that ships "mail" instead of "email" (Entra v1 token shape). The
// allowed_domains check AND the stored UpstreamEmail must both come
// from raw["mail"] — anything else means the gates fall out of sync
// with the persisted value.
func TestApplyAutoProvision_HonorsEmailClaimOverride(t *testing.T) {
	q := newFakeModesQueries()
	a := &recordingAudit{}
	idp := newIDP(federationoidc.ModeAutoProvision)
	idp.EmailClaim = "mail"
	idp.AllowedDomains = []string{"corp.example"}

	tok := goodTokens()
	delete(tok.Raw, "email")
	tok.Email = ""
	tok.Raw["mail"] = "alice@corp.example"

	id, isNew, err := federationoidc.Resolve(context.Background(), q, a, idp, tok, nil)
	if err != nil {
		t.Fatalf("Resolve: %v (domainAllowed must read raw[mail])", err)
	}
	if !isNew || id != 100 {
		t.Fatalf("want new id=100, got id=%d isNew=%v", id, isNew)
	}
	if !q.insertedIdentity.UpstreamEmail.Valid ||
		q.insertedIdentity.UpstreamEmail.String != "alice@corp.example" {
		t.Errorf("InsertAccountIdentity.UpstreamEmail = %+v, want alice@corp.example", q.insertedIdentity.UpstreamEmail)
	}
}

// TestApplyAutoProvision_HonorsDisplayNameClaimOverride exercises an
// upstream that ships a different display-name key. The display_name
// stored on the local account must come from the override.
func TestApplyAutoProvision_HonorsDisplayNameClaimOverride(t *testing.T) {
	q := newFakeModesQueries()
	a := &recordingAudit{}
	idp := newIDP(federationoidc.ModeAutoProvision)
	idp.DisplayNameClaim = "given_name"

	tok := goodTokens()
	delete(tok.Raw, "name")
	tok.Name = ""
	tok.Raw["given_name"] = "Alice From Override"

	if _, _, err := federationoidc.Resolve(context.Background(), q, a, idp, tok, nil); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if q.insertedAccount.DisplayName != "Alice From Override" {
		t.Errorf("DisplayName = %q, want %q", q.insertedAccount.DisplayName, "Alice From Override")
	}
}

// TestResolve_SyncClaims_HonorsOverrides exercises the re-login drift-sync
// path with non-default claim names. Without the override wiring, syncClaims
// would (a) try to set display_name to "" and (b) zero out upstream_email
// every re-login — both observable user-data regressions for Entra-style
// upstreams.
func TestResolve_SyncClaims_HonorsOverrides(t *testing.T) {
	q := newFakeModesQueries()
	// Existing identity bound to account 50 with stale display + email.
	q.identityErr = nil
	q.identityResult = db.AccountIdentity{
		ID:            300,
		AccountID:     50,
		UpstreamIdpID: 42,
		UpstreamIss:   "https://issuer.example/",
		UpstreamSub:   "sub-1",
		UpstreamEmail: pgtype.Text{String: "alice-old@corp.example", Valid: true},
	}
	q.accountByIDResults[50] = db.Account{ID: 50, Username: "alice", DisplayName: "Alice Old"}

	a := &recordingAudit{}
	idp := newIDP(federationoidc.ModeAutoProvision)
	idp.DisplayNameClaim = "given_name"
	idp.EmailClaim = "mail"

	tok := goodTokens()
	delete(tok.Raw, "name")
	delete(tok.Raw, "email")
	tok.Name = ""
	tok.Email = ""
	tok.Raw["given_name"] = "Alice Override"
	tok.Raw["mail"] = "alice-new@corp.example"

	if _, _, err := federationoidc.Resolve(context.Background(), q, a, idp, tok, nil); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if len(q.displayNameCalls) != 1 || q.displayNameCalls[0].DisplayName != "Alice Override" {
		t.Errorf("display-name update should have used override claim, got %+v", q.displayNameCalls)
	}
	if len(q.emailCalls) != 1 ||
		!q.emailCalls[0].UpstreamEmail.Valid ||
		q.emailCalls[0].UpstreamEmail.String != "alice-new@corp.example" {
		t.Errorf("email update should have used override claim, got %+v", q.emailCalls)
	}
}

// Sanity check the error wrapping convention used by Resolve for the
// pgx.ErrNoRows fall-through: pgx.ErrNoRows is consumed (treated as the
// "first-sight" branch), other lookup errors propagate.
func TestResolve_PropagatesUnknownLookupError(t *testing.T) {
	q := newFakeModesQueries()
	q.identityErr = errors.New("db down")
	a := &recordingAudit{}
	if _, _, err := federationoidc.Resolve(context.Background(), q, a, newIDP(federationoidc.ModeAutoProvision), goodTokens(), nil); err == nil {
		t.Fatal("want error to propagate")
	}
}
