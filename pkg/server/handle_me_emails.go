package server

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sirupsen/logrus"

	"prohibitorum/pkg/account/email"
	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/logx"
	"prohibitorum/pkg/mail"
)

// meEmailsQueries is the narrow surface the /me/emails handlers read
// through; tests inject a fake. Nil in production — falls back to s.queries.
type meEmailsQueries interface {
	ListAccountEmails(ctx context.Context, accountID int32) ([]db.ListAccountEmailsRow, error)
	GetAccountEmail(ctx context.Context, arg db.GetAccountEmailParams) (db.GetAccountEmailRow, error)
	GetAccountPrimaryEmail(ctx context.Context, id int32) (string, error)
}

// meEmailsQ returns the read surface for the /me/emails handlers: the test
// override when present, the shared queries otherwise.
func (s *Server) meEmailsQ() meEmailsQueries {
	if s.meEmailsOverride != nil {
		return s.meEmailsOverride
	}
	return s.queries
}

// meEmailTx is the transaction seam the write endpoints share with tests.
type meEmailTx interface {
	Queries() email.Queries
	Commit(context.Context) error
	Rollback(context.Context) error
}

type meEmailTxRunner interface {
	BeginMeEmailTx(context.Context) (meEmailTx, error)
}

type pgMeEmailTx struct {
	tx      pgx.Tx
	queries email.Queries
}

func (t *pgMeEmailTx) Queries() email.Queries { return t.queries }
func (t *pgMeEmailTx) Commit(ctx context.Context) error {
	return t.tx.Commit(ctx)
}
func (t *pgMeEmailTx) Rollback(ctx context.Context) error {
	return t.tx.Rollback(ctx)
}

func (s *Server) beginMeEmailTx(ctx context.Context) (meEmailTx, error) {
	if s.meEmailTxRunnerOverride != nil {
		return s.meEmailTxRunnerOverride.BeginMeEmailTx(ctx)
	}
	tx, err := s.dbPool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &pgMeEmailTx{tx: tx, queries: s.queries.WithTx(tx)}, nil
}

// emailView builds the self-service projection: verifiedBy (the asserting
// admin) is admin-surface only and stays empty here.
func emailView(row db.ListAccountEmailsRow) contract.EmailAddressView {
	v := contract.EmailAddressView{
		ID:        row.ID,
		Address:   row.Address,
		Primary:   row.IsPrimary,
		CreatedAt: row.CreatedAt.Time,
	}
	if row.VerifiedAt.Valid {
		verifiedAt := row.VerifiedAt.Time
		v.Verified = true
		v.VerifiedAt = &verifiedAt
		v.VerifiedVia = row.VerifiedVia.String
		v.VerifiedProvider = row.VerifiedProvider.String
	}
	return v
}

func myEmailsOut(rows []db.ListAccountEmailsRow, delivery bool) *contract.MyEmailsView {
	emails := make([]contract.EmailAddressView, 0, len(rows))
	for _, row := range rows {
		emails = append(emails, emailView(row))
	}
	return &contract.MyEmailsView{Emails: emails, DeliveryAvailable: delivery, Limit: email.MaxPerAccount}
}

// mapEmailRuleError translates the write-path sentinels into their
// self-service public codes.
func mapEmailRuleError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, email.ErrInvalidAddress):
		return authErrToHuma(authn.ErrEmailInvalid())
	case errors.Is(err, email.ErrCapReached):
		return authErrToHuma(authn.ErrEmailLimitReached())
	case errors.Is(err, email.ErrDuplicate):
		return authErrToHuma(authn.ErrEmailExists())
	case errors.Is(err, email.ErrTaken):
		// Deliberately vague: the caller must not learn who holds it.
		return authErrToHuma(authn.ErrEmailUnavailable())
	case errors.Is(err, email.ErrNotVerifiedForPrimary):
		return authErrToHuma(authn.ErrEmailNotVerifiedForPrimary())
	case errors.Is(err, email.ErrPrimaryUndeletable):
		return authErrToHuma(authn.ErrEmailPrimaryUndeletable())
	case errors.Is(err, email.ErrNotFound):
		return authErrToHuma(authn.ErrEmailNotFound())
	case errors.Is(err, email.ErrAlreadyVerified):
		return authErrToHuma(authn.ErrEmailAlreadyVerified())
	}
	return err
}

// ----- GET /me/emails ------------------------------------------------------

type listMyEmailsOut struct {
	Body contract.MyEmailsView
}

func (s *Server) handleListMyEmails(ctx context.Context, _ *struct{}) (*listMyEmailsOut, error) {
	sess := authn.SessionFromContext(ctx)
	if sess == nil {
		return nil, authErrToHuma(authn.ErrNoSession())
	}
	rows, err := s.meEmailsQ().ListAccountEmails(ctx, sess.Account.ID)
	if err != nil {
		return nil, fmt.Errorf("handleListMyEmails: %w", err)
	}
	return &listMyEmailsOut{Body: *myEmailsOut(rows, s.mailSender.Available())}, nil
}

// ----- POST /me/emails -----------------------------------------------------

type addMyEmailIn struct {
	Body struct {
		Address string `json:"address"`
	}
}

type addMyEmailOut struct {
	Body contract.EmailAddressView
}

func (s *Server) handleAddMyEmail(ctx context.Context, in *addMyEmailIn) (*addMyEmailOut, error) {
	sess := authn.SessionFromContext(ctx)
	if sess == nil {
		return nil, authErrToHuma(authn.ErrNoSession())
	}
	tx, err := s.beginMeEmailTx(ctx)
	if err != nil {
		return nil, fmt.Errorf("handleAddMyEmail: begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	row, err := email.Add(ctx, tx.Queries(), sess.Account.ID, in.Body.Address)
	if err != nil {
		return nil, mapEmailRuleError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("handleAddMyEmail: commit: %w", err)
	}

	acctID := sess.Account.ID
	audit.RecordOrLog(ctx, s.Audit, audit.Record{
		AccountID:     &acctID,
		Factor:        audit.FactorEmail,
		Event:         audit.EventRegister,
		CredentialRef: &row.ID,
		Detail:        map[string]any{"address": row.Address},
	})

	return &addMyEmailOut{Body: contract.EmailAddressView{
		ID: row.ID, Address: row.Address, CreatedAt: row.CreatedAt.Time,
	}}, nil
}

// ----- POST /me/emails/{id}/send-code --------------------------------------

type sendMyEmailCodeIn struct {
	ID   int64 `path:"id"`
	Body struct {
		Locale string `json:"locale,omitempty"`
	}
}

type sendMyEmailCodeOut struct {
	Body struct {
		ResendAfterSeconds int `json:"resendAfterSeconds"`
		ExpiresInSeconds   int `json:"expiresInSeconds"`
	}
}

func (s *Server) handleSendMyEmailCode(ctx context.Context, in *sendMyEmailCodeIn) (*sendMyEmailCodeOut, error) {
	sess := authn.SessionFromContext(ctx)
	if sess == nil {
		return nil, authErrToHuma(authn.ErrNoSession())
	}
	if !s.mailSender.Available() {
		return nil, authErrToHuma(authn.ErrEmailDeliveryUnavailable())
	}

	row, err := s.meEmailsQ().GetAccountEmail(ctx, db.GetAccountEmailParams{ID: in.ID, AccountID: sess.Account.ID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, authErrToHuma(authn.ErrEmailNotFound())
		}
		return nil, fmt.Errorf("handleSendMyEmailCode: load: %w", err)
	}
	if row.VerifiedAt.Valid {
		return nil, authErrToHuma(authn.ErrEmailAlreadyVerified())
	}

	wait, ok := s.emailLimits.AllowCodeSend(ctx, sess.Account.ID, row.ID, row.Address)
	if !ok {
		return nil, rateLimitedHuma(wait)
	}

	code, err := email.Issue(ctx, s.kvStore, row.ID, sess.Account.ID, lower(row.Address))
	if err != nil {
		return nil, fmt.Errorf("handleSendMyEmailCode: issue: %w", err)
	}

	msg, err := mail.CodeMail(in.Body.Locale, s.mailBrand(ctx), code, int(email.CodeTTL/time.Minute))
	if err != nil {
		email.Drop(ctx, s.kvStore, row.ID)
		return nil, fmt.Errorf("handleSendMyEmailCode: template: %w", err)
	}
	msg.To = row.Address
	if err := s.mailSender.Send(ctx, msg); err != nil {
		// The mail never went out; a code the user can't receive must not
		// linger (it would burn the cooldown for nothing).
		email.Drop(ctx, s.kvStore, row.ID)
		logx.WithContext(ctx).WithFields(logrus.Fields{
			"event": "email.delivery_failed", "email_id": row.ID,
		}).WithError(err).Warn("email")
		return nil, authErrToHuma(authn.ErrEmailDeliveryFailed())
	}

	acctID := sess.Account.ID
	audit.RecordOrLog(ctx, s.Audit, audit.Record{
		AccountID:     &acctID,
		Factor:        audit.FactorEmail,
		Event:         audit.EventVerificationSent,
		CredentialRef: &row.ID,
		Detail:        map[string]any{"address": row.Address},
	})

	out := &sendMyEmailCodeOut{}
	out.Body.ResendAfterSeconds = 60
	out.Body.ExpiresInSeconds = int(email.CodeTTL / time.Second)
	return out, nil
}

// ----- POST /me/emails/{id}/verify ------------------------------------------

type verifyMyEmailIn struct {
	ID   int64 `path:"id"`
	Body struct {
		Code string `json:"code"`
	}
}

type verifyMyEmailOut struct {
	Body contract.EmailAddressView
}

func (s *Server) handleVerifyMyEmail(ctx context.Context, in *verifyMyEmailIn) (*verifyMyEmailOut, error) {
	sess := authn.SessionFromContext(ctx)
	if sess == nil {
		return nil, authErrToHuma(authn.ErrNoSession())
	}

	result, attemptsRemaining, err := email.Verify(ctx, s.kvStore, in.ID, sess.Account.ID, in.Body.Code)
	if err != nil {
		return nil, fmt.Errorf("handleVerifyMyEmail: %w", err)
	}
	switch result {
	case email.VerifyExpired:
		return nil, authErrToHuma(authn.ErrEmailCodeExpired())
	case email.VerifyInvalid:
		acctID := sess.Account.ID
		audit.RecordOrLog(ctx, s.Audit, audit.Record{
			AccountID:     &acctID,
			Factor:        audit.FactorEmail,
			Event:         audit.EventFail,
			CredentialRef: &in.ID,
			Detail:        map[string]any{"reason": "code_invalid"},
		})
		return nil, authErrToHuma(authn.ErrEmailCodeInvalid(attemptsRemaining))
	}

	// The code matched: flip the row in one transaction. ErrTaken maps to
	// the vague email_unavailable, same as everywhere on this surface.
	tx, err := s.beginMeEmailTx(ctx)
	if err != nil {
		return nil, fmt.Errorf("handleVerifyMyEmail: begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	before, err := s.meEmailsQ().GetAccountPrimaryEmail(ctx, sess.Account.ID)
	hadPrimary := err == nil
	row, err := email.AssertVerified(ctx, tx.Queries(), sess.Account.ID, in.ID, "code", nil, nil)
	if err != nil {
		return nil, mapEmailRuleError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("handleVerifyMyEmail: commit: %w", err)
	}

	acctID := sess.Account.ID
	audit.RecordOrLog(ctx, s.Audit, audit.Record{
		AccountID:     &acctID,
		Factor:        audit.FactorEmail,
		Event:         audit.EventVerified,
		CredentialRef: &row.ID,
		Detail:        map[string]any{"address": row.Address, "via": "code"},
	})
	if !hadPrimary || before != row.Address {
		audit.RecordOrLog(ctx, s.Audit, audit.Record{
			AccountID:     &acctID,
			Factor:        audit.FactorEmail,
			Event:         audit.EventPrimaryChanged,
			CredentialRef: &row.ID,
			Detail:        map[string]any{"address": row.Address, "via": "auto"},
		})
	}

	out := &verifyMyEmailOut{Body: contract.EmailAddressView{
		ID: row.ID, Address: row.Address, Verified: true,
		VerifiedVia: row.VerifiedVia.String, CreatedAt: row.CreatedAt.Time,
	}}
	if !hadPrimary {
		out.Body.Primary = true
	}
	return out, nil
}

// ----- POST /me/emails/{id}/primary -----------------------------------------

type setMyEmailPrimaryIn struct {
	ID int64 `path:"id"`
}

type setMyEmailPrimaryOut struct {
	Body contract.MyEmailsView
}

func (s *Server) handleSetMyEmailPrimary(ctx context.Context, in *setMyEmailPrimaryIn) (*setMyEmailPrimaryOut, error) {
	sess := authn.SessionFromContext(ctx)
	if sess == nil {
		return nil, authErrToHuma(authn.ErrNoSession())
	}
	tx, err := s.beginMeEmailTx(ctx)
	if err != nil {
		return nil, fmt.Errorf("handleSetMyEmailPrimary: begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if err := email.SetPrimary(ctx, tx.Queries(), sess.Account.ID, in.ID); err != nil {
		return nil, mapEmailRuleError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("handleSetMyEmailPrimary: commit: %w", err)
	}

	rows, err := s.meEmailsQ().ListAccountEmails(ctx, sess.Account.ID)
	if err != nil {
		return nil, fmt.Errorf("handleSetMyEmailPrimary: reload: %w", err)
	}
	var address string
	for _, row := range rows {
		if row.IsPrimary {
			address = row.Address
		}
	}

	acctID := sess.Account.ID
	audit.RecordOrLog(ctx, s.Audit, audit.Record{
		AccountID:     &acctID,
		Factor:        audit.FactorEmail,
		Event:         audit.EventPrimaryChanged,
		CredentialRef: &in.ID,
		Detail:        map[string]any{"address": address},
	})
	return &setMyEmailPrimaryOut{Body: *myEmailsOut(rows, s.mailSender.Available())}, nil
}

// ----- DELETE /me/emails/{id} ------------------------------------------------

type deleteMyEmailIn struct {
	ID int64 `path:"id"`
}

func (s *Server) handleDeleteMyEmail(ctx context.Context, in *deleteMyEmailIn) (*struct{}, error) {
	sess := authn.SessionFromContext(ctx)
	if sess == nil {
		return nil, authErrToHuma(authn.ErrNoSession())
	}
	tx, err := s.beginMeEmailTx(ctx)
	if err != nil {
		return nil, fmt.Errorf("handleDeleteMyEmail: begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	address, err := s.meEmailsQ().GetAccountEmail(ctx, db.GetAccountEmailParams{ID: in.ID, AccountID: sess.Account.ID})
	if err == nil {
		if _, err := email.Remove(ctx, tx.Queries(), sess.Account.ID, in.ID, false); err != nil {
			return nil, mapEmailRuleError(err)
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("handleDeleteMyEmail: commit: %w", err)
		}
		acctID := sess.Account.ID
		audit.RecordOrLog(ctx, s.Audit, audit.Record{
			AccountID:     &acctID,
			Factor:        audit.FactorEmail,
			Event:         audit.EventRevoke,
			CredentialRef: &in.ID,
			Detail:        map[string]any{"address": address.Address},
		})
		return &struct{}{}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("handleDeleteMyEmail: load: %w", err)
	}
	// The row may exist on another account; the only safe answer is 404.
	return nil, authErrToHuma(authn.ErrEmailNotFound())
}

func lower(s string) string { return strings.ToLower(s) }
