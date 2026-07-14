package federation

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"
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

// --- Exhaustive outbound destination classifier (Task 10) ---
//
// TestClassifyDestination is the table-driven authority for outbound IP
// classification: every IANA special-use range, plus the public/private split,
// must map to exactly one of destinationPublic / destinationPrivate /
// destinationAlwaysBlocked. Adding a new special-use block means adding a row
// here, not a new ad-hoc check.
func TestClassifyDestination(t *testing.T) {
	cases := []struct {
		name string
		ip   string
		want destinationClass
	}{
		// --- Public (routable, non-special-use) ---
		{"public v4 a", "8.8.8.8", destinationPublic},
		{"public v4 b", "1.1.1.1", destinationPublic},
		{"public v6", "2606:4700:4700::1111", destinationPublic},
		{"public v4 mapped v6", "::ffff:8.8.8.8", destinationPublic},
		// --- Loopback (private — allowed only in private mode) ---
		{"loopback v4", "127.0.0.1", destinationPrivate},
		{"loopback v4 hi", "127.255.255.254", destinationPrivate},
		{"loopback v6", "::1", destinationPrivate},

		// --- Link-local / cloud metadata (always blocked) ---
		{"link-local v4 metadata", "169.254.169.254", destinationAlwaysBlocked},
		{"link-local v4 low", "169.254.0.1", destinationAlwaysBlocked},
		{"link-local v6", "fe80::1", destinationAlwaysBlocked},
		{"link-local v6 hi", "febf::1", destinationAlwaysBlocked},

		// --- Multicast (always blocked) ---
		{"multicast v4", "224.0.0.1", destinationAlwaysBlocked},
		{"multicast v4 hi", "239.255.255.255", destinationAlwaysBlocked},
		{"multicast v6", "ff02::1", destinationAlwaysBlocked},

		// --- Unspecified (always blocked) ---
		{"unspecified v4", "0.0.0.0", destinationAlwaysBlocked},
		{"unspecified v6", "::", destinationAlwaysBlocked},

		// --- Documentation (always blocked) ---
		{"doc v4 192.0.2", "192.0.2.1", destinationAlwaysBlocked},
		{"doc v4 198.51.100", "198.51.100.1", destinationAlwaysBlocked},
		{"doc v4 203.0.113", "203.0.113.1", destinationAlwaysBlocked},
		{"doc v6", "2001:db8::1", destinationAlwaysBlocked},

		// --- Benchmarking (always blocked) ---
		{"benchmark v4", "198.18.0.1", destinationAlwaysBlocked},
		{"benchmark v4 hi", "198.19.255.255", destinationAlwaysBlocked},

		// --- Reserved (always blocked) — 240.0.0.0/4 and IPv6 reserved
		{"reserved v4", "240.0.0.1", destinationAlwaysBlocked},
		{"reserved v4 hi", "255.255.255.254", destinationAlwaysBlocked},
		{"reserved v6", "::ffff:0:1", destinationAlwaysBlocked},

		// --- CGNAT 100.64.0.0/10 (always blocked; not RFC1918) ---
		{"cgnat low", "100.64.0.1", destinationAlwaysBlocked},
		{"cgnat mid", "100.100.100.100", destinationAlwaysBlocked},
		{"cgnat hi", "100.127.255.254", destinationAlwaysBlocked},

		// --- IPv4-mapped forms of special-use ---
		{"v4-mapped loopback", "::ffff:127.0.0.1", destinationPrivate},
		{"v4-mapped link-local", "::ffff:169.254.169.254", destinationAlwaysBlocked},
		{"v4-mapped private", "::ffff:10.0.0.1", destinationPrivate},
		{"v4-mapped multicast", "::ffff:224.0.0.1", destinationAlwaysBlocked},

		// --- RFC1918 (private) ---
		{"private 10", "10.0.0.1", destinationPrivate},
		{"private 10 hi", "10.255.255.254", destinationPrivate},
		{"private 172.16", "172.16.0.1", destinationPrivate},
		{"private 172.31", "172.31.255.254", destinationPrivate},
		{"private 172.32 not", "172.32.0.1", destinationPublic},
		{"private 192.168", "192.168.1.1", destinationPrivate},
		{"private 192.168 hi", "192.168.255.254", destinationPrivate},

		// --- ULA fc00::/7 (private) ---
		{"ula fc00", "fc00::1", destinationPrivate},
		{"ula fd00", "fd12:3456:789a::1", destinationPrivate},
		{"ula fdff", "fdff:ffff:ffff:ffff:ffff:ffff:ffff:fffe", destinationPrivate},

		// --- Boundary: just outside private ---
		{"public next to 10", "11.0.0.1", destinationPublic},
		{"public next to 172.16", "172.15.0.1", destinationPublic},
		{"public next to 192.168", "192.169.0.1", destinationPublic},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			addr, err := netip.ParseAddr(tc.ip)
			if err != nil {
				t.Fatalf("ParseAddr(%q): %v", tc.ip, err)
			}
			got := classifyDestination(addr)
			if got != tc.want {
				t.Errorf("classifyDestination(%s) = %v, want %v", tc.ip, got, tc.want)
			}
		})
	}
}

// TestScreenDialControl_AllowsPublicBlocksPrivate asserts the dial screen
// permits only destinationPublic in production mode (allowPrivate=false) and
// rejects every other class. The screen must screen the ACTUAL resolved dial
// address, including IPv4-mapped IPv6 forms.
func TestScreenDialControl_AllowsPublicBlocksPrivate(t *testing.T) {
	allow := false // production
	screen := screenDialControl(allow)
	allowed := []string{"8.8.8.8:443", "[2606:4700:4700::1111]:443"}
	for _, addr := range allowed {
		if err := screen("tcp", addr, nil); err != nil {
			t.Errorf("screen(%q) production: unexpected block %v", addr, err)
		}
	}
	blocked := []string{
		"127.0.0.1:443",
		"169.254.169.254:80",
		"10.0.0.1:443",      // private — must be blocked in production
		"172.16.0.1:443",    // private
		"192.168.1.1:443",   // private
		"[fc00::1]:443",     // ULA private
		"[::1]:443",         // loopback v6
		"[fe80::1]:443",     // link-local v6
		"[ff02::1]:443",     // multicast v6
		"224.0.0.1:443",     // multicast v4
		"0.0.0.0:443",       // unspecified
		"100.64.0.1:443",    // CGNAT
		"240.0.0.1:443",     // reserved
		"192.0.2.1:443",     // documentation
		"198.18.0.1:443",    // benchmark
		"[::ffff:127.0.0.1]:443", // v4-mapped loopback
		"[::ffff:10.0.0.1]:443",  // v4-mapped private — blocked in production
	}
	for _, addr := range blocked {
		if err := screen("tcp", addr, nil); err == nil {
			t.Errorf("screen(%q) production: expected block, got nil", addr)
		}
	}
}

// TestScreenDialControl_PrivateModePermitsRFC1918ULA asserts that when
// allowPrivate=true the dial screen permits RFC1918, ULA, and loopback
// destinations but STILL rejects link-local/metadata, multicast, unspecified,
// CGNAT, documentation, benchmark, reserved, and every other always-blocked
// class. Private mode is a narrow allowance, not a blanket bypass.
func TestScreenDialControl_PrivateModePermitsRFC1918ULA(t *testing.T) {
	screen := screenDialControl(true) // private mode
	allowed := []string{
		"10.0.0.1:443",
		"172.16.0.1:443",
		"192.168.1.1:443",
		"[fc00::1]:443",
		"[fd12:3456:789a::1]:443",
		"127.0.0.1:443",   // loopback permitted in private mode
		"[::1]:443",       // loopback v6 permitted in private mode
		"8.8.8.8:443",     // public still allowed
	}
	for _, addr := range allowed {
		if err := screen("tcp", addr, nil); err != nil {
			t.Errorf("screen(%q) private: unexpected block %v", addr, err)
		}
	}
	stillBlocked := []string{
		"169.254.169.254:80",
		"[fe80::1]:443",
		"[ff02::1]:443",
		"224.0.0.1:443",
		"0.0.0.0:443",
		"100.64.0.1:443",
		"240.0.0.1:443",
		"192.0.2.1:443",
		"198.18.0.1:443",
		"[::ffff:169.254.169.254]:443", // v4-mapped metadata still blocked
	}
	for _, addr := range stillBlocked {
		if err := screen("tcp", addr, nil); err == nil {
			t.Errorf("screen(%q) private: expected block, got nil", addr)
		}
	}
}

// TestScreenDialControl_UnconditionalMetadataRejection asserts that the cloud
// metadata address 169.254.169.254 is rejected in BOTH production and private
// mode — it is never reachable regardless of policy.
func TestScreenDialControl_UnconditionalMetadataRejection(t *testing.T) {
	for _, allowPrivate := range []bool{false, true} {
		screen := screenDialControl(allowPrivate)
		if err := screen("tcp", "169.254.169.254:80", nil); err == nil {
			t.Errorf("metadata address reached in allowPrivate=%v; must always be blocked", allowPrivate)
		}
	}
}

// TestScreenDialControl_RejectsNonIPLiteral asserts the dial screen fails
// closed when the Control hook receives a non-IP host (should not happen —
// Control runs after DNS resolution — but must not silently pass).
func TestScreenDialControl_RejectsNonIPLiteral(t *testing.T) {
	screen := screenDialControl(false)
	if err := screen("tcp", "example.com:443", nil); err == nil {
		t.Error("screen accepted a non-IP literal; want fail-closed")
	}
}

// dnsSteeringResolver is a net.Resolver that returns canned IPs for a fixed
// set of hostnames, so tests can exercise the DNS-answer screening path
// without real DNS. Each hostname maps to a list of IPs (mirrors a real
// resolver returning multiple A/AAAA records).
type dnsSteeringResolver struct {
	answers map[string][]string
}

func (r dnsSteeringResolver) LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error) {
	if ips, ok := r.answers[host]; ok {
		out := make([]netip.Addr, 0, len(ips))
		for _, s := range ips {
			a, err := netip.ParseAddr(s)
			if err != nil {
				return nil, err
			}
			out = append(out, a)
		}
		return out, nil
	}
	return nil, fmt.Errorf("dns steering: no answer for %q", host)
}

// errScreenedDialBaseReached is returned by a stub base DialContext when the
// screen ALLOWED the hostname through to the connect step — tests assert the
// screen blocks BEFORE this base is reached.
var errScreenedDialBaseReached = errors.New("test: screened dial base reached")

// newScreenedDialTestClient builds a client whose DialContext screens every
// DNS answer through the steering resolver and never actually connects
// (base returns errScreenedDialBaseReached). allowPrivate selects the policy;
// redirect allowPrivate is passed separately.
func newScreenedDialTestClient(t *testing.T, allowPrivate bool, maxBytes int64, res dnsResolver) *http.Client {
	t.Helper()
	base := func(ctx context.Context, network, address string) (net.Conn, error) {
		return nil, errScreenedDialBaseReached
	}
	transport := &http.Transport{
		DialContext:         screenedDialContext(allowPrivate, res, base),
		TLSHandshakeTimeout: 5 * time.Second,
	}
	return &http.Client{
		Transport: cappingTransport{base: transport, max: maxBytes},
		Timeout:   federationHTTPTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxFederationRedirects {
				return fmt.Errorf("federation/oidc: too many redirects (>%d)", maxFederationRedirects)
			}
			return validateRedirectScheme(req, via, allowPrivate)
		},
	}
}

// TestHardenedClient_BlocksMixedPublicPrivateDNS asserts that when DNS
// returns a mix of public and private/internal IPs for one hostname, the dial
// screen rejects the connection (every DNS answer is screened). A mixed
// answer set must not let the private record slip through.
func TestHardenedClient_BlocksMixedPublicPrivateDNS(t *testing.T) {
	res := dnsSteeringResolver{answers: map[string][]string{
		"mixed.example.test": {"8.8.8.8", "10.0.0.1"}, // public + private
	}}
	client := newScreenedDialTestClient(t, false, maxFederationResponseBytes, res)
	_, err := client.Get("https://mixed.example.test/")
	if err == nil {
		t.Fatal("dial screen accepted a host whose DNS includes a private answer; want block")
	}
	if !errors.Is(err, errScreenedDialBaseReached) {
		// Good: the screen blocked before reaching the base dialer.
		if !strings.Contains(err.Error(), "blocked") && !strings.Contains(err.Error(), "resolves") {
			t.Errorf("error %q does not identify the blocked DNS answer", err)
		}
	} else {
		t.Fatal("screen let the mixed answer through to the base dialer")
	}
}

// TestHardenedClient_BlocksAllPrivateDNS asserts that when DNS returns only
// private IPs, the production dial screen blocks.
func TestHardenedClient_BlocksAllPrivateDNS(t *testing.T) {
	res := dnsSteeringResolver{answers: map[string][]string{
		"internal.example.test": {"10.0.0.1", "172.16.0.1"},
	}}
	client := newScreenedDialTestClient(t, false, maxFederationResponseBytes, res)
	_, err := client.Get("https://internal.example.test/")
	if err == nil {
		t.Fatal("dial screen accepted an all-private DNS answer in production; want block")
	}
	if errors.Is(err, errScreenedDialBaseReached) {
		t.Fatal("screen let the all-private answer through to the base dialer")
	}
}

// TestHardenedClient_AllowsAllPublicDNS asserts that when DNS returns only
// public IPs, the production dial screen passes the screen and reaches the
// base dialer (which then returns the sentinel — proving the screen did not
// block).
func TestHardenedClient_AllowsAllPublicDNS(t *testing.T) {
	res := dnsSteeringResolver{answers: map[string][]string{
		"public.example.test": {"8.8.8.8", "2606:4700:4700::1111"},
	}}
	client := newScreenedDialTestClient(t, false, maxFederationResponseBytes, res)
	_, err := client.Get("https://public.example.test/")
	if err == nil {
		t.Fatal("expected the base dialer sentinel error (screen must pass but connect must not succeed)")
	}
	if !errors.Is(err, errScreenedDialBaseReached) {
		t.Fatalf("screen blocked an all-public DNS answer: %v", err)
	}
}

// TestHardenedClient_PrivateModeAllowsPrivateDNS asserts that in private mode
// a private DNS answer is permitted (the screen passes and the base dialer is
// reached).
func TestHardenedClient_PrivateModeAllowsPrivateDNS(t *testing.T) {
	res := dnsSteeringResolver{answers: map[string][]string{
		"internal.example.test": {"10.0.0.1"},
	}}
	client := newScreenedDialTestClient(t, true, maxFederationResponseBytes, res)
	_, err := client.Get("https://internal.example.test/")
	if err == nil {
		t.Fatal("expected the base dialer sentinel error (screen must pass but connect must not succeed)")
	}
	if !errors.Is(err, errScreenedDialBaseReached) {
		t.Fatalf("dial screen blocked a private DNS answer in private mode: %v", err)
	}
}

// TestHardenedClient_PrivateModeBlocksMetadataDNS asserts that even in private
// mode a DNS answer that resolves to the metadata address is rejected.
func TestHardenedClient_PrivateModeBlocksMetadataDNS(t *testing.T) {
	res := dnsSteeringResolver{answers: map[string][]string{
		"meta.example.test": {"169.254.169.254"},
	}}
	client := newScreenedDialTestClient(t, true, maxFederationResponseBytes, res)
	_, err := client.Get("https://meta.example.test/")
	if err == nil {
		t.Fatal("dial screen accepted a metadata DNS answer in private mode; want always-blocked")
	}
	if errors.Is(err, errScreenedDialBaseReached) {
		t.Fatal("screen let the metadata answer through to the base dialer")
	}
}

// TestHardenedClient_BlocksPublicToPrivateRedirect asserts the full client
// (CheckRedirect + dial screen) refuses a redirect to a hostname whose DNS
// resolves to an always-blocked IP (metadata) — the redirect-to-internal SSRF
// is closed at the dial screen even in private mode. The origin is a loopback
// httptest TLS server (reachable in private mode); the redirect target
// resolves to the metadata address, which is alwaysBlocked regardless of
// policy.
func TestHardenedClient_BlocksPublicToPrivateRedirect(t *testing.T) {
	res := dnsSteeringResolver{answers: map[string][]string{
		"meta.example.test": {"169.254.169.254"},
	}}
	// Build a client with the resolver-steered dial screen AND a real dialer
	// base (with the IP-literal Control hook) so the loopback origin is
	// reachable. The redirect target is resolved through the steering
	// resolver and blocked at the dial screen.
	dialer := &net.Dialer{
		Timeout:   5 * time.Second,
		Control:   screenDialControl(true),
	}
	transport := &http.Transport{
		DialContext:           screenedDialContext(true, res, dialer.DialContext),
		TLSHandshakeTimeout:   5 * time.Second,
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{
		Transport: cappingTransport{base: transport, max: maxFederationResponseBytes},
		Timeout:   federationHTTPTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxFederationRedirects {
				return fmt.Errorf("federation/oidc: too many redirects (>%d)", maxFederationRedirects)
			}
			return validateRedirectScheme(req, via, true)
		},
	}
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://meta.example.test/secret", http.StatusFound)
	}))
	defer origin.Close()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, origin.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	_, err = client.Do(req)
	if err == nil {
		t.Fatal("client followed a redirect to an always-blocked metadata target; want dial-screen block")
	}
	if !strings.Contains(err.Error(), "blocked") && !strings.Contains(err.Error(), "resolves") {
		t.Errorf("error %q does not identify the blocked redirect target", err)
	}
}

// TestAvatarFetch_PrivateModeBlocksMetadata asserts the avatar fetch path
// inherits the classifier: even in private mode, an avatar URL whose host
// resolves to the metadata address is rejected by the dial screen.
func TestAvatarFetch_PrivateModeBlocksMetadata(t *testing.T) {
	res := dnsSteeringResolver{answers: map[string][]string{
		"meta.example.test": {"169.254.169.254"},
	}}
	client := newScreenedDialTestClient(t, true, maxAvatarFetchBytes, res)
	_, err := fetchUpstreamAvatarWithClient(context.Background(), "https://meta.example.test/a.png", client, true)
	if err == nil {
		t.Fatal("avatar fetch reached a metadata address in private mode; want dial-screen block")
	}
	if errors.Is(err, errScreenedDialBaseReached) {
		t.Fatal("screen let the metadata avatar target through to the base dialer")
	}
}
