package oidc

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// hardenedHTTPClientWithTLS is a test-only seam that builds the hardened
// client with a caller-supplied TLS config (so a test can trust a loopback
// httptest.NewTLSServer's self-signed cert for the *origin* hop while the
// CheckRedirect scheme policy and the redirect-hop cap remain in force).
// The dial-time resolved-IP screen is still governed by allowPrivate, exactly
// as in production. It is the single shared outbound client — tests reuse it
// rather than copying dial-screen code.
func hardenedHTTPClientWithTLS(allowPrivate bool, maxBytes int64, tlsConf *tls.Config) *http.Client {
	c := hardenedHTTPClient(allowPrivate, maxBytes)
	if tr, ok := c.Transport.(cappingTransport); ok {
		if ht, ok := tr.base.(*http.Transport); ok {
			ht.TLSClientConfig = tlsConf
		}
	}
	return c
}

// TestHardenedClient_RejectsHTTPDowngrade asserts that an HTTPS request the
// server redirects to a plaintext http:// URL is refused by the hardened
// client's CheckRedirect hook — the redirect cannot downgrade the connection
// security. This holds even in allowPrivate (trusted-internal / loopback-test)
// mode: http is only ever permitted when the *initial* request was itself http,
// so an HTTPS→HTTP hop is always a downgrade.
func TestHardenedClient_RejectsRedirectHTTPDowngrade(t *testing.T) {
	// The plaintext target the redirect points at. It must never be reached.
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Errorf("downgraded http target was reached; redirect policy failed")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("leaked-over-http"))
	}))
	defer target.Close()

	// The origin TLS server responds 302 → the plaintext target.
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer origin.Close()

	client := hardenedHTTPClientWithTLS(true, maxFederationResponseBytes, &tls.Config{InsecureSkipVerify: true})

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, origin.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := client.Do(req)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatalf("client followed HTTPS→HTTP downgrade; want redirect rejection")
	}
	if !strings.Contains(err.Error(), "http") && !strings.Contains(err.Error(), "scheme") && !strings.Contains(err.Error(), "downgrade") {
		t.Errorf("error %q does not explain the scheme downgrade rejection", err)
	}
}

// TestHardenedClient_AllowsHTTPRedirectInPrivateMode confirms that when
// allowPrivate is true and the *initial* request is http:// (permitted by the
// private-mode policy), same-scheme http redirects are followed.
func TestHardenedClient_AllowsHTTPRedirectInPrivateMode(t *testing.T) {
	final := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok"))
	}))
	defer final.Close()

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, final.URL, http.StatusFound)
	}))
	defer origin.Close()

	client := hardenedHTTPClient(true, maxFederationResponseBytes)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, origin.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("client refused same-scheme http redirect in private mode: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "ok" {
		t.Errorf("body = %q, want %q", body, "ok")
	}
}

// TestHardenedClient_RejectsHTTPDowngradeFromHTTPSInPrivateMode confirms the
// downgrade rule holds in private mode even when the initial request is http:
// an http→https→http chain must NOT re-downgrade after the https upgrade.
func TestHardenedClient_RejectsRedirectHTTPDowngradeInPrivateMode(t *testing.T) {
	// Plaintext "final" target that must never be reached.
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Errorf("downgraded http target was reached after https upgrade")
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	// HTTPS middle hop: http origin redirects here, then this redirects to the
	// plaintext target — that second hop is an https→http downgrade and must
	// be refused even in private mode.
	middle := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer middle.Close()

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, middle.URL, http.StatusFound)
	}))
	defer origin.Close()

	client := hardenedHTTPClientWithTLS(true, maxFederationResponseBytes, &tls.Config{InsecureSkipVerify: true})

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, origin.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	_, err = client.Do(req)
	if err == nil {
		t.Fatalf("client followed https→http downgrade in private mode; want rejection")
	}
	if !strings.Contains(err.Error(), "downgrade") && !strings.Contains(err.Error(), "http") {
		t.Errorf("error %q does not explain the downgrade rejection", err)
	}
}

// TestHardenedClient_TooManyRedirects confirms the redirect-hop cap still
// fires regardless of scheme policy.
func TestHardenedClient_TooManyRedirects(t *testing.T) {
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Always redirect to self, keeping scheme https so the scheme policy
		// does not fire — only the hop cap should.
		next := &url.URL{Scheme: "https", Host: r.Host, Path: "/loop"}
		http.Redirect(w, r, next.String(), http.StatusFound)
	}))
	defer origin.Close()

	client := hardenedHTTPClientWithTLS(true, maxFederationResponseBytes, &tls.Config{InsecureSkipVerify: true})

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, origin.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	_, err = client.Do(req)
	if err == nil {
		t.Fatalf("client followed an unbounded redirect loop; want hop-cap error")
	}
	if !strings.Contains(err.Error(), "too many redirects") {
		t.Errorf("error %q does not mention the redirect cap", err)
	}
}

// TestAvatarFetch_RejectsHTTPDowngrade wires the downgrade policy through the
// avatar fetch path (validateAvatarURL + fetchUpstreamAvatarWithClient) so the
// redirect policy is exercised end-to-end on the production avatar path.
func TestAvatarFetch_RejectsHTTPDowngrade(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Errorf("downgraded http avatar target reached")
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("\x89PNG\r\n\x1a\n"))
	}))
	defer target.Close()

	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer origin.Close()

	client := hardenedHTTPClientWithTLS(true, maxAvatarFetchBytes, &tls.Config{InsecureSkipVerify: true})
	_, err := fetchUpstreamAvatarWithClient(context.Background(), origin.URL, client, true)
	if err == nil {
		t.Fatalf("avatar fetch followed HTTPS→HTTP downgrade; want rejection")
	}
	if !strings.Contains(err.Error(), "downgrade") && !strings.Contains(err.Error(), "http") {
		t.Errorf("error %q does not explain the downgrade rejection", err)
	}
}

// TestHardenedClient_RejectsRedirectToInternalTarget asserts that in production
// mode (!allowPrivate) a redirect to an internal IP-literal target is refused
// by the per-hop outbound-URL policy before any dial — the internal address is
// never reached. A controlled-transport client (no dial screen, so the loopback
// test origin is reachable, but the production CheckRedirect policy) isolates
// the redirect-policy behavior from the dial screen, which is independently
// covered by TestHardenedClient_BlocksInternalIssuer.
func TestHardenedClient_RejectsRedirectToInternalTarget(t *testing.T) {
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://169.254.169.254/latest/meta-data", http.StatusFound)
	}))
	defer origin.Close()

	// Controlled transport: trust the loopback origin's self-signed cert, no
	// dial screen, but the production redirect policy (allowPrivate=false) so
	// validateRedirectScheme runs validateOutboundURL on the redirect target.
	base := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	client := &http.Client{
		Transport: cappingTransport{base: base, max: maxFederationResponseBytes},
		Timeout:   federationHTTPTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxFederationRedirects {
				return fmt.Errorf("federation/oidc: too many redirects (>%d)", maxFederationRedirects)
			}
			return validateRedirectScheme(req, via, false /* production */)
		},
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, origin.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	_, err = client.Do(req)
	if err == nil {
		t.Fatal("client followed a redirect to an internal target; want rejection")
	}
	if !strings.Contains(err.Error(), "IP literal") && !strings.Contains(err.Error(), "blocked") && !strings.Contains(err.Error(), "internal") {
		t.Errorf("error %q does not identify the internal redirect target", err)
	}
}

// TestHardenedClient_RejectsOversizeResponse asserts the shared response-body
// size cap (cappingTransport) rejects a body over maxFederationResponseBytes.
// This cap is reused by the CLI metadata fetch via NewOutboundHTTPClient, so
// the CLI does not re-test it.
func TestHardenedClient_RejectsOversizeResponse(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		// Write well over the 2 MiB cap.
		chunk := strings.Repeat("x", 64*1024)
		for range 40 { // 40 * 64 KiB = 2.5 MiB > 2 MiB cap
			_, _ = io.WriteString(w, chunk)
		}
	}))
	defer srv.Close()

	client := hardenedHTTPClientWithTLS(true, maxFederationResponseBytes, &tls.Config{InsecureSkipVerify: true})
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	// The size cap fires as the body is read (cappingTransport wraps it in a
	// cappedBody that errors past the cap); a truncated doc must fail, not
	// parse partially.
	_, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr == nil {
		t.Fatal("client accepted an oversize response body; want size-cap error")
	}
	if !strings.Contains(readErr.Error(), "large") && !strings.Contains(readErr.Error(), "exceed") && !strings.Contains(readErr.Error(), "cap") {
		t.Errorf("read error %q does not mention the size cap", readErr)
	}
}

// TestHardenedClient_RejectsResponseAtCapPlusOne asserts the cap is exact: a
// body of maxFederationResponseBytes+1 bytes is rejected.
func TestHardenedClient_RejectsResponseAtCapPlusOne(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write(make([]byte, maxFederationResponseBytes+1))
	}))
	defer srv.Close()

	client := hardenedHTTPClientWithTLS(true, maxFederationResponseBytes, &tls.Config{InsecureSkipVerify: true})
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	_, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr == nil {
		t.Fatal("client accepted a cap+1 response body; want size-cap rejection")
	}
}

// TestHardenedClient_AcceptsResponseAtCap asserts a body of exactly
// maxFederationResponseBytes bytes is accepted (the cap is inclusive).
func TestHardenedClient_AcceptsResponseAtCap(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write(make([]byte, maxFederationResponseBytes))
	}))
	defer srv.Close()

	client := hardenedHTTPClientWithTLS(true, maxFederationResponseBytes, &tls.Config{InsecureSkipVerify: true})
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("client rejected a within-cap response: %v", err)
	}
	b, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if len(b) != maxFederationResponseBytes {
		t.Errorf("body len = %d, want %d", len(b), maxFederationResponseBytes)
	}
}
