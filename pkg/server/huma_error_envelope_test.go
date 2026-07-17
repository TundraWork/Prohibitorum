package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/diagnostic"
	"prohibitorum/pkg/weberr"
)

// buildTestAPI creates a huma.API with the production humaConfig (including
// the response transformer and NewError override) so end-to-end tests
// exercise the real humachi serialization path.
func buildTestAPI(t *testing.T) (*chi.Mux, huma.API) {
	t.Helper()
	router := chi.NewMux()
	api := humachi.New(router, humaConfig())
	return router, api
}

func buildTestAPIWithDiagnostic(t *testing.T, store diagnostic.StoreWriter) (*chi.Mux, huma.API) {
	t.Helper()
	router := chi.NewMux()
	router.Use(diagnosticCaptureMW(store))
	api := humachi.New(router, humaConfig())
	return router, api
}

// testSession returns a minimal admin session for test requests.
func testSession() *authn.Session {
	return &authn.Session{Account: &db.Account{ID: 1, Role: "admin", Username: "admin"}}
}

// testReqWithSession builds a request with a session in context and optional
// Content-Type.
func testReqWithSession(method, path, body, contentType string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return req.WithContext(authn.WithSession(req.Context(), testSession()))
}

// assertNoRFC9457Fields fails the test if the body contains any RFC 9457
// Problem Details field.
func assertNoRFC9457Fields(t *testing.T, body []byte) {
	t.Helper()
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v\nbody: %s", err, body)
	}
	for _, forbidden := range []string{"message", "detail", "title", "errors", "type", "instance", "$schema"} {
		if _, has := raw[forbidden]; has {
			t.Errorf("body contains forbidden RFC 9457 field %q: %s", forbidden, body)
		}
	}
}

// assertRequestIDMatchesHeader unmarshals the body as a PublicError and
// asserts the RequestID matches the X-Request-ID response header.
func assertRequestIDMatchesHeader(t *testing.T, rr *httptest.ResponseRecorder) weberr.PublicError {
	t.Helper()
	headerID := rr.Header().Get(weberr.HeaderRequestID)
	if headerID == "" {
		t.Fatal("X-Request-ID header is empty")
	}
	var env weberr.PublicError
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal body: %v\nbody: %s", err, rr.Body.String())
	}
	if env.RequestID != headerID {
		t.Errorf("body requestId = %q, want header id %q", env.RequestID, headerID)
	}
	return env
}

// --- Gap 1: Typed Huma returned PublicError must carry body requestId ---

func TestTypedHumaError_RequestIDInBody(t *testing.T) {
	router, api := buildTestAPI(t)

	type in struct {
		ID string `path:"id"`
	}
	type out struct{ Body struct{ OK bool } }
	registerOp(api, huma.Operation{
		Method: http.MethodGet,
		Path:   "/test/typed-err/{id}",
	}, func(ctx context.Context, input *in) (*out, error) {
		return nil, authErrToHuma(authn.ErrAccountNotFound())
	}, contract.AuthRequirement{Kind: contract.AuthSession})

	handler := weberr.RequestID(router)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, testReqWithSession(http.MethodGet, "/test/typed-err/42", "", ""))

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rr.Code)
	}
	env := assertRequestIDMatchesHeader(t, rr)
	if env.Code != "account_not_found" {
		t.Errorf("code = %q, want account_not_found", env.Code)
	}
	assertNoRFC9457Fields(t, rr.Body.Bytes())
}

// --- Gap 2a: Huma schema validation (too-short field) -> 422 validation_failed ---

func TestHumaValidationError_422_ValidationFailed(t *testing.T) {
	router, api := buildTestAPI(t)

	type body struct {
		Name string `json:"name" minLength:"2"`
	}
	type in struct{ Body body }
	type out struct{ Body struct{ OK bool } }
	registerOp(api, huma.Operation{
		Method: http.MethodPost,
		Path:   "/test/validation-err",
	}, func(ctx context.Context, input *in) (*out, error) {
		return &out{Body: struct{ OK bool }{OK: true}}, nil
	}, contract.AuthRequirement{Kind: contract.AuthSession})

	handler := weberr.RequestID(router)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, testReqWithSession(
		http.MethodPost, "/test/validation-err",
		`{"name":"a"}`, "application/json",
	))

	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422\nbody: %s", rr.Code, rr.Body.String())
	}
	env := assertRequestIDMatchesHeader(t, rr)
	if env.Code != "validation_failed" {
		t.Errorf("code = %q, want validation_failed", env.Code)
	}
	assertNoRFC9457Fields(t, rr.Body.Bytes())
}

// --- Gap 2b: Malformed JSON -> exactly 400 bad_request ---

func TestHumaMalformedJSON_400_BadRequest(t *testing.T) {
	router, api := buildTestAPI(t)

	type body struct {
		Name string `json:"name"`
	}
	type in struct{ Body body }
	type out struct{ Body struct{ OK bool } }
	registerOp(api, huma.Operation{
		Method: http.MethodPost,
		Path:   "/test/malformed-json",
	}, func(ctx context.Context, input *in) (*out, error) {
		return &out{Body: struct{ OK bool }{OK: true}}, nil
	}, contract.AuthRequirement{Kind: contract.AuthSession})

	handler := weberr.RequestID(router)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, testReqWithSession(
		http.MethodPost, "/test/malformed-json",
		`{not json`, "application/json",
	))

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want exactly 400\nbody: %s", rr.Code, rr.Body.String())
	}
	env := assertRequestIDMatchesHeader(t, rr)
	if env.Code != "bad_request" {
		t.Errorf("code = %q, want bad_request", env.Code)
	}
	assertNoRFC9457Fields(t, rr.Body.Bytes())
}

// --- Gap 2c: Unsupported media type -> exactly 415 unsupported_media_type ---

func TestHumaUnsupportedMediaType_415_UnsupportedMediaType(t *testing.T) {
	router, api := buildTestAPI(t)

	type body struct {
		Name string `json:"name"`
	}
	type in struct{ Body body }
	type out struct{ Body struct{ OK bool } }
	registerOp(api, huma.Operation{
		Method: http.MethodPost,
		Path:   "/test/unsupported-media",
	}, func(ctx context.Context, input *in) (*out, error) {
		return &out{Body: struct{ OK bool }{OK: true}}, nil
	}, contract.AuthRequirement{Kind: contract.AuthSession})

	handler := weberr.RequestID(router)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, testReqWithSession(
		http.MethodPost, "/test/unsupported-media",
		`{"name":"ok"}`, "text/plain",
	))

	if rr.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want exactly 415\nbody: %s", rr.Code, rr.Body.String())
	}
	env := assertRequestIDMatchesHeader(t, rr)
	if env.Code != "unsupported_media_type" {
		t.Errorf("code = %q, want unsupported_media_type", env.Code)
	}
	assertNoRFC9457Fields(t, rr.Body.Bytes())
}

// --- Gap 1+2d: Typed handler raw fmt.Errorf -> 500 server_error, no details, no raw text ---

func TestTypedHumaRawError_500_ServerErrorNoDetails(t *testing.T) {
	router, api := buildTestAPI(t)

	type in struct {
		ID string `path:"id"`
	}
	type out struct{ Body struct{ OK bool } }
	secret := "postgres://user:super-secret-password@db.internal:5432/prod"
	registerOp(api, huma.Operation{
		Method: http.MethodGet,
		Path:   "/test/raw-err/{id}",
	}, func(ctx context.Context, input *in) (*out, error) {
		return nil, fmt.Errorf("load account: %w", errors.New(secret))
	}, contract.AuthRequirement{Kind: contract.AuthSession})

	handler := weberr.RequestID(router)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, testReqWithSession(http.MethodGet, "/test/raw-err/1", "", ""))

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500\nbody: %s", rr.Code, rr.Body.String())
	}
	env := assertRequestIDMatchesHeader(t, rr)
	if env.Code != "server_error" {
		t.Errorf("code = %q, want server_error", env.Code)
	}
	if env.Details != nil {
		t.Errorf("details = %v, want nil for server_error", env.Details)
	}
	bodyStr := rr.Body.String()
	if strings.Contains(bodyStr, secret) {
		t.Errorf("body leaked raw error text: %s", bodyStr)
	}
	if strings.Contains(bodyStr, "load account") {
		t.Errorf("body leaked handler prose: %s", bodyStr)
	}
	assertNoRFC9457Fields(t, rr.Body.Bytes())
}

// --- Gap 3: writeHumaPublicErr passes ae.Details through ---

func TestWriteHumaPublicErr_PassesDetails(t *testing.T) {
	router, api := buildTestAPI(t)

	type in struct {
		ID string `path:"id"`
	}
	type out struct{ Body struct{ OK bool } }
	registerOp(api, huma.Operation{
		Method: http.MethodGet,
		Path:   "/test/invalid-role/{id}",
	}, func(ctx context.Context, input *in) (*out, error) {
		return nil, authErrToHuma(authn.ErrInvalidRole())
	}, contract.AuthRequirement{Kind: contract.AuthSession})

	handler := weberr.RequestID(router)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, testReqWithSession(http.MethodGet, "/test/invalid-role/1", "", ""))

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
	env := assertRequestIDMatchesHeader(t, rr)
	if env.Code != "invalid_role" {
		t.Errorf("code = %q, want invalid_role", env.Code)
	}
	if env.Details == nil {
		t.Fatal("details is nil — expected allowed roles")
	}
	allowed, ok := env.Details["allowed"]
	if !ok {
		t.Fatal("details missing 'allowed' key")
	}
	// JSON round-trip converts []string to []interface{}.
	arr, ok := allowed.([]any)
	if !ok {
		t.Fatalf("allowed is %T, want []any (from JSON round-trip)", allowed)
	}
	if len(arr) == 0 {
		t.Fatal("allowed is empty")
	}
}

func TestDiagnosticCaptureRecordsTypedHumaError(t *testing.T) {
	store := &recordingDiagnosticWriter{}
	router, api := buildTestAPIWithDiagnostic(t, store)

	type in struct {
		ID string `path:"id"`
	}
	type out struct{ Body struct{ OK bool } }
	registerOp(api, huma.Operation{
		Method: http.MethodGet,
		Path:   "/test/diagnostic/{id}",
	}, func(ctx context.Context, input *in) (*out, error) {
		return nil, authErrToHuma(authn.ErrInvalidRole())
	}, contract.AuthRequirement{Kind: contract.AuthSession})

	handler := weberr.RequestID(router)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, testReqWithSession(http.MethodGet, "/test/diagnostic/1", "", ""))

	if len(store.records) != 1 {
		t.Fatalf("records = %d, want 1", len(store.records))
	}
	got := store.records[0]
	if got.Code != "invalid_role" || got.Route != "/test/diagnostic/{id}" || got.Method != http.MethodGet {
		t.Fatalf("record = %#v", got)
	}
	if got.AccountID == nil || *got.AccountID != 1 {
		t.Fatalf("account ID = %v, want 1", got.AccountID)
	}
	if len(got.Fields) != 1 || got.Fields["allowed"] == nil {
		t.Fatalf("fields = %#v, want only allowed", got.Fields)
	}
}

func TestDiagnosticCaptureRecordsDirectHumaAuthRejection(t *testing.T) {
	store := &recordingDiagnosticWriter{}
	router, api := buildTestAPIWithDiagnostic(t, store)

	type out struct{ Body struct{ OK bool } }
	registerOp(api, huma.Operation{
		Method: http.MethodGet,
		Path:   "/test/diagnostic-auth/{id}",
	}, func(ctx context.Context, input *struct {
		ID string `path:"id"`
	}) (*out, error) {
		return &out{}, nil
	}, contract.AuthRequirement{Kind: contract.AuthSession})

	handler := weberr.RequestID(router)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/test/diagnostic-auth/1", nil))

	if len(store.records) != 1 {
		t.Fatalf("records = %d, want 1", len(store.records))
	}
	got := store.records[0]
	if got.Code != "no_session" || got.Route != "/test/diagnostic-auth/{id}" || got.Method != http.MethodGet {
		t.Fatalf("record = %#v", got)
	}
	if got.AccountID != nil || len(got.Fields) != 0 {
		t.Fatalf("account/fields = %v/%#v, want unauthenticated safe record", got.AccountID, got.Fields)
	}
}
