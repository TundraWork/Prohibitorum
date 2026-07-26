package server

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"

	"prohibitorum/pkg/appaccess"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
)

type fakeLaunchpadQ struct {
	etags map[string]string // "kind/id" -> etag
}

type fakeAppLister struct {
	apps      []appaccess.AppSummary
	err       error
	accountID int32
	calls     int
}

func (f *fakeAppLister) ListAllowedApps(_ context.Context, accountID int32) ([]appaccess.AppSummary, error) {
	f.calls++
	f.accountID = accountID
	if f.err != nil {
		return nil, f.err
	}
	return f.apps, nil
}
func (f *fakeLaunchpadQ) GetEntityIconMeta(_ context.Context, p db.GetEntityIconMetaParams) (db.GetEntityIconMetaRow, error) {
	if e, ok := f.etags[p.OwnerKind+"/"+p.OwnerID]; ok {
		return db.GetEntityIconMetaRow{Etag: e}, nil
	}
	return db.GetEntityIconMetaRow{}, pgx.ErrNoRows
}

func TestHandleMyApps(t *testing.T) {
	lister := &fakeAppLister{apps: []appaccess.AppSummary{
		{Ref: appaccess.AppRef{Kind: appaccess.KindOIDC, OIDCClientID: "grafana"}, DisplayName: "Grafana", RedirectURIs: []string{"https://grafana.example/login/generic_oauth"}},
		{Ref: appaccess.AppRef{Kind: appaccess.KindOIDC, OIDCClientID: "no-redirect"}, DisplayName: "Headless"},
		{Ref: appaccess.AppRef{Kind: appaccess.KindForwardAuth, OIDCClientID: "wiki"}, DisplayName: "Wiki", ForwardAuthHost: "wiki.example"},
		{Ref: appaccess.AppRef{Kind: appaccess.KindSAML, SAMLSPID: 7}, DisplayName: "GitHub", EntityID: "https://ghe.example/saml"},
	}}
	s := &Server{
		launchpadOverride: &fakeLaunchpadQ{etags: map[string]string{"oidc_client/grafana": "abcdef1234"}},
		appLister:         lister,
	}
	apps, err := s.buildLaunchpad(context.Background(), 1)
	if err != nil {
		t.Fatalf("buildLaunchpad: %v", err)
	}
	if len(apps) != 3 { // Headless omitted
		t.Fatalf("want 3 apps, got %d: %+v", len(apps), apps)
	}
	if lister.calls != 1 || lister.accountID != 1 {
		t.Fatalf("ListAllowedApps calls/account = %d/%d, want 1/1", lister.calls, lister.accountID)
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

// Compile-time assertions for the launchpad seams.
var _ launchpadQueries = (*fakeLaunchpadQ)(nil)
var _ appaccess.AppLister = (*fakeAppLister)(nil)

// Compile-time assertion: contract.LaunchpadApp used in test.
var _ contract.LaunchpadApp = contract.LaunchpadApp{}
