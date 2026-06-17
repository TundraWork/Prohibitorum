// Package server — handle_federation_confirm_test.go
//
// Handler-level tests for the /welcome federated-identity confirmation step
// (Task 6). These reuse the fedTestHarness from handle_federation_test.go (real
// Federator + memory KV + fakeFedQueries), which is also wired with the three
// confirm routes and s.confirmFedOverride.
//
// Two surfaces are covered:
//
//  1. The callback BRANCH: a first-time (unconfirmed) federated sign-in must
//     WITHHOLD the durable session and 302 to /welcome with a fed-state cookie.
//  2. The confirm ENDPOINTS: GET peeks the pending identity, POST single-use-
//     consumes the grant + issues a session, decline pops the grant. Every
//     bad/absent/replayed grant collapses onto federation_state_invalid (401).
package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
	fedoidc "prohibitorum/pkg/federation/oidc"
	sessstore "prohibitorum/pkg/session"
)

// --- callback branch: unconfirmed → /welcome, no session -------------------

// TestFederationCallback_UnconfirmedRedirectsToWelcome guards the Task 6 core:
// a fresh auto-provision (Confirmed=false) must NOT mint a session cookie and
// must 302 to /welcome with a fed-state (confirmation-grant) cookie.
func TestFederationCallback_UnconfirmedRedirectsToWelcome(t *testing.T) {
	h := newFederationTestServer(t)
	// No seedConfirmedIdentity → auto_provision yields a new, UNCONFIRMED
	// identity (Confirmed=false).

	loc, resp := h.driveLogin(t, "mockop", "/me")
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("login: want 302, got %d", resp.StatusCode)
	}
	code, state, iss := driveAuthorize(t, loc)
	q := url.Values{}
	q.Set("code", code)
	q.Set("state", state)
	q.Set("iss", iss)
	resp = h.hitCallback(t, "mockop", q)

	if resp.StatusCode != http.StatusFound {
		body, _ := readAll(resp.Body)
		t.Fatalf("callback: want 302, got %d (body=%s)", resp.StatusCode, body)
	}
	if got := resp.Header.Get("Location"); got != "/welcome" {
		t.Errorf("Location: want /welcome, got %q", got)
	}

	// NO session cookie may be set on the unconfirmed path.
	for _, c := range resp.Cookies() {
		if c.Name == sessstore.SessionCookieName && c.Value != "" {
			t.Fatalf("session cookie set on unconfirmed path: %q", c.Value)
		}
	}
	// A fed-state (confirmation grant) cookie MUST be set, carrying token.anti.
	var fedCookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == sessstore.FedStateCookieName {
			fedCookie = c
			break
		}
	}
	if fedCookie == nil || fedCookie.Value == "" {
		t.Fatal("fed-state confirmation cookie not set on unconfirmed path")
	}
	if !strings.Contains(fedCookie.Value, ".") {
		t.Errorf("fed-state cookie should be token.anti, got %q", fedCookie.Value)
	}
	// The grant must be readable from KV under ConfirmKey(token).
	tok, _ := splitConfirmCookie(fedCookie.Value)
	if _, err := h.s.kvStore.Get(context.Background(), fedoidc.ConfirmKey(tok)); err != nil {
		t.Errorf("grant not stashed under ConfirmKey: %v", err)
	}
	// No session row was inserted.
	if len(h.q.sessions) != 0 {
		t.Errorf("sessions inserted: want 0 (unconfirmed), got %d", len(h.q.sessions))
	}
}

// --- confirm endpoints ------------------------------------------------------

// confirmFixture seeds an account + IdP on the fake, mints a real confirmation
// grant via the Federator, and returns the fed-state cookie value the confirm
// endpoints expect (token.anti).
type confirmFixture struct {
	h          *fedTestHarness
	accountID  int32
	identityID int64
	cookie     string
}

func seedConfirmGrant(t *testing.T, h *fedTestHarness) confirmFixture {
	t.Helper()
	const accountID int32 = 321
	const identityID int64 = 654
	h.q.accountByIDResults[accountID] = db.Account{
		ID:          accountID,
		Username:    "newbie",
		DisplayName: "New Bie",
		Email:       pgtype.Text{String: "newbie@example.com", Valid: true},
	}
	// h.idp is already seeded under slug "mockop" by the harness.

	token, anti, err := h.s.federator.CreateConfirmGrant(
		context.Background(), accountID, identityID, h.idp.ID, h.idp.Slug, "/me", nil,
	)
	if err != nil {
		t.Fatalf("CreateConfirmGrant: %v", err)
	}
	return confirmFixture{
		h:          h,
		accountID:  accountID,
		identityID: identityID,
		cookie:     token + "." + anti,
	}
}

func doConfirm(t *testing.T, fx confirmFixture, method, path, cookie string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, fx.h.srvTS.URL+path, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: sessstore.FedStateCookieName, Value: cookie})
	}
	resp, err := noFollow().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

const confirmPath = "/api/prohibitorum/auth/federation/confirm"
const confirmDeclinePath = "/api/prohibitorum/auth/federation/confirm/decline"

func TestFederationConfirmGet_ValidGrant(t *testing.T) {
	h := newFederationTestServer(t)
	fx := seedConfirmGrant(t, h)
	// Mark the avatar fetch in flight so avatarPending must be true.
	if err := h.s.kvStore.SetEx(context.Background(), fedoidc.AvatarFetchKey(fx.accountID, 1), "1", time.Minute); err != nil {
		t.Fatalf("seed avatar key: %v", err)
	}

	resp := doConfirm(t, fx, http.MethodGet, confirmPath, fx.cookie)
	if resp.StatusCode != http.StatusOK {
		body, _ := readAll(resp.Body)
		t.Fatalf("GET confirm: want 200, got %d (body=%s)", resp.StatusCode, body)
	}
	var view contract.FederationConfirmView
	if err := json.NewDecoder(resp.Body).Decode(&view); err != nil {
		t.Fatalf("decode view: %v", err)
	}
	if view.IDPDisplayName != h.idp.DisplayName {
		t.Errorf("idpDisplayName: want %q, got %q", h.idp.DisplayName, view.IDPDisplayName)
	}
	if view.DisplayName != "New Bie" {
		t.Errorf("displayName: want %q, got %q", "New Bie", view.DisplayName)
	}
	if view.Username != "newbie" {
		t.Errorf("username: want %q, got %q", "newbie", view.Username)
	}
	if view.Email != "newbie@example.com" {
		t.Errorf("email: want %q, got %q", "newbie@example.com", view.Email)
	}
	if !view.AvatarPending {
		t.Error("avatarPending: want true (fetch key present)")
	}
}

func TestFederationConfirmGet_NoCookie(t *testing.T) {
	h := newFederationTestServer(t)
	fx := seedConfirmGrant(t, h)

	resp := doConfirm(t, fx, http.MethodGet, confirmPath, "")
	assertStateInvalid(t, resp)
}

func TestFederationConfirmGet_BadAntiForgery(t *testing.T) {
	h := newFederationTestServer(t)
	fx := seedConfirmGrant(t, h)
	tok, _ := splitConfirmCookie(fx.cookie)

	resp := doConfirm(t, fx, http.MethodGet, confirmPath, tok+".not-the-real-anti-forgery")
	assertStateInvalid(t, resp)
}

func TestFederationConfirmPost_ConfirmsAndIssuesSession(t *testing.T) {
	h := newFederationTestServer(t)
	fx := seedConfirmGrant(t, h)

	resp := doConfirm(t, fx, http.MethodPost, confirmPath, fx.cookie)
	if resp.StatusCode != http.StatusOK {
		body, _ := readAll(resp.Body)
		t.Fatalf("POST confirm: want 200, got %d (body=%s)", resp.StatusCode, body)
	}

	// confirmed_at stamped via ConfirmAccountIdentity(identityID).
	if h.q.confirmedIdentityID != fx.identityID {
		t.Errorf("ConfirmAccountIdentity: want id %d, got %d", fx.identityID, h.q.confirmedIdentityID)
	}
	// Session cookie issued.
	var sessCookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == sessstore.SessionCookieName {
			sessCookie = c
			break
		}
	}
	if sessCookie == nil || sessCookie.Value == "" {
		t.Fatal("session cookie not set on confirm")
	}
	if len(h.q.sessions) != 1 {
		t.Errorf("sessions inserted: want 1, got %d", len(h.q.sessions))
	}
	// Fed-state cookie must be cleared (MaxAge<0 or empty value): a second POST
	// with the same grant must not re-use a stale fed-state cookie to succeed.
	for _, c := range resp.Cookies() {
		if c.Name == sessstore.FedStateCookieName {
			if c.Value != "" && c.MaxAge >= 0 {
				t.Errorf("fed-state cookie not cleared on confirm: value=%q MaxAge=%d", c.Value, c.MaxAge)
			}
			break
		}
	}
	// Body carries the return-to redirect.
	var out map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode confirm body: %v", err)
	}
	if out["redirect"] != "/me" {
		t.Errorf("redirect: want /me, got %q", out["redirect"])
	}

	// Single-use: a SECOND POST with the same (popped) grant must 401.
	resp2 := doConfirm(t, fx, http.MethodPost, confirmPath, fx.cookie)
	assertStateInvalid(t, resp2)
}

func TestFederationConfirmDecline_PopsGrant(t *testing.T) {
	h := newFederationTestServer(t)
	fx := seedConfirmGrant(t, h)

	resp := doConfirm(t, fx, http.MethodPost, confirmDeclinePath, fx.cookie)
	if resp.StatusCode != http.StatusNoContent {
		body, _ := readAll(resp.Body)
		t.Fatalf("decline: want 204, got %d (body=%s)", resp.StatusCode, body)
	}
	// No session, no confirmation.
	if h.q.confirmedIdentityID != 0 {
		t.Errorf("decline must not confirm; got confirmed id %d", h.q.confirmedIdentityID)
	}
	if len(h.q.sessions) != 0 {
		t.Errorf("decline must not issue a session; got %d", len(h.q.sessions))
	}
	// The grant is now popped: a subsequent GET (and POST) must 401.
	getResp := doConfirm(t, fx, http.MethodGet, confirmPath, fx.cookie)
	assertStateInvalid(t, getResp)
	postResp := doConfirm(t, fx, http.MethodPost, confirmPath, fx.cookie)
	assertStateInvalid(t, postResp)
}

// --- helpers ----------------------------------------------------------------

func assertStateInvalid(t *testing.T, resp *http.Response) {
	t.Helper()
	if resp.StatusCode != http.StatusUnauthorized {
		body, _ := readAll(resp.Body)
		t.Fatalf("status: want 401, got %d (body=%s)", resp.StatusCode, body)
	}
	body := decodeErrBody(t, resp)
	if body.Code != "federation_state_invalid" {
		t.Errorf("code: want federation_state_invalid, got %q", body.Code)
	}
}
