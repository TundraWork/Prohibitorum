package email

import (
	"context"
	"fmt"

	"prohibitorum/pkg/db"
)

// AbsorbOutcome reports what AbsorbUpstream did, for audit records.
type AbsorbOutcome string

const (
	// AbsorbAdded: the address was inserted as a verified row.
	AbsorbAdded AbsorbOutcome = "added"
	// AbsorbVerified: an existing unverified row was bumped to verified.
	AbsorbVerified AbsorbOutcome = "verified"
	// AbsorbExisting: the row was already verified; nothing changed.
	AbsorbExisting AbsorbOutcome = "existing"
	// AbsorbSkipTaken: another account has the address verified.
	AbsorbSkipTaken AbsorbOutcome = "taken"
	// AbsorbSkipFull: the account is at the address cap.
	AbsorbSkipFull AbsorbOutcome = "full"
	// AbsorbSkipInvalid: the upstream address failed validation.
	AbsorbSkipInvalid AbsorbOutcome = "invalid"
)

// AbsorbUpstream folds a verified upstream-asserted address into the
// account's list. Never fails the sign-in: a conflict or full list is
// returned as a skip outcome for the caller to audit. Sets the address
// primary when the account has none.
//
// The caller is responsible for only passing addresses the upstream asserted
// verified; unverified upstream emails stay on account_identity.upstream_email.
func AbsorbUpstream(ctx context.Context, q Queries, accountID int32, address string, idpID int64) (AbsorbOutcome, db.AccountEmail, error) {
	normalized, err := ValidateAddress(address)
	if err != nil {
		return AbsorbSkipInvalid, db.AccountEmail{}, nil
	}
	if err := lock(ctx, q, accountID); err != nil {
		return AbsorbSkipInvalid, db.AccountEmail{}, err
	}

	existing, getErr := q.GetAccountEmailByAddress(ctx, db.GetAccountEmailByAddressParams{AccountID: accountID, Lower: normalized})
	if getErr == nil {
		if existing.VerifiedAt.Valid {
			return AbsorbExisting, db.AccountEmail{
				ID: existing.ID, AccountID: existing.AccountID, Address: existing.Address,
				VerifiedAt: existing.VerifiedAt, VerifiedVia: existing.VerifiedVia,
				VerifiedByIdpID: existing.VerifiedByIdpID, VerifiedByAdminID: existing.VerifiedByAdminID,
				CreatedAt: existing.CreatedAt,
			}, nil
		}
		if count, err := q.CountVerifiedAccountEmailsByAddress(ctx, db.CountVerifiedAccountEmailsByAddressParams{Lower: normalized, AccountID: accountID}); err != nil {
			return AbsorbSkipInvalid, db.AccountEmail{}, fmt.Errorf("email: conflict check: %w", err)
		} else if count > 0 {
			return AbsorbSkipTaken, db.AccountEmail{}, nil
		}
		if n, err := q.MarkAccountEmailVerified(ctx, db.MarkAccountEmailVerifiedParams{
			ID: existing.ID, AccountID: accountID, VerifiedVia: pgText("upstream"), VerifiedByIdpID: pgInt8(idpID),
		}); err != nil {
			if isVerifiedAddressTaken(err) {
				return AbsorbSkipTaken, db.AccountEmail{}, nil
			}
			return AbsorbSkipInvalid, db.AccountEmail{}, fmt.Errorf("email: verify upstream: %w", err)
		} else if n == 0 {
			return AbsorbExisting, db.AccountEmail{}, nil
		}
		return AbsorbVerified, db.AccountEmail{
			ID: existing.ID, AccountID: accountID, Address: existing.Address,
			CreatedAt: existing.CreatedAt,
		}, ensurePrimaryWhenEmpty(ctx, q, accountID, existing.ID)
	}

	if count, err := q.CountAccountEmails(ctx, accountID); err != nil {
		return AbsorbSkipInvalid, db.AccountEmail{}, fmt.Errorf("email: count: %w", err)
	} else if count >= MaxPerAccount {
		return AbsorbSkipFull, db.AccountEmail{}, nil
	}
	if count, err := q.CountVerifiedAccountEmailsByAddress(ctx, db.CountVerifiedAccountEmailsByAddressParams{Lower: normalized, AccountID: accountID}); err != nil {
		return AbsorbSkipInvalid, db.AccountEmail{}, fmt.Errorf("email: conflict check: %w", err)
	} else if count > 0 {
		return AbsorbSkipTaken, db.AccountEmail{}, nil
	}
	row, err := q.InsertAccountEmailVerified(ctx, db.InsertAccountEmailVerifiedParams{
		AccountID: accountID, Address: normalized, VerifiedVia: pgText("upstream"), VerifiedByIdpID: pgInt8(idpID),
	})
	if err != nil {
		// The partial unique index can race a concurrent verify; that is a
		// skip, not a sign-in failure.
		if isVerifiedAddressTaken(err) {
			return AbsorbSkipTaken, db.AccountEmail{}, nil
		}
		return AbsorbSkipInvalid, db.AccountEmail{}, fmt.Errorf("email: insert upstream: %w", err)
	}
	return AbsorbAdded, row, ensurePrimaryWhenEmpty(ctx, q, accountID, row.ID)
}

// ensurePrimaryWhenEmpty sets emailID primary when the account has none.
// The GetAccountPrimaryEmail ErrNoRows IS the "no primary" signal; any other
// error propagates.
func ensurePrimaryWhenEmpty(ctx context.Context, q Queries, accountID int32, emailID int64) error {
	if _, err := q.GetAccountPrimaryEmail(ctx, accountID); err != nil {
		return q.SetAccountPrimaryEmail(ctx, db.SetAccountPrimaryEmailParams{ID: accountID, PrimaryEmailID: pgInt8(emailID)})
	}
	return nil
}
