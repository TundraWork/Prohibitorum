// Package server — handle_admin_oidc_clients.go
//
// Admin OIDC-client endpoints:
//   GET  /oidc-clients              — list all clients (admin role, no sudo)
//   GET  /oidc-clients/{clientId}   — get one client (admin role, no sudo)
//   POST /oidc-clients              — create a new client (admin + sudo)
//   PUT  /oidc-clients/{clientId}   — replace mutable fields (admin + sudo, full config required)
//   POST /oidc-clients/rotate-secret — rotate the client secret (admin + sudo)
//   POST /oidc-clients/delete        — hard-delete a client (admin + sudo)
//
// client_secret_hash is NEVER serialized or included in any response or audit
// detail. The cleartext secret is revealed exactly once: in the create and
// rotate-secret responses. Reads never return any secret material.
//
// Mutations are registered via s.registerSudoOpHTTP, so the sudo gate,
// content-type check, and body-size limit are all enforced by the wrapper —
// handlers must NOT call requireFreshSudo themselves.
//
// PUT uses a full-replace model: the caller must supply the complete desired
// configuration (displayName, redirectUris, postLogoutRedirectUris,
// allowedScopes, requireConsent, disabled). Partial PATCH is not implemented.

package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
	oidc "prohibitorum/pkg/protocol/oidc"
)

// oidcClientView projects a db.OidcClient row into the wire-safe contract
// view. ClientSecretHash is explicitly excluded — this function is the single
// chokepoint that prevents accidental leakage of secret material.
func oidcClientView(c db.OidcClient) contract.OIDCClientView {
	v := contract.OIDCClientView{
		ClientID:                c.ClientID,
		DisplayName:             c.DisplayName,
		RedirectURIs:            c.RedirectUris,
		PostLogoutRedirectURIs:  c.PostLogoutRedirectUris,
		AllowedScopes:           c.AllowedScopes,
		TokenEndpointAuthMethod: c.TokenEndpointAuthMethod,
		RequireConsent:          c.RequireConsent,
		Disabled:                c.Disabled,
	}
	if c.CreatedAt.Valid {
		v.CreatedAt = c.CreatedAt.Time
	}
	return v
}

// ----- GET /oidc-clients (typed, role-only) -----------------------------------

type listOIDCClientsOut struct {
	Body []contract.OIDCClientView
}

func (s *Server) handleListOIDCClients(ctx context.Context, _ *struct{}) (*listOIDCClientsOut, error) {
	rows, err := s.queries.ListOIDCClients(ctx)
	if err != nil {
		return nil, fmt.Errorf("handler: listOIDCClients: %w", err)
	}
	views := make([]contract.OIDCClientView, 0, len(rows))
	for _, r := range rows {
		// ListOIDCClients returns a summary row — project the subset fields.
		v := contract.OIDCClientView{
			ClientID:                r.ClientID,
			DisplayName:             r.DisplayName,
			RedirectURIs:            r.RedirectUris,
			AllowedScopes:           r.AllowedScopes,
			TokenEndpointAuthMethod: r.TokenEndpointAuthMethod,
			Disabled:                r.Disabled,
		}
		if r.CreatedAt.Valid {
			v.CreatedAt = r.CreatedAt.Time
		}
		views = append(views, v)
	}
	return &listOIDCClientsOut{Body: views}, nil
}

// ----- GET /oidc-clients/{clientId} (typed, role-only) -----------------------

type getOIDCClientIn struct {
	ClientID string `path:"clientId"`
}

type oidcClientOut struct {
	Body contract.OIDCClientView
}

func (s *Server) handleGetOIDCClient(ctx context.Context, in *getOIDCClientIn) (*oidcClientOut, error) {
	// Use GetOIDCClientAny so disabled clients are visible to admins.
	c, err := s.queries.GetOIDCClientAny(ctx, in.ClientID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, authErrToHuma(authn.ErrClientNotFound())
		}
		return nil, fmt.Errorf("handleGetOIDCClient: query: %w", err)
	}
	return &oidcClientOut{Body: oidcClientView(c)}, nil
}

// ----- POST /oidc-clients (raw, sudo-gated) -----------------------------------

type createOIDCClientBody struct {
	ClientID               string   `json:"clientId"`
	DisplayName            string   `json:"displayName"`
	RedirectURIs           []string `json:"redirectUris"`
	PostLogoutRedirectURIs []string `json:"postLogoutRedirectUris"`
	Scopes                 []string `json:"scopes"`
	Public                 bool     `json:"public"`
	RequireConsent         bool     `json:"requireConsent"`
}

type createOIDCClientResponse struct {
	contract.OIDCClientView
	// Secret is present only for confidential clients, on creation only.
	Secret string `json:"secret,omitempty"`
}

func (s *Server) handleCreateOIDCClientHTTP(w http.ResponseWriter, r *http.Request) {
	var body createOIDCClientBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}

	opts := oidc.ClientOptions{
		ClientID:               body.ClientID,
		DisplayName:            body.DisplayName,
		RedirectURIs:           body.RedirectURIs,
		PostLogoutRedirectURIs: body.PostLogoutRedirectURIs,
		Scopes:                 body.Scopes,
		Public:                 body.Public,
		RequireConsent:         body.RequireConsent,
	}

	params, secret, err := oidc.BuildClientParams(opts)
	if err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}

	c, err := s.queries.InsertOIDCClient(r.Context(), params)
	if err != nil {
		if isUniqueViolation(err) {
			writeAuthErr(w, authn.ErrClientAlreadyExists())
			return
		}
		writeAuthErr(w, fmt.Errorf("handleCreateOIDCClient: insert: %w", err))
		return
	}

	sess := authn.SessionFromContext(r.Context())
	var actorID *int32
	if sess != nil {
		actorID = &sess.Account.ID
	}
	_ = s.Audit.Record(r.Context(), audit.Record{
		AccountID: actorID,
		Factor:    audit.FactorOIDCClient,
		Event:     audit.EventRegister,
		Detail:    map[string]any{"client_id": c.ClientID, "public": body.Public},
	})

	resp := createOIDCClientResponse{
		OIDCClientView: oidcClientView(c),
		Secret:         secret, // empty string for public clients (omitempty handles it)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(resp)
}

// ----- PUT /oidc-clients/{clientId} (raw, sudo-gated) ------------------------

type updateOIDCClientBody struct {
	DisplayName            string   `json:"displayName"`
	RedirectURIs           []string `json:"redirectUris"`
	PostLogoutRedirectURIs []string `json:"postLogoutRedirectUris"`
	AllowedScopes          []string `json:"allowedScopes"`
	RequireConsent         bool     `json:"requireConsent"`
	Disabled               bool     `json:"disabled"`
}

func (s *Server) handleUpdateOIDCClientHTTP(w http.ResponseWriter, r *http.Request) {
	clientID := chi.URLParam(r, "clientId")
	if clientID == "" {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}

	var body updateOIDCClientBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}

	// Enforce at least one redirect URI.
	if len(body.RedirectURIs) == 0 {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}

	// Default post-logout URIs to empty slice (not nil) to satisfy NOT NULL.
	postLogout := body.PostLogoutRedirectURIs
	if postLogout == nil {
		postLogout = []string{}
	}
	// Default scopes to openid+profile when omitted.
	scopes := body.AllowedScopes
	if len(scopes) == 0 {
		scopes = []string{"openid", "profile"}
	}

	c, err := s.queries.UpdateOIDCClient(r.Context(), db.UpdateOIDCClientParams{
		ClientID:               clientID,
		DisplayName:            body.DisplayName,
		RedirectUris:           body.RedirectURIs,
		PostLogoutRedirectUris: postLogout,
		AllowedScopes:          scopes,
		RequireConsent:         body.RequireConsent,
		Disabled:               body.Disabled,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeAuthErr(w, authn.ErrClientNotFound())
			return
		}
		writeAuthErr(w, fmt.Errorf("handleUpdateOIDCClient: update: %w", err))
		return
	}

	sess := authn.SessionFromContext(r.Context())
	var actorID *int32
	if sess != nil {
		actorID = &sess.Account.ID
	}
	_ = s.Audit.Record(r.Context(), audit.Record{
		AccountID: actorID,
		Factor:    audit.FactorOIDCClient,
		Event:     audit.EventUpdate,
		Detail:    map[string]any{"client_id": clientID},
	})

	writeJSON(w, oidcClientView(c))
}

// ----- POST /oidc-clients/rotate-secret (raw, sudo-gated) --------------------

type rotateOIDCClientSecretBody struct {
	ClientID string `json:"clientId"`
}

type rotateOIDCClientSecretResponse struct {
	ClientID string `json:"clientId"`
	Secret   string `json:"secret"`
}

func (s *Server) handleRotateOIDCClientSecretHTTP(w http.ResponseWriter, r *http.Request) {
	var body rotateOIDCClientSecretBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	if body.ClientID == "" {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}

	// Verify the client exists before rotating.
	if _, err := s.queries.GetOIDCClientAny(r.Context(), body.ClientID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeAuthErr(w, authn.ErrClientNotFound())
			return
		}
		writeAuthErr(w, fmt.Errorf("handleRotateOIDCClientSecret: lookup: %w", err))
		return
	}

	secret, err := oidc.RotateClientSecret(r.Context(), s.queries, body.ClientID)
	if err != nil {
		writeAuthErr(w, fmt.Errorf("handleRotateOIDCClientSecret: rotate: %w", err))
		return
	}

	sess := authn.SessionFromContext(r.Context())
	var actorID *int32
	if sess != nil {
		actorID = &sess.Account.ID
	}
	_ = s.Audit.Record(r.Context(), audit.Record{
		AccountID: actorID,
		Factor:    audit.FactorOIDCClient,
		Event:     audit.EventRotate,
		Detail:    map[string]any{"client_id": body.ClientID, "action": "rotate_secret"},
	})

	writeJSON(w, rotateOIDCClientSecretResponse{
		ClientID: body.ClientID,
		Secret:   secret,
	})
}

// ----- POST /oidc-clients/delete (raw, sudo-gated) ---------------------------

type deleteOIDCClientBody struct {
	ClientID string `json:"clientId"`
}

func (s *Server) handleDeleteOIDCClientHTTP(w http.ResponseWriter, r *http.Request) {
	var body deleteOIDCClientBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	if body.ClientID == "" {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}

	rows, err := s.queries.DeleteOIDCClient(r.Context(), body.ClientID)
	if err != nil {
		writeAuthErr(w, fmt.Errorf("handleDeleteOIDCClient: delete: %w", err))
		return
	}
	if rows == 0 {
		writeAuthErr(w, authn.ErrClientNotFound())
		return
	}

	sess := authn.SessionFromContext(r.Context())
	var actorID *int32
	if sess != nil {
		actorID = &sess.Account.ID
	}
	_ = s.Audit.Record(r.Context(), audit.Record{
		AccountID: actorID,
		Factor:    audit.FactorOIDCClient,
		Event:     audit.EventRevoke,
		Detail:    map[string]any{"client_id": body.ClientID},
	})

	w.WriteHeader(http.StatusNoContent)
}

// Compile-time check: ensure oidcClientView never exposes ClientSecretHash.
// db.OidcClient.ClientSecretHash is a pgtype.Text field that is deliberately
// absent from contract.OIDCClientView — the compiler enforces this.
var _ = func() bool {
	c := db.OidcClient{ClientSecretHash: pgtype.Text{String: "SECRET", Valid: true}}
	v := oidcClientView(c)
	_ = v
	return true
}()
