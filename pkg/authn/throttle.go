// Package authn — throttle.go
//
// Persistent exponential-backoff helper around the auth_throttle table.
// Used by the password, TOTP, and recovery-code verify paths, plus the
// sudo step-up paths for those factors. All schedule decisions live in
// configx; this file only walks the schedule and persists state.
//
// Semantics:
//
//   - CheckLocked is read-only; it returns the remaining lockout duration
//     paired with ErrFactorLocked, or (0, nil) when the (account, factor)
//     pair has no row or has an expired lockout.
//   - RegisterFailure delegates the increment AND the lockout-deadline
//     computation to a single Postgres round-trip (BumpAuthThrottle). The
//     pre-bundle code did read-then-UPSERT, which under K-way concurrency
//     both lost increments (K racers all read the same prior count) and
//     picked the lockout from the racing snapshot rather than the
//     post-increment value. The schedule travels as a bigint[] of
//     microsecond durations; SQL indexes into it via the just-incremented
//     failed_attempts so the lockout always matches the persisted counter.
//   - Reset deletes the row on a successful verify. Idempotent — the
//     underlying DELETE is a no-op against a missing row.
package authn

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/db"
)

// ThrottleQueries is the locally-declared subset of db.Querier the throttle
// needs. Tests supply an in-memory fake without re-implementing the full
// sqlc-generated surface (mirrors the SessionQueries pattern in pkg/session).
type ThrottleQueries interface {
	GetAuthThrottle(ctx context.Context, arg db.GetAuthThrottleParams) (db.AuthThrottle, error)
	BumpAuthThrottle(ctx context.Context, arg db.BumpAuthThrottleParams) (db.BumpAuthThrottleRow, error)
	ResetAuthThrottle(ctx context.Context, arg db.ResetAuthThrottleParams) error
}

// Throttle drives the per-(account, factor) lockout state machine.
type Throttle struct {
	q             ThrottleQueries
	schedule      []time.Duration
	scheduleMicro []int64
	audit         audit.Writer
	now           func() time.Time
}

// NewThrottle constructs a Throttle with the supplied schedule. The schedule
// is indexed by (failed_attempts - 1) and clamps to its last entry; an empty
// schedule falls back to a tiny conservative ladder so a misconfigured
// deployment still gets some back-off.
//
// The audit.Writer is used to emit a single EventFactorLocked event on the
// transition from "unlocked (or expired lockout)" → "now locked". A nil
// writer is tolerated for tests and pure-helper use; production wiring in
// pkg/server always supplies one.
func NewThrottle(q ThrottleQueries, schedule []time.Duration, w audit.Writer) *Throttle {
	if len(schedule) == 0 {
		schedule = []time.Duration{0, 0, time.Second, 5 * time.Second, 15 * time.Minute}
	}
	micros := make([]int64, len(schedule))
	for i, d := range schedule {
		if d < 0 {
			d = 0
		}
		micros[i] = int64(d / time.Microsecond)
	}
	return &Throttle{q: q, schedule: schedule, scheduleMicro: micros, audit: w, now: time.Now}
}

// CheckLocked reports whether the (account, factor) pair is currently in a
// lockout window. Returns (0, nil) when no row exists or the lockout has
// expired; returns (retryAfter, *AuthError) otherwise so callers can both
// short-circuit and surface a Retry-After hint.
func (t *Throttle) CheckLocked(ctx context.Context, accountID int32, factor string) (time.Duration, error) {
	row, err := t.q.GetAuthThrottle(ctx, db.GetAuthThrottleParams{AccountID: accountID, Factor: factor})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nil
		}
		return 0, err
	}
	if !row.LockedUntil.Valid {
		return 0, nil
	}
	now := t.now()
	if !row.LockedUntil.Time.After(now) {
		return 0, nil
	}
	retry := row.LockedUntil.Time.Sub(now)
	return retry, ErrFactorLocked(retry)
}

// RegisterFailure bumps the consecutive-failure counter for (account, factor)
// in a single atomic SQL round-trip and returns the lockout duration applied
// (zero when the current attempt is still within the schedule's free-attempt
// prefix). The schedule is passed by value with the call, so a config reload
// changes the next failure's behaviour without any cache invalidation.
//
// On the transition from "unlocked (or expired lockout)" → "now locked",
// emits an EventFactorLocked audit event so SOC pipelines can detect
// lockouts without pattern-matching fail-row counts (Bundle-3 Low-1).
// The pre-bump read is racy under K-way concurrency — a missed/duplicate
// emission is acceptable for a defense-in-depth signal; the lockout
// computation itself remains atomic in BumpAuthThrottle.
func (t *Throttle) RegisterFailure(ctx context.Context, accountID int32, factor string) (time.Duration, error) {
	// Snapshot the pre-bump lockout state. A racy read is fine: this only
	// gates audit emission, not the lockout decision. Worst case is a
	// missed/duplicate factor_locked event under heavy concurrency.
	wasLocked := false
	if prior, perr := t.q.GetAuthThrottle(ctx, db.GetAuthThrottleParams{AccountID: accountID, Factor: factor}); perr == nil {
		if prior.LockedUntil.Valid && prior.LockedUntil.Time.After(t.now()) {
			wasLocked = true
		}
	}

	row, err := t.q.BumpAuthThrottle(ctx, db.BumpAuthThrottleParams{
		AccountID:      accountID,
		Factor:         factor,
		ScheduleMicros: t.scheduleMicro,
	})
	if err != nil {
		return 0, err
	}
	if !row.LockedUntil.Valid {
		return 0, nil
	}
	retry := row.LockedUntil.Time.Sub(t.now())
	if !wasLocked {
		audit.RecordOrLog(ctx, t.audit, audit.Record{
			AccountID: &accountID,
			Factor:    audit.Factor(factor),
			Event:     audit.EventFactorLocked,
			Detail: map[string]any{
				"failed_attempts": row.FailedAttempts,
				"until":           row.LockedUntil.Time,
			},
		})
	}
	return retry, nil
}

// Reset deletes the throttle row on a successful verify. No-op when the row
// is already absent.
func (t *Throttle) Reset(ctx context.Context, accountID int32, factor string) error {
	return t.q.ResetAuthThrottle(ctx, db.ResetAuthThrottleParams{AccountID: accountID, Factor: factor})
}
