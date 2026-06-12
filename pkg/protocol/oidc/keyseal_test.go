package oidc

import (
	"context"
	"crypto/rand"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/db"
)

func testDEK(t *testing.T) []byte {
	t.Helper()
	dek := make([]byte, 32)
	if _, err := rand.Read(dek); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return dek
}

// sealedSigningKeyRow mints a fresh key and returns a sealed (no-plaintext)
// signing_key row, exactly as InsertPendingKey / the boot backfill produce.
func sealedSigningKeyRow(t *testing.T, dek []byte, keyVer int32) db.SigningKey {
	t.Helper()
	gen, err := GenerateSigningKey()
	if err != nil {
		t.Fatalf("GenerateSigningKey: %v", err)
	}
	enc, nonce, err := sealPrivateKey(dek, gen.PrivatePEM, gen.Kid, keyVer)
	if err != nil {
		t.Fatalf("sealPrivateKey: %v", err)
	}
	return db.SigningKey{
		Kid:             gen.Kid,
		Algorithm:       "RS256",
		Use:             "sig",
		Status:          "active",
		PublicJwk:       gen.PublicJWK,
		X509CertPem:     pgtype.Text{String: gen.X509CertPEM, Valid: true},
		PrivatePemEnc:   enc,
		PrivatePemNonce: nonce,
		KeyVersion:      keyVer,
	}
}

// A sealed row carries no plaintext, and the cache unseals it with the DEK and
// resolves the active signer — proving seal → unseal → use end to end.
func TestKeyCacheUnsealsSealedRow(t *testing.T) {
	dek := testDEK(t)
	row := sealedSigningKeyRow(t, dek, 1)
	c := newKeyCache(&fakeSigningKeyQueries{rows: []db.SigningKey{row}}, map[int][]byte{1: dek})
	k, ok := c.signingKey(context.Background())
	if !ok {
		t.Fatal("signingKey: expected active key after unseal")
	}
	if k.kid != row.Kid {
		t.Fatalf("kid = %q, want %q", k.kid, row.Kid)
	}
	if k.private == nil || k.private.N.BitLen() != 2048 {
		t.Fatal("unsealed private key invalid")
	}
}

// refresh must fail closed when the row's DEK version is not configured, rather
// than silently dropping the key or panicking.
func TestKeyCacheFailsClosedOnMissingDEK(t *testing.T) {
	dek := testDEK(t)
	row := sealedSigningKeyRow(t, dek, 2) // sealed under DEK v2
	c := newKeyCache(&fakeSigningKeyQueries{rows: []db.SigningKey{row}}, map[int][]byte{1: dek})
	if err := c.refresh(context.Background()); err == nil {
		t.Fatal("expected refresh to fail closed when DEK version is missing")
	}
}

// AAD + GCM integrity: a tampered ciphertext must not unseal.
func TestKeyCacheFailsOnTamperedCiphertext(t *testing.T) {
	dek := testDEK(t)
	row := sealedSigningKeyRow(t, dek, 1)
	row.PrivatePemEnc[0] ^= 0xFF
	c := newKeyCache(&fakeSigningKeyQueries{rows: []db.SigningKey{row}}, map[int][]byte{1: dek})
	if err := c.refresh(context.Background()); err == nil {
		t.Fatal("expected refresh to fail on tampered ciphertext")
	}
}
