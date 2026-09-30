package federation

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"testing"
)

func TestProviderSecretKeepsLegacyAAD(t *testing.T) {
	dek := bytes.Repeat([]byte{0x42}, 32)
	block, err := aes.NewCipher(dek)
	if err != nil { t.Fatal(err) }
	aead, err := cipher.NewGCM(block)
	if err != nil { t.Fatal(err) }
	nonce := bytes.Repeat([]byte{0x17}, aead.NonceSize())
	sealed := SealedSecret{
		Ciphertext: aead.Seal(nil, nonce, []byte("secret"), []byte("upstream_idp:41:3")),
		Nonce:      nonce,
		KeyVersion: 3,
	}
	got, err := OpenProviderSecret(dek, sealed, 41)
	if err != nil { t.Fatal(err) }
	if string(got) != "secret" { t.Fatalf("plaintext = %q", got) }
	if _, err := OpenProviderSecret(dek, sealed, 42); err == nil { t.Fatal("row-swapped secret opened") }
}

func TestTemporarySecretUsesChallengeAAD(t *testing.T) {
	dek := bytes.Repeat([]byte{0x24}, 32)
	sealed, err := SealTemporary(dek, []byte("operator-secret"), 41, 3, "challenge-a")
	if err != nil { t.Fatal(err) }
	got, err := OpenTemporary(dek, *sealed, 41, "challenge-a")
	if err != nil { t.Fatal(err) }
	if string(got) != "operator-secret" { t.Fatalf("plaintext = %q", got) }
	if _, err := OpenTemporary(dek, *sealed, 41, "challenge-b"); err == nil { t.Fatal("wrong challenge opened") }
	if _, err := OpenProviderSecret(dek, *sealed, 41); err == nil { t.Fatal("temporary secret opened as persisted secret") }
}

func TestSecretStoreSelectsKeyVersion(t *testing.T) {
	store := NewSecretStore(map[int][]byte{3: bytes.Repeat([]byte{0x42}, 32)})
	sealed, err := store.SealProviderSecret([]byte("secret"), 9, 3)
	if err != nil { t.Fatal(err) }
	got, err := store.OpenProviderSecret(*sealed, 9)
	if err != nil { t.Fatal(err) }
	if string(got) != "secret" { t.Fatalf("plaintext = %q", got) }
	sealed.KeyVersion = 4
	if _, err := store.OpenProviderSecret(*sealed, 9); err == nil { t.Fatal("unknown key version accepted") }
}

func TestInstanceSecretRoundTripAndAAD(t *testing.T) {
	dek := bytes.Repeat([]byte{0x33}, 32)
	sealed, err := SealInstanceSecret(dek, []byte("access-key"), "unsplash_access_key", 2)
	if err != nil { t.Fatal(err) }
	got, err := OpenInstanceSecret(dek, *sealed, "unsplash_access_key")
	if err != nil { t.Fatal(err) }
	if string(got) != "access-key" { t.Fatalf("plaintext = %q", got) }
	if _, err := OpenInstanceSecret(dek, *sealed, "other_secret"); err == nil { t.Fatal("secret opened under another name") }
	bumped := *sealed
	bumped.KeyVersion = 3
	if _, err := OpenInstanceSecret(dek, bumped, "unsplash_access_key"); err == nil { t.Fatal("secret opened under another key version") }
	if _, err := OpenProviderSecret(dek, *sealed, 2); err == nil { t.Fatal("instance secret opened as provider secret") }
	if _, err := SealInstanceSecret(dek, []byte("x"), "", 2); err == nil { t.Fatal("empty name accepted") }
}
