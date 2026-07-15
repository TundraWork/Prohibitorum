package federation

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"


	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/kv"
)

type fakeProviderLoader struct {
	provider  Provider
	bySlugErr error
	inviteErr error
}

func (s fakeProviderLoader) BySlug(context.Context, string) (Provider, error) {
	return s.provider, s.bySlugErr
}

func (s fakeProviderLoader) ByBinding(_ context.Context, id int64, slug, protocol string) (Provider, error) {
	if s.bySlugErr != nil {
		return Provider{}, s.bySlugErr
	}
	if s.provider.ID != id || s.provider.Slug != slug || s.provider.Protocol != protocol {
		return Provider{}, ErrUnknownProvider
	}
	return s.provider, nil
}

func (s fakeProviderLoader) InviteProvider(context.Context, string) (Provider, error) {
	return s.provider, s.inviteErr
}

type serviceFakeAdapter struct {
	beginState    json.RawMessage
	beginAction   NextAction
	beginContexts []BeginContext
	advance       func(json.RawMessage, ActionInput) (AdvanceResult, error)
	advanceContext func(context.Context, json.RawMessage, ActionInput) (AdvanceResult, error)
	calls         int
}

func (a *serviceFakeAdapter) Protocol() string { return "fake" }

func (a *serviceFakeAdapter) Begin(_ context.Context, _ Provider, begin BeginContext) (json.RawMessage, NextAction, error) {
	a.calls++
	a.beginContexts = append(a.beginContexts, begin)
	return a.beginState, a.beginAction, nil
}

func (a *serviceFakeAdapter) Advance(ctx context.Context, _ Provider, state json.RawMessage, input ActionInput) (AdvanceResult, error) {
	a.calls++
	if a.advanceContext != nil {
		return a.advanceContext(ctx, state, input)
	}
	return a.advance(state, input)
}

type serviceFakeResolver struct {
	known           bool
	knownErr        error
	knownCalls      int
	outcome         ResolveOutcome
	err             error
	calls           int
	resolveContexts []ResolveContext
}

func (r *serviceFakeResolver) IdentityKnown(context.Context, IdentityKey) (bool, error) {
	r.knownCalls++
	return r.known, r.knownErr
}

func (r *serviceFakeResolver) ResolveIdentity(_ context.Context, _ Provider, _ VerifiedIdentity, resolution ResolveContext) (ResolveOutcome, error) {
	r.calls++
	r.resolveContexts = append(r.resolveContexts, resolution)
	return r.outcome, r.err
}

type serviceFakeAvatar struct {
	calls int
}

func (a *serviceFakeAvatar) Inherit(int32, Provider, AvatarDelivery, AvatarResolver) {
	a.calls++
}

func (*serviceFakeAvatar) Pending(context.Context, int32) bool {
	return false
}

type serviceRecordingAudit struct {
	records []audit.Record
}

func (w *serviceRecordingAudit) Record(_ context.Context, record audit.Record) error {
	w.records = append(w.records, record)
	return nil
}

type failingRestoreStore struct {
	kv.Store
	failSetEx bool
}

func (s *failingRestoreStore) SetEx(ctx context.Context, key, value string, ttl time.Duration) error {
	if s.failSetEx {
		return errors.New("setex unavailable")
	}
	return s.Store.SetEx(ctx, key, value, ttl)
}

type setNXRecordingStore struct {
	kv.Store
	keys []string
}

func (s *setNXRecordingStore) SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error) {
	s.keys = append(s.keys, key)
	return s.Store.SetNX(ctx, key, value, ttl)
}

type serviceDefinition struct {
	ready bool
}

func (serviceDefinition) Protocol() string                      { return "fake" }
func (serviceDefinition) Descriptor() Descriptor                { return descriptor("fake") }
func (serviceDefinition) ValidateConfig(json.RawMessage) error  { return nil }
func (serviceDefinition) ValidateSecret([]byte) error            { return nil }
func (d serviceDefinition) Ready(Provider) bool                  { return d.ready }

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
	tests := []struct {
		name       string
		intent     Intent
		returnTo   string
		begin      func(*Service) (*BeginResult, error)
		accountID  *int32
		sessionID  string
		enrollment string
	}{
		{
			name: "login", intent: IntentLogin, returnTo: "/me",
			begin: func(service *Service) (*BeginResult, error) {
				return service.BeginLogin(context.Background(), "corp", "/me")
			},
		},
		{
			name: "link", intent: IntentLink, returnTo: "/identities",
			begin: func(service *Service) (*BeginResult, error) {
				return service.BeginLink(context.Background(), "corp", "/identities", 9, "session-1")
			},
			accountID: new(int32(9)), sessionID: "session-1",
		},
		{
			name: "invite", intent: IntentInvite, returnTo: "/welcome",
			begin: func(service *Service) (*BeginResult, error) {
				return service.BeginInvite(context.Background(), "invite-token", "/welcome")
			},
			enrollment: "invite-token",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, adapter, _, store := newServiceHarness(t)
			begin, err := test.begin(service)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := store.Get(context.Background(), FlowKey(begin.FlowID))
			if err != nil {
				t.Fatal(err)
			}
			state, err := DecodeFlowState(raw)
			if err != nil {
				t.Fatal(err)
			}
			if state.Intent != test.intent || state.ProviderID != 7 || state.ProviderSlug != "corp" ||
				state.Protocol != "fake" || state.ReturnTo != test.returnTo ||
				state.LinkSessionID != test.sessionID || state.EnrollmentToken != test.enrollment {
				t.Fatalf("stored bindings = %+v", state)
			}
			if test.accountID == nil && state.LinkAccountID != nil ||
				test.accountID != nil && (state.LinkAccountID == nil || *state.LinkAccountID != *test.accountID) {
				t.Fatalf("stored link account = %v, want %v", state.LinkAccountID, test.accountID)
			}
			if state.BrowserDigest == begin.BrowserToken || !BrowserBindingOK(state.BrowserDigest, begin.BrowserToken) {
				t.Fatal("browser token was not hashed and bound")
			}
			if string(state.AdapterState) != `{"step":1}` || state.CurrentAction.URL != begin.Action.URL {
				t.Fatalf("stored adapter projection = %+v", state)
			}
			if len(adapter.beginContexts) != 1 {
				t.Fatalf("adapter begin calls = %d", len(adapter.beginContexts))
			}
			ctx := adapter.beginContexts[0]
			if ctx.Intent != test.intent || ctx.ReturnTo != test.returnTo ||
				ctx.LinkSessionID != test.sessionID || ctx.EnrollmentToken != test.enrollment {
				t.Fatalf("adapter begin context = %+v", ctx)
			}
		})
	}
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
	base := AdvanceRequest{FlowID: begin.FlowID, BrowserToken: begin.BrowserToken, ProviderSlug: "corp", Protocol: "fake", CallbackRoute: CallbackRouteLink, AccountID: new(int32(9)), SessionID: "session-1", Input: ActionInput{Kind: ActionRedirect}}
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
	view, err := service.PrepareFlow(context.Background(), AdvanceRequest{FlowID: begin.FlowID, BrowserToken: begin.BrowserToken, ProviderSlug: "corp", Protocol: "fake", CallbackRoute: CallbackRoutePublic, Input: ActionInput{Kind: ActionRedirect}})
	if err != nil { t.Fatal(err) }
	if view.Action.Kind != ActionPublishProof || view.Action.Public["requiresLocalUsername"] != true { t.Fatalf("action = %+v", view.Action) }
	if resolver.calls != 0 { t.Fatal("non-terminal prepare resolved identity") }
}

func TestServiceCandidateUsernameRequirementMatrix(t *testing.T) {
	tests := []struct {
		name       string
		known      bool
		begin      func(*Service) (*BeginResult, error)
		request    func(*BeginResult) AdvanceRequest
		wantLookup bool
		wantPrompt bool
	}{
		{
			name: "known login", known: true,
			begin: func(service *Service) (*BeginResult, error) {
				return service.BeginLogin(context.Background(), "corp", "/")
			},
			request: func(begin *BeginResult) AdvanceRequest {
				return AdvanceRequest{
					FlowID: begin.FlowID, BrowserToken: begin.BrowserToken,
					ProviderSlug: "corp", Protocol: "fake", CallbackRoute: CallbackRoutePublic, Input: ActionInput{Kind: ActionRedirect},
				}
			},
			wantLookup: true,
		},
		{
			name: "invite", known: false,
			begin: func(service *Service) (*BeginResult, error) {
				return service.BeginInvite(context.Background(), "invite-token", "/")
			},
			request: func(begin *BeginResult) AdvanceRequest {
				return AdvanceRequest{
					FlowID: begin.FlowID, BrowserToken: begin.BrowserToken,
					ProviderSlug: "corp", Protocol: "fake", CallbackRoute: CallbackRoutePublic, Input: ActionInput{Kind: ActionRedirect},
				}
			},
		},
		{
			name: "link", known: false,
			begin: func(service *Service) (*BeginResult, error) {
				return service.BeginLink(context.Background(), "corp", "/", 9, "session-1")
			},
			request: func(begin *BeginResult) AdvanceRequest {
				return AdvanceRequest{
					FlowID: begin.FlowID, BrowserToken: begin.BrowserToken,
					ProviderSlug: "corp", Protocol: "fake", CallbackRoute: CallbackRouteLink, AccountID: new(int32(9)), SessionID: "session-1",
					Input: ActionInput{Kind: ActionRedirect},
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, adapter, resolver, _ := newServiceHarness(t)
			resolver.known = test.known
			adapter.advance = func(_ json.RawMessage, _ ActionInput) (AdvanceResult, error) {
				return AdvanceResult{
					State: json.RawMessage(`{"step":2}`),
					Next:  &NextAction{Kind: ActionPublishProof, Public: map[string]any{"proof": "abc"}},
					Candidate: &IdentityKey{Issuer: "iss", Subject: "candidate"},
				}, nil
			}
			begin, err := test.begin(service)
			if err != nil {
				t.Fatal(err)
			}
			view, err := service.PrepareFlow(context.Background(), test.request(begin))
			if err != nil {
				t.Fatal(err)
			}
			_, prompted := view.Action.Public["requiresLocalUsername"]
			if prompted != test.wantPrompt {
				t.Fatalf("requiresLocalUsername present = %v, want %v; action=%+v", prompted, test.wantPrompt, view.Action)
			}
			wantCalls := 0
			if test.wantLookup {
				wantCalls = 1
			}
			if resolver.knownCalls != wantCalls {
				t.Fatalf("identity lookup calls = %d, want %d", resolver.knownCalls, wantCalls)
			}
		})
	}
}

func TestServiceTerminalFailureRestoresAndCommitConsumes(t *testing.T) {
	service, adapter, resolver, store := newServiceHarness(t)
	adapter.advance = func(_ json.RawMessage, _ ActionInput) (AdvanceResult, error) {
		return AdvanceResult{}, errors.New("upstream temporary")
	}
	begin, err := service.BeginLogin(context.Background(), "corp", "/")
	if err != nil {
		t.Fatal(err)
	}
	key := FlowKey(begin.FlowID)
	before, err := store.Get(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	beforeTTL, err := store.TTL(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	request := AdvanceRequest{
		FlowID: begin.FlowID, BrowserToken: begin.BrowserToken,
		ProviderSlug: "corp", Protocol: "fake", CallbackRoute: CallbackRoutePublic, Input: ActionInput{Kind: ActionRedirect},
	}
	if _, err := service.VerifyFlow(context.Background(), request); err == nil {
		t.Fatal("adapter failure accepted")
	}
	after, err := store.Get(context.Background(), key)
	if err != nil {
		t.Fatalf("flow not restored: %v", err)
	}
	afterTTL, err := store.TTL(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	if after != before || afterTTL > beforeTTL {
		t.Fatalf("restored flow changed: rawEqual=%v ttlBefore=%d ttlAfter=%d", after == before, beforeTTL, afterTTL)
	}

	adapter.advance = func(_ json.RawMessage, _ ActionInput) (AdvanceResult, error) {
		return AdvanceResult{Identity: &VerifiedIdentity{Issuer: "iss", Subject: "sub", AMR: []string{"fake"}}}, nil
	}
	completion, err := service.VerifyFlow(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if completion.AccountID != 5 || completion.ProviderSlug != "corp" {
		t.Fatalf("completion = %+v", completion)
	}
	if resolver.calls != 1 {
		t.Fatalf("resolver calls = %d", resolver.calls)
	}
	if _, err := service.VerifyFlow(context.Background(), request); err == nil {
		t.Fatal("terminal flow replayed")
	}
}

func TestServicePrepareFailuresPreserveExactFlow(t *testing.T) {
	service, adapter, resolver, store := newServiceHarness(t)
	begin, err := service.BeginLogin(context.Background(), "corp", "/")
	if err != nil {
		t.Fatal(err)
	}
	key := FlowKey(begin.FlowID)
	before, err := store.Get(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	request := AdvanceRequest{
		FlowID: begin.FlowID, BrowserToken: begin.BrowserToken,
		ProviderSlug: "corp", Protocol: "fake", CallbackRoute: CallbackRoutePublic, Input: ActionInput{Kind: ActionRedirect},
	}

	adapter.advance = func(_ json.RawMessage, _ ActionInput) (AdvanceResult, error) {
		return AdvanceResult{}, errors.New("adapter failed")
	}
	if _, err := service.PrepareFlow(context.Background(), request); err == nil {
		t.Fatal("adapter failure accepted")
	}
	afterAdapter, err := store.Get(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	if afterAdapter != before {
		t.Fatal("adapter failure changed persisted flow")
	}

	adapter.advance = func(_ json.RawMessage, _ ActionInput) (AdvanceResult, error) {
		return AdvanceResult{
			State: json.RawMessage(`{"step":2}`),
			Next: &NextAction{Kind: ActionPublishProof, Public: map[string]any{"proof": "abc"}},
			Candidate: &IdentityKey{Issuer: "iss", Subject: "sub"},
		}, nil
	}
	resolver.knownErr = errors.New("identity lookup failed")
	if _, err := service.PrepareFlow(context.Background(), request); err == nil {
		t.Fatal("identity lookup failure accepted")
	}
	afterLookup, err := store.Get(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	if afterLookup != before {
		t.Fatal("identity lookup failure changed persisted flow")
	}
}

func TestServiceResolverFailureRestoresExactFlow(t *testing.T) {
	service, adapter, resolver, store := newServiceHarness(t)
	adapter.advance = func(_ json.RawMessage, _ ActionInput) (AdvanceResult, error) {
		return AdvanceResult{Identity: &VerifiedIdentity{Issuer: "iss", Subject: "sub"}}, nil
	}
	resolver.err = errors.New("transaction rolled back")
	begin, err := service.BeginLogin(context.Background(), "corp", "/")
	if err != nil {
		t.Fatal(err)
	}
	key := FlowKey(begin.FlowID)
	before, err := store.Get(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	request := AdvanceRequest{
		FlowID: begin.FlowID, BrowserToken: begin.BrowserToken,
		ProviderSlug: "corp", Protocol: "fake", CallbackRoute: CallbackRoutePublic, Input: ActionInput{Kind: ActionRedirect},
	}
	if _, err := service.VerifyFlow(context.Background(), request); err == nil {
		t.Fatal("resolver failure accepted")
	}
	after, err := store.Get(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatal("rolled-back resolver failure changed persisted flow")
	}
}

func TestServiceRestoreFailureReturnsKVUnavailableAndConsumesFlow(t *testing.T) {
	service, adapter, _, baseStore := newServiceHarness(t)
	adapter.advance = func(_ json.RawMessage, _ ActionInput) (AdvanceResult, error) {
		return AdvanceResult{}, errors.New("adapter failed")
	}
	begin, err := service.BeginLogin(context.Background(), "corp", "/")
	if err != nil {
		t.Fatal(err)
	}
	service.kv = &failingRestoreStore{Store: baseStore, failSetEx: true}
	request := AdvanceRequest{
		FlowID: begin.FlowID, BrowserToken: begin.BrowserToken,
		ProviderSlug: "corp", Protocol: "fake", CallbackRoute: CallbackRoutePublic, Input: ActionInput{Kind: ActionRedirect},
	}
	if _, err := service.VerifyFlow(context.Background(), request); !errors.Is(err, ErrKVUnavailable) {
		t.Fatalf("VerifyFlow error = %v, want kv unavailable", err)
	}
	if _, err := baseStore.Get(context.Background(), FlowKey(begin.FlowID)); !errors.Is(err, kv.ErrKeyNotFound) {
		t.Fatalf("flow survived failed restoration: %v", err)
	}
}

func TestServiceRejectsUnavailableProvidersBeforeAdapterCall(t *testing.T) {
	provider := Provider{ID: 7, Slug: "corp", Protocol: "fake", Mode: ModeAutoProvision}
	tests := []struct {
		name       string
		loader     fakeProviderLoader
		definition serviceDefinition
	}{
		{
			name: "unknown",
			loader: fakeProviderLoader{provider: provider, bySlugErr: ErrUnknownProvider},
			definition: serviceDefinition{ready: true},
		},
		{
			name: "disabled",
			loader: fakeProviderLoader{provider: func() Provider {
				disabled := provider
				disabled.Disabled = true
				return disabled
			}()},
			definition: serviceDefinition{ready: true},
		},
		{
			name: "unready",
			loader: fakeProviderLoader{provider: provider},
			definition: serviceDefinition{ready: false},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registry := NewRegistry()
			if err := registry.RegisterDefinition(test.definition); err != nil {
				t.Fatal(err)
			}
			adapter := &serviceFakeAdapter{
				beginState: json.RawMessage(`{"step":1}`),
				beginAction: NextAction{Kind: ActionRedirect, URL: "https://upstream.test"},
			}
			if err := registry.RegisterAdapter(adapter); err != nil {
				t.Fatal(err)
			}
			service := NewService(registry, test.loader, kv.NewMemoryStore(), &serviceFakeResolver{}, ServiceConfig{})
			if _, err := service.BeginLogin(context.Background(), "corp", "/"); err == nil {
				t.Fatal("unavailable provider began a flow")
			}
			if adapter.calls != 0 {
				t.Fatalf("adapter called %d times", adapter.calls)
			}
		})
	}
}

func TestServiceDisabledInviteOnlyProviderStaysOpaqueBeforeModeGate(t *testing.T) {
	service, adapter, _, _ := newServiceHarness(t)
	service.providers = fakeProviderLoader{provider: Provider{
		ID: 7, Slug: "corp", Protocol: "fake", Mode: ModeInviteOnly, Disabled: true,
	}}

	_, err := service.BeginLogin(context.Background(), "corp", "/")
	if !errors.Is(err, ErrUnknownProvider) {
		t.Fatalf("BeginLogin error = %v, want opaque ErrUnknownProvider", err)
	}
	if adapter.calls != 0 {
		t.Fatalf("disabled invite-only provider invoked adapter %d times", adapter.calls)
	}
}

func TestServiceKnownAtPrepareBecomingUnknownRestoresUsernamePromptOnly(t *testing.T) {
	service, adapter, resolver, store := newServiceHarness(t)
	resolver.known = true
	adapter.advance = func(_ json.RawMessage, input ActionInput) (AdvanceResult, error) {
		if input.Kind == ActionRedirect {
			return AdvanceResult{
				State: json.RawMessage(`{"proof":"private"}`),
				Next: &NextAction{Kind: ActionPublishProof, Public: map[string]any{"challenge": "public"}},
				Candidate: &IdentityKey{Issuer: "iss", Subject: "sub"},
			}, nil
		}
		return AdvanceResult{Identity: &VerifiedIdentity{Issuer: "iss", Subject: "sub"}}, nil
	}
	begin, err := service.BeginLogin(context.Background(), "corp", "/")
	if err != nil {
		t.Fatal(err)
	}
	baseRequest := AdvanceRequest{
		FlowID: begin.FlowID, BrowserToken: begin.BrowserToken,
		ProviderSlug: "corp", Protocol: "fake", CallbackRoute: CallbackRoutePublic, Input: ActionInput{Kind: ActionRedirect},
	}
	view, err := service.PrepareFlow(context.Background(), baseRequest)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := view.Action.Public["requiresLocalUsername"]; exists {
		t.Fatalf("known identity prompted before race: %+v", view.Action)
	}
	key := FlowKey(begin.FlowID)
	before, err := store.Get(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	beforeState, err := DecodeFlowState(before)
	if err != nil {
		t.Fatal(err)
	}

	resolver.err = ErrLocalUsernameRequired
	baseRequest.Input.Kind = ActionPublishProof
	if _, err := service.VerifyFlow(context.Background(), baseRequest); !errors.Is(err, ErrLocalUsernameRequired) {
		t.Fatalf("error = %v, want local username required", err)
	}
	if len(resolver.resolveContexts) != 1 || !resolver.resolveContexts[0].RequireLocalUsername {
		t.Fatalf("resolver context did not preserve prepared local-username contract: %+v", resolver.resolveContexts)
	}
	after, err := store.Get(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	afterState, err := DecodeFlowState(after)
	if err != nil {
		t.Fatal(err)
	}
	if string(beforeState.AdapterState) != string(afterState.AdapterState) ||
		beforeState.ProviderID != afterState.ProviderID ||
		beforeState.ProviderSlug != afterState.ProviderSlug ||
		beforeState.Protocol != afterState.Protocol ||
		beforeState.Intent != afterState.Intent ||
		beforeState.ReturnTo != afterState.ReturnTo ||
		beforeState.BrowserDigest != afterState.BrowserDigest ||
		!beforeState.ExpiresAt.Equal(afterState.ExpiresAt) ||
		beforeState.CurrentAction.Kind != afterState.CurrentAction.Kind ||
		beforeState.CurrentAction.Public["challenge"] != afterState.CurrentAction.Public["challenge"] ||
		afterState.CurrentAction.Public["requiresLocalUsername"] != true {
		t.Fatalf("restored state changed beyond username prompt:\nbefore=%+v\nafter=%+v", beforeState, afterState)
	}
}

func TestServiceLinkCompletionDoesNotInheritAvatar(t *testing.T) {
	service, adapter, _, _ := newServiceHarness(t)
	avatars := &serviceFakeAvatar{}
	service.SetAvatarManager(avatars)
	adapter.advance = func(_ json.RawMessage, _ ActionInput) (AdvanceResult, error) {
		return AdvanceResult{Identity: &VerifiedIdentity{
			Issuer: "iss", Subject: "sub", AvatarURL: "https://cdn.test/avatar.png",
		}}, nil
	}
	begin, err := service.BeginLink(context.Background(), "corp", "/identities", 9, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.VerifyFlow(context.Background(), AdvanceRequest{
		FlowID: begin.FlowID, BrowserToken: begin.BrowserToken,
		ProviderSlug: "corp", Protocol: "fake", CallbackRoute: CallbackRouteLink,
		AccountID: new(int32(9)), SessionID: "session-1",
		Input: ActionInput{Kind: ActionRedirect},
	})
	if err != nil {
		t.Fatal(err)
	}
	if avatars.calls != 0 {
		t.Fatalf("link completion inherited avatar %d times", avatars.calls)
	}
}

func TestServiceAuditsBrowserAndProviderFailures(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*Service, *BeginResult)
		browser    func(*BeginResult) string
		wantReason FailureReason
	}{
		{
			name: "browser binding",
			mutate: func(*Service, *BeginResult) {},
			browser: func(*BeginResult) string { return "wrong-browser" },
			wantReason: FailureBrowserBindingMismatch,
		},
		{
			name: "provider disabled after begin",
			mutate: func(service *Service, _ *BeginResult) {
				service.providers = fakeProviderLoader{provider: Provider{
					ID: 7, Slug: "corp", Protocol: "fake", Mode: ModeAutoProvision, Disabled: true,
				}}
			},
			browser: func(begin *BeginResult) string { return begin.BrowserToken },
			wantReason: FailureProviderUnavailable,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, _, _, _ := newServiceHarness(t)
			writer := &serviceRecordingAudit{}
			service.audit = writer
			begin, err := service.BeginLogin(context.Background(), "corp", "/")
			if err != nil {
				t.Fatal(err)
			}
			test.mutate(service, begin)

			_, err = service.VerifyFlow(context.Background(), AdvanceRequest{
				FlowID: begin.FlowID, BrowserToken: test.browser(begin),
				ProviderSlug: "corp", Protocol: "fake", CallbackRoute: CallbackRoutePublic, Input: ActionInput{Kind: ActionRedirect},
			})
			if ae := authn.AsAuthError(err); ae == nil || ae.Code != "federation_state_invalid" {
				t.Fatalf("public error = %v, want federation_state_invalid", err)
			}
			if len(writer.records) != 1 || writer.records[0].Detail["reason"] != string(test.wantReason) {
				t.Fatalf("audit records = %+v, want reason %q", writer.records, test.wantReason)
			}
			if writer.records[0].AccountID != nil {
				t.Fatalf("login failure attached account: %+v", writer.records[0])
			}
		})
	}
}

func TestServiceAuditsAllowlistedAdapterFailuresForEveryIntent(t *testing.T) {
	tests := []struct {
		name  string
		begin func(*Service) (*BeginResult, error)
		request func(*BeginResult) AdvanceRequest
		wantAccountID *int32
	}{
		{
			name: "login",
			begin: func(service *Service) (*BeginResult, error) {
				return service.BeginLogin(context.Background(), "corp", "/")
			},
			request: func(begin *BeginResult) AdvanceRequest {
				return AdvanceRequest{FlowID: begin.FlowID, BrowserToken: begin.BrowserToken, ProviderSlug: "corp", Protocol: "fake", CallbackRoute: CallbackRoutePublic, Input: ActionInput{Kind: ActionRedirect}}
			},
		},
		{
			name: "invite",
			begin: func(service *Service) (*BeginResult, error) {
				return service.BeginInvite(context.Background(), "invite-token", "/")
			},
			request: func(begin *BeginResult) AdvanceRequest {
				return AdvanceRequest{FlowID: begin.FlowID, BrowserToken: begin.BrowserToken, ProviderSlug: "corp", Protocol: "fake", CallbackRoute: CallbackRoutePublic, Input: ActionInput{Kind: ActionRedirect}}
			},
		},
		{
			name: "link",
			begin: func(service *Service) (*BeginResult, error) {
				return service.BeginLink(context.Background(), "corp", "/", 9, "session-1")
			},
			request: func(begin *BeginResult) AdvanceRequest {
				return AdvanceRequest{
					FlowID: begin.FlowID, BrowserToken: begin.BrowserToken, ProviderSlug: "corp", Protocol: "fake", CallbackRoute: CallbackRouteLink,
					AccountID: new(int32(9)), SessionID: "session-1", Input: ActionInput{Kind: ActionRedirect},
				}
			},
			wantAccountID: new(int32(9)),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, adapter, _, _ := newServiceHarness(t)
			writer := &serviceRecordingAudit{}
			service.audit = writer
			adapter.advance = func(json.RawMessage, ActionInput) (AdvanceResult, error) {
				return AdvanceResult{}, NewFailure(FailureTokenEndpointDrift, map[string]any{
					"expected": "https://issuer.test/token",
					"got":      "https://other.test/token",
					"secret":   "must-not-be-audited",
				})
			}
			begin, err := test.begin(service)
			if err != nil {
				t.Fatal(err)
			}

			_, err = service.VerifyFlow(context.Background(), test.request(begin))
			if ae := authn.AsAuthError(err); ae == nil || ae.Code != "federation_state_invalid" {
				t.Fatalf("public error = %v, want federation_state_invalid", err)
			}
			if len(writer.records) != 1 {
				t.Fatalf("audit records = %+v", writer.records)
			}
			record := writer.records[0]
			if record.Detail["reason"] != string(FailureTokenEndpointDrift) ||
				record.Detail["idp_slug"] != "corp" ||
				record.Detail["expected"] != "https://issuer.test/token" ||
				record.Detail["got"] != "https://other.test/token" {
				t.Fatalf("audit detail = %+v", record.Detail)
			}
			if _, leaked := record.Detail["secret"]; leaked {
				t.Fatalf("audit detail leaked non-allowlisted field: %+v", record.Detail)
			}
			if test.wantAccountID == nil && record.AccountID != nil ||
				test.wantAccountID != nil && (record.AccountID == nil || *record.AccountID != *test.wantAccountID) {
				t.Fatalf("audit account = %v, want %v", record.AccountID, test.wantAccountID)
			}
		})
	}
}

func TestServiceAuditsInviteStartFailureWithoutLeakingDetails(t *testing.T) {
	service, adapter, _, _ := newServiceHarness(t)
	service.providers = fakeProviderLoader{inviteErr: NewFailure(FailureInviteExpired, map[string]any{
		"token": "must-not-be-audited",
	})}
	writer := &serviceRecordingAudit{}
	service.audit = writer

	_, err := service.BeginInvite(context.Background(), "secret-invite", "/")
	if ae := authn.AsAuthError(err); ae == nil || ae.Code != "invite_required" {
		t.Fatalf("public error = %v, want invite_required", err)
	}
	if adapter.calls != 0 {
		t.Fatalf("invalid invite invoked adapter %d times", adapter.calls)
	}
	if len(writer.records) != 1 || writer.records[0].Detail["reason"] != string(FailureInviteExpired) {
		t.Fatalf("audit records = %+v", writer.records)
	}
	if _, leaked := writer.records[0].Detail["token"]; leaked {
		t.Fatalf("audit leaked invite token: %+v", writer.records[0].Detail)
	}
}

func TestServiceAuditsLinkResolverFailureClassification(t *testing.T) {
	service, adapter, resolver, _ := newServiceHarness(t)
	writer := &serviceRecordingAudit{}
	service.audit = writer
	adapter.advance = func(json.RawMessage, ActionInput) (AdvanceResult, error) {
		return AdvanceResult{Identity: &VerifiedIdentity{
			Issuer: "https://issuer.test", Subject: "sub", EmailVerificationSupported: true,
		}}, nil
	}
	resolver.err = NewFailure(FailureEmailNotVerified, map[string]any{
		"upstream_iss": "https://issuer.test",
		"email":        "must-not-be-audited@example.test",
	})
	begin, err := service.BeginLink(context.Background(), "corp", "/", 9, "session-1")
	if err != nil {
		t.Fatal(err)
	}

	_, err = service.VerifyFlow(context.Background(), AdvanceRequest{
		FlowID: begin.FlowID, BrowserToken: begin.BrowserToken, ProviderSlug: "corp", Protocol: "fake", CallbackRoute: CallbackRouteLink,
		AccountID: new(int32(9)), SessionID: "session-1", Input: ActionInput{Kind: ActionRedirect},
	})
	if ae := authn.AsAuthError(err); ae == nil || ae.Code != "email_not_verified" {
		t.Fatalf("public error = %v, want email_not_verified", err)
	}
	if len(writer.records) != 1 {
		t.Fatalf("audit records = %+v", writer.records)
	}
	record := writer.records[0]
	if record.AccountID == nil || *record.AccountID != 9 ||
		record.Detail["reason"] != string(FailureEmailNotVerified) ||
		record.Detail["upstream_iss"] != "https://issuer.test" {
		t.Fatalf("audit record = %+v", record)
	}
	if _, leaked := record.Detail["email"]; leaked {
		t.Fatalf("audit leaked email: %+v", record.Detail)
	}
}

func TestServiceEnforcesCallbackRouteIntent(t *testing.T) {
	tests := []struct {
		name          string
		begin         func(*Service) (*BeginResult, error)
		route         CallbackRoute
		accountID     *int32
		sessionID     string
		wantAccountID *int32
	}{
		{
			name: "login state on link route",
			begin: func(service *Service) (*BeginResult, error) {
				return service.BeginLogin(context.Background(), "corp", "/")
			},
			route: CallbackRouteLink, accountID: new(int32(9)), sessionID: "session-1",
			wantAccountID: new(int32(9)),
		},
		{
			name: "invite state on link route",
			begin: func(service *Service) (*BeginResult, error) {
				return service.BeginInvite(context.Background(), "invite-token", "/")
			},
			route: CallbackRouteLink, accountID: new(int32(9)), sessionID: "session-1",
			wantAccountID: new(int32(9)),
		},
		{
			name: "link state on public route",
			begin: func(service *Service) (*BeginResult, error) {
				return service.BeginLink(context.Background(), "corp", "/", 9, "session-1")
			},
			route: CallbackRoutePublic,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, adapter, resolver, _ := newServiceHarness(t)
			writer := &serviceRecordingAudit{}
			service.audit = writer
			begin, err := test.begin(service)
			if err != nil {
				t.Fatal(err)
			}
			beginCalls := adapter.calls

			_, err = service.VerifyFlow(context.Background(), AdvanceRequest{
				FlowID: begin.FlowID, BrowserToken: begin.BrowserToken,
				ProviderSlug: "corp", Protocol: "fake", CallbackRoute: test.route,
				AccountID: test.accountID, SessionID: test.sessionID,
				Input: ActionInput{Kind: ActionRedirect},
			})
			if ae := authn.AsAuthError(err); ae == nil || ae.Code != "federation_state_invalid" {
				t.Fatalf("public error = %v, want federation_state_invalid", err)
			}
			if adapter.calls != beginCalls || resolver.calls != 0 {
				t.Fatalf("route mismatch reached adapter/resolver: adapter=%d resolver=%d", adapter.calls, resolver.calls)
			}
			if len(writer.records) != 1 || writer.records[0].Detail["reason"] != string(FailureStateInvalid) {
				t.Fatalf("audit records = %+v", writer.records)
			}
			record := writer.records[0]
			if test.wantAccountID == nil && record.AccountID != nil ||
				test.wantAccountID != nil && (record.AccountID == nil || *record.AccountID != *test.wantAccountID) {
				t.Fatalf("audit account = %v, want %v", record.AccountID, test.wantAccountID)
			}
		})
	}
}

func TestServicePublicCallbackAcceptsLoginAndInvite(t *testing.T) {
	tests := []struct {
		name  string
		begin func(*Service) (*BeginResult, error)
	}{
		{
			name: "login",
			begin: func(service *Service) (*BeginResult, error) {
				return service.BeginLogin(context.Background(), "corp", "/")
			},
		},
		{
			name: "invite",
			begin: func(service *Service) (*BeginResult, error) {
				return service.BeginInvite(context.Background(), "invite-token", "/")
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, adapter, _, _ := newServiceHarness(t)
			adapter.advance = func(json.RawMessage, ActionInput) (AdvanceResult, error) {
				return AdvanceResult{Identity: &VerifiedIdentity{Issuer: "iss", Subject: "sub"}}, nil
			}
			begin, err := test.begin(service)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.VerifyFlow(context.Background(), AdvanceRequest{
				FlowID: begin.FlowID, BrowserToken: begin.BrowserToken,
				ProviderSlug: "corp", Protocol: "fake", CallbackRoute: CallbackRoutePublic,
				Input: ActionInput{Kind: ActionRedirect},
			}); err != nil {
				t.Fatalf("public callback rejected %s state: %v", test.name, err)
			}
		})
	}
}

func TestServiceAttributesInvalidLinkStateToRequestAccount(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*Service, kv.Store) AdvanceRequest
	}{
		{
			name: "missing",
			setup: func(_ *Service, _ kv.Store) AdvanceRequest {
				return AdvanceRequest{FlowID: "missing-flow"}
			},
		},
		{
			name: "malformed",
			setup: func(_ *Service, store kv.Store) AdvanceRequest {
				if err := store.SetEx(context.Background(), FlowKey("malformed-flow"), "database-secret", time.Minute); err != nil {
					t.Fatal(err)
				}
				return AdvanceRequest{FlowID: "malformed-flow"}
			},
		},
		{
			name: "replayed",
			setup: func(service *Service, _ kv.Store) AdvanceRequest {
				begin, err := service.BeginLink(context.Background(), "corp", "/", 9, "session-1")
				if err != nil {
					t.Fatal(err)
				}
				request := AdvanceRequest{
					FlowID: begin.FlowID, BrowserToken: begin.BrowserToken, ProviderSlug: "corp", Protocol: "fake",
					CallbackRoute: CallbackRouteLink, AccountID: new(int32(9)), SessionID: "session-1",
					Input: ActionInput{Kind: ActionRedirect},
				}
				if _, err := service.VerifyFlow(context.Background(), request); err != nil {
					t.Fatal(err)
				}
				return request
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, adapter, _, store := newServiceHarness(t)
			adapter.advance = func(json.RawMessage, ActionInput) (AdvanceResult, error) {
				return AdvanceResult{Identity: &VerifiedIdentity{Issuer: "iss", Subject: "sub"}}, nil
			}
			request := test.setup(service, store)
			request.CallbackRoute = CallbackRouteLink
			request.AccountID = new(int32(9))
			request.SessionID = "session-1"
			request.Input.Kind = ActionRedirect
			writer := &serviceRecordingAudit{}
			service.audit = writer

			_, err := service.VerifyFlow(context.Background(), request)
			if ae := authn.AsAuthError(err); ae == nil || ae.Code != "federation_state_invalid" {
				t.Fatalf("public error = %v, want federation_state_invalid", err)
			}
			if len(writer.records) != 1 {
				t.Fatalf("audit records = %+v", writer.records)
			}
			record := writer.records[0]
			if record.AccountID == nil || *record.AccountID != 9 ||
				record.Detail["reason"] != string(FailureStateInvalid) {
				t.Fatalf("audit record = %+v", record)
			}
			if len(record.Detail) != 1 {
				t.Fatalf("invalid state audit leaked detail: %+v", record.Detail)
			}
		})
	}
}

func TestServiceRejectsInvalidOrMissingFlowBeforeLeaseWrite(t *testing.T) {
	const (
		missingCanonicalFlowID = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
		oversizedFlowID        = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	)
	for _, flowID := range []string{missingCanonicalFlowID, oversizedFlowID} {
		t.Run(flowID, func(t *testing.T) {
			service, _, _, baseStore := newServiceHarness(t)
			store := &setNXRecordingStore{Store: baseStore}
			service.kv = store
			request := AdvanceRequest{
				FlowID: flowID, CallbackRoute: CallbackRoutePublic,
				ProviderSlug: "corp", Protocol: "fake", Input: ActionInput{Kind: ActionRedirect},
			}

			if _, err := service.PrepareFlow(context.Background(), request); err == nil {
				t.Fatal("PrepareFlow accepted invalid or missing flow")
			}
			if _, err := service.VerifyFlow(context.Background(), request); err == nil {
				t.Fatal("VerifyFlow accepted invalid or missing flow")
			}
			if len(store.keys) != 0 {
				t.Fatalf("lease writes = %v, want none", store.keys)
			}
		})
	}
}

func TestServiceLeaseReleaseCannotDeleteNewOwner(t *testing.T) {
	const flowID = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	service, _, _, store := newServiceHarness(t)
	oldLease, err := service.lock(context.Background(), flowID)
	if err != nil {
		t.Fatal(err)
	}
	lockKey := FlowLockKey(flowID)
	oldOwner, err := store.Get(context.Background(), lockKey)
	if err != nil {
		t.Fatal(err)
	}
	if oldOwner == "" || oldOwner == "1" {
		t.Fatalf("lease owner = %q, want unique owner token", oldOwner)
	}
	if err := store.Del(context.Background(), lockKey); err != nil {
		t.Fatal(err)
	}
	locked, err := store.SetNX(context.Background(), lockKey, "new-owner", time.Minute)
	if err != nil || !locked {
		t.Fatalf("replacement lease = (%v, %v), want acquired", locked, err)
	}

	oldLease.release()

	if got, err := store.Get(context.Background(), lockKey); err != nil || got != "new-owner" {
		t.Fatalf("replacement lease after stale release = (%q, %v), want new-owner", got, err)
	}
}

func TestServiceRenewsLeaseDuringLongAdapterOperation(t *testing.T) {
	service, adapter, _, store := newServiceHarness(t)
	service.config.LockTTL = 30 * time.Millisecond
	entered := make(chan struct{})
	proceed := make(chan struct{})
	adapter.advanceContext = func(ctx context.Context, _ json.RawMessage, _ ActionInput) (AdvanceResult, error) {
		close(entered)
		select {
		case <-proceed:
			return AdvanceResult{Identity: &VerifiedIdentity{Issuer: "iss", Subject: "sub"}}, nil
		case <-ctx.Done():
			return AdvanceResult{}, ctx.Err()
		}
	}
	begin, err := service.BeginLogin(context.Background(), "corp", "/")
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, verifyErr := service.VerifyFlow(context.Background(), AdvanceRequest{
			FlowID: begin.FlowID, BrowserToken: begin.BrowserToken,
			ProviderSlug: "corp", Protocol: "fake", CallbackRoute: CallbackRoutePublic,
			Input: ActionInput{Kind: ActionRedirect},
		})
		result <- verifyErr
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("adapter did not start")
	}
	time.Sleep(75 * time.Millisecond)
	competing, competingErr := store.SetNX(context.Background(), FlowLockKey(begin.FlowID), "competing-owner", time.Minute)
	close(proceed)
	select {
	case verifyErr := <-result:
		if verifyErr != nil {
			t.Fatalf("VerifyFlow after long operation: %v", verifyErr)
		}
	case <-time.After(time.Second):
		t.Fatal("VerifyFlow did not stop")
	}
	if competingErr != nil {
		t.Fatal(competingErr)
	}
	if competing {
		t.Fatal("lease expired while adapter operation was active")
	}
	if _, err := store.Get(context.Background(), FlowLockKey(begin.FlowID)); !errors.Is(err, kv.ErrKeyNotFound) {
		t.Fatalf("lease survived operation: %v", err)
	}
}
