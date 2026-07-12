// Package server — handle_admin_app_access.go
//
// Admin app-access endpoints for OIDC clients and SAML SPs:
//   GET  /oidc-applications/{clientId}/access          — read access state (admin, no sudo)
//   POST /oidc-applications/{clientId}/access/set-restricted — toggle access_restricted (admin, no sudo)
//   POST /oidc-applications/{clientId}/access/grant    — grant group/account access (admin, no sudo)
//   POST /oidc-applications/{clientId}/access/revoke   — revoke group/account access (admin, no sudo)
//
//   GET  /saml-applications/{id}/access                — read access state (admin, no sudo)
//   POST /saml-applications/{id}/access/set-restricted — toggle access_restricted (admin, no sudo)
//   POST /saml-applications/{id}/access/grant          — grant group/account access (admin, no sudo)
//   POST /saml-applications/{id}/access/revoke         — revoke group/account access (admin, no sudo)
//
// Mutations are registered via s.registerAdminBodyOpHTTP — content-type check
// and body-size limit are enforced by the wrapper (no sudo gate; access
// grants/restrictions are reversible per api.md). Handlers must NOT call
// requireFreshSudo themselves.

package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/db"
)

// ----- shared body types -------------------------------------------------------

// accessPrincipalBody is the decoded request body for grant and revoke endpoints.
// principalKind must be "group" or "account"; principalId is the int32 ID.
type accessPrincipalBody struct {
	PrincipalKind string `json:"principalKind"`
	PrincipalID   int32  `json:"principalId"`
}

// setAccessRestrictedBody is the decoded request body for set-restricted.
type setAccessRestrictedBody struct {
	Restricted bool `json:"restricted"`
}

// ----- GET /oidc-applications/{clientId}/access — paginated in handle_nested_pagination.go ----

// ----- POST /oidc-applications/{clientId}/access/set-restricted (raw, sudo-gated) ----

func (s *Server) handleSetOIDCClientAccessRestrictedHTTP(w http.ResponseWriter, r *http.Request) {
	clientID := chi.URLParam(r, "clientId")
	if clientID == "" {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}

	var body setAccessRestrictedBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}

	c, err := s.queries.SetOIDCClientAccessRestricted(r.Context(), db.SetOIDCClientAccessRestrictedParams{
		ClientID:         clientID,
		AccessRestricted: body.Restricted,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeAuthErr(w, authn.ErrClientNotFound())
			return
		}
		writeAuthErr(w, fmt.Errorf("handleSetOIDCClientAccessRestricted: update: %w", err))
		return
	}

	sess := authn.SessionFromContext(r.Context())
	var actorID *int32
	if sess != nil {
		actorID = &sess.Account.ID
	}
	audit.RecordOrLog(r.Context(), s.Audit, audit.Record{
		AccountID: actorID,
		Factor:    audit.FactorOIDCClient,
		Event:     audit.EventAccessRestrictedSet,
		Detail:    map[string]any{"client_id": clientID, "restricted": body.Restricted},
	})

	writeJSON(w, oidcApplicationView(c))
}

// ----- POST /oidc-applications/{clientId}/access/grant (raw, sudo-gated) ----------

func (s *Server) handleGrantOIDCClientAccessHTTP(w http.ResponseWriter, r *http.Request) {
	clientID := chi.URLParam(r, "clientId")
	if clientID == "" {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}

	var body accessPrincipalBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}

	principalID := pgtype.Int4{Int32: body.PrincipalID, Valid: true}

	switch body.PrincipalKind {
	case "group":
		if err := s.queries.GrantOIDCClientAccessGroup(r.Context(), db.GrantOIDCClientAccessGroupParams{
			ClientID: clientID,
			GroupID:  principalID,
		}); err != nil {
			writeAuthErr(w, fmt.Errorf("handleGrantOIDCClientAccess: grant group: %w", err))
			return
		}
	case "account":
		if err := s.queries.GrantOIDCClientAccessAccount(r.Context(), db.GrantOIDCClientAccessAccountParams{
			ClientID:  clientID,
			AccountID: principalID,
		}); err != nil {
			writeAuthErr(w, fmt.Errorf("handleGrantOIDCClientAccess: grant account: %w", err))
			return
		}
	default:
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}

	sess := authn.SessionFromContext(r.Context())
	var actorID *int32
	if sess != nil {
		actorID = &sess.Account.ID
	}
	audit.RecordOrLog(r.Context(), s.Audit, audit.Record{
		AccountID: actorID,
		Factor:    audit.FactorOIDCClient,
		Event:     audit.EventAccessGranted,
		Detail:    map[string]any{"client_id": clientID, "principal_kind": body.PrincipalKind, "principal_id": body.PrincipalID},
	})

	w.WriteHeader(http.StatusNoContent)
}

// ----- POST /oidc-applications/{clientId}/access/revoke (raw, sudo-gated) ---------

func (s *Server) handleRevokeOIDCClientAccessHTTP(w http.ResponseWriter, r *http.Request) {
	clientID := chi.URLParam(r, "clientId")
	if clientID == "" {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}

	var body accessPrincipalBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}

	principalID := pgtype.Int4{Int32: body.PrincipalID, Valid: true}

	var rows int64
	var err error
	switch body.PrincipalKind {
	case "group":
		rows, err = s.queries.RevokeOIDCClientAccessGroup(r.Context(), db.RevokeOIDCClientAccessGroupParams{
			ClientID: clientID,
			GroupID:  principalID,
		})
	case "account":
		rows, err = s.queries.RevokeOIDCClientAccessAccount(r.Context(), db.RevokeOIDCClientAccessAccountParams{
			ClientID:  clientID,
			AccountID: principalID,
		})
	default:
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	if err != nil {
		writeAuthErr(w, fmt.Errorf("handleRevokeOIDCClientAccess: revoke: %w", err))
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
	audit.RecordOrLog(r.Context(), s.Audit, audit.Record{
		AccountID: actorID,
		Factor:    audit.FactorOIDCClient,
		Event:     audit.EventAccessRevoked,
		Detail:    map[string]any{"client_id": clientID, "principal_kind": body.PrincipalKind, "principal_id": body.PrincipalID},
	})

	w.WriteHeader(http.StatusNoContent)
}

// ----- GET /saml-applications/{id}/access — paginated in handle_nested_pagination.go ----

// ----- POST /saml-applications/{id}/access/set-restricted (raw, sudo-gated) -----

func (s *Server) handleSetSAMLSPAccessRestrictedHTTP(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	if idStr == "" {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}

	var body setAccessRestrictedBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}

	sp, err := s.queries.SetSAMLSPAccessRestricted(r.Context(), db.SetSAMLSPAccessRestrictedParams{
		ID:               id,
		AccessRestricted: body.Restricted,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeAuthErr(w, samlSPNotFound())
			return
		}
		writeAuthErr(w, fmt.Errorf("handleSetSAMLSPAccessRestricted: update: %w", err))
		return
	}

	sess := authn.SessionFromContext(r.Context())
	var actorID *int32
	if sess != nil {
		actorID = &sess.Account.ID
	}
	audit.RecordOrLog(r.Context(), s.Audit, audit.Record{
		AccountID: actorID,
		Factor:    audit.FactorSAMLSP,
		Event:     audit.EventAccessRestrictedSet,
		Detail:    map[string]any{"sp_id": id, "restricted": body.Restricted},
	})

	acs, _ := s.queries.ListSAMLSPACSEndpoints(r.Context(), sp.ID)
	keys, _ := s.queries.ListSAMLSPKeys(r.Context(), db.ListSAMLSPKeysParams{SpID: sp.ID, Use: "signing"})
	writeJSON(w, samlApplicationView(sp, acs, keys))
}

// ----- POST /saml-applications/{id}/access/grant (raw, sudo-gated) ---------------

func (s *Server) handleGrantSAMLSPAccessHTTP(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	if idStr == "" {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}

	var body accessPrincipalBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}

	principalID := pgtype.Int4{Int32: body.PrincipalID, Valid: true}

	switch body.PrincipalKind {
	case "group":
		if err := s.queries.GrantSAMLSPAccessGroup(r.Context(), db.GrantSAMLSPAccessGroupParams{
			SamlSpID: id,
			GroupID:  principalID,
		}); err != nil {
			writeAuthErr(w, fmt.Errorf("handleGrantSAMLSPAccess: grant group: %w", err))
			return
		}
	case "account":
		if err := s.queries.GrantSAMLSPAccessAccount(r.Context(), db.GrantSAMLSPAccessAccountParams{
			SamlSpID:  id,
			AccountID: principalID,
		}); err != nil {
			writeAuthErr(w, fmt.Errorf("handleGrantSAMLSPAccess: grant account: %w", err))
			return
		}
	default:
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}

	sess := authn.SessionFromContext(r.Context())
	var actorID *int32
	if sess != nil {
		actorID = &sess.Account.ID
	}
	audit.RecordOrLog(r.Context(), s.Audit, audit.Record{
		AccountID: actorID,
		Factor:    audit.FactorSAMLSP,
		Event:     audit.EventAccessGranted,
		Detail:    map[string]any{"sp_id": id, "principal_kind": body.PrincipalKind, "principal_id": body.PrincipalID},
	})

	w.WriteHeader(http.StatusNoContent)
}

// ----- POST /saml-applications/{id}/access/revoke (raw, sudo-gated) --------------

func (s *Server) handleRevokeSAMLSPAccessHTTP(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	if idStr == "" {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}

	var body accessPrincipalBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}

	principalID := pgtype.Int4{Int32: body.PrincipalID, Valid: true}

	var rows int64
	switch body.PrincipalKind {
	case "group":
		rows, err = s.queries.RevokeSAMLSPAccessGroup(r.Context(), db.RevokeSAMLSPAccessGroupParams{
			SamlSpID: id,
			GroupID:  principalID,
		})
	case "account":
		rows, err = s.queries.RevokeSAMLSPAccessAccount(r.Context(), db.RevokeSAMLSPAccessAccountParams{
			SamlSpID:  id,
			AccountID: principalID,
		})
	default:
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	if err != nil {
		writeAuthErr(w, fmt.Errorf("handleRevokeSAMLSPAccess: revoke: %w", err))
		return
	}
	if rows == 0 {
		writeAuthErr(w, samlSPNotFound())
		return
	}

	sess := authn.SessionFromContext(r.Context())
	var actorID *int32
	if sess != nil {
		actorID = &sess.Account.ID
	}
	audit.RecordOrLog(r.Context(), s.Audit, audit.Record{
		AccountID: actorID,
		Factor:    audit.FactorSAMLSP,
		Event:     audit.EventAccessRevoked,
		Detail:    map[string]any{"sp_id": id, "principal_kind": body.PrincipalKind, "principal_id": body.PrincipalID},
	})

	w.WriteHeader(http.StatusNoContent)
}
