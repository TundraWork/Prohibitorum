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

	// The internal detail MUST be captured in structured logs.
	logOut := buf.String()
	if !strings.Contains(logOut, secret) {
		t.Errorf("internal detail not captured in structured logs; log: %s", logOut)
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

	logOut := buf.String()
	if !strings.Contains(logOut, secret) {
		t.Errorf("internal detail not in structured logs; log: %s", logOut)
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
