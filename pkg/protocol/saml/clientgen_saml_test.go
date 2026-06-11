package saml

import (
	"bytes"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"encoding/xml"
	"testing"

	crewjam "github.com/crewjam/saml"
)

// buildSPMetadataFixture marshals a minimal SP EntityDescriptor with one
// HTTP-POST ACS (index 0, default) and one signing KeyDescriptor carrying the
// given cert DER. It mirrors the fixture in metadata_test.go.
func buildSPMetadataFixture(t *testing.T, entityID string, certDER []byte) []byte {
	t.Helper()
	isDefault := true
	ed := crewjam.EntityDescriptor{
		EntityID: entityID,
		SPSSODescriptors: []crewjam.SPSSODescriptor{
			{
				SSODescriptor: crewjam.SSODescriptor{
					RoleDescriptor: crewjam.RoleDescriptor{
						ProtocolSupportEnumeration: "urn:oasis:names:tc:SAML:2.0:protocol",
						KeyDescriptors: []crewjam.KeyDescriptor{
							{
								Use: "signing",
								KeyInfo: crewjam.KeyInfo{
									X509Data: crewjam.X509Data{
										X509Certificates: []crewjam.X509Certificate{
											{Data: base64.StdEncoding.EncodeToString(certDER)},
										},
									},
								},
							},
						},
					},
				},
				AssertionConsumerServices: []crewjam.IndexedEndpoint{
					{
						Binding:   crewjam.HTTPPostBinding,
						Location:  entityID + "/saml/consume",
						Index:     0,
						IsDefault: &isDefault,
					},
				},
			},
		},
	}
	body, err := xml.Marshal(ed)
	if err != nil {
		t.Fatalf("marshal SP metadata: %v", err)
	}
	return append([]byte(xml.Header), body...)
}

func TestSPGenGHESMetadata(t *testing.T) {
	certDER := generateTestCertDER(t)
	fixture := buildSPMetadataFixture(t, "https://ghes.example.test", certDER)

	params, acs, certPEMs, err := BuildSPParams(SPOptions{
		MetadataXML: fixture,
		Kind:        "ghes",
		DisplayName: "GHES",
	})
	if err != nil {
		t.Fatalf("BuildSPParams: %v", err)
	}

	if params.EntityID != "https://ghes.example.test" {
		t.Fatalf("EntityID = %q", params.EntityID)
	}
	if !params.SpKind.Valid || params.SpKind.String != "ghes" {
		t.Fatalf("SpKind = %+v, want ghes", params.SpKind)
	}
	if len(acs) != 1 {
		t.Fatalf("acs = %d, want 1", len(acs))
	}
	if acs[0].Binding != crewjam.HTTPPostBinding || acs[0].Location != "https://ghes.example.test/saml/consume" {
		t.Fatalf("acs[0] = %+v", acs[0])
	}
	if !acs[0].IsDefault || acs[0].Index != 0 {
		t.Fatalf("acs[0] default/index = %+v", acs[0])
	}

	if !bytes.Equal(params.AttributeMap, ghesDefaultAttributeMap()) {
		t.Fatalf("AttributeMap = %s, want GHES default", params.AttributeMap)
	}
	if !params.RequireSignedAuthnRequest {
		t.Fatal("RequireSignedAuthnRequest = false, want true for ghes")
	}
	// NameID format default.
	if params.NameIDFormat != persistentNameIDFormat11 {
		t.Fatalf("NameIDFormat = %q, want %q", params.NameIDFormat, persistentNameIDFormat11)
	}
	// MetadataXml stored.
	if !params.MetadataXml.Valid || params.MetadataXml.String == "" {
		t.Fatal("MetadataXml not stored")
	}

	if len(certPEMs) != 1 {
		t.Fatalf("certPEMs = %d, want 1", len(certPEMs))
	}
	block, _ := pem.Decode([]byte(certPEMs[0]))
	if block == nil || block.Type != "CERTIFICATE" {
		t.Fatalf("certPEM did not PEM-decode to a CERTIFICATE block: %q", certPEMs[0])
	}
	if _, err := x509.ParseCertificate(block.Bytes); err != nil {
		t.Fatalf("parse PEM-decoded cert: %v", err)
	}
}

func TestSPGenManualGeneric(t *testing.T) {
	params, acs, certPEMs, err := BuildSPParams(SPOptions{
		EntityID:    "https://sp.example.test",
		DisplayName: "Generic SP",
		Kind:        "generic",
		ManualACS: []SPACSEntry{
			{Binding: crewjam.HTTPPostBinding, Location: "https://sp.example.test/acs", Index: 0, IsDefault: true},
		},
		RequireSignedAuthnRequest: true,
	})
	if err != nil {
		t.Fatalf("BuildSPParams: %v", err)
	}

	if params.EntityID != "https://sp.example.test" {
		t.Fatalf("EntityID = %q", params.EntityID)
	}
	if !params.SpKind.Valid || params.SpKind.String != "generic" {
		t.Fatalf("SpKind = %+v, want generic", params.SpKind)
	}
	// Generic uses an empty/minimal attribute map.
	if string(params.AttributeMap) != "[]" {
		t.Fatalf("AttributeMap = %s, want []", params.AttributeMap)
	}
	// require_signed_authn_request honored from the flag.
	if !params.RequireSignedAuthnRequest {
		t.Fatal("RequireSignedAuthnRequest = false, want true (from flag)")
	}
	if len(acs) != 1 {
		t.Fatalf("acs = %d, want 1", len(acs))
	}
	if acs[0].Location != "https://sp.example.test/acs" {
		t.Fatalf("acs[0] location = %q", acs[0].Location)
	}
	if len(certPEMs) != 0 {
		t.Fatalf("certPEMs = %d, want 0 (no metadata)", len(certPEMs))
	}
	// MetadataXml is invalid when no metadata supplied.
	if params.MetadataXml.Valid {
		t.Fatal("MetadataXml.Valid = true, want false (no metadata)")
	}
}

func TestSPGenAllowIdpInitiatedDefaultsFalse(t *testing.T) {
	// Omitting AllowIdpInitiated leaves the SP NOT opted into IdP-initiated SSO.
	params, _, _, err := BuildSPParams(SPOptions{
		EntityID: "https://sp.example.test",
		Kind:     "generic",
		ManualACS: []SPACSEntry{
			{Binding: crewjam.HTTPPostBinding, Location: "https://sp.example.test/acs", Index: 0, IsDefault: true},
		},
	})
	if err != nil {
		t.Fatalf("BuildSPParams: %v", err)
	}
	if params.AllowIdpInitiated {
		t.Fatal("AllowIdpInitiated = true, want default false")
	}
}

func TestSPGenAllowIdpInitiatedSet(t *testing.T) {
	// --allow-idp-initiated → AllowIdpInitiated carried verbatim into the insert.
	params, _, _, err := BuildSPParams(SPOptions{
		EntityID: "https://sp.example.test",
		Kind:     "generic",
		ManualACS: []SPACSEntry{
			{Binding: crewjam.HTTPPostBinding, Location: "https://sp.example.test/acs", Index: 0, IsDefault: true},
		},
		AllowIdpInitiated: true,
	})
	if err != nil {
		t.Fatalf("BuildSPParams: %v", err)
	}
	if !params.AllowIdpInitiated {
		t.Fatal("AllowIdpInitiated = false, want true (from flag)")
	}
}

func TestSPGenMissingEntityID(t *testing.T) {
	_, _, _, err := BuildSPParams(SPOptions{
		Kind: "generic",
		ManualACS: []SPACSEntry{
			{Binding: crewjam.HTTPPostBinding, Location: "https://sp.example.test/acs"},
		},
	})
	if err == nil {
		t.Fatal("BuildSPParams accepted missing entity-id; want error")
	}
}

func TestSPGenNoACS(t *testing.T) {
	// Manual path: entity_id but no ACS at all.
	_, _, _, err := BuildSPParams(SPOptions{
		EntityID: "https://sp.example.test",
		Kind:     "generic",
	})
	if err == nil {
		t.Fatal("BuildSPParams accepted zero ACS; want error")
	}
}

func TestSPGenEmptyKindIsGeneric(t *testing.T) {
	// Empty Kind must behave identically to "generic": sp_kind="generic" + "[]".
	params, _, _, err := BuildSPParams(SPOptions{
		EntityID: "https://sp.example.test",
		Kind:     "",
		ManualACS: []SPACSEntry{
			{Binding: crewjam.HTTPPostBinding, Location: "https://sp.example.test/acs", Index: 0, IsDefault: true},
		},
	})
	if err != nil {
		t.Fatalf("BuildSPParams: %v", err)
	}
	if !params.SpKind.Valid || params.SpKind.String != "generic" {
		t.Fatalf("SpKind = %+v, want {String:generic, Valid:true}", params.SpKind)
	}
	if !bytes.Equal(params.AttributeMap, []byte("[]")) {
		t.Fatalf("AttributeMap = %s, want []", params.AttributeMap)
	}
}

func TestSPGenUnknownKind(t *testing.T) {
	_, _, _, err := BuildSPParams(SPOptions{
		EntityID: "https://sp.example.test",
		Kind:     "samlfish",
		ManualACS: []SPACSEntry{
			{Binding: crewjam.HTTPPostBinding, Location: "https://sp.example.test/acs", Index: 0, IsDefault: true},
		},
	})
	if err == nil {
		t.Fatal("BuildSPParams accepted unknown --kind; want error")
	}
}

func TestSPGenEntityIDOverride(t *testing.T) {
	certDER := generateTestCertDER(t)
	fixture := buildSPMetadataFixture(t, "https://parsed.example.test", certDER)

	params, _, _, err := BuildSPParams(SPOptions{
		MetadataXML: fixture,
		EntityID:    "https://override.example.test",
		Kind:        "generic",
	})
	if err != nil {
		t.Fatalf("BuildSPParams: %v", err)
	}
	if params.EntityID != "https://override.example.test" {
		t.Fatalf("EntityID = %q, want the override", params.EntityID)
	}
}
