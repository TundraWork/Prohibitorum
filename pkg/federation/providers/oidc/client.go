// Package oidc — RP client wrapper.
//
// client.go isolates the zitadel/oidc/v3 RP API behind a small surface
// (Client, Tokens). The rest of the federation package and the rest of
// the codebase MUST NOT import zitadel/oidc/v3 directly; this file is
// the one place where the upstream library's vocabulary leaks into
// project code. That keeps the JWT alg allowlist, project-specific
// error mapping, and any future library upgrades in one bounded
// blast radius.
//
// Per RFC 9700 §4.4.2.1 (mix-up resistance) we snapshot the issuer and
// token endpoint at NewClient time and expose them as Issuer() and
// TokenEndpoint() so the adapter can compare against the expected issuer
// captured in its flow state without re-running discovery. We also re-check
// nonce and issuer on the decoded ID token claims; zitadel/oidc
// already enforces these against the discovery values, but the
// caller-supplied expectedIss/expectedNonce is the actual security
// boundary, and re-checking here is defense in depth.
package oidc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/tidwall/gjson"
	"github.com/zitadel/oidc/v3/pkg/client/rp"
	oidclib "github.com/zitadel/oidc/v3/pkg/oidc"
	"golang.org/x/oauth2"

	federationcore "prohibitorum/pkg/federation"
)

// errUpstreamIdentityParse marks a token response whose id_token could not be
// parsed (form-encoded response, malformed JWT): the identity is unrecoverable
// even though the exchange itself may have succeeded. The adapter maps it to
// the upstream_identity_unavailable failure with the original error text.
var errUpstreamIdentityParse = errors.New("upstream identity parse")

// errNoUserInfoEndpoint and errNoSubject mark why the no-id_token fallback
// found no identity, so the connection test can name the cause. Login still
// reports upstream_identity_unavailable for both.
var (
	errNoUserInfoEndpoint = errors.New("no userinfo endpoint")
	errNoSubject          = errors.New("no subject claim")
)

// DefaultAllowedAlgs returns the JWT signing-alg allowlist used when NewClient
// is called with nil allowedAlgs. RS256, ES256, EdDSA only. HS256 and "none"
// are explicitly excluded.
//
// A function (not a var) so the allowlist cannot be mutated process-wide by
// a buggy or malicious caller.
func DefaultAllowedAlgs() []string {
	return []string{"RS256", "ES256", "EdDSA"}
}

// Tokens is the project-facing result of a successful code exchange.
// It deliberately does not expose zitadel/oidc's internal claim types;
// the wrapper extracts the fields the rest of the codebase needs and
// drops the rest. AMR is the RFC 8176 list of authentication method
// references the upstream OP reported (e.g., ["pwd","mfa","hwk"]).
//
// Raw is a unified view of all id_token claims (extras merged with the
// OIDC-typed standard claims hoisted under their JSON-tag keys). It is
// the source of truth for the per-IdP claim-name overrides — admins can
// point upstream_idp.{username,display_name,email}_claim at non-default
// keys like Entra ID's "upn", and the adapter reads those through
// ClaimString. The typed fields above are kept for backwards-compat and
// convenience — they remain the
// right place to read fields with no override knob (Subject, Issuer,
// EmailVerified, AMR, Nonce).
//
// TokenType is the token_type from the token response ("Bearer", …). Empty
// means the upstream omitted it; userinfo requests treat that as
// oidc.BearerToken. It is set even when IDToken is empty (the userinfo
// fallback path).
type Tokens struct {
	TokenType         string
	IDToken           string
	AccessToken       string
	Subject           string
	Issuer            string
	Nonce             string
	Email             string
	EmailVerified     bool
	PreferredUsername string
	Name              string
	AMR               []string
	// AuthTime is the time at which the end-user was last authenticated by
	// the upstream OP, as reported in the id_token auth_time claim (OIDC
	// Core §2). Zero if the OP did not include auth_time in the id_token.
	AuthTime time.Time
	Raw      map[string]any
}

// ClaimString returns the value of the named claim as text, or "" when the
// name resolves to nothing usable. Used by mode policies and LinkCallback
// to honor the per-upstream_idp claim-name overrides (username_claim,
// display_name_claim, email_claim) — admins can point at non-OIDC-default
// names like Entra ID's "upn", at a nested value through gjson path syntax
// ("avatar.large", "emails.0.email", `annotations.email\.primary`), or at a
// claim whose own name contains dots, which the exact-key lookup reads as
// one literal key before any path splitting happens.
//
// Numbers and booleans stringify (a numeric GitHub id, a JSON true becoming
// "true"); objects, arrays and null count as absent — an account field
// needs one value, not a document, and guessing a member of a composite
// would silently pick the wrong person's data.
func ClaimString(raw map[string]any, name string) string {
	if name == "" {
		return ""
	}
	return claimLeafText(claimLookup(raw, name))
}

// claimLookup resolves a claim name to one gjson result. An exact map key
// wins: a dotted name such as Auth0's namespaced "https://example.com/roles"
// is the literal claim key a provider configured before paths existed, and
// must keep reading it rather than be split into segments. Only a miss is
// evaluated as a gjson path, so "avatar.large" reaches into the nested
// object because no top-level claim is spelled that way. A miss or an
// unmarshalable document yields the zero Result, whose Null type every
// reader treats as absent.
func claimLookup(raw map[string]any, name string) gjson.Result {
	if v, ok := raw[name]; ok {
		return claimValue(v)
	}
	if raw == nil {
		return gjson.Result{}
	}
	bytes, err := json.Marshal(raw)
	if err != nil {
		return gjson.Result{}
	}
	return gjson.GetBytes(bytes, name)
}

// claimValue types one decoded Go claim value the way gjson would have typed
// it in the document, so a flat hit and a path hit share one set of leaf
// rules. Scalars convert directly — a json.Number keeps its exact digits,
// which a marshal round-trip would not guarantee for oddly-spelled numbers —
// and composite values go through JSON, where anything unmarshalable reads
// as absent.
func claimValue(v any) gjson.Result {
	switch v := v.(type) {
	case nil:
		return gjson.Result{Type: gjson.Null}
	case string:
		return gjson.Result{Type: gjson.String, Str: v}
	case bool:
		if v {
			return gjson.Result{Type: gjson.True, Raw: "true"}
		}
		return gjson.Result{Type: gjson.False, Raw: "false"}
	case json.Number:
		return gjson.Result{Type: gjson.Number, Raw: v.String()}
	default:
		bytes, err := json.Marshal(v)
		if err != nil {
			return gjson.Result{}
		}
		return gjson.ParseBytes(bytes)
	}
}

// claimLeafText renders a resolved leaf as account-field text: strings and
// numbers keep their JSON encoding, booleans become "true"/"false", and
// anything composite (objects, arrays, null, a multi-match wildcard) counts
// as absent rather than guessing one of its members.
func claimLeafText(result gjson.Result) string {
	switch result.Type {
	case gjson.String:
		return result.String()
	case gjson.Number, gjson.True, gjson.False:
		return result.Raw
	default:
		return ""
	}
}

// ClaimIdentifier returns the value of the named claim as an upstream subject
// identifier: strings pass through, integers return their exact original
// text. Floats, booleans, objects, arrays and absent claims all yield "" —
// the caller treats that as "no usable subject". Like ClaimString the name
// resolves an exact key first, then a gjson path, but only a string or
// integer leaf can identify a subject.
func ClaimIdentifier(raw map[string]any, name string) string {
	if name == "" {
		return ""
	}
	result := claimLookup(raw, name)
	switch result.Type {
	case gjson.String:
		return result.String()
	case gjson.Number:
		// Large identifiers keep their exact digits only when the whole
		// number fits an int64; a float — 1e3, 1.5 — is not an identifier.
		if _, err := strconv.ParseInt(result.Raw, 10, 64); err != nil {
			return ""
		}
		return result.Raw
	default:
		return ""
	}
}

// ClaimBool returns the boolean value of the named claim, or false when the
// claim is absent or not a bool. ClaimString cannot serve this role: its
// number/string coercion would read a JSON 1 as email_verified=true, which
// the upstream never said.
func ClaimBool(raw map[string]any, name string) bool {
	if name == "" {
		return false
	}
	return claimLookup(raw, name).Type == gjson.True
}

// acceptHeaderTransport sets Accept: application/json on requests that carry
// no Accept header, leaving an explicit one untouched.
//
// golang.org/x/oauth2 never sends Accept, so a GitHub-style token endpoint
// answers with application/x-www-form-urlencoded; there Token.Extra("id_token")
// returns "" rather than nil and zitadel parses the empty string as a JWT,
// losing the access token it could have returned. JSON keeps the missing
// id_token observable (rp.ErrMissingIDToken) with the access token intact,
// which is what the userinfo fallback needs. Every request through this client
// targets a JSON endpoint (discovery, JWKS, token exchange, userinfo).
type acceptHeaderTransport struct {
	base http.RoundTripper
}

func (t *acceptHeaderTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Header.Get("Accept") == "" {
		req = req.Clone(req.Context())
		req.Header.Set("Accept", "application/json")
	}
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(req)
}

// Client wraps a single configured upstream OIDC IdP.
//
// One Client instance corresponds to one row in upstream_idp: it holds
// the discovered endpoints, the configured allowlist, and the JWKS
// cache (which lives inside the embedded RelyingParty's verifier).
// A Client is safe for concurrent use by multiple goroutines — the
// underlying zitadel/oidc RelyingParty is goroutine-safe for read
// operations (CodeExchange, AuthURL).
type Client struct {
	pkceMethod    string
	rp            rp.RelyingParty
	issuer        string // snapshot at NewClient time
	tokenEndpoint string // snapshot at NewClient time
}

// resolvedRelyingParty retains the complete library RP contract while supplying
// explicit OIDC endpoints and a verifier. IsOAuth2Only is true for a JWKS-none
// provider: zitadel's verifyTokenResponse then returns the token without
// reading the id_token, which is what keeps the plain-OAuth2 path in Exchange
// from ever verifying one.
type resolvedRelyingParty struct {
	rp.RelyingParty
	resolved ResolvedConfig
	verifier *rp.IDTokenVerifier
}

func (r *resolvedRelyingParty) Issuer() string                       { return r.resolved.Issuer }
func (r *resolvedRelyingParty) IsOAuth2Only() bool                   { return r.resolved.JWKSEndpoint == jwksNone }
func (r *resolvedRelyingParty) IsPKCE() bool                         { return r.resolved.PKCEMethod != "off" }
func (r *resolvedRelyingParty) UserinfoEndpoint() string             { return r.resolved.UserInfoEndpoint }
func (r *resolvedRelyingParty) IDTokenVerifier() *rp.IDTokenVerifier { return r.verifier }

// NewClient consumes a resolved snapshot; it does not perform discovery.
func NewClient(_ context.Context, clientID, clientSecret, redirectURI string, resolved ResolvedConfig, allowedAlgs []string, allowPrivateNetwork bool) (*Client, error) {
	if allowedAlgs == nil {
		allowedAlgs = DefaultAllowedAlgs()
	}
	if len(allowedAlgs) == 0 {
		return nil, errors.New("federation/oidc: allowedAlgs is empty")
	}
	style := oauth2.AuthStyleInParams
	switch resolved.TokenAuthMethod {
	case "client_secret_basic":
		style = oauth2.AuthStyleInHeader
	case "client_secret_post":
	case "none":
		if resolved.PKCEMethod != "S256" {
			return nil, errors.New("federation/oidc: public clients require S256")
		}
		clientSecret = ""
	default:
		return nil, errors.New("federation/oidc: unresolved token authentication method")
	}
	// Copy the client struct instead of building a fresh one: CheckRedirect
	// (max 5 hops, no https→http downgrade, re-validation of redirect targets)
	// is the outbound policy against following a token-endpoint redirect that
	// would replay the Authorization header in plaintext. Only the Transport
	// changes here.
	hardened := federationcore.NewOutboundHTTPClient(allowPrivateNetwork, 2<<20)
	wrapped := *hardened
	wrapped.Transport = newRecordingTransport(&acceptHeaderTransport{base: hardened.Transport}, resolved)
	httpClient := &wrapped
	base, err := rp.NewRelyingPartyOAuth(&oauth2.Config{ClientID: clientID, ClientSecret: clientSecret, RedirectURL: redirectURI, Scopes: append([]string(nil), resolved.Scopes...), Endpoint: oauth2.Endpoint{AuthURL: resolved.AuthorizationEndpoint, TokenURL: resolved.TokenEndpoint}}, rp.WithHTTPClient(httpClient), rp.WithAuthStyle(style))
	if err != nil {
		return nil, err
	}
	// A JWKS-none provider never verifies an id_token, so no key set is
	// fetched or held: the verifier stays nil and IsOAuth2Only (above) keeps
	// the library from reaching it during a code exchange.
	var verifier *rp.IDTokenVerifier
	if resolved.JWKSEndpoint != jwksNone {
		verifier = rp.NewIDTokenVerifier(resolved.Issuer, clientID, rp.NewRemoteKeySet(httpClient, resolved.JWKSEndpoint), rp.WithSupportedSigningAlgorithms(allowedAlgs...), rp.WithNonce(nonceFromCtx))
	}
	configured := &resolvedRelyingParty{RelyingParty: base, resolved: resolved, verifier: verifier}
	return &Client{rp: configured, issuer: resolved.Issuer, tokenEndpoint: resolved.TokenEndpoint, pkceMethod: resolved.PKCEMethod}, nil
}

// Issuer returns the issuer URL as it was reported by discovery at
// NewClient time. This is the value the adapter compares against the
// id_token's iss claim and against the expected issuer in its flow state.
// It is intentionally a snapshot: admin edits to upstream_idp must not
// retroactively change what an in-flight Exchange call considers a valid
// issuer.
func (c *Client) Issuer() string {
	return c.issuer
}

// TokenEndpoint returns the token endpoint URL snapshotted at
// NewClient time. Exposed for logging and for the adapter's
// mix-up-resistance bookkeeping; Exchange uses it internally via
// the embedded RelyingParty.
func (c *Client) TokenEndpoint() string {
	return c.tokenEndpoint
}

// AuthURL includes state and nonce in every mode. codeChallenge is the S256
// digest or plain verifier supplied by the adapter; off omits all PKCE fields.
func (c *Client) AuthURL(state, nonce, codeChallenge string, extra ...oauth2.AuthCodeOption) string {
	opts := make([]rp.AuthURLOpt, 0, 2+len(extra))
	if c.pkceMethod != "off" {
		opts = append(opts, authURLOpt(oauth2.SetAuthURLParam("code_challenge", codeChallenge)), authURLOpt(oauth2.SetAuthURLParam("code_challenge_method", c.pkceMethod)))
	}
	opts = append(opts, authURLOpt(oauth2.SetAuthURLParam("nonce", nonce)))
	for _, o := range extra {
		opts = append(opts, authURLOpt(o))
	}
	return rp.AuthURL(state, c.rp, opts...)
}

// ImplicitAuthURL builds the OIDC implicit-flow authorization request
// (response_type=id_token) used by public clients. The nonce is the flow's
// only front-channel binding — no code, no PKCE — so it must be unguessable
// and stored server-side in the flow state, exactly as Begin does.
func (c *Client) ImplicitAuthURL(state, nonce string) string {
	return rp.AuthURL(state, c.rp,
		authURLOpt(oauth2.SetAuthURLParam("response_type", "id_token")),
		authURLOpt(oauth2.SetAuthURLParam("nonce", nonce)))
}

// VerifyImplicit verifies a front-channel id_token from the implicit flow
// (response_type=id_token) and projects it into Tokens exactly as Exchange
// treats a token-response id_token: full signature, issuer, audience, expiry,
// nonce and alg checks, with IDToken set and the OAuth2 token fields empty.
// expectedIss and expectedNonce are the values snapshotted in the flow state.
func (c *Client) VerifyImplicit(ctx context.Context, idToken, expectedIss, expectedNonce string) (*Tokens, error) {
	// A JWKS-none provider builds no verifier; the save rule keeps the
	// implicit flow off such a provider, and ResolveConfig rejects a
	// discovery-supplied "none". This guard is the third lock on that door:
	// zitadel panics on a nil verifier, so refuse here instead.
	if c.rp.IDTokenVerifier() == nil {
		return nil, errors.New("federation/oidc: implicit id_token verification has no key set")
	}
	ctx = context.WithValue(ctx, nonceCtxKey{}, expectedNonce)
	claims, err := rp.VerifyIDToken[*oidclib.IDTokenClaims](ctx, idToken, c.rp.IDTokenVerifier())
	if err != nil {
		return nil, fmt.Errorf("federation/oidc: implicit id_token: %w", err)
	}
	if claims == nil {
		return nil, errors.New("federation/oidc: implicit id_token returned no claims")
	}
	if claims.Issuer != expectedIss {
		return nil, fmt.Errorf("federation/oidc: issuer mismatch: id_token iss=%q, expected %q", claims.Issuer, expectedIss)
	}
	if claims.Nonce != expectedNonce {
		return nil, errors.New("federation/oidc: nonce mismatch in id_token")
	}
	if alg := string(claims.GetSignatureAlgorithm()); alg != "" && !algInAllowlist(alg, c.allowedAlgs()) {
		return nil, fmt.Errorf("federation/oidc: id_token signed with disallowed alg %q", alg)
	}
	result := tokensFromIDTokenClaims(idToken, "", claims)
	recorderFrom(ctx).exchanged(c, result)
	return result, nil
}

// authURLOpt is a convenience adapter so we can drop a single
// oauth2.AuthCodeOption (e.g. SetAuthURLParam("nonce", n)) into the
// variadic rp.AuthURL call without writing a one-off rp.AuthURLOpt
// factory. zitadel/oidc has rp.WithURLParam but it builds a URLParamOpt
// that's compatible at the func-type level; this small adapter
// short-circuits that ceremony.
func authURLOpt(o oauth2.AuthCodeOption) rp.AuthURLOpt {
	return func() []oauth2.AuthCodeOption {
		return []oauth2.AuthCodeOption{o}
	}
}

// Exchange performs the OAuth 2.0 authorization-code exchange and
// verifies the returned ID token (signature, issuer, audience,
// expiration, nonce, and signing algorithm).
//
// expectedIss MUST be the issuer the caller intended to talk to (the
// value snapshotted in the KV state blob at BeginLogin time). The
// library already verifies that the id_token iss matches the
// discovery issuer of the RelyingParty; this method additionally
// rejects any token whose iss does not match the caller-supplied
// expectedIss. That second check is what mix-up resistance hinges on
// when an attacker swaps the OP between BeginLogin and the callback.
//
// expectedNonce MUST be the nonce embedded in the AuthURL for this
// flow (and stored in the state blob). The library verifies nonce
// equality with the verifier's configured nonce; we re-check here so
// the wrapper's behaviour is self-contained and so the error message
// is consistent with our project vocabulary.
func (c *Client) Exchange(
	ctx context.Context,
	code, codeVerifier, expectedIss, expectedNonce string,
) (*Tokens, error) {
	// Stash the expected nonce in context so the library-side nonce
	// check (configured at NewClient via rp.WithNonce(nonceFromCtx))
	// sees the right value for this flow.
	ctx = context.WithValue(ctx, nonceCtxKey{}, expectedNonce)

	var opts []rp.CodeExchangeOpt
	if c.pkceMethod != "off" {
		opts = append(opts, rp.WithCodeVerifier(codeVerifier))
	}
	tokens, err := rp.CodeExchange[*oidclib.IDTokenClaims](ctx, code, c.rp, opts...)
	// The two shapes that reach here without IDTokenClaims are one path: an
	// upstream that returned no id_token (ErrMissingIDToken) and an OAuth2-only
	// provider (IsOAuth2Only above), where the library returns the bare token
	// with no error. Both continue below into the same fallback.
	if err == nil && tokens != nil && tokens.IDTokenClaims == nil && tokens.AccessToken != "" {
		err = rp.ErrMissingIDToken
	}
	if err != nil {
		// Upstream returned no id_token but kept the access token usable:
		// that is a result, not an error. Hand it back with IDToken empty and
		// let the caller decide (the adapter falls back to userinfo). No
		// signature/issuer/nonce/alg checks run — there is nothing to check.
		// tokens is never nil with ErrMissingIDToken (zitadel returns
		// &oidc.Tokens{Token: token}), but the AccessToken guard keeps us
		// honest if the library ever changes shape.
		if errors.Is(err, rp.ErrMissingIDToken) && tokens != nil && tokens.AccessToken != "" {
			tokenType := tokens.TokenType
			if tokenType == "" {
				tokenType = oidclib.BearerToken
			}
			result := &Tokens{AccessToken: tokens.AccessToken, TokenType: tokenType}
			recorderFrom(ctx).exchanged(c, result)
			return result, nil
		}
		// The remaining failure that loses the token entirely: a form-encoded
		// response (upstream ignored Accept) or a malformed id_token. Both
		// mean "no usable identity came back"; the adapter maps the wrapped
		// sentinel to upstream_identity_unavailable with the original text.
		if errors.Is(err, oidclib.ErrParse) {
			return nil, fmt.Errorf("%w: federation/oidc: code exchange: %w", errUpstreamIdentityParse, err)
		}
		return nil, fmt.Errorf("federation/oidc: code exchange: %w", err)
	}
	if tokens == nil || tokens.IDTokenClaims == nil {
		return nil, errors.New("federation/oidc: code exchange returned no id_token claims")
	}

	claims := tokens.IDTokenClaims

	// Defensive re-check of issuer. The library verifies this against
	// the discovery issuer; the caller-supplied expectedIss is the
	// stronger check because it pins the OP per-flow, not per-Client.
	if claims.Issuer != expectedIss {
		return nil, fmt.Errorf(
			"federation/oidc: issuer mismatch: id_token iss=%q, expected %q",
			claims.Issuer, expectedIss,
		)
	}

	// Defensive re-check of nonce.
	if claims.Nonce != expectedNonce {
		return nil, fmt.Errorf("federation/oidc: nonce mismatch in id_token")
	}

	// Defensive re-check of the signing algorithm. The verifier is
	// configured with the allowlist so we should never reach this
	// branch for a disallowed alg — but better to fail loudly here
	// than silently trust the library to have applied our config.
	if alg := string(claims.GetSignatureAlgorithm()); alg != "" && !algInAllowlist(alg, c.allowedAlgs()) {
		return nil, fmt.Errorf("federation/oidc: id_token signed with disallowed alg %q", alg)
	}

	// Build a unified Raw view of the id_token claims so per-IdP claim-name
	// overrides can read either an "extra" claim or the typed OIDC standard
	// claim under its JSON-tag key. See tokensFromIDTokenClaims.
	result := tokensFromIDTokenClaims(tokens.IDToken, tokens.AccessToken, claims)
	result.TokenType = tokens.TokenType
	recorderFrom(ctx).exchanged(c, result)
	return result, nil
}

// tokensFromIDTokenClaims projects verified IDTokenClaims into the project
// Tokens shape shared by the code flow (Exchange) and the implicit flow
// (VerifyImplicit). accessToken/tokenType carry the OAuth2 fields only the
// code flow has; the implicit flow passes them empty.
//
// Raw is a unified view of all id_token claims (extras merged with the
// OIDC-typed standard claims hoisted under their JSON-tag keys). It is
// the source of truth for the per-IdP claim-name overrides — admins can
// point upstream_idp.{username,display_name,email}_claim at non-default
// keys like Entra ID's "upn", and the adapter reads those through
// ClaimString. Empty strings are skipped so "claim genuinely absent" stays
// distinguishable from an empty override target.
func tokensFromIDTokenClaims(idToken, accessToken string, claims *oidclib.IDTokenClaims) *Tokens {
	raw := make(map[string]any, len(claims.Claims)+8)
	for k, v := range claims.Claims {
		raw[k] = v
	}
	if claims.PreferredUsername != "" {
		raw["preferred_username"] = claims.PreferredUsername
	}
	if claims.Name != "" {
		raw["name"] = claims.Name
	}
	if claims.Email != "" {
		raw["email"] = claims.Email
	}
	if claims.Subject != "" {
		raw["sub"] = claims.Subject
	}
	if claims.Issuer != "" {
		raw["iss"] = claims.Issuer
	}
	// picture is parsed by the library into UserInfoProfile.Picture and
	// dropped from claims.Claims, so it must be explicitly hoisted here
	// alongside the other typed standard claims.
	if claims.Picture != "" {
		raw["picture"] = claims.Picture
	}
	return &Tokens{
		IDToken:           idToken,
		AccessToken:       accessToken,
		Subject:           claims.Subject,
		Issuer:            claims.Issuer,
		Nonce:             claims.Nonce,
		Email:             claims.Email,
		EmailVerified:     bool(claims.EmailVerified),
		PreferredUsername: claims.PreferredUsername,
		Name:              claims.Name,
		AMR:               []string(claims.AuthenticationMethodsReferences),
		// AuthTime: zero when the OP omitted auth_time (AuthTime field is
		// oidc.Time which is int64; AsTime() on the zero value returns the
		// Go zero time).
		AuthTime: claims.AuthTime.AsTime(),
		Raw:      raw,
	}
}

// allowedAlgs returns the alg allowlist configured on the underlying
// verifier. zitadel/oidc stores it on the IDTokenVerifier; we read it
// back rather than carrying a duplicate copy in Client.
func (c *Client) allowedAlgs() []string {
	v := c.rp.IDTokenVerifier()
	if v == nil {
		return nil
	}
	return v.SupportedSignAlgs
}

// nonceCtxKey is the private context key under which Exchange stashes
// the per-flow expected nonce so the verifier's nonce-check callback
// (configured once at NewClient time via rp.WithNonce) can read it.
type nonceCtxKey struct{}

func nonceFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(nonceCtxKey{}).(string)
	return v
}

func algInAllowlist(alg string, allowed []string) bool {
	for _, a := range allowed {
		if a == alg {
			return true
		}
	}
	return false
}

// UserInfoToRaw converts a *oidc.UserInfo into a unified claims map using the
// same hoisting convention as Exchange: extras from info.Claims are merged
// first, then typed standard claims (picture, name, preferred_username, email)
// are written under their JSON-tag keys so they win over any OP that happened to
// also put them in the extras map.
//
// Exported so the package test can exercise the transform without a live HTTP
// server. Callers inside this package should prefer Client.UserInfo, which
// fetches the endpoint and then calls this helper.
func UserInfoToRaw(info *oidclib.UserInfo) map[string]any {
	raw := make(map[string]any, len(info.Claims)+4)
	for k, v := range info.Claims {
		raw[k] = v
	}
	if info.Picture != "" {
		raw["picture"] = info.Picture
	}
	if info.PreferredUsername != "" {
		raw["preferred_username"] = info.PreferredUsername
	}
	if info.Name != "" {
		raw["name"] = info.Name
	}
	if info.Email != "" {
		raw["email"] = info.Email
	}
	return raw
}

// UserInfo fetches the OIDC UserInfo endpoint with the given access token,
// through the same SSRF-hardened HTTP client as discovery/token-exchange. It
// returns a unified claims map (typed standard claims hoisted under their
// JSON-tag keys, plus any extras) so ClaimString can read picture/etc.
// uniformly. subject must be the id_token sub for the same token exchange —
// the library rejects a UserInfo response whose sub does not match.
// Errors are returned for the caller to treat as non-fatal.
func (c *Client) UserInfo(ctx context.Context, accessToken, subject string) (map[string]any, error) {
	if c.rp.UserinfoEndpoint() == "" {
		return nil, nil
	}
	info, err := rp.Userinfo[*oidclib.UserInfo](ctx, accessToken, oidclib.BearerToken, subject, c.rp)
	if err != nil {
		return nil, fmt.Errorf("federation/oidc: userinfo: %w", err)
	}
	return UserInfoToRaw(info), nil
}

// UserInfoRaw fetches the resolved userinfo endpoint without comparing any
// subject: it serves the no-id_token fallback path, where there is no id_token
// sub to check, and upstreams like GitHub whose userinfo carries no sub at
// all. Claims decode with json.Number so large integer identifiers keep their
// exact text — read the subject through ClaimIdentifier, not ClaimString.
//
// The resolved endpoint may legitimately be empty (discovery omitted
// userinfo_endpoint, or manual mode left userinfo null = do not call userinfo);
// that is an error here, not a silent skip. Errors go back raw for the caller
// to fold into its failure vocabulary.
func (c *Client) UserInfoRaw(ctx context.Context, accessToken, tokenType string) (map[string]any, error) {
	endpoint := c.rp.UserinfoEndpoint()
	if endpoint == "" {
		return nil, fmt.Errorf("%w: federation/oidc: no userinfo endpoint resolved", errNoUserInfoEndpoint)
	}
	if tokenType == "" {
		tokenType = oidclib.BearerToken
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("federation/oidc: userinfo request: %w", err)
	}
	req.Header.Set("Authorization", tokenType+" "+accessToken)
	resp, err := c.rp.HttpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("federation/oidc: userinfo request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("federation/oidc: userinfo: status %d", resp.StatusCode)
	}
	var claims map[string]any
	decoder := json.NewDecoder(resp.Body)
	decoder.UseNumber()
	if err := decoder.Decode(&claims); err != nil {
		return nil, fmt.Errorf("federation/oidc: userinfo decode: %w", err)
	}
	return claims, nil
}
