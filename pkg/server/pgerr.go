package server

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// isUniqueViolation returns true iff err carries Postgres SQLSTATE 23505.
// Centralized here so every handler's "is this a duplicate-key collision?"
// branch shares one canonical check.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// uniqueViolationConstraint returns the constraint name from a SQLSTATE 23505
// error, or "" if err is not a unique-violation. Handlers use this to
// distinguish which UNIQUE column tripped so they can surface the right
// 409 message (e.g. "key already exists" vs. "name already in use").
func uniqueViolationConstraint(err error) string {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		return ""
	}
	return pgErr.ConstraintName
}
