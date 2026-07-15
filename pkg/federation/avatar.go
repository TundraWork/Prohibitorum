// Package oidc — avatar_fetch.go
//
// fetchUpstreamAvatar GETs an upstream OIDC picture URL through the same
// SSRF-hardened dial screen as the rest of federation. It is https-only,
// rejects non-image content types, and caps the body to maxAvatarFetchBytes
// (5 MiB), matching the input cap enforced by pkg/avatar.Process. The
// returned bytes are ready to pass directly to avatar.Process.
package federation

import (
	"context"
	"fmt"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
	
	"github.com/jackc/pgx/v5/pgtype"
	
	avatarpkg "prohibitorum/pkg/avatar"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/kv"
)

const maxAvatarFetchBytes = 5 << 20 // 5 MiB, matches pkg/avatar input cap.

// fetchUpstreamAvatar GETs an upstream picture URL through the same SSRF-hardened
// dial-screen as the rest of federation, capped to 5 MiB. https-only; rejects
// non-image responses. Returns raw bytes for pkg/avatar.Process.
func fetchUpstreamAvatar(ctx context.Context, rawURL string, allowPrivate bool) ([]byte, error) {
	// https-only in production. allowPrivate (trusted-internal-IdP deployments +
	// loopback-OP tests; the same flag that disables the dial-time IP screen)
	// additionally permits http so a plaintext loopback OP can serve the picture.
	if err := validateAvatarURL(rawURL, allowPrivate); err != nil {
		return nil, err
	}
	return fetchUpstreamAvatarWithClient(ctx, rawURL, hardenedHTTPClient(allowPrivate, maxAvatarFetchBytes), allowPrivate)
}

func validateAvatarURL(rawURL string, allowPrivate bool) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("federation/oidc: avatar url parse: %w", err)
	}
	if u.Scheme == "https" {
		return nil
	}
	if u.Scheme == "http" && allowPrivate {
		return nil
	}
	return fmt.Errorf("federation/oidc: avatar url must be https, got %q", u.Scheme)
}

func fetchUpstreamAvatarWithClient(ctx context.Context, rawURL string, client *http.Client, allowPrivate bool) ([]byte, error) {
	if err := validateAvatarURL(rawURL, allowPrivate); err != nil {
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

type AvatarQueries interface {
	GetAccountByID(context.Context, int32) (db.Account, error)
	ListAvatarSourcesByAccount(context.Context, int32) ([]db.ListAvatarSourcesByAccountRow, error)
	UpsertAvatarSource(context.Context, db.UpsertAvatarSourceParams) error
	SetActiveAvatar(context.Context, db.SetActiveAvatarParams) error
}

type AvatarManager struct {
	queries AvatarQueries
	kv      kv.Store
	fetch   func(context.Context, string, bool) ([]byte, error)
}

func NewAvatarManager(queries AvatarQueries, store kv.Store) *AvatarManager {
	return &AvatarManager{queries: queries, kv: store, fetch: fetchUpstreamAvatar}
}

func (m *AvatarManager) Inherit(accountID int32, provider Provider, delivery AvatarDelivery, resolver AvatarResolver) {
	if delivery.URL == "" && (resolver == nil || delivery.Opaque == nil) {
		return
	}
	go m.run(context.Background(), accountID, provider, delivery, resolver)
}

func (m *AvatarManager) Pending(ctx context.Context, accountID int32) bool {
	pattern := AvatarFetchPattern(accountID)
	var cursor uint64
	for {
		result, err := m.kv.ScanEntries(ctx, pattern, cursor, 64)
		if err != nil {
			return false
		}
		if len(result.Entries) > 0 {
			return true
		}
		if result.NextCursor == 0 {
			return false
		}
		cursor = result.NextCursor
	}
}

func (m *AvatarManager) run(parent context.Context, accountID int32, provider Provider, delivery AvatarDelivery, resolver AvatarResolver) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	key := AvatarFetchKey(accountID, provider.ID)
	locked, err := m.kv.SetNX(ctx, key, "1", time.Minute)
	if err != nil || !locked {
		return
	}
	defer func() { _ = m.kv.Del(ctx, key) }()

	avatarURL := delivery.URL
	if avatarURL == "" {
		avatarURL, err = resolver.ResolveAvatar(ctx, provider, delivery)
		if err != nil {
			slog.WarnContext(ctx, "federation: upstream avatar resolution failed", "account_id", accountID, "err", err)
			return
		}
	}
	if avatarURL == "" {
		return
	}
	var config providerConfig
	if err := json.Unmarshal(provider.Config, &config); err != nil {
		return
	}
	raw, err := m.fetch(ctx, avatarURL, config.AllowPrivateNetwork)
	if err != nil {
		slog.WarnContext(ctx, "federation: upstream avatar fetch failed", "account_id", accountID, "err", err)
		return
	}
	processed, etag, err := avatarpkg.Process(raw)
	if err != nil {
		slog.WarnContext(ctx, "federation: upstream avatar process failed", "account_id", accountID, "err", err)
		return
	}
	account, err := m.queries.GetAccountByID(ctx, accountID)
	if err != nil {
		return
	}
	sources, err := m.queries.ListAvatarSourcesByAccount(ctx, accountID)
	if err != nil {
		return
	}
	source := "upstream:" + provider.Slug
	var oldETag string
	for _, existing := range sources {
		if existing.Source == source && existing.Etag.Valid {
			oldETag = existing.Etag.String
			break
		}
	}
	changed := oldETag != etag
	if changed {
		providerID := provider.ID
		if err := m.queries.UpsertAvatarSource(ctx, db.UpsertAvatarSourceParams{
			AccountID: accountID, Source: source, Bytes: processed,
			ContentType: pgtype.Text{String: "image/webp", Valid: true},
			Etag: pgtype.Text{String: etag, Valid: true}, IdpID: &providerID,
		}); err != nil {
			return
		}
	}
	if (!account.AvatarSource.Valid || account.AvatarSource.String == source) && (changed || !account.AvatarSource.Valid) {
		_ = m.queries.SetActiveAvatar(ctx, db.SetActiveAvatarParams{Source: source, AccountID: accountID})
	}
}
