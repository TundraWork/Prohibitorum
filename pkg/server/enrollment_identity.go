package server

// Shared identity-write helpers for invitation completion. Both finish paths
// (passkey at handle_enrollment.go, password+TOTP at
// handle_enrollment_password_totp.go) call them inside the same transaction
// that consumes the invite and creates the account, so a failure anywhere
// rolls back and the invitation stays unconsumed.

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/federation"
)

// recheckInviteIdentityProvider re-reads the provider an invite snapshot was
// written by, inside the completion transaction. It must still be enabled and
// not link_only, and — when the invitation carries a binding — match it. Every
// failure projects to provider_not_ready; the completion rolls back.
func recheckInviteIdentityProvider(ctx context.Context, q db.Querier, row db.Enrollment) (federation.Provider, error) {
	if !row.FederatedUpstreamIdpID.Valid || row.FederatedUpstreamIdpID.Int64 <= 0 {
		return federation.Provider{}, authn.ErrProviderNotReady()
	}
	provider, err := federation.NewProviderStore(q).ByIDForEnrollmentGate(ctx, row.FederatedUpstreamIdpID.Int64)
	if err != nil || provider.ID != row.FederatedUpstreamIdpID.Int64 || provider.Disabled || provider.Mode == federation.ModeLinkOnly {
		return federation.Provider{}, authn.ErrProviderNotReady()
	}
	if row.FederatedUpstreamIdpSlug.Valid && row.FederatedUpstreamIdpSlug.String != "" && row.FederatedUpstreamIdpSlug.String != provider.Slug {
		return federation.Provider{}, authn.ErrProviderNotReady()
	}
	return provider, nil
}

// insertInviteIdentityTx writes the invite snapshot as a confirmed
// account_identity row. The (iss, sub) conflict check mirrors the federation
// resolver: a lost race fails with the public identity-conflict error so the
// transaction — and with it the invite consumption — rolls back.
func insertInviteIdentityTx(ctx context.Context, q db.Querier, w audit.Writer, accountID int32, row db.Enrollment) (int64, error) {
	if !row.FederatedUpstreamIdpID.Valid || !row.FederatedUpstreamIss.Valid ||
		!row.FederatedUpstreamSub.Valid || len(row.FederatedUpstreamData) == 0 {
		return 0, authn.ErrCeremonyState()
	}
	provider, err := recheckInviteIdentityProvider(ctx, q, row)
	if err != nil {
		audit.RecordOrLog(ctx, w, audit.Record{
			Factor: audit.FactorEnrollment,
			Event:  audit.EventFail,
			Detail: map[string]any{"reason": "provider_unavailable"},
		})
		return 0, err
	}
	_, err = q.GetAccountIdentityByIssuerSub(ctx, db.GetAccountIdentityByIssuerSubParams{
		UpstreamIss: row.FederatedUpstreamIss.String,
		UpstreamSub: row.FederatedUpstreamSub.String,
	})
	if err == nil {
		audit.RecordOrLog(ctx, w, audit.Record{
			Factor: audit.FactorEnrollment,
			Event:  audit.EventFail,
			Detail: map[string]any{"reason": "identity_conflict"},
		})
		return 0, authn.ErrFederationIdentityConflict(provider.DisplayName)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("enrollment: check invite identity: %w", err)
	}
	identity, err := q.InsertAccountIdentity(ctx, db.InsertAccountIdentityParams{
		AccountID:     accountID,
		UpstreamIdpID: provider.ID,
		UpstreamIss:   row.FederatedUpstreamIss.String,
		UpstreamSub:   row.FederatedUpstreamSub.String,
		UpstreamData:  append([]byte(nil), row.FederatedUpstreamData...),
	})
	if err != nil {
		if isUniqueViolation(err) {
			audit.RecordOrLog(ctx, w, audit.Record{
				Factor: audit.FactorEnrollment,
				Event:  audit.EventFail,
				Detail: map[string]any{"reason": "identity_conflict"},
			})
			return 0, authn.ErrFederationIdentityConflict(provider.DisplayName)
		}
		return 0, fmt.Errorf("enrollment: insert invite identity: %w", err)
	}
	if err := q.ConfirmAccountIdentity(ctx, identity.ID); err != nil {
		return 0, fmt.Errorf("enrollment: confirm invite identity: %w", err)
	}
	audit.RecordOrLog(ctx, w, audit.Record{
		AccountID: &accountID,
		Factor:    audit.FactorEnrollment,
		Event:     audit.EventLink,
		Detail: map[string]any{
			"idp_slug":     provider.Slug,
			"upstream_iss": row.FederatedUpstreamIss.String,
			"upstream_sub": row.FederatedUpstreamSub.String,
		},
	})
	return identity.ID, nil
}

// inviteSnapshotAvatar returns the snapshot's avatar URL for the post-commit
// refresh, or "" when the snapshot carries none.
func inviteSnapshotAvatar(row db.Enrollment) string {
	if row.FederatedAvatarUrl.Valid {
		return row.FederatedAvatarUrl.String
	}
	return ""
}
