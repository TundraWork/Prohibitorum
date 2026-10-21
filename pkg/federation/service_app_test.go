package federation

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/kv"
)

func appTestService(t *testing.T, advanceErr error) (*Service, Provider, kv.Store) {
	t.Helper()
	registry := NewRegistry()
	if err := registry.RegisterDefinition(fakeDefinition{protocol: "oidc", descriptor: descriptor("oidc")}); err != nil {
		t.Fatal(err)
	}
	adapter := &serviceFakeAdapter{
		protocol: "oidc", beginState: json.RawMessage(`{"step":1}`),
		beginAction: NextAction{Kind: ActionRedirect, URL: "https://upstream.test"},
		advance: func(json.RawMessage, ActionInput) (AdvanceResult, error) {
			if advanceErr != nil {
				return AdvanceResult{}, advanceErr
			}
			return AdvanceResult{Identity: &VerifiedIdentity{Issuer: "https://upstream.test", Subject: "sub-1", AMR: []string{"federated"}}}, nil
		},
	}
	if err := registry.RegisterAdapter(adapter); err != nil {
		t.Fatal(err)
	}
	provider := Provider{ID: 7, Slug: "corp", Protocol: "oidc", Mode: ModeAutoProvision, AllowSignIn: true}
	store := kv.NewMemoryStore()
	resolver := &serviceFakeResolver{outcome: ResolveOutcome{AccountID: 9, IdentityID: 8}}
	service := NewService(registry, fakeProviderLoader{provider: provider}, store, resolver, ServiceConfig{StateTTL: time.Minute, PublicOrigin: "https://idp.test"})
	return service, provider, store
}

// The application is named once, when the flow begins, and every later step
// reads that reference rather than resolving return_to again.
func TestServiceBeginNamesApplicationOnce(t *testing.T) {
	service, provider, _ := appTestService(t, nil)
	var asked []string
	service.SetAppResolver(func(_ context.Context, returnTo string) *AppRef {
		asked = append(asked, returnTo)
		if returnTo == "/oauth/authorize?client_id=wiki" {
			return &AppRef{Kind: "oidc_client", ID: "wiki", Host: "wiki.test"}
		}
		return nil
	})
	want := AppRef{Kind: "oidc_client", ID: "wiki", Host: "wiki.test"}

	begin, err := service.BeginPublic(context.Background(), provider.Slug, "/oauth/authorize?client_id=wiki", false)
	if err != nil {
		t.Fatal(err)
	}
	view, err := service.ReadFlow(context.Background(), FlowReadRequest{
		FlowID: begin.FlowID, BrowserToken: begin.BrowserToken, CallbackRoute: CallbackRoutePublic,
	})
	if err != nil {
		t.Fatal(err)
	}
	if view.App == nil || *view.App != want {
		t.Fatalf("view.App = %+v, want %+v", view.App, want)
	}
	completion, err := service.AdvanceCallback(context.Background(), AdvanceRequest{
		FlowID: begin.FlowID, BrowserToken: begin.BrowserToken, ProviderSlug: provider.Slug,
		CallbackRoute: CallbackRoutePublic, Input: ActionInput{Kind: ActionRedirect, Code: "c"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if completion.App == nil || *completion.App != want {
		t.Fatalf("completion.App = %+v, want %+v", completion.App, want)
	}
	if len(asked) != 1 {
		t.Fatalf("resolver asked %d times (%q), want once at begin", len(asked), asked)
	}

	// A return_to that names nothing leaves the flow without an application.
	plain, err := service.BeginPublic(context.Background(), provider.Slug, "/security", false)
	if err != nil {
		t.Fatal(err)
	}
	plainView, err := service.ReadFlow(context.Background(), FlowReadRequest{
		FlowID: plain.FlowID, BrowserToken: plain.BrowserToken, CallbackRoute: CallbackRoutePublic,
	})
	if err != nil {
		t.Fatal(err)
	}
	if plainView.App != nil {
		t.Fatalf("plain flow App = %+v, want nil", plainView.App)
	}
}

// The confirmation grant keeps the reference for /welcome, and rejects one
// that is not an application reference.
func TestConfirmGrantCarriesApplication(t *testing.T) {
	service, provider, _ := appTestService(t, nil)
	app := &AppRef{Kind: "saml_sp", ID: "12", Host: "sp.test"}
	token, browser, err := service.CreateConfirmGrant(context.Background(), 9, 8, provider.ID, provider.Slug, "/saml/sso/init?sp=x", app, nil, false, false)
	if err != nil {
		t.Fatal(err)
	}
	grant, err := service.PeekConfirmGrant(context.Background(), token, browser)
	if err != nil {
		t.Fatal(err)
	}
	if grant.App == nil || *grant.App != *app {
		t.Fatalf("grant.App = %+v, want %+v", grant.App, app)
	}
	if _, _, err := service.CreateConfirmGrant(context.Background(), 9, 8, provider.ID, provider.Slug, "/", &AppRef{Kind: "upstream_idp", ID: "x"}, nil, false, false); err == nil {
		t.Fatal("grant with a non-application reference was stored")
	}
}

// A failed callback hands its return_to to the error page, but only to the
// browser the flow is bound to.
func TestAdvanceCallbackFailureCarriesReturnTo(t *testing.T) {
	failure := authn.ErrFederationStateInvalid()
	service, provider, _ := appTestService(t, failure)

	begin, err := service.BeginPublic(context.Background(), provider.Slug, "/oauth/authorize?client_id=wiki", false)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.AdvanceCallback(context.Background(), AdvanceRequest{
		FlowID: begin.FlowID, BrowserToken: begin.BrowserToken, ProviderSlug: provider.Slug,
		CallbackRoute: CallbackRoutePublic, Input: ActionInput{Kind: ActionRedirect, Code: "c"},
	})
	var callbackErr *CallbackError
	if !errors.As(err, &callbackErr) || callbackErr.ReturnTo != "/oauth/authorize?client_id=wiki" {
		t.Fatalf("err = %v, want a CallbackError with the flow's return_to", err)
	}
	if authn.AsAuthError(err) == nil {
		t.Fatalf("err = %v, want the auth error to stay readable", err)
	}

	other, err := service.BeginPublic(context.Background(), provider.Slug, "/oauth/authorize?client_id=wiki", false)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.AdvanceCallback(context.Background(), AdvanceRequest{
		FlowID: other.FlowID, BrowserToken: "another-browser", ProviderSlug: provider.Slug,
		CallbackRoute: CallbackRoutePublic, Input: ActionInput{Kind: ActionRedirect, Code: "c"},
	})
	if err == nil || errors.As(err, &callbackErr) {
		t.Fatalf("err = %v, want a plain failure for an unbound browser", err)
	}
}
