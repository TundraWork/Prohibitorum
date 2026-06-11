// Package server — handle_invite_federation_test.go
//
// Handler-level tests for the invite-bound federation entrypoint
// (GET /enrollments/{token}/start-federation).
//
// Scaffolding decision: extend the fedTestHarness from handle_federation_test.go
// rather than build a new one. The two flows share every collaborator
// (mock OP, fake querier, KV, audit, sessionStore) and the only new surface
// is enrollment seeding + the start-federation handler. The existing
// fakeFedQueries embeds db.Querier, so we add GetEnrollmentByToken +
// ConsumeEnrollment as new methods on the same fake (Go method sets
// span files within a package).

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/db"
	fedoidc "prohibitorum/pkg/federation/oidc"
	sessstore "prohibitorum/pkg/session"
)

// --- fake extensions for enrollment lookup / consume ----------------------
//
// These methods extend the fakeFedQueries declared in handle_federation_test.go.
// Tokens are stored in a single map: GetEnrollmentByToken reads it for the
// /start-federation validation, ConsumeEnrollment mutates ConsumedAt + returns
// the row only if it was unconsumed AND unexpired (mirrors the SQL CTE).

func (f *fakeFedQueries) seedEnrollment(enr db.Enrollment) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.enrollmentByToken == nil {
		f.enrollmentByToken = map[string]db.Enrollment{}
	}
	f.enrollmentByToken[enr.Token] = enr
}

func (f *fakeFedQueries) GetEnrollmentByToken(_ context.Context, token string) (db.Enrollment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if e, ok := f.enrollmentByToken[token]; ok {
		return e, nil
	}
	return db.Enrollment{}, pgx.ErrNoRows
}

func (f *fakeFedQueries) ConsumeEnrollment(_ context.Context, token string) (db.Enrollment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.enrollmentByToken[token]
	if !ok {
		return db.Enrollment{}, pgx.ErrNoRows
	}
	// Mirror the UPDATE ... WHERE consumed_at IS NULL AND expires_at > now()
	// semantics — caller sees pgx.ErrNoRows for any "not redeemable" branch.
	if e.ConsumedAt.Valid {
		return db.Enrollment{}, pgx.ErrNoRows
	}
	if !e.ExpiresAt.Valid || !e.ExpiresAt.Time.After(time.Now()) {
		return db.Enrollment{}, pgx.ErrNoRows
	}
	e.ConsumedAt = pgtype.Timestamptz{Time: time.Now(), Valid: true}
	f.enrollmentByToken[token] = e
	return e, nil
}

// --- helpers --------------------------------------------------------------

// driveStartFederation hits /enrollments/{token}/start-federation and returns
// (authorizeURL, response). Empty location on non-302.
func (h *fedTestHarness) driveStartFederation(t *testing.T, token, returnTo string) (string, *http.Response) {
	t.Helper()
	u := h.srvTS.URL + "/api/prohibitorum/enrollments/" + token + "/start-federation"
	if returnTo != "" {
		u += "?return_to=" + url.QueryEscape(returnTo)
	}
	resp, err := h.client.Get(u)
	if err != nil {
		t.Fatalf("GET %s: %v", u, err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusFound {
		return "", resp
	}
	return resp.Header.Get("Location"), resp
}

// newInviteTestServer wraps newFederationTestServer, then additionally
// mounts the /start-federation route under the same chi router. The base
// harness only mounts /login + /callback — we need start-federation too.
func newInviteTestServer(t *testing.T) *fedTestHarness {
	t.Helper()
	h := newFederationTestServer(t)
	// Re-mount with start-federation added. The original chi.Router is owned
	// by httptest.Server, so we replace srvTS to swap the handler.
	r := chi.NewRouter()
	r.Get("/api/prohibitorum/auth/federation/{slug}/login", h.s.handleFederationLoginHTTP)
	r.Get("/api/prohibitorum/auth/federation/{slug}/callback", h.s.handleFederationCallbackHTTP)
	r.Get("/api/prohibitorum/enrollments/{token}/start-federation", h.s.handleEnrollmentStartFederationHTTP)
	h.srvTS.Config.Handler = r
	return h
}

// futureExp returns a pgtype.Timestamptz an hour in the future.
func futureExp() pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true}
}

// pastExp returns a pgtype.Timestamptz an hour in the past.
func pastExp() pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), Valid: true}
}

// validInvite builds a redeemable enrollment row bound to the given slug.
func validInvite(token, slug, username string) db.Enrollment {
	return db.Enrollment{
		Token:                   token,
		Intent:                  "invite",
		ExpectedUpstreamIdpSlug: pgtype.Text{String: slug, Valid: true},
		TemplateUsername:        pgtype.Text{String: username, Valid: true},
		TemplateDisplayName:     pgtype.Text{String: "Alice", Valid: true},
		TemplateRole:            pgtype.Text{String: "user", Valid: true},
		TemplateAttributes:      []byte("{}"),
		ExpiresAt:               futureExp(),
	}
}

// --- tests ----------------------------------------------------------------

func TestEnrollmentStartFederation_HappyPath(t *testing.T) {
	h := newInviteTestServer(t)
	h.q.seedEnrollment(validInvite("tok-happy", h.idp.Slug, "alice"))

	loc, resp := h.driveStartFederation(t, "tok-happy", "/me")
	if resp.StatusCode != http.StatusFound {
		body, _ := readAll(resp.Body)
		t.Fatalf("status: want 302, got %d (body=%s)", resp.StatusCode, body)
	}
	if !strings.HasPrefix(loc, h.opTS.URL+"/authorize") {
		t.Errorf("Location: want prefix %q, got %q", h.opTS.URL+"/authorize", loc)
	}
	if got := resp.Header.Get("Referrer-Policy"); got != "no-referrer" {
		t.Errorf("Referrer-Policy: want no-referrer, got %q", got)
	}

	// State must live under LoginKey with EnrollmentToken set.
	u, _ := url.Parse(loc)
	state := u.Query().Get("state")
	if state == "" {
		t.Fatal("state missing from authorize URL")
	}
	blob, err := h.s.kvStore.Get(context.Background(), fedoidc.LoginKey(state))
	if err != nil {
		t.Fatalf("state not stashed under LoginKey: %v", err)
	}
	fs, err := fedoidc.DecodeFedState(blob)
	if err != nil {
		t.Fatalf("DecodeFedState: %v", err)
	}
	if fs.EnrollmentToken != "tok-happy" {
		t.Errorf("FedState.EnrollmentToken = %q, want tok-happy", fs.EnrollmentToken)
	}
	if fs.LinkingAccountID != nil {
		t.Errorf("LinkingAccountID = %v, want nil (invite flow has no account yet)", *fs.LinkingAccountID)
	}
	if fs.ReturnTo != "/me" {
		t.Errorf("ReturnTo = %q, want /me", fs.ReturnTo)
	}
}

func TestEnrollmentStartFederation_UnknownToken(t *testing.T) {
	h := newInviteTestServer(t)
	// No enrollment seeded.

	_, resp := h.driveStartFederation(t, "no-such-token", "/me")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status: want 403, got %d", resp.StatusCode)
	}
	body := decodeErrBody(t, resp)
	if body.Code != "invite_required" {
		t.Errorf("code: want invite_required, got %q", body.Code)
	}
}

func TestEnrollmentStartFederation_ConsumedToken(t *testing.T) {
	h := newInviteTestServer(t)
	enr := validInvite("tok-consumed", h.idp.Slug, "alice")
	enr.ConsumedAt = pgtype.Timestamptz{Time: time.Now().Add(-time.Minute), Valid: true}
	h.q.seedEnrollment(enr)

	_, resp := h.driveStartFederation(t, "tok-consumed", "/me")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status: want 403, got %d", resp.StatusCode)
	}
	body := decodeErrBody(t, resp)
	if body.Code != "invite_required" {
		t.Errorf("code: want invite_required, got %q", body.Code)
	}
}

func TestEnrollmentStartFederation_ExpiredToken(t *testing.T) {
	h := newInviteTestServer(t)
	enr := validInvite("tok-expired", h.idp.Slug, "alice")
	enr.ExpiresAt = pastExp()
	h.q.seedEnrollment(enr)

	_, resp := h.driveStartFederation(t, "tok-expired", "/me")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status: want 403, got %d", resp.StatusCode)
	}
	body := decodeErrBody(t, resp)
	if body.Code != "invite_required" {
		t.Errorf("code: want invite_required, got %q", body.Code)
	}
}

func TestEnrollmentStartFederation_NonFederationIntent(t *testing.T) {
	h := newInviteTestServer(t)
	enr := validInvite("tok-bootstrap", h.idp.Slug, "alice")
	enr.Intent = "bootstrap" // Even with slug binding, non-invite intent must reject.
	h.q.seedEnrollment(enr)

	_, resp := h.driveStartFederation(t, "tok-bootstrap", "/me")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status: want 403, got %d", resp.StatusCode)
	}
	body := decodeErrBody(t, resp)
	if body.Code != "invite_required" {
		t.Errorf("code: want invite_required, got %q", body.Code)
	}
}

func TestEnrollmentStartFederation_NoSlugBinding(t *testing.T) {
	h := newInviteTestServer(t)
	enr := validInvite("tok-no-slug", h.idp.Slug, "alice")
	enr.ExpectedUpstreamIdpSlug = pgtype.Text{Valid: false} // NULL — WebAuthn invite, not federation.
	h.q.seedEnrollment(enr)

	_, resp := h.driveStartFederation(t, "tok-no-slug", "/me")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status: want 403, got %d", resp.StatusCode)
	}
	body := decodeErrBody(t, resp)
	if body.Code != "invite_required" {
		t.Errorf("code: want invite_required, got %q", body.Code)
	}
}

func TestEnrollmentStartFederation_InvalidReturnTo(t *testing.T) {
	h := newInviteTestServer(t)
	h.q.seedEnrollment(validInvite("tok-rt", h.idp.Slug, "alice"))

	_, resp := h.driveStartFederation(t, "tok-rt", "https://evil.example.com")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d", resp.StatusCode)
	}
	body := decodeErrBody(t, resp)
	if body.Code != "invalid_return_to" {
		t.Errorf("code: want invalid_return_to, got %q", body.Code)
	}
}

func TestEnrollmentStartFederation_FullFlow_RedeemsInvite(t *testing.T) {
	h := newInviteTestServer(t)
	// Mock OP is in ModeAutoProvision per harness defaults — mode-decoupling
	// means the EnrollmentToken on FedState is what routes through
	// applyInviteOnly. Upstream "alice" preferred_username is ignored; the
	// template_username "invited-bob" is authoritative.
	h.q.seedEnrollment(validInvite("tok-full", h.idp.Slug, "invited-bob"))

	// Step 1: /start-federation → 302 to /authorize.
	loc, resp := h.driveStartFederation(t, "tok-full", "/me")
	if resp.StatusCode != http.StatusFound {
		body, _ := readAll(resp.Body)
		t.Fatalf("start-federation: want 302, got %d (body=%s)", resp.StatusCode, body)
	}

	// Step 2: follow upstream /authorize → 302 to /callback with code+state.
	code, state, iss := driveAuthorize(t, loc)

	// Step 3: hit /callback → session cookie + 302 to /me.
	q := url.Values{}
	q.Set("code", code)
	q.Set("state", state)
	q.Set("iss", iss)
	resp = h.hitCallback(t, h.idp.Slug, q)
	if resp.StatusCode != http.StatusFound {
		body, _ := readAll(resp.Body)
		t.Fatalf("callback: want 302, got %d (body=%s)", resp.StatusCode, body)
	}
	if got := resp.Header.Get("Location"); got != "/me" {
		t.Errorf("Location: want /me, got %q", got)
	}
	var sessCookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == sessstore.SessionCookieName {
			sessCookie = c
			break
		}
	}
	if sessCookie == nil || sessCookie.Value == "" {
		t.Fatal("session cookie not set after invite redemption")
	}

	// Enrollment must be consumed.
	enr, err := h.q.GetEnrollmentByToken(context.Background(), "tok-full")
	if err != nil {
		t.Fatalf("post-flow GetEnrollmentByToken: %v", err)
	}
	if !enr.ConsumedAt.Valid {
		t.Error("enrollment ConsumedAt: want set, got NULL")
	}

	// Exactly one account inserted with template values.
	if len(h.q.insertedAccounts) != 1 {
		t.Fatalf("accounts inserted: want 1, got %d", len(h.q.insertedAccounts))
	}
	acct := h.q.insertedAccounts[0]
	if acct.Username != "invited-bob" {
		t.Errorf("account Username: want invited-bob (from template), got %q", acct.Username)
	}
	if acct.Role != "user" {
		t.Errorf("account Role: want user (from template), got %q", acct.Role)
	}

	// Identity row links to (upstream_iss, upstream_sub).
	if len(h.q.insertIdentitys) != 1 {
		t.Fatalf("identities inserted: want 1, got %d", len(h.q.insertIdentitys))
	}
	if h.q.insertIdentitys[0].UpstreamSub == "" {
		t.Error("identity UpstreamSub: want non-empty")
	}

	// Audit must show a federation_oidc register with reason=invite_only_redemption.
	var found bool
	for _, ev := range h.q.events {
		if ev.Factor != string(audit.FactorFederationOIDC) {
			continue
		}
		if ev.Event != audit.EventRegister {
			continue
		}
		var detail map[string]any
		if err := json.Unmarshal(ev.Detail, &detail); err != nil {
			t.Fatalf("decode audit detail: %v", err)
		}
		if detail["reason"] == "invite_only_redemption" {
			found = true
			break
		}
	}
	if !found {
		t.Error("no audit register row with reason=invite_only_redemption")
	}
}
