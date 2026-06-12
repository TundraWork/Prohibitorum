// Package saml — keyseal_saml.go
//
// Decrypts DEK-sealed signing-key private PEMs (signing_key.private_pem_enc).
// The SAML layer only ever READS signing keys, so there is no seal counterpart
// here — sealing happens on the oidc generate path. The AAD convention is
// duplicated from the oidc package per the no-cross-package-coupling decision
// (saml must not import oidc), same as parseRSAPrivatePEM.

package saml

import (
	"crypto/aes"
	"crypto/cipher"
	"fmt"
	"strconv"
)

func keySealAAD(kid string, keyVersion int32) []byte {
	return []byte("signing_key:" + kid + ":" + strconv.Itoa(int(keyVersion)))
}

// openPrivateKey decrypts a DEK-sealed signing private key (AES-256-GCM),
// returning the PEM bytes.
func openPrivateKey(dek, ciphertext, nonce []byte, kid string, keyVersion int32) ([]byte, error) {
	if len(dek) != 32 {
		return nil, fmt.Errorf("saml: DEK must be 32 bytes (AES-256), got %d", len(dek))
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
