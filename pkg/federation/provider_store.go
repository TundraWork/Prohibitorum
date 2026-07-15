package federation

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/db"
)

var ErrUnknownProvider = errors.New("federation: unknown provider")

type ProviderQueries interface {
	GetUpstreamIDPBySlugAny(context.Context, string) (db.UpstreamIdp, error)
	GetEnrollmentByToken(context.Context, string) (db.Enrollment, error)
}

type ProviderStore struct {
	queries ProviderQueries
	now     func() time.Time
}

func NewProviderStore(queries ProviderQueries) *ProviderStore {
	return &ProviderStore{queries: queries, now: time.Now}
}

func (s *ProviderStore) BySlug(ctx context.Context, slug string) (Provider, error) {
	row, err := s.queries.GetUpstreamIDPBySlugAny(ctx, slug)
	if err != nil {
		// The slug is public input. Collapse absence and database failures to the
		// same opaque classification so begin/link cannot disclose store health
		// or configured-provider membership through their redirect codes.
		return Provider{}, ErrUnknownProvider
	}
	return providerFromRow(row)
}

func (s *ProviderStore) ByBinding(ctx context.Context, id int64, slug, protocol string) (Provider, error) {
	provider, err := s.BySlug(ctx, slug)
	if err != nil {
		return Provider{}, err
	}
	if provider.ID != id || provider.Protocol != protocol {
		return Provider{}, ErrUnknownProvider
	}
	return provider, nil
}

func (s *ProviderStore) InviteProvider(ctx context.Context, token string) (Provider, error) {
	enrollment, err := s.queries.GetEnrollmentByToken(ctx, token)
	if err != nil {
		return Provider{}, NewFailure(FailureInviteLookup, nil)
	}
	if enrollment.Intent != "invite" {
		return Provider{}, NewFailure(FailureInviteWrongIntent, map[string]any{"intent": enrollment.Intent})
	}
	if enrollment.ConsumedAt.Valid {
		return Provider{}, NewFailure(FailureInviteConsumed, nil)
	}
	if !enrollment.ExpiresAt.Valid || !enrollment.ExpiresAt.Time.After(s.now()) {
		return Provider{}, NewFailure(FailureInviteExpired, nil)
	}
	if !enrollment.ExpectedUpstreamIdpSlug.Valid || enrollment.ExpectedUpstreamIdpSlug.String == "" {
		return Provider{}, NewFailure(FailureInviteNotFederated, nil)
	}
	return s.BySlug(ctx, enrollment.ExpectedUpstreamIdpSlug.String)
}

func providerFromRow(row db.UpstreamIdp) (Provider, error) {
	provider := Provider{
		ID:           row.ID,
		Slug:         row.Slug,
		DisplayName:  row.DisplayName,
		Protocol:     row.Protocol,
		Mode:         row.Mode,
		Config:       append([]byte(nil), row.ProviderConfig...),
		SecretStatus: row.SecretStatus,
		Disabled:     row.Disabled,
	}
	if row.SecretValidatedAt.Valid {
		validatedAt := row.SecretValidatedAt.Time
		provider.SecretValidatedAt = &validatedAt
	}
	if row.KeyVersion.Valid {
		provider.Secret = &SealedSecret{
			Ciphertext: append([]byte(nil), row.SecretEnc...),
			Nonce:      append([]byte(nil), row.SecretNonce...),
			KeyVersion: row.KeyVersion.Int32,
		}
	}
	return provider, nil
}

func providerRow(provider Provider) (db.UpstreamIdp, error) {
	row := db.UpstreamIdp{
		ID:             provider.ID,
		Slug:           provider.Slug,
		DisplayName:    provider.DisplayName,
		Protocol:       provider.Protocol,
		Mode:           provider.Mode,
		ProviderConfig: append([]byte(nil), provider.Config...),
		SecretStatus:   provider.SecretStatus,
		Disabled:       provider.Disabled,
	}
	if provider.SecretValidatedAt != nil {
		row.SecretValidatedAt = pgtype.Timestamptz{Time: *provider.SecretValidatedAt, Valid: true}
	}
	if provider.Secret != nil {
		row.SecretEnc = append([]byte(nil), provider.Secret.Ciphertext...)
		row.SecretNonce = append([]byte(nil), provider.Secret.Nonce...)
		row.KeyVersion = pgtype.Int4{Int32: provider.Secret.KeyVersion, Valid: true}
	}
	return row, nil
}
