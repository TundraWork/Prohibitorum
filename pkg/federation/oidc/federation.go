package oidc

import (
	"context"
	"errors"

	// Imported here so go.mod retains zitadel/oidc/v3 as a direct
	// dependency for downstream v0.3 tasks. Task 5 will replace this
	// blank import with the real RP client wrapper in client.go.
	_ "github.com/zitadel/oidc/v3/pkg/client/rp"

	"prohibitorum/pkg/db"
)

var (
	ErrUnknownIDP     = errors.New("federation: unknown IdP")
	ErrModeRejection  = errors.New("federation: provisioning mode rejected this sign-in")
	ErrIssuerMismatch = errors.New("federation: discovery issuer doesn't match configured issuer_url")
	ErrStaleState     = errors.New("federation: state TTL expired")
)

type LoginRequest struct {
	AuthorizeURL string
	StateKey     string
}

type CallbackResult struct {
	AccountID  int32
	SessionID  string
	NewAccount bool
	Linked     bool
}

type Federator struct {
	q db.Querier
}

func NewFederator(q db.Querier) *Federator {
	return &Federator{q: q}
}

// TODO(v0.3): GetUpstreamIDPBySlug, fetch+cache discovery doc, snapshot
// expected_iss into KV state blob, build authorize URL with PKCE.
func (f *Federator) BeginLogin(ctx context.Context, idpSlug, returnTo string) (*LoginRequest, error) {
	return nil, ErrUnknownIDP
}

// TODO(v0.3): pull KV state, verify expected_iss matches ID token issuer,
// exchange code, validate ID token, apply mode (auto_provision / invite_only /
// link_only), upsert account_identity, mint session.
func (f *Federator) HandleCallback(ctx context.Context, idpSlug, code, state string) (*CallbackResult, error) {
	return nil, ErrUnknownIDP
}
