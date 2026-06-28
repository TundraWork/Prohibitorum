// Package server — handle_admin_forward_auth_apps.go
//
// Admin forward-auth application endpoints. A forward-auth app is an oidc_client
// with forward_auth_enabled=true; it is presented as its own section and
// excluded from the OIDC-applications list (see handle_admin_oidc_clients.go).
// Per-service RBAC reuses the OIDC app-access endpoints
// (/oidc-applications/{clientId}/access/*) unchanged.
//
// Reads are typed (registerOp); mutations are raw and sudo-gated via
// registerSudoOpHTTP (create/update/delete) — except set-disabled which mirrors
// the OIDC set-disabled (admin-only, no sudo). Handlers must NOT call
// requireFreshSudo themselves.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
	oidc "prohibitorum/pkg/protocol/oidc"
)

// faScopeNameRe requires a scope name to start AND end with an alphanumeric;
// dot/dash/underscore/colon are allowed only internally. This rejects leading or
// trailing separators (e.g. "-bad", "a.", ":bad") while still accepting
// "repo:read", "a.b-c", and single-char names.
var faScopeNameRe = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9._:-]*[a-zA-Z0-9])?$`)

// validateFAScopes returns normalized scopes (or an error) — names must match
// the label pattern and be unique, and descriptions are capped. nil/empty is
// valid (no vocabulary).
func validateFAScopes(in []contract.ForwardAuthScope) ([]contract.ForwardAuthScope, error) {
	seen := map[string]bool{}
	out := make([]contract.ForwardAuthScope, 0, len(in))
	for _, sc := range in {
		name := strings.TrimSpace(sc.Name)
		if name == "" || len(name) > 64 || !faScopeNameRe.MatchString(name) || seen[name] {
			return nil, authn.ErrBadRequest()
		}
		desc := strings.TrimSpace(sc.Description)
		if len(desc) > 256 {
			return nil, authn.ErrBadRequest()
		}
		seen[name] = true
		out = append(out, contract.ForwardAuthScope{Name: name, Description: desc})
	}
	return out, nil
}

// forwardAuthAppView projects the common FA columns into the wire view. Shared
// by every FA row shape (list/get/update) since they select the same columns.
func forwardAuthAppView(clientID, displayName string, host pgtype.Text, scopesJSON []byte, accessRestricted, disabled bool, createdAt pgtype.Timestamptz) contract.ForwardAuthAppView {
	v := contract.ForwardAuthAppView{
		ClientID:         clientID,
		DisplayName:      displayName,
		ForwardAuthHost:  host.String, // "" when !Valid
		Scopes:           parseFAScopes(scopesJSON),
		AccessRestricted: accessRestricted,
		Disabled:         disabled,
	}
	if createdAt.Valid {
		v.CreatedAt = createdAt.Time
	}
	return v
}

func faActorID(ctx context.Context) *int32 {
	if sess := authn.SessionFromContext(ctx); sess != nil {
		return &sess.Account.ID
	}
	return nil
}

// ----- GET /forward-auth-apps (typed, role-only) -----------------------------

type listForwardAuthAppsOut struct {
	Body []contract.ForwardAuthAppView
}

func (s *Server) handleListForwardAuthApps(ctx context.Context, _ *struct{}) (*listForwardAuthAppsOut, error) {
	rows, err := s.queries.ListForwardAuthClients(ctx)
	if err != nil {
		return nil, fmt.Errorf("handler: listForwardAuthApps: %w", err)
	}
	views := make([]contract.ForwardAuthAppView, 0, len(rows))
	for _, r := range rows {
		views = append(views, forwardAuthAppView(r.ClientID, r.DisplayName, r.ForwardAuthHost, r.ForwardAuthScopes, r.AccessRestricted, r.Disabled, r.CreatedAt))
	}
	return &listForwardAuthAppsOut{Body: views}, nil
}

// ----- GET /forward-auth-apps/{clientId} (typed, role-only) ------------------

type getForwardAuthAppIn struct {
	ClientID string `path:"clientId"`
}

type forwardAuthAppOut struct {
	Body contract.ForwardAuthAppView
}

func (s *Server) handleGetForwardAuthApp(ctx context.Context, in *getForwardAuthAppIn) (*forwardAuthAppOut, error) {
	r, err := s.queries.GetForwardAuthAppByID(ctx, in.ClientID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, authErrToHuma(authn.ErrClientNotFound())
		}
		return nil, fmt.Errorf("handleGetForwardAuthApp: %w", err)
	}
	view := forwardAuthAppView(r.ClientID, r.DisplayName, r.ForwardAuthHost, r.ForwardAuthScopes, r.AccessRestricted, r.Disabled, r.CreatedAt)
	view.IconURL = entityIconURLPtr("oidc_client", r.ClientID, s.lookupEntityIconEtag(ctx, "oidc_client", r.ClientID))
	return &forwardAuthAppOut{Body: view}, nil
}

// ----- POST /forward-auth-apps (raw, sudo-gated) -----------------------------

type createForwardAuthAppBody struct {
	ClientID    string                   `json:"clientId"`
	Host        string                   `json:"host"`
	DisplayName string                   `json:"displayName"`
	Scopes      []contract.ForwardAuthScope `json:"scopes"`
}

func (s *Server) handleCreateForwardAuthAppHTTP(w http.ResponseWriter, r *http.Request) {
	var body createForwardAuthAppBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	if body.ClientID == "" || body.Host == "" {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}

	validated, err := validateFAScopes(body.Scopes)
	if err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	scopesJSON, _ := json.Marshal(validated)

	c, err := oidc.RegisterForwardAuthApp(r.Context(), s.queries, body.ClientID, body.Host, body.DisplayName)
	if err != nil {
		if isUniqueViolation(err) {
			writeAuthErr(w, authn.ErrClientAlreadyExists())
			return
		}
		writeAuthErr(w, fmt.Errorf("handleCreateForwardAuthApp: %w", err))
		return
	}

	// The scope vocabulary is written as a third, non-transactional write after
	// RegisterForwardAuthApp's own two writes. A transient failure here leaves a
	// fully-created forward-auth app with an empty scope vocabulary, which the
	// admin can recover by saving scopes again via the FA-app PUT.
	if err := s.queries.SetForwardAuthScopes(r.Context(), db.SetForwardAuthScopesParams{
		ClientID:          body.ClientID,
		ForwardAuthScopes: scopesJSON,
	}); err != nil {
		writeAuthErr(w, fmt.Errorf("handleCreateForwardAuthApp: set scopes: %w", err))
		return
	}

	_ = s.Audit.Record(r.Context(), audit.Record{
		AccountID: faActorID(r.Context()),
		Factor:    audit.FactorOIDCClient,
		Event:     audit.EventRegister,
		Detail:    map[string]any{"client_id": body.ClientID, "forward_auth": true, "host": body.Host},
	})

	// c is the full OidcClient returned by InsertOIDCClient (before the FA
	// flag/host update is applied by RegisterForwardAuthApp's SetForwardAuthConfig
	// call). Build the view from known create-time values: a fresh FA app is
	// never access-restricted and is enabled.
	view := forwardAuthAppView(c.ClientID, c.DisplayName, pgtype.Text{String: body.Host, Valid: true}, scopesJSON, false, c.Disabled, c.CreatedAt)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(view)
}

// ----- PUT /forward-auth-apps/{clientId} (raw, sudo-gated) -------------------

type updateForwardAuthAppBody struct {
	DisplayName string                      `json:"displayName"`
	Host        string                      `json:"host"`
	Scopes      []contract.ForwardAuthScope `json:"scopes"`
}

func (s *Server) handleUpdateForwardAuthAppHTTP(w http.ResponseWriter, r *http.Request) {
	clientID := chi.URLParam(r, "clientId")
	if clientID == "" {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	var body updateForwardAuthAppBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	if body.Host == "" {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}

	validated, err := validateFAScopes(body.Scopes)
	if err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	scopesJSON, _ := json.Marshal(validated)

	row, err := s.queries.UpdateForwardAuthApp(r.Context(), db.UpdateForwardAuthAppParams{
		ClientID:          clientID,
		DisplayName:       body.DisplayName,
		RedirectUris:      []string{oidc.ForwardAuthCallbackURI(body.Host)},
		ForwardAuthHost:   pgtype.Text{String: body.Host, Valid: true},
		ForwardAuthScopes: scopesJSON,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeAuthErr(w, authn.ErrClientNotFound())
			return
		}
		if isUniqueViolation(err) {
			writeAuthErr(w, authn.ErrClientAlreadyExists())
			return
		}
		writeAuthErr(w, fmt.Errorf("handleUpdateForwardAuthApp: %w", err))
		return
	}

	_ = s.Audit.Record(r.Context(), audit.Record{
		AccountID: faActorID(r.Context()),
		Factor:    audit.FactorOIDCClient,
		Event:     audit.EventUpdate,
		Detail:    map[string]any{"client_id": clientID, "forward_auth": true, "host": body.Host},
	})

	writeJSON(w, forwardAuthAppView(row.ClientID, row.DisplayName, row.ForwardAuthHost, row.ForwardAuthScopes, row.AccessRestricted, row.Disabled, row.CreatedAt))
}

// ----- POST /forward-auth-apps/set-disabled (raw, admin-only, no sudo) -------

type setForwardAuthAppDisabledBody struct {
	ClientID string `json:"clientId"`
	Disabled bool   `json:"disabled"`
}

func (s *Server) handleSetForwardAuthAppDisabledHTTP(w http.ResponseWriter, r *http.Request) {
	var body setForwardAuthAppDisabledBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	if body.ClientID == "" {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	// Guard: only operate on a forward-auth app.
	if _, err := s.queries.GetForwardAuthAppByID(r.Context(), body.ClientID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeAuthErr(w, authn.ErrClientNotFound())
			return
		}
		writeAuthErr(w, fmt.Errorf("handleSetForwardAuthAppDisabled: lookup: %w", err))
		return
	}

	c, err := s.queries.SetOIDCClientDisabled(r.Context(), db.SetOIDCClientDisabledParams{
		ClientID: body.ClientID,
		Disabled: body.Disabled,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeAuthErr(w, authn.ErrClientNotFound())
			return
		}
		writeAuthErr(w, fmt.Errorf("handleSetForwardAuthAppDisabled: update: %w", err))
		return
	}

	_ = s.Audit.Record(r.Context(), audit.Record{
		AccountID: faActorID(r.Context()),
		Factor:    audit.FactorOIDCClient,
		Event:     audit.EventUpdate,
		Detail:    map[string]any{"client_id": body.ClientID, "forward_auth": true, "disabled": body.Disabled},
	})

	// SetOIDCClientDisabled returns a full OidcClient; project only FA fields.
	writeJSON(w, forwardAuthAppView(c.ClientID, c.DisplayName, c.ForwardAuthHost, c.ForwardAuthScopes, c.AccessRestricted, c.Disabled, c.CreatedAt))
}

// ----- POST /forward-auth-apps/delete (raw, sudo-gated) ----------------------

type deleteForwardAuthAppBody struct {
	ClientID string `json:"clientId"`
}

func (s *Server) handleDeleteForwardAuthAppHTTP(w http.ResponseWriter, r *http.Request) {
	var body deleteForwardAuthAppBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	if body.ClientID == "" {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	// Guard: ensure it's a forward-auth app before dropping the backing client.
	if _, err := s.queries.GetForwardAuthAppByID(r.Context(), body.ClientID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeAuthErr(w, authn.ErrClientNotFound())
			return
		}
		writeAuthErr(w, fmt.Errorf("handleDeleteForwardAuthApp: lookup: %w", err))
		return
	}

	rows, err := s.queries.DeleteOIDCClient(r.Context(), body.ClientID)
	if err != nil {
		writeAuthErr(w, fmt.Errorf("handleDeleteForwardAuthApp: delete: %w", err))
		return
	}
	if rows == 0 {
		writeAuthErr(w, authn.ErrClientNotFound())
		return
	}

	// Remove the entity icon row if one was uploaded. Errors are silently
	// ignored — the icon is orphaned data, not a consistency risk.
	_ = s.queries.DeleteEntityIcon(r.Context(), db.DeleteEntityIconParams{
		OwnerKind: "oidc_client", OwnerID: body.ClientID,
	})

	_ = s.Audit.Record(r.Context(), audit.Record{
		AccountID: faActorID(r.Context()),
		Factor:    audit.FactorOIDCClient,
		Event:     audit.EventRevoke,
		Detail:    map[string]any{"client_id": body.ClientID, "forward_auth": true},
	})

	w.WriteHeader(http.StatusNoContent)
}
