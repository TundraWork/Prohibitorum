// Package server — handle_invitations_test.go
//
// Handler-level tests for POST /invitations (handleCreateInvitation) and
// GET /invitations (handleListInvitations), focusing on the federation-slug
// binding added in Task 7.
//
// These tests are DB-free: a minimal fake querier implements only the two
// methods exercised by the handlers under test (InsertEnrollment, called
// transitively via enrollment.IssueEnrollment, and ListPendingInvitations).
// All other querier methods are left to the embedded nil interface — calling
// them panics, which catches accidental over-reach.

package server

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/credential/enrollment"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/weberr"
)

// --- minimal fake querier ---------------------------------------------------

// fakeInvitationQ implements db.Querier by embedding the nil interface and
// overriding only the two methods the invitation handlers exercise. Calling any
// other method panics — intentional, to catch accidental over-reach in tests.
type fakeInvitationQ struct {
	db.Querier // embedded nil — unimplemented methods panic if called.

	// inserted stores every InsertEnrollmentParams received, in order.
	inserted []db.InsertEnrollmentParams

	// seedRows is returned by ListPendingInvitations.
	seedRows       []db.Enrollment
	groups         []db.ListInvitationGroupsRow
	listedGroupIDs []int32

	// idpMissing makes GetUpstreamIDPBySlug return pgx.ErrNoRows (the slug is
	// unknown or disabled), exercising the invite slug-validation reject path.
	idpMissing bool
}

// GetUpstreamIDPBySlug validates a federated-invite slug binding. Returns a
// minimal row by default; pgx.ErrNoRows when idpMissing is set.
func (f *fakeInvitationQ) GetUpstreamIDPBySlug(_ context.Context, slug string) (db.UpstreamIdp, error) {
	if f.idpMissing {
		return db.UpstreamIdp{}, pgx.ErrNoRows
	}
	return db.UpstreamIdp{Slug: slug}, nil
}

func (f *fakeInvitationQ) InsertEnrollment(_ context.Context, p db.InsertEnrollmentParams) (db.Enrollment, error) {
	f.inserted = append(f.inserted, p)
	return db.Enrollment{
		Token:                   p.Token,
		Intent:                  p.Intent,
		TemplateRole:            p.TemplateRole,
		TemplateAttributes:      p.TemplateAttributes,
		ExpectedUpstreamIdpSlug: p.ExpectedUpstreamIdpSlug,
		TemplateUsername:        p.TemplateUsername,
		GroupIds:                append([]int32(nil), p.GroupIds...),
		CreatedByAccountID:      p.CreatedByAccountID,
		CreatedAt:               pgtype.Timestamptz{Time: time.Now(), Valid: true},
		ExpiresAt:               p.ExpiresAt,
	}, nil
}

func (f *fakeInvitationQ) ListInvitationGroups(_ context.Context, groupIDs []int32) ([]db.ListInvitationGroupsRow, error) {
	f.listedGroupIDs = append([]int32(nil), groupIDs...)
	return append([]db.ListInvitationGroupsRow(nil), f.groups...), nil
}

func (f *fakeInvitationQ) ListPendingInvitations(_ context.Context, _ db.ListPendingInvitationsParams) ([]db.Enrollment, error) {
	return f.seedRows, nil
}

// InsertCredentialEvent is a no-op sink for the audit rows the invitation
// handlers now emit (audit.Writer is wired in minimalServerForInvitations).
func (f *fakeInvitationQ) InsertCredentialEvent(_ context.Context, _ db.InsertCredentialEventParams) error {
	return nil
}

// --- helpers ----------------------------------------------------------------

// minimalServerForInvitations builds the smallest Server that can run the
// invitation handlers via the invitationOverride seam.
func minimalServerForInvitations(q *fakeInvitationQ) *Server {
	return &Server{
		config:             &configx.Config{PublicOrigins: []string{"https://id.example.com"}},
		invitationOverride: q,
		Audit:              audit.NewWriter(q),
	}
}

// --- tests ------------------------------------------------------------------

// TestCreateInvitation_SlugBound checks that when a caller supplies
// expectedUpstreamIdpSlug the slug is stored in the enrollment row.
func TestCreateInvitation_SlugBound(t *testing.T) {
	t.Parallel()

	slug := "okta"
	q := &fakeInvitationQ{}
	s := minimalServerForInvitations(q)

	in := &createInvitationIn{}
	in.Body.Role = "user"
	in.Body.ExpectedUpstreamIdpSlug = &slug

	out, err := s.handleCreateInvitation(context.Background(), in)
	if err != nil {
		t.Fatalf("handleCreateInvitation: %v", err)
	}
	if out == nil || out.Body.URL == "" {
		t.Fatal("expected a non-empty URL in the response")
	}

	// Exactly one enrollment should have been inserted.
	if len(q.inserted) != 1 {
		t.Fatalf("InsertEnrollment call count: want 1, got %d", len(q.inserted))
	}
	params := q.inserted[0]

	// The slug must be present and valid in the stored params.
	if !params.ExpectedUpstreamIdpSlug.Valid {
		t.Error("ExpectedUpstreamIdpSlug.Valid: want true, got false")
	}
	if params.ExpectedUpstreamIdpSlug.String != slug {
		t.Errorf("ExpectedUpstreamIdpSlug.String: want %q, got %q", slug, params.ExpectedUpstreamIdpSlug.String)
	}
	if params.Intent != enrollment.IntentInvite {
		t.Errorf("Intent: want %q, got %q", enrollment.IntentInvite, params.Intent)
	}
}

func TestCreateInvitationRejectsRemovedAppManagerRole(t *testing.T) {
	q := &fakeInvitationQ{}
	s := minimalServerForInvitations(q)
	in := &createInvitationIn{}
	in.Body.Role = "app_manager"

	_, err := s.handleCreateInvitation(context.Background(), in)
	if publicErr := weberr.AsPublic(err); publicErr == nil || publicErr.Code != "invalid_role" {
		t.Fatalf("handleCreateInvitation error = %v, want invalid_role", err)
	}
	if len(q.inserted) != 0 {
		t.Fatalf("InsertEnrollment call count = %d, want 0", len(q.inserted))
	}
}

func TestCreateInvitation_NormalizesUsernameAndGroups(t *testing.T) {
	q := &fakeInvitationQ{}
	s := minimalServerForInvitations(q)
	in := &createInvitationIn{}
	in.Body.Role = "user"
	in.Body.Username = "  alice  "
	in.Body.GroupIDs = []int32{12, 7, 12}
	ctx := authn.WithSession(context.Background(), &authn.Session{Account: &db.Account{ID: 42, Role: "admin"}})

	out, err := s.handleCreateInvitation(ctx, in)
	if err != nil {
		t.Fatalf("handleCreateInvitation: %v", err)
	}
	if len(q.inserted) != 1 {
		t.Fatalf("inserts = %d, want 1", len(q.inserted))
	}
	p := q.inserted[0]
	if !p.TemplateUsername.Valid || p.TemplateUsername.String != "alice" {
		t.Fatalf("template username = %+v, want alice", p.TemplateUsername)
	}
	if len(p.GroupIds) != 2 || p.GroupIds[0] != 12 || p.GroupIds[1] != 7 {
		t.Fatalf("group IDs = %v, want [12 7]", p.GroupIds)
	}
	if !p.CreatedByAccountID.Valid || p.CreatedByAccountID.Int32 != 42 {
		t.Fatalf("creator = %+v, want 42", p.CreatedByAccountID)
	}
	if out.Body.Username == nil || *out.Body.Username != "alice" {
		t.Fatalf("response username = %#v, want alice", out.Body.Username)
	}
	if len(out.Body.GroupIDs) != 2 || out.Body.GroupIDs[0] != 12 || out.Body.GroupIDs[1] != 7 {
		t.Fatalf("response group IDs = %v, want [12 7]", out.Body.GroupIDs)
	}
}

func TestCreateInvitation_RejectsInvalidUsernameWithoutQueryingGroups(t *testing.T) {
	q := &fakeInvitationQ{}
	s := minimalServerForInvitations(q)
	in := &createInvitationIn{}
	in.Body.Role = "user"
	in.Body.Username = "Invalid Name"
	in.Body.GroupIDs = []int32{999}

	if _, err := s.handleCreateInvitation(context.Background(), in); err == nil {
		t.Fatal("invalid username was accepted")
	}
	if len(q.inserted) != 0 || len(q.listedGroupIDs) != 0 {
		t.Fatalf("invalid request queried or inserted: inserted=%d groups=%v", len(q.inserted), q.listedGroupIDs)
	}
}

// TestCreateInvitation_NoSlug checks that omitting expectedUpstreamIdpSlug
// stores a NULL slug in the enrollment row (unbound invite).
func TestCreateInvitation_NoSlug(t *testing.T) {
	t.Parallel()

	q := &fakeInvitationQ{}
	s := minimalServerForInvitations(q)

	in := &createInvitationIn{}
	in.Body.Role = "user"
	// ExpectedUpstreamIdpSlug is nil (omitted)

	if _, err := s.handleCreateInvitation(context.Background(), in); err != nil {
		t.Fatalf("handleCreateInvitation: %v", err)
	}

	if len(q.inserted) != 1 {
		t.Fatalf("InsertEnrollment call count: want 1, got %d", len(q.inserted))
	}
	if q.inserted[0].ExpectedUpstreamIdpSlug.Valid {
		t.Error("ExpectedUpstreamIdpSlug.Valid: want false (NULL) for unbound invite, got true")
	}
}

// TestCreateInvitation_EmptySlugStoresNoBinding checks the other spelling of
// "no provider required": an empty expectedUpstreamIdpSlug must store NULL,
// not an empty binding. The handler skips its slug-exists validation for it,
// so storing it as a binding would persist an unvalidated provider requirement
// — and the redemption callback would then refuse the invite.
func TestCreateInvitation_EmptySlugStoresNoBinding(t *testing.T) {
	t.Parallel()

	empty := ""
	q := &fakeInvitationQ{}
	s := minimalServerForInvitations(q)

	in := &createInvitationIn{}
	in.Body.Role = "user"
	in.Body.ExpectedUpstreamIdpSlug = &empty

	if _, err := s.handleCreateInvitation(context.Background(), in); err != nil {
		t.Fatalf("handleCreateInvitation: %v", err)
	}
	if len(q.inserted) != 1 {
		t.Fatalf("InsertEnrollment call count: want 1, got %d", len(q.inserted))
	}
	if got := q.inserted[0].ExpectedUpstreamIdpSlug; got.Valid {
		t.Errorf("ExpectedUpstreamIdpSlug: want NULL for an empty slug, got %+v", got)
	}
}

// TestCreateInvitation_UnknownSlugRejected guards T3.4: a federated invite
// bound to a non-existent or disabled IdP slug is rejected at create time
// (rather than minting a permanently un-redeemable invite).
func TestCreateInvitation_UnknownSlugRejected(t *testing.T) {
	t.Parallel()

	slug := "ghost"
	q := &fakeInvitationQ{idpMissing: true}
	s := minimalServerForInvitations(q)

	in := &createInvitationIn{}
	in.Body.Role = "user"
	in.Body.ExpectedUpstreamIdpSlug = &slug

	_, err := s.handleCreateInvitation(context.Background(), in)
	if err == nil {
		t.Fatal("expected an error for an unknown/disabled IdP slug")
	}
	// The handler wraps the AuthError via authErrToHuma → a 404 huma StatusError.
	se, ok := err.(huma.StatusError)
	if !ok || se.GetStatus() != http.StatusNotFound {
		t.Fatalf("want 404 huma StatusError (upstream_idp_not_found), got %T %v", err, err)
	}
	// No enrollment should have been issued.
	if len(q.inserted) != 0 {
		t.Errorf("InsertEnrollment must not be called when slug is invalid; got %d", len(q.inserted))
	}
}

// TestListInvitations_SlugRoundTrip seeds a fake enrollment with a slug and
// asserts that the returned InvitationView carries it in ExpectedUpstreamIdpSlug.
func TestListInvitations_SlugRoundTrip(t *testing.T) {
	t.Parallel()

	const token = "test-token-abc"
	const slug = "google"
	const origin = "https://id.example.com"

	q := &fakeInvitationQ{
		seedRows: []db.Enrollment{
			{
				Token:                   token,
				Intent:                  "invite",
				CreatedAt:               pgtype.Timestamptz{Time: time.Now(), Valid: true},
				ExpiresAt:               pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
				TemplateRole:            pgtype.Text{String: "user", Valid: true},
				ExpectedUpstreamIdpSlug: pgtype.Text{String: slug, Valid: true},
			},
		},
	}
	s := minimalServerForInvitations(q)

	out, err := s.handleListInvitations(context.Background(), &listInvitationsIn{pageInput: pageInput{Limit: 10}})
	if err != nil {
		t.Fatalf("handleListInvitations: %v", err)
	}

	if len(out.Body.Items) != 1 {
		t.Fatalf("list length: want 1, got %d", len(out.Body.Items))
	}
	view := out.Body.Items[0]

	// URL must be constructed from origin + token.
	wantURL := origin + "/enroll/" + token
	if view.URL != wantURL {
		t.Errorf("URL: want %q, got %q", wantURL, view.URL)
	}

	// Slug must be populated.
	if view.ExpectedUpstreamIdpSlug == nil {
		t.Fatal("ExpectedUpstreamIdpSlug: want non-nil, got nil")
	}
	if *view.ExpectedUpstreamIdpSlug != slug {
		t.Errorf("ExpectedUpstreamIdpSlug: want %q, got %q", slug, *view.ExpectedUpstreamIdpSlug)
	}
}

// TestListInvitations_NoSlugOmitted verifies that an unbound invite (NULL slug)
// renders with a nil ExpectedUpstreamIdpSlug (omitempty in JSON).
func TestListInvitations_NoSlugOmitted(t *testing.T) {
	t.Parallel()

	const token = "test-token-xyz"

	q := &fakeInvitationQ{
		seedRows: []db.Enrollment{
			{
				Token:                   token,
				Intent:                  "invite",
				CreatedAt:               pgtype.Timestamptz{Time: time.Now(), Valid: true},
				ExpiresAt:               pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
				TemplateRole:            pgtype.Text{String: "admin", Valid: true},
				ExpectedUpstreamIdpSlug: pgtype.Text{Valid: false}, // NULL
			},
		},
	}
	s := minimalServerForInvitations(q)

	out, err := s.handleListInvitations(context.Background(), &listInvitationsIn{pageInput: pageInput{Limit: 10}})
	if err != nil {
		t.Fatalf("handleListInvitations: %v", err)
	}
	if len(out.Body.Items) != 1 {
		t.Fatalf("list length: want 1, got %d", len(out.Body.Items))
	}

	view := out.Body.Items[0]
	if view.ExpectedUpstreamIdpSlug != nil {
		t.Errorf("ExpectedUpstreamIdpSlug: want nil for unbound invite, got %q", *view.ExpectedUpstreamIdpSlug)
	}

	// Sanity: role should be populated from the seeded row.
	wantRole := "admin"
	if view.Role != wantRole {
		t.Errorf("Role: want %q, got %q", wantRole, view.Role)
	}
}

func TestListInvitations_ReturnsSavedIDsAndResolvableGroups(t *testing.T) {
	q := &fakeInvitationQ{
		seedRows: []db.Enrollment{{
			Token: "tok", Intent: enrollment.IntentInvite,
			TemplateUsername: pgtype.Text{String: "alice", Valid: true},
			GroupIds:         []int32{12, 404, 7},
			CreatedAt:        pgtype.Timestamptz{Time: time.Now(), Valid: true},
			ExpiresAt:        pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
		}},
		groups: []db.ListInvitationGroupsRow{
			{ID: 7, Slug: "ops", DisplayName: "Operations"},
			{ID: 12, Slug: "eng", DisplayName: "Engineering"},
		},
	}
	s := minimalServerForInvitations(q)
	out, err := s.handleListInvitations(context.Background(), &listInvitationsIn{pageInput: pageInput{Limit: 10}})
	if err != nil {
		t.Fatalf("handleListInvitations: %v", err)
	}
	view := out.Body.Items[0]
	if view.Username == nil || *view.Username != "alice" {
		t.Fatalf("username = %#v, want alice", view.Username)
	}
	if len(view.GroupIDs) != 3 || view.GroupIDs[1] != 404 {
		t.Fatalf("saved group IDs = %v, want [12 404 7]", view.GroupIDs)
	}
	if len(view.Groups) != 2 || view.Groups[0].ID != 12 || view.Groups[1].ID != 7 {
		t.Fatalf("resolved groups = %+v, want IDs [12 7] in saved order", view.Groups)
	}
}

// TestCreateInvitation_ViewType confirms the response body is contract.InvitationResponse.
func TestCreateInvitation_ViewType(t *testing.T) {
	t.Parallel()

	q := &fakeInvitationQ{}
	s := minimalServerForInvitations(q)

	in := &createInvitationIn{}
	in.Body.Role = "admin"

	out, err := s.handleCreateInvitation(context.Background(), in)
	if err != nil {
		t.Fatalf("handleCreateInvitation: %v", err)
	}

	// Compile-time guarantee: out.Body must be contract.InvitationResponse.
	var _ contract.InvitationResponse = out.Body
	if out.Body.URL == "" {
		t.Error("URL should be non-empty")
	}
	if out.Body.ExpiresAt.IsZero() {
		t.Error("ExpiresAt should be non-zero")
	}
}
