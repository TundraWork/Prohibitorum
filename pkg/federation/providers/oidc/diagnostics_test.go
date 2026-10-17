package oidc

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"prohibitorum/cmd/smoke/mockop"
	federationcore "prohibitorum/pkg/federation"
	"prohibitorum/pkg/kv"
)

type diagnosticHarness struct {
	service         *Diagnostics
	provider        federationcore.Provider
	actor           DiagnosticActor
	browser         string
	server          *httptest.Server
	op              *mockop.Server
	tokenCalls      atomic.Int32
	userInfoCalls   atomic.Int32
	tokenFailure    atomic.Bool
	userInfoFailure atomic.Bool
	discoveryDown   atomic.Bool
	callbackSeen    string
	callbackMu      sync.Mutex
	// tokenBody and userInfoBody, when set, replace the mock OP's responses.
	tokenBody    atomic.Pointer[string]
	userInfoBody atomic.Pointer[string]
	secrets      *federationcore.SecretStore
}

func newDiagnosticHarness(t *testing.T) *diagnosticHarness {
	t.Helper()
	op, err := mockop.New("")
	if err != nil {
		t.Fatal(err)
	}
	h := &diagnosticHarness{op: op, actor: DiagnosticActor{AccountID: 7, SessionID: "admin-session"}}
	h.browser, _ = randomB64(32)
	h.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			h.tokenCalls.Add(1)
			_ = r.ParseForm()
			h.callbackMu.Lock()
			h.callbackSeen = r.PostForm.Get("redirect_uri")
			h.callbackMu.Unlock()
			if h.tokenFailure.Load() {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(401)
				_, _ = w.Write([]byte(`{"error":"invalid_client","error_description":"secret-never-display"}`))
				return
			}
		}
		if r.URL.Path == "/token" {
			if body := h.tokenBody.Load(); body != nil {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(*body))
				return
			}
		}
		if r.URL.Path == "/userinfo" {
			h.userInfoCalls.Add(1)
			if h.userInfoFailure.Load() {
				http.Error(w, "secret-never-display", 500)
				return
			}
			if body := h.userInfoBody.Load(); body != nil {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(*body))
				return
			}
		}
		if r.URL.Path == "/.well-known/openid-configuration" && h.discoveryDown.Load() {
			http.Error(w, "down", 500)
			return
		}
		op.Routes().ServeHTTP(w, r)
	}))
	t.Cleanup(h.server.Close)
	op.SetBase(h.server.URL)
	op.SetClaims("subject", "admin@example.com", true, "upstream-user", "Upstream Name")
	store := federationcore.NewSecretStore(map[int][]byte{1: make([]byte, 32)})
	sealed, err := store.SealProviderSecret([]byte("secret-never-display"), 7, 1)
	if err != nil {
		t.Fatal(err)
	}
	c := testConfig(t)
	c.IssuerURL = h.server.URL
	c.AllowPrivateNetwork = true
	c.TokenAuthMethod = "client_secret_post"
	raw, _ := json.Marshal(c)
	h.provider = federationcore.Provider{ID: 7, Slug: "corp", Protocol: Protocol, Config: raw, Secret: sealed, SecretStatus: "configured"}
	h.secrets = store
	h.service = NewDiagnostics(kv.NewMemoryStore(), store)
	return h
}
func (h *diagnosticHarness) configure(edit func(*Config)) {
	var c Config
	_ = json.Unmarshal(h.provider.Config, &c)
	edit(&c)
	h.provider.Config, _ = json.Marshal(c)
}

// fallback makes the token endpoint return no id_token, as a GitHub OAuth App
// does, and serves userinfo as GitHub's numeric-id user document.
func (h *diagnosticHarness) fallback(userinfo string) {
	token := `{"access_token":"fallback-access-token","token_type":"Bearer"}`
	h.tokenBody.Store(&token)
	h.userInfoBody.Store(&userinfo)
	h.configure(func(c *Config) {
		c.SubjectClaim = "id"
		c.UsernameClaim = "login"
		c.PictureClaim = "avatar_url"
	})
}

const githubUserInfo = `{"id": 9007199254740993, "login": "octo", "name": "Octo Cat", "email": "octo@example.com", "avatar_url": "https://avatars.example/octo.png"}`

// authorize follows the mock OP's authorization redirect and returns the
// callback's code and iss.
func authorize(t *testing.T, authorizationURL string) (string, string) {
	t.Helper()
	browser := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := browser.Get(authorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	location, err := response.Location()
	if err != nil {
		t.Fatal(err)
	}
	return location.Query().Get("code"), location.Query().Get("iss")
}

// run drives a whole connection test: start, provider redirect, callback,
// complete.
func (h *diagnosticHarness) run(t *testing.T) DiagnosticResult {
	t.Helper()
	s := h.start(t)
	h.callback(t, s)
	completed, err := h.service.Complete(context.Background(), s.ID, h.provider, h.browser, h.actor, "request-complete")
	if err != nil {
		t.Fatal(err)
	}
	// The dashboard discards the Complete response and reads the stored
	// result, so the stored copy must carry every field Complete returned.
	stored, err := h.service.Get(context.Background(), s.ID, h.provider.Slug, h.browser, h.actor)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := json.Marshal(completed)
	got, _ := json.Marshal(stored)
	if string(got) != string(want) {
		t.Fatalf("stored result differs from Complete:\n got %s\nwant %s", got, want)
	}
	return stored
}

// login runs the real login path against the same mock OP.
func (h *diagnosticHarness) login(t *testing.T) *federationcore.VerifiedIdentity {
	t.Helper()
	adapter := NewAdapter(h.secrets)
	state, next, err := adapter.Begin(context.Background(), h.provider, federationcore.BeginContext{Intent: federationcore.IntentLogin, FlowID: "login-flow", CallbackURL: "https://rp.example/api/prohibitorum/auth/federation/corp/callback"})
	if err != nil {
		t.Fatal(err)
	}
	code, iss := authorize(t, next.URL)
	result, err := adapter.Advance(context.Background(), h.provider, state, federationcore.ActionInput{Kind: federationcore.ActionRedirect, Code: code, Issuer: iss})
	if err != nil {
		t.Fatal(err)
	}
	return result.Identity
}

func stageOf(result DiagnosticResult, name string) DiagnosticStage {
	for _, stage := range result.Stages {
		if stage.Name == name {
			return stage
		}
	}
	return DiagnosticStage{}
}
func (h *diagnosticHarness) start(t *testing.T) DiagnosticStart {
	t.Helper()
	s, err := h.service.Start(context.Background(), h.provider, h.actor, h.browser, "https://rp.example/api/prohibitorum/auth/federation/corp/test/callback", "request-start")
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func (h *diagnosticHarness) callback(t *testing.T, s DiagnosticStart) string {
	t.Helper()
	code, iss := authorize(t, s.AuthorizationURL)
	if err := h.service.Callback(context.Background(), s.ID, h.provider.Slug, h.browser, code, "", "", iss, "", "", "request-callback"); err != nil {
		t.Fatal(err)
	}
	return code
}

func TestDiagnosticsCompleteUsesDedicatedCallbackAndDoesNotReplay(t *testing.T) {
	h := newDiagnosticHarness(t)
	s := h.start(t)
	authorize, _ := url.Parse(s.AuthorizationURL)
	if authorize.Query().Get("redirect_uri") != "https://rp.example/api/prohibitorum/auth/federation/corp/test/callback" {
		t.Fatal("wrong callback URI")
	}
	code := h.callback(t, s)
	if h.tokenCalls.Load() != 0 {
		t.Fatal("callback exchanged code")
	}
	if err := h.service.Callback(context.Background(), s.ID, h.provider.Slug, h.browser, code, "", "", "", "", "", "replay"); err == nil {
		t.Fatal("callback replay accepted")
	}
	result, err := h.service.Complete(context.Background(), s.ID, h.provider, h.browser, h.actor, "request-complete")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "succeeded" || len(result.Stages) != 6 || result.Identity == nil || result.Identity.Subject != (DiagnosticField{Value: "subject", Source: "id_token", Claim: "sub"}) {
		t.Fatalf("result=%+v", result)
	}
	if result.IDToken == nil || !strings.Contains(result.IDToken.JSON, `"aud":`) || !strings.Contains(result.IDToken.JSON, `"nonce":`) {
		t.Fatalf("id_token document=%+v", result.IDToken)
	}
	if result.UserInfo == nil || !strings.Contains(result.UserInfo.JSON, `"sub":"subject"`) {
		t.Fatalf("userinfo document=%+v", result.UserInfo)
	}
	if h.callbackSeen != authorize.Query().Get("redirect_uri") {
		t.Fatal("exchange changed callback URI")
	}
	for i := 0; i < 3; i++ {
		got, err := h.service.Complete(context.Background(), s.ID, h.provider, h.browser, h.actor, "again")
		if err != nil || got.Status != "succeeded" {
			t.Fatalf("repeat: %v %+v", err, got)
		}
	}
	if h.tokenCalls.Load() != 1 {
		t.Fatal("code exchanged more than once")
	}
	raw, err := h.service.store.Get(context.Background(), diagnosticKey(s.ID))
	if err != nil {
		t.Fatal(err)
	}
	// "eyJ" opens every base64url JWT segment: no raw id_token or JWT access
	// token is stored.
	for _, secret := range []string{code, "secret-never-display", "access_token", "eyJ"} {
		if strings.Contains(raw, secret) {
			t.Fatalf("stored result contains %s", secret)
		}
	}
	var state diagnosticFlow
	_ = json.Unmarshal([]byte(raw), &state)
	if state.Code != "" || state.Issuer != "" || len(state.AdapterState) != 0 {
		t.Fatal("transient credentials retained")
	}
}

// TestDiagnosticsImplicitFlowForPublicClient runs a whole connection test
// for a public client set to ID token only: the authorization URL requests response_type=id_token,
// the callback stores the front-channel id_token, Complete verifies it with
// no token endpoint call, and the id_token document reaches the result.
func TestDiagnosticsImplicitFlowForPublicClient(t *testing.T) {
	h := newDiagnosticHarness(t)
	h.configure(func(c *Config) {
		c.TokenAuthMethod = "none"
		c.ResponseType = responseTypeIDToken
	})
	// A public provider carries no stored secret.
	h.provider.Secret = nil
	h.provider.SecretStatus = "unconfigured"
	s := h.start(t)
	authorize, err := url.Parse(s.AuthorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	if authorize.Query().Get("response_type") != "id_token" {
		t.Fatalf("authorization response_type = %q, want id_token", authorize.Query().Get("response_type"))
	}
	// The implicit response puts the id_token in the fragment; simulate the
	// relay page by parsing the fragment the mock OP redirected with.
	browser := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := browser.Get(s.AuthorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	loc, err := resp.Location()
	if err != nil {
		t.Fatal(err)
	}
	fragment, err := url.ParseQuery(loc.Fragment)
	if err != nil {
		t.Fatal(err)
	}
	idToken := fragment.Get("id_token")
	if idToken == "" {
		t.Fatalf("fragment carries no id_token: %s", loc)
	}
	if err := h.service.Callback(context.Background(), s.ID, h.provider.Slug, h.browser, "", "", "", fragment.Get("iss"), idToken, "", "request-callback"); err != nil {
		t.Fatal(err)
	}
	result, err := h.service.Complete(context.Background(), s.ID, h.provider, h.browser, h.actor, "request-complete")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "succeeded" || result.Identity == nil || result.Identity.Subject.Value != "subject" {
		t.Fatalf("result=%+v", result)
	}
	if stageOf(result, "token_exchange").Status != "skipped" {
		t.Fatalf("implicit token_exchange stage = %+v, want skipped", stageOf(result, "token_exchange"))
	}
	if stageOf(result, "id_token").Status != "succeeded" {
		t.Fatalf("implicit id_token stage = %+v", stageOf(result, "id_token"))
	}
	if result.IDToken == nil || !strings.Contains(result.IDToken.JSON, `"sub":"subject"`) {
		t.Fatalf("id_token document = %+v", result.IDToken)
	}
	if h.tokenCalls.Load() != 0 {
		t.Fatalf("implicit diagnostic hit /token %d times", h.tokenCalls.Load())
	}
	// The raw id_token must not be retained after Complete.
	raw, err := h.service.store.Get(context.Background(), diagnosticKey(s.ID))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, idToken) || strings.Contains(raw, "eyJ") {
		t.Fatal("stored result retains the raw id_token")
	}
}

// TestDiagnosticsImplicitTokenFlowForPublicClient runs the connection test
// for the id_token token response: the record holds the fragment's access
// token between callback and Complete (the one place a front-channel
// credential is stored), Complete verifies it against the id_token's
// at_hash and runs userinfo with it, and nothing raw survives afterwards.
func TestDiagnosticsImplicitTokenFlowForPublicClient(t *testing.T) {
	h := newDiagnosticHarness(t)
	h.configure(func(c *Config) {
		c.TokenAuthMethod = "none"
		c.ResponseType = responseTypeIDTokenToken
	})
	h.provider.Secret = nil
	h.provider.SecretStatus = "unconfigured"
	s := h.start(t)
	authorize, err := url.Parse(s.AuthorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	if authorize.Query().Get("response_type") != responseTypeIDTokenToken {
		t.Fatalf("authorization response_type = %q, want %s", authorize.Query().Get("response_type"), responseTypeIDTokenToken)
	}
	browser := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := browser.Get(s.AuthorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	loc, err := resp.Location()
	if err != nil {
		t.Fatal(err)
	}
	fragment, err := url.ParseQuery(loc.Fragment)
	if err != nil {
		t.Fatal(err)
	}
	idToken, accessToken := fragment.Get("id_token"), fragment.Get("access_token")
	if idToken == "" || accessToken == "" {
		t.Fatalf("fragment tokens: %s", loc)
	}
	if err := h.service.Callback(context.Background(), s.ID, h.provider.Slug, h.browser, "", "", "", fragment.Get("iss"), idToken, accessToken, "request-callback"); err != nil {
		t.Fatal(err)
	}
	// Between callback and Complete the token sits in the record — and only
	// there, never in a result the API could echo.
	stored, err := h.service.store.Get(context.Background(), diagnosticKey(s.ID))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stored, accessToken) {
		t.Fatal("ready record does not hold the access token for Complete")
	}
	var echoed DiagnosticResult
	if err := json.Unmarshal([]byte(stored), &echoed); err != nil {
		t.Fatal(err)
	}
	wire, _ := json.Marshal(echoed)
	if strings.Contains(string(wire), accessToken) || strings.Contains(string(wire), idToken) {
		t.Fatal("diagnostics result echoes a raw token")
	}
	result, err := h.service.Complete(context.Background(), s.ID, h.provider, h.browser, h.actor, "request-complete")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "succeeded" || result.Identity == nil {
		t.Fatalf("result=%+v", result)
	}
	wire, _ = json.Marshal(result)
	if strings.Contains(string(wire), accessToken) || strings.Contains(string(wire), idToken) {
		t.Fatal("completed result echoes a raw token")
	}
	if stage := stageOf(result, "token_exchange"); stage.Status != "skipped" {
		t.Fatalf("token_exchange stage = %+v, want skipped", stage)
	}
	if stage := stageOf(result, "id_token"); stage.Status != "succeeded" {
		t.Fatalf("id_token stage = %+v", stage)
	}
	if stage := stageOf(result, "userinfo"); stage.Status != "succeeded" {
		t.Fatalf("userinfo stage = %+v, want succeeded with the retained token", stage)
	}
	if result.UserInfo == nil {
		t.Fatal("result carries no userinfo document")
	}
	if h.tokenCalls.Load() != 0 || h.userInfoCalls.Load() != 1 {
		t.Fatalf("token/userinfo calls = %d/%d, want 0/1", h.tokenCalls.Load(), h.userInfoCalls.Load())
	}
	// Neither token may be retained after Complete.
	raw, err := h.service.store.Get(context.Background(), diagnosticKey(s.ID))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, idToken) || strings.Contains(raw, accessToken) || strings.Contains(raw, "eyJ") {
		t.Fatal("stored result retains a raw token")
	}
}

// TestDiagnosticsImplicitTokenRejectsBadAtHash fails the connection test when
// the fragment's access token does not match the id_token's at_hash: the
// id_token stage reports access_token_hash and userinfo never runs.
func TestDiagnosticsImplicitTokenRejectsBadAtHash(t *testing.T) {
	h := newDiagnosticHarness(t)
	h.configure(func(c *Config) {
		c.TokenAuthMethod = "none"
		c.ResponseType = responseTypeIDTokenToken
	})
	h.provider.Secret = nil
	h.provider.SecretStatus = "unconfigured"
	s := h.start(t)
	browser := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := browser.Get(s.AuthorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	loc, err := resp.Location()
	if err != nil {
		t.Fatal(err)
	}
	fragment, err := url.ParseQuery(loc.Fragment)
	if err != nil {
		t.Fatal(err)
	}
	idToken, accessToken := fragment.Get("id_token"), fragment.Get("access_token")
	if idToken == "" || accessToken == "" {
		t.Fatalf("fragment tokens: %s", loc)
	}
	// Hand the callback a token that does not match the signed at_hash.
	if err := h.service.Callback(context.Background(), s.ID, h.provider.Slug, h.browser, "", "", "", fragment.Get("iss"), idToken, accessToken+"substituted", "request-callback"); err != nil {
		t.Fatal(err)
	}
	result, err := h.service.Complete(context.Background(), s.ID, h.provider, h.browser, h.actor, "request-complete")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "failed" {
		t.Fatalf("status = %s, want failed", result.Status)
	}
	if stage := stageOf(result, "id_token"); stage.Status != "failed" || stage.ErrorCode != "access_token_hash" {
		t.Fatalf("id_token stage = %+v, want failed access_token_hash", stage)
	}
	// A failed verification returns before the userinfo stage is appended;
	// there is nothing to skip-report because userinfo never ran.
	if stage := stageOf(result, "userinfo"); stage.Name != "" {
		t.Fatalf("userinfo stage = %+v, want none after a failed id_token", stage)
	}
	if h.userInfoCalls.Load() != 0 {
		t.Fatalf("userinfo was called %d times with a bad at_hash", h.userInfoCalls.Load())
	}
	// The bad token must not survive the failed Complete either.
	raw, err := h.service.store.Get(context.Background(), diagnosticKey(s.ID))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, accessToken) || strings.Contains(raw, "substituted") {
		t.Fatal("failed result retains the raw access token")
	}
}

func TestDiagnosticsConcurrentCompleteExchangesOnce(t *testing.T) {
	h := newDiagnosticHarness(t)
	s := h.start(t)
	h.callback(t, s)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := h.service.Complete(context.Background(), s.ID, h.provider, h.browser, h.actor, "concurrent")
			if err != nil {
				t.Error(err)
			} else if r.Status != "running" && r.Status != "succeeded" {
				t.Errorf("status=%s", r.Status)
			}
		}()
	}
	wg.Wait()
	if h.tokenCalls.Load() != 1 {
		t.Fatalf("exchanges=%d", h.tokenCalls.Load())
	}
}

func TestDiagnosticsRejectsWrongOwnerBrowserProviderAndExpiry(t *testing.T) {
	for _, scenario := range []string{"account", "session", "browser", "provider", "expiry", "config", "secret"} {
		t.Run(scenario, func(t *testing.T) {
			h := newDiagnosticHarness(t)
			s := h.start(t)
			h.callback(t, s)
			actor := h.actor
			browser := h.browser
			provider := h.provider
			switch scenario {
			case "account":
				actor.AccountID++
			case "session":
				actor.SessionID = "another"
			case "browser":
				browser, _ = randomB64(32)
			case "provider":
				provider.Slug = "other"
			case "expiry":
				h.service.now = func() time.Time { return s.ExpiresAt.Add(time.Second) }
			case "config":
				var c Config
				_ = json.Unmarshal(provider.Config, &c)
				c.EmailClaim = "upn"
				provider.Config, _ = json.Marshal(c)
			case "secret":
				sealed := *provider.Secret
				sealed.Ciphertext = []byte("rotated")
				provider.Secret = &sealed
			}
			result, err := h.service.Complete(context.Background(), s.ID, provider, browser, actor, "request")
			if scenario == "config" || scenario == "secret" {
				if err != nil || result.Status != "failed" || result.Stages[len(result.Stages)-1].ErrorCode != "configuration_changed" {
					t.Fatalf("drift=%+v err=%v", result, err)
				}
			} else if err == nil {
				t.Fatal("wrong binding accepted")
			}
			if h.tokenCalls.Load() != 0 {
				t.Fatal("invalid test exchanged code")
			}
		})
	}
}

func TestDiagnosticsFailureStagesAreRedacted(t *testing.T) {
	for _, scenario := range []string{"denied", "callback issuer", "token", "id token", "userinfo"} {
		t.Run(scenario, func(t *testing.T) {
			h := newDiagnosticHarness(t)
			s := h.start(t)
			switch scenario {
			case "denied":
				if err := h.service.Callback(context.Background(), s.ID, h.provider.Slug, h.browser, "", "access_denied", "", "", "", "", "callback"); err != nil {
					t.Fatal(err)
				}
			case "callback issuer":
				if err := h.service.Callback(context.Background(), s.ID, h.provider.Slug, h.browser, "code", "", "", "https://other.example", "", "", "callback"); err != nil {
					t.Fatal(err)
				}
			default:
				if scenario == "id token" {
					h.op.OverrideIssuer("https://wrong.example")
				}
				code, _ := authorize(t, s.AuthorizationURL)
				if err := h.service.Callback(context.Background(), s.ID, h.provider.Slug, h.browser, code, "", "", "", "", "", "callback"); err != nil {
					t.Fatal(err)
				}
				h.tokenFailure.Store(scenario == "token")
				h.userInfoFailure.Store(scenario == "userinfo")
			}
			result, err := h.service.Complete(context.Background(), s.ID, h.provider, h.browser, h.actor, "complete")
			if err != nil {
				t.Fatal(err)
			}
			if result.Status != "failed" {
				t.Fatalf("result=%+v", result)
			}
			raw, _ := json.Marshal(result)
			if strings.Contains(string(raw), "secret-never-display") {
				t.Fatal("upstream body exposed")
			}
			expected := map[string]string{"denied": "callback", "callback issuer": "token_exchange", "token": "token_exchange", "id token": "id_token", "userinfo": "userinfo"}[scenario]
			found := false
			for _, stage := range result.Stages {
				if stage.Name == expected && stage.Status == "failed" {
					found = true
				}
			}
			if !found {
				t.Fatalf("missing failed %s: %+v", expected, result.Stages)
			}
		})
	}
}

func TestDiagnosticsCallbackIssuerIsCheckedByAdvance(t *testing.T) {
	h := newDiagnosticHarness(t)
	s := h.start(t)
	code, _ := authorize(t, s.AuthorizationURL)
	if err := h.service.Callback(context.Background(), s.ID, h.provider.Slug, h.browser, code, "", "", "https://other.example", "", "", "callback"); err != nil {
		t.Fatal(err)
	}
	result, err := h.service.Complete(context.Background(), s.ID, h.provider, h.browser, h.actor, "complete")
	if err != nil {
		t.Fatal(err)
	}
	if stage := stageOf(result, "token_exchange"); stage.Status != "failed" || stage.ErrorCode != "issuer_mismatch" {
		t.Fatalf("stages=%+v", result.Stages)
	}
	if stageOf(result, "id_token").Status != "skipped" || result.Identity != nil {
		t.Fatalf("result=%+v", result)
	}
	if h.tokenCalls.Load() != 0 {
		t.Fatal("code exchanged despite wrong iss")
	}
}

// The test and login share Adapter.Begin/Advance; this guards against protocol
// logic creeping back into the diagnostics.
func TestDiagnosticsMappedFieldsMatchLogin(t *testing.T) {
	for _, path := range []string{"id_token", "fallback"} {
		t.Run(path, func(t *testing.T) {
			h := newDiagnosticHarness(t)
			h.op.SetPicture(h.op.PictureURL())
			if path == "fallback" {
				h.fallback(githubUserInfo)
			}
			login := h.login(t)
			result := h.run(t)
			if result.Status != "succeeded" || result.Identity == nil {
				t.Fatalf("result=%+v", result)
			}
			email := ""
			if login.Email != nil {
				email = *login.Email
			}
			got := result.Identity
			for name, pair := range map[string][2]any{
				"issuer": {got.Issuer.Value, login.Issuer}, "subject": {got.Subject.Value, login.Subject},
				"username": {got.Username.Value, login.Username}, "displayName": {got.DisplayName.Value, login.DisplayName},
				"email": {got.Email.Value, email}, "emailVerified": {got.EmailVerified.Value, login.EmailVerified},
				"picture": {got.Picture.Value, login.AvatarURL},
			} {
				if pair[0] != pair[1] {
					t.Errorf("%s: test=%v login=%v", name, pair[0], pair[1])
				}
			}
		})
	}
}

func TestDiagnosticsUserInfoSubjectMismatch(t *testing.T) {
	h := newDiagnosticHarness(t)
	body := `{"sub":"someone-else","picture":"https://example.com/a.png"}`
	h.userInfoBody.Store(&body)
	result := h.run(t)
	if stage := stageOf(result, "userinfo"); stage.Status != "failed" || stage.ErrorCode != "userinfo_subject_mismatch" {
		t.Fatalf("stages=%+v", result.Stages)
	}
	if result.Status != "failed" || result.Identity == nil || result.Identity.Picture.Value != nil {
		t.Fatalf("result=%+v", result)
	}
	if result.UserInfo == nil || !strings.Contains(result.UserInfo.JSON, "someone-else") {
		t.Fatalf("userinfo=%+v", result.UserInfo)
	}
}

func TestDiagnosticsPictureSource(t *testing.T) {
	t.Run("userinfo supplies the picture", func(t *testing.T) {
		h := newDiagnosticHarness(t)
		h.op.SetPictureUserInfoOnly(h.op.PictureURL())
		result := h.run(t)
		if result.Identity.Picture != (DiagnosticField{Value: h.op.PictureURL(), Source: "userinfo", Claim: "picture"}) {
			t.Fatalf("picture=%+v", result.Identity.Picture)
		}
		if strings.Contains(result.IDToken.JSON, `"picture"`) {
			t.Fatal("id_token carries a picture")
		}
	})
	t.Run("id_token has the picture", func(t *testing.T) {
		h := newDiagnosticHarness(t)
		h.op.SetPicture(h.op.PictureURL())
		result := h.run(t)
		if result.Identity.Picture != (DiagnosticField{Value: h.op.PictureURL(), Source: "id_token", Claim: "picture"}) {
			t.Fatalf("picture=%+v", result.Identity.Picture)
		}
		if h.userInfoCalls.Load() != 1 || stageOf(result, "userinfo").Status != "succeeded" || result.UserInfo == nil {
			t.Fatalf("userinfo calls=%d result=%+v", h.userInfoCalls.Load(), result)
		}
	})
}

func TestDiagnosticsUserInfoFallback(t *testing.T) {
	h := newDiagnosticHarness(t)
	h.fallback(githubUserInfo)
	result := h.run(t)
	if result.Status != "succeeded" || stageOf(result, "id_token").Status != "skipped" || stageOf(result, "userinfo").Status != "succeeded" {
		t.Fatalf("result=%+v", result)
	}
	if result.IDToken != nil {
		t.Fatalf("id_token document=%+v", result.IDToken)
	}
	identity := result.Identity
	if identity.Subject != (DiagnosticField{Value: "9007199254740993", Source: "userinfo", Claim: "id"}) {
		t.Fatalf("subject=%+v", identity.Subject)
	}
	if identity.Issuer != (DiagnosticField{Value: h.server.URL, Source: "configuration"}) {
		t.Fatalf("issuer=%+v", identity.Issuer)
	}
	if identity.Username != (DiagnosticField{Value: "octo", Source: "userinfo", Claim: "login"}) || identity.Picture != (DiagnosticField{Value: "https://avatars.example/octo.png", Source: "userinfo", Claim: "avatar_url"}) {
		t.Fatalf("identity=%+v", identity)
	}
	if identity.EmailVerified != (DiagnosticField{Value: false, Source: "userinfo", Claim: "email_verified"}) {
		t.Fatalf("emailVerified=%+v", identity.EmailVerified)
	}
	if result.UserInfo == nil || !strings.HasPrefix(result.UserInfo.JSON, `{"id":9007199254740993,"login":"octo",`) {
		t.Fatalf("userinfo=%+v", result.UserInfo)
	}
	// The stored result keeps the large integer exactly.
	raw, _ := json.Marshal(result)
	var reread DiagnosticResult
	if json.Unmarshal(raw, &reread) != nil || reread.UserInfo.JSON != result.UserInfo.JSON {
		t.Fatal("userinfo text changed in a round trip")
	}
}

// TestDiagnosticsJWKSNoneSkipsIDTokenStage runs a whole connection test for
// a plain OAuth 2.0 provider: JWKS overridden to none, no scopes, and a token
// response that still carries an id_token. The client drops it, so the
// id_token stage reads skipped with no idToken document, and every identity
// field is labelled as read from userinfo.
func TestDiagnosticsJWKSNoneSkipsIDTokenStage(t *testing.T) {
	h := newDiagnosticHarness(t)
	none := "none"
	h.op.SetExtraClaims(map[string]any{"id": json.Number("9007199254740993"), "login": "octo"})
	h.configure(func(c *Config) {
		c.Endpoints.JWKS = &none
		c.Scopes = []string{}
		c.SubjectClaim = "id"
		c.UsernameClaim = "login"
	})
	s := h.start(t)
	if authorize, err := url.Parse(s.AuthorizationURL); err != nil || authorize.Query().Has("scope") {
		t.Fatalf("authorization scope = %q, want none sent (err %v)", authorize.Query().Get("scope"), err)
	}
	h.callback(t, s)
	result, err := h.service.Complete(context.Background(), s.ID, h.provider, h.browser, h.actor, "request-complete")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "succeeded" {
		t.Fatalf("result=%+v", result)
	}
	if stage := stageOf(result, "id_token"); stage.Status != "skipped" {
		t.Fatalf("id_token stage = %+v, want skipped", stage)
	}
	if result.IDToken != nil {
		t.Fatalf("id_token document=%+v, want none", result.IDToken)
	}
	if stage := stageOf(result, "userinfo"); stage.Status != "succeeded" {
		t.Fatalf("userinfo stage = %+v, want succeeded", stage)
	}
	identity := result.Identity
	if identity == nil || identity.Subject != (DiagnosticField{Value: "9007199254740993", Source: "userinfo", Claim: "id"}) {
		t.Fatalf("subject=%+v identity=%+v", identity.Subject, identity)
	}
	if identity.Username != (DiagnosticField{Value: "octo", Source: "userinfo", Claim: "login"}) {
		t.Fatalf("username=%+v", identity.Username)
	}
	if identity.Issuer != (DiagnosticField{Value: h.server.URL, Source: "configuration"}) {
		t.Fatalf("issuer=%+v", identity.Issuer)
	}
	if result.UserInfo == nil || !strings.Contains(result.UserInfo.JSON, `"id":9007199254740993`) {
		t.Fatalf("userinfo=%+v", result.UserInfo)
	}
}

func TestDiagnosticsUserInfoFallbackFailures(t *testing.T) {
	for _, scenario := range []struct{ name, code string }{{"no userinfo endpoint", "userinfo_required"}, {"no subject", "subject_missing"}} {
		t.Run(scenario.name, func(t *testing.T) {
			h := newDiagnosticHarness(t)
			if scenario.name == "no subject" {
				h.fallback(`{"login":"octo"}`)
			} else {
				h.fallback(githubUserInfo)
				h.configure(func(c *Config) {
					c.ConfigurationMode = "manual"
					c.Endpoints = Endpoints{Authorization: new(h.server.URL + "/authorize"), Token: new(h.server.URL + "/token"), JWKS: new(h.server.URL + "/jwks")}
				})
			}
			result := h.run(t)
			if stage := stageOf(result, "userinfo"); result.Status != "failed" || stage.Status != "failed" || stage.ErrorCode != scenario.code {
				t.Fatalf("result=%+v", result)
			}
			if stageOf(result, "id_token").Status != "skipped" || result.Identity != nil {
				t.Fatalf("result=%+v", result)
			}
			if scenario.name == "no subject" && (result.UserInfo == nil || result.UserInfo.JSON != `{"login":"octo"}`) {
				t.Fatalf("userinfo=%+v", result.UserInfo)
			}
		})
	}
}

func TestDiagnosticsOversizedUserInfoKeepsOnlyItsSize(t *testing.T) {
	h := newDiagnosticHarness(t)
	body := `{"sub": "subject", "padding": "` + strings.Repeat("x", 70000) + `"}`
	h.userInfoBody.Store(&body)
	result := h.run(t)
	if result.Status != "succeeded" || result.Identity == nil || result.UserInfo == nil {
		t.Fatalf("result=%+v", result)
	}
	if want := len(body) - 3; result.UserInfo.JSON != "" || result.UserInfo.OmittedBytes != want {
		t.Fatalf("userinfo=%+v want %d bytes", result.UserInfo, want)
	}
}

func TestDiagnosticsDiscoveryFailure(t *testing.T) {
	h := newDiagnosticHarness(t)
	h.discoveryDown.Store(true)
	_, err := h.service.Start(context.Background(), h.provider, h.actor, h.browser, "https://rp.example/cb", "request-start")
	var resolution *DiagnosticResolutionError
	if !errors.As(err, &resolution) || resolution.Stage.ErrorCode != "discovery_failed" || resolution.Stage.RequestID != "request-start" {
		t.Fatalf("err=%v", err)
	}
}

func TestDiagnosticsCallbackErrorDescription(t *testing.T) {
	h := newDiagnosticHarness(t)
	s := h.start(t)
	description := "AADSTS9002346: Application 'mock-client' is configured for Microsoft Account users only."
	if err := h.service.Callback(context.Background(), s.ID, h.provider.Slug, h.browser, "", "invalid_request", description, "", "", "", "callback"); err != nil {
		t.Fatal(err)
	}
	result, err := h.service.Get(context.Background(), s.ID, h.provider.Slug, h.browser, h.actor)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "failed" {
		t.Fatalf("result=%+v", result)
	}
	stage := stageOf(result, "callback")
	if stage.Status != "failed" || stage.ErrorCode != "invalid_request" || stage.ErrorDescription != description {
		t.Fatalf("stage=%+v", stage)
	}
	// Oversized descriptions are dropped whole rather than truncated: a cut
	// mid-code would mislead more than an absent description.
	s = h.start(t)
	if err := h.service.Callback(context.Background(), s.ID, h.provider.Slug, h.browser, "", "access_denied", strings.Repeat("x", 8193), "", "", "", "callback"); err != nil {
		t.Fatal(err)
	}
	result, err = h.service.Get(context.Background(), s.ID, h.provider.Slug, h.browser, h.actor)
	if err != nil {
		t.Fatal(err)
	}
	if stage := stageOf(result, "callback"); stage.Status != "failed" || stage.ErrorCode != "access_denied" || stage.ErrorDescription != "" {
		t.Fatalf("oversized stage=%+v", stage)
	}
}

// TestDiagnosticsUpstreamErrorDescriptionFromExchangeIsNotShown pins the
// boundary: the front-channel description from the authorize redirect is kept,
// while a description in the token endpoint's response body — text the admin
// has never seen — stays redacted.
func TestDiagnosticsUpstreamErrorDescriptionFromExchangeIsNotShown(t *testing.T) {
	h := newDiagnosticHarness(t)
	s := h.start(t)
	code, _ := authorize(t, s.AuthorizationURL)
	if err := h.service.Callback(context.Background(), s.ID, h.provider.Slug, h.browser, code, "", "", "", "", "", "callback"); err != nil {
		t.Fatal(err)
	}
	h.tokenFailure.Store(true)
	result, err := h.service.Complete(context.Background(), s.ID, h.provider, h.browser, h.actor, "complete")
	if err != nil {
		t.Fatal(err)
	}
	stage := stageOf(result, "token_exchange")
	if stage.Status != "failed" || stage.ErrorCode != "invalid_client" || stage.ErrorDescription != "" {
		t.Fatalf("stage=%+v", stage)
	}
}
