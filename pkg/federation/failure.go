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
	FailureStateInvalid           FailureReason = "state_invalid"
	FailureBrowserBindingMismatch FailureReason = "browser_binding_mismatch"
	FailureProviderUnavailable    FailureReason = "idp_disabled_or_deleted"
	FailureActionInvalid          FailureReason = "action_invalid"
	FailureLocalUsernameRequired  FailureReason = "local_username_required"
	FailureIssuerMismatch         FailureReason = "iss_mismatch_callback"
	FailureTokenEndpointDrift     FailureReason = "token_endpoint_drift"
	FailureCodeExchange           FailureReason = "code_exchange_failed"
	FailureSteamVerification      FailureReason = "steam_verify_failed"
	FailureSessionSwap            FailureReason = "session_swap"
	FailureEmailNotVerified       FailureReason = "email_not_verified"
	FailureDomainNotAllowed       FailureReason = "domain_not_allowed"
	FailureLinkConflict           FailureReason = "link_conflict"
	FailureLinkInsert             FailureReason = "link_insert_failed"
	FailureInviteLookup           FailureReason = "invite_lookup_failed"
	FailureInviteWrongIntent      FailureReason = "invite_wrong_intent"
	FailureInviteConsumed         FailureReason = "invite_already_consumed"
	FailureInviteExpired          FailureReason = "invite_expired"
	FailureInviteNotFederated     FailureReason = "invite_not_federated"
	FailureInviteRequiredPreAuth  FailureReason = "invite_required_pre_auth"
	FailureVRChatIdentityInvalid  FailureReason = "vrchat_identity_invalid"
	FailureVRChatProofMissing     FailureReason = "vrchat_proof_missing"
	FailureVRChatProviderNotReady FailureReason = "vrchat_provider_not_ready"
	FailureUpstreamRateLimited    FailureReason = "upstream_rate_limited"
	FailureUpstreamUnavailable    FailureReason = "upstream_temporarily_unavailable"
)

type failurePolicy struct {
	public     func() error
	detailKeys map[string]struct{}
}

var failurePolicies = map[FailureReason]failurePolicy{
	FailureStateInvalid:           {public: stateInvalid},
	FailureBrowserBindingMismatch: {public: stateInvalid},
	FailureProviderUnavailable:    {public: stateInvalid},
	FailureActionInvalid:          {public: func() error { return authn.ErrFederationActionInvalid() }},
	FailureLocalUsernameRequired:  {public: func() error { return authn.ErrLocalUsernameRequired() }},
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
		public:     func() error { return authn.ErrEmailNotVerified() },
		detailKeys: keys("upstream_iss"),
	},
	FailureDomainNotAllowed: {public: func() error { return authn.ErrInviteRequired() }},
	FailureLinkConflict: {
		public:     stateInvalid,
		detailKeys: keys("iss", "sub"),
	},
	FailureLinkInsert: {
		public:     stateInvalid,
		detailKeys: keys("iss", "sub"),
	},
	FailureInviteLookup:           {public: func() error { return authn.ErrInviteRequired() }},
	FailureInviteWrongIntent:      {public: func() error { return authn.ErrInviteRequired() }, detailKeys: keys("intent")},
	FailureInviteConsumed:         {public: func() error { return authn.ErrInviteRequired() }},
	FailureInviteExpired:          {public: func() error { return authn.ErrInviteRequired() }},
	FailureInviteNotFederated:     {public: func() error { return authn.ErrInviteRequired() }},
	FailureInviteRequiredPreAuth:  {public: func() error { return authn.ErrInviteRequired() }},
	FailureVRChatIdentityInvalid:  {public: func() error { return authn.ErrVRChatIdentityInvalid() }},
	FailureVRChatProofMissing:     {public: func() error { return authn.ErrVRChatProofMissing() }},
	FailureVRChatProviderNotReady: {public: func() error { return authn.ErrProviderNotReady() }},
	FailureUpstreamRateLimited:    {public: func() error { return authn.ErrUpstreamRateLimited(0) }},
	FailureUpstreamUnavailable:    {public: func() error { return authn.ErrUpstreamTemporarilyUnavailable() }},
}

func stateInvalid() error { return authn.ErrFederationStateInvalid() }

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
	failure := &flowFailure{reason: reason, detail: filtered, public: policy.public()}
	if reason == FailureLocalUsernameRequired {
		failure.cause = ErrLocalUsernameRequired
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

func failureProjection(err error) (FailureReason, map[string]any, error, bool) {
	var failure *flowFailure
	if !errors.As(err, &failure) {
		return "", nil, nil, false
	}
	detail := make(map[string]any, len(failure.detail))
	for key, value := range failure.detail {
		detail[key] = value
	}
	return failure.reason, detail, failure.public, true
}
