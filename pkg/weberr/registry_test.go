package weberr

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestRegistryRejectsDuplicateCode proves ValidateDefinitions rejects two
// definitions with the same Code.
func TestRegistryRejectsDuplicateCode(t *testing.T) {
	err := ValidateDefinitions([]Definition{
		{Code: "account_disabled", Status: 403},
		{Code: "account_disabled", Status: 403},
	})
	if err == nil {
		t.Fatal("ValidateDefinitions accepted a duplicate code")
	}
}

// TestRegistryRejectsEmptyCode proves a definition with an empty Code is
// rejected.
func TestRegistryRejectsEmptyCode(t *testing.T) {
	err := ValidateDefinitions([]Definition{{Code: "", Status: 400}})
	if err == nil {
		t.Fatal("ValidateDefinitions accepted an empty code")
	}
}

// TestRegistryRejectsInvalidStatus proves a definition with a non-HTTP status
// (e.g. 0 or 99) is rejected.
func TestRegistryRejectsInvalidStatus(t *testing.T) {
	err := ValidateDefinitions([]Definition{{Code: "x", Status: 0}})
	if err == nil {
		t.Fatal("ValidateDefinitions accepted status 0")
	}
}

// TestDefinitionFor proves lookup returns the registered definition and ok=true,
// and returns ok=false for an unknown code.
func TestDefinitionFor(t *testing.T) {
	// "server_error" is always registered as part of the built-in catalogue.
	def, ok := DefinitionFor("server_error")
	if !ok {
		t.Fatal("DefinitionFor(server_error) returned ok=false")
	}
	if def.Code != "server_error" {
		t.Fatalf("DefinitionFor returned code %q, want server_error", def.Code)
	}
	if def.Status != http.StatusInternalServerError {
		t.Fatalf("DefinitionFor returned status %d, want 500", def.Status)
	}

	if _, ok := DefinitionFor("nonexistent_code_xyz"); ok {
		t.Fatal("DefinitionFor returned ok=true for an unknown code")
	}
}

// TestNewValidatesDetailKeys proves that New rejects detail keys not declared
// in the definition's DetailKeys.
func TestNewValidatesDetailKeys(t *testing.T) {
	// Register a definition with a specific allowed detail key.
	err := Register([]Definition{{
		Code:       "test_validated_details",
		Status:     400,
		DetailKeys: map[string]struct{}{"field": {}, "reason": {}},
	}})
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	// Valid details should produce a *PublicError (a valid error to return
	// from a handler, not a validation failure).
	err = New("test_validated_details", map[string]any{"field": "username", "reason": "too_short"})
	pe := AsPublic(err)
	if pe == nil || pe.Code != "test_validated_details" {
		t.Fatalf("New with valid details did not produce a PublicError: %v", err)
	}

	// Undeclared detail key should fail with a non-PublicError validation error.
	err = New("test_validated_details", map[string]any{"rawCause": "postgres://secret@db"})
	if err == nil {
		t.Fatal("New accepted an undeclared detail key")
	}
	if AsPublic(err) != nil {
		t.Fatalf("New returned a PublicError for invalid details: %v", err)
	}
}

// TestNewUnknownCode proves New rejects an unregistered code.
func TestNewUnknownCode(t *testing.T) {
	err := New("totally_unknown_code", nil)
	if err == nil {
		t.Fatal("New accepted an unknown code")
	}
}

// TestNewProducesPublicError proves New returns an error whose JSON envelope
// is {code, details?, requestId} with no message field.
func TestNewProducesPublicError(t *testing.T) {
	err := Register([]Definition{{
		Code:       "test_envelope_code",
		Status:     400,
		DetailKeys: map[string]struct{}{"field": {}},
	}})
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	_ = AsPublic(err)
	err = New("test_envelope_code", map[string]any{"field": "redirectUri"})
	if err == nil {
		t.Fatal("New returned nil")
	}
	pe := AsPublic(err)
	if pe.Code != "test_envelope_code" {
		t.Fatalf("PublicError.Code = %q, want test_envelope_code", pe.Code)
	}
	if pe.Details["field"] != "redirectUri" {
		t.Fatalf("PublicError.Details = %v, want field=redirectUri", pe.Details)
	}
	// No message field in JSON.
	b, _ := json.Marshal(pe)
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("unmarshal PublicError JSON: %v", err)
	}
	if _, hasMsg := raw["message"]; hasMsg {
		t.Fatal("PublicError JSON contains a message field")
	}
	if _, hasReqID := raw["requestId"]; !hasReqID {
		t.Fatal("PublicError JSON missing requestId field")
	}
}

// TestRegistryRejectsDuplicateDetailKey is a structural test proving that two
// definitions with the same Code across separate Register calls are rejected.
func TestRegistryRejectsDuplicateRegistration(t *testing.T) {
	err := Register([]Definition{{Code: "test_dup_reg", Status: 400}})
	if err != nil {
		t.Fatalf("first Register failed: %v", err)
	}
	err = Register([]Definition{{Code: "test_dup_reg", Status: 400}})
	if err == nil {
		t.Fatal("Register accepted a duplicate code on second call")
	}
}

// TestPublicErrorIsError proves PublicError satisfies the error interface.
func TestPublicErrorIsError(t *testing.T) {
	err := Register([]Definition{{Code: "test_is_error", Status: 400}})
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	e := New("test_is_error", nil)
	if e == nil {
		t.Fatal("New returned nil")
	}
	if errors.Is(e, e) {
		// just ensure it doesn't panic
	}
	_ = e.Error()
}

// TestRegisterAtomicOnPartialCollision proves that when a batch contains a
// mix of new and already-registered codes, Register rejects the entire batch
// atomically — the new codes in the batch must NOT be inserted. This guards
// against a partial-register leaving the registry in an inconsistent state.
func TestRegisterAtomicOnPartialCollision(t *testing.T) {
	// Register a unique code we will then attempt to collide against.
	newCode := "test_atomic_new"
	err := Register([]Definition{{Code: newCode, Status: 400}})
	if err != nil {
		t.Fatalf("first Register failed: %v", err)
	}
	// Batch: one new code + one colliding code. The entire batch must fail.
	err = Register([]Definition{
		{Code: "test_atomic_other_new", Status: 400},
		{Code: newCode, Status: 400}, // collides with the registered code
	})
	if err == nil {
		t.Fatal("Register accepted a batch with a colliding code")
	}
	// The non-colliding code in the batch must NOT be present — Register
	// must be atomic.
	if _, ok := DefinitionFor("test_atomic_other_new"); ok {
		t.Fatal("Register was not atomic: a code from a rejected batch was inserted")
	}
}

// TestWriteJSONRoutesThroughRegistry proves that WriteJSON validates the code
// against the registry: an unregistered code falls back to server_error with
// the registered status, and the caller cannot inject an arbitrary status.
func TestWriteJSONRoutesThroughRegistry(t *testing.T) {
	rr := httptest.NewRecorder()
	// Pass an unregistered code — WriteJSON must fall back to server_error.
	WriteJSON(rr, "this_code_is_not_registered", nil, "req-123")
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (server_error)", rr.Code)
	}
	var pe PublicError
	if err := json.Unmarshal(rr.Body.Bytes(), &pe); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, rr.Body.String())
	}
	if pe.Code != "server_error" {
		t.Errorf("code = %q, want server_error", pe.Code)
	}
	if pe.RequestID != "req-123" {
		t.Errorf("requestId = %q, want req-123", pe.RequestID)
	}
}

// TestWriteJSONUsesRegisteredStatus proves WriteJSON takes the HTTP status
// from the registered definition, not from the caller. Registering a code
// with status 429 and calling WriteJSON without a status argument must emit
// 429.
func TestWriteJSONUsesRegisteredStatus(t *testing.T) {
	code := "test_wj_status_429"
	if err := Register([]Definition{{Code: code, Status: 429}}); err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	rr := httptest.NewRecorder()
	WriteJSON(rr, code, nil, "")
	if rr.Code != 429 {
		t.Fatalf("status = %d, want 429 from registered definition", rr.Code)
	}
}

// TestWriteJSONFiltersUndeclaredDetails proves WriteJSON drops detail keys not
// in the definition's DetailKeys whitelist, preventing raw cause leakage.
func TestWriteJSONFiltersUndeclaredDetails(t *testing.T) {
	code := "test_wj_filter_details"
	if err := Register([]Definition{{
		Code:       code,
		Status:     400,
		DetailKeys: map[string]struct{}{"field": {}},
	}}); err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	rr := httptest.NewRecorder()
	// Pass a declared key + an undeclared key carrying a secret.
	WriteJSON(rr, code, map[string]any{
		"field":    "status",
		"rawCause": "postgres://secret@db:5432",
	}, "")
	var pe PublicError
	if err := json.Unmarshal(rr.Body.Bytes(), &pe); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, rr.Body.String())
	}
	if _, has := pe.Details["rawCause"]; has {
		t.Errorf("undeclared detail key 'rawCause' leaked to response: %s", rr.Body.String())
	}
	if pe.Details["field"] != "status" {
		t.Errorf("declared detail key 'field' was dropped: %v", pe.Details)
	}
}
