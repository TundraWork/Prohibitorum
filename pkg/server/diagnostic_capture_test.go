package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"prohibitorum/pkg/diagnostic"
	"prohibitorum/pkg/weberr"
)

type recordingDiagnosticWriter struct {
	records []diagnostic.Record
	err     error
}

func (w *recordingDiagnosticWriter) Record(_ context.Context, rec diagnostic.Record) error {
	w.records = append(w.records, rec)
	return w.err
}

func TestDiagnosticCaptureRecordsCanonicalRawError(t *testing.T) {
	store := &recordingDiagnosticWriter{}
	router := chi.NewRouter()
	router.Use(weberr.RequestID)
	router.Use(diagnosticCaptureMW(store))
	router.Get("/broken/{id}", func(w http.ResponseWriter, r *http.Request) {
		weberr.WriteJSON(w, "validation_failed", map[string]any{
			"location": "body.name",
			"secret":   "must-not-survive",
		}, weberr.RequestIDFromContext(r.Context()))
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/broken/42", nil))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d", rec.Code)
	}
	if len(store.records) != 1 {
		t.Fatalf("records = %d, want 1", len(store.records))
	}
	got := store.records[0]
	if got.RequestID == "" || got.RequestID != rec.Header().Get("X-Request-ID") || got.Code != "validation_failed" || got.Method != http.MethodGet || got.Route != "/broken/{id}" {
		t.Fatalf("record = %#v", got)
	}
	if got.Operation != "GET /broken/{id}" || got.AccountID != nil {
		t.Fatalf("operation/account = %q/%v", got.Operation, got.AccountID)
	}
	if len(got.Fields) != 1 || got.Fields["location"] != "body.name" {
		t.Fatalf("fields = %#v, want only safe location", got.Fields)
	}
	if _, leaked := got.Fields["secret"]; leaked {
		t.Fatalf("record leaked rejected detail: %#v", got.Fields)
	}
}

func TestDiagnosticCaptureWriteFailureDoesNotChangeResponse(t *testing.T) {
	store := &recordingDiagnosticWriter{err: errors.New("database unavailable: private value")}
	router := chi.NewRouter()
	router.Use(weberr.RequestID)
	router.Use(diagnosticCaptureMW(store))
	router.Get("/broken", func(w http.ResponseWriter, r *http.Request) {
		weberr.WriteJSON(w, "server_error", nil, weberr.RequestIDFromContext(r.Context()))
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/broken", nil))
	if rec.Code != http.StatusInternalServerError || rec.Body.String() != `{"code":"server_error","requestId":"`+rec.Header().Get("X-Request-ID")+`"}`+"\n" {
		t.Fatalf("response changed: status=%d body=%q", rec.Code, rec.Body.String())
	}
}
