package oidc

import (
	"crypto/rand"
	"encoding/base64"
	"errors"

	"prohibitorum/pkg/credential/password"
	"prohibitorum/pkg/db"

	"github.com/jackc/pgx/v5/pgtype"
)

// ClientOptions describes the operator-supplied inputs for registering a new
// OIDC client via the CLI.
type ClientOptions struct {
	ClientID               string
	DisplayName            string
	RedirectURIs           []string
	PostLogoutRedirectURIs []string
	Scopes                 []string
	Public                 bool
	RequireConsent         bool
}

// BuildClientParams builds the DB insert params for a new OIDC client.
//
// For a confidential client (the default) it generates a 32-byte secret,
// returns the plaintext (to be shown once) and stores only the argon2id PHC
// hash. For a public client (Public=true) there is no secret and the token
// endpoint auth method is "none". PKCE is required for every client.
func BuildClientParams(opts ClientOptions) (db.InsertOIDCClientParams, string, error) {
	if opts.ClientID == "" {
		return db.InsertOIDCClientParams{}, "", errors.New("client-id is required")
	}
	if len(opts.RedirectURIs) == 0 {
		return db.InsertOIDCClientParams{}, "", errors.New("at least one redirect-uri is required")
	}

	scopes := opts.Scopes
	if len(scopes) == 0 {
		scopes = []string{"openid", "profile"}
	}

	// Default post_logout_redirect_uris to an empty slice. A nil slice marshals
	// to SQL NULL via pgx, which violates the post_logout_redirect_uris NOT NULL
	// constraint (column DEFAULT '{}'); an explicit empty slice matches the
	// default and lets `oidc-client create` succeed when the CLI flag is omitted.
	// RedirectURIs is validated non-empty above and Scopes is defaulted, so only
	// the post-logout list has the nil-on-omit hazard.
	postLogout := opts.PostLogoutRedirectURIs
	if postLogout == nil {
		postLogout = []string{}
	}

	params := db.InsertOIDCClientParams{
		ClientID:                    opts.ClientID,
		DisplayName:                 opts.DisplayName,
		RedirectUris:                opts.RedirectURIs,
		PostLogoutRedirectUris:      postLogout,
		AllowedScopes:               scopes,
		RequirePkce:                 true,
		AllowedCodeChallengeMethods: []string{"S256"},
		SubjectType:                 "public",
		RequireConsent:              opts.RequireConsent,
	}

	if opts.Public {
		params.ClientSecretHash = pgtype.Text{Valid: false}
		params.TokenEndpointAuthMethod = "none"
		return params, "", nil
	}

	secret, hash, err := generateClientSecret()
	if err != nil {
		return db.InsertOIDCClientParams{}, "", err
	}
	params.ClientSecretHash = pgtype.Text{String: hash, Valid: true}
	params.TokenEndpointAuthMethod = "client_secret_basic"

	return params, secret, nil
}

// generateClientSecret generates a 32-byte random client secret, returns the
// plaintext (to be revealed once) and the argon2id PHC hash (to be stored).
// Shared between BuildClientParams (create) and RotateClientSecret (rotate).
func generateClientSecret() (secret, hash string, err error) {
	buf := make([]byte, 32)
	if _, err = rand.Read(buf); err != nil {
		return "", "", err
	}
	secret = base64.RawURLEncoding.EncodeToString(buf)
	hash, err = password.HashRaw(secret, password.DefaultParams())
	if err != nil {
		return "", "", err
	}
	return secret, hash, nil
}
