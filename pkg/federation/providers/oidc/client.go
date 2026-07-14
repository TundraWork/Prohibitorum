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
// TokenEndpoint() so the Federator can compare against the per-state
// "expected_iss" without re-running discovery. We also re-check
// nonce and issuer on the decoded ID token claims; zitadel/oidc
// already enforces these against the discovery values, but the
// caller-supplied expectedIss/expectedNonce is the actual security
// boundary, and re-checking here is defense in depth.
package oidc

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/zitadel/oidc/v3/pkg/client/rp"
	"github.com/zitadel/oidc/v3/pkg/oidc"
	"golang.org/x/oauth2"

	federationcore "prohibitorum/pkg/federation"
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
// keys like Entra ID's "upn", and modes.go / federation.go read those
// via ClaimString(tokens.Raw, idp.UsernameClaim). The typed fields
// above are kept for backwards-compat and convenience — they remain the
// right place to read fields with no override knob (Subject, Issuer,
// EmailVerified, AMR, Nonce).
type Tokens struct {
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

// Client wraps a single configured upstream OIDC IdP.
//
// One Client instance corresponds to one row in upstream_idp: it holds
// the discovered endpoints, the configured allowlist, and the JWKS
// cache (which lives inside the embedded RelyingParty's verifier).
// A Client is safe for concurrent use by multiple goroutines — the
// underlying zitadel/oidc RelyingParty is goroutine-safe for read
// operations (CodeExchange, AuthURL).
type Client struct {
	rp            rp.RelyingParty
	issuer        string // snapshot at NewClient time
	tokenEndpoint string // snapshot at NewClient time
}

// NewClient constructs a Client by running OIDC discovery against
// discoveryIssuer and configuring an alg allowlist on the ID-token
// verifier. If allowedAlgs is nil, DefaultAllowedAlgs is used. Passing
// an empty (non-nil) slice is treated as "no algorithms allowed" by
// the underlying library and is almost certainly a caller bug; we
// surface that as an error here to fail fast.
//
// Discovery is performed exactly once during NewClient. Subsequent
// calls to Exchange reuse the cached endpoints and the JWKS cache
// managed by zitadel/oidc internally.
// allowPrivateNetwork, when true, disables the outbound client's dial-time
// internal-IP screen. Sourced from the per-IdP upstream_idp.allow_private_network
// column (default false) — set true only when the upstream issuer is a trusted
// IdP on a private/internal network.
func NewClient(
	ctx context.Context,
	clientID, clientSecret, redirectURI string,
	scopes []string,
	discoveryIssuer string,
	allowedAlgs []string,
	allowPrivateNetwork bool,
) (*Client, error) {
	if allowedAlgs == nil {
		allowedAlgs = DefaultAllowedAlgs()
	}
	if len(allowedAlgs) == 0 {
		return nil, errors.New("federation/oidc: allowedAlgs is empty; pass nil for defaults")
	}

	rpInst, err := rp.NewRelyingPartyOIDC(
		ctx,
		discoveryIssuer,
		clientID,
		clientSecret,
		redirectURI,
		scopes,
		// SSRF-hardened, size-capped outbound client for discovery / JWKS /
		// token-exchange. Without this, zitadel/oidc uses a bare default client
		// (no internal-IP screen, follows redirects, unbounded body) against the
		// operator-supplied — and publicly-triggerable — issuer URL. See
		// httpclient.go (audit follow-up N2 + N3).
		rp.WithHTTPClient(federationcore.NewOutboundHTTPClient(allowPrivateNetwork, 2<<20)),
		rp.WithVerifierOpts(
			rp.WithSupportedSigningAlgorithms(allowedAlgs...),
			// Thread the per-flow expected nonce through the
			// verifier via context. Exchange stashes the nonce
			// under nonceCtxKey before calling CodeExchange.
			rp.WithNonce(nonceFromCtx),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("federation/oidc: discovery failed for %q: %w", discoveryIssuer, err)
	}

	return &Client{
		rp:            rpInst,
		issuer:        rpInst.Issuer(),
		tokenEndpoint: rpInst.OAuthConfig().Endpoint.TokenURL,
	}, nil
}

// Issuer returns the issuer URL as it was reported by discovery at
// NewClient time. This is the value the Federator compares against
// the id_token's iss claim and against the per-state expected_iss
// blob. It is intentionally a snapshot: admin edits to upstream_idp
// must not retroactively change what an in-flight Exchange call
// considers a valid issuer.
func (c *Client) Issuer() string {
	return c.issuer
}

// TokenEndpoint returns the token endpoint URL snapshotted at
// NewClient time. Exposed for logging and for the Federator's
// mix-up-resistance bookkeeping; Exchange uses it internally via
// the embedded RelyingParty.
func (c *Client) TokenEndpoint() string {
	return c.tokenEndpoint
}

// AuthURL builds the upstream /authorize URL with PKCE (S256), state,
// and nonce parameters. The caller is responsible for generating
// state, nonce, and codeChallenge (the SHA-256 base64url-encoded
// transform of the verifier) and for persisting the corresponding
// codeVerifier alongside state in the KV.
//
// extra accepts additional oauth2.AuthCodeOption values appended after
// the base parameters. Callers that pass no extras get identical URLs as
// before (backward-compatible).
func (c *Client) AuthURL(state, nonce, codeChallenge string, extra ...oauth2.AuthCodeOption) string {
	opts := make([]rp.AuthURLOpt, 0, 2+len(extra))
	opts = append(opts, rp.WithCodeChallenge(codeChallenge))
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

	tokens, err := rp.CodeExchange[*oidc.IDTokenClaims](
		ctx,
		code,
		c.rp,
		rp.WithCodeVerifier(codeVerifier),
	)
	if err != nil {
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
func UserInfoToRaw(info *oidc.UserInfo) map[string]any {
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
	info, err := rp.Userinfo[*oidc.UserInfo](ctx, accessToken, oidc.BearerToken, subject, c.rp)
	if err != nil {
		return nil, fmt.Errorf("federation/oidc: userinfo: %w", err)
	}
	return UserInfoToRaw(info), nil
}
