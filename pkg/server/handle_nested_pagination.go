// Package server — handle_nested_pagination.go
//
// Shared types and query interface for nested admin collection pagination.
// Each nested collection (account credentials/sessions/PATs/groups, group
// members, OIDC/SAML access groups/accounts) embeds pageInput and returns
// contract.Page[T] with cursors bound to both the collection name and the
// parent entity ID (accountId, groupId, clientId, or samlSpId).

package server

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/pagination"
	sessstore "prohibitorum/pkg/session"
)

// nestedQueries is the query surface the nested pagination handlers need.
// Production uses *db.Queries; tests inject a fake via
// Server.nestedQueriesOverride.
type nestedQueries interface {
	GetAccountByID(ctx context.Context, id int32) (db.Account, error)
	GetGroup(ctx context.Context, id int32) (db.UserGroup, error)
	GetOIDCClientAny(ctx context.Context, clientID string) (db.OidcClient, error)
	GetSAMLSPByID(ctx context.Context, id int64) (db.SamlSp, error)
	ListCredentialsByAccountPage(ctx context.Context, arg db.ListCredentialsByAccountPageParams) ([]db.WebauthnCredential, error)
	ListPATsByAccountPage(ctx context.Context, arg db.ListPATsByAccountPageParams) ([]db.PersonalAccessToken, error)
	ListGroupsForAccountPage(ctx context.Context, arg db.ListGroupsForAccountPageParams) ([]db.UserGroup, error)
	ListGroupMembersPage(ctx context.Context, arg db.ListGroupMembersPageParams) ([]db.ListGroupMembersPageRow, error)
	ListOIDCClientAccessGroupsPage(ctx context.Context, arg db.ListOIDCClientAccessGroupsPageParams) ([]db.ListOIDCClientAccessGroupsPageRow, error)
	ListOIDCClientAccessAccountsPage(ctx context.Context, arg db.ListOIDCClientAccessAccountsPageParams) ([]db.ListOIDCClientAccessAccountsPageRow, error)
	ListSAMLSPAccessGroupsPage(ctx context.Context, arg db.ListSAMLSPAccessGroupsPageParams) ([]db.ListSAMLSPAccessGroupsPageRow, error)
	ListSAMLSPAccessAccountsPage(ctx context.Context, arg db.ListSAMLSPAccessAccountsPageParams) ([]db.ListSAMLSPAccessAccountsPageRow, error)
	ListAccountIdentitiesByAccountPage(ctx context.Context, arg db.ListAccountIdentitiesByAccountPageParams) ([]db.ListAccountIdentitiesByAccountPageRow, error)
}

// nestedQ resolves the nested query surface: override (tests) or production.
func (s *Server) nestedQ() nestedQueries {
	if s.nestedQueriesOverride != nil {
		return s.nestedQueriesOverride
	}
	return s.queries
}

// listAccountPageIn is the shared input for nested account collections:
// /accounts/{id}/credentials, /accounts/{id}/sessions, /accounts/{id}/tokens,
// /accounts/{id}/groups.
type listAccountPageIn struct {
	ID int32 `path:"id"`
	pageInput
}

// listGroupMembersPageIn is the input for GET /groups/{id}/members.
type listGroupMembersPageIn struct {
	ID int32 `path:"id"`
	pageInput
}

// getOIDCClientAccessPageIn is the input for GET /oidc-applications/{clientId}/access.
type getOIDCClientAccessPageIn struct {
	ClientID string `path:"clientId"`
	pageInput
}

// getSAMLSPAccessPageIn is the input for GET /saml-applications/{id}/access.
type getSAMLSPAccessPageIn struct {
	ID int64 `path:"id"`
	pageInput
}

// ---------------------------------------------------------------------------
// Cursor key helpers — encode/decode the keyset tuple as []string for the
// pagination.CursorPayload.Keys field.
// ---------------------------------------------------------------------------

// encodeDescKey encodes a (timestamp, id) keyset position for DESC ordering
// (created_at DESC, id DESC). Returns [rfc3339Nano(timestamp), strconv(id)].
func encodeDescKey(t time.Time, id int32) []string {
	return []string{t.UTC().Format(time.RFC3339Nano), strconv.FormatInt(int64(id), 10)}
}

// decodeDescKey decodes a DESC keyset position from CursorPayload.Keys.
// Returns zero values when keys are absent (first page).
func decodeDescKey(keys []string) (pgtype.Timestamptz, int32) {
	if len(keys) < 2 {
		return pgtype.Timestamptz{}, 0
	}
	t, err := time.Parse(time.RFC3339Nano, keys[0])
	if err != nil {
		return pgtype.Timestamptz{}, 0
	}
	id, err := strconv.ParseInt(keys[1], 10, 32)
	if err != nil {
		return pgtype.Timestamptz{}, 0
	}
	return pgtype.Timestamptz{Time: t, Valid: true}, int32(id)
}

// encodeASCDescKey encodes an ASC keyset position (textColumn, intId).
func encodeASCTextIntKey(textCol string, id int32) []string {
	return []string{textCol, strconv.FormatInt(int64(id), 10)}
}

// decodeASCTextIntKey decodes an ASC keyset position (textColumn, intId).
func decodeASCTextIntKey(keys []string) (string, int32) {
	if len(keys) < 2 {
		return "", 0
	}
	id, err := strconv.ParseInt(keys[1], 10, 32)
	if err != nil {
		return "", 0
	}
	return keys[0], int32(id)
}

// encodeIdentityKeyset encodes a (linked_at, id) DESC keyset position for
// identity pagination. Returns [rfc3339Nano(linkedAt), strconv(id)].
func encodeIdentityKeyset(t pgtype.Timestamptz, id int64) []string {
	return []string{t.Time.UTC().Format(time.RFC3339Nano), strconv.FormatInt(id, 10)}
}

// decodeIdentityKeyset decodes a DESC (linked_at, id) keyset position.
func decodeIdentityKeyset(keys []string) (pgtype.Timestamptz, int64) {
	if len(keys) < 2 {
		return pgtype.Timestamptz{}, 0
	}
	t, err := time.Parse(time.RFC3339Nano, keys[0])
	if err != nil {
		return pgtype.Timestamptz{}, 0
	}
	id, err := strconv.ParseInt(keys[1], 10, 64)
	if err != nil {
		return pgtype.Timestamptz{}, 0
	}
	return pgtype.Timestamptz{Time: t, Valid: true}, id
}

// ---------------------------------------------------------------------------
// GET /accounts/{id}/credentials
// ---------------------------------------------------------------------------

type accountCredentialsPageOut struct {
	Body contract.Page[contract.CredentialView]
}

func (s *Server) handleListAccountCredentials(ctx context.Context, in *listAccountPageIn) (*accountCredentialsPageOut, error) {
	if _, err := s.nestedQ().GetAccountByID(ctx, in.ID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, authErrToHuma(authn.ErrAccountNotFound())
		}
		return nil, fmt.Errorf("handleListAccountCredentials: load account: %w", err)
	}
	lim := pagination.Limit(in.Limit)
	const collection = "account_credentials"
	const sortID = "created_at"
	filters := map[string]string{"accountId": strconv.FormatInt(int64(in.ID), 10)}
	payload, err := s.decodeCursor(in.Cursor, collection, sortID, filters)
	if err != nil {
		return nil, cursorInvalidErr(err)
	}
	afterCreatedAt, afterID := decodeDescKey(payload.Keys)
	rows, err := s.nestedQ().ListCredentialsByAccountPage(ctx, db.ListCredentialsByAccountPageParams{
		AccountID:      in.ID,
		AfterCreatedAt: afterCreatedAt,
		AfterID:        afterID,
		RowLimit:       int32(lim + 1),
	})
	if err != nil {
		return nil, fmt.Errorf("handleListAccountCredentials: list: %w", err)
	}
	hasMore := len(rows) > lim
	if hasMore {
		rows = rows[:lim]
	}
	views := make([]contract.CredentialView, 0, len(rows))
	for i := range rows {
		views = append(views, credentialView(&rows[i]))
	}
	nextCursor := ""
	if hasMore && len(rows) > 0 {
		last := rows[len(rows)-1]
		nextCursor = s.encodeNextCursor(collection, sortID, filters, encodeDescKey(last.CreatedAt.Time, last.ID))
	}
	return &accountCredentialsPageOut{Body: buildPage(views, nextCursor)}, nil
}

// ---------------------------------------------------------------------------
// GET /accounts/{id}/sessions (KV-backed)
// ---------------------------------------------------------------------------

type accountSessionsPageOut struct {
	Body contract.Page[contract.SessionListItem]
}

func (s *Server) handleListAccountSessions(ctx context.Context, in *listAccountPageIn) (*accountSessionsPageOut, error) {
	if _, err := s.nestedQ().GetAccountByID(ctx, in.ID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, authErrToHuma(authn.ErrAccountNotFound())
		}
		return nil, fmt.Errorf("handleListAccountSessions: load: %w", err)
	}
	lim := pagination.Limit(in.Limit)
	const collection = "account_sessions"
	const sortID = "issued_at"
	filters := map[string]string{"accountId": strconv.FormatInt(int64(in.ID), 10)}
	payload, err := s.decodeCursor(in.Cursor, collection, sortID, filters)
	if err != nil {
		return nil, cursorInvalidErr(err)
	}
	var after *sessstore.SessionPageCursor
	if len(payload.Keys) == 2 {
		if t, perr := time.Parse(time.RFC3339Nano, payload.Keys[0]); perr == nil {
			after = &sessstore.SessionPageCursor{IssuedAt: t, SessionID: payload.Keys[1]}
		}
	}
	records, hasMore, err := s.sessionStore.ListPageByAccount(ctx, in.ID, after, lim)
	if err != nil {
		return nil, fmt.Errorf("handleListAccountSessions: list: %w", err)
	}
	items := make([]contract.SessionListItem, 0, len(records))
	for _, r := range records {
		items = append(items, sessionRecordToItem(r))
	}
	nextCursor := ""
	if hasMore && len(records) > 0 {
		last := records[len(records)-1]
		nextCursor = s.encodeNextCursor(collection, sortID, filters, []string{
			last.Data.IssuedAt.Format(time.RFC3339Nano),
			last.Data.SessionID,
		})
	}
	return &accountSessionsPageOut{Body: buildPage(items, nextCursor)}, nil
}

// (sessionPageCursor is sessstore.SessionPageCursor; the handler decodes the
// pagination.CursorPayload.Keys into it at the call site.)

// ---------------------------------------------------------------------------
// GET /accounts/{id}/tokens
// ---------------------------------------------------------------------------

type accountTokensPageOut struct {
	Body contract.Page[contract.PersonalAccessTokenView]
}

func (s *Server) handleListAccountTokens(ctx context.Context, in *listAccountPageIn) (*accountTokensPageOut, error) {
	q := s.nestedQ()
	if _, err := q.GetAccountByID(ctx, in.ID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, authErrToHuma(authn.ErrAccountNotFound())
		}
		return nil, fmt.Errorf("handleListAccountTokens: load account: %w", err)
	}
	lim := pagination.Limit(in.Limit)
	const collection = "account_tokens"
	const sortID = "created_at"
	filters := map[string]string{"accountId": strconv.FormatInt(int64(in.ID), 10)}
	payload, err := s.decodeCursor(in.Cursor, collection, sortID, filters)
	if err != nil {
		return nil, cursorInvalidErr(err)
	}
	afterCreatedAt, afterID := decodeDescKey(payload.Keys)
	rows, err := q.ListPATsByAccountPage(ctx, db.ListPATsByAccountPageParams{
		AccountID:      in.ID,
		AfterCreatedAt: afterCreatedAt,
		AfterID:        afterID,
		RowLimit:       int32(lim + 1),
	})
	if err != nil {
		return nil, fmt.Errorf("handleListAccountTokens: %w", err)
	}
	hasMore := len(rows) > lim
	if hasMore {
		rows = rows[:lim]
	}
	views := make([]contract.PersonalAccessTokenView, 0, len(rows))
	for _, r := range rows {
		views = append(views, patView(r))
	}
	nextCursor := ""
	if hasMore && len(rows) > 0 {
		last := rows[len(rows)-1]
		nextCursor = s.encodeNextCursor(collection, sortID, filters, encodeDescKey(last.CreatedAt.Time, last.ID))
	}
	return &accountTokensPageOut{Body: buildPage(views, nextCursor)}, nil
}

// ---------------------------------------------------------------------------
// GET /accounts/{id}/groups
// ---------------------------------------------------------------------------

type accountGroupsPageOut struct {
	Body contract.Page[contract.GroupView]
}

func (s *Server) handleListAccountGroups(ctx context.Context, in *listAccountPageIn) (*accountGroupsPageOut, error) {
	if _, err := s.nestedQ().GetAccountByID(ctx, in.ID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, authErrToHuma(authn.ErrAccountNotFound())
		}
		return nil, fmt.Errorf("handleListAccountGroups: load: %w", err)
	}
	lim := pagination.Limit(in.Limit)
	const collection = "account_groups"
	const sortID = "display_name"
	filters := map[string]string{"accountId": strconv.FormatInt(int64(in.ID), 10)}
	payload, err := s.decodeCursor(in.Cursor, collection, sortID, filters)
	if err != nil {
		return nil, cursorInvalidErr(err)
	}
	afterDisplayName, afterGroupID := decodeASCTextIntKey(payload.Keys)
	rows, err := s.nestedQ().ListGroupsForAccountPage(ctx, db.ListGroupsForAccountPageParams{
		AccountID:       in.ID,
		AfterDisplayName: afterDisplayName,
		AfterGroupID:    afterGroupID,
		RowLimit:        int32(lim + 1),
	})
	if err != nil {
		return nil, fmt.Errorf("handleListAccountGroups: list: %w", err)
	}
	hasMore := len(rows) > lim
	if hasMore {
		rows = rows[:lim]
	}
	views := make([]contract.GroupView, 0, len(rows))
	for _, g := range rows {
		views = append(views, groupView(g, 0))
	}
	nextCursor := ""
	if hasMore && len(rows) > 0 {
		last := rows[len(rows)-1]
		nextCursor = s.encodeNextCursor(collection, sortID, filters, encodeASCTextIntKey(last.DisplayName, last.ID))
	}
	return &accountGroupsPageOut{Body: buildPage(views, nextCursor)}, nil
}

// ---------------------------------------------------------------------------
// GET /groups/{id}/members
// ---------------------------------------------------------------------------

type listGroupMembersPageOut struct {
	Body contract.Page[contract.GroupMemberView]
}

func (s *Server) handleListGroupMembers(ctx context.Context, in *listGroupMembersPageIn) (*listGroupMembersPageOut, error) {
	if _, err := s.nestedQ().GetGroup(ctx, in.ID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, authErrToHuma(authn.ErrGroupNotFound())
		}
		return nil, fmt.Errorf("handleListGroupMembers: existence check: %w", err)
	}
	lim := pagination.Limit(in.Limit)
	const collection = "group_members"
	const sortID = "username"
	filters := map[string]string{"groupId": strconv.FormatInt(int64(in.ID), 10)}
	payload, err := s.decodeCursor(in.Cursor, collection, sortID, filters)
	if err != nil {
		return nil, cursorInvalidErr(err)
	}
	afterUsername, afterAccountID := decodeASCTextIntKey(payload.Keys)
	rows, err := s.nestedQ().ListGroupMembersPage(ctx, db.ListGroupMembersPageParams{
		GroupID:        in.ID,
		AfterUsername:  afterUsername,
		AfterAccountID: afterAccountID,
		RowLimit:       int32(lim + 1),
	})
	if err != nil {
		return nil, fmt.Errorf("handleListGroupMembers: query: %w", err)
	}
	hasMore := len(rows) > lim
	if hasMore {
		rows = rows[:lim]
	}
	views := make([]contract.GroupMemberView, 0, len(rows))
	for _, r := range rows {
		views = append(views, contract.GroupMemberView{
			ID:          r.ID,
			Username:    r.Username,
			DisplayName: r.DisplayName,
		})
	}
	nextCursor := ""
	if hasMore && len(rows) > 0 {
		last := rows[len(rows)-1]
		nextCursor = s.encodeNextCursor(collection, sortID, filters, encodeASCTextIntKey(last.Username, last.ID))
	}
	return &listGroupMembersPageOut{Body: buildPage(views, nextCursor)}, nil
}

// ---------------------------------------------------------------------------
// GET /oidc-applications/{clientId}/access
// ---------------------------------------------------------------------------

type getOIDCClientAccessPageOut struct {
	Body contract.AppAccessView
}

func (s *Server) handleGetOIDCClientAccess(ctx context.Context, in *getOIDCClientAccessPageIn) (*getOIDCClientAccessPageOut, error) {
	c, err := s.nestedQ().GetOIDCClientAny(ctx, in.ClientID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, authErrToHuma(authn.ErrClientNotFound())
		}
		return nil, fmt.Errorf("handleGetOIDCClientAccess: get client: %w", err)
	}

	lim := pagination.Limit(in.Limit)
	parentFilter := map[string]string{"clientId": in.ClientID}

	// Groups page
	grpCollection := "oidc_access_groups"
	grpPayload, err := s.decodeCursor(in.Cursor, grpCollection, "display_name", parentFilter)
	if err != nil {
		return nil, cursorInvalidErr(err)
	}
	grpAfterDisplayName, grpAfterGroupID := decodeASCTextIntKey(grpPayload.Keys)
	groupRows, err := s.nestedQ().ListOIDCClientAccessGroupsPage(ctx, db.ListOIDCClientAccessGroupsPageParams{
		ClientID:         in.ClientID,
		AfterDisplayName: grpAfterDisplayName,
		AfterGroupID:     grpAfterGroupID,
		RowLimit:         int32(lim + 1),
	})
	if err != nil {
		return nil, fmt.Errorf("handleGetOIDCClientAccess: list groups: %w", err)
	}
	grpHasMore := len(groupRows) > lim
	if grpHasMore {
		groupRows = groupRows[:lim]
	}
	groups := make([]contract.GroupRef, 0, len(groupRows))
	for _, r := range groupRows {
		groups = append(groups, contract.GroupRef{ID: r.ID, Slug: r.Slug, DisplayName: r.DisplayName})
	}
	grpNextCursor := ""
	if grpHasMore && len(groupRows) > 0 {
		last := groupRows[len(groupRows)-1]
		grpNextCursor = s.encodeNextCursor(grpCollection, "display_name", parentFilter, encodeASCTextIntKey(last.DisplayName, last.ID))
	}

	// Accounts page
	accCollection := "oidc_access_accounts"
	accPayload, err := s.decodeCursor(in.Cursor, accCollection, "username", parentFilter)
	if err != nil {
		return nil, cursorInvalidErr(err)
	}
	accAfterUsername, accAfterAccountID := decodeASCTextIntKey(accPayload.Keys)
	accountRows, err := s.nestedQ().ListOIDCClientAccessAccountsPage(ctx, db.ListOIDCClientAccessAccountsPageParams{
		ClientID:       in.ClientID,
		AfterUsername:  accAfterUsername,
		AfterAccountID: accAfterAccountID,
		RowLimit:       int32(lim + 1),
	})
	if err != nil {
		return nil, fmt.Errorf("handleGetOIDCClientAccess: list accounts: %w", err)
	}
	accHasMore := len(accountRows) > lim
	if accHasMore {
		accountRows = accountRows[:lim]
	}
	accounts := make([]contract.AccountRef, 0, len(accountRows))
	for _, r := range accountRows {
		accounts = append(accounts, contract.AccountRef{ID: r.ID, Username: r.Username, DisplayName: r.DisplayName})
	}
	accNextCursor := ""
	if accHasMore && len(accountRows) > 0 {
		last := accountRows[len(accountRows)-1]
		accNextCursor = s.encodeNextCursor(accCollection, "username", parentFilter, encodeASCTextIntKey(last.Username, last.ID))
	}

	return &getOIDCClientAccessPageOut{Body: contract.AppAccessView{
		AccessRestricted: c.AccessRestricted,
		Groups:           buildPage(groups, grpNextCursor),
		Accounts:         buildPage(accounts, accNextCursor),
	}}, nil
}

// ---------------------------------------------------------------------------
// GET /saml-applications/{id}/access
// ---------------------------------------------------------------------------

type getSAMLSPAccessPageOut struct {
	Body contract.AppAccessView
}

func (s *Server) handleGetSAMLSPAccess(ctx context.Context, in *getSAMLSPAccessPageIn) (*getSAMLSPAccessPageOut, error) {
	sp, err := s.nestedQ().GetSAMLSPByID(ctx, in.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, authErrToHuma(samlSPNotFound())
		}
		return nil, fmt.Errorf("handleGetSAMLSPAccess: get sp: %w", err)
	}

	lim := pagination.Limit(in.Limit)
	parentFilter := map[string]string{"samlSpId": strconv.FormatInt(in.ID, 10)}

	// Groups page
	grpCollection := "saml_access_groups"
	grpPayload, err := s.decodeCursor(in.Cursor, grpCollection, "display_name", parentFilter)
	if err != nil {
		return nil, cursorInvalidErr(err)
	}
	grpAfterDisplayName, grpAfterGroupID := decodeASCTextIntKey(grpPayload.Keys)
	groupRows, err := s.nestedQ().ListSAMLSPAccessGroupsPage(ctx, db.ListSAMLSPAccessGroupsPageParams{
		SamlSpID:        in.ID,
		AfterDisplayName: grpAfterDisplayName,
		AfterGroupID:    grpAfterGroupID,
		RowLimit:        int32(lim + 1),
	})
	if err != nil {
		return nil, fmt.Errorf("handleGetSAMLSPAccess: list groups: %w", err)
	}
	grpHasMore := len(groupRows) > lim
	if grpHasMore {
		groupRows = groupRows[:lim]
	}
	groups := make([]contract.GroupRef, 0, len(groupRows))
	for _, r := range groupRows {
		groups = append(groups, contract.GroupRef{ID: r.ID, Slug: r.Slug, DisplayName: r.DisplayName})
	}
	grpNextCursor := ""
	if grpHasMore && len(groupRows) > 0 {
		last := groupRows[len(groupRows)-1]
		grpNextCursor = s.encodeNextCursor(grpCollection, "display_name", parentFilter, encodeASCTextIntKey(last.DisplayName, last.ID))
	}

	// Accounts page
	accCollection := "saml_access_accounts"
	accPayload, err := s.decodeCursor(in.Cursor, accCollection, "username", parentFilter)
	if err != nil {
		return nil, cursorInvalidErr(err)
	}
	accAfterUsername, accAfterAccountID := decodeASCTextIntKey(accPayload.Keys)
	accountRows, err := s.nestedQ().ListSAMLSPAccessAccountsPage(ctx, db.ListSAMLSPAccessAccountsPageParams{
		SamlSpID:       in.ID,
		AfterUsername:  accAfterUsername,
		AfterAccountID: accAfterAccountID,
		RowLimit:       int32(lim + 1),
	})
	if err != nil {
		return nil, fmt.Errorf("handleGetSAMLSPAccess: list accounts: %w", err)
	}
	accHasMore := len(accountRows) > lim
	if accHasMore {
		accountRows = accountRows[:lim]
	}
	accounts := make([]contract.AccountRef, 0, len(accountRows))
	for _, r := range accountRows {
		accounts = append(accounts, contract.AccountRef{ID: r.ID, Username: r.Username, DisplayName: r.DisplayName})
	}
	accNextCursor := ""
	if accHasMore && len(accountRows) > 0 {
		last := accountRows[len(accountRows)-1]
		accNextCursor = s.encodeNextCursor(accCollection, "username", parentFilter, encodeASCTextIntKey(last.Username, last.ID))
	}

	return &getSAMLSPAccessPageOut{Body: contract.AppAccessView{
		AccessRestricted: sp.AccessRestricted,
		Groups:           buildPage(groups, grpNextCursor),
		Accounts:         buildPage(accounts, accNextCursor),
	}}, nil
}
