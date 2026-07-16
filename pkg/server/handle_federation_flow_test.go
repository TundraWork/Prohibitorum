package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/clientip"
	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/federation"
	"prohibitorum/pkg/kv"
	sessstore "prohibitorum/pkg/session"
)

const (
	localProviderSlug = "vrchat"
	localUserID       = "usr_00000000-0000-0000-0000-000000000000"
	localIssuer       = "https://api.vrchat.cloud"
)

type localFlowDefinition struct{ ready bool }

func (localFlowDefinition) Protocol() string { return "vrchat" }
func (localFlowDefinition) Descriptor() federation.Descriptor {
	return federation.Descriptor{Protocol: "vrchat", RequiresSecret: true}
}
func (localFlowDefinition) ValidateConfig(json.RawMessage) error { return nil }
func (localFlowDefinition) ValidateSecret([]byte) error          { return nil }
func (d localFlowDefinition) Ready(provider federation.Provider) bool {
	return d.ready && provider.SecretStatus == "valid"
}

type localFlowAdapter struct {
	mu         sync.Mutex
	beginCalls int
	inputs     []federation.ActionInput
	verifyErr  error
}

func (*localFlowAdapter) Protocol() string { return "vrchat" }
func (a *localFlowAdapter) Begin(_ context.Context, _ federation.Provider, _ federation.BeginContext) (json.RawMessage, federation.NextAction, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.beginCalls++
	return json.RawMessage(`{"private":"identify-secret"}`), federation.NextAction{Kind: federation.ActionCollectIdentity}, nil
}
func (a *localFlowAdapter) Advance(_ context.Context, _ federation.Provider, _ json.RawMessage, input federation.ActionInput) (federation.AdvanceResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.inputs = append(a.inputs, input)
	switch input.Kind {
	case federation.ActionCollectIdentity:
		return federation.AdvanceResult{
			State: json.RawMessage(`{"private":"proof-secret"}`),
			Next: &federation.NextAction{Kind: federation.ActionPublishProof, Public: map[string]any{
				"profileUrl": "https://vrchat.com/home/user/" + localUserID,
				"proofUrl":   "https://id.example.test/verify/vrchat/public-proof",
				"private":    "must-not-project",
			}},
			Candidate: &federation.IdentityKey{Issuer: localIssuer, Subject: localUserID},
		}, nil
	case federation.ActionPublishProof:
		if a.verifyErr != nil {
			return federation.AdvanceResult{}, a.verifyErr
		}
		return federation.AdvanceResult{Identity: &federation.VerifiedIdentity{
			Issuer: localIssuer, Subject: localUserID, Username: "vrchat-user", DisplayName: "VRChat User", AMR: []string{"vrchat_profile_proof"},
		}}, nil
	default:
		return federation.AdvanceResult{}, errors.New("unexpected action")
	}
}

func (a *localFlowAdapter) snapshotInputs() []federation.ActionInput {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]federation.ActionInput(nil), a.inputs...)
}

type directIPStore struct{}

func (directIPStore) Get(context.Context) (clientip.Stored, error) {
	return clientip.Stored{Strategy: "direct"}, nil
}
func (directIPStore) Set(context.Context, clientip.Stored) error { return nil }

type confirmFailStore struct {
	kv.Store
	fail bool
}

func (s *confirmFailStore) SetEx(ctx context.Context, key, value string, ttl time.Duration) error {
	if s.fail && strings.HasPrefix(key, "federation:confirm:") {
		return errors.New("confirmation storage unavailable")
	}
	return s.Store.SetEx(ctx, key, value, ttl)
}

type localFlowHarness struct {
	t       *testing.T
	s       *Server
	q       *fakeFedQueries
	store   kv.Store
	adapter *localFlowAdapter
	ts      *httptest.Server
	client  *http.Client
}

func newLocalFlowHarnessWithConfirmFailure(t *testing.T) (*localFlowHarness, *confirmFailStore) {
	t.Helper()
	base := kv.NewMemoryStore()
	t.Cleanup(func() { _ = base.Close() })
	store := &confirmFailStore{Store: base, fail: true}
	return newLocalFlowHarnessWithStore(t, store), store
}

func newLocalFlowHarness(t *testing.T) *localFlowHarness {
	t.Helper()
	base := kv.NewMemoryStore()
	t.Cleanup(func() { _ = base.Close() })
	return newLocalFlowHarnessWithStore(t, base)
}

func newLocalFlowHarnessWithStore(t *testing.T, store kv.Store) *localFlowHarness {
	t.Helper()
	q := newFakeFedQueries()
	provider := db.UpstreamIdp{
		ID: 77, Slug: localProviderSlug, DisplayName: "VRChat", Protocol: "vrchat",
		Mode: federation.ModeAutoProvision, ProviderConfig: json.RawMessage(`{}`), SecretStatus: "valid",
	}
	q.idpBySlug[provider.Slug] = provider
	q.identityErr = nil
	q.identityResult = db.AccountIdentity{
		ID: 88, AccountID: 99, UpstreamIdpID: provider.ID, UpstreamIss: localIssuer, UpstreamSub: localUserID,
		ConfirmedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}
	q.accountByIDResults[99] = db.Account{ID: 99, Username: "vrchat-user", DisplayName: "VRChat User"}

	writer := audit.NewWriter(q)
	registry := federation.NewRegistry()
	if err := registry.RegisterDefinition(localFlowDefinition{ready: true}); err != nil {
		t.Fatal(err)
	}
	adapter := &localFlowAdapter{}
	if err := registry.RegisterAdapter(adapter); err != nil {
		t.Fatal(err)
	}
	service := federation.NewService(registry, federation.NewProviderStore(q), store, federation.NewResolver(q, writer, nil), federation.ServiceConfig{
		StateTTL: 5 * time.Minute, PublicOrigin: "https://id.example.test", Audit: writer,
	})
	cfg := &configx.Config{SessionTTL: time.Hour}
	s := &Server{
		config: cfg, kvStore: store, sessionStore: sessstore.NewSessionStore(store, q, time.Hour),
		federationService: service, Audit: writer, clientIP: clientip.NewResolver(directIPStore{}),
	}
	router := chi.NewRouter()
	router.Get("/api/prohibitorum/auth/federation/{slug}/login", s.handleFederationLoginHTTP)
	router.Get("/api/prohibitorum/enrollments/{token}/start-federation", s.handleEnrollmentStartFederationHTTP)
	router.Get("/api/prohibitorum/auth/federation/flows/{flow}", s.handleFederationFlowGetHTTP)
	router.Post("/api/prohibitorum/auth/federation/flows/{flow}/prepare", withFederationFlowBodyControls(s.handleFederationFlowPrepareHTTP))
	router.Post("/api/prohibitorum/auth/federation/flows/{flow}/verify", withFederationFlowBodyControls(s.handleFederationFlowVerifyHTTP))
	ts := httptest.NewServer(router)
	t.Cleanup(ts.Close)
	cfg.PublicOrigins = []string{ts.URL}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &localFlowHarness{t: t, s: s, q: q, store: store, adapter: adapter, ts: ts, client: client}
}

func (h *localFlowHarness) beginLogin(t *testing.T) (flow string, response *http.Response) {
	t.Helper()
	response, err := h.client.Get(h.ts.URL + "/api/prohibitorum/auth/federation/" + localProviderSlug + "/login?return_to=%2Fafter")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	prefix := "/federation/flow/"
	location := response.Header.Get("Location")
	if response.StatusCode != http.StatusFound || !strings.HasPrefix(location, prefix) {
		t.Fatalf("begin status/location = %d %q", response.StatusCode, location)
	}
	return strings.TrimPrefix(location, prefix), response
}

func TestFederationFlowInviteBeginUsesLocalDestinationAndBindingCookie(t *testing.T) {
	h := newLocalFlowHarness(t)
	h.q.seedEnrollment(validInvite("local-invite", localProviderSlug, "invited-user"))
	response := h.request(t, http.MethodGet, "/api/prohibitorum/enrollments/local-invite/start-federation?return_to=%2Fafter", "")
	if response.StatusCode != http.StatusFound || !strings.HasPrefix(response.Header.Get("Location"), "/federation/flow/") {
		t.Fatalf("status/location = %d %q", response.StatusCode, response.Header.Get("Location"))
	}
	if response.Header.Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("referrer policy = %q", response.Header.Get("Referrer-Policy"))
	}
	var binding bool
	for _, cookie := range response.Cookies() {
		binding = binding || cookie.Name == sessstore.FedStateCookieName && cookie.Value != ""
	}
	if !binding {
		t.Fatal("invite begin omitted browser-binding cookie")
	}
}

func (h *localFlowHarness) request(t *testing.T, method, path, body string) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, h.ts.URL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := h.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	return response
}

func (h *localFlowHarness) rawRequest(t *testing.T, path, body, contentType string, chunked bool) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, h.ts.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if chunked {
		req.ContentLength = -1
		req.TransferEncoding = []string{"chunked"}
	}
	response, err := h.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	return response
}

func TestFederationFlowLoginPrepareVerify(t *testing.T) {
	h := newLocalFlowHarness(t)
	flow, beginResponse := h.beginLogin(t)
	if len(beginResponse.Cookies()) == 0 {
		t.Fatal("begin did not set browser-binding cookie")
	}

	response := h.request(t, http.MethodGet, "/api/prohibitorum/auth/federation/flows/"+url.PathEscape(flow), "")
	if response.StatusCode != http.StatusOK || response.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("GET = %d headers=%v", response.StatusCode, response.Header)
	}
	var identify map[string]any
	if err := json.NewDecoder(response.Body).Decode(&identify); err != nil {
		t.Fatal(err)
	}
	if identify["intent"] != "login" || identify["step"] != "identify" || identify["requiresLocalUsername"] != false {
		t.Fatalf("identify view = %#v", identify)
	}
	if provider, ok := identify["provider"].(map[string]any); !ok || provider["slug"] != "vrchat" || provider["displayName"] != "VRChat" || provider["protocol"] != "vrchat" {
		t.Fatalf("provider = %#v", identify["provider"])
	}
	encoded, _ := json.Marshal(identify)
	if bytes.Contains(encoded, []byte("private")) || bytes.Contains(encoded, []byte("public-proof")) {
		t.Fatalf("identify leaked state: %s", encoded)
	}

	response = h.request(t, http.MethodPost, "/api/prohibitorum/auth/federation/flows/"+flow+"/prepare", `{"identity":"`+localUserID+`"}`)
	if response.StatusCode != http.StatusOK || response.Header.Get("Cache-Control") != "no-store" {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("prepare = %d body=%s", response.StatusCode, body)
	}
	var proof map[string]any
	if err := json.NewDecoder(response.Body).Decode(&proof); err != nil {
		t.Fatal(err)
	}
	if proof["step"] != "proof" || proof["profileUrl"] == "" || proof["proofUrl"] == "" {
		t.Fatalf("proof view = %#v", proof)
	}
	encoded, _ = json.Marshal(proof)
	if bytes.Contains(encoded, []byte("private")) || bytes.Contains(encoded, []byte("proof-secret")) {
		t.Fatalf("proof leaked private state: %s", encoded)
	}

	response = h.request(t, http.MethodPost, "/api/prohibitorum/auth/federation/flows/"+flow+"/verify", `{}`)
	if response.StatusCode != http.StatusOK || response.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("verify = %d", response.StatusCode)
	}
	var result contract.LoginResult
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.Redirect != "/after" {
		t.Fatalf("redirect = %q", result.Redirect)
	}
	inputs := h.adapter.snapshotInputs()
	if len(inputs) != 2 || inputs[0].Kind != federation.ActionCollectIdentity || inputs[0].Identity != localUserID || inputs[0].LocalUsername != "" || inputs[1].Kind != federation.ActionPublishProof || inputs[1].Identity != "" {
		t.Fatalf("adapter inputs = %+v", inputs)
	}

	replay := h.request(t, http.MethodPost, "/api/prohibitorum/auth/federation/flows/"+flow+"/verify", `{}`)
	if replay.StatusCode != http.StatusUnauthorized {
		t.Fatalf("replay status = %d", replay.StatusCode)
	}
}

func TestFederationFlowBodyControlFailuresAreNoStore(t *testing.T) {
	for _, endpoint := range []string{"prepare", "verify"} {
		t.Run(endpoint, func(t *testing.T) {
			tests := []struct {
				name        string
				body        string
				contentType string
				chunked     bool
				status      int
			}{
				{name: "malformed", body: `{`, contentType: "application/json", status: http.StatusBadRequest},
				{name: "missing content type", body: `{}`, status: http.StatusBadRequest},
				{name: "wrong content type", body: `{}`, contentType: "text/plain", status: http.StatusBadRequest},
				{name: "advertised oversized", body: `{"value":"` + strings.Repeat("x", maxAdminBody) + `"}`, contentType: "application/json", status: http.StatusRequestEntityTooLarge},
				{name: "chunked oversized", body: `{}` + strings.Repeat(" ", maxAdminBody+1), contentType: "application/json", chunked: true, status: http.StatusRequestEntityTooLarge},
			}
			for _, test := range tests {
				t.Run(test.name, func(t *testing.T) {
					h := newLocalFlowHarness(t)
					response := h.rawRequest(t, "/api/prohibitorum/auth/federation/flows/opaque/"+endpoint, test.body, test.contentType, test.chunked)
					if response.StatusCode != test.status {
						t.Fatalf("status = %d, want %d", response.StatusCode, test.status)
					}
					if response.Header.Get("Cache-Control") != "no-store" {
						t.Fatalf("Cache-Control = %q", response.Header.Get("Cache-Control"))
					}
				})
			}
		})
	}
}

func TestFederationFlowRejectsActionSkippingAndInvalidBodies(t *testing.T) {
	for _, test := range []struct {
		name, body string
		wantStatus int
		wantCode   string
	}{
		{"action skip", `{}`, http.StatusConflict, "federation_action_invalid"},
		{"malformed", `{`, http.StatusBadRequest, "bad_request"},
		{"extra provider", `{"identity":"` + localUserID + `","provider":"other"}`, http.StatusBadRequest, "bad_request"},
		{"extra protocol", `{"identity":"` + localUserID + `","protocol":"oidc"}`, http.StatusBadRequest, "bad_request"},
		{"extra intent", `{"identity":"` + localUserID + `","intent":"link"}`, http.StatusBadRequest, "bad_request"},
		{"extra action", `{"identity":"` + localUserID + `","action":"publish_proof"}`, http.StatusBadRequest, "bad_request"},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newLocalFlowHarness(t)
			flow, _ := h.beginLogin(t)
			path := "/api/prohibitorum/auth/federation/flows/" + flow + "/prepare"
			if test.name == "action skip" {
				path = "/api/prohibitorum/auth/federation/flows/" + flow + "/verify"
			}
			response := h.request(t, http.MethodPost, path, test.body)
			if response.StatusCode != test.wantStatus || response.Header.Get("Cache-Control") != "no-store" {
				t.Fatalf("status = %d", response.StatusCode)
			}
			var public struct {
				Code string `json:"code"`
			}
			if err := json.NewDecoder(response.Body).Decode(&public); err != nil {
				t.Fatal(err)
			}
			if public.Code != test.wantCode {
				t.Fatalf("code = %q", public.Code)
			}
		})
	}

	h := newLocalFlowHarness(t)
	flow, _ := h.beginLogin(t)
	response := h.request(t, http.MethodPost, "/api/prohibitorum/auth/federation/flows/"+flow+"/prepare", `{"identity":"`+strings.Repeat("x", maxAdminBody)+`"}`)
	if response.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized = %d", response.StatusCode)
	}
}

func TestFederationFlowLocalUsernameRequiredIsRetryable(t *testing.T) {
	h := newLocalFlowHarness(t)
	h.q.identityErr = pgx.ErrNoRows
	flow, _ := h.beginLogin(t)
	prepare := h.request(t, http.MethodPost, "/api/prohibitorum/auth/federation/flows/"+flow+"/prepare", `{"identity":"`+localUserID+`"}`)
	if prepare.StatusCode != http.StatusOK {
		t.Fatalf("prepare = %d", prepare.StatusCode)
	}
	var proof federationFlowView
	if err := json.NewDecoder(prepare.Body).Decode(&proof); err != nil {
		t.Fatal(err)
	}
	if !proof.RequiresLocalUsername {
		t.Fatal("unknown auto-provision identity did not require local username")
	}
	verify := h.request(t, http.MethodPost, "/api/prohibitorum/auth/federation/flows/"+flow+"/verify", `{}`)
	if verify.StatusCode != http.StatusConflict {
		t.Fatalf("verify = %d body unavailable", verify.StatusCode)
	}
	var public struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(verify.Body).Decode(&public); err != nil {
		t.Fatal(err)
	}
	if public.Code != "local_username_required" {
		t.Fatalf("code = %q", public.Code)
	}
	retry := h.request(t, http.MethodPost, "/api/prohibitorum/auth/federation/flows/"+flow+"/verify", `{"localUsername":"new-vrchat-user"}`)
	if retry.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(retry.Body)
		t.Fatalf("retry = %d body=%s", retry.StatusCode, body)
	}
}

func TestFederationFlowRateLimitSetsRetryAfter(t *testing.T) {
	h := newLocalFlowHarness(t)
	flow, _ := h.beginLogin(t)
	if response := h.request(t, http.MethodPost, "/api/prohibitorum/auth/federation/flows/"+flow+"/prepare", `{"identity":"`+localUserID+`"}`); response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("prepare = %d body=%s", response.StatusCode, body)
	}
	h.adapter.verifyErr = federation.NewRateLimitedFailure(17 * time.Second)
	response := h.request(t, http.MethodPost, "/api/prohibitorum/auth/federation/flows/"+flow+"/verify", `{}`)
	if response.StatusCode != http.StatusTooManyRequests || response.Header.Get("Retry-After") != "17" || response.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("rate response = %d %#v", response.StatusCode, response.Header)
	}
}

func TestFederationFlowIdentityConflictIsGeneric(t *testing.T) {
	h := newLocalFlowHarness(t)
	flow, _ := h.beginLogin(t)
	if response := h.request(t, http.MethodPost, "/api/prohibitorum/auth/federation/flows/"+flow+"/prepare", `{"identity":"`+localUserID+`"}`); response.StatusCode != http.StatusOK {
		t.Fatalf("prepare = %d", response.StatusCode)
	}
	h.adapter.verifyErr = federation.NewFailure(federation.FailureLinkConflict, map[string]any{
		"iss": "must-not-leak", "sub": "must-not-leak",
	})
	response := h.request(t, http.MethodPost, "/api/prohibitorum/auth/federation/flows/"+flow+"/verify", `{}`)
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d body=%s", response.StatusCode, response.Body)
	}
	var public struct {
		Code    string         `json:"code"`
		Details map[string]any `json:"details"`
	}
	if err := json.NewDecoder(response.Body).Decode(&public); err != nil {
		t.Fatal(err)
	}
	if public.Code != "federation_identity_conflict" || len(public.Details) != 0 {
		t.Fatalf("public conflict = %+v", public)
	}
}

func TestFederationFlowProviderTamperingFailsBeforeAdapter(t *testing.T) {
	for _, field := range []string{"id", "slug", "protocol"} {
		t.Run(field, func(t *testing.T) {
			h := newLocalFlowHarness(t)
			flow, _ := h.beginLogin(t)
			raw, err := h.store.Get(context.Background(), federation.FlowKey(flow))
			if err != nil {
				t.Fatal(err)
			}
			state, err := federation.DecodeFlowState(raw)
			if err != nil {
				t.Fatal(err)
			}
			switch field {
			case "id":
				state.ProviderID++
			case "slug":
				state.ProviderSlug = "other"
			case "protocol":
				state.Protocol = "oidc"
			}
			tampered, err := state.Encode()
			if err != nil {
				t.Fatal(err)
			}
			if err := h.store.SetEx(context.Background(), federation.FlowKey(flow), tampered, time.Minute); err != nil {
				t.Fatal(err)
			}
			before := len(h.adapter.snapshotInputs())
			response := h.request(t, http.MethodPost, "/api/prohibitorum/auth/federation/flows/"+flow+"/prepare", `{"identity":"`+localUserID+`"}`)
			if response.StatusCode != http.StatusUnauthorized {
				t.Fatalf("status = %d", response.StatusCode)
			}
			if len(h.adapter.snapshotInputs()) != before {
				t.Fatal("adapter called for tampered provider binding")
			}
		})
	}
}

func TestFederationFlowProviderBecomingUnreadyReturnsStableError(t *testing.T) {
	h := newLocalFlowHarness(t)
	flow, _ := h.beginLogin(t)
	provider := h.q.idpBySlug[localProviderSlug]
	provider.SecretStatus = "invalid"
	h.q.idpBySlug[localProviderSlug] = provider
	response := h.request(t, http.MethodGet, "/api/prohibitorum/auth/federation/flows/"+flow, "")
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", response.StatusCode)
	}
	var public struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(response.Body).Decode(&public); err != nil {
		t.Fatal(err)
	}
	if public.Code != "provider_not_ready" {
		t.Fatalf("code = %q", public.Code)
	}
}

func TestFederationFlowReadRequiresBrowserCookie(t *testing.T) {
	h := newLocalFlowHarness(t)
	flow, _ := h.beginLogin(t)
	client := &http.Client{}
	response, err := client.Get(h.ts.URL + "/api/prohibitorum/auth/federation/flows/" + flow)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized || response.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("status = %d", response.StatusCode)
	}
}

func TestFederationFlowLocalLinkUsesCurrentSessionAndNeverMintsSession(t *testing.T) {
	h := newLocalFlowHarness(t)
	begin, err := h.s.federationService.BeginLink(context.Background(), localProviderSlug, "/ignored", 99, "link-session")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/prohibitorum/auth/federation/flows/"+begin.FlowID, nil)
	req.AddCookie(&http.Cookie{Name: sessstore.FedStateCookieName, Value: begin.BrowserToken})
	req = req.WithContext(authn.WithSession(req.Context(), &authn.Session{Account: &db.Account{ID: 99}, Data: &authn.SessionData{AccountID: 99, SessionID: "link-session"}}))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("flow", begin.FlowID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rr := httptest.NewRecorder()
	h.s.handleFederationFlowGetHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("bound GET = %d %s", rr.Code, rr.Body.String())
	}

	swapped := req.Clone(authn.WithSession(req.Context(), &authn.Session{Account: &db.Account{ID: 99}, Data: &authn.SessionData{AccountID: 99, SessionID: "other"}}))
	rr = httptest.NewRecorder()
	h.s.handleFederationFlowGetHTTP(rr, swapped)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("swapped GET = %d", rr.Code)
	}
}

func TestFederationCompletionConfirmedLoginJSON(t *testing.T) {
	h := newLocalFlowHarness(t)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/complete", nil)
	h.s.writeFederationCompletion(rr, req, &federation.CompletionResult{
		Intent: federation.IntentLogin, AccountID: 99, IdentityID: 88,
		ProviderID: 77, ProviderSlug: localProviderSlug, ReturnTo: "/after",
		AMR: []string{"vrchat_profile_proof"}, Confirmed: true,
	}, federationCompletionJSON)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	var result contract.LoginResult
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.Redirect != "/after" || len(h.q.sessions) != 1 {
		t.Fatalf("result=%+v sessions=%d", result, len(h.q.sessions))
	}
	var cleared, session bool
	for _, cookie := range rr.Result().Cookies() {
		cleared = cleared || cookie.Name == sessstore.FedStateCookieName && cookie.MaxAge < 0
		session = session || cookie.Name == sessstore.SessionCookieName && cookie.Value != ""
	}
	if !cleared || !session {
		t.Fatalf("completion cookies = %+v", rr.Result().Cookies())
	}
}

func TestFederationCompletionLinkAlwaysConnectedWithoutSession(t *testing.T) {
	h := newLocalFlowHarness(t)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/complete", nil)
	h.s.writeFederationCompletion(rr, req, &federation.CompletionResult{
		Intent: federation.IntentLink, AccountID: 99, ProviderID: 77, ReturnTo: "/attacker-selected",
	}, federationCompletionJSON)
	if rr.Code != http.StatusOK || len(h.q.sessions) != 0 {
		t.Fatalf("status=%d sessions=%d body=%s", rr.Code, len(h.q.sessions), rr.Body.String())
	}
	var result contract.LoginResult
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.Redirect != "/connected" {
		t.Fatalf("redirect = %q", result.Redirect)
	}
}

func TestFederationCompletionSessionDeliveryFailureReturns500(t *testing.T) {
	h := newLocalFlowHarness(t)
	h.q.sessionInsertErr = errors.New("session storage unavailable")
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/complete", nil)
	h.s.writeFederationCompletion(rr, req, &federation.CompletionResult{
		Intent: federation.IntentLogin, AccountID: 99, IdentityID: 88,
		ProviderID: 77, ProviderSlug: localProviderSlug, ReturnTo: "/after",
		Confirmed: true,
	}, federationCompletionJSON)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestFederationCompletionGrantDeliveryFailureConsumesCommittedFlow(t *testing.T) {
	h, store := newLocalFlowHarnessWithConfirmFailure(t)
	h.q.identityErr = pgx.ErrNoRows
	flow, _ := h.beginLogin(t)
	if response := h.request(t, http.MethodPost, "/api/prohibitorum/auth/federation/flows/"+flow+"/prepare", `{"identity":"`+localUserID+`"}`); response.StatusCode != http.StatusOK {
		t.Fatalf("prepare = %d", response.StatusCode)
	}
	failed := h.request(t, http.MethodPost, "/api/prohibitorum/auth/federation/flows/"+flow+"/verify", `{"localUsername":"committed-user"}`)
	if failed.StatusCode != http.StatusInternalServerError {
		body, _ := io.ReadAll(failed.Body)
		t.Fatalf("delivery failure = %d body=%s", failed.StatusCode, body)
	}
	if _, err := h.store.Get(context.Background(), federation.FlowKey(flow)); !errors.Is(err, kv.ErrKeyNotFound) {
		t.Fatalf("committed flow survived delivery failure: %v", err)
	}
	if len(h.q.insertedAccounts) != 1 || len(h.q.insertIdentitys) != 1 {
		t.Fatalf("committed mutations = accounts:%d identities:%d", len(h.q.insertedAccounts), len(h.q.insertIdentitys))
	}
	registerAudits := func() int {
		count := 0
		for _, event := range h.q.events {
			if event.Event == audit.EventRegister {
				count++
			}
		}
		return count
	}
	if got := registerAudits(); got != 1 {
		t.Fatalf("register audits after commit = %d", got)
	}
	replay := h.request(t, http.MethodPost, "/api/prohibitorum/auth/federation/flows/"+flow+"/verify", `{"localUsername":"committed-user"}`)
	if replay.StatusCode != http.StatusUnauthorized {
		t.Fatalf("replay = %d", replay.StatusCode)
	}

	account := h.q.insertedAccounts[0]
	identity := h.q.insertIdentitys[0]
	h.q.identityErr = nil
	h.q.identityResult = db.AccountIdentity{
		ID: 200, AccountID: account.ID, UpstreamIdpID: identity.UpstreamIdpID,
		UpstreamIss: identity.UpstreamIss, UpstreamSub: identity.UpstreamSub,
	}
	store.fail = false
	fresh, _ := h.beginLogin(t)
	if response := h.request(t, http.MethodPost, "/api/prohibitorum/auth/federation/flows/"+fresh+"/prepare", `{"identity":"`+localUserID+`"}`); response.StatusCode != http.StatusOK {
		t.Fatalf("fresh prepare = %d", response.StatusCode)
	}
	delivered := h.request(t, http.MethodPost, "/api/prohibitorum/auth/federation/flows/"+fresh+"/verify", `{}`)
	if delivered.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(delivered.Body)
		t.Fatalf("fresh verify = %d body=%s", delivered.StatusCode, body)
	}
	if len(h.q.insertedAccounts) != 1 || len(h.q.insertIdentitys) != 1 || registerAudits() != 1 {
		t.Fatalf("committed mutation repeated: accounts=%d identities=%d registerAudits=%d", len(h.q.insertedAccounts), len(h.q.insertIdentitys), registerAudits())
	}
}
