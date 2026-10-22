package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/federation"
)

type fakeLoginAppQueries struct {
	clients map[string]db.OidcClient
	sps     map[int64]db.SamlSp
	icons   map[string]string // "kind/id" → etag
}

func (f fakeLoginAppQueries) GetOIDCClientAny(_ context.Context, clientID string) (db.OidcClient, error) {
	if c, ok := f.clients[clientID]; ok {
		return c, nil
	}
	return db.OidcClient{}, pgx.ErrNoRows
}

func (f fakeLoginAppQueries) GetSAMLSPByID(_ context.Context, id int64) (db.SamlSp, error) {
	if sp, ok := f.sps[id]; ok {
		return sp, nil
	}
	return db.SamlSp{}, pgx.ErrNoRows
}

func (f fakeLoginAppQueries) GetEntityIconEtag(_ context.Context, arg db.GetEntityIconEtagParams) (string, error) {
	if etag, ok := f.icons[arg.OwnerKind+"/"+arg.OwnerID]; ok {
		return etag, nil
	}
	return "", pgx.ErrNoRows
}

func TestLoginApplicationViewReadsCurrentRegistration(t *testing.T) {
	q := fakeLoginAppQueries{
		clients: map[string]db.OidcClient{
			"wiki":     {ClientID: "wiki", DisplayName: "Wiki"},
			"nameless": {ClientID: "nameless"},
			"off":      {ClientID: "off", DisplayName: "Off", Disabled: true},
		},
		sps: map[int64]db.SamlSp{
			12: {ID: 12, EntityID: "https://sp.test/metadata"},
			13: {ID: 13, DisplayName: "Gone", Disabled: true},
		},
		icons: map[string]string{"oidc_client/wiki": "etag-wiki-0001", "saml_sp/12": "etag-sp-00012"},
	}
	s := &Server{loginAppOverride: q}
	ctx := context.Background()

	wiki := s.loginApplicationView(ctx, &federation.AppRef{Kind: "oidc_client", ID: "wiki", Host: "wiki.test"})
	wantIcon := entityIconURL("oidc_client", "wiki", "etag-wiki-0001")
	if wiki == nil || wiki.Label != "Wiki" || wiki.Host != "wiki.test" || wiki.IconURL == nil || *wiki.IconURL != wantIcon {
		t.Fatalf("wiki = %+v, want Wiki at wiki.test with icon %q", wiki, wantIcon)
	}
	// No display name: the OIDC client is named by the host the user returns to.
	if got := s.loginApplicationView(ctx, &federation.AppRef{Kind: "oidc_client", ID: "nameless", Host: "app.test"}); got == nil || got.Label != "app.test" || got.IconURL != nil {
		t.Fatalf("nameless = %+v, want label app.test without icon", got)
	}
	// ... and the SAML service provider by its entity ID.
	if got := s.loginApplicationView(ctx, &federation.AppRef{Kind: "saml_sp", ID: "12"}); got == nil || got.Label != "https://sp.test/metadata" || got.IconURL == nil {
		t.Fatalf("saml = %+v, want the entity ID with its icon", got)
	}
	for name, ref := range map[string]*federation.AppRef{
		"nil":           nil,
		"disabled oidc": {Kind: "oidc_client", ID: "off"},
		"removed oidc":  {Kind: "oidc_client", ID: "removed"},
		"disabled saml": {Kind: "saml_sp", ID: "13"},
		"bad saml id":   {Kind: "saml_sp", ID: "x"},
		"other kind":    {Kind: "upstream_idp", ID: "wiki"},
	} {
		if got := s.loginApplicationView(ctx, ref); got != nil {
			t.Errorf("%s: got %+v, want no application", name, got)
		}
	}
}

func TestFederationConfirmGetNamesApplication(t *testing.T) {
	h := newFederationTestServer(t)
	h.s.loginAppOverride = fakeLoginAppQueries{clients: map[string]db.OidcClient{"wiki": {ClientID: "wiki", DisplayName: "Wiki"}}}
	const accountID int32 = 321
	h.q.accountByIDResults[accountID] = db.Account{ID: accountID, Username: "newbie", DisplayName: "New Bie"}
	returnTo := "/oauth/authorize?client_id=wiki&redirect_uri=https%3A%2F%2Fwiki.test%2Fcb&response_type=code"
	token, anti, err := h.s.federationService.CreateConfirmGrant(context.Background(), accountID, 654, h.idp.ID, h.idp.Slug,
		returnTo, &federation.AppRef{Kind: "oidc_client", ID: "wiki", Host: "wiki.test"}, nil, false, false)
	if err != nil {
		t.Fatal(err)
	}
	fx := confirmFixture{h: h, accountID: accountID, identityID: 654, cookie: token + "." + anti}

	resp := doConfirm(t, fx, http.MethodGet, confirmPath, fx.cookie)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET confirm: status %d", resp.StatusCode)
	}
	var view contract.FederationConfirmView
	if err := json.NewDecoder(resp.Body).Decode(&view); err != nil {
		t.Fatal(err)
	}
	if view.Application == nil || view.Application.Label != "Wiki" || view.Application.Host != "wiki.test" {
		t.Fatalf("application = %+v, want Wiki at wiki.test", view.Application)
	}
	if view.ReturnTo != returnTo {
		t.Fatalf("returnTo = %q, want %q", view.ReturnTo, returnTo)
	}
}

type iconlessConsentQueries struct{ oidcConsentQueries }

type iconConsentQueries struct {
	oidcConsentQueries
	etag string
}

func (q iconConsentQueries) GetEntityIconEtag(context.Context, db.GetEntityIconEtagParams) (string, error) {
	if q.etag == "" {
		return "", errors.New("no icon")
	}
	return q.etag, nil
}

// The consent page draws the uploaded icon first, as the sign-in page does,
// and the registered logo_uri only for an application without one.
func TestConsentClientIconPrefersUploadedIcon(t *testing.T) {
	ctx := context.Background()
	client := db.OidcClient{ClientID: "wiki", LogoUri: pgtype.Text{String: "https://wiki.test/logo.png", Valid: true}}
	if got, want := consentClientIcon(ctx, iconConsentQueries{etag: "etag-wiki-0001"}, client), entityIconURL("oidc_client", "wiki", "etag-wiki-0001"); got != want {
		t.Fatalf("with an uploaded icon: got %q, want %q", got, want)
	}
	if got := consentClientIcon(ctx, iconConsentQueries{}, client); got != "https://wiki.test/logo.png" {
		t.Fatalf("without an uploaded icon: got %q, want the logo_uri", got)
	}
	if got := consentClientIcon(ctx, iconlessConsentQueries{}, client); got != "https://wiki.test/logo.png" {
		t.Fatalf("query set without icons: got %q, want the logo_uri", got)
	}
}
