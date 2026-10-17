package main

import (
	"fmt"
	"net/url"
	"slices"
	"strings"
)

// requireIdentityRequest is what an application's require-identity request
// left for the in-between page.
type requireIdentityRequest struct {
	ticket      string
	entry       string // path of the entry request, relative to the base URL
	redirectURI string
}

// smokeRequireIdentityStart registers an application, sends c (signed in, with
// redirects not followed) to its require-identity entry for provider slug, and
// expects the in-between page, since c has no identity there yet.
func smokeRequireIdentityStart(c *client, baseURL, slug string) (requireIdentityRequest, error) {
	const clientID = "smoke-require-identity-rp"
	redirectURI := baseURL + "/require-identity-rp/after"
	if err := c.postJSON("/api/prohibitorum/oidc-applications", map[string]any{
		"clientId": clientID, "displayName": "Smoke require-identity RP",
		"redirectUris": []string{redirectURI}, "scopes": []string{"openid"}, "public": false,
	}, nil); err != nil {
		return requireIdentityRequest{}, fmt.Errorf("create client: %w", err)
	}
	entry := "/api/prohibitorum/require-identity?" + url.Values{
		"client_id": {clientID}, "redirect_uri": {redirectURI}, "providers": {slug, slug}, // listed twice: the server collapses duplicates
	}.Encode()
	loc, err := c.getRedirect(entry)
	if err != nil {
		return requireIdentityRequest{}, err
	}
	ticket, ok := strings.CutPrefix(loc, baseURL+"/require-identity/")
	if !ok || ticket == "" {
		return requireIdentityRequest{}, fmt.Errorf("entry redirected to %q, want the require-identity page", loc)
	}
	request := requireIdentityRequest{ticket: ticket, entry: entry, redirectURI: redirectURI}
	satisfied, providers, err := smokeRequireIdentityRead(c, ticket)
	if err != nil {
		return requireIdentityRequest{}, err
	}
	if satisfied || !slices.Equal(providers, []string{slug}) {
		return requireIdentityRequest{}, fmt.Errorf("ticket before linking: satisfied=%v providers=%v", satisfied, providers)
	}
	return request, nil
}

// smokeRequireIdentityRead reads the in-between page's data for a ticket.
func smokeRequireIdentityRead(c *client, ticket string) (bool, []string, error) {
	var out struct {
		Satisfied bool `json:"satisfied"`
		Providers []struct {
			Slug string `json:"slug"`
		} `json:"providers"`
	}
	if err := c.get("/api/prohibitorum/require-identity/"+url.PathEscape(ticket), &out); err != nil {
		return false, nil, fmt.Errorf("read ticket: %w", err)
	}
	var slugs []string
	for _, p := range out.Providers {
		slugs = append(slugs, p.Slug)
	}
	return out.Satisfied, slugs, nil
}

// smokeRequireIdentityFinish checks a request after the link: the ticket now
// reads satisfied, the entry goes straight back to the bare redirect_uri, and
// a hint that is not an ID token ends on the error page, not the application.
func smokeRequireIdentityFinish(c *client, request requireIdentityRequest) error {
	satisfied, _, err := smokeRequireIdentityRead(c, request.ticket)
	if err != nil {
		return err
	}
	if !satisfied {
		return fmt.Errorf("ticket after linking: not satisfied")
	}
	loc, err := c.getRedirect(request.entry)
	if err != nil {
		return err
	}
	if loc != request.redirectURI {
		return fmt.Errorf("entry after linking redirected to %q, want %q", loc, request.redirectURI)
	}
	loc, err = c.getRedirect(request.entry + "&id_token_hint=not-a-token")
	if err != nil {
		return err
	}
	if !strings.HasPrefix(loc, "/error?error=require_identity_invalid_id_token&") {
		return fmt.Errorf("a bad id_token_hint redirected to %q, want the error page", loc)
	}
	return nil
}
