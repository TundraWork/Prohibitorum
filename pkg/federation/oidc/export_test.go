package oidc

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/db"
)

// SetClientCacheTTLForTest lets tests in the _test package shrink (or expire)
// the per-Federator discovery cache TTL without touching the const. Setting
// to 0 forces every buildClient call to re-run discovery; setting to a
// negative duration evicts on the next access.
func SetClientCacheTTLForTest(f *Federator, d time.Duration) {
	f.clientCacheTTL = d
}

// ClientCacheLenForTest returns the number of live entries in the client
// cache. Useful for asserting eviction behavior.
func ClientCacheLenForTest(f *Federator) int {
	n := 0
	f.clientCache.Range(func(_, _ any) bool {
		n++
		return true
	})
	return n
}

// ClearClientCacheForTest evicts every entry from the client cache. Tests
// use this to simulate a "cold" cache after changing TTL without waiting.
func ClearClientCacheForTest(f *Federator) {
	f.clientCache.Range(func(k, _ any) bool {
		f.clientCache.Delete(k)
		return true
	})
}

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
