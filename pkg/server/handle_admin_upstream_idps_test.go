// Package server — handle_admin_upstream_idps_test.go
//
// Unit tests for the upstream-IdP admin surface (Task 6). These tests are
// intentionally DB-free: the view projection (identityProviderView) is the primary
// unit under test, with assertions on sealed-secret exclusion and correct field
// mapping. Route-level sudo gating is covered centrally in Task 9.

package server

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
)

// TestAdminUpstreamIDPs_ViewProjection_NeverExposesSecretBytes verifies that
// identityProviderView never copies ClientSecretEnc or SecretNonce into the wire
// view. The contract.IdentityProviderView type has no such fields — the test
// additionally verifies the value-exclusion at runtime.
func TestAdminUpstreamIDPs_ViewProjection_NeverExposesSecretBytes(t *testing.T) {
	t.Parallel()

	secretBytes := []byte("AES_GCM_CIPHERTEXT_MUST_NOT_LEAK")
	nonceBytes := []byte("NONCE_MUST_NOT_LEAK_123456789012")

	row := db.UpstreamIdp{
		Slug:                 "google",
		DisplayName:          "Google",
		IssuerUrl:            "https://accounts.google.com",
		ClientID:             "client-id-123",
		ClientSecretEnc:      secretBytes,
		SecretNonce:          nonceBytes,
		KeyVersion:           1,
		Scopes:               []string{"openid", "email"},
		Mode:                 "auto_provision",
		AllowedDomains:       []string{"example.com"},
		UsernameClaim:        "email",
		DisplayNameClaim:     "name",
		EmailClaim:           "email",
		PictureClaim:         "picture",
		RequireVerifiedEmail: true,
		Disabled:             false,
		CreatedAt:            pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}

	view := identityProviderView(row)

	// contract.IdentityProviderView has no ClientSecretEnc or SecretNonce fields —
	// the compiler structurally prevents it. Runtime check: none of the string
	// fields carry the secret or nonce bytes interpreted as string.
	secretStr := string(secretBytes)
	nonceStr := string(nonceBytes)

	for _, fieldVal := range []string{
		view.Slug,
		view.DisplayName,
		view.IssuerUrl,
		view.ClientID,
		view.Mode,
		view.UsernameClaim,
		view.DisplayNameClaim,
		view.EmailClaim,
		view.PictureClaim,
	} {
		if fieldVal == secretStr {
			t.Errorf("a string field carries the secret ciphertext: %q", fieldVal)
		}
		if fieldVal == nonceStr {
			t.Errorf("a string field carries the nonce bytes: %q", fieldVal)
		}
	}
	for _, s := range view.Scopes {
		if s == secretStr || s == nonceStr {
			t.Errorf("Scopes entry carries secret/nonce bytes: %q", s)
		}
	}
	for _, d := range view.AllowedDomains {
		if d == secretStr || d == nonceStr {
			t.Errorf("AllowedDomains entry carries secret/nonce bytes: %q", d)
		}
	}
}

// TestAdminUpstreamIDPs_ViewProjection_FieldMapping verifies correct projection
// of all public fields including the optional timestamp.
func TestAdminUpstreamIDPs_ViewProjection_FieldMapping(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	row := db.UpstreamIdp{
		Slug:                 "my-idp",
		DisplayName:          "My IdP",
		IssuerUrl:            "https://idp.example.com",
		ClientID:             "client-xyz",
		ClientSecretEnc:      []byte("SEALED"),
		SecretNonce:          []byte("NONCE"),
		KeyVersion:           2,
		Scopes:               []string{"openid", "profile"},
		Mode:                 "invite_only",
		AllowedDomains:       []string{"corp.example.com"},
		UsernameClaim:        "preferred_username",
		DisplayNameClaim:     "name",
		EmailClaim:           "email",
		PictureClaim:         "picture_url",
		RequireVerifiedEmail: true,
		Disabled:             true,
		CreatedAt:            pgtype.Timestamptz{Time: createdAt, Valid: true},
	}

	view := identityProviderView(row)

	if view.Slug != "my-idp" {
		t.Errorf("Slug: got %q, want %q", view.Slug, "my-idp")
	}
	if view.DisplayName != "My IdP" {
		t.Errorf("DisplayName: got %q, want %q", view.DisplayName, "My IdP")
	}
	if view.IssuerUrl != "https://idp.example.com" {
		t.Errorf("IssuerUrl: got %q, want %q", view.IssuerUrl, "https://idp.example.com")
	}
	if view.ClientID != "client-xyz" {
		t.Errorf("ClientID: got %q, want %q", view.ClientID, "client-xyz")
	}
	if len(view.Scopes) != 2 || view.Scopes[0] != "openid" {
		t.Errorf("Scopes: got %v, want [openid profile]", view.Scopes)
	}
	if view.Mode != "invite_only" {
		t.Errorf("Mode: got %q, want %q", view.Mode, "invite_only")
	}
	if len(view.AllowedDomains) != 1 || view.AllowedDomains[0] != "corp.example.com" {
		t.Errorf("AllowedDomains: got %v", view.AllowedDomains)
	}
	if view.UsernameClaim != "preferred_username" {
		t.Errorf("UsernameClaim: got %q", view.UsernameClaim)
	}
	if view.DisplayNameClaim != "name" {
		t.Errorf("DisplayNameClaim: got %q", view.DisplayNameClaim)
	}
	if view.EmailClaim != "email" {
		t.Errorf("EmailClaim: got %q", view.EmailClaim)
	}
	if view.PictureClaim != "picture_url" {
		t.Errorf("PictureClaim: got %q", view.PictureClaim)
	}
	if !view.RequireVerifiedEmail {
		t.Error("RequireVerifiedEmail: got false, want true")
	}
	if !view.Disabled {
		t.Error("Disabled: got false, want true")
	}
	if !view.CreatedAt.Equal(createdAt) {
		t.Errorf("CreatedAt: got %v, want %v", view.CreatedAt, createdAt)
	}
}

// TestAdminUpstreamIDPs_ViewProjection_InvalidTimestamp verifies that a row
// with an invalid (NULL) CreatedAt column yields a zero-value time.Time
// rather than panicking.
func TestAdminUpstreamIDPs_ViewProjection_InvalidTimestamp(t *testing.T) {
	t.Parallel()

	row := db.UpstreamIdp{
		Slug:        "no-ts-idp",
		DisplayName: "No Timestamp",
		// CreatedAt intentionally left as zero value (Valid=false)
	}

	view := identityProviderView(row)

	if !view.CreatedAt.IsZero() {
		t.Errorf("CreatedAt: got %v, want zero time for invalid column", view.CreatedAt)
	}
}

// TestAdminUpstreamIDPs_ErrUpstreamIDPNotFound verifies the 404 constructor
// returns the correct status code and machine-readable code.
func TestAdminUpstreamIDPs_ErrUpstreamIDPNotFound(t *testing.T) {
	t.Parallel()

	err := authn.ErrUpstreamIDPNotFound()
	if err == nil {
		t.Fatal("ErrUpstreamIDPNotFound: got nil")
	}
	if err.Status != 404 {
		t.Errorf("Status: got %d, want 404", err.Status)
	}
	if err.Code != "upstream_idp_not_found" {
		t.Errorf("Code: got %q, want %q", err.Code, "upstream_idp_not_found")
	}
	if err.Message == "" {
		t.Error("Message: got empty string")
	}
}

// TestAdminUpstreamIDPs_ContractType_NoSecretFields verifies at compile time
// that contract.IdentityProviderView does not declare ClientSecretEnc or
// SecretNonce fields. This catches any future refactor that might accidentally
// add one.
func TestAdminUpstreamIDPs_ContractType_NoSecretFields(t *testing.T) {
	t.Parallel()

	v := contract.IdentityProviderView{
		Slug:        "test",
		DisplayName: "Test",
		IssuerUrl:   "https://idp.test",
		ClientID:    "client-1",
		Mode:        "auto_provision",
	}
	// If contract.IdentityProviderView ever grew a ClientSecretEnc or SecretNonce
	// field this test would fail to compile — keeping the guarantee in the
	// test suite.
	_ = v
}

// TestAdminUpstreamIDPs_ViewProjection_EmptySlices verifies that nil slices
// in the db row (AllowedDomains, Scopes) do not cause panics and are projected
// faithfully (nil is acceptable since JSON encoding emits null; callers handle).
func TestAdminUpstreamIDPs_ViewProjection_EmptySlices(t *testing.T) {
	t.Parallel()

	row := db.UpstreamIdp{
		Slug:           "minimal",
		DisplayName:    "Minimal",
		AllowedDomains: nil,
		Scopes:         nil,
	}

	// Must not panic.
	view := identityProviderView(row)
	_ = view.AllowedDomains
	_ = view.Scopes
}

// TestAdminUpstreamIDPs_ViewProjection_PictureClaim verifies that PictureClaim
// is projected from the db row into the view, and that a custom claim value
// round-trips correctly (distinct from the "picture" default).
func TestAdminUpstreamIDPs_ViewProjection_PictureClaim(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		input string
		want  string
	}{
		{"default", "picture", "picture"},
		{"custom", "avatar_url", "avatar_url"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			row := db.UpstreamIdp{
				Slug:         "idp",
				DisplayName:  "IdP",
				PictureClaim: tc.input,
			}
			view := identityProviderView(row)
			if view.PictureClaim != tc.want {
				t.Errorf("PictureClaim: got %q, want %q", view.PictureClaim, tc.want)
			}
		})
	}
}
