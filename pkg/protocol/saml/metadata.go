package saml

import (
	"context"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"net/http"
	"strings"

	crewjam "github.com/crewjam/saml"
)

// acsEntry is a parsed AssertionConsumerService endpoint extracted from an SP's
// metadata document, flattened into the fields the CLI ingestion path needs.
type acsEntry struct {
	Binding   string
	Location  string
	Index     int
	IsDefault bool
}

// idpMetadata renders this IdP's SAML EntityDescriptor as XML. It advertises
// every non-retired signing cert (audit R6: so a key rotation does not break a
// verifier that has only cached an older cert), both HTTP-Redirect and HTTP-POST
// SSO/SLO endpoints, and the configured persistent NameID format.
func (i *IdP) idpMetadata(ctx context.Context) ([]byte, error) {
	wantSigned := true

	keyDescriptors := make([]crewjam.KeyDescriptor, 0)
	for _, cert := range i.keys.allCerts(ctx) {
		if cert == nil {
			continue
		}
		keyDescriptors = append(keyDescriptors, crewjam.KeyDescriptor{
			Use: "signing",
			KeyInfo: crewjam.KeyInfo{
				X509Data: crewjam.X509Data{
					X509Certificates: []crewjam.X509Certificate{
						{Data: base64.StdEncoding.EncodeToString(cert.Raw)},
					},
				},
			},
		})
	}

	ed := crewjam.EntityDescriptor{
		EntityID: i.entityID(),
		IDPSSODescriptors: []crewjam.IDPSSODescriptor{
			{
				SSODescriptor: crewjam.SSODescriptor{
					RoleDescriptor: crewjam.RoleDescriptor{
						ProtocolSupportEnumeration: "urn:oasis:names:tc:SAML:2.0:protocol",
						KeyDescriptors:             keyDescriptors,
					},
					NameIDFormats: []crewjam.NameIDFormat{
						crewjam.NameIDFormat(i.cfg.SAML.DefaultNameIDFormat),
					},
					SingleLogoutServices: []crewjam.Endpoint{
						{Binding: crewjam.HTTPRedirectBinding, Location: i.sloURL()},
						{Binding: crewjam.HTTPPostBinding, Location: i.sloURL()},
					},
				},
				WantAuthnRequestsSigned: &wantSigned,
				// SSO-in is HTTP-Redirect only: HandleSSO decodes the
				// AuthnRequest from the query string. We do NOT advertise an
				// HTTP-POST SSO binding we cannot serve (POST-binding
				// AuthnRequest intake is unimplemented). SLO accepts both
				// bindings, so the SingleLogoutService list keeps both above.
				SingleSignOnServices: []crewjam.Endpoint{
					{Binding: crewjam.HTTPRedirectBinding, Location: i.ssoURL()},
				},
			},
		},
	}

	body, err := xml.MarshalIndent(ed, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), body...), nil
}

// HandleMetadata serves the IdP metadata document over HTTP.
func (i *IdP) HandleMetadata(w http.ResponseWriter, r *http.Request) {
	body, err := i.idpMetadata(r.Context())
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/samlmetadata+xml")
	_, _ = w.Write(body)
}

// parseSPMetadata parses an SP's SAML metadata document for CLI ingestion. It
// gates on the hardened parseXMLSecure (XXE/DTD/dup-ID protection) before
// unmarshaling, then extracts the entityID, the AssertionConsumerService
// endpoints, and any signing certificates (DER). It does not require certs to be
// present or valid — the CLI decides — but it does require at least one
// SPSSODescriptor with at least one ACS endpoint.
func parseSPMetadata(raw []byte) (entityID string, acs []acsEntry, certs [][]byte, err error) {
	// Security gate: reject DTDs/entities/dup-IDs before parsing the structure.
	if _, serr := parseXMLSecure(raw); serr != nil {
		return "", nil, nil, serr
	}

	var ed crewjam.EntityDescriptor
	if uerr := xml.Unmarshal(raw, &ed); uerr != nil {
		return "", nil, nil, uerr
	}

	if len(ed.SPSSODescriptors) == 0 {
		return "", nil, nil, errors.New("saml: metadata has no SPSSODescriptor")
	}
	spd := ed.SPSSODescriptors[0]

	if len(spd.AssertionConsumerServices) == 0 {
		return "", nil, nil, errors.New("saml: SPSSODescriptor has no AssertionConsumerService")
	}
	acs = make([]acsEntry, 0, len(spd.AssertionConsumerServices))
	for _, e := range spd.AssertionConsumerServices {
		isDefault := false
		if e.IsDefault != nil {
			isDefault = *e.IsDefault
		}
		acs = append(acs, acsEntry{
			Binding:   e.Binding,
			Location:  e.Location,
			Index:     e.Index,
			IsDefault: isDefault,
		})
	}

	certs = make([][]byte, 0)
	for _, kd := range spd.KeyDescriptors {
		// Empty use = both signing and encryption per the SAML metadata spec.
		if kd.Use != "signing" && kd.Use != "" {
			continue
		}
		for _, xc := range kd.KeyInfo.X509Data.X509Certificates {
			cleaned := stripWhitespace(xc.Data)
			if cleaned == "" {
				continue
			}
			der, derr := base64.StdEncoding.DecodeString(cleaned)
			if derr != nil {
				// Skip undecodable certs; the CLI decides on the surviving set.
				continue
			}
			certs = append(certs, der)
		}
	}

	return ed.EntityID, acs, certs, nil
}

// stripWhitespace removes all ASCII whitespace from s. X509Certificate bodies in
// metadata are routinely pretty-printed with newlines and indentation, which
// base64.StdEncoding rejects.
func stripWhitespace(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case ' ', '\t', '\n', '\r':
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
