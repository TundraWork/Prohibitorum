package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
)

// smokeIdentityScopes checks an identity:<slug> scope against the account c is
// signed in as, which holds one linked identity at slug with upstream subject
// wantSub: the admin API refuses an identity scope for a provider that does not
// exist, discovery lists the scope, and both the ID token and /userinfo carry
// the identity. It returns the access token, so the caller can read /userinfo
// again after unlinking.
func smokeIdentityScopes(c *client, baseURL, slug, wantSub string) (string, error) {
	redirectURI := baseURL + "/identity-rp/callback"
	resp, err := c.postJSONRaw("/api/prohibitorum/oidc-applications", map[string]any{
		"clientId": "smoke-identity-unknown", "displayName": "Smoke identity RP",
		"redirectUris": []string{redirectURI}, "scopes": []string{"openid", "identity:does-not-exist"}, "public": false,
	})
	if err != nil {
		return "", err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		return "", fmt.Errorf("create with identity:does-not-exist: status %d, want 400", resp.StatusCode)
	}

	const clientID = "smoke-identity-rp"
	scope := "identity:" + slug
	var created struct {
		Secret string `json:"secret"`
	}
	if err := c.postJSON("/api/prohibitorum/oidc-applications", map[string]any{
		"clientId": clientID, "displayName": "Smoke identity RP",
		"redirectUris": []string{redirectURI}, "scopes": []string{"openid", scope}, "public": false,
	}, &created); err != nil {
		return "", fmt.Errorf("create with %s: %w", scope, err)
	}

	discovery, err := http.Get(baseURL + "/.well-known/openid-configuration")
	if err != nil {
		return "", err
	}
	var doc struct {
		ScopesSupported []string `json:"scopes_supported"`
	}
	err = json.NewDecoder(discovery.Body).Decode(&doc)
	discovery.Body.Close()
	if err != nil || !slices.Contains(doc.ScopesSupported, scope) {
		return "", fmt.Errorf("discovery scopes_supported = %v (err=%v), want %s", doc.ScopesSupported, err, scope)
	}

	verifier, challenge := genPKCE()
	state := randState()
	location, err := authorizeWithSession(c, fmt.Sprintf(
		"/oauth/authorize?response_type=code&client_id=%s&redirect_uri=%s&scope=%s&state=%s&nonce=%s&code_challenge=%s&code_challenge_method=S256",
		url.QueryEscape(clientID), url.QueryEscape(redirectURI), url.QueryEscape("openid "+scope),
		url.QueryEscape(state), url.QueryEscape(randState()), url.QueryEscape(challenge),
	))
	if err != nil {
		return "", fmt.Errorf("authorize: %w", err)
	}
	code, err := parseAuthorizeRedirect(location, redirectURI, state, baseURL)
	if err != nil {
		return "", err
	}
	tokens, err := tokenExchange(baseURL, clientID, created.Secret, url.Values{
		"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirectURI}, "code_verifier": {verifier},
	})
	if err != nil {
		return "", err
	}
	idClaims, err := verifyIDToken(baseURL, tokens.IDToken)
	if err != nil {
		return "", err
	}
	if subs, ok := identitySubs(idClaims, slug); !ok || !slices.Equal(subs, []string{wantSub}) {
		return "", fmt.Errorf("id_token identities = %v, want %s: [%s]", idClaims["identities"], slug, wantSub)
	}
	userinfo, err := fetchUserinfo(baseURL, tokens.AccessToken)
	if err != nil {
		return "", err
	}
	if subs, ok := identitySubs(userinfo, slug); !ok || !slices.Equal(subs, []string{wantSub}) {
		return "", fmt.Errorf("userinfo identities = %v, want %s: [%s]", userinfo["identities"], slug, wantSub)
	}
	return tokens.AccessToken, nil
}

// identitySubs returns the sub of each identity under identities.<slug>, and
// whether that key is present at all.
func identitySubs(claims map[string]any, slug string) ([]string, bool) {
	identities, _ := claims["identities"].(map[string]any)
	list, ok := identities[slug].([]any)
	if !ok {
		return nil, false
	}
	subs := []string{}
	for _, item := range list {
		identity, _ := item.(map[string]any)
		sub, _ := identity["sub"].(string)
		subs = append(subs, sub)
	}
	return subs, true
}
