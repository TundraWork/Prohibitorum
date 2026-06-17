// Package server — returnto_test.go
//
// Table-driven tests for validateReturnTo (fail-soft wrapper around
// resolveReturnTo). The test matrix mirrors dashboard/src/lib/returnTo.test.ts
// (safeReturnTo) so client and server share the same security contract.
// The cfg() helper is defined in handle_consent_test.go (same package).

package server

import "testing"

func TestValidateReturnTo(t *testing.T) {
	c := cfg("https://idp.example")

	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty string defaults to /",
			input: "",
			want:  "/",
		},
		{
			name:  "relative path preserved",
			input: "/me/security",
			want:  "/me/security",
		},
		{
			name:  "relative path with query string",
			input: "/consent?ticket=abc",
			want:  "/consent?ticket=abc",
		},
		{
			name:  "same-origin absolute URL normalised to relative path with query",
			input: "https://idp.example/oauth/authorize?x=1",
			want:  "/oauth/authorize?x=1",
		},
		{
			name:  "same-origin absolute URL with nested encoded query preserved verbatim",
			input: "https://idp.example/oauth/authorize?client_id=x&redirect_uri=http%3A%2F%2Frp%2Fcb",
			want:  "/oauth/authorize?client_id=x&redirect_uri=http%3A%2F%2Frp%2Fcb",
		},
		{
			name:  "bare issuer (no path) normalised to /",
			input: "https://idp.example",
			want:  "/",
		},
		{
			name:  "cross-origin absolute URL rejected",
			input: "https://evil.test/x",
			want:  "/",
		},
		{
			name:  "scheme mismatch (http vs https) rejected",
			input: "http://idp.example/x",
			want:  "/",
		},
		{
			name:  "port mismatch rejected",
			input: "https://idp.example:8443/x",
			want:  "/",
		},
		{
			name:  "userinfo trick (real host evil.com) rejected",
			input: "https://idp.example@evil.com/x",
			want:  "/",
		},
		{
			name:  "protocol-relative // rejected",
			input: "//evil.test",
			want:  "/",
		},
		{
			name:  "javascript: scheme rejected",
			input: "javascript:alert(1)",
			want:  "/",
		},
		{
			name:  "data: scheme rejected",
			input: "data:text/html,x",
			want:  "/",
		},
		{
			name:  "backslash trick /\\evil.test rejected",
			input: `/\evil.test`,
			want:  "/",
		},

		// Hardening: inputs that LOOK like escapes but must stay on-origin or fall to "/".
		{
			// Encoded slashes are NOT path separators; the path stays on-origin
			// and resolveReturnTo returns it verbatim (proves no decode-then-leak).
			name:  "percent-encoded double slash stays on-origin",
			input: "/%2F%2Fevil.test",
			want:  "/%2F%2Fevil.test",
		},
		{
			// "@" is an ordinary path character in a relative ref; not treated as
			// userinfo separator — the path stays on-origin.
			name:  "@ in path is an ordinary char",
			input: "/@evil.com",
			want:  "/@evil.com",
		},
		{
			// Uppercase scheme: Go lowercases to "javascript", which ≠ "https"
			// → origin mismatch → fail-soft "/".
			name:  "uppercase JAVASCRIPT: scheme rejected",
			input: "JAVASCRIPT:alert(1)",
			want:  "/",
		},
		{
			// Leading TAB makes url.Parse return an error → fail-soft "/".
			name:  "leading TAB before // rejected",
			input: "\t//evil.test",
			want:  "/",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := validateReturnTo(tc.input, c)
			if got != tc.want {
				t.Errorf("validateReturnTo(%q) = %q; want %q", tc.input, got, tc.want)
			}
		})
	}

	// Nil-config fallback: absolute URLs are rejected (no issuer to match),
	// but relative paths resolve normally.
	t.Run("nil config relative path passes", func(t *testing.T) {
		got := validateReturnTo("/me", nil)
		if got != "/me" {
			t.Errorf("validateReturnTo(%q, nil) = %q; want %q", "/me", got, "/me")
		}
	})
	t.Run("nil config absolute URL rejected", func(t *testing.T) {
		got := validateReturnTo("https://idp.example/x", nil)
		if got != "/" {
			t.Errorf("validateReturnTo(%q, nil) = %q; want %q", "https://idp.example/x", got, "/")
		}
	})
}
