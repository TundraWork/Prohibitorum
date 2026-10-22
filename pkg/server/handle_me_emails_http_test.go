package server

import (
	"context"

	"encoding/json"
	"net/http"
	"net/http/httptest"
	"prohibitorum/pkg/account/email"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/mail"
)

// meEmailHTTPQueries fakes the read surface behind GET /me/emails and
// POST /me/emails/{id}/primary: the caller's account has one verified
// primary address and one unverified spare.
type meEmailHTTPQueries struct {
	rows []db.ListAccountEmailsRow
}

func (q *meEmailHTTPQueries) ListAccountEmails(_ context.Context, accountID int32) ([]db.ListAccountEmailsRow, error) {
	out := make([]db.ListAccountEmailsRow, 0, len(q.rows))
	for _, row := range q.rows {
		if row.AccountID == accountID {
			out = append(out, row)
		}
	}
	return out, nil
}

func (q *meEmailHTTPQueries) GetAccountEmail(_ context.Context, arg db.GetAccountEmailParams) (db.GetAccountEmailRow, error) {
	for _, row := range q.rows {
		if row.ID == arg.ID && row.AccountID == arg.AccountID {
			return db.GetAccountEmailRow{
				ID: row.ID, AccountID: row.AccountID, Address: row.Address,
				VerifiedAt: row.VerifiedAt, VerifiedVia: row.VerifiedVia,
				CreatedAt: row.CreatedAt, IsPrimary: row.IsPrimary,
			}, nil
		}
	}
	return db.GetAccountEmailRow{}, pgx.ErrNoRows
}

func (q *meEmailHTTPQueries) GetAccountPrimaryEmail(_ context.Context, _ int32) (string, error) {
	return "primary@example.test", nil
}

// meEmailHTTPTx satisfies the write seam with the same fake rows: the
// primary handler only needs GetAccountEmail + SetAccountPrimaryEmail.
type meEmailHTTPTx struct{ q *meEmailHTTPQueries }

func (t *meEmailHTTPTx) Queries() email.Queries         { return t.q }
func (t *meEmailHTTPTx) Commit(context.Context) error   { return nil }
func (t *meEmailHTTPTx) Rollback(context.Context) error { return nil }

// The email.Queries surface beyond the reads above: locking and writes the
// HTTP shape test does not assert (the DB integration tests cover them).
func (q *meEmailHTTPQueries) GetAccountByIDForUpdate(_ context.Context, _ int32) (db.Account, error) {
	return db.Account{ID: 5}, nil
}
func (q *meEmailHTTPQueries) CountAccountEmails(_ context.Context, _ int32) (int64, error) {
	return int64(len(q.rows)), nil
}
func (q *meEmailHTTPQueries) GetAccountEmailByAddress(_ context.Context, _ db.GetAccountEmailByAddressParams) (db.GetAccountEmailByAddressRow, error) {
	return db.GetAccountEmailByAddressRow{}, pgx.ErrNoRows
}
func (q *meEmailHTTPQueries) InsertAccountEmail(_ context.Context, _ db.InsertAccountEmailParams) (db.AccountEmail, error) {
	return db.AccountEmail{}, nil
}
func (q *meEmailHTTPQueries) InsertAccountEmailVerified(_ context.Context, _ db.InsertAccountEmailVerifiedParams) (db.AccountEmail, error) {
	return db.AccountEmail{}, nil
}
func (q *meEmailHTTPQueries) MarkAccountEmailVerified(_ context.Context, _ db.MarkAccountEmailVerifiedParams) (int64, error) {
	return 1, nil
}
func (q *meEmailHTTPQueries) DeleteAccountEmail(_ context.Context, _ db.DeleteAccountEmailParams) (int64, error) {
	return 1, nil
}
func (q *meEmailHTTPQueries) SetAccountPrimaryEmail(_ context.Context, arg db.SetAccountPrimaryEmailParams) error {
	return nil
}
func (q *meEmailHTTPQueries) ClearAccountPrimaryEmail(_ context.Context, _ int32) error {
	return nil
}
func (q *meEmailHTTPQueries) PickFallbackPrimary(_ context.Context, _ db.PickFallbackPrimaryParams) (db.PickFallbackPrimaryRow, error) {
	return db.PickFallbackPrimaryRow{}, pgx.ErrNoRows
}
func (q *meEmailHTTPQueries) CountVerifiedAccountEmailsByAddress(_ context.Context, _ db.CountVerifiedAccountEmailsByAddressParams) (int64, error) {
	return 0, nil
}

// TestMeEmailsHTTPResponsesCarryABody pins the review's B2: typed Huma
// handlers that return a bare view struct make Huma emit the fields as
// response headers with a 204 and no body, so the dashboard's
// requireJsonData fails. Both reads must answer 200 with a JSON body.
func TestMeEmailsHTTPResponsesCarryABody(t *testing.T) {
	router, s := realAdminOnlyRouter(t)
	q := &meEmailHTTPQueries{rows: []db.ListAccountEmailsRow{
		{
			ID: 1, AccountID: 5, Address: "primary@example.test",
			VerifiedAt: pgtype.Timestamptz{Valid: true}, VerifiedVia: pgtype.Text{String: "code", Valid: true},
			CreatedAt: pgtype.Timestamptz{Valid: true}, IsPrimary: true,
		},
		{
			ID: 2, AccountID: 5, Address: "spare@example.test",
			VerifiedAt: pgtype.Timestamptz{Valid: true}, VerifiedVia: pgtype.Text{String: "code", Valid: true},
			CreatedAt:  pgtype.Timestamptz{Valid: true},
		},
	}}
	s.meEmailsOverride = q
	s.mailSender = mail.NullSender{}
	s.meEmailTxRunnerOverride = fixedMeEmailTxRunner{tx: &meEmailHTTPTx{q: q}}

	sess := &authn.Session{
		Account: &db.Account{ID: 5, Role: "user"},
		Data:    &authn.SessionData{},
	}

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, reqWithSession(http.MethodGet, "/api/prohibitorum/me/emails", "", "", sess))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /me/emails status = %d (headers %v); want 200 with a body", rr.Code, rr.Header())
	}
	var list struct {
		Emails            []json.RawMessage `json:"emails"`
		DeliveryAvailable bool              `json:"deliveryAvailable"`
		Limit             int               `json:"limit"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &list); err != nil {
		t.Fatalf("GET /me/emails body is not the JSON view: %v (%q)", err, rr.Body.String())
	}
	if len(list.Emails) != 2 || list.Limit != 10 {
		t.Fatalf("GET /me/emails body = %q; want the two-address view with limit", rr.Body.String())
	}
	for _, header := range []string{"Emails", "Deliveryavailable", "Limit"} {
		if _, ok := rr.Header()[http.CanonicalHeaderKey(header)]; ok {
			t.Errorf("GET /me/emails leaked %q as a response header", header)
		}
	}

	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, reqWithSession(http.MethodPost, "/api/prohibitorum/me/emails/2/primary", "", "", sess))
	if rr.Code != http.StatusOK {
		t.Fatalf("POST /me/emails/{id}/primary status = %d (headers %v); want 200 with a body", rr.Code, rr.Header())
	}
	var updated struct {
		Emails []json.RawMessage `json:"emails"`
		Limit  int               `json:"limit"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &updated); err != nil {
		t.Fatalf("POST primary body is not the JSON view: %v (%q)", err, rr.Body.String())
	}
	if len(updated.Emails) != 2 || updated.Limit != 10 {
		t.Fatalf("POST primary body = %q; want the refreshed list view", rr.Body.String())
	}
}

type fixedMeEmailTxRunner struct{ tx meEmailTx }

func (r fixedMeEmailTxRunner) BeginMeEmailTx(context.Context) (meEmailTx, error) {
	return r.tx, nil
}
