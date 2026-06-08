package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sirupsen/logrus"

	"prohibitorum/pkg/account"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/credential/enrollment"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/logx"
)

// decodeAttributes converts a sqlc-generated jsonb []byte into map[string]any.
// sqlc with pgx-v5 returns jsonb columns as []byte; we unmarshal into a map.
// Returns nil if the input is empty or unparseable.
func decodeAttributes(raw []byte) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	return m
}

// encodeAttributes marshals a map[string]any into JSONB bytes for storage.
// Returns the JSON encoding of an empty object if the map is nil.
func encodeAttributes(attrs map[string]any) []byte {
	if attrs == nil {
		return []byte("{}")
	}
	b, err := json.Marshal(attrs)
	if err != nil {
		return []byte("{}")
	}
	return b
}

// accountViewFromRow projects a ListAccountsRow into AccountView. The row
// carries the same columns as db.Account plus a pre-computed LastSignInAt.
func accountViewFromRow(r *db.ListAccountsRow) contract.AccountView {
	var lsi *time.Time
	if r.LastSignInAt.Valid {
		v := r.LastSignInAt.Time
		lsi = &v
	}
	return contract.AccountView{
		ID:           r.ID,
		Username:     r.Username,
		DisplayName:  r.DisplayName,
		Role:         r.Role,
		Attributes:   decodeAttributes(r.Attributes),
		Disabled:     r.Disabled,
		CreatedAt:    r.CreatedAt.Time,
		UpdatedAt:    r.UpdatedAt.Time,
		LastSignInAt: lsi,
	}
}

// accountViewFromAccount projects a db.Account into AccountView with an
// optional lastSignInAt (nil on single-row fetches that don't carry the
// credential subquery).
func accountViewFromAccount(a *db.Account, lastSignInAt *time.Time) contract.AccountView {
	return contract.AccountView{
		ID:           a.ID,
		Username:     a.Username,
		DisplayName:  a.DisplayName,
		Role:         a.Role,
		Attributes:   decodeAttributes(a.Attributes),
		Disabled:     a.Disabled,
		CreatedAt:    a.CreatedAt.Time,
		UpdatedAt:    a.UpdatedAt.Time,
		LastSignInAt: lastSignInAt,
	}
}

// ----- GET /accounts ---------------------------------------------------------

type listAccountsOut struct {
	Body []contract.AccountView
}

func (s *Server) handleListAccounts(ctx context.Context, _ *struct{}) (*listAccountsOut, error) {
	rows, err := s.queries.ListAccounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("handleListAccounts: query: %w", err)
	}
	views := make([]contract.AccountView, 0, len(rows))
	for i := range rows {
		views = append(views, accountViewFromRow(&rows[i]))
	}
	return &listAccountsOut{Body: views}, nil
}

// ----- GET /accounts/{id} ----------------------------------------------------

type getAccountIn struct {
	ID int32 `path:"id"`
}

func (s *Server) handleGetAccount(ctx context.Context, in *getAccountIn) (*accountOut, error) {
	a, err := s.queries.GetAccountByID(ctx, in.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, authErrToHuma(authn.ErrAccountNotFound())
		}
		return nil, fmt.Errorf("handleGetAccount: query: %w", err)
	}
	return &accountOut{Body: accountViewFromAccount(&a, nil)}, nil
}

// ----- PUT /accounts/{id} ----------------------------------------------------

type updateAccountIn struct {
	ID   int32 `path:"id"`
	Body struct {
		// username is immutable; reject if the caller supplies any value.
		Username    string         `json:"username,omitempty"`
		DisplayName string         `json:"displayName"`
		Role        string         `json:"role"`
		Attributes  map[string]any `json:"attributes,omitempty"`
		Disabled    bool           `json:"disabled"`
	}
}

func (s *Server) handleUpdateAccount(ctx context.Context, in *updateAccountIn) (*accountOut, error) {
	if in.Body.Role != "user" && in.Body.Role != "admin" {
		return nil, authErrToHuma(authn.ErrInvalidRole())
	}
	if in.Body.Username != "" {
		return nil, authErrToHuma(authn.ErrUsernameImmutable())
	}
	if err := account.ValidateDisplayName(in.Body.DisplayName); err != nil {
		return nil, authErrToHuma(err)
	}

	// Admin accounts cannot be disabled — demote first. Keeps the active-admin
	// invariant clean (a "disabled admin" is a confusing state).
	if in.Body.Role == "admin" && in.Body.Disabled {
		return nil, authErrToHuma(authn.ErrAdminCannotBeDisabled())
	}

	tx, err := s.dbPool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("handleUpdateAccount: begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	q := s.queries.WithTx(tx)

	current, err := q.GetAccountByID(ctx, in.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, authErrToHuma(authn.ErrAccountNotFound())
		}
		return nil, fmt.Errorf("handleUpdateAccount: load: %w", err)
	}

	// If this account currently contributes to the active-admin count and the
	// update would remove that contribution, enforce the last-admin invariant.
	demoting := current.Role == "admin" && in.Body.Role != "admin"
	disabling := !current.Disabled && in.Body.Disabled
	if (demoting || disabling) && current.Role == "admin" && !current.Disabled {
		n, err := q.CountActiveAdminsForUpdate(ctx)
		if err != nil {
			return nil, fmt.Errorf("handleUpdateAccount: count admins: %w", err)
		}
		if n <= 1 {
			return nil, authErrToHuma(authn.ErrLastAdmin())
		}
	}

	attrs := encodeAttributes(in.Body.Attributes)

	updated, err := q.UpdateAccount(ctx, db.UpdateAccountParams{
		ID:          in.ID,
		DisplayName: in.Body.DisplayName,
		Role:        in.Body.Role,
		Attributes:  attrs,
		Disabled:    in.Body.Disabled,
	})
	if err != nil {
		return nil, fmt.Errorf("handleUpdateAccount: update: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("handleUpdateAccount: commit: %w", err)
	}

	sess := authn.SessionFromContext(ctx)
	changes := logrus.Fields{}
	if current.Role != updated.Role {
		changes["role"] = []string{current.Role, updated.Role}
	}
	if current.Disabled != updated.Disabled {
		changes["disabled"] = []bool{current.Disabled, updated.Disabled}
	}
	currentAttrs := decodeAttributes(current.Attributes)
	updatedAttrs := decodeAttributes(updated.Attributes)
	if !reflect.DeepEqual(currentAttrs, updatedAttrs) {
		changes["attributes"] = []any{currentAttrs, updatedAttrs}
	}
	if len(changes) > 0 {
		actorID := int32(0)
		if sess != nil {
			actorID = sess.Account.ID
		}
		logx.WithContext(ctx).WithFields(logrus.Fields{
			"event":     "auth.account_updated",
			"actor_id":  actorID,
			"target_id": updated.ID,
			"changes":   changes,
		}).Info("auth")
	}

	// Best-effort: kick sessions when an account is freshly disabled so active
	// browsers are signed out before their next session refresh window.
	if disabling {
		_, _ = s.sessionStore.RevokeAllForAccount(ctx, in.ID)
	}

	return &accountOut{Body: accountViewFromAccount(&updated, nil)}, nil
}

// ----- POST /accounts/delete -------------------------------------------------

type deleteAccountIn struct {
	Body struct {
		ID int32 `json:"id"`
	}
}

func (s *Server) handleDeleteAccount(ctx context.Context, in *deleteAccountIn) (*struct{}, error) {
	sess := authn.SessionFromContext(ctx)
	// Admins may not delete their own row — ask another admin to do it.
	if sess != nil && in.Body.ID == sess.Account.ID {
		return nil, authErrToHuma(authn.ErrCannotDeleteSelf())
	}

	tx, err := s.dbPool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("handleDeleteAccount: begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	q := s.queries.WithTx(tx)

	current, err := q.GetAccountByID(ctx, in.Body.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, authErrToHuma(authn.ErrAccountNotFound())
		}
		return nil, fmt.Errorf("handleDeleteAccount: load: %w", err)
	}

	// Deleting the only active admin would leave the system in an
	// unrecoverable state.
	if current.Role == "admin" && !current.Disabled {
		n, err := q.CountActiveAdminsForUpdate(ctx)
		if err != nil {
			return nil, fmt.Errorf("handleDeleteAccount: count admins: %w", err)
		}
		if n <= 1 {
			return nil, authErrToHuma(authn.ErrLastAdmin())
		}
	}

	if err := q.DeleteAccountByID(ctx, in.Body.ID); err != nil {
		return nil, fmt.Errorf("handleDeleteAccount: delete: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("handleDeleteAccount: commit: %w", err)
	}

	actorID := int32(0)
	if sess != nil {
		actorID = sess.Account.ID
	}
	logx.WithContext(ctx).WithFields(logrus.Fields{
		"event":     "auth.account_deleted",
		"actor_id":  actorID,
		"target_id": in.Body.ID,
	}).Info("auth")

	// Best-effort: active sessions for this account are now dangling; revoke
	// them so browsers are signed out immediately.
	_, _ = s.sessionStore.RevokeAllForAccount(ctx, in.Body.ID)

	return &struct{}{}, nil
}

// ----- POST /accounts/credentials/delete -------------------------------------

type deleteAccountCredentialIn struct {
	Body struct {
		AccountID    int32 `json:"accountId"`
		CredentialID int32 `json:"credentialId"`
	}
}

func (s *Server) handleDeleteAccountCredential(ctx context.Context, in *deleteAccountCredentialIn) (*struct{}, error) {
	// Verify the account exists.
	_, err := s.queries.GetAccountByID(ctx, in.Body.AccountID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, authErrToHuma(authn.ErrAccountNotFound())
		}
		return nil, fmt.Errorf("handleDeleteAccountCredential: load account: %w", err)
	}

	// Verify ownership: the credential must belong to the given account. The
	// delete query is already owner-scoped (WHERE id=$1 AND account_id=$2), but
	// it's :exec so a no-match is silent. Scan the list to surface 404 cleanly.
	creds, err := s.queries.ListCredentialsByAccount(ctx, in.Body.AccountID)
	if err != nil {
		return nil, fmt.Errorf("handleDeleteAccountCredential: list creds: %w", err)
	}
	found := false
	for _, c := range creds {
		if c.ID == in.Body.CredentialID {
			found = true
			break
		}
	}
	if !found {
		return nil, authErrToHuma(authn.ErrCredentialNotFound())
	}

	// The precheck above guarantees the row exists; rows-affected == 0 here
	// means a concurrent delete — treat as success (idempotent admin op).
	if _, err := s.queries.DeleteCredentialByID(ctx, db.DeleteCredentialByIDParams{
		ID:        in.Body.CredentialID,
		AccountID: in.Body.AccountID,
	}); err != nil {
		return nil, fmt.Errorf("handleDeleteAccountCredential: delete: %w", err)
	}

	sess := authn.SessionFromContext(ctx)
	actorID := int32(0)
	if sess != nil {
		actorID = sess.Account.ID
	}
	logx.WithContext(ctx).WithFields(logrus.Fields{
		"event":         "auth.credential_revoked_admin",
		"actor_id":      actorID,
		"target_id":     in.Body.AccountID,
		"credential_id": in.Body.CredentialID,
	}).Info("auth")

	return &struct{}{}, nil
}

// handleDeleteAccountCredentialHTTP is the raw http.HandlerFunc wrapper used by
// registerSudoOpHTTP. It mirrors handleDeleteAccountCredential but operates on
// the raw net/http layer (sudo gating is performed by the wrapper, not here).
func (s *Server) handleDeleteAccountCredentialHTTP(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AccountID    int32 `json:"accountId"`
		CredentialID int32 `json:"credentialId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	if body.AccountID == 0 || body.CredentialID == 0 {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}

	ctx := r.Context()

	// Verify the account exists.
	_, err := s.queries.GetAccountByID(ctx, body.AccountID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeAuthErr(w, authn.ErrAccountNotFound())
			return
		}
		writeAuthErr(w, fmt.Errorf("handleDeleteAccountCredential: load account: %w", err))
		return
	}

	// Verify ownership: the credential must belong to the given account.
	creds, err := s.queries.ListCredentialsByAccount(ctx, body.AccountID)
	if err != nil {
		writeAuthErr(w, fmt.Errorf("handleDeleteAccountCredential: list creds: %w", err))
		return
	}
	found := false
	for _, c := range creds {
		if c.ID == body.CredentialID {
			found = true
			break
		}
	}
	if !found {
		writeAuthErr(w, authn.ErrCredentialNotFound())
		return
	}

	if _, err := s.queries.DeleteCredentialByID(ctx, db.DeleteCredentialByIDParams{
		ID:        body.CredentialID,
		AccountID: body.AccountID,
	}); err != nil {
		writeAuthErr(w, fmt.Errorf("handleDeleteAccountCredential: delete: %w", err))
		return
	}

	sess := authn.SessionFromContext(ctx)
	actorID := int32(0)
	if sess != nil {
		actorID = sess.Account.ID
	}
	logx.WithContext(ctx).WithFields(logrus.Fields{
		"event":         "auth.credential_revoked_admin",
		"actor_id":      actorID,
		"target_id":     body.AccountID,
		"credential_id": body.CredentialID,
	}).Info("auth")

	w.WriteHeader(http.StatusNoContent)
}

// ----- POST /accounts/revoke-sessions ----------------------------------------

type revokeAccountSessionsIn struct {
	Body struct {
		ID int32 `json:"id"`
	}
}

type revokeAccountSessionsOut struct {
	Body struct {
		Revoked int `json:"revoked"`
	}
}

func (s *Server) handleRevokeAccountSessions(ctx context.Context, in *revokeAccountSessionsIn) (*revokeAccountSessionsOut, error) {
	// Ensure the account exists before attempting any revocation.
	_, err := s.queries.GetAccountByID(ctx, in.Body.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, authErrToHuma(authn.ErrAccountNotFound())
		}
		return nil, fmt.Errorf("handleRevokeAccountSessions: load: %w", err)
	}

	revoked, err := s.sessionStore.RevokeAllForAccount(ctx, in.Body.ID)
	if err != nil {
		return nil, fmt.Errorf("handleRevokeAccountSessions: revoke: %w", err)
	}

	sess := authn.SessionFromContext(ctx)
	actorID := int32(0)
	if sess != nil {
		actorID = sess.Account.ID
	}
	logx.WithContext(ctx).WithFields(logrus.Fields{
		"event":     "auth.sessions_revoked",
		"actor_id":  actorID,
		"target_id": in.Body.ID,
		"revoked":   revoked,
	}).Info("auth")

	out := &revokeAccountSessionsOut{}
	out.Body.Revoked = revoked
	return out, nil
}

// ----- POST /accounts/reissue-enrollment -------------------------------------

type reissueEnrollmentIn struct {
	Body struct {
		ID int32 `json:"id"`
	}
}

type enrollmentURLOut struct {
	Body contract.EnrollmentURLResponse
}

func (s *Server) handleReissueEnrollment(ctx context.Context, in *reissueEnrollmentIn) (*enrollmentURLOut, error) {
	// Ensure the account exists.
	_, err := s.queries.GetAccountByID(ctx, in.Body.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, authErrToHuma(authn.ErrAccountNotFound())
		}
		return nil, fmt.Errorf("handleReissueEnrollment: load: %w", err)
	}

	id := in.Body.ID
	token, expiresAt, err := enrollment.IssueEnrollment(ctx, s.queries, enrollment.IntentReset, &id, 0, nil)
	if err != nil {
		return nil, fmt.Errorf("handleReissueEnrollment: issue: %w", err)
	}

	sess := authn.SessionFromContext(ctx)
	actorID := int32(0)
	if sess != nil {
		actorID = sess.Account.ID
	}
	logx.WithContext(ctx).WithFields(logrus.Fields{
		"event":     "auth.enrollment_issued",
		"actor_id":  actorID,
		"target_id": in.Body.ID,
		"intent":    "reset",
	}).Info("auth")

	url := s.config.PublicOrigins[0] + "/enroll/" + token
	return &enrollmentURLOut{Body: contract.EnrollmentURLResponse{
		URL:       url,
		ExpiresAt: expiresAt,
	}}, nil
}

// ----- POST /invitations / GET /invitations ----------------------------------

// invitationQueries is the query surface the invitation handlers need.
// IssueEnrollment (called by handleCreateInvitation) takes db.Querier, so the
// full interface is required here. Tests inject a fake via
// Server.invitationOverride; production falls back to s.queries.
type invitationQueries = db.Querier

func (s *Server) invitationQ() db.Querier {
	if s.invitationOverride != nil {
		return s.invitationOverride
	}
	return s.queries
}

type createInvitationIn struct {
	Body struct {
		Role                    string         `json:"role"`
		Attributes              map[string]any `json:"attributes,omitempty"`
		ExpectedUpstreamIdpSlug *string        `json:"expectedUpstreamIdpSlug,omitempty"`
	}
}

type invitationOut struct {
	Body contract.InvitationResponse
}

func (s *Server) handleCreateInvitation(ctx context.Context, in *createInvitationIn) (*invitationOut, error) {
	if in.Body.Role != "user" && in.Body.Role != "admin" {
		return nil, authErrToHuma(authn.ErrInvalidRole())
	}
	tpl := &enrollment.EnrollmentTemplate{
		Role:                    in.Body.Role,
		Attributes:              in.Body.Attributes,
		ExpectedUpstreamIDPSlug: in.Body.ExpectedUpstreamIdpSlug,
	}
	token, expiresAt, err := enrollment.IssueEnrollment(ctx, s.invitationQ(), enrollment.IntentInvite, nil, 0, tpl)
	if err != nil {
		return nil, fmt.Errorf("handleCreateInvitation: issue enrollment: %w", err)
	}

	sess := authn.SessionFromContext(ctx)
	actorID := int32(0)
	if sess != nil {
		actorID = sess.Account.ID
	}
	logx.WithContext(ctx).WithFields(logrus.Fields{
		"event":         "auth.account_invited",
		"actor_id":      actorID,
		"template_role": in.Body.Role,
	}).Info("auth")

	url := s.config.PublicOrigins[0] + "/enroll/" + token
	return &invitationOut{
		Body: contract.InvitationResponse{
			URL:       url,
			ExpiresAt: expiresAt,
		},
	}, nil
}

// ----- GET /invitations ------------------------------------------------------

type listInvitationsOut struct {
	Body []contract.InvitationView
}

func (s *Server) handleListInvitations(ctx context.Context, _ *struct{}) (*listInvitationsOut, error) {
	rows, err := s.invitationQ().ListPendingInvitations(ctx)
	if err != nil {
		return nil, fmt.Errorf("handleListInvitations: %w", err)
	}
	views := make([]contract.InvitationView, 0, len(rows))
	for _, r := range rows {
		role := "user"
		if r.TemplateRole.Valid {
			role = r.TemplateRole.String
		}
		attrs := enrollment.DecodeTemplateAttributes(r.TemplateAttributes)
		view := contract.InvitationView{
			Token:      r.Token,
			URL:        s.config.PublicOrigins[0] + "/enroll/" + r.Token,
			Role:       role,
			Attributes: attrs,
			CreatedAt:  r.CreatedAt.Time,
			ExpiresAt:  r.ExpiresAt.Time,
		}
		if r.ExpectedUpstreamIdpSlug.Valid {
			slug := r.ExpectedUpstreamIdpSlug.String
			view.ExpectedUpstreamIdpSlug = &slug
		}
		views = append(views, view)
	}
	return &listInvitationsOut{Body: views}, nil
}

// ----- POST /invitations/revoke ----------------------------------------------

type revokeInvitationIn struct {
	Body struct {
		Token string `json:"token"`
	}
}

func (s *Server) handleRevokeInvitation(ctx context.Context, in *revokeInvitationIn) (*struct{}, error) {
	sess := authn.SessionFromContext(ctx)
	_, err := s.queries.RevokeInvitation(ctx, in.Body.Token)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, authErrToHuma(authn.ErrInvitationNotFound())
		}
		return nil, fmt.Errorf("handleRevokeInvitation: %w", err)
	}
	// Safe to log a 4-char prefix — uniquely identifies the row for audit
	// without exposing the full bearer token. (See P4.08 logging conventions.)
	tokenPrefix := in.Body.Token
	if len(tokenPrefix) > 4 {
		tokenPrefix = tokenPrefix[:4]
	}
	actorID := int32(0)
	if sess != nil {
		actorID = sess.Account.ID
	}
	logx.WithContext(ctx).WithFields(logrus.Fields{
		"event":    "auth.invitation_revoked",
		"actor_id": actorID,
		"token4":   tokenPrefix,
	}).Info("auth")
	return &struct{}{}, nil
}

// ----- GET /accounts/{id}/credentials ----------------------------------------

type accountCredentialsOut struct {
	Body []contract.CredentialView
}

func (s *Server) handleListAccountCredentials(ctx context.Context, in *getAccountIn) (*accountCredentialsOut, error) {
	if _, err := s.queries.GetAccountByID(ctx, in.ID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, authErrToHuma(authn.ErrAccountNotFound())
		}
		return nil, fmt.Errorf("handleListAccountCredentials: load account: %w", err)
	}
	rows, err := s.queries.ListCredentialsByAccount(ctx, in.ID)
	if err != nil {
		return nil, fmt.Errorf("handleListAccountCredentials: list: %w", err)
	}
	views := make([]contract.CredentialView, 0, len(rows))
	for i := range rows {
		views = append(views, credentialView(&rows[i]))
	}
	return &accountCredentialsOut{Body: views}, nil
}

// ----- shared output types ---------------------------------------------------

type accountOut struct {
	Body contract.AccountView
}
