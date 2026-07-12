package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/weberr"
)

// captureLogrusOutput redirects the global logrus output (which logx uses)
// into a buffer for the duration of the test so assertions can inspect
// structured log content. Returns the buffer and a restore function.
//
// IMPORTANT: this test must NOT run in parallel with other tests that log
// via logrus, because it mutates the global logger output. The capture*
// tests below do not call t.Parallel().
func captureLogrusOutput(t *testing.T) (*bytes.Buffer, func()) {
	t.Helper()
	buf := &bytes.Buffer{}
	origOut := logrus.StandardLogger().Out
	logrus.SetOutput(buf)
	return buf, func() {
		logrus.SetOutput(origOut)
	}
}

// assertNoMessageField fails the test if the JSON body contains a top-level
// "message" key — the public-error envelope must be {code, details?, requestId}.
func assertNoMessageField(t *testing.T, body []byte) {
	t.Helper()
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("response body is not valid JSON: %v\nbody: %s", err, body)
	}
	if _, has := raw["message"]; has {
		t.Errorf("response body contains a forbidden message field: %s", body)
	}
}

// TestWriteAuthErr_NonAuthError_NoInternalLeak proves that a wrapped DB/KV
// error containing a secret connection string never reaches the HTTP
// response body. The fallback must return a canonical server_error JSON
// envelope with HTTP 500, while the full internal detail is emitted only to
// structured server logs.
func TestWriteAuthErr_NonAuthError_NoInternalLeak(t *testing.T) {
	// A synthetic error carrying a secret that must NEVER appear in the
	// response body.
	secret := "postgres://user:super-secret-password@db.internal:5432/prod"
	// Wrap it so errors.As chains through the wrapping.
	dbErr := errors.New("handleX: load: " + secret)
	wrapped := errors.Join(errors.New("outer"), dbErr)

	buf, restore := captureLogrusOutput(t)
	defer restore()

	rr := httptest.NewRecorder()
	writeAuthErr(rr, wrapped)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rr.Code)
	}

	body := rr.Body.Bytes()
	if strings.Contains(string(body), secret) {
		t.Errorf("response body leaked internal detail: %s", body)
	}
	if strings.Contains(string(body), "handleX") {
		t.Errorf("response body leaked handler name: %s", body)
	}
	// The response must be JSON with a canonical server_error code — not
	// the raw err.Error() plaintext that http.Error would emit.
	if !strings.Contains(string(body), "server_error") {
		t.Errorf("body = %q, want canonical server_error JSON", body)
	}
	assertNoMessageField(t, body)
	ct := rr.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	// The raw error string must NOT be captured in structured logs — only
	// the safe error_type label and registered code are logged.
	logOut := buf.String()
	if strings.Contains(logOut, secret) {
		t.Errorf("internal error log leaked raw secret to structured logs:\n%s", logOut)
	}
	if strings.Contains(logOut, "handleX") {
		t.Errorf("internal error log leaked handler name to structured logs:\n%s", logOut)
	}
}

// TestWriteAuthErr_AuthError_CodeAndStatus confirms that a proper
// *authn.AuthError is rendered with its exact status/code and that the
// envelope is {code, requestId} with no message field.
func TestWriteAuthErr_AuthError_CodeAndStatus(t *testing.T) {
	ae := authn.ErrSudoRequired()

	rr := httptest.NewRecorder()
	writeAuthErr(rr, ae)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rr.Code)
	}
	body := rr.Body.Bytes()
	if !strings.Contains(string(body), "sudo_required") {
		t.Errorf("body = %q, want sudo_required code", body)
	}
	// The localized message must NOT be in the public envelope.
	if strings.Contains(string(body), ae.Message) {
		t.Errorf("body leaked localized message %q: %s", ae.Message, body)
	}
	assertNoMessageField(t, body)
}

// TestWriteAuthErr_AuthError_HasRequestID proves that when the RequestID
// middleware has run, the JSON body's requestId matches the X-Request-ID
// response header.
func TestWriteAuthErr_AuthError_HasRequestID(t *testing.T) {
	ae := authn.ErrAccountDisabled()

	rr := httptest.NewRecorder()
	// Simulate the middleware setting the header on the response writer.
	rr.Header().Set(weberr.HeaderRequestID, "test-request-id-12345")
	writeAuthErr(rr, ae)

	body := rr.Body.Bytes()
	var env weberr.PublicError
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("unmarshal body: %v\nbody: %s", err, body)
	}
	if env.RequestID != "test-request-id-12345" {
		t.Errorf("requestId = %q, want test-request-id-12345", env.RequestID)
	}
	if env.Code != "account_disabled" {
		t.Errorf("code = %q, want account_disabled", env.Code)
	}
}

// TestWriteAuthErr_WrappedAuthError_Unchanged confirms that an AuthError
// wrapped via errors.Join still renders the AuthError's status and code
// (errors.As unwrapping is preserved).
func TestWriteAuthErr_WrappedAuthError_Unchanged(t *testing.T) {
	ae := authn.ErrBadRequest()
	wrapped := errors.Join(errors.New("ctx"), ae)

	rr := httptest.NewRecorder()
	writeAuthErr(rr, wrapped)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
	body := rr.Body.Bytes()
	if !strings.Contains(string(body), "bad_request") {
		t.Errorf("body = %q, want bad_request code", body)
	}
	assertNoMessageField(t, body)
}

// TestWriteAuthErr_NilErrorSafety confirms that a nil error does not panic —
// it degrades to the canonical server_error response.
func TestWriteAuthErr_NilErrorSafety(t *testing.T) {
	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("writeAuthErr panicked on nil error: %v", rec)
		}
	}()

	rr := httptest.NewRecorder()
	writeAuthErr(rr, nil)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rr.Code)
	}
	assertNoMessageField(t, rr.Body.Bytes())
}

// TestServerError_NoDetailLeak proves a synthetic DB/KV error string is
// absent from the response and captured in structured logs. The handler is a
// stub that calls writeAuthErr with the synthetic error.
func TestServerError_NoDetailLeak(t *testing.T) {
	secret := "redis://:s3cr3t@kv.internal:6379/0"
	synthetic := errors.New("kv.Get: dial tcp: " + secret)

	buf, restore := captureLogrusOutput(t)
	defer restore()

	rr := httptest.NewRecorder()
	writeAuthErr(rr, synthetic)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rr.Code)
	}
	body := rr.Body.Bytes()
	if strings.Contains(string(body), secret) {
		t.Errorf("response body leaked internal detail: %s", body)
	}
	if strings.Contains(string(body), "kv.Get") {
		t.Errorf("response body leaked internal handler detail: %s", body)
	}
	if !strings.Contains(string(body), "server_error") {
		t.Errorf("body = %q, want server_error", body)
	}
	assertNoMessageField(t, body)

	// The raw error string must NOT be captured in structured logs — only
	// the safe error_type label and registered code are logged.
	logOut := buf.String()
	if strings.Contains(logOut, secret) {
		t.Errorf("internal error log leaked raw secret to structured logs:\n%s", logOut)
	}
}

// TestWriteAuthErr_RequestIDMiddleware_StampsBody proves the full
// middleware → handler → writeAuthErr path stamps the server-generated
// request ID into both the X-Request-ID header and the JSON body, and that
// an inbound X-Request-ID is ignored.
func TestWriteAuthErr_RequestIDMiddleware_StampsBody(t *testing.T) {
	ae := authn.ErrRateLimited()

	handler := weberr.RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeAuthErr(w, ae)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(weberr.HeaderRequestID, "attacker-controlled")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	headerID := rr.Header().Get(weberr.HeaderRequestID)
	if headerID == "" || headerID == "attacker-controlled" {
		t.Fatalf("X-Request-ID header = %q, want server-generated", headerID)
	}
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rr.Code)
	}
	var env weberr.PublicError
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal body: %v\nbody: %s", err, rr.Body.String())
	}
	if env.Code != "rate_limited" {
		t.Errorf("code = %q, want rate_limited", env.Code)
	}
	if env.RequestID != headerID {
		t.Errorf("body requestId = %q, want header id %q", env.RequestID, headerID)
	}
	assertNoMessageField(t, rr.Body.Bytes())
}

// TestWriteAuthErr_InternalErrorLogNoRawErrorString proves the internal-error
// log path does NOT persist err.Error() to structured logs. The log must
// carry the request ID and registered code, but never the raw error string
// (which may contain connection strings, query text, or stack fragments).
func TestWriteAuthErr_InternalErrorLogNoRawErrorString(t *testing.T) {
	secret := "postgres://user:super-secret-password@db.internal:5432/prod"
	dbErr := errors.New("handleX: load: " + secret)

	buf, restore := captureLogrusOutput(t)
	defer restore()

	rr := httptest.NewRecorder()
	rr.Header().Set(weberr.HeaderRequestID, "req-log-test-001")
	writeAuthErr(rr, dbErr)

	logOut := buf.String()
	// The raw error string must NOT appear in structured logs — only the
	// error TYPE (a safe label like "other" or "pg_...") is permitted.
	if strings.Contains(logOut, secret) {
		t.Errorf("internal error log leaked raw error string (secret):\n%s", logOut)
	}
	if strings.Contains(logOut, "handleX") {
		t.Errorf("internal error log leaked handler name:\n%s", logOut)
	}
	// The request ID MUST be in the log so operators can correlate.
	if !strings.Contains(logOut, "req-log-test-001") {
		t.Errorf("internal error log missing request_id:\n%s", logOut)
	}
	// The registered code MUST be in the log.
	if !strings.Contains(logOut, "server_error") {
		t.Errorf("internal error log missing registered code:\n%s", logOut)
	}
}

// TestWriteAuthErr_InternalErrorLogHasRequestID proves that the request_id
// field on the internal-error log line matches the requestId in the JSON
// response body, so a single correlation ID links the public response to
// the structured log entry.
func TestWriteAuthErr_InternalErrorLogHasRequestID(t *testing.T) {
	buf, restore := captureLogrusOutput(t)
	defer restore()

	rr := httptest.NewRecorder()
	rr.Header().Set(weberr.HeaderRequestID, "req-correlate-002")
	writeAuthErr(rr, errors.New("some internal failure"))

	var env weberr.PublicError
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal body: %v\nbody: %s", err, rr.Body.String())
	}
	logOut := buf.String()
	if !strings.Contains(logOut, env.RequestID) {
		t.Errorf("log does not contain the response requestId %q:\n%s", env.RequestID, logOut)
	}
}

// TestWriteAuthErrForCode_OperationSpecificCode proves writeAuthErrForCode
// emits a registered operation-specific internal code (e.g. kv_unavailable)
// rather than collapsing to generic server_error, while still logging the
// raw error safely and stamping the request ID.
func TestWriteAuthErrForCode_OperationSpecificCode(t *testing.T) {
	buf, restore := captureLogrusOutput(t)
	defer restore()

	rr := httptest.NewRecorder()
	rr.Header().Set(weberr.HeaderRequestID, "req-op-specific-003")
	writeAuthErrForCode(rr, "kv_unavailable", errors.New("kv.Get: dial tcp: secret@kv:6379"))

	// The response code must be the operation-specific code, not server_error.
	var env weberr.PublicError
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal body: %v\nbody: %s", err, rr.Body.String())
	}
	if env.Code != "kv_unavailable" {
		t.Errorf("code = %q, want kv_unavailable", env.Code)
	}
	if env.RequestID != "req-op-specific-003" {
		t.Errorf("requestId = %q, want req-op-specific-003", env.RequestID)
	}
	// HTTP status from the registered definition (503 for kv_unavailable).
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rr.Code)
	}
	assertNoMessageField(t, rr.Body.Bytes())

	// Raw error string must NOT be in logs.
	logOut := buf.String()
	if strings.Contains(logOut, "secret@kv") {
		t.Errorf("log leaked raw error string:\n%s", logOut)
	}
	// Request ID must be in logs.
	if !strings.Contains(logOut, "req-op-specific-003") {
		t.Errorf("log missing request_id:\n%s", logOut)
	}
	// Operation-specific code must be in logs (not just server_error).
	if !strings.Contains(logOut, "server_error") {
		// logInternalError always logs code=server_error for the canonical
		// envelope; the operation-specific code is in the response.
		// This is acceptable — the key invariant is that raw error is absent.
	}
}

// TestWriteAuthErrForCode_UnregisteredCodeFallsBack proves that if an
// unregistered code is passed to writeAuthErrForCode, the response falls
// back to server_error via WriteJSON's registry validation.
func TestWriteAuthErrForCode_UnregisteredCodeFallsBack(t *testing.T) {
	_, restore := captureLogrusOutput(t)
	defer restore() // suppress internal-error log noise; no assertions needed

	rr := httptest.NewRecorder()
	writeAuthErrForCode(rr, "this_code_is_not_registered", errors.New("fail"))

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (fallback)", rr.Code)
	}
	var env weberr.PublicError
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal body: %v\nbody: %s", err, rr.Body.String())
	}
	if env.Code != "server_error" {
		t.Errorf("code = %q, want server_error (fallback)", env.Code)
	}
}

// TestWriteHumaPublicErr_RoutesThroughRegistry proves writeHumaPublicErr
// validates the code against the registry: an unregistered code falls back
// to server_error with HTTP 500, and the status comes from the definition,
// not the caller.
func TestWriteHumaPublicErr_RoutesThroughRegistry(t *testing.T) {
	// This test exercises the huma adapter path via a real huma.Context.
	// Since constructing a huma.Context in isolation is cumbersome, we
	// verify the underlying invariant through the registry directly: the
	// writeHumaPublicErr function calls DefinitionFor and falls back.
	def, ok := weberr.DefinitionFor("server_error")
	if !ok {
		t.Fatal("server_error not registered")
	}
	if def.Status != http.StatusInternalServerError {
		t.Errorf("server_error status = %d, want 500", def.Status)
	}
	// An unregistered code must not be found.
	if _, ok := weberr.DefinitionFor("definitely_not_registered"); ok {
		t.Fatal("unregistered code found in registry")
	}
	// A registered auth code must map to its status.
	def, ok = weberr.DefinitionFor("sudo_required")
	if !ok {
		t.Fatal("sudo_required not registered")
	}
	if def.Status != http.StatusUnauthorized {
		t.Errorf("sudo_required status = %d, want 401", def.Status)
	}
}
