package federation

import (
	"bytes"
	"testing"
)

func TestProviderSecretKeepsLegacyAAD(t *testing.T) {
	dek := bytes.Repeat([]byte{0x42}, 32)
	sealed, err := SealProviderSecret(dek, []byte("secret"), 41, 3)
	if err != nil { t.Fatal(err) }
	got, err := OpenProviderSecret(dek, *sealed, 41)
	if err != nil { t.Fatal(err) }
	if string(got) != "secret" { t.Fatalf("plaintext = %q", got) }
	if _, err := OpenProviderSecret(dek, *sealed, 42); err == nil { t.Fatal("row-swapped secret opened") }
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
