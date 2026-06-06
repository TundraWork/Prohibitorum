package oidc

import (
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"testing"
)

func TestKeygenGenerateSigningKey(t *testing.T) {
	params, err := GenerateSigningKey()
	if err != nil {
		t.Fatalf("GenerateSigningKey: %v", err)
	}

	if params.Algorithm != "RS256" {
		t.Errorf("algorithm = %q, want RS256", params.Algorithm)
	}
	// Private PEM must round-trip via the package's PKCS#1/PKCS#8 parser.
	priv, err := parseRSAPrivatePEM(params.PrivatePem)
	if err != nil {
		t.Fatalf("parseRSAPrivatePEM: %v", err)
	}
	if bits := priv.N.BitLen(); bits != 2048 {
		t.Errorf("key size = %d bits, want 2048", bits)
	}

	// kid must equal the RFC 7638 thumbprint of the public key.
	if want := jwkThumbprint(&priv.PublicKey); params.Kid != want {
		t.Errorf("kid = %q, want thumbprint %q", params.Kid, want)
	}

	// public_jwk must unmarshal to a map with kty=RSA.
	var jwk map[string]any
	if err := json.Unmarshal(params.PublicJwk, &jwk); err != nil {
		t.Fatalf("unmarshal public_jwk: %v", err)
	}
	if jwk["kty"] != "RSA" {
		t.Errorf("jwk kty = %v, want RSA", jwk["kty"])
	}
	if jwk["kid"] != params.Kid {
		t.Errorf("jwk kid = %v, want %q", jwk["kid"], params.Kid)
	}

	// x509 cert PEM must decode and parse.
	if !params.X509CertPem.Valid {
		t.Fatalf("X509CertPem.Valid = false, want true")
	}
	block, _ := pem.Decode([]byte(params.X509CertPem.String))
	if block == nil {
		t.Fatalf("x509 cert PEM did not decode")
	}
	if block.Type != "CERTIFICATE" {
		t.Errorf("cert PEM block type = %q, want CERTIFICATE", block.Type)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("x509.ParseCertificate: %v", err)
	}
	if cert.Subject.CommonName != params.Kid {
		t.Errorf("cert CN = %q, want kid %q", cert.Subject.CommonName, params.Kid)
	}
	if cert.KeyUsage&x509.KeyUsageDigitalSignature == 0 {
		t.Errorf("cert KeyUsage missing DigitalSignature")
	}
}

func TestKeygenUnique(t *testing.T) {
	a, err := GenerateSigningKey()
	if err != nil {
		t.Fatalf("first GenerateSigningKey: %v", err)
	}
	b, err := GenerateSigningKey()
	if err != nil {
		t.Fatalf("second GenerateSigningKey: %v", err)
	}
	if a.Kid == b.Kid {
		t.Errorf("two generated keys share kid %q", a.Kid)
	}
}
