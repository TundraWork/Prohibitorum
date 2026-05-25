package authn

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/db"
)

// fakeThrottleQueries is an in-memory ThrottleQueries used by these tests.
// Keys are "<accountID>:<factor>". Mirrors the sqlc UPSERT semantics: a
// missing row reports pgx.ErrNoRows from GetAuthThrottle and replaces a row
// on UpsertAuthThrottle, regardless of prior state.
type fakeThrottleQueries struct {
	rows map[string]db.AuthThrottle
}

func newFakeThrottleQueries() *fakeThrottleQueries {
	return &fakeThrottleQueries{rows: map[string]db.AuthThrottle{}}
}

func (f *fakeThrottleQueries) key(accountID int32, factor string) string {
	return string(rune(accountID)) + ":" + factor
}

func (f *fakeThrottleQueries) GetAuthThrottle(_ context.Context, arg db.GetAuthThrottleParams) (db.AuthThrottle, error) {
	row, ok := f.rows[f.key(arg.AccountID, arg.Factor)]
	if !ok {
		return db.AuthThrottle{}, pgx.ErrNoRows
	}
	return row, nil
}

func (f *fakeThrottleQueries) UpsertAuthThrottle(_ context.Context, arg db.UpsertAuthThrottleParams) (db.AuthThrottle, error) {
	row := db.AuthThrottle{
		AccountID:      arg.AccountID,
		Factor:         arg.Factor,
		FailedAttempts: arg.FailedAttempts,
		WindowStart:    arg.WindowStart,
		LockedUntil:    arg.LockedUntil,
	}
	f.rows[f.key(arg.AccountID, arg.Factor)] = row
	return row, nil
}

func (f *fakeThrottleQueries) ResetAuthThrottle(_ context.Context, arg db.ResetAuthThrottleParams) error {
	delete(f.rows, f.key(arg.AccountID, arg.Factor))
	return nil
}

// newThrottleAt builds a Throttle whose clock is pinned to t.
func newThrottleAt(q ThrottleQueries, schedule []time.Duration, t time.Time) *Throttle {
	th := NewThrottle(q, schedule)
	th.now = func() time.Time { return t }
	return th
}

func TestThrottle_NoRowMeansUnlocked(t *testing.T) {
	q := newFakeThrottleQueries()
	th := NewThrottle(q, []time.Duration{0, 0, time.Second})
	retry, err := th.CheckLocked(context.Background(), 42, "password")
	if err != nil {
		t.Fatalf("CheckLocked on empty store: want nil err, got %v", err)
	}
	if retry != 0 {
		t.Fatalf("CheckLocked retry: want 0, got %v", retry)
	}
}

func TestThrottle_RegisterFailureFollowsSchedule(t *testing.T) {
	schedule := []time.Duration{0, 0, time.Second, 2 * time.Second, 4 * time.Second}
	q := newFakeThrottleQueries()
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	th := newThrottleAt(q, schedule, now)
	want := []time.Duration{0, 0, time.Second, 2 * time.Second, 4 * time.Second, 4 * time.Second}
	for i, expected := range want {
		got, err := th.RegisterFailure(context.Background(), 1, "password")
		if err != nil {
			t.Fatalf("attempt %d: RegisterFailure error: %v", i+1, err)
		}
		if got != expected {
			t.Fatalf("attempt %d: lockout want %v, got %v", i+1, expected, got)
		}
	}
	// After 6 failures, row should show failed_attempts=6 and locked_until=now+4s.
	row, err := q.GetAuthThrottle(context.Background(), db.GetAuthThrottleParams{AccountID: 1, Factor: "password"})
	if err != nil {
		t.Fatalf("GetAuthThrottle after 6 failures: %v", err)
	}
	if row.FailedAttempts != 6 {
		t.Fatalf("FailedAttempts: want 6, got %d", row.FailedAttempts)
	}
	if !row.LockedUntil.Valid || !row.LockedUntil.Time.Equal(now.Add(4*time.Second)) {
		t.Fatalf("LockedUntil: want %v, got %+v", now.Add(4*time.Second), row.LockedUntil)
	}
}

func TestThrottle_RegisterFailurePreservesWindowStart(t *testing.T) {
	schedule := []time.Duration{0, 0, time.Second}
	q := newFakeThrottleQueries()
	first := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	th := newThrottleAt(q, schedule, first)
	if _, err := th.RegisterFailure(context.Background(), 1, "password"); err != nil {
		t.Fatalf("first RegisterFailure: %v", err)
	}
	// Advance the clock and fail again; window_start should remain pinned to
	// the very first failure so window-based audit reasoning stays sound.
	th.now = func() time.Time { return first.Add(30 * time.Second) }
	if _, err := th.RegisterFailure(context.Background(), 1, "password"); err != nil {
		t.Fatalf("second RegisterFailure: %v", err)
	}
	row, _ := q.GetAuthThrottle(context.Background(), db.GetAuthThrottleParams{AccountID: 1, Factor: "password"})
	if !row.WindowStart.Valid || !row.WindowStart.Time.Equal(first) {
		t.Fatalf("WindowStart: want %v, got %+v", first, row.WindowStart)
	}
}

func TestThrottle_ResetClearsRow(t *testing.T) {
	q := newFakeThrottleQueries()
	th := NewThrottle(q, []time.Duration{0, 0, time.Second})
	if _, err := th.RegisterFailure(context.Background(), 1, "password"); err != nil {
		t.Fatalf("RegisterFailure: %v", err)
	}
	if _, ok := q.rows[q.key(1, "password")]; !ok {
		t.Fatal("RegisterFailure did not persist a row")
	}
	if err := th.Reset(context.Background(), 1, "password"); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if _, ok := q.rows[q.key(1, "password")]; ok {
		t.Fatal("Reset did not delete the row")
	}
	// Reset on an empty store must be a no-op (the underlying DELETE is
	// idempotent so the helper should mirror that).
	if err := th.Reset(context.Background(), 1, "password"); err != nil {
		t.Fatalf("Reset on absent row: %v", err)
	}
}

func TestThrottle_CheckLockedReturnsRetryAfter(t *testing.T) {
	schedule := []time.Duration{0, 0, 0, 10 * time.Second}
	q := newFakeThrottleQueries()
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	th := newThrottleAt(q, schedule, now)
	// Four failures → index 3 → 10s lockout.
	for i := 0; i < 4; i++ {
		if _, err := th.RegisterFailure(context.Background(), 7, "totp"); err != nil {
			t.Fatalf("RegisterFailure %d: %v", i+1, err)
		}
	}
	// Move the clock forward 3 seconds; we should still be locked for ~7s.
	th.now = func() time.Time { return now.Add(3 * time.Second) }
	retry, err := th.CheckLocked(context.Background(), 7, "totp")
	if err == nil {
		t.Fatal("CheckLocked during lockout: want error, got nil")
	}
	ae := AsAuthError(err)
	if ae == nil || ae.Code != "factor_locked" {
		t.Fatalf("CheckLocked error: want factor_locked AuthError, got %v", err)
	}
	if ae.RetryAfter != 7*time.Second {
		t.Fatalf("AuthError.RetryAfter: want 7s, got %v", ae.RetryAfter)
	}
	if retry != 7*time.Second {
		t.Fatalf("CheckLocked retry: want 7s, got %v", retry)
	}
	// After lockout expires, CheckLocked must return (0, nil) even though
	// the row is still present.
	th.now = func() time.Time { return now.Add(time.Hour) }
	retry, err = th.CheckLocked(context.Background(), 7, "totp")
	if err != nil {
		t.Fatalf("CheckLocked after lockout: want nil err, got %v", err)
	}
	if retry != 0 {
		t.Fatalf("CheckLocked retry after lockout: want 0, got %v", retry)
	}
}

func TestThrottle_CheckLockedSurfacesUnknownError(t *testing.T) {
	bad := errors.New("boom")
	q := &errorThrottleQueries{getErr: bad}
	th := NewThrottle(q, []time.Duration{time.Second})
	_, err := th.CheckLocked(context.Background(), 1, "password")
	if !errors.Is(err, bad) {
		t.Fatalf("CheckLocked must propagate non-ErrNoRows errors, got %v", err)
	}
}

// errorThrottleQueries lets us assert error propagation without faking the
// whole row store.
type errorThrottleQueries struct {
	getErr error
}

func (e *errorThrottleQueries) GetAuthThrottle(context.Context, db.GetAuthThrottleParams) (db.AuthThrottle, error) {
	return db.AuthThrottle{}, e.getErr
}

func (e *errorThrottleQueries) UpsertAuthThrottle(context.Context, db.UpsertAuthThrottleParams) (db.AuthThrottle, error) {
	return db.AuthThrottle{}, nil
}

func (e *errorThrottleQueries) ResetAuthThrottle(context.Context, db.ResetAuthThrottleParams) error {
	return nil
}

// Sanity: pgtype.Timestamptz zero value is correctly invalid.
func TestThrottle_NullLockedUntilIsUnlocked(t *testing.T) {
	q := newFakeThrottleQueries()
	q.rows[q.key(1, "password")] = db.AuthThrottle{
		AccountID:      1,
		Factor:         "password",
		FailedAttempts: 1,
		WindowStart:    pgtype.Timestamptz{Time: time.Now(), Valid: true},
		LockedUntil:    pgtype.Timestamptz{Valid: false},
	}
	th := NewThrottle(q, []time.Duration{0, time.Second})
	retry, err := th.CheckLocked(context.Background(), 1, "password")
	if err != nil {
		t.Fatalf("CheckLocked with null locked_until: %v", err)
	}
	if retry != 0 {
		t.Fatalf("retry: want 0, got %v", retry)
	}
}
