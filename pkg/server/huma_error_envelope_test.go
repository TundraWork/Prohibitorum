package server

import (
	"context"
	"encoding/json"
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
	"prohibitorum/pkg/weberr"
)

// buildTestAPI creates a huma.API with the same config as production (including
// the response transformer and NewError override) so end-to-end tests exercise
// the real humachi serialization path.
func buildTestAPI(t *testing.T) (*chi.Mux, huma.API) {
	t.Helper()
	router := chi.NewMux()
	api := humachi.New(router, humaConfig())
	return router, api
}

// TestTypedHumaError_RequestIDInBody proves that when a typed Huma handler
// returns a *weberr.PublicError (via authErrToHuma), the response body's
// requestId matches the X-Request-ID header set by the RequestID middleware.
// This is the end-to-end test through the real humachi route path.
func TestTypedHumaError_RequestIDInBody(t *testing.T) {
	router, api := buildTestAPI(t)

	type in struct{ ID string `path:"id"` }
	type out struct{ Body struct{ OK bool } }
	registerOp(api, huma.Operation{
		Method: http.MethodGet,
		Path:   "/test/typed-err/{id}",
	}, func(ctx context.Context, input *in) (*out, error) {
		return nil, authErrToHuma(authn.ErrAccountNotFound())
	}, contract.AuthRequirement{Kind: contract.AuthSession})

	// Wrap with RequestID middleware.
	handler := weberr.RequestID(router)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test/typed-err/42", nil)
	sess := &authn.Session{Account: &db.Account{ID: 1, Role: "admin", Username: "admin"}}
	req = req.WithContext(authn.WithSession(req.Context(), sess))
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rr.Code)
	}
	headerID := rr.Header().Get(weberr.HeaderRequestID)
	if headerID == "" {
		t.Fatal("X-Request-ID header is empty")
	}
	var env weberr.PublicError
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal body: %v\nbody: %s", err, rr.Body.String())
	}
	if env.Code != "account_not_found" {
		t.Errorf("code = %q, want account_not_found", env.Code)
	}
	if env.RequestID != headerID {
		t.Errorf("body requestId = %q, want header id %q", env.RequestID, headerID)
	}
	// No message field.
	var raw map[string]json.RawMessage
	_ = json.Unmarshal(rr.Body.Bytes(), &raw)
	if _, has := raw["message"]; has {
		t.Errorf("body contains message field: %s", rr.Body.String())
	}
}

// TestHumaValidationError_EnvelopeShape proves that huma validation errors
// (too-short fields) emit the {code, details?, requestId} envelope with no
// RFC 9457 fields (message/detail/title/errors).
func TestHumaValidationError_EnvelopeShape(t *testing.T) {
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

	// Send a too-short name — huma should validate minLength and return 422.
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/test/validation-err", strings.NewReader(`{"name":"a"}`))
	req.Header.Set("Content-Type", "application/json")
	sess := &authn.Session{Account: &db.Account{ID: 1, Role: "admin", Username: "admin"}}
	req = req.WithContext(authn.WithSession(req.Context(), sess))
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422\nbody: %s", rr.Code, rr.Body.String())
	}
	headerID := rr.Header().Get(weberr.HeaderRequestID)
	if headerID == "" {
		t.Fatal("X-Request-ID header is empty")
	}
	var env weberr.PublicError
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal body: %v\nbody: %s", err, rr.Body.String())
	}
	if env.Code != "validation_failed" {
		t.Errorf("code = %q, want validation_failed", env.Code)
	}
	// Must NOT contain RFC 9457 fields.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rr.Body.Bytes(), &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	for _, forbidden := range []string{"message", "detail", "title", "errors", "type", "instance"} {
		if _, has := raw[forbidden]; has {
			t.Errorf("body contains forbidden RFC 9457 field %q: %s", forbidden, rr.Body.String())
		}
	}
	// requestId must match the header.
	if env.RequestID != headerID {
		t.Errorf("body requestId = %q, want header id %q", env.RequestID, headerID)
	}
}

// TestHumaMalformedJSON_EnvelopeShape proves that a completely malformed JSON
// body (not just a validation failure) emits the same safe envelope.
func TestHumaMalformedJSON_EnvelopeShape(t *testing.T) {
	router, api := buildTestAPI(t)

	type body struct{ Name string `json:"name"` }
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
	req := httptest.NewRequest(http.MethodPost, "/test/malformed-json", strings.NewReader(`{not json`))
	req.Header.Set("Content-Type", "application/json")
	sess := &authn.Session{Account: &db.Account{ID: 1, Role: "admin", Username: "admin"}}
	req = req.WithContext(authn.WithSession(req.Context(), sess))
	handler.ServeHTTP(rr, req)

	if rr.Code < 400 {
		t.Fatalf("status = %d, want 4xx for malformed JSON\nbody: %s", rr.Code, rr.Body.String())
	}
	var env weberr.PublicError
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal body: %v\nbody: %s", err, rr.Body.String())
	}
	if env.Code != "validation_failed" {
		t.Errorf("code = %q, want validation_failed", env.Code)
	}
	// No RFC 9457 fields.
	var raw map[string]json.RawMessage
	_ = json.Unmarshal(rr.Body.Bytes(), &raw)
	for _, forbidden := range []string{"message", "detail", "title", "errors", "type", "instance"} {
		if _, has := raw[forbidden]; has {
			t.Errorf("body contains forbidden RFC 9457 field %q: %s", forbidden, rr.Body.String())
		}
	}
}
