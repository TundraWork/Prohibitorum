package federation

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"time"
)

type Intent string

const (
	IntentLogin  Intent = "login"
	IntentLink   Intent = "link"
	IntentInvite Intent = "invite"
)

// ReservedSlug is the path segment under /api/prohibitorum/auth/federation/
// that holds the routes not keyed by a provider (flows, confirm). No provider
// may take it as its slug, so those routes never shadow a provider's own.
const ReservedSlug = "-"

type CallbackRoute string

const (
	CallbackRoutePublic CallbackRoute = "public"
	CallbackRouteLink   CallbackRoute = "link"
	CallbackRouteLocal  CallbackRoute = "local"
)

type ActionKind string

const (
	ActionRedirect        ActionKind = "redirect"
	ActionCollectIdentity ActionKind = "collect_identity"
	ActionPublishProof    ActionKind = "publish_proof"
)

type SearchOperator string

const (
	SearchExact    SearchOperator = "exact"
	SearchPrefix   SearchOperator = "prefix"
	SearchContains SearchOperator = "contains"
)

type SearchField struct {
	Key       string
	Operators []SearchOperator
}

type Descriptor struct {
	Protocol         string
	SearchFields     []SearchField
	SupportsOperator bool
	RequiresSecret   bool
}

type SealedSecret struct {
	Ciphertext []byte
	Nonce      []byte
	KeyVersion int32
}

type Provider struct {
	ID           int64
	Slug         string
	DisplayName  string
	Protocol     string
	Mode         string
	Config       json.RawMessage
	Secret       *SealedSecret
	SecretStatus string
	// AllowSignIn is false when the provider may only verify identities
	// (invites, account linking) and never backs a sign-in button.
	AllowSignIn       bool
	SecretValidatedAt *time.Time
	Disabled          bool
}

type BeginContext struct {
	Intent          Intent
	FlowID          string
	CallbackURL     string
	ReturnTo        string
	LinkAccountID   *int32
	LinkSessionID   string
	EnrollmentToken string
}

type NextAction struct {
	Kind   ActionKind
	URL    string
	Public map[string]any
}

type ActionInput struct {
	Kind          ActionKind
	Code          string
	Issuer        string
	Params        url.Values
	Identity      string
	LocalUsername string
	// IDToken carries a front-channel id_token for OIDC flows that return one
	// without a code exchange (the implicit flow public clients use). The
	// adapter verifies it exactly as it would one from a token response.
	IDToken string
	// AccessToken carries the front-channel access_token of the OIDC
	// id_token token implicit response, relayed into the callback query next
	// to the id_token. The adapter checks it against the verified id_token's
	// at_hash and keeps it in callback memory only — it is never written to
	// flow state, the KV store or the database.
	AccessToken string
}

type IdentityKey struct {
	Issuer  string
	Subject string
}

type AdvanceResult struct {
	Next      *NextAction
	Identity  *VerifiedIdentity
	Candidate *IdentityKey
	State     json.RawMessage
	Avatar    *AvatarDelivery
}

// AvatarDelivery carries avatar-only data from a terminal adapter result to
// AvatarManager.Refresh. Opaque is never passed to identity resolution or
// persisted in flow state. UserAgent, when set, replaces Go's default on the
// fetch: VRChat's firewall answers 403 to a request that does not name its
// application and contact, including requests for profile icons.
//
// BearerToken, when an adapter sets it, is sent as `Authorization: Bearer …`
// on the picture fetch to whatever URL the provider returned — the
// authenticateAvatarFetch setting is the operator's explicit instruction to
// send the token, so no origin restriction is applied here. Go's http client
// itself drops the Authorization header on a redirect to a different host.
// The token lives in callback memory only, like Opaque.
type AvatarDelivery struct {
	URL                 string
	Opaque              any
	AllowPrivateNetwork bool
	UserAgent           string
	BearerToken         string
}

// AvatarResolver is an optional adapter capability used only by
// AvatarManager.Refresh when the verified identity has no direct avatar URL.
type AvatarResolver interface {
	ResolveAvatar(context.Context, Provider, AvatarDelivery) (string, error)
}

type VerifiedIdentity struct {
	Issuer                     string
	Subject                    string
	Email                      *string
	EmailVerified              bool
	EmailVerificationSupported bool
	Username                   string
	DisplayName                string
	AMR                        []string
	AvatarURL                  string
	UpstreamData               map[string]string
	// UserInfoFallback records that OIDC authentication succeeded without an
	// ID token and used the userinfo endpoint as the identity source. It is
	// transient flow metadata and is not persisted with upstream identity data.
	UserInfoFallback bool
}

type EnrollmentGrant struct {
	Token     string
	Intent    string
	ExpiresAt time.Time
}


type CompletionResult struct {
	Intent       Intent
	Enrollment   *EnrollmentGrant
	AccountID    int32
	IdentityID   int64
	ProviderID   int64
	ProviderSlug string
	ReturnTo     string
	// Remember carries the login page's "keep me signed in" choice from the
	// flow state to the session-issuing handlers.
	Remember  bool
	AMR       []string
	IsNew     bool
	Confirmed bool
	AvatarURL string
	// OfferLocalSignin marks an invite+provider provisioning: after /welcome
	// confirmation the SPA offers the one-time "add local sign-in" step.
	OfferLocalSignin bool
}

func (r *CompletionResult) Validate() error {
	if r == nil {
		return errors.New("federation: missing completion result")
	}
	if r.Intent == IntentInvite && r.Enrollment != nil {
		// The verify-then-create invite path: the identity snapshot is stored
		// on the invitation, no account exists yet, and the browser goes to
		// /enroll/{token}.
		if r.Enrollment.Token == "" {
			return errors.New("federation: invalid enrollment completion")
		}
		if r.AccountID != 0 || r.IdentityID != 0 ||
			len(r.AMR) != 0 || r.IsNew || r.Confirmed || r.AvatarURL != "" || r.OfferLocalSignin {
			return errors.New("federation: mixed enrollment completion")
		}
		return nil
	}
	switch r.Intent {
	case IntentLogin, IntentLink, IntentInvite:
	default:
		return errors.New("federation: invalid completion intent")
	}
	if r.Enrollment != nil {
		return errors.New("federation: unexpected enrollment grant")
	}
	return nil
}

type Definition interface {
	Protocol() string
	Descriptor() Descriptor
	ValidateConfig(json.RawMessage) error
	ValidateSecret(json.RawMessage, []byte) error
	Ready(Provider) bool
	// RelaysFragment reports whether the protocol's authorization response
	// arrives in the redirect fragment, so its callback may need the relay
	// page. Protocols that never front-channel a response return false.
	RelaysFragment(Provider) bool
}

type Adapter interface {
	Protocol() string
	Begin(context.Context, Provider, BeginContext) (json.RawMessage, NextAction, error)
	Advance(context.Context, Provider, json.RawMessage, ActionInput) (AdvanceResult, error)
}
