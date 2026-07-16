package federation

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/kv"
)

const maxLeaseCleanupTimeout = 5 * time.Second

var (
	ErrKVUnavailable   = errors.New("federation: kv unavailable")
	ErrProviderUnready = errors.New("federation: provider unready")
)

type ProviderLoader interface {
	BySlug(context.Context, string) (Provider, error)
	ByBinding(context.Context, int64, string, string) (Provider, error)
	InviteProvider(context.Context, string) (Provider, error)
}

type ServiceConfig struct {
	StateTTL     time.Duration
	LockTTL      time.Duration
	PublicOrigin string
	Audit        audit.Writer
}

type BeginResult struct {
	FlowID       string
	BrowserToken string
	Action       NextAction
}

type FlowView struct {
	FlowID       string
	Intent       Intent
	ProviderSlug string
	Protocol     string
	Action       NextAction
}

type AdvanceRequest struct {
	FlowID        string
	BrowserToken  string
	ProviderSlug  string
	Protocol      string
	CallbackRoute CallbackRoute
	AccountID     *int32
	SessionID     string
	Input         ActionInput
}

type avatarInheritor interface {
	Inherit(int32, Provider, AvatarDelivery, AvatarResolver)
	Pending(context.Context, int32) bool
}

type Service struct {
	registry  *Registry
	providers ProviderLoader
	kv        kv.Store
	resolver  IdentityResolver
	avatar    avatarInheritor
	audit     audit.Writer
	config    ServiceConfig
	now       func() time.Time
}

type flowLease struct {
	ctx               context.Context
	cancel            context.CancelFunc
	maintenanceCtx    context.Context
	cancelMaintenance context.CancelFunc
	store             kv.Store
	key               string
	owner             string
	ttl               time.Duration
	stop              chan struct{}
	done              chan struct{}
	lost              chan struct{}
	releaseOnce       sync.Once
}

func NewService(registry *Registry, providers ProviderLoader, store kv.Store, resolver IdentityResolver, config ServiceConfig) *Service {
	if config.StateTTL <= 0 {
		config.StateTTL = 10 * time.Minute
	}
	if config.LockTTL <= 0 {
		config.LockTTL = 30 * time.Second
	}
	return &Service{registry: registry, providers: providers, kv: store, resolver: resolver, audit: config.Audit, config: config, now: time.Now}
}

func (s *Service) SetAvatarManager(manager avatarInheritor) {
	s.avatar = manager
}

func (s *Service) BeginLogin(ctx context.Context, providerSlug, returnTo string) (*BeginResult, error) {
	provider, err := s.providers.BySlug(ctx, providerSlug)
	if err != nil {
		return nil, err
	}
	if provider.Disabled {
		return nil, ErrUnknownProvider
	}
	if provider.Mode == ModeInviteOnly {
		return nil, s.recordFailure(ctx, nil, nil, provider.Slug, NewFailure(FailureInviteRequiredPreAuth, nil))
	}
	return s.begin(ctx, provider, IntentLogin, returnTo, nil, "", "")
}

func (s *Service) BeginLink(ctx context.Context, providerSlug, returnTo string, accountID int32, sessionID string) (*BeginResult, error) {
	if accountID <= 0 || sessionID == "" {
		return nil, authn.ErrFederationStateInvalid()
	}
	return s.beginBySlug(ctx, providerSlug, IntentLink, returnTo, new(accountID), sessionID, "")
}

func (s *Service) BeginInvite(ctx context.Context, enrollmentToken, returnTo string) (*BeginResult, error) {
	provider, err := s.providers.InviteProvider(ctx, enrollmentToken)
	if err != nil {
		if _, _, _, ok := failureProjection(err); ok {
			return nil, s.recordFailure(ctx, nil, nil, "", err)
		}
		return nil, authn.ErrInviteRequired()
	}
	return s.begin(ctx, provider, IntentInvite, returnTo, nil, "", enrollmentToken)
}

func (s *Service) beginBySlug(ctx context.Context, providerSlug string, intent Intent, returnTo string, accountID *int32, sessionID, enrollmentToken string) (*BeginResult, error) {
	provider, err := s.providers.BySlug(ctx, providerSlug)
	if err != nil {
		return nil, err
	}
	return s.begin(ctx, provider, intent, returnTo, accountID, sessionID, enrollmentToken)
}

func (s *Service) begin(ctx context.Context, provider Provider, intent Intent, returnTo string, accountID *int32, sessionID, enrollmentToken string) (*BeginResult, error) {
	definition, adapter, err := s.flowProvider(provider)
	if err != nil {
		return nil, err
	}
	if !definition.Ready(provider) {
		return nil, ErrProviderUnready
	}
	flowID, err := randomToken()
	if err != nil {
		return nil, err
	}
	browserToken, err := randomToken()
	if err != nil {
		return nil, err
	}
	callbackURL := s.callbackURL(provider.Slug, intent)
	adapterState, action, err := adapter.Begin(ctx, provider, BeginContext{
		Intent: intent, FlowID: flowID, CallbackURL: callbackURL, ReturnTo: returnTo,
		LinkAccountID: accountID, LinkSessionID: sessionID, EnrollmentToken: enrollmentToken,
	})
	if err != nil {
		return nil, err
	}
	if err := validateAdapterState(adapterState); err != nil {
		return nil, err
	}
	if err := validateAdapterAction(action); err != nil {
		return nil, err
	}
	expiresAt := s.now().Add(s.config.StateTTL).UTC()
	state := FlowState{
		ProviderID: provider.ID, ProviderSlug: provider.Slug, Protocol: provider.Protocol,
		Intent: intent, ReturnTo: returnTo, BrowserDigest: BrowserDigest(browserToken),
		LinkAccountID: accountID, LinkSessionID: sessionID, EnrollmentToken: enrollmentToken,
		ExpiresAt: expiresAt, AdapterState: append(json.RawMessage(nil), adapterState...), CurrentAction: cloneAction(action),
	}
	raw, err := state.Encode()
	if err != nil {
		return nil, err
	}
	if err := s.kv.SetEx(ctx, FlowKey(flowID), raw, s.config.StateTTL); err != nil {
		return nil, fmt.Errorf("%w: stash flow: %v", ErrKVUnavailable, err)
	}
	return &BeginResult{FlowID: flowID, BrowserToken: browserToken, Action: cloneAction(action)}, nil
}

func (s *Service) ReadFlow(ctx context.Context, flowID, browserToken string) (*FlowView, error) {
	if !validFlowID(flowID) {
		return nil, authn.ErrFederationStateInvalid()
	}
	raw, err := s.kv.Get(ctx, FlowKey(flowID))
	if err != nil {
		return nil, authn.ErrFederationStateInvalid()
	}
	state, err := DecodeFlowState(raw)
	if err != nil || !state.ExpiresAt.After(s.now()) || !BrowserBindingOK(state.BrowserDigest, browserToken) {
		return nil, authn.ErrFederationStateInvalid()
	}
	return flowView(flowID, state), nil
}

func (s *Service) AdvanceCallback(ctx context.Context, request AdvanceRequest) (*CompletionResult, error) {
	return s.VerifyFlow(ctx, request)
}

func (s *Service) PrepareFlow(ctx context.Context, request AdvanceRequest) (*FlowView, error) {
	if !validFlowID(request.FlowID) || !validCallbackRoute(request.CallbackRoute) {
		return nil, s.recordFailure(ctx, nil, &request, "", NewFailure(FailureStateInvalid, nil))
	}
	if _, err := s.kv.Get(ctx, FlowKey(request.FlowID)); err != nil {
		return nil, s.recordFailure(ctx, nil, &request, "", NewFailure(FailureStateInvalid, nil))
	}
	lease, err := s.lock(ctx, request.FlowID)
	if err != nil {
		return nil, err
	}
	defer lease.release()
	operationCtx := lease.ctx

	raw, state, provider, adapter, err := s.loadForAdvance(operationCtx, request)
	if err != nil {
		return nil, err
	}
	result, err := adapter.Advance(operationCtx, provider, append(json.RawMessage(nil), state.AdapterState...), request.Input)
	if leaseErr := lease.check(); leaseErr != nil {
		return nil, leaseErr
	}
	if err != nil {
		return nil, s.recordFailure(ctx, state, &request, provider.Slug, err)
	}
	if result.Identity != nil || result.Next == nil || result.Avatar != nil ||
		result.Candidate != nil && (result.Candidate.Issuer == "" || result.Candidate.Subject == "") {
		return nil, s.recordFailure(ctx, state, &request, provider.Slug, NewFailure(FailureStateInvalid, nil))
	}
	if err := validateAdapterState(result.State); err != nil {
		return nil, err
	}
	if err := validateAdapterAction(*result.Next); err != nil {
		return nil, err
	}
	next := cloneAction(*result.Next)
	if result.Candidate != nil && state.Intent == IntentLogin && provider.Mode == ModeAutoProvision {
		known, lookupErr := s.resolver.IdentityKnown(operationCtx, *result.Candidate)
		if leaseErr := lease.check(); leaseErr != nil {
			return nil, leaseErr
		}
		if lookupErr != nil {
			return nil, lookupErr
		}
		if !known {
			if next.Public == nil {
				next.Public = make(map[string]any)
			}
			next.Public["requiresLocalUsername"] = true
		}
	}
	state.AdapterState = append(json.RawMessage(nil), result.State...)
	state.CurrentAction = next
	updated, err := state.Encode()
	if err != nil {
		return nil, err
	}
	remaining := state.ExpiresAt.Sub(s.now())
	if remaining <= 0 {
		return nil, authn.ErrFederationStateInvalid()
	}
	swapped, err := s.kv.FencedCompareAndSwap(operationCtx, lease.key, lease.owner, FlowKey(request.FlowID), raw, updated, remaining)
	if err != nil {
		return nil, fmt.Errorf("%w: advance flow: %v", ErrKVUnavailable, err)
	}
	if !swapped {
		return nil, authn.ErrFederationStateInvalid()
	}
	if state.Protocol == "vrchat" {
		s.recordVRChatTransition(ctx, "vrchat_proof_prepared", state, provider.Slug, "")
	}
	return flowView(request.FlowID, state), nil
}

func (s *Service) VerifyFlow(ctx context.Context, request AdvanceRequest) (*CompletionResult, error) {
	if !validFlowID(request.FlowID) || !validCallbackRoute(request.CallbackRoute) {
		return nil, s.recordFailure(ctx, nil, &request, "", NewFailure(FailureStateInvalid, nil))
	}
	if _, err := s.kv.Get(ctx, FlowKey(request.FlowID)); err != nil {
		return nil, s.recordFailure(ctx, nil, &request, "", NewFailure(FailureStateInvalid, nil))
	}
	lease, err := s.lock(ctx, request.FlowID)
	if err != nil {
		return nil, err
	}
	defer lease.release()
	operationCtx := lease.ctx

	raw, state, provider, adapter, err := s.loadForAdvance(operationCtx, request)
	if err != nil {
		return nil, err
	}
	consumed, err := s.kv.FencedCompareAndDelete(operationCtx, lease.key, lease.owner, FlowKey(request.FlowID), raw)
	if err != nil {
		return nil, s.recordFailure(operationCtx, state, &request, provider.Slug, fmt.Errorf("%w: consume flow", ErrKVUnavailable))
	}
	if !consumed {
		return nil, s.recordFailure(operationCtx, state, &request, provider.Slug, NewFailure(FailureStateInvalid, nil))
	}

	result, err := adapter.Advance(operationCtx, provider, append(json.RawMessage(nil), state.AdapterState...), request.Input)
	if err != nil {
		return nil, s.restoreAfterFailure(operationCtx, request, provider.Slug, state, raw, err, false)
	}
	if leaseErr := lease.check(); leaseErr != nil {
		return nil, s.restoreAfterFailure(operationCtx, request, provider.Slug, state, raw, leaseErr, false)
	}
	if result.Identity == nil || result.Next != nil || len(result.State) != 0 || result.Candidate != nil {
		return nil, s.restoreAfterFailure(operationCtx, request, provider.Slug, state, raw, NewFailure(FailureStateInvalid, nil), false)
	}
	requireLocalUsername := state.Intent == IntentLogin && state.CurrentAction.Kind != ActionRedirect
	outcome, err := s.resolver.ResolveIdentity(operationCtx, provider, *result.Identity, ResolveContext{
		Intent: state.Intent, EnrollmentToken: state.EnrollmentToken, LocalUsername: request.Input.LocalUsername,
		LinkAccountID:        state.LinkAccountID,
		RequireLocalUsername: requireLocalUsername,
	})
	if err != nil {
		return nil, s.restoreAfterFailure(operationCtx, request, provider.Slug, state, raw, err, requireLocalUsername && errors.Is(err, ErrLocalUsernameRequired))
	}
	if state.Protocol == "vrchat" {
		s.recordVRChatTransition(operationCtx, "vrchat_proof_verified", state, provider.Slug, "")
	}
	completion := &CompletionResult{
		Intent: state.Intent, AccountID: outcome.AccountID, IdentityID: outcome.IdentityID,
		ProviderID: provider.ID, ProviderSlug: provider.Slug, ReturnTo: state.ReturnTo,
		AMR: append([]string(nil), outcome.AMR...), IsNew: outcome.IsNew,
		Confirmed: outcome.Confirmed, AvatarURL: result.Identity.AvatarURL,
	}
	if s.avatar != nil && state.Intent != IntentLink {
		delivery := AvatarDelivery{URL: result.Identity.AvatarURL}
		if result.Avatar != nil {
			delivery = *result.Avatar
		}
		avatarResolver, _ := adapter.(AvatarResolver)
		s.avatar.Inherit(outcome.AccountID, provider, delivery, avatarResolver)
	}
	return completion, nil
}

func (s *Service) CreateConfirmGrant(ctx context.Context, accountID int32, identityID, providerID int64, providerSlug, returnTo string, amr []string) (token, browserToken string, err error) {
	token, err = randomToken()
	if err != nil {
		return "", "", err
	}
	browserToken, err = randomToken()
	if err != nil {
		return "", "", err
	}
	grant := ConfirmGrant{
		AccountID: accountID, IdentityID: identityID, ProviderID: providerID,
		ProviderSlug: providerSlug, ReturnTo: returnTo, BrowserDigest: BrowserDigest(browserToken),
		AMR: append([]string(nil), amr...),
	}
	raw, err := grant.Encode()
	if err != nil {
		return "", "", err
	}
	if err := s.kv.SetEx(ctx, ConfirmKey(token), raw, 15*time.Minute); err != nil {
		return "", "", fmt.Errorf("%w: store confirmation grant: %v", ErrKVUnavailable, err)
	}
	return token, browserToken, nil
}

func (s *Service) PopConfirmGrant(ctx context.Context, token, browserToken string) (*ConfirmGrant, error) {
	raw, err := s.kv.Pop(ctx, ConfirmKey(token))
	if err != nil {
		return nil, authn.ErrFederationStateInvalid()
	}
	grant, err := DecodeConfirmGrant(raw)
	if err != nil || !BrowserBindingOK(grant.BrowserDigest, browserToken) {
		return nil, authn.ErrFederationStateInvalid()
	}
	return grant, nil
}

func (s *Service) PeekConfirmGrant(ctx context.Context, token, browserToken string) (*ConfirmGrant, error) {
	raw, err := s.kv.Get(ctx, ConfirmKey(token))
	if err != nil {
		return nil, authn.ErrFederationStateInvalid()
	}
	grant, err := DecodeConfirmGrant(raw)
	if err != nil || !BrowserBindingOK(grant.BrowserDigest, browserToken) {
		return nil, authn.ErrFederationStateInvalid()
	}
	return grant, nil
}

func (s *Service) AvatarPending(ctx context.Context, accountID int32) bool {
	return s.avatar != nil && s.avatar.Pending(ctx, accountID)
}

func (s *Service) loadForAdvance(ctx context.Context, request AdvanceRequest) (string, *FlowState, Provider, Adapter, error) {
	raw, err := s.kv.Get(ctx, FlowKey(request.FlowID))
	if err != nil {
		return "", nil, Provider{}, nil, s.recordFailure(ctx, nil, &request, "", NewFailure(FailureStateInvalid, nil))
	}
	state, err := DecodeFlowState(raw)
	if err != nil || !state.ExpiresAt.After(s.now()) {
		return "", nil, Provider{}, nil, s.recordFailure(ctx, nil, &request, "", NewFailure(FailureStateInvalid, nil))
	}
	if !callbackRouteAllowsIntent(request.CallbackRoute, state.Intent) {
		return "", nil, Provider{}, nil, s.recordFailure(ctx, state, &request, state.ProviderSlug, NewFailure(FailureStateInvalid, nil))
	}
	if !BrowserBindingOK(state.BrowserDigest, request.BrowserToken) {
		return "", nil, Provider{}, nil, s.recordFailure(ctx, state, &request, state.ProviderSlug, NewFailure(FailureBrowserBindingMismatch, nil))
	}
	if state.ProviderSlug != request.ProviderSlug ||
		request.Protocol != "" && state.Protocol != request.Protocol ||
		state.CurrentAction.Kind != request.Input.Kind {
		return "", nil, Provider{}, nil, s.recordFailure(ctx, state, &request, state.ProviderSlug, NewFailure(FailureStateInvalid, nil))
	}
	if state.Intent == IntentLink {
		if request.AccountID == nil || state.LinkAccountID == nil || *request.AccountID != *state.LinkAccountID || request.SessionID == "" || request.SessionID != state.LinkSessionID {
			var stateAccountID any
			if state.LinkAccountID != nil {
				stateAccountID = *state.LinkAccountID
			}
			return "", nil, Provider{}, nil, s.recordFailure(ctx, state, &request, state.ProviderSlug, NewFailure(FailureSessionSwap, map[string]any{
				"state_account_id": stateAccountID,
			}))
		}
	}
	provider, err := s.providers.ByBinding(ctx, state.ProviderID, state.ProviderSlug, state.Protocol)
	if err != nil || provider.Disabled {
		return "", nil, Provider{}, nil, s.recordFailure(ctx, state, &request, state.ProviderSlug, NewFailure(FailureProviderUnavailable, nil))
	}
	definition, adapter, err := s.flowProvider(provider)
	if err != nil || !definition.Ready(provider) {
		return "", nil, Provider{}, nil, s.recordFailure(ctx, state, &request, state.ProviderSlug, NewFailure(FailureProviderUnavailable, nil))
	}
	return raw, state, provider, adapter, nil
}

func (s *Service) recordFailure(ctx context.Context, state *FlowState, request *AdvanceRequest, providerSlug string, err error) error {
	reason, extra, publicErr, ok := failureProjection(err)
	if state != nil && state.Protocol == "vrchat" {
		s.recordVRChatFailure(ctx, state, providerSlug, err)
		if ok {
			return publicErr
		}
		return err
	}
	if !ok {
		return err
	}
	detail := map[string]any{"reason": string(reason)}
	if providerSlug != "" {
		detail["idp_slug"] = providerSlug
	}
	for key, value := range extra {
		detail[key] = value
	}
	var accountID *int32
	if request != nil && request.CallbackRoute == CallbackRouteLink && request.AccountID != nil {
		accountID = new(*request.AccountID)
	}
	audit.RecordOrLog(ctx, s.audit, audit.Record{
		AccountID: accountID,
		Factor:    audit.FactorFederationOIDC,
		Event:     audit.EventFail,
		Detail:    detail,
	})
	return publicErr
}

func (s *Service) recordVRChatFailure(ctx context.Context, state *FlowState, providerSlug string, err error) {
	s.recordVRChatTransition(ctx, "vrchat_proof_failed", state, providerSlug, vrchatFailureCategory(err))
}

func vrchatFailureCategory(err error) string {
	if errors.Is(err, ErrKVUnavailable) {
		return "kv_unavailable"
	}
	if errors.Is(err, ErrLocalUsernameRequired) {
		return "local_username_required"
	}
	if reason, ok := FailureReasonOf(err); ok {
		switch reason {
		case FailureStateInvalid,
			FailureBrowserBindingMismatch,
			FailureProviderUnavailable,
			FailureIssuerMismatch,
			FailureTokenEndpointDrift,
			FailureCodeExchange,
			FailureSteamVerification,
			FailureSessionSwap,
			FailureEmailNotVerified,
			FailureDomainNotAllowed,
			FailureLinkConflict,
			FailureLinkInsert,
			FailureInviteLookup,
			FailureInviteWrongIntent,
			FailureInviteConsumed,
			FailureInviteExpired,
			FailureInviteNotFederated,
			FailureInviteRequiredPreAuth,
			FailureVRChatIdentityInvalid,
			FailureVRChatProofMissing,
			FailureVRChatProviderNotReady,
			FailureUpstreamRateLimited,
			FailureUpstreamUnavailable:
			return string(reason)
		}
	}
	if authErr := authn.AsAuthError(err); authErr != nil {
		switch authErr.Code {
		case "username_collision",
			"invalid_username",
			"bad_credentials",
			"invite_required",
			"link_required",
			"email_not_verified",
			"federation_state_invalid",
			"provider_not_ready",
			"upstream_rate_limited",
			"upstream_temporarily_unavailable",
			"vrchat_identity_invalid",
			"vrchat_proof_missing":
			return authErr.Code
		}
	}
	var databaseErr interface{ SQLState() string }
	if errors.As(err, &databaseErr) && databaseErr.SQLState() != "" {
		return "database_unavailable"
	}
	return "resolution_failed"
}

func (s *Service) recordVRChatTransition(ctx context.Context, event string, state *FlowState, providerSlug, category string) {
	detail := map[string]any{
		"provider_slug": providerSlug,
		"intent":        string(state.Intent),
	}
	if category != "" {
		detail["category"] = category
	}
	audit.RecordOrLog(ctx, s.audit, audit.Record{
		Factor: audit.FactorFederationOIDC,
		Event:  event,
		Detail: detail,
	})
}

func (s *Service) restoreAfterFailure(ctx context.Context, request AdvanceRequest, providerSlug string, state *FlowState, originalRaw string, cause error, requireUsername bool) error {
	if state.Protocol != "vrchat" {
		publicErr := s.recordFailure(ctx, state, &request, providerSlug, cause)
		return s.restore(ctx, request.FlowID, state, originalRaw, publicErr, requireUsername)
	}
	publicErr := cause
	if _, _, projected, ok := failureProjection(cause); ok {
		publicErr = projected
	}
	restored := s.restore(ctx, request.FlowID, state, originalRaw, publicErr, requireUsername)
	auditCause := cause
	if errors.Is(restored, ErrKVUnavailable) {
		auditCause = restored
	}
	s.recordVRChatFailure(ctx, state, providerSlug, auditCause)
	return restored
}

func (s *Service) restore(ctx context.Context, flowID string, state *FlowState, originalRaw string, cause error, requireUsername bool) error {
	remaining := state.ExpiresAt.Sub(s.now())
	if remaining <= 0 {
		return cause
	}
	raw := originalRaw
	if requireUsername {
		restored := *state
		restored.AdapterState = append(json.RawMessage(nil), state.AdapterState...)
		restored.CurrentAction = cloneAction(state.CurrentAction)
		if restored.CurrentAction.Public == nil {
			restored.CurrentAction.Public = make(map[string]any)
		}
		restored.CurrentAction.Public["requiresLocalUsername"] = true
		encoded, err := restored.Encode()
		if err != nil {
			return fmt.Errorf("%w: restore encoding: %v", ErrKVUnavailable, err)
		}
		raw = encoded
	}
	cleanupCtx, cancel := detachedCleanupContext(ctx, s.config.LockTTL)
	defer cancel()
	if err := s.kv.SetEx(cleanupCtx, FlowKey(flowID), raw, remaining); err != nil {
		return fmt.Errorf("%w: restore flow: %v", ErrKVUnavailable, err)
	}
	return cause
}

func (s *Service) lock(ctx context.Context, flowID string) (*flowLease, error) {
	if !validFlowID(flowID) {
		return nil, authn.ErrFederationStateInvalid()
	}
	owner, err := randomToken()
	if err != nil {
		return nil, err
	}
	operationCtx, cancelOperation := context.WithCancel(ctx)
	maintenanceCtx, cancelMaintenance := context.WithCancel(context.WithoutCancel(ctx))
	key := FlowLockKey(flowID)
	locked, err := s.kv.SetNX(ctx, key, owner, s.config.LockTTL)
	if err != nil {
		cancelMaintenance()
		cancelOperation()
		return nil, fmt.Errorf("%w: lock flow: %v", ErrKVUnavailable, err)
	}
	if !locked {
		cancelMaintenance()
		cancelOperation()
		return nil, authn.ErrFederationStateInvalid()
	}
	lease := &flowLease{
		ctx: operationCtx, cancel: cancelOperation,
		maintenanceCtx: maintenanceCtx, cancelMaintenance: cancelMaintenance,
		store: s.kv, key: key, owner: owner,
		ttl: s.config.LockTTL, stop: make(chan struct{}), done: make(chan struct{}),
		lost: make(chan struct{}),
	}
	go lease.renew()
	return lease, nil
}

func (l *flowLease) renew() {
	defer close(l.done)
	interval := l.ttl / 3
	if interval <= 0 {
		interval = l.ttl
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			renewCtx, cancel := context.WithTimeout(l.maintenanceCtx, boundedLeaseCleanupTimeout(l.ttl))
			renewed, err := l.store.CompareAndSwap(renewCtx, l.key, l.owner, l.owner, l.ttl)
			cancel()
			if err != nil || !renewed {
				select {
				case <-l.stop:
					return
				default:
					close(l.lost)
					l.cancel()
					return
				}
			}
		case <-l.stop:
			return
		case <-l.maintenanceCtx.Done():
			return
		}
	}
}

func (l *flowLease) check() error {
	select {
	case <-l.lost:
		return fmt.Errorf("%w: flow lease lost", ErrKVUnavailable)
	default:
		return nil
	}
}

func (l *flowLease) release() {
	l.releaseOnce.Do(func() {
		close(l.stop)
		l.cancelMaintenance()
		l.cancel()
		<-l.done
		releaseCtx, cancel := detachedCleanupContext(l.ctx, l.ttl)
		defer cancel()
		_, _ = l.store.CompareAndDelete(releaseCtx, l.key, l.owner)
	})
}

func detachedCleanupContext(parent context.Context, ttl time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(parent), boundedLeaseCleanupTimeout(ttl))
}

func boundedLeaseCleanupTimeout(ttl time.Duration) time.Duration {
	if ttl > 0 && ttl < maxLeaseCleanupTimeout {
		return ttl
	}
	return maxLeaseCleanupTimeout
}

func (s *Service) flowProvider(provider Provider) (Definition, Adapter, error) {
	if provider.Disabled {
		return nil, nil, ErrUnknownProvider
	}
	definition, err := s.registry.Definition(provider.Protocol)
	if err != nil {
		return nil, nil, err
	}
	adapter, err := s.registry.Adapter(provider.Protocol)
	if err != nil {
		return nil, nil, err
	}
	return definition, adapter, nil
}

func validCallbackRoute(route CallbackRoute) bool {
	return route == CallbackRoutePublic || route == CallbackRouteLink
}

func callbackRouteAllowsIntent(route CallbackRoute, intent Intent) bool {
	switch route {
	case CallbackRoutePublic:
		return intent == IntentLogin || intent == IntentInvite
	case CallbackRouteLink:
		return intent == IntentLink
	default:
		return false
	}
}

func (s *Service) callbackURL(slug string, intent Intent) string {
	origin := strings.TrimRight(s.config.PublicOrigin, "/")
	escapedSlug := url.PathEscape(slug)
	if intent == IntentLink {
		return origin + "/api/prohibitorum/me/identities/link/" + escapedSlug + "/callback"
	}
	return origin + "/api/prohibitorum/auth/federation/" + escapedSlug + "/callback"
}

func flowView(flowID string, state *FlowState) *FlowView {
	return &FlowView{FlowID: flowID, Intent: state.Intent, ProviderSlug: state.ProviderSlug, Protocol: state.Protocol, Action: cloneAction(state.CurrentAction)}
}

func cloneAction(action NextAction) NextAction {
	clone := action
	if action.Public != nil {
		clone.Public = make(map[string]any, len(action.Public))
		for key, value := range action.Public {
			clone.Public[key] = value
		}
	}
	return clone
}

func validFlowID(flowID string) bool {
	if len(flowID) != 43 {
		return false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(flowID)
	return err == nil && len(decoded) == 32
}

func randomToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("federation: random token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}
