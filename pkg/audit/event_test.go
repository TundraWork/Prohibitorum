package audit

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/db"
)

type captureQ struct {
	db.Querier
	got    db.InsertCredentialEventParams
	called bool
	err    error
}

func (c *captureQ) InsertCredentialEvent(_ context.Context, p db.InsertCredentialEventParams) error {
	c.called = true
	c.got = p
	return c.err
}

func ptrInt32(v int32) *int32 { return &v }
func ptrInt64(v int64) *int64 { return &v }
func ptrAddr(s string) *netip.Addr {
	a := netip.MustParseAddr(s)
	return &a
}

func TestWriter_RecordMarshalsDetail(t *testing.T) {
	cap := &captureQ{}
	w := NewWriter(cap)

	credRef := int64(7)
	err := w.Record(context.Background(), Record{
		AccountID:     ptrInt32(42),
		Factor:        FactorPassword,
		Event:         EventUse,
		CredentialRef: &credRef,
		IP:            ptrAddr("203.0.113.7"),
		UserAgent:     "test/1",
		Detail:        map[string]any{"reason": "ok"},
	})
	if err != nil {
		t.Fatalf("Record returned error: %v", err)
	}
	if !cap.called {
		t.Fatal("InsertCredentialEvent was not called")
	}

	if cap.got.AccountID == nil || *cap.got.AccountID != 42 {
		t.Errorf("AccountID = %v, want *42", cap.got.AccountID)
	}
	if cap.got.Factor != string(FactorPassword) {
		t.Errorf("Factor = %q, want %q", cap.got.Factor, FactorPassword)
	}
	if cap.got.Event != EventUse {
		t.Errorf("Event = %q, want %q", cap.got.Event, EventUse)
	}
	if !cap.got.CredentialRef.Valid || cap.got.CredentialRef.Int64 != 7 {
		t.Errorf("CredentialRef = %+v, want {Int64: 7, Valid: true}", cap.got.CredentialRef)
	}
	if cap.got.Ip == nil || cap.got.Ip.String() != "203.0.113.7" {
		t.Errorf("Ip = %v, want 203.0.113.7", cap.got.Ip)
	}
	if !cap.got.UserAgent.Valid || cap.got.UserAgent.String != "test/1" {
		t.Errorf("UserAgent = %+v, want {String: test/1, Valid: true}", cap.got.UserAgent)
	}

	var decoded map[string]any
	if err := json.Unmarshal(cap.got.Detail, &decoded); err != nil {
		t.Fatalf("Detail not valid JSON: %v (raw: %q)", err, cap.got.Detail)
	}
	if decoded["reason"] != "ok" {
		t.Errorf("Detail.reason = %v, want ok", decoded["reason"])
	}
}

func TestWriter_RecordNilAccountWritesNull(t *testing.T) {
	cap := &captureQ{}
	w := NewWriter(cap)

	err := w.Record(context.Background(), Record{
		Factor: FactorPassword,
		Event:  EventFail,
	})
	if err != nil {
		t.Fatalf("Record returned error: %v", err)
	}
	if cap.got.AccountID != nil {
		t.Errorf("AccountID = %v, want nil (SQL NULL)", *cap.got.AccountID)
	}
}

func TestWriter_RecordNilCredentialRefWritesNull(t *testing.T) {
	cap := &captureQ{}
	w := NewWriter(cap)

	err := w.Record(context.Background(), Record{
		AccountID: ptrInt32(1),
		Factor:    FactorPassword,
		Event:     EventUse,
	})
	if err != nil {
		t.Fatalf("Record returned error: %v", err)
	}
	if cap.got.CredentialRef.Valid {
		t.Errorf("CredentialRef.Valid = true, want false (SQL NULL)")
	}
}

func TestWriter_RecordNilDetailWritesNull(t *testing.T) {
	cap := &captureQ{}
	w := NewWriter(cap)

	err := w.Record(context.Background(), Record{
		AccountID: ptrInt32(1),
		Factor:    FactorPassword,
		Event:     EventUse,
		Detail:    nil,
	})
	if err != nil {
		t.Fatalf("Record returned error: %v", err)
	}
	if cap.got.Detail != nil {
		t.Errorf("Detail = %q, want nil byte slice (got literal JSON null would be a bug)", cap.got.Detail)
	}
}

func TestWriter_RecordNilIPLeavesIPNil(t *testing.T) {
	cap := &captureQ{}
	w := NewWriter(cap)

	err := w.Record(context.Background(), Record{
		AccountID: ptrInt32(1),
		Factor:    FactorPassword,
		Event:     EventUse,
		IP:        nil,
	})
	if err != nil {
		t.Fatalf("Record returned error: %v", err)
	}
	if cap.got.Ip != nil {
		t.Errorf("Ip = %v, want nil (SQL NULL)", cap.got.Ip)
	}
}

func TestWriter_RecordEmptyUserAgentWritesNull(t *testing.T) {
	cap := &captureQ{}
	w := NewWriter(cap)

	err := w.Record(context.Background(), Record{
		AccountID: ptrInt32(1),
		Factor:    FactorPassword,
		Event:     EventUse,
		UserAgent: "",
	})
	if err != nil {
		t.Fatalf("Record returned error: %v", err)
	}
	if cap.got.UserAgent.Valid {
		t.Errorf("UserAgent = %+v, want {Valid: false} (SQL NULL)", cap.got.UserAgent)
	}
}

func TestWriter_RecordPropagatesError(t *testing.T) {
	sentinel := errors.New("simulated PG failure")
	cap := &captureQ{err: sentinel}
	w := NewWriter(cap)

	err := w.Record(context.Background(), Record{
		AccountID: ptrInt32(1),
		Factor:    FactorPassword,
		Event:     EventUse,
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("Record err = %v, want wrapping %v", err, sentinel)
	}
}

// Defensive: confirm pgtype.Int8 zero value is invalid, matching SQL NULL semantics.
func TestWriter_PgtypeInt8ZeroIsNull(t *testing.T) {
	var z pgtype.Int8
	if z.Valid {
		t.Fatal("pgtype.Int8 zero value unexpectedly Valid; test assumptions broken")
	}
}
