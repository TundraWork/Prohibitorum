// Package diagnostic provides a curated, bounded store for request-diagnostic
// records. Each record ties a server-generated request ID to the stable error
// code, the operation that produced it, and a small set of registry-approved
// detail fields — nothing else.
//
// The store is the sole write/read path for the diagnostic_event table. It
// validates every field key against the weberr registry before insert, so raw
// cause text, DSNs, headers, tokens, and other secrets can never enter the
// table. Records expire after seven days; the exact-ID lookup query filters
// on expires_at > now() so expired rows are invisible (404) before the prune
// reaper deletes them.
//
// There is intentionally no list/enumeration method. The only access path is
// Lookup(ctx, requestID), called by the admin diagnostic handler.
package diagnostic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/db"
	"prohibitorum/pkg/logx"
	"prohibitorum/pkg/weberr"
)

// DefaultTTL is the seven-day diagnostic retention window. Records older than
// this are invisible to lookups and deleted by the prune reaper.
const DefaultTTL = 7 * 24 * time.Hour

// ErrNotFound is returned by Lookup when no non-expired record exists for
// the given request ID. The handler maps this to HTTP 404.
var ErrNotFound = errors.New("diagnostic: record not found")

// Record is the curated diagnostic record. Fields is a map of registry-
// approved detail keys (validated against the weberr Definition for Code
// before insert). Raw cause text, secrets, and unchecked error strings are
// never placed here — callers retain those only for in-process
// classification and logging.
type Record struct {
	RequestID  string         `json:"requestId"`
	Code       string         `json:"code"`
	Operation  string         `json:"operation"`
	Method     string         `json:"method"`
	Route      string         `json:"route"`
	AccountID  *int32         `json:"accountId,omitempty"`
	Retryable  bool           `json:"retryable"`
	Fields     map[string]any `json:"fields,omitempty"`
	OccurredAt time.Time      `json:"occurredAt"`
	ExpiresAt  time.Time      `json:"expiresAt"`
}

// StoreWriter is the write surface for diagnostic records.
type StoreWriter interface {
	Record(ctx context.Context, rec Record) error
}

// StoreReader is the read surface — exact-ID lookup only, no enumeration.
type StoreReader interface {
	Lookup(ctx context.Context, requestID string) (Record, error)
}

// StorePruner deletes expired records.
type StorePruner interface {
	PruneExpired(ctx context.Context) error
}

// StoreService is the combined read + prune surface the server uses. The
// concrete *Store satisfies this; tests can inject fakes that implement both
// methods. Write access (Record) is NOT included — the server only reads and
// prunes; write callers (Task 3) construct the store directly.
type StoreService interface {
	StoreReader
	PruneExpired(ctx context.Context) error
}

// Store is the concrete diagnostic store backed by a db.Querier. It
// satisfies StoreWriter, StoreReader, and StorePruner.
type Store struct {
	q   diagnosticQueries
	ttl time.Duration
}

// diagnosticQueries is the narrow db surface the store needs.
type diagnosticQueries interface {
	InsertDiagnosticEvent(ctx context.Context, arg db.InsertDiagnosticEventParams) error
	GetDiagnosticEvent(ctx context.Context, requestID string) (db.DiagnosticEvent, error)
	DeleteExpiredDiagnosticEvents(ctx context.Context) (int64, error)
}

// New returns a Store with the default seven-day TTL.
func New(q diagnosticQueries) *Store {
	return &Store{q: q, ttl: DefaultTTL}
}

// Record validates rec against the weberr registry and inserts it. The Code
// must be a registered definition; every key in Fields must be declared in
// that definition's DetailKeys. Undeclared keys (e.g. "rawCause", "dsn") are
// rejected so raw cause text and secrets can never enter the table.
//
// The structured log line emitted on insert carries only the registered code,
// the diagnostic category, and the request ID — never unchecked err.Error()
// text.
func (s *Store) Record(ctx context.Context, rec Record) error {
	if rec.RequestID == "" {
		return errors.New("diagnostic: empty request ID")
	}
	def, ok := weberr.DefinitionFor(rec.Code)
	if !ok {
		return fmt.Errorf("diagnostic: unregistered code %q", rec.Code)
	}
	for k := range rec.Fields {
		if _, allowed := def.DetailKeys[k]; !allowed {
			return fmt.Errorf("diagnostic: code %q does not declare field %q", rec.Code, k)
		}
	}

	fieldsJSON, err := json.Marshal(rec.Fields)
	if err != nil {
		return fmt.Errorf("diagnostic: marshal fields: %w", err)
	}
	if fieldsJSON == nil {
		fieldsJSON = []byte(`{}`)
	}

	now := time.Now()
	if rec.OccurredAt.IsZero() {
		rec.OccurredAt = now
	}
	expiresAt := rec.OccurredAt.Add(s.ttl)

	if err := s.q.InsertDiagnosticEvent(ctx, db.InsertDiagnosticEventParams{
		RequestID:  rec.RequestID,
		OccurredAt: pgTimestamptz(rec.OccurredAt),
		ExpiresAt:  pgTimestamptz(expiresAt),
		AccountID:  rec.AccountID,
		Method:     rec.Method,
		Route:      rec.Route,
		Operation:  rec.Operation,
		Code:       rec.Code,
		Retryable:  rec.Retryable,
		Fields:     fieldsJSON,
	}); err != nil {
		return fmt.Errorf("diagnostic: insert: %w", err)
	}

	// Structured log: only the registered code, diagnostic category, and
	// request ID — never raw cause text or unchecked error strings.
	logx.WithContext(ctx).WithFields(map[string]any{
		"code":      rec.Code,
		"category":  def.DiagnosticKind,
		"requestId": rec.RequestID,
		"operation": rec.Operation,
	}).Info("diagnostic record stored")
	return nil
}

// Lookup returns the non-expired diagnostic record for the exact request ID.
// If no row exists or the row has expired, it returns ErrNotFound.
func (s *Store) Lookup(ctx context.Context, requestID string) (Record, error) {
	row, err := s.q.GetDiagnosticEvent(ctx, requestID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Record{}, ErrNotFound
		}
		return Record{}, fmt.Errorf("diagnostic: lookup: %w", err)
	}
	return rowToRecord(row), nil
}

// PruneExpired deletes all diagnostic_event rows whose expires_at has passed.
// Called by the hourly reaper in server.Serve.
func (s *Store) PruneExpired(ctx context.Context) error {
	if _, err := s.q.DeleteExpiredDiagnosticEvents(ctx); err != nil {
		return fmt.Errorf("diagnostic: prune: %w", err)
	}
	return nil
}

func rowToRecord(row db.DiagnosticEvent) Record {
	var fields map[string]any
	if len(row.Fields) > 0 {
		_ = json.Unmarshal(row.Fields, &fields)
	}
	occurred := time.Time{}
	if row.OccurredAt.Valid {
		occurred = row.OccurredAt.Time
	}
	expires := time.Time{}
	if row.ExpiresAt.Valid {
		expires = row.ExpiresAt.Time
	}
	return Record{
		RequestID:  row.RequestID,
		Code:       row.Code,
		Operation:  row.Operation,
		Method:     row.Method,
		Route:      row.Route,
		AccountID:  row.AccountID,
		Retryable:  row.Retryable,
		Fields:     fields,
		OccurredAt: occurred,
		ExpiresAt:  expires,
	}
}

// pgTimestamptz wraps a time.Time as a pgtype.Timestamptz. A zero time
// produces an invalid (NULL) value.
func pgTimestamptz(t time.Time) pgtype.Timestamptz {
	if t.IsZero() {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: t, Valid: true}
}
