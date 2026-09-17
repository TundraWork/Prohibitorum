package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/db"
	oidc "prohibitorum/pkg/protocol/oidc"
)

type updateOIDCIdentityProjectionBody struct {
	SubjectSource string            `json:"subjectSource"`
	ClaimAliases  map[string]string `json:"claimAliases"`
}

type updateForwardAuthIdentityProjectionBody struct {
	RemoteUserSource string `json:"remoteUserSource"`
}

func changedAliasKeys(before, after map[string]string) []string {
	changed := make([]string, 0)
	seen := make(map[string]struct{}, len(before)+len(after))
	for key := range before {
		seen[key] = struct{}{}
	}
	for key := range after {
		seen[key] = struct{}{}
	}
	for key := range seen {
		if before[key] != after[key] {
			changed = append(changed, key)
		}
	}
	sort.Strings(changed)
	return changed
}

func (s *Server) handleUpdateOIDCIdentityProjectionHTTP(w http.ResponseWriter, r *http.Request) {
	clientID := chi.URLParam(r, "clientId")
	var body updateOIDCIdentityProjectionBody
	if clientID == "" || json.NewDecoder(r.Body).Decode(&body) != nil ||
		!oidc.ValidPrincipalSource(body.SubjectSource) || oidc.ValidateClaimAliases(body.ClaimAliases) != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	if body.ClaimAliases == nil {
		body.ClaimAliases = map[string]string{}
	}
	if err := s.authorizeApplicationManager(r.Context(), oidcApplicationRef(clientID, false)); err != nil {
		writeAuthErr(w, err)
		return
	}
	current, err := s.queries.GetOIDCClientAny(r.Context(), clientID)
	if err != nil || current.ForwardAuthEnabled {
		if errors.Is(err, pgx.ErrNoRows) || current.ForwardAuthEnabled {
			writeAuthErr(w, authn.ErrClientNotFound())
		} else {
			writeAuthErr(w, fmt.Errorf("identity projection lookup: %w", err))
		}
		return
	}
	before := map[string]string{}
	_ = json.Unmarshal(current.ClaimAliases, &before)
	rawAliases, _ := json.Marshal(body.ClaimAliases)
	updated, err := s.queries.UpdateOIDCIdentityProjection(r.Context(), db.UpdateOIDCIdentityProjectionParams{
		ClientID: clientID, PrincipalSource: body.SubjectSource, ClaimAliases: rawAliases,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeAuthErr(w, authn.ErrClientNotFound())
		} else {
			writeAuthErr(w, fmt.Errorf("update OIDC identity projection: %w", err))
		}
		return
	}
	audit.RecordOrLog(r.Context(), s.Audit, audit.Record{
		AccountID: faActorID(r.Context()), Factor: audit.FactorOIDCClient, Event: audit.EventUpdate,
		Detail: map[string]any{"client_id": clientID, "forward_auth": false, "principal_source": body.SubjectSource, "changed_alias_keys": changedAliasKeys(before, body.ClaimAliases)},
	})
	view := oidcApplicationView(updated)
	view.IconURL = s.enrichIconURL(r.Context(), "oidc_client", clientID)
	writeJSON(w, view)
}

func (s *Server) handleUpdateForwardAuthIdentityProjectionHTTP(w http.ResponseWriter, r *http.Request) {
	clientID := chi.URLParam(r, "clientId")
	var body updateForwardAuthIdentityProjectionBody
	if clientID == "" || json.NewDecoder(r.Body).Decode(&body) != nil || !oidc.ValidPrincipalSource(body.RemoteUserSource) {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	if err := s.authorizeApplicationManager(r.Context(), oidcApplicationRef(clientID, true)); err != nil {
		writeAuthErr(w, err)
		return
	}
	updated, err := s.queries.UpdateForwardAuthIdentityProjection(r.Context(), db.UpdateForwardAuthIdentityProjectionParams{
		ClientID: clientID, PrincipalSource: body.RemoteUserSource,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeAuthErr(w, authn.ErrClientNotFound())
		} else {
			writeAuthErr(w, fmt.Errorf("update forward-auth identity projection: %w", err))
		}
		return
	}
	audit.RecordOrLog(r.Context(), s.Audit, audit.Record{
		AccountID: faActorID(r.Context()), Factor: audit.FactorOIDCClient, Event: audit.EventUpdate,
		Detail: map[string]any{"client_id": clientID, "forward_auth": true, "principal_source": body.RemoteUserSource},
	})
	view := forwardAuthAppView(updated.ClientID, updated.DisplayName, updated.ForwardAuthHost, updated.ForwardAuthScopes, updated.AccessRestricted, updated.Disabled, updated.CreatedAt, updated.PrincipalSource)
	view.IconURL = s.enrichIconURL(r.Context(), "oidc_client", clientID)
	writeJSON(w, view)
}
