// Package oidc — httpclient.go
//
// hardenedHTTPClient is the single outbound HTTP client every upstream-OIDC
// federation fetch rides on: OIDC discovery
// ({issuer}/.well-known/openid-configuration), the JWKS fetch, and the
// authorization-code token-exchange POST. It is injected into the zitadel/oidc
// RelyingParty via rp.WithHTTPClient in NewClient.
//
// Why it exists (audit follow-up N2 + N3): the issuer URL is operator-supplied
// AND the fetch trigger is unauthenticated (GET /federation/{slug}/login and
// /callback are public). Without hardening, an issuer of
// http://169.254.169.254/… or an https:// issuer that 302s discovery to an
// internal address turns the federation path into an SSRF primitive (cloud
// metadata exfil / internal port scan), and an unbounded io.ReadAll on the
// response body turns a malicious "IdP" into an OOM DoS. This client closes
// both:
//
//   - A dial-time IP screen rejects loopback / RFC1918+ULA / link-local /
//     metadata IPs. Because it screens the RESOLVED IP on every connection
//     (including redirected hops), it defeats DNS rebinding and
//     redirect-to-internal without a brittle host allowlist, and it transitively
//     re-screens the discovery-returned token_endpoint / jwks_uri hosts.
//   - A response-body size cap bounds memory per fetch (and transitively the
//     JWKS key count, so no separate key-count cap is needed).
//   - A redirect-hop cap and an overall timeout bound the rest.
package oidc

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"syscall"
	"time"
)

const (
	// maxFederationResponseBytes caps any single upstream response body.
	// Discovery docs and JWKS sets are a few KB; 2 MiB is generous headroom
	// while still bounding a malicious multi-GB body (N3 OOM defense).
	maxFederationResponseBytes = 2 << 20 // 2 MiB

	// federationHTTPTimeout bounds the wall-clock of an entire fetch
	// (connect + TLS + headers + body). The dialer carries its own shorter
	// connect timeout.
	federationHTTPTimeout = 30 * time.Second

	// maxFederationRedirects caps redirect hops. Each hop is re-screened by the
	// dialer, so this is a belt-and-suspenders bound, not the security boundary.
	maxFederationRedirects = 5
)

// errBlockedDialTarget is returned by the dialer's Control hook when a resolved
// IP falls in a blocked range. Surfaced through the http.Client as a dial
// error, which the federation layer collapses onto ErrFederationStateInvalid.
var errBlockedDialTarget = errors.New("federation/oidc: refusing to dial blocked (internal/metadata) address")

// isBlockedDialIP reports whether ip is in a range the federation client must
// never connect to: loopback, RFC1918 / ULA private, link-local unicast
// (which covers 169.254.169.254 and fe80::/10) and multicast, the unspecified
// address, and multicast. IPv4-mapped IPv6 is normalized by net.IP's methods.
func isBlockedDialIP(ip net.IP) bool {
	return ip == nil ||
		ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() ||
		ip.IsUnspecified()
}

// screenDialControl builds a net.Dialer.Control hook. The hook runs AFTER DNS
// resolution with the concrete address (ip:port) about to be connected, so it
// screens the real target on every connection — which is exactly what defeats
// DNS rebinding and redirect-to-internal. When allowPrivate is true the screen
// is disabled (trusted-internal-IdP deployments + tests against a loopback OP).
func screenDialControl(allowPrivate bool) func(network, address string, c syscall.RawConn) error {
	return func(_, address string, _ syscall.RawConn) error {
		if allowPrivate {
			return nil
		}
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return fmt.Errorf("federation/oidc: malformed dial address %q: %w", address, err)
		}
		ip := net.ParseIP(host)
		if ip == nil {
			// Control is invoked with a resolved IP literal; a non-IP here is
			// unexpected — fail closed.
			return fmt.Errorf("%w: %q", errBlockedDialTarget, host)
		}
		if isBlockedDialIP(ip) {
			return fmt.Errorf("%w: %s", errBlockedDialTarget, ip)
		}
		return nil
	}
}

// cappingTransport wraps a RoundTripper so every response body is bounded by
// max bytes. It also rejects an over-cap declared Content-Length up front
// (cheap fail-fast before reading).
type cappingTransport struct {
	base http.RoundTripper
	max  int64
}

func (t cappingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	if resp.ContentLength > t.max {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("federation/oidc: upstream response too large (%d bytes)", resp.ContentLength)
	}
	resp.Body = &cappedBody{r: io.LimitReader(resp.Body, t.max+1), c: resp.Body, max: t.max}
	return resp, nil
}

// cappedBody enforces the byte cap as the body is read: any read that would
// push the total past the cap returns an error rather than silently truncating
// (a truncated discovery/JWKS doc must fail, not parse partially).
type cappedBody struct {
	r    io.Reader
	c    io.Closer
	read int64
	max  int64
}

func (b *cappedBody) Read(p []byte) (int, error) {
	n, err := b.r.Read(p)
	b.read += int64(n)
	if b.read > b.max {
		return n, fmt.Errorf("federation/oidc: upstream response exceeded %d-byte cap", b.max)
	}
	return n, err
}

func (b *cappedBody) Close() error { return b.c.Close() }

// hardenedHTTPClient builds the SSRF-aware, size-capped client described in the
// file header. A fresh client per NewClient is fine — there is one Client per
// upstream_idp row and discovery runs once per client construction. When
// allowPrivate is true the dial-time internal-IP screen is disabled (for
// deployments federating to a trusted internal IdP, and for tests).
// maxBytes caps every response body; callers pass maxFederationResponseBytes
// for federation metadata or maxAvatarFetchBytes for avatar fetches.
func hardenedHTTPClient(allowPrivate bool, maxBytes int64) *http.Client {
	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
		Control:   screenDialControl(allowPrivate),
	}
	transport := &http.Transport{
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	return &http.Client{
		Transport: cappingTransport{base: transport, max: maxBytes},
		Timeout:   federationHTTPTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxFederationRedirects {
				return fmt.Errorf("federation/oidc: too many redirects (>%d)", maxFederationRedirects)
			}
			return validateRedirectScheme(req, via, allowPrivate)
		},
	}
}

// validateRedirectScheme enforces the per-hop policy on followed redirects.
// A redirect may never downgrade a hop from https to plaintext http — not in
// production, and not in allowPrivate (trusted-internal / test) mode, where
// http is only ever permitted when the immediately preceding hop was itself
// http. A redirect to any scheme other than http/https is always rejected.
//
// In production (!allowPrivate) the redirect target must additionally satisfy
// the single outbound-URL policy (validateOutboundURL): https-only, no IP
// literal, no userinfo. This is a fail-fast before the dial-time resolved-IP
// screen connects to the redirect target, and it makes redirect-to-internal
// deterministic. The dial screen remains the runtime backstop and still
// screens every hop regardless of allowPrivate.
func validateRedirectScheme(req *http.Request, via []*http.Request, allowPrivate bool) error {
	if req == nil || req.URL == nil {
		return errors.New("federation/oidc: redirect target missing URL")
	}
	target := req.URL.Scheme
	if target != "http" && target != "https" {
		return fmt.Errorf("federation/oidc: redirect to non-http(s) scheme %q refused", target)
	}
	// Production: the redirect target must satisfy the single outbound-URL
	// policy (https-only, domain host, no IP literal, no userinfo). This
	// fail-fast rejects redirect-to-internal/IP-literal before dialing.
	if !allowPrivate {
		if err := validateOutboundURL(req.URL.String(), "redirect target"); err != nil {
			return err
		}
		return nil
	}
	// allowPrivate: http is only permitted when the previous hop was http (no
	// downgrade). https hops are always fine.
	if target == "https" {
		return nil
	}
	prevScheme := ""
	if len(via) > 0 && via[len(via)-1] != nil && via[len(via)-1].URL != nil {
		prevScheme = via[len(via)-1].URL.Scheme
	}
	if prevScheme != "http" {
		return errors.New("federation/oidc: refusing http redirect downgrade from an https request")
	}
	return nil
}

// ValidateIssuerURL enforces the operator-facing rules for an upstream_idp
// issuer_url at create/update time (audit follow-up N2): it must be a parseable
// absolute https:// URL with a non-empty host that is NOT an IP literal and
// carries no userinfo. The dial-time screen is the runtime backstop; this is
// the fail-fast that stops an obviously-internal or plaintext issuer from ever
// being stored.
func ValidateIssuerURL(raw string) error {
	return validateOutboundURL(raw, "issuer_url")
}

// ValidateOutboundURL enforces the same operator-facing rules as
// ValidateIssuerURL but with a generic label, so non-issuer outbound fetches
// (e.g. the CLI `saml-sp create --metadata-url` fetch) reuse the single
// outbound policy rather than a divergent copy. It must be a parseable
// absolute https:// URL with a non-empty, non-IP-literal domain host and no
// userinfo.
func ValidateOutboundURL(raw string) error {
	return validateOutboundURL(raw, "metadata url")
}

// validateOutboundURL is the single shared outbound-URL policy. label appears
// in error messages so callers get a meaningful identifier without a second,
// divergent validation implementation.
func validateOutboundURL(raw, label string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("federation/oidc: %s is not a valid URL: %w", label, err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("federation/oidc: %s must use https", label)
	}
	if u.User != nil {
		return fmt.Errorf("federation/oidc: %s must not contain userinfo", label)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("federation/oidc: %s must have a host", label)
	}
	if net.ParseIP(host) != nil {
		return fmt.Errorf("federation/oidc: %s host must be a domain name, not an IP literal", label)
	}
	if u.Host == "" || !u.IsAbs() {
		return fmt.Errorf("federation/oidc: %s must be an absolute URL", label)
	}
	return nil
}

// NewOutboundHTTPClient returns the same SSRF-aware, redirect-scheme-checked,
// size-capped outbound HTTP client used by federation/avatar fetches, for
// reuse by operator-facing CLI fetches (e.g. `saml-sp create --metadata-url`).
// This is the single hardened client/policy: it shares the dial-time
// resolved-IP screen, the per-hop redirect scheme + hop-cap policy, and the
// response-body size cap with the federation path, so the CLI does not
// introduce a second, divergent HTTP security policy.
// allowPrivate should be false for production CLI fetches (the dial screen
// rejects internal/metadata IPs); tests pass true to reach a loopback server.
// maxBytes caps every response body.
func NewOutboundHTTPClient(allowPrivate bool, maxBytes int64) *http.Client {
	return hardenedHTTPClient(allowPrivate, maxBytes)
}
