package oidc

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// assertErrorPageRedirect asserts a 302 to /error?error=<wantCode>&ref=<non-empty>.
func assertErrorPageRedirect(t *testing.T, rec *httptest.ResponseRecorder, wantCode string) {
	t.Helper()
	if rec.Code != http.StatusFound {
		t.Fatalf("want 302, got %d (body: %s)", rec.Code, rec.Body.String())
	}
	loc := rec.Result().Header.Get("Location")
	prefix := fmt.Sprintf("/error?error=%s&ref=", url.QueryEscape(wantCode))
	if !strings.HasPrefix(loc, prefix) {
		t.Fatalf("Location = %q, want prefix %q", loc, prefix)
	}
	// ref must be non-empty (8 hex chars appended by weberr.NewRef)
	ref := strings.TrimPrefix(loc, prefix)
	if len(ref) == 0 {
		t.Fatalf("Location missing ref; got %q", loc)
	}
}

// TestAuthorize_UnknownClient_RedirectsToErrorPage: an unknown client_id
// (maps to errInvalidClient via pgx.ErrNoRows) must redirect to
// /error?error=invalid_request, not return JSON.
func TestAuthorize_UnknownClient_RedirectsToErrorPage(t *testing.T) {
	q := &fakeAuthzQueries{clientErr: pgx.ErrNoRows}
	p := newProvider(q, &recordingAudit{})

	rec := httptest.NewRecorder()
	p.HandleAuthorize(rec, authedReq(baseParams()))

	assertErrorPageRedirect(t, rec, errCodeInvalidRequest)
}

// TestAuthorize_TransientClientLoad_RedirectsToErrorPage: a non-ErrNoRows
// client-load error (transient DB failure) must redirect to
// /error?error=server_error.
func TestAuthorize_TransientClientLoad_RedirectsToErrorPage(t *testing.T) {
	q := &fakeAuthzQueries{clientErr: fmt.Errorf("db pool exhausted")}
	p := newProvider(q, &recordingAudit{})

	rec := httptest.NewRecorder()
	p.HandleAuthorize(rec, authedReq(baseParams()))

	assertErrorPageRedirect(t, rec, errCodeServerError)
}

// TestAuthorize_BadRedirectURI_RedirectsToErrorPage: a registered client but
// an unregistered redirect_uri must redirect to /error?error=invalid_request
// (still pre-validation; must NOT redirect to the untrusted URI).
func TestAuthorize_BadRedirectURI_RedirectsToErrorPage(t *testing.T) {
	q := &fakeAuthzQueries{client: validClient()}
	p := newProvider(q, &recordingAudit{})

	v := baseParams()
	v.Set("redirect_uri", "https://attacker.example.com/steal")
	rec := httptest.NewRecorder()
	p.HandleAuthorize(rec, authedReq(v))

	assertErrorPageRedirect(t, rec, errCodeInvalidRequest)
	// Confirm we never touch the attacker URI.
	if loc := rec.Result().Header.Get("Location"); strings.Contains(loc, "attacker.example.com") {
		t.Fatalf("must not redirect to attacker URI; got Location=%q", loc)
	}
}

// TestAuthorize_PostValidationError_StillRedirectsToRP: after client +
// redirect_uri are validated, errors must still redirect to the RP via
// redirectError — NOT to /error. response_type=token is the cleanest trigger.
func TestAuthorize_PostValidationError_StillRedirectsToRP(t *testing.T) {
	q := &fakeAuthzQueries{client: validClient(), session: validSession()}
	p := newProvider(q, &recordingAudit{})

	v := baseParams()
	v.Set("response_type", "token") // post-validation error
	rec := httptest.NewRecorder()
	p.HandleAuthorize(rec, authedReq(v))

	if rec.Code != http.StatusFound {
		t.Fatalf("want 302, got %d", rec.Code)
	}
	loc := rec.Result().Header.Get("Location")
	// Must redirect to the RP redirect_uri.
	rpBase := "https://rp.example.com/callback"
	if !strings.HasPrefix(loc, rpBase) {
		t.Fatalf("post-validation error must redirect to RP %q, got Location=%q", rpBase, loc)
	}
	// Must carry the OAuth error param.
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("parse Location %q: %v", loc, err)
	}
	if got := u.Query().Get("error"); got != errCodeUnsupportedResponseType {
		t.Fatalf("RP error param = %q, want %q", got, errCodeUnsupportedResponseType)
	}
	// Must NOT be an /error redirect.
	if strings.HasPrefix(loc, "/error") {
		t.Fatalf("post-validation error must NOT go to /error, got Location=%q", loc)
	}
}

// TestLogout_InvalidHint_RedirectsToErrorPage: a garbage id_token_hint (bad
// signature / not a JWT) must redirect to /error?error=invalid_request.
func TestLogout_InvalidHint_RedirectsToErrorPage(t *testing.T) {
	h := newEndpointHarness(t)
	h.withSessions(t)

	q := url.Values{}
	q.Set("id_token_hint", "garbage.not.a.jwt")

	rec := httptest.NewRecorder()
	h.p.HandleLogout(rec, logoutReq(q))

	assertErrorPageRedirect(t, rec, errCodeInvalidRequest)
}

// TestLogout_WrongIssuer_RedirectsToErrorPage: a hint signed by this provider
// but with a foreign iss claim must redirect to /error.
func TestLogout_WrongIssuer_RedirectsToErrorPage(t *testing.T) {
	h := newEndpointHarness(t)
	sid := h.withSessions(t)

	tok, err := h.p.signJWT(context.Background(), map[string]any{
		"iss": "https://other.example.com",
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

	assertErrorPageRedirect(t, rec, errCodeInvalidRequest)
}

// TestLogout_PostLogoutWithoutHint_RedirectsToErrorPage: a
// post_logout_redirect_uri presented without an id_token_hint must redirect
// to /error (cannot safely validate the URI without knowing the client).
func TestLogout_PostLogoutWithoutHint_RedirectsToErrorPage(t *testing.T) {
	h := newEndpointHarness(t)

	q := url.Values{}
	q.Set("post_logout_redirect_uri", "https://rp.example.com/loggedout")

	rec := httptest.NewRecorder()
	h.p.HandleLogout(rec, logoutReq(q))

	assertErrorPageRedirect(t, rec, errCodeInvalidRequest)
}

// TestLogout_UnregisteredPostLogout_RedirectsToErrorPage: a
// post_logout_redirect_uri not in the client allowlist must redirect to
// /error, NOT to the unregistered URI.
func TestLogout_UnregisteredPostLogout_RedirectsToErrorPage(t *testing.T) {
	h := newEndpointHarness(t)
	sid := h.withSessions(t)
	h.registerPostLogout(t)

	hint := h.mintHint(t, sid, time.Now().Add(time.Hour))
	q := url.Values{}
	q.Set("id_token_hint", hint)
	q.Set("post_logout_redirect_uri", "https://evil.example.com/steal")

	rec := httptest.NewRecorder()
	h.p.HandleLogout(rec, logoutReq(q))

	assertErrorPageRedirect(t, rec, errCodeInvalidRequest)
	if loc := rec.Result().Header.Get("Location"); strings.Contains(loc, "evil.example.com") {
		t.Fatalf("must not redirect to attacker URI; got Location=%q", loc)
	}
}

// TestLogout_ValidHint_SuccessPathUnchanged: the happy-path with a registered
// post_logout_redirect_uri must still redirect to the RP (regression guard).
func TestLogout_ValidHint_SuccessPathUnchanged(t *testing.T) {
	h := newEndpointHarness(t)
	sid := h.withSessions(t)
	h.registerPostLogout(t)

	hint := h.mintHint(t, sid, time.Now().Add(time.Hour))
	q := url.Values{}
	q.Set("id_token_hint", hint)
	q.Set("post_logout_redirect_uri", testPostLogout)
	q.Set("state", "abc")

	rec := httptest.NewRecorder()
	h.p.HandleLogout(rec, logoutReq(q))

	if rec.Code != http.StatusFound {
		t.Fatalf("success path want 302, got %d", rec.Code)
	}
	loc := rec.Result().Header.Get("Location")
	if !strings.HasPrefix(loc, testPostLogout) {
		t.Fatalf("success redirect must go to RP, got %q", loc)
	}
	u, _ := url.Parse(loc)
	if u.Query().Get("state") != "abc" {
		t.Fatalf("state not echoed; got Location=%q", loc)
	}
}

// TestLogout_NoHintNoPostLogout_DefaultRedirectUnchanged: with no hint and no
// post_logout_redirect_uri the handler must redirect to the issuer root
// (regression guard).
func TestLogout_NoHintNoPostLogout_DefaultRedirectUnchanged(t *testing.T) {
	h := newEndpointHarness(t)
	rec := httptest.NewRecorder()
	h.p.HandleLogout(rec, logoutReq(url.Values{}))

	if rec.Code != http.StatusFound {
		t.Fatalf("want 302, got %d", rec.Code)
	}
	if loc := rec.Result().Header.Get("Location"); loc != testIssuer {
		t.Fatalf("default landing = %q, want %q", loc, testIssuer)
	}
}
