package saml

// Tests for the failure-diagnostics contract delivered by PHB-47: errorPage
// must stamp every browser-facing SAML dead-end with event=saml_flow_failure,
// a fine-grained reason, and the raw wrapped error — server-side only. The
// public code/ref redirect is unchanged; the audit Detail stays untouched.
//
// captureSAMLLog mutates the global slog default logger, so nothing in this
// file may call t.Parallel() (the package currently has none either).

import (
	"bytes"
	"compress/flate"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	crewjam "github.com/crewjam/saml"

	"prohibitorum/pkg/authn"
)

// captureSAMLLog redirects slog's default JSON output into a buffer for the
// duration of the test and restores the previous logger afterwards.
func captureSAMLLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return buf
}

// logEntries decodes every complete JSON object in the buffer.
func logEntries(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	dec := json.NewDecoder(strings.NewReader(buf.String()))
	for dec.More() {
		var m map[string]any
		if err := dec.Decode(&m); err != nil {
			t.Fatalf("decode log line: %v\nbuffer:\n%s", err, buf.String())
		}
		out = append(out, m)
	}
	if len(out) == 0 {
		t.Fatalf("no log entries captured:\n%s", buf.String())
	}
	return out
}

// requireFlowFailure finds the saml_flow_failure entry and checks the fields
// every failure line must carry.
func requireFlowFailure(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	for _, m := range logEntries(t, buf) {
		if m["event"] == "saml_flow_failure" {
			for _, k := range []string{"code", "ref", "path", "reason"} {
				if _, ok := m[k]; !ok {
					t.Errorf("saml_flow_failure log missing %q: %v", k, m)
				}
			}
			if ref, _ := m["ref"].(string); ref == "" {
				t.Errorf("saml_flow_failure log has empty ref: %v", m)
			}
			return m
		}
	}
	t.Fatalf("no saml_flow_failure entry found:\n%s", buf.String())
	return nil
}

// foreignIssuerSignedRequest builds a signed redirect-binding AuthnRequest
// whose Issuer is the supplied foreign entity ID and whose detached signature
// is made by the SP's registered key — so the flow fails ONLY on the unknown
// SP, exercising the sp_unknown reason end-to-end.
func foreignIssuerSignedRequest(t *testing.T, spKey *rsa.PrivateKey, issuer, id, acsURL string) *http.Request {
	t.Helper()
	ar := crewjam.AuthnRequest{
		ID:                          id,
		Version:                     "2.0",
		IssueInstant:                time.Now().UTC(),
		Destination:                 testSSOURL,
		Issuer:                      &crewjam.Issuer{Value: issuer},
		AssertionConsumerServiceURL: acsURL,
	}
	xmlBytes, err := xml.Marshal(ar)
	if err != nil {
		t.Fatalf("marshal authnrequest: %v", err)
	}
	return signedRedirectRequest(t, spKey, xmlBytes)
}

// staleIssuerSignedRequest builds a signed redirect-binding AuthnRequest whose
// XML body carries the supplied IssueInstant — used to put a stale instant
// behind a valid SP signature.
func staleIssuerSignedRequest(t *testing.T, spKey *rsa.PrivateKey, instant time.Time) *http.Request {
	t.Helper()
	ar := crewjam.AuthnRequest{
		ID:           "_sso-stale-log",
		Version:      "2.0",
		IssueInstant: instant.UTC(),
		Destination:  testSSOURL,
		Issuer:       &crewjam.Issuer{Value: testSPEntityID},
	}
	xmlBytes, err := xml.Marshal(ar)
	if err != nil {
		t.Fatalf("marshal authnrequest: %v", err)
	}
	return signedRedirectRequest(t, spKey, xmlBytes)
}

// signedRedirectRequest deflates+base64s xmlBytes into SAMLRequest, appends
// the detached RSA-SHA256 signature over the query octet string, and returns
// the GET request for /saml/sso with a live session attached.
func signedRedirectRequest(t *testing.T, spKey *rsa.PrivateKey, xmlBytes []byte) *http.Request {
	t.Helper()
	var deflated bytes.Buffer
	fw, err := flate.NewWriter(&deflated, flate.DefaultCompression)
	if err != nil {
		t.Fatalf("new flate writer: %v", err)
	}
	if _, err := fw.Write(xmlBytes); err != nil {
		t.Fatalf("deflate write: %v", err)
	}
	if err := fw.Close(); err != nil {
		t.Fatalf("deflate close: %v", err)
	}
	samlRequest := base64.StdEncoding.EncodeToString(deflated.Bytes())
	encReq := url.QueryEscape(samlRequest)
	encSigAlg := url.QueryEscape(rsaSHA256SigAlg)
	signed := "SAMLRequest=" + encReq + "&SigAlg=" + encSigAlg
	h := sha256.Sum256([]byte(signed))
	sigBytes, serr := rsa.SignPKCS1v15(rand.Reader, spKey, crypto.SHA256, h[:])
	if serr != nil {
		t.Fatalf("sign: %v", serr)
	}
	rawQuery := signed + "&Signature=" + url.QueryEscape(base64.StdEncoding.EncodeToString(sigBytes))
	req := httptest.NewRequest(http.MethodGet, testSSOURL+"?"+rawQuery, nil)
	req.RemoteAddr = "203.0.113.7:54321"
	req = req.WithContext(authn.WithSession(req.Context(), liveSession(testAccount())))
	return req
}

// ---------------------------------------------------------------------------
// Sentinel-reason field assertions, one per design §3 table row that a handler
// exercises end-to-end.

// TestSSO_UnknownSP_LogsReasonAndCause drives HandleSSO with a signed request
// from an unregistered issuer. The public code is saml_request_invalid; the
// log line must carry reason=sp_unknown and the wrapped error naming the
// issuer — the discriminator that collapses the field report to one of three
// causes.
func TestSSO_UnknownSP_LogsReasonAndCause(t *testing.T) {
	buf := captureSAMLLog(t)
	h := newSSOHarness(t, ssoSP())

	req := foreignIssuerSignedRequest(t, h.spKey, "https://stranger.example.test/saml/metadata", "_sso-unknown-sp-log", testACSURL)

	rec := httptest.NewRecorder()
	h.idp.HandleSSO(rec, req)

	assertErrorRedirect(t, rec, "saml_request_invalid")

	m := requireFlowFailure(t, buf)
	if m["code"] != "saml_request_invalid" {
		t.Errorf("code = %v, want saml_request_invalid", m["code"])
	}
	if m["reason"] != "sp_unknown" {
		t.Errorf("reason = %v, want sp_unknown", m["reason"])
	}
	se, _ := m["saml_error"].(string)
	if !strings.Contains(se, "stranger.example.test") {
		t.Errorf("saml_error = %q, want the unregistered issuer inside", se)
	}
}

// TestSSO_StaleRequest_PublicCodeIsInvalid pins the rehoming: an AuthnRequest
// outside the ±AuthnRequestTTL window now maps to saml_request_invalid (was
// server_error) with reason=request_stale and the IssueInstant discriminator.
func TestSSO_StaleRequest_PublicCodeIsInvalid(t *testing.T) {
	buf := captureSAMLLog(t)
	h := newSSOHarness(t, ssoSP())

	req := staleIssuerSignedRequest(t, h.spKey, time.Now().Add(-AuthnRequestTTL-time.Minute))
	rec := httptest.NewRecorder()
	h.idp.HandleSSO(rec, req)

	assertErrorRedirect(t, rec, "saml_request_invalid")

	m := requireFlowFailure(t, buf)
	if m["code"] != "saml_request_invalid" {
		t.Errorf("code = %v, want saml_request_invalid (stale rehomed)", m["code"])
	}
	if m["reason"] != "request_stale" {
		t.Errorf("reason = %v, want request_stale", m["reason"])
	}
	se, _ := m["saml_error"].(string)
	if !strings.Contains(se, "IssueInstant") {
		t.Errorf("saml_error = %q, want the IssueInstant discriminator", se)
	}
}

// TestSSO_ACSNotRegistered_LogsBothSides drives HandleSSO with a signed
// request whose AssertionConsumerServiceURL is not among the SP's registered
// Locations. The saml_error must name BOTH the requested ACS and the
// registered one — the exact contrast the triage needs.
func TestSSO_ACSNotRegistered_LogsBothSides(t *testing.T) {
	buf := captureSAMLLog(t)
	h := newSSOHarness(t, ssoSP())

	req := foreignIssuerSignedRequest(t, h.spKey, testSPEntityID, "_sso-acs-log", "http://evil.example.test/saml/acs")

	rec := httptest.NewRecorder()
	h.idp.HandleSSO(rec, req)

	assertErrorRedirect(t, rec, "saml_request_invalid")

	m := requireFlowFailure(t, buf)
	if m["reason"] != "acs_not_registered" {
		t.Errorf("reason = %v, want acs_not_registered", m["reason"])
	}
	se, _ := m["saml_error"].(string)
	if !strings.Contains(se, "http://evil.example.test/saml/acs") {
		t.Errorf("saml_error = %q, want the requested ACS URL", se)
	}
	if !strings.Contains(se, testACSURL) {
		t.Errorf("saml_error = %q, want the registered Location %q", se, testACSURL)
	}
}

// TestSSO_SignatureMismatch_LogsCertStats drives HandleSSO with a validly
// formed but wrongly signed request: reason=signature_mismatch and the
// saml_error must show the cert-scan bookkeeping (1 cert scanned, none
// skipped, verification failed).
func TestSSO_SignatureMismatch_LogsCertStats(t *testing.T) {
	buf := captureSAMLLog(t)
	h := newSSOHarness(t, ssoSP())

	wrongKey, _ := testSPKey(t)
	req := foreignIssuerSignedRequest(t, wrongKey, testSPEntityID, "_sso-badsig-log", testACSURL)

	rec := httptest.NewRecorder()
	h.idp.HandleSSO(rec, req)

	assertErrorRedirect(t, rec, "saml_request_invalid")

	m := requireFlowFailure(t, buf)
	if m["reason"] != "signature_mismatch" {
		t.Errorf("reason = %v, want signature_mismatch", m["reason"])
	}
	se, _ := m["saml_error"].(string)
	if !strings.Contains(se, "1 cert(s) scanned") || !strings.Contains(se, "signature did not verify") {
		t.Errorf("saml_error = %q, want cert-scan statistics", se)
	}
}

// TestSLO_DestinationMismatch_LogsReason covers the SLO side of the table:
// a signed LogoutRequest aimed at the wrong Destination logs
// reason=slo_destination_mismatch under code=saml_request_invalid.
func TestSLO_DestinationMismatch_LogsReason(t *testing.T) {
	buf := captureSAMLLog(t)
	h := newSLOHarness(t, sloSP())
	const nameID = "user-nameid-dest-log"
	_ = h.seedSession(t, 42, nameID, "")

	req := buildLogoutRedirect(t, sloReqOpts{
		id:          "_slo-dest-log",
		destination: "https://elsewhere.example.test/saml/slo",
		nameID:      nameID,
		sign:        true,
		signKey:     h.spKey,
	})
	rec := httptest.NewRecorder()
	h.idp.HandleSLO(rec, req)

	assertErrorRedirect(t, rec, "saml_request_invalid")

	m := requireFlowFailure(t, buf)
	if m["reason"] != "slo_destination_mismatch" {
		t.Errorf("reason = %v, want slo_destination_mismatch", m["reason"])
	}
	se, _ := m["saml_error"].(string)
	if !strings.Contains(se, "https://elsewhere.example.test/saml/slo") {
		t.Errorf("saml_error = %q, want the rogue Destination", se)
	}
}

// ---------------------------------------------------------------------------
// cause == nil: saml_error must be ABSENT.

// TestIdPInit_RateLimited_LogsWithoutSamlError exercises a nil-cause failure:
// the rate-limited dead-end logs reason=rate_limited and MUST NOT carry a
// saml_error field.
func TestIdPInit_RateLimited_LogsWithoutSamlError(t *testing.T) {
	buf := captureSAMLLog(t)
	sp := ssoSP()
	sp.AllowIdpInitiated = true
	h := newSSOHarness(t, sp)
	acct := testAccount()

	for n := 0; n < samlSSORateMax; n++ {
		req := idpInitRequest(testSPEntityID, "", liveSession(acct))
		rec := httptest.NewRecorder()
		h.idp.HandleIdPInitiated(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: status = %d, want 200", n, rec.Code)
		}
	}

	req := idpInitRequest(testSPEntityID, "", liveSession(acct))
	rec := httptest.NewRecorder()
	h.idp.HandleIdPInitiated(rec, req)

	assertErrorRedirect(t, rec, "rate_limited")

	m := requireFlowFailure(t, buf)
	if m["reason"] != "rate_limited" {
		t.Errorf("reason = %v, want rate_limited", m["reason"])
	}
	if _, present := m["saml_error"]; present {
		t.Errorf("nil cause must omit saml_error, got %v", m["saml_error"])
	}
}

// ---------------------------------------------------------------------------
// No-leak guard.

// TestErrorPage_LogNeverCarriesRequestSecrets builds a redirect-binding
// request whose SAMLRequest base64 payload and RelayState value are globally
// unique strings, drives it to a parse failure, and asserts the captured log
// never contains the SAMLRequest value, the RelayState value, a NameID, or an
// assertion fragment. The parameter NAMES appear inside the wrapped error only
// as presence booleans, never values.
func TestErrorPage_LogNeverCarriesRequestSecrets(t *testing.T) {
	buf := captureSAMLLog(t)
	h := newSSOHarness(t, ssoSP())

	const uniqueRelay = "relay-secret-9f3a1c-DOES-NOT-BELONG-IN-LOGS"
	const uniqueB64 = "c2FtbC1zZWNyZXQtYmFzZTY0LURFQUJTRQ"
	req := httptest.NewRequest(http.MethodGet,
		testSSOURL+"?SAMLRequest="+uniqueB64+"&RelayState="+url.QueryEscape(uniqueRelay), nil)
	req.RemoteAddr = "203.0.113.7:54321"
	req = req.WithContext(authn.WithSession(req.Context(), liveSession(testAccount())))

	rec := httptest.NewRecorder()
	h.idp.HandleSSO(rec, req)

	assertErrorRedirect(t, rec, "saml_request_invalid")

	out := buf.String()
	for _, forbidden := range []string{
		uniqueB64,    // the SAMLRequest wire value
		uniqueRelay,  // the RelayState value
		"<Assertion", // no assertion fragments
		"NameID",     // no NameID
	} {
		if strings.Contains(out, forbidden) {
			t.Errorf("log leaks %q:\n%s", forbidden, out)
		}
	}
	// The failure line itself must still be complete and specific.
	m := requireFlowFailure(t, buf)
	if m["reason"] != "saml_request_missing" {
		t.Errorf("reason = %v, want saml_request_missing", m["reason"])
	}
	if se, _ := m["saml_error"].(string); !strings.Contains(se, "base64") {
		t.Errorf("saml_error = %q, want the base64 discriminator", se)
	}
}

// TestSSO_InternalError_LogsStepReasonAndCause forces an access-evaluation
// error: the public code stays server_error while the log names the failed
// step (reason=access_evaluate) and carries the underlying driver error —
// the previously-unreachable class of failures.
func TestSSO_InternalError_LogsStepReasonAndCause(t *testing.T) {
	buf := captureSAMLLog(t)
	h := newSSOHarness(t, ssoSP())
	h.q.authzErr = errStubPredicate

	req := h.request(t, "_sso-internal-log", liveSession(testAccount()))
	rec := httptest.NewRecorder()
	h.idp.HandleSSO(rec, req)

	assertErrorRedirect(t, rec, "server_error")

	m := requireFlowFailure(t, buf)
	if m["code"] != "server_error" {
		t.Errorf("code = %v, want server_error", m["code"])
	}
	if m["reason"] != "access_evaluate" {
		t.Errorf("reason = %v, want access_evaluate", m["reason"])
	}
	se, _ := m["saml_error"].(string)
	if !strings.Contains(se, errStubPredicate.Error()) {
		t.Errorf("saml_error = %q, want the underlying %q", se, errStubPredicate.Error())
	}
}

// ---------------------------------------------------------------------------
// Audit stays closed.

// TestErrorPage_AuditDetailUnchanged re-runs the SLO bad-signature flow and
// asserts the audit record carries exactly the two whitelisted keys it had
// before the diagnostics card — the audit channel did not widen.
func TestErrorPage_AuditDetailUnchanged(t *testing.T) {
	captureSAMLLog(t)
	h := newSLOHarness(t, sloSP())
	const nameID = "user-nameid-audit-closed"
	_ = h.seedSession(t, 42, nameID, "")

	wrongKey, _ := testSPKey(t)
	req := buildLogoutRedirect(t, sloReqOpts{
		id:          "_slo-audit-closed",
		destination: testSLOURL,
		nameID:      nameID,
		sign:        true,
		signKey:     wrongKey,
	})
	rec := httptest.NewRecorder()
	h.idp.HandleSLO(rec, req)
	assertErrorRedirect(t, rec, "saml_request_invalid")

	recs := h.auditW.all()
	if len(recs) != 1 {
		t.Fatalf("audit records = %d, want 1", len(recs))
	}
	detail := recs[0].Detail
	if len(detail) != 2 {
		t.Fatalf("audit Detail keys = %v, want exactly reason and sp", detail)
	}
	if _, ok := detail["reason"]; !ok {
		t.Errorf("audit Detail missing reason: %v", detail)
	}
	if _, ok := detail["sp"]; !ok {
		t.Errorf("audit Detail missing sp: %v", detail)
	}
}
