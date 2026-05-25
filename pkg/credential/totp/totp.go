// Package totp — totp.go
//
// RFC 6238 TOTP store: enrollment, verification with ±N step drift, and
// 80-bit single-use recovery codes. The cryptographic primitives are split
// across sibling files (code.go for HMAC-based code computation, aead.go
// for AES-256-GCM at-rest encryption, recovery.go for the recovery code
// format/hash). No third-party OTP library — the algorithm is small enough
// that the supply-chain surface isn't worth it.
//
// All persistent state lives behind the narrow TOTPQueries interface, which
// is a strict subset of db.Querier. Tests inject in-memory fakes.

package totp

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/db"
)

var (
	ErrTOTPNotSet          = errors.New("totp: not set")
	ErrTOTPInvalidCode     = errors.New("totp: invalid code")
	ErrTOTPReplay          = errors.New("totp: code already used")
	ErrRecoveryCodeInvalid = errors.New("totp: invalid recovery code")
)

type Enrollment struct {
	SecretBase32    string
	ProvisioningURI string
	RecoveryCodes   []string
}

// TOTPQueries is the narrow subset of db.Querier the store touches directly.
// Throttle and audit queries are consumed through the *authn.Throttle and
// audit.Writer parameters passed to NewStore, so we don't restate them here —
// the production wiring satisfies all three from the same *db.Queries handle,
// and tests pass a single fake that implements every method.
type TOTPQueries interface {
	GetTOTPCredential(ctx context.Context, accountID int32) (db.TotpCredential, error)
	InsertTOTPCredential(ctx context.Context, arg db.InsertTOTPCredentialParams) (db.TotpCredential, error)
	DeleteTOTPCredential(ctx context.Context, accountID int32) error
	ConfirmTOTPCredential(ctx context.Context, accountID int32) error
	UpdateTOTPLastStep(ctx context.Context, arg db.UpdateTOTPLastStepParams) error

	ListRecoveryCodesByAccount(ctx context.Context, accountID int32) ([]db.RecoveryCode, error)
	InsertRecoveryCode(ctx context.Context, arg db.InsertRecoveryCodeParams) (db.RecoveryCode, error)
	ConsumeRecoveryCode(ctx context.Context, arg db.ConsumeRecoveryCodeParams) (db.RecoveryCode, error)
	DeleteAllRecoveryCodesByAccount(ctx context.Context, accountID int32) error
}

type Store struct {
	q             TOTPQueries
	deks          map[int][]byte
	currentKeyVer int32
	cfg           configx.TOTPConfig
	throttle      *authn.Throttle
	audit         audit.Writer
	now           func() time.Time
}

func NewStore(q TOTPQueries, deks map[int][]byte, cfg configx.TOTPConfig, throttle *authn.Throttle, w audit.Writer) *Store {
	var current int32 = 1
	for v := range deks {
		if int32(v) > current {
			current = int32(v)
		}
	}
	return &Store{
		q:             q,
		deks:          deks,
		currentKeyVer: current,
		cfg:           cfg,
		throttle:      throttle,
		audit:         w,
		now:           time.Now,
	}
}

func (s *Store) Begin(ctx context.Context, accountID int32, username string) (*Enrollment, error) {
	// RFC 6238 §4: secret SHOULD be at least as long as the HMAC digest
	// output. SHA-1 → 20 bytes / 160 bits.
	secret := make([]byte, 20)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("totp.Begin: rand: %w", err)
	}
	dek, ok := s.deks[int(s.currentKeyVer)]
	if !ok {
		return nil, fmt.Errorf("totp.Begin: no DEK for version %d", s.currentKeyVer)
	}
	ct, nonce, err := encryptSecret(dek, secret, aadFor(accountID, s.currentKeyVer))
	if err != nil {
		return nil, fmt.Errorf("totp.Begin: encrypt: %w", err)
	}

	// Wipe any prior row (confirmed or unconfirmed) and its recovery codes
	// before inserting the new enrollment. Caller (the /me handler in
	// Task 7) is responsible for sudo gating; this Store reset is
	// unconditional once Begin is reached.
	_ = s.q.DeleteTOTPCredential(ctx, accountID)
	_ = s.q.DeleteAllRecoveryCodesByAccount(ctx, accountID)

	if _, err := s.q.InsertTOTPCredential(ctx, db.InsertTOTPCredentialParams{
		AccountID:   accountID,
		SecretEnc:   ct,
		SecretNonce: nonce,
		KeyVersion:  s.currentKeyVer,
		Period:      int32(s.cfg.DefaultPeriod),
		Digits:      int32(s.cfg.DefaultDigits),
		Algorithm:   s.cfg.DefaultAlgorithm,
	}); err != nil {
		return nil, fmt.Errorf("totp.Begin: insert: %w", err)
	}

	secretB32 := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secret)
	return &Enrollment{
		SecretBase32:    secretB32,
		ProvisioningURI: provisioningURI(s.cfg.Issuer, username, secretB32, s.cfg.DefaultAlgorithm, s.cfg.DefaultDigits, s.cfg.DefaultPeriod),
	}, nil
}

// Verify checks a 6-digit code against the stored secret. On the FIRST
// successful verify it also confirms the enrollment and mints 10 recovery
// codes, returning their plaintext. On subsequent successful verifies it
// returns (nil, nil). On failure (invalid code, replay, or missing row) it
// returns the appropriate sentinel and bumps the throttle.
func (s *Store) Verify(ctx context.Context, accountID int32, code string) ([]string, error) {
	if _, err := s.throttle.CheckLocked(ctx, accountID, "totp"); err != nil {
		return nil, err
	}
	row, err := s.q.GetTOTPCredential(ctx, accountID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrTOTPNotSet
		}
		return nil, fmt.Errorf("totp.Verify: get: %w", err)
	}

	dek, ok := s.deks[int(row.KeyVersion)]
	if !ok {
		return nil, fmt.Errorf("totp.Verify: missing DEK version %d", row.KeyVersion)
	}
	secret, err := decryptSecret(dek, row.SecretEnc, row.SecretNonce, aadFor(accountID, row.KeyVersion))
	if err != nil {
		return nil, fmt.Errorf("totp.Verify: decrypt: %w", err)
	}

	nowStep := stepFor(s.now().Unix(), int64(row.Period))
	drift := int64(s.cfg.DriftSteps)
	matchedStep := int64(-1)
	for delta := -drift; delta <= drift; delta++ {
		candidate := computeCode(secret, nowStep+delta, int(row.Digits))
		if candidate == code {
			matchedStep = nowStep + delta
			break
		}
	}
	if matchedStep < 0 {
		_, _ = s.throttle.RegisterFailure(ctx, accountID, "totp")
		_ = s.audit.Record(ctx, audit.Record{AccountID: &accountID, Factor: audit.FactorTOTP, Event: audit.EventFail})
		return nil, ErrTOTPInvalidCode
	}
	// RFC 6238 §5.2: a code accepted at step T may not be accepted again at
	// any step ≤ T. The DB-side UPDATE also enforces $2 > last_step, but
	// we surface the replay error here before mutating audit state.
	if matchedStep <= row.LastStep {
		_, _ = s.throttle.RegisterFailure(ctx, accountID, "totp")
		_ = s.audit.Record(ctx, audit.Record{
			AccountID: &accountID,
			Factor:    audit.FactorTOTP,
			Event:     audit.EventFail,
			Detail:    map[string]any{"reason": "replay"},
		})
		return nil, ErrTOTPReplay
	}

	if err := s.q.UpdateTOTPLastStep(ctx, db.UpdateTOTPLastStepParams{
		AccountID: accountID,
		LastStep:  matchedStep,
	}); err != nil {
		return nil, fmt.Errorf("totp.Verify: update last_step: %w", err)
	}
	_ = s.throttle.Reset(ctx, accountID, "totp")
	_ = s.audit.Record(ctx, audit.Record{AccountID: &accountID, Factor: audit.FactorTOTP, Event: audit.EventUse})

	if !row.ConfirmedAt.Valid {
		if err := s.q.ConfirmTOTPCredential(ctx, accountID); err != nil {
			return nil, fmt.Errorf("totp.Verify: confirm: %w", err)
		}
		// First successful verify is the registration milestone — the row
		// existed since Begin() but is unusable until confirmed. Emit the
		// audit event here (rather than at Begin) so credential_event reflects
		// the factor's actual go-live moment. cmd/smoke step 45 asserts this.
		_ = s.audit.Record(ctx, audit.Record{
			AccountID: &accountID,
			Factor:    audit.FactorTOTP,
			Event:     audit.EventRegister,
		})
		codes, err := s.mintRecoveryCodes(ctx, accountID)
		if err != nil {
			return nil, fmt.Errorf("totp.Verify: mint recovery: %w", err)
		}
		return codes, nil
	}
	return nil, nil
}

func (s *Store) VerifyRecoveryCode(ctx context.Context, accountID int32, code, sessionID, ip string) error {
	if _, err := s.throttle.CheckLocked(ctx, accountID, "recovery_code"); err != nil {
		return err
	}
	normalized := normalizeRecoveryCode(code)
	if len(normalized) != recoveryCodeLen {
		_, _ = s.throttle.RegisterFailure(ctx, accountID, "recovery_code")
		_ = s.audit.Record(ctx, audit.Record{AccountID: &accountID, Factor: audit.FactorRecoveryCode, Event: audit.EventFail})
		return ErrRecoveryCodeInvalid
	}

	rows, err := s.q.ListRecoveryCodesByAccount(ctx, accountID)
	if err != nil {
		return fmt.Errorf("totp.VerifyRecoveryCode: list: %w", err)
	}

	sessParam := pgtype.Text{String: sessionID, Valid: sessionID != ""}
	ipParam := audit.ParseIPOrNil(ip)

	for _, row := range rows {
		if !verifyRecoveryCode(normalized, row.Hash) {
			continue
		}
		if _, err := s.q.ConsumeRecoveryCode(ctx, db.ConsumeRecoveryCodeParams{
			ID:            row.ID,
			UsedSessionID: sessParam,
			UsedIp:        ipParam,
		}); err != nil {
			return fmt.Errorf("totp.VerifyRecoveryCode: consume: %w", err)
		}
		_ = s.throttle.Reset(ctx, accountID, "recovery_code")
		_ = s.audit.Record(ctx, audit.Record{AccountID: &accountID, Factor: audit.FactorRecoveryCode, Event: audit.EventUse})
		return nil
	}

	_, _ = s.throttle.RegisterFailure(ctx, accountID, "recovery_code")
	_ = s.audit.Record(ctx, audit.Record{AccountID: &accountID, Factor: audit.FactorRecoveryCode, Event: audit.EventFail})
	return ErrRecoveryCodeInvalid
}

func (s *Store) RegenerateRecoveryCodes(ctx context.Context, accountID int32) ([]string, error) {
	if err := s.q.DeleteAllRecoveryCodesByAccount(ctx, accountID); err != nil {
		return nil, fmt.Errorf("totp.RegenerateRecoveryCodes: delete: %w", err)
	}
	return s.mintRecoveryCodes(ctx, accountID)
}

// Delete removes the totp_credential row only. Recovery codes are not
// touched — they cascade from account, not from totp_credential. Callers
// performing a full factor revocation should also call
// DeleteAllRecoveryCodesByAccount (or use authn.DisableNonWebAuthnFallbacks
// which handles all three rowsets transactionally).
func (s *Store) Delete(ctx context.Context, accountID int32) error {
	if err := s.q.DeleteTOTPCredential(ctx, accountID); err != nil {
		return fmt.Errorf("totp.Delete: %w", err)
	}
	_ = s.audit.Record(ctx, audit.Record{AccountID: &accountID, Factor: audit.FactorTOTP, Event: audit.EventRevoke})
	return nil
}

func (s *Store) mintRecoveryCodes(ctx context.Context, accountID int32) ([]string, error) {
	count := s.cfg.RecoveryCodeCount
	if count <= 0 {
		count = 10
	}
	codes := make([]string, 0, count)
	for i := 0; i < count; i++ {
		code, err := generateRecoveryCode()
		if err != nil {
			return nil, err
		}
		normalized := normalizeRecoveryCode(code)
		phc, err := hashRecoveryCode(normalized)
		if err != nil {
			return nil, err
		}
		if _, err := s.q.InsertRecoveryCode(ctx, db.InsertRecoveryCodeParams{
			AccountID: accountID,
			Hash:      phc,
		}); err != nil {
			return nil, err
		}
		codes = append(codes, code)
		_ = s.audit.Record(ctx, audit.Record{AccountID: &accountID, Factor: audit.FactorRecoveryCode, Event: audit.EventRegister})
	}
	return codes, nil
}

func provisioningURI(issuer, username, secretB32, algorithm string, digits, period int) string {
	label := url.PathEscape(issuer + ":" + username)
	q := url.Values{}
	q.Set("secret", secretB32)
	q.Set("issuer", issuer)
	q.Set("algorithm", algorithm)
	q.Set("digits", strconv.Itoa(digits))
	q.Set("period", strconv.Itoa(period))
	return "otpauth://totp/" + label + "?" + q.Encode()
}
