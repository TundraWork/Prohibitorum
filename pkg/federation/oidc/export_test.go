package oidc

import (
	"context"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/federation/steam"
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
) (ResolveOutcome, error) {
	return applyInviteOnly(ctx, q, w, idp, tokens, enrollmentToken, pool)
}

// RunAvatarInheritForTest drives the unexported background avatar-inherit job
// synchronously so tests can assert its store / no-op behavior without spawning
// a goroutine. client may be nil — runAvatarInherit only falls back to UserInfo
// when the id_token Raw carries no picture claim.
func RunAvatarInheritForTest(f *Federator, ctx context.Context, client *Client, idp db.UpstreamIdp, tokens *Tokens, accountID int32) {
	f.runAvatarInherit(ctx, client, idp, tokens, accountID)
}

// SetAvatarFetchForTest swaps the Federator's upstream-picture fetcher for a
// stub, so the avatar-inherit job can run against canned bytes (no live image
// server, no SSRF dance).
func SetAvatarFetchForTest(f *Federator, fn func(ctx context.Context, url string, allowPrivate bool) ([]byte, error)) {
	f.avatarFetch = fn
}

// SetSteamSeamsForTest replaces the Federator's Steam verify and summary functions
// with stubs, so the Steam callback branch can be exercised without a live Steam
// endpoint. Mirror of SetAvatarFetchForTest.
func SetSteamSeamsForTest(
	f *Federator,
	verify func(ctx context.Context, params url.Values, expectedReturnTo string) (string, error),
	summary func(ctx context.Context, apiKey, steamID string) (steam.Summary, error),
) {
	f.steamVerify = verify
	f.steamSummary = summary
}
