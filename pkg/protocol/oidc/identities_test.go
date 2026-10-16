package oidc

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"testing"
	"time"

	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/db"
)

func identityRow(slug, sub, data string) db.ListConfirmedIdentitiesForProvidersRow {
	return db.ListConfirmedIdentitiesForProvidersRow{IdpSlug: slug, UpstreamSub: sub, UpstreamData: []byte(data)}
}

func TestGrantedIdentitySlugs(t *testing.T) {
	got := grantedIdentitySlugs([]string{"openid", "identity:vrchat", "groups", "identity:steam", "identity:vrchat", "identity:-"})
	if want := []string{"vrchat", "steam"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("grantedIdentitySlugs = %v, want %v", got, want)
	}
	if got := grantedIdentitySlugs([]string{"openid", "profile"}); got != nil {
		t.Fatalf("grantedIdentitySlugs(no identity scope) = %v, want nil", got)
	}
}

func TestIdentitiesClaimMapsEachProtocol(t *testing.T) {
	rows := []db.ListConfirmedIdentitiesForProvidersRow{
		identityRow("vrchat", "usr_1", `{"userId":"usr_1","displayName":"Neptune","profileUrl":"https://vrchat.com/home/user/usr_1"}`),
		identityRow("steam", "7656", `{"steamId":"7656","personaName":"nep","profileUrl":"https://steamcommunity.com/profiles/7656","avatarUrl":"https://x/a.jpg"}`),
		identityRow("corp", "oidc-sub", `{}`),
		identityRow("vrchat", "usr_2", `{"displayName":"Alt","profileUrl":"javascript:alert(1)"}`),
		identityRow("unrequested", "x", `{}`),
	}
	got := identitiesClaim([]string{"vrchat", "steam", "corp", "unlinked"}, rows)
	want := map[string][]identityClaim{
		"vrchat": {
			{Sub: "usr_1", Name: "Neptune", ProfileURL: "https://vrchat.com/home/user/usr_1"},
			{Sub: "usr_2", Name: "Alt"},
		},
		"steam":    {{Sub: "7656", Name: "nep", ProfileURL: "https://steamcommunity.com/profiles/7656"}},
		"corp":     {{Sub: "oidc-sub"}},
		"unlinked": {},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("identitiesClaim =\n%#v\nwant\n%#v", got, want)
	}
	encoded, _ := json.Marshal(got["unlinked"])
	if string(encoded) != "[]" {
		t.Fatalf("an unlinked provider encodes as %s, want []", encoded)
	}
	encoded, _ = json.Marshal(got["corp"])
	if string(encoded) != `[{"sub":"oidc-sub"}]` {
		t.Fatalf("an OIDC identity encodes as %s, want only sub", encoded)
	}
}

func TestIDTokenClaimsIdentities(t *testing.T) {
	a := db.Account{ID: 7, Username: "alice"}
	base := idTokenInput{Issuer: testIssuer, Audience: testClientID, Scope: []string{"openid"}}
	if _, ok := idTokenClaims(a, base)["identities"]; ok {
		t.Fatal("identities must be absent when no identity scope was granted")
	}
	with := base
	with.Identities = map[string][]identityClaim{"vrchat": {}}
	if got := idTokenClaims(a, with)["identities"]; !reflect.DeepEqual(got, with.Identities) {
		t.Fatalf("identities = %#v, want %#v", got, with.Identities)
	}
}

func TestIdentitiesForScopesSkipsQueryWithoutIdentityScope(t *testing.T) {
	h := newEndpointHarness(t)
	got, err := h.p.identitiesForScopes(context.Background(), 7, []string{"openid", "profile", "groups"})
	if err != nil || got != nil {
		t.Fatalf("identitiesForScopes = %v, %v; want nil, nil", got, err)
	}
	if len(h.q.identityCalls) != 0 {
		t.Fatalf("queried %d times without an identity scope, want 0", len(h.q.identityCalls))
	}
}

func TestUserinfoIdentities(t *testing.T) {
	h := newEndpointHarness(t)
	h.q.identities = []db.ListConfirmedIdentitiesForProvidersRow{
		identityRow("vrchat", "usr_1", `{"displayName":"Neptune","profileUrl":"https://vrchat.com/home/user/usr_1"}`),
		identityRow("steam", "7656", `{"personaName":"nep"}`),
	}
	at := h.mintAccessToken(t, testSubject, testClientID, "openid identity:vrchat", "jti-identities", time.Now().Add(time.Hour))
	rec := httptest.NewRecorder()
	h.p.HandleUserinfo(rec, userinfoReq(at))
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body)
	}
	var body struct {
		Identities map[string][]identityClaim `json:"identities"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	want := map[string][]identityClaim{"vrchat": {{Sub: "usr_1", Name: "Neptune", ProfileURL: "https://vrchat.com/home/user/usr_1"}}}
	if !reflect.DeepEqual(body.Identities, want) {
		t.Fatalf("identities = %#v, want %#v (steam was not granted)", body.Identities, want)
	}
	if len(h.q.identityCalls) != 1 || h.q.identityCalls[0].AccountID != 7 {
		t.Fatalf("identity queries = %#v, want one for account 7", h.q.identityCalls)
	}

	h.q.identitiesErr = errors.New("database unavailable")
	rec = httptest.NewRecorder()
	h.p.HandleUserinfo(rec, userinfoReq(at))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("a failed identity read must not answer without the claim: got %d", rec.Code)
	}
}

func TestMintedIDTokenCarriesCurrentIdentities(t *testing.T) {
	h := newEndpointHarness(t)
	ctx := context.Background()
	acct := h.q.byID[7]
	client := h.q.clients[testClientID]
	scope := []string{"openid", "identity:vrchat"}
	mint := func() map[string]any {
		t.Helper()
		_, idToken, err := h.p.mintAccessAndIDTokens(ctx, acct, client, "", "sid", "", []string{"webauthn"}, scope, nil, time.Now(), time.Now())
		if err != nil {
			t.Fatalf("mint: %v", err)
		}
		claims, _, err := h.p.verifyJWT(ctx, idToken)
		if err != nil {
			t.Fatalf("verify: %v", err)
		}
		return claims
	}
	if got := mint()["identities"]; !reflect.DeepEqual(got, map[string]any{"vrchat": []any{}}) {
		t.Fatalf("identities before linking = %#v, want vrchat: []", got)
	}
	// A refresh mints through the same path, so a link made since the first
	// token shows up in the next one.
	h.q.identities = []db.ListConfirmedIdentitiesForProvidersRow{identityRow("vrchat", "usr_1", `{}`)}
	if got := mint()["identities"]; !reflect.DeepEqual(got, map[string]any{"vrchat": []any{map[string]any{"sub": "usr_1"}}}) {
		t.Fatalf("identities after linking = %#v", got)
	}
	h.q.identitiesErr = errors.New("database unavailable")
	if _, _, err := h.p.mintAccessAndIDTokens(ctx, acct, client, "", "sid", "", nil, scope, nil, time.Now(), time.Now()); err == nil {
		t.Fatal("mint succeeded although the identity read failed")
	}
}

func TestClaimAliasCannotShadowIdentities(t *testing.T) {
	if err := validateClaimAliases(map[string]string{"identities": "name"}); err == nil {
		t.Fatal("an alias named identities would overwrite the identities claim")
	}
}

// discoveryProviders answers ListUpstreamIDPs (enabled providers only).
type discoveryProviders struct {
	db.Querier
	rows []db.UpstreamIdp
	err  error
}

func (d discoveryProviders) ListUpstreamIDPs(context.Context) ([]db.UpstreamIdp, error) {
	return d.rows, d.err
}

func TestDiscoveryListsIdentityScopes(t *testing.T) {
	discover := func(q db.Querier) map[string]any {
		t.Helper()
		cfg := &configx.Config{}
		cfg.OIDC.Issuer = "https://idp.example"
		rec := httptest.NewRecorder()
		(&Provider{cfg: cfg, queries: q}).HandleDiscovery(rec, httptest.NewRequest(http.MethodGet, "/.well-known/openid-configuration", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var doc map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &doc)
		return doc
	}
	doc := discover(discoveryProviders{rows: []db.UpstreamIdp{{Slug: "vrchat"}, {Slug: "corp"}}})
	want := []any{"openid", "profile", "email", "offline_access", "groups", "identity:corp", "identity:vrchat"}
	if !reflect.DeepEqual(doc["scopes_supported"], want) {
		t.Fatalf("scopes_supported = %v, want %v", doc["scopes_supported"], want)
	}
	if !slices.Contains(doc["claims_supported"].([]any), any("identities")) {
		t.Fatalf("claims_supported = %v, want it to include identities", doc["claims_supported"])
	}
	doc = discover(discoveryProviders{err: errors.New("database unavailable")})
	if !reflect.DeepEqual(doc["scopes_supported"], []any{"openid", "profile", "email", "offline_access", "groups"}) {
		t.Fatalf("scopes_supported on a failed provider read = %v, want the fixed scopes", doc["scopes_supported"])
	}
}
