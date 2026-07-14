package federation

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/db"
)

func ApplyInviteOnlyForTest(
	ctx context.Context,
	q ModesQueries,
	w audit.Writer,
	idp *db.UpstreamIdp,
	identity *VerifiedIdentity,
	enrollmentToken string,
	pool *pgxpool.Pool,
) (ResolveOutcome, error) {
	return applyInviteOnly(ctx, q, w, idp, identity, enrollmentToken, pool)
}
