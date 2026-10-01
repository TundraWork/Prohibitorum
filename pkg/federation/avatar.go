// Package federation owns protocol-neutral avatar inheritance from verified
// upstream identities, fetched while the sign-in, link or enrollment completes. Avatar fetches use the shared hardened outbound policy,
// reject non-image responses, and cap bodies to the avatar processor's input
// limit.
package federation

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	avatarpkg "prohibitorum/pkg/avatar"
	"prohibitorum/pkg/db"
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
		return &avatarFetchError{category: "invalid url"}
	}
	if u.Scheme == "https" {
		return nil
	}
	if u.Scheme == "http" && allowPrivate {
		return nil
	}
	return &avatarFetchError{category: "url scheme rejected"}
}

type avatarFetchError struct {
	category string
}

func (e *avatarFetchError) Error() string {
	return "federation/oidc: avatar fetch " + e.category
}

func sanitizeAvatarFetchError(err error) error {
	category := "transport error"
	switch {
	case errors.Is(err, errHTTPRedirectDowngrade):
		category = "redirect downgrade blocked"
	case errors.Is(err, context.Canceled):
		category = "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		category = "timeout"
	default:
		var networkError net.Error
		if errors.As(err, &networkError) && networkError.Timeout() {
			category = "timeout"
		}
	}
	return &avatarFetchError{category: category}
}

func fetchUpstreamAvatarWithClient(ctx context.Context, rawURL string, client *http.Client, allowPrivate bool) ([]byte, error) {
	if err := validateAvatarURL(rawURL, allowPrivate); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, &avatarFetchError{category: "request error"}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, sanitizeAvatarFetchError(err)
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
	fetch   func(context.Context, string, bool) ([]byte, error)
	timeout time.Duration
	logger  *slog.Logger
}

func NewAvatarManager(queries AvatarQueries) *AvatarManager {
	return &AvatarManager{queries: queries, fetch: fetchUpstreamAvatar, timeout: 10 * time.Second, logger: slog.Default()}
}

// Refresh fetches the avatar the provider currently hands out and applies it to
// the account under the activation rules in shouldActivate. A URL equal to the
// one the stored avatar was fetched from is not downloaded again. Failures are
// logged and not returned, so the sign-in, link or enrollment that called it
// completes either way; a failed fetch keeps the stored avatar and leaves the
// URL unrecorded so the next sign-in tries again.
func (m *AvatarManager) Refresh(ctx context.Context, accountID int32, provider Provider, delivery AvatarDelivery, resolver AvatarResolver) {
	ctx, cancel := context.WithTimeout(ctx, m.timeout)
	defer cancel()

	avatarURL := delivery.URL
	if avatarURL == "" && resolver != nil && delivery.Opaque != nil {
		var err error
		avatarURL, err = resolver.ResolveAvatar(ctx, provider, delivery)
		if err != nil {
			m.logger.WarnContext(ctx, "federation: upstream avatar resolution failed", "account_id", accountID, "err", err)
			return
		}
	}
	if avatarURL == "" {
		return
	}
	sources, err := m.queries.ListAvatarSourcesByAccount(ctx, accountID)
	if err != nil {
		m.logPersistenceFailure(ctx, "source list failed", accountID, provider, err)
		return
	}
	source := "upstream:" + provider.Slug
	var stored *db.ListAvatarSourcesByAccountRow
	for i := range sources {
		if sources[i].Source == source {
			stored = &sources[i]
			break
		}
	}
	changed := false
	if stored == nil || !stored.UpstreamUrl.Valid || stored.UpstreamUrl.String != avatarURL {
		raw, err := m.fetch(ctx, avatarURL, delivery.AllowPrivateNetwork)
		if err != nil {
			m.logger.WarnContext(ctx, "federation: upstream avatar fetch failed", "account_id", accountID, "err", err)
			return
		}
		processed, etag, err := avatarpkg.Process(raw)
		if err != nil {
			m.logger.WarnContext(ctx, "federation: upstream avatar process failed", "account_id", accountID, "err", err)
			return
		}
		providerID := provider.ID
		if err := m.queries.UpsertAvatarSource(ctx, db.UpsertAvatarSourceParams{
			AccountID: accountID, Source: source, Bytes: processed,
			ContentType: pgtype.Text{String: "image/webp", Valid: true},
			Etag:        pgtype.Text{String: etag, Valid: true}, IdpID: &providerID,
			UpstreamUrl: pgtype.Text{String: avatarURL, Valid: true},
		}); err != nil {
			m.logPersistenceFailure(ctx, "source upsert failed", accountID, provider, err)
			return
		}
		changed = stored == nil || !stored.Etag.Valid || stored.Etag.String != etag
	}
	account, err := m.queries.GetAccountByID(ctx, accountID)
	if err != nil {
		m.logPersistenceFailure(ctx, "account lookup failed", accountID, provider, err)
		return
	}
	if !shouldActivate(account, source, changed) {
		return
	}
	if err := m.queries.SetActiveAvatar(ctx, db.SetActiveAvatarParams{Source: source, AccountID: accountID}); err != nil {
		m.logPersistenceFailure(ctx, "activation failed", accountID, provider, err)
	}
}

// shouldActivate reports whether the stored upstream avatar under source should
// be made (or kept, with a fresh etag) the account's active avatar. A new
// picture for the active source always refreshes it. Otherwise an upstream
// avatar only fills an empty slot (never set, or "none") on an account whose
// user has never picked an avatar; it never replaces another source.
func shouldActivate(account db.Account, source string, contentChanged bool) bool {
	active := account.AvatarSource
	if active.Valid && active.String == source {
		return contentChanged
	}
	empty := !active.Valid || active.String == "none"
	return empty && !account.AvatarSelectedAt.Valid
}

func (m *AvatarManager) logPersistenceFailure(ctx context.Context, operation string, accountID int32, provider Provider, err error) {
	m.logger.WarnContext(ctx, "federation: upstream avatar "+operation,
		"account_id", accountID,
		"provider_id", provider.ID,
		"provider_slug", provider.Slug,
		"err", err,
	)
}
