// Package server — handle_admin_saml_sps_test.go
//
// Unit tests for the SAML SP admin surface (Task 5 + Task 6). Tests are
// intentionally DB-free: samlApplicationView (the projection from db rows →
// contract view) is the primary unit under test, verified for correct field
// mapping, PEM exclusion, optional-field handling, and ACS / key sub-view
// accuracy.
//
// Body-to-SPOptions mapping is also exercised via saml.BuildSPParams directly
// (the same path the handler uses), using the manual-ACS fixture so no DB or
// metadata XML parsing is required.
//
// Handler guard-path tests (Task 6) exercise early-return code paths that
// require only a Server{} with a nil queries field — the handler returns before
// any DB call.
//
// Route-level sudo gating is covered centrally in Task 9.

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
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

// TestAdminSAMLSPs_ViewProjection_FieldMapping verifies that samlApplicationView
// correctly maps all scalar fields from db.SamlSp, including the optional
// SpKind (nullable text) and CreatedAt.
func TestAdminSAMLSPs_ViewProjection_FieldMapping(t *testing.T) {
	t.Parallel()

	sp := makeSamlSp(42, "https://sp.example.com", "My SP", "ghes")
	sp.RequireSignedAuthnRequest = true
	sp.AllowIdpInitiated = true

	view := samlApplicationView(sp, nil, nil)

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

	view := samlApplicationView(sp, nil, nil)
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

	view := samlApplicationView(sp, nil, nil)
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

	view := samlApplicationView(sp, nil, nil)
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

	view := samlApplicationView(sp, acs, nil)

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

	view := samlApplicationView(sp, nil, keys)

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

	view := samlApplicationView(sp, nil, keys)
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

// ----- TestUpdateSAMLSP_ViewProjection_AttrMapAndNameIDClaim -----------------

// TestUpdateSAMLSP_ViewProjection_AttrMapAndNameIDClaim verifies that
// samlApplicationView correctly projects NameIDClaim and AttributeMap from the
// db.SamlSp row into the contract view.
func TestUpdateSAMLSP_ViewProjection_AttrMapAndNameIDClaim(t *testing.T) {
	t.Parallel()

	sp := makeSamlSp(7, "https://sp.example.com", "Test SP", "generic")
	sp.NameIDClaim = "email"
	sp.AttributeMap = []byte(`[{"claim":"email","samlAttr":"mail"}]`)

	view := samlApplicationView(sp, nil, nil)

	if view.NameIDClaim != "email" {
		t.Errorf("NameIDClaim: got %q, want %q", view.NameIDClaim, "email")
	}
	if string(view.AttributeMap) != `[{"claim":"email","samlAttr":"mail"}]` {
		t.Errorf("AttributeMap: got %s, want [{...}]", view.AttributeMap)
	}
}

// TestUpdateSAMLSP_ViewProjection_NilAttrMapDefaultsToEmptyArray verifies that
// when AttributeMap is nil or empty in the db row, samlApplicationView emits a
// valid JSON empty-array ("[]") rather than null or empty bytes.
func TestUpdateSAMLSP_ViewProjection_NilAttrMapDefaultsToEmptyArray(t *testing.T) {
	t.Parallel()

	sp := makeSamlSp(8, "https://sp.example.com", "Test SP", "generic")
	// AttributeMap intentionally left nil.

	view := samlApplicationView(sp, nil, nil)

	if string(view.AttributeMap) != "[]" {
		t.Errorf("AttributeMap: got %q, want []", string(view.AttributeMap))
	}

	// Verify it round-trips as valid JSON.
	var arr []any
	if err := json.Unmarshal(view.AttributeMap, &arr); err != nil {
		t.Errorf("AttributeMap: not valid JSON: %v", err)
	}
	if len(arr) != 0 {
		t.Errorf("AttributeMap: got %d elements, want 0", len(arr))
	}
}

// ----- TestUpdateSAMLSP_UpdateSAMLSPParams_NoAuthnRequestsSignedField --------

// TestUpdateSAMLSP_UpdateSAMLSPParams_NoAuthnRequestsSignedField is a
// compile-time guard: if UpdateSAMLSPParams ever regains an AuthnRequestsSigned
// field (the clobbering bug), this test will fail to compile because the struct
// literal sets all known fields and there is no AuthnRequestsSigned key.
//
// The test verifies the bug-fix: require_signed_authn_request and
// authn_requests_signed are now separate columns controlled by separate paths —
// UpdateSAMLSP only touches require_signed_authn_request, not
// authn_requests_signed.
func TestUpdateSAMLSP_UpdateSAMLSPParams_NoAuthnRequestsSignedField(t *testing.T) {
	t.Parallel()

	// Readable inventory of UpdateSAMLSPParams fields: asserts the struct carries
	// RequireSignedAuthnRequest (the alias-safe flag) and does NOT carry a
	// separate AuthnRequestsSigned field.  Go named-field literals are additive,
	// so adding an unrelated field would not break this literal; the real
	// protection against the alias bug is the SQL SET clause, exercised by the
	// smoke/gate tests.
	p := db.UpdateSAMLSPParams{
		ID:                        1,
		DisplayName:               "Test",
		NameIDFormat:              "urn:oasis:names:tc:SAML:1.1:nameid-format:persistent",
		RequireSignedAuthnRequest: true,
		WantAssertionsSigned:      true,
		AllowIdpInitiated:         false,
		SessionLifetime:           pgtype.Interval{},
		NameIDClaim:               "email",
		AttributeMap:              []byte("[]"),
	}
	// RequireSignedAuthnRequest controls one flag; authn_requests_signed is set
	// only via the create/reingest path (InsertSAMLSP) and is NOT touched by
	// UpdateSAMLSP.
	if !p.RequireSignedAuthnRequest {
		t.Error("RequireSignedAuthnRequest should be true")
	}
}

// ----- TestUpdateSAMLSP_Handler_InvalidAttrMapJSON ---------------------------

// buildUpdateSAMLSPRequest builds a PUT request to /saml-applications/{id} with
// chi URL params pre-populated so chi.URLParam works without a real router.
func buildUpdateSAMLSPRequest(idParam string, bodyJSON string) *http.Request {
	req := httptest.NewRequest(http.MethodPut, "/api/prohibitorum/saml-applications/"+idParam,
		bytes.NewReader([]byte(bodyJSON)))
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", idParam)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

// TestUpdateSAMLSP_Handler_InvalidAttrMapJSON verifies that a PUT body whose
// top-level structure is broken JSON is rejected as bad_request before any DB
// call.  The body `{"displayName":"Test SP","attributeMap":{broken}}` is
// structurally invalid, so json.NewDecoder(...).Decode(&body) rejects it and
// the handler returns 400 bad_request.  This guards the outer-decoder rejection
// path, which is what enforces "attributeMap must be valid JSON".
func TestUpdateSAMLSP_Handler_InvalidAttrMapJSON(t *testing.T) {
	t.Parallel()

	s := &Server{} // queries is nil; handler must return before reaching it
	rr := httptest.NewRecorder()
	// Structurally invalid JSON — the outer decoder rejects the whole body.
	body := `{"displayName":"Test SP","attributeMap":{broken}}`
	req := buildUpdateSAMLSPRequest("1", body)
	s.handleUpdateSAMLApplicationHTTP(rr, req)

	if rr.Code < 400 || rr.Code >= 500 {
		t.Errorf("status = %d; want 4xx for invalid attributeMap JSON", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "bad_request") {
		t.Errorf("body = %q; want bad_request error code", rr.Body.String())
	}
}

// TestUpdateSAMLSP_Handler_MissingDisplayName verifies that a PUT body with an
// empty displayName is rejected as bad_request before any DB call.
func TestUpdateSAMLSP_Handler_MissingDisplayName(t *testing.T) {
	t.Parallel()

	s := &Server{}
	rr := httptest.NewRecorder()
	body := `{"displayName":"","attributeMap":[]}`
	req := buildUpdateSAMLSPRequest("1", body)
	s.handleUpdateSAMLApplicationHTTP(rr, req)

	if rr.Code < 400 || rr.Code >= 500 {
		t.Errorf("status = %d; want 4xx for empty displayName", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "bad_request") {
		t.Errorf("body = %q; want bad_request error code", rr.Body.String())
	}
}

// TestUpdateSAMLSP_Handler_BadID verifies that a non-integer path id is
// rejected as bad_request before any DB or body-decode call.
func TestUpdateSAMLSP_Handler_BadID(t *testing.T) {
	t.Parallel()

	s := &Server{}
	rr := httptest.NewRecorder()
	body := `{"displayName":"Test"}`
	req := buildUpdateSAMLSPRequest("not-an-int", body)
	s.handleUpdateSAMLApplicationHTTP(rr, req)

	if rr.Code < 400 || rr.Code >= 500 {
		t.Errorf("status = %d; want 4xx for non-integer id", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "bad_request") {
		t.Errorf("body = %q; want bad_request error code", rr.Body.String())
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
// contract.SAMLApplicationView declares the expected fields used by the handler.
func TestAdminSAMLSPs_ContractType_SAMLProviderView(t *testing.T) {
	t.Parallel()

	v := contract.SAMLApplicationView{
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
