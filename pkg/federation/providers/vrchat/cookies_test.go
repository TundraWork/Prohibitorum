package vrchat

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func cookieTestOrigin(t *testing.T) *url.URL {
	t.Helper()
	origin, err := url.Parse("https://api.vrchat.cloud/api/1")
	if err != nil {
		t.Fatal(err)
	}
	return origin
}

func cookieTestHeader(lines ...string) http.Header {
	header := make(http.Header)
	for _, line := range lines {
		header.Add("Set-Cookie", line)
	}
	return header
}

func TestCookieValidateAcceptsHostOnlyAndExactDomain(t *testing.T) {
	now := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	expires := now.Add(time.Hour).Format(http.TimeFormat)

	cookies, err := validateResponseCookies(cookieTestOrigin(t), cookieTestHeader(
		"auth=host-secret; Path=/; Secure; HttpOnly; Expires="+expires,
		"twoFactorAuth=domain-secret; Path=/; Domain=API.VRCHAT.CLOUD; Secure; HttpOnly; SameSite=Strict; Expires="+expires,
	), now)
	if err != nil {
		t.Fatalf("validateResponseCookies() error = %v", err)
	}
	if len(cookies) != 2 {
		t.Fatalf("len(cookies) = %d, want 2", len(cookies))
	}
	if cookies[0].Name != "auth" || cookies[0].Domain != "" {
		t.Fatalf("host-only cookie = %#v", cookies[0])
	}
	if cookies[1].Name != "twoFactorAuth" || cookies[1].Domain != "" {
		t.Fatalf("domain cookie name/domain = %q/%q", cookies[1].Name, cookies[1].Domain)
	}
}

func TestCookieValidateNormalizesPositiveMaxAge(t *testing.T) {
	now := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	later := now.Add(24 * time.Hour).Format(http.TimeFormat)
	cookies, err := validateResponseCookies(cookieTestOrigin(t), cookieTestHeader(
		"auth=secret; Path=/; Secure; HttpOnly; Max-Age=60; Expires="+later,
	), now)
	if err != nil {
		t.Fatal(err)
	}
	if len(cookies) != 1 || cookies[0].MaxAge != 0 || !cookies[0].Expires.Equal(now.Add(time.Minute)) || cookies[0].Domain != "" {
		t.Fatalf("normalized cookie = %#v", cookies)
	}
}

func TestCookieValidateRejectsUnsafeCookies(t *testing.T) {
	now := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	past := now.Add(-time.Hour).Format(http.TimeFormat)
	tests := map[string][]string{
		"unexpected name":    {"session=value; Path=/; Secure; HttpOnly"},
		"case changed name":  {"Auth=value; Path=/; Secure; HttpOnly"},
		"subdomain":          {"auth=value; Path=/; Domain=x.api.vrchat.cloud; Secure; HttpOnly"},
		"parent domain":      {"auth=value; Path=/; Domain=vrchat.cloud; Secure; HttpOnly"},
		"leading dot domain": {"auth=value; Path=/; Domain=.api.vrchat.cloud; Secure; HttpOnly"},
		"wrong path":         {"auth=value; Path=/api; Secure; HttpOnly"},
		"missing path":       {"auth=value; Secure; HttpOnly"},
		"missing secure":     {"auth=value; Path=/; HttpOnly"},
		"missing httponly":   {"auth=value; Path=/; Secure"},
		"expired":            {"auth=value; Path=/; Secure; HttpOnly; Expires=" + past},
		"duplicate": {
			"auth=first; Path=/; Secure; HttpOnly",
			"auth=second; Path=/; Secure; HttpOnly",
		},
		"malformed":            {"auth=secret; Path=/; Secure; HttpOnly; SameSite=Broken"},
		"unexpected attribute": {"auth=secret; Path=/; Secure; HttpOnly; Mystery=yes"},
		"malformed expires":    {"auth=secret; Path=/; Secure; HttpOnly; Expires=not-a-date"},
		"malformed max age":    {"auth=secret; Path=/; Secure; HttpOnly; Max-Age=tomorrow"},
	}

	for name, lines := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := validateResponseCookies(cookieTestOrigin(t), cookieTestHeader(lines...), now)
			if err == nil {
				t.Fatal("validateResponseCookies() error = nil")
			}
			for _, secret := range []string{"value", "first", "second", "secret", lines[0]} {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("error %q exposes cookie data", err)
				}
			}
		})
	}
}

func TestCookieValidateRejectsWrongOrigin(t *testing.T) {
	origin, err := url.Parse("https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	_, err = validateResponseCookies(origin, cookieTestHeader("auth=value; Path=/; Secure; HttpOnly"), time.Now())
	if err == nil {
		t.Fatal("validateResponseCookies() error = nil")
	}
}

func TestCookieEncodingOmitsDomainAndRoundTrips(t *testing.T) {
	now := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	input := []http.Cookie{
		{Name: "auth", Value: "first-secret", Path: "/", Domain: "api.vrchat.cloud", Expires: now.Add(time.Hour), Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode},
		{Name: "twoFactorAuth", Value: "second-secret", Path: "/", Domain: "api.vrchat.cloud", Secure: true, HttpOnly: true, SameSite: http.SameSiteNoneMode},
	}

	client := testClient(t, func(*http.Request) (*http.Response, error) { t.Fatal("transport called"); return nil, nil })
	encoded, err := client.EncodeCookies(input)
	if err != nil {
		t.Fatalf("EncodeCookies() error = %v", err)
	}
	if bytes.Contains(bytes.ToLower(encoded), []byte("domain")) || bytes.Contains(encoded, []byte("api.vrchat.cloud")) {
		t.Fatalf("encoded cookies contain domain: %s", encoded)
	}

	decoded, err := client.DecodeCookies(encoded, now)
	if err != nil {
		t.Fatalf("DecodeCookies() error = %v", err)
	}
	if len(decoded) != len(input) {
		t.Fatalf("len(decoded) = %d, want %d", len(decoded), len(input))
	}
	for i := range decoded {
		if decoded[i].Name != input[i].Name || decoded[i].Value != input[i].Value || decoded[i].Path != "/" || decoded[i].Domain != "" || decoded[i].Secure != true || decoded[i].HttpOnly != true || decoded[i].SameSite != input[i].SameSite || !decoded[i].Expires.Equal(input[i].Expires) {
			t.Errorf("decoded[%d] = %#v, input = %#v", i, decoded[i], input[i])
		}
	}
}

func TestCookieDecodeRejectsInvalidPayloads(t *testing.T) {
	now := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	future := now.Add(time.Hour).Format(time.RFC3339)
	valid := `{"name":"auth","value":"secret","path":"/","expires":"` + future + `","secure":true,"httpOnly":true,"sameSite":2}`
	tests := map[string][]byte{
		"malformed":       []byte(`[{`),
		"unknown field":   []byte(`[` + strings.TrimSuffix(valid, `}`) + `,"domain":"api.vrchat.cloud"}]`),
		"trailing json":   []byte(`[` + valid + `] []`),
		"duplicate names": []byte(`[` + valid + `,` + valid + `]`),
		"expired":         []byte(`[{"name":"auth","value":"secret","path":"/","expires":"2029-01-01T00:00:00Z","secure":true,"httpOnly":true,"sameSite":2}]`),
		"bad name":        []byte(`[{"name":"session","value":"secret","path":"/","secure":true,"httpOnly":true,"sameSite":2}]`),
		"bad path":        []byte(`[{"name":"auth","value":"secret","path":"/api","secure":true,"httpOnly":true,"sameSite":2}]`),
		"not secure":      []byte(`[{"name":"auth","value":"secret","path":"/","secure":false,"httpOnly":true,"sameSite":2}]`),
		"not httponly":    []byte(`[{"name":"auth","value":"secret","path":"/","secure":true,"httpOnly":false,"sameSite":2}]`),
		"bad same site":   []byte(`[{"name":"auth","value":"secret","path":"/","secure":true,"httpOnly":true,"sameSite":99}]`),
		"oversized":       bytes.Repeat([]byte("x"), 16*1024+1),
	}

	for name, payload := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := testClient(t, func(*http.Request) (*http.Response, error) { t.Fatal("transport called"); return nil, nil }).DecodeCookies(payload, now)
			if err == nil {
				t.Fatal("DecodeCookies() error = nil")
			}
			if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), string(payload)) {
				t.Fatalf("error %q exposes payload", err)
			}
		})
	}
}

func TestCookieDecodeUsesClientResolvedOrigin(t *testing.T) {
	payload, err := json.Marshal([]map[string]any{{
		"name": "auth", "value": "secret", "path": "/", "secure": true, "httpOnly": true, "sameSite": 0,
	}})
	if err != nil {
		t.Fatal(err)
	}
	client := testClient(t, func(*http.Request) (*http.Response, error) { t.Fatal("transport called"); return nil, nil })
	decoded, err := client.DecodeCookies(payload, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded) != 1 || decoded[0].Domain != "" {
		t.Fatalf("DecodeCookies() = %#v", decoded)
	}
}

func TestCookieEncodeRejectsOversizedPayload(t *testing.T) {
	cookies := []http.Cookie{{Name: "auth", Value: strings.Repeat("x", 16*1024), Path: "/", Secure: true, HttpOnly: true}}
	if _, err := testClient(t, func(*http.Request) (*http.Response, error) { t.Fatal("transport called"); return nil, nil }).EncodeCookies(cookies); err == nil {
		t.Fatal("EncodeCookies() error = nil")
	}
}
