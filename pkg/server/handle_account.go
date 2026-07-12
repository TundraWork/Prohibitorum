package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sirupsen/logrus"

	"prohibitorum/pkg/account"
	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/avatar"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/credential/enrollment"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/logx"
	"prohibitorum/pkg/pagination"
	sessstore "prohibitorum/pkg/session"
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

// textPtr converts a nullable db text column to a *string (nil when NULL) for
// the wire view — an absent email serializes as omitted rather than "".
func textPtr(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	v := t.String
	return &v
}

// auditActor returns the acting admin's account id (nil when no session), for
// the AccountID field of an admin-mutation audit row. account_id is nullable in
// credential_event, so a nil actor records cleanly rather than a bogus id.
func auditActor(sess *authn.Session) *int32 {
	if sess == nil {
		return nil
	}
	return &sess.Account.ID
}

// accountViewFromRow projects a ListAccountsRow into AccountView. The row
// carries the same columns as db.Account plus a pre-computed LastSignInAt.
func accountViewFromRow(r *db.ListAccountsRow, origin string) contract.AccountView {
	var lsi *time.Time
	if r.LastSignInAt.Valid {
		v := r.LastSignInAt.Time
		lsi = &v
	}
	v := contract.AccountView{
		ID:            r.ID,
		Username:      r.Username,
		DisplayName:   r.DisplayName,
		Email:         textPtr(r.Email),
		EmailVerified: r.EmailVerified,
		Role:          r.Role,
		Attributes:    decodeAttributes(r.Attributes),
		Disabled:      r.Disabled,
		CreatedAt:     r.CreatedAt.Time,
		UpdatedAt:     r.UpdatedAt.Time,
		LastSignInAt:  lsi,
	}
	if r.AvatarEtag.Valid {
		if u := avatar.PublicURL(r.OidcSubject.String(), r.AvatarEtag.String, origin); u != "" {
			v.AvatarURL = &u
		}
	}
	return v
}

// accountViewFromAccount projects a db.Account into AccountView with an
// optional lastSignInAt (nil on single-row fetches that don't carry the
// credential subquery).
func accountViewFromAccount(a *db.Account, lastSignInAt *time.Time, origin string) contract.AccountView {
	v := contract.AccountView{
		ID:            a.ID,
		Username:      a.Username,
		DisplayName:   a.DisplayName,
		Email:         textPtr(a.Email),
		EmailVerified: a.EmailVerified,
		Role:          a.Role,
		Attributes:    decodeAttributes(a.Attributes),
		Disabled:      a.Disabled,
		CreatedAt:     a.CreatedAt.Time,
		UpdatedAt:     a.UpdatedAt.Time,
		LastSignInAt:  lastSignInAt,
	}
	if u := avatar.AccountURL(*a, origin); u != "" {
		v.AvatarURL = &u
	}
	return v
}

// ----- GET /accounts ---------------------------------------------------------

type listAccountsIn struct {
	pageInput
}

type listAccountsOut struct {
	Body contract.Page[contract.AccountView]
}

func (s *Server) handleListAccounts(ctx context.Context, in *listAccountsIn) (*listAccountsOut, error) {
	lim := pagination.Limit(in.Limit)
	const collection = "accounts"
	const sort = "created_at"
	filters := map[string]string{}
	payload, err := s.decodeCursor(in.Cursor, collection, sort, filters)
	if err != nil {
		return nil, cursorInvalidErr(err)
	}
	params := db.ListAccountsParams{Limit: int32(lim + 1)}
	if len(payload.Keys) == 2 {
		if t, perr := time.Parse(time.RFC3339Nano, payload.Keys[0]); perr == nil {
			params.AfterCreatedAt = tsToPgType(t)
		}
		params.AfterID = pgtype.Int4{}
		var id int32
		if _, serr := fmt.Sscanf(payload.Keys[1], "%d", &id); serr == nil {
			params.AfterID = pgtype.Int4{Int32: id, Valid: true}
		}
	}
	rows, err := s.listQ().ListAccounts(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("handleListAccounts: query: %w", err)
	}
	more := hasMore(len(rows), lim)
	if more {
		rows = rows[:lim]
	}
	origin := s.config.PublicOrigins[0]
	views := make([]contract.AccountView, 0, len(rows))
	for i := range rows {
		views = append(views, accountViewFromRow(&rows[i], origin))
	}
	var nextCursor string
	if more && len(rows) > 0 {
		last := rows[len(rows)-1]
		nextCursor = s.encodeNextCursor(collection, sort, filters, []string{
			last.CreatedAt.Time.Format(time.RFC3339Nano),
			fmt.Sprintf("%d", last.ID),
		})
	}
	return &listAccountsOut{Body: buildPage(views, nextCursor)}, nil
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
	return &accountOut{Body: accountViewFromAccount(&a, nil, s.config.PublicOrigins[0])}, nil
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
		// Email is a pointer so "omitted" (nil → preserve the current value,
		// including a federation-verified address) is distinguishable from
		// "set to empty" (clear it). An admin-supplied email is unverified.
		Email *string `json:"email,omitempty"`
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

	// Email: preserve the current value (incl. a federation-verified address)
	// unless the admin explicitly supplies one; a manual set is unverified, and
	// an empty string clears it. (T3.2)
	email := current.Email
	emailVerified := current.EmailVerified
	if in.Body.Email != nil {
		v := strings.TrimSpace(*in.Body.Email)
		email = pgtype.Text{String: v, Valid: v != ""}
		emailVerified = false
	}

	updated, err := q.UpdateAccount(ctx, db.UpdateAccountParams{
		ID:            in.ID,
		DisplayName:   in.Body.DisplayName,
		Role:          in.Body.Role,
		Attributes:    attrs,
		Disabled:      in.Body.Disabled,
		Email:         email,
		EmailVerified: emailVerified,
	})
	if err != nil {
		return nil, fmt.Errorf("handleUpdateAccount: update: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("handleUpdateAccount: commit: %w", err)
	}

	sess := authn.SessionFromContext(ctx)
	changes := logrus.Fields{}
	if current.DisplayName != updated.DisplayName {
		changes["displayName"] = []string{current.DisplayName, updated.DisplayName}
	}
	if current.Role != updated.Role {
		changes["role"] = []string{current.Role, updated.Role}
	}
	if current.Disabled != updated.Disabled {
		changes["disabled"] = []bool{current.Disabled, updated.Disabled}
	}
	if current.Email.String != updated.Email.String || current.Email.Valid != updated.Email.Valid {
		changes["email"] = []*string{textPtr(current.Email), textPtr(updated.Email)}
	}
	if current.EmailVerified != updated.EmailVerified {
		changes["emailVerified"] = []bool{current.EmailVerified, updated.EmailVerified}
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
		detail := map[string]any{"target_id": updated.ID}
		for k, v := range changes {
			detail[k] = v
		}
		audit.RecordOrLog(ctx, s.Audit, audit.Record{
			AccountID: auditActor(sess),
			Factor:    audit.FactorAccount,
			Event:     audit.EventUpdate,
			Detail:    detail,
		})
	}

	// Best-effort: kick sessions when an account is freshly disabled so active
	// browsers are signed out before their next session refresh window.
	if disabling {
		_, _ = s.sessionStore.RevokeAllForAccount(ctx, in.ID)
	}

	return &accountOut{Body: accountViewFromAccount(&updated, nil, s.config.PublicOrigins[0])}, nil
}

// ----- POST /accounts/set-disabled (raw, sudo-gated) -------------------------

type setAccountDisabledBody struct {
	ID       int32 `json:"id"`
	Disabled bool  `json:"disabled"`
}

// handleSetAccountDisabledHTTP flips ONLY the disabled flag, independent of the
// identity-form PUT. Mirrors the dedicated set-disabled endpoints for OIDC
// clients and upstream IdPs. The auth layer already rejects disabled accounts,
// so this only flips the flag (no session-revocation logic here — UpdateAccount
// owns that). Preserves the safety invariant: an admin-role account cannot be
// disabled (demote to user first); re-enabling is always allowed.
func (s *Server) handleSetAccountDisabledHTTP(w http.ResponseWriter, r *http.Request) {
	var body setAccountDisabledBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	if body.ID <= 0 {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}

	ctx := r.Context()

	current, err := s.queries.GetAccountByID(ctx, body.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeAuthErr(w, authn.ErrAccountNotFound())
			return
		}
		writeAuthErr(w, fmt.Errorf("handleSetAccountDisabled: load: %w", err))
		return
	}

	// Admin accounts cannot be disabled — demote first. (Re-enabling is fine.)
	if body.Disabled && current.Role == "admin" {
		writeAuthErr(w, authn.ErrAdminCannotBeDisabled())
		return
	}

	updated, err := s.queries.SetAccountDisabled(ctx, db.SetAccountDisabledParams{
		ID:       body.ID,
		Disabled: body.Disabled,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeAuthErr(w, authn.ErrAccountNotFound())
			return
		}
		writeAuthErr(w, fmt.Errorf("handleSetAccountDisabled: update: %w", err))
		return
	}

	sess := authn.SessionFromContext(ctx)
	audit.RecordOrLog(ctx, s.Audit, audit.Record{
		AccountID: auditActor(sess),
		Factor:    audit.FactorAccount,
		Event:     audit.EventUpdate,
		Detail:    map[string]any{"account_id": body.ID, "disabled": body.Disabled},
	})

	// Best-effort: kick active sessions when an account is freshly disabled so
	// browsers are signed out immediately (parity with handleUpdateAccount; this
	// is now the primary disable path, so the "revokes sessions" promise lives here).
	if body.Disabled && !current.Disabled {
		_, _ = s.sessionStore.RevokeAllForAccount(ctx, body.ID)
	}

	writeJSON(w, accountViewFromAccount(&updated, nil, s.config.PublicOrigins[0]))
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
	audit.RecordOrLog(ctx, s.Audit, audit.Record{
		AccountID: auditActor(sess),
		Factor:    audit.FactorAccount,
		Event:     audit.EventRevoke,
		Detail:    map[string]any{"target_id": in.Body.ID, "action": "delete"},
	})

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
	audit.RecordOrLog(ctx, s.Audit, audit.Record{
		AccountID: auditActor(sess),
		Factor:    audit.FactorWebAuthn,
		Event:     audit.EventRevoke,
		Detail: map[string]any{
			"target_id":     in.Body.AccountID,
			"credential_id": in.Body.CredentialID,
		},
	})

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
	audit.RecordOrLog(ctx, s.Audit, audit.Record{
		AccountID: auditActor(sess),
		Factor:    audit.FactorWebAuthn,
		Event:     audit.EventRevoke,
		Detail: map[string]any{
			"target_id":     body.AccountID,
			"credential_id": body.CredentialID,
		},
	})

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
	audit.RecordOrLog(ctx, s.Audit, audit.Record{
		AccountID: auditActor(sess),
		Factor:    audit.FactorAccount,
		Event:     audit.EventRevoke,
		Detail:    map[string]any{"target_id": in.Body.ID, "action": "revoke_all_sessions", "revoked": revoked},
	})

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
	audit.RecordOrLog(ctx, s.Audit, audit.Record{
		AccountID: auditActor(sess),
		Factor:    audit.FactorEnrollment,
		Event:     audit.EventEnrollmentIssued,
		Detail:    map[string]any{"target_id": in.Body.ID, "intent": "reset"},
	})

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
	// A federated invite bound to a non-existent or disabled IdP slug is
	// permanently un-redeemable. Validate it at create (GetUpstreamIDPBySlug
	// filters WHERE NOT disabled, so this also rejects a disabled slug). T3.4.
	if slug := in.Body.ExpectedUpstreamIdpSlug; slug != nil && *slug != "" {
		if _, err := s.invitationQ().GetUpstreamIDPBySlug(ctx, *slug); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, authErrToHuma(authn.ErrUpstreamIDPNotFound())
			}
			return nil, fmt.Errorf("handleCreateInvitation: validate slug: %w", err)
		}
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
	audit.RecordOrLog(ctx, s.Audit, audit.Record{
		AccountID: auditActor(sess),
		Factor:    audit.FactorInvitation,
		Event:     audit.EventRegister,
		Detail:    map[string]any{"template_role": in.Body.Role},
	})

	url := s.config.PublicOrigins[0] + "/enroll/" + token
	return &invitationOut{
		Body: contract.InvitationResponse{
			URL:       url,
			ExpiresAt: expiresAt,
		},
	}, nil
}

// ----- GET /invitations ------------------------------------------------------

type listInvitationsIn struct {
	pageInput
}

type listInvitationsOut struct {
	Body contract.Page[contract.InvitationView]
}

func (s *Server) handleListInvitations(ctx context.Context, in *listInvitationsIn) (*listInvitationsOut, error) {
	lim := pagination.Limit(in.Limit)
	const collection = "invitations"
	const sort = "created_at"
	filters := map[string]string{}
	payload, err := s.decodeCursor(in.Cursor, collection, sort, filters)
	if err != nil {
		return nil, cursorInvalidErr(err)
	}
	params := db.ListPendingInvitationsParams{Limit: int32(lim + 1)}
	if len(payload.Keys) == 2 {
		if t, perr := time.Parse(time.RFC3339Nano, payload.Keys[0]); perr == nil {
			params.AfterCreatedAt = tsToPgType(t)
		}
		params.AfterToken = pgtype.Text{String: payload.Keys[1], Valid: payload.Keys[1] != ""}
	}
	rows, err := s.invitationQ().ListPendingInvitations(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("handleListInvitations: %w", err)
	}
	more := hasMore(len(rows), lim)
	if more {
		rows = rows[:lim]
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
	var nextCursor string
	if more && len(rows) > 0 {
		last := rows[len(rows)-1]
		nextCursor = s.encodeNextCursor(collection, sort, filters, []string{
			last.CreatedAt.Time.Format(time.RFC3339Nano),
			last.Token,
		})
	}
	return &listInvitationsOut{Body: buildPage(views, nextCursor)}, nil
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
	audit.RecordOrLog(ctx, s.Audit, audit.Record{
		AccountID: auditActor(sess),
		Factor:    audit.FactorInvitation,
		Event:     audit.EventRevoke,
		Detail:    map[string]any{"token4": tokenPrefix},
	})
	return &struct{}{}, nil
}

// ----- GET /accounts/{id}/credentials, sessions, groups ---------------------
// These nested collection handlers are paginated and live in
// handle_nested_pagination.go. The sessionRecordToItem helper below is shared
// between the paginated admin handler and handleListMySessions in handle_me.go.

// sessionRecordToItem maps a session record to the wire view with IsCurrent=false
// (admin viewing another account). handleListMySessions in handle_me.go keeps an
// inline copy because it derives IsCurrent from the caller's own session token.
func sessionRecordToItem(r sessstore.SessionRecord) contract.SessionListItem {
	return contract.SessionListItem{
		ID:         r.Data.SessionID,
		IsCurrent:  false,
		IssuedAt:   r.Data.IssuedAt,
		ExpiresAt:  r.Data.ExpiresAt,
		LastSeenIP: r.Data.LastSeenIP,
		UserAgent:  r.Data.UserAgent,
	}
}

// ----- POST /accounts/{id}/sessions/revoke (raw sudo) ------------------------

func (s *Server) handleRevokeAccountSessionHTTP(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	if idStr == "" {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	id64, err := strconv.ParseInt(idStr, 10, 32)
	if err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	accountID := int32(id64)

	var body struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.SessionID == "" {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}

	ctx := r.Context()

	if _, err := s.queries.GetAccountByID(ctx, accountID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeAuthErr(w, authn.ErrAccountNotFound())
			return
		}
		writeAuthErr(w, fmt.Errorf("handleRevokeAccountSession: load: %w", err))
		return
	}

	ok, err := s.sessionStore.RevokeBySessionID(ctx, accountID, body.SessionID)
	if err != nil {
		writeAuthErr(w, fmt.Errorf("handleRevokeAccountSession: %w", err))
		return
	}
	if !ok {
		writeAuthErr(w, authn.ErrSessionNotFound())
		return
	}

	sess := authn.SessionFromContext(ctx)
	actorID := int32(0)
	if sess != nil {
		actorID = sess.Account.ID
	}
	logx.WithContext(ctx).WithFields(logrus.Fields{
		"event":          "auth.session_revoked_admin",
		"actor_id":       actorID,
		"target_id":      accountID,
		"target_session": body.SessionID,
	}).Info("auth")
	audit.RecordOrLog(r.Context(), s.Audit, audit.Record{
		AccountID: auditActor(sess),
		Factor:    audit.FactorSession,
		Event:     audit.EventSessionEnd,
		Detail: map[string]any{
			"target_id":  accountID,
			"session_id": body.SessionID,
		},
	})

	w.WriteHeader(http.StatusNoContent)
}

// ----- shared output types ---------------------------------------------------

type accountOut struct {
	Body contract.AccountView
}
