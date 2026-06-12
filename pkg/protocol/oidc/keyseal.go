// Package oidc — keyseal.go
//
// AES-256-GCM encryption for signing_key.private_pem_enc. AAD binds the
// ciphertext to (kid, key_version) so a ciphertext copied between signing_key
// rows or replayed under a different DEK version fails decryption — the same
// row-swap defense used for TOTP secrets and upstream client secrets.
//
// Mirrors pkg/federation/oidc/secret.go and pkg/credential/totp/aead.go.

package oidc

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"strconv"
)

func keySealAAD(kid string, keyVersion int32) []byte {
	return []byte("signing_key:" + kid + ":" + strconv.Itoa(int(keyVersion)))
}

// sealPrivateKey encrypts a PEM-encoded signing private key under the DEK,
// returning the ciphertext and the random per-row nonce.
func sealPrivateKey(dek, pemBytes []byte, kid string, keyVersion int32) (ciphertext, nonce []byte, err error) {
	if len(dek) != 32 {
		return nil, nil, fmt.Errorf("oidc: DEK must be 32 bytes (AES-256), got %d", len(dek))
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
	ciphertext = aead.Seal(nil, nonce, pemBytes, keySealAAD(kid, keyVersion))
	return ciphertext, nonce, nil
}

// openPrivateKey reverses sealPrivateKey, returning the PEM bytes.
func openPrivateKey(dek, ciphertext, nonce []byte, kid string, keyVersion int32) ([]byte, error) {
	if len(dek) != 32 {
		return nil, fmt.Errorf("oidc: DEK must be 32 bytes (AES-256), got %d", len(dek))
	}
	block, err := aes.NewCipher(dek)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return aead.Open(nil, nonce, ciphertext, keySealAAD(kid, keyVersion))
}
