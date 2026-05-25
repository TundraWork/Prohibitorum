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

func (f *fakeQueries) UpdateTOTPLastStep(_ context.Context, arg db.UpdateTOTPLastStepParams) error {
	f.lastStepCalls = append(f.lastStepCalls, arg)
	if f.totpRow != nil && f.totpRow.AccountID == arg.AccountID && arg.LastStep > f.totpRow.LastStep {
		f.totpRow.LastStep = arg.LastStep
	}
	return nil
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

func (f *fakeQueries) UpsertAuthThrottle(_ context.Context, arg db.UpsertAuthThrottleParams) (db.AuthThrottle, error) {
	row := db.AuthThrottle{
		AccountID:      arg.AccountID,
		Factor:         arg.Factor,
		FailedAttempts: arg.FailedAttempts,
		WindowStart:    arg.WindowStart,
		LockedUntil:    arg.LockedUntil,
	}
	f.throttle[f.throttleKey(arg.AccountID, arg.Factor)] = row
	return row, nil
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
	throttle := authn.NewThrottle(f, schedule)
	w := audit.NewWriter(f)
	s := NewStore(f, deks, cfg, throttle, w)
	s.now = func() time.Time { return at }
	return s, f, dek
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
	return computeCode(secret, step+delta, int(row.Digits), row.Algorithm)
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

	var sawUse, sawFail, sawRegister bool
	for _, e := range f.events {
		switch {
		case e.Factor == "totp" && e.Event == "use":
			sawUse = true
		case e.Factor == "totp" && e.Event == "fail":
			sawFail = true
		case e.Factor == "recovery_code" && e.Event == "register":
			sawRegister = true
		}
	}
	if !sawUse {
		t.Error("expected totp/use event")
	}
	if !sawFail {
		t.Error("expected totp/fail event")
	}
	if !sawRegister {
		t.Error("expected recovery_code/register events (10x)")
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
