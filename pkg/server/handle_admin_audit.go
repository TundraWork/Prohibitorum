// Package server — handle_admin_audit.go
//
// Admin audit-events endpoint:
//   GET /audit-events — paginated, filterable view of credential_event rows
//                       (admin role, read-only, no sudo required).
//
// The viewer passes `detail` through unchanged from the stored row. Redaction is
// a write-site invariant: the mutation handlers (Tasks 3-6) that call
// audit.Writer.Record are responsible for never placing private key material,
// client secrets, tokens, auth codes, or raw SAML in Detail. The
// credential_event schema has no column for such secrets.

package server

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/pagination"
)

// ----- input / output types --------------------------------------------------

// listAuditEventsIn holds the filterable, pageable query parameters for
// GET /audit-events. Zero values mean "no filter" / "use default".
// Cursor is the opaque pagination cursor from a prior response (bound to the
// active filter set); Before is deprecated and replaced by Cursor.
type listAuditEventsIn struct {
	Factor    string    `query:"factor"    doc:"Filter by factor (e.g. 'webauthn', 'password', 'signing_key')."`
	Event     string    `query:"event"     doc:"Filter by event type (e.g. 'register', 'revoke')."`
	AccountID int32     `query:"accountId" doc:"Filter to events for a specific account ID."`
	Since     time.Time `query:"since"     doc:"Return events at or after this RFC3339 timestamp."`
	Until     time.Time `query:"until"     doc:"Return events at or before this RFC3339 timestamp."`
	pageInput
}

type listAuditEventsOut struct {
	Body contract.Page[contract.AuditEventView]
}

// ----- projection helper -----------------------------------------------------

// auditEventView projects a db.CredentialEvent row into the wire-safe view.
// IP is formatted to string (empty if nil); UserAgent is from pgtype.Text.
// Detail is decoded from JSON bytes into map[string]any — nil if empty.
// This function is the projection chokepoint; it adds and removes no keys.
func auditEventView(r db.CredentialEvent) contract.AuditEventView {
	v := contract.AuditEventView{
		ID:     r.ID,
		At:     r.At.Time,
		Factor: r.Factor,
		Event:  r.Event,
		Detail: decodeAttributes(r.Detail),
	}
	if r.AccountID != nil {
		aid := *r.AccountID
		v.AccountID = &aid
	}
	if r.Ip != nil && r.Ip.IsValid() {
		v.IP = r.Ip.String()
	}
	if r.UserAgent.Valid {
		v.UserAgent = r.UserAgent.String
	}
	return v
}

// ----- GET /audit-events -----------------------------------------------------

func (s *Server) handleListAuditEvents(ctx context.Context, in *listAuditEventsIn) (*listAuditEventsOut, error) {
	lim := pagination.Limit(in.Limit)
	const collection = "audit_events"
	const sort = "id"

	// Build the normalized filter map for cursor binding. Only non-zero
	// filters are included so an omitted filter and an explicitly-empty
	// filter produce the same cursor binding.
	filters := map[string]string{}
	if in.Factor != "" {
		filters["factor"] = in.Factor
	}
	if in.Event != "" {
		filters["event"] = in.Event
	}
	if in.AccountID != 0 {
		filters["accountId"] = fmt.Sprintf("%d", in.AccountID)
	}
	if !in.Since.IsZero() {
		filters["since"] = in.Since.Format(time.RFC3339Nano)
	}
	if !in.Until.IsZero() {
		filters["until"] = in.Until.Format(time.RFC3339Nano)
	}

	payload, err := s.decodeCursor(in.Cursor, collection, sort, filters)
	if err != nil {
		return nil, cursorInvalidErr(err)
	}

	params := db.ListCredentialEventsParams{Lim: int32(lim + 1)}
	if in.Factor != "" {
		params.Factor = pgtype.Text{String: in.Factor, Valid: true}
	}
	if in.Event != "" {
		params.Event = pgtype.Text{String: in.Event, Valid: true}
	}
	if in.AccountID != 0 {
		params.AccountID = pgtype.Int4{Int32: in.AccountID, Valid: true}
	}
	if !in.Since.IsZero() {
		params.Since = pgtype.Timestamptz{Time: in.Since, Valid: true}
	}
	if !in.Until.IsZero() {
		params.Until = pgtype.Timestamptz{Time: in.Until, Valid: true}
	}
	if len(payload.Keys) == 1 {
		var afterID int64
		if _, serr := fmt.Sscanf(payload.Keys[0], "%d", &afterID); serr == nil {
			params.AfterID = pgtype.Int8{Int64: afterID, Valid: true}
		}
	}

	rows, err := s.listQ().ListCredentialEvents(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("handleListAuditEvents: query: %w", err)
	}

	more := hasMore(len(rows), lim)
	if more {
		rows = rows[:lim]
	}
	views := make([]contract.AuditEventView, 0, len(rows))
	for _, r := range rows {
		views = append(views, auditEventView(r))
	}
	var nextCursor string
	if more && len(rows) > 0 {
		last := rows[len(rows)-1]
		nextCursor = s.encodeNextCursor(collection, sort, filters, []string{
			fmt.Sprintf("%d", last.ID),
		})
	}
	return &listAuditEventsOut{Body: buildPage(views, nextCursor)}, nil
}
