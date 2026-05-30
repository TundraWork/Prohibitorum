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
	errBadSigAlg      = errors.New("saml: signature does not use RSA-SHA256/SHA-256")
	errSigRefMismatch = errors.New("saml: signature reference does not cover the target element")
	errNoSignature    = errors.New("saml: no enveloped signature present")
)

// Algorithm URIs we positively require on every verified signature. These are
// exactly what signElement produces (RSA-SHA256 over SHA-256 digests) and what
// dsig's constants name (RSASHA256SignatureMethod / the SHA-256 digest URI in
// xmlenc). Anything else — SHA-1, SHA-384/512, ECDSA, HMAC — is rejected, so a
// non-SHA-1 method can no longer slip a denylist.
const (
	requiredSigAlgURI    = "http://www.w3.org/2001/04/xmldsig-more#rsa-sha256"
	requiredDigestAlgURI = "http://www.w3.org/2001/04/xmlenc#sha256"
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

	signed, err := ctx.SignEnveloped(el)
	if err != nil {
		return nil, err
	}
	// goxmldsig APPENDS <ds:Signature> as the LAST child. The SAML 2.0 XSD
	// mandates ds:Signature immediately AFTER <Issuer> in both AssertionType
	// (Issuer, Signature, Subject, ...) and StatusResponseType/ResponseType
	// (Issuer, Signature, ..., Status, Assertion). Strict schema-validating SPs
	// (Shibboleth, ADFS, OpenSAML) reject a misordered Signature
	// (cvc-complex-type.2.4). Relocate it. The enveloped transform excludes the
	// Signature by ID regardless of its position and the Reference is by ID, so
	// the signature still validates after the move.
	relocateSignatureAfterIssuer(signed)
	return signed, nil
}

// relocateSignatureAfterIssuer moves signed's direct-child <ds:Signature> to
// immediately follow its <Issuer> child (or to be the first child if no Issuer
// is present), matching the SAML 2.0 schema's required element ordering. It is a
// no-op if there is no direct-child Signature.
func relocateSignatureAfterIssuer(signed *etree.Element) {
	sig := childByLocalName(signed, "Signature")
	if sig == nil {
		return
	}

	// goxmldsig's SignEnveloped appends the <Signature> by raw-slice append
	// (ret.Child = append(ret.Child, sig)) WITHOUT updating sig's parent/index
	// bookkeeping, so sig.Parent() still points at the pre-sign element and
	// sig.Index() is stale. etree's RemoveChild relies on that bookkeeping and
	// would silently no-op, while InsertChildAt would then duplicate the node.
	// We therefore detach by the Signature's REAL slot in signed.Child and
	// reinsert with RemoveChildAt/InsertChildAt, which rebuild the indices.
	sigSlot := -1
	for i, tok := range signed.Child {
		if e, ok := tok.(*etree.Element); ok && e == sig {
			sigSlot = i
			break
		}
	}
	if sigSlot < 0 {
		return
	}
	signed.RemoveChildAt(sigSlot)

	// InsertChildAt indexes the TOKEN list (signed.Child), which interleaves any
	// CharData/Comment tokens with elements, so we anchor on the Issuer token's
	// real slot rather than its element-only ordinal. With no Issuer, the
	// Signature becomes the very first child.
	insertIdx := 0
	if issuer := childByLocalName(signed, "Issuer"); issuer != nil {
		for i, tok := range signed.Child {
			if e, ok := tok.(*etree.Element); ok && e == issuer {
				insertIdx = i + 1
				break
			}
		}
	}
	signed.InsertChildAt(insertIdx, sig)
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

	// Anti-XSW defense-in-depth: goxmldsig's Validate searches the WHOLE subtree
	// and latches onto the FIRST <Signature> whose Reference URI is empty or
	// names the top-level element's ID. Our SHA-1/URI/reference-tie checks below
	// inspect only the DIRECT-CHILD Signature. If a *deeper* Signature also
	// claims el's ID (or an empty URI), goxmldsig could validate that one
	// instead of the one we vetted. Reject any such stray Signature so the
	// element we gate is provably the element goxmldsig validates. (A nested
	// signed Assertion's own Signature references the Assertion's ID, not el's,
	// so the legitimate Response-wraps-signed-Assertion case is unaffected.)
	elID := el.SelectAttrValue(samlIDAttr, "")
	if hasNestedSignatureForID(el, sig, elID) {
		return errSigRefMismatch
	}

	signedInfo := childByLocalName(sig, "SignedInfo")
	if signedInfo == nil {
		return errNoSignature
	}

	// Positive algorithm allowlist: the SignatureMethod MUST be RSA-SHA256 and
	// the DigestMethod MUST be SHA-256 — exactly what signElement produces.
	// Rejecting anything else (not just SHA-1) closes the gap where a SHA-384/512
	// or other non-SHA-1 method would slip a denylist.
	sm := childByLocalName(signedInfo, "SignatureMethod")
	if sm == nil {
		return errNoSignature
	}
	smAlg := sm.SelectAttrValue("Algorithm", "")
	if isSHA1Algorithm(smAlg) {
		return errWeakSigAlg
	}
	if smAlg != requiredSigAlgURI {
		return errBadSigAlg
	}
	ref := childByLocalName(signedInfo, "Reference")
	if ref == nil {
		return errNoSignature
	}
	dm := childByLocalName(ref, "DigestMethod")
	if dm == nil {
		return errNoSignature
	}
	dmAlg := dm.SelectAttrValue("Algorithm", "")
	if isSHA1Algorithm(dmAlg) {
		return errWeakSigAlg
	}
	if dmAlg != requiredDigestAlgURI {
		return errBadSigAlg
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

// hasNestedSignatureForID reports whether, anywhere in el's subtree below the
// vetted direct-child Signature (keep), there is another <Signature> that
// goxmldsig could mistake for keep — i.e. one carrying a Reference whose URI is
// empty or names elID. Such a stray signature is the hallmark of a
// signature-wrapping (XSW) payload: an attacker hides a second, self-referential
// signed copy of the target ID deeper in the tree hoping the verifier validates
// it instead of the one our gate inspected. A legitimately nested signed
// Assertion's Signature references the Assertion's own ID (not elID) and is
// therefore NOT flagged. keep is skipped so the gated signature itself never
// trips the check.
func hasNestedSignatureForID(el, keep *etree.Element, elID string) bool {
	var walk func(*etree.Element) bool
	walk = func(n *etree.Element) bool {
		for _, c := range n.ChildElements() {
			if c == keep {
				continue
			}
			if c.Tag == "Signature" && signatureReferencesID(c, elID) {
				return true
			}
			if walk(c) {
				return true
			}
		}
		return false
	}
	return walk(el)
}

// signatureReferencesID reports whether sig's SignedInfo/Reference URI is empty
// (whole-document) or points at "#<elID>". This is the same matching rule
// goxmldsig uses to decide a Signature covers the top-level element.
func signatureReferencesID(sig *etree.Element, elID string) bool {
	si := childByLocalName(sig, "SignedInfo")
	if si == nil {
		return false
	}
	for _, ref := range si.ChildElements() {
		if ref.Tag != "Reference" {
			continue
		}
		uri := ref.SelectAttrValue("URI", "")
		if uri == "" || (elID != "" && uri == "#"+elID) {
			return true
		}
	}
	return false
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
