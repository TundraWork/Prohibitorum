package email

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/db"
)

// failingAtInsertQueries drives the review's should-fix #11: the conflict
// pre-check sees no verified copy, but the insert loses a race on the
// partial unique index.
type failingAtInsertQueries struct {
	Queries
	insertErr error
}

func (f *failingAtInsertQueries) GetAccountByIDForUpdate(_ context.Context, id int32) (db.Account, error) {
	return db.Account{ID: id}, nil
}
func (f *failingAtInsertQueries) CountAccountEmails(context.Context, int32) (int64, error) {
	return 0, nil
}
func (f *failingAtInsertQueries) GetAccountEmailByAddress(_ context.Context, _ db.GetAccountEmailByAddressParams) (db.GetAccountEmailByAddressRow, error) {
	return db.GetAccountEmailByAddressRow{}, errNoRowForTest
}
func (f *failingAtInsertQueries) CountVerifiedAccountEmailsByAddress(context.Context, db.CountVerifiedAccountEmailsByAddressParams) (int64, error) {
	return 0, nil
}
func (f *failingAtInsertQueries) InsertAccountEmailVerified(context.Context, db.InsertAccountEmailVerifiedParams) (db.AccountEmail, error) {
	return db.AccountEmail{}, f.insertErr
}
func (f *failingAtInsertQueries) MarkAccountEmailVerified(context.Context, db.MarkAccountEmailVerifiedParams) (int64, error) {
	return 0, f.insertErr
}
func (f *failingAtInsertQueries) InsertAccountEmail(context.Context, db.InsertAccountEmailParams) (db.AccountEmail, error) {
	return db.AccountEmail{}, errNoRowForTest
}

var errNoRowForTest = errors.New("no row")

// verifiedTakenError builds the exact 23505 the partial unique index raises.
func verifiedTakenError() error {
	return &pgconn.PgError{
		Code:           "23505",
		ConstraintName: "account_email_verified_address_uidx",
	}
}

// TestAbsorbUpstreamLostInsertRaceIsASkip pins should-fix #11: when the
// insert races a concurrent verification and the index rejects it, the
// outcome is a skip (login proceeds) — never a raw error that would abort
// the caller's transaction.
func TestAbsorbUpstreamLostInsertRaceIsASkip(t *testing.T) {
	q := &failingAtInsertQueries{insertErr: verifiedTakenError()}
	outcome, _, err := AbsorbUpstream(context.Background(), q, 5, "race@example.test", 1)
	if err != nil {
		t.Fatalf("AbsorbUpstream error = %v; a lost race is a skip, not an error", err)
	}
	if outcome != AbsorbSkipTaken {
		t.Fatalf("outcome = %v, want skip_taken", outcome)
	}
}

// TestAbsorbUpstreamLostMarkRaceIsASkip covers the same race on the
// mark-verified path (an unverified local copy flipped by two logins).
func TestAbsorbUpstreamLostMarkRaceIsASkip(t *testing.T) {
	seeder := &seedFakeQueries{accountID: 5, address: "race@example.test", verified: false}
	if _, err := seedFakeInstall(seeder); err != nil {
		t.Fatal(err)
	}
	q := &failingAtMarkQueries{seedRealQueries: seedFakeWrap(seeder), markErr: verifiedTakenError()}
	outcome, _, err := AbsorbUpstream(context.Background(), q, 5, "race@example.test", 1)
	if err != nil {
		t.Fatalf("AbsorbUpstream error = %v; a lost race is a skip, not an error", err)
	}
	if outcome != AbsorbSkipTaken {
		t.Fatalf("outcome = %v, want skip_taken", outcome)
	}
}

// --- fake plumbing for the mark-race test ---

type seedFakeQueries struct {
	accountID int32
	address   string
	verified  bool
	id        int64
}

func seedFakeInstall(s *seedFakeQueries) (int64, error) {
	s.id = 77
	return s.id, nil
}

func seedFakeWrap(s *seedFakeQueries) *seedRealQueries { return &seedRealQueries{s: s} }

type seedRealQueries struct{ s *seedFakeQueries }

func (r *seedRealQueries) MarkAccountEmailVerified(context.Context, db.MarkAccountEmailVerifiedParams) (int64, error) {
	return 1, nil
}
func (r *seedRealQueries) DeleteAccountEmail(context.Context, db.DeleteAccountEmailParams) (int64, error) {
	return 1, nil
}
func (r *seedRealQueries) GetAccountEmail(_ context.Context, arg db.GetAccountEmailParams) (db.GetAccountEmailRow, error) {
	return db.GetAccountEmailRow{ID: arg.ID, AccountID: arg.AccountID, Address: r.s.address}, nil
}
func (r *seedRealQueries) SetAccountPrimaryEmail(context.Context, db.SetAccountPrimaryEmailParams) error {
	return nil
}
func (r *seedRealQueries) ClearAccountPrimaryEmail(context.Context, int32) error { return nil }
func (r *seedRealQueries) PickFallbackPrimary(context.Context, db.PickFallbackPrimaryParams) (db.PickFallbackPrimaryRow, error) {
	return db.PickFallbackPrimaryRow{}, errNoRowForTest
}
func (r *seedRealQueries) GetAccountPrimaryEmail(context.Context, int32) (string, error) {
	return "", errNoRowForTest
}

func (r *seedRealQueries) GetAccountByIDForUpdate(_ context.Context, id int32) (db.Account, error) {
	return db.Account{ID: id}, nil
}
func (r *seedRealQueries) CountAccountEmails(context.Context, int32) (int64, error) {
	return 1, nil
}
func (r *seedRealQueries) GetAccountEmailByAddress(_ context.Context, arg db.GetAccountEmailByAddressParams) (db.GetAccountEmailByAddressRow, error) {
	if arg.AccountID == r.s.accountID {
		return db.GetAccountEmailByAddressRow{
			ID: r.s.id, AccountID: r.s.accountID, Address: r.s.address,
			CreatedAt: pgtype.Timestamptz{Valid: true},
		}, nil
	}
	return db.GetAccountEmailByAddressRow{}, errNoRowForTest
}
func (r *seedRealQueries) CountVerifiedAccountEmailsByAddress(context.Context, db.CountVerifiedAccountEmailsByAddressParams) (int64, error) {
	return 0, nil
}
func (r *seedRealQueries) InsertAccountEmailVerified(context.Context, db.InsertAccountEmailVerifiedParams) (db.AccountEmail, error) {
	return db.AccountEmail{}, errNoRowForTest
}
func (r *seedRealQueries) InsertAccountEmail(context.Context, db.InsertAccountEmailParams) (db.AccountEmail, error) {
	return db.AccountEmail{}, errNoRowForTest
}

type failingAtMarkQueries struct {
	*seedRealQueries
	markErr error
}

func (f *failingAtMarkQueries) MarkAccountEmailVerified(context.Context, db.MarkAccountEmailVerifiedParams) (int64, error) {
	return 0, f.markErr
}
