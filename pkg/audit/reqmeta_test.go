package audit

import (
	"context"
	"net/netip"
	"testing"
)

// captureQ is defined in event_test.go and is available here (same package).

func TestRecordFillsFromCtx(t *testing.T) {
	cap := &captureQ{}
	w := NewWriter(cap)

	ctx := WithRequestMeta(context.Background(), "203.0.113.7", "curl/8")
	err := w.Record(ctx, Record{
		Factor: FactorWebAuthn,
		Event:  EventUse,
	})
	if err != nil {
		t.Fatalf("Record returned error: %v", err)
	}
	if cap.got.Ip == nil {
		t.Fatal("Ip = nil, want 203.0.113.7")
	}
	if cap.got.Ip.String() != "203.0.113.7" {
		t.Errorf("Ip = %v, want 203.0.113.7", cap.got.Ip)
	}
	if !cap.got.UserAgent.Valid || cap.got.UserAgent.String != "curl/8" {
		t.Errorf("UserAgent = %+v, want {String: curl/8, Valid: true}", cap.got.UserAgent)
	}
}

func TestRecordExplicitWins(t *testing.T) {
	cap := &captureQ{}
	w := NewWriter(cap)

	explicitIP := netip.MustParseAddr("198.51.100.9")
	// ctx carries different meta — explicit values on the Record must be preserved.
	ctx := WithRequestMeta(context.Background(), "10.0.0.1", "ctx-agent/1")
	err := w.Record(ctx, Record{
		Factor:    FactorWebAuthn,
		Event:     EventUse,
		IP:        &explicitIP,
		UserAgent: "explicit-ua",
	})
	if err != nil {
		t.Fatalf("Record returned error: %v", err)
	}
	if cap.got.Ip == nil || cap.got.Ip.String() != "198.51.100.9" {
		t.Errorf("Ip = %v, want 198.51.100.9 (explicit should win)", cap.got.Ip)
	}
	if !cap.got.UserAgent.Valid || cap.got.UserAgent.String != "explicit-ua" {
		t.Errorf("UserAgent = %+v, want {String: explicit-ua, Valid: true} (explicit should win)", cap.got.UserAgent)
	}
}

func TestRecordBackgroundNoMeta(t *testing.T) {
	cap := &captureQ{}
	w := NewWriter(cap)

	err := w.Record(context.Background(), Record{
		Factor: FactorWebAuthn,
		Event:  EventUse,
	})
	if err != nil {
		t.Fatalf("Record returned error: %v", err)
	}
	if cap.got.Ip != nil {
		t.Errorf("Ip = %v, want nil for background context with no meta", cap.got.Ip)
	}
	if cap.got.UserAgent.Valid {
		t.Errorf("UserAgent = %+v, want {Valid: false} for background context with no meta", cap.got.UserAgent)
	}
}
