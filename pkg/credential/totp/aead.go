// Package totp — aead.go
//
// AES-256-GCM encryption for TOTP secrets at rest. The AAD binds each
// ciphertext to (account_id, key_version) so a ciphertext lifted out of one
// row and dropped onto a different account_id fails decryption — defence in
// depth against row-id confusion bugs in the surrounding query layer.

package totp

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"strconv"
)

func aadFor(accountID int32, keyVersion int32) []byte {
	return []byte("totp:" + strconv.Itoa(int(accountID)) + ":" + strconv.Itoa(int(keyVersion)))
}

func encryptSecret(dek, plaintext, aad []byte) (ciphertext, nonce []byte, err error) {
	if len(dek) != 32 {
		return nil, nil, fmt.Errorf("totp: DEK must be 32 bytes (AES-256), got %d", len(dek))
	}
	block, err := aes.NewCipher(dek)
	if err != nil {
		return nil, nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, err
	}
	nonce = make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, err
	}
	ciphertext = aead.Seal(nil, nonce, plaintext, aad)
	return ciphertext, nonce, nil
}

func decryptSecret(dek, ciphertext, nonce, aad []byte) ([]byte, error) {
	if len(dek) != 32 {
		return nil, fmt.Errorf("totp: DEK must be 32 bytes (AES-256), got %d", len(dek))
	}
	block, err := aes.NewCipher(dek)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return aead.Open(nil, nonce, ciphertext, aad)
}
