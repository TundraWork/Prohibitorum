package server

import (
	"context"
	"testing"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/weberr"
)

// accountUpdateTestTx records the small query surface used by handleUpdateAccount.
// It deliberately models a single transaction so tests can prove the account
// update and last-admin guard remain atomic.
type accountUpdateTestTx struct {
	current     db.Account
	updateCalls []db.UpdateAccountParams
	order       []string
	committed   bool
	rolledBack  bool
}

func (q *accountUpdateTestTx) Queries() accountUpdateQueries { return q }

func (q *accountUpdateTestTx) Commit(context.Context) error {
	q.committed = true
	q.order = append(q.order, "commit")
	return nil
}

func (q *accountUpdateTestTx) Rollback(context.Context) error {
	if !q.committed {
		q.rolledBack = true
		q.order = append(q.order, "rollback")
	}
	return nil
}

func (q *accountUpdateTestTx) GetAccountByIDForUpdate(context.Context, int32) (db.Account, error) {
	q.order = append(q.order, "lock")
	return q.current, nil
}

func (q *accountUpdateTestTx) CountActiveAdminsForUpdate(context.Context) (int64, error) {
	q.order = append(q.order, "count_active_admins")
	return 2, nil
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
		config:                        &configx.Config{PublicOrigins: []string{"https://id.example.test"}},
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

func TestHandleUpdateAccountRejectsRemovedAppManagerRole(t *testing.T) {
	tx := &accountUpdateTestTx{current: db.Account{ID: 7, Role: "user"}}
	s := newAccountUpdateTestServer(tx)

	_, err := s.handleUpdateAccount(accountUpdateContext(), updateAccountInput("app_manager"))
	if publicErr := weberr.AsPublic(err); publicErr == nil || publicErr.Code != "invalid_role" {
		t.Fatalf("handleUpdateAccount error = %v, want invalid_role", err)
	}
	if len(tx.updateCalls) != 0 || len(tx.order) != 0 {
		t.Fatalf("invalid role reached transaction: calls=%#v order=%#v", tx.updateCalls, tx.order)
	}
}

func TestHandleUpdateAccountPreservesAssignmentsAcrossRoleChanges(t *testing.T) {
	tx := &accountUpdateTestTx{current: db.Account{ID: 7, Role: "admin"}}
	s := newAccountUpdateTestServer(tx)

	if _, err := s.handleUpdateAccount(accountUpdateContext(), updateAccountInput("user")); err != nil {
		t.Fatalf("handleUpdateAccount: %v", err)
	}
	wantOrder := []string{"begin", "lock", "count_active_admins", "update", "commit"}
	if len(tx.order) != len(wantOrder) {
		t.Fatalf("transaction order = %#v, want %#v", tx.order, wantOrder)
	}
	for i := range wantOrder {
		if tx.order[i] != wantOrder[i] {
			t.Fatalf("transaction order = %#v, want %#v", tx.order, wantOrder)
		}
	}
}
