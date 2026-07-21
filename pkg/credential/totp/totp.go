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
	"github.com/jackc/pgx/v5/pgxpool"

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
	// ErrTOTPCorrupt is returned by Verify when the stored ciphertext fails
	// AES-GCM authentication. The HTTP layer collapses this to a generic
	// bad-credentials response so a tampered/rotated-DEK-with-stale-row
	// state doesn't leak crypto-failure detail to clients (Bundle-3 Crypto-6).
	// The underlying decrypt failure is recorded server-side via audit
	// EventFail with detail.reason="decrypt_failed" before this sentinel
	// is returned.
	ErrTOTPCorrupt = errors.New("totp: stored secret is corrupt or DEK rotated improperly")
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
	UpdateTOTPLastStep(ctx context.Context, arg db.UpdateTOTPLastStepParams) (int64, error)

	ListRecoveryCodesByAccount(ctx context.Context, accountID int32) ([]db.RecoveryCode, error)
	InsertRecoveryCode(ctx context.Context, arg db.InsertRecoveryCodeParams) (db.RecoveryCode, error)
	ConsumeRecoveryCode(ctx context.Context, arg db.ConsumeRecoveryCodeParams) (db.RecoveryCode, error)
	DeleteAllRecoveryCodesByAccount(ctx context.Context, accountID int32) error
}

// TxRunner executes fn inside a single database transaction, supplying a
// TOTPQueries handle bound to that transaction. If fn returns a non-nil error
// the transaction is rolled back; otherwise it is committed. The Store uses
// this to make recovery-code mints atomic with the credential row write that
// gates them (audit Medium #2 / #3).
//
// Production wires this to a *pgxpool.Pool — see NewPoolTxRunner. Tests inject
// an in-memory implementation that snapshots/restores the fake's state on
// rollback so the rollback path can be exercised end-to-end.
type TxRunner interface {
	InTx(ctx context.Context, fn func(q TOTPQueries) error) error
}

// PoolTxRunner is the production TxRunner: BEGIN on a *pgxpool.Pool, run fn
// against db.Queries.WithTx(tx), COMMIT or ROLLBACK. The base *db.Queries
// is captured so we don't need a second handle just to call WithTx.
type PoolTxRunner struct {
	Pool    *pgxpool.Pool
	Queries *db.Queries
}

func (p *PoolTxRunner) InTx(ctx context.Context, fn func(q TOTPQueries) error) error {
	tx, err := p.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("totp.tx: begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback is a no-op after commit
	if err := fn(p.Queries.WithTx(tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("totp.tx: commit: %w", err)
	}
	return nil
}

type Store struct {
	q             TOTPQueries
	tx            TxRunner
	deks          map[int][]byte
	currentKeyVer int32
	cfg           configx.TOTPConfig
	throttle      *authn.Throttle
	audit         audit.Writer
	now           func() time.Time
}

func NewStore(q TOTPQueries, tx TxRunner, deks map[int][]byte, cfg configx.TOTPConfig, throttle *authn.Throttle, w audit.Writer) *Store {
	var current int32 = 1
	for v := range deks {
		if int32(v) > current {
			current = int32(v)
		}
	}
	return &Store{
		q:             q,
		tx:            tx,
		deks:          deks,
		currentKeyVer: current,
		cfg:           cfg,
		throttle:      throttle,
		audit:         w,
		now:           time.Now,
	}
}

// Begin starts a fresh TOTP enrollment. Wipes any prior TOTP credential row
// AND all recovery codes for the account, then inserts a new unconfirmed row.
// This is the normal /me/totp/begin path: starting over from scratch.
//
// Use BeginPreservingRecovery for the recovery-ceremony path
// (/auth/recovery/totp/begin), which must keep the remaining recovery codes
// live until the new TOTP is successfully confirmed at /verify — so the
// user can retry recovery with a different code if they abandon mid-ceremony.
func (s *Store) Begin(ctx context.Context, accountID int32, username string) (*Enrollment, error) {
	return s.begin(ctx, accountID, username, true /* wipeRecovery */, "reenroll")
}

// BeginPreservingRecovery is the recovery-ceremony variant of Begin: wipes the
// old TOTP credential row but leaves recovery codes untouched. The remaining
// recovery codes are wiped on a successful VerifyAndCommitRecovery (atomic
// with the new-batch mint).
//
// Rationale: if the user starts /auth/recovery/totp/begin but never completes
// /auth/recovery/totp/verify (e.g., walks away), they must still be able to
// retry recovery with another recovery code. Wiping at /begin would brick the
// account.
func (s *Store) BeginPreservingRecovery(ctx context.Context, accountID int32, username string) (*Enrollment, error) {
	return s.begin(ctx, accountID, username, false /* wipeRecovery */, "recovery")
}

// begin is the shared implementation of Begin / BeginPreservingRecovery. The
// wipeRecovery flag controls whether the recovery-code rows are wiped along
// with the TOTP credential row. revokeReason flows into the audit detail
// so investigators can distinguish a normal re-enrollment from a recovery
// ceremony.
func (s *Store) begin(ctx context.Context, accountID int32, username string, wipeRecovery bool, revokeReason string) (*Enrollment, error) {
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

	// Wipe any prior TOTP row (confirmed or unconfirmed) before inserting
	// the new enrollment. Caller is responsible for sudo gating; this
	// Store reset is unconditional once begin is reached.
	//
	// Audit-revoke pre-existing material BEFORE delete (audit Medium #3).
	// Without this, the credential_event log shows registers without matching
	// revokes — investigators can't see when a batch was wiped. Best-effort
	// emit: a record failure does not block re-enrollment.
	if oldRow, gerr := s.q.GetTOTPCredential(ctx, accountID); gerr == nil && oldRow.ConfirmedAt.Valid {
		audit.RecordOrLog(ctx, s.audit, audit.Record{
			AccountID: &accountID,
			Factor:    audit.FactorTOTP,
			Event:     audit.EventRevoke,
			Detail:    map[string]any{"reason": revokeReason},
		})
	}
	if wipeRecovery {
		if existing, lerr := s.q.ListRecoveryCodesByAccount(ctx, accountID); lerr == nil {
			for range existing {
				audit.RecordOrLog(ctx, s.audit, audit.Record{
					AccountID: &accountID,
					Factor:    audit.FactorRecoveryCode,
					Event:     audit.EventRevoke,
					Detail:    map[string]any{"reason": revokeReason},
				})
			}
		}
	}
	_ = s.q.DeleteTOTPCredential(ctx, accountID)
	if wipeRecovery {
		_ = s.q.DeleteAllRecoveryCodesByAccount(ctx, accountID)
	}

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
	return s.verify(ctx, accountID, code, false /* purgePriorRecoveryOnFirstConfirm */)
}

// VerifyAndCommitRecovery is the recovery-ceremony variant of Verify. Same
// drift / replay / throttle semantics, but on the first-confirm transaction
// it ALSO deletes any remaining recovery codes (the ones that survived
// /auth/recovery/totp/begin) inside the same transaction as the mint of the
// fresh batch. Emits one recovery_code/revoke audit event per wiped code
// with detail.reason="recovery_complete" so the trail shows a clean
// before/after for the ceremony.
//
// On TOTP failure: the recovery-session-token caller is expected to have
// already consumed (Pop'd) the token at the HTTP layer, so the user must
// restart recovery from /auth/recovery-code/verify. This is intentional:
// keeping the token live for retry creates atomicity hazards we'd rather
// not chase.
func (s *Store) VerifyAndCommitRecovery(ctx context.Context, accountID int32, code string) ([]string, error) {
	return s.verify(ctx, accountID, code, true /* purgePriorRecoveryOnFirstConfirm */)
}

// verify is the shared implementation. purgePriorRecoveryOnFirstConfirm
// controls whether the first-confirm transaction also wipes the existing
// recovery codes (recovery ceremony) before minting the fresh batch.
func (s *Store) verify(ctx context.Context, accountID int32, code string, purgePriorRecoveryOnFirstConfirm bool) ([]string, error) {
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
		// Record the underlying failure mode for forensics — this is the
		// only place we have the original error string. Then collapse to
		// ErrTOTPCorrupt so the HTTP layer can return a generic
		// bad-credentials response without leaking "cipher: message
		// authentication failed" or similar AES-GCM diagnostics.
		audit.RecordOrLog(ctx, s.audit, audit.Record{
			AccountID: &accountID,
			Factor:    audit.FactorTOTP,
			Event:     audit.EventFail,
			Detail:    map[string]any{"reason": "decrypt_failed"},
		})
		return nil, ErrTOTPCorrupt
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
		audit.RecordOrLog(ctx, s.audit, audit.Record{AccountID: &accountID, Factor: audit.FactorTOTP, Event: audit.EventFail})
		return nil, ErrTOTPInvalidCode
	}
	// RFC 6238 §5.2: a code accepted at step T may not be accepted again at
	// any step ≤ T. The DB-side UPDATE also enforces $2 > last_step, but
	// we surface the replay error here before mutating audit state.
	if matchedStep <= row.LastStep {
		_, _ = s.throttle.RegisterFailure(ctx, accountID, "totp")
		audit.RecordOrLog(ctx, s.audit, audit.Record{
			AccountID: &accountID,
			Factor:    audit.FactorTOTP,
			Event:     audit.EventFail,
			Detail:    map[string]any{"reason": "replay"},
		})
		return nil, ErrTOTPReplay
	}

	// The SQL UPDATE is the atomic gate: WHERE $2 > last_step. Under K-way
	// concurrency only one caller's row matches; the rest see pgx.ErrNoRows
	// (because the query is :one RETURNING last_step) and we treat that as a
	// replay-race loss. The Go-side check above short-circuits the common
	// serial replay before the DB round-trip; this catches the race.
	if _, err := s.q.UpdateTOTPLastStep(ctx, db.UpdateTOTPLastStepParams{
		AccountID: accountID,
		LastStep:  matchedStep,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			_, _ = s.throttle.RegisterFailure(ctx, accountID, "totp")
			audit.RecordOrLog(ctx, s.audit, audit.Record{
				AccountID: &accountID,
				Factor:    audit.FactorTOTP,
				Event:     audit.EventFail,
				Detail:    map[string]any{"reason": "replay"},
			})
			return nil, ErrTOTPReplay
		}
		return nil, fmt.Errorf("totp.Verify: update last_step: %w", err)
	}
	_ = s.throttle.Reset(ctx, accountID, "totp")
	audit.RecordOrLog(ctx, s.audit, audit.Record{AccountID: &accountID, Factor: audit.FactorTOTP, Event: audit.EventUse})

	if !row.ConfirmedAt.Valid {
		// Atomic confirm + recovery-code mint (audit Medium #2). Prior
		// implementation called ConfirmTOTPCredential then looped 10x over
		// InsertRecoveryCode with no transaction. A failure on insert #5 left
		// the row confirmed but the caller saw an error and never received the
		// codes — subsequent verifies skipped this branch (row already
		// confirmed), stranding the user with 4 invisible codes and zero
		// plaintext for the other 6.
		//
		// Ordering note: UpdateTOTPLastStep + throttle Reset + audit EventUse
		// have already happened OUTSIDE the tx above. If the tx fails, the
		// step bump is retained — re-using the same code on retry is still
		// rejected — and the next code at the next step boundary will see
		// row.ConfirmedAt.Valid == false and re-enter this branch.
		//
		// purgePriorRecoveryOnFirstConfirm (recovery-ceremony path):
		// snapshot existing codes (for post-commit revoke audits), wipe
		// them inside the same tx, then mint the fresh batch. The wipe
		// must be inside the tx so a mid-mint failure rolls back the
		// delete — otherwise the user loses every recovery code with
		// no replacement on the failure path.
		var (
			codes      []string
			wipedCount int
		)
		txErr := s.tx.InTx(ctx, func(q TOTPQueries) error {
			if purgePriorRecoveryOnFirstConfirm {
				existing, err := q.ListRecoveryCodesByAccount(ctx, accountID)
				if err != nil {
					return fmt.Errorf("list prior recovery: %w", err)
				}
				wipedCount = len(existing)
				if err := q.DeleteAllRecoveryCodesByAccount(ctx, accountID); err != nil {
					return fmt.Errorf("delete prior recovery: %w", err)
				}
			}
			if err := q.ConfirmTOTPCredential(ctx, accountID); err != nil {
				return fmt.Errorf("confirm: %w", err)
			}
			minted, err := s.mintRecoveryCodesNoAudit(ctx, q, accountID)
			if err != nil {
				return fmt.Errorf("mint recovery: %w", err)
			}
			codes = minted
			return nil
		})
		if txErr != nil {
			return nil, fmt.Errorf("totp.Verify: %w", txErr)
		}
		// Emit audit events AFTER commit so the trail reflects what actually
		// persisted (a commit failure that leaves audit registers behind is
		// worse for forensics than the symmetric pre-commit alternative).
		if purgePriorRecoveryOnFirstConfirm {
			// Symmetric per-row revokes for the wiped recovery codes,
			// matching RegenerateRecoveryCodes' pattern. Investigators
			// see one revoke per code with reason=recovery_complete,
			// followed by the totp/register + 10 fresh registers.
			for i := 0; i < wipedCount; i++ {
				audit.RecordOrLog(ctx, s.audit, audit.Record{
					AccountID: &accountID,
					Factor:    audit.FactorRecoveryCode,
					Event:     audit.EventRevoke,
					Detail:    map[string]any{"reason": "recovery_complete"},
				})
			}
		}
		audit.RecordOrLog(ctx, s.audit, audit.Record{
			AccountID: &accountID,
			Factor:    audit.FactorTOTP,
			Event:     audit.EventRegister,
		})
		for range codes {
			audit.RecordOrLog(ctx, s.audit, audit.Record{
				AccountID: &accountID,
				Factor:    audit.FactorRecoveryCode,
				Event:     audit.EventRegister,
			})
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
		audit.RecordOrLog(ctx, s.audit, audit.Record{AccountID: &accountID, Factor: audit.FactorRecoveryCode, Event: audit.EventFail})
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
		audit.RecordOrLog(ctx, s.audit, audit.Record{AccountID: &accountID, Factor: audit.FactorRecoveryCode, Event: audit.EventUse})
		return nil
	}

	_, _ = s.throttle.RegisterFailure(ctx, accountID, "recovery_code")
	audit.RecordOrLog(ctx, s.audit, audit.Record{AccountID: &accountID, Factor: audit.FactorRecoveryCode, Event: audit.EventFail})
	return ErrRecoveryCodeInvalid
}

func (s *Store) RegenerateRecoveryCodes(ctx context.Context, accountID int32) ([]string, error) {
	// Snapshot the pre-existing batch so we can emit one revoke audit event
	// per deleted code — symmetric with mintRecoveryCodes emitting one
	// register per code (audit Medium #3). List BEFORE the tx so a
	// list error is reported but does not block the delete: audit is
	// best-effort.
	existing, _ := s.q.ListRecoveryCodesByAccount(ctx, accountID)

	// Atomic delete + mint (audit Medium #2). The prior implementation
	// deleted then minted with no transaction — an error mid-mint left the
	// caller with zero codes and an error, after their old codes had already
	// been wiped.
	var codes []string
	txErr := s.tx.InTx(ctx, func(q TOTPQueries) error {
		if err := q.DeleteAllRecoveryCodesByAccount(ctx, accountID); err != nil {
			return fmt.Errorf("delete: %w", err)
		}
		minted, err := s.mintRecoveryCodesNoAudit(ctx, q, accountID)
		if err != nil {
			return fmt.Errorf("mint: %w", err)
		}
		codes = minted
		return nil
	})
	if txErr != nil {
		return nil, fmt.Errorf("totp.RegenerateRecoveryCodes: %w", txErr)
	}
	// Audit revoke for each deleted code, then register for each new one —
	// per-row matches mintRecoveryCodes's emission pattern and gives
	// investigators a clean before/after trail. Emit after commit so the
	// audit reflects what actually persisted.
	for range existing {
		audit.RecordOrLog(ctx, s.audit, audit.Record{
			AccountID: &accountID,
			Factor:    audit.FactorRecoveryCode,
			Event:     audit.EventRevoke,
			Detail:    map[string]any{"reason": "regenerate"},
		})
	}
	for range codes {
		audit.RecordOrLog(ctx, s.audit, audit.Record{
			AccountID: &accountID,
			Factor:    audit.FactorRecoveryCode,
			Event:     audit.EventRegister,
		})
	}
	return codes, nil
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
	audit.RecordOrLog(ctx, s.audit, audit.Record{AccountID: &accountID, Factor: audit.FactorTOTP, Event: audit.EventRevoke})
	return nil
}

// mintRecoveryCodesNoAudit generates and persists a fresh batch of recovery
// codes using the supplied TOTPQueries handle (which the caller binds to a
// pgx.Tx). Audit emission is the caller's responsibility — done AFTER the
// surrounding transaction commits so the credential_event log only reflects
// state that actually persisted. If any insert fails, the partial codes are
// discarded and the surrounding tx is rolled back by the caller.
func (s *Store) mintRecoveryCodesNoAudit(ctx context.Context, q TOTPQueries, accountID int32) ([]string, error) {
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
		if _, err := q.InsertRecoveryCode(ctx, db.InsertRecoveryCodeParams{
			AccountID: accountID,
			Hash:      phc,
		}); err != nil {
			return nil, err
		}
		codes = append(codes, code)
	}
	return codes, nil
}

// GenerateEnrollment produces a fresh TOTP secret + provisioning URI WITHOUT
// any database write or at-rest encryption. The password-totp enrollment
// ceremony (/enrollments/{token}/password-totp/begin) calls this for accounts
// that may not exist yet: the base32 secret is stashed in KV + returned to the
// client for the QR, and only persisted — encrypted, bound to the new
// account_id via the AAD — once the account row exists at "verify" (see
// EnrollConfirmedForTx). Contrast Begin, which writes an unconfirmed row and so
// requires an existing account_id.
func (s *Store) GenerateEnrollment(username string) (*Enrollment, error) {
	secret := make([]byte, 20)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("totp.GenerateEnrollment: rand: %w", err)
	}
	secretB32 := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secret)
	return &Enrollment{
		SecretBase32:    secretB32,
		ProvisioningURI: provisioningURI(s.cfg.Issuer, username, secretB32, s.cfg.DefaultAlgorithm, s.cfg.DefaultDigits, s.cfg.DefaultPeriod),
	}, nil
}

// VerifyCandidateSecret checks a code against a base32 secret that has NOT been
// persisted yet (the password-totp enrollment "verify" step). It runs the same
// ±DriftSteps window as Verify but touches no DB row, no throttle, and no
// audit — the caller owns those. Returns the matched step (for seeding
// last_step so the confirming code can't be replayed as an immediate login)
// and whether the code was accepted. A malformed base32 secret yields
// (0, false).
func (s *Store) VerifyCandidateSecret(secretB32, code string) (int64, bool) {
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secretB32)
	if err != nil {
		return 0, false
	}
	nowStep := stepFor(s.now().Unix(), int64(s.cfg.DefaultPeriod))
	drift := int64(s.cfg.DriftSteps)
	for delta := -drift; delta <= drift; delta++ {
		if computeCode(secret, nowStep+delta, s.cfg.DefaultDigits) == code {
			return nowStep + delta, true
		}
	}
	return 0, false
}

// EnrollConfirmedForTx persists a CONFIRMED TOTP credential + a fresh batch of
// recovery codes for accountID, inside the caller's transaction (q bound to a
// pgx.Tx). It is the password-totp enrollment "verify" commit path:
//   - encrypt secretB32 under the current DEK, bound to account_id via the AAD;
//   - idempotently wipe any pre-existing TOTP + recovery rows (makes the reset
//     intent safe to re-run);
//   - insert + confirm the new row;
//   - seed last_step to the confirming code's step so it can't be replayed as
//     an immediate login (mirrors Verify's replay hardening);
//   - mint and return the recovery-code plaintexts.
//
// Emits NO audit — the caller emits password/totp/recovery register events
// AFTER the surrounding tx commits (mirrors mintRecoveryCodesNoAudit's
// contract), so credential_event only reflects persisted state and the FK to a
// freshly-inserted account resolves on all connections.
func (s *Store) EnrollConfirmedForTx(ctx context.Context, q TOTPQueries, accountID int32, secretB32 string, lastStep int64) ([]string, error) {
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secretB32)
	if err != nil {
		return nil, fmt.Errorf("totp.EnrollConfirmedForTx: decode secret: %w", err)
	}
	dek, ok := s.deks[int(s.currentKeyVer)]
	if !ok {
		return nil, fmt.Errorf("totp.EnrollConfirmedForTx: no DEK for version %d", s.currentKeyVer)
	}
	ct, nonce, err := encryptSecret(dek, secret, aadFor(accountID, s.currentKeyVer))
	if err != nil {
		return nil, fmt.Errorf("totp.EnrollConfirmedForTx: encrypt: %w", err)
	}
	if err := q.DeleteTOTPCredential(ctx, accountID); err != nil {
		return nil, fmt.Errorf("totp.EnrollConfirmedForTx: delete prior totp: %w", err)
	}
	if err := q.DeleteAllRecoveryCodesByAccount(ctx, accountID); err != nil {
		return nil, fmt.Errorf("totp.EnrollConfirmedForTx: delete prior recovery: %w", err)
	}
	if _, err := q.InsertTOTPCredential(ctx, db.InsertTOTPCredentialParams{
		AccountID:   accountID,
		SecretEnc:   ct,
		SecretNonce: nonce,
		KeyVersion:  s.currentKeyVer,
		Period:      int32(s.cfg.DefaultPeriod),
		Digits:      int32(s.cfg.DefaultDigits),
		Algorithm:   s.cfg.DefaultAlgorithm,
	}); err != nil {
		return nil, fmt.Errorf("totp.EnrollConfirmedForTx: insert: %w", err)
	}
	if err := q.ConfirmTOTPCredential(ctx, accountID); err != nil {
		return nil, fmt.Errorf("totp.EnrollConfirmedForTx: confirm: %w", err)
	}
	if lastStep > 0 {
		if _, err := q.UpdateTOTPLastStep(ctx, db.UpdateTOTPLastStepParams{
			AccountID: accountID,
			LastStep:  lastStep,
		}); err != nil {
			return nil, fmt.Errorf("totp.EnrollConfirmedForTx: seed last_step: %w", err)
		}
	}
	return s.mintRecoveryCodesNoAudit(ctx, q, accountID)
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
