package oidc

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/db"
)

// RotateClientSecret generates a new random secret, stores only its argon2id
// hash in the database, and returns the cleartext exactly once. It shares the
// generateClientSecret helper with BuildClientParams so both code paths use
// the same entropy and hashing parameters.
func RotateClientSecret(ctx context.Context, q *db.Queries, clientID string) (string, error) {
	secret, hash, err := generateClientSecret()
	if err != nil {
		return "", err
	}
	if err := q.UpdateOIDCClientSecret(ctx, db.UpdateOIDCClientSecretParams{
		ClientID:         clientID,
		ClientSecretHash: pgtype.Text{String: hash, Valid: true},
	}); err != nil {
		return "", err
	}
	return secret, nil
}
