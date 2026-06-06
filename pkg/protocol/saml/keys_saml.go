package saml

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"sync"
	"time"

	"prohibitorum/pkg/db"
)

// keyCacheTTL mirrors the OIDC signing-key cache refresh interval. It is
// defined locally so the saml package does not import oidc.
const keyCacheTTL = 5 * time.Minute

// samlSigningKeyQueries is the narrow slice of db.Querier the cache needs.
// ListPublishableSigningKeys returns all use='sig' keys with status in
// (pending,active,decommissioning); the status='active' one is the signer, the
// whole set is the metadata cert list (so a rotation does not break verifiers).
type samlSigningKeyQueries interface {
	ListPublishableSigningKeys(ctx context.Context) ([]db.SigningKey, error)
}

// samlCachedKey holds the material the SAML layer needs per signing key: the
// RSA private key (for signing assertions/metadata), the cert in DER form (for
// signElement, which embeds <ds:X509Certificate>), and the parsed cert (for
// metadata rendering and verifyElementSignature).
type samlCachedKey struct {
	kid     string
	status  string
	private *rsa.PrivateKey
	certDER []byte
	cert    *x509.Certificate
}

// samlKeyCache is a SAML-local mirror of the OIDC keyCache: it reloads the
// active signing keys from the DB every keyCacheTTL and exposes the active
// signer plus the full non-retired cert set for metadata.
type samlKeyCache struct {
	q        samlSigningKeyQueries
	mu       sync.RWMutex
	keys     []samlCachedKey
	loadedAt time.Time
	clockNow func() time.Time
}

func newSAMLKeyCache(q samlSigningKeyQueries) *samlKeyCache {
	return &samlKeyCache{q: q, clockNow: time.Now}
}

func (c *samlKeyCache) refresh(ctx context.Context) error {
	rows, err := c.q.ListPublishableSigningKeys(ctx)
	if err != nil {
		return err
	}
	parsed := make([]samlCachedKey, 0, len(rows))
	for _, r := range rows {
		priv, perr := parseRSAPrivatePEM(r.PrivatePem)
		if perr != nil {
			return perr
		}
		ck := samlCachedKey{kid: r.Kid, status: r.Status, private: priv}
		if r.X509CertPem.Valid && r.X509CertPem.String != "" {
			block, _ := pem.Decode([]byte(r.X509CertPem.String))
			if block == nil {
				return errors.New("saml: invalid X509 cert PEM")
			}
			cert, cerr := x509.ParseCertificate(block.Bytes)
			if cerr != nil {
				return cerr
			}
			ck.certDER = block.Bytes
			ck.cert = cert
		}
		parsed = append(parsed, ck)
	}
	c.mu.Lock()
	c.keys = parsed
	c.loadedAt = c.clockNow()
	c.mu.Unlock()
	return nil
}

func (c *samlKeyCache) maybeRefresh(ctx context.Context) {
	c.mu.RLock()
	stale := c.clockNow().Sub(c.loadedAt) > keyCacheTTL || len(c.keys) == 0
	c.mu.RUnlock()
	if stale {
		_ = c.refresh(ctx)
	}
}

// signingKey returns the active signer: its RSA private key, the cert DER (for
// signElement), and the kid. ok is false if no active key is loaded, OR if the
// active key has no usable cert. SAML signing REQUIRES a cert: signElement
// embeds <ds:X509Certificate>, and a signature carrying an empty cert is one no
// verifier accepts. Guarding here (rather than in refresh) keeps a malformed
// NON-active key from poisoning the whole cache — fail closed only on the
// active signer.
func (c *samlKeyCache) signingKey(ctx context.Context) (priv *rsa.PrivateKey, certDER []byte, kid string, ok bool) {
	c.maybeRefresh(ctx)
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, k := range c.keys {
		if k.status == "active" && len(k.certDER) > 0 {
			return k.private, k.certDER, k.kid, true
		}
	}
	return nil, nil, "", false
}

// allCerts returns all non-retired signing certs for the IdP metadata
// document. Keys without a parsed cert are skipped.
func (c *samlKeyCache) allCerts(ctx context.Context) []*x509.Certificate {
	c.maybeRefresh(ctx)
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]*x509.Certificate, 0, len(c.keys))
	for _, k := range c.keys {
		if k.cert != nil {
			out = append(out, k.cert)
		}
	}
	return out
}

// parseRSAPrivatePEM is duplicated from the oidc package (per the v0.5 plan's
// no-cross-package-coupling decision). It accepts PKCS#1 or PKCS#8 PEM.
func parseRSAPrivatePEM(pemStr string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, errors.New("saml: invalid private PEM")
	}
	if k, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return k, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	rk, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("saml: PEM is not an RSA private key")
	}
	return rk, nil
}
