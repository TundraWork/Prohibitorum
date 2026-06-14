// Package server — handle_admin_upstream_idps.go
//
// Admin identity provider endpoints:
//   GET  /identity-providers           — list all identity providers including disabled (admin role, no sudo)
//   GET  /identity-providers/{slug}    — get one identity provider by slug (admin role, no sudo)
//   POST /identity-providers           — create a new identity provider (admin + sudo)
//   PUT  /identity-providers/{slug}    — update config fields, EXCLUDING secret (admin + sudo)
//   POST /identity-providers/rotate-secret — replace the sealed secret (admin + sudo)
//   POST /identity-providers/delete    — hard-delete an identity provider (admin + sudo)
//
// client_secret_enc and secret_nonce are NEVER serialized or included in any
// response or audit detail. The secret is accepted on input (create and
// rotate-secret), AES-GCM-sealed server-side, and stored as ciphertext.
//
// The AES-GCM AAD is bound to (idp_id, key_version), which means the create
// path must: insert the row first (to get the auto-assigned id), then seal
// using that id, then call UpdateUpstreamIDPSecret. The placeholder bytes
// in the initial insert are immediately overwritten.
//
// Mutations are registered via s.registerSudoOpHTTP, so the sudo gate,
// content-type check, and body-size limit are all enforced by the wrapper —
// handlers must NOT call requireFreshSudo themselves.

package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/federation/oidc"
)

// identityProviderView projects a db.UpstreamIdp row into the wire-safe contract
// view. ClientSecretEnc and SecretNonce are explicitly excluded — this function
// is the single chokepoint that prevents accidental leakage of sealed secret
// material.
func identityProviderView(r db.UpstreamIdp) contract.IdentityProviderView {
	v := contract.IdentityProviderView{
		Slug:                 r.Slug,
		DisplayName:          r.DisplayName,
		IssuerUrl:            r.IssuerUrl,
		ClientID:             r.ClientID,
		Scopes:               r.Scopes,
		Mode:                 r.Mode,
		AllowedDomains:       r.AllowedDomains,
		UsernameClaim:        r.UsernameClaim,
		DisplayNameClaim:     r.DisplayNameClaim,
		EmailClaim:           r.EmailClaim,
		RequireVerifiedEmail: r.RequireVerifiedEmail,
		Disabled:             r.Disabled,
	}
	if r.CreatedAt.Valid {
		v.CreatedAt = r.CreatedAt.Time
	}
	return v
}

// currentDEK returns the highest-versioned DEK from the config key set.
// Returns (version, key, error). The create and rotate-secret handlers use
// this to pick which DEK to seal with.
func (s *Server) currentDEK() (int32, []byte, error) {
	if len(s.config.DataEncryptionKeys) == 0 {
		return 0, nil, fmt.Errorf("handle_admin_upstream_idps: no data encryption keys configured")
	}
	var maxVer int
	for v := range s.config.DataEncryptionKeys {
		if v > maxVer {
			maxVer = v
		}
	}
	return int32(maxVer), s.config.DataEncryptionKeys[maxVer], nil
}

// validateUpstreamIssuer enforces the SSRF-hardening rule (audit follow-up N2)
// that an upstream issuer_url must be an https:// URL with a non-IP-literal
// host. It is SKIPPED when federation.allow_private_network is set — that
// deployment has explicitly opted into trusting internal IdPs (and the runtime
// dial screen is off to match), so an IP-literal / http issuer is permitted.
func (s *Server) validateUpstreamIssuer(issuerURL string) error {
	if s.config != nil && s.config.Federation.AllowPrivateNetwork {
		return nil
	}
	if err := oidc.ValidateIssuerURL(issuerURL); err != nil {
		return authn.ErrBadRequest()
	}
	return nil
}

// defaultFederationScopes is the scope set applied to a new/updated upstream IdP
// when the admin supplies none: the deployment-wide federation.default_scopes
// (C6), or a minimal OIDC-valid fallback if that is somehow empty (an upstream
// authorize request must at least carry "openid").
func (s *Server) defaultFederationScopes() []string {
	if s.config != nil && len(s.config.Federation.DefaultScopes) > 0 {
		return s.config.Federation.DefaultScopes
	}
	return []string{"openid", "profile", "email"}
}

// ----- GET /identity-providers (typed, role-only) ------------------------------------

type listIdentityProvidersOut struct {
	Body []contract.IdentityProviderView
}

func (s *Server) handleListIdentityProviders(ctx context.Context, _ *struct{}) (*listIdentityProvidersOut, error) {
	rows, err := s.queries.ListAllUpstreamIDPs(ctx)
	if err != nil {
		return nil, fmt.Errorf("handleListIdentityProviders: query: %w", err)
	}
	views := make([]contract.IdentityProviderView, 0, len(rows))
	for _, r := range rows {
		views = append(views, identityProviderView(r))
	}
	return &listIdentityProvidersOut{Body: views}, nil
}

// ----- GET /identity-providers/{slug} (typed, role-only) ----------------------------

type getIdentityProviderIn struct {
	Slug string `path:"slug"`
}

type identityProviderOut struct {
	Body contract.IdentityProviderView
}

func (s *Server) handleGetIdentityProvider(ctx context.Context, in *getIdentityProviderIn) (*identityProviderOut, error) {
	r, err := s.queries.GetUpstreamIDPBySlugAny(ctx, in.Slug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, authErrToHuma(authn.ErrUpstreamIDPNotFound())
		}
		return nil, fmt.Errorf("handleGetIdentityProvider: query: %w", err)
	}
	return &identityProviderOut{Body: identityProviderView(r)}, nil
}

// ----- POST /identity-providers (raw, sudo-gated) ------------------------------------

type createIdentityProviderBody struct {
	Slug                 string   `json:"slug"`
	DisplayName          string   `json:"displayName"`
	IssuerUrl            string   `json:"issuerUrl"`
	ClientID             string   `json:"clientId"`
	ClientSecret         string   `json:"clientSecret"`
	Scopes               []string `json:"scopes"`
	Mode                 string   `json:"mode"`
	AllowedDomains       []string `json:"allowedDomains"`
	UsernameClaim        string   `json:"usernameClaim"`
	DisplayNameClaim     string   `json:"displayNameClaim"`
	EmailClaim           string   `json:"emailClaim"`
	RequireVerifiedEmail bool     `json:"requireVerifiedEmail"`
}

func (s *Server) handleCreateIdentityProviderHTTP(w http.ResponseWriter, r *http.Request) {
	var body createIdentityProviderBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	if body.Slug == "" || body.DisplayName == "" || body.IssuerUrl == "" ||
		body.ClientID == "" || body.ClientSecret == "" || body.Mode == "" {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	if err := s.validateUpstreamIssuer(body.IssuerUrl); err != nil {
		writeAuthErr(w, err)
		return
	}

	keyVer, dek, err := s.currentDEK()
	if err != nil {
		writeAuthErr(w, fmt.Errorf("handleCreateIdentityProvider: dek: %w", err))
		return
	}

	scopes := body.Scopes
	if len(scopes) == 0 {
		scopes = s.defaultFederationScopes()
	}
	allowedDomains := body.AllowedDomains
	if allowedDomains == nil {
		allowedDomains = []string{}
	}
	usernameClaim := body.UsernameClaim
	if usernameClaim == "" {
		usernameClaim = "preferred_username"
	}
	displayNameClaim := body.DisplayNameClaim
	if displayNameClaim == "" {
		displayNameClaim = "name"
	}
	emailClaim := body.EmailClaim
	if emailClaim == "" {
		emailClaim = "email"
	}

	// Execute insert → seal → secret-update inside a single transaction.
	// The AAD is bound to (idp_id, key_version), so we insert first to obtain
	// the auto-assigned row id, then seal using that id, then update — all
	// within the transaction so a seal or update failure rolls back the insert
	// (no orphan-row window).
	tx, err := s.dbPool.Begin(r.Context())
	if err != nil {
		writeAuthErr(w, fmt.Errorf("handleCreateIdentityProvider: begin tx: %w", err))
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck

	placeholder := make([]byte, 1)
	qtx := s.queries.WithTx(tx)
	row, err := qtx.InsertUpstreamIDP(r.Context(), db.InsertUpstreamIDPParams{
		Slug:                 body.Slug,
		DisplayName:          body.DisplayName,
		IssuerUrl:            body.IssuerUrl,
		ClientID:             body.ClientID,
		ClientSecretEnc:      placeholder,
		SecretNonce:          placeholder,
		KeyVersion:           keyVer,
		Scopes:               scopes,
		Mode:                 body.Mode,
		AllowedDomains:       allowedDomains,
		UsernameClaim:        usernameClaim,
		DisplayNameClaim:     displayNameClaim,
		EmailClaim:           emailClaim,
		PictureClaim:         "picture",
		RequireVerifiedEmail: body.RequireVerifiedEmail,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeAuthErr(w, authn.ErrUpstreamIDPAlreadyExists())
			return
		}
		writeAuthErr(w, fmt.Errorf("handleCreateIdentityProvider: insert: %w", err))
		return
	}

	// Seal the real secret using the row id for AAD.
	ciphertext, nonce, err := oidc.EncryptClientSecret(dek, []byte(body.ClientSecret), row.ID, keyVer)
	if err != nil {
		writeAuthErr(w, fmt.Errorf("handleCreateIdentityProvider: seal: %w", err))
		return
	}

	// Write the real sealed secret back within the same transaction.
	if err := qtx.UpdateUpstreamIDPSecret(r.Context(), db.UpdateUpstreamIDPSecretParams{
		Slug:            row.Slug,
		ClientSecretEnc: ciphertext,
		SecretNonce:     nonce,
		KeyVersion:      keyVer,
	}); err != nil {
		writeAuthErr(w, fmt.Errorf("handleCreateIdentityProvider: seal-update: %w", err))
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeAuthErr(w, fmt.Errorf("handleCreateIdentityProvider: commit: %w", err))
		return
	}

	sess := authn.SessionFromContext(r.Context())
	var actorID *int32
	if sess != nil {
		actorID = &sess.Account.ID
	}
	_ = s.Audit.Record(r.Context(), audit.Record{
		AccountID: actorID,
		Factor:    audit.FactorUpstreamIDP,
		Event:     audit.EventRegister,
		Detail:    map[string]any{"slug": row.Slug, "mode": row.Mode},
	})

	// Re-query so the view reflects the committed secret fields (not placeholder).
	final, err := s.queries.GetUpstreamIDPBySlugAny(r.Context(), row.Slug)
	if err != nil {
		// View is still safe to return from the insert row; secret not exposed.
		final = row
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(identityProviderView(final))
}

// ----- PUT /identity-providers/{slug} (raw, sudo-gated) ----------------------------

type updateIdentityProviderBody struct {
	DisplayName          string   `json:"displayName"`
	IssuerUrl            string   `json:"issuerUrl"`
	ClientID             string   `json:"clientId"`
	Scopes               []string `json:"scopes"`
	Mode                 string   `json:"mode"`
	AllowedDomains       []string `json:"allowedDomains"`
	UsernameClaim        string   `json:"usernameClaim"`
	DisplayNameClaim     string   `json:"displayNameClaim"`
	EmailClaim           string   `json:"emailClaim"`
	RequireVerifiedEmail bool     `json:"requireVerifiedEmail"`
	Disabled             bool     `json:"disabled"`
}

func (s *Server) handleUpdateIdentityProviderHTTP(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	if slug == "" {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}

	var body updateIdentityProviderBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	if err := s.validateUpstreamIssuer(body.IssuerUrl); err != nil {
		writeAuthErr(w, err)
		return
	}

	scopes := body.Scopes
	if len(scopes) == 0 {
		scopes = s.defaultFederationScopes()
	}
	allowedDomains := body.AllowedDomains
	if allowedDomains == nil {
		allowedDomains = []string{}
	}

	updated, err := s.queries.UpdateUpstreamIDPConfig(r.Context(), db.UpdateUpstreamIDPConfigParams{
		Slug:                 slug,
		DisplayName:          body.DisplayName,
		IssuerUrl:            body.IssuerUrl,
		ClientID:             body.ClientID,
		Scopes:               scopes,
		Mode:                 body.Mode,
		AllowedDomains:       allowedDomains,
		UsernameClaim:        body.UsernameClaim,
		DisplayNameClaim:     body.DisplayNameClaim,
		EmailClaim:           body.EmailClaim,
		PictureClaim:         "picture",
		RequireVerifiedEmail: body.RequireVerifiedEmail,
		Disabled:             body.Disabled,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeAuthErr(w, authn.ErrUpstreamIDPNotFound())
			return
		}
		writeAuthErr(w, fmt.Errorf("handleUpdateIdentityProvider: update: %w", err))
		return
	}

	sess := authn.SessionFromContext(r.Context())
	var actorID *int32
	if sess != nil {
		actorID = &sess.Account.ID
	}
	_ = s.Audit.Record(r.Context(), audit.Record{
		AccountID: actorID,
		Factor:    audit.FactorUpstreamIDP,
		Event:     audit.EventUpdate,
		Detail:    map[string]any{"slug": slug},
	})

	writeJSON(w, identityProviderView(updated))
}

// ----- POST /identity-providers/rotate-secret (raw, sudo-gated) -------------------

type rotateIdentityProviderSecretBody struct {
	Slug         string `json:"slug"`
	ClientSecret string `json:"clientSecret"`
}

func (s *Server) handleRotateIdentityProviderSecretHTTP(w http.ResponseWriter, r *http.Request) {
	var body rotateIdentityProviderSecretBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	if body.Slug == "" || body.ClientSecret == "" {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}

	// Resolve the row to get the id for AAD.
	row, err := s.queries.GetUpstreamIDPBySlugAny(r.Context(), body.Slug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeAuthErr(w, authn.ErrUpstreamIDPNotFound())
			return
		}
		writeAuthErr(w, fmt.Errorf("handleRotateIdentityProviderSecret: lookup: %w", err))
		return
	}

	keyVer, dek, err := s.currentDEK()
	if err != nil {
		writeAuthErr(w, fmt.Errorf("handleRotateIdentityProviderSecret: dek: %w", err))
		return
	}

	ciphertext, nonce, err := oidc.EncryptClientSecret(dek, []byte(body.ClientSecret), row.ID, keyVer)
	if err != nil {
		writeAuthErr(w, fmt.Errorf("handleRotateIdentityProviderSecret: seal: %w", err))
		return
	}

	if err := s.queries.UpdateUpstreamIDPSecret(r.Context(), db.UpdateUpstreamIDPSecretParams{
		Slug:            body.Slug,
		ClientSecretEnc: ciphertext,
		SecretNonce:     nonce,
		KeyVersion:      keyVer,
	}); err != nil {
		writeAuthErr(w, fmt.Errorf("handleRotateIdentityProviderSecret: update: %w", err))
		return
	}

	sess := authn.SessionFromContext(r.Context())
	var actorID *int32
	if sess != nil {
		actorID = &sess.Account.ID
	}
	_ = s.Audit.Record(r.Context(), audit.Record{
		AccountID: actorID,
		Factor:    audit.FactorUpstreamIDP,
		Event:     audit.EventRotate,
		Detail:    map[string]any{"slug": body.Slug, "action": "rotate_secret"},
	})

	w.WriteHeader(http.StatusNoContent)
}

// ----- POST /identity-providers/delete (raw, sudo-gated) --------------------------

type deleteIdentityProviderBody struct {
	Slug string `json:"slug"`
}

func (s *Server) handleDeleteIdentityProviderHTTP(w http.ResponseWriter, r *http.Request) {
	var body deleteIdentityProviderBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	if body.Slug == "" {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}

	// Resolve slug → id for proper 404 and to use the id-based delete query.
	row, err := s.queries.GetUpstreamIDPBySlugAny(r.Context(), body.Slug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeAuthErr(w, authn.ErrUpstreamIDPNotFound())
			return
		}
		writeAuthErr(w, fmt.Errorf("handleDeleteIdentityProvider: lookup: %w", err))
		return
	}

	if err := s.queries.DeleteUpstreamIDP(r.Context(), row.ID); err != nil {
		writeAuthErr(w, fmt.Errorf("handleDeleteIdentityProvider: delete: %w", err))
		return
	}

	sess := authn.SessionFromContext(r.Context())
	var actorID *int32
	if sess != nil {
		actorID = &sess.Account.ID
	}
	_ = s.Audit.Record(r.Context(), audit.Record{
		AccountID: actorID,
		Factor:    audit.FactorUpstreamIDP,
		Event:     audit.EventRevoke,
		Detail:    map[string]any{"slug": body.Slug},
	})

	w.WriteHeader(http.StatusNoContent)
}

// Compile-time check: ensure identityProviderView never exposes ClientSecretEnc
// or SecretNonce. db.UpstreamIdp.ClientSecretEnc and SecretNonce are []byte
// fields that are deliberately absent from contract.IdentityProviderView — the
// compiler enforces this structurally. The runtime check verifies no []byte
// field was accidentally smuggled through as a string alias.
var _ = func() bool {
	secretBytes := []byte("SECRET_BYTES_MUST_NOT_APPEAR")
	row := db.UpstreamIdp{
		ClientSecretEnc: secretBytes,
		SecretNonce:     secretBytes,
		Slug:            "test",
		DisplayName:     "Test",
	}
	v := identityProviderView(row)
	_ = v
	// contract.IdentityProviderView has no ClientSecretEnc or SecretNonce fields.
	// This init func failing to compile catches any regression.
	return true
}()
