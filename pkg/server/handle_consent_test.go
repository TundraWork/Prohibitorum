// Package server — handle_consent_test.go
//
// Unit tests for the pure helper unionScopes in handle_consent.go, and the
// cfg() helper shared with returnto_test.go. Open-redirect cases previously
// covered by TestSameOriginAsIssuer are now covered by TestValidateReturnTo
// in returnto_test.go (sameOriginAsIssuer was deleted; validateReturnTo is
// the shared successor).

package server

import (
	"reflect"
	"testing"

	"prohibitorum/pkg/configx"
)

func cfg(issuer string) *configx.Config {
	return &configx.Config{OIDC: configx.OIDCConfig{Issuer: issuer}}
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
