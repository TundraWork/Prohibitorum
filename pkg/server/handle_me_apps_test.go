package server

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
)

type fakeLaunchpadQ struct {
	oidc  []db.ListAuthorizedOIDCClientsForAccountRow
	fwd   []db.ListAuthorizedForwardAuthAppsForAccountRow
	saml  []db.ListAuthorizedSAMLSPsForAccountRow
	etags map[string]string // "kind/id" -> etag
}

func (f *fakeLaunchpadQ) ListAuthorizedOIDCClientsForAccount(context.Context, pgtype.Int4) ([]db.ListAuthorizedOIDCClientsForAccountRow, error) {
	return f.oidc, nil
}
func (f *fakeLaunchpadQ) ListAuthorizedForwardAuthAppsForAccount(context.Context, pgtype.Int4) ([]db.ListAuthorizedForwardAuthAppsForAccountRow, error) {
	return f.fwd, nil
}
func (f *fakeLaunchpadQ) ListAuthorizedSAMLSPsForAccount(context.Context, pgtype.Int4) ([]db.ListAuthorizedSAMLSPsForAccountRow, error) {
	return f.saml, nil
}
func (f *fakeLaunchpadQ) GetEntityIconMeta(_ context.Context, p db.GetEntityIconMetaParams) (db.GetEntityIconMetaRow, error) {
	if e, ok := f.etags[p.OwnerKind+"/"+p.OwnerID]; ok {
		return db.GetEntityIconMetaRow{Etag: e}, nil
	}
	return db.GetEntityIconMetaRow{}, pgx.ErrNoRows
}

func TestHandleMyApps(t *testing.T) {
	s := &Server{launchpadOverride: &fakeLaunchpadQ{
		oidc: []db.ListAuthorizedOIDCClientsForAccountRow{
			{ClientID: "grafana", DisplayName: "Grafana", LaunchUrl: pgtype.Text{}, RedirectUris: []string{"https://grafana.example/login/generic_oauth"}},
			{ClientID: "no-redirect", DisplayName: "Headless", LaunchUrl: pgtype.Text{}, RedirectUris: nil}, // omitted: no launch URL
		},
		fwd:  []db.ListAuthorizedForwardAuthAppsForAccountRow{{ClientID: "wiki", DisplayName: "Wiki", ForwardAuthHost: pgtype.Text{String: "wiki.example", Valid: true}}},
		saml: []db.ListAuthorizedSAMLSPsForAccountRow{{ID: 7, EntityID: "https://ghe.example/saml", DisplayName: "GitHub"}},
		etags: map[string]string{"oidc_client/grafana": "abcdef1234"},
	}}
	apps, err := s.buildLaunchpad(context.Background(), 1)
	if err != nil {
		t.Fatalf("buildLaunchpad: %v", err)
	}
	if len(apps) != 3 { // Headless omitted
		t.Fatalf("want 3 apps, got %d: %+v", len(apps), apps)
	}
	idx := map[string]int{}
	for i, a := range apps {
		idx[a.ID] = i
	}
	g := apps[idx["grafana"]]
	if g.Kind != "oidc" || g.LaunchURL != "https://grafana.example/" {
		t.Fatalf("grafana: kind=%q launch=%q", g.Kind, g.LaunchURL)
	}
	if g.IconURL == nil || *g.IconURL == "" {
		t.Fatalf("grafana icon should be set, got %v", g.IconURL)
	}
	if w := apps[idx["wiki"]]; w.Kind != "forward_auth" || w.LaunchURL != "https://wiki.example/" {
		t.Fatalf("wiki: kind=%q launch=%q", w.Kind, w.LaunchURL)
	}
	if sm := apps[idx["7"]]; sm.Kind != "saml" || sm.LaunchURL != "/saml/sso/init?sp=https%3A%2F%2Fghe.example%2Fsaml" {
		t.Fatalf("saml: kind=%q launch=%q", sm.Kind, sm.LaunchURL)
	}

	// Verify sort: GitHub < Grafana < Wiki (case-sensitive lexicographic)
	names := make([]string, len(apps))
	for i, a := range apps {
		names[i] = a.Name
	}
	for i := 1; i < len(names); i++ {
		if names[i-1] > names[i] {
			t.Fatalf("apps not sorted: %v", names)
		}
	}
}

// Compile-time assertion: fakeLaunchpadQ satisfies launchpadQueries.
var _ launchpadQueries = (*fakeLaunchpadQ)(nil)

// Compile-time assertion: contract.LaunchpadApp used in test.
var _ contract.LaunchpadApp = contract.LaunchpadApp{}
