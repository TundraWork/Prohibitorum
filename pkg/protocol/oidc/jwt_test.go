package oidc

import (
	"context"
	"strings"
	"testing"

	"prohibitorum/pkg/db"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

func TestJWTRoundTrip(t *testing.T) {
	row, _ := testSigningKeyRow(t)
	p := &Provider{keys: newKeyCache(&fakeSigningKeyQueries{rows: []db.SigningKey{row}})}
	ctx := context.Background()

	token, err := p.signJWT(ctx, map[string]any{"sub": "x", "iss": "y"}, "JWT")
	if err != nil {
		t.Fatalf("signJWT: %v", err)
	}

	claims, err := p.verifyJWT(ctx, token)
	if err != nil {
		t.Fatalf("verifyJWT: %v", err)
	}
	if claims["sub"] != "x" {
		t.Fatalf("sub = %v, want x", claims["sub"])
	}
	if claims["iss"] != "y" {
		t.Fatalf("iss = %v, want y", claims["iss"])
	}

	// Inspect the JOSE header: typ and kid.
	parsed, err := jwt.ParseSigned(token, []jose.SignatureAlgorithm{jose.RS256})
	if err != nil {
		t.Fatalf("parse header: %v", err)
	}
	if len(parsed.Headers) != 1 {
		t.Fatalf("header count = %d, want 1", len(parsed.Headers))
	}
	if parsed.Headers[0].KeyID != row.Kid {
		t.Fatalf("header kid = %q, want %q", parsed.Headers[0].KeyID, row.Kid)
	}
	if typ, _ := parsed.Headers[0].ExtraHeaders[jose.HeaderType].(string); typ != "JWT" {
		t.Fatalf("header typ = %q, want JWT", typ)
	}
}

func TestJWTRejectsHS256(t *testing.T) {
	row, _ := testSigningKeyRow(t)
	p := &Provider{keys: newKeyCache(&fakeSigningKeyQueries{rows: []db.SigningKey{row}})}

	// Sign an HS256 token by hand with a symmetric key.
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.HS256, Key: []byte("0123456789abcdef0123456789abcdef")},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", row.Kid),
	)
	if err != nil {
		t.Fatalf("new HS256 signer: %v", err)
	}
	hsToken, err := jwt.Signed(signer).Claims(map[string]any{"sub": "x"}).Serialize()
	if err != nil {
		t.Fatalf("sign HS256: %v", err)
	}

	if _, err := p.verifyJWT(context.Background(), hsToken); err == nil {
		t.Fatal("verifyJWT: expected rejection of HS256 token")
	}
}

func TestJWTRejectsUnknownKID(t *testing.T) {
	row, _ := testSigningKeyRow(t)
	p := &Provider{keys: newKeyCache(&fakeSigningKeyQueries{rows: []db.SigningKey{row}})}
	ctx := context.Background()

	// A different provider with a different key forges a token whose kid
	// is unknown to p.
	otherRow, _ := testSigningKeyRow(t)
	other := &Provider{keys: newKeyCache(&fakeSigningKeyQueries{rows: []db.SigningKey{otherRow}})}
	token, err := other.signJWT(ctx, map[string]any{"sub": "x"}, "JWT")
	if err != nil {
		t.Fatalf("signJWT: %v", err)
	}

	_, err = p.verifyJWT(ctx, token)
	if err == nil {
		t.Fatal("verifyJWT: expected unknown-kid rejection")
	}
	if !strings.Contains(err.Error(), "unknown kid") {
		t.Fatalf("error = %v, want 'unknown kid'", err)
	}
}
