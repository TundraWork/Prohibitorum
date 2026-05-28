// Package oidc — secret.go
//
// AES-256-GCM encryption for upstream_idp.client_secret_enc. AAD binds the
// ciphertext to (idp_id, key_version) so copying a ciphertext between
// upstream_idp rows fails decryption — defense against the row-swap class
// of attacks documented in the v0.1 master spec.
//
// Mirrors pkg/credential/totp/aead.go.

package oidc

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"strconv"
)

func aadFor(idpID int64, keyVersion int32) []byte {
	return []byte("upstream_idp:" +
		strconv.FormatInt(idpID, 10) + ":" +
		strconv.Itoa(int(keyVersion)))
}

func EncryptClientSecret(dek, plaintext []byte, idpID int64, keyVersion int32) (ciphertext, nonce []byte, err error) {
	if len(dek) != 32 {
		return nil, nil, fmt.Errorf("federation/oidc: DEK must be 32 bytes (AES-256), got %d", len(dek))
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
	ciphertext = aead.Seal(nil, nonce, plaintext, aadFor(idpID, keyVersion))
	return ciphertext, nonce, nil
}

func DecryptClientSecret(dek, ciphertext, nonce []byte, idpID int64, keyVersion int32) ([]byte, error) {
	if len(dek) != 32 {
		return nil, fmt.Errorf("federation/oidc: DEK must be 32 bytes (AES-256), got %d", len(dek))
	}
	block, err := aes.NewCipher(dek)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return aead.Open(nil, nonce, ciphertext, aadFor(idpID, keyVersion))
}
