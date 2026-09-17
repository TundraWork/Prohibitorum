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

type providerByIDLoader interface {
	GetUpstreamIDPByIDAny(context.Context, int64) (db.UpstreamIdp, error)
}

type providerForEnrollmentGateLoader interface {
	GetUpstreamIDPByIDForUpdate(context.Context, int64) (db.UpstreamIdp, error)
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

// ByID loads a provider row, including its sealed-secret readiness state, by
// its immutable database identifier. Lookup and storage failures are collapsed
// to ErrUnknownProvider just like BySlug.
func (s *ProviderStore) ByID(ctx context.Context, id int64) (Provider, error) {
	queries, ok := s.queries.(providerByIDLoader)
	if !ok {
		return Provider{}, ErrUnknownProvider
	}
	row, err := queries.GetUpstreamIDPByIDAny(ctx, id)
	if err != nil {
		return Provider{}, ErrUnknownProvider
	}
	return providerFromRow(row)
}

// ByIDForEnrollmentGate locks the immutable provider row until the caller's
// transaction commits or rolls back. Enrollment completion uses this gate so
// readiness updates cannot cross its commit boundary.
func (s *ProviderStore) ByIDForEnrollmentGate(ctx context.Context, id int64) (Provider, error) {
	queries, ok := s.queries.(providerForEnrollmentGateLoader)
	if !ok {
		return Provider{}, ErrUnknownProvider
	}
	row, err := queries.GetUpstreamIDPByIDForUpdate(ctx, id)
	if err != nil {
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

// boundInviteSlug returns the provider slug an invite is bound to, or "" when
// the invite is unbound and the invitee picks the provider.
//
// An empty stored value is a binding to nothing rather than a binding to the
// provider whose slug is "": the invitation API takes an empty
// expectedUpstreamIdpSlug as "no provider required" and skips its slug-exists
// validation for it, and the enrollment preview, the register/begin gate and
// the admin invitation list all read it back that way. Both redemption gates
// read the binding through here so they cannot disagree about which provider an
// invite may be redeemed through: while they did, an unbound invite passed
// start-federation and was then refused at the callback as a slug mismatch,
// after the invitee had already authenticated upstream.
func boundInviteSlug(enrollment db.Enrollment) string {
	if !enrollment.ExpectedUpstreamIdpSlug.Valid {
		return ""
	}
	return enrollment.ExpectedUpstreamIdpSlug.String
}

// InviteProvider resolves the provider an invite will be redeemed through.
// The invitee's selected slug (the provider query parameter on
// start-federation) is judged against the invite's binding per the api
// contract:
//
//   - invite bound to a slug: that slug wins; a differing selection is
//     rejected (audit reason invite_slug_mismatch),
//   - unbound invite: the selection is required (invite_not_federated
//     otherwise),
//   - either way the effective provider must not be link_only nor disabled —
//     link_only never creates accounts, on any entrypoint
//     (link_only_provision_denied).
func (s *ProviderStore) InviteProvider(ctx context.Context, token, selectedSlug string) (Provider, error) {
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

	bound := boundInviteSlug(enrollment)
	effective := bound
	if bound == "" {
		effective = selectedSlug
	} else if selectedSlug != "" && selectedSlug != bound {
		return Provider{}, NewFailure(FailureInviteSlugMismatch, map[string]any{
			"enrollment_expected_slug": bound,
		})
	}
	if effective == "" {
		return Provider{}, NewFailure(FailureInviteNotFederated, nil)
	}

	provider, err := s.BySlug(ctx, effective)
	if err != nil {
		return Provider{}, err
	}
	if provider.Disabled || provider.Mode == ModeLinkOnly {
		return Provider{}, NewFailure(FailureLinkOnlyProvisionDenied, map[string]any{
			"idp_slug": effective,
		})
	}
	return provider, nil
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
