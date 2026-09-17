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
	"time"

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

// ClaimString returns the string value of the named claim, or "" if the
// claim is absent or not a string. Used by mode policies and LinkCallback
// to honor the per-upstream_idp claim-name overrides (username_claim,
// display_name_claim, email_claim) — admins can point at non-OIDC-default
// names like Entra ID's "upn".
func ClaimString(raw map[string]any, name string) string {
	if name == "" {
		return ""
	}
	v, ok := raw[name]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

// ClaimIdentifier returns the value of the named claim as an upstream subject
// identifier: strings pass through, integers decoded as json.Number return as
// their exact original text. Floats, booleans, objects, arrays and absent
// claims all yield "" — the caller treats that as "no usable subject".
//
// Only meaningful on claims decoded with json.Decoder.UseNumber (UserInfoRaw);
// ClaimString stays the reader for id_token-derived claims.
func ClaimIdentifier(raw map[string]any, name string) string {
	if name == "" {
		return ""
	}
	switch v := raw[name].(type) {
	case string:
		return v
	case json.Number:
		if _, err := v.Int64(); err != nil {
			return ""
		}
		return v.String()
	default:
		return ""
	}
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
// explicit OIDC endpoints and a verifier. IsOAuth2Only must remain false.
type resolvedRelyingParty struct {
	rp.RelyingParty
	resolved ResolvedConfig
	verifier *rp.IDTokenVerifier
}

func (r *resolvedRelyingParty) Issuer() string                       { return r.resolved.Issuer }
func (r *resolvedRelyingParty) IsOAuth2Only() bool                   { return false }
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
	httpClient := federationcore.NewOutboundHTTPClient(allowPrivateNetwork, 2<<20)
	httpClient = &http.Client{Transport: &acceptHeaderTransport{base: httpClient.Transport}, Timeout: httpClient.Timeout}
	base, err := rp.NewRelyingPartyOAuth(&oauth2.Config{ClientID: clientID, ClientSecret: clientSecret, RedirectURL: redirectURI, Scopes: append([]string(nil), resolved.Scopes...), Endpoint: oauth2.Endpoint{AuthURL: resolved.AuthorizationEndpoint, TokenURL: resolved.TokenEndpoint}}, rp.WithHTTPClient(httpClient), rp.WithAuthStyle(style))
	if err != nil {
		return nil, err
	}
	verifier := rp.NewIDTokenVerifier(resolved.Issuer, clientID, rp.NewRemoteKeySet(httpClient, resolved.JWKSEndpoint), rp.WithSupportedSigningAlgorithms(allowedAlgs...), rp.WithNonce(nonceFromCtx))
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
			return &Tokens{AccessToken: tokens.AccessToken, TokenType: tokenType}, nil
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
	// overrides (username_claim, display_name_claim, email_claim) can read
	// either an "extra" claim shipped by the OP (e.g. Entra ID's "upn",
	// available in claims.Claims) OR the typed OIDC standard claim shipped
	// under its JSON-tag key (e.g. "preferred_username", which the library
	// parses into the typed field and DROPS from claims.Claims).
	//
	// Both code paths converge on a single map[string]any so ClaimString can
	// uniformly serve either an "Entra-style" OP (sets "upn") or an
	// "OIDC-default" OP (sets "preferred_username") without duplicating
	// extraction logic in the caller.
	raw := make(map[string]any, len(claims.Claims)+8)
	for k, v := range claims.Claims {
		raw[k] = v
	}
	// Hoist typed standard claims under their JSON-tag keys. Empty strings
	// are skipped so ClaimString(...) of an unconfigured field stays "" —
	// otherwise an override pointing at "name" would resolve to "" but
	// override pointing at "missing" would also resolve to "", and we'd
	// lose the "claim genuinely absent" signal.
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
		TokenType:         tokens.TokenType,
		IDToken:           tokens.IDToken,
		AccessToken:       tokens.AccessToken,
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
	}, nil
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
		return nil, errors.New("federation/oidc: no userinfo endpoint resolved")
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
