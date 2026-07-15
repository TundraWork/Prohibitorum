package federation

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"prohibitorum/pkg/audit"
)

func ApplyInviteOnlyForTest(
	ctx context.Context,
	q ModesQueries,
	w audit.Writer,
	idp *Provider,
	identity *VerifiedIdentity,
	enrollmentToken string,
	pool *pgxpool.Pool,
) (ResolveOutcome, error) {
	resolverIDP, err := resolverProviderFromProvider(*idp)
	if err != nil {
		return ResolveOutcome{}, err
	}
	return applyInviteOnly(ctx, q, w, &resolverIDP, identity, enrollmentToken, pool)
}
