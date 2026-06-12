package oidc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"prohibitorum/pkg/db"
)

// TestRetireSigningKeyRefusesActive proves the active-key guard in
// RetireSigningKey: a target whose status is 'active' is refused with
// ErrActiveKeyNoReplacement and the underlying RetireSigningKey UPDATE is never
// issued (Task 3 maps the error to HTTP 409). This is the slice of the lifecycle
// logic that does not need a real DB; the tx-ordering of ActivateSigningKey and
// the partial-unique-index behavior are exercised by the smoke arc (Task 10).
func TestRetireSigningKeyRefusesActive(t *testing.T) {
	ctx := context.Background()
	q := db.New(&fakeGetByKID{status: "active"})

	_, err := RetireSigningKey(ctx, q, "active-kid", time.Hour)
	if !errors.Is(err, ErrActiveKeyNoReplacement) {
		t.Fatalf("RetireSigningKey(active) error = %v, want ErrActiveKeyNoReplacement", err)
	}
}

// TestRetireSigningKeyMissing surfaces a not-found target as the underlying
// pgx.ErrNoRows (Task 3 maps it to 404), not as the active-key guard.
func TestRetireSigningKeyMissing(t *testing.T) {
	ctx := context.Background()
	q := db.New(&fakeGetByKID{getErr: pgx.ErrNoRows})

	_, err := RetireSigningKey(ctx, q, "nope", time.Hour)
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("RetireSigningKey(missing) error = %v, want pgx.ErrNoRows", err)
	}
}

// fakeGetByKID is a db.DBTX stub whose only meaningful behavior is to let
// db.Queries.GetSigningKeyByKID resolve to a row with a chosen status (or
// error). It drives the pure-Go branches of RetireSigningKey without Postgres.
// Only QueryRow is exercised: the active guard returns before any UPDATE, and
// the missing-key case errors out of QueryRow, so Exec/Query must never run.
type fakeGetByKID struct {
	status string
	getErr error
}

func (f *fakeGetByKID) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	return fakeRow{status: f.status, err: f.getErr}
}

func (f *fakeGetByKID) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	panic("unexpected Exec in keylifecycle unit test")
}

func (f *fakeGetByKID) Query(context.Context, string, ...any) (pgx.Rows, error) {
	panic("unexpected Query in keylifecycle unit test")
}

// fakeRow returns the canned error, or scans only the Status field (the sole
// field RetireSigningKey reads); the remaining dests are left at their zero
// values, which is fine for the guard branches under test.
type fakeRow struct {
	status string
	err    error
}

func (r fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	// GetSigningKeyByKID scans Status at index 8 (see signing_key column order:
	// kid, algorithm, use, public_jwk, x509_cert_pem, private_pem_enc,
	// private_pem_nonce, key_version, status, …).
	const statusIdx = 8
	if statusIdx < len(dest) {
		if p, ok := dest[statusIdx].(*string); ok {
			*p = r.status
		}
	}
	return nil
}
