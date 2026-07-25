package server

import (
	"context"
	"testing"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/db"
)

// accountUpdateTestTx records the small query surface used by handleUpdateAccount.
// It deliberately models a single transaction so tests can prove cleanup occurs
// before the account update commits.
type accountUpdateTestTx struct {
	current       db.Account
	updateCalls   []db.UpdateAccountParams
	deleteAccount []int32
	order         []string
}

func (q *accountUpdateTestTx) Queries() accountUpdateQueries { return q }

func (q *accountUpdateTestTx) Commit(context.Context) error {
	q.order = append(q.order, "commit")
	return nil
}

func (q *accountUpdateTestTx) Rollback(context.Context) error { return nil }

func (q *accountUpdateTestTx) GetAccountByID(context.Context, int32) (db.Account, error) {
	q.order = append(q.order, "load")
	return q.current, nil
}

func (q *accountUpdateTestTx) CountActiveAdminsForUpdate(context.Context) (int64, error) {
	q.order = append(q.order, "count_active_admins")
	return 2, nil
}

func (q *accountUpdateTestTx) DeleteManagerAssignmentsForAccount(_ context.Context, accountID int32) error {
	q.order = append(q.order, "delete_manager_assignments")
	q.deleteAccount = append(q.deleteAccount, accountID)
	return nil
}

func (q *accountUpdateTestTx) UpdateAccount(_ context.Context, arg db.UpdateAccountParams) (db.Account, error) {
	q.order = append(q.order, "update")
	q.updateCalls = append(q.updateCalls, arg)
	updated := q.current
	updated.ID = arg.ID
	updated.DisplayName = arg.DisplayName
	updated.Role = arg.Role
	updated.Attributes = arg.Attributes
	updated.Disabled = arg.Disabled
	updated.Email = arg.Email
	updated.EmailVerified = arg.EmailVerified
	return updated, nil
}

type accountUpdateTestRunner struct{ tx *accountUpdateTestTx }

func (r accountUpdateTestRunner) BeginAccountUpdateTx(context.Context) (accountUpdateTx, error) {
	r.tx.order = append(r.tx.order, "begin")
	return r.tx, nil
}

func newAccountUpdateTestServer(tx *accountUpdateTestTx) *Server {
	return &Server{
		config:                       &configx.Config{PublicOrigins: []string{"https://id.example.test"}},
		accountUpdateTxRunnerOverride: accountUpdateTestRunner{tx: tx},
	}
}

func updateAccountInput(role string) *updateAccountIn {
	in := &updateAccountIn{ID: 7}
	in.Body.DisplayName = "Managed Account"
	in.Body.Role = role
	return in
}

func accountUpdateContext() context.Context {
	return authn.WithSession(context.Background(), &authn.Session{Account: &db.Account{ID: 1, Role: "admin"}})
}

func TestHandleUpdateAccount_AcceptsAppManagerRole(t *testing.T) {
	tx := &accountUpdateTestTx{current: db.Account{ID: 7, Role: "user"}}
	s := newAccountUpdateTestServer(tx)

	out, err := s.handleUpdateAccount(accountUpdateContext(), updateAccountInput("app_manager"))
	if err != nil {
		t.Fatalf("handleUpdateAccount: %v", err)
	}
	if out.Body.Role != "app_manager" {
		t.Fatalf("updated role = %q, want app_manager", out.Body.Role)
	}
	if len(tx.updateCalls) != 1 || tx.updateCalls[0].Role != "app_manager" {
		t.Fatalf("update calls = %#v, want one app_manager update", tx.updateCalls)
	}
	if len(tx.deleteAccount) != 0 {
		t.Fatalf("manager cleanup called for promotion: %#v", tx.deleteAccount)
	}
}

func TestHandleUpdateAccount_DemotionDeletesManagerAssignmentsInTransaction(t *testing.T) {
	tx := &accountUpdateTestTx{current: db.Account{ID: 7, Role: "app_manager"}}
	s := newAccountUpdateTestServer(tx)

	if _, err := s.handleUpdateAccount(accountUpdateContext(), updateAccountInput("user")); err != nil {
		t.Fatalf("handleUpdateAccount: %v", err)
	}
	if len(tx.deleteAccount) != 1 || tx.deleteAccount[0] != 7 {
		t.Fatalf("manager cleanup accounts = %#v, want [7]", tx.deleteAccount)
	}
	wantOrder := []string{"begin", "load", "delete_manager_assignments", "update", "commit"}
	if len(tx.order) != len(wantOrder) {
		t.Fatalf("transaction order = %#v, want %#v", tx.order, wantOrder)
	}
	for i := range wantOrder {
		if tx.order[i] != wantOrder[i] {
			t.Fatalf("transaction order = %#v, want %#v", tx.order, wantOrder)
		}
	}
}
