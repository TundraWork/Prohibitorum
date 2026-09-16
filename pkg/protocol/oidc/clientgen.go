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
// OIDC client via the CLI or the admin API.
type ClientOptions struct {
	ClientID               string
	DisplayName            string
	RedirectURIs           []string
	PostLogoutRedirectURIs []string
	Scopes                 []string
	Public                 bool
	RequireConsent         bool
	// RequirePKCE records the operator's require_pkce choice. Callers must
	// fill it explicitly; a public client may not set it to false.
	RequirePKCE bool
}

// BuildClientParams builds the DB insert params for a new OIDC client.
//
// For a confidential client (the default) it generates a 32-byte secret,
// returns the plaintext (to be shown once) and stores only the argon2id PHC
// hash, and sets client_auth_method to "client_secret" — which accepts the
// secret through either the Basic header or the request body. For a public
// client (Public=true) there is no secret and client_auth_method is "none".
// A public client cannot opt out of PKCE; a confidential client follows
// opts.RequirePKCE (new clients default to requiring it).
func BuildClientParams(opts ClientOptions) (db.InsertOIDCClientParams, string, error) {
	if opts.ClientID == "" {
		return db.InsertOIDCClientParams{}, "", errors.New("client-id is required")
	}
	if len(opts.RedirectURIs) == 0 {
		return db.InsertOIDCClientParams{}, "", errors.New("at least one redirect-uri is required")
	}
	// A public client's only code protection is PKCE, so opting out is not a
	// configuration error the DB CHECK should have to catch: refuse it where
	// both the CLI and the admin API create path meet.
	if opts.Public && !opts.RequirePKCE {
		return db.InsertOIDCClientParams{}, "", errors.New("a public client cannot opt out of PKCE")
	}

	scopes := opts.Scopes
	if len(scopes) == 0 {
		scopes = []string{"openid", "profile"}
	}
	// Downstream allowed_scopes must stay inside the OP's closed vocabulary —
	// a scope the OP has no handling logic for is meaningless. This covers both
	// the admin API and the CLI create path (the admin handler also pre-checks).
	if err := ValidateAllowedScopes(scopes); err != nil {
		return db.InsertOIDCClientParams{}, "", err
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
		RequirePkce:                 opts.RequirePKCE,
		AllowedCodeChallengeMethods: []string{"S256"},
		SubjectType:                 "public",
		RequireConsent:              opts.RequireConsent,
	}

	if opts.Public {
		params.ClientSecretHash = pgtype.Text{Valid: false}
		params.ClientAuthMethod = "none"
		return params, "", nil
	}

	secret, hash, err := generateClientSecret()
	if err != nil {
		return db.InsertOIDCClientParams{}, "", err
	}
	params.ClientSecretHash = pgtype.Text{String: hash, Valid: true}
	params.ClientAuthMethod = "client_secret"

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
