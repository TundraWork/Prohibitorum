package oidc

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"prohibitorum/cmd/smoke/mockop"
	federationcore "prohibitorum/pkg/federation"
)

func TestClientExplicitAuthenticationAndPKCEMatrix(t *testing.T) {
	for _, auth := range []string{"client_secret_basic", "client_secret_post", "none"} {
		for _, pkce := range []string{"S256", "plain", "off"} {
			if auth == "none" && pkce != "S256" {
				continue
			}
			t.Run(auth+"/"+pkce, func(t *testing.T) {
				op, err := mockop.New("")
				if err != nil {
					t.Fatal(err)
				}
				handler := op.Routes()
				tokenCalls, discoveryCalls := 0, 0
				ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/.well-known/openid-configuration" {
						discoveryCalls++
						http.Error(w, "no discovery", 404)
						return
					}
					if r.URL.Path == "/token" {
						tokenCalls++
						if err := r.ParseForm(); err != nil {
							t.Error(err)
						}
						id, secret, basic := r.BasicAuth()
						switch auth {
						case "client_secret_basic":
							if !basic || id != "client" || secret != "secret" || r.PostForm.Has("client_secret") {
								t.Error("incorrect basic authentication")
							}
						case "client_secret_post":
							if basic || r.PostForm.Get("client_id") != "client" || r.PostForm.Get("client_secret") != "secret" {
								t.Error("incorrect post authentication")
							}
						case "none":
							if r.Header.Get("Authorization") != "" || r.PostForm.Has("client_secret") || r.PostForm.Get("client_id") != "client" {
								t.Error("public client sent credentials or omitted client id")
							}
						}
						if pkce == "off" && r.PostForm.Has("code_verifier") {
							t.Error("off sent verifier")
						}
						if pkce != "off" && r.PostForm.Get("code_verifier") != "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789" {
							t.Error("missing verifier")
						}
					}
					handler.ServeHTTP(w, r)
				}))
				defer ts.Close()
				op.SetBase(ts.URL)
				op.SetClaims("subject", "a@example.com", true, "alice", "Alice")
				c := testConfig(t)
				c.IssuerURL = ts.URL
				c.ClientID = "client"
				c.AllowPrivateNetwork = true
				c.ConfigurationMode = "manual"
				c.TokenAuthMethod = auth
				c.PKCEMethod = pkce
				c.Endpoints = Endpoints{Authorization: new(ts.URL + "/authorize"), Token: new(ts.URL + "/token"), JWKS: new(ts.URL + "/jwks")}
				resolved, err := ResolveConfig(context.Background(), c)
				if err != nil {
					t.Fatal(err)
				}
				client, err := NewClient(context.Background(), c.ClientID, "secret", "https://rp.example/callback", resolved, nil, true)
				if err != nil {
					t.Fatal(err)
				}
				verifier := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
				challenge := verifier
				if pkce == "S256" {
					digest := sha256.Sum256([]byte(verifier))
					challenge = base64.RawURLEncoding.EncodeToString(digest[:])
				}
				authorize, err := url.Parse(client.AuthURL("state", "nonce", challenge))
				if err != nil {
					t.Fatal(err)
				}
				q := authorize.Query()
				if q.Get("state") != "state" || q.Get("nonce") != "nonce" {
					t.Fatal("missing state or nonce")
				}
				if pkce == "off" {
					if q.Has("code_challenge") || q.Has("code_challenge_method") {
						t.Fatal("off sent challenge")
					}
				} else if q.Get("code_challenge") != challenge || q.Get("code_challenge_method") != pkce {
					t.Fatal("incorrect PKCE request")
				}
				browser := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
				response, err := browser.Get(authorize.String())
				if err != nil {
					t.Fatal(err)
				}
				defer response.Body.Close()
				if response.StatusCode != 302 {
					t.Fatalf("authorize status %d", response.StatusCode)
				}
				callback, err := response.Location()
				if err != nil {
					t.Fatal(err)
				}
				tokens, err := client.Exchange(context.Background(), callback.Query().Get("code"), verifier, ts.URL, "nonce")
				if err != nil {
					t.Fatal(err)
				}
				if tokens.Subject != "subject" || tokenCalls != 1 || discoveryCalls != 0 {
					t.Fatalf("subject=%s token=%d discovery=%d", tokens.Subject, tokenCalls, discoveryCalls)
				}
				info, err := client.UserInfo(context.Background(), tokens.AccessToken, tokens.Subject)
				if err != nil || info != nil {
					t.Fatalf("manual null UserInfo: %v %v", info, err)
				}
			})
		}
	}
}

func TestClientDoesNotRetryAuthenticationFailure(t *testing.T) {
	for _, auth := range []string{"client_secret_basic", "client_secret_post", "none"} {
		t.Run(auth, func(t *testing.T) {
			calls := 0
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(401)
				_, _ = w.Write([]byte(`{"error":"invalid_client"}`))
			}))
			defer ts.Close()
			c, err := NewClient(context.Background(), "client", "secret", "https://rp.example/cb", ResolvedConfig{Issuer: ts.URL, TokenEndpoint: ts.URL, TokenAuthMethod: auth, PKCEMethod: "S256"}, nil, true)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := c.Exchange(context.Background(), "code", "verifier", ts.URL, "nonce"); err == nil || calls != 1 {
				t.Fatalf("error=%v requests=%d", err, calls)
			}
		})
	}
}

func TestAdapterRejectsConfigAndSameKeySecretDrift(t *testing.T) {
	for _, change := range []string{"config", "secret"} {
		t.Run(change, func(t *testing.T) {
			store := federationcore.NewSecretStore(map[int][]byte{1: make([]byte, 32)})
			sealed, err := store.SealProviderSecret([]byte("first"), 7, 1)
			if err != nil {
				t.Fatal(err)
			}
			c := testConfig(t)
			c.ConfigurationMode = "manual"
			c.TokenAuthMethod = "client_secret_basic"
			c.Endpoints = Endpoints{Authorization: new("https://issuer.test/auth"), Token: new("https://issuer.test/token"), JWKS: new("https://issuer.test/keys")}
			raw, _ := json.Marshal(c)
			provider := federationcore.Provider{ID: 7, Slug: "corp", Protocol: Protocol, Config: raw, Secret: sealed, SecretStatus: "configured"}
			adapter := NewAdapter(store)
			builds := 0
			exchanges := 0
			adapter.newClient = func(context.Context, Config, ResolvedConfig, string, string) (clientAPI, error) {
				builds++
				return &adapterFakeClient{exchanges: &exchanges}, nil
			}
			state, _, err := adapter.Begin(context.Background(), provider, federationcore.BeginContext{FlowID: "flow", CallbackURL: "https://rp.example/cb"})
			if err != nil {
				t.Fatal(err)
			}
			if change == "config" {
				c.EmailClaim = "upn"
				provider.Config, _ = json.Marshal(c)
			} else {
				provider.Secret, err = store.SealProviderSecret([]byte("second"), 7, 1)
				if err != nil {
					t.Fatal(err)
				}
			}
			// A different process has no cache and received no invalidation notification.
			other := NewAdapter(store)
			other.newClient = adapter.newClient
			if _, err := other.Advance(context.Background(), provider, state, federationcore.ActionInput{Kind: federationcore.ActionRedirect, Code: "code"}); err == nil {
				t.Fatal("accepted changed config/secret")
			}
			if exchanges != 0 || builds != 1 {
				t.Fatal("drift reached exchange")
			}
			if _, _, err := adapter.Begin(context.Background(), provider, federationcore.BeginContext{FlowID: "next", CallbackURL: "https://rp.example/cb"}); err != nil {
				t.Fatal(err)
			}
			if builds != 2 {
				t.Fatal("new flow reused stale client")
			}
		})
	}
}

func TestAdapterCallbackUsesSnapshotAcrossProcesses(t *testing.T) {
	c := testConfig(t)
	c.TokenAuthMethod = "none"
	raw, _ := json.Marshal(c)
	// Deliberately unreadable stored credentials must never be decrypted for none.
	provider := federationcore.Provider{ID: 7, Slug: "public", Protocol: Protocol, Config: raw, Secret: &federationcore.SealedSecret{KeyVersion: 999}}
	adapter := NewAdapter(nil)
	resolved := ResolvedConfig{Issuer: "https://issuer.test", AuthorizationEndpoint: "https://issuer.test/auth", TokenEndpoint: "https://issuer.test/token", JWKSEndpoint: "https://issuer.test/keys", PKCEMethod: "S256", TokenAuthMethod: "none"}
	adapter.resolveConfig = func(context.Context, Config) (ResolvedConfig, error) { return resolved, nil }
	adapter.newClient = func(_ context.Context, _ Config, r ResolvedConfig, secret, _ string) (clientAPI, error) {
		if secret != "" {
			t.Fatal("public secret opened")
		}
		if r.TokenEndpoint != resolved.TokenEndpoint {
			t.Fatal("endpoint changed")
		}
		return &adapterFakeClient{tokens: &Tokens{IDToken: "fake-jwt", Issuer: resolved.Issuer, Subject: "subject"}}, nil
	}
	state, _, err := adapter.Begin(context.Background(), provider, federationcore.BeginContext{FlowID: "flow", CallbackURL: "https://rp.example/cb"})
	if err != nil {
		t.Fatal(err)
	}
	other := NewAdapter(nil)
	other.newClient = adapter.newClient
	other.resolveConfig = func(context.Context, Config) (ResolvedConfig, error) {
		t.Fatal("callback re-ran discovery")
		return ResolvedConfig{}, nil
	}
	if _, err := other.Advance(context.Background(), provider, state, federationcore.ActionInput{Kind: federationcore.ActionRedirect, Code: "code"}); err != nil {
		t.Fatal(err)
	}
}

func TestAdapterPKCEStateMatchesAuthorization(t *testing.T) {
	for _, method := range []string{"S256", "plain", "off"} {
		t.Run(method, func(t *testing.T) {
			c := testConfig(t)
			c.ConfigurationMode = "manual"
			c.TokenAuthMethod = "client_secret_basic"
			c.PKCEMethod = method
			c.Endpoints = Endpoints{Authorization: new("https://issuer.test/auth"), Token: new("https://issuer.test/token"), JWKS: new("https://issuer.test/keys")}
			raw, _ := json.Marshal(c)
			store := federationcore.NewSecretStore(map[int][]byte{1: make([]byte, 32)})
			sealed, err := store.SealProviderSecret([]byte("secret"), 7, 1)
			if err != nil {
				t.Fatal(err)
			}
			provider := federationcore.Provider{ID: 7, Slug: "corp", Protocol: Protocol, Config: raw, Secret: sealed}
			adapter := NewAdapter(store)
			adapter.newClient = func(context.Context, Config, ResolvedConfig, string, string) (clientAPI, error) {
				return &adapterFakeClient{}, nil
			}
			rawState, action, err := adapter.Begin(context.Background(), provider, federationcore.BeginContext{FlowID: "flow", CallbackURL: "https://rp.example/cb"})
			if err != nil {
				t.Fatal(err)
			}
			var state adapterState
			if err := json.Unmarshal(rawState, &state); err != nil {
				t.Fatal(err)
			}
			var fields map[string]any
			_ = json.Unmarshal(rawState, &fields)
			u, err := url.Parse(action.URL)
			if err != nil {
				t.Fatal(err)
			}
			challenge := u.Query().Get("challenge")
			switch method {
			case "off":
				if _, ok := fields["codeVerifier"]; ok || challenge != "" {
					t.Fatal("off stored or sent verifier")
				}
			case "plain":
				if len(state.CodeVerifier) < 43 || challenge != state.CodeVerifier {
					t.Fatal("plain challenge differs from verifier")
				}
			case "S256":
				digest := sha256.Sum256([]byte(state.CodeVerifier))
				if challenge != base64.RawURLEncoding.EncodeToString(digest[:]) {
					t.Fatal("S256 challenge mismatch")
				}
			}
		})
	}
}
