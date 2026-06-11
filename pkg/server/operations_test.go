package server

import (
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
