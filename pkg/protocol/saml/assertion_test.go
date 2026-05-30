package saml

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/xml"
	"net/url"
	"testing"
	"time"

	"github.com/beevik/etree"
	crewjam "github.com/crewjam/saml"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/db"
)

// testResponseFixture bundles the inputs and outputs of a buildResponse call so
// the individual assertions in each subtest stay readable.
type testResponseFixture struct {
	idp          *IdP
	cert         *x509.Certificate
	sp           db.SamlSp
	acsURL       string
	inResponseTo string
	nameID       string
	attrs        []samlAttr
	out          []byte
}

func buildTestResponse(t *testing.T) testResponseFixture {
	t.Helper()

	cfg := &configx.Config{PublicOrigins: []string{"https://idp.example.test"}}
	cfg.SAML.DefaultNameIDFormat = "urn:oasis:names:tc:SAML:1.1:nameid-format:persistent"

	row, _, cert := testSAMLSigningKeyRow(t)
	idp := newTestIdP(t, cfg, []db.SigningKey{row})

	sp := db.SamlSp{
		EntityID:     "https://sp.example.test",
		NameIDFormat: "urn:oasis:names:tc:SAML:1.1:nameid-format:persistent",
	}
	acsURL := "https://sp.example.test/saml/consume"
	inResponseTo, err := newSAMLID()
	if err != nil {
		t.Fatalf("newSAMLID: %v", err)
	}
	nameID := "stable-subject-123"
	attrs := []samlAttr{
		{Name: "USERNAME", NameFormat: nameFormatBasic, Values: []string{"octocat"}},
		{Name: "administrator", NameFormat: nameFormatBasic, Values: []string{"true"}},
		{Name: "emails", NameFormat: nameFormatBasic, Values: []string{"octocat@example.test", "cat@example.test"}},
	}

	out, err := idp.buildResponse(
		context.Background(),
		sp,
		acsURL,
		inResponseTo,
		nameID,
		attrs,
		time.Now(),
		"sess-1",
	)
	if err != nil {
		t.Fatalf("buildResponse: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("buildResponse returned empty bytes")
	}

	return testResponseFixture{
		idp:          idp,
		cert:         cert,
		sp:           sp,
		acsURL:       acsURL,
		inResponseTo: inResponseTo,
		nameID:       nameID,
		attrs:        attrs,
		out:          out,
	}
}

// TestAssertionOwnVerifyRoundTrip is the primitive proof: serialize→reparse→
// verify, asserting that BOTH the Response and the embedded Assertion carry a
// valid enveloped RSA-SHA256 signature over their own IDs. We MUST reparse the
// wire bytes first (signElement's output does not self-verify in-memory).
func TestAssertionOwnVerifyRoundTrip(t *testing.T) {
	f := buildTestResponse(t)

	doc, err := parseXMLSecure(f.out)
	if err != nil {
		t.Fatalf("parseXMLSecure: %v", err)
	}
	responseEl := doc.Root()
	if responseEl == nil {
		t.Fatal("no root element in response")
	}
	if responseEl.Tag != "Response" {
		t.Fatalf("root tag = %q, want Response", responseEl.Tag)
	}

	assertionEl := childByLocalName(responseEl, "Assertion")
	if assertionEl == nil {
		t.Fatal("no Assertion child in Response")
	}

	if err := verifyElementSignature(responseEl, f.cert); err != nil {
		t.Errorf("Response signature did not verify: %v", err)
	}
	if err := verifyElementSignature(assertionEl, f.cert); err != nil {
		t.Errorf("Assertion signature did not verify: %v", err)
	}

	// Child order sanity. goxmldsig's SignEnveloped APPENDS the <Signature> as
	// the last child rather than inserting it right after <Issuer> (the strict
	// SAML schema position). This is well-tolerated in practice — the enveloped
	// transform excludes the Signature wherever it sits and the Reference URI
	// pins the covered element by ID — and the crewjam SP-side interop test
	// proves a real verifier accepts it. So the realized order is:
	// Issuer, Status, Assertion, Signature.
	children := responseEl.ChildElements()
	wantOrder := []string{"Issuer", "Status", "Assertion", "Signature"}
	if len(children) != len(wantOrder) {
		t.Fatalf("Response has %d children, want %d (%v)", len(children), len(wantOrder), childTags(children))
	}
	for idx, want := range wantOrder {
		if children[idx].Tag != want {
			t.Errorf("Response child[%d] = %q, want %q (full: %v)", idx, children[idx].Tag, want, childTags(children))
		}
	}
}

// TestAssertionCrewjamInterop is the real interop proof: feed our signed
// Response into a crewjam ServiceProvider configured from our own published IdP
// metadata, and assert ParseXMLResponse accepts it and recovers the right
// NameID and attributes. This exercises the full SP-side validation chain:
// signature against IdP metadata certs, Conditions window vs the real clock,
// Recipient vs currentURL, Audience vs sp.EntityID, and InResponseTo.
func TestAssertionCrewjamInterop(t *testing.T) {
	f := buildTestResponse(t)
	ctx := context.Background()

	idpMetaXML, err := f.idp.idpMetadata(ctx)
	if err != nil {
		t.Fatalf("idpMetadata: %v", err)
	}
	var idpED crewjam.EntityDescriptor
	if err := xml.Unmarshal(idpMetaXML, &idpED); err != nil {
		t.Fatalf("unmarshal IdP metadata: %v", err)
	}

	acsParsed, err := url.Parse(f.acsURL)
	if err != nil {
		t.Fatalf("parse acsURL: %v", err)
	}

	spProvider := crewjam.ServiceProvider{
		EntityID:    f.sp.EntityID,
		AcsURL:      *acsParsed,
		IDPMetadata: &idpED,
	}

	assertion, err := spProvider.ParseXMLResponse(f.out, []string{f.inResponseTo}, *acsParsed)
	if err != nil {
		t.Fatalf("crewjam ParseXMLResponse rejected our Response: %v", err)
	}
	if assertion == nil {
		t.Fatal("ParseXMLResponse returned nil assertion without error")
	}

	// NameID round-trips.
	if assertion.Subject == nil || assertion.Subject.NameID == nil {
		t.Fatal("parsed assertion has no Subject/NameID")
	}
	if got := assertion.Subject.NameID.Value; got != f.nameID {
		t.Errorf("NameID = %q, want %q", got, f.nameID)
	}

	// Recipient matches the ACS URL.
	foundRecipient := false
	for _, sc := range assertion.Subject.SubjectConfirmations {
		if sc.SubjectConfirmationData != nil && sc.SubjectConfirmationData.Recipient == f.acsURL {
			foundRecipient = true
		}
	}
	if !foundRecipient {
		t.Errorf("no SubjectConfirmation Recipient matched %q", f.acsURL)
	}

	// Audience matches sp.EntityID.
	if assertion.Conditions == nil {
		t.Fatal("parsed assertion has no Conditions")
	}
	foundAudience := false
	for _, ar := range assertion.Conditions.AudienceRestrictions {
		if ar.Audience.Value == f.sp.EntityID {
			foundAudience = true
		}
	}
	if !foundAudience {
		t.Errorf("no AudienceRestriction matched sp.EntityID %q", f.sp.EntityID)
	}

	// GHES attributes are present.
	got := map[string][]string{}
	for _, as := range assertion.AttributeStatements {
		for _, a := range as.Attributes {
			for _, v := range a.Values {
				got[a.Name] = append(got[a.Name], v.Value)
			}
		}
	}
	if vals := got["USERNAME"]; len(vals) != 1 || vals[0] != "octocat" {
		t.Errorf("USERNAME attribute = %v, want [octocat]", vals)
	}
	if vals := got["administrator"]; len(vals) != 1 || vals[0] != "true" {
		t.Errorf("administrator attribute = %v, want [true]", vals)
	}
	if vals := got["emails"]; len(vals) != 2 {
		t.Errorf("emails attribute = %v, want 2 values", vals)
	}
}

// TestAssertionTamperRejected flips a byte inside an attribute value and asserts
// both our own verifier and crewjam's SP-side parse reject the mutated message.
// Both rejection checks ALWAYS run: the crewjam check operates on raw bytes
// (no pre-parse), and the our-verifier check treats a parse failure as a valid
// rejection without short-circuiting the crewjam assertion. We exercise tampers
// targeting BOTH the Assertion and the Response signature coverage.
func TestAssertionTamperRejected(t *testing.T) {
	f := buildTestResponse(t)

	// Build the SP provider once; both subtests feed it tampered bytes.
	idpMetaXML, err := f.idp.idpMetadata(context.Background())
	if err != nil {
		t.Fatalf("idpMetadata: %v", err)
	}
	var idpED crewjam.EntityDescriptor
	if err := xml.Unmarshal(idpMetaXML, &idpED); err != nil {
		t.Fatalf("unmarshal IdP metadata: %v", err)
	}
	acsParsed, err := url.Parse(f.acsURL)
	if err != nil {
		t.Fatalf("parse acsURL: %v", err)
	}
	spProvider := crewjam.ServiceProvider{
		EntityID:    f.sp.EntityID,
		AcsURL:      *acsParsed,
		IDPMetadata: &idpED,
	}

	tests := []struct {
		name string
		// old/new are the byte substitution applied to the wire form. Lengths
		// may differ; XML/C14N is length-agnostic so the digest still breaks.
		old string
		new string
		// targetEl is the local name of the element whose signature our own
		// verifier should reject (the element whose covered content changed).
		targetEl string
	}{
		{
			// Mutating an attribute value breaks the Assertion digest (and, via
			// the enveloping Response signature, the Response digest too).
			name:     "assertion attribute value",
			old:      "octocat",
			new:      "attacker",
			targetEl: "Assertion",
		},
		{
			// Mutating the Response Destination breaks the Response digest. The
			// Assertion subtree is untouched, so we point our verifier at the
			// Response element.
			name:     "response destination",
			old:      "/saml/consume",
			new:      "/saml/evilco",
			targetEl: "Response",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tampered := bytes.Replace(f.out, []byte(tc.old), []byte(tc.new), 1)
			if bytes.Equal(tampered, f.out) {
				t.Fatalf("tamper %q->%q produced identical bytes; expected substitution", tc.old, tc.new)
			}

			// crewjam's SP-side parse must reject — ALWAYS runs (raw bytes, no
			// pre-parse needed).
			if _, err := spProvider.ParseXMLResponse(tampered, []string{f.inResponseTo}, *acsParsed); err == nil {
				t.Error("crewjam ParseXMLResponse accepted a tampered Response; expected rejection")
			}

			// Our own verifier must also reject. A parse failure here is itself
			// a valid rejection for our path; either way the crewjam assertion
			// above has already executed.
			doc, err := parseXMLSecure(tampered)
			if err != nil {
				t.Logf("tampered message failed to parse (acceptable rejection): %v", err)
				return
			}
			responseEl := doc.Root()
			if responseEl == nil {
				t.Fatal("no root element after tamper reparse")
			}
			var targetEl *etree.Element
			if tc.targetEl == "Response" {
				targetEl = responseEl
			} else {
				targetEl = childByLocalName(responseEl, tc.targetEl)
			}
			if targetEl == nil {
				t.Fatalf("no %s element after tamper reparse", tc.targetEl)
			}
			if err := verifyElementSignature(targetEl, f.cert); err == nil {
				t.Errorf("verifyElementSignature accepted a tampered %s; expected rejection", tc.targetEl)
			}
		})
	}
}

// TestAssertionSessionNotOnOrAfter exercises the pgtype.Interval -> expiry
// helper directly. It passes a fixed base time so the expected expiry is exact
// (no reliance on time.Now), with a small tolerance to absorb any rounding.
func TestAssertionSessionNotOnOrAfter(t *testing.T) {
	base := time.Date(2026, time.May, 30, 12, 0, 0, 0, time.UTC)
	const tolerance = time.Second

	tests := []struct {
		name string
		iv   pgtype.Interval
		want time.Duration
	}{
		{
			name: "zero-value invalid interval -> default window",
			iv:   pgtype.Interval{}, // Valid == false
			want: defaultSessionLifetime,
		},
		{
			name: "valid but all-zero -> default window (d <= 0)",
			iv:   pgtype.Interval{Valid: true},
			want: defaultSessionLifetime,
		},
		{
			name: "pure microseconds",
			iv:   pgtype.Interval{Microseconds: int64(2 * time.Hour / time.Microsecond), Valid: true},
			want: 2 * time.Hour,
		},
		{
			name: "pure days",
			iv:   pgtype.Interval{Days: 3, Valid: true},
			want: 3 * 24 * time.Hour,
		},
		{
			name: "pure months (months * 30 days)",
			iv:   pgtype.Interval{Months: 2, Valid: true},
			want: 2 * 30 * 24 * time.Hour,
		},
		{
			name: "mixed months + days + microseconds",
			iv:   pgtype.Interval{Months: 1, Days: 2, Microseconds: int64(3 * time.Hour / time.Microsecond), Valid: true},
			want: 30*24*time.Hour + 2*24*time.Hour + 3*time.Hour,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sp := db.SamlSp{SessionLifetime: tc.iv}
			got := sessionNotOnOrAfter(sp, base)
			want := base.Add(tc.want)
			if delta := got.Sub(want); delta < -tolerance || delta > tolerance {
				t.Errorf("sessionNotOnOrAfter = %v, want ~%v (delta %v, tolerance %v)", got, want, delta, tolerance)
			}
		})
	}
}

// childTags is a small debugging helper for clearer failure messages.
func childTags(els []*etree.Element) []string {
	out := make([]string, len(els))
	for i, e := range els {
		out[i] = e.Tag
	}
	return out
}
