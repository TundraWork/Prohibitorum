package oidc

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

func (p *Provider) signJWT(ctx context.Context, claims map[string]any, typ string) (string, error) {
	k, ok := p.keys.signingKey(ctx)
	if !ok {
		return "", errors.New("oidc: no active signing key")
	}
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: k.private},
		(&jose.SignerOptions{}).WithType(jose.ContentType(typ)).WithHeader("kid", k.kid),
	)
	if err != nil {
		return "", err
	}
	return jwt.Signed(signer).Claims(claims).Serialize()
}

func (p *Provider) verifyJWT(ctx context.Context, token string) (map[string]any, error) {
	parsed, err := jwt.ParseSigned(token, []jose.SignatureAlgorithm{jose.RS256})
	if err != nil {
		return nil, fmt.Errorf("oidc: parse jwt: %w", err)
	}
	if len(parsed.Headers) != 1 {
		return nil, errors.New("oidc: unexpected JOSE header count")
	}
	k, ok := p.keys.byKID(ctx, parsed.Headers[0].KeyID)
	if !ok {
		return nil, fmt.Errorf("oidc: unknown kid %q", parsed.Headers[0].KeyID)
	}
	var claims map[string]any
	if err := parsed.Claims(k.public, &claims); err != nil {
		return nil, fmt.Errorf("oidc: verify jwt: %w", err)
	}
	return claims, nil
}
