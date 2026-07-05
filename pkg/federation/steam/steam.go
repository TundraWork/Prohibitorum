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
	"os"
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

// init allows tests and CI to override Steam endpoints via environment variables.
// PROHIBITORUM_STEAM_LOGIN_ENDPOINT overrides loginEndpoint (the OpenID 2.0 endpoint).
// PROHIBITORUM_STEAM_SUMMARY_ENDPOINT overrides summaryEndpoint (the Web API base URL).
// The test export_test.go SetEndpoints seam still works; env vars are read once at startup.
func init() {
	if v := os.Getenv("PROHIBITORUM_STEAM_LOGIN_ENDPOINT"); v != "" {
		loginEndpoint = v
	}
	if v := os.Getenv("PROHIBITORUM_STEAM_SUMMARY_ENDPOINT"); v != "" {
		summaryEndpoint = v
	}
}

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
	// OpenID 2.0 §11.4.2: is_valid:true only attests the fields listed in
	// openid.signed are covered by the signature — it does NOT guarantee
	// claimed_id/identity are among them. An OP could validly sign a response
	// that omits identity fields, letting an attacker substitute an arbitrary
	// SteamID. Confirm the identity fields are signed before trusting them.
	if !signedCovers(params.Get("openid.signed"), "claimed_id", "identity") {
		return "", errors.New("steam: openid.signed does not cover claimed_id/identity")
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

// signedCovers reports whether the comma-separated openid.signed list contains
// every required field. Steam's is_valid:true only attests the fields in this
// list are signed; the RP MUST confirm the identity fields are among them
// (OpenID 2.0 §11.4.2) or a validly-signed-but-identity-unsigned assertion could
// authenticate an attacker-chosen SteamID.
func signedCovers(signed string, required ...string) bool {
	have := make(map[string]bool)
	for _, f := range strings.Split(signed, ",") {
		have[strings.TrimSpace(f)] = true
	}
	for _, r := range required {
		if !have[r] {
			return false
		}
	}
	return true
}
