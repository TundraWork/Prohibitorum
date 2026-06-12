package saml

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/db"
)

// samlTestDEK is a fixed 32-byte DEK used to seal signing-key rows in tests;
// samlTestDEKs is the version map the SAML key cache unseals with.
var (
	samlTestDEK  = bytes.Repeat([]byte{0x42}, 32)
	samlTestDEKs = map[int][]byte{1: samlTestDEK}
)

// sealForTest mirrors the oidc package's sealPrivateKey (the SAML package only
// ever unseals, so it has no production seal). AAD matches keySealAAD.
func sealForTest(t *testing.T, dek, pemBytes []byte, kid string, keyVer int32) (ciphertext, nonce []byte) {
	t.Helper()
	block, err := aes.NewCipher(dek)
	if err != nil {
		t.Fatalf("aes: %v", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("gcm: %v", err)
	}
	nonce = make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return aead.Seal(nil, nonce, pemBytes, keySealAAD(kid, keyVer)), nonce
}

// fakeSAMLSigningKeyQueries serves a fixed set of rows from memory.
type fakeSAMLSigningKeyQueries struct {
	rows []db.SigningKey
	err  error
}

func (f *fakeSAMLSigningKeyQueries) ListPublishableSigningKeys(context.Context) ([]db.SigningKey, error) {
	return f.rows, f.err
}

// testSAMLSigningKeyRow mints a fresh RSA-2048 key plus a matching self-signed
// cert and returns a db.SigningKey row (PKCS#8 private PEM + X509 cert PEM,
// Active=true) the way Task 0's keygen produces them.
func testSAMLSigningKeyRow(t *testing.T) (db.SigningKey, *rsa.PrivateKey, *x509.Certificate) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "idp.example.test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
	}
	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}
	certPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}))

	keyDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatalf("marshal pkcs8: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	enc, nonce := sealForTest(t, samlTestDEK, keyPEM, "test-kid", 1)

	row := db.SigningKey{
		Kid:             "test-kid",
		Algorithm:       "RS256",
		Use:             "sig",
		PrivatePemEnc:   enc,
		PrivatePemNonce: nonce,
		KeyVersion:      1,
		X509CertPem:     pgtype.Text{String: certPEM, Valid: true},
		Status:          "active",
	}
	return row, priv, cert
}

func newTestIdP(t *testing.T, cfg *configx.Config, rows []db.SigningKey) *IdP {
	t.Helper()
	return &IdP{
		cfg:  cfg,
		keys: newSAMLKeyCache(&fakeSAMLSigningKeyQueries{rows: rows}, samlTestDEKs),
	}
}

func TestIdPURLHelpers(t *testing.T) {
	cfg := &configx.Config{PublicOrigins: []string{"https://idp.example.test"}}
	i := newTestIdP(t, cfg, nil)

	if got := i.entityID(); got != "https://idp.example.test" {
		t.Fatalf("entityID() = %q, want %q", got, "https://idp.example.test")
	}
	if got := i.ssoURL(); got != "https://idp.example.test/saml/sso" {
		t.Fatalf("ssoURL() = %q", got)
	}
	if got := i.sloURL(); got != "https://idp.example.test/saml/slo" {
		t.Fatalf("sloURL() = %q", got)
	}
	if got := i.metadataURL(); got != "https://idp.example.test/saml/metadata" {
		t.Fatalf("metadataURL() = %q", got)
	}
}

func TestIdPURLHelpersEmptyOrigins(t *testing.T) {
	i := newTestIdP(t, &configx.Config{}, nil)
	if got := i.entityID(); got != "" {
		t.Fatalf("entityID() with no origins = %q, want \"\"", got)
	}
	if got := i.ssoURL(); got != "" {
		t.Fatalf("ssoURL() with no origins = %q, want \"\"", got)
	}
	if got := i.sloURL(); got != "" {
		t.Fatalf("sloURL() with no origins = %q, want \"\"", got)
	}
	if got := i.metadataURL(); got != "" {
		t.Fatalf("metadataURL() with no origins = %q, want \"\"", got)
	}
}

func TestIdPSigningKey(t *testing.T) {
	cfg := &configx.Config{PublicOrigins: []string{"https://idp.example.test"}}
	row, priv, _ := testSAMLSigningKeyRow(t)
	i := newTestIdP(t, cfg, []db.SigningKey{row})
	ctx := context.Background()

	gotPriv, certDER, kid, ok := i.keys.signingKey(ctx)
	if !ok {
		t.Fatal("signingKey: expected active key, got none")
	}
	if gotPriv == nil {
		t.Fatal("signingKey: private key is nil")
	}
	if gotPriv.D.Cmp(priv.D) != 0 {
		t.Fatal("signingKey: parsed private key differs from source")
	}
	if kid != row.Kid {
		t.Fatalf("signingKey kid = %q, want %q", kid, row.Kid)
	}
	if len(certDER) == 0 {
		t.Fatal("signingKey: cert DER is empty")
	}
	if _, err := x509.ParseCertificate(certDER); err != nil {
		t.Fatalf("signingKey: cert DER does not round-trip: %v", err)
	}
}

// TestIdPSigningKeyNoCert verifies the fail-closed guard: an active key whose
// X509CertPem is invalid/empty must NOT be returned as a usable signer.
// signElement embeds <ds:X509Certificate>, so a certless active key would
// otherwise produce a signature no verifier accepts.
func TestIdPSigningKeyNoCert(t *testing.T) {
	cfg := &configx.Config{PublicOrigins: []string{"https://idp.example.test"}}
	row, _, _ := testSAMLSigningKeyRow(t)
	row.X509CertPem = pgtype.Text{Valid: false}
	i := newTestIdP(t, cfg, []db.SigningKey{row})

	if _, certDER, _, ok := i.keys.signingKey(context.Background()); ok {
		t.Fatalf("signingKey: active key with no cert must not be usable; got ok=true (certDER len %d)", len(certDER))
	}
}

func TestIdPCerts(t *testing.T) {
	cfg := &configx.Config{PublicOrigins: []string{"https://idp.example.test"}}
	row, _, _ := testSAMLSigningKeyRow(t)
	i := newTestIdP(t, cfg, []db.SigningKey{row})

	certs := i.keys.allCerts(context.Background())
	if len(certs) != 1 {
		t.Fatalf("certs: got %d, want 1", len(certs))
	}
	if certs[0] == nil {
		t.Fatal("certs: nil cert returned")
	}
}
