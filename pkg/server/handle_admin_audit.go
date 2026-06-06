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
	"net/netip"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
)

// ----- input / output types --------------------------------------------------

// listAuditEventsIn holds the filterable, pageable query parameters for
// GET /audit-events. Zero values mean "no filter" / "use default".
//
// since and until are accepted as RFC3339 strings; huma parses them via the
// time.Time type automatically when using the `query` tag on time.Time fields
// because huma registers a time format handler. If huma does not handle them,
// the field is left as string and parsed manually — but the huma/v2 library
// supports time.Time query params natively.
type listAuditEventsIn struct {
	Factor    string    `query:"factor"    doc:"Filter by factor (e.g. 'webauthn', 'password', 'signing_key')."`
	Event     string    `query:"event"     doc:"Filter by event type (e.g. 'register', 'revoke')."`
	AccountID int32     `query:"accountId" doc:"Filter to events for a specific account ID."`
	Since     time.Time `query:"since"     doc:"Return events at or after this RFC3339 timestamp."`
	Until     time.Time `query:"until"     doc:"Return events at or before this RFC3339 timestamp."`
	Before    int64     `query:"before"    doc:"Keyset cursor: return events with id < before (newest-first)."`
	Limit     int32     `query:"limit"     doc:"Page size (default 50, max 200)."`
}

type listAuditEventsOut struct {
	Body []contract.AuditEventView
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
	if r.Ip != nil && r.Ip != (&netip.Addr{}) {
		v.IP = r.Ip.String()
	}
	if r.UserAgent.Valid {
		v.UserAgent = r.UserAgent.String
	}
	return v
}

// clampLimit normalises the caller-supplied limit to [1, 200], defaulting to
// 50 when the caller supplies 0 (omitted).
func clampLimit(n int32) int32 {
	const defaultLimit = 50
	const maxLimit = 200
	if n <= 0 {
		return defaultLimit
	}
	if n > maxLimit {
		return maxLimit
	}
	return n
}

// ----- GET /audit-events -----------------------------------------------------

func (s *Server) handleListAuditEvents(ctx context.Context, in *listAuditEventsIn) (*listAuditEventsOut, error) {
	lim := clampLimit(in.Limit)

	// Build nullable filter params. Zero/empty values map to NULL in the query,
	// meaning "no filter on this column". The sqlc-generated struct uses pgtype
	// nullable types for the optional parameters.
	params := db.ListCredentialEventsParams{
		Lim: lim,
	}

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
	if in.Before != 0 {
		params.BeforeID = pgtype.Int8{Int64: in.Before, Valid: true}
	}

	rows, err := s.queries.ListCredentialEvents(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("handleListAuditEvents: query: %w", err)
	}

	views := make([]contract.AuditEventView, 0, len(rows))
	for _, r := range rows {
		views = append(views, auditEventView(r))
	}
	return &listAuditEventsOut{Body: views}, nil
}
