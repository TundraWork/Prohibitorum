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
//   - RegisterFailure increments failed_attempts (creating the row on first
//     failure) and indexes the schedule by failed_attempts-1, clamping to
//     the last entry. window_start is set on first failure and otherwise
//     preserved across upserts so audit can reason about windows.
//   - Reset deletes the row on a successful verify. Idempotent — the
//     underlying DELETE is a no-op against a missing row.
package authn

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/db"
)

// ThrottleQueries is the locally-declared subset of db.Querier the throttle
// needs. Tests supply an in-memory fake without re-implementing the full
// sqlc-generated surface (mirrors the SessionQueries pattern in pkg/session).
type ThrottleQueries interface {
	GetAuthThrottle(ctx context.Context, arg db.GetAuthThrottleParams) (db.AuthThrottle, error)
	UpsertAuthThrottle(ctx context.Context, arg db.UpsertAuthThrottleParams) (db.AuthThrottle, error)
	ResetAuthThrottle(ctx context.Context, arg db.ResetAuthThrottleParams) error
}

// Throttle drives the per-(account, factor) lockout state machine.
type Throttle struct {
	q        ThrottleQueries
	schedule []time.Duration
	now      func() time.Time
}

// NewThrottle constructs a Throttle with the supplied schedule. The schedule
// is indexed by (failed_attempts - 1) and clamps to its last entry; an empty
// schedule falls back to a tiny conservative ladder so a misconfigured
// deployment still gets some back-off.
func NewThrottle(q ThrottleQueries, schedule []time.Duration) *Throttle {
	if len(schedule) == 0 {
		schedule = []time.Duration{0, 0, time.Second, 5 * time.Second, 15 * time.Minute}
	}
	return &Throttle{q: q, schedule: schedule, now: time.Now}
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
// and writes the new lockout deadline. Returns the lockout duration applied
// (zero when the current attempt is still within the schedule's free-attempt
// prefix). window_start is set to t.now() on first failure and otherwise
// preserved from the existing row.
func (t *Throttle) RegisterFailure(ctx context.Context, accountID int32, factor string) (time.Duration, error) {
	now := t.now()
	row, err := t.q.GetAuthThrottle(ctx, db.GetAuthThrottleParams{AccountID: accountID, Factor: factor})
	var failed int32
	var windowStart pgtype.Timestamptz
	switch {
	case err == nil:
		failed = row.FailedAttempts + 1
		windowStart = row.WindowStart
	case errors.Is(err, pgx.ErrNoRows):
		failed = 1
		windowStart = pgtype.Timestamptz{Time: now, Valid: true}
	default:
		return 0, err
	}

	idx := int(failed) - 1
	if idx >= len(t.schedule) {
		idx = len(t.schedule) - 1
	}
	lockout := t.schedule[idx]
	lockedUntil := pgtype.Timestamptz{Valid: false}
	if lockout > 0 {
		lockedUntil = pgtype.Timestamptz{Time: now.Add(lockout), Valid: true}
	}

	if _, err := t.q.UpsertAuthThrottle(ctx, db.UpsertAuthThrottleParams{
		AccountID:      accountID,
		Factor:         factor,
		FailedAttempts: failed,
		WindowStart:    windowStart,
		LockedUntil:    lockedUntil,
	}); err != nil {
		return 0, err
	}
	return lockout, nil
}

// Reset deletes the throttle row on a successful verify. No-op when the row
// is already absent.
func (t *Throttle) Reset(ctx context.Context, accountID int32, factor string) error {
	return t.q.ResetAuthThrottle(ctx, db.ResetAuthThrottleParams{AccountID: accountID, Factor: factor})
}
