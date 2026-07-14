package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	fedoidc "prohibitorum/pkg/federation"
)

// metadataXML is a minimal but well-formed SAML SP metadata document used as the
// happy-path response body.
const metadataXML = `<?xml version="1.0" encoding="UTF-8"?>
<EntityDescriptor xmlns="urn:oasis:names:tc:SAML:2.0:metadata" entityID="https://sp.example.test">
  <SPSSODescriptor protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol">
    <AssertionConsumerService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST" Location="https://sp.example.test/acs" index="0" isDefault="true"/>
  </SPSSODescriptor>
</EntityDescriptor>`

// metadataTLSServer is an https test server serving body at status.
func metadataTLSServer(t *testing.T, body string, status int) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	return srv
}

// These tests cover the CLI fetchMetadata URL-validation gate (which uses the
// shared fedoidc.ValidateOutboundURL policy) and the fetchMetadataWithClient
// fetch/size/cancellation parsing behavior. The redirect scheme/hop/internal-
// target policy and the response-body size-cap enforcement are exercised in
// pkg/federation/oidc, where the unexported hardened client lives. CLI tests
// inject an httptest.Server.Client() through fetchMetadataWithClient so no
// test-only export or transport-internal type assertion is needed.

func TestFetchMetadata_RejectsPlainHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = io.WriteString(w, metadataXML)
	}))
	defer srv.Close()
	_, err := fetchMetadata(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("fetchMetadata accepted a plain http:// metadata URL; want https-only rejection")
	}
	if !strings.Contains(err.Error(), "https") && !strings.Contains(err.Error(), "scheme") {
		t.Errorf("error %q does not mention the https requirement", err)
	}
}

func TestFetchMetadata_RejectsIPLiteral(t *testing.T) {
	for _, u := range []string{
		"https://127.0.0.1:8443/metadata",
		"https://169.254.169.254/latest/meta-data",
		"https://[::1]/metadata",
		"https://10.0.0.5/metadata",
	} {
		_, err := fetchMetadata(context.Background(), u)
		if err == nil {
			t.Errorf("fetchMetadata accepted IP-literal metadata URL %q; want rejection", u)
		}
	}
}

func TestFetchMetadata_RejectsLoopbackHost(t *testing.T) {
	_, err := fetchMetadata(context.Background(), "https://localhost/metadata")
	if err == nil {
		t.Errorf("fetchMetadata accepted localhost host; want rejection (loopback is not a domain for production fetch)")
	}
}

func TestFetchMetadata_RejectsUserinfo(t *testing.T) {
	_, err := fetchMetadata(context.Background(), "https://user:secrets@sp.example.test/metadata")
	if err == nil {
		t.Fatal("fetchMetadata accepted a URL carrying userinfo; want rejection")
	}
	// The secret must never appear in any error output.
	if err != nil && strings.Contains(err.Error(), "secrets") {
		t.Errorf("error %q leaks the userinfo secret", err)
	}
}

func TestFetchMetadata_RejectsRelativeURL(t *testing.T) {
	_, err := fetchMetadata(context.Background(), "/metadata")
	if err == nil {
		t.Fatal("fetchMetadata accepted a relative URL; want rejection")
	}
}

func TestFetchMetadata_AcceptsValidHTTPSDomain(t *testing.T) {
	srv := metadataTLSServer(t, metadataXML, http.StatusOK)
	defer srv.Close()
	// Use the test server's own client (trusts its self-signed cert) through
	// the fetchMetadataWithClient seam to exercise the fetch + status/body
	// parsing path. The shared redirect/size policy is tested in
	// pkg/federation/oidc.
	b, err := fetchMetadataWithClient(context.Background(), srv.URL, srv.Client())
	if err != nil {
		t.Fatalf("fetchMetadata rejected a valid https metadata response: %v", err)
	}
	if !strings.Contains(string(b), "EntityDescriptor") {
		t.Errorf("body = %q, want SAML metadata", b)
	}
}

func TestFetchMetadata_AcceptsResponseAtCap(t *testing.T) {
	// Exactly metadataMaxBytes bytes must be accepted — the LimitReader bound
	// in fetchMetadataWithClient reads the full within-cap body.
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write(make([]byte, metadataMaxBytes))
	}))
	defer srv.Close()

	b, err := fetchMetadataWithClient(context.Background(), srv.URL, srv.Client())
	if err != nil {
		t.Fatalf("fetchMetadata rejected a within-cap response: %v", err)
	}
	if len(b) != metadataMaxBytes {
		t.Errorf("body len = %d, want %d", len(b), metadataMaxBytes)
	}
}

func TestFetchMetadata_RespectsContextCancellation(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * time.Second)
		w.Header().Set("Content-Type", "application/xml")
		_, _ = io.WriteString(w, metadataXML)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := fetchMetadataWithClient(ctx, srv.URL, srv.Client())
	if err == nil {
		t.Fatal("fetchMetadata did not honor context cancellation; want timeout/cancel error")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) && !strings.Contains(err.Error(), "context") {
		t.Errorf("error %q is not a context cancellation error", err)
	}
}

func TestFetchMetadata_AcceptsInlineFileBranchUntouched(t *testing.T) {
	// The inline --metadata-file path is os.ReadFile, not fetchMetadata. This
	// guards the branch boundary: a file:// URL must not be treated as a remote
	// https fetch (fetchMetadata rejects non-https schemes).
	if _, err := fetchMetadata(context.Background(), "file:///tmp/nope"); err == nil {
		t.Error("fetchMetadata accepted a file:// URL; remote fetch should reject non-https schemes")
	}
}

// TestFetchMetadata_HardenedClientBlocksMetadataIP asserts the CLI's shared
// hardened client (NewOutboundHTTPClient) rejects a dial to the cloud metadata
// address. The URL validation gate (https + domain host) passes for a domain
// that resolves to 169.254.169.254, but the dial screen must block the
// resolved IP. This test uses a test server on loopback with allowPrivate=true
// to confirm the metadata address is still blocked (alwaysBlocked class).
func TestFetchMetadata_HardenedClientBlocksMetadataIP(t *testing.T) {
	// The hardened client with allowPrivate=true still blocks metadata (169.254.169.254)
	// because it is alwaysBlocked, not private.
	client := fedoidc.NewOutboundHTTPClient(true, metadataMaxBytes)
	// Dial the metadata address directly — the dial screen must reject it.
	// We use a request to a URL with the metadata IP literal; the URL
	// validation in fetchMetadata would reject an IP literal, so we test the
	// client dial path directly.
	_, err := client.Get("https://169.254.169.254/latest/meta-data")
	if err == nil {
		t.Fatal("hardened client dialed the cloud metadata address; want always-blocked")
	}
	if !strings.Contains(err.Error(), "blocked") {
		t.Errorf("error %q does not mention the dial block", err)
	}
}

// TestFetchMetadata_HardenedClientBlocksCGNAT asserts the CLI's shared hardened
// client rejects CGNAT (100.64.0.0/10) addresses — a range Go's net.IP.IsPrivate
// does NOT cover but the exhaustive classifier does.
func TestFetchMetadata_HardenedClientBlocksCGNAT(t *testing.T) {
	client := fedoidc.NewOutboundHTTPClient(true, metadataMaxBytes)
	_, err := client.Get("https://100.64.0.1/metadata")
	if err == nil {
		t.Fatal("hardened client dialed a CGNAT address; want always-blocked")
	}
	if !strings.Contains(err.Error(), "blocked") {
		t.Errorf("error %q does not mention the dial block", err)
	}
}
