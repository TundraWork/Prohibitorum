package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"prohibitorum/pkg/server"
)

// runUpstreamIDPDelete is the upstream-idp delete verb's body: hard-delete the
// provider with its dependents in one transaction, the same semantics as the
// admin API's delete.
func runUpstreamIDPDelete(ctx context.Context, pool *pgxpool.Pool, slug string) (server.UpstreamIDPDeleteSummary, error) {
	summary, err := server.DeleteUpstreamIDPWithDependents(ctx, pool, slug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return summary, fmt.Errorf("IdP %q not found", slug)
		}
		return summary, err
	}
	return summary, nil
}

// planUpstreamIDPDeleteOutput is the confirmation the CLI prints when --yes is
// missing: what this delete will remove, mirroring the admin dialog's copy.
func planUpstreamIDPDeleteOutput(ctx context.Context, pool *pgxpool.Pool, slug string) (string, error) {
	plan, err := server.PlanUpstreamIDPDelete(ctx, pool, slug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", fmt.Errorf("IdP %q not found", slug)
		}
		return "", err
	}
	return fmt.Sprintf(
		"Upstream IdP %q (id=%d) is linked to %d account(s); deleting it unlinks them (the accounts stay), "+
			"deletes %d pending invitation(s) whose identity was verified through it, "+
			"and removes %s from the applications and consents that were granted it. "+
			"A provider added later with the same slug starts fresh.",
		plan.Slug, plan.ProviderID, plan.LinkedAccounts, plan.PendingInvitations, plan.IdentityScope), nil
}
