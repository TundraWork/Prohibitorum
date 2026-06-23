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
	rows        []db.ListConsentsByAccountRow
	samlRows    []db.ListSAMLConsentsByAccountRow
	deleted     []db.DeleteConsentParams
	deletedSAML []db.DeleteSAMLConsentParams
}

func (f *fakeConsentQ) ListConsentsByAccount(context.Context, int32) ([]db.ListConsentsByAccountRow, error) {
	return f.rows, nil
}
func (f *fakeConsentQ) DeleteConsent(_ context.Context, p db.DeleteConsentParams) error {
	f.deleted = append(f.deleted, p)
	return nil
}
func (f *fakeConsentQ) ListSAMLConsentsByAccount(context.Context, int32) ([]db.ListSAMLConsentsByAccountRow, error) {
	return f.samlRows, nil
}
func (f *fakeConsentQ) DeleteSAMLConsent(_ context.Context, p db.DeleteSAMLConsentParams) error {
	f.deletedSAML = append(f.deletedSAML, p)
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
	if out[0].Kind != "oidc" {
		t.Fatalf("expected kind=oidc, got %q", out[0].Kind)
	}
}

func TestHandleMyConsentRevoke(t *testing.T) {
	f := &fakeConsentQ{}
	s := &Server{consentMgmtOverride: f}
	if err := s.revokeConsent(context.Background(), 5, "", "grafana"); err != nil {
		t.Fatalf("revokeConsent: %v", err)
	}
	if len(f.deleted) != 1 || f.deleted[0].AccountID != 5 || f.deleted[0].ClientID != "grafana" {
		t.Fatalf("delete not scoped to caller: %+v", f.deleted)
	}
}

// TestHandleMyConsentListMergedAndSorted verifies that listConsents returns
// OIDC and SAML entries merged and sorted by name, each tagged with the
// correct kind.
func TestHandleMyConsentListMergedAndSorted(t *testing.T) {
	now := time.Now()
	ts := func() pgtype.Timestamptz { return pgtype.Timestamptz{Time: now, Valid: true} }

	f := &fakeConsentQ{
		rows: []db.ListConsentsByAccountRow{
			{ClientID: "oidc-z", DisplayName: "Zephyr SSO", GrantedScopes: []string{"openid"}, UpdatedAt: ts()},
			{ClientID: "oidc-a", DisplayName: "Alpha Corp", GrantedScopes: []string{"openid", "profile"}, UpdatedAt: ts()},
		},
		samlRows: []db.ListSAMLConsentsByAccountRow{
			{SpID: 42, EntityID: "https://mid.example.com", DisplayName: "Midway App", UpdatedAt: ts()},
			{SpID: 7, EntityID: "https://beta.example.com", DisplayName: "Beta SP", UpdatedAt: ts()},
		},
	}
	s := &Server{consentMgmtOverride: f}
	out, err := s.listConsents(context.Background(), 1)
	if err != nil {
		t.Fatalf("listConsents: %v", err)
	}
	if len(out) != 4 {
		t.Fatalf("expected 4 entries, got %d: %+v", len(out), out)
	}

	// Expected sort order: Alpha Corp (oidc), Beta SP (saml), Midway App (saml), Zephyr SSO (oidc)
	want := []struct {
		name string
		kind string
	}{
		{"Alpha Corp", "oidc"},
		{"Beta SP", "saml"},
		{"Midway App", "saml"},
		{"Zephyr SSO", "oidc"},
	}
	for i, w := range want {
		if out[i].Name != w.name {
			t.Errorf("entry[%d]: want name %q, got %q", i, w.name, out[i].Name)
		}
		if out[i].Kind != w.kind {
			t.Errorf("entry[%d]: want kind %q, got %q", i, w.kind, out[i].Kind)
		}
	}

	// SAML entries carry an empty (non-nil) scope list so the JSON is "scopes":[]
	// not "scopes":null.
	for _, e := range out {
		if e.Kind == "saml" && (e.Scopes == nil || len(e.Scopes) != 0) {
			t.Errorf("saml entry %q should have empty non-nil scopes, got %v", e.Name, e.Scopes)
		}
	}

	// SAML ClientID should be the string form of SpID
	for _, e := range out {
		if e.Kind == "saml" {
			if e.Name == "Midway App" && e.ClientID != "42" {
				t.Errorf("Midway App clientId: want %q, got %q", "42", e.ClientID)
			}
			if e.Name == "Beta SP" && e.ClientID != "7" {
				t.Errorf("Beta SP clientId: want %q, got %q", "7", e.ClientID)
			}
		}
	}
}

// TestHandleMyConsentRevokeSAML verifies that revoking kind="saml" calls
// DeleteSAMLConsent and does NOT call DeleteConsent.
func TestHandleMyConsentRevokeSAML(t *testing.T) {
	f := &fakeConsentQ{}
	s := &Server{consentMgmtOverride: f}
	if err := s.revokeConsent(context.Background(), 9, "saml", "42"); err != nil {
		t.Fatalf("revokeConsent saml: %v", err)
	}
	if len(f.deletedSAML) != 1 || f.deletedSAML[0].AccountID != 9 || f.deletedSAML[0].SpID != 42 {
		t.Fatalf("DeleteSAMLConsent not called correctly: %+v", f.deletedSAML)
	}
	if len(f.deleted) != 0 {
		t.Fatalf("DeleteConsent should not have been called, got: %+v", f.deleted)
	}
}

// TestHandleMyConsentRevokeOIDCDefaultKind verifies that revoking with no
// kind (empty string) defaults to the OIDC path.
func TestHandleMyConsentRevokeOIDCDefaultKind(t *testing.T) {
	f := &fakeConsentQ{}
	s := &Server{consentMgmtOverride: f}
	if err := s.revokeConsent(context.Background(), 3, "", "app-x"); err != nil {
		t.Fatalf("revokeConsent default: %v", err)
	}
	if len(f.deleted) != 1 || f.deleted[0].AccountID != 3 || f.deleted[0].ClientID != "app-x" {
		t.Fatalf("DeleteConsent not called correctly: %+v", f.deleted)
	}
	if len(f.deletedSAML) != 0 {
		t.Fatalf("DeleteSAMLConsent should not have been called, got: %+v", f.deletedSAML)
	}
}
