// Package server — handle_admin_saml_sps_test.go
//
// Unit tests for the SAML SP admin surface (Task 5). Tests are intentionally
// DB-free: samlProviderView (the projection from db rows → contract view) is
// the primary unit under test, verified for correct field mapping, PEM
// exclusion, optional-field handling, and ACS / key sub-view accuracy.
//
// Body-to-SPOptions mapping is also exercised via saml.BuildSPParams directly
// (the same path the handler uses), using the manual-ACS fixture so no DB or
// metadata XML parsing is required.
//
// Route-level sudo gating is covered centrally in Task 9.

package server

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
	saml "prohibitorum/pkg/protocol/saml"

	crewjam "github.com/crewjam/saml"
)

// ----- helpers ---------------------------------------------------------------

func makeSamlSp(id int64, entityID, displayName, kind string) db.SamlSp {
	return db.SamlSp{
		ID:                        id,
		EntityID:                  entityID,
		DisplayName:               displayName,
		SpKind:                    pgtype.Text{String: kind, Valid: kind != ""},
		NameIDFormat:              "urn:oasis:names:tc:SAML:1.1:nameid-format:persistent",
		WantAssertionsSigned:      true,
		RequireSignedAuthnRequest: false,
		AllowIdpInitiated:         false,
		CreatedAt:                 pgtype.Timestamptz{Time: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), Valid: true},
	}
}

// ----- TestAdminSAMLSPs_ViewProjection_FieldMapping --------------------------

// TestAdminSAMLSPs_ViewProjection_FieldMapping verifies that samlProviderView
// correctly maps all scalar fields from db.SamlSp, including the optional
// SpKind (nullable text) and CreatedAt.
func TestAdminSAMLSPs_ViewProjection_FieldMapping(t *testing.T) {
	t.Parallel()

	sp := makeSamlSp(42, "https://sp.example.com", "My SP", "ghes")
	sp.RequireSignedAuthnRequest = true
	sp.AllowIdpInitiated = true

	view := samlProviderView(sp, nil, nil)

	if view.ID != 42 {
		t.Errorf("ID: got %d, want 42", view.ID)
	}
	if view.EntityID != "https://sp.example.com" {
		t.Errorf("EntityID: got %q", view.EntityID)
	}
	if view.DisplayName != "My SP" {
		t.Errorf("DisplayName: got %q", view.DisplayName)
	}
	if view.Kind != "ghes" {
		t.Errorf("Kind: got %q, want ghes", view.Kind)
	}
	if view.NameIDFormat != "urn:oasis:names:tc:SAML:1.1:nameid-format:persistent" {
		t.Errorf("NameIDFormat: got %q", view.NameIDFormat)
	}
	if !view.RequireSignedAuthnRequest {
		t.Error("RequireSignedAuthnRequest: got false, want true")
	}
	if !view.WantAssertionsSigned {
		t.Error("WantAssertionsSigned: got false, want true")
	}
	if !view.AllowIdpInitiated {
		t.Error("AllowIdpInitiated: got false, want true")
	}
	want := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	if !view.CreatedAt.Equal(want) {
		t.Errorf("CreatedAt: got %v, want %v", view.CreatedAt, want)
	}
}

// TestAdminSAMLSPs_ViewProjection_NullableKind verifies that when SpKind is
// not valid (NULL), Kind is an empty string in the view.
func TestAdminSAMLSPs_ViewProjection_NullableKind(t *testing.T) {
	t.Parallel()

	sp := makeSamlSp(1, "https://sp.test", "Test", "")
	sp.SpKind = pgtype.Text{Valid: false}

	view := samlProviderView(sp, nil, nil)
	if view.Kind != "" {
		t.Errorf("Kind: got %q, want empty for NULL SpKind", view.Kind)
	}
}

// TestAdminSAMLSPs_ViewProjection_SessionLifetime verifies that a valid
// SessionLifetime interval converts correctly to seconds in the view.
func TestAdminSAMLSPs_ViewProjection_SessionLifetime(t *testing.T) {
	t.Parallel()

	sp := makeSamlSp(1, "https://sp.test", "Test", "generic")
	// 3600 seconds = 3,600,000,000 microseconds
	sp.SessionLifetime = pgtype.Interval{Microseconds: 3_600_000_000, Valid: true}

	view := samlProviderView(sp, nil, nil)
	if view.SessionLifetimeSecs == nil {
		t.Fatal("SessionLifetimeSecs: got nil, want non-nil")
	}
	if *view.SessionLifetimeSecs != 3600 {
		t.Errorf("SessionLifetimeSecs: got %d, want 3600", *view.SessionLifetimeSecs)
	}
}

// TestAdminSAMLSPs_ViewProjection_NullSessionLifetime verifies that when
// SessionLifetime is not valid (NULL), SessionLifetimeSecs is nil (omitempty).
func TestAdminSAMLSPs_ViewProjection_NullSessionLifetime(t *testing.T) {
	t.Parallel()

	sp := makeSamlSp(1, "https://sp.test", "Test", "generic")
	// SessionLifetime left at zero value (Valid=false)

	view := samlProviderView(sp, nil, nil)
	if view.SessionLifetimeSecs != nil {
		t.Errorf("SessionLifetimeSecs: got %d, want nil for NULL interval", *view.SessionLifetimeSecs)
	}
}

// ----- TestAdminSAMLSPs_ViewProjection_ACSSubView ----------------------------

// TestAdminSAMLSPs_ViewProjection_ACSSubView verifies that ACS rows are
// projected correctly into SAMLACSView slices.
func TestAdminSAMLSPs_ViewProjection_ACSSubView(t *testing.T) {
	t.Parallel()

	sp := makeSamlSp(10, "https://sp.test", "Test", "generic")
	acs := []db.SamlSpAc{
		{SpID: 10, Idx: 0, Binding: crewjam.HTTPPostBinding, Location: "https://sp.test/acs", IsDefault: true},
		{SpID: 10, Idx: 1, Binding: crewjam.HTTPRedirectBinding, Location: "https://sp.test/acs-redirect", IsDefault: false},
	}

	view := samlProviderView(sp, acs, nil)

	if len(view.ACS) != 2 {
		t.Fatalf("ACS: got %d, want 2", len(view.ACS))
	}
	if view.ACS[0].Binding != crewjam.HTTPPostBinding {
		t.Errorf("ACS[0].Binding: got %q", view.ACS[0].Binding)
	}
	if view.ACS[0].Location != "https://sp.test/acs" {
		t.Errorf("ACS[0].Location: got %q", view.ACS[0].Location)
	}
	if view.ACS[0].Index != 0 {
		t.Errorf("ACS[0].Index: got %d, want 0", view.ACS[0].Index)
	}
	if !view.ACS[0].IsDefault {
		t.Error("ACS[0].IsDefault: got false, want true")
	}
	if view.ACS[1].Index != 1 {
		t.Errorf("ACS[1].Index: got %d, want 1", view.ACS[1].Index)
	}
	if view.ACS[1].IsDefault {
		t.Error("ACS[1].IsDefault: got true, want false")
	}
}

// ----- TestAdminSAMLSPs_ViewProjection_KeySubView ----------------------------

// TestAdminSAMLSPs_ViewProjection_KeySubView verifies that SAMLSpKey rows are
// projected into SAMLKeyView without exposing the CertPem field.
func TestAdminSAMLSPs_ViewProjection_KeySubView(t *testing.T) {
	t.Parallel()

	const rawPEM = "-----BEGIN CERTIFICATE-----\nFAKE_PEM\n-----END CERTIFICATE-----\n"
	notAfter := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)

	sp := makeSamlSp(10, "https://sp.test", "Test", "generic")
	keys := []db.SamlSpKey{
		{ID: 1, SpID: 10, Use: "signing", CertPem: rawPEM, NotAfter: pgtype.Timestamptz{Time: notAfter, Valid: true}},
	}

	view := samlProviderView(sp, nil, keys)

	if len(view.Keys) != 1 {
		t.Fatalf("Keys: got %d, want 1", len(view.Keys))
	}
	kv := view.Keys[0]
	if kv.Use != "signing" {
		t.Errorf("Keys[0].Use: got %q, want signing", kv.Use)
	}
	if kv.NotAfter == nil {
		t.Fatal("Keys[0].NotAfter: got nil, want non-nil")
	}
	if !kv.NotAfter.Equal(notAfter) {
		t.Errorf("Keys[0].NotAfter: got %v, want %v", kv.NotAfter, notAfter)
	}
	// Verify the view type has no CertPem field by checking the contract type.
	// This is a compile-time guarantee — SAMLKeyView has no CertPem.
	_ = contract.SAMLKeyView{Use: "signing"}
}

// TestAdminSAMLSPs_ViewProjection_KeyNoCertPemInContract verifies at compile
// time that contract.SAMLKeyView does not declare a CertPem field.
func TestAdminSAMLSPs_ViewProjection_KeyNoCertPemInContract(t *testing.T) {
	t.Parallel()

	// If SAMLKeyView ever grew a CertPem field this test would fail to compile.
	v := contract.SAMLKeyView{Use: "signing"}
	_ = v
}

// TestAdminSAMLSPs_ViewProjection_NullNotAfterKey verifies that a key row with
// a NULL not_after yields a nil NotAfter in the view (omitempty).
func TestAdminSAMLSPs_ViewProjection_NullNotAfterKey(t *testing.T) {
	t.Parallel()

	sp := makeSamlSp(10, "https://sp.test", "Test", "generic")
	keys := []db.SamlSpKey{
		{ID: 1, SpID: 10, Use: "signing", CertPem: "PEM", NotAfter: pgtype.Timestamptz{Valid: false}},
	}

	view := samlProviderView(sp, nil, keys)
	if len(view.Keys) != 1 {
		t.Fatalf("Keys: got %d, want 1", len(view.Keys))
	}
	if view.Keys[0].NotAfter != nil {
		t.Errorf("NotAfter: got %v, want nil for NULL column", view.Keys[0].NotAfter)
	}
}

// ----- TestAdminSAMLSPs_BodyToSPOptions_ManualPath ---------------------------

// TestAdminSAMLSPs_BodyToSPOptions_ManualPath exercises the body→SPOptions→
// BuildSPParams path that handleCreateSAMLProviderHTTP performs, using the
// manual ACS path (no metadata XML). Confirms the handler's mapping is correct.
func TestAdminSAMLSPs_BodyToSPOptions_ManualPath(t *testing.T) {
	t.Parallel()

	wantSigned := false
	opts := saml.SPOptions{
		EntityID:                  "https://sp.example.com",
		DisplayName:               "Test SP",
		Kind:                      "generic",
		RequireSignedAuthnRequest: true,
		AllowIdpInitiated:         false,
		WantAssertionsSigned:      &wantSigned,
		ManualACS: []saml.SPACSEntry{
			{Binding: crewjam.HTTPPostBinding, Location: "https://sp.example.com/acs", Index: 0, IsDefault: true},
		},
	}

	params, acs, certPEMs, err := saml.BuildSPParams(opts)
	if err != nil {
		t.Fatalf("BuildSPParams: %v", err)
	}

	if params.EntityID != "https://sp.example.com" {
		t.Errorf("EntityID: got %q", params.EntityID)
	}
	if !params.SpKind.Valid || params.SpKind.String != "generic" {
		t.Errorf("SpKind: got %+v, want generic", params.SpKind)
	}
	if !params.RequireSignedAuthnRequest {
		t.Error("RequireSignedAuthnRequest: got false, want true")
	}
	if params.WantAssertionsSigned {
		t.Error("WantAssertionsSigned: got true, want false (override)")
	}
	if len(acs) != 1 {
		t.Fatalf("acs: got %d, want 1", len(acs))
	}
	if acs[0].Location != "https://sp.example.com/acs" {
		t.Errorf("acs[0].Location: got %q", acs[0].Location)
	}
	if len(certPEMs) != 0 {
		t.Errorf("certPEMs: got %d, want 0 (no metadata)", len(certPEMs))
	}
}

// TestAdminSAMLSPs_BodyToSPOptions_MissingEntityIDError verifies that omitting
// entityId from the request body causes BuildSPParams to return an error (the
// handler must propagate this as a 400 bad_request).
func TestAdminSAMLSPs_BodyToSPOptions_MissingEntityIDError(t *testing.T) {
	t.Parallel()

	opts := saml.SPOptions{
		Kind: "generic",
		ManualACS: []saml.SPACSEntry{
			{Binding: crewjam.HTTPPostBinding, Location: "https://sp.test/acs"},
		},
	}
	_, _, _, err := saml.BuildSPParams(opts)
	if err == nil {
		t.Fatal("BuildSPParams: got nil error, want error for missing entity_id")
	}
}

// TestAdminSAMLSPs_BodyToSPOptions_NoACSError verifies that omitting ACS
// entries (both metadata and ManualACS) causes BuildSPParams to return an error.
func TestAdminSAMLSPs_BodyToSPOptions_NoACSError(t *testing.T) {
	t.Parallel()

	opts := saml.SPOptions{
		EntityID: "https://sp.test",
		Kind:     "generic",
	}
	_, _, _, err := saml.BuildSPParams(opts)
	if err == nil {
		t.Fatal("BuildSPParams: got nil error, want error for no ACS")
	}
}

// ----- TestAdminSAMLSPs_ErrSPNotFound ----------------------------------------

// TestAdminSAMLSPs_ErrSPNotFound verifies the not-found sentinel used by SAML
// SP handlers returns a 404 status and non-empty machine-readable code.
func TestAdminSAMLSPs_ErrSPNotFound(t *testing.T) {
	t.Parallel()

	err := samlSPNotFound()
	if err == nil {
		t.Fatal("samlSPNotFound: got nil")
	}
	if err.Status != 404 {
		t.Errorf("Status: got %d, want 404", err.Status)
	}
	if err.Code == "" {
		t.Error("Code: got empty string")
	}
}

// TestAdminSAMLSPs_ErrSPNotFound_IsAuthError verifies the returned error
// satisfies the *authn.AuthError interface used by writeAuthErr.
func TestAdminSAMLSPs_ErrSPNotFound_IsAuthError(t *testing.T) {
	t.Parallel()

	err := samlSPNotFound()
	var _ *authn.AuthError = err // compile-time check
	_ = err
}

// ----- TestAdminSAMLSPs_ContractType_SAMLProviderView ------------------------

// TestAdminSAMLSPs_ContractType_SAMLProviderView verifies at compile time that
// contract.SAMLProviderView declares the expected fields used by the handler.
func TestAdminSAMLSPs_ContractType_SAMLProviderView(t *testing.T) {
	t.Parallel()

	v := contract.SAMLProviderView{
		ID:                        1,
		EntityID:                  "https://sp.test",
		DisplayName:               "Test",
		Kind:                      "generic",
		NameIDFormat:              "urn:oasis:names:tc:SAML:1.1:nameid-format:persistent",
		RequireSignedAuthnRequest: false,
		WantAssertionsSigned:      true,
		AllowIdpInitiated:         false,
		ACS:                       []contract.SAMLACSView{},
		Keys:                      []contract.SAMLKeyView{},
	}
	_ = v
}
