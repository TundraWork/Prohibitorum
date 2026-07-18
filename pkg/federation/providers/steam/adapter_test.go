package steam

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"prohibitorum/pkg/authn"
	federationcore "prohibitorum/pkg/federation"
	"prohibitorum/pkg/kv"
)

func TestAdapterMapsVerifiedSteamIdentity(t *testing.T) {
	store := federationcore.NewSecretStore(map[int][]byte{1: make([]byte, 32)})
	secret, err := store.SealProviderSecret([]byte("api-key"), 7, 1)
	if err != nil {
		t.Fatal(err)
	}
	provider := federationcore.Provider{ID: 7, Slug: "steam", Protocol: Protocol, Config: json.RawMessage(`{}`), Secret: secret, SecretStatus: "valid"}
	adapter := NewAdapter(store)
	adapter.verify = func(context.Context, url.Values, string) (string, error) { return "76561198000000000", nil }
	adapter.summary = func(context.Context, string, string) (Summary, error) {
		return Summary{PersonaName: "Gaben", AvatarURL: "https://cdn/avatar.jpg"}, nil
	}
	state, action, err := adapter.Begin(context.Background(), provider, federationcore.BeginContext{FlowID: "flow", CallbackURL: "https://idp.test/api/prohibitorum/auth/federation/steam/callback"})
	if err != nil {
		t.Fatal(err)
	}
	if action.Kind != federationcore.ActionRedirect || action.URL == "" {
		t.Fatalf("action = %+v", action)
	}
	result, err := adapter.Advance(context.Background(), provider, state, federationcore.ActionInput{Kind: federationcore.ActionRedirect, Params: url.Values{"openid.mode": {"id_res"}}})
	if err != nil {
		t.Fatal(err)
	}
	identity := result.Identity
	if identity == nil ||
		identity.Issuer != Issuer ||
		identity.Subject != "76561198000000000" ||
		identity.Username != "steam_76561198000000000" ||
		identity.DisplayName != "Gaben" ||
		identity.AvatarURL != "https://cdn/avatar.jpg" ||
		len(identity.UpstreamData) != 4 ||
		identity.UpstreamData["steamId"] != "76561198000000000" ||
		identity.UpstreamData["personaName"] != "Gaben" ||
		identity.UpstreamData["profileUrl"] != "https://steamcommunity.com/profiles/76561198000000000" ||
		identity.UpstreamData["avatarUrl"] != "https://cdn/avatar.jpg" {
		t.Fatalf("identity = %+v", identity)
	}
}

func TestAdapterReusesHardenedClient(t *testing.T) {
	secrets := federationcore.NewSecretStore(map[int][]byte{1: make([]byte, 32)})
	sealed, err := secrets.SealProviderSecret([]byte("api-key"), 7, 1)
	if err != nil {
		t.Fatal(err)
	}
	adapter := NewAdapter(secrets)
	created := map[bool]int{}
	adapter.newHTTPClient = func(allowPrivate bool) *http.Client {
		created[allowPrivate]++
		return &http.Client{}
	}
	adapter.verify = func(context.Context, url.Values, string) (string, error) {
		return "76561198000000000", nil
	}
	adapter.summary = func(context.Context, string, string) (Summary, error) {
		return Summary{PersonaName: "Player"}, nil
	}
	state := json.RawMessage(`{"returnTo":"https://idp.test/callback?state=flow"}`)
	input := federationcore.ActionInput{Kind: federationcore.ActionRedirect}

	provider := federationcore.Provider{
		ID: 7, Protocol: Protocol, Secret: sealed, SecretStatus: "valid",
		Config: json.RawMessage(`{}`),
	}
	for range 3 {
		if _, err := adapter.Advance(context.Background(), provider, state, input); err != nil {
			t.Fatal(err)
		}
	}
	if created[false] != 1 || created[true] != 0 {
		t.Fatalf("hardened client constructions = %v, want one public client", created)
	}
}

func TestAdapterClassifiesOpenIDVerificationFailureAsStateInvalid(t *testing.T) {
	store := federationcore.NewSecretStore(map[int][]byte{1: make([]byte, 32)})
	secret, err := store.SealProviderSecret([]byte("api-key"), 7, 1)
	if err != nil {
		t.Fatal(err)
	}
	provider := federationcore.Provider{
		ID: 7, Slug: "steam", Protocol: Protocol, Config: json.RawMessage(`{}`),
		Secret: secret, SecretStatus: "valid",
	}
	adapter := NewAdapter(store)
	adapter.verify = func(context.Context, url.Values, string) (string, error) {
		return "", errors.New("openid check_authentication rejected")
	}
	state, _, err := adapter.Begin(context.Background(), provider, federationcore.BeginContext{
		FlowID: "flow", CallbackURL: "https://idp.test/api/prohibitorum/auth/federation/steam/callback",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = adapter.Advance(context.Background(), provider, state, federationcore.ActionInput{
		Kind:   federationcore.ActionRedirect,
		Params: url.Values{"openid.mode": {"id_res"}},
	})
	if ae := authn.AsAuthError(err); ae == nil || ae.Code != "federation_state_invalid" {
		t.Fatalf("Advance error = %v, want federation_state_invalid", err)
	}
	if reason, ok := federationcore.FailureReasonOf(err); !ok || reason != federationcore.FailureSteamVerification {
		t.Fatalf("failure reason = %q, want steam_verify_failed", reason)
	}
}

func TestDefinitionRequiresValidatedSecret(t *testing.T) {
	definition := Definition{}
	provider := federationcore.Provider{Protocol: Protocol, Config: json.RawMessage(`{}`), Secret: &federationcore.SealedSecret{Ciphertext: []byte{1}, Nonce: []byte{2}, KeyVersion: 1}, SecretStatus: "valid"}
	if !definition.Ready(provider) {
		t.Fatal("valid Steam provider not ready")
	}
	provider.SecretStatus = "invalid"
	if definition.Ready(provider) {
		t.Fatal("invalid secret status ready")
	}
}

type steamServiceProviders struct {
	provider federationcore.Provider
}

func (p steamServiceProviders) BySlug(context.Context, string) (federationcore.Provider, error) {
	return p.provider, nil
}

func (p steamServiceProviders) ByBinding(_ context.Context, id int64, slug, protocol string) (federationcore.Provider, error) {
	if p.provider.ID != id || p.provider.Slug != slug || p.provider.Protocol != protocol {
		return federationcore.Provider{}, federationcore.ErrUnknownProvider
	}
	return p.provider, nil
}

func (p steamServiceProviders) InviteProvider(context.Context, string) (federationcore.Provider, error) {
	return p.provider, nil
}

type steamServiceResolver struct {
	identity federationcore.VerifiedIdentity
}

func (*steamServiceResolver) IdentityKnown(context.Context, federationcore.IdentityKey) (bool, error) {
	return false, nil
}

func (r *steamServiceResolver) ResolveIdentity(_ context.Context, provider federationcore.Provider, identity federationcore.VerifiedIdentity, _ federationcore.ResolveContext) (federationcore.ResolveOutcome, error) {
	r.identity = identity
	return federationcore.ResolveOutcome{
		AccountID: 9, IdentityID: 11, ProviderID: provider.ID,
		AMR: append([]string(nil), identity.AMR...), Confirmed: true,
	}, nil
}

func TestSteamHTTPFlowThroughFederationService(t *testing.T) {
	const (
		steamID      = "76561198000000000"
		providerSlug = "steam?#primary"
	)
	var checkAuthenticationCalls, summaryCalls int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/openid":
			checkAuthenticationCalls++
			if err := r.ParseForm(); err != nil {
				t.Error(err)
				http.Error(w, "bad form", http.StatusBadRequest)
				return
			}
			if r.Method != http.MethodPost || r.FormValue("openid.mode") != "check_authentication" {
				t.Errorf("OpenID verification request = %s %v", r.Method, r.Form)
			}
			_, _ = w.Write([]byte("ns:http://specs.openid.net/auth/2.0\nis_valid:true\n"))
		case "/summary":
			summaryCalls++
			if r.URL.Query().Get("key") != "steam-api-key" || r.URL.Query().Get("steamids") != steamID {
				t.Errorf("summary query = %v", r.URL.Query())
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"response":{"players":[{"personaname":"Pro Gamer","avatarfull":"https://cdn.test/avatar.jpg"}]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	restoreEndpoints := SetEndpoints(upstream.URL+"/openid", upstream.URL+"/summary")
	defer restoreEndpoints()

	secrets := federationcore.NewSecretStore(map[int][]byte{1: make([]byte, 32)})
	sealed, err := secrets.SealProviderSecret([]byte("steam-api-key"), 7, 1)
	if err != nil {
		t.Fatal(err)
	}
	provider := federationcore.Provider{
		ID: 7, Slug: providerSlug, Protocol: Protocol, Mode: federationcore.ModeAutoProvision,
		Config: json.RawMessage(`{}`),
		Secret: sealed, SecretStatus: "valid",
	}
	registry := federationcore.NewRegistry()
	adapter := NewAdapter(secrets)
	adapter.newHTTPClient = func(bool) *http.Client { return upstream.Client() }
	if err := registry.RegisterDefinition(Definition{}); err != nil {
		t.Fatal(err)
	}
	if err := registry.RegisterAdapter(adapter); err != nil {
		t.Fatal(err)
	}
	resolver := &steamServiceResolver{}
	store := kv.NewMemoryStore()
	service := federationcore.NewService(
		registry,
		steamServiceProviders{provider: provider},
		store,
		resolver,
		nil,
		federationcore.ServiceConfig{PublicOrigin: "https://idp.test"},
	)
	begin, err := service.BeginPublic(context.Background(), providerSlug, "/me")
	if err != nil {
		t.Fatal(err)
	}
	authorizeURL, err := url.Parse(begin.Action.URL)
	if err != nil {
		t.Fatal(err)
	}
	returnTo := authorizeURL.Query().Get("openid.return_to")
	if authorizeURL.Host != upstream.Listener.Addr().String() || returnTo == "" {
		t.Fatalf("authorize action = %s", begin.Action.URL)
	}
	callbackURL, err := url.Parse(returnTo)
	if err != nil {
		t.Fatal(err)
	}
	if callbackURL.EscapedPath() != "/api/prohibitorum/auth/federation/steam%3F%23primary/callback" ||
		callbackURL.Query().Get("state") != begin.FlowID || callbackURL.Fragment != "" {
		t.Fatalf("callback URL did not round-trip reserved slug: %s", returnTo)
	}
	claimedID := Issuer + "/id/" + steamID
	completion, err := service.VerifyFlow(context.Background(), federationcore.AdvanceRequest{
		FlowID: begin.FlowID, BrowserToken: begin.BrowserToken,
		ProviderSlug: providerSlug, Protocol: Protocol, CallbackRoute: federationcore.CallbackRoutePublic,
		Input: federationcore.ActionInput{
			Kind: federationcore.ActionRedirect,
			Params: url.Values{
				"openid.mode":       {"id_res"},
				"openid.return_to":  {returnTo},
				"openid.claimed_id": {claimedID},
				"openid.identity":   {claimedID},
				"openid.signed":     {"mode,return_to,claimed_id,identity"},
				"openid.sig":        {"signature"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if checkAuthenticationCalls != 1 || summaryCalls != 1 {
		t.Fatalf("upstream HTTP calls: check_authentication=%d summary=%d", checkAuthenticationCalls, summaryCalls)
	}
	if resolver.identity.Issuer != Issuer || resolver.identity.Subject != steamID ||
		resolver.identity.Username != "steam_"+steamID || resolver.identity.DisplayName != "Pro Gamer" ||
		resolver.identity.AvatarURL != "https://cdn.test/avatar.jpg" {
		t.Fatalf("resolved identity = %+v", resolver.identity)
	}
	if completion.AccountID != 9 || completion.ProviderID != 7 ||
		len(completion.AMR) != 1 || completion.AMR[0] != "steam" {
		t.Fatalf("completion = %+v", completion)
	}
}
