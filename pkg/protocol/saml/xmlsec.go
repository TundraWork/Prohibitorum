package saml

import (
	"bytes"
	"crypto/rsa"
	"crypto/x509"
	"errors"
	"strings"

	"github.com/beevik/etree"
	dsig "github.com/russellhaering/goxmldsig"
)

// Sentinel errors for the hardened XML/DSig layer. Every other SAML task
// builds on this primitive, so failures are surfaced as comparable sentinels
// (errors.Is) rather than opaque strings.
var (
	errXMLDTD         = errors.New("saml: XML contains a DTD or entity declaration")
	errDuplicateID    = errors.New("saml: XML contains duplicate element IDs")
	errWeakSigAlg     = errors.New("saml: signature uses a weak (SHA-1) algorithm")
	errSigRefMismatch = errors.New("saml: signature reference does not cover the target element")
	errNoSignature    = errors.New("saml: no enveloped signature present")
)

// samlIDAttr is the attribute name SAML uses to identify signable elements.
// goxmldsig defaults to "Id" (lowercase d); SAML uses "ID" (uppercase). The
// signing and validation contexts MUST have their IdAttribute set to this, or
// the Reference URI will point at the wrong/no attribute and verification will
// fail to locate the signed element.
const samlIDAttr = "ID"

// parseXMLSecure parses raw XML with XXE/DTD defenses and duplicate-ID
// rejection. etree is a non-validating parser (it never expands external
// entities), but we additionally scan the raw bytes for DOCTYPE/ENTITY
// directives and reject them outright as defense-in-depth, then walk the tree
// to reject documents that reuse an ID.
func parseXMLSecure(raw []byte) (*etree.Document, error) {
	if containsDoctypeOrEntity(raw) {
		return nil, errXMLDTD
	}

	doc := etree.NewDocument()
	// Strict (non-permissive) parsing; no custom entity table, no auto-close.
	doc.ReadSettings = etree.ReadSettings{
		Permissive:    false,
		ValidateInput: true,
		Entity:        nil,
	}
	if err := doc.ReadFromBytes(raw); err != nil {
		return nil, err
	}

	root := doc.Root()
	if root != nil {
		if err := assertUniqueIDs(root, map[string]struct{}{}); err != nil {
			return nil, err
		}
	}

	return doc, nil
}

// containsDoctypeOrEntity scans for XML DTD / entity declarations. We tokenize
// only enough to skip comments and CDATA so that a literal "<!DOCTYPE" inside a
// comment or CDATA section is not flagged, while a real directive anywhere in
// the prolog or body is rejected.
func containsDoctypeOrEntity(raw []byte) bool {
	for i := 0; i < len(raw); i++ {
		if raw[i] != '<' {
			continue
		}
		rest := raw[i:]
		switch {
		case bytes.HasPrefix(rest, []byte("<!--")):
			// Skip to end of comment.
			end := bytes.Index(rest, []byte("-->"))
			if end < 0 {
				return false
			}
			i += end + 2
		case bytes.HasPrefix(rest, []byte("<![CDATA[")):
			// Skip to end of CDATA.
			end := bytes.Index(rest, []byte("]]>"))
			if end < 0 {
				return false
			}
			i += end + 2
		case bytes.HasPrefix(rest, []byte("<!DOCTYPE")) || bytes.HasPrefix(rest, []byte("<!ENTITY")):
			return true
		case bytes.HasPrefix(rest, []byte("<!")):
			// Any other markup declaration (e.g. <!ATTLIST, <!ELEMENT, <!NOTATION)
			// only occurs inside a DTD; reject conservatively.
			return true
		}
	}
	return false
}

// assertUniqueIDs walks the element tree and errors if any "ID" attribute value
// repeats. Duplicate IDs are the foundation of several signature-wrapping
// attacks, so we reject them before any signature processing.
func assertUniqueIDs(el *etree.Element, seen map[string]struct{}) error {
	if id := el.SelectAttrValue(samlIDAttr, ""); id != "" {
		if _, dup := seen[id]; dup {
			return errDuplicateID
		}
		seen[id] = struct{}{}
	}
	for _, child := range el.ChildElements() {
		if err := assertUniqueIDs(child, seen); err != nil {
			return err
		}
	}
	return nil
}

// signElement produces an enveloped RSA-SHA256 signature over el using
// exclusive C14N, embedding certDER in <ds:X509Certificate>. It returns a copy
// of el with the <ds:Signature> appended (goxmldsig's SignEnveloped does not
// mutate the input). The element must carry an "ID" attribute for the Reference
// URI to resolve.
//
// IMPORTANT: the returned element must be serialized to bytes and reparsed (via
// parseXMLSecure) before verifyElementSignature will accept it. goxmldsig's
// exclusive C14N is sensitive to etree's in-memory namespace bookkeeping, so a
// freshly-signed in-memory element does NOT verify until it has round-tripped
// through the wire form (which is what every real SAML flow does anyway).
func signElement(el *etree.Element, key *rsa.PrivateKey, certDER []byte) (*etree.Element, error) {
	ctx, err := dsig.NewSigningContext(key, [][]byte{certDER})
	if err != nil {
		return nil, err
	}
	// Exclusive C14N with an empty prefix list.
	ctx.Canonicalizer = dsig.MakeC14N10ExclusiveCanonicalizerWithPrefixList("")
	if err := ctx.SetSignatureMethod(dsig.RSASHA256SignatureMethod); err != nil {
		return nil, err
	}
	// SAML uses "ID", not goxmldsig's default "Id".
	ctx.IdAttribute = samlIDAttr

	return ctx.SignEnveloped(el)
}

// verifyElementSignature verifies the enveloped signature on el against a
// caller-pinned certificate (NOT the cert embedded in the message). It applies
// three defenses before delegating to goxmldsig:
//
//  1. errNoSignature  — no <ds:Signature>/<ds:SignedInfo> child present.
//  2. errWeakSigAlg   — SignatureMethod/DigestMethod names a SHA-1 algorithm.
//  3. errSigRefMismatch — the single <ds:Reference URI> does not point at el's
//     own ID (anti signature-wrapping / XSW defense-in-depth).
//
// Only then does it call ctx.Validate, returning nil on success.
func verifyElementSignature(el *etree.Element, cert *x509.Certificate) error {
	sig := findSignatureChild(el)
	if sig == nil {
		return errNoSignature
	}
	signedInfo := childByLocalName(sig, "SignedInfo")
	if signedInfo == nil {
		return errNoSignature
	}

	// Reject SHA-1 in either the signature or digest method.
	if sm := childByLocalName(signedInfo, "SignatureMethod"); sm != nil {
		if isSHA1Algorithm(sm.SelectAttrValue("Algorithm", "")) {
			return errWeakSigAlg
		}
	}
	ref := childByLocalName(signedInfo, "Reference")
	if ref == nil {
		return errNoSignature
	}
	if dm := childByLocalName(ref, "DigestMethod"); dm != nil {
		if isSHA1Algorithm(dm.SelectAttrValue("Algorithm", "")) {
			return errWeakSigAlg
		}
	}

	// Anti-XSW: the Reference must cover el itself. SAML signs the element by
	// its own ID, so the URI must be "#<ID>" (an empty URI — whole-document —
	// is not acceptable for our enveloped-element model).
	wantID := el.SelectAttrValue(samlIDAttr, "")
	gotURI := ref.SelectAttrValue("URI", "")
	if wantID == "" || gotURI != "#"+wantID {
		return errSigRefMismatch
	}

	store := &dsig.MemoryX509CertificateStore{Roots: []*x509.Certificate{cert}}
	ctx := dsig.NewDefaultValidationContext(store)
	ctx.IdAttribute = samlIDAttr

	if _, err := ctx.Validate(el); err != nil {
		return err
	}
	return nil
}

// findSignatureChild returns the direct <ds:Signature> child of el (matched by
// local name so it is namespace-prefix agnostic), or nil.
func findSignatureChild(el *etree.Element) *etree.Element {
	return childByLocalName(el, "Signature")
}

// childByLocalName returns the first direct child element whose local (tag)
// name equals name, ignoring namespace prefix.
func childByLocalName(el *etree.Element, name string) *etree.Element {
	for _, c := range el.ChildElements() {
		if c.Tag == name {
			return c
		}
	}
	return nil
}

// isSHA1Algorithm reports whether an algorithm URI denotes a SHA-1 based
// signature or digest method. Match is case-insensitive on the "sha1" token.
func isSHA1Algorithm(uri string) bool {
	return strings.Contains(strings.ToLower(uri), "sha1")
}
