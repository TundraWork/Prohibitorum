package vrchat

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func testClient(t *testing.T, fn roundTripFunc) *Client {
	t.Helper()
	base, err := url.Parse("https://api.vrchat.cloud/api/1")
	if err != nil {
		t.Fatal(err)
	}
	client, err := newClient("(devel)", "https://example.test", originConfig{BaseURL: base, Transport: fn})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func response(status int, body string, headers ...http.Header) *http.Response {
	h := make(http.Header)
	if len(headers) != 0 {
		h = headers[0]
	}
	return &http.Response{StatusCode: status, Header: h, Body: io.NopCloser(strings.NewReader(body))}
}

func TestClientAuthenticateAuthorizationAndUserAgent(t *testing.T) {
	client := testClient(t, func(r *http.Request) (*http.Response, error) {
		if got, want := r.URL.String(), "https://api.vrchat.cloud/api/1/auth/user"; got != want {
			t.Fatalf("URL = %q, want %q", got, want)
		}
		wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte(url.QueryEscape("a+b@example.test")+":"+url.QueryEscape("p@ss: word")))
		if got := r.Header.Get("Authorization"); got != wantAuth {
			t.Fatalf("Authorization = %q, want %q", got, wantAuth)
		}
		if got, want := r.Header.Get("User-Agent"), "Prohibitorum/dev https://example.test"; got != want {
			t.Fatalf("User-Agent = %q, want %q", got, want)
		}
		return response(200, `{"id":"usr_12345678-1234-1234-1234-123456789abc","displayName":"Name"}`), nil
	})
	user, cookies, err := client.Authenticate(context.Background(), "a+b@example.test", "p@ss: word")
	if err != nil {
		t.Fatal(err)
	}
	if user.ID == "" || user.DisplayName != "Name" || len(user.RequiresTwoFactorAuth) != 0 || len(cookies) != 0 {
		t.Fatalf("unexpected result: %#v %#v", user, cookies)
	}
}

func TestClientEveryRequestHasUserAgentAndFixedPaths(t *testing.T) {
	var paths []string
	client := testClient(t, func(r *http.Request) (*http.Response, error) {
		if got := r.Header.Get("User-Agent"); got != "Prohibitorum/dev https://example.test" {
			t.Errorf("User-Agent = %q", got)
		}
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/api/1/auth/user":
			return response(200, `{"id":"usr_12345678-1234-1234-1234-123456789abc","displayName":"Name"}`), nil
		case "/api/1/users/usr_12345678-1234-1234-1234-123456789abc":
			return response(200, `{"id":"usr_12345678-1234-1234-1234-123456789abc","displayName":"Name","bioLinks":[],"currentAvatarThumbnailImageUrl":"https://example.test/a.png"}`), nil
		default:
			return response(200, `{"verified":true}`), nil
		}
	})
	ctx := context.Background()
	_, _, _ = client.CurrentUser(ctx, nil)
	_, _, _ = client.PublicUser(ctx, "usr_12345678-1234-1234-1234-123456789abc", nil)
	for _, method := range []string{"totp", "emailOtp", "otp"} {
		if _, err := client.VerifyTwoFactor(ctx, method, "123456", nil); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"/api/1/auth/user", "/api/1/users/usr_12345678-1234-1234-1234-123456789abc", "/api/1/auth/twofactorauth/totp/verify", "/api/1/auth/twofactorauth/emailotp/verify", "/api/1/auth/twofactorauth/otp/verify"}
	if strings.Join(paths, "|") != strings.Join(want, "|") {
		t.Fatalf("paths = %#v, want %#v", paths, want)
	}
}

func TestClientRefusesRedirects(t *testing.T) {
	followed := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/followed" {
			followed = true
			w.WriteHeader(200)
			return
		}
		http.Redirect(w, r, "/followed", http.StatusFound)
	}))
	defer server.Close()
	base, _ := url.Parse(server.URL + "/api/1")
	client, err := newClient("1.2.3", "https://public.test", originConfig{BaseURL: base, Transport: server.Client().Transport})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = client.CurrentUser(context.Background(), nil)
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) || httpErr.Status != http.StatusFound {
		t.Fatalf("error = %#v", err)
	}
	if followed {
		t.Fatal("redirect was followed")
	}
}

func TestClientCurrentUserUnionBoundaries(t *testing.T) {
	tests := []struct {
		name, body string
		ok         bool
		methods    []string
	}{
		{"authenticated absent", `{"id":"usr_12345678-1234-1234-1234-123456789abc","displayName":"Name"}`, true, nil},
		{"authenticated null", `{"id":"usr_12345678-1234-1234-1234-123456789abc","displayName":"Name","requiresTwoFactorAuth":null}`, true, nil},
		{"authenticated empty", `{"id":"usr_12345678-1234-1234-1234-123456789abc","displayName":"Name","requiresTwoFactorAuth":[]}`, true, []string{}},
		{"challenge", `{"requiresTwoFactorAuth":["totp","emailOtp","otp"]}`, true, []string{"totp", "emailOtp", "otp"}},
		{"partial id", `{"id":"usr_12345678-1234-1234-1234-123456789abc"}`, false, nil}, {"partial name", `{"displayName":"Name"}`, false, nil},
		{"mixed", `{"id":"usr_12345678-1234-1234-1234-123456789abc","displayName":"Name","requiresTwoFactorAuth":["totp"]}`, false, nil},
		{"missing", `{}`, false, nil}, {"null", `{"requiresTwoFactorAuth":null}`, false, nil}, {"empty", `{"requiresTwoFactorAuth":[]}`, false, nil},
		{"non string", `{"requiresTwoFactorAuth":[1]}`, false, nil}, {"unknown", `{"requiresTwoFactorAuth":["other"]}`, false, nil},
		{"empty user field", `{"id":"","displayName":"Name"}`, false, nil},
		{"invalid authenticated ID", `{"id":"usr_x","displayName":"Name"}`, false, nil},
		{"long display name", `{"id":"usr_12345678-1234-1234-1234-123456789abc","displayName":"` + strings.Repeat("x", 257) + `"}`, false, nil},
		{"too many methods", `{"requiresTwoFactorAuth":["totp","emailOtp","otp","totp"]}`, false, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := testClient(t, func(*http.Request) (*http.Response, error) { return response(200, tt.body), nil })
			user, _, err := client.CurrentUser(context.Background(), nil)
			if (err == nil) != tt.ok {
				t.Fatalf("error = %v", err)
			}
			if tt.ok && tt.methods != nil && strings.Join(user.RequiresTwoFactorAuth, ",") != strings.Join(tt.methods, ",") {
				t.Fatalf("methods = %#v", user.RequiresTwoFactorAuth)
			}
		})
	}
}

func TestClientPublicUserBoundariesAndIDValidation(t *testing.T) {
	id := "usr_12345678-1234-1234-1234-123456789abc"
	valid := `{"id":"` + id + `","displayName":"Name","bioLinks":[],"currentAvatarThumbnailImageUrl":"https://example.test/a.png"}`
	tests := []struct {
		name, requested, body string
		ok                    bool
	}{
		{"valid", id, valid, true},
		{"safe noncanonical segment", "usr_x", `{"id":"usr_x","displayName":"Name","bioLinks":[],"currentAvatarThumbnailImageUrl":"x"}`, true},
		{"missing id", id, `{"displayName":"Name","bioLinks":[],"currentAvatarThumbnailImageUrl":"x"}`, false},
		{"missing display", id, `{"id":"` + id + `","bioLinks":[],"currentAvatarThumbnailImageUrl":"x"}`, false},
		{"missing links", id, `{"id":"` + id + `","displayName":"Name","currentAvatarThumbnailImageUrl":"x"}`, false},
		{"null links", id, `{"id":"` + id + `","displayName":"Name","bioLinks":null,"currentAvatarThumbnailImageUrl":"x"}`, false},
		{"bad links", id, `{"id":"` + id + `","displayName":"Name","bioLinks":[1],"currentAvatarThumbnailImageUrl":"x"}`, false},
		{"missing avatar", id, `{"id":"` + id + `","displayName":"Name","bioLinks":[]}`, false},
		{"wrong id", id, `{"id":"usr_aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa","displayName":"Name","bioLinks":[],"currentAvatarThumbnailImageUrl":"x"}`, false},
		{"path escape", "../auth/user", valid, false}, {"encoded slash", "usr_a%2Fb", valid, false},
		{"long display", id, `{"id":"` + id + `","displayName":"` + strings.Repeat("x", 257) + `","bioLinks":[],"currentAvatarThumbnailImageUrl":"x"}`, false},
		{"too many links", id, `{"id":"` + id + `","displayName":"Name","bioLinks":["1","2","3","4","5","6","7","8","9","10","11","12","13","14","15","16","17"],"currentAvatarThumbnailImageUrl":"x"}`, false},
		{"long link", id, `{"id":"` + id + `","displayName":"Name","bioLinks":["` + strings.Repeat("x", 2049) + `"],"currentAvatarThumbnailImageUrl":"x"}`, false},
		{"long avatar", id, `{"id":"` + id + `","displayName":"Name","bioLinks":[],"currentAvatarThumbnailImageUrl":"` + strings.Repeat("x", 4097) + `"}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			client := testClient(t, func(*http.Request) (*http.Response, error) { called = true; return response(200, tt.body), nil })
			_, _, err := client.PublicUser(context.Background(), tt.requested, nil)
			if (err == nil) != tt.ok {
				t.Fatalf("error = %v", err)
			}
			if !tt.ok && (tt.name == "path escape" || tt.name == "encoded slash") && called {
				t.Fatal("invalid ID reached transport")
			}
		})
	}
}

func TestClientTwoFactorMethodsCodesAndVerification(t *testing.T) {
	for _, method := range []string{"totp", "emailOtp", "otp"} {
		t.Run(method, func(t *testing.T) {
			client := testClient(t, func(r *http.Request) (*http.Response, error) {
				if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
					t.Fatalf("request = %s %q", r.Method, r.Header.Get("Content-Type"))
				}
				body, _ := io.ReadAll(r.Body)
				if string(body) != `{"code":"secret"}` {
					t.Fatalf("body = %q", body)
				}
				return response(200, `{"verified":true,"ignored":"ok"}`), nil
			})
			if _, err := client.VerifyTwoFactor(context.Background(), method, "secret", nil); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, method := range []string{"", "TOTP", "other"} {
		client := testClient(t, func(*http.Request) (*http.Response, error) { t.Fatal("transport called"); return nil, nil })
		if _, err := client.VerifyTwoFactor(context.Background(), method, "x", nil); err == nil {
			t.Fatalf("method %q accepted", method)
		}
	}
	client := testClient(t, func(*http.Request) (*http.Response, error) { t.Fatal("transport called"); return nil, nil })
	if _, err := client.VerifyTwoFactor(context.Background(), "totp", strings.Repeat("x", 257), nil); err == nil {
		t.Fatal("oversized code accepted")
	}
	for _, body := range []string{`{"verified":false}`, `{"verified":null}`, `{}`} {
		client := testClient(t, func(*http.Request) (*http.Response, error) { return response(200, body), nil })
		_, err := client.VerifyTwoFactor(context.Background(), "totp", "x", nil)
		var verifyErr *VerificationError
		if !errors.As(err, &verifyErr) {
			t.Fatalf("body %q error = %#v", body, err)
		}
	}
}

func TestClientStatusDecodeAndBodyCapsAreSanitized(t *testing.T) {
	secret := "RAW_SECRET_NEVER_EXPOSE"
	tests := []struct {
		name    string
		status  int
		headers http.Header
		body    string
		target  any
	}{
		{"unauthorized", 401, nil, secret, &HTTPError{}}, {"forbidden", 403, nil, secret, &HTTPError{}},
		{"rate seconds", 429, http.Header{"Retry-After": []string{"120"}}, secret, &HTTPError{}},
		{"server", 503, nil, secret, &HTTPError{}}, {"unexpected", 418, nil, secret, &HTTPError{}},
		{"decode", 200, nil, "{" + secret, &DecodeError{}},
		{"oversize user", 200, nil, strings.Repeat("x", 1<<20+1) + secret, &OversizeError{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := testClient(t, func(*http.Request) (*http.Response, error) { return response(tt.status, tt.body, tt.headers), nil })
			_, _, err := client.CurrentUser(context.Background(), nil)
			if err == nil || !errors.As(err, &tt.target) {
				t.Fatalf("error = %#v", err)
			}
			if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "120") {
				t.Fatalf("error leaked data: %v", err)
			}
			if tt.name == "rate seconds" {
				var e *HTTPError
				errors.As(err, &e)
				if e.RetryAfter != 120*time.Second {
					t.Fatalf("RetryAfter = %v", e.RetryAfter)
				}
			}
		})
	}
	date := time.Now().Add(2 * time.Minute).UTC().Format(http.TimeFormat)
	client := testClient(t, func(*http.Request) (*http.Response, error) {
		return response(429, "", http.Header{"Retry-After": []string{date}}), nil
	})
	_, _, err := client.CurrentUser(context.Background(), nil)
	var e *HTTPError
	if !errors.As(err, &e) || e.RetryAfter < time.Minute || e.RetryAfter > 3*time.Minute {
		t.Fatalf("date RetryAfter = %v, err %v", e.RetryAfter, err)
	}
	client = testClient(t, func(*http.Request) (*http.Response, error) { return response(200, strings.Repeat("x", 4097)), nil })
	_, err = client.VerifyTwoFactor(context.Background(), "totp", "x", nil)
	var oe *OversizeError
	if !errors.As(err, &oe) {
		t.Fatalf("2FA oversize error = %#v", err)
	}
}

func TestClientTransportAndHeaderErrorsAreSanitized(t *testing.T) {
	secret := "RAW_SECRET_NEVER_EXPOSE"
	client := testClient(t, func(*http.Request) (*http.Response, error) {
		return nil, errors.New(secret)
	})
	_, _, err := client.Authenticate(context.Background(), secret, secret)
	var requestErr *RequestError
	if !errors.As(err, &requestErr) || strings.Contains(err.Error(), secret) {
		t.Fatalf("transport error = %v", err)
	}

	client = testClient(t, func(*http.Request) (*http.Response, error) {
		return response(200, `{}`, http.Header{"Set-Cookie": []string{"other=" + secret + "; Secure; HttpOnly; Path=/"}}), nil
	})
	_, _, err = client.CurrentUser(context.Background(), nil)
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("cookie error = %v", err)
	}
}

func TestClientTimeoutIsTenSeconds(t *testing.T) {
	client := testClient(t, func(*http.Request) (*http.Response, error) { return response(200, `{}`), nil })
	if client.httpClient.Timeout != 10*time.Second {
		t.Fatalf("timeout = %v", client.httpClient.Timeout)
	}
}
