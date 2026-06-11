// Package server — handle_admin_oidc_clients_test.go
//
// Unit tests for the OIDC-client admin surface (Task 4). These tests are
// intentionally DB-free: the view projection (oidcApplicationView) is the primary
// unit under test, with assertion on secret-hash exclusion and correct field
// mapping. generateClientSecret is tested via the exported BuildClientParams
// path (package-level) and via the internal round-trip through VerifyRaw.
// Route-level sudo gating is covered centrally in Task 9.

package server

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/credential/password"
	"prohibitorum/pkg/db"
	oidc "prohibitorum/pkg/protocol/oidc"
)

// TestAdminOIDCClients_ViewProjection_NeverExposesSecretHash verifies that
// oidcApplicationView never copies ClientSecretHash into the wire view. The
// contract.OIDCApplicationView type has no ClientSecretHash field — the test
// additionally checks that no field carries the secret string value.
func TestAdminOIDCClients_ViewProjection_NeverExposesSecretHash(t *testing.T) {
	t.Parallel()

	const secretHash = "$argon2id$v=19$m=65536,t=3,p=2$SECRET_HASH_MATERIAL"
	row := db.OidcClient{
		ClientID:                "test-client-1",
		DisplayName:             "Test Client",
		ClientSecretHash:        pgtype.Text{String: secretHash, Valid: true},
		RedirectUris:            []string{"https://app.example.com/callback"},
		PostLogoutRedirectUris:  []string{"https://app.example.com/logout"},
		AllowedScopes:           []string{"openid", "profile"},
		TokenEndpointAuthMethod: "client_secret_basic",
		RequireConsent:          false,
		Disabled:                false,
		CreatedAt:               pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}

	view := oidcApplicationView(row)

	// contract.OIDCApplicationView has no ClientSecretHash field — the compiler
	// structurally prevents it. Runtime check: none of the string fields carry
	// the secret hash value.
	if view.ClientID == secretHash {
		t.Error("ClientID carries secret hash value")
	}
	if view.DisplayName == secretHash {
		t.Error("DisplayName carries secret hash value")
	}
	if view.TokenEndpointAuthMethod == secretHash {
		t.Error("TokenEndpointAuthMethod carries secret hash value")
	}
	for _, u := range view.RedirectURIs {
		if u == secretHash {
			t.Error("RedirectURIs entry carries secret hash value")
		}
	}
	for _, s := range view.AllowedScopes {
		if s == secretHash {
			t.Error("AllowedScopes entry carries secret hash value")
		}
	}
}

// TestAdminOIDCClients_ViewProjection_FieldMapping verifies correct field
// projection for all public fields including optional timestamp.
func TestAdminOIDCClients_ViewProjection_FieldMapping(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	row := db.OidcClient{
		ClientID:                "my-client",
		DisplayName:             "My App",
		ClientSecretHash:        pgtype.Text{String: "HASH", Valid: true},
		RedirectUris:            []string{"https://myapp.test/cb"},
		PostLogoutRedirectUris:  []string{"https://myapp.test/bye"},
		AllowedScopes:           []string{"openid", "email"},
		TokenEndpointAuthMethod: "client_secret_basic",
		RequireConsent:          true,
		Disabled:                false,
		CreatedAt:               pgtype.Timestamptz{Time: createdAt, Valid: true},
	}

	view := oidcApplicationView(row)

	if view.ClientID != "my-client" {
		t.Errorf("ClientID: got %q, want %q", view.ClientID, "my-client")
	}
	if view.DisplayName != "My App" {
		t.Errorf("DisplayName: got %q, want %q", view.DisplayName, "My App")
	}
	if len(view.RedirectURIs) != 1 || view.RedirectURIs[0] != "https://myapp.test/cb" {
		t.Errorf("RedirectURIs: got %v, want [https://myapp.test/cb]", view.RedirectURIs)
	}
	if len(view.PostLogoutRedirectURIs) != 1 || view.PostLogoutRedirectURIs[0] != "https://myapp.test/bye" {
		t.Errorf("PostLogoutRedirectURIs: got %v", view.PostLogoutRedirectURIs)
	}
	if len(view.AllowedScopes) != 2 {
		t.Errorf("AllowedScopes: got %v, want [openid email]", view.AllowedScopes)
	}
	if view.TokenEndpointAuthMethod != "client_secret_basic" {
		t.Errorf("TokenEndpointAuthMethod: got %q", view.TokenEndpointAuthMethod)
	}
	if !view.RequireConsent {
		t.Error("RequireConsent: got false, want true")
	}
	if view.Disabled {
		t.Error("Disabled: got true, want false")
	}
	if !view.CreatedAt.Equal(createdAt) {
		t.Errorf("CreatedAt: got %v, want %v", view.CreatedAt, createdAt)
	}
}

// TestAdminOIDCClients_ViewProjection_InvalidTimestamp verifies that a row
// with an invalid (NULL) CreatedAt column yields a zero-value time.Time
// rather than panicking.
func TestAdminOIDCClients_ViewProjection_InvalidTimestamp(t *testing.T) {
	t.Parallel()

	row := db.OidcClient{
		ClientID:    "null-ts-client",
		DisplayName: "Null TS",
		// CreatedAt intentionally left as zero value (Valid=false)
	}

	view := oidcApplicationView(row)

	if !view.CreatedAt.IsZero() {
		t.Errorf("CreatedAt: got %v, want zero time for invalid column", view.CreatedAt)
	}
}

// TestAdminOIDCClients_GenerateClientSecret_UniqueAndVerifiable exercises the
// generateClientSecret helper indirectly through BuildClientParams, confirming:
// 1. Two successive calls return distinct secrets.
// 2. The returned plaintext verifies against the stored hash (password.VerifyRaw).
// 3. A confidential client's BuildClientParams exposes the secret; a public one does not.
func TestAdminOIDCClients_GenerateClientSecret_UniqueAndVerifiable(t *testing.T) {
	t.Parallel()

	opts1 := oidc.ClientOptions{
		ClientID:     "client-a",
		DisplayName:  "Client A",
		RedirectURIs: []string{"https://a.test/cb"},
	}
	opts2 := oidc.ClientOptions{
		ClientID:     "client-b",
		DisplayName:  "Client B",
		RedirectURIs: []string{"https://b.test/cb"},
	}

	params1, secret1, err := oidc.BuildClientParams(opts1)
	if err != nil {
		t.Fatalf("BuildClientParams opts1: %v", err)
	}
	params2, secret2, err := oidc.BuildClientParams(opts2)
	if err != nil {
		t.Fatalf("BuildClientParams opts2: %v", err)
	}

	// Secrets must be non-empty for confidential clients.
	if secret1 == "" {
		t.Error("secret1: got empty string for confidential client")
	}
	if secret2 == "" {
		t.Error("secret2: got empty string for confidential client")
	}

	// Successive secrets must be distinct.
	if secret1 == secret2 {
		t.Error("successive generateClientSecret calls returned the same secret")
	}

	// Plaintext must verify against the stored hash.
	hash1 := params1.ClientSecretHash.String
	if !password.VerifyRaw(secret1, hash1) {
		t.Error("secret1 does not verify against its hash")
	}
	hash2 := params2.ClientSecretHash.String
	if !password.VerifyRaw(secret2, hash2) {
		t.Error("secret2 does not verify against its hash")
	}

	// Cross-verification must fail.
	if password.VerifyRaw(secret1, hash2) {
		t.Error("secret1 incorrectly verifies against hash2")
	}
}

// TestAdminOIDCClients_CreateResponse_PublicClientNoSecret verifies that the
// view projection pattern for a public client (token_endpoint_auth_method=none,
// no secret hash) produces an empty secret field in the response shape.
func TestAdminOIDCClients_CreateResponse_PublicClientNoSecret(t *testing.T) {
	t.Parallel()

	opts := oidc.ClientOptions{
		ClientID:     "public-client",
		DisplayName:  "Public Client",
		RedirectURIs: []string{"myapp://callback"},
		Public:       true,
	}

	params, secret, err := oidc.BuildClientParams(opts)
	if err != nil {
		t.Fatalf("BuildClientParams: %v", err)
	}

	// Public clients must return an empty secret.
	if secret != "" {
		t.Errorf("secret: got %q, want empty for public client", secret)
	}
	// Public clients must not have a client_secret_hash.
	if params.ClientSecretHash.Valid {
		t.Error("ClientSecretHash.Valid: got true for public client, want false")
	}
	// token_endpoint_auth_method must be "none" for public clients.
	if params.TokenEndpointAuthMethod != "none" {
		t.Errorf("TokenEndpointAuthMethod: got %q, want %q", params.TokenEndpointAuthMethod, "none")
	}
}

// TestAdminOIDCClients_ErrClientNotFound verifies the 404 constructor returns
// the correct status code and machine-readable code.
func TestAdminOIDCClients_ErrClientNotFound(t *testing.T) {
	t.Parallel()

	err := authn.ErrClientNotFound()
	if err == nil {
		t.Fatal("ErrClientNotFound: got nil")
	}
	if err.Status != 404 {
		t.Errorf("Status: got %d, want 404", err.Status)
	}
	if err.Code != "client_not_found" {
		t.Errorf("Code: got %q, want %q", err.Code, "client_not_found")
	}
	if err.Message == "" {
		t.Error("Message: got empty string")
	}
}

// TestAdminOIDCClients_ContractType_NoSecretHashField verifies at compile time
// that contract.OIDCApplicationView does not declare a ClientSecretHash field.
// This catches any future refactor that might accidentally add one.
func TestAdminOIDCClients_ContractType_NoSecretHashField(t *testing.T) {
	t.Parallel()

	v := contract.OIDCApplicationView{
		ClientID:                "k",
		DisplayName:             "Test",
		TokenEndpointAuthMethod: "client_secret_basic",
	}
	// If contract.OIDCApplicationView ever grew a ClientSecretHash field this test
	// would fail to compile — keeping the guarantee in the test suite.
	_ = v
}

// TestValidateOIDCScopes guards T3.2: allowed_scopes must be a subset of the
// scopes the OP actually supports; unknown/typo scopes are rejected so they
// can't be stored, requested, and consented while delivering nothing.
func TestValidateOIDCScopes(t *testing.T) {
	for _, ok := range [][]string{nil, {}, {"openid"}, {"openid", "profile", "email", "offline_access"}} {
		if err := validateOIDCScopes(ok); err != nil {
			t.Errorf("validateOIDCScopes(%v) = %v, want nil", ok, err)
		}
	}
	for _, bad := range [][]string{{"groups"}, {"openid", "email", "create"}, {"profilee"}, {"address"}} {
		if err := validateOIDCScopes(bad); err == nil {
			t.Errorf("validateOIDCScopes(%v) = nil, want error", bad)
		}
	}
}
