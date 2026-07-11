package server

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"

	"prohibitorum/pkg/authn"
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

	body := rr.Body.String()
	if strings.Contains(body, secret) {
		t.Errorf("response body leaked internal detail: %s", body)
	}
	if strings.Contains(body, "handleX") {
		t.Errorf("response body leaked handler name: %s", body)
	}
	// The response must be JSON with a canonical server_error code — not
	// the raw err.Error() plaintext that http.Error would emit.
	if !strings.Contains(body, "server_error") {
		t.Errorf("body = %q, want canonical server_error JSON", body)
	}
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

// TestWriteAuthErr_AuthError_Unchanged confirms that a proper *authn.AuthError
// is still rendered with its exact status/code/message — the fallback change
// must not alter AuthError behavior.
func TestWriteAuthErr_AuthError_Unchanged(t *testing.T) {
	ae := authn.ErrSudoRequired()

	rr := httptest.NewRecorder()
	writeAuthErr(rr, ae)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "sudo_required") {
		t.Errorf("body = %q, want sudo_required code", body)
	}
	if !strings.Contains(body, ae.Message) {
		t.Errorf("body = %q, want message %q", body, ae.Message)
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
	if !strings.Contains(rr.Body.String(), "bad_request") {
		t.Errorf("body = %q, want bad_request code", rr.Body.String())
	}
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
}

// TestServerError_NoDetailLeak is the plan's Task 2 RED test: a synthetic
// DB/KV error string is absent from the response and captured in structured
// logs. Uses the real router so the full path (wrapper → handler → writeAuthErr)
// is exercised; the handler is a stub that calls writeAuthErr with the
// synthetic error.
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
	body := rr.Body.String()
	if strings.Contains(body, secret) {
		t.Errorf("response body leaked internal detail: %s", body)
	}
	if strings.Contains(body, "kv.Get") {
		t.Errorf("response body leaked internal handler detail: %s", body)
	}
	if !strings.Contains(body, "server_error") {
		t.Errorf("body = %q, want server_error", body)
	}

	logOut := buf.String()
	if !strings.Contains(logOut, secret) {
		t.Errorf("internal detail not in structured logs; log: %s", logOut)
	}
}
