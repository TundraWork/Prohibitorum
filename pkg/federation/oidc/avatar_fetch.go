// Package oidc — avatar_fetch.go
//
// fetchUpstreamAvatar GETs an upstream OIDC picture URL through the same
// SSRF-hardened dial screen as the rest of federation. It is https-only,
// rejects non-image content types, and caps the body to maxAvatarFetchBytes
// (5 MiB), matching the input cap enforced by pkg/avatar.Process. The
// returned bytes are ready to pass directly to avatar.Process.
package oidc

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const maxAvatarFetchBytes = 5 << 20 // 5 MiB, matches pkg/avatar input cap.

// fetchUpstreamAvatar GETs an upstream picture URL through the same SSRF-hardened
// dial-screen as the rest of federation, capped to 5 MiB. https-only; rejects
// non-image responses. Returns raw bytes for pkg/avatar.Process.
func fetchUpstreamAvatar(ctx context.Context, rawURL string, allowPrivate bool) ([]byte, error) {
	if err := validateAvatarURL(rawURL); err != nil {
		return nil, err
	}
	return fetchUpstreamAvatarWithClient(ctx, rawURL, hardenedHTTPClient(allowPrivate, maxAvatarFetchBytes))
}

func validateAvatarURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("federation/oidc: avatar url parse: %w", err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("federation/oidc: avatar url must be https, got %q", u.Scheme)
	}
	return nil
}

func fetchUpstreamAvatarWithClient(ctx context.Context, rawURL string, client *http.Client) ([]byte, error) {
	if err := validateAvatarURL(rawURL); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("federation/oidc: avatar request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("federation/oidc: avatar fetch: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("federation/oidc: avatar fetch status %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "image/") {
		return nil, fmt.Errorf("federation/oidc: avatar content-type %q is not an image", ct)
	}
	b, err := io.ReadAll(resp.Body) // when called via fetchUpstreamAvatar, the body is byte-capped by cappingTransport
	if err != nil {
		return nil, fmt.Errorf("federation/oidc: avatar read: %w", err)
	}
	return b, nil
}
