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
package federation

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
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

// destinationClass is the pure result of classifying a single destination IP
// against the exhaustive special-use table. The dial policy is:
//   - destinationPublic: always allowed;
//   - destinationPrivate: allowed only for an IdP with allow_private_network;
//   - destinationAlwaysBlocked: never allowed, regardless of policy.
type destinationClass uint8

const (
	destinationPublic destinationClass = iota
	destinationPrivate
	destinationAlwaysBlocked
)

func (c destinationClass) String() string {
	switch c {
	case destinationPublic:
		return "public"
	case destinationPrivate:
		return "private"
	case destinationAlwaysBlocked:
		return "alwaysBlocked"
	}
	return "unknown"
}

// alwaysBlockedPrefixes is the exhaustive table of IANA special-use IPv4/IPv6
// ranges that the federation client must NEVER connect to, regardless of the
// per-IdP allow_private_network policy: link-local (which covers the
// 169.254.169.254 cloud-metadata address), multicast, unspecified,
// documentation, benchmark, reserved (240.0.0.0/4), and CGNAT
// (100.64.0.0/10, which is shared-internal and not a legitimate federation
// target). Loopback is intentionally NOT here — see privatePrefixes.
//
// Every entry is netip.Prefix so classification is a pure prefix-contains
// check over an unmapped netip.Addr — no net.IP allocations, no IsPrivate()
// surprises (Go's net.IP.IsPrivate does NOT cover CGNAT, documentation, or
// benchmark ranges; the explicit table closes those gaps).
var alwaysBlockedPrefixes = []netip.Prefix{
	// IPv4 link-local 169.254.0.0/16 — includes 169.254.169.254 metadata.
	netip.MustParsePrefix("169.254.0.0/16"),
	// IPv4 multicast 224.0.0.0/4.
	netip.MustParsePrefix("224.0.0.0/4"),
	// IPv4 unspecified 0.0.0.0/8 (covers 0.0.0.0; /8 is conservative but
	// matches IANA "This network on this host" reservation).
	netip.MustParsePrefix("0.0.0.0/8"),
	// IPv4 documentation 192.0.2.0/24, 198.51.100.0/24, 203.0.113.0/24.
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	// IPv4 benchmark 198.18.0.0/15.
	netip.MustParsePrefix("198.18.0.0/15"),
	// IPv4 reserved 240.0.0.0/4 (class E + 255.255.255.255 broadcast).
	netip.MustParsePrefix("240.0.0.0/4"),
	// IPv4 CGNAT 100.64.0.0/10 — shared address space, never a real target.
	netip.MustParsePrefix("100.64.0.0/10"),

	// IPv6 link-local fe80::/10.
	netip.MustParsePrefix("fe80::/10"),
	// IPv6 multicast ff00::/8.
	netip.MustParsePrefix("ff00::/8"),
	// IPv6 unspecified ::/128.
	netip.MustParsePrefix("::/128"),
	// IPv6 documentation 2001:db8::/32.
	netip.MustParsePrefix("2001:db8::/32"),
	// IPv6 IPv4-mapped ::ffff:0:0/96 — covers all IPv4-mapped special-use
	// forms once the address is unmapped to its v4 form; keep it as a
	// backstop so a v4-mapped v6 that did not unmap is still classified.
	netip.MustParsePrefix("::ffff:0:0/96"),
}

// privatePrefixes is the narrow set of ranges the per-IdP allow_private_network
// policy may reach: RFC1918, IPv6 ULA (fc00::/7), and loopback (127.0.0.0/8 +
// ::1/128). Loopback is classified private rather than alwaysBlocked so a
// trusted-internal IdP and the loopback-OP test infrastructure remain reachable
// when the operator opts in — private mode permits ONLY these ranges; every
// other special-use block (metadata, CGNAT, multicast, documentation, …)
// remains alwaysBlocked. Private mode is a narrow allowance, NOT a blanket
// bypass.
var privatePrefixes = []netip.Prefix{
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("fc00::/7"),
	// Loopback — permitted in private mode (loopback-OP / test infra), never
	// in production.
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("::1/128"),
}

// classifyDestination is the single pure classifier for an outbound
// destination IP. It unmaps IPv4-mapped IPv6 addresses before classification
// (so ::ffff:127.0.0.1 is treated as 127.0.0.1, not as a routable v6) and
// returns one of destinationPublic, destinationPrivate, or
// destinationAlwaysBlocked. The dial policy allows public always, private
// only for an IdP with allow_private_network, and never allows
// always-blocked.
func classifyDestination(addr netip.Addr) destinationClass {
	// Unmap IPv4-mapped IPv6 (::ffff:a.b.c.d) so the v4 prefix table applies.
	// netip.Addr.Unmap returns the v4 form for mapped addresses, unchanged
	// otherwise; this is the exact normalization the classifier needs.
	addr = addr.Unmap()

	// alwaysBlocked takes precedence over private: a CGNAT address is not a
	// legitimate federation target even if the IdP allows private networks.
	for _, p := range alwaysBlockedPrefixes {
		if p.Contains(addr) {
			return destinationAlwaysBlocked
		}
	}
	for _, p := range privatePrefixes {
		if p.Contains(addr) {
			return destinationPrivate
		}
	}
	return destinationPublic
}

// ipDestinationAllowed reports whether a resolved IP may be dialed under the
// given allow_private policy. Public is always allowed; private is allowed
// only when allowPrivate is true; alwaysBlocked is never allowed.
func ipDestinationAllowed(addr netip.Addr, allowPrivate bool) bool {
	switch classifyDestination(addr) {
	case destinationPublic:
		return true
	case destinationPrivate:
		return allowPrivate
	default:
		return false
	}
}

// isBlockedDialIP is retained as a thin wrapper for any legacy caller that
// still holds a net.IP. New code should call classifyDestination on a
// netip.Addr directly. Returns true for every non-public class (private
// included) so the production dial screen (allowPrivate=false) blocks both.
func isBlockedDialIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if a, ok := netip.AddrFromSlice(ip); ok {
		return !ipDestinationAllowed(a, false)
	}
	return true
}

// dnsResolver is the minimal DNS lookup interface the screened dial context
// needs, so tests can substitute a steering resolver without touching the real
// network. Mirrors net.Resolver.LookupNetIP.
type dnsResolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// netDefaultResolver adapts the standard library resolver to dnsResolver.
// LookupNetIP is only invoked when the dial address is a hostname (not an IP
// literal), so the zero-value net.Resolver is fine.
type netDefaultResolver struct{}

func (netDefaultResolver) LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error) {
	return net.DefaultResolver.LookupNetIP(ctx, network, host)
}

// screenDialControl builds a net.Dialer.Control hook that screens the IP
// literal in the dial address. The standard library invokes Control AFTER DNS
// resolution with each resolved IP:port, so this hook screens the real target
// on every connection — which is what defeats DNS rebinding and
// redirect-to-internal. It NEVER disables: allowPrivate only relaxes the
// policy to permit RFC1918/ULA, while loopback, link-local/metadata,
// multicast, unspecified, documentation, benchmark, reserved, and CGNAT
// remain blocked. A non-IP literal (which should never reach Control) fails
// closed.
func screenDialControl(allowPrivate bool) func(network, address string, c syscall.RawConn) error {
	return func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return fmt.Errorf("federation/oidc: malformed dial address %q: %w", address, err)
		}
		addr, parseErr := netip.ParseAddr(host)
		if parseErr != nil {
			// Control always runs after resolution; a non-IP literal here is
			// unexpected — fail closed.
			return fmt.Errorf("%w: non-IP literal %q", errBlockedDialTarget, host)
		}
		if !ipDestinationAllowed(addr, allowPrivate) {
			return fmt.Errorf("%w: %s", errBlockedDialTarget, addr)
		}
		return nil
	}
}

// screenedDialContext wraps a base DialContext so a hostname is resolved and
// EVERY DNS answer is screened before any connection is attempted. This makes
// the "every DNS answer checked on every hop" guarantee explicit: if any
// answer is not allowed under the policy, the entire dial is rejected (a
// poisoned or split-horizon response cannot slip a private IP through). IP
// literals skip the resolution step and delegate directly to base (Control
// screens them). The base dialer's Control hook remains as a
// belt-and-suspenders IP-literal backstop on the actual connect.
func screenedDialContext(
	allowPrivate bool,
	res dnsResolver,
	base func(ctx context.Context, network, address string) (net.Conn, error),
) func(ctx context.Context, network, address string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("federation/oidc: malformed dial address %q: %w", address, err)
		}
		// IP literal: delegate to base (the Control hook screens it).
		if _, parseErr := netip.ParseAddr(host); parseErr == nil {
			return base(ctx, network, address)
		}
		// Hostname: resolve and screen EVERY answer. The lookup context is
		// the dial context, so cancellation/deadline propagates.
		addrs, err := res.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, fmt.Errorf("%w: dns lookup for %q failed: %w", errBlockedDialTarget, host, err)
		}
		if len(addrs) == 0 {
			return nil, fmt.Errorf("%w: %q (no dns answers)", errBlockedDialTarget, host)
		}
		for _, addr := range addrs {
			if !ipDestinationAllowed(addr, allowPrivate) {
				return nil, fmt.Errorf("%w: %s resolves to %s", errBlockedDialTarget, host, addr)
			}
		}
		// All answers passed — dial each in turn until one connects (mirrors
		// the standard library's sequential fallback).
		var lastErr error
		for _, addr := range addrs {
			conn, err := base(ctx, network, net.JoinHostPort(addr.String(), port))
			if err == nil {
				return conn, nil
			}
			lastErr = err
		}
		return nil, lastErr
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

// hardenedHTTPClient builds the SSRF-aware, size-capped client described in
// the file header. A fresh client per NewClient is fine — there is one Client
// per upstream_idp row and discovery runs once per client construction. When
// allowPrivate is true the dial screen permits RFC1918/ULA private
// destinations (for deployments federating to a trusted internal IdP, and for
// tests against a loopback OP); loopback, link-local/metadata, multicast,
// unspecified, documentation, benchmark, reserved, and CGNAT remain blocked
// regardless. maxBytes caps every response body; callers pass
// maxFederationResponseBytes for federation metadata or maxAvatarFetchBytes
// for avatar fetches.
func hardenedHTTPClient(allowPrivate bool, maxBytes int64) *http.Client {
	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
		Control:   screenDialControl(allowPrivate),
	}
	transport := &http.Transport{
		// screenedDialContext resolves every hostname and screens EVERY DNS
		// answer before connecting; IP literals are screened by the dialer's
		// Control hook. The two layers together guarantee every DNS answer
		// AND every actual dial address is checked on every hop.
		DialContext:           screenedDialContext(allowPrivate, netDefaultResolver{}, dialer.DialContext),
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
