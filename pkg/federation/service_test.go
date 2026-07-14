package federation

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"prohibitorum/pkg/kv"
)

type fakeProviderLoader struct{ provider Provider }
func (s fakeProviderLoader) BySlug(context.Context, string) (Provider, error) { return s.provider, nil }
func (s fakeProviderLoader) ByBinding(_ context.Context, id int64, slug, protocol string) (Provider, error) {
	if s.provider.ID != id || s.provider.Slug != slug || s.provider.Protocol != protocol { return Provider{}, ErrUnknownProvider }
	return s.provider, nil
}
func (s fakeProviderLoader) InviteProvider(context.Context, string) (Provider, error) { return s.provider, nil }

type serviceFakeAdapter struct {
	beginState json.RawMessage
	beginAction NextAction
	advance func(json.RawMessage, ActionInput) (AdvanceResult, error)
	calls int
}
func (a *serviceFakeAdapter) Protocol() string { return "fake" }
func (a *serviceFakeAdapter) Begin(context.Context, Provider, BeginContext) (json.RawMessage, NextAction, error) {
	a.calls++; return a.beginState, a.beginAction, nil
}
func (a *serviceFakeAdapter) Advance(_ context.Context, _ Provider, state json.RawMessage, input ActionInput) (AdvanceResult, error) {
	a.calls++; return a.advance(state, input)
}

type serviceFakeResolver struct {
	known bool
	knownErr error
	outcome ResolveOutcome
	err error
	calls int
}
func (r *serviceFakeResolver) IdentityKnown(context.Context, IdentityKey) (bool, error) { return r.known, r.knownErr }
func (r *serviceFakeResolver) ResolveIdentity(context.Context, Provider, VerifiedIdentity, ResolveContext) (ResolveOutcome, error) {
	r.calls++; return r.outcome, r.err
}

func newServiceHarness(t *testing.T) (*Service, *serviceFakeAdapter, *serviceFakeResolver, kv.Store) {
	t.Helper()
	registry := NewRegistry()
	definition := fakeDefinition{protocol: "fake", descriptor: descriptor("fake")}
	if err := registry.RegisterDefinition(definition); err != nil { t.Fatal(err) }
	adapter := &serviceFakeAdapter{beginState: json.RawMessage(`{"step":1}`), beginAction: NextAction{Kind: ActionRedirect, URL: "https://upstream.test"}}
	if err := registry.RegisterAdapter(adapter); err != nil { t.Fatal(err) }
	resolver := &serviceFakeResolver{outcome: ResolveOutcome{AccountID: 5, IdentityID: 8, ProviderID: 7, AMR: []string{"fake"}, Confirmed: true}}
	store := kv.NewMemoryStore()
	service := NewService(registry, fakeProviderLoader{provider: Provider{ID: 7, Slug: "corp", Protocol: "fake", Mode: ModeAutoProvision}}, store, resolver, ServiceConfig{StateTTL: time.Minute, PublicOrigin: "https://idp.test"})
	return service, adapter, resolver, store
}

func TestServiceBeginStoresExactBindings(t *testing.T) {
	service, _, _, store := newServiceHarness(t)
	link, err := service.BeginLink(context.Background(), "corp", "/identities", 9, "session-1")
	if err != nil { t.Fatal(err) }
	raw, err := store.Get(context.Background(), FlowKey(link.FlowID)); if err != nil { t.Fatal(err) }
	state, err := DecodeFlowState(raw); if err != nil { t.Fatal(err) }
	if state.Intent != IntentLink || state.ProviderID != 7 || state.ProviderSlug != "corp" || state.Protocol != "fake" || state.LinkAccountID == nil || *state.LinkAccountID != 9 || state.LinkSessionID != "session-1" || state.ReturnTo != "/identities" {
		t.Fatalf("stored bindings = %+v", state)
	}
	if state.BrowserDigest == link.BrowserToken || !BrowserBindingOK(state.BrowserDigest, link.BrowserToken) { t.Fatal("browser token was not hashed and bound") }
	if state.CurrentAction.URL != link.Action.URL { t.Fatal("current action not persisted") }
}

func TestServiceReadFlowProjectsPersistedLocalAction(t *testing.T) {
	service, adapter, _, _ := newServiceHarness(t)
	adapter.beginAction = NextAction{Kind: ActionCollectIdentity, Public: map[string]any{"prompt": "handle"}}
	begin, err := service.BeginLogin(context.Background(), "corp", "/"); if err != nil { t.Fatal(err) }
	view, err := service.ReadFlow(context.Background(), begin.FlowID, begin.BrowserToken); if err != nil { t.Fatal(err) }
	if view.Action.Kind != ActionCollectIdentity || view.Action.Public["prompt"] != "handle" { t.Fatalf("view = %+v", view) }
}

func TestServiceRejectsRouteActionAndLinkBindingMismatch(t *testing.T) {
	service, _, _, _ := newServiceHarness(t)
	begin, err := service.BeginLink(context.Background(), "corp", "/", 9, "session-1"); if err != nil { t.Fatal(err) }
	base := AdvanceRequest{FlowID: begin.FlowID, BrowserToken: begin.BrowserToken, ProviderSlug: "corp", Protocol: "fake", AccountID: new(int32(9)), SessionID: "session-1", Input: ActionInput{Kind: ActionRedirect}}
	for name, mutate := range map[string]func(*AdvanceRequest){
		"slug": func(r *AdvanceRequest){ r.ProviderSlug = "other" },
		"protocol": func(r *AdvanceRequest){ r.Protocol = "other" },
		"action": func(r *AdvanceRequest){ r.Input.Kind = ActionPublishProof },
		"account": func(r *AdvanceRequest){ r.AccountID = new(int32(10)) },
		"session": func(r *AdvanceRequest){ r.SessionID = "session-2" },
	} {
		t.Run(name, func(t *testing.T) { request := base; mutate(&request); if _, err := service.AdvanceCallback(context.Background(), request); err == nil { t.Fatal("mismatch accepted") } })
	}
}

func TestServiceNonTerminalAdvanceReplacesStateAndAction(t *testing.T) {
	service, adapter, resolver, _ := newServiceHarness(t)
	adapter.advance = func(_ json.RawMessage, _ ActionInput) (AdvanceResult, error) {
		return AdvanceResult{State: json.RawMessage(`{"step":2}`), Next: &NextAction{Kind: ActionPublishProof, Public: map[string]any{"proof": "abc"}}, Candidate: &IdentityKey{Issuer: "iss", Subject: "unknown"}}, nil
	}
	begin, err := service.BeginLogin(context.Background(), "corp", "/"); if err != nil { t.Fatal(err) }
	view, err := service.PrepareFlow(context.Background(), AdvanceRequest{FlowID: begin.FlowID, BrowserToken: begin.BrowserToken, ProviderSlug: "corp", Protocol: "fake", Input: ActionInput{Kind: ActionRedirect}})
	if err != nil { t.Fatal(err) }
	if view.Action.Kind != ActionPublishProof || view.Action.Public["requiresLocalUsername"] != true { t.Fatalf("action = %+v", view.Action) }
	if resolver.calls != 0 { t.Fatal("non-terminal prepare resolved identity") }
}

func TestServiceTerminalFailureRestoresAndCommitConsumes(t *testing.T) {
	service, adapter, resolver, store := newServiceHarness(t)
	adapter.advance = func(_ json.RawMessage, _ ActionInput) (AdvanceResult, error) { return AdvanceResult{}, errors.New("upstream temporary") }
	begin, err := service.BeginLogin(context.Background(), "corp", "/"); if err != nil { t.Fatal(err) }
	request := AdvanceRequest{FlowID: begin.FlowID, BrowserToken: begin.BrowserToken, ProviderSlug: "corp", Protocol: "fake", Input: ActionInput{Kind: ActionRedirect}}
	if _, err := service.VerifyFlow(context.Background(), request); err == nil { t.Fatal("adapter failure accepted") }
	if _, err := store.Get(context.Background(), FlowKey(begin.FlowID)); err != nil { t.Fatalf("flow not restored: %v", err) }

	adapter.advance = func(_ json.RawMessage, _ ActionInput) (AdvanceResult, error) { return AdvanceResult{Identity: &VerifiedIdentity{Issuer: "iss", Subject: "sub", AMR: []string{"fake"}}}, nil }
	completion, err := service.VerifyFlow(context.Background(), request); if err != nil { t.Fatal(err) }
	if completion.AccountID != 5 || completion.ProviderSlug != "corp" { t.Fatalf("completion = %+v", completion) }
	if resolver.calls != 1 { t.Fatalf("resolver calls = %d", resolver.calls) }
	if _, err := service.VerifyFlow(context.Background(), request); err == nil { t.Fatal("terminal flow replayed") }
}

func TestServiceLocalUsernameRequiredRestoresOnlyPublicProjection(t *testing.T) {
	service, adapter, resolver, store := newServiceHarness(t)
	adapter.advance = func(_ json.RawMessage, _ ActionInput) (AdvanceResult, error) { return AdvanceResult{Identity: &VerifiedIdentity{Issuer: "iss", Subject: "sub"}}, nil }
	resolver.err = ErrLocalUsernameRequired
	begin, err := service.BeginLogin(context.Background(), "corp", "/"); if err != nil { t.Fatal(err) }
	before, _ := store.Get(context.Background(), FlowKey(begin.FlowID))
	request := AdvanceRequest{FlowID: begin.FlowID, BrowserToken: begin.BrowserToken, ProviderSlug: "corp", Protocol: "fake", Input: ActionInput{Kind: ActionRedirect}}
	if _, err := service.VerifyFlow(context.Background(), request); !errors.Is(err, ErrLocalUsernameRequired) { t.Fatalf("error = %v", err) }
	after, err := store.Get(context.Background(), FlowKey(begin.FlowID)); if err != nil { t.Fatal(err) }
	beforeState, _ := DecodeFlowState(before); afterState, _ := DecodeFlowState(after)
	if string(beforeState.AdapterState) != string(afterState.AdapterState) || !beforeState.ExpiresAt.Equal(afterState.ExpiresAt) || afterState.CurrentAction.Public["requiresLocalUsername"] != true { t.Fatalf("restored state = %+v", afterState) }
}
