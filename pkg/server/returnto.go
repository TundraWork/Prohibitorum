package server

import (
	"net/url"
	"strings"

	"prohibitorum/pkg/configx"
)

// resolveReturnTo is the single same-origin source of truth for every
// return_to consumer. Given untrusted input and the configured issuer it
// reports the safe, same-origin RELATIVE path to navigate to and whether the
// input was usable. ok=false means the value is empty-unsafe or off-origin and
// the caller must fall back ("/" for fail-soft callers, or an error for
// fail-closed callers). It is the Go twin of the client safeReturnTo.
//
// Accepts: a same-origin absolute URL (scheme+host == issuer) OR a
// path-absolute relative ref ("/x"). Rejects: cross-origin, protocol-relative
// ("//", and the "\"-normalised "/\", "\\" tricks a browser reads as "//"),
// non-http schemes (javascript:/data:), userinfo/port origin deviations, and
// any result that resolves protocol-relative. Empty input → ("/", true).
// nil/empty-issuer config → absolute URLs cannot match and are rejected;
// relative paths still resolve (preserves the nil-config federation behaviour).
func resolveReturnTo(raw string, cfg *configx.Config) (string, bool) {
	// 1. Empty → safe default.
	if raw == "" {
		return "/", true
	}

	// 2. Reject protocol-relative including the backslash normalisation tricks:
	// //evil, /\evil, \\evil, \/evil — a browser normalises \ → / on navigation.
	if strings.HasPrefix(strings.ReplaceAll(raw, "\\", "/"), "//") {
		return "", false
	}

	// 3. Parse the input.
	u, err := url.Parse(raw)
	if err != nil {
		return "", false
	}

	// 4. Resolve the issuer origin defensively — only when config and issuer
	// are both present and parseable.
	var issuer *url.URL
	if cfg != nil && cfg.OIDC.Issuer != "" {
		if iss, ierr := url.Parse(cfg.OIDC.Issuer); ierr == nil {
			issuer = iss
		}
	}

	// 5. Absolute URL: require a known issuer AND scheme+host match.
	// This rejects javascript:/data: via scheme mismatch, cross-origin, port
	// deviations, and userinfo tricks (Go's url.Parse puts userinfo outside
	// u.Host, so "https://idp.example@evil.com/x" has u.Host == "evil.com").
	if u.IsAbs() {
		if issuer == nil {
			return "", false
		}
		if u.Scheme != issuer.Scheme || u.Host != issuer.Host {
			return "", false
		}
	} else {
		// 6. Relative ref: must be path-absolute (starts with "/") and not
		// protocol-relative (no host component).
		if u.Host != "" || !strings.HasPrefix(raw, "/") {
			return "", false
		}
	}

	// 7. Build the relative path from PARSED components (never from raw, so a
	// host cannot leak back into the result).
	path := u.EscapedPath()
	if path == "" {
		path = "/"
	}
	if u.RawQuery != "" {
		path += "?" + u.RawQuery
	}
	if u.Fragment != "" {
		path += "#" + u.EscapedFragment()
	}

	// 8. Final guard: result must be path-absolute and not protocol-relative.
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return "", false
	}

	// 9. Safe.
	return path, true
}

// validateReturnTo is the fail-soft policy wrapper used by the login ceremony
// and the consent decision: a safe same-origin relative path, or "/" for any
// empty/unsafe input. Never errors. Single source of truth shared with
// validateFederationReturnTo (which applies a fail-closed policy over the same
// resolveReturnTo core).
func validateReturnTo(raw string, cfg *configx.Config) string {
	if p, ok := resolveReturnTo(raw, cfg); ok {
		return p
	}
	return "/"
}
