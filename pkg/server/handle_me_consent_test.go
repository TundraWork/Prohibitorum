package server

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"prohibitorum/pkg/db"
)

type fakeConsentQ struct {
	rows    []db.ListConsentsByAccountRow
	deleted []db.DeleteConsentParams
}

func (f *fakeConsentQ) ListConsentsByAccount(context.Context, int32) ([]db.ListConsentsByAccountRow, error) {
	return f.rows, nil
}
func (f *fakeConsentQ) DeleteConsent(_ context.Context, p db.DeleteConsentParams) error {
	f.deleted = append(f.deleted, p)
	return nil
}
func (f *fakeConsentQ) GetEntityIconEtag(context.Context, db.GetEntityIconEtagParams) (string, error) {
	return "", pgx.ErrNoRows
}

func TestHandleMyConsentList(t *testing.T) {
	s := &Server{consentMgmtOverride: &fakeConsentQ{rows: []db.ListConsentsByAccountRow{
		{ClientID: "grafana", DisplayName: "Grafana", GrantedScopes: []string{"openid", "profile"}, UpdatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true}},
	}}}
	out, err := s.listConsents(context.Background(), 1)
	if err != nil {
		t.Fatalf("listConsents: %v", err)
	}
	if len(out) != 1 || out[0].ClientID != "grafana" || len(out[0].Scopes) != 2 {
		t.Fatalf("unexpected: %+v", out)
	}
}

func TestHandleMyConsentRevoke(t *testing.T) {
	f := &fakeConsentQ{}
	s := &Server{consentMgmtOverride: f}
	if err := s.revokeConsent(context.Background(), 5, "grafana"); err != nil {
		t.Fatalf("revokeConsent: %v", err)
	}
	if len(f.deleted) != 1 || f.deleted[0].AccountID != 5 || f.deleted[0].ClientID != "grafana" {
		t.Fatalf("delete not scoped to caller: %+v", f.deleted)
	}
}
