package saml

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/xml"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	crewjam "github.com/crewjam/saml"

	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/db"
)

const persistentNameID11 = "urn:oasis:names:tc:SAML:1.1:nameid-format:persistent"

func metadataTestIdP(t *testing.T) *IdP {
	t.Helper()
	cfg := &configx.Config{PublicOrigins: []string{"https://idp.example.test"}}
	cfg.SAML.DefaultNameIDFormat = persistentNameID11
	row, _, _ := testSAMLSigningKeyRow(t)
	return newTestIdP(t, cfg, []db.SigningKey{row})
}

func TestMetadataIdPDocument(t *testing.T) {
	i := metadataTestIdP(t)

	raw, err := i.idpMetadata(context.Background())
	if err != nil {
		t.Fatalf("idpMetadata: %v", err)
	}

	var ed crewjam.EntityDescriptor
	if err := xml.Unmarshal(raw, &ed); err != nil {
		t.Fatalf("unmarshal idp metadata: %v\n%s", err, raw)
	}

	if ed.EntityID != "https://idp.example.test" {
		t.Fatalf("EntityID = %q, want %q", ed.EntityID, "https://idp.example.test")
	}
	if len(ed.IDPSSODescriptors) != 1 {
		t.Fatalf("IDPSSODescriptors = %d, want 1", len(ed.IDPSSODescriptors))
	}
	idp := ed.IDPSSODescriptors[0]

	if idp.WantAuthnRequestsSigned == nil || !*idp.WantAuthnRequestsSigned {
		t.Fatalf("WantAuthnRequestsSigned = %v, want non-nil true", idp.WantAuthnRequestsSigned)
	}
	if idp.ProtocolSupportEnumeration != "urn:oasis:names:tc:SAML:2.0:protocol" {
		t.Fatalf("ProtocolSupportEnumeration = %q", idp.ProtocolSupportEnumeration)
	}

	// At least one signing KeyDescriptor whose cert DER parses.
	signingCount := 0
	for _, kd := range idp.KeyDescriptors {
		if kd.Use != "signing" {
			continue
		}
		for _, xc := range kd.KeyInfo.X509Data.X509Certificates {
			der, derr := base64.StdEncoding.DecodeString(stripWhitespace(xc.Data))
			if derr != nil {
				t.Fatalf("decode advertised cert: %v", derr)
			}
			if _, perr := x509.ParseCertificate(der); perr != nil {
				t.Fatalf("parse advertised cert: %v", perr)
			}
			signingCount++
		}
	}
	if signingCount < 1 {
		t.Fatalf("signing KeyDescriptors with parseable cert = %d, want >=1", signingCount)
	}

	assertBothBindings(t, "SingleSignOnService", idp.SingleSignOnServices, "https://idp.example.test/saml/sso")
	assertBothBindings(t, "SingleLogoutService", idp.SingleLogoutServices, "https://idp.example.test/saml/slo")

	foundFormat := false
	for _, f := range idp.NameIDFormats {
		if string(f) == persistentNameID11 {
			foundFormat = true
		}
	}
	if !foundFormat {
		t.Fatalf("NameIDFormats = %v, want to contain %q", idp.NameIDFormats, persistentNameID11)
	}
}

func assertBothBindings(t *testing.T, name string, eps []crewjam.Endpoint, wantLoc string) {
	t.Helper()
	var redirect, post bool
	for _, e := range eps {
		if e.Location != wantLoc {
			t.Fatalf("%s endpoint location = %q, want %q", name, e.Location, wantLoc)
		}
		switch e.Binding {
		case crewjam.HTTPRedirectBinding:
			redirect = true
		case crewjam.HTTPPostBinding:
			post = true
		}
	}
	if !redirect {
		t.Fatalf("%s missing HTTP-Redirect binding", name)
	}
	if !post {
		t.Fatalf("%s missing HTTP-POST binding", name)
	}
}

func TestMetadataHandleHTTP(t *testing.T) {
	i := metadataTestIdP(t)

	req := httptest.NewRequest(http.MethodGet, "https://idp.example.test/saml/metadata", nil)
	rec := httptest.NewRecorder()
	i.HandleMetadata(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/samlmetadata+xml" {
		t.Fatalf("Content-Type = %q", ct)
	}
	var ed crewjam.EntityDescriptor
	if err := xml.Unmarshal(rec.Body.Bytes(), &ed); err != nil {
		t.Fatalf("response body does not unmarshal: %v", err)
	}
	if ed.EntityID != "https://idp.example.test" {
		t.Fatalf("response EntityID = %q", ed.EntityID)
	}
}

// generateTestCertDER mints a self-signed cert and returns its DER bytes.
func generateTestCertDER(t *testing.T) []byte {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "ghes.example.test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	return der
}

func TestMetadataParseSP(t *testing.T) {
	certDER := generateTestCertDER(t)
	isDefault := true
	ed := crewjam.EntityDescriptor{
		EntityID: "https://ghes.example.test",
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
						Location:  "https://ghes.example.test/saml/consume",
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
	fixture := append([]byte(xml.Header), body...)

	entityID, acs, certs, err := parseSPMetadata(fixture)
	if err != nil {
		t.Fatalf("parseSPMetadata: %v\n%s", err, fixture)
	}
	if entityID != "https://ghes.example.test" {
		t.Fatalf("entityID = %q", entityID)
	}
	if len(acs) != 1 {
		t.Fatalf("acs = %d, want 1", len(acs))
	}
	a := acs[0]
	if a.Binding != crewjam.HTTPPostBinding {
		t.Fatalf("acs binding = %q", a.Binding)
	}
	if a.Location != "https://ghes.example.test/saml/consume" {
		t.Fatalf("acs location = %q", a.Location)
	}
	if a.Index != 0 {
		t.Fatalf("acs index = %d", a.Index)
	}
	if !a.IsDefault {
		t.Fatalf("acs isDefault = false, want true")
	}
	if len(certs) != 1 {
		t.Fatalf("certs = %d, want 1", len(certs))
	}
	if _, err := x509.ParseCertificate(certs[0]); err != nil {
		t.Fatalf("parse extracted cert: %v", err)
	}
}

func TestMetadataParseSPRejectsDoctype(t *testing.T) {
	xxe := []byte(`<?xml version="1.0"?>
<!DOCTYPE foo [ <!ENTITY xxe SYSTEM "file:///etc/passwd"> ]>
<EntityDescriptor xmlns="urn:oasis:names:tc:SAML:2.0:metadata" entityID="https://evil.example.test">
  <SPSSODescriptor protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol">
    <AssertionConsumerService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST" Location="https://evil.example.test/acs" index="0"/>
  </SPSSODescriptor>
</EntityDescriptor>`)

	if _, _, _, err := parseSPMetadata(xxe); err == nil {
		t.Fatal("parseSPMetadata accepted a DOCTYPE-bearing document; want error")
	}
}

func TestMetadataParseSPNoACS(t *testing.T) {
	certDER := generateTestCertDER(t)
	ed := crewjam.EntityDescriptor{
		EntityID: "https://ghes.example.test",
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
				// Intentionally zero AssertionConsumerServices.
			},
		},
	}
	body, err := xml.Marshal(ed)
	if err != nil {
		t.Fatalf("marshal SP metadata: %v", err)
	}
	fixture := append([]byte(xml.Header), body...)

	if _, _, _, err := parseSPMetadata(fixture); err == nil {
		t.Fatal("parseSPMetadata accepted an SPSSODescriptor with no AssertionConsumerService; want error")
	}
}

func TestMetadataParseSPNoSPSSODescriptor(t *testing.T) {
	doc := []byte(`<?xml version="1.0"?>
<EntityDescriptor xmlns="urn:oasis:names:tc:SAML:2.0:metadata" entityID="https://idp.example.test">
  <IDPSSODescriptor protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol">
    <SingleSignOnService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST" Location="https://idp.example.test/saml/sso"/>
  </IDPSSODescriptor>
</EntityDescriptor>`)

	if _, _, _, err := parseSPMetadata(doc); err == nil {
		t.Fatal("parseSPMetadata accepted metadata with no SPSSODescriptor; want error")
	}
}
