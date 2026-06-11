package password

import (
	"context"
	"errors"
	"fmt"
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

type fakeQueries struct {
	db.Querier
	pwRow           *db.PasswordCredential
	upserts         []db.UpsertPasswordCredentialParams
	hashOnlyUpdates []db.UpdatePasswordHashOnlyParams
	deletes         []int32
	getCalled       int
	throttle        map[string]db.AuthThrottle
	events          []db.InsertCredentialEventParams
}

func newFakeQueries() *fakeQueries {
	return &fakeQueries{throttle: map[string]db.AuthThrottle{}}
}

func (f *fakeQueries) GetPasswordCredential(_ context.Context, accountID int32) (db.PasswordCredential, error) {
	f.getCalled++
	if f.pwRow == nil || f.pwRow.AccountID != accountID {
		return db.PasswordCredential{}, pgx.ErrNoRows
	}
	return *f.pwRow, nil
}

func (f *fakeQueries) UpsertPasswordCredential(_ context.Context, arg db.UpsertPasswordCredentialParams) error {
	f.upserts = append(f.upserts, arg)
	row := db.PasswordCredential{AccountID: arg.AccountID, Hash: arg.Hash}
	f.pwRow = &row
	return nil
}

func (f *fakeQueries) UpdatePasswordHashOnly(_ context.Context, arg db.UpdatePasswordHashOnlyParams) error {
	f.hashOnlyUpdates = append(f.hashOnlyUpdates, arg)
	if f.pwRow != nil && f.pwRow.AccountID == arg.AccountID {
		// Mirror the SQL: hash changes, password_changed_at does NOT.
		f.pwRow.Hash = arg.Hash
	}
	return nil
}

func (f *fakeQueries) DeletePasswordCredential(_ context.Context, accountID int32) error {
	f.deletes = append(f.deletes, accountID)
	if f.pwRow != nil && f.pwRow.AccountID == accountID {
		f.pwRow = nil
	}
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

// testParams keeps argon2id cost low so unit tests run in milliseconds.
func testParams() configx.PasswordHashParams {
	return configx.PasswordHashParams{MemoryKiB: 8192, Iterations: 1, Parallelism: 1}
}

func newTestStore(f *fakeQueries, params configx.PasswordHashParams) *Store {
	schedule := []time.Duration{0, 0, time.Second, 2 * time.Second, 4 * time.Second}
	w := audit.NewWriter(f)
	throttle := authn.NewThrottle(f, schedule, w)
	return NewStore(f, params, throttle, w)
}

func TestStore_SetAndVerify(t *testing.T) {
	f := newFakeQueries()
	s := newTestStore(f, testParams())
	ctx := context.Background()

	if err := s.Set(ctx, 42, "correct horse battery staple"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if len(f.upserts) != 1 {
		t.Fatalf("upserts after Set: want 1, got %d", len(f.upserts))
	}

	if err := s.Verify(ctx, 42, "correct horse battery staple"); err != nil {
		t.Fatalf("Verify with correct pw: %v", err)
	}
	if err := s.Verify(ctx, 42, "wrong"); !errors.Is(err, ErrPasswordIncorrect) {
		t.Fatalf("Verify with wrong pw: want ErrPasswordIncorrect, got %v", err)
	}
}

func TestStore_VerifyNoRow(t *testing.T) {
	f := newFakeQueries()
	s := newTestStore(f, testParams())
	err := s.Verify(context.Background(), 42, "anything")
	if !errors.Is(err, ErrPasswordNotSet) {
		t.Fatalf("Verify with no row: want ErrPasswordNotSet, got %v", err)
	}
	// Verify path must NOT have run argon2id (no upserts, no audit events).
	if len(f.upserts) != 0 {
		t.Errorf("expected no upserts on missing row, got %d", len(f.upserts))
	}
	if len(f.events) != 0 {
		t.Errorf("expected no audit events on missing row (caller handles dummy), got %d", len(f.events))
	}
}

func TestStore_VerifyRehashesOnParamsUpgrade(t *testing.T) {
	f := newFakeQueries()
	oldParams := configx.PasswordHashParams{MemoryKiB: 8192, Iterations: 1, Parallelism: 1}
	s := newTestStore(f, oldParams)
	ctx := context.Background()

	if err := s.Set(ctx, 42, "pw"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	oldHash := f.pwRow.Hash

	// Upgrade params and verify again — should trigger re-hash.
	s.params = configx.PasswordHashParams{MemoryKiB: 16384, Iterations: 2, Parallelism: 1}
	if err := s.Verify(ctx, 42, "pw"); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	// The rehash must go through the hash-only path (T4.3a): exactly one
	// UpdatePasswordHashOnly, and NO second Upsert — an Upsert would bump
	// password_changed_at and make the silent re-encoding look like a real
	// password change to age/reauth policy.
	if len(f.upserts) != 1 {
		t.Fatalf("upserts after rehash: want 1 (the initial Set only), got %d", len(f.upserts))
	}
	if len(f.hashOnlyUpdates) != 1 {
		t.Fatalf("hash-only updates after rehash: want 1, got %d", len(f.hashOnlyUpdates))
	}
	if f.pwRow.Hash == oldHash {
		t.Errorf("hash should have been replaced after params upgrade")
	}
	rehashed, err := PHCDecode(f.pwRow.Hash)
	if err != nil {
		t.Fatalf("PHCDecode rehashed: %v", err)
	}
	if rehashed.Params != s.params {
		t.Errorf("rehashed params %+v want %+v", rehashed.Params, s.params)
	}
}

func TestStore_VerifyThrottlesOnFailure(t *testing.T) {
	f := newFakeQueries()
	s := newTestStore(f, testParams())
	ctx := context.Background()

	if err := s.Set(ctx, 42, "pw"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	for i := 0; i < 3; i++ {
		if err := s.Verify(ctx, 42, "wrong"); !errors.Is(err, ErrPasswordIncorrect) {
			t.Fatalf("verify %d: want ErrPasswordIncorrect, got %v", i+1, err)
		}
	}
	row, ok := f.throttle[f.throttleKey(42, "password")]
	if !ok {
		t.Fatal("expected auth_throttle row after 3 failures")
	}
	if row.FailedAttempts < 3 {
		t.Errorf("FailedAttempts: want >=3, got %d", row.FailedAttempts)
	}
	if !row.LockedUntil.Valid {
		t.Errorf("LockedUntil: want set after 3 failures (schedule = 0,0,1s)")
	}
}

func TestStore_VerifySuccessResetsThrottle(t *testing.T) {
	f := newFakeQueries()
	s := newTestStore(f, testParams())
	ctx := context.Background()

	if err := s.Set(ctx, 42, "pw"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := s.Verify(ctx, 42, "wrong"); !errors.Is(err, ErrPasswordIncorrect) {
		t.Fatalf("Verify wrong: %v", err)
	}
	if _, ok := f.throttle[f.throttleKey(42, "password")]; !ok {
		t.Fatal("expected throttle row after failure")
	}
	if err := s.Verify(ctx, 42, "pw"); err != nil {
		t.Fatalf("Verify correct: %v", err)
	}
	if _, ok := f.throttle[f.throttleKey(42, "password")]; ok {
		t.Errorf("throttle row should have been deleted after success")
	}
}

func TestStore_VerifyChecksLockedBeforeCrypto(t *testing.T) {
	f := newFakeQueries()
	s := newTestStore(f, testParams())
	ctx := context.Background()

	// Pre-populate a locked throttle row and a garbage hash. If the locked
	// check happens AFTER crypto, the garbage hash would yield a PHC-decode
	// error (not factor_locked).
	f.throttle[f.throttleKey(42, "password")] = db.AuthThrottle{
		AccountID:      42,
		Factor:         "password",
		FailedAttempts: 5,
		WindowStart:    pgtype.Timestamptz{Time: time.Now().Add(-time.Minute), Valid: true},
		LockedUntil:    pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
	}
	f.pwRow = &db.PasswordCredential{AccountID: 42, Hash: "not-a-valid-phc-string"}
	getsBefore := f.getCalled

	err := s.Verify(ctx, 42, "anything")
	if err == nil {
		t.Fatal("expected ErrFactorLocked, got nil")
	}
	ae := authn.AsAuthError(err)
	if ae == nil || ae.Code != "factor_locked" {
		t.Fatalf("want factor_locked AuthError, got %v", err)
	}
	if f.getCalled != getsBefore {
		t.Errorf("GetPasswordCredential must not have been called when locked (calls before=%d after=%d)", getsBefore, f.getCalled)
	}
}

func TestStore_Delete(t *testing.T) {
	f := newFakeQueries()
	s := newTestStore(f, testParams())
	ctx := context.Background()

	if err := s.Set(ctx, 42, "pw"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := s.Delete(ctx, 42); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := f.GetPasswordCredential(ctx, 42); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("after Delete, GetPasswordCredential: want ErrNoRows, got %v", err)
	}
	var sawRevoke bool
	for _, e := range f.events {
		if e.Factor == "password" && e.Event == "revoke" {
			sawRevoke = true
		}
	}
	if !sawRevoke {
		t.Errorf("expected a credential_event{factor:password, event:revoke}, got %+v", f.events)
	}
}

func TestStore_VerifyEmitsAuditEvents(t *testing.T) {
	f := newFakeQueries()
	s := newTestStore(f, testParams())
	ctx := context.Background()

	if err := s.Set(ctx, 42, "pw"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	_ = s.Verify(ctx, 42, "pw")
	_ = s.Verify(ctx, 42, "wrong")

	var sawUse, sawFail, sawRegister bool
	for _, e := range f.events {
		if e.Factor != "password" {
			continue
		}
		switch e.Event {
		case "use":
			sawUse = true
		case "fail":
			sawFail = true
		case "register":
			sawRegister = true
		}
	}
	if !sawRegister {
		t.Errorf("expected credential_event{event:register} from Set")
	}
	if !sawUse {
		t.Errorf("expected credential_event{event:use} from successful Verify")
	}
	if !sawFail {
		t.Errorf("expected credential_event{event:fail} from failed Verify")
	}
}

func TestHashRawAndVerifyRaw(t *testing.T) {
	phc, err := HashRaw("hello", testParams())
	if err != nil {
		t.Fatalf("HashRaw: %v", err)
	}
	if !VerifyRaw("hello", phc) {
		t.Errorf("VerifyRaw should match correct password")
	}
	if VerifyRaw("wrong", phc) {
		t.Errorf("VerifyRaw should reject wrong password")
	}
	if VerifyRaw("hello", "not-a-phc-string") {
		t.Errorf("VerifyRaw should reject malformed PHC")
	}
}

func TestDefaultParams(t *testing.T) {
	p := DefaultParams()
	if p.MemoryKiB != 65536 || p.Iterations != 3 || p.Parallelism != 1 {
		t.Errorf("DefaultParams = %+v, want OWASP 2026 baseline", p)
	}
}

func TestStore_VerifyAgainstDummyDoesNotPanic(t *testing.T) {
	f := newFakeQueries()
	s := newTestStore(f, testParams())
	s.VerifyAgainstDummy(context.Background(), "anything")
}

// TestStore_VerifyRejectsTamperedTag verifies the constant-time-compare path:
// a structurally valid PHC string with one byte flipped in the encoded tag
// must cause Verify(originalPassword) to return ErrPasswordIncorrect — proving
// the tag is actually being compared, not just the structural fields.
func TestStore_VerifyRejectsTamperedTag(t *testing.T) {
	f := newFakeQueries()
	s := newTestStore(f, testParams())
	ctx := context.Background()

	const pw = "alicepass"
	if err := s.Set(ctx, 1, pw); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// PHC layout: $argon2id$v=19$m=...,t=...,p=...$<b64 salt>$<b64 tag>
	parts := strings.Split(f.pwRow.Hash, "$")
	if len(parts) != 6 {
		t.Fatalf("unexpected PHC structure: %d parts, want 6: %q", len(parts), f.pwRow.Hash)
	}
	tagBytes := []byte(parts[5])
	if len(tagBytes) == 0 {
		t.Fatalf("empty tag segment in PHC: %q", f.pwRow.Hash)
	}
	// Set the first tag char to a different base64-safe character. This keeps
	// the PHC structurally valid (PHCDecode succeeds) but changes the decoded
	// tag bytes, so the constant-time compare against the freshly-derived key
	// must fail.
	if tagBytes[0] != 'A' {
		tagBytes[0] = 'A'
	} else {
		tagBytes[0] = 'B'
	}
	parts[5] = string(tagBytes)
	f.pwRow.Hash = strings.Join(parts, "$")

	// Sanity check: the tampered string must still decode as a valid PHC,
	// otherwise we'd be testing the decode-error path instead of the
	// compare-mismatch path.
	if _, err := PHCDecode(f.pwRow.Hash); err != nil {
		t.Fatalf("tampered PHC should still decode structurally: %v", err)
	}

	if err := s.Verify(ctx, 1, pw); !errors.Is(err, ErrPasswordIncorrect) {
		t.Errorf("tampered tag: want ErrPasswordIncorrect, got %v", err)
	}
}
