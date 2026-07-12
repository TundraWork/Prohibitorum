// Package weberr defines the typed public-error registry and HTTP request
// correlation for Prohibitorum. Every application JSON error is constructed
// through this package so the wire contract is {code, details?, requestId}
// with no localized message — the frontend selects display copy from the
// stable code, and the server-generated request ID lets operators correlate
// the public error to structured logs and diagnostic records.
//
// The registry is initialized at package load time (init) with all known
// AuthError codes plus the canonical fallback codes (server_error,
// request_too_large). The authn package registers its own codes in its init.
package weberr

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
)

// Definition describes a single public-error case. Every known user-actionable
// failure has a dedicated Definition with a globally unique stable Code.
type Definition struct {
	// Code is the stable, machine-readable identifier exposed to clients and
	// used by the frontend to select localized copy. Never reused, never
	// changed once shipped.
	Code string

	// Status is the HTTP status code for this error case.
	Status int

	// LocaleKey is the frontend i18n key for this code (e.g.
	// "errors.account_disabled"). Empty means the frontend maps from Code
	// directly; Task 4 will populate this.
	LocaleKey string

	// DetailKeys is the set of public detail field names this code is allowed
	// to carry (e.g. "field", "reason", "retryAfterSeconds"). Keys not in this
	// set are rejected by New to prevent leaking raw causes or secrets. A nil
	// or empty set means no details are permitted.
	DetailKeys map[string]struct{}

	// Retryable indicates whether the client can safely retry the request
	// (e.g. rate_limited → true; account_disabled → false).
	Retryable bool

	// Recovery is a short stable hint the frontend can use to pick a recovery
	// action (e.g. "retry", "reauth", "contact_admin"). Empty means the
	// frontend uses its default.
	Recovery string

	// DiagnosticKind categorizes the failure for safe internal diagnostic
	// records (e.g. "auth", "validation", "protocol", "internal"). This is
	// the only internal classification that may appear in stored diagnostic
	// records alongside the public code and request ID.
	DiagnosticKind string
}

// PublicError is the wire envelope for every application JSON error:
//
//	{"code":"account_disabled","details":{"field":"status"},"requestId":"..."}
//
// There is intentionally no "message" field — the server does not select a
// display language. The frontend maps from the stable Code to localized copy.
//
// PublicError implements huma.StatusError (via GetStatus) so it can be
// returned directly from typed Huma handlers — huma serializes it with
// the same {code, details?, requestId} envelope and the HTTP status from
// the registry definition.
type PublicError struct {
	Code      string         `json:"code"`
	Details   map[string]any `json:"details,omitempty"`
	RequestID string         `json:"requestId"`
}

// GetStatus returns the HTTP status for this error's registered code. This
// makes *PublicError implement huma.StatusError, so typed Huma handlers can
// return a *PublicError directly and huma will serialize it with the correct
// status. If the code is unknown (should never happen — New validates),
// falls back to 500.
func (e *PublicError) GetStatus() int {
	def, ok := DefinitionFor(e.Code)
	if !ok {
		return http.StatusInternalServerError
	}
	return def.Status
}

// Error returns a compact string for logging/debugging. It never includes
// wrapped raw cause text — only the registered code and request ID.
func (e *PublicError) Error() string {
	if e.RequestID != "" {
		return fmt.Sprintf("%s (requestId=%s)", e.Code, e.RequestID)
	}
	return e.Code
}

// MarshalJSON ensures the envelope is always {code, details?, requestId} even
// when Details is nil (omitempty drops it) — and never includes a message.
func (e *PublicError) MarshalJSON() ([]byte, error) {
	type alias PublicError
	return json.Marshal((*alias)(e))
}

// PublicErrorProvider is implemented by error types that carry a registered
// code and optional details. The authn.AuthError type implements this so
// AsPublic can extract a PublicError from it without a direct dependency
// (authn imports weberr, not vice versa).
type PublicErrorProvider interface {
	PublicError() *PublicError
}

// --- registry ---

var (
	regMu sync.RWMutex
	reg   = map[string]Definition{}
)

// init seeds the registry with canonical fallback codes that are not tied to
// a specific AuthError constructor. The authn package's init registers all of
// its AuthError codes.
func init() {
	must := func(defs ...Definition) {
		if err := Register(defs); err != nil {
			panic(fmt.Sprintf("weberr: failed to register built-in definitions: %v", err))
		}
	}

	must(
		Definition{
			Code:           "server_error",
			Status:         http.StatusInternalServerError,
			LocaleKey:      "errors.server_error",
			Retryable:      true,
			Recovery:       "retry",
			DiagnosticKind: "internal",
		},
		Definition{
			Code:           "request_too_large",
			Status:         http.StatusRequestEntityTooLarge,
			LocaleKey:      "errors.request_too_large",
			Retryable:      false,
			Recovery:       "reduce_payload",
			DiagnosticKind: "validation",
		},
		// Operation-specific internal codes for raw-boundary failures.
		// These let writeAuthErrForCode emit a more specific code than the
		// generic server_error at raw HTTP boundaries (DB/KV/crypto), without
		// migrating typed Huma handler returns (Task 3).
		Definition{
			Code:           "database_unavailable",
			Status:         http.StatusServiceUnavailable,
			LocaleKey:      "errors.database_unavailable",
			Retryable:      true,
			Recovery:       "retry",
			DiagnosticKind: "internal",
		},
		Definition{
			Code:           "kv_unavailable",
			Status:         http.StatusServiceUnavailable,
			LocaleKey:      "errors.kv_unavailable",
			Retryable:      true,
			Recovery:       "retry",
			DiagnosticKind: "internal",
		},
		Definition{
			Code:           "ceremony_internal_error",
			Status:         http.StatusInternalServerError,
			LocaleKey:      "errors.ceremony_internal_error",
			Retryable:      true,
			Recovery:       "retry",
			DiagnosticKind: "internal",
		},
		// validation_failed is the code for huma request validation errors
		// (malformed JSON, schema violations). Details carry safe location +
		// reason, never raw input values.
		Definition{
			Code:           "validation_failed",
			Status:         http.StatusUnprocessableEntity,
			LocaleKey:      "errors.validation_failed",
			Retryable:      false,
			Recovery:       "fix_input",
			DiagnosticKind: "validation",
			DetailKeys:     map[string]struct{}{"location": {}, "reason": {}},
		},
	)
}

// ValidateDefinitions checks that every definition has a non-empty Code and a
// valid HTTP status (1xx–5xx), and that no two definitions share a Code.
// It does NOT check against the existing registry — use Register for that.
func ValidateDefinitions(defs []Definition) error {
	seen := make(map[string]struct{}, len(defs))
	for _, d := range defs {
		if d.Code == "" {
			return fmt.Errorf("weberr: definition with empty code")
		}
		if d.Status < 100 || d.Status > 599 {
			return fmt.Errorf("weberr: definition %q has invalid status %d", d.Code, d.Status)
		}
		if _, ok := seen[d.Code]; ok {
			return fmt.Errorf("weberr: duplicate code %q in definitions", d.Code)
		}
		seen[d.Code] = struct{}{}
	}
	return nil
}

// Register validates the given definitions and adds them to the registry. It
// returns an error if any definition has an empty Code, invalid Status, or a
// Code that already exists in the registry (from a prior Register call or the
// built-in init). Safe for concurrent use; typical callers pass a fixed slice
// at package init time.
//
// Atomicity: all collision checks run under the write lock BEFORE any
// insertion. If any code in the batch is already registered (or duplicated
// within the batch), the entire batch is rejected and the registry is left
// untouched — a failed Register never leaves a partial batch behind.
func Register(defs []Definition) error {
	if err := ValidateDefinitions(defs); err != nil {
		return err
	}
	regMu.Lock()
	defer regMu.Unlock()
	for _, d := range defs {
		if _, exists := reg[d.Code]; exists {
			return fmt.Errorf("weberr: code %q already registered", d.Code)
		}
	}
	for _, d := range defs {
		reg[d.Code] = d
	}
	return nil
}

// DefinitionFor looks up a registered definition by code. Returns ok=false
// for an unknown code.
func DefinitionFor(code string) (Definition, bool) {
	regMu.RLock()
	defer regMu.RUnlock()
	d, ok := reg[code]
	return d, ok
}

// New creates a PublicError for the given registered code, validating that
// every detail key is declared in the definition's DetailKeys. If the code is
// unknown or a detail key is undeclared, it returns a non-nil error (which
// is NOT a PublicError) so the caller can distinguish validation failure from
// a valid public error.
//
// A nil or empty details map is always valid (the definition may still restrict
// which keys are allowed when details are present).
func New(code string, details map[string]any) error {
	def, ok := DefinitionFor(code)
	if !ok {
		return fmt.Errorf("weberr: unknown code %q", code)
	}
	for k := range details {
		if _, allowed := def.DetailKeys[k]; !allowed {
			return fmt.Errorf("weberr: code %q does not declare detail key %q", code, k)
		}
	}
	return &PublicError{
		Code:    code,
		Details: details,
	}
}

// AsPublic extracts a PublicError from an error value. It handles:
//   - *PublicError directly (or wrapped via errors.As).
//   - Any error implementing PublicErrorProvider (e.g. *authn.AuthError),
//     which carries a registered code and optional details.
// Returns nil for errors that don't carry a public code (internal failures).
// The request ID is NOT set here — it is stamped at the HTTP boundary where
// the context is available.
func AsPublic(err error) *PublicError {
	if err == nil {
		return nil
	}
	var pe *PublicError
	if errors.As(err, &pe) {
		return pe
	}
	if provider, ok := err.(PublicErrorProvider); ok {
		return provider.PublicError()
	}
	return nil
}
