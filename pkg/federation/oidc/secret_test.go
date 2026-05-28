package oidc

import (
	"bytes"
	"crypto/rand"
	"testing"
)

func TestSecret_RoundTrip(t *testing.T) {
	dek := make([]byte, 32)
	_, _ = rand.Read(dek)
	plaintext := []byte("upstream-client-secret-here")

	ct, nonce, err := EncryptClientSecret(dek, plaintext, 42, 1)
	if err != nil {
		t.Fatal(err)
	}
	pt, err := DecryptClientSecret(dek, ct, nonce, 42, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(pt, plaintext) {
		t.Errorf("plaintext mismatch")
	}
}

func TestSecret_WrongAADFails(t *testing.T) {
	dek := make([]byte, 32)
	_, _ = rand.Read(dek)
	ct, nonce, _ := EncryptClientSecret(dek, []byte("secret"), 42, 1)

	if _, err := DecryptClientSecret(dek, ct, nonce, 43, 1); err == nil {
		t.Error("decrypt with wrong idpID should fail")
	}
	if _, err := DecryptClientSecret(dek, ct, nonce, 42, 2); err == nil {
		t.Error("decrypt with wrong keyVersion should fail")
	}
}

func TestSecret_WrongKeyFails(t *testing.T) {
	key1 := make([]byte, 32)
	key2 := make([]byte, 32)
	_, _ = rand.Read(key1)
	_, _ = rand.Read(key2)
	ct, nonce, _ := EncryptClientSecret(key1, []byte("secret"), 42, 1)
	if _, err := DecryptClientSecret(key2, ct, nonce, 42, 1); err == nil {
		t.Error("decrypt with wrong DEK should fail")
	}
}

func TestSecret_RejectsNon256Key(t *testing.T) {
	if _, _, err := EncryptClientSecret(make([]byte, 16), []byte("x"), 1, 1); err == nil {
		t.Error("16-byte key should be rejected by Encrypt")
	}
	if _, err := DecryptClientSecret(make([]byte, 16), []byte("x"), make([]byte, 12), 1, 1); err == nil {
		t.Error("16-byte key should be rejected by Decrypt")
	}
}

func TestSecret_DistinctNoncesPerEncryption(t *testing.T) {
	dek := make([]byte, 32)
	_, _ = rand.Read(dek)
	_, n1, _ := EncryptClientSecret(dek, []byte("a"), 1, 1)
	_, n2, _ := EncryptClientSecret(dek, []byte("a"), 1, 1)
	if bytes.Equal(n1, n2) {
		t.Error("two encryptions produced identical nonces — broken randomness")
	}
}
