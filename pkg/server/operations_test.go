package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
)

// adminSession builds a minimal *authn.Session with role=admin and
// an expired (zero) SudoUntil, so requireFreshSudo will reject it.
func adminSession(sudoUntil time.Time) *authn.Session {
	return &authn.Session{
		Account: &db.Account{Role: "admin"},
		Token:   "tok",
		Data:    &authn.SessionData{SudoUntil: sudoUntil},
	}
}

// reqWithSession constructs a test request with the given session in context.
// Content-Type is set to application/json only when body is non-empty and
// forceContentType is "". Pass a non-empty forceContentType to override.
func reqWithSession(method, path, body, forceContentType string, sess *authn.Session) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if forceContentType != "" {
		r.Header.Set("Content-Type", forceContentType)
		r.ContentLength = int64(len(body))
	} else if body != "" {
		r.Header.Set("Content-Type", "application/json")
		r.ContentLength = int64(len(body))
	}
	return r.WithContext(authn.WithSession(r.Context(), sess))
}

// TestRegisterSudoOpHTTP_RejectsWithoutFreshSudo verifies that a request with
// no fresh sudo grant is rejected 401 "sudo_required" before the handler runs.
// The session has no SudoUntil (zero value → expired), so requireFreshSudo
// rejects it without touching sessionStore — &Server{} is safe here.
func TestRegisterSudoOpHTTP_RejectsWithoutFreshSudo(t *testing.T) {
	s := &Server{}
	router := chi.NewRouter()
	called := false
	s.registerSudoOpHTTP(router, "POST", "/x", contract.AuthRequirement{Kind: contract.AuthAdmin},
		func(w http.ResponseWriter, r *http.Request) { called = true; w.WriteHeader(http.StatusNoContent) })

	sess := adminSession(time.Time{}) // zero SudoUntil = no fresh sudo
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, reqWithSession("POST", "/x", `{}`, "", sess))

	if called {
		t.Fatal("handler ran despite missing fresh sudo")
	}
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "sudo_required") {
		t.Fatalf("body = %q, want sudo_required", rr.Body.String())
	}
}

// TestAddPasskeyBeginRequiresFreshSudo guards T1.3: registering a new
// authenticator requires fresh sudo. The gate runs before any DB access, so a
// no-sudo session is rejected 401 "sudo_required" without touching s.queries —
// &Server{} is safe. (Only /begin is gated; /complete relies on the
// sudo-gated begin having produced the server-side ceremony stash.)
func TestAddPasskeyBeginRequiresFreshSudo(t *testing.T) {
	s := &Server{}
	rr := httptest.NewRecorder()
	req := reqWithSession("POST", "/api/prohibitorum/me/credentials/register/begin", "", "", adminSession(time.Time{}))
	s.handleAddCredentialBeginHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 sudo_required", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "sudo_required") {
		t.Errorf("body = %q, want sudo_required", rr.Body.String())
	}
}

// TestRegisterSudoOpHTTP_RejectsNonJSON verifies that a request with the wrong
// Content-Type is rejected 400 "bad_request" before the handler runs — and
// crucially BEFORE requireFreshSudo, so no sudo grant is consumed.
//
// Because the content-type check runs first, &Server{} is safe here too:
// requireFreshSudo (which would need sessionStore) is never reached.
func TestRegisterSudoOpHTTP_RejectsNonJSON(t *testing.T) {
	s := &Server{}
	router := chi.NewRouter()
	called := false
	s.registerSudoOpHTTP(router, "POST", "/x", contract.AuthRequirement{Kind: contract.AuthAdmin},
		func(w http.ResponseWriter, r *http.Request) { called = true; w.WriteHeader(http.StatusNoContent) })

	// Fresh sudo grant (far future) — content-type check should fire before it.
	sess := adminSession(time.Now().Add(time.Hour))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, reqWithSession("POST", "/x", `data`, "text/plain", sess))

	if called {
		t.Fatal("handler ran despite non-JSON content-type")
	}
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "bad_request") {
		t.Fatalf("body = %q, want bad_request", rr.Body.String())
	}
}

// TestRegisterSudoOpHTTP_RejectsOversizedBody verifies that a body exceeding
// maxAdminBody is rejected 413 before the handler runs — not a generic 500.
// The Content-Length header advertises the size, so the proactive check in
// withAdminBodyControls fires before MaxBytesReader's lazy enforcement.
func TestRegisterSudoOpHTTP_RejectsOversizedBody(t *testing.T) {
	s := &Server{}
	router := chi.NewRouter()
	called := false
	s.registerSudoOpHTTP(router, "POST", "/x", contract.AuthRequirement{Kind: contract.AuthAdmin},
		func(w http.ResponseWriter, r *http.Request) { called = true; w.WriteHeader(http.StatusNoContent) })

	// Fresh sudo grant — the 413 must fire before the sudo gate too.
	sess := adminSession(time.Now().Add(time.Hour))
	oversized := strings.Repeat("x", maxAdminBody+1)
	rr := httptest.NewRecorder()
	req := reqWithSession("POST", "/x", oversized, "", sess)
	router.ServeHTTP(rr, req)

	if called {
		t.Fatal("handler ran despite oversized body")
	}
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rr.Code)
	}
}

// TestRegisterSudoOpHTTP_RejectsUnknownLengthOversizedBody is the regression
// for the ContentLength=-1 bypass: a chunked/unknown-length request whose
// advertised Content-Length is -1, carrying a valid small JSON object
// followed by enough trailing whitespace/bytes to exceed maxAdminBody. The
// proactive Content-Length check cannot fire (no length advertised), and the
// lazy MaxBytesReader never trips because json.Decode stops after the valid
// JSON value and never reads the trailing bytes. The wrapper must therefore
// fully drain (bounded) the body before invoking the sudo gate or handler;
// otherwise an oversized unknown-length request reaches the handler and
// mutates state despite exceeding the cap. The handler must not be called.
func TestRegisterSudoOpHTTP_RejectsUnknownLengthOversizedBody(t *testing.T) {
	s := &Server{}
	router := chi.NewRouter()
	called := false
	s.registerSudoOpHTTP(router, "POST", "/x", contract.AuthRequirement{Kind: contract.AuthAdmin},
		func(w http.ResponseWriter, r *http.Request) { called = true; w.WriteHeader(http.StatusNoContent) })

	// Fresh sudo grant — the 413 must fire before the sudo gate too.
	sess := adminSession(time.Now().Add(time.Hour))
	// A valid small JSON object followed by trailing whitespace that pushes
	// the total well past maxAdminBody. Whitespace after a complete JSON
	// value is harmless to json.Decode (it stops at the end of the value),
	// so this is exactly the shape that bypasses lazy MaxBytesReader.
	body := `{}` + strings.Repeat(" ", maxAdminBody+1)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/x", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = -1 // unknown length — defeats the proactive check
	req = req.WithContext(authn.WithSession(req.Context(), sess))
	router.ServeHTTP(rr, req)

	if called {
		t.Fatal("handler ran despite unknown-length oversized body")
	}
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rr.Code)
	}
}

// TestRegisterAdminBodyOpHTTP_RejectsUnknownLengthOversizedBody mirrors the
// sudo variant for the admin-only (no-sudo) registration helper: an
// unknown-length body (ContentLength=-1) carrying valid JSON plus trailing
// bytes exceeding maxAdminBody must be rejected 413 before the handler runs.
func TestRegisterAdminBodyOpHTTP_RejectsUnknownLengthOversizedBody(t *testing.T) {
	s := &Server{}
	router := chi.NewRouter()
	called := false
	s.registerAdminBodyOpHTTP(router, "POST", "/x", contract.AuthRequirement{Kind: contract.AuthAdmin},
		func(w http.ResponseWriter, r *http.Request) { called = true; w.WriteHeader(http.StatusNoContent) })

	sess := adminSession(time.Time{}) // no sudo gate on this tier
	body := `{}` + strings.Repeat(" ", maxAdminBody+1)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/x", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = -1
	req = req.WithContext(authn.WithSession(req.Context(), sess))
	router.ServeHTTP(rr, req)

	if called {
		t.Fatal("handler ran despite unknown-length oversized body")
	}
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rr.Code)
	}
}

// TestRegisterSudoOpHTTP_RejectsMissingContentType verifies that a request
// with a body but no Content-Type header is rejected 400 bad_request before
// the handler runs. The body controls require an explicit JSON content type.
func TestRegisterSudoOpHTTP_RejectsMissingContentType(t *testing.T) {
	s := &Server{}
	router := chi.NewRouter()
	called := false
	s.registerSudoOpHTTP(router, "POST", "/x", contract.AuthRequirement{Kind: contract.AuthAdmin},
		func(w http.ResponseWriter, r *http.Request) { called = true; w.WriteHeader(http.StatusNoContent) })

	sess := adminSession(time.Now().Add(time.Hour))
	rr := httptest.NewRecorder()
	// Build a request with a body but no Content-Type header.
	req := httptest.NewRequest("POST", "/x", strings.NewReader(`{}`))
	req.ContentLength = 2
	req = req.WithContext(authn.WithSession(req.Context(), sess))
	router.ServeHTTP(rr, req)

	if called {
		t.Fatal("handler ran despite missing content-type")
	}
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "bad_request") {
		t.Fatalf("body = %q, want bad_request", rr.Body.String())
	}
}

// TestRegisterAdminBodyOpHTTP_EnforcesBodyControls verifies the admin-only
// (no-sudo) registration helper installs the same content-type + body-size
// controls as registerSudoOpHTTP, minus the sudo gate.
func TestRegisterAdminBodyOpHTTP_EnforcesBodyControls(t *testing.T) {
	s := &Server{}
	router := chi.NewRouter()
	called := false
	s.registerAdminBodyOpHTTP(router, "POST", "/x", contract.AuthRequirement{Kind: contract.AuthAdmin},
		func(w http.ResponseWriter, r *http.Request) { called = true; w.WriteHeader(http.StatusNoContent) })

	sess := adminSession(time.Time{}) // no fresh sudo — should still work (no sudo gate)

	t.Run("oversized body → 413", func(t *testing.T) {
		called = false
		oversized := strings.Repeat("x", maxAdminBody+1)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, reqWithSession("POST", "/x", oversized, "", sess))
		if called {
			t.Fatal("handler ran despite oversized body")
		}
		if rr.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("status = %d, want 413", rr.Code)
		}
	})

	t.Run("wrong content-type → 400", func(t *testing.T) {
		called = false
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, reqWithSession("POST", "/x", `data`, "text/plain", sess))
		if called {
			t.Fatal("handler ran despite non-JSON content-type")
		}
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rr.Code)
		}
	})

	t.Run("valid JSON → handler runs (no sudo gate)", func(t *testing.T) {
		called = false
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, reqWithSession("POST", "/x", `{}`, "", sess))
		if !called {
			t.Fatal("handler did not run for valid JSON admin-only request")
		}
		if rr.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want 204", rr.Code)
		}
	})
}

// TestWriteAuthErr_MaxBytesError_Returns413 verifies that a *http.MaxBytesError
// (from the lazy MaxBytesReader path) is detected through wrapping and mapped
// to 413, not a generic 500.
func TestWriteAuthErr_MaxBytesError_Returns413(t *testing.T) {
	// Simulate what json.Decode does: it wraps the MaxBytesError.
	maxErr := &http.MaxBytesError{Limit: maxAdminBody}
	wrapped := fmt.Errorf("json.Decode: %w", maxErr)

	rr := httptest.NewRecorder()
	writeAuthErr(rr, wrapped)

	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "request_too_large") {
		t.Fatalf("body = %q, want request_too_large code", rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "request body too large") {
		t.Fatalf("body = %q, want 'request body too large' message", rr.Body.String())
	}
}
