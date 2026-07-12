// Package server — handle_admin_audit_test.go
//
// Unit tests for the audit-events viewer (Task 7). These tests are intentionally
// DB-free: the input→query-param mapping (clampLimit, filter construction) and
// the row→AuditEventView projection (auditEventView) are the primary units under
// test. No HTTP stack, no database.
//
// Redaction invariant (honest scope):
// The audit-events VIEWER does not redact detail — it passes the stored JSON
// through unchanged. Redaction is a WRITE-SITE invariant: the mutation handlers
// (Tasks 3-6) that call audit.Writer.Record are responsible for never placing
// private key material, client secrets, raw tokens, auth codes, or SAML assertions
// in Detail. The credential_event table has no column for such secrets — the
// schema-level invariant ensures this viewer can never expose material it was
// never given. Tests in this file assert the viewer passes detail through
// unchanged (neither adds nor drops keys) and that AuditEventView declares
// exactly the intended fields (none of which correspond to any secret-carrying DB
// column).

package server

import (
	"net/netip"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/pagination"
)

// ---- limit clamping tests --------------------------------------------------
// The audit handler now uses the shared pagination.Limit (1-100) instead of
// the legacy clampLimit (1-200). These tests verify the shared clamp behavior.

func TestAdminAuditEvents_ClampLimit_Default(t *testing.T) {
	t.Parallel()
	if got := pagination.Limit(0); got != 50 {
		t.Errorf("Limit(0): got %d, want 50 (default)", got)
	}
}

func TestAdminAuditEvents_ClampLimit_Negative(t *testing.T) {
	t.Parallel()
	if got := pagination.Limit(-1); got != 50 {
		t.Errorf("Limit(-1): got %d, want 50 (default)", got)
	}
}

func TestAdminAuditEvents_ClampLimit_Cap(t *testing.T) {
	t.Parallel()
	if got := pagination.Limit(500); got != 100 {
		t.Errorf("Limit(500): got %d, want 100 (cap)", got)
	}
}

func TestAdminAuditEvents_ClampLimit_ExactMax(t *testing.T) {
	t.Parallel()
	if got := pagination.Limit(100); got != 100 {
		t.Errorf("Limit(100): got %d, want 100", got)
	}
}

func TestAdminAuditEvents_ClampLimit_MidRange(t *testing.T) {
	t.Parallel()
	if got := pagination.Limit(75); got != 75 {
		t.Errorf("Limit(75): got %d, want 75", got)
	}
}

// ---- auditEventView projection tests ----------------------------------------

func TestAdminAuditEvents_Projection_BasicFields(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	accountID := int32(42)
	row := db.CredentialEvent{
		ID:        99,
		AccountID: &accountID,
		Factor:    "webauthn",
		Event:     "register",
		At:        pgtype.Timestamptz{Time: at, Valid: true},
		UserAgent: pgtype.Text{String: "Mozilla/5.0", Valid: true},
		Detail:    []byte(`{"kid":"abc123"}`),
	}

	v := auditEventView(row)

	if v.ID != 99 {
		t.Errorf("ID: got %d, want 99", v.ID)
	}
	if !v.At.Equal(at) {
		t.Errorf("At: got %v, want %v", v.At, at)
	}
	if v.AccountID == nil || *v.AccountID != 42 {
		t.Errorf("AccountID: got %v, want &42", v.AccountID)
	}
	if v.Factor != "webauthn" {
		t.Errorf("Factor: got %q, want %q", v.Factor, "webauthn")
	}
	if v.Event != "register" {
		t.Errorf("Event: got %q, want %q", v.Event, "register")
	}
	if v.UserAgent != "Mozilla/5.0" {
		t.Errorf("UserAgent: got %q, want %q", v.UserAgent, "Mozilla/5.0")
	}
	if v.Detail == nil {
		t.Fatal("Detail: got nil, want map")
	}
	if v.Detail["kid"] != "abc123" {
		t.Errorf("Detail[kid]: got %v, want %q", v.Detail["kid"], "abc123")
	}
}

func TestAdminAuditEvents_Projection_NilAccountID(t *testing.T) {
	t.Parallel()

	row := db.CredentialEvent{
		ID:        1,
		AccountID: nil, // system event, no account
		Factor:    "system",
		Event:     "startup",
		At:        pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}

	v := auditEventView(row)

	if v.AccountID != nil {
		t.Errorf("AccountID: got %v, want nil for nil input", v.AccountID)
	}
}

func TestAdminAuditEvents_Projection_IPAddress(t *testing.T) {
	t.Parallel()

	addr := netip.MustParseAddr("192.168.1.100")
	row := db.CredentialEvent{
		ID:     5,
		Factor: "webauthn",
		Event:  "verify",
		Ip:     &addr,
		At:     pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}

	v := auditEventView(row)

	if v.IP != "192.168.1.100" {
		t.Errorf("IP: got %q, want %q", v.IP, "192.168.1.100")
	}
}

func TestAdminAuditEvents_Projection_NilIP(t *testing.T) {
	t.Parallel()

	row := db.CredentialEvent{
		ID:     6,
		Factor: "webauthn",
		Event:  "verify",
		Ip:     nil,
		At:     pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}

	v := auditEventView(row)

	if v.IP != "" {
		t.Errorf("IP: got %q, want empty string for nil IP", v.IP)
	}
}

func TestAdminAuditEvents_Projection_EmptyUserAgent(t *testing.T) {
	t.Parallel()

	row := db.CredentialEvent{
		ID:        7,
		Factor:    "signing_key",
		Event:     "register",
		UserAgent: pgtype.Text{Valid: false},
		At:        pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}

	v := auditEventView(row)

	if v.UserAgent != "" {
		t.Errorf("UserAgent: got %q, want empty string for invalid pgtype.Text", v.UserAgent)
	}
}

func TestAdminAuditEvents_Projection_EmptyDetail(t *testing.T) {
	t.Parallel()

	row := db.CredentialEvent{
		ID:     8,
		Factor: "webauthn",
		Event:  "verify",
		At:     pgtype.Timestamptz{Time: time.Now(), Valid: true},
		Detail: nil, // no detail stored
	}

	v := auditEventView(row)

	if v.Detail != nil {
		t.Errorf("Detail: got %v, want nil for nil input", v.Detail)
	}
}

// ---- TestAdminAuditEvents_ViewerPassesDetailThrough -------------------------

// TestAdminAuditEvents_ViewerPassesDetailThrough asserts that the projection
// neither adds nor drops detail keys. The viewer is a pass-through for detail;
// it does not add metadata, strip fields, or transform values.
//
// Write-site invariant: private keys, client secrets, tokens, auth codes, and
// raw SAML are NEVER placed in credential_event.detail by the emitting handlers
// (Tasks 3-6). This viewer is not the redaction point — it faithfully surfaces
// whatever the emitter stored. The test below confirms the viewer does not
// interfere with that contract.
func TestAdminAuditEvents_ViewerPassesDetailThrough(t *testing.T) {
	t.Parallel()

	// Arbitrary detail map — simulates what a Task 3-6 handler would emit.
	// Keys represent safe, non-secret identifiers only (audit IDs, action names).
	inputDetail := map[string]any{
		"kid":    "test-kid",
		"action": "activate",
		"status": "active",
	}

	detail := encodeAttributes(inputDetail)
	row := db.CredentialEvent{
		ID:     100,
		Factor: "signing_key",
		Event:  "update",
		At:     pgtype.Timestamptz{Time: time.Now(), Valid: true},
		Detail: detail,
	}

	v := auditEventView(row)

	if v.Detail == nil {
		t.Fatal("Detail: got nil, want map (viewer dropped the detail)")
	}

	// Key count must be identical — no keys added, no keys dropped.
	if len(v.Detail) != len(inputDetail) {
		t.Errorf("Detail key count: got %d, want %d — viewer changed detail shape",
			len(v.Detail), len(inputDetail))
	}

	// Each key must survive with the same string value.
	for k, want := range inputDetail {
		got, ok := v.Detail[k]
		if !ok {
			t.Errorf("Detail[%q]: key missing from projection output", k)
			continue
		}
		// JSON round-trip preserves string values as string.
		if got != want {
			t.Errorf("Detail[%q]: got %v (%T), want %v (%T)", k, got, got, want, want)
		}
	}
}

// ---- AuditEventView field-set assertion -------------------------------------

// TestAdminAuditEvents_ContractType_ExactFields verifies at compile time that
// contract.AuditEventView declares exactly the intended fields and no others.
// The credential_event table has no column that stores private key material,
// client secrets, tokens, auth codes, or SAML assertions — this assertion
// documents the expected schema of the view type.
//
// If a future developer adds a field to AuditEventView that corresponds to a
// secret-carrying column, this test must be updated to reflect the new field
// and accompanying security review must confirm the secret is not exposed.
func TestAdminAuditEvents_ContractType_ExactFields(t *testing.T) {
	t.Parallel()

	// Construct a value using all exported fields — the compiler will error if
	// any field name changes or is removed.
	_ = contract.AuditEventView{
		ID:        1,
		At:        time.Now(),
		AccountID: nil,
		Factor:    "webauthn",
		Event:     "register",
		IP:        "1.2.3.4",
		UserAgent: "test-ua",
		Detail:    map[string]any{"key": "val"},
	}

	// Use reflection to enumerate the actual exported fields and assert exactly
	// the seven expected fields are present and no more. Any additional field
	// that lands here needs a security review (is it from a secret column?).
	expectedFields := []string{"ID", "At", "AccountID", "Factor", "Event", "IP", "UserAgent", "Detail"}
	ty := reflect.TypeOf(contract.AuditEventView{})
	actualFields := make([]string, 0, ty.NumField())
	for i := 0; i < ty.NumField(); i++ {
		actualFields = append(actualFields, ty.Field(i).Name)
	}

	if len(actualFields) != len(expectedFields) {
		t.Errorf("AuditEventView field count: got %d fields %v, want %d fields %v — "+
			"any added field requires a security review (secret-column exposure check)",
			len(actualFields), actualFields, len(expectedFields), expectedFields)
	}

	expectedSet := make(map[string]bool, len(expectedFields))
	for _, f := range expectedFields {
		expectedSet[f] = true
	}
	for _, f := range actualFields {
		if !expectedSet[f] {
			t.Errorf("AuditEventView has unexpected field %q — "+
				"review: is this from a secret-carrying column?", f)
		}
	}
}
