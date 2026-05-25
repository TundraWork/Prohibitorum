package totp

import (
	"bytes"
	"crypto/rand"
	"testing"
)

func TestAEAD_RoundTrip(t *testing.T) {
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	secret := []byte("hello-totp-secret")
	aad := aadFor(42, 1)

	ct, nonce, err := encryptSecret(key, secret, aad)
	if err != nil {
		t.Fatal(err)
	}
	pt, err := decryptSecret(key, ct, nonce, aad)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(pt, secret) {
		t.Errorf("plaintext mismatch: got %q want %q", pt, secret)
	}
}

func TestAEAD_WrongAADFails(t *testing.T) {
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	ct, nonce, _ := encryptSecret(key, []byte("secret"), aadFor(42, 1))
	if _, err := decryptSecret(key, ct, nonce, aadFor(43, 1)); err == nil {
		t.Error("decrypt with wrong AAD (different account) should fail")
	}
	if _, err := decryptSecret(key, ct, nonce, aadFor(42, 2)); err == nil {
		t.Error("decrypt with wrong AAD (different key version) should fail")
	}
}

func TestAEAD_WrongKeyFails(t *testing.T) {
	key1 := make([]byte, 32)
	key2 := make([]byte, 32)
	_, _ = rand.Read(key1)
	_, _ = rand.Read(key2)
	ct, nonce, _ := encryptSecret(key1, []byte("secret"), aadFor(42, 1))
	if _, err := decryptSecret(key2, ct, nonce, aadFor(42, 1)); err == nil {
		t.Error("decrypt with wrong key should fail")
	}
}

func TestAEAD_RejectsNon256Key(t *testing.T) {
	if _, _, err := encryptSecret(make([]byte, 16), []byte("secret"), nil); err == nil {
		t.Error("16-byte key should fail (we require 32 bytes / AES-256)")
	}
	if _, err := decryptSecret(make([]byte, 16), nil, nil, nil); err == nil {
		t.Error("16-byte key should fail on decrypt")
	}
}

func TestAEAD_TamperedCiphertextFails(t *testing.T) {
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	aad := aadFor(42, 1)
	ct, nonce, _ := encryptSecret(key, []byte("secret"), aad)
	ct[0] ^= 0x01
	if _, err := decryptSecret(key, ct, nonce, aad); err == nil {
		t.Error("decrypt with tampered ciphertext should fail")
	}
}

func TestAAD_Format(t *testing.T) {
	got := string(aadFor(42, 3))
	want := "totp:42:3"
	if got != want {
		t.Errorf("aadFor(42, 3) = %q, want %q", got, want)
	}
}
