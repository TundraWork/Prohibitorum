package oidc

import (
	"encoding/json"
	"fmt"
)

// FedState is the per-request flow context stashed in KV between
// BeginLogin (or BeginLink) and the callback handler. It is the
// security-critical bridge between the two HTTP requests: every field
// participates in mix-up resistance, replay defense, or post-callback
// dispatch.
//
// Persistence is server-side only — the opaque token returned to the
// client is the KV key; the JSON-encoded FedState is the value. The
// client never sees these fields directly, so we can put implementation
// details (IDPSlug) in here without leaking schema.
type FedState struct {
	// IDPID identifies the row in upstream_idp this flow targets. Used at
	// callback time to load the IdP's mode + allowlist + DEK.
	IDPID int64 `json:"idp_id"`

	// IDPSlug is duplicated from upstream_idp.slug so the callback handler
	// can construct the redirect URI and look up the IdP without an extra
	// by-ID query. Cheap to carry; the blob is server-side.
	IDPSlug string `json:"idp_slug"`

	// ExpectedIss is the discovery issuer snapshot at BeginLogin time.
	// RFC 9700 §4.4.2.1: pinning per-flow defeats mix-up attacks where
	// the attacker swaps OPs between authorize and callback.
	ExpectedIss string `json:"expected_iss"`

	// ExpectedTokenEndpoint is the discovery token_endpoint snapshot at
	// BeginLogin time. Same mix-up-resistance rationale as ExpectedIss.
	ExpectedTokenEndpoint string `json:"expected_token_endpoint"`

	// Nonce is the per-flow OIDC nonce embedded in the authorize request
	// and verified against the id_token.nonce claim at callback time.
	Nonce string `json:"nonce"`

	// CodeVerifier is the PKCE verifier; the SHA-256 base64url-encoded
	// transform was sent as code_challenge with method=S256.
	CodeVerifier string `json:"code_verifier"`

	// ReturnTo is the dashboard URL to redirect back to after a successful
	// callback. Validated against an allowlist by the HTTP handler before
	// going into KV — never trust on read.
	ReturnTo string `json:"return_to"`

	// LinkingAccountID, when non-nil, marks this as a link flow rather
	// than a login flow: the callback handler must bind the upstream
	// identity to this existing account instead of resolving via mode
	// policies. Stored in addition to the key-prefix separation so a
	// future Pop bug can't quietly cross-thread the two flows.
	LinkingAccountID *int32 `json:"linking_account_id,omitempty"`
}

// Encode serializes the state to a JSON string for KV storage.
func (s FedState) Encode() (string, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return "", fmt.Errorf("federation/oidc: encode fed state: %w", err)
	}
	return string(b), nil
}

// DecodeFedState parses a JSON-encoded FedState back into a struct.
func DecodeFedState(raw string) (*FedState, error) {
	var s FedState
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return nil, fmt.Errorf("federation/oidc: decode fed state: %w", err)
	}
	return &s, nil
}

// LoginKey returns the KV key under which login-flow state lives.
// Login and Link namespaces are intentionally distinct so a token minted
// for one purpose cannot be Pop'd by the handler for the other purpose
// (defense against scope confusion).
func LoginKey(token string) string {
	return "oidc:fed:state:" + token
}

// LinkKey returns the KV key under which link-flow state lives. See
// LoginKey for the cross-purpose-token-reuse rationale.
func LinkKey(token string) string {
	return "oidc:fed:link:" + token
}
