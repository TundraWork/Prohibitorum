package oidc

import (
	"context"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"sync"
	"time"

	"prohibitorum/pkg/db"
)

const keyCacheTTL = 5 * time.Minute

type signingKeyQueries interface {
	ListPublishableSigningKeys(ctx context.Context) ([]db.SigningKey, error)
}

type cachedKey struct {
	kid     string
	status  string
	public  *rsa.PublicKey
	private *rsa.PrivateKey
	jwk     map[string]any
}

type keyCache struct {
	q        signingKeyQueries
	mu       sync.RWMutex
	keys     []cachedKey
	loadedAt time.Time
	clockNow func() time.Time
}

func newKeyCache(q signingKeyQueries) *keyCache {
	return &keyCache{q: q, clockNow: time.Now}
}

func (c *keyCache) refresh(ctx context.Context) error {
	rows, err := c.q.ListPublishableSigningKeys(ctx)
	if err != nil {
		return err
	}
	parsed := make([]cachedKey, 0, len(rows))
	for _, r := range rows {
		priv, perr := parseRSAPrivatePEM(r.PrivatePem)
		if perr != nil {
			return perr
		}
		var jwk map[string]any
		_ = json.Unmarshal(r.PublicJwk, &jwk)
		parsed = append(parsed, cachedKey{
			kid: r.Kid, status: r.Status,
			public: &priv.PublicKey, private: priv, jwk: jwk,
		})
	}
	c.mu.Lock()
	c.keys = parsed
	c.loadedAt = c.clockNow()
	c.mu.Unlock()
	return nil
}

func (c *keyCache) maybeRefresh(ctx context.Context) {
	c.mu.RLock()
	stale := c.clockNow().Sub(c.loadedAt) > keyCacheTTL || len(c.keys) == 0
	c.mu.RUnlock()
	if stale {
		_ = c.refresh(ctx)
	}
}

func (c *keyCache) signingKey(ctx context.Context) (cachedKey, bool) {
	c.maybeRefresh(ctx)
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, k := range c.keys {
		if k.status == "active" {
			return k, true
		}
	}
	return cachedKey{}, false
}

func (c *keyCache) byKID(ctx context.Context, kid string) (cachedKey, bool) {
	c.maybeRefresh(ctx)
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, k := range c.keys {
		if k.kid == kid {
			return k, true
		}
	}
	return cachedKey{}, false
}

func (c *keyCache) jwks(ctx context.Context) map[string]any {
	c.maybeRefresh(ctx)
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]map[string]any, 0, len(c.keys))
	for _, k := range c.keys {
		out = append(out, k.jwk)
	}
	return map[string]any{"keys": out}
}

func publicJWK(kid string, pub *rsa.PublicKey) map[string]any {
	return map[string]any{
		"kty": "RSA", "use": "sig", "alg": "RS256", "kid": kid,
		"n": base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
		"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
	}
}

// jwkThumbprint computes the RFC 7638 SHA-256 thumbprint of an RSA public
// key. The canonical JSON must contain exactly the required members in
// lexicographic order with no whitespace, hence the hand-built string.
func jwkThumbprint(pub *rsa.PublicKey) string {
	n := base64.RawURLEncoding.EncodeToString(pub.N.Bytes())
	e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes())
	canonical := `{"e":"` + e + `","kty":"RSA","n":"` + n + `"}`
	sum := sha256.Sum256([]byte(canonical))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func parseRSAPrivatePEM(pemStr string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, errors.New("oidc: invalid private PEM")
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
		return nil, errors.New("oidc: PEM is not an RSA private key")
	}
	return rk, nil
}
