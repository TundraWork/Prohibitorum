// Package server — handle_consent_test.go
//
// Unit tests for the pure helpers sameOriginAsIssuer and unionScopes in
// handle_consent.go. These are exercised here because the smoke tests only
// cover the happy-path browser flow and will not exercise the open-redirect
// negative cases or scope-deduplication edge cases.

package server

import (
	"reflect"
	"testing"

	"prohibitorum/pkg/configx"
)

func cfg(issuer string) *configx.Config {
	return &configx.Config{OIDC: configx.OIDCConfig{Issuer: issuer}}
}

func TestSameOriginAsIssuer(t *testing.T) {
	const issuer = "https://idp.example"

	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{
			name: "same origin with path and query",
			raw:  "https://idp.example/oauth/authorize?x=1",
			want: true,
		},
		{
			name: "bare issuer",
			raw:  "https://idp.example",
			want: true,
		},
		{
			name: "different port",
			raw:  "https://idp.example:8443/x",
			want: false,
		},
		{
			name: "scheme mismatch (http vs https)",
			raw:  "http://idp.example/x",
			want: false,
		},
		{
			name: "different host",
			raw:  "https://evil.com/x",
			want: false,
		},
		{
			name: "scheme-relative URL",
			raw:  "//evil.com/x",
			want: false,
		},
		{
			name: "userinfo trick (host is evil.com)",
			raw:  "https://idp.example@evil.com/x",
			want: false,
		},
		{
			name: "empty string",
			raw:  "",
			want: false,
		},
		{
			name: "relative URL (no scheme or host)",
			raw:  "/oauth/authorize",
			want: false,
		},
	}

	c := cfg(issuer)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sameOriginAsIssuer(tc.raw, c)
			if got != tc.want {
				t.Errorf("sameOriginAsIssuer(%q, cfg{issuer=%q}) = %v; want %v",
					tc.raw, issuer, got, tc.want)
			}
		})
	}
}

func TestUnionScopes(t *testing.T) {
	cases := []struct {
		name string
		a, b []string
		want []string
	}{
		{
			name: "dedup overlapping element",
			a:    []string{"openid"},
			b:    []string{"openid", "profile"},
			want: []string{"openid", "profile"},
		},
		{
			name: "nil first slice",
			a:    nil,
			b:    []string{"openid"},
			want: []string{"openid"},
		},
		{
			name: "partial overlap preserves order",
			a:    []string{"a", "b"},
			b:    []string{"b", "c"},
			want: []string{"a", "b", "c"},
		},
		{
			name: "nil second slice",
			a:    []string{"openid"},
			b:    nil,
			want: []string{"openid"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := unionScopes(tc.a, tc.b)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("unionScopes(%v, %v) = %v; want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}
