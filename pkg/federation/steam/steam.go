// Package steam implements the narrow slice of Steam's OpenID 2.0 login flow needed
// to authenticate a user and read their public profile. Steam does NOT speak
// OAuth2/OIDC — this is a hand-rolled adapter (a general OpenID 2.0 library is
// deliberately avoided: several have documented Claimed-ID spoofing CVEs, and we
// need only this one flow). Verification delegates to Steam's own
// check_authentication endpoint; we never re-implement the DH signature.
package steam

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// Issuer is the fixed pseudo-issuer stored as account_identity.upstream_iss for
// Steam identities (Steam has no OIDC issuer). Paired with the SteamID64 subject
// under UNIQUE(upstream_iss, upstream_sub).
const Issuer = "https://steamcommunity.com/openid"

const (
	nsOpenID2        = "http://specs.openid.net/auth/2.0"
	identifierSelect = "http://specs.openid.net/auth/2.0/identifier_select"
)

// Endpoints are package vars (not consts) so tests can point them at an httptest
// server via export_test.go. Production values are Steam's real endpoints.
var (
	loginEndpoint   = "https://steamcommunity.com/openid/login"
	summaryEndpoint = "https://api.steampowered.com/ISteamUser/GetPlayerSummaries/v2/"
)

// claimedIDRe anchors the Claimed-ID to Steam's exact format so a look-alike host or
// a non-numeric id cannot pass. THE anti-spoofing control (paired with
// check_authentication). A SteamID64 is exactly 17 digits.
var claimedIDRe = regexp.MustCompile(`^https://steamcommunity\.com/openid/id/(\d{17})$`)

// BuildAuthURL builds the OpenID 2.0 checkid_setup redirect. realm is the origin
// (e.g. "https://idp.example.com"); returnTo is the callback URL (must be under
// realm) carrying our state token as a query param.
func BuildAuthURL(realm, returnTo string) string {
	q := url.Values{
		"openid.ns":         {nsOpenID2},
		"openid.mode":       {"checkid_setup"},
		"openid.return_to":  {returnTo},
		"openid.realm":      {realm},
		"openid.identity":   {identifierSelect},
		"openid.claimed_id": {identifierSelect},
	}
	return loginEndpoint + "?" + q.Encode()
}

// Verify validates an OpenID 2.0 id_res callback and returns the SteamID64. params
// are the raw callback query values; expectedReturnTo is the exact openid.return_to
// we sent at begin (bound to our state token) — a mismatch is rejected before we
// contact Steam.
func Verify(ctx context.Context, hc *http.Client, params url.Values, expectedReturnTo string) (string, error) {
	if params.Get("openid.mode") != "id_res" {
		return "", fmt.Errorf("steam: unexpected openid.mode %q", params.Get("openid.mode"))
	}
	if params.Get("openid.return_to") != expectedReturnTo {
		return "", errors.New("steam: openid.return_to mismatch")
	}
	// Ask Steam to authenticate the assertion (mode=check_authentication). We echo
	// every openid.* param back verbatim, only flipping the mode.
	check := url.Values{}
	for k, v := range params {
		check[k] = v
	}
	check.Set("openid.mode", "check_authentication")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, loginEndpoint, strings.NewReader(check.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("steam: check_authentication: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return "", err
	}
	if !isValid(string(body)) {
		return "", errors.New("steam: check_authentication returned is_valid:false")
	}
	m := claimedIDRe.FindStringSubmatch(params.Get("openid.claimed_id"))
	if m == nil {
		return "", errors.New("steam: claimed_id did not match the Steam identifier format")
	}
	return m[1], nil
}

// isValid parses the key-value OpenID 2.0 response body for is_valid:true.
func isValid(body string) bool {
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) == "is_valid:true" {
			return true
		}
	}
	return false
}
