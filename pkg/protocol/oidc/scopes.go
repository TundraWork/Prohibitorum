// Package oidc — scopes.go
//
// The closed scope vocabulary this OP understands. A scope string carries no
// inherent meaning: the OP can only act on a scope it has explicit handling
// logic for (claims released or token behaviour). So the set below is the
// SINGLE SOURCE OF TRUTH for everything that touches downstream scopes —
// OIDC discovery (scopes_supported), client allowed_scopes validation (admin
// API + CLI, create + update), and the /authorize request check — and a
// downstream client can neither register nor be granted anything outside it.
//
// Each supported scope maps to concrete handling:
//
//	openid          — base OIDC scope; gates id_token + the sub claim
//	profile         — name / preferred_username / picture / … (claims.go)
//	email           — email / email_verified (claims.go)
//	offline_access  — issue a refresh token (token.go)
//	groups          — group memberships (claims.go / userinfo.go)
//
// The upstream-federation side is deliberately NOT constrained by this set: in
// that direction the platform is a CLIENT to a foreign OP that owns its own
// scope vocabulary, the scope is merely forwarded (its meaning is bound later
// by the per-IdP claim mappings, not by scope-handling logic here), so
// upstream_idp.scopes is free-form by design.
package oidc

import (
	"fmt"
	"slices"
)

// SupportedScopes returns the OP's closed scope vocabulary, in discovery order.
// A function (not a var) so the set cannot be mutated process-wide by a caller
// — mirrors DefaultAllowedAlgs.
func SupportedScopes() []string {
	return []string{"openid", "profile", "email", "offline_access", "groups"}
}

// IsSupportedScope reports whether s is a scope the OP handles.
func IsSupportedScope(s string) bool {
	return slices.Contains(SupportedScopes(), s)
}

// ValidateAllowedScopes returns an error naming the first scope that is not in
// SupportedScopes. Used to keep a downstream client's allowed_scopes inside the
// OP's closed vocabulary at every write path (admin API, CLI create/update).
func ValidateAllowedScopes(scopes []string) error {
	for _, s := range scopes {
		if !IsSupportedScope(s) {
			return fmt.Errorf("oidc: unsupported scope %q (the OP only honors %v)", s, SupportedScopes())
		}
	}
	return nil
}
