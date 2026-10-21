// Package email is the single write path for account email addresses. Every
// mutation of account_email and account.primary_email_id goes through here —
// handlers and the federation resolver only call these functions — so the
// rules (cap, uniqueness, primary-must-be-verified, fallback on primary
// removal) live in exactly one place.
//
// Callers pass a transaction-scoped Queries; every write locks the account
// row first (GetAccountByIDForUpdate) so concurrent primary changes, deletes,
// and cap checks cannot interleave.
package email

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"

	"golang.org/x/net/idna"

	"prohibitorum/pkg/db"
)

// MaxPerAccount is the address cap per account, unverified included.
const MaxPerAccount = 10

// Sentinel error kinds for the rules. Handlers translate these into their
// public error codes (email_exists, email_unavailable, email_taken, …).
var (
	ErrInvalidAddress = errors.New("email: invalid address")
	ErrCapReached     = errors.New("email: address cap reached")
	ErrDuplicate      = errors.New("email: address already on this account")
	// ErrTaken means the address is verified on another account. Handlers
	// decide how much to reveal: users get email_unavailable (no details),
	// admins get email_taken.
	ErrTaken                 = errors.New("email: address verified on another account")
	ErrNotVerifiedForPrimary = errors.New("email: address not verified; cannot be primary")
	ErrPrimaryUndeletable    = errors.New("email: primary address cannot be deleted")
	ErrNotFound              = errors.New("email: no such address on this account")
	ErrAlreadyVerified       = errors.New("email: address already verified")
)

// Queries is the transaction-scoped surface the write functions need.
// *db.Queries satisfies it.
type Queries interface {
	GetAccountByIDForUpdate(ctx context.Context, id int32) (db.Account, error)
	CountAccountEmails(ctx context.Context, accountID int32) (int64, error)
	GetAccountEmailByAddress(ctx context.Context, arg db.GetAccountEmailByAddressParams) (db.GetAccountEmailByAddressRow, error)
	InsertAccountEmail(ctx context.Context, arg db.InsertAccountEmailParams) (db.AccountEmail, error)
	InsertAccountEmailVerified(ctx context.Context, arg db.InsertAccountEmailVerifiedParams) (db.AccountEmail, error)
	MarkAccountEmailVerified(ctx context.Context, arg db.MarkAccountEmailVerifiedParams) (int64, error)
	DeleteAccountEmail(ctx context.Context, arg db.DeleteAccountEmailParams) (int64, error)
	GetAccountEmail(ctx context.Context, arg db.GetAccountEmailParams) (db.GetAccountEmailRow, error)
	SetAccountPrimaryEmail(ctx context.Context, arg db.SetAccountPrimaryEmailParams) error
	ClearAccountPrimaryEmail(ctx context.Context, id int32) error
	PickFallbackPrimary(ctx context.Context, arg db.PickFallbackPrimaryParams) (db.PickFallbackPrimaryRow, error)
	CountVerifiedAccountEmailsByAddress(ctx context.Context, arg db.CountVerifiedAccountEmailsByAddressParams) (int64, error)
	// GetAccountPrimaryEmail returns ErrNoRows when the account has no
	// primary; callers use that to decide auto-primary.
	GetAccountPrimaryEmail(ctx context.Context, id int32) (string, error)
}

// ValidateAddress parses and canonicalizes one address. Rules: RFC 5322 via
// net/mail with no display name and byte-equality with the trimmed input, an
// IDNA-encodable domain (stored in ASCII form), and the RFC 5321 length cap.
// The local part is stored as given; comparisons lowercase the whole address.
func ValidateAddress(input string) (string, error) {
	s := strings.TrimSpace(input)
	if s == "" {
		return "", ErrInvalidAddress
	}
	if len(s) > 254 {
		return "", ErrInvalidAddress
	}
	addr, err := mail.ParseAddress(s)
	if err != nil || addr.Address != s || addr.Name != "" {
		return "", ErrInvalidAddress
	}
	at := strings.LastIndex(s, "@")
	if at <= 0 || at == len(s)-1 {
		return "", ErrInvalidAddress
	}
	domain, err := idna.Lookup.ToASCII(s[at+1:])
	if err != nil || domain == "" {
		return "", ErrInvalidAddress
	}
	return s[:at+1] + domain, nil
}

func lock(ctx context.Context, q Queries, accountID int32) error {
	if _, err := q.GetAccountByIDForUpdate(ctx, accountID); err != nil {
		return fmt.Errorf("email: lock account: %w", err)
	}
	return nil
}

// Add registers a new unverified address for the account: format check,
// per-account cap, in-account duplicate check.
func Add(ctx context.Context, q Queries, accountID int32, address string) (db.AccountEmail, error) {
	normalized, err := ValidateAddress(address)
	if err != nil {
		return db.AccountEmail{}, err
	}
	if err := lock(ctx, q, accountID); err != nil {
		return db.AccountEmail{}, err
	}
	if count, err := q.CountAccountEmails(ctx, accountID); err != nil {
		return db.AccountEmail{}, fmt.Errorf("email: count: %w", err)
	} else if count >= MaxPerAccount {
		return db.AccountEmail{}, ErrCapReached
	}
	if _, err := q.GetAccountEmailByAddress(ctx, db.GetAccountEmailByAddressParams{AccountID: accountID, Lower: normalized}); err == nil {
		return db.AccountEmail{}, ErrDuplicate
	}
	return q.InsertAccountEmail(ctx, db.InsertAccountEmailParams{AccountID: accountID, Address: normalized})
}

// AdminAdd registers an address with optional immediate verification
// (verified_via='admin', recorded against adminID) and optional primary.
// Conflict with another account's verified address returns ErrTaken — admins
// are allowed to know the conflict exists.
func AdminAdd(ctx context.Context, q Queries, accountID int32, address string, verified, primary bool, adminID int32) (db.AccountEmail, error) {
	if primary && !verified {
		return db.AccountEmail{}, ErrNotVerifiedForPrimary
	}
	normalized, err := ValidateAddress(address)
	if err != nil {
		return db.AccountEmail{}, err
	}
	if err := lock(ctx, q, accountID); err != nil {
		return db.AccountEmail{}, err
	}
	if count, err := q.CountAccountEmails(ctx, accountID); err != nil {
		return db.AccountEmail{}, fmt.Errorf("email: count: %w", err)
	} else if count >= MaxPerAccount {
		return db.AccountEmail{}, ErrCapReached
	}
	existing, err := q.GetAccountEmailByAddress(ctx, db.GetAccountEmailByAddressParams{AccountID: accountID, Lower: normalized})
	if err == nil {
		if verified && !existing.VerifiedAt.Valid {
			return db.AccountEmail{}, adminVerifyExisting(ctx, q, accountID, existing.ID, adminID, primary)
		}
		return db.AccountEmail{}, ErrDuplicate
	}
	if !verified {
		row, err := q.InsertAccountEmail(ctx, db.InsertAccountEmailParams{AccountID: accountID, Address: normalized})
		if err != nil {
			return db.AccountEmail{}, fmt.Errorf("email: insert: %w", err)
		}
		if primary {
			// unreachable: primary implies verified above
			return db.AccountEmail{}, ErrNotVerifiedForPrimary
		}
		return row, nil
	}
	if count, err := q.CountVerifiedAccountEmailsByAddress(ctx, db.CountVerifiedAccountEmailsByAddressParams{Lower: normalized, AccountID: accountID}); err != nil {
		return db.AccountEmail{}, fmt.Errorf("email: conflict check: %w", err)
	} else if count > 0 {
		return db.AccountEmail{}, ErrTaken
	}
	row, err := q.InsertAccountEmailVerified(ctx, db.InsertAccountEmailVerifiedParams{
		AccountID: accountID, Address: normalized, VerifiedVia: pgText("admin"), VerifiedByAdminID: pgInt8(int64(adminID)),
	})
	if err != nil {
		return db.AccountEmail{}, fmt.Errorf("email: insert verified: %w", err)
	}
	if primary {
		if err := q.SetAccountPrimaryEmail(ctx, db.SetAccountPrimaryEmailParams{ID: accountID, PrimaryEmailID: pgInt8(row.ID)}); err != nil {
			return db.AccountEmail{}, fmt.Errorf("email: set primary: %w", err)
		}
	}
	return row, nil
}

// adminVerifyExisting bumps an existing unverified row to admin-verified —
// the "verify without re-adding" path of the admin add endpoint.
func adminVerifyExisting(ctx context.Context, q Queries, accountID int32, emailID int64, adminID int32, primary bool) error {
	if count, err := q.CountVerifiedAccountEmailsByAddress(ctx, db.CountVerifiedAccountEmailsByAddressParams{
		Lower: addressOfExisting(ctx, q, accountID, emailID), AccountID: accountID,
	}); err != nil {
		return fmt.Errorf("email: conflict check: %w", err)
	} else if count > 0 {
		return ErrTaken
	}
	if n, err := q.MarkAccountEmailVerified(ctx, db.MarkAccountEmailVerifiedParams{
		ID: emailID, AccountID: accountID, VerifiedVia: pgText("admin"), VerifiedByAdminID: pgInt8(int64(adminID)),
	}); err != nil {
		return fmt.Errorf("email: verify: %w", err)
	} else if n == 0 {
		return ErrAlreadyVerified
	}
	if primary {
		if err := q.SetAccountPrimaryEmail(ctx, db.SetAccountPrimaryEmailParams{ID: accountID, PrimaryEmailID: pgInt8(emailID)}); err != nil {
			return fmt.Errorf("email: set primary: %w", err)
		}
	}
	return nil
}

func addressOfExisting(ctx context.Context, q Queries, accountID int32, emailID int64) string {
	row, err := q.GetAccountEmail(ctx, db.GetAccountEmailParams{ID: emailID, AccountID: accountID})
	if err != nil {
		return ""
	}
	return row.Address
}

// AssertVerified marks the address verified with the given via ('code' or
// 'admin'). It returns ErrTaken when the address is verified on another
// account (the caller picks the public error), and sets the address primary
// when the account has none.
func AssertVerified(ctx context.Context, q Queries, accountID int32, emailID int64, via string, idpID, adminID *int64) (db.AccountEmail, error) {
	if err := lock(ctx, q, accountID); err != nil {
		return db.AccountEmail{}, err
	}
	row, err := q.GetAccountEmail(ctx, db.GetAccountEmailParams{ID: emailID, AccountID: accountID})
	if err != nil {
		return db.AccountEmail{}, ErrNotFound
	}
	if row.VerifiedAt.Valid {
		return db.AccountEmail{}, ErrAlreadyVerified
	}
	if count, err := q.CountVerifiedAccountEmailsByAddress(ctx, db.CountVerifiedAccountEmailsByAddressParams{Lower: row.Address, AccountID: accountID}); err != nil {
		return db.AccountEmail{}, fmt.Errorf("email: conflict check: %w", err)
	} else if count > 0 {
		return db.AccountEmail{}, ErrTaken
	}
	if n, err := q.MarkAccountEmailVerified(ctx, db.MarkAccountEmailVerifiedParams{
		ID: emailID, AccountID: accountID, VerifiedVia: pgText(via), VerifiedByIdpID: pgInt8Ptr(idpID), VerifiedByAdminID: pgInt8Ptr(adminID),
	}); err != nil {
		return db.AccountEmail{}, fmt.Errorf("email: verify: %w", err)
	} else if n == 0 {
		return db.AccountEmail{}, ErrAlreadyVerified
	}
	if !row.IsPrimary {
		if _, err := q.GetAccountPrimaryEmail(ctx, accountID); err != nil {
			// No primary yet — this address becomes it.
			if err := q.SetAccountPrimaryEmail(ctx, db.SetAccountPrimaryEmailParams{ID: accountID, PrimaryEmailID: pgInt8(emailID)}); err != nil {
				return db.AccountEmail{}, fmt.Errorf("email: set primary: %w", err)
			}
		}
	}
	updated, err := q.GetAccountEmail(ctx, db.GetAccountEmailParams{ID: emailID, AccountID: accountID})
	if err != nil {
		return db.AccountEmail{}, err
	}
	return db.AccountEmail{
		ID: updated.ID, AccountID: updated.AccountID, Address: updated.Address,
		VerifiedAt: updated.VerifiedAt, VerifiedVia: updated.VerifiedVia,
		VerifiedByIdpID: updated.VerifiedByIdpID, VerifiedByAdminID: updated.VerifiedByAdminID,
		CreatedAt: updated.CreatedAt,
	}, nil
}

// SetPrimary makes a verified address of this account the primary.
func SetPrimary(ctx context.Context, q Queries, accountID int32, emailID int64) error {
	if err := lock(ctx, q, accountID); err != nil {
		return err
	}
	row, err := q.GetAccountEmail(ctx, db.GetAccountEmailParams{ID: emailID, AccountID: accountID})
	if err != nil {
		return ErrNotFound
	}
	if !row.VerifiedAt.Valid {
		return ErrNotVerifiedForPrimary
	}
	return q.SetAccountPrimaryEmail(ctx, db.SetAccountPrimaryEmailParams{ID: accountID, PrimaryEmailID: pgInt8(emailID)})
}

// Remove deletes an address. With allowPrimary=false (self-service) the
// primary cannot be deleted; with true (admin) deleting the primary promotes
// the earliest-verified remaining address, or clears the pointer when none
// exists. Returns whether a fallback primary was set and its address.
func Remove(ctx context.Context, q Queries, accountID int32, emailID int64, allowPrimary bool) (fallback *string, err error) {
	if err := lock(ctx, q, accountID); err != nil {
		return nil, err
	}
	row, err := q.GetAccountEmail(ctx, db.GetAccountEmailParams{ID: emailID, AccountID: accountID})
	if err != nil {
		return nil, ErrNotFound
	}
	wasPrimary := row.IsPrimary
	if wasPrimary && !allowPrimary {
		return nil, ErrPrimaryUndeletable
	}
	if wasPrimary {
		// Re-point (or clear) the primary BEFORE the delete: the composite
		// FK would otherwise reject deleting the row the account points at.
		next, pickErr := q.PickFallbackPrimary(ctx, db.PickFallbackPrimaryParams{AccountID: accountID, ID: emailID})
		if pickErr != nil {
			// No candidate: clear the pointer and leave the account
			// without a primary.
			if err := q.ClearAccountPrimaryEmail(ctx, accountID); err != nil {
				return nil, fmt.Errorf("email: clear primary: %w", err)
			}
		} else if err := q.SetAccountPrimaryEmail(ctx, db.SetAccountPrimaryEmailParams{ID: accountID, PrimaryEmailID: pgInt8(next.ID)}); err != nil {
			return nil, fmt.Errorf("email: fallback primary: %w", err)
		}
		if n, err := q.DeleteAccountEmail(ctx, db.DeleteAccountEmailParams{ID: emailID, AccountID: accountID}); err != nil {
			return nil, fmt.Errorf("email: delete: %w", err)
		} else if n == 0 {
			return nil, ErrNotFound
		}
		if pickErr != nil {
			return nil, nil
		}
		addr := next.Address
		return &addr, nil
	}
	if n, err := q.DeleteAccountEmail(ctx, db.DeleteAccountEmailParams{ID: emailID, AccountID: accountID}); err != nil {
		return nil, fmt.Errorf("email: delete: %w", err)
	} else if n == 0 {
		return nil, ErrNotFound
	}
	return nil, nil
}
