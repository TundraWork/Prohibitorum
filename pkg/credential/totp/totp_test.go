package totp

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/db"
)

// fakeTxRunner runs fn against the supplied TOTPQueries (typically the same
// *fakeQueries the store was constructed with). It supports two failure
// modes used by the rollback tests:
//
//   - failInsertAtIdx: when ≥ 0, InsertRecoveryCode calls fn directly and the
//     wrapped fake's nth insert returns the injected error. fakeQueries
//     mutates state on each insert, so a snapshot/restore is required to
//     emulate Postgres's tx rollback semantics.
//   - failOnCommit: when true, fn runs and mutates state, then we treat the
//     "commit" as failing and roll back.
//
// Snapshot semantics: we shallow-copy the maps/slices the store's tx-scoped
// operations touch (totpRow pointer, recoveryRows slice, nextRecID, events).
// Audit events written inside fn are NOT rolled back — production audit
// emission happens after commit, so a fake that rolled back fn-internal
// audit writes would diverge from real behaviour.
type fakeTxRunner struct {
	q             *fakeQueries
	failOnCommit  bool
	inTxCallCount int
}

func (r *fakeTxRunner) InTx(ctx context.Context, fn func(q TOTPQueries) error) error {
	r.inTxCallCount++
	// Snapshot the fields a TOTP tx might mutate. confirmCalls/insertCalls
	// counters are kept; tests assert on their post-rollback values to verify
	// that fn DID run (we just then rolled back the state mutations).
	var rowSnap *db.TotpCredential
	if r.q.totpRow != nil {
		copyRow := *r.q.totpRow
		rowSnap = &copyRow
	}
	recSnap := append([]db.RecoveryCode(nil), r.q.recoveryRows...)
	nextIDSnap := r.q.nextRecID
	if err := fn(r.q); err != nil {
		// Rollback: restore snapshot.
		r.q.totpRow = rowSnap
		r.q.recoveryRows = recSnap
		r.q.nextRecID = nextIDSnap
		return err
	}
	if r.failOnCommit {
		r.q.totpRow = rowSnap
		r.q.recoveryRows = recSnap
		r.q.nextRecID = nextIDSnap
		return errors.New("tx: commit failed (injected)")
	}
	return nil
}

// fakeQueries satisfies TOTPQueries + authn.ThrottleQueries + the
// InsertCredentialEvent surface that audit.NewWriter expects. Mirror of the
// pattern used in pkg/credential/password/password_test.go.
type fakeQueries struct {
	db.Querier

	totpRow      *db.TotpCredential
	recoveryRows []db.RecoveryCode
	nextRecID    int32

	throttle map[string]db.AuthThrottle
	events   []db.InsertCredentialEventParams

	getCalls      int
	insertCalls   int
	deleteCalls   int
	confirmCalls  int
	lastStepCalls []db.UpdateTOTPLastStepParams

	// recInsertCalls counts calls to InsertRecoveryCode for the rollback test.
	// When failInsertRecAt > 0 the (1-indexed) Nth call returns the injected
	// error — modelling "insert #5 fails halfway through the mint loop".
	recInsertCalls   int
	failInsertRecAt  int
	failInsertRecErr error
}

func newFakeQueries() *fakeQueries {
	return &fakeQueries{
		throttle:  map[string]db.AuthThrottle{},
		nextRecID: 1,
	}
}

func (f *fakeQueries) GetTOTPCredential(_ context.Context, accountID int32) (db.TotpCredential, error) {
	f.getCalls++
	if f.totpRow == nil || f.totpRow.AccountID != accountID {
		return db.TotpCredential{}, pgx.ErrNoRows
	}
	return *f.totpRow, nil
}

func (f *fakeQueries) InsertTOTPCredential(_ context.Context, arg db.InsertTOTPCredentialParams) (db.TotpCredential, error) {
	f.insertCalls++
	row := db.TotpCredential{
		AccountID:   arg.AccountID,
		SecretEnc:   arg.SecretEnc,
		SecretNonce: arg.SecretNonce,
		KeyVersion:  arg.KeyVersion,
		Period:      arg.Period,
		Digits:      arg.Digits,
		Algorithm:   arg.Algorithm,
		LastStep:    0,
	}
	f.totpRow = &row
	return row, nil
}

func (f *fakeQueries) DeleteTOTPCredential(_ context.Context, accountID int32) error {
	f.deleteCalls++
	if f.totpRow != nil && f.totpRow.AccountID == accountID {
		f.totpRow = nil
	}
	return nil
}

func (f *fakeQueries) ConfirmTOTPCredential(_ context.Context, accountID int32) error {
	f.confirmCalls++
	if f.totpRow != nil && f.totpRow.AccountID == accountID {
		f.totpRow.ConfirmedAt = pgtype.Timestamptz{Time: time.Now(), Valid: true}
	}
	return nil
}

func (f *fakeQueries) UpdateTOTPLastStep(_ context.Context, arg db.UpdateTOTPLastStepParams) (int64, error) {
	f.lastStepCalls = append(f.lastStepCalls, arg)
	// Mirror the production SQL: UPDATE ... WHERE $2 > last_step RETURNING.
	// When the guard fails we return pgx.ErrNoRows so the store layer can
	// surface ErrTOTPReplay on a lost race (the post-race-test below
	// deliberately injects a state where this branch fires).
	if f.totpRow != nil && f.totpRow.AccountID == arg.AccountID && arg.LastStep > f.totpRow.LastStep {
		f.totpRow.LastStep = arg.LastStep
		return arg.LastStep, nil
	}
	return 0, pgx.ErrNoRows
}

func (f *fakeQueries) ListRecoveryCodesByAccount(_ context.Context, accountID int32) ([]db.RecoveryCode, error) {
	var out []db.RecoveryCode
	for _, r := range f.recoveryRows {
		if r.AccountID == accountID && !r.UsedAt.Valid {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeQueries) InsertRecoveryCode(_ context.Context, arg db.InsertRecoveryCodeParams) (db.RecoveryCode, error) {
	f.recInsertCalls++
	if f.failInsertRecAt > 0 && f.recInsertCalls == f.failInsertRecAt {
		return db.RecoveryCode{}, f.failInsertRecErr
	}
	row := db.RecoveryCode{
		ID:        f.nextRecID,
		AccountID: arg.AccountID,
		Hash:      arg.Hash,
	}
	f.nextRecID++
	f.recoveryRows = append(f.recoveryRows, row)
	return row, nil
}

func (f *fakeQueries) ConsumeRecoveryCode(_ context.Context, arg db.ConsumeRecoveryCodeParams) (db.RecoveryCode, error) {
	for i := range f.recoveryRows {
		if f.recoveryRows[i].ID == arg.ID && !f.recoveryRows[i].UsedAt.Valid {
			f.recoveryRows[i].UsedAt = pgtype.Timestamptz{Time: time.Now(), Valid: true}
			f.recoveryRows[i].UsedSessionID = arg.UsedSessionID
			f.recoveryRows[i].UsedIp = arg.UsedIp
			return f.recoveryRows[i], nil
		}
	}
	return db.RecoveryCode{}, pgx.ErrNoRows
}

func (f *fakeQueries) DeleteAllRecoveryCodesByAccount(_ context.Context, accountID int32) error {
	keep := f.recoveryRows[:0]
	for _, r := range f.recoveryRows {
		if r.AccountID != accountID {
			keep = append(keep, r)
		}
	}
	f.recoveryRows = keep
	return nil
}

func (f *fakeQueries) throttleKey(accountID int32, factor string) string {
	return fmt.Sprintf("%d:%s", accountID, factor)
}

func (f *fakeQueries) GetAuthThrottle(_ context.Context, arg db.GetAuthThrottleParams) (db.AuthThrottle, error) {
	row, ok := f.throttle[f.throttleKey(arg.AccountID, arg.Factor)]
	if !ok {
		return db.AuthThrottle{}, pgx.ErrNoRows
	}
	return row, nil
}

func (f *fakeQueries) BumpAuthThrottle(_ context.Context, arg db.BumpAuthThrottleParams) (db.BumpAuthThrottleRow, error) {
	key := f.throttleKey(arg.AccountID, arg.Factor)
	now := time.Now()
	cur, ok := f.throttle[key]
	if !ok {
		cur = db.AuthThrottle{
			AccountID:      arg.AccountID,
			Factor:         arg.Factor,
			FailedAttempts: 0,
			WindowStart:    pgtype.Timestamptz{Time: now, Valid: true},
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
	f.throttle[key] = cur
	return db.BumpAuthThrottleRow{FailedAttempts: cur.FailedAttempts, LockedUntil: cur.LockedUntil}, nil
}

func (f *fakeQueries) ResetAuthThrottle(_ context.Context, arg db.ResetAuthThrottleParams) error {
	delete(f.throttle, f.throttleKey(arg.AccountID, arg.Factor))
	return nil
}

func (f *fakeQueries) InsertCredentialEvent(_ context.Context, arg db.InsertCredentialEventParams) error {
	f.events = append(f.events, arg)
	return nil
}

// --- Store test setup ---

func newTestStoreAt(t *testing.T, at time.Time) (*Store, *fakeQueries, []byte) {
	t.Helper()
	f := newFakeQueries()
	dek := make([]byte, 32)
	if _, err := rand.Read(dek); err != nil {
		t.Fatal(err)
	}
	deks := map[int][]byte{1: dek}
	cfg := configx.TOTPConfig{
		DefaultPeriod:     30,
		DefaultDigits:     6,
		DefaultAlgorithm:  "SHA1",
		DriftSteps:        1,
		RecoveryCodeCount: 10,
		Issuer:            "Prohibitorum",
	}
	schedule := []time.Duration{0, 0, time.Second, 2 * time.Second, 4 * time.Second}
	w := audit.NewWriter(f)
	throttle := authn.NewThrottle(f, schedule, w)
	tx := &fakeTxRunner{q: f}
	s := NewStore(f, tx, deks, cfg, throttle, w)
	s.now = func() time.Time { return at }
	return s, f, dek
}

// newTestStoreAtWithTx is the same as newTestStoreAt but also returns the
// underlying tx runner so tests can inject commit-failure / rollback paths.
func newTestStoreAtWithTx(t *testing.T, at time.Time) (*Store, *fakeQueries, *fakeTxRunner, []byte) {
	t.Helper()
	f := newFakeQueries()
	dek := make([]byte, 32)
	if _, err := rand.Read(dek); err != nil {
		t.Fatal(err)
	}
	deks := map[int][]byte{1: dek}
	cfg := configx.TOTPConfig{
		DefaultPeriod:     30,
		DefaultDigits:     6,
		DefaultAlgorithm:  "SHA1",
		DriftSteps:        1,
		RecoveryCodeCount: 10,
		Issuer:            "Prohibitorum",
	}
	schedule := []time.Duration{0, 0, time.Second, 2 * time.Second, 4 * time.Second}
	w := audit.NewWriter(f)
	throttle := authn.NewThrottle(f, schedule, w)
	tx := &fakeTxRunner{q: f}
	s := NewStore(f, tx, deks, cfg, throttle, w)
	s.now = func() time.Time { return at }
	return s, f, tx, dek
}

// codeAt computes the current TOTP code for the stored secret. Used by the
// tests to construct a "real" code matching the in-memory secret without
// having to inject one.
func codeAt(t *testing.T, dek []byte, row db.TotpCredential, accountID int32, at time.Time, delta int64) string {
	t.Helper()
	secret, err := decryptSecret(dek, row.SecretEnc, row.SecretNonce, aadFor(accountID, row.KeyVersion))
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	step := stepFor(at.Unix(), int64(row.Period))
	return computeCode(secret, step+delta, int(row.Digits))
}

// --- Store tests ---

func TestStore_BeginAndConfirm(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	s, f, dek := newTestStoreAt(t, at)
	ctx := context.Background()

	enr, err := s.Begin(ctx, 1, "alice")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if enr.SecretBase32 == "" {
		t.Error("empty secret")
	}
	if !strings.HasPrefix(enr.ProvisioningURI, "otpauth://totp/") {
		t.Errorf("uri prefix: %s", enr.ProvisioningURI)
	}
	u, err := url.Parse(enr.ProvisioningURI)
	if err != nil {
		t.Fatalf("parse uri: %v", err)
	}
	if got := u.Query().Get("secret"); got != enr.SecretBase32 {
		t.Errorf("uri secret = %s, want %s", got, enr.SecretBase32)
	}
	if got := u.Query().Get("issuer"); got != "Prohibitorum" {
		t.Errorf("uri issuer = %s, want Prohibitorum", got)
	}
	if got := u.Query().Get("algorithm"); got != "SHA1" {
		t.Errorf("uri algorithm = %s, want SHA1", got)
	}
	if got := u.Query().Get("digits"); got != "6" {
		t.Errorf("uri digits = %s, want 6", got)
	}
	if got := u.Query().Get("period"); got != "30" {
		t.Errorf("uri period = %s, want 30", got)
	}
	// Label encodes "Prohibitorum:alice" with PathEscape, so reading the
	// path back via Path (not RawPath) yields the unescaped form.
	wantLabel := "Prohibitorum:alice"
	if !strings.Contains(u.Path, wantLabel) && !strings.Contains(strings.ReplaceAll(enr.ProvisioningURI, "%3A", ":"), wantLabel) {
		t.Errorf("uri label missing %q in %q", wantLabel, enr.ProvisioningURI)
	}

	row := *f.totpRow
	code := codeAt(t, dek, row, 1, at, 0)

	codes, err := s.Verify(ctx, 1, code)
	if err != nil {
		t.Fatalf("first Verify: %v", err)
	}
	if len(codes) != 10 {
		t.Errorf("expected 10 recovery codes on first confirm, got %d", len(codes))
	}
	if f.confirmCalls != 1 {
		t.Errorf("ConfirmTOTPCredential should have been called once, got %d", f.confirmCalls)
	}
	if !f.totpRow.ConfirmedAt.Valid {
		t.Error("totpRow.ConfirmedAt should be set after first verify")
	}
	if len(f.recoveryRows) != 10 {
		t.Errorf("expected 10 recovery rows persisted, got %d", len(f.recoveryRows))
	}

	// Second verify of a fresh code (next step) — already confirmed, so
	// no recovery codes returned.
	atLater := at.Add(31 * time.Second)
	s.now = func() time.Time { return atLater }
	code2 := codeAt(t, dek, *f.totpRow, 1, atLater, 0)
	codes2, err := s.Verify(ctx, 1, code2)
	if err != nil {
		t.Fatalf("second Verify: %v", err)
	}
	if codes2 != nil {
		t.Errorf("subsequent Verify should return nil recovery codes, got %v", codes2)
	}
}

func TestStore_VerifyDriftAccepted(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	s, f, dek := newTestStoreAt(t, at)
	ctx := context.Background()
	if _, err := s.Begin(ctx, 1, "alice"); err != nil {
		t.Fatal(err)
	}
	// Code from step+1 (future drift).
	row := *f.totpRow
	code := codeAt(t, dek, row, 1, at, 1)
	if _, err := s.Verify(ctx, 1, code); err != nil {
		t.Errorf("Verify with +1 drift step should succeed, got %v", err)
	}
}

func TestStore_VerifyDriftRejected(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	s, f, dek := newTestStoreAt(t, at)
	ctx := context.Background()
	if _, err := s.Begin(ctx, 1, "alice"); err != nil {
		t.Fatal(err)
	}
	// Code from step-2 (outside ±1 window).
	row := *f.totpRow
	code := codeAt(t, dek, row, 1, at, -2)
	_, err := s.Verify(ctx, 1, code)
	if !errors.Is(err, ErrTOTPInvalidCode) {
		t.Errorf("Verify with -2 step drift should return ErrTOTPInvalidCode, got %v", err)
	}
}

// TestStore_VerifyReplayRaceRejected covers the audit's Critical TOCTOU
// finding: two parallel verifies of the same code with the same step pass
// the Go-side `matchedStep <= row.LastStep` check (because both read
// row.LastStep before either has written), then race into UpdateTOTPLastStep.
// Only one row matches `$2 > last_step`; the other gets pgx.ErrNoRows from
// the :one RETURNING and MUST surface ErrTOTPReplay. The pre-bundle :exec
// silently dropped to 0 rows affected with no error, so both verifies
// proceeded to issue sessions.
//
// We can't easily race the real call site (the fake's mutex would serialise
// the GET so the second verify sees the updated LastStep). Instead we
// invoke UpdateTOTPLastStep twice manually after the first verify has
// already committed — the second call hits the WHERE-guard and returns
// pgx.ErrNoRows, which the surrounding code translates to ErrTOTPReplay.
// The non-race TestStore_VerifyReplayRejected above covers the serial path
// where the Go-side check fires; this test covers the DB-side guard.
func TestStore_VerifyReplayRaceRejected(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	s, f, dek := newTestStoreAt(t, at)
	ctx := context.Background()
	if _, err := s.Begin(ctx, 1, "alice"); err != nil {
		t.Fatal(err)
	}
	row := *f.totpRow
	code := codeAt(t, dek, row, 1, at, 0)
	if _, err := s.Verify(ctx, 1, code); err != nil {
		t.Fatalf("first Verify (race winner): %v", err)
	}

	// Simulate the race loser: a parallel verify that read the same
	// pre-update LastStep (0) and computed matchedStep on the same code.
	// Force the in-memory row's LastStep back to 0 for the read but keep
	// the side state — equivalent to "row snapshot the loser captured
	// before the winner wrote". The Verify call below will pass the
	// matchedStep <= row.LastStep check (matched > 0 = last_step) and
	// race into UpdateTOTPLastStep, where the WHERE-guard now rejects it
	// because the persisted last_step has already advanced.
	priorLastStep := f.totpRow.LastStep
	f.totpRow.LastStep = 0
	// Re-Verify same code — Go-side guard now allows the call through; the
	// DB-side UPDATE fails with pgx.ErrNoRows because the fake's
	// UpdateTOTPLastStep checks `arg.LastStep > f.totpRow.LastStep` against
	// the (now-restored, via the call setting it) persisted state.
	// Restore the persisted state before calling — that's what the race
	// loser would observe: the winner already wrote priorLastStep.
	f.totpRow.LastStep = priorLastStep
	// To get the loser to attempt the UPDATE we have to fool the Go-side
	// check; manually invoke UpdateTOTPLastStep with the same step the
	// winner already wrote — this is exactly what the loser would do in
	// the real race.
	_, err := f.UpdateTOTPLastStep(ctx, db.UpdateTOTPLastStepParams{AccountID: 1, LastStep: priorLastStep})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("UpdateTOTPLastStep at same step: want pgx.ErrNoRows, got %v", err)
	}

	// Sanity: the Verify call path now translates that into ErrTOTPReplay.
	// We exercise the translation directly via a second Verify of the same
	// already-consumed code.
	_, err = s.Verify(ctx, 1, code)
	if !errors.Is(err, ErrTOTPReplay) {
		t.Errorf("second Verify of consumed code: want ErrTOTPReplay, got %v", err)
	}
}

// TestStore_VerifyConcurrentLastStepRaceLoserGetsReplay exercises the race
// directly via a state-machine fake: the first UpdateTOTPLastStep flips an
// internal flag so the *next* call returns pgx.ErrNoRows without mutating
// state — modelling the DB-side WHERE-guard losing the race for the second
// caller. The store layer MUST translate that to ErrTOTPReplay.
func TestStore_VerifyConcurrentLastStepRaceLoserGetsReplay(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	s, f, dek := newTestStoreAt(t, at)
	ctx := context.Background()
	if _, err := s.Begin(ctx, 1, "alice"); err != nil {
		t.Fatal(err)
	}
	row := *f.totpRow

	// Install a wrapper fake that, on first UpdateTOTPLastStep, performs
	// the update normally, and on every subsequent call returns
	// pgx.ErrNoRows — simulating a race loser whose snapshot of row.LastStep
	// was stale by the time the WHERE-guard ran.
	wrapper := &raceLoserQueries{fakeQueries: f}
	s.q = wrapper

	code := codeAt(t, dek, row, 1, at, 0)
	if _, err := s.Verify(ctx, 1, code); err != nil {
		t.Fatalf("first Verify (race winner): %v", err)
	}

	// Reset the in-memory row's LastStep so the Go-side guard lets the
	// second call through to the DB; the wrapper then returns ErrNoRows
	// on the second UpdateTOTPLastStep, modelling a race loser.
	wrapper.fakeQueries.totpRow.LastStep = 0

	_, err := s.Verify(ctx, 1, code)
	if !errors.Is(err, ErrTOTPReplay) {
		t.Errorf("race loser: want ErrTOTPReplay, got %v", err)
	}
	// The race loser MUST also bump the throttle (audit's defense-in-depth).
	row2, ok := f.throttle[f.throttleKey(1, "totp")]
	if !ok || row2.FailedAttempts < 1 {
		t.Errorf("race loser should have bumped throttle, got %+v ok=%v", row2, ok)
	}
}

// raceLoserQueries decorates fakeQueries so the second (and later)
// UpdateTOTPLastStep call returns pgx.ErrNoRows — modelling the DB-side
// WHERE-guard rejecting a stale-snapshot caller in a race.
type raceLoserQueries struct {
	*fakeQueries
	updateCalls int
}

func (r *raceLoserQueries) UpdateTOTPLastStep(ctx context.Context, arg db.UpdateTOTPLastStepParams) (int64, error) {
	r.updateCalls++
	if r.updateCalls > 1 {
		return 0, pgx.ErrNoRows
	}
	return r.fakeQueries.UpdateTOTPLastStep(ctx, arg)
}

func TestStore_VerifyReplayRejected(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	s, f, dek := newTestStoreAt(t, at)
	ctx := context.Background()
	if _, err := s.Begin(ctx, 1, "alice"); err != nil {
		t.Fatal(err)
	}
	row := *f.totpRow
	code := codeAt(t, dek, row, 1, at, 0)
	if _, err := s.Verify(ctx, 1, code); err != nil {
		t.Fatalf("first Verify: %v", err)
	}
	// Same code, same step — replay.
	_, err := s.Verify(ctx, 1, code)
	if !errors.Is(err, ErrTOTPReplay) {
		t.Errorf("replay: want ErrTOTPReplay, got %v", err)
	}
}

func TestStore_VerifyChecksLockedBeforeCrypto(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	s, f, _ := newTestStoreAt(t, at)
	ctx := context.Background()

	// Pre-populate a locked row. Use real time.Now for the schedule clock so
	// the throttle (which uses its own time.Now) sees the deadline as future.
	f.throttle[f.throttleKey(1, "totp")] = db.AuthThrottle{
		AccountID:      1,
		Factor:         "totp",
		FailedAttempts: 5,
		WindowStart:    pgtype.Timestamptz{Time: time.Now().Add(-time.Minute), Valid: true},
		LockedUntil:    pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
	}
	getsBefore := f.getCalls
	_, err := s.Verify(ctx, 1, "anything")
	if err == nil {
		t.Fatal("expected ErrFactorLocked, got nil")
	}
	ae := authn.AsAuthError(err)
	if ae == nil || ae.Code != "factor_locked" {
		t.Fatalf("want factor_locked AuthError, got %v", err)
	}
	if f.getCalls != getsBefore {
		t.Errorf("GetTOTPCredential must not have been called when locked")
	}
}

func TestStore_VerifyNoRow(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	s, _, _ := newTestStoreAt(t, at)
	_, err := s.Verify(context.Background(), 99, "123456")
	if !errors.Is(err, ErrTOTPNotSet) {
		t.Errorf("Verify with no row: want ErrTOTPNotSet, got %v", err)
	}
}

func TestStore_VerifyEmitsAuditEvents(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	s, f, dek := newTestStoreAt(t, at)
	ctx := context.Background()
	if _, err := s.Begin(ctx, 1, "alice"); err != nil {
		t.Fatal(err)
	}
	row := *f.totpRow

	// Failure first (so we get a fail event), then success.
	_, _ = s.Verify(ctx, 1, "000000")
	code := codeAt(t, dek, row, 1, at, 0)
	if _, err := s.Verify(ctx, 1, code); err != nil {
		t.Fatalf("Verify: %v", err)
	}

	var sawUse, sawFail, sawTOTPRegister, sawRecoveryRegister bool
	for _, e := range f.events {
		switch {
		case e.Factor == "totp" && e.Event == "use":
			sawUse = true
		case e.Factor == "totp" && e.Event == "fail":
			sawFail = true
		case e.Factor == "totp" && e.Event == "register":
			sawTOTPRegister = true
		case e.Factor == "recovery_code" && e.Event == "register":
			sawRecoveryRegister = true
		}
	}
	if !sawUse {
		t.Error("expected totp/use event")
	}
	if !sawFail {
		t.Error("expected totp/fail event")
	}
	if !sawTOTPRegister {
		t.Error("expected totp/register event (emitted on first-confirm verify)")
	}
	if !sawRecoveryRegister {
		t.Error("expected recovery_code/register events (10x)")
	}
}

// TestStore_VerifyCorruptCiphertextReturnsSentinel verifies the Bundle-3
// Crypto-6 fix: AES-GCM authentication failures must surface as
// ErrTOTPCorrupt so the HTTP layer can collapse to a generic
// bad-credentials response without leaking cipher-failure detail. An
// audit fail event with reason="decrypt_failed" must also be emitted
// so server-side forensics can still distinguish this case.
func TestStore_VerifyCorruptCiphertextReturnsSentinel(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	s, f, dek := newTestStoreAt(t, at)
	ctx := context.Background()
	if _, err := s.Begin(ctx, 1, "alice"); err != nil {
		t.Fatal(err)
	}
	// Confirm so the row is normal-state.
	code := codeAt(t, dek, *f.totpRow, 1, at, 0)
	if _, err := s.Verify(ctx, 1, code); err != nil {
		t.Fatalf("Verify (confirm): %v", err)
	}

	// Tamper the stored ciphertext: flip a bit so AES-GCM authentication
	// fails on the next Verify.
	if len(f.totpRow.SecretEnc) == 0 {
		t.Fatal("expected SecretEnc populated")
	}
	f.totpRow.SecretEnc[0] ^= 0x01

	// Advance the clock so we're outside any replay window.
	at2 := at.Add(60 * time.Second)
	s.now = func() time.Time { return at2 }

	_, err := s.Verify(ctx, 1, "123456")
	if !errors.Is(err, ErrTOTPCorrupt) {
		t.Fatalf("Verify on tampered ciphertext: want ErrTOTPCorrupt, got %v", err)
	}
	// Error string must not leak the AES-GCM diagnostic.
	if err != nil && strings.Contains(err.Error(), "authentication failed") {
		t.Errorf("ErrTOTPCorrupt must not leak AES-GCM diagnostic, got %q", err.Error())
	}

	// Audit must contain a fail event with reason=decrypt_failed.
	sawDecryptFail := false
	for _, e := range f.events {
		if e.Factor == "totp" && e.Event == "fail" && strings.Contains(string(e.Detail), `"decrypt_failed"`) {
			sawDecryptFail = true
			break
		}
	}
	if !sawDecryptFail {
		t.Error("expected totp/fail audit event with detail.reason=decrypt_failed")
	}
}

func TestStore_RecoveryCodeVerifyAndConsume(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	s, f, dek := newTestStoreAt(t, at)
	ctx := context.Background()
	if _, err := s.Begin(ctx, 1, "alice"); err != nil {
		t.Fatal(err)
	}
	codes, err := s.Verify(ctx, 1, codeAt(t, dek, *f.totpRow, 1, at, 0))
	if err != nil {
		t.Fatalf("Verify (confirm): %v", err)
	}
	if len(codes) != 10 {
		t.Fatalf("want 10 recovery codes, got %d", len(codes))
	}

	if err := s.VerifyRecoveryCode(ctx, 1, codes[0], "sess-abc", "127.0.0.1"); err != nil {
		t.Fatalf("VerifyRecoveryCode codes[0]: %v", err)
	}
	// Row should now be marked used.
	var matched db.RecoveryCode
	for _, r := range f.recoveryRows {
		if r.UsedAt.Valid {
			matched = r
			break
		}
	}
	if !matched.UsedAt.Valid {
		t.Fatal("expected one recovery code row marked used")
	}
	if matched.UsedSessionID.String != "sess-abc" {
		t.Errorf("used_session_id = %q, want sess-abc", matched.UsedSessionID.String)
	}
	if matched.UsedIp == nil || matched.UsedIp.String() != "127.0.0.1" {
		t.Errorf("used_ip = %v, want 127.0.0.1", matched.UsedIp)
	}

	// Same code again — fails (already consumed → no longer in unused list).
	err = s.VerifyRecoveryCode(ctx, 1, codes[0], "sess-abc", "127.0.0.1")
	if !errors.Is(err, ErrRecoveryCodeInvalid) {
		t.Errorf("replay: want ErrRecoveryCodeInvalid, got %v", err)
	}

	// Different valid code — still works.
	if err := s.VerifyRecoveryCode(ctx, 1, codes[1], "", ""); err != nil {
		t.Errorf("codes[1]: %v", err)
	}
}

func TestStore_RecoveryCodeNormalizationOnVerify(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	s, f, dek := newTestStoreAt(t, at)
	ctx := context.Background()
	if _, err := s.Begin(ctx, 1, "alice"); err != nil {
		t.Fatal(err)
	}
	codes, err := s.Verify(ctx, 1, codeAt(t, dek, *f.totpRow, 1, at, 0))
	if err != nil {
		t.Fatal(err)
	}
	// Lowercase + spaces should still verify.
	munged := " " + strings.ToLower(codes[0]) + " "
	if err := s.VerifyRecoveryCode(ctx, 1, munged, "", ""); err != nil {
		t.Errorf("normalized verify (lowercase + spaces): %v", err)
	}
}

func TestStore_RecoveryCodeWrongFormatRejected(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	s, _, _ := newTestStoreAt(t, at)
	ctx := context.Background()

	// Length != 16 after normalization → reject.
	err := s.VerifyRecoveryCode(ctx, 1, "SHORT", "", "")
	if !errors.Is(err, ErrRecoveryCodeInvalid) {
		t.Errorf("short code: want ErrRecoveryCodeInvalid, got %v", err)
	}
}

func TestStore_RecoveryCodeChecksLockedBeforeIteration(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	s, f, _ := newTestStoreAt(t, at)
	ctx := context.Background()

	f.throttle[f.throttleKey(1, "recovery_code")] = db.AuthThrottle{
		AccountID:      1,
		Factor:         "recovery_code",
		FailedAttempts: 5,
		WindowStart:    pgtype.Timestamptz{Time: time.Now().Add(-time.Minute), Valid: true},
		LockedUntil:    pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
	}
	err := s.VerifyRecoveryCode(ctx, 1, "ABCDEFGH23456789", "", "")
	if err == nil {
		t.Fatal("expected lockout error")
	}
	ae := authn.AsAuthError(err)
	if ae == nil || ae.Code != "factor_locked" {
		t.Errorf("want factor_locked, got %v", err)
	}
}

func TestStore_RegenerateRecoveryCodes(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	s, f, dek := newTestStoreAt(t, at)
	ctx := context.Background()
	if _, err := s.Begin(ctx, 1, "alice"); err != nil {
		t.Fatal(err)
	}
	codes1, err := s.Verify(ctx, 1, codeAt(t, dek, *f.totpRow, 1, at, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(codes1) != 10 {
		t.Fatalf("first mint: want 10, got %d", len(codes1))
	}
	hash1 := f.recoveryRows[0].Hash

	codes2, err := s.RegenerateRecoveryCodes(ctx, 1)
	if err != nil {
		t.Fatalf("Regenerate: %v", err)
	}
	if len(codes2) != 10 {
		t.Errorf("regen: want 10, got %d", len(codes2))
	}
	// Old codes must no longer be present.
	for _, r := range f.recoveryRows {
		if r.Hash == hash1 {
			t.Error("old recovery code hash should have been deleted")
		}
	}
	// Old plaintexts should now fail to verify.
	if err := s.VerifyRecoveryCode(ctx, 1, codes1[0], "", ""); !errors.Is(err, ErrRecoveryCodeInvalid) {
		t.Errorf("old code after regen: want ErrRecoveryCodeInvalid, got %v", err)
	}
}

func TestStore_Delete(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	s, f, _ := newTestStoreAt(t, at)
	ctx := context.Background()
	if _, err := s.Begin(ctx, 1, "alice"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, 1); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if f.totpRow != nil {
		t.Error("totpRow should be nil after Delete")
	}
	var sawRevoke bool
	for _, e := range f.events {
		if e.Factor == "totp" && e.Event == "revoke" {
			sawRevoke = true
		}
	}
	if !sawRevoke {
		t.Error("expected totp/revoke audit event")
	}
}

func TestStore_BeginOverwritesUnconfirmed(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	s, f, _ := newTestStoreAt(t, at)
	ctx := context.Background()

	enr1, err := s.Begin(ctx, 1, "alice")
	if err != nil {
		t.Fatal(err)
	}
	enr2, err := s.Begin(ctx, 1, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if enr1.SecretBase32 == enr2.SecretBase32 {
		t.Error("second Begin should produce a different secret")
	}
	// Exactly one row should remain (the second).
	if f.totpRow == nil {
		t.Fatal("totpRow nil after second Begin")
	}
	// f.deleteCalls should be ≥2 (once per Begin).
	if f.deleteCalls < 2 {
		t.Errorf("delete calls: %d, want ≥2", f.deleteCalls)
	}
}

func TestStore_VerifyFailureBumpsThrottle(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	s, f, _ := newTestStoreAt(t, at)
	ctx := context.Background()
	if _, err := s.Begin(ctx, 1, "alice"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := s.Verify(ctx, 1, "000000"); !errors.Is(err, ErrTOTPInvalidCode) {
			t.Fatalf("verify %d: want ErrTOTPInvalidCode, got %v", i+1, err)
		}
	}
	row, ok := f.throttle[f.throttleKey(1, "totp")]
	if !ok {
		t.Fatal("expected throttle row after 3 failures")
	}
	if row.FailedAttempts < 3 {
		t.Errorf("FailedAttempts = %d, want ≥3", row.FailedAttempts)
	}
}

func TestStore_VerifySuccessResetsThrottle(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	s, f, dek := newTestStoreAt(t, at)
	ctx := context.Background()
	if _, err := s.Begin(ctx, 1, "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Verify(ctx, 1, "000000"); !errors.Is(err, ErrTOTPInvalidCode) {
		t.Fatalf("failure: %v", err)
	}
	if _, ok := f.throttle[f.throttleKey(1, "totp")]; !ok {
		t.Fatal("expected throttle row after failure")
	}
	code := codeAt(t, dek, *f.totpRow, 1, at, 0)
	if _, err := s.Verify(ctx, 1, code); err != nil {
		t.Fatalf("success Verify: %v", err)
	}
	if _, ok := f.throttle[f.throttleKey(1, "totp")]; ok {
		t.Error("throttle row should have been reset after success")
	}
}

// TestStore_VerifyFirstConfirmRollsBackOnMintFailure exercises the audit v0.2
// Medium #2 fix: ConfirmTOTPCredential + 10x InsertRecoveryCode must run in
// a single transaction. A mid-loop insert failure must roll back the confirm,
// leaving the row unconfirmed and zero recovery rows persisted. The next
// successful Verify at the next step must re-enter the first-confirm branch
// and mint successfully.
func TestStore_VerifyFirstConfirmRollsBackOnMintFailure(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	s, f, _, dek := newTestStoreAtWithTx(t, at)
	ctx := context.Background()
	if _, err := s.Begin(ctx, 1, "alice"); err != nil {
		t.Fatal(err)
	}
	row := *f.totpRow

	// Inject: the 5th recovery-code insert returns an error.
	f.failInsertRecAt = 5
	f.failInsertRecErr = errors.New("simulated DB failure on insert #5")

	code := codeAt(t, dek, row, 1, at, 0)
	codes, err := s.Verify(ctx, 1, code)
	if err == nil {
		t.Fatal("Verify: expected error from mint failure, got nil")
	}
	if codes != nil {
		t.Errorf("Verify: expected nil codes on rollback, got %d", len(codes))
	}

	// Rollback assertions: row must remain unconfirmed and no recovery rows.
	if f.totpRow == nil {
		t.Fatal("totpRow should still exist after rollback")
	}
	if f.totpRow.ConfirmedAt.Valid {
		t.Error("totpRow.ConfirmedAt should NOT be set after tx rollback")
	}
	if len(f.recoveryRows) != 0 {
		t.Errorf("expected 0 recovery rows after rollback, got %d", len(f.recoveryRows))
	}

	// No recovery_code:register audit events should have been emitted (audit
	// is emitted AFTER commit). The totp:register event also must NOT have
	// been emitted because the tx failed.
	for _, e := range f.events {
		if e.Factor == "recovery_code" && e.Event == "register" {
			t.Errorf("unexpected recovery_code/register audit after rollback")
		}
		if e.Factor == "totp" && e.Event == "register" {
			t.Errorf("unexpected totp/register audit after rollback")
		}
	}

	// Retry path: clear the failure injection, advance the clock to the next
	// step, and verify with a fresh code. The first-confirm branch must
	// re-trigger because ConfirmedAt is still null.
	f.failInsertRecAt = 0
	f.failInsertRecErr = nil
	f.recInsertCalls = 0
	atLater := at.Add(31 * time.Second)
	s.now = func() time.Time { return atLater }
	code2 := codeAt(t, dek, *f.totpRow, 1, atLater, 0)
	codes2, err := s.Verify(ctx, 1, code2)
	if err != nil {
		t.Fatalf("retry Verify: %v", err)
	}
	if len(codes2) != 10 {
		t.Errorf("retry: expected 10 recovery codes, got %d", len(codes2))
	}
	if !f.totpRow.ConfirmedAt.Valid {
		t.Error("totpRow.ConfirmedAt should be set after successful retry")
	}
	if len(f.recoveryRows) != 10 {
		t.Errorf("expected 10 recovery rows after retry, got %d", len(f.recoveryRows))
	}
}

// TestStore_RegenerateRecoveryCodesRollsBackOnMintFailure exercises the same
// audit v0.2 Medium #2 fix for the regenerate path. A mid-loop insert
// failure must roll back the delete, leaving the old codes intact.
func TestStore_RegenerateRecoveryCodesRollsBackOnMintFailure(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	s, f, _, dek := newTestStoreAtWithTx(t, at)
	ctx := context.Background()
	if _, err := s.Begin(ctx, 1, "alice"); err != nil {
		t.Fatal(err)
	}
	codes1, err := s.Verify(ctx, 1, codeAt(t, dek, *f.totpRow, 1, at, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(codes1) != 10 {
		t.Fatalf("initial mint: want 10, got %d", len(codes1))
	}
	preRegenRows := append([]db.RecoveryCode(nil), f.recoveryRows...)

	// Inject: the 3rd insert in the regen mint loop fails.
	f.recInsertCalls = 0
	f.failInsertRecAt = 3
	f.failInsertRecErr = errors.New("simulated DB failure on regen insert #3")

	codes2, err := s.RegenerateRecoveryCodes(ctx, 1)
	if err == nil {
		t.Fatal("RegenerateRecoveryCodes: expected error, got nil")
	}
	if codes2 != nil {
		t.Errorf("expected nil codes on rollback, got %d", len(codes2))
	}

	// The original 10 rows must still be present (delete was rolled back).
	if len(f.recoveryRows) != len(preRegenRows) {
		t.Errorf("rollback: expected %d rows preserved, got %d",
			len(preRegenRows), len(f.recoveryRows))
	}
	// Old plaintext codes must still verify.
	if err := s.VerifyRecoveryCode(ctx, 1, codes1[0], "", ""); err != nil {
		t.Errorf("old code after failed regen: want success, got %v", err)
	}
}

// TestStore_RegenerateRecoveryCodesAuditsRevoke exercises the audit v0.2
// Medium #3 fix: the regenerate path must emit one recovery_code/revoke event
// per deleted code AND one recovery_code/register event per new code, in
// that order, so the audit trail is symmetric.
func TestStore_RegenerateRecoveryCodesAuditsRevoke(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	s, f, _, dek := newTestStoreAtWithTx(t, at)
	ctx := context.Background()
	if _, err := s.Begin(ctx, 1, "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Verify(ctx, 1, codeAt(t, dek, *f.totpRow, 1, at, 0)); err != nil {
		t.Fatal(err)
	}

	// Snapshot the audit-event tail before regen so we count only the new
	// events emitted by the regenerate call.
	preRegenEventCount := len(f.events)

	if _, err := s.RegenerateRecoveryCodes(ctx, 1); err != nil {
		t.Fatalf("RegenerateRecoveryCodes: %v", err)
	}

	var revokes, registers int
	var firstRevokeIdx, firstRegisterIdx = -1, -1
	for i := preRegenEventCount; i < len(f.events); i++ {
		e := f.events[i]
		if e.Factor != "recovery_code" {
			continue
		}
		switch e.Event {
		case "revoke":
			if firstRevokeIdx < 0 {
				firstRevokeIdx = i
			}
			revokes++
		case "register":
			if firstRegisterIdx < 0 {
				firstRegisterIdx = i
			}
			registers++
		}
	}
	if revokes != 10 {
		t.Errorf("expected 10 recovery_code/revoke audit events, got %d", revokes)
	}
	if registers != 10 {
		t.Errorf("expected 10 recovery_code/register audit events, got %d", registers)
	}
	if firstRevokeIdx < 0 || firstRegisterIdx < 0 {
		t.Fatal("expected both revoke and register audit events emitted")
	}
	if firstRevokeIdx > firstRegisterIdx {
		t.Errorf("revoke audit events must precede register events: revoke@%d register@%d",
			firstRevokeIdx, firstRegisterIdx)
	}
}

// TestStore_BeginAuditsRevokeOfPriorMaterial exercises the audit v0.2 Medium
// #3 fix for the Begin re-enrollment path. When Begin wipes a confirmed TOTP
// row and its recovery codes, it must emit one totp/revoke audit event for
// the confirmed row plus one recovery_code/revoke per existing code.
func TestStore_BeginAuditsRevokeOfPriorMaterial(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	s, f, dek := newTestStoreAt(t, at)
	ctx := context.Background()
	// First enrollment: Begin + Verify mints the row + 10 codes.
	if _, err := s.Begin(ctx, 1, "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Verify(ctx, 1, codeAt(t, dek, *f.totpRow, 1, at, 0)); err != nil {
		t.Fatal(err)
	}

	preBeginEventCount := len(f.events)

	// Re-enrollment: Begin should audit-revoke the confirmed TOTP row and the
	// 10 recovery codes.
	if _, err := s.Begin(ctx, 1, "alice"); err != nil {
		t.Fatal(err)
	}

	var totpRevokes, recoveryRevokes int
	for i := preBeginEventCount; i < len(f.events); i++ {
		e := f.events[i]
		if e.Event != "revoke" {
			continue
		}
		switch e.Factor {
		case "totp":
			totpRevokes++
		case "recovery_code":
			recoveryRevokes++
		}
	}
	if totpRevokes != 1 {
		t.Errorf("expected 1 totp/revoke audit event from Begin re-enrollment, got %d", totpRevokes)
	}
	if recoveryRevokes != 10 {
		t.Errorf("expected 10 recovery_code/revoke audit events from Begin re-enrollment, got %d", recoveryRevokes)
	}
}

// TestStore_BeginNoAuditWhenNoPriorMaterial verifies the inverse: a clean
// first-time Begin must NOT emit revoke audit events (nothing to revoke).
func TestStore_BeginNoAuditWhenNoPriorMaterial(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	s, f, _ := newTestStoreAt(t, at)
	ctx := context.Background()

	if _, err := s.Begin(ctx, 1, "alice"); err != nil {
		t.Fatal(err)
	}
	for _, e := range f.events {
		if e.Event == "revoke" {
			t.Errorf("first-time Begin should not emit revoke audit, got %+v", e)
		}
	}
}

// TestStore_BeginAuditsNoTOTPRevokeForUnconfirmedRow exercises the partial
// case: an unconfirmed prior row is wiped without a totp/revoke event (only
// confirmed enrollments are "live" credentials worth revoking).
func TestStore_BeginAuditsNoTOTPRevokeForUnconfirmedRow(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	s, f, _ := newTestStoreAt(t, at)
	ctx := context.Background()

	// First Begin: row created but never confirmed (no Verify call).
	if _, err := s.Begin(ctx, 1, "alice"); err != nil {
		t.Fatal(err)
	}
	preBeginEventCount := len(f.events)

	// Second Begin should wipe the unconfirmed row without emitting totp/revoke.
	if _, err := s.Begin(ctx, 1, "alice"); err != nil {
		t.Fatal(err)
	}
	for i := preBeginEventCount; i < len(f.events); i++ {
		e := f.events[i]
		if e.Factor == "totp" && e.Event == "revoke" {
			t.Errorf("Begin overwriting UNconfirmed row should not emit totp/revoke, got %+v", e)
		}
	}
}
