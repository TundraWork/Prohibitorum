package saml

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/beevik/etree"
	dsig "github.com/russellhaering/goxmldsig"
)

// testKeyCert generates a fresh RSA-2048 key + self-signed cert for a test.
func testKeyCert(t *testing.T) (*rsa.PrivateKey, []byte, *x509.Certificate) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "prohibitorum-test"},
		NotBefore:    time.Now().Add(-1 * time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	return key, der, cert
}

// reparse serializes el and parses it back through parseXMLSecure, mirroring
// the real SAML wire flow (sign -> marshal -> transmit -> parse -> verify).
// goxmldsig's exclusive C14N is sensitive to etree's in-memory parent/namespace
// bookkeeping, so an element straight out of SignEnveloped does not verify until
// it has been round-tripped through serialization. This helper makes the tests
// exercise the production path.
func reparse(t *testing.T, el *etree.Element) *etree.Element {
	t.Helper()
	doc := etree.NewDocument()
	doc.SetRoot(el.Copy())
	raw, err := doc.WriteToBytes()
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	parsed, err := parseXMLSecure(raw)
	if err != nil {
		t.Fatalf("parseXMLSecure: %v", err)
	}
	return parsed.Root()
}

// newIDElement builds a tiny <Thing ID="_<hex>"> element with some text, the
// shape signElement expects (an ID attribute it can reference).
func newIDElement(t *testing.T) *etree.Element {
	t.Helper()
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		t.Fatalf("rand: %v", err)
	}
	el := etree.NewElement("Thing")
	el.CreateAttr("ID", "_"+hex.EncodeToString(buf[:]))
	el.CreateAttr("xmlns", "urn:test")
	el.CreateElement("Inner").SetText("hello")
	return el
}

func TestXMLSecSignVerifyRoundTrip(t *testing.T) {
	key, certDER, cert := testKeyCert(t)
	el := newIDElement(t)

	signed, err := signElement(el, key, certDER)
	if err != nil {
		t.Fatalf("signElement: %v", err)
	}
	if findSignatureChild(signed) == nil {
		t.Fatal("expected a <Signature> child on the signed element")
	}

	wire := reparse(t, signed)
	if err := verifyElementSignature(wire, cert); err != nil {
		t.Fatalf("verifyElementSignature with matching cert: got %v, want nil", err)
	}
}

func TestXMLSecVerifyWrongCert(t *testing.T) {
	key, certDER, _ := testKeyCert(t)
	_, _, otherCert := testKeyCert(t) // different key entirely
	el := newIDElement(t)

	signed, err := signElement(el, key, certDER)
	if err != nil {
		t.Fatalf("signElement: %v", err)
	}

	wire := reparse(t, signed)
	if err := verifyElementSignature(wire, otherCert); err == nil {
		t.Fatal("verifyElementSignature with a different cert: got nil, want error")
	}
}

func TestXMLSecSHA1Rejected(t *testing.T) {
	_, _, cert := testKeyCert(t)

	// Hand-build a <Thing ID> with a <ds:Signature> whose SignatureMethod is
	// the SHA-1 RSA URI. This exercises our alg-gate, not goxmldsig.
	el := etree.NewElement("Thing")
	el.CreateAttr("ID", "_abc")
	sig := el.CreateElement("ds:Signature")
	sig.CreateAttr("xmlns:ds", dsig.Namespace)
	si := sig.CreateElement("ds:SignedInfo")
	sm := si.CreateElement("ds:SignatureMethod")
	sm.CreateAttr("Algorithm", dsig.RSASHA1SignatureMethod)
	ref := si.CreateElement("ds:Reference")
	ref.CreateAttr("URI", "#_abc")
	dm := ref.CreateElement("ds:DigestMethod")
	dm.CreateAttr("Algorithm", "http://www.w3.org/2001/04/xmlenc#sha256")

	if err := verifyElementSignature(el, cert); !errors.Is(err, errWeakSigAlg) {
		t.Fatalf("SHA-1 SignatureMethod: got %v, want errWeakSigAlg", err)
	}
}

func TestXMLSecSHA1DigestRejected(t *testing.T) {
	_, _, cert := testKeyCert(t)

	el := etree.NewElement("Thing")
	el.CreateAttr("ID", "_abc")
	sig := el.CreateElement("ds:Signature")
	sig.CreateAttr("xmlns:ds", dsig.Namespace)
	si := sig.CreateElement("ds:SignedInfo")
	sm := si.CreateElement("ds:SignatureMethod")
	sm.CreateAttr("Algorithm", dsig.RSASHA256SignatureMethod)
	ref := si.CreateElement("ds:Reference")
	ref.CreateAttr("URI", "#_abc")
	dm := ref.CreateElement("ds:DigestMethod")
	dm.CreateAttr("Algorithm", "http://www.w3.org/2000/09/xmldsig#sha1")

	if err := verifyElementSignature(el, cert); !errors.Is(err, errWeakSigAlg) {
		t.Fatalf("SHA-1 DigestMethod: got %v, want errWeakSigAlg", err)
	}
}

// TestXMLSecNonSHA1WrongAlgRejected proves Fix A3's positive allowlist: a
// SignatureMethod that is neither SHA-1 nor our required RSA-SHA256 (here
// RSA-SHA512) is rejected with errBadSigAlg, closing the gap where a non-SHA-1
// method would slip the old SHA-1-only denylist.
func TestXMLSecNonSHA1WrongAlgRejected(t *testing.T) {
	_, _, cert := testKeyCert(t)

	el := etree.NewElement("Thing")
	el.CreateAttr("ID", "_abc")
	sig := el.CreateElement("ds:Signature")
	sig.CreateAttr("xmlns:ds", dsig.Namespace)
	si := sig.CreateElement("ds:SignedInfo")
	sm := si.CreateElement("ds:SignatureMethod")
	sm.CreateAttr("Algorithm", "http://www.w3.org/2001/04/xmldsig-more#rsa-sha512")
	ref := si.CreateElement("ds:Reference")
	ref.CreateAttr("URI", "#_abc")
	dm := ref.CreateElement("ds:DigestMethod")
	dm.CreateAttr("Algorithm", "http://www.w3.org/2001/04/xmlenc#sha256")

	if err := verifyElementSignature(el, cert); !errors.Is(err, errBadSigAlg) {
		t.Fatalf("RSA-SHA512 SignatureMethod: got %v, want errBadSigAlg", err)
	}
}

// TestXMLSecNonSHA1WrongDigestRejected mirrors A3 for the DigestMethod: a
// non-SHA-1, non-SHA-256 digest (SHA-512) is rejected with errBadSigAlg.
func TestXMLSecNonSHA1WrongDigestRejected(t *testing.T) {
	_, _, cert := testKeyCert(t)

	el := etree.NewElement("Thing")
	el.CreateAttr("ID", "_abc")
	sig := el.CreateElement("ds:Signature")
	sig.CreateAttr("xmlns:ds", dsig.Namespace)
	si := sig.CreateElement("ds:SignedInfo")
	sm := si.CreateElement("ds:SignatureMethod")
	sm.CreateAttr("Algorithm", dsig.RSASHA256SignatureMethod)
	ref := si.CreateElement("ds:Reference")
	ref.CreateAttr("URI", "#_abc")
	dm := ref.CreateElement("ds:DigestMethod")
	dm.CreateAttr("Algorithm", "http://www.w3.org/2001/04/xmlenc#sha512")

	if err := verifyElementSignature(el, cert); !errors.Is(err, errBadSigAlg) {
		t.Fatalf("SHA-512 DigestMethod: got %v, want errBadSigAlg", err)
	}
}

// TestXMLSecNestedSignatureSameIDRejected proves Fix A4: after the genuine,
// direct-child Signature, a SECOND <Signature> buried deeper in the subtree that
// also claims the top-level element's ID (a signature-wrapping payload) is
// rejected. This guarantees the Signature our gate inspects is the same one
// goxmldsig would latch onto.
func TestXMLSecNestedSignatureSameIDRejected(t *testing.T) {
	key, certDER, cert := testKeyCert(t)
	el := newIDElement(t)

	signed, err := signElement(el, key, certDER)
	if err != nil {
		t.Fatalf("signElement: %v", err)
	}
	id := signed.SelectAttrValue("ID", "")

	// Bury a stray <Signature> with a Reference URI naming the top-level ID
	// inside the Inner child (deeper than the vetted direct-child Signature).
	inner := childByLocalName(signed, "Inner")
	if inner == nil {
		t.Fatal("expected an <Inner> child to hide a stray Signature under")
	}
	stray := inner.CreateElement("ds:Signature")
	stray.CreateAttr("xmlns:ds", dsig.Namespace)
	ssi := stray.CreateElement("ds:SignedInfo")
	sref := ssi.CreateElement("ds:Reference")
	sref.CreateAttr("URI", "#"+id)

	wire := reparse(t, signed)
	if err := verifyElementSignature(wire, cert); !errors.Is(err, errSigRefMismatch) {
		t.Fatalf("nested same-ID Signature: got %v, want errSigRefMismatch", err)
	}
}

// TestXMLSecNestedSignatureForIDHelper unit-tests the A4 gate directly: a nested
// Signature referencing the top-level ID (or an empty URI) is flagged, while one
// referencing a DIFFERENT ID is not (so the legitimate Response-wraps-signed-
// Assertion shape, where the inner Signature names the Assertion's own ID, is
// tolerated — that end-to-end case is also exercised by
// TestAssertionOwnVerifyRoundTrip's Response verify).
func TestXMLSecNestedSignatureForIDHelper(t *testing.T) {
	build := func(refURI string) (root, keep *etree.Element) {
		root = etree.NewElement("Root")
		root.CreateAttr("ID", "_top")
		keep = root.CreateElement("ds:Signature") // the vetted direct-child sig
		inner := root.CreateElement("Inner")
		stray := inner.CreateElement("ds:Signature")
		si := stray.CreateElement("ds:SignedInfo")
		ref := si.CreateElement("ds:Reference")
		ref.CreateAttr("URI", refURI)
		return root, keep
	}

	t.Run("same ID flagged", func(t *testing.T) {
		root, keep := build("#_top")
		if !hasNestedSignatureForID(root, keep, "_top") {
			t.Fatal("nested Signature naming the top-level ID should be flagged")
		}
	})
	t.Run("empty URI flagged", func(t *testing.T) {
		root, keep := build("")
		if !hasNestedSignatureForID(root, keep, "_top") {
			t.Fatal("nested Signature with empty (whole-doc) URI should be flagged")
		}
	})
	t.Run("different ID not flagged", func(t *testing.T) {
		root, keep := build("#_assertion")
		if hasNestedSignatureForID(root, keep, "_top") {
			t.Fatal("nested Signature naming a different ID must NOT be flagged")
		}
	})
}

func TestXMLSecDuplicateID(t *testing.T) {
	raw := []byte(`<Root xmlns="urn:test"><A ID="x"/><B ID="x"/></Root>`)
	if _, err := parseXMLSecure(raw); !errors.Is(err, errDuplicateID) {
		t.Fatalf("duplicate ID: got %v, want errDuplicateID", err)
	}
}

func TestXMLSecUniqueIDsOK(t *testing.T) {
	raw := []byte(`<Root xmlns="urn:test"><A ID="x"/><B ID="y"/></Root>`)
	if _, err := parseXMLSecure(raw); err != nil {
		t.Fatalf("unique IDs: got %v, want nil", err)
	}
}

func TestXMLSecDTDRejected(t *testing.T) {
	raw := []byte(`<?xml version="1.0"?>
<!DOCTYPE foo [ <!ENTITY x "y"> ]>
<Root>&x;</Root>`)
	if _, err := parseXMLSecure(raw); !errors.Is(err, errXMLDTD) {
		t.Fatalf("DTD/XXE: got %v, want errXMLDTD", err)
	}
}

func TestXMLSecEntityOnlyRejected(t *testing.T) {
	// An ENTITY declaration without a full DOCTYPE wrapper should still trip.
	raw := []byte(`<!ENTITY xxe SYSTEM "file:///etc/passwd"><Root/>`)
	if _, err := parseXMLSecure(raw); !errors.Is(err, errXMLDTD) {
		t.Fatalf("ENTITY decl: got %v, want errXMLDTD", err)
	}
}

func TestXMLSecDoctypeInCommentAllowed(t *testing.T) {
	// A literal <!DOCTYPE inside a comment is not a real directive.
	raw := []byte(`<Root><!-- not a real <!DOCTYPE foo> --><Child/></Root>`)
	if _, err := parseXMLSecure(raw); err != nil {
		t.Fatalf("DOCTYPE-in-comment: got %v, want nil", err)
	}
}

func TestXMLSecWrappedSignatureRefMismatch(t *testing.T) {
	key, certDER, cert := testKeyCert(t)
	el := newIDElement(t)

	signed, err := signElement(el, key, certDER)
	if err != nil {
		t.Fatalf("signElement: %v", err)
	}

	wire := reparse(t, signed)
	// Rewrite the element's own ID so the (validly-signed) Reference URI no
	// longer points at it — simulating a signature-wrapping rearrangement
	// where the verifier's target element is not the one the signature covers.
	wire.RemoveAttr("ID")
	wire.CreateAttr("ID", "_attacker_chosen")

	if err := verifyElementSignature(wire, cert); !errors.Is(err, errSigRefMismatch) {
		t.Fatalf("wrapped signature: got %v, want errSigRefMismatch", err)
	}
}

func TestXMLSecNoSignature(t *testing.T) {
	_, _, cert := testKeyCert(t)
	el := newIDElement(t) // no <Signature> child

	if err := verifyElementSignature(el, cert); !errors.Is(err, errNoSignature) {
		t.Fatalf("no signature: got %v, want errNoSignature", err)
	}
}
