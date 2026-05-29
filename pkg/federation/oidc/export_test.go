package oidc

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/db"
)

// ApplyInviteOnlyForTest exposes the unexported applyInviteOnly to the
// _test package so we can drive happy-path + negative branches directly
// (Resolve always routes invite_only with an empty token, which is the
// "no invite was supplied" branch — useless for testing the redemption
// happy path). pool is nil-safe; tests pass nil.
func ApplyInviteOnlyForTest(
	ctx context.Context,
	q ModesQueries,
	w audit.Writer,
	idp *db.UpstreamIdp,
	tokens *Tokens,
	enrollmentToken string,
	pool *pgxpool.Pool,
) (int32, bool, error) {
	return applyInviteOnly(ctx, q, w, idp, tokens, enrollmentToken, pool)
}
