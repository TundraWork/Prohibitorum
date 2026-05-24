package totp

import (
	"context"
	"errors"

	"prohibitorum/pkg/db"
)

var (
	ErrTOTPNotSet      = errors.New("totp: not set")
	ErrTOTPUnconfirmed = errors.New("totp: enrollment not confirmed")
	ErrTOTPInvalidCode = errors.New("totp: invalid code")
	// RFC 6238 §5.2: a code must not be accepted more than once within its window.
	ErrTOTPReplay = errors.New("totp: code already used")
)

type Enrollment struct {
	SecretBase32    string
	ProvisioningURI string
	RecoveryCodes   []string
}

type Store struct {
	q db.Querier
}

func NewStore(q db.Querier) *Store {
	return &Store{q: q}
}

// TODO(v0.2): generate secret (RFC 6238 §3), AES-GCM encrypt with AAD
// 'totp:'||account_id||':'||key_version, insert into totp_credential
// (confirmed_at NULL), mint 10 recovery codes (argon2id-hash, insert).
func (s *Store) Begin(ctx context.Context, accountID int32, label string) (*Enrollment, error) {
	return nil, errors.New("totp.Begin: TODO(v0.2)")
}

// TODO(v0.2): GetTOTPCredential, decrypt with AAD, compute T = unix/period,
// reject if T <= last_step, otherwise UpdateTOTPLastStep + ConfirmTOTPCredential.
func (s *Store) Verify(ctx context.Context, accountID int32, code string) error {
	return ErrTOTPNotSet
}

// TODO(v0.2): argon2id-verify against each ListRecoveryCodesByAccount row;
// first match → ConsumeRecoveryCode with session id + IP.
func (s *Store) VerifyRecoveryCode(ctx context.Context, accountID int32, code string, sessionID string, ip string) error {
	return ErrTOTPInvalidCode
}
