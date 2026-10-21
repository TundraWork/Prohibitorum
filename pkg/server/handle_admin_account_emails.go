package server

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"prohibitorum/pkg/account/email"
	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
)

// mapAdminEmailRuleError is the admin variant: ErrTaken maps to email_taken
// (admins are allowed to know a conflict exists), 404s stay 404.
func mapAdminEmailRuleError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, email.ErrTaken):
		return authErrToHuma(authn.ErrEmailTaken())
	}
	return mapEmailRuleError(err)
}

// adminEmailView includes the asserting admin's username (VerifiedBy) —
// provenance the admin surface is allowed to show.
func adminEmailView(row db.ListAccountEmailsRow) contract.EmailAddressView {
	v := emailView(row)
	v.VerifiedBy = row.VerifiedByUsername.String
	return v
}

func adminEmailsOut(rows []db.ListAccountEmailsRow) *contract.AccountEmailsView {
	emails := make([]contract.EmailAddressView, 0, len(rows))
	for _, row := range rows {
		emails = append(emails, adminEmailView(row))
	}
	return &contract.AccountEmailsView{Emails: emails, Limit: email.MaxPerAccount}
}

// accountEmailAuditDetail is the shared detail shape for admin email writes:
// the actor is the admin, the target the account being modified.
func accountEmailAuditDetail(actor *authn.Session, emailID *int64, address, event string) (map[string]any, *int32) {
	detail := map[string]any{"address": address}
	if emailID != nil {
		detail["emailId"] = *emailID
	}
	var actorID *int32
	if actor != nil && actor.Account != nil {
		id := actor.Account.ID
		actorID = &id
		detail["actorAccountId"] = id
	}
	return detail, actorID
}

// ----- GET /accounts/{id}/emails -------------------------------------------

type listAccountEmailsIn struct {
	ID int32 `path:"id"`
}

type listAccountEmailsOut struct {
	Body contract.AccountEmailsView
}

func (s *Server) handleListAccountEmails(ctx context.Context, in *listAccountEmailsIn) (*listAccountEmailsOut, error) {
	if _, err := s.queries.GetAccountByID(ctx, in.ID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, authErrToHuma(authn.ErrAccountNotFound())
		}
		return nil, fmt.Errorf("handleListAccountEmails: %w", err)
	}
	rows, err := s.queries.ListAccountEmails(ctx, in.ID)
	if err != nil {
		return nil, fmt.Errorf("handleListAccountEmails: %w", err)
	}
	return &listAccountEmailsOut{Body: *adminEmailsOut(rows)}, nil
}

// ----- POST /accounts/{id}/emails -------------------------------------------

type addAccountEmailIn struct {
	ID   int32 `path:"id"`
	Body struct {
		Address  string `json:"address"`
		Verified bool   `json:"verified,omitempty"`
		Primary  bool   `json:"primary,omitempty"`
	}
}

type addAccountEmailOut struct {
	Body contract.EmailAddressView
}

func (s *Server) handleAddAccountEmail(ctx context.Context, in *addAccountEmailIn) (*addAccountEmailOut, error) {
	sess := authn.SessionFromContext(ctx)
	if in.Body.Primary && !in.Body.Verified {
		return nil, authErrToHuma(authn.ErrEmailNotVerifiedForPrimary())
	}
	tx, err := s.beginMeEmailTx(ctx)
	if err != nil {
		return nil, fmt.Errorf("handleAddAccountEmail: begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	row, err := email.AdminAdd(ctx, tx.Queries(), in.ID, in.Body.Address, in.Body.Verified, in.Body.Primary, adminIDFrom(sess))
	if err != nil {
		return nil, mapAdminEmailRuleError(err)
	}
	rows, err := s.queries.ListAccountEmails(ctx, in.ID)
	if err != nil {
		return nil, fmt.Errorf("handleAddAccountEmail: reload: %w", err)
	}
	var view contract.EmailAddressView
	for _, r := range rows {
		if r.ID == row.ID {
			view = adminEmailView(r)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("handleAddAccountEmail: commit: %w", err)
	}

	detail, actorID := accountEmailAuditDetail(sess, &row.ID, row.Address, audit.EventRegister)
	if in.Body.Verified {
		detail["via"] = "admin"
	}
	audit.RecordOrLog(ctx, s.Audit, audit.Record{
		AccountID: actorID,
		Factor:    audit.FactorEmail,
		Event:     audit.EventRegister,
		Detail:    detail,
	})
	return &addAccountEmailOut{Body: view}, nil
}

func adminIDFrom(sess *authn.Session) int32 {
	if sess == nil || sess.Account == nil {
		return 0
	}
	return sess.Account.ID
}

// ----- POST /accounts/{id}/emails/{emailId}/verify --------------------------

type verifyAccountEmailIn struct {
	ID      int32 `path:"id"`
	EmailID int64 `path:"emailId"`
}

type verifyAccountEmailOut struct {
	Body contract.EmailAddressView
}

func (s *Server) handleVerifyAccountEmail(ctx context.Context, in *verifyAccountEmailIn) (*verifyAccountEmailOut, error) {
	sess := authn.SessionFromContext(ctx)
	tx, err := s.beginMeEmailTx(ctx)
	if err != nil {
		return nil, fmt.Errorf("handleVerifyAccountEmail: begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Ownership pre-check keeps foreign email ids a plain 404 before any
	// write fires; AssertVerified would return the same via ErrNotFound.
	if _, err := s.queries.GetAccountEmail(ctx, db.GetAccountEmailParams{ID: in.EmailID, AccountID: in.ID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, authErrToHuma(authn.ErrEmailNotFound())
		}
		return nil, fmt.Errorf("handleVerifyAccountEmail: load: %w", err)
	}

	var adminID int64
	if id := adminIDFrom(sess); id != 0 {
		adminID = int64(id)
	}
	row, err := email.AssertVerified(ctx, tx.Queries(), in.ID, in.EmailID, "admin", nil, &adminID)
	if err != nil {
		return nil, mapAdminEmailRuleError(err)
	}
	rows, err := s.queries.ListAccountEmails(ctx, in.ID)
	if err != nil {
		return nil, fmt.Errorf("handleVerifyAccountEmail: reload: %w", err)
	}
	var view contract.EmailAddressView
	for _, r := range rows {
		if r.ID == row.ID {
			view = adminEmailView(r)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("handleVerifyAccountEmail: commit: %w", err)
	}

	detail, actorID := accountEmailAuditDetail(sess, &row.ID, row.Address, audit.EventVerified)
	detail["via"] = "admin"
	audit.RecordOrLog(ctx, s.Audit, audit.Record{
		AccountID: actorID,
		Factor:    audit.FactorEmail,
		Event:     audit.EventVerified,
		Detail:    detail,
	})
	return &verifyAccountEmailOut{Body: view}, nil
}

// ----- POST /accounts/{id}/emails/{emailId}/primary --------------------------

type setAccountEmailPrimaryIn struct {
	ID      int32 `path:"id"`
	EmailID int64 `path:"emailId"`
}

type setAccountEmailPrimaryOut = contract.AccountEmailsView

func (s *Server) handleSetAccountEmailPrimary(ctx context.Context, in *setAccountEmailPrimaryIn) (*contract.AccountEmailsView, error) {
	sess := authn.SessionFromContext(ctx)
	tx, err := s.beginMeEmailTx(ctx)
	if err != nil {
		return nil, fmt.Errorf("handleSetAccountEmailPrimary: begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if err := email.SetPrimary(ctx, tx.Queries(), in.ID, in.EmailID); err != nil {
		return nil, mapAdminEmailRuleError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("handleSetAccountEmailPrimary: commit: %w", err)
	}
	rows, err := s.queries.ListAccountEmails(ctx, in.ID)
	if err != nil {
		return nil, fmt.Errorf("handleSetAccountEmailPrimary: reload: %w", err)
	}
	var address string
	for _, r := range rows {
		if r.IsPrimary {
			address = r.Address
		}
	}

	detail, actorID := accountEmailAuditDetail(sess, &in.EmailID, address, audit.EventPrimaryChanged)
	audit.RecordOrLog(ctx, s.Audit, audit.Record{
		AccountID: actorID,
		Factor:    audit.FactorEmail,
		Event:     audit.EventPrimaryChanged,
		Detail:    detail,
	})
	return adminEmailsOut(rows), nil
}

// ----- DELETE /accounts/{id}/emails/{emailId} ---------------------------------

type deleteAccountEmailIn struct {
	ID      int32 `path:"id"`
	EmailID int64 `path:"emailId"`
}

func (s *Server) handleDeleteAccountEmail(ctx context.Context, in *deleteAccountEmailIn) (*struct{}, error) {
	sess := authn.SessionFromContext(ctx)
	tx, err := s.beginMeEmailTx(ctx)
	if err != nil {
		return nil, fmt.Errorf("handleDeleteAccountEmail: begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	row, err := s.queries.GetAccountEmail(ctx, db.GetAccountEmailParams{ID: in.EmailID, AccountID: in.ID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, authErrToHuma(authn.ErrEmailNotFound())
		}
		return nil, fmt.Errorf("handleDeleteAccountEmail: load: %w", err)
	}
	fallback, err := email.Remove(ctx, tx.Queries(), in.ID, in.EmailID, true)
	if err != nil {
		return nil, mapAdminEmailRuleError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("handleDeleteAccountEmail: commit: %w", err)
	}

	detail, actorID := accountEmailAuditDetail(sess, &in.EmailID, row.Address, audit.EventRevoke)
	if fallback != nil {
		detail["fallbackPrimary"] = *fallback
	}
	audit.RecordOrLog(ctx, s.Audit, audit.Record{
		AccountID: actorID,
		Factor:    audit.FactorEmail,
		Event:     audit.EventRevoke,
		Detail:    detail,
	})
	return &struct{}{}, nil
}
