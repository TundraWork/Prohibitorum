// Package password — password.go
//
// Argon2id PHC-string storage and verification per RFC 9106 + OWASP 2026.
// Self-describing PHC format lets us upgrade params over time and re-hash
// transparently on next verify.
//
// Throttle and audit are cross-cutting helpers reused from pkg/authn and
// pkg/audit. The HTTP layer is responsible for the username-enumeration
// timing equalization (it calls VerifyAgainstDummy when no row exists);
// Verify here only handles the row-present case.

package password

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/argon2"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/db"
)

var (
	ErrPasswordNotSet    = errors.New("password: not set")
	ErrPasswordIncorrect = errors.New("password: incorrect")
)

const (
	tagLength  = 32
	saltLength = 16
)

type PasswordQueries interface {
	GetPasswordCredential(ctx context.Context, accountID int32) (db.PasswordCredential, error)
	UpsertPasswordCredential(ctx context.Context, arg db.UpsertPasswordCredentialParams) error
	DeletePasswordCredential(ctx context.Context, accountID int32) error
}

type Store struct {
	q        PasswordQueries
	params   configx.PasswordHashParams
	throttle *authn.Throttle
	audit    audit.Writer
}

func NewStore(q PasswordQueries, params configx.PasswordHashParams, throttle *authn.Throttle, w audit.Writer) *Store {
	return &Store{q: q, params: params, throttle: throttle, audit: w}
}

func (s *Store) Set(ctx context.Context, accountID int32, pw string) error {
	hash, err := HashRaw(pw, s.params)
	if err != nil {
		return fmt.Errorf("password.Set: hash: %w", err)
	}
	if err := s.q.UpsertPasswordCredential(ctx, db.UpsertPasswordCredentialParams{
		AccountID: accountID,
		Hash:      hash,
	}); err != nil {
		return fmt.Errorf("password.Set: upsert: %w", err)
	}
	_ = s.audit.Record(ctx, audit.Record{
		AccountID: &accountID,
		Factor:    audit.FactorPassword,
		Event:     audit.EventRegister,
	})
	return nil
}

func (s *Store) Verify(ctx context.Context, accountID int32, pw string) error {
	if _, err := s.throttle.CheckLocked(ctx, accountID, "password"); err != nil {
		return err
	}
	row, err := s.q.GetPasswordCredential(ctx, accountID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrPasswordNotSet
		}
		return fmt.Errorf("password.Verify: get: %w", err)
	}

	decoded, err := PHCDecode(row.Hash)
	if err != nil {
		return fmt.Errorf("password.Verify: corrupt PHC: %w", err)
	}
	candidate := argon2.IDKey(
		[]byte(pw), decoded.Salt,
		decoded.Params.Iterations, decoded.Params.MemoryKiB,
		decoded.Params.Parallelism, uint32(len(decoded.Tag)),
	)
	if subtle.ConstantTimeCompare(candidate, decoded.Tag) != 1 {
		_, _ = s.throttle.RegisterFailure(ctx, accountID, "password")
		_ = s.audit.Record(ctx, audit.Record{
			AccountID: &accountID,
			Factor:    audit.FactorPassword,
			Event:     audit.EventFail,
		})
		return ErrPasswordIncorrect
	}

	_ = s.throttle.Reset(ctx, accountID, "password")
	_ = s.audit.Record(ctx, audit.Record{
		AccountID: &accountID,
		Factor:    audit.FactorPassword,
		Event:     audit.EventUse,
	})

	if decoded.Params != s.params {
		if newHash, herr := HashRaw(pw, s.params); herr == nil {
			_ = s.q.UpsertPasswordCredential(ctx, db.UpsertPasswordCredentialParams{
				AccountID: accountID,
				Hash:      newHash,
			})
		}
	}
	return nil
}

func (s *Store) Delete(ctx context.Context, accountID int32) error {
	if err := s.q.DeletePasswordCredential(ctx, accountID); err != nil {
		return fmt.Errorf("password.Delete: %w", err)
	}
	_ = s.audit.Record(ctx, audit.Record{
		AccountID: &accountID,
		Factor:    audit.FactorPassword,
		Event:     audit.EventRevoke,
	})
	return nil
}

// VerifyAgainstDummy runs an argon2id verify against a fixed all-zero salt
// dummy with the store's CURRENT params so step-1 timing on missing
// accounts matches the row-present case. Used by the HTTP layer when no
// password_credential row exists (missing/disabled/wrong-password).
//
// Timing-equalization caveat (Bundle-3 Low-2): this dummy is hashed at the
// Store's current params. When the deployment upgrades params (e.g.,
// m=64MiB → 128MiB), users whose stored hashes still carry the old params
// take longer than the dummy on Verify — argon2id at the OLD params plus
// a transparent re-hash at the NEW params. Until every active user has
// hit the rehash branch, the timing-equalization is imperfect and a
// careful attacker could probe which accounts have been rehashed.
//
// Eventually all active users succeed once, triggering the rehash, and
// the variance disappears. Deployments that need strict equalization
// across a params-upgrade window should run a one-shot rehash sweep over
// password_credential rows; v0.2 does not ship that tool.
func (s *Store) VerifyAgainstDummy(_ context.Context, pw string) {
	dummySalt := make([]byte, saltLength)
	_ = argon2.IDKey(
		[]byte(pw), dummySalt,
		s.params.Iterations, s.params.MemoryKiB, s.params.Parallelism, tagLength,
	)
}

// HashRaw is exported for cross-package reuse (e.g., recovery code hashing
// in pkg/credential/totp). Accepts explicit params to decouple from any
// particular Store instance.
func HashRaw(s string, params configx.PasswordHashParams) (string, error) {
	salt := make([]byte, saltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("password: rand salt: %w", err)
	}
	tag := argon2.IDKey(
		[]byte(s), salt,
		params.Iterations, params.MemoryKiB, params.Parallelism, tagLength,
	)
	return PHCEncode(params, salt, tag), nil
}

// VerifyRaw argon2id-verifies s against the supplied PHC string. Returns
// true on match; false on any mismatch, malformed PHC, or KDF failure.
func VerifyRaw(s, phc string) bool {
	decoded, err := PHCDecode(phc)
	if err != nil {
		return false
	}
	candidate := argon2.IDKey(
		[]byte(s), decoded.Salt,
		decoded.Params.Iterations, decoded.Params.MemoryKiB,
		decoded.Params.Parallelism, uint32(len(decoded.Tag)),
	)
	return subtle.ConstantTimeCompare(candidate, decoded.Tag) == 1
}

// DefaultParams returns the OWASP 2026 recommended argon2id parameters.
// Used by callers that don't have a Store handy (e.g. one-shot recovery
// code hashing in pkg/credential/totp).
func DefaultParams() configx.PasswordHashParams {
	return configx.PasswordHashParams{
		MemoryKiB:   65536,
		Iterations:  3,
		Parallelism: 1,
	}
}
