package oidc

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/db"
)

// ErrActiveKeyNoReplacement is returned by RetireSigningKey when the caller
// targets the currently active signing key. Retiring the active key without
// first promoting a replacement would leave the OP/IdP with no signer, so the
// operation is refused. Task 3 maps this to HTTP 409 Conflict.
var ErrActiveKeyNoReplacement = errors.New("cannot retire the active signing key without a replacement")

// txBeginner is the slice of *pgxpool.Pool that ActivateSigningKey needs to
// open a transaction. Kept narrow so callers can substitute it in tests.
type txBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// InsertPendingKey mints a fresh RSA-2048 signing key and persists it in the
// 'pending' lifecycle state. The private key is sealed at rest with the DEK
// (AES-256-GCM, AAD bound to kid+keyVersion) before it is written. It does NOT
// activate the key — promotion to 'active' is an explicit, separate step via
// ActivateSigningKey.
func InsertPendingKey(ctx context.Context, q *db.Queries, dek []byte, keyVersion int32) (db.SigningKey, error) {
	gen, err := GenerateSigningKey()
	if err != nil {
		return db.SigningKey{}, err
	}
	enc, nonce, err := sealPrivateKey(dek, gen.PrivatePEM, gen.Kid, keyVersion)
	if err != nil {
		return db.SigningKey{}, err
	}
	return q.InsertPendingSigningKey(ctx, db.InsertPendingSigningKeyParams{
		Kid:             gen.Kid,
		Algorithm:       "RS256",
		PublicJwk:       gen.PublicJWK,
		X509CertPem:     pgtype.Text{String: gen.X509CertPEM, Valid: true},
		PrivatePemEnc:   enc,
		PrivatePemNonce: nonce,
		KeyVersion:      keyVersion,
	})
}

// ActivateSigningKey promotes the pending key identified by kid to 'active' in
// a single transaction. The prior active key (if any) is demoted to
// 'decommissioning' FIRST — stamping decommissioned_at and retire_after =
// now()+grace — and only then is the target promoted. This demote-before-promote
// ordering is mandatory: the `one_active_signing_key UNIQUE (use) WHERE
// status='active'` partial index must hold at every statement boundary within
// the tx, and demoting first guarantees there is never a moment with two active
// rows for the same `use`.
//
// If kid does not name a 'pending' key, PromoteSigningKey affects no rows
// (pgx.ErrNoRows) and the whole transaction is rolled back, leaving the prior
// active key untouched.
func ActivateSigningKey(ctx context.Context, pool txBeginner, q *db.Queries, kid string, grace time.Duration) (db.SigningKey, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return db.SigningKey{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	qtx := q.WithTx(tx)

	retireAfter := pgtype.Timestamptz{Time: time.Now().Add(grace), Valid: true}
	if err := qtx.DemoteActiveSigningKey(ctx, retireAfter); err != nil {
		return db.SigningKey{}, err
	}

	promoted, err := qtx.PromoteSigningKey(ctx, kid)
	if err != nil {
		return db.SigningKey{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return db.SigningKey{}, err
	}
	return promoted, nil
}

// RetireSigningKey moves a pending or decommissioning key toward 'retired' by
// setting it to 'decommissioning' (stamping decommissioned_at if not already
// set, and retire_after = now()+grace; the background reconcile loop flips it to
// 'retired' once retire_after passes). Retiring the active key is refused with
// ErrActiveKeyNoReplacement — a replacement must be activated first (which
// demotes the active key as a side effect).
func RetireSigningKey(ctx context.Context, q *db.Queries, kid string, grace time.Duration) (db.SigningKey, error) {
	key, err := q.GetSigningKeyByKID(ctx, kid)
	if err != nil {
		return db.SigningKey{}, err
	}
	if key.Status == "active" {
		return db.SigningKey{}, ErrActiveKeyNoReplacement
	}
	return q.RetireSigningKey(ctx, db.RetireSigningKeyParams{
		Kid:         kid,
		RetireAfter: pgtype.Timestamptz{Time: time.Now().Add(grace), Valid: true},
	})
}
