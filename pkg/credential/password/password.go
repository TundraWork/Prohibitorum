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
//
// Every argon2id computation in the process goes through deriveKey, which
// holds one of a fixed number of slots while it runs, so memory stays near
// slots × memory_kib however many requests arrive (PHB-181).

package password

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/argon2"
	"golang.org/x/sync/semaphore"

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

// ErrBusy reports that every argon2id slot stayed taken for maxWait, so the
// hash never ran. It is one shared value, so errors.Is matches it, and an
// AuthError, so the dashboard API answers 429 rate_limited with Retry-After: 1
// without special handling. Busy is not a failed attempt: nothing is counted
// against the account and no failure is audited.
var ErrBusy = &authn.AuthError{
	Status:     http.StatusTooManyRequests,
	Code:       "rate_limited",
	Message:    authn.ErrRateLimited().Message,
	Details:    map[string]any{"retryAfterSeconds": 1},
	RetryAfter: time.Second,
}

var (
	// hashSlots bounds the argon2id computations running at once across the
	// process. SetMaxConcurrent replaces it once at startup.
	hashSlots atomic.Pointer[semaphore.Weighted]
	// maxWait is how long a hash waits for a slot before ErrBusy. A variable
	// only so tests can shorten it.
	maxWait = 5 * time.Second
	// idKey is the KDF seam; tests swap it to observe or stall hashing.
	idKey = argon2.IDKey
)

func init() { SetMaxConcurrent(0) }

// SetMaxConcurrent sets how many argon2id computations may run at once.
// n <= 0 selects min(4, GOMAXPROCS): at parallelism 1 each hash uses one
// core, so more slots than cores add memory without adding throughput.
// Call it before serving; hashes already holding a slot finish on the old
// limit.
func SetMaxConcurrent(n int) {
	if n <= 0 {
		n = min(4, runtime.GOMAXPROCS(0))
	}
	hashSlots.Store(semaphore.NewWeighted(int64(n)))
}

// withSlot runs fn while holding one hash slot. It waits at most maxWait for
// the slot: the caller's context ending first returns that context's error,
// the wait running out returns ErrBusy. Either way fn has not run.
func withSlot(ctx context.Context, fn func()) error {
	sem := hashSlots.Load()
	waitCtx, cancel := context.WithTimeout(ctx, maxWait)
	defer cancel()
	if err := sem.Acquire(waitCtx, 1); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return ErrBusy
	}
	defer sem.Release(1)
	fn()
	return nil
}

// computeKey is the one place the package runs argon2id. Callers hold a slot.
func computeKey(pw, salt []byte, params configx.PasswordHashParams, tagLen uint32) []byte {
	return idKey(pw, salt, params.Iterations, params.MemoryKiB, params.Parallelism, tagLen)
}

// deriveKey runs one argon2id computation in a slot. It returns ErrBusy or the
// context's error when no slot came free, and then no hash was computed.
func deriveKey(ctx context.Context, pw, salt []byte, params configx.PasswordHashParams, tagLen uint32) ([]byte, error) {
	var key []byte
	err := withSlot(ctx, func() { key = computeKey(pw, salt, params, tagLen) })
	return key, err
}

type PasswordQueries interface {
	GetPasswordCredential(ctx context.Context, accountID int32) (db.PasswordCredential, error)
	UpsertPasswordCredential(ctx context.Context, arg db.UpsertPasswordCredentialParams) error
	UpdatePasswordHashOnly(ctx context.Context, arg db.UpdatePasswordHashOnlyParams) error
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
	hash, err := HashRaw(ctx, pw, s.params)
	if err != nil {
		return fmt.Errorf("password.Set: hash: %w", err)
	}
	if err := s.q.UpsertPasswordCredential(ctx, db.UpsertPasswordCredentialParams{
		AccountID: accountID,
		Hash:      hash,
	}); err != nil {
		return fmt.Errorf("password.Set: upsert: %w", err)
	}
	audit.RecordOrLog(ctx, s.audit, audit.Record{
		AccountID: &accountID,
		Factor:    audit.FactorPassword,
		Event:     audit.EventRegister,
	})
	return nil
}

// Hash computes the argon2id PHC string for pw using the store's configured
// params WITHOUT touching the database. The password-totp enrollment ceremony
// hashes at "begin" (stashing the PHC in KV) and inserts the row at "verify"
// via a tx-scoped UpsertPasswordCredential — so the plaintext never lands in
// the KV stash and the account row (which may not exist until "verify") is not
// required here.
func (s *Store) Hash(ctx context.Context, pw string) (string, error) {
	return HashRaw(ctx, pw, s.params)
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
	candidate, err := deriveKey(ctx, []byte(pw), decoded.Salt, decoded.Params, uint32(len(decoded.Tag)))
	if err != nil {
		// argon2id never ran (ErrBusy, or the request ended): the password was
		// not checked, so it is not a failed attempt and is not counted.
		return err
	}
	if subtle.ConstantTimeCompare(candidate, decoded.Tag) != 1 {
		_, _ = s.throttle.RegisterFailure(ctx, accountID, "password")
		audit.RecordOrLog(ctx, s.audit, audit.Record{
			AccountID: &accountID,
			Factor:    audit.FactorPassword,
			Event:     audit.EventFail,
		})
		return ErrPasswordIncorrect
	}

	_ = s.throttle.Reset(ctx, accountID, "password")
	audit.RecordOrLog(ctx, s.audit, audit.Record{
		AccountID: &accountID,
		Factor:    audit.FactorPassword,
		Event:     audit.EventUse,
	})

	if decoded.Params != s.params {
		// Transparent param-upgrade rehash: the secret is unchanged, so use the
		// hash-only update — bumping password_changed_at here would make a silent
		// re-encoding look like a deliberate password change to age/reauth policy
		// (T4.3a). A busy hasher skips the rehash; the next sign-in retries it.
		if newHash, herr := HashRaw(ctx, pw, s.params); herr == nil {
			_ = s.q.UpdatePasswordHashOnly(ctx, db.UpdatePasswordHashOnlyParams{
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
	audit.RecordOrLog(ctx, s.audit, audit.Record{
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
// password_credential rows; no such tool ships yet.
//
// It returns ErrBusy or the context's error when the dummy hash could not
// run, so the caller can answer exactly as a busy Verify would.
func (s *Store) VerifyAgainstDummy(ctx context.Context, pw string) error {
	dummySalt := make([]byte, saltLength)
	_, err := deriveKey(ctx, []byte(pw), dummySalt, s.params, tagLength)
	return err
}

// HashRaw is exported for cross-package reuse (e.g., OIDC client secrets in
// pkg/protocol/oidc). Accepts explicit params to decouple from any particular
// Store instance. Returns ErrBusy or the context's error when no hash slot
// came free.
func HashRaw(ctx context.Context, s string, params configx.PasswordHashParams) (string, error) {
	salt, err := newSalt()
	if err != nil {
		return "", err
	}
	tag, err := deriveKey(ctx, []byte(s), salt, params, tagLength)
	if err != nil {
		return "", err
	}
	return PHCEncode(params, salt, tag), nil
}

// VerifyRaw argon2id-verifies s against the supplied PHC string. (false, nil)
// is a mismatch or a malformed PHC; a non-nil error is ErrBusy or the
// context's error, and then nothing was compared.
func VerifyRaw(ctx context.Context, s, phc string) (bool, error) {
	decoded, err := PHCDecode(phc)
	if err != nil {
		return false, nil
	}
	candidate, err := deriveKey(ctx, []byte(s), decoded.Salt, decoded.Params, uint32(len(decoded.Tag)))
	if err != nil {
		return false, err
	}
	return subtle.ConstantTimeCompare(candidate, decoded.Tag) == 1, nil
}

// HashRawEach hashes every value under one slot and returns their PHC strings
// in order. A batch (a set of recovery codes) waits for a slot once instead of
// once per value, so a busy server delays it by at most maxWait in total.
func HashRawEach(ctx context.Context, values []string, params configx.PasswordHashParams) ([]string, error) {
	salts := make([][]byte, len(values))
	for i := range values {
		salt, err := newSalt()
		if err != nil {
			return nil, err
		}
		salts[i] = salt
	}
	phcs := make([]string, len(values))
	err := withSlot(ctx, func() {
		for i, v := range values {
			phcs[i] = PHCEncode(params, salts[i], computeKey([]byte(v), salts[i], params, tagLength))
		}
	})
	if err != nil {
		return nil, err
	}
	return phcs, nil
}

// VerifyRawAny verifies s against each PHC in turn under one slot and returns
// the index of the first match, or -1 when none matches (malformed entries
// never match). A non-nil error is ErrBusy or the context's error, and then
// nothing was compared.
func VerifyRawAny(ctx context.Context, s string, phcs []string) (int, error) {
	match := -1
	err := withSlot(ctx, func() {
		for i, phc := range phcs {
			decoded, err := PHCDecode(phc)
			if err != nil {
				continue
			}
			candidate := computeKey([]byte(s), decoded.Salt, decoded.Params, uint32(len(decoded.Tag)))
			if subtle.ConstantTimeCompare(candidate, decoded.Tag) == 1 {
				match = i
				return
			}
		}
	})
	if err != nil {
		return -1, err
	}
	return match, nil
}

func newSalt() ([]byte, error) {
	salt := make([]byte, saltLength)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("password: rand salt: %w", err)
	}
	return salt, nil
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
