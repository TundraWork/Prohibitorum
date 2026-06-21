package server

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestForwardAuthAppView_MapsAllFields(t *testing.T) {
	t.Parallel()
	now := time.Now()
	v := forwardAuthAppView("fa-client", "My App",
		pgtype.Text{String: "app.example.test", Valid: true},
		true, false,
		pgtype.Timestamptz{Time: now, Valid: true})
	if v.ClientID != "fa-client" || v.DisplayName != "My App" {
		t.Errorf("id/name mismatch: %+v", v)
	}
	if v.ForwardAuthHost != "app.example.test" {
		t.Errorf("host = %q", v.ForwardAuthHost)
	}
	if !v.AccessRestricted || v.Disabled {
		t.Errorf("flags mismatch: restricted=%v disabled=%v", v.AccessRestricted, v.Disabled)
	}
	if !v.CreatedAt.Equal(now) {
		t.Errorf("createdAt = %v, want %v", v.CreatedAt, now)
	}
}

func TestForwardAuthAppView_EmptyHostAndTime(t *testing.T) {
	t.Parallel()
	v := forwardAuthAppView("c", "n", pgtype.Text{}, false, true, pgtype.Timestamptz{})
	if v.ForwardAuthHost != "" {
		t.Errorf("invalid host should map to empty string, got %q", v.ForwardAuthHost)
	}
	if !v.CreatedAt.IsZero() {
		t.Errorf("invalid timestamptz should map to zero time, got %v", v.CreatedAt)
	}
}
