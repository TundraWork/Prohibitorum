package oidc

import (
	"context"
	"encoding/json"
	"slices"
	"strings"

	"prohibitorum/pkg/db"
)

// identityClaim is one linked identity in the identities claim. Only these
// fields leave the OP; upstream_data is never passed through whole, so a
// protocol that stores more later cannot leak it into tokens by accident.
type identityClaim struct {
	Sub        string `json:"sub"`
	Name       string `json:"name,omitempty"`
	ProfileURL string `json:"profile_url,omitempty"`
}

// grantedIdentitySlugs returns the provider slugs of the identity:<slug>
// scopes in scope, deduplicated, in request order.
func grantedIdentitySlugs(scope []string) []string {
	var slugs []string
	for _, s := range scope {
		if slug, ok := IdentityScopeSlug(s); ok && !slices.Contains(slugs, slug) {
			slugs = append(slugs, slug)
		}
	}
	return slugs
}

// identitiesForScopes reads the identities claim for the identity:<slug>
// scopes in scope. It returns nil, without a query, when none was granted.
func (p *Provider) identitiesForScopes(ctx context.Context, accountID int32, scope []string) (map[string][]identityClaim, error) {
	slugs := grantedIdentitySlugs(scope)
	if len(slugs) == 0 {
		return nil, nil
	}
	rows, err := p.queries.ListConfirmedIdentitiesForProviders(ctx, db.ListConfirmedIdentitiesForProvidersParams{
		AccountID: accountID, Slugs: slugs,
	})
	if err != nil {
		return nil, err
	}
	return identitiesClaim(slugs, rows), nil
}

// identitiesClaim groups rows by provider. Every granted slug gets a key, so an
// application can tell "not linked" ([]) from "not granted" (no key).
func identitiesClaim(slugs []string, rows []db.ListConfirmedIdentitiesForProvidersRow) map[string][]identityClaim {
	out := make(map[string][]identityClaim, len(slugs))
	for _, slug := range slugs {
		out[slug] = []identityClaim{}
	}
	for _, row := range rows {
		if list, ok := out[row.IdpSlug]; ok {
			out[row.IdpSlug] = append(list, identityClaimFromRow(row))
		}
	}
	return out
}

// identityClaimFromRow maps the per-protocol upstream_data keys onto the claim:
// VRChat stores displayName/profileUrl, Steam personaName/profileUrl, OIDC
// nothing.
func identityClaimFromRow(row db.ListConfirmedIdentitiesForProvidersRow) identityClaim {
	var data map[string]any
	_ = json.Unmarshal(row.UpstreamData, &data)
	text := func(key string) string {
		value, _ := data[key].(string)
		return value
	}
	claim := identityClaim{Sub: row.UpstreamSub, Name: text("displayName")}
	if claim.Name == "" {
		claim.Name = text("personaName")
	}
	if profile := text("profileUrl"); strings.HasPrefix(profile, "https://") {
		claim.ProfileURL = profile
	}
	return claim
}
