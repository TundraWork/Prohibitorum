package saml

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/beevik/etree"
	crewjam "github.com/crewjam/saml"

	"prohibitorum/pkg/db"
)

// SAML protocol constants for the response/assertion builder. Pinned here to
// guard against namespace drift and to make the security-critical values
// (clock skew, validity windows) auditable in one place.
const (
	// bearerConfirmationMethod is the SubjectConfirmation Method for a
	// bearer assertion (the only method the Web Browser SSO profile uses).
	bearerConfirmationMethod = "urn:oasis:names:tc:SAML:2.0:cm:bearer"

	// statusSuccess is the SAML2 top-level status code for a successful
	// authentication response.
	statusSuccess = "urn:oasis:names:tc:SAML:2.0:status:Success"

	// authnContextPasswordProtected is the AuthnContextClassRef we assert:
	// the subject authenticated with a password over a protected transport
	// (TLS). This matches the IdP's actual password+TOTP login flow.
	authnContextPasswordProtected = "urn:oasis:names:tc:SAML:2.0:ac:classes:PasswordProtectedTransport"

	// clockSkew is the leeway applied to Conditions (NotBefore/NotOnOrAfter)
	// to tolerate clock drift between the IdP and the SP. ±180s mirrors
	// crewjam's own MaxClockSkew so a freshly-issued assertion validates on
	// the SP side without being rejected as not-yet-valid or expired.
	clockSkew = 180 * time.Second

	// assertionValidity is how long after issuance the bearer assertion (and
	// its SubjectConfirmationData / Conditions window) remains acceptable.
	assertionValidity = 5 * time.Minute

	// defaultSessionLifetime is the SessionNotOnOrAfter horizon used when the
	// SP does not configure an explicit session_lifetime.
	defaultSessionLifetime = 8 * time.Hour
)

// errNoSigningKey is returned by buildResponse when the key cache has no active
// SAML signing key (and matching cert). A SAML IdP that cannot sign cannot
// produce a usable Response, so we fail closed rather than emit an unsigned one.
var errNoSigningKey = errors.New("saml: no active SAML signing key")

// newSAMLID mints a fresh SAML element ID. SAML IDs are XML IDs, which must be
// valid NCNames — in particular they may NOT begin with a digit. Prefixing 16
// bytes of crypto/rand hex with an underscore guarantees a valid NCName and
// 128 bits of entropy (well above the spec's recommended ≥128 bits).
func newSAMLID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "_" + hex.EncodeToString(b[:]), nil
}

// sessionNotOnOrAfter derives the AuthnStatement's SessionNotOnOrAfter from the
// SP's configured session_lifetime interval (if set), falling back to the
// IdP-wide default the caller supplies (i.samlSessionLifetime(), itself the
// operator-configured saml.session_lifetime or the package default). The
// interval is interpreted relative to base (the auth instant / now). Months are
// treated as 30 days, which is adequate for a session-horizon hint (no SP relies
// on calendar-exact session expiry).
func sessionNotOnOrAfter(sp db.SamlSp, base time.Time, fallback time.Duration) time.Time {
	iv := sp.SessionLifetime
	if !iv.Valid {
		return base.Add(fallback)
	}
	d := time.Duration(iv.Microseconds)*time.Microsecond +
		time.Duration(iv.Days)*24*time.Hour +
		time.Duration(iv.Months)*30*24*time.Hour
	if d <= 0 {
		return base.Add(fallback)
	}
	return base.Add(d)
}

// buildResponse constructs, signs, and serializes a SAML Response carrying a
// signed bearer Assertion per the GHES Web Browser SSO profile. BOTH the
// Assertion and the enclosing Response are signed (RSA-SHA256, exclusive C14N):
// signing the Response defends the wrapper (Status, Destination, InResponseTo)
// while signing the Assertion lets SPs that strip the Response still trust the
// assertion.
//
// The build/sign sequence is load-bearing and deliberately avoids
// Response.Element() regenerating from a populated Assertion field (which would
// drop our already-signed assertion subtree):
//
//  1. build + self-exc-C14N the Assertion (crewjam's Element()),
//  2. sign it,
//  3. build the Response with Assertion == nil,
//  4. append the signed assertion etree as a child (lands after <Status>),
//  5. sign the Response (goxmldsig appends <Signature> as the final child;
//     the enveloped transform tolerates that position and crewjam's SP-side
//     ParseXMLResponse accepts the resulting Response),
//  6. serialize to bytes.
//
// The returned bytes are the wire form; production verifiers reparse them, so
// the in-memory "freshly-signed element does not self-verify" gotcha documented
// on signElement does not affect callers of buildResponse.
func (i *IdP) buildResponse(ctx context.Context, sp db.SamlSp, acsURL, inResponseTo, nameID string, attrs []samlAttr, authTime time.Time, sessionIndex string) (responseXML []byte, err error) {
	priv, certDER, _, ok := i.keys.signingKey(ctx)
	if !ok {
		return nil, errNoSigningKey
	}

	now := time.Now()
	entityID := i.entityID()

	assertionID, err := newSAMLID()
	if err != nil {
		return nil, err
	}
	responseID, err := newSAMLID()
	if err != nil {
		return nil, err
	}

	notOnOrAfter := now.Add(assertionValidity)
	sessionExpiry := sessionNotOnOrAfter(sp, authTime, i.samlSessionLifetime())

	// Build the AttributeStatement only when there are attributes to emit;
	// an empty <AttributeStatement> is omitted entirely (preferred over an
	// empty element).
	var attrStatements []crewjam.AttributeStatement
	if len(attrs) > 0 {
		samlAttrs := make([]crewjam.Attribute, 0, len(attrs))
		for _, a := range attrs {
			values := make([]crewjam.AttributeValue, 0, len(a.Values))
			for _, v := range a.Values {
				values = append(values, crewjam.AttributeValue{Type: "xs:string", Value: v})
			}
			samlAttrs = append(samlAttrs, crewjam.Attribute{
				FriendlyName: a.FriendlyName,
				Name:         a.Name,
				NameFormat:   a.NameFormat,
				Values:       values,
			})
		}
		attrStatements = []crewjam.AttributeStatement{{Attributes: samlAttrs}}
	}

	assertion := crewjam.Assertion{
		ID:           assertionID,
		IssueInstant: now,
		Version:      "2.0",
		Issuer:       crewjam.Issuer{Value: entityID},
		Subject: &crewjam.Subject{
			NameID: &crewjam.NameID{
				// Core §8.3.7: a persistent NameID SHOULD be scoped by the
				// IdP (NameQualifier) and the SP (SPNameQualifier) that the
				// opaque value is meaningful for. Strict SPs key their link
				// table on the full qualified triple.
				NameQualifier:   entityID,
				SPNameQualifier: sp.EntityID,
				Format:          sp.NameIDFormat,
				Value:           nameID,
			},
			SubjectConfirmations: []crewjam.SubjectConfirmation{
				{
					Method: bearerConfirmationMethod,
					SubjectConfirmationData: &crewjam.SubjectConfirmationData{
						Recipient:    acsURL,
						NotOnOrAfter: notOnOrAfter,
						InResponseTo: inResponseTo,
					},
				},
			},
		},
		Conditions: &crewjam.Conditions{
			NotBefore:    now.Add(-clockSkew),
			NotOnOrAfter: notOnOrAfter.Add(clockSkew),
			AudienceRestrictions: []crewjam.AudienceRestriction{
				{Audience: crewjam.Audience{Value: sp.EntityID}},
			},
		},
		AuthnStatements: []crewjam.AuthnStatement{
			{
				AuthnInstant:        authTime,
				SessionIndex:        sessionIndex,
				SessionNotOnOrAfter: &sessionExpiry,
				AuthnContext: crewjam.AuthnContext{
					AuthnContextClassRef: &crewjam.AuthnContextClassRef{
						Value: authnContextPasswordProtected,
					},
				},
			},
		},
		AttributeStatements: attrStatements,
	}

	// 1+2: build the assertion etree (crewjam self-applies exclusive C14N) and
	// sign it.
	assertionEl := assertion.Element()
	signedAssertion, err := signElement(assertionEl, priv, certDER)
	if err != nil {
		return nil, fmt.Errorf("saml: sign assertion: %w", err)
	}

	// 3: build the Response WITHOUT the assertion. Response.Element()
	// regenerates its children from the struct, so a populated Assertion field
	// would emit an unsigned copy and drop our signed subtree.
	response := crewjam.Response{
		ID:           responseID,
		InResponseTo: inResponseTo,
		Version:      "2.0",
		IssueInstant: now,
		Destination:  acsURL,
		Issuer:       &crewjam.Issuer{Value: entityID},
		Status: crewjam.Status{
			StatusCode: crewjam.StatusCode{Value: statusSuccess},
		},
		Assertion: nil,
	}

	// 4: emit the Response element and append the signed assertion. It lands
	// after <Status> (the pre-signature child order is Issuer, Status,
	// Assertion); signing in step 5 appends <Signature> as the final child.
	responseEl := response.Element()
	responseEl.AddChild(signedAssertion)

	// 5: sign the Response (envelopes a <ds:Signature> over the whole element,
	// including the already-signed assertion subtree).
	signedResponse, err := signElement(responseEl, priv, certDER)
	if err != nil {
		return nil, fmt.Errorf("saml: sign response: %w", err)
	}

	// 6: serialize to wire bytes.
	doc := etree.NewDocument()
	doc.SetRoot(signedResponse)
	out, err := doc.WriteToBytes()
	if err != nil {
		return nil, fmt.Errorf("saml: serialize response: %w", err)
	}
	return out, nil
}
