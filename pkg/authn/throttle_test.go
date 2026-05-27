package authn

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/db"
)

// recordingAudit captures audit emissions for assertion. The throttle is the
// only emitter inside pkg/authn that this package's tests need to observe;
// other packages use pkg/audit's writer directly.
type recordingAudit struct {
	mu      sync.Mutex
	records []audit.Record
}

func (r *recordingAudit) Record(_ context.Context, rec audit.Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, rec)
	return nil
}

func (r *recordingAudit) snapshot() []audit.Record {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]audit.Record, len(r.records))
	copy(out, r.records)
	return out
}

// fakeThrottleQueries is an in-memory ThrottleQueries used by these tests.
// Keys are "<accountID>:<factor>". Mirrors the sqlc UPSERT semantics: a
// missing row reports pgx.ErrNoRows from GetAuthThrottle and replaces a row
// on UpsertAuthThrottle, regardless of prior state.
// fakeThrottleQueries models the production SQL atomically: BumpAuthThrottle
// reads, increments, computes the lockout from the just-incremented count,
// and writes back under a single mutex. The mutex is the in-memory analogue
// of Postgres's row-level lock on the UPSERT; without it the K-way race test
// would be undefined behaviour rather than a regression check.
type fakeThrottleQueries struct {
	mu        sync.Mutex
	rows      map[string]db.AuthThrottle
	clock     func() time.Time
	bumpCalls int
}

func newFakeThrottleQueries() *fakeThrottleQueries {
	return &fakeThrottleQueries{rows: map[string]db.AuthThrottle{}, clock: time.Now}
}

func (f *fakeThrottleQueries) key(accountID int32, factor string) string {
	return string(rune(accountID)) + ":" + factor
}

func (f *fakeThrottleQueries) GetAuthThrottle(_ context.Context, arg db.GetAuthThrottleParams) (db.AuthThrottle, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	row, ok := f.rows[f.key(arg.AccountID, arg.Factor)]
	if !ok {
		return db.AuthThrottle{}, pgx.ErrNoRows
	}
	return row, nil
}

func (f *fakeThrottleQueries) BumpAuthThrottle(_ context.Context, arg db.BumpAuthThrottleParams) (db.BumpAuthThrottleRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bumpCalls++
	now := f.clock()
	key := f.key(arg.AccountID, arg.Factor)
	cur, ok := f.rows[key]
	if !ok {
		cur = db.AuthThrottle{
			AccountID:   arg.AccountID,
			Factor:      arg.Factor,
			WindowStart: pgtype.Timestamptz{Time: now, Valid: true},
		}
	}
	cur.FailedAttempts++
	idx := int(cur.FailedAttempts) - 1
	if idx >= len(arg.ScheduleMicros) {
		idx = len(arg.ScheduleMicros) - 1
	}
	if idx < 0 || arg.ScheduleMicros[idx] <= 0 {
		cur.LockedUntil = pgtype.Timestamptz{Valid: false}
	} else {
		d := time.Duration(arg.ScheduleMicros[idx]) * time.Microsecond
		cur.LockedUntil = pgtype.Timestamptz{Time: now.Add(d), Valid: true}
	}
	f.rows[key] = cur
	return db.BumpAuthThrottleRow{FailedAttempts: cur.FailedAttempts, LockedUntil: cur.LockedUntil}, nil
}

func (f *fakeThrottleQueries) ResetAuthThrottle(_ context.Context, arg db.ResetAuthThrottleParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.rows, f.key(arg.AccountID, arg.Factor))
	return nil
}

// newThrottleAt builds a Throttle whose clock is pinned to t. The same
// clock is shared with the fake's BumpAuthThrottle so the lockout deadline
// is computed against the pinned now, matching the production semantics
// where Postgres now() runs server-side under the same transaction.
func newThrottleAt(q ThrottleQueries, schedule []time.Duration, t time.Time) *Throttle {
	th := NewThrottle(q, schedule, nil)
	th.now = func() time.Time { return t }
	if f, ok := q.(*fakeThrottleQueries); ok {
		f.clock = func() time.Time { return t }
	}
	return th
}

func TestThrottle_NoRowMeansUnlocked(t *testing.T) {
	q := newFakeThrottleQueries()
	th := NewThrottle(q, []time.Duration{0, 0, time.Second}, nil)
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
	th := NewThrottle(q, []time.Duration{0, 0, time.Second}, nil)
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
	th := NewThrottle(q, []time.Duration{time.Second}, nil)
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

func (e *errorThrottleQueries) BumpAuthThrottle(context.Context, db.BumpAuthThrottleParams) (db.BumpAuthThrottleRow, error) {
	return db.BumpAuthThrottleRow{}, nil
}

func (e *errorThrottleQueries) ResetAuthThrottle(context.Context, db.ResetAuthThrottleParams) error {
	return nil
}

// TestThrottle_RegisterFailureKWayRace exercises the regression that the
// audit's Race Critical-3 finding called out: K parallel RegisterFailure
// calls for the same (account, factor) MUST increment by K rather than 1.
// The pre-bundle read-then-UPSERT lost increments because each racer
// read the same prior count and last-write-wins clobbered to that count+1.
// The fake now models the production SQL by holding a mutex across
// read+increment+write, mirroring Postgres's row-level UPSERT lock.
func TestThrottle_RegisterFailureKWayRace(t *testing.T) {
	const K = 10
	schedule := []time.Duration{0, 0, time.Second, 2 * time.Second, 4 * time.Second}
	q := newFakeThrottleQueries()
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	th := newThrottleAt(q, schedule, now)

	var wg sync.WaitGroup
	wg.Add(K)
	errs := make(chan error, K)
	for i := 0; i < K; i++ {
		go func() {
			defer wg.Done()
			if _, err := th.RegisterFailure(context.Background(), 42, "password"); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("RegisterFailure error: %v", err)
	}

	row, err := q.GetAuthThrottle(context.Background(), db.GetAuthThrottleParams{AccountID: 42, Factor: "password"})
	if err != nil {
		t.Fatalf("GetAuthThrottle: %v", err)
	}
	if row.FailedAttempts != int32(K) {
		t.Fatalf("FailedAttempts after %d racing failures: want %d, got %d", K, K, row.FailedAttempts)
	}
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
	th := NewThrottle(q, []time.Duration{0, time.Second}, nil)
	retry, err := th.CheckLocked(context.Background(), 1, "password")
	if err != nil {
		t.Fatalf("CheckLocked with null locked_until: %v", err)
	}
	if retry != 0 {
		t.Fatalf("retry: want 0, got %v", retry)
	}
}

// TestThrottle_FactorLockedEmittedOnTransition verifies the Bundle-3 Low-1
// fix: a factor_locked audit event MUST be emitted exactly once on the
// transition from unlocked → locked, and MUST NOT re-fire on subsequent
// failures while the row remains locked.
func TestThrottle_FactorLockedEmittedOnTransition(t *testing.T) {
	// Schedule: first two failures free, third locks for 1s.
	schedule := []time.Duration{0, 0, time.Second}
	q := newFakeThrottleQueries()
	rec := &recordingAudit{}
	now := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)
	th := newThrottleAt(q, schedule, now)
	th.audit = rec

	// Failures 1 and 2: no lockout, no event.
	for i := 0; i < 2; i++ {
		if _, err := th.RegisterFailure(context.Background(), 1, "password"); err != nil {
			t.Fatalf("free-attempt RegisterFailure %d: %v", i+1, err)
		}
	}
	if got := rec.snapshot(); len(got) != 0 {
		t.Fatalf("expected no audit events before lockout, got %d: %+v", len(got), got)
	}

	// Failure 3: lockout transition — exactly one event.
	if _, err := th.RegisterFailure(context.Background(), 1, "password"); err != nil {
		t.Fatalf("locking RegisterFailure: %v", err)
	}
	got := rec.snapshot()
	if len(got) != 1 {
		t.Fatalf("expected 1 factor_locked event on transition, got %d: %+v", len(got), got)
	}
	if got[0].Event != audit.EventFactorLocked {
		t.Fatalf("event: want %q, got %q", audit.EventFactorLocked, got[0].Event)
	}
	if got[0].Factor != audit.FactorPassword {
		t.Fatalf("factor: want %q, got %q", audit.FactorPassword, got[0].Factor)
	}
	if got[0].AccountID == nil || *got[0].AccountID != 1 {
		t.Fatalf("account_id: want 1, got %v", got[0].AccountID)
	}
	if got[0].Detail["failed_attempts"] == nil {
		t.Fatalf("detail.failed_attempts missing: %+v", got[0].Detail)
	}

	// Failure 4: still inside the active lockout window, must NOT re-emit.
	if _, err := th.RegisterFailure(context.Background(), 1, "password"); err != nil {
		t.Fatalf("post-lockout RegisterFailure: %v", err)
	}
	if got := rec.snapshot(); len(got) != 1 {
		t.Fatalf("expected exactly 1 factor_locked event after re-failure inside lockout, got %d", len(got))
	}
}

// TestThrottle_FactorLockedReEmittedAfterExpiry verifies that once a lockout
// expires and the user trips back into a new lockout, a fresh factor_locked
// event fires — the transition signal MUST work across lockout cycles.
func TestThrottle_FactorLockedReEmittedAfterExpiry(t *testing.T) {
	schedule := []time.Duration{0, 0, time.Second}
	q := newFakeThrottleQueries()
	rec := &recordingAudit{}
	now := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)
	th := newThrottleAt(q, schedule, now)
	th.audit = rec

	// Drive into lockout (3 failures → 1s lockout).
	for i := 0; i < 3; i++ {
		if _, err := th.RegisterFailure(context.Background(), 1, "password"); err != nil {
			t.Fatalf("RegisterFailure %d: %v", i+1, err)
		}
	}
	if got := rec.snapshot(); len(got) != 1 {
		t.Fatalf("first lockout: want 1 event, got %d", len(got))
	}

	// Advance the clock past the lockout window. Next failure should re-emit.
	later := now.Add(time.Hour)
	th.now = func() time.Time { return later }
	q.clock = func() time.Time { return later }
	if _, err := th.RegisterFailure(context.Background(), 1, "password"); err != nil {
		t.Fatalf("post-expiry RegisterFailure: %v", err)
	}
	if got := rec.snapshot(); len(got) != 2 {
		t.Fatalf("post-expiry: want 2 events total, got %d: %+v", len(got), got)
	}
}
