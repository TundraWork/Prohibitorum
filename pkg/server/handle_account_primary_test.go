package server

import (
	"context"
	"testing"

	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/db"
)

// accountDetailQueries fakes the reads behind GET /accounts/{id}.
type fakeAccountDetailQueries struct {
	account  db.Account
	primary  string
	primaryE error
}

func (q *fakeAccountDetailQueries) GetAccountByID(_ context.Context, id int32) (db.Account, error) {
	q.account.ID = id
	return q.account, nil
}

func (q *fakeAccountDetailQueries) GetAccountPrimaryEmail(_ context.Context, _ int32) (string, error) {
	return q.primary, q.primaryE
}

// TestGetAccountCarriesPrimaryEmail pins the review's should-fix #10: the
// account detail view's primaryEmail must reflect the account's primary
// address, not stay empty. Failing-before proof: with the field hard-wired
// to nil, the body omits primaryEmail entirely.
func TestGetAccountCarriesPrimaryEmail(t *testing.T) {
	s := &Server{
		accountDetailOverride: &fakeAccountDetailQueries{
			account: db.Account{ID: 12, Username: "eve", DisplayName: "Eve"},
			primary: "eve@example.test",
		},
		config: &configx.Config{PublicOrigins: []string{"http://localhost:8080"}},
	}
	in := &getAccountIn{ID: 12}
	out, err := s.handleGetAccount(context.Background(), in)
	if err != nil {
		t.Fatalf("handleGetAccount: %v", err)
	}
	if out.Body.PrimaryEmail == nil || *out.Body.PrimaryEmail != "eve@example.test" {
		t.Fatalf("primaryEmail = %v; want eve@example.test", out.Body.PrimaryEmail)
	}

	// No primary: the field is omitted, not an empty string.
	s.accountDetailOverride = &fakeAccountDetailQueries{account: db.Account{ID: 12, Username: "eve"}}
	out, err = s.handleGetAccount(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if out.Body.PrimaryEmail != nil {
		t.Fatalf("primaryEmail = %v; want omitted", *out.Body.PrimaryEmail)
	}
}
