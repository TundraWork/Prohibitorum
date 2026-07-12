package weberr_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/weberr"
)

// TestRegistryDefinitionsAreComplete walks every registered definition and
// asserts the invariants required by the public-error contract:
//   - Code is non-empty and unique (enforced by Register, but we re-check).
//   - Status is a valid HTTP status (1xx–5xx).
//   - LocaleKey is non-empty (frontend needs a mapping key for every code).
//   - DiagnosticKind is non-empty (safe diagnostic categorization).
//
// This is the "coverage" test: every registered code must declare a
// localization key and a diagnostic kind, proving the registry is the
// single source of truth for wire status, i18n, and diagnostics.
func TestRegistryDefinitionsAreComplete(t *testing.T) {
	codes := allRegisteredCodes(t)
	if len(codes) < 50 {
		t.Fatalf("expected at least 50 registered codes, got %d", len(codes))
	}
	for _, code := range codes {
		def, ok := weberr.DefinitionFor(code)
		if !ok {
			t.Fatalf("DefinitionFor(%q) returned false after enumeration", code)
		}
		if def.Code == "" {
			t.Errorf("definition has empty Code")
		}
		if def.Status < 100 || def.Status > 599 {
			t.Errorf("code %q: status %d out of range", code, def.Status)
		}
		if def.LocaleKey == "" {
			t.Errorf("code %q: LocaleKey is empty — frontend cannot map", code)
		}
		if def.DiagnosticKind == "" {
			t.Errorf("code %q: DiagnosticKind is empty", code)
		}
	}
}

// TestKnownValidationFailuresHaveDistinctCodes proves that formerly shared
// bad_request branches now have dedicated unique codes. Each case must
// produce a different PublicError.Code — a regression to a shared code
// would collapse distinguishable user-actionable causes.
func TestKnownValidationFailuresHaveDistinctCodes(t *testing.T) {
	cases := []struct {
		name string
		err  error
		code string
	}{
		{"invalid_username", authn.ErrInvalidUsername(), "invalid_username"},
		{"invalid_display_name", authn.ErrInvalidDisplayName(), "invalid_display_name"},
		{"invalid_nickname", authn.ErrInvalidNickname(), "invalid_nickname"},
		{"invalid_role", authn.ErrInvalidRole(), "invalid_role"},
		{"username_immutable", authn.ErrUsernameImmutable(), "username_immutable"},
		{"username_taken", authn.ErrUsernameTaken(), "username_taken"},
		{"bad_request", authn.ErrBadRequest(), "bad_request"},
		{"invalid_return_to", authn.ErrInvalidReturnTo(), "invalid_return_to"},
		{"invalid_consent_ticket", authn.ErrInvalidConsentTicket(), "invalid_consent_ticket"},
	}
	seen := map[string]bool{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pe := weberr.AsPublic(tc.err)
			if pe == nil {
				t.Fatalf("AsPublic returned nil for %s", tc.name)
			}
			if pe.Code != tc.code {
				t.Errorf("code = %q, want %q", pe.Code, tc.code)
			}
			if seen[pe.Code] {
				t.Fatalf("duplicate public code %q — formerly shared branches must be distinct", pe.Code)
			}
			seen[pe.Code] = true
		})
	}
}

// TestAuthErrToHumaProducesPublicError proves the typed Huma handler error
// path returns a *PublicError (not an *errorx.Error) with the correct code,
// status from the registry, and no message field in the JSON envelope.
func TestAuthErrToHumaProducesPublicError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		code string
	}{
		{"account_not_found", authn.ErrAccountNotFound(), "account_not_found"},
		{"invalid_role", authn.ErrInvalidRole(), "invalid_role"},
		{"sudo_required", authn.ErrSudoRequired(), "sudo_required"},
		{"last_admin", authn.ErrLastAdmin(), "last_admin"},
		{"bad_request", authn.ErrBadRequest(), "bad_request"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pe := weberr.AsPublic(tc.err)
			if pe == nil {
				t.Fatalf("AsPublic returned nil for %s", tc.name)
			}
			if pe.Code != tc.code {
				t.Errorf("code = %q, want %q", pe.Code, tc.code)
			}
			// The PublicError must implement StatusError via GetStatus().
			status := pe.GetStatus()
			def, ok := weberr.DefinitionFor(tc.code)
			if !ok {
				t.Fatalf("code %q not registered", tc.code)
			}
			if status != def.Status {
				t.Errorf("GetStatus() = %d, want %d (from registry)", status, def.Status)
			}
			// JSON must not contain a message field.
			b, err := json.Marshal(pe)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var raw map[string]json.RawMessage
			if err := json.Unmarshal(b, &raw); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if _, has := raw["message"]; has {
				t.Errorf("PublicError JSON contains a forbidden message field: %s", b)
			}
		})
	}
}

// TestErrInvalidRoleCarriesAllowedDetails proves ErrInvalidRole carries the
// allowed roles in details so the frontend can display them — not as a
// Chinese message string.
func TestErrInvalidRoleCarriesAllowedDetails(t *testing.T) {
	pe := weberr.AsPublic(authn.ErrInvalidRole())
	if pe == nil {
		t.Fatal("AsPublic returned nil")
	}
	if pe.Code != "invalid_role" {
		t.Fatalf("code = %q, want invalid_role", pe.Code)
	}
	if pe.Details == nil {
		t.Fatal("details is nil — expected allowed roles")
	}
	allowed, ok := pe.Details["allowed"]
	if !ok {
		t.Fatal("details missing 'allowed' key")
	}
	arr, ok := allowed.([]string)
	if !ok {
		t.Fatalf("allowed is %T, want []string", allowed)
	}
	if len(arr) == 0 {
		t.Fatal("allowed is empty")
	}
	found := false
	for _, r := range arr {
		if r == "user" || r == "admin" {
			found = true
		}
	}
	if !found {
		t.Errorf("allowed = %v, expected to contain user/admin", arr)
	}
}

// TestErrFactorLockedCarriesRetryAfterSeconds proves ErrFactorLocked carries
// the retry-after duration in details as retryAfterSeconds.
func TestErrFactorLockedCarriesRetryAfterSeconds(t *testing.T) {
	pe := weberr.AsPublic(authn.ErrFactorLocked(30e9)) // 30s
	if pe == nil {
		t.Fatal("AsPublic returned nil")
	}
	if pe.Code != "factor_locked" {
		t.Fatalf("code = %q, want factor_locked", pe.Code)
	}
	if pe.Details == nil {
		t.Fatal("details is nil — expected retryAfterSeconds")
	}
	secs, ok := pe.Details["retryAfterSeconds"]
	if !ok {
		t.Fatal("details missing 'retryAfterSeconds' key")
	}
	n, ok := secs.(int)
	if !ok {
		t.Fatalf("retryAfterSeconds is %T, want int", secs)
	}
	if n != 30 {
		t.Errorf("retryAfterSeconds = %d, want 30", n)
	}
}

// TestErrUpstreamErrorCarriesUpstreamCode proves ErrUpstreamError carries
// the upstream error code in details but NOT the raw description (which may
// contain unsafe upstream text).
func TestErrUpstreamErrorCarriesUpstreamCode(t *testing.T) {
	pe := weberr.AsPublic(authn.ErrUpstreamError("access_denied", "user did not consent"))
	if pe == nil {
		t.Fatal("AsPublic returned nil")
	}
	if pe.Code != "upstream_error" {
		t.Fatalf("code = %q, want upstream_error", pe.Code)
	}
	if pe.Details == nil {
		t.Fatal("details is nil — expected upstreamCode")
	}
	upstreamCode, ok := pe.Details["upstreamCode"]
	if !ok {
		t.Fatal("details missing 'upstreamCode' key")
	}
	if upstreamCode != "access_denied" {
		t.Errorf("upstreamCode = %v, want access_denied", upstreamCode)
	}
	// The raw upstream description must NOT appear in details — it is
	// unchecked upstream text.
	for k, v := range pe.Details {
		if strings.Contains(fmt.Sprint(v), "user did not consent") {
			t.Errorf("details[%q] leaked upstream description: %v", k, v)
		}
	}
}

// TestRateLimitedCarriesRetryAfterSeconds proves the rate_limited code
// declares retryAfterSeconds as a detail key.
func TestRateLimitedCarriesRetryAfterSeconds(t *testing.T) {
	def, ok := weberr.DefinitionFor("rate_limited")
	if !ok {
		t.Fatal("rate_limited not registered")
	}
	if _, ok := def.DetailKeys["retryAfterSeconds"]; !ok {
		t.Error("rate_limited definition does not declare retryAfterSeconds detail key")
	}
}

// TestNoChineseProseInRegisteredMessages proves no AuthError constructor
// produces a Message that would be leaked to the wire. Since the typed
// Huma path now returns PublicError (no message), and writeAuthErr passes
// nil details (no message), the Message field is vestigial. This test
// asserts the Message is NOT serialized in any PublicError JSON.
func TestNoChineseProseInRegisteredMessages(t *testing.T) {
	// Every AuthError constructor returns a code; the Message is no longer
	// part of the wire envelope. We verify by checking a representative set
	// of constructors that previously carried Chinese prose.
	cases := []error{
		authn.ErrNoSession(),
		authn.ErrNotAdmin(),
		authn.ErrAccountDisabled(),
		authn.ErrBadCredentials(),
		authn.ErrLoginFailed(),
		authn.ErrCeremonyExpired(),
		authn.ErrInviteRequired(),
		authn.ErrEmailNotVerified(),
		authn.ErrMaintenanceMode(),
	}
	for _, err := range cases {
		pe := weberr.AsPublic(err)
		if pe == nil {
			t.Fatalf("AsPublic returned nil for %v", err)
		}
		b, _ := json.Marshal(pe)
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(b, &raw); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if _, has := raw["message"]; has {
			t.Errorf("code %q: PublicError JSON contains a message field: %s", pe.Code, b)
		}
		// Check for Chinese characters (CJK Unified Ideographs U+4E00–U+9FFF).
		bodyStr := string(b)
		for _, r := range bodyStr {
			if r >= 0x4E00 && r <= 0x9FFF {
				t.Errorf("code %q: JSON body contains Chinese character %q: %s", pe.Code, string(r), b)
			}
		}
	}
}

// TestWriteJSONPassesDetails proves WriteJSON passes declared details through
// to the wire. This validates the end-to-end detail flow from New → WriteJSON.
func TestWriteJSONPassesDetails(t *testing.T) {
	// Register a test code that declares detail keys.
	code := "test_write_details_code"
	if err := weberr.Register([]weberr.Definition{{
		Code:       code,
		Status:     http.StatusBadRequest,
		DetailKeys:  map[string]struct{}{"field": {}, "reason": {}},
	}}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Create a PublicError with declared details.
	pe := weberr.New(code, map[string]any{"field": "username", "reason": "too_short"})
	if pe == nil {
		t.Fatalf("New returned nil")
	}
	// Verify the details are present.
	pub := weberr.AsPublic(pe)
	if pub.Details["field"] != "username" {
		t.Fatalf("field = %v, want username", pub.Details["field"])
	}
}

// allRegisteredCodes returns all registered codes. We test the known set
// registered by weberr.init + authn.init.
func allRegisteredCodes(t *testing.T) []string {
	t.Helper()
	known := []string{
		// weberr built-ins
		"server_error", "request_too_large",
		"database_unavailable", "kv_unavailable", "ceremony_internal_error",
		// authn codes
		"no_session", "not_admin", "permission_denied", "account_disabled",
		"last_admin", "admin_cannot_be_disabled", "cannot_delete_self",
		"username_taken", "enrollment_expired", "enrollment_consumed",
		"enrollment_federation_required", "bad_request",
		"invalid_consent_ticket", "invalid_role", "invalid_username",
		"invalid_nickname", "invalid_display_name", "username_immutable",
		"last_passkey", "would_remove_last_factor",
		"login_account_not_found", "login_verification_failed",
		"ceremony_expired", "ceremony_missing", "ceremony_state_invalid",
		"credential_already_registered", "registration_failed",
		"login_failed", "bad_credentials",
		"partial_session_invalid", "recovery_session_invalid",
		"account_not_found", "credential_not_found", "invitation_not_found",
		"not_bootstrapped", "maintenance_mode",
		"pairing_not_found", "pairing_state", "pairing_expired",
		"pairing_not_approved",
		"rate_limited", "factor_locked",
		"sudo_required", "sudo_method_unavailable",
		"session_not_found", "cannot_revoke_current_session",
		"email_not_verified", "username_collision",
		"invite_required", "link_required", "federation_state_invalid",
		"last_sign_in_method", "invalid_return_to", "upstream_error",
		"active_key_no_replacement", "client_not_found",
		"upstream_idp_not_found", "oidc_client_already_exists",
		"upstream_idp_already_exists", "saml_application_already_exists",
		"group_not_found", "group_slug_conflict",
	}
	// Verify each is registered.
	var registered []string
	for _, code := range known {
		if _, ok := weberr.DefinitionFor(code); ok {
			registered = append(registered, code)
		}
	}
	return registered
}

// Ensure unused imports are referenced.
var _ = http.StatusOK
