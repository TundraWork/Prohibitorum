package main

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestMockOperatorChallengeAndSanitizedRecords(t *testing.T) {
	state := newMockState()
	state.fixture.Username = "operator+secret@example.test"
	state.fixture.Password = "distinct-password-secret"
	state.fixture.Code = "distinct-code-secret"
	state.fixture.RequireTwoFactor = true
	server := httptest.NewServer(state.routes())
	defer server.Close()

	auth := "Basic " + base64.StdEncoding.EncodeToString([]byte(url.QueryEscape(state.fixture.Username)+":"+url.QueryEscape(state.fixture.Password)))
	req, _ := http.NewRequest(http.MethodGet, server.URL+"/api/1/auth/user", nil)
	req.Header.Set("Authorization", auth)
	req.Header.Set("User-Agent", "Prohibitorum/test https://id.example")
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || len(response.Cookies()) != 1 || response.Cookies()[0].Name != "auth" {
		t.Fatalf("challenge response = %d cookies=%v", response.StatusCode, response.Cookies())
	}
	var challenge map[string]any
	if err := json.NewDecoder(response.Body).Decode(&challenge); err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if methods, ok := challenge["requiresTwoFactorAuth"].([]any); !ok || len(methods) != 1 || methods[0] != "totp" {
		t.Fatalf("challenge = %#v", challenge)
	}

	verifyReq, _ := http.NewRequest(http.MethodPost, server.URL+"/api/1/auth/twofactorauth/totp/verify", strings.NewReader(`{"code":"distinct-code-secret"}`))
	verifyReq.Header.Set("Content-Type", "application/json")
	verifyReq.Header.Set("User-Agent", "Prohibitorum/test https://id.example")
	for _, cookie := range response.Cookies() {
		verifyReq.AddCookie(cookie)
	}
	verified, err := http.DefaultClient.Do(verifyReq)
	if err != nil {
		t.Fatal(err)
	}
	if verified.StatusCode != http.StatusOK || len(verified.Cookies()) != 1 || verified.Cookies()[0].Name != "twoFactorAuth" {
		t.Fatalf("verify response = %d cookies=%v", verified.StatusCode, verified.Cookies())
	}
	_ = verified.Body.Close()

	records := state.requestRecords()
	encoded, _ := json.Marshal(records)
	for _, secret := range []string{state.fixture.Username, state.fixture.Password, state.fixture.Code, state.fixture.AuthCookieValue, state.fixture.TwoFactorCookieValue, auth} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("records leaked %q: %s", secret, encoded)
		}
	}
	if len(records) != 2 || records[0].Method != http.MethodGet || records[0].Path != "/api/1/auth/user" || len(records[0].CookieNames) != 0 || records[1].CookieNames[0] != "auth" {
		t.Fatalf("records = %#v", records)
	}
}

func TestMockControlMutatesPublicUserAndErrorModes(t *testing.T) {
	state := newMockState()
	server := httptest.NewServer(state.routes())
	defer server.Close()

	fixture := state.fixture
	fixture.PublicUserID = "usr_11111111-1111-1111-1111-111111111111"
	fixture.DisplayName = "Mutable Display"
	fixture.BioLinks = []string{"https://id.example/verify/vrchat/proof-marker"}
	fixture.PublicStatus = http.StatusTooManyRequests
	fixture.RetryAfter = "7"
	body, _ := json.Marshal(fixture)
	response, err := http.Post(server.URL+"/control/state", "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("control status = %d", response.StatusCode)
	}
	_ = response.Body.Close()

	public, err := http.Get(server.URL + "/api/1/users/" + fixture.PublicUserID)
	if err != nil {
		t.Fatal(err)
	}
	if public.StatusCode != http.StatusTooManyRequests || public.Header.Get("Retry-After") != "7" {
		t.Fatalf("public status=%d retry=%q", public.StatusCode, public.Header.Get("Retry-After"))
	}
	_ = public.Body.Close()

	fixture.PublicStatus = 0
	fixture.PublicBodyMode = "malformed"
	body, _ = json.Marshal(fixture)
	response, _ = http.Post(server.URL+"/control/state", "application/json", strings.NewReader(string(body)))
	_ = response.Body.Close()
	publicRequest, _ := http.NewRequest(http.MethodGet, server.URL+"/api/1/users/"+fixture.PublicUserID, nil)
	publicRequest.AddCookie(&http.Cookie{Name: "auth", Value: fixture.AuthCookieValue})
	public, _ = http.DefaultClient.Do(publicRequest)
	malformed, _ := io.ReadAll(public.Body)
	_ = public.Body.Close()
	if string(malformed) != "{" {
		t.Fatalf("malformed prefix = %q", malformed)
	}
}

func TestLoopbackProxyForwardsWithoutExternalDependency(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Forwarded-Proto") != "https" {
			t.Fatalf("X-Forwarded-Proto = %q", r.Header.Get("X-Forwarded-Proto"))
		}
		_, _ = io.WriteString(w, r.URL.Path)
	}))
	defer upstream.Close()
	proxy, err := newLoopbackProxy(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	proxy.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "https://127.0.0.1:18443/proof", nil))
	if recorder.Code != http.StatusOK || recorder.Body.String() != "/proof" {
		t.Fatalf("proxy = %d %q", recorder.Code, recorder.Body.String())
	}
	if _, err := newLoopbackProxy("https://example.com"); err == nil {
		t.Fatal("non-loopback proxy target accepted")
	}
}
