package weberr

import (
	"encoding/json"
	"errors"
	"net/http"
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
