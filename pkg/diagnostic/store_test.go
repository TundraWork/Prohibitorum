package diagnostic

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/db"
)

// ensure authn package init registers its error codes with the weberr
// registry, so DefinitionFor("upstream_error") etc. succeed in tests.
var _ = authn.ErrBadRequest

// fakeQ embeds db.Querier so only the diagnostic methods are overridden.
type fakeQ struct {
	db.Querier
	inserted db.InsertDiagnosticEventParams
	expired  bool
	pruned   bool
	lookupID string
}

func (f *fakeQ) InsertDiagnosticEvent(_ context.Context, p db.InsertDiagnosticEventParams) error {
	f.inserted = p
	return nil
}

func (f *fakeQ) GetDiagnosticEvent(_ context.Context, rid string) (db.DiagnosticEvent, error) {
	f.lookupID = rid
	if f.expired {
		// Simulate an expired row being absent (query filters expires_at > now()).
		return db.DiagnosticEvent{}, pgx.ErrNoRows
	}
	return db.DiagnosticEvent{
		RequestID: rid,
		Code:      "oidc_exchange_failed",
		Operation: "oidc.exchange",
		Method:    "POST",
		Route:     "/oauth/token",
		Retryable: false,
		Fields:    []byte(`{"provider":"corp"}`),
		OccurredAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
		ExpiresAt:  pgtype.Timestamptz{Time: time.Now().Add(7 * 24 * time.Hour), Valid: true},
	}, nil
}

func (f *fakeQ) DeleteExpiredDiagnosticEvents(_ context.Context) (int64, error) {
	f.pruned = true
	return 0, nil
}


func TestStore_RecordAcceptsRegistryApprovedFields(t *testing.T) {
	fq := &fakeQ{}
	s := New(fq)
	rec := Record{
		RequestID: "rid-abc",
		Code:      "upstream_error",
		Operation: "oidc.exchange",
		Method:    "POST",
		Route:     "/oauth/token",
		AccountID: new(int32(5)),
		Fields:    map[string]any{"upstreamCode": "invalid_grant"},
	}
	if err := s.Record(context.Background(), rec); err != nil {
		t.Fatalf("Record rejected approved fields: %v", err)
	}
	if fq.inserted.RequestID != "rid-abc" {
		t.Fatalf("inserted request_id = %q", fq.inserted.RequestID)
	}
	if fq.inserted.Code != "upstream_error" {
		t.Fatalf("inserted code = %q", fq.inserted.Code)
	}
}

func TestStore_RecordRejectsUndeclaredField(t *testing.T) {
	fq := &fakeQ{}
	s := New(fq)
	rec := Record{
		RequestID: "rid",
		Code:      "upstream_error",
		Operation: "oidc.exchange",
		Method:    "POST",
		Route:     "/oauth/token",
		Fields:    map[string]any{"rawCause": "postgres://user:secret@db/private"},
	}
	if err := s.Record(context.Background(), rec); err == nil {
		t.Fatal("diagnostic store accepted undeclared rawCause field")
	}
}

func TestStore_RecordRejectsUnknownCode(t *testing.T) {
	fq := &fakeQ{}
	s := New(fq)
	rec := Record{
		RequestID: "rid",
		Code:      "totally_made_up_code",
		Operation: "x",
		Method:    "GET",
		Route:     "/",
	}
	if err := s.Record(context.Background(), rec); err == nil {
		t.Fatal("diagnostic store accepted an unregistered code")
	}
}

func TestStore_RecordRejectsEmptyRequestID(t *testing.T) {
	fq := &fakeQ{}
	s := New(fq)
	rec := Record{
		RequestID: "",
		Code:      "upstream_error",
		Operation: "oidc.exchange",
		Method:    "POST",
		Route:     "/oauth/token",
	}
	if err := s.Record(context.Background(), rec); err == nil {
		t.Fatal("diagnostic store accepted an empty request ID")
	}
}

func TestStore_RecordRejectsDSNSecretInUndeclaredField(t *testing.T) {
	fq := &fakeQ{}
	s := New(fq)
	// "dsn" is not a declared field for any registry code; the store must
	// reject it even when the value looks like a connection string.
	rec := Record{
		RequestID: "rid",
		Code:      "upstream_error",
		Operation: "oidc.exchange",
		Method:    "POST",
		Route:     "/oauth/token",
		Fields:    map[string]any{"dsn": "postgres://user:secret@db/private"},
	}
	if err := s.Record(context.Background(), rec); err == nil {
		t.Fatal("diagnostic store accepted undeclared dsn field carrying a secret")
	}
}

func TestStore_RecordInsertsCuratedFieldsOnly(t *testing.T) {
	fq := &fakeQ{}
	s := New(fq)
	rec := Record{
		RequestID: "rid-curated",
		Code:      "rate_limited",
		Operation: "oidc.exchange",
		Method:    "POST",
		Route:     "/oauth/token",
		Retryable: true,
		Fields:    map[string]any{"retryAfterSeconds": 30},
	}
	if err := s.Record(context.Background(), rec); err != nil {
		t.Fatalf("Record: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(fq.inserted.Fields, &got); err != nil {
		t.Fatalf("inserted Fields is not valid JSON: %v", err)
	}
	if _, ok := got["retryAfterSeconds"]; !ok {
		t.Fatalf("inserted Fields missing retryAfterSeconds: %v", got)
	}
	if _, ok := got["rawCause"]; ok {
		t.Fatal("inserted Fields must not contain rawCause")
	}
}

func TestStore_LookupReturnsExactNonExpiredRecord(t *testing.T) {
	fq := &fakeQ{}
	s := New(fq)
	got, err := s.Lookup(context.Background(), "rid-exact")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if fq.lookupID != "rid-exact" {
		t.Fatalf("lookup queried request_id = %q", fq.lookupID)
	}
	if got.RequestID != "rid-exact" {
		t.Fatalf("returned request_id = %q", got.RequestID)
	}
	if got.Code != "oidc_exchange_failed" {
		t.Fatalf("returned code = %q", got.Code)
	}
}

func TestStore_LookupReturnsNotFoundForExpiredRecord(t *testing.T) {
	fq := &fakeQ{expired: true}
	s := New(fq)
	_, err := s.Lookup(context.Background(), "rid-old")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Lookup expired: err = %v, want ErrNotFound", err)
	}
}

func TestStore_PruneExpiredDeletesOldRecords(t *testing.T) {
	fq := &fakeQ{}
	s := New(fq)
	if err := s.PruneExpired(context.Background()); err != nil {
		t.Fatalf("PruneExpired: %v", err)
	}
	if !fq.pruned {
		t.Fatal("PruneExpired did not call DeleteExpiredDiagnosticEvents")
	}
}
