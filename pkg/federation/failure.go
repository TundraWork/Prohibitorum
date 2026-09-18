package federation

import (
	"errors"
	"time"

	"prohibitorum/pkg/authn"
)

// FailureReason is an allowlisted operator-facing federation failure
// classification. The corresponding public error is fixed by failurePolicy so
// protocol adapters cannot weaken the non-oracular wire contract.
type FailureReason string

const (
	FailureStateInvalid            FailureReason = "state_invalid"
	FailureBrowserBindingMismatch  FailureReason = "browser_binding_mismatch"
	FailureProviderUnavailable     FailureReason = "idp_disabled_or_deleted"
	FailureActionInvalid           FailureReason = "action_invalid"
	FailureLocalUsernameRequired   FailureReason = "local_username_required"
	FailureIssuerMismatch          FailureReason = "iss_mismatch_callback"
	FailureTokenEndpointDrift      FailureReason = "token_endpoint_drift"
	FailureCodeExchange            FailureReason = "code_exchange_failed"
	FailureSteamVerification       FailureReason = "steam_verify_failed"
	FailureSessionSwap             FailureReason = "session_swap"
	FailureEmailNotVerified        FailureReason = "email_not_verified"
	FailureDomainNotAllowed        FailureReason = "domain_not_allowed"
	FailureLinkConflict            FailureReason = "link_conflict"
	FailureLinkInsert              FailureReason = "link_insert_failed"
	FailureInviteLookup            FailureReason = "invite_lookup_failed"
	FailureInviteWrongIntent       FailureReason = "invite_wrong_intent"
	FailureInviteConsumed          FailureReason = "invite_already_consumed"
	FailureInviteExpired           FailureReason = "invite_expired"
	FailureInviteSlugMismatch      FailureReason = "invite_slug_mismatch"
	FailureLinkOnlyProvisionDenied FailureReason = "link_only_provision_denied"
	FailureInviteNotFederated      FailureReason = "invite_not_federated"
	FailureVRChatIdentityInvalid   FailureReason = "vrchat_identity_invalid"
	FailureVRChatProofMissing      FailureReason = "vrchat_proof_missing"
	FailureVRChatProviderNotReady  FailureReason = "vrchat_provider_not_ready"
	FailureUpstreamRateLimited     FailureReason = "upstream_rate_limited"
	FailureUpstreamUnavailable     FailureReason = "upstream_temporarily_unavailable"
	FailureUpstreamNoIdentity      FailureReason = "upstream_identity_unavailable"
)

type failurePolicy struct {
	public     func(map[string]any) error
	detailKeys map[string]struct{}
}

var failurePolicies = map[FailureReason]failurePolicy{
	FailureStateInvalid:           {public: stateInvalid},
	FailureBrowserBindingMismatch: {public: stateInvalid},
	FailureProviderUnavailable:    {public: stateInvalid},
	FailureActionInvalid:          {public: staticPublic(authn.ErrFederationActionInvalid)},
	FailureLocalUsernameRequired:  {public: staticPublic(authn.ErrLocalUsernameRequired)},
	FailureIssuerMismatch: {
		public:     stateInvalid,
		detailKeys: keys("expected_iss", "got_iss"),
	},
	FailureTokenEndpointDrift: {
		public:     stateInvalid,
		detailKeys: keys("expected", "got"),
	},
	FailureCodeExchange:      {public: stateInvalid},
	FailureSteamVerification: {public: stateInvalid},
	FailureSessionSwap: {
		public:     stateInvalid,
		detailKeys: keys("state_account_id"),
	},
	FailureEmailNotVerified: {
		public:     staticPublic(authn.ErrEmailNotVerified),
		detailKeys: keys("upstream_iss"),
	},
	FailureDomainNotAllowed: {public: staticPublic(authn.ErrInviteRequired)},
	FailureLinkConflict: {
		public:     stateInvalid,
		detailKeys: keys("iss", "sub"),
	},
	FailureLinkInsert: {
		public:     stateInvalid,
		detailKeys: keys("iss", "sub"),
	},
	FailureInviteLookup:       {public: staticPublic(authn.ErrInviteRequired)},
	FailureInviteWrongIntent:  {public: staticPublic(authn.ErrInviteRequired), detailKeys: keys("intent")},
	FailureInviteConsumed:     {public: staticPublic(authn.ErrInviteRequired)},
	FailureInviteExpired:      {public: staticPublic(authn.ErrInviteRequired)},
	FailureInviteNotFederated: {public: staticPublic(authn.ErrInviteRequired)},
	FailureInviteSlugMismatch: {
		public: func(detail map[string]any) error {
			name, _ := detail["federationName"].(string)
			return authn.ErrFederationInviteProviderMismatch(name)
		},
		detailKeys: keys("enrollment_expected_slug"),
	},
	FailureLinkOnlyProvisionDenied: {
		public:     staticPublic(authn.ErrInviteRequired),
		detailKeys: keys("idp_slug"),
	},
	FailureVRChatIdentityInvalid:  {public: staticPublic(authn.ErrVRChatIdentityInvalid)},
	FailureVRChatProofMissing:     {public: staticPublic(authn.ErrVRChatProofMissing)},
	FailureVRChatProviderNotReady: {public: staticPublic(authn.ErrProviderNotReady)},
	FailureUpstreamRateLimited:    {public: func(map[string]any) error { return authn.ErrUpstreamRateLimited(0) }},
	FailureUpstreamUnavailable:    {public: staticPublic(authn.ErrUpstreamTemporarilyUnavailable)},
	FailureUpstreamNoIdentity:     {public: stateInvalid},
}

func stateInvalid(_ map[string]any) error { return authn.ErrFederationStateInvalid() }

func staticPublic(factory func() *authn.AuthError) func(map[string]any) error {
	return func(map[string]any) error { return factory() }
}

func keys(values ...string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		out[value] = struct{}{}
	}
	return out
}

type flowFailure struct {
	reason FailureReason
	detail map[string]any
	public error
	cause  error
}

func (e *flowFailure) Error() string { return "federation flow failed" }
func (e *flowFailure) Unwrap() []error {
	if e.cause == nil {
		return []error{e.public}
	}
	return []error{e.public, e.cause}
}

// NewFailure creates an opaque public federation error carrying only
// allowlisted audit detail. Unknown reasons collapse to state_invalid.
func NewFailure(reason FailureReason, detail map[string]any) error {
	policy, ok := failurePolicies[reason]
	if !ok {
		reason = FailureStateInvalid
		policy = failurePolicies[reason]
	}
	filtered := make(map[string]any, len(policy.detailKeys))
	for key := range policy.detailKeys {
		if value, exists := detail[key]; exists {
			filtered[key] = value
		}
	}
	failure := &flowFailure{reason: reason, detail: filtered, public: policy.public(detail)}
	if reason == FailureLocalUsernameRequired {
		failure.cause = ErrLocalUsernameRequired
	}
	return failure
}

// NewFailureWithCause wraps an upstream error into the opaque public
// federation error, preserving the raw cause for the operator-facing
// federation_flow_failure log. The cause never reaches the wire or the
// audit Detail: failureProjection carries it only to the federation log
// boundary, while Unwrap keeps exposing the public AuthError alongside it
// so errors.Is/As semantics are unchanged. A nil cause behaves exactly
// like NewFailure.
func NewFailureWithCause(reason FailureReason, detail map[string]any, cause error) error {
	failure := NewFailure(reason, detail).(*flowFailure)
	if cause != nil {
		failure.cause = cause
	}
	return failure
}

// NewRateLimitedFailureWithCause is NewRateLimitedFailure with the upstream
// error preserved for the federation_flow_failure log; it stays out of the
// wire and the audit Detail. Use it where the 429 came from a real upstream
// response (classifyUpstream); the local backoff path keeps NewRateLimited.
func NewRateLimitedFailureWithCause(retryAfter time.Duration, cause error) error {
	failure := NewRateLimitedFailure(retryAfter).(*flowFailure)
	if cause != nil {
		failure.cause = cause
	}
	return failure
}

// NewRateLimitedFailure preserves the bounded Retry-After value while retaining
// the allowlisted federation failure classification used by audit.
func NewRateLimitedFailure(retryAfter time.Duration) error {
	failure := NewFailure(FailureUpstreamRateLimited, nil).(*flowFailure)
	failure.public = authn.ErrUpstreamRateLimited(retryAfter)
	return failure
}

func FailureReasonOf(err error) (FailureReason, bool) {
	var failure *flowFailure
	if !errors.As(err, &failure) {
		return "", false
	}
	return failure.reason, true
}

// failureProjection projects a flow failure into its audit-safe parts: the
// reason, a copy of the allowlisted detail, and the public error. cause
// carries the raw upstream error for the federation log boundary only — it
// never re-enters the audit Detail and must never reach the wire.
func failureProjection(err error) (FailureReason, map[string]any, error, error, bool) {
	var failure *flowFailure
	if !errors.As(err, &failure) {
		return "", nil, nil, nil, false
	}
	detail := make(map[string]any, len(failure.detail))
	for key, value := range failure.detail {
		detail[key] = value
	}
	return failure.reason, detail, failure.public, failure.cause, true
}
