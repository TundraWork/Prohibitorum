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
	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/credential/enrollment"
	"prohibitorum/pkg/db"
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
	seedRows []db.Enrollment

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
		CreatedAt:               pgtype.Timestamptz{Time: time.Now(), Valid: true},
		ExpiresAt:               p.ExpiresAt,
	}, nil
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
				Token:     token,
				Intent:    "invite",
				CreatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
				ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
				TemplateRole: pgtype.Text{String: "user", Valid: true},
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
