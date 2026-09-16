package oidc

import (
	"context"
	"encoding/json"
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
	tokenFailure    atomic.Bool
	userInfoFailure atomic.Bool
	callbackSeen    string
	callbackMu      sync.Mutex
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
		if r.URL.Path == "/userinfo" && h.userInfoFailure.Load() {
			http.Error(w, "secret-never-display", 500)
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
	h.service = NewDiagnostics(kv.NewMemoryStore(), store)
	return h
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
	browser := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := browser.Get(s.AuthorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	location, err := response.Location()
	if err != nil {
		t.Fatal(err)
	}
	code := location.Query().Get("code")
	if err := h.service.Callback(context.Background(), s.ID, h.provider.Slug, h.browser, code, "", location.Query().Get("iss"), "request-callback"); err != nil {
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
	if err := h.service.Callback(context.Background(), s.ID, h.provider.Slug, h.browser, code, "", "", "replay"); err == nil {
		t.Fatal("callback replay accepted")
	}
	result, err := h.service.Complete(context.Background(), s.ID, h.provider, h.browser, h.actor, "request-complete")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "succeeded" || result.Claims["subject"] != "subject" || len(result.Stages) != 6 {
		t.Fatalf("result=%+v", result)
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
	for _, secret := range []string{code, "secret-never-display", "access_token"} {
		if strings.Contains(raw, secret) {
			t.Fatalf("stored result contains %s", secret)
		}
	}
	var state diagnosticFlow
	_ = json.Unmarshal([]byte(raw), &state)
	if state.Code != "" || state.Nonce != "" || state.Verifier != "" {
		t.Fatal("transient credentials retained")
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
				if err := h.service.Callback(context.Background(), s.ID, h.provider.Slug, h.browser, "", "access_denied", "", "callback"); err != nil {
					t.Fatal(err)
				}
			case "callback issuer":
				if err := h.service.Callback(context.Background(), s.ID, h.provider.Slug, h.browser, "code", "", "https://other.example", "callback"); err != nil {
					t.Fatal(err)
				}
			default:
				if scenario == "id token" {
					h.op.OverrideIssuer("https://wrong.example")
				}
				browser := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
				res, err := browser.Get(s.AuthorizationURL)
				if err != nil {
					t.Fatal(err)
				}
				defer res.Body.Close()
				loc, err := res.Location()
				if err != nil {
					t.Fatal(err)
				}
				if err := h.service.Callback(context.Background(), s.ID, h.provider.Slug, h.browser, loc.Query().Get("code"), "", "", "callback"); err != nil {
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
			expected := map[string]string{"denied": "callback", "callback issuer": "callback", "token": "token_exchange", "id token": "id_token", "userinfo": "userinfo"}[scenario]
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
