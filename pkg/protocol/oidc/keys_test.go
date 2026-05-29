package oidc

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"testing"

	"prohibitorum/pkg/db"
)

// fakeSigningKeyQueries serves a fixed set of rows from memory.
type fakeSigningKeyQueries struct {
	rows []db.SigningKey
	err  error
}

func (f *fakeSigningKeyQueries) ListActiveSigningKeys(context.Context) ([]db.SigningKey, error) {
	return f.rows, f.err
}

// testSigningKeyRow mints a fresh RSA-2048 key and returns a db.SigningKey
// row built the way Task 2's keygen will (PKCS#8 PEM, JWK from publicJWK,
// kid = RFC 7638 thumbprint, active). Downstream test files reuse this.
func testSigningKeyRow(t *testing.T) (db.SigningKey, *rsa.PrivateKey) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	kid := jwkThumbprint(&priv.PublicKey)
	jwkBytes, err := json.Marshal(publicJWK(kid, &priv.PublicKey))
	if err != nil {
		t.Fatalf("marshal jwk: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatalf("marshal pkcs8: %v", err)
	}
	pemStr := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	row := db.SigningKey{
		Kid:        kid,
		Algorithm:  "RS256",
		Use:        "sig",
		PublicJwk:  jwkBytes,
		PrivatePem: pemStr,
		Active:     true,
	}
	return row, priv
}

func TestKeysCacheResolves(t *testing.T) {
	row, priv := testSigningKeyRow(t)
	fake := &fakeSigningKeyQueries{rows: []db.SigningKey{row}}
	c := newKeyCache(fake)
	ctx := context.Background()

	got, ok := c.signingKey(ctx)
	if !ok {
		t.Fatal("signingKey: expected active key, got none")
	}
	if got.kid != row.Kid {
		t.Fatalf("signingKey kid = %q, want %q", got.kid, row.Kid)
	}
	if got.private.D.Cmp(priv.D) != 0 {
		t.Fatal("signingKey: parsed private key differs from source")
	}

	if _, ok := c.byKID(ctx, row.Kid); !ok {
		t.Fatalf("byKID(%q): expected resolution", row.Kid)
	}
	if _, ok := c.byKID(ctx, "nope"); ok {
		t.Fatal("byKID(nope): expected ok=false")
	}
}

func TestKeysJWKS(t *testing.T) {
	row, _ := testSigningKeyRow(t)
	c := newKeyCache(&fakeSigningKeyQueries{rows: []db.SigningKey{row}})
	set := c.jwks(context.Background())

	keys, ok := set["keys"].([]map[string]any)
	if !ok {
		t.Fatalf("jwks keys type = %T, want []map[string]any", set["keys"])
	}
	if len(keys) != 1 {
		t.Fatalf("jwks: got %d keys, want 1", len(keys))
	}
	if keys[0]["kty"] != "RSA" {
		t.Fatalf("jwks key kty = %v, want RSA", keys[0]["kty"])
	}
}

func TestKeysThumbprintDeterministic(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	a := jwkThumbprint(&priv.PublicKey)
	b := jwkThumbprint(&priv.PublicKey)
	if a != b {
		t.Fatalf("thumbprint not deterministic: %q != %q", a, b)
	}
	// base64url of a 32-byte SHA-256 digest is 43 chars (no padding).
	if len(a) != 43 {
		t.Fatalf("thumbprint length = %d, want 43", len(a))
	}
}
