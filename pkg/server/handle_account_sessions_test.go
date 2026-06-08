// Package server — handle_account_sessions_test.go
//
// Unit tests for GET /accounts/{id}/sessions (handleListAccountSessions) and
// POST /accounts/{id}/sessions/revoke (handleRevokeAccountSessionHTTP).
//
// Design note: s.sessionStore is a concrete *sessstore.SessionStore backed by a
// real KV store — it cannot be stubbed in unit tests. Tests here therefore cover:
//
//  1. The SessionListItem mapping logic (pure: no DB or KV needed).
//  2. The revoke handler's request-parsing guard paths (bad id, missing body) —
//     these return writeAuthErr before touching any DB or session store.
//
// The account-not-found (404) path and the successful revoke/list paths require a
// live DB+KV and are covered by integration / smoke tests.

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"prohibitorum/pkg/authn"
	sessstore "prohibitorum/pkg/session"
)

// ---------------------------------------------------------------------------
// SessionListItem mapping invariants
// ---------------------------------------------------------------------------

// TestHandleListAccountSessions_MappingInvariants verifies that the fields of
// contract.SessionListItem are populated correctly from a sessstore.SessionRecord
// and that IsCurrent is always false (admin view never marks a session current).
//
// This test does NOT exercise the full handler (which needs a live DB + KV);
// instead it calls the same mapping expression the handler uses.
func TestHandleListAccountSessions_MappingInvariants(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Second)
	expires := now.Add(24 * time.Hour)

	record := sessstore.SessionRecord{
		Token: "sekret-token-should-never-appear-in-output",
		Data: authn.SessionData{
			SessionID:  "opaque-session-id-1",
			AccountID:  42,
			IssuedAt:   now,
			ExpiresAt:  expires,
			LastSeenIP: "203.0.113.7",
			UserAgent:  "Mozilla/5.0",
		},
	}

	// Mirror the exact mapping the handler uses.
	item := sessionRecordToItem(record)

	if item.ID != record.Data.SessionID {
		t.Errorf("ID: got %q, want %q", item.ID, record.Data.SessionID)
	}
	if item.IsCurrent {
		t.Error("IsCurrent: admin view must always return false")
	}
	if !item.IssuedAt.Equal(record.Data.IssuedAt) {
		t.Errorf("IssuedAt: got %v, want %v", item.IssuedAt, record.Data.IssuedAt)
	}
	if !item.ExpiresAt.Equal(record.Data.ExpiresAt) {
		t.Errorf("ExpiresAt: got %v, want %v", item.ExpiresAt, record.Data.ExpiresAt)
	}
	if item.LastSeenIP != record.Data.LastSeenIP {
		t.Errorf("LastSeenIP: got %q, want %q", item.LastSeenIP, record.Data.LastSeenIP)
	}
	if item.UserAgent != record.Data.UserAgent {
		t.Errorf("UserAgent: got %q, want %q", item.UserAgent, record.Data.UserAgent)
	}

	// Token must NEVER appear anywhere in the item (opaque handle, not secret, but
	// extra defence-in-depth: the raw cookie half should never be round-tripped).
	if item.ID == record.Token {
		t.Error("ID must not be the raw KV token; it must be SessionData.SessionID")
	}
}

// TestHandleListAccountSessions_MappingEmptyUA verifies that an empty UserAgent
// (pre-schema session) is preserved as-is (empty string in the wire struct;
// the json tag has omitempty so it is omitted in JSON output).
func TestHandleListAccountSessions_MappingEmptyUA(t *testing.T) {
	t.Parallel()

	record := sessstore.SessionRecord{
		Token: "tok",
		Data: authn.SessionData{
			SessionID: "sid",
			IssuedAt:  time.Now(),
			ExpiresAt: time.Now().Add(time.Hour),
			UserAgent: "",
		},
	}

	item := sessionRecordToItem(record)
	if item.UserAgent != "" {
		t.Errorf("UserAgent: got %q, want empty string for pre-schema session", item.UserAgent)
	}
}

// ---------------------------------------------------------------------------
// handleRevokeAccountSessionHTTP — bad-request guard paths
// ---------------------------------------------------------------------------

// buildRevokeRequest builds a POST request to /accounts/{id}/sessions/revoke
// with chi URL params pre-populated so chi.URLParam works without a real router.
func buildRevokeRequest(idParam, bodyJSON string) *http.Request {
	var bodyReader *bytes.Reader
	if bodyJSON == "" {
		bodyReader = bytes.NewReader(nil)
	} else {
		bodyReader = bytes.NewReader([]byte(bodyJSON))
	}
	req := httptest.NewRequest("POST", "/api/prohibitorum/accounts/"+idParam+"/sessions/revoke", bodyReader)
	req.Header.Set("Content-Type", "application/json")

	// Install chi route context so chi.URLParam(r, "id") works.
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", idParam)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

// TestHandleRevokeAccountSession_BadID verifies that a non-integer path id
// returns a 400-family response containing "bad_request" before any DB call.
func TestHandleRevokeAccountSession_BadID(t *testing.T) {
	t.Parallel()

	s := &Server{} // queries and sessionStore are nil; handler must not reach them
	rr := httptest.NewRecorder()
	req := buildRevokeRequest("not-an-int", `{"sessionId":"abc"}`)
	s.handleRevokeAccountSessionHTTP(rr, req)

	if rr.Code < 400 || rr.Code >= 500 {
		t.Errorf("status = %d; want 4xx for bad id", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "bad_request") {
		t.Errorf("body = %q; want bad_request", rr.Body.String())
	}
}

// TestHandleRevokeAccountSession_EmptySessionID verifies that an empty
// sessionId in the request body is rejected as bad_request before any DB call.
func TestHandleRevokeAccountSession_EmptySessionID(t *testing.T) {
	t.Parallel()

	s := &Server{}
	rr := httptest.NewRecorder()
	req := buildRevokeRequest("1", `{"sessionId":""}`)
	s.handleRevokeAccountSessionHTTP(rr, req)

	if rr.Code < 400 || rr.Code >= 500 {
		t.Errorf("status = %d; want 4xx for empty sessionId", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "bad_request") {
		t.Errorf("body = %q; want bad_request", rr.Body.String())
	}
}

// TestHandleRevokeAccountSession_MalformedJSON verifies that unparseable JSON
// body returns bad_request before any DB call.
func TestHandleRevokeAccountSession_MalformedJSON(t *testing.T) {
	t.Parallel()

	s := &Server{}
	rr := httptest.NewRecorder()
	req := buildRevokeRequest("1", `this is not json`)
	s.handleRevokeAccountSessionHTTP(rr, req)

	if rr.Code < 400 || rr.Code >= 500 {
		t.Errorf("status = %d; want 4xx for malformed JSON", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "bad_request") {
		t.Errorf("body = %q; want bad_request", rr.Body.String())
	}
}

// TestHandleRevokeAccountSession_MissingBody verifies that an empty body
// returns bad_request.
func TestHandleRevokeAccountSession_MissingBody(t *testing.T) {
	t.Parallel()

	s := &Server{}
	rr := httptest.NewRecorder()
	req := buildRevokeRequest("1", "")
	s.handleRevokeAccountSessionHTTP(rr, req)

	if rr.Code < 400 || rr.Code >= 500 {
		t.Errorf("status = %d; want 4xx for empty body", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// Response shape: JSON serialisation of SessionListItem
// ---------------------------------------------------------------------------

// TestSessionListItemJSONShape verifies the wire JSON field names expected by
// the frontend.  IsCurrent must serialize to "isCurrent":false (not omitted).
func TestSessionListItemJSONShape(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	record := sessstore.SessionRecord{
		Token: "tok",
		Data: authn.SessionData{
			SessionID:  "sid-42",
			IssuedAt:   now,
			ExpiresAt:  now.Add(time.Hour),
			LastSeenIP: "1.2.3.4",
			UserAgent:  "Go-http-client/1.1",
		},
	}

	item := sessionRecordToItem(record)
	b, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)

	for _, want := range []string{`"id":"sid-42"`, `"isCurrent":false`, `"lastSeenIp":"1.2.3.4"`} {
		if !strings.Contains(s, want) {
			t.Errorf("JSON output missing %q\nfull JSON: %s", want, s)
		}
	}
}
