package federation

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"strconv"
)

type SecretStore struct {
	deks map[int][]byte
}

func NewSecretStore(deks map[int][]byte) *SecretStore {
	copied := make(map[int][]byte, len(deks))
	for version, dek := range deks {
		copied[version] = append([]byte(nil), dek...)
	}
	return &SecretStore{deks: copied}
}

func (s *SecretStore) SealProviderSecret(plaintext []byte, providerID int64, keyVersion int32) (*SealedSecret, error) {
	dek, err := s.key(keyVersion)
	if err != nil { return nil, err }
	return SealProviderSecret(dek, plaintext, providerID, keyVersion)
}

func (s *SecretStore) OpenProviderSecret(secret SealedSecret, providerID int64) ([]byte, error) {
	dek, err := s.key(secret.KeyVersion)
	if err != nil { return nil, err }
	return OpenProviderSecret(dek, secret, providerID)
}

func (s *SecretStore) SealTemporary(plaintext []byte, providerID int64, keyVersion int32, challenge string) (*SealedSecret, error) {
	dek, err := s.key(keyVersion)
	if err != nil { return nil, err }
	return SealTemporary(dek, plaintext, providerID, keyVersion, challenge)
}

func (s *SecretStore) OpenTemporary(secret SealedSecret, providerID int64, challenge string) ([]byte, error) {
	dek, err := s.key(secret.KeyVersion)
	if err != nil { return nil, err }
	return OpenTemporary(dek, secret, providerID, challenge)
}

func (s *SecretStore) key(version int32) ([]byte, error) {
	dek, ok := s.deks[int(version)]
	if !ok {
		return nil, fmt.Errorf("federation: no DEK for key version %d", version)
	}
	return dek, nil
}

func SealProviderSecret(dek, plaintext []byte, providerID int64, keyVersion int32) (*SealedSecret, error) {
	return sealSecret(dek, plaintext, keyVersion, providerAAD(providerID, keyVersion))
}

func OpenProviderSecret(dek []byte, secret SealedSecret, providerID int64) ([]byte, error) {
	return openSecret(dek, secret, providerAAD(providerID, secret.KeyVersion))
}

func SealTemporary(dek, plaintext []byte, providerID int64, keyVersion int32, challenge string) (*SealedSecret, error) {
	if challenge == "" {
		return nil, fmt.Errorf("federation: empty operator challenge")
	}
	return sealSecret(dek, plaintext, keyVersion, temporaryAAD(providerID, keyVersion, challenge))
}

func OpenTemporary(dek []byte, secret SealedSecret, providerID int64, challenge string) ([]byte, error) {
	if challenge == "" {
		return nil, fmt.Errorf("federation: empty operator challenge")
	}
	return openSecret(dek, secret, temporaryAAD(providerID, secret.KeyVersion, challenge))
}

func providerAAD(providerID int64, keyVersion int32) []byte {
	return []byte("upstream_idp:" + strconv.FormatInt(providerID, 10) + ":" + strconv.Itoa(int(keyVersion)))
}

func temporaryAAD(providerID int64, keyVersion int32, challenge string) []byte {
	return []byte("upstream_idp:" + strconv.FormatInt(providerID, 10) + ":" + strconv.Itoa(int(keyVersion)) + ":operator:" + challenge)
}

func sealSecret(dek, plaintext []byte, keyVersion int32, aad []byte) (*SealedSecret, error) {
	aead, err := newGCM(dek)
	if err != nil { return nil, err }
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil { return nil, err }
	return &SealedSecret{Ciphertext: aead.Seal(nil, nonce, plaintext, aad), Nonce: nonce, KeyVersion: keyVersion}, nil
}

func openSecret(dek []byte, secret SealedSecret, aad []byte) ([]byte, error) {
	aead, err := newGCM(dek)
	if err != nil { return nil, err }
	return aead.Open(nil, secret.Nonce, secret.Ciphertext, aad)
}

func newGCM(dek []byte) (cipher.AEAD, error) {
	if len(dek) != 32 {
		return nil, fmt.Errorf("federation: DEK must be 32 bytes (AES-256), got %d", len(dek))
	}
	block, err := aes.NewCipher(dek)
	if err != nil { return nil, err }
	return cipher.NewGCM(block)
}
