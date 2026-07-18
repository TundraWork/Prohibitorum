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
	IntentEnroll Intent = "enroll"
)

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
	ID                int64
	Slug              string
	DisplayName       string
	Protocol          string
	Mode              string
	Config            json.RawMessage
	Secret            *SealedSecret
	SecretStatus      string
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

// AvatarDelivery carries avatar-only data from a terminal adapter result to the
// detached inheritance worker. Opaque is never passed to identity resolution or
// persisted in flow state.
type AvatarDelivery struct {
	URL                 string
	Opaque              any
	AllowPrivateNetwork bool
}

// AvatarResolver is an optional adapter capability used only by the detached
// avatar inheritance path when the verified identity has no direct avatar URL.
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
}

type EnrollmentGrant struct {
	Token     string
	Intent    string
	ExpiresAt time.Time
}

type EnrollmentIssuer interface {
	Issue(context.Context, Provider, VerifiedIdentity) (EnrollmentGrant, error)
}

type CompletionResult struct {
	Intent       Intent
	Enrollment   *EnrollmentGrant
	AccountID    int32
	IdentityID   int64
	ProviderID   int64
	ProviderSlug string
	ReturnTo     string
	AMR          []string
	IsNew        bool
	Confirmed    bool
	AvatarURL    string
}

func (r *CompletionResult) Validate() error {
	if r == nil {
		return errors.New("federation: missing completion result")
	}
	if r.Intent == IntentEnroll {
		if r.Enrollment == nil || r.Enrollment.Token == "" {
			return errors.New("federation: invalid enrollment completion")
		}
		if r.AccountID != 0 || r.IdentityID != 0 || r.ProviderID != 0 || r.ProviderSlug != "" ||
			r.ReturnTo != "" || len(r.AMR) != 0 || r.IsNew || r.Confirmed || r.AvatarURL != "" {
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
	ValidateSecret([]byte) error
	Ready(Provider) bool
}

type Adapter interface {
	Protocol() string
	Begin(context.Context, Provider, BeginContext) (json.RawMessage, NextAction, error)
	Advance(context.Context, Provider, json.RawMessage, ActionInput) (AdvanceResult, error)
}
