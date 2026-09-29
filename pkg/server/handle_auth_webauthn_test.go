// Package server — handle_auth_webauthn_test.go
//
// Unit tests for the WebAuthn login handler's audit emissions (Task 4) and
// its ?mediation= handling (PHB-98). Only the branches that return before
// calling s.webauthn.FinishPasskeyLogin are covered here; the
// FinishPasskeyLogin, unknown_credential, account_disabled, clone_warning, and success paths require a
// real WebAuthn ceremony (CBOR-encoded authenticator data + signatures) and
// are covered by the final smoke test (Task 14).

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/db"
	sessstore "prohibitorum/pkg/session"
	"prohibitorum/pkg/weberr"
)

// newWebAuthnTestServer builds the minimum Server needed to exercise the
// ceremony-failure branches of handleLoginCompleteHTTP. The webauthn field
// is left nil because those branches return before touching it.
func newWebAuthnTestServer(t *testing.T) (*Server, *fakeAuthQueries) {
	t.Helper()
	s, f, _ := newTestServer(t)
	return s, f
}

// postWebAuthnComplete fires POST /auth/webauthn/login/complete with the given
// cookie and body, returning the response recorder.
func postWebAuthnComplete(t *testing.T, s *Server, cerCookieValue string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	u := &url.URL{Path: "/auth/webauthn/login/complete"}
	req := httptest.NewRequest(http.MethodPost, u.String(), nil)
	if cerCookieValue != "" {
		req.AddCookie(&http.Cookie{Name: "prohibitorum_ceremony", Value: cerCookieValue})
	}
	req.Body = http.NoBody
	_ = body // ceremony body not consumed by early-exit branches
	s.handleLoginCompleteHTTP(rec, req)
	return rec
}

// TestWebAuthnLoginAudit_CeremonyMissing verifies that a request with a
// ceremony cookie whose key is absent from the KV store emits a
// webauthn|fail record with reason=ceremony_missing.
func TestWebAuthnLoginAudit_CeremonyMissing(t *testing.T) {
	t.Parallel()
	s, f := newWebAuthnTestServer(t)

	// Use a ceremony cookie value that was never stashed → KV Pop returns ErrKeyNotFound.
	rec := postWebAuthnComplete(t, s, "no-such-key", nil)

	// ceremony_missing → ErrCeremonyExpired() → 400 Bad Request
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", rec.Code)
	}

	if len(f.events) != 1 {
		t.Fatalf("audit events: got %d, want 1", len(f.events))
	}
	ev := f.events[0]
	if ev.Factor != string(audit.FactorWebAuthn) {
		t.Errorf("Factor: got %q, want %q", ev.Factor, audit.FactorWebAuthn)
	}
	if ev.Event != audit.EventFail {
		t.Errorf("Event: got %q, want %q", ev.Event, audit.EventFail)
	}
	if ev.AccountID != nil {
		t.Errorf("AccountID: got %v, want nil", ev.AccountID)
	}
	var detail map[string]any
	if err := json.Unmarshal(ev.Detail, &detail); err != nil {
		t.Fatalf("detail unmarshal: %v", err)
	}
	if reason, _ := detail["reason"].(string); reason != "ceremony_missing" {
		t.Errorf("detail.reason: got %q, want %q", reason, "ceremony_missing")
	}
}

// TestWebAuthnLoginAudit_CeremonyCorrupt verifies that a KV entry containing
// invalid JSON (corrupt ceremony state) emits a webauthn|fail record with
// reason=ceremony_corrupt.
func TestWebAuthnLoginAudit_CeremonyCorrupt(t *testing.T) {
	t.Parallel()
	s, f := newWebAuthnTestServer(t)

	// Stash a corrupt (non-JSON) ceremony state so Pop succeeds but Unmarshal fails.
	stashCtx := httptest.NewRequest(http.MethodPost, "/", nil).Context()
	if err := s.kvStore.SetEx(
		stashCtx,
		"webauthn_ceremony:login:corrupt-key",
		"not-valid-json",
		ceremonyTTL,
	); err != nil {
		t.Fatalf("kvStore.SetEx: %v", err)
	}

	rec := postWebAuthnComplete(t, s, "corrupt-key", nil)

	// ceremony_corrupt → ErrCeremonyState() → 500 Internal Server Error
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500", rec.Code)
	}

	if len(f.events) != 1 {
		t.Fatalf("audit events: got %d, want 1", len(f.events))
	}
	ev := f.events[0]
	if ev.Factor != string(audit.FactorWebAuthn) {
		t.Errorf("Factor: got %q, want %q", ev.Factor, audit.FactorWebAuthn)
	}
	if ev.Event != audit.EventFail {
		t.Errorf("Event: got %q, want %q", ev.Event, audit.EventFail)
	}
	if ev.AccountID != nil {
		t.Errorf("AccountID: got %v, want nil", ev.AccountID)
	}
	var detail map[string]any
	if err := json.Unmarshal(ev.Detail, &detail); err != nil {
		t.Fatalf("detail unmarshal: %v", err)
	}
	if reason, _ := detail["reason"].(string); reason != "ceremony_corrupt" {
		t.Errorf("detail.reason: got %q, want %q", reason, "ceremony_corrupt")
	}
}

// TestParseLoginMediation pins which ?mediation= values are accepted: an
// absent key is modal, exactly one "conditional" is conditional, and every
// other shape is a bad request rather than a silent fallback.
func TestParseLoginMediation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		query   string
		want    loginMediation
		wantErr bool
	}{
		{query: "", want: mediationModal},
		{query: "return_to=%2Fapps", want: mediationModal},
		{query: "mediation=conditional", want: mediationConditional},
		{query: "mediation=", wantErr: true},
		{query: "mediation", wantErr: true},
		{query: "mediation=Conditional", wantErr: true},
		{query: "mediation=modal", wantErr: true},
		{query: "mediation=conditional&mediation=conditional", wantErr: true},
	}
	for _, tc := range cases {
		r := httptest.NewRequest(http.MethodPost, "/api/prohibitorum/auth/login/begin?"+tc.query, nil)
		got, err := parseLoginMediation(r)
		if tc.wantErr {
			if ae := authn.AsAuthError(err); ae == nil || ae.Code != "bad_request" {
				t.Errorf("%q: err = %v, want bad_request", tc.query, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: unexpected err %v", tc.query, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%q: mediation = %v, want %v", tc.query, got, tc.want)
		}
	}
}

// loginCeremonyEntries returns the login ceremony stashes currently in KV.
func loginCeremonyEntries(t *testing.T, s *Server) map[string]string {
	t.Helper()
	res, err := s.kvStore.ScanEntries(context.Background(), "webauthn_ceremony:login:*", 0, 100)
	if err != nil {
		t.Fatalf("ScanEntries: %v", err)
	}
	out := make(map[string]string, len(res.Entries))
	for _, e := range res.Entries {
		out[e.Key] = e.Value
	}
	return out
}

// assertPublicErrorCode checks the response status and public-error code.
func assertPublicErrorCode(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, status, rec.Body.String())
	}
	var env weberr.PublicError
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal body: %v\nbody: %s", err, rec.Body.String())
	}
	if env.Code != code {
		t.Fatalf("code = %q, want %q", env.Code, code)
	}
}

// TestLoginBegin_InvalidMediationRejected proves a malformed ?mediation= is a
// 400 that neither sets a ceremony cookie nor stashes a ceremony.
func TestLoginBegin_InvalidMediationRejected(t *testing.T) {
	t.Parallel()
	for _, query := range []string{"mediation=", "mediation=bogus", "mediation=Conditional", "mediation=conditional&mediation=conditional"} {
		s := newLoginBeginTestServer(t)
		s.queries = db.New(bootstrappedDBTX{})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/prohibitorum/auth/login/begin?"+query, nil)
		s.handleLoginBeginHTTP(rec, req)

		assertPublicErrorCode(t, rec, http.StatusBadRequest, "bad_request")
		if got := rec.Result().Cookies(); len(got) != 0 {
			t.Errorf("%q: Set-Cookie = %v, want none", query, got)
		}
		if got := loginCeremonyEntries(t, s); len(got) != 0 {
			t.Errorf("%q: KV ceremonies = %v, want none", query, got)
		}
	}
}

// TestLoginBegin_MediationSelectsCookieAndLifetime proves each mediation sets
// its own ceremony cookie and lifetime: the button ceremony keeps the 60 s
// go-webauthn timeout, the autofill ceremony lives for ceremonyTTL, and the
// browser timeout, SessionData.Expires and cookie all agree.
func TestLoginBegin_MediationSelectsCookieAndLifetime(t *testing.T) {
	t.Parallel()
	cases := []struct {
		query       string
		cookieName  string
		wantTimeout int
		wantExpiry  time.Duration
	}{
		{query: "", cookieName: sessstore.CeremonyCookieName, wantTimeout: 60000, wantExpiry: time.Minute},
		{query: "?mediation=conditional", cookieName: sessstore.ConditionalCeremonyCookieName, wantTimeout: 300000, wantExpiry: ceremonyTTL},
	}
	for _, tc := range cases {
		s := newLoginBeginTestServer(t)
		s.queries = db.New(bootstrappedDBTX{})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/prohibitorum/auth/login/begin"+tc.query, nil)
		before := time.Now()
		s.handleLoginBeginHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("%q: status = %d, body: %s", tc.query, rec.Code, rec.Body.String())
		}
		var body struct {
			Timeout int `json:"timeout"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("%q: unmarshal: %v", tc.query, err)
		}
		if body.Timeout != tc.wantTimeout {
			t.Errorf("%q: timeout = %d, want %d", tc.query, body.Timeout, tc.wantTimeout)
		}

		cookies := rec.Result().Cookies()
		if len(cookies) != 1 || cookies[0].Name != tc.cookieName {
			t.Fatalf("%q: cookies = %v, want one %s", tc.query, cookies, tc.cookieName)
		}
		if cookies[0].MaxAge != 300 || cookies[0].Path != "/api/prohibitorum/auth" || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
			t.Errorf("%q: cookie attributes = %+v", tc.query, cookies[0])
		}

		raw, err := s.kvStore.Get(context.Background(), "webauthn_ceremony:login:"+cookies[0].Value)
		if err != nil {
			t.Fatalf("%q: ceremony not stashed under the cookie token: %v", tc.query, err)
		}
		var sd webauthn.SessionData
		if err := json.Unmarshal([]byte(raw), &sd); err != nil {
			t.Fatalf("%q: unmarshal SessionData: %v", tc.query, err)
		}
		if lo, hi := before.Add(tc.wantExpiry-time.Second), time.Now().Add(tc.wantExpiry+time.Second); sd.Expires.Before(lo) || sd.Expires.After(hi) {
			t.Errorf("%q: Expires = %v, want about %v from now", tc.query, sd.Expires, tc.wantExpiry)
		}
	}
}

// stashLoginCeremony stores a discoverable SessionData under token.
func stashLoginCeremony(t *testing.T, s *Server, token string) {
	t.Helper()
	payload, err := json.Marshal(webauthn.SessionData{Challenge: "c", Expires: time.Now().Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.kvStore.SetEx(context.Background(), "webauthn_ceremony:login:"+token, string(payload), ceremonyTTL); err != nil {
		t.Fatalf("SetEx: %v", err)
	}
}

// TestLoginComplete_MediationReadsOwnCookie proves /complete reads only the
// cookie of the requested mediation: a pending ceremony under the other
// mediation's cookie is ceremony_missing and is left in KV untouched.
func TestLoginComplete_MediationReadsOwnCookie(t *testing.T) {
	t.Parallel()
	cases := []struct {
		query      string
		cookieName string
	}{
		{query: "?mediation=conditional", cookieName: sessstore.CeremonyCookieName},
		{query: "", cookieName: sessstore.ConditionalCeremonyCookieName},
	}
	for _, tc := range cases {
		s, _ := newWebAuthnTestServer(t)
		stashLoginCeremony(t, s, "pending")
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/prohibitorum/auth/login/complete"+tc.query, nil)
		req.AddCookie(&http.Cookie{Name: tc.cookieName, Value: "pending"})
		s.handleLoginCompleteHTTP(rec, req)

		assertPublicErrorCode(t, rec, http.StatusBadRequest, "ceremony_missing")
		if got := loginCeremonyEntries(t, s); len(got) != 1 {
			t.Errorf("%q with %s: ceremony consumed, KV = %v", tc.query, tc.cookieName, got)
		}
	}
}

// TestLoginComplete_ConditionalCookieReachesCeremony proves the conditional
// cookie is the one /complete?mediation=conditional claims: the stash is
// popped (the corrupt payload then fails as ceremony state, which is enough to
// show the lookup went through that cookie).
func TestLoginComplete_ConditionalCookieReachesCeremony(t *testing.T) {
	t.Parallel()
	s, _ := newWebAuthnTestServer(t)
	if err := s.kvStore.SetEx(context.Background(), "webauthn_ceremony:login:auto", "not-json", ceremonyTTL); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/prohibitorum/auth/login/complete?mediation=conditional", nil)
	req.AddCookie(&http.Cookie{Name: sessstore.ConditionalCeremonyCookieName, Value: "auto"})
	s.handleLoginCompleteHTTP(rec, req)

	assertPublicErrorCode(t, rec, http.StatusInternalServerError, "ceremony_state_invalid")
	if got := loginCeremonyEntries(t, s); len(got) != 0 {
		t.Errorf("ceremony not consumed, KV = %v", got)
	}
}

// TestLoginComplete_InvalidMediationKeepsCeremony proves a malformed
// ?mediation= on /complete is a 400 that consumes no ceremony.
func TestLoginComplete_InvalidMediationKeepsCeremony(t *testing.T) {
	t.Parallel()
	s, f := newWebAuthnTestServer(t)
	stashLoginCeremony(t, s, "pending")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/prohibitorum/auth/login/complete?mediation=bogus", nil)
	req.AddCookie(&http.Cookie{Name: sessstore.CeremonyCookieName, Value: "pending"})
	req.AddCookie(&http.Cookie{Name: sessstore.ConditionalCeremonyCookieName, Value: "pending"})
	s.handleLoginCompleteHTTP(rec, req)

	assertPublicErrorCode(t, rec, http.StatusBadRequest, "bad_request")
	if got := loginCeremonyEntries(t, s); len(got) != 1 {
		t.Errorf("ceremony consumed, KV = %v", got)
	}
	if len(f.events) != 0 {
		t.Errorf("audit events = %d, want 0", len(f.events))
	}
}
