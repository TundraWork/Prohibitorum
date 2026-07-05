// Package clientip resolves the effective client IP from an *http.Request under a
// configurable, spoof-resistant trust policy. A forwarding header (X-Forwarded-For
// or a single named header such as CF-Connecting-IP) is honored ONLY when the direct
// TCP peer is inside the configured trusted-proxy CIDR set; otherwise the peer address
// is used. This defeats header spoofing by clients that can reach the origin directly.
package clientip

import (
	"net"
	"net/http"
	"strings"
)

// Strategy selects how the client IP is extracted.
type Strategy string

const (
	// Direct ignores all forwarding headers and uses the TCP peer. Default.
	Direct Strategy = "direct"
	// Forwarded reads X-Forwarded-For, walking right-to-left past trusted proxies.
	Forwarded Strategy = "forwarded"
	// Header reads a single named header (e.g. CF-Connecting-IP) holding one IP.
	Header Strategy = "header"
)

// Config is the parsed, ready-to-use trust policy.
type Config struct {
	Strategy       Strategy
	Header         string       // header name for the Header strategy
	TrustedProxies []*net.IPNet // ranges permitted to set forwarding headers
}

// Extract returns the effective client IP as a bare host string (no port). It never
// panics; on any ambiguity it returns the direct peer host.
func Extract(r *http.Request, cfg Config) string {
	peer := peerHost(r.RemoteAddr)
	switch cfg.Strategy {
	case Header:
		if !trusted(peer, cfg.TrustedProxies) {
			return peer
		}
		if ip := parseHost(r.Header.Get(cfg.Header)); ip != "" {
			return ip
		}
		return peer
	case Forwarded:
		if !trusted(peer, cfg.TrustedProxies) {
			return peer
		}
		if c := clientFromXFF(r.Header.Get("X-Forwarded-For"), cfg.TrustedProxies); c != "" {
			return c
		}
		return peer
	default: // Direct and anything unrecognized
		return peer
	}
}

// peerHost strips the port from a RemoteAddr ("1.2.3.4:5678" -> "1.2.3.4",
// "[::1]:5678" -> "::1"). If there is no port it returns s trimmed of brackets.
func peerHost(s string) string {
	if host, _, err := net.SplitHostPort(s); err == nil {
		return host
	}
	return strings.Trim(s, "[]")
}

// trusted reports whether ip (bare host) parses and falls inside any CIDR.
func trusted(ip string, cidrs []*net.IPNet) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	for _, n := range cidrs {
		if n != nil && n.Contains(parsed) {
			return true
		}
	}
	return false
}

// parseHost validates a single-IP value (tolerating an "ip:port" form) and returns
// the canonical IP string, or "" if empty/invalid.
func parseHost(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(v); err == nil {
		v = host
	}
	if ip := net.ParseIP(strings.Trim(v, "[]")); ip != nil {
		return ip.String()
	}
	return ""
}

// clientFromXFF walks X-Forwarded-For right-to-left and returns the first entry that
// is a valid IP and NOT inside a trusted CIDR — the real client behind the trusted
// proxies. If every entry is trusted, it returns the leftmost valid entry. Returns
// "" when no valid entry exists.
func clientFromXFF(xff string, cidrs []*net.IPNet) string {
	parts := strings.Split(xff, ",")
	var leftmost string
	for i := len(parts) - 1; i >= 0; i-- {
		ip := parseHost(parts[i])
		if ip == "" {
			continue
		}
		leftmost = ip // walking left, the last valid one we see is the leftmost
		if !trusted(ip, cidrs) {
			return ip
		}
	}
	return leftmost
}
