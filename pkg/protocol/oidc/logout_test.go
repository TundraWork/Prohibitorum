package oidc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"prohibitorum/pkg/db"
	"prohibitorum/pkg/session"
)

// fakeSessionQueries is a no-op SessionQueries: the KV side of SessionStore is
// what logout exercises (ListByAccount + Del), and the PG metadata insert /
// revoke are best-effort. InsertSession returns a zero row; RevokeSession is a
// no-op. This lets us build a real SessionStore over the harness MemoryStore.
type fakeSessionQueries struct{}

func (fakeSessionQueries) InsertSession(context.Context, db.InsertSessionParams) (db.Session, error) {
	return db.Session{}, nil
}
func (fakeSessionQueries) RevokeSession(context.Context, string) error            { return nil }
func (fakeSessionQueries) RevokeAllSessionsByAccount(context.Context, int32) error { return nil }

// withSessions attaches a real SessionStore (over the harness's in-memory KV)
// to the Provider, then issues a session for the harness account and returns
// its session ID so a test can mint an id_token_hint carrying that sid.
func (h *endpointHarness) withSessions(t *testing.T) string {
	t.Helper()
	store := session.NewSessionStore(h.p.kv, fakeSessionQueries{}, time.Hour)
	h.p.sessions = store
	_, data, err := store.Issue(context.Background(), 7, "127.0.0.1", "test-agent", []string{"pwd"}, nil)
	if err != nil {
		t.Fatalf("issue session: %v", err)
	}
	return data.SessionID
}

// sessionLive reports whether a session with sid still exists for account 7.
func (h *endpointHarness) sessionLive(t *testing.T, sid string) bool {
	t.Helper()
	sessions, err := h.p.sessions.ListByAccount(context.Background(), 7)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	for _, sr := range sessions {
		if sr.Data.SessionID == sid {
			return true
		}
	}
	return false
}

// mintHint signs an id_token_hint (typ JWT) for the harness subject/client with
// the given sid and expiry.
func (h *endpointHarness) mintHint(t *testing.T, sid string, exp time.Time) string {
	t.Helper()
	tok, err := h.p.signJWT(context.Background(), map[string]any{
		"iss": testIssuer,
		"sub": testSubject,
		"sid": sid,
		"aud": testClientID,
		"exp": exp.Unix(),
		"iat": time.Now().Add(-time.Hour).Unix(),
	}, "JWT")
	if err != nil {
		t.Fatalf("mint hint: %v", err)
	}
	return tok
}

func logoutReq(query url.Values) *http.Request {
	return httptest.NewRequest(http.MethodGet, "/oidc/logout?"+query.Encode(), nil)
}

const testPostLogout = "https://rp.example.com/loggedout"

// registerPostLogout adds testPostLogout to the harness client's allowlist.
func (h *endpointHarness) registerPostLogout(t *testing.T) {
	t.Helper()
	c := h.q.clients[testClientID]
	c.PostLogoutRedirectUris = []string{testPostLogout}
	h.q.clients[testClientID] = c
}

func TestLogoutValidHintRevokesAndRedirects(t *testing.T) {
	h := newEndpointHarness(t)
	sid := h.withSessions(t)
	h.registerPostLogout(t)
	if !h.sessionLive(t, sid) {
		t.Fatal("precondition: session should be live before logout")
	}

	hint := h.mintHint(t, sid, time.Now().Add(time.Hour))
	q := url.Values{}
	q.Set("id_token_hint", hint)
	q.Set("post_logout_redirect_uri", testPostLogout)
	q.Set("state", "xyz")

	rec := httptest.NewRecorder()
	h.p.HandleLogout(rec, logoutReq(q))

	if rec.Code != http.StatusFound {
		t.Fatalf("want 302, got %d (%s)", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("parse Location %q: %v", loc, err)
	}
	if u.Scheme+"://"+u.Host+u.Path != testPostLogout {
		t.Fatalf("redirect base = %q, want %q", u.Scheme+"://"+u.Host+u.Path, testPostLogout)
	}
	if u.Query().Get("state") != "xyz" {
		t.Fatalf("state not echoed: %q", loc)
	}
	if h.sessionLive(t, sid) {
		t.Fatal("session should have been revoked")
	}

	// logout audited under oidc_client / use.
	var found bool
	for _, rrec := range h.audit.records {
		if rrec.Detail["reason"] == "logout" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected a logout audit record")
	}
}

func TestLogoutExpiredHintStillWorks(t *testing.T) {
	h := newEndpointHarness(t)
	sid := h.withSessions(t)
	h.registerPostLogout(t)

	// exp in the past — signature is still valid, which is all logout needs.
	hint := h.mintHint(t, sid, time.Now().Add(-time.Hour))
	q := url.Values{}
	q.Set("id_token_hint", hint)
	q.Set("post_logout_redirect_uri", testPostLogout)

	rec := httptest.NewRecorder()
	h.p.HandleLogout(rec, logoutReq(q))

	if rec.Code != http.StatusFound {
		t.Fatalf("expired-but-valid hint must work; got %d (%s)", rec.Code, rec.Body.String())
	}
	if h.sessionLive(t, sid) {
		t.Fatal("session should have been revoked even with an expired hint")
	}
}

func TestLogoutUnregisteredPostLogoutIsDirectError(t *testing.T) {
	h := newEndpointHarness(t)
	sid := h.withSessions(t)
	h.registerPostLogout(t)

	hint := h.mintHint(t, sid, time.Now().Add(time.Hour))
	q := url.Values{}
	q.Set("id_token_hint", hint)
	q.Set("post_logout_redirect_uri", "https://evil.example.com/steal")

	rec := httptest.NewRecorder()
	h.p.HandleLogout(rec, logoutReq(q))

	// Unregistered post_logout_redirect_uri → /error page redirect, NOT to the evil URI.
	if rec.Code != http.StatusFound {
		t.Fatalf("unregistered post_logout_redirect_uri must redirect to /error (302), got %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/error?error=invalid_request&ref=") {
		t.Fatalf("must redirect to /error; got Location %q", loc)
	}
	if strings.Contains(loc, "evil.example.com") {
		t.Fatalf("must not redirect to evil URI; got Location %q", loc)
	}
}

func TestLogoutMissingHintRedirectsToDefault(t *testing.T) {
	h := newEndpointHarness(t)
	rec := httptest.NewRecorder()
	h.p.HandleLogout(rec, logoutReq(url.Values{}))

	if rec.Code != http.StatusFound {
		t.Fatalf("missing hint should redirect to default landing, got %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != testIssuer {
		t.Fatalf("default landing = %q, want %q", loc, testIssuer)
	}
}

func TestLogoutPostLogoutWithoutHintIsError(t *testing.T) {
	h := newEndpointHarness(t)
	q := url.Values{}
	q.Set("post_logout_redirect_uri", testPostLogout)

	rec := httptest.NewRecorder()
	h.p.HandleLogout(rec, logoutReq(q))

	// Without a hint we can't validate the URI — redirect to /error.
	if rec.Code != http.StatusFound {
		t.Fatalf("post_logout_redirect_uri without a hint must redirect to /error (302), got %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/error?error=invalid_request&ref=") {
		t.Fatalf("must redirect to /error; got Location %q", loc)
	}
}

func TestLogoutInvalidSignatureRejected(t *testing.T) {
	h := newEndpointHarness(t)
	sid := h.withSessions(t)
	hint := h.mintHint(t, sid, time.Now().Add(time.Hour))
	// Tamper a character in the middle of the token. A segment's final
	// base64url character encodes only 2 meaningful bits (the rest are
	// ignored padding), so flipping it can decode to identical bytes.
	// Mid-token positions are full 6-bit characters, so the change is
	// guaranteed to alter the signed bytes and force verification failure.
	b := []byte(hint)
	i := len(b) / 2
	for b[i] == '.' {
		i++
	}
	if b[i] == 'A' {
		b[i] = 'B'
	} else {
		b[i] = 'A'
	}
	tampered := string(b)

	q := url.Values{}
	q.Set("id_token_hint", tampered)

	rec := httptest.NewRecorder()
	h.p.HandleLogout(rec, logoutReq(q))

	if rec.Code != http.StatusFound {
		t.Fatalf("tampered hint must redirect to /error (302), got %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/error?error=invalid_request&ref=") {
		t.Fatalf("tampered hint must redirect to /error; got Location %q", loc)
	}
	if !h.sessionLive(t, sid) {
		t.Fatal("session must NOT be revoked on an invalid hint")
	}
}

func TestLogoutWrongIssuerRejected(t *testing.T) {
	h := newEndpointHarness(t)
	sid := h.withSessions(t)
	tok, err := h.p.signJWT(context.Background(), map[string]any{
		"iss": "https://someone-else.example.com",
		"sub": testSubject,
		"sid": sid,
		"aud": testClientID,
		"exp": time.Now().Add(time.Hour).Unix(),
	}, "JWT")
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	q := url.Values{}
	q.Set("id_token_hint", tok)

	rec := httptest.NewRecorder()
	h.p.HandleLogout(rec, logoutReq(q))

	if rec.Code != http.StatusFound {
		t.Fatalf("hint with wrong iss must redirect to /error (302), got %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/error?error=invalid_request&ref=") {
		t.Fatalf("wrong iss must redirect to /error; got Location %q", loc)
	}
	if !h.sessionLive(t, sid) {
		t.Fatal("session must NOT be revoked when iss mismatches")
	}
}
