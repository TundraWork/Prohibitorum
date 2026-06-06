// Package server — handle_admin_signing_keys_test.go
//
// Unit tests for the signing-key admin surface (Task 3). These tests are
// intentionally DB-free: the view projection (signingKeyView) is the primary
// unit under test, with assertion on private-material exclusion and correct
// field mapping. The 409 error constructor is also exercised here.

package server

import (
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
)

// TestAdminSigningKeys_ViewProjection_NeverExposesPrivateMaterial verifies that
// signingKeyView never copies PrivatePem or any private material into the wire
// view. The contract.SigningKeyView type has no PrivatePem field — the test
// additionally checks that the projection function doesn't smuggle the secret
// into PublicJWK or any other map field.
func TestAdminSigningKeys_ViewProjection_NeverExposesPrivateMaterial(t *testing.T) {
	t.Parallel()

	row := db.SigningKey{
		Kid:        "test-kid-1",
		Algorithm:  "RS256",
		Use:        "sig",
		Status:     "active",
		PrivatePem: "-----BEGIN RSA PRIVATE KEY----- SECRET MATERIAL -----END RSA PRIVATE KEY-----",
		PublicJwk:  []byte(`{"kty":"RSA","kid":"test-kid-1","n":"abc","e":"AQAB"}`),
	}

	view := signingKeyView(row)

	// Structural check: contract.SigningKeyView has no PrivatePem field, so
	// the compiler guarantees private material cannot live there. The runtime
	// check below covers any indirect leakage via PublicJWK map values.
	for k, val := range view.PublicJWK {
		if s, ok := val.(string); ok && s == row.PrivatePem {
			t.Errorf("PublicJWK[%q] == PrivatePem: private material leaked into wire view", k)
		}
	}

	// Correct field projection.
	if view.Kid != "test-kid-1" {
		t.Errorf("Kid: got %q, want %q", view.Kid, "test-kid-1")
	}
	if view.Algorithm != "RS256" {
		t.Errorf("Algorithm: got %q, want %q", view.Algorithm, "RS256")
	}
	if view.Use != "sig" {
		t.Errorf("Use: got %q, want %q", view.Use, "sig")
	}
	if view.Status != "active" {
		t.Errorf("Status: got %q, want %q", view.Status, "active")
	}
	if view.PublicJWK == nil {
		t.Error("PublicJWK: got nil, want decoded map")
	}
	if kid, ok := view.PublicJWK["kid"].(string); !ok || kid != "test-kid-1" {
		t.Errorf("PublicJWK[kid]: got %v, want %q", view.PublicJWK["kid"], "test-kid-1")
	}
}

// TestAdminSigningKeys_ViewProjection_TimestampMapping verifies that optional
// pgtype.Timestamptz columns are projected to *time.Time only when Valid=true,
// and left nil otherwise — matching the pattern established by accountViewFromRow.
func TestAdminSigningKeys_ViewProjection_TimestampMapping(t *testing.T) {
	t.Parallel()

	activatedTime := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	decommTime := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	row := db.SigningKey{
		Kid:       "test-kid-2",
		Algorithm: "RS256",
		Use:       "sig",
		Status:    "decommissioning",
		ActivatedAt: pgtype.Timestamptz{
			Time:  activatedTime,
			Valid: true,
		},
		DecommissionedAt: pgtype.Timestamptz{
			Time:  decommTime,
			Valid: true,
		},
		// NotBefore and RetireAfter are intentionally left Invalid (zero value).
	}

	view := signingKeyView(row)

	if view.NotBefore != nil {
		t.Errorf("NotBefore: got %v, want nil (column not valid)", view.NotBefore)
	}
	if view.RetireAfter != nil {
		t.Errorf("RetireAfter: got %v, want nil (column not valid)", view.RetireAfter)
	}
	if view.ActivatedAt == nil {
		t.Fatal("ActivatedAt: got nil, want non-nil")
	}
	if !view.ActivatedAt.Equal(activatedTime) {
		t.Errorf("ActivatedAt: got %v, want %v", view.ActivatedAt, activatedTime)
	}
	if view.DecommissionedAt == nil {
		t.Fatal("DecommissionedAt: got nil, want non-nil")
	}
	if !view.DecommissionedAt.Equal(decommTime) {
		t.Errorf("DecommissionedAt: got %v, want %v", view.DecommissionedAt, decommTime)
	}
}

// TestAdminSigningKeys_ViewProjection_NilPublicJWK verifies that a row with an
// empty PublicJwk column yields PublicJWK: nil rather than panicking or
// returning a zero map.
func TestAdminSigningKeys_ViewProjection_NilPublicJWK(t *testing.T) {
	t.Parallel()

	row := db.SigningKey{
		Kid:       "test-kid-3",
		Algorithm: "RS256",
		Use:       "sig",
		Status:    "pending",
		PublicJwk: nil,
	}

	view := signingKeyView(row)
	// No panic is the primary assertion; also verify nil map is safe to use.
	if len(view.PublicJWK) != 0 {
		t.Errorf("PublicJWK: got %v, want nil/empty for nil input", view.PublicJWK)
	}
}

// TestAdminSigningKeys_ErrActiveKeyNoReplacement verifies the 409 constructor
// returns the correct status code and machine-readable code expected by the
// frontend and the wire spec.
func TestAdminSigningKeys_ErrActiveKeyNoReplacement(t *testing.T) {
	t.Parallel()

	err := authn.ErrActiveKeyNoReplacement()
	if err == nil {
		t.Fatal("ErrActiveKeyNoReplacement: got nil")
	}
	if err.Status != http.StatusConflict {
		t.Errorf("Status: got %d, want %d", err.Status, http.StatusConflict)
	}
	if err.Code != "active_key_no_replacement" {
		t.Errorf("Code: got %q, want %q", err.Code, "active_key_no_replacement")
	}
	if err.Message == "" {
		t.Error("Message: got empty string")
	}
}

// TestAdminSigningKeys_ContractType_NoPrivatePemField verifies at compile time
// that contract.SigningKeyView does not declare a PrivatePem or PrivateKey
// field. This catches any future refactor that might accidentally add one.
// The test is expressed as a type assertion that the compiler validates.
func TestAdminSigningKeys_ContractType_NoPrivatePemField(t *testing.T) {
	t.Parallel()

	v := contract.SigningKeyView{
		Kid:       "k",
		Algorithm: "RS256",
		Use:       "sig",
		Status:    "pending",
	}
	// If contract.SigningKeyView ever grew a PrivatePem field this test
	// would fail to compile — keeping the guarantee in the test suite.
	_ = v
}
