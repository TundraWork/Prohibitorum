package oidc

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/db"
)

// GenerateSigningKey mints a fresh RSA-2048 signing keypair and returns the
// parameters needed to persist it as a 'pending' key. The kid is the RFC 7638
// thumbprint of the public key. Activation is a separate, explicit step
// (ActivateSigningKey). The private key is encoded as PKCS#8 PEM and the public key is
// wrapped in a self-signed x509 certificate (CN = kid, ~10 year validity).
func GenerateSigningKey() (db.InsertPendingSigningKeyParams, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return db.InsertPendingSigningKeyParams{}, err
	}

	kid := jwkThumbprint(&priv.PublicKey)

	jwkBytes, err := json.Marshal(publicJWK(kid, &priv.PublicKey))
	if err != nil {
		return db.InsertPendingSigningKeyParams{}, err
	}

	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return db.InsertPendingSigningKeyParams{}, err
	}
	privPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))

	certPEM, err := selfSignedCertPEM(priv, kid)
	if err != nil {
		return db.InsertPendingSigningKeyParams{}, err
	}

	return db.InsertPendingSigningKeyParams{
		Kid:         kid,
		Algorithm:   "RS256",
		PublicJwk:   jwkBytes,
		X509CertPem: pgtype.Text{String: certPEM, Valid: true},
		PrivatePem:  privPEM,
	}, nil
}

// selfSignedCertPEM builds a self-signed x509 certificate over the RSA public
// key with CN = kid, valid for ~10 years, usable for digital signatures, and
// returns it as a PEM-encoded "CERTIFICATE" block.
func selfSignedCertPEM(priv *rsa.PrivateKey, kid string) (string, error) {
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return "", err
	}

	now := time.Now()
	template := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: kid},
		NotBefore:             now,
		NotAfter:              now.AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}

	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), nil
}
