//go:build smoke

package steam

import "testing"

func TestSmokeAllowsOnlyLoopbackOverrideEndpoints(t *testing.T) {
	oldLogin, oldSummary := loginEndpoint, summaryEndpoint
	t.Cleanup(func() { loginEndpoint, summaryEndpoint = oldLogin, oldSummary })
	loginEndpoint = "http://127.0.0.1:18099/openid/login"
	summaryEndpoint = "http://127.0.0.1:18099/ISteamUser/GetPlayerSummaries/v2/"
	if !smokeAllowPrivateEndpoints(false) {
		t.Fatal("loopback smoke endpoints were not enabled")
	}
	loginEndpoint = "https://steamcommunity.com/openid/login"
	if smokeAllowPrivateEndpoints(false) {
		t.Fatal("mixed production/loopback endpoints must not disable SSRF policy")
	}
}
