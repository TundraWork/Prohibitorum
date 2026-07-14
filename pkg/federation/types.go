package federation

import (
	"context"
	"encoding/json"
	"net/url"
	"time"
)

type Intent string

const (
	IntentLogin  Intent = "login"
	IntentLink   Intent = "link"
	IntentInvite Intent = "invite"
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
	Next       *NextAction
	Identity   *VerifiedIdentity
	Candidate  *IdentityKey
	State      json.RawMessage
}

type VerifiedIdentity struct {
	Issuer        string
	Subject       string
	Email         *string
	EmailVerified bool
	Username      string
	DisplayName   string
	AMR           []string
	AvatarURL     string
	UpstreamData  map[string]string
}

type CompletionResult struct {
	Intent       Intent
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
