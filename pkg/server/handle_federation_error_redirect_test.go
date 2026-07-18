// Package server — handle_federation_error_redirect_test.go
//
// Tests that browser-navigated federation error paths redirect to /error
// instead of returning JSON (Task 2: federation/invite/link → /error).
//
// Reuses fedTestHarness, fakeFedQueries, newFederationTestServer,
// newInviteTestServer, and helpers from handle_federation_test.go and
// handle_invite_federation_test.go (same package).
package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"prohibitorum/pkg/audit"
)

// withTokenParam attaches a chi RouteContext with the given token value onto r,
// mirroring how chi populates URL params for named path segments.
func withTokenParam(r *http.Request, token string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("token", token)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

// TestFederationLogin_BadReturnTo_RedirectsToErrorPage: a GET /login with an
// off-origin return_to must 302 to /error?error=invalid_return_to&ref=…
// (not a JSON 400).
func TestFederationLogin_BadReturnTo_RedirectsToErrorPage(t *testing.T) {
	h := newFederationTestServer(t)

	_, resp := h.driveLogin(t, "mockop", "https://evil.example/x")
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status: want 302, got %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	const wantPrefix = "/error?error=invalid_return_to&ref="
	if !strings.HasPrefix(loc, wantPrefix) {
		t.Errorf("Location: want prefix %q, got %q", wantPrefix, loc)
	}
	// ref must be a non-empty hex string (8 chars from weberr.NewRef).
	ref := strings.TrimPrefix(loc, wantPrefix)
	if ref == "" {
		t.Errorf("ref: want non-empty, got empty (Location=%q)", loc)
	}
}

// TestFederationLogin_BeginError_ForwardsReturnTo: when BeginPublic fails but
// the same-origin return_to was validated, it must be forwarded to /error so
// the page's "go back" link can resume where the user started. An unknown slug
// drives BeginPublic into ErrUnknownProvider (collapsed to federation_state_invalid).
func TestFederationLogin_BeginError_ForwardsReturnTo(t *testing.T) {
	h := newFederationTestServer(t)

	_, resp := h.driveLogin(t, "no-such-idp", "/connected")
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status: want 302, got %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, "/error?error=") {
		t.Fatalf("want an /error redirect, got %q", loc)
	}
	if !strings.Contains(loc, "return_to=%2Fconnected") {
		t.Errorf("Location must forward return_to=%%2Fconnected, got %q", loc)
	}
}

// TestFederationCallback_UpstreamError_RedirectsToErrorPage: ?error=access_denied
// must 302 to /error?error=upstream_error&ref=…; the audit row must include a
// non-empty "ref" field in its Detail map, and the ref in the Location must
// match the ref in the audit row.
func TestFederationCallback_UpstreamError_RedirectsToErrorPage(t *testing.T) {
	h := newFederationTestServer(t)

	q := url.Values{}
	q.Set("error", "access_denied")
	q.Set("error_description", "nope")
	resp := h.hitCallback(t, "mockop", q)

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status: want 302, got %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	const wantPrefix = "/error?error=upstream_error&ref="
	if !strings.HasPrefix(loc, wantPrefix) {
		t.Errorf("Location: want prefix %q, got %q", wantPrefix, loc)
	}

	// Audit row must exist and carry a non-empty "ref".
	if len(h.q.events) == 0 {
		t.Fatal("no audit event recorded")
	}
	ev := h.q.events[len(h.q.events)-1]
	if ev.Factor != string(audit.FactorFederationOIDC) || ev.Event != audit.EventFail {
		t.Errorf("audit factor/event: got %s/%s, want %s/%s",
			ev.Factor, ev.Event, audit.FactorFederationOIDC, audit.EventFail)
	}
	var detail map[string]any
	if err := json.Unmarshal(ev.Detail, &detail); err != nil {
		t.Fatalf("decode audit detail: %v", err)
	}
	auditRef, _ := detail["ref"].(string)
	if auditRef == "" {
		t.Errorf("audit detail: want non-empty ref, got %v (detail=%v)", detail["ref"], detail)
	}
	// ref in Location and ref in audit detail must match.
	locRef := strings.TrimPrefix(loc, wantPrefix)
	if locRef != auditRef {
		t.Errorf("ref mismatch: Location has %q, audit has %q", locRef, auditRef)
	}
}

// TestFederationCallback_MissingState_RedirectsToErrorPage: missing state/code
// must 302 to /error?error=federation_state_invalid&ref=… (no audit row).
func TestFederationCallback_MissingState_RedirectsToErrorPage(t *testing.T) {
	h := newFederationTestServer(t)

	q := url.Values{}
	q.Set("state", "")
	q.Set("code", "abc")
	resp := h.hitCallback(t, "mockop", q)

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status: want 302, got %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	const wantPrefix = "/error?error=federation_state_invalid&ref="
	if !strings.HasPrefix(loc, wantPrefix) {
		t.Errorf("Location: want prefix %q, got %q", wantPrefix, loc)
	}
	// No audit row — stray browser hits must not flood the log.
	for _, ev := range h.q.events {
		if ev.Event == audit.EventFail {
			t.Errorf("unexpected fail audit row on missing-state: %+v", ev)
		}
	}
}

// TestInviteStartFederation_EmptyToken_RedirectsToErrorPage: an empty token
// must 302 to /error?error=invite_required&ref=….
// chi won't route an empty {token} segment, so we call the handler directly
// with a chi RouteContext that has token="" — the same pattern used by
// handle_account_sessions_test.go's withRouteParam helper.
func TestInviteStartFederation_EmptyToken_RedirectsToErrorPage(t *testing.T) {
	h := newInviteTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/prohibitorum/enrollments//start-federation", nil)
	req = withTokenParam(req, "") // token="" triggers the handler's guard
	w := httptest.NewRecorder()
	h.s.handleEnrollmentStartFederationHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status: want 302, got %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	const wantPrefix = "/error?error=invite_required&ref="
	if !strings.HasPrefix(loc, wantPrefix) {
		t.Errorf("Location: want prefix %q, got %q", wantPrefix, loc)
	}
	ref := strings.TrimPrefix(loc, wantPrefix)
	if ref == "" {
		t.Errorf("ref: want non-empty, got empty (Location=%q)", loc)
	}
}

// TestIdentityLinkCallback_UpstreamError_RedirectsToErrorPage: the link
// callback with ?error=access_denied must 302 to
// /error?error=upstream_error&ref=… and stamp ref onto the audit Detail.
func TestIdentityLinkCallback_UpstreamError_RedirectsToErrorPage(t *testing.T) {
	h := newLinkTestHarness(t)

	q := url.Values{}
	q.Set("error", "access_denied")
	q.Set("error_description", "user denied consent")
	resp := h.hitLinkCallback(t, "mockop", q)

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status: want 302, got %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	const wantPrefix = "/error?error=upstream_error&ref="
	if !strings.HasPrefix(loc, wantPrefix) {
		t.Errorf("Location: want prefix %q, got %q", wantPrefix, loc)
	}

	// Audit row must carry account_id + non-empty ref.
	found := false
	for _, ev := range h.q.events {
		if ev.Factor != string(audit.FactorFederationOIDC) || ev.Event != audit.EventFail {
			continue
		}
		var detail map[string]any
		if err := json.Unmarshal(ev.Detail, &detail); err != nil {
			continue
		}
		if detail["reason"] != "upstream_error" {
			continue
		}
		found = true
		if ev.AccountID == nil || *ev.AccountID != h.linkAccountID {
			t.Errorf("audit account_id: want %d, got %v", h.linkAccountID, ev.AccountID)
		}
		auditRef, _ := detail["ref"].(string)
		if auditRef == "" {
			t.Errorf("audit detail: want non-empty ref; detail=%v", detail)
		}
		// ref must match the one in the Location header.
		locRef := strings.TrimPrefix(loc, wantPrefix)
		if locRef != auditRef {
			t.Errorf("ref mismatch: Location=%q audit=%q", locRef, auditRef)
		}
		break
	}
	if !found {
		t.Errorf("audit: missing upstream_error fail row; events=%+v", h.q.events)
	}
}
