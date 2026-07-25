// Package server — handle_nested_pagination.go
//
// Shared types and query interface for nested admin collection pagination.
// Each retained nested collection (account credentials, sessions, and PATs)
// embeds pageInput and returns contract.Page[T] with cursors bound to its
// parent account ID.

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
	ListCredentialsByAccountPage(ctx context.Context, arg db.ListCredentialsByAccountPageParams) ([]db.WebauthnCredential, error)
	ListPATsByAccountPage(ctx context.Context, arg db.ListPATsByAccountPageParams) ([]db.PersonalAccessToken, error)
}

// nestedQ resolves the nested query surface: override (tests) or production.
func (s *Server) nestedQ() nestedQueries {
	if s.nestedQueriesOverride != nil {
		return s.nestedQueriesOverride
	}
	return s.queries
}

// listAccountPageIn is the shared input for retained nested account
// collections: /accounts/{id}/credentials, /accounts/{id}/sessions, and
// /accounts/{id}/tokens.
type listAccountPageIn struct {
	ID int32 `path:"id"`
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

