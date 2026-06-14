package oidc

import (
	"encoding/json"
	"fmt"
	"strconv"
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

	// EnrollmentToken, when non-empty, signals that the flow was started
	// from an invite URL. The callback dispatches to applyInviteOnly
	// regardless of idp.Mode, atomically consuming the enrollment row and
	// minting the local account inside a single transaction. The token
	// itself is the secret that grants account creation — protected by
	// living in server-side KV only.
	EnrollmentToken string `json:"enrollment_token,omitempty"`

	// SudoAccountID, when non-nil, marks this as a sudo step-up flow: the
	// callback handler must confirm the re-authenticated upstream identity
	// resolves back to ExpectedSub for this account before satisfying the
	// sudo gate. Like LinkingAccountID it is stored alongside the key-prefix
	// separation (SudoKey) so a Pop bug can't cross-thread the flows. Sudo
	// and link are mutually exclusive — a sudo flow never sets LinkingAccountID.
	SudoAccountID *int32 `json:"sudo_account_id,omitempty"`

	// ExpectedSub, when non-empty, is the upstream subject of the account's
	// existing linked identity at SudoBegin time. The sudo callback (Task 3)
	// requires the forced re-auth to resolve back to this exact subject —
	// re-authenticating as a DIFFERENT upstream user must not satisfy the gate.
	ExpectedSub string `json:"expected_sub,omitempty"`

	// BrowserBinding, when non-empty, is the SHA-256 (base64url) of a random
	// anti-forgery token that the HTTP layer set as a short-lived cookie at
	// BeginLogin time. HandleCallback requires the callback request's cookie
	// to hash to this value, binding the flow to the initiating browser and
	// defeating login-CSRF / session-fixation (audit follow-up N4). Empty for
	// flows that do not carry the cookie (e.g. the link flow, which is already
	// account-bound).
	BrowserBinding string `json:"browser_binding,omitempty"`
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

// SudoKey returns the KV key under which sudo-step-up-flow state lives. A
// distinct namespace from LoginKey/LinkKey so a token minted for sudo
// re-auth cannot be Pop'd by the login or link callbacks (and vice versa) —
// same cross-purpose-token-reuse defense as the other two.
func SudoKey(token string) string {
	return "oidc:fed:sudo:" + token
}

// AvatarFetchKey is the KV key marking an in-flight upstream avatar fetch for an
// account. Presence = "pending"; value unused. Short TTL backstops a dead
// goroutine; SetNX on this key dedupes concurrent logins.
func AvatarFetchKey(accountID int32) string {
	return "oidc:fed:avatar:" + strconv.Itoa(int(accountID))
}

// ConfirmGrant is the short-lived, single-use, browser-bound context for the
// /welcome identity-confirmation step. Created when the callback withholds a
// session for an unconfirmed identity; consumed by the confirm endpoint.
type ConfirmGrant struct {
	AccountID      int32    `json:"account_id"`
	IdentityID     int64    `json:"identity_id"`
	IDPID          int64    `json:"idp_id"`
	IDPSlug        string   `json:"idp_slug"`
	ReturnTo       string   `json:"return_to"`
	BrowserBinding string   `json:"browser_binding"`
	AMR            []string `json:"amr,omitempty"`
}

// Encode serializes the grant to a JSON string for KV storage.
func (g ConfirmGrant) Encode() (string, error) {
	b, err := json.Marshal(g)
	if err != nil {
		return "", fmt.Errorf("federation/oidc: encode confirm grant: %w", err)
	}
	return string(b), nil
}

// DecodeConfirmGrant parses a JSON-encoded ConfirmGrant back into a struct.
func DecodeConfirmGrant(raw string) (*ConfirmGrant, error) {
	var g ConfirmGrant
	if err := json.Unmarshal([]byte(raw), &g); err != nil {
		return nil, fmt.Errorf("federation/oidc: decode confirm grant: %w", err)
	}
	return &g, nil
}

// ConfirmKey namespaces the confirmation-grant token, distinct from the
// login/link/sudo flow namespaces so a token minted for one purpose cannot be
// Pop'd by the handler for another (same cross-purpose-token-reuse defense).
func ConfirmKey(token string) string {
	return "oidc:fed:confirm:" + token
}
