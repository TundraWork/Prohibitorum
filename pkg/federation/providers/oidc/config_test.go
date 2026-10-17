package oidc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testConfig(t *testing.T) Config {
	t.Helper()
	c, err := decodeConfig([]byte(validDefinitionConfig))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestConfigRejectsInvalidSchemaAndCombinations(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(map[string]any)
	}{
		{"manual missing endpoints", func(c map[string]any) {
			c["configurationMode"] = "manual"
			c["tokenAuthMethod"] = "client_secret_basic"
		}},
		{"manual discovery auth", func(c map[string]any) { c["configurationMode"] = "manual" }},
		{"public plain", func(c map[string]any) { c["tokenAuthMethod"] = "none"; c["pkceMethod"] = "plain" }},
		{"public off", func(c map[string]any) { c["tokenAuthMethod"] = "none"; c["pkceMethod"] = "off" }},
		{"null boolean", func(c map[string]any) { c["allowPrivateNetwork"] = nil }},
		{"empty endpoint", func(c map[string]any) { c["endpoints"].(map[string]any)["token"] = "" }},
		{"bad query", func(c map[string]any) { c["endpoints"].(map[string]any)["token"] = "https://idp.example/token?q=%zz" }},
		{"fragment", func(c map[string]any) { c["endpoints"].(map[string]any)["jwks"] = "https://idp.example/keys#key" }},
		{"credentials", func(c map[string]any) {
			c["endpoints"].(map[string]any)["token"] = "https://user:secret@idp.example/token"
		}},
		{"unknown endpoint", func(c map[string]any) { c["endpoints"].(map[string]any)["extra"] = nil }},
		{"missing field", func(c map[string]any) { delete(c, "pkceMethod") }},
		{"unknown field", func(c map[string]any) { c["responseMode"] = "form_post" }},
		{"unknown response type", func(c map[string]any) { c["responseType"] = "code id_token" }},
		{"null response type", func(c map[string]any) { c["responseType"] = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var c map[string]any
			_ = json.Unmarshal([]byte(validDefinitionConfig), &c)
			tc.edit(c)
			raw, _ := json.Marshal(c)
			if err := (Definition{}).ValidateConfig(raw); err == nil {
				t.Fatal("accepted invalid config")
			}
		})
	}
	for _, raw := range []string{strings.Replace(validDefinitionConfig, "\"issuerUrl\":", "\"pkceMethod\":\"off\",\"issuerUrl\":", 1), strings.Replace(validDefinitionConfig, "\"authorization\":null", "\"authorization\":null,\"authorization\":null", 1)} {
		if err := (Definition{}).ValidateConfig([]byte(raw)); err == nil {
			t.Fatal("accepted duplicate field")
		}
	}
}

// TestConfigResponseTypeRules pins which sign-in responses a configuration
// may carry: the setting may be absent, and ID token only is refused where no
// ID token can be issued (no openid scope) or verified (JWKS none).
func TestConfigResponseTypeRules(t *testing.T) {
	edit := func(f func(map[string]any)) []byte {
		var c map[string]any
		_ = json.Unmarshal([]byte(validDefinitionConfig), &c)
		f(c)
		raw, _ := json.Marshal(c)
		return raw
	}
	for _, value := range []any{nil, "code", "id_token"} {
		raw := edit(func(c map[string]any) {
			if value != nil {
				c["responseType"] = value
			}
		})
		if err := (Definition{}).ValidateConfig(raw); err != nil {
			t.Fatalf("responseType %v refused: %v", value, err)
		}
	}
	for _, tc := range []struct {
		name, want string
		edit       func(map[string]any)
	}{
		{"id_token without openid", "requires the openid scope", func(c map[string]any) {
			c["responseType"] = "id_token"
			c["scopes"] = []string{"profile"}
		}},
		{"id_token with jwks none", "cannot use JWKS none", func(c map[string]any) {
			c["responseType"] = "id_token"
			c["endpoints"].(map[string]any)["jwks"] = "none"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := (Definition{}).ValidateConfig(edit(tc.edit))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ValidateConfig = %v, want an error naming %q", err, tc.want)
			}
		})
	}
	// The code flow does not need an ID token, so the same scopes stay valid
	// there.
	if err := (Definition{}).ValidateConfig(edit(func(c map[string]any) {
		c["responseType"] = "code"
		c["scopes"] = []string{"profile"}
	})); err != nil {
		t.Fatalf("code flow without openid refused: %v", err)
	}
}

// TestConfigAuthenticateAvatarFetch pins the avatar-authentication switch:
// optional (absent means anonymous fetch), boolean, and covered by the
// unknown-field screen like every other key.
func TestConfigAuthenticateAvatarFetch(t *testing.T) {
	edit := func(f func(map[string]any)) []byte {
		var c map[string]any
		_ = json.Unmarshal([]byte(validDefinitionConfig), &c)
		f(c)
		raw, _ := json.Marshal(c)
		return raw
	}
	for _, tc := range []struct {
		name  string
		value any // nil leaves the key out entirely
		want  bool
	}{
		{"absent", nil, false},
		{"true", true, true},
		{"false", false, false},
	} {
		raw := edit(func(c map[string]any) {
			if tc.value != nil {
				c["authenticateAvatarFetch"] = tc.value
			}
		})
		config, err := decodeConfig(raw)
		if err != nil {
			t.Fatalf("authenticateAvatarFetch %s refused: %v", tc.name, err)
		}
		if config.AuthenticateAvatarFetch != tc.want {
			t.Fatalf("authenticateAvatarFetch %s decoded as %v", tc.name, config.AuthenticateAvatarFetch)
		}
	}
	for name, value := range map[string]any{"string": "true", "number": 1, "null": nil} {
		if err := (Definition{}).ValidateConfig(edit(func(c map[string]any) { c["authenticateAvatarFetch"] = value })); err == nil {
			t.Fatalf("accepted %s authenticateAvatarFetch", name)
		}
	}
}

func TestResolveConfigDiscoveryAndOverrides(t *testing.T) {
	for _, tc := range []struct {
		name    string
		methods any
		want    string
		bad     bool
	}{
		{"absent defaults basic", nil, "client_secret_basic", false},
		{"post only", []string{"client_secret_post"}, "client_secret_post", false},
		{"basic preferred", []string{"client_secret_post", "client_secret_basic"}, "client_secret_basic", false},
		{"unsupported", []string{"private_key_jwt"}, "", true},
		{"empty", []string{}, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var base string
			calls := 0
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != "/.well-known/openid-configuration" {
					t.Error("unexpected discovery path")
				}
				doc := map[string]any{"issuer": base, "authorization_endpoint": base + "/authorize", "token_endpoint": base + "/token", "jwks_uri": base + "/keys"}
				if tc.methods != nil {
					doc["token_endpoint_auth_methods_supported"] = tc.methods
				}
				_ = json.NewEncoder(w).Encode(doc)
			}))
			defer ts.Close()
			base = ts.URL
			c := testConfig(t)
			c.IssuerURL = base
			c.AllowPrivateNetwork = true
			c.Endpoints.Token = new(base + "/custom-token")
			resolved, err := ResolveConfig(context.Background(), c)
			if (err != nil) != tc.bad {
				t.Fatalf("resolve: %v", err)
			}
			if tc.bad {
				return
			}
			if resolved.TokenEndpoint != *c.Endpoints.Token || resolved.TokenAuthMethod != tc.want || resolved.Sources["tokenEndpoint"] != "override" || resolved.Sources["authorizationEndpoint"] != "discovery" || resolved.FetchedAt.IsZero() {
				t.Fatalf("resolved = %+v", resolved)
			}
			_, err = ResolveConfig(context.Background(), c)
			if err != nil || calls != 2 {
				t.Fatalf("fresh read: calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestResolveConfigManualMakesNoDiscoveryRequest(t *testing.T) {
	calls := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(500) }))
	defer ts.Close()
	c := testConfig(t)
	c.IssuerURL = ts.URL
	c.AllowPrivateNetwork = true
	c.ConfigurationMode = "manual"
	c.TokenAuthMethod = "none"
	c.Endpoints = Endpoints{Authorization: new(ts.URL + "/auth"), Token: new(ts.URL + "/token"), JWKS: new(ts.URL + "/keys")}
	r, err := ResolveConfig(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 0 || r.UserInfoEndpoint != "" || r.Sources["tokenEndpoint"] != "manual" {
		t.Fatalf("calls=%d resolved=%+v", calls, r)
	}
}

func TestResolveConfigRejectsDiscoveredIssuerMismatch(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"issuer":"https://other.example"}`))
	}))
	defer ts.Close()
	c := testConfig(t)
	c.IssuerURL = ts.URL
	c.AllowPrivateNetwork = true
	if _, err := ResolveConfig(context.Background(), c); err == nil {
		t.Fatal("accepted mismatched discovery issuer")
	}
}
