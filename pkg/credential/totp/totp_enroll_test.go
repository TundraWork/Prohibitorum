package totp

import (
	"context"
	"encoding/base32"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/db"
)

func decodeSecretB32(t *testing.T, b32 string) []byte {
	t.Helper()
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(b32)
	if err != nil {
		t.Fatalf("decode secret: %v", err)
	}
	return secret
}

func TestGenerateEnrollment_NoDBNoRow(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	s, f, _ := newTestStoreAt(t, at)

	enr, err := s.GenerateEnrollment("alice")
	if err != nil {
		t.Fatalf("GenerateEnrollment: %v", err)
	}
	if enr.SecretBase32 == "" {
		t.Fatal("empty secret")
	}
	// Crucially, no DB write happened — the account may not exist yet.
	if f.insertCalls != 0 || f.getCalls != 0 || f.totpRow != nil {
		t.Fatalf("GenerateEnrollment touched the DB: insert=%d get=%d row=%v", f.insertCalls, f.getCalls, f.totpRow)
	}
	u, err := url.Parse(enr.ProvisioningURI)
	if err != nil {
		t.Fatalf("parse uri: %v", err)
	}
	if !strings.HasPrefix(enr.ProvisioningURI, "otpauth://totp/") {
		t.Errorf("uri prefix: %s", enr.ProvisioningURI)
	}
	if got := u.Query().Get("secret"); got != enr.SecretBase32 {
		t.Errorf("uri secret = %s, want %s", got, enr.SecretBase32)
	}
	if got := u.Query().Get("issuer"); got != "Prohibitorum" {
		t.Errorf("uri issuer = %s, want Prohibitorum", got)
	}
}

func TestVerifyCandidateSecret(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	s, _, _ := newTestStoreAt(t, at)
	enr, err := s.GenerateEnrollment("alice")
	if err != nil {
		t.Fatalf("GenerateEnrollment: %v", err)
	}
	secret := decodeSecretB32(t, enr.SecretBase32)
	nowStep := stepFor(at.Unix(), 30)

	// Correct current-step code is accepted; matched step is returned for
	// last_step seeding.
	code := computeCode(secret, nowStep, 6)
	step, ok := s.VerifyCandidateSecret(enr.SecretBase32, code)
	if !ok {
		t.Fatal("current-step code rejected")
	}
	if step != nowStep {
		t.Errorf("matched step = %d, want %d", step, nowStep)
	}

	// The ±1 drift window is honored.
	if _, ok := s.VerifyCandidateSecret(enr.SecretBase32, computeCode(secret, nowStep-1, 6)); !ok {
		t.Error("prev-step (within drift) code rejected")
	}
	// Outside the drift window is rejected.
	if _, ok := s.VerifyCandidateSecret(enr.SecretBase32, computeCode(secret, nowStep+5, 6)); ok {
		t.Error("far-future code accepted")
	}
	// A wrong code is rejected.
	if _, ok := s.VerifyCandidateSecret(enr.SecretBase32, "000000"); ok {
		t.Error("wrong code accepted")
	}
	// A malformed base32 secret yields (0, false), never a panic.
	if step, ok := s.VerifyCandidateSecret("!!!not-base32!!!", code); ok || step != 0 {
		t.Errorf("malformed secret = (%d,%v), want (0,false)", step, ok)
	}
}

func TestEnrollConfirmedForTx(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	s, f, dek := newTestStoreAt(t, at)
	ctx := context.Background()

	enr, err := s.GenerateEnrollment("alice")
	if err != nil {
		t.Fatalf("GenerateEnrollment: %v", err)
	}
	secret := decodeSecretB32(t, enr.SecretBase32)
	step, ok := s.VerifyCandidateSecret(enr.SecretBase32, computeCode(secret, stepFor(at.Unix(), 30), 6))
	if !ok {
		t.Fatal("candidate verify failed")
	}

	codes, err := s.EnrollConfirmedForTx(ctx, f, 7, enr.SecretBase32, step)
	if err != nil {
		t.Fatalf("EnrollConfirmedForTx: %v", err)
	}
	if len(codes) != 10 {
		t.Fatalf("recovery codes = %d, want 10", len(codes))
	}
	if f.totpRow == nil {
		t.Fatal("no totp row persisted")
	}
	if !f.totpRow.ConfirmedAt.Valid {
		t.Error("row not confirmed")
	}
	if f.totpRow.KeyVersion != 1 || f.totpRow.Period != 30 || f.totpRow.Digits != 6 || f.totpRow.Algorithm != "SHA1" {
		t.Errorf("row params = key=%d period=%d digits=%d alg=%s", f.totpRow.KeyVersion, f.totpRow.Period, f.totpRow.Digits, f.totpRow.Algorithm)
	}
	if f.totpRow.LastStep != step {
		t.Errorf("last_step = %d, want %d (confirming-code replay guard)", f.totpRow.LastStep, step)
	}
	// The stored ciphertext decrypts (bound to account_id=7 via the AAD) back to
	// the original secret — i.e. login-time Verify will match the same codes.
	got, err := decryptSecret(dek, f.totpRow.SecretEnc, f.totpRow.SecretNonce, aadFor(7, f.totpRow.KeyVersion))
	if err != nil {
		t.Fatalf("decrypt persisted secret: %v", err)
	}
	if string(got) != string(secret) {
		t.Error("persisted secret != generated secret")
	}

	// End-to-end replay guard: the confirming code cannot be reused as an
	// immediate login, but a code at the NEXT step succeeds silently (row is
	// already confirmed, so no fresh codes are returned).
	if _, err := s.Verify(ctx, 7, computeCode(secret, step, 6)); err != ErrTOTPReplay {
		t.Errorf("confirming-code login = %v, want ErrTOTPReplay", err)
	}
	later := time.Unix(at.Unix()+60, 0)
	s.now = func() time.Time { return later }
	fresh, err := s.Verify(ctx, 7, computeCode(secret, stepFor(later.Unix(), 30), 6))
	if err != nil {
		t.Errorf("later-step login = %v, want nil", err)
	}
	if fresh != nil {
		t.Errorf("confirmed account should not re-mint recovery codes, got %d", len(fresh))
	}
}

func TestEnrollConfirmedForTx_WipesPriorRows(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	s, f, _ := newTestStoreAt(t, at)
	ctx := context.Background()

	// Simulate a reset over an account that already has a (stale) TOTP row +
	// old recovery codes.
	f.totpRow = &db.TotpCredential{AccountID: 7, KeyVersion: 1, Period: 30, Digits: 6, Algorithm: "SHA1", LastStep: 999, ConfirmedAt: pgtype.Timestamptz{Time: at, Valid: true}}
	f.recoveryRows = []db.RecoveryCode{{ID: 100, AccountID: 7, Hash: "old"}, {ID: 101, AccountID: 7, Hash: "old"}}
	f.nextRecID = 200

	enr, err := s.GenerateEnrollment("alice")
	if err != nil {
		t.Fatalf("GenerateEnrollment: %v", err)
	}
	codes, err := s.EnrollConfirmedForTx(ctx, f, 7, enr.SecretBase32, 0)
	if err != nil {
		t.Fatalf("EnrollConfirmedForTx: %v", err)
	}
	if len(codes) != 10 {
		t.Fatalf("recovery codes = %d, want 10", len(codes))
	}
	// Exactly the 10 fresh rows remain — the two stale codes were wiped.
	if len(f.recoveryRows) != 10 {
		t.Errorf("recovery rows = %d, want 10 (stale wiped)", len(f.recoveryRows))
	}
	for _, r := range f.recoveryRows {
		if r.Hash == "old" {
			t.Error("stale recovery code survived the reset")
		}
	}
}
