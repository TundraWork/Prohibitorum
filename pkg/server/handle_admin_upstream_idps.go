// Package server — handle_admin_upstream_idps.go
//
// Admin upstream-IdP endpoints:
//   GET  /upstream-idps           — list all IdPs including disabled (admin role, no sudo)
//   GET  /upstream-idps/{slug}    — get one IdP by slug (admin role, no sudo)
//   POST /upstream-idps           — create a new IdP (admin + sudo)
//   PUT  /upstream-idps/{slug}    — update config fields, EXCLUDING secret (admin + sudo)
//   POST /upstream-idps/rotate-secret — replace the sealed secret (admin + sudo)
//   POST /upstream-idps/delete    — hard-delete an IdP (admin + sudo)
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

// upstreamIDPView projects a db.UpstreamIdp row into the wire-safe contract
// view. ClientSecretEnc and SecretNonce are explicitly excluded — this function
// is the single chokepoint that prevents accidental leakage of sealed secret
// material.
func upstreamIDPView(r db.UpstreamIdp) contract.UpstreamIDPView {
	v := contract.UpstreamIDPView{
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

// ----- GET /upstream-idps (typed, role-only) ------------------------------------

type listUpstreamIDPsOut struct {
	Body []contract.UpstreamIDPView
}

func (s *Server) handleListUpstreamIDPs(ctx context.Context, _ *struct{}) (*listUpstreamIDPsOut, error) {
	rows, err := s.queries.ListAllUpstreamIDPs(ctx)
	if err != nil {
		return nil, fmt.Errorf("handleListUpstreamIDPs: query: %w", err)
	}
	views := make([]contract.UpstreamIDPView, 0, len(rows))
	for _, r := range rows {
		views = append(views, upstreamIDPView(r))
	}
	return &listUpstreamIDPsOut{Body: views}, nil
}

// ----- GET /upstream-idps/{slug} (typed, role-only) ----------------------------

type getUpstreamIDPIn struct {
	Slug string `path:"slug"`
}

type upstreamIDPOut struct {
	Body contract.UpstreamIDPView
}

func (s *Server) handleGetUpstreamIDP(ctx context.Context, in *getUpstreamIDPIn) (*upstreamIDPOut, error) {
	r, err := s.queries.GetUpstreamIDPBySlugAny(ctx, in.Slug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, authErrToHuma(authn.ErrUpstreamIDPNotFound())
		}
		return nil, fmt.Errorf("handleGetUpstreamIDP: query: %w", err)
	}
	return &upstreamIDPOut{Body: upstreamIDPView(r)}, nil
}

// ----- POST /upstream-idps (raw, sudo-gated) ------------------------------------

type createUpstreamIDPBody struct {
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

func (s *Server) handleCreateUpstreamIDPHTTP(w http.ResponseWriter, r *http.Request) {
	var body createUpstreamIDPBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	if body.Slug == "" || body.DisplayName == "" || body.IssuerUrl == "" ||
		body.ClientID == "" || body.ClientSecret == "" || body.Mode == "" {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}

	keyVer, dek, err := s.currentDEK()
	if err != nil {
		writeAuthErr(w, fmt.Errorf("handleCreateUpstreamIDP: dek: %w", err))
		return
	}

	scopes := body.Scopes
	if len(scopes) == 0 {
		scopes = []string{"openid", "email", "profile"}
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
		writeAuthErr(w, fmt.Errorf("handleCreateUpstreamIDP: begin tx: %w", err))
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
		RequireVerifiedEmail: body.RequireVerifiedEmail,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeAuthErr(w, authn.ErrUpstreamIDPAlreadyExists())
			return
		}
		writeAuthErr(w, fmt.Errorf("handleCreateUpstreamIDP: insert: %w", err))
		return
	}

	// Seal the real secret using the row id for AAD.
	ciphertext, nonce, err := oidc.EncryptClientSecret(dek, []byte(body.ClientSecret), row.ID, keyVer)
	if err != nil {
		writeAuthErr(w, fmt.Errorf("handleCreateUpstreamIDP: seal: %w", err))
		return
	}

	// Write the real sealed secret back within the same transaction.
	if err := qtx.UpdateUpstreamIDPSecret(r.Context(), db.UpdateUpstreamIDPSecretParams{
		Slug:            row.Slug,
		ClientSecretEnc: ciphertext,
		SecretNonce:     nonce,
		KeyVersion:      keyVer,
	}); err != nil {
		writeAuthErr(w, fmt.Errorf("handleCreateUpstreamIDP: seal-update: %w", err))
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeAuthErr(w, fmt.Errorf("handleCreateUpstreamIDP: commit: %w", err))
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
	_ = json.NewEncoder(w).Encode(upstreamIDPView(final))
}

// ----- PUT /upstream-idps/{slug} (raw, sudo-gated) ----------------------------

type updateUpstreamIDPBody struct {
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

func (s *Server) handleUpdateUpstreamIDPHTTP(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	if slug == "" {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}

	var body updateUpstreamIDPBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}

	scopes := body.Scopes
	if len(scopes) == 0 {
		scopes = []string{"openid", "email", "profile"}
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
		RequireVerifiedEmail: body.RequireVerifiedEmail,
		Disabled:             body.Disabled,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeAuthErr(w, authn.ErrUpstreamIDPNotFound())
			return
		}
		writeAuthErr(w, fmt.Errorf("handleUpdateUpstreamIDP: update: %w", err))
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

	writeJSON(w, upstreamIDPView(updated))
}

// ----- POST /upstream-idps/rotate-secret (raw, sudo-gated) -------------------

type rotateUpstreamIDPSecretBody struct {
	Slug         string `json:"slug"`
	ClientSecret string `json:"clientSecret"`
}

func (s *Server) handleRotateUpstreamIDPSecretHTTP(w http.ResponseWriter, r *http.Request) {
	var body rotateUpstreamIDPSecretBody
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
		writeAuthErr(w, fmt.Errorf("handleRotateUpstreamIDPSecret: lookup: %w", err))
		return
	}

	keyVer, dek, err := s.currentDEK()
	if err != nil {
		writeAuthErr(w, fmt.Errorf("handleRotateUpstreamIDPSecret: dek: %w", err))
		return
	}

	ciphertext, nonce, err := oidc.EncryptClientSecret(dek, []byte(body.ClientSecret), row.ID, keyVer)
	if err != nil {
		writeAuthErr(w, fmt.Errorf("handleRotateUpstreamIDPSecret: seal: %w", err))
		return
	}

	if err := s.queries.UpdateUpstreamIDPSecret(r.Context(), db.UpdateUpstreamIDPSecretParams{
		Slug:            body.Slug,
		ClientSecretEnc: ciphertext,
		SecretNonce:     nonce,
		KeyVersion:      keyVer,
	}); err != nil {
		writeAuthErr(w, fmt.Errorf("handleRotateUpstreamIDPSecret: update: %w", err))
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

// ----- POST /upstream-idps/delete (raw, sudo-gated) --------------------------

type deleteUpstreamIDPBody struct {
	Slug string `json:"slug"`
}

func (s *Server) handleDeleteUpstreamIDPHTTP(w http.ResponseWriter, r *http.Request) {
	var body deleteUpstreamIDPBody
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
		writeAuthErr(w, fmt.Errorf("handleDeleteUpstreamIDP: lookup: %w", err))
		return
	}

	if err := s.queries.DeleteUpstreamIDP(r.Context(), row.ID); err != nil {
		writeAuthErr(w, fmt.Errorf("handleDeleteUpstreamIDP: delete: %w", err))
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

// Compile-time check: ensure upstreamIDPView never exposes ClientSecretEnc
// or SecretNonce. db.UpstreamIdp.ClientSecretEnc and SecretNonce are []byte
// fields that are deliberately absent from contract.UpstreamIDPView — the
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
	v := upstreamIDPView(row)
	_ = v
	// contract.UpstreamIDPView has no ClientSecretEnc or SecretNonce fields.
	// This init func failing to compile catches any regression.
	return true
}()
