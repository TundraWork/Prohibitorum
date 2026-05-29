package saml

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"

	"github.com/jackc/pgx/v5"

	"prohibitorum/pkg/db"
)

// subjectID returns a stable, opaque, persistent NameID for the given
// (account, sp) pair. The first call mints a 32-byte crypto/rand value,
// base64url-encodes it (RawURLEncoding -> 43 url-safe chars, no padding),
// and persists it via InsertSAMLSubjectID. Every subsequent call for the
// same (account, sp) returns the identical stored value, so a given SP
// always sees the same subject identifier for a given user while two
// different SPs see unlinkable identifiers for the same user.
//
// format is recorded as the NameIDFormat alongside the minted value on
// first creation.
func (i *IdP) subjectID(ctx context.Context, accountID int32, spID int64, format string) (string, error) {
	row, err := i.queries.GetSAMLSubjectID(ctx, db.GetSAMLSubjectIDParams{
		AccountID: accountID,
		SpID:      spID,
	})
	if err == nil {
		return row.NameID, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}

	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	nameID := base64.RawURLEncoding.EncodeToString(buf)

	inserted, err := i.queries.InsertSAMLSubjectID(ctx, db.InsertSAMLSubjectIDParams{
		AccountID:    accountID,
		SpID:         spID,
		NameID:       nameID,
		NameIDFormat: format,
	})
	if err != nil {
		return "", err
	}
	return inserted.NameID, nil
}
