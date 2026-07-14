package federation_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/db"
	federationoidc "prohibitorum/pkg/federation"
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

	// confirmedIdentityID records the id passed to the most recent
	// ConfirmAccountIdentity call (0 = never called). Invite redemption
	// auto-confirms in-tx; happy-path tests assert this matches the
	// just-inserted identity id.
	confirmedIdentityID int64
	confirmIdentityErr  error

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

func (f *fakeModesQueries) ConfirmAccountIdentity(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.confirmIdentityErr != nil {
		return f.confirmIdentityErr
	}
	f.confirmedIdentityID = id
	return nil
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

func goodTokens() *federationoidc.VerifiedIdentity {
	return &federationoidc.VerifiedIdentity{
		Issuer:        "https://issuer.example/",
		Subject:       "sub-1",
		Email:         new("alice@example.com"),
		EmailVerified: true,
		Username:      "alice",
		DisplayName:   "Alice Example",
		AMR:           []string{"pwd", "mfa"},
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

	out, err := federationoidc.Resolve(context.Background(), q, a, idp, tok, nil)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !out.IsNew {
		t.Fatalf("want isNew=true, got false")
	}
	if out.AccountID != 100 {
		t.Fatalf("want accountID=100, got %d", out.AccountID)
	}
	if out.Confirmed {
		t.Fatalf("auto-provision must yield Confirmed=false (routes to /welcome gate)")
	}
	if out.IdentityID == 0 {
		t.Fatalf("auto-provision must capture the inserted IdentityID, got 0")
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
		q.insertedIdentity.UpstreamEmail.String != *tok.Email {
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

	_, err := federationoidc.Resolve(context.Background(), q, a, idp, tok, nil)
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

	_, err := federationoidc.Resolve(context.Background(), q, a, idp, tok, nil)
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

	if _, err := federationoidc.Resolve(context.Background(), q, a, idp, tok, nil); err != nil {
		t.Fatalf("expected provision to succeed (case-insensitive domain match), got %v", err)
	}
}

func TestApplyAutoProvision_UsernameCollisionRejected(t *testing.T) {
	q := newFakeModesQueries()
	q.accountByUsername["alice"] = db.Account{ID: 7, Username: "alice"}
	a := &recordingAudit{}
	idp := newIDP(federationoidc.ModeAutoProvision)
	tok := goodTokens()

	_, err := federationoidc.Resolve(context.Background(), q, a, idp, tok, nil)
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
	tok.Username = ""

	_, err := federationoidc.Resolve(context.Background(), q, a, idp, tok, nil)
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

	_, err := federationoidc.Resolve(context.Background(), q, a, idp, goodTokens(), nil)
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

	_, err := federationoidc.Resolve(context.Background(), q, a, idp, goodTokens(), nil)
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

	_, err := federationoidc.ApplyInviteOnlyForTest(context.Background(), q, a, idp, goodTokens(), "invite-token-xyz", nil)
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

	_, err := federationoidc.ApplyInviteOnlyForTest(context.Background(), q, a, idp, goodTokens(), "invite-token-xyz", nil)
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

	_, err := federationoidc.Resolve(context.Background(), q, a, idp, tok, nil)
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

	out, err := federationoidc.ApplyInviteOnlyForTest(
		context.Background(), q, a, idp, tok, "invite-token-xyz", nil,
	)
	if err != nil {
		t.Fatalf("applyInviteOnly: %v", err)
	}
	if !out.IsNew {
		t.Fatalf("want isNew=true, got false")
	}
	if out.AccountID != 100 {
		t.Fatalf("want accountID=100, got %d", out.AccountID)
	}
	// The invite IS the authorization: invite redemption auto-confirms the
	// identity in-tx, so the HTTP layer issues a session immediately (no
	// /welcome gate).
	if !out.Confirmed {
		t.Fatalf("invite redemption must yield Confirmed=true")
	}
	if out.IdentityID == 0 {
		t.Fatalf("invite redemption must capture the inserted IdentityID, got 0")
	}
	if q.confirmedIdentityID != out.IdentityID {
		t.Fatalf("ConfirmAccountIdentity called with id=%d, want the inserted identity id=%d", q.confirmedIdentityID, out.IdentityID)
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

	_, err := federationoidc.ApplyInviteOnlyForTest(
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

	_, err := federationoidc.ApplyInviteOnlyForTest(
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

	_, err := federationoidc.ApplyInviteOnlyForTest(
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

	if _, err := federationoidc.ApplyInviteOnlyForTest(
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

	_, err := federationoidc.Resolve(context.Background(), q, a, idp, tok, nil)
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
		ConfirmedAt:   pgtype.Timestamptz{Time: time.Now(), Valid: true}, // already confirmed
	}
	q.accountByIDResults[50] = db.Account{ID: 50, Username: "alice", DisplayName: "Alice Old"}

	a := &recordingAudit{}
	idp := newIDP(federationoidc.ModeAutoProvision)
	tok := goodTokens() // Name: "Alice Example", Email: "alice@example.com"

	out, err := federationoidc.Resolve(context.Background(), q, a, idp, tok, nil)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if out.IsNew {
		t.Fatalf("want isNew=false, got true")
	}
	if out.AccountID != 50 {
		t.Fatalf("want accountID=50, got %d", out.AccountID)
	}
	if !out.Confirmed {
		t.Fatalf("confirmed re-login must yield Confirmed=true")
	}
	if out.IdentityID != 300 {
		t.Fatalf("want IdentityID=300, got %d", out.IdentityID)
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
		// Confirmed so this stays a pure "no drift -> no sync" test rather than
		// silently exercising the unconfirmed re-login branch.
		ConfirmedAt: pgtype.Timestamptz{Time: time.Unix(1, 0), Valid: true},
	}
	q.accountByIDResults[50] = db.Account{ID: 50, Username: "alice", DisplayName: "Alice Example"}

	a := &recordingAudit{}
	idp := newIDP(federationoidc.ModeAutoProvision)
	tok := goodTokens()

	if _, err := federationoidc.Resolve(context.Background(), q, a, idp, tok, nil); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if len(q.displayNameCalls) != 0 {
		t.Fatalf("want 0 display-name updates (no diff), got %d", len(q.displayNameCalls))
	}
	if len(q.emailCalls) != 0 {
		t.Fatalf("want 0 email updates (no diff), got %d", len(q.emailCalls))
	}
}

// TestResolve_ExistingUnconfirmed_NotConfirmed covers a re-login against an
// account_identity whose confirmed_at IS NULL (the user abandoned the /welcome
// gate previously, or a federated invite is still pending). Resolve must report
// Confirmed=false with the EXISTING account+identity ids (no re-insert) so the
// HTTP layer routes back to the confirmation gate, and MUST NOT emit a Use
// audit (the Use is recorded on confirm in Task 6).
func TestResolve_ExistingUnconfirmed_NotConfirmed(t *testing.T) {
	q := newFakeModesQueries()
	q.identityErr = nil
	q.identityResult = db.AccountIdentity{
		ID:            5,
		AccountID:     9,
		UpstreamIdpID: 42,
		UpstreamIss:   "https://issuer.example/",
		UpstreamSub:   "sub-1",
		UpstreamEmail: pgtype.Text{String: "alice@example.com", Valid: true},
		ConfirmedAt:   pgtype.Timestamptz{}, // pending — NULL confirmed_at
	}
	q.accountByIDResults[9] = db.Account{ID: 9, Username: "alice", DisplayName: "Alice Example"}

	a := &recordingAudit{}
	idp := newIDP(federationoidc.ModeAutoProvision)
	tok := goodTokens()

	out, err := federationoidc.Resolve(context.Background(), q, a, idp, tok, nil)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if out.Confirmed {
		t.Fatalf("pending identity must yield Confirmed=false")
	}
	if out.AccountID != 9 {
		t.Fatalf("want AccountID=9 (existing), got %d", out.AccountID)
	}
	if out.IdentityID != 5 {
		t.Fatalf("want IdentityID=5 (existing), got %d", out.IdentityID)
	}
	if out.IsNew {
		t.Fatalf("want IsNew=false (no re-insert), got true")
	}
	// No re-insert of either an account or an identity.
	if len(q.insertedAccounts) != 0 {
		t.Fatalf("pending re-login must not insert an account; got %d", len(q.insertedAccounts))
	}
	// MUST NOT audit Use for an unconfirmed identity (recorded on confirm).
	if findEvent(a.snapshot(), audit.EventUse) != nil {
		t.Fatalf("unconfirmed re-login must NOT emit a Use audit (recorded on confirm)")
	}
}

// TestResolve_ExistingConfirmed_Confirmed covers a normal returning user: the
// account_identity has a valid confirmed_at. Resolve issues Confirmed=true and
// emits the Use audit as it always has.
func TestResolve_ExistingConfirmed_Confirmed(t *testing.T) {
	q := newFakeModesQueries()
	q.identityErr = nil
	q.identityResult = db.AccountIdentity{
		ID:            5,
		AccountID:     9,
		UpstreamIdpID: 42,
		UpstreamIss:   "https://issuer.example/",
		UpstreamSub:   "sub-1",
		UpstreamEmail: pgtype.Text{String: "alice@example.com", Valid: true},
		ConfirmedAt:   pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}
	q.accountByIDResults[9] = db.Account{ID: 9, Username: "alice", DisplayName: "Alice Example"}

	a := &recordingAudit{}
	idp := newIDP(federationoidc.ModeAutoProvision)
	tok := goodTokens()

	out, err := federationoidc.Resolve(context.Background(), q, a, idp, tok, nil)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !out.Confirmed {
		t.Fatalf("confirmed identity must yield Confirmed=true")
	}
	if out.AccountID != 9 || out.IdentityID != 5 || out.IsNew {
		t.Fatalf("want {AccountID:9 IdentityID:5 IsNew:false}, got %+v", out)
	}
	if findEvent(a.snapshot(), audit.EventUse) == nil {
		t.Fatalf("confirmed re-login must emit a Use audit")
	}
}

// TestApplyAutoProvision_NotConfirmed asserts the first-sight auto_provision
// path yields a PENDING identity: Confirmed=false, IsNew=true, with a captured
// IdentityID. The user must confirm on /welcome before a session is issued.
func TestApplyAutoProvision_NotConfirmed(t *testing.T) {
	q := newFakeModesQueries()
	a := &recordingAudit{}
	idp := newIDP(federationoidc.ModeAutoProvision)
	tok := goodTokens()

	out, err := federationoidc.Resolve(context.Background(), q, a, idp, tok, nil)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if out.Confirmed {
		t.Fatalf("auto-provision must yield Confirmed=false")
	}
	if !out.IsNew {
		t.Fatalf("auto-provision must yield IsNew=true")
	}
	if out.IdentityID == 0 {
		t.Fatalf("auto-provision must capture a non-zero IdentityID")
	}
	// Auto-provision must NOT confirm the identity (no ConfirmAccountIdentity).
	if q.confirmedIdentityID != 0 {
		t.Fatalf("auto-provision must NOT call ConfirmAccountIdentity; got id=%d", q.confirmedIdentityID)
	}
}

// TestApplyInviteOnly_Confirmed asserts invite redemption auto-confirms the
// just-inserted identity in-tx (the invite IS the authorization) → Confirmed=true,
// and ConfirmAccountIdentity was called with the inserted identity id.
func TestApplyInviteOnly_Confirmed(t *testing.T) {
	q := newFakeModesQueries()
	q.consumeEnrollmentResult = makeInviteEnrollment("test-idp", "alice", "Alice Inv", "user", nil)
	a := &recordingAudit{}
	idp := newIDP(federationoidc.ModeInviteOnly)
	tok := goodTokens()

	out, err := federationoidc.ApplyInviteOnlyForTest(
		context.Background(), q, a, idp, tok, "invite-token-xyz", nil,
	)
	if err != nil {
		t.Fatalf("applyInviteOnly: %v", err)
	}
	if !out.Confirmed {
		t.Fatalf("invite redemption must yield Confirmed=true")
	}
	if !out.IsNew {
		t.Fatalf("invite redemption must yield IsNew=true")
	}
	if out.IdentityID == 0 {
		t.Fatalf("invite redemption must capture a non-zero IdentityID")
	}
	if q.confirmedIdentityID != out.IdentityID {
		t.Fatalf("ConfirmAccountIdentity id=%d, want inserted identity id=%d", q.confirmedIdentityID, out.IdentityID)
	}
}

// TestApplyInviteOnly_ConfirmFails asserts that when the in-tx
// ConfirmAccountIdentity fails (e.g. the DB went away mid-redemption), the whole
// invite redemption aborts with a non-nil (wrapped) error rather than silently
// issuing an unconfirmed identity. The invite IS the authorization, so a failed
// confirm must roll the redemption back (the real handler runs applyInviteOnly
// inside a tx; this exercises the error-propagation seam).
func TestApplyInviteOnly_ConfirmFails(t *testing.T) {
	q := newFakeModesQueries()
	q.consumeEnrollmentResult = makeInviteEnrollment("test-idp", "alice", "Alice Inv", "user", nil)
	q.confirmIdentityErr = errors.New("db down")
	a := &recordingAudit{}
	idp := newIDP(federationoidc.ModeInviteOnly)
	tok := goodTokens()

	_, err := federationoidc.ApplyInviteOnlyForTest(
		context.Background(), q, a, idp, tok, "invite-token-xyz", nil,
	)
	if err == nil {
		t.Fatal("want a non-nil error when ConfirmAccountIdentity fails")
	}
}

func TestResolve_NilArgs(t *testing.T) {
	if _, err := federationoidc.Resolve(context.Background(), newFakeModesQueries(), &recordingAudit{}, nil, goodTokens(), nil); err == nil {
		t.Fatal("want error for nil idp")
	}
	if _, err := federationoidc.Resolve(context.Background(), newFakeModesQueries(), &recordingAudit{}, newIDP(federationoidc.ModeAutoProvision), nil, nil); err == nil {
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
	tok.Username = "alice-upn"

	out, err := federationoidc.Resolve(context.Background(), q, a, idp, tok, nil)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !out.IsNew || out.AccountID != 100 {
		t.Fatalf("want new id=100, got id=%d isNew=%v", out.AccountID, out.IsNew)
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
	tok.Email = new("alice@corp.example")

	out, err := federationoidc.Resolve(context.Background(), q, a, idp, tok, nil)
	if err != nil {
		t.Fatalf("Resolve: %v (domainAllowed must read raw[mail])", err)
	}
	if !out.IsNew || out.AccountID != 100 {
		t.Fatalf("want new id=100, got id=%d isNew=%v", out.AccountID, out.IsNew)
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
	tok.DisplayName = "Alice From Override"

	if _, err := federationoidc.Resolve(context.Background(), q, a, idp, tok, nil); err != nil {
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
	tok.DisplayName = "Alice Override"
	tok.Email = new("alice-new@corp.example")

	if _, err := federationoidc.Resolve(context.Background(), q, a, idp, tok, nil); err != nil {
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
	if _, err := federationoidc.Resolve(context.Background(), q, a, newIDP(federationoidc.ModeAutoProvision), goodTokens(), nil); err == nil {
		t.Fatal("want error to propagate")
	}
}
