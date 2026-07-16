package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/federation"
	"prohibitorum/pkg/federation/providers/vrchat"
)

type fakeVRChatOperatorService struct {
	result                                                       vrchat.OperatorSessionResult
	err                                                          error
	action                                                       string
	slug, username, password, challenge, method, code, sessionID string
	accountID                                                    int32
}

func (f *fakeVRChatOperatorService) Start(_ context.Context, slug string, accountID int32, sessionID, username, password string) (vrchat.OperatorSessionResult, error) {
	f.action = "start"
	f.slug, f.accountID, f.sessionID, f.username, f.password = slug, accountID, sessionID, username, password
	return f.result, f.err
}
func (f *fakeVRChatOperatorService) Verify(_ context.Context, slug string, accountID int32, sessionID, challenge, method, code string) (vrchat.OperatorSessionResult, error) {
	f.action = "verify"
	f.slug, f.accountID, f.sessionID, f.challenge, f.method, f.code = slug, accountID, sessionID, challenge, method, code
	return f.result, f.err
}
func (f *fakeVRChatOperatorService) Validate(_ context.Context, slug string) (vrchat.OperatorSessionResult, error) {
	f.action = "validate"
	f.slug = slug
	return f.result, f.err
}

type vrchatAuditCapture struct{ records []audit.Record }

func (c *vrchatAuditCapture) Record(_ context.Context, r audit.Record) error {
	c.records = append(c.records, r)
	return nil
}

func vrchatHandlerServer(t *testing.T, service *fakeVRChatOperatorService) (*Server, *vrchatAuditCapture) {
	t.Helper()
	registry := federation.NewRegistry()
	if err := registry.RegisterDefinition(vrchat.Definition{}); err != nil {
		t.Fatal(err)
	}
	capture := &vrchatAuditCapture{}
	return &Server{vrchatOperatorOverride: service, federationRegistry: registry, Audit: capture}, capture
}
func vrchatAdminRequest(t *testing.T, method, path, body string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("slug", "social")
	ctx := context.WithValue(r.Context(), chi.RouteCtxKey, routeCtx)
	ctx = authn.WithSession(ctx, &authn.Session{Account: &db.Account{ID: 7, Role: "admin"}, Data: &authn.SessionData{AccountID: 7, SessionID: "sid-7", SudoUntil: time.Now().Add(time.Minute)}})
	return r.WithContext(ctx)
}
func validVRChatRow() db.UpstreamIdp {
	return db.UpstreamIdp{ID: 42, Slug: "social", DisplayName: "VRChat", Protocol: "vrchat", Mode: "link_only", ProviderConfig: []byte(`{}`), SecretEnc: []byte{1}, SecretNonce: []byte{2}, KeyVersion: pgtype.Int4{Int32: 3, Valid: true}, SecretStatus: "valid", SecretValidatedAt: pgtype.Timestamptz{Time: time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC), Valid: true}, Disabled: true}
}

func TestVRChatOperatorStartResponseOmitsSecretAndChallengeFieldsWhenValid(t *testing.T) {
	row := validVRChatRow()
	fake := &fakeVRChatOperatorService{result: vrchat.OperatorSessionResult{Status: vrchat.OperatorStatusValid, Provider: &row}}
	s, audits := vrchatHandlerServer(t, fake)
	w := httptest.NewRecorder()
	s.handleVRChatOperatorStartHTTP(w, vrchatAdminRequest(t, http.MethodPost, "/api/prohibitorum/identity-providers/social/operator-session/start", `{"username":"private-user","password":"private-password"}`))
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(got))
	for key := range got {
		keys = append(keys, key)
	}
	if !sameStringSet(keys, []string{"status", "provider"}) {
		t.Fatalf("top-level keys=%v body=%s", keys, w.Body.String())
	}
	if _, ok := got["challenge"]; ok {
		t.Fatalf("challenge present: %s", w.Body.String())
	}
	if _, ok := got["methods"]; ok {
		t.Fatalf("methods present: %s", w.Body.String())
	}
	if _, ok := got["expiresAt"]; ok {
		t.Fatalf("expiry present: %s", w.Body.String())
	}
	if got["status"] != "valid" || got["provider"] == nil {
		t.Fatalf("response=%s", w.Body.String())
	}
	if containsAnyString(w.Body.String(), "private-user", "private-password") {
		t.Fatalf("credentials leaked: %s", w.Body.String())
	}
	if fake.accountID != 7 || fake.sessionID != "sid-7" || fake.slug != "social" {
		t.Fatalf("bindings=%+v", fake)
	}
	assertVRChatAuditAllowlist(t, audits.records)
}

func TestVRChatOperatorChallengeResponseExact(t *testing.T) {
	expires := time.Date(2026, 7, 16, 12, 10, 0, 0, time.UTC)
	fake := &fakeVRChatOperatorService{result: vrchat.OperatorSessionResult{Status: vrchat.OperatorStatusChallenge, Challenge: "opaque", Methods: []string{"totp", "emailOtp"}, ExpiresAt: &expires}}
	s, _ := vrchatHandlerServer(t, fake)
	w := httptest.NewRecorder()
	s.handleVRChatOperatorStartHTTP(w, vrchatAdminRequest(t, http.MethodPost, "/api/prohibitorum/identity-providers/social/operator-session/start", `{"username":"u","password":"p"}`))
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var got map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	wantKeys := []string{"challenge", "expiresAt", "methods", "status"}
	keys := make([]string, 0, len(got))
	for key := range got {
		keys = append(keys, key)
	}
	if !sameStringSet(keys, wantKeys) {
		t.Fatalf("keys=%v body=%s", keys, w.Body.String())
	}
}

func TestVRChatOperatorHandlersRejectUnknownTrailingAndMissingFields(t *testing.T) {
	cases := []struct {
		name, route, body string
		handler           func(*Server, http.ResponseWriter, *http.Request)
	}{{"start unknown", "start", `{"username":"u","password":"p","extra":1}`, (*Server).handleVRChatOperatorStartHTTP}, {"start trailing", "start", `{"username":"u","password":"p"}{}`, (*Server).handleVRChatOperatorStartHTTP}, {"start missing", "start", `{"username":"u"}`, (*Server).handleVRChatOperatorStartHTTP}, {"verify unknown", "verify", `{"challenge":"c","method":"totp","code":"1","extra":1}`, (*Server).handleVRChatOperatorVerifyHTTP}, {"verify trailing", "verify", `{"challenge":"c","method":"totp","code":"1"}[]`, (*Server).handleVRChatOperatorVerifyHTTP}, {"verify missing", "verify", `{"challenge":"c","method":"totp"}`, (*Server).handleVRChatOperatorVerifyHTTP}, {"validate body", "validate", `{}`, (*Server).handleVRChatOperatorValidateHTTP}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeVRChatOperatorService{}
			s, _ := vrchatHandlerServer(t, fake)
			w := httptest.NewRecorder()
			tc.handler(s, w, vrchatAdminRequest(t, http.MethodPost, "/api/prohibitorum/identity-providers/social/operator-session/"+tc.route, tc.body))
			if w.Code != 400 || !strings.Contains(w.Body.String(), "bad_request") || fake.action != "" {
				t.Fatalf("status=%d body=%s action=%q", w.Code, w.Body.String(), fake.action)
			}
		})
	}
}

func TestVRChatOperatorErrorMappingAndAuditRedaction(t *testing.T) {
	cases := []struct {
		category vrchat.OperatorCategory
		code     string
		status   int
		retry    time.Duration
	}{{vrchat.OperatorCategoryCredentialsInvalid, "vrchat_operator_credentials_invalid", 422, 0}, {vrchat.OperatorCategoryChallengeInvalid, "vrchat_operator_challenge_invalid", 410, 0}, {vrchat.OperatorCategoryCodeInvalid, "vrchat_operator_code_invalid", 422, 0}, {vrchat.OperatorCategoryUpstreamTemporarilyUnavailable, "upstream_temporarily_unavailable", 503, 0}, {vrchat.OperatorCategoryUpstreamRateLimited, "upstream_rate_limited", 429, 13 * time.Second}, {vrchat.OperatorCategoryProviderNotReady, "provider_not_ready", 503, 0}, {vrchat.OperatorCategoryDatabaseUnavailable, "database_unavailable", 503, 0}, {vrchat.OperatorCategoryKVUnavailable, "kv_unavailable", 503, 0}}
	for _, tc := range cases {
		t.Run(string(tc.category), func(t *testing.T) {
			fake := &fakeVRChatOperatorService{err: &vrchat.OperatorError{Category: tc.category, RetryAfter: tc.retry}}
			s, audits := vrchatHandlerServer(t, fake)
			w := httptest.NewRecorder()
			s.handleVRChatOperatorVerifyHTTP(w, vrchatAdminRequest(t, http.MethodPost, "/api/prohibitorum/identity-providers/social/operator-session/verify", `{"challenge":"private-challenge","method":"totp","code":"private-code"}`))
			if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.code) || containsAnyString(w.Body.String(), "private-challenge", "private-code") {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if tc.retry > 0 && w.Header().Get("Retry-After") != "13" {
				t.Fatalf("retry-after=%q", w.Header().Get("Retry-After"))
			}
			assertVRChatAuditAllowlist(t, audits.records)
		})
	}
}

func TestVRChatOperatorAuditDoesNotPersistUnrecognizedMethod(t *testing.T) {
	fake := &fakeVRChatOperatorService{err: &vrchat.OperatorError{Category: vrchat.OperatorCategoryChallengeInvalid}}
	s, capture := vrchatHandlerServer(t, fake)
	w := httptest.NewRecorder()
	s.handleVRChatOperatorVerifyHTTP(w, vrchatAdminRequest(t, http.MethodPost, "/api/prohibitorum/identity-providers/social/operator-session/verify", `{"challenge":"c","method":"credential-disguised-as-method","code":"x"}`))
	for _, record := range capture.records {
		if _, exists := record.Detail["method"]; exists {
			t.Fatalf("unrecognized request method persisted in audit: %#v", record.Detail)
		}
	}
}

func TestVRChatOperatorAuditLifecycle(t *testing.T) {
	row := validVRChatRow()
	tests := []struct {
		name   string
		invoke func(*Server, http.ResponseWriter, *http.Request)
		body   string
		result vrchat.OperatorSessionResult
		err    error
		events []string
	}{{"challenge", (*Server).handleVRChatOperatorStartHTTP, `{"username":"u","password":"p"}`, vrchat.OperatorSessionResult{Status: "challenge", Challenge: "c", Methods: []string{"totp"}, ExpiresAt: new(time.Now().Add(time.Minute))}, nil, []string{"vrchat_operator_setup_started", "vrchat_operator_challenge_issued"}}, {"verify success", (*Server).handleVRChatOperatorVerifyHTTP, `{"challenge":"c","method":"totp","code":"x"}`, vrchat.OperatorSessionResult{Status: "valid", Provider: &row}, nil, []string{"vrchat_operator_setup_started", "vrchat_operator_session_validated"}}, {"validate invalid", (*Server).handleVRChatOperatorValidateHTTP, "", vrchat.OperatorSessionResult{}, &vrchat.OperatorError{Category: vrchat.OperatorCategoryCredentialsInvalid}, []string{"vrchat_operator_setup_started", "vrchat_operator_session_invalidated"}}}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if op, ok := tc.err.(*vrchat.OperatorError); ok && tc.name == "validate invalid" {
				op.SessionInvalidated = true
			}
			fake := &fakeVRChatOperatorService{result: tc.result, err: tc.err}
			s, capture := vrchatHandlerServer(t, fake)
			w := httptest.NewRecorder()
			tc.invoke(s, w, vrchatAdminRequest(t, http.MethodPost, "/api/prohibitorum/identity-providers/social/operator-session/x", tc.body))
			events := make([]string, len(capture.records))
			for i := range capture.records {
				events[i] = capture.records[i].Event
			}
			if !reflect.DeepEqual(events, tc.events) {
				t.Fatalf("events=%v want=%v", events, tc.events)
			}
			assertVRChatAuditAllowlist(t, capture.records)
		})
	}
}

func TestVRChatOperatorAuditDoesNotInvalidateReplacementSession(t *testing.T) {
	fake := &fakeVRChatOperatorService{err: &vrchat.OperatorError{Category: vrchat.OperatorCategoryCredentialsInvalid}}
	s, capture := vrchatHandlerServer(t, fake)
	w := httptest.NewRecorder()
	s.handleVRChatOperatorValidateHTTP(w, vrchatAdminRequest(t, http.MethodPost, "/api/prohibitorum/identity-providers/social/operator-session/validate", ""))
	if len(capture.records) != 1 || capture.records[0].Event != "vrchat_operator_setup_started" {
		t.Fatalf("events=%+v", capture.records)
	}
}

func assertVRChatAuditAllowlist(t *testing.T, records []audit.Record) {
	t.Helper()
	allowed := map[string]bool{"slug": true, "action": true, "method": true, "methods": true, "category": true}
	for _, record := range records {
		for key, value := range record.Detail {
			if !allowed[key] {
				t.Fatalf("audit key %q not allowed", key)
			}
			encoded, _ := json.Marshal(value)
			if containsAnyString(string(encoded), "private-user", "private-password", "private-code", "private-challenge", "opaque") {
				t.Fatalf("audit secret leak: %s", encoded)
			}
		}
	}
}
func containsAnyString(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}
func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	m := map[string]int{}
	for _, v := range a {
		m[v]++
	}
	for _, v := range b {
		m[v]--
	}
	for _, v := range m {
		if v != 0 {
			return false
		}
	}
	return true
}

func TestVRChatOperatorRoutesUseSudoAndBodyGate(t *testing.T) {
	router := chi.NewRouter()
	s := &Server{}
	s.registerSudoOpHTTP(router, http.MethodPost, "/api/prohibitorum/identity-providers/{slug}/operator-session/start", contract.AuthRequirement{Kind: contract.AuthAdmin}, s.handleVRChatOperatorStartHTTP)
	for name, request := range map[string]*http.Request{"no sudo": vrchatAdminRequest(t, http.MethodPost, "/api/prohibitorum/identity-providers/social/operator-session/start", `{"username":"u","password":"p"}`), "oversize": vrchatAdminRequest(t, http.MethodPost, "/api/prohibitorum/identity-providers/social/operator-session/start", `{"username":"`+strings.Repeat("x", 70<<10)+`","password":"p"}`)} {
		t.Run(name, func(t *testing.T) {
			request.Header.Set("Content-Type", "application/json")
			if name == "no sudo" {
				sess := authn.SessionFromContext(request.Context())
				sess.Data.SudoUntil = time.Time{}
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, request)
			if name == "no sudo" && w.Code != 401 {
				t.Fatalf("status=%d", w.Code)
			}
			if name == "oversize" && w.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("status=%d", w.Code)
			}
		})
	}
}
