// Package server — handle_enrollment_preview_test.go
//
// Handler-level tests for the invite branch of GET /enrollments/{token}
// (handlePreviewEnrollment): the preview must surface the provider binding and
// the redeemable provider list so the enroll page can render the choice
// without a probe round-trip.
package server

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/credential/enrollment"
	"prohibitorum/pkg/db"
	fedoidc "prohibitorum/pkg/federation"
)

type previewQueries struct {
	db.Querier
	mu          sync.Mutex
	enrollments map[string]db.Enrollment
	idps        []db.UpstreamIdp
	listErr     error
}

func (q *previewQueries) GetEnrollmentByToken(_ context.Context, token string) (db.Enrollment, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	e, ok := q.enrollments[token]
	if !ok {
		return db.Enrollment{}, pgx.ErrNoRows
	}
	return e, nil
}

func (q *previewQueries) ListUpstreamIDPs(_ context.Context) ([]db.UpstreamIdp, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.listErr != nil {
		return nil, q.listErr
	}
	return q.idps, nil
}

func (q *previewQueries) ListEntityIconEtags(_ context.Context, _ string) ([]db.ListEntityIconEtagsRow, error) {
	return nil, nil
}

func oidcIDP(id int64, slug, mode string) db.UpstreamIdp {
	return db.UpstreamIdp{
		ID:          id,
		Slug:        slug,
		DisplayName: slug,
		Mode:        mode,
		Protocol:    "oidc",
	}
}

func invitePreview(token string, boundSlug pgtype.Text) db.Enrollment {
	return db.Enrollment{
		Token:                   token,
		Intent:                  enrollment.IntentInvite,
		ExpectedUpstreamIdpSlug: boundSlug,
		ExpiresAt:               pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
	}
}

func TestPreviewInviteUnboundListsAllRedeemableProviders(t *testing.T) {
	q := &previewQueries{
		enrollments: map[string]db.Enrollment{
			"tok": invitePreview("tok", pgtype.Text{}),
		},
		idps: []db.UpstreamIdp{
			oidcIDP(1, "auto", fedoidc.ModeAutoProvision),
			oidcIDP(2, "invite", fedoidc.ModeInviteOnly),
			oidcIDP(3, "link", fedoidc.ModeLinkOnly),
		},
	}
	s := &Server{enrollmentQueriesOverride: q}

	out, err := s.handlePreviewEnrollment(context.Background(), &previewIn{Token: "tok"})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if out.Body.ExpectedUpstreamIdpSlug != "" {
		t.Errorf("expectedUpstreamIdpSlug = %q, want omitted for an unbound invite", out.Body.ExpectedUpstreamIdpSlug)
	}
	if len(out.Body.Providers) != 2 {
		t.Fatalf("providers = %+v, want exactly auto_provision + invite_only", out.Body.Providers)
	}
	for _, p := range out.Body.Providers {
		if p.Slug == "link" {
			t.Errorf("link_only provider must not be offered: %+v", out.Body.Providers)
		}
	}
}

func TestPreviewInviteBoundReturnsOnlyBinding(t *testing.T) {
	q := &previewQueries{
		enrollments: map[string]db.Enrollment{
			"tok": invitePreview("tok", pgtype.Text{String: "bound", Valid: true}),
		},
		idps: []db.UpstreamIdp{
			oidcIDP(1, "auto", fedoidc.ModeAutoProvision),
			oidcIDP(2, "bound", fedoidc.ModeInviteOnly),
		},
	}
	s := &Server{enrollmentQueriesOverride: q}

	out, err := s.handlePreviewEnrollment(context.Background(), &previewIn{Token: "tok"})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if out.Body.ExpectedUpstreamIdpSlug != "bound" {
		t.Errorf("expectedUpstreamIdpSlug = %q, want bound", out.Body.ExpectedUpstreamIdpSlug)
	}
	if len(out.Body.Providers) != 1 || out.Body.Providers[0].Slug != "bound" {
		t.Fatalf("providers = %+v, want only the bound provider", out.Body.Providers)
	}
}

func TestPreviewInviteBoundToLinkOnlyYieldsEmptyProviders(t *testing.T) {
	q := &previewQueries{
		enrollments: map[string]db.Enrollment{
			"tok": invitePreview("tok", pgtype.Text{String: "link", Valid: true}),
		},
		idps: []db.UpstreamIdp{
			oidcIDP(1, "auto", fedoidc.ModeAutoProvision),
			oidcIDP(3, "link", fedoidc.ModeLinkOnly),
		},
	}
	s := &Server{enrollmentQueriesOverride: q}

	out, err := s.handlePreviewEnrollment(context.Background(), &previewIn{Token: "tok"})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if len(out.Body.Providers) != 0 {
		t.Fatalf("providers = %+v, want empty when the binding is link_only", out.Body.Providers)
	}
}
