// Package server — pagination.go
//
// Shared helpers for top-level admin index handlers that convert bare-array
// list endpoints to contract.Page[T] keyset pagination. Each handler:
//  1. Clamps the caller-supplied limit via pagination.Limit.
//  2. Decodes the bound cursor (if any) through pagination.Codec, validating
//     the collection and filter set so a cursor from another endpoint or a
//     different filter set is rejected as pagination_cursor_invalid.
//  3. Passes limit+1 to the query; the extra row signals "more pages".
//  4. Projects at most `limit` rows, then encodes the next keyset tuple only
//     when the extra row exists.
package server

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/pagination"
)

type applicationListScope struct {
	includeAll bool
	accountID  int32
	filters    map[string]string
}

func applicationListScopeFromContext(ctx context.Context) (applicationListScope, error) {
	sess := authn.SessionFromContext(ctx)
	if sess == nil || sess.Account == nil {
		return applicationListScope{}, authn.ErrNoSession()
	}
	if sess.Account.Role == "admin" {
		return applicationListScope{includeAll: true, filters: map[string]string{"scope": "all"}}, nil
	}
	return applicationListScope{
		accountID: sess.Account.ID,
		filters: map[string]string{
			"scope":      "assigned",
			"account_id": strconv.FormatInt(int64(sess.Account.ID), 10),
		},
	}, nil
}

// topLevelQueries is the narrow query surface the top-level paginated list
// handlers need. Declared as an interface so tests can inject a fake without
// constructing a real *db.Queries. Production wiring falls back to s.queries.
type topLevelQueries interface {
	ListAccounts(ctx context.Context, p db.ListAccountsParams) ([]db.ListAccountsRow, error)
	GetUpstreamIDPBySlugAny(ctx context.Context, slug string) (db.UpstreamIdp, error)
	ListPendingInvitations(ctx context.Context, p db.ListPendingInvitationsParams) ([]db.Enrollment, error)
	ListNonForwardAuthOIDCClients(ctx context.Context, p db.ListNonForwardAuthOIDCClientsParams) ([]db.ListNonForwardAuthOIDCClientsRow, error)
	ListSAMLSPs(ctx context.Context, p db.ListSAMLSPsParams) ([]db.SamlSp, error)
	ListAllUpstreamIDPs(ctx context.Context, p db.ListAllUpstreamIDPsParams) ([]db.UpstreamIdp, error)
	ListAllSigningKeys(ctx context.Context, p db.ListAllSigningKeysParams) ([]db.SigningKey, error)
	ListForwardAuthClients(ctx context.Context, p db.ListForwardAuthClientsParams) ([]db.ListForwardAuthClientsRow, error)
	ListCredentialEvents(ctx context.Context, p db.ListCredentialEventsParams) ([]db.ListCredentialEventsRow, error)
	// ListEntityIconEtags backs the iconUrl each list row carries, so the list
	// handlers reach it through the same seam as their paging query.
	ListEntityIconEtags(ctx context.Context, ownerKind string) ([]db.ListEntityIconEtagsRow, error)
}

// listQ returns the override (for tests) or the real queries. The real
// *db.Queries satisfies topLevelQueries because sqlc emits all these methods.
func (s *Server) listQ() topLevelQueries {
	if s.topLevelQueriesOverride != nil {
		return s.topLevelQueriesOverride
	}
	return s.queries
}

// PageInput is the shared query-string shape every top-level admin list
// endpoint embeds in its huma input struct. Limit and Cursor are optional —
// limit defaults to 50 (via pagination.Limit) and an empty cursor means
// "first page".
//
// Exported deliberately: huma reflects over the input struct to build the
// operation's OpenAPI parameters, and it does not descend into an unexported
// embedded struct. While this type was lowercase the two fields never reached
// the schema, so generated clients could not send them and every list stayed
// on the first page of 50.
type PageInput struct {
	Limit  int    `query:"limit" doc:"Page size (default 50, max 100)."`
	Cursor string `query:"cursor" doc:"Opaque pagination cursor from a prior response."`
}

// decodeCursor opens the bound cursor for the given collection and filter
// set. An empty cursor string starts a new page (returns a zero CursorPayload
// with no keys). Any decode failure returns ErrCursorInvalid so the handler
// can map it to the pagination_cursor_invalid public error code.
func (s *Server) decodeCursor(cursor, collection, sort string, filters map[string]string) (pagination.CursorPayload, error) {
	if cursor == "" {
		return pagination.CursorPayload{}, nil
	}
	if s.cursorCodec == nil {
		return pagination.CursorPayload{}, pagination.ErrCursorInvalid
	}
	return s.cursorCodec.Decode(cursor, collection, sort, filters)
}

// encodeNextCursor seals the keyset tuple of the last returned row into a new
// cursor. Called only when the limit+1 probe row exists (hasMore=true). The
// keys slice is the tuple of the last *projected* row (not the probe row).
func (s *Server) encodeNextCursor(collection, sort string, filters map[string]string, keys []string) string {
	if s.cursorCodec == nil || len(keys) == 0 {
		return ""
	}
	now := time.Now()
	c, err := s.cursorCodec.Encode(pagination.CursorPayload{
		Collection: collection,
		Filters:    filters,
		Sort:       sort,
		Keys:       keys,
		IssuedAt:   now,
		ExpiresAt:  now.Add(24 * time.Hour),
	})
	if err != nil {
		return ""
	}
	return c
}

// hasMore reports whether the query returned a probe row beyond the page
// limit. The caller passes the total row count and the clamped limit.
func hasMore(rowCount, limit int) bool {
	return rowCount > limit
}

// buildPage constructs a contract.Page[T] from the projected views and the
// optional next cursor. If hasMore is false, nextCursor is "" (final page).
func buildPage[T any](items []T, nextCursor string) contract.Page[T] {
	return contract.Page[T]{
		Items:      items,
		NextCursor: nextCursor,
	}
}

// tsToPgType converts a time.Time to pgtype.Timestamptz for sqlc params.
func tsToPgType(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: !t.IsZero()}
}

// cursorInvalidErr is the convenience wrapper handlers return when a cursor
// fails validation. It lets huma map the error to the registered
// pagination_cursor_invalid public-error code.
func cursorInvalidErr(err error) error {
	if errors.Is(err, pagination.ErrCursorInvalid) {
		return contract.CursorInvalidError()
	}
	return err
}
