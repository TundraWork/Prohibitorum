package server

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"prohibitorum/pkg/db"
	"prohibitorum/pkg/protocol/oidc"
)

// UpstreamIDPDeleteSummary reports what one completed upstream IdP delete
// removed, counted inside the deleting transaction itself.
type UpstreamIDPDeleteSummary struct {
	Slug                      string
	ProviderID                int64
	IdentityScope             string
	UnlinkedAccounts          int
	DeletedPendingInvitations int32
	RemovedClientScopes       int64
	RemovedConsentScopes      int64
}

func (s UpstreamIDPDeleteSummary) String() string {
	return fmt.Sprintf(
		"Deleted upstream IdP %q (id=%d); unlinked %d account(s); deleted %d pending invitation(s); removed %s from %d client(s) and %d consent(s)",
		s.Slug, s.ProviderID, s.UnlinkedAccounts, s.DeletedPendingInvitations,
		s.IdentityScope, s.RemovedClientScopes, s.RemovedConsentScopes)
}

// UpstreamIDPDeletePlan is the pre-delete read of what deleting this provider
// will remove — the numbers the admin dialog and the CLI confirmation show
// before the operator commits.
type UpstreamIDPDeletePlan struct {
	Slug               string
	ProviderID         int64
	IdentityScope      string
	LinkedAccounts     int64
	PendingInvitations int32
}

// PlanUpstreamIDPDelete reads the confirmation counts for one provider.
// Returns pgx.ErrNoRows when the slug matches nothing.
func PlanUpstreamIDPDelete(ctx context.Context, pool *pgxpool.Pool, slug string) (UpstreamIDPDeletePlan, error) {
	q := db.New(pool)
	row, err := q.GetUpstreamIDPBySlugAny(ctx, slug)
	if err != nil {
		return UpstreamIDPDeletePlan{}, err
	}
	linked, err := q.CountAccountsLinkedToUpstreamIDP(ctx, row.ID)
	if err != nil {
		return UpstreamIDPDeletePlan{}, fmt.Errorf("count linked accounts: %w", err)
	}
	pendingRows, err := q.CountPendingInvitationsByUpstreamIDP(ctx)
	if err != nil {
		return UpstreamIDPDeletePlan{}, fmt.Errorf("count pending invitations: %w", err)
	}
	var pending int32
	for _, p := range pendingRows {
		if p.UpstreamIdpID.Valid && p.UpstreamIdpID.Int64 == row.ID {
			pending = p.PendingInvitations
		}
	}
	return UpstreamIDPDeletePlan{
		Slug:               row.Slug,
		ProviderID:         row.ID,
		IdentityScope:      oidc.IdentityScopePrefix + row.Slug,
		LinkedAccounts:     linked,
		PendingInvitations: pending,
	}, nil
}

// DeleteUpstreamIDPWithDependents hard-deletes an upstream IdP together with
// everything that cannot outlive it — linked account identities, pending
// invitations whose verified snapshot points at it, and its identity:<slug>
// scopes on clients and consents — in one transaction, so a failure part-way
// leaves nothing half-deleted. Shared by the admin API and the CLI delete verb;
// returns pgx.ErrNoRows when the slug matches nothing.
func DeleteUpstreamIDPWithDependents(ctx context.Context, pool *pgxpool.Pool, slug string) (UpstreamIDPDeleteSummary, error) {
	q := db.New(pool)
	row, err := q.GetUpstreamIDPBySlugAny(ctx, slug)
	if err != nil {
		return UpstreamIDPDeleteSummary{}, err
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return UpstreamIDPDeleteSummary{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	qtx := q.WithTx(tx)

	// Pending invitations whose verified snapshot points here die with the
	// provider (the snapshot FK cascades). Counted before the deletes so the
	// summary can report what disappeared.
	pendingRows, err := qtx.CountPendingInvitationsByUpstreamIDP(ctx)
	if err != nil {
		return UpstreamIDPDeleteSummary{}, fmt.Errorf("count pending invitations: %w", err)
	}
	var deletedInvitations int32
	for _, pending := range pendingRows {
		if pending.UpstreamIdpID.Valid && pending.UpstreamIdpID.Int64 == row.ID {
			deletedInvitations = pending.PendingInvitations
		}
	}
	// account_identity carries a RESTRICT foreign key to upstream_idp, so the
	// provider can only go once its identities do; the accounts themselves
	// survive — only the sign-in link disappears.
	unlinked, err := qtx.DeleteAccountIdentitiesByUpstreamIDP(ctx, row.ID)
	if err != nil {
		return UpstreamIDPDeleteSummary{}, fmt.Errorf("delete identities: %w", err)
	}
	unlinkedAccounts := make(map[int32]struct{}, len(unlinked))
	for _, accountID := range unlinked {
		unlinkedAccounts[accountID] = struct{}{}
	}
	if err := qtx.DeleteUpstreamIDP(ctx, row.ID); err != nil {
		return UpstreamIDPDeleteSummary{}, fmt.Errorf("delete: %w", err)
	}
	// A provider created later with the same slug must not inherit what
	// applications and users allowed for this one.
	identityScope := oidc.IdentityScopePrefix + row.Slug
	clientsChanged, err := qtx.RemoveScopeFromOIDCClients(ctx, identityScope)
	if err != nil {
		return UpstreamIDPDeleteSummary{}, fmt.Errorf("remove client scope: %w", err)
	}
	consentsChanged, err := qtx.RemoveScopeFromConsents(ctx, identityScope)
	if err != nil {
		return UpstreamIDPDeleteSummary{}, fmt.Errorf("remove consent scope: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return UpstreamIDPDeleteSummary{}, fmt.Errorf("commit: %w", err)
	}
	return UpstreamIDPDeleteSummary{
		Slug:                      row.Slug,
		ProviderID:                row.ID,
		IdentityScope:             identityScope,
		UnlinkedAccounts:          len(unlinkedAccounts),
		DeletedPendingInvitations: deletedInvitations,
		RemovedClientScopes:       clientsChanged,
		RemovedConsentScopes:      consentsChanged,
	}, nil
}
