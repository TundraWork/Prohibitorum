// Package server — handle_admin_saml_sps.go
//
// Admin SAML service-provider endpoints:
//   GET  /saml-providers              — list all SPs (admin role, no sudo)
//   GET  /saml-providers/{id}         — get one SP with ACS + key summaries (admin role, no sudo)
//   POST /saml-providers              — create SP + ACS + keys in one tx (admin + sudo)
//   PUT  /saml-providers/{id}         — update mutable SP flags/fields (admin + sudo)
//   POST /saml-providers/{id}/reingest-metadata — replace ACS + keys from fresh metadata (admin + sudo)
//   POST /saml-providers/delete       — hard-delete SP + children in one tx (admin + sudo)
//
// Raw certificate PEM is NEVER returned — callers get SAMLKeyView{Use, NotAfter}
// summaries only. SPs have only public certificates (signing certs from metadata)
// so there is no private-key material to protect, but we keep the boundary clean.
//
// Mutations are registered via s.registerSudoOpHTTP, so the sudo gate,
// content-type check, and body-size limit are all enforced by the wrapper —
// handlers must NOT call requireFreshSudo themselves.
//
// Create and reingest use saml.BuildSPParams (the same ingest path as the CLI)
// and execute their inserts inside a single pgx transaction.

package server

import (
	"context"
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
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
	saml "prohibitorum/pkg/protocol/saml"
)

// ----- projection helpers ---------------------------------------------------

// samlProviderView projects a db.SamlSp row + its ACS + key rows into the
// wire-safe contract view. Raw cert PEM is deliberately excluded — SAMLKeyView
// carries only the lifecycle summary fields.
func samlProviderView(sp db.SamlSp, acs []db.SamlSpAc, keys []db.SamlSpKey) contract.SAMLProviderView {
	v := contract.SAMLProviderView{
		ID:                        sp.ID,
		EntityID:                  sp.EntityID,
		DisplayName:               sp.DisplayName,
		NameIDFormat:              sp.NameIDFormat,
		RequireSignedAuthnRequest: sp.RequireSignedAuthnRequest,
		WantAssertionsSigned:      sp.WantAssertionsSigned,
		AllowIdpInitiated:         sp.AllowIdpInitiated,
		ACS:                       make([]contract.SAMLACSView, 0, len(acs)),
		Keys:                      make([]contract.SAMLKeyView, 0, len(keys)),
	}
	if sp.SpKind.Valid {
		v.Kind = sp.SpKind.String
	}
	if sp.CreatedAt.Valid {
		v.CreatedAt = sp.CreatedAt.Time
	}
	if sp.SessionLifetime.Valid {
		secs := sp.SessionLifetime.Microseconds / 1_000_000
		v.SessionLifetimeSecs = &secs
	}
	for _, a := range acs {
		v.ACS = append(v.ACS, contract.SAMLACSView{
			Binding:   a.Binding,
			Location:  a.Location,
			Index:     a.Idx,
			IsDefault: a.IsDefault,
		})
	}
	for _, k := range keys {
		kv := contract.SAMLKeyView{Use: k.Use}
		if k.NotAfter.Valid {
			t := k.NotAfter.Time
			kv.NotAfter = &t
		}
		v.Keys = append(v.Keys, kv)
	}
	return v
}

// samlSPNotFound returns the canonical 404 for a missing SAML SP.
func samlSPNotFound() *authn.AuthError {
	return authn.ErrCredentialNotFound() // reuse 404 shape; no need for a dedicated code
}

// ----- GET /saml-providers (typed, role-only) --------------------------------

type listSAMLProvidersOut struct {
	Body []contract.SAMLProviderView
}

func (s *Server) handleListSAMLProviders(ctx context.Context, _ *struct{}) (*listSAMLProvidersOut, error) {
	rows, err := s.queries.ListSAMLSPs(ctx)
	if err != nil {
		return nil, fmt.Errorf("handleListSAMLProviders: query: %w", err)
	}
	views := make([]contract.SAMLProviderView, 0, len(rows))
	for _, sp := range rows {
		// List omits ACS + key details (summary only — id, entityId, displayName, flags).
		views = append(views, samlProviderView(sp, nil, nil))
	}
	return &listSAMLProvidersOut{Body: views}, nil
}

// ----- GET /saml-providers/{id} (typed, role-only) ---------------------------

type getSAMLProviderIn struct {
	ID int64 `path:"id"`
}

type getSAMLProviderOut struct {
	Body contract.SAMLProviderView
}

func (s *Server) handleGetSAMLProvider(ctx context.Context, in *getSAMLProviderIn) (*getSAMLProviderOut, error) {
	sp, err := s.queries.GetSAMLSPByID(ctx, in.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, authErrToHuma(samlSPNotFound())
		}
		return nil, fmt.Errorf("handleGetSAMLProvider: query: %w", err)
	}
	acs, err := s.queries.ListSAMLSPACSEndpoints(ctx, sp.ID)
	if err != nil {
		return nil, fmt.Errorf("handleGetSAMLProvider: acs: %w", err)
	}
	keys, err := s.queries.ListSAMLSPKeys(ctx, db.ListSAMLSPKeysParams{SpID: sp.ID, Use: "signing"})
	if err != nil {
		return nil, fmt.Errorf("handleGetSAMLProvider: keys: %w", err)
	}
	return &getSAMLProviderOut{Body: samlProviderView(sp, acs, keys)}, nil
}

// ----- POST /saml-providers (raw, sudo-gated) --------------------------------

type createSAMLProviderBody struct {
	// Metadata path: supply MetadataXML + Kind (+ optional overrides).
	MetadataXML string `json:"metadataXml,omitempty"`
	Kind        string `json:"kind"` // "ghes" | "generic" | "" (defaults to generic)
	DisplayName string `json:"displayName,omitempty"`
	EntityID    string `json:"entityId,omitempty"`
	NameIDFormat string `json:"nameIdFormat,omitempty"`

	// Override flags (also respected on the manual path).
	RequireSignedAuthnRequest bool `json:"requireSignedAuthnRequest"`
	AllowIdpInitiated         bool `json:"allowIdpInitiated"`
	WantAssertionsSigned      *bool `json:"wantAssertionsSigned,omitempty"`

	// Manual path: supply ACS entries directly (no metadata).
	ACS []struct {
		Binding   string `json:"binding"`
		Location  string `json:"location"`
		Index     int    `json:"index"`
		IsDefault bool   `json:"isDefault"`
	} `json:"acs,omitempty"`

	// Optional session lifetime in seconds. 0 / omitted = no override (IdP default).
	SessionLifetimeSecs *int64 `json:"sessionLifetimeSecs,omitempty"`
}

func (s *Server) handleCreateSAMLProviderHTTP(w http.ResponseWriter, r *http.Request) {
	var body createSAMLProviderBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}

	manualACS := make([]saml.SPACSEntry, 0, len(body.ACS))
	for _, a := range body.ACS {
		manualACS = append(manualACS, saml.SPACSEntry{
			Binding:   a.Binding,
			Location:  a.Location,
			Index:     a.Index,
			IsDefault: a.IsDefault,
		})
	}

	opts := saml.SPOptions{
		MetadataXML:               []byte(body.MetadataXML),
		Kind:                      body.Kind,
		DisplayName:               body.DisplayName,
		EntityID:                  body.EntityID,
		NameIDFormat:              body.NameIDFormat,
		RequireSignedAuthnRequest: body.RequireSignedAuthnRequest,
		AllowIdpInitiated:         body.AllowIdpInitiated,
		WantAssertionsSigned:      body.WantAssertionsSigned,
		ManualACS:                 manualACS,
	}

	params, acs, certPEMs, err := saml.BuildSPParams(opts)
	if err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}

	// Apply optional session lifetime.
	if body.SessionLifetimeSecs != nil && *body.SessionLifetimeSecs > 0 {
		params.SessionLifetime = pgtype.Interval{
			Microseconds: *body.SessionLifetimeSecs * 1_000_000,
			Valid:        true,
		}
	}

	tx, err := s.dbPool.Begin(r.Context())
	if err != nil {
		writeAuthErr(w, fmt.Errorf("handleCreateSAMLProvider: begin tx: %w", err))
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck

	qtx := s.queries.WithTx(tx)
	sp, err := qtx.InsertSAMLSP(r.Context(), params)
	if err != nil {
		writeAuthErr(w, fmt.Errorf("handleCreateSAMLProvider: insert sp: %w", err))
		return
	}
	for _, a := range acs {
		if err := qtx.InsertSAMLSPACS(r.Context(), db.InsertSAMLSPACSParams{
			SpID:      sp.ID,
			Idx:       int32(a.Index),
			Binding:   a.Binding,
			Location:  a.Location,
			IsDefault: a.IsDefault,
		}); err != nil {
			writeAuthErr(w, fmt.Errorf("handleCreateSAMLProvider: insert acs: %w", err))
			return
		}
	}
	for _, certPEM := range certPEMs {
		if err := qtx.InsertSAMLSPKey(r.Context(), db.InsertSAMLSPKeyParams{
			SpID:    sp.ID,
			Use:     "signing",
			CertPem: certPEM,
		}); err != nil {
			writeAuthErr(w, fmt.Errorf("handleCreateSAMLProvider: insert key: %w", err))
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeAuthErr(w, fmt.Errorf("handleCreateSAMLProvider: commit: %w", err))
		return
	}

	sess := authn.SessionFromContext(r.Context())
	var actorID *int32
	if sess != nil {
		actorID = &sess.Account.ID
	}
	_ = s.Audit.Record(r.Context(), audit.Record{
		AccountID: actorID,
		Factor:    audit.FactorSAMLSP,
		Event:     audit.EventRegister,
		Detail:    map[string]any{"sp_id": sp.ID, "entity_id": sp.EntityID},
	})

	// Re-query children to build the full view (keys inserted without not_after).
	spACS, _ := s.queries.ListSAMLSPACSEndpoints(r.Context(), sp.ID)
	spKeys, _ := s.queries.ListSAMLSPKeys(r.Context(), db.ListSAMLSPKeysParams{SpID: sp.ID, Use: "signing"})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(samlProviderView(sp, spACS, spKeys))
}

// ----- PUT /saml-providers/{id} (raw, sudo-gated) ----------------------------

type updateSAMLProviderBody struct {
	DisplayName               string  `json:"displayName"`
	NameIDFormat              string  `json:"nameIdFormat"`
	RequireSignedAuthnRequest bool    `json:"requireSignedAuthnRequest"`
	WantAssertionsSigned      bool    `json:"wantAssertionsSigned"`
	AllowIdpInitiated         bool    `json:"allowIdpInitiated"`
	SessionLifetimeSecs       *int64  `json:"sessionLifetimeSecs,omitempty"`
}

func (s *Server) handleUpdateSAMLProviderHTTP(w http.ResponseWriter, r *http.Request) {
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

	var body updateSAMLProviderBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	if body.DisplayName == "" {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}

	var sessionLifetime pgtype.Interval
	if body.SessionLifetimeSecs != nil && *body.SessionLifetimeSecs > 0 {
		sessionLifetime = pgtype.Interval{
			Microseconds: *body.SessionLifetimeSecs * 1_000_000,
			Valid:        true,
		}
	}

	sp, err := s.queries.UpdateSAMLSP(r.Context(), db.UpdateSAMLSPParams{
		ID:                        id,
		DisplayName:               body.DisplayName,
		NameIDFormat:              body.NameIDFormat,
		RequireSignedAuthnRequest: body.RequireSignedAuthnRequest,
		WantAssertionsSigned:      body.WantAssertionsSigned,
		AllowIdpInitiated:         body.AllowIdpInitiated,
		SessionLifetime:           sessionLifetime,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeAuthErr(w, samlSPNotFound())
			return
		}
		writeAuthErr(w, fmt.Errorf("handleUpdateSAMLProvider: update: %w", err))
		return
	}

	sess := authn.SessionFromContext(r.Context())
	var actorID *int32
	if sess != nil {
		actorID = &sess.Account.ID
	}
	_ = s.Audit.Record(r.Context(), audit.Record{
		AccountID: actorID,
		Factor:    audit.FactorSAMLSP,
		Event:     audit.EventUpdate,
		Detail:    map[string]any{"sp_id": id, "entity_id": sp.EntityID},
	})

	acs, _ := s.queries.ListSAMLSPACSEndpoints(r.Context(), sp.ID)
	keys, _ := s.queries.ListSAMLSPKeys(r.Context(), db.ListSAMLSPKeysParams{SpID: sp.ID, Use: "signing"})
	writeJSON(w, samlProviderView(sp, acs, keys))
}

// ----- POST /saml-providers/{id}/reingest-metadata (raw, sudo-gated) ---------

type reingestSAMLProviderBody struct {
	MetadataXML string `json:"metadataXml"`
}

func (s *Server) handleReingestSAMLProviderHTTP(w http.ResponseWriter, r *http.Request) {
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

	var body reingestSAMLProviderBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	if body.MetadataXML == "" {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}

	// Verify the SP exists before starting the tx.
	sp, err := s.queries.GetSAMLSPByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeAuthErr(w, samlSPNotFound())
			return
		}
		writeAuthErr(w, fmt.Errorf("handleReingestSAMLProvider: lookup: %w", err))
		return
	}

	// Parse the new metadata, preserving the existing entity_id.
	opts := saml.SPOptions{
		MetadataXML: []byte(body.MetadataXML),
		EntityID:    sp.EntityID, // keep the canonical entity_id; metadata entityID is ignored
		DisplayName: sp.DisplayName,
		Kind:        "",           // re-derive kind from SpKind if valid
	}
	if sp.SpKind.Valid {
		opts.Kind = sp.SpKind.String
	}

	_, acs, certPEMs, err := saml.BuildSPParams(opts)
	if err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}

	tx, err := s.dbPool.Begin(r.Context())
	if err != nil {
		writeAuthErr(w, fmt.Errorf("handleReingestSAMLProvider: begin tx: %w", err))
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck

	qtx := s.queries.WithTx(tx)
	// Delete children first, then re-insert from fresh metadata.
	if err := qtx.DeleteSAMLSPACSByID(r.Context(), id); err != nil {
		writeAuthErr(w, fmt.Errorf("handleReingestSAMLProvider: delete acs: %w", err))
		return
	}
	if err := qtx.DeleteSAMLSPKeysByID(r.Context(), id); err != nil {
		writeAuthErr(w, fmt.Errorf("handleReingestSAMLProvider: delete keys: %w", err))
		return
	}
	for _, a := range acs {
		if err := qtx.InsertSAMLSPACS(r.Context(), db.InsertSAMLSPACSParams{
			SpID:      id,
			Idx:       int32(a.Index),
			Binding:   a.Binding,
			Location:  a.Location,
			IsDefault: a.IsDefault,
		}); err != nil {
			writeAuthErr(w, fmt.Errorf("handleReingestSAMLProvider: insert acs: %w", err))
			return
		}
	}
	for _, certPEM := range certPEMs {
		if err := qtx.InsertSAMLSPKey(r.Context(), db.InsertSAMLSPKeyParams{
			SpID:    id,
			Use:     "signing",
			CertPem: certPEM,
		}); err != nil {
			writeAuthErr(w, fmt.Errorf("handleReingestSAMLProvider: insert key: %w", err))
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeAuthErr(w, fmt.Errorf("handleReingestSAMLProvider: commit: %w", err))
		return
	}

	sess := authn.SessionFromContext(r.Context())
	var actorID *int32
	if sess != nil {
		actorID = &sess.Account.ID
	}
	_ = s.Audit.Record(r.Context(), audit.Record{
		AccountID: actorID,
		Factor:    audit.FactorSAMLSP,
		Event:     audit.EventUpdate,
		Detail:    map[string]any{"sp_id": id, "entity_id": sp.EntityID, "action": "reingest_metadata"},
	})

	newACS, _ := s.queries.ListSAMLSPACSEndpoints(r.Context(), id)
	newKeys, _ := s.queries.ListSAMLSPKeys(r.Context(), db.ListSAMLSPKeysParams{SpID: id, Use: "signing"})
	writeJSON(w, samlProviderView(sp, newACS, newKeys))
}

// ----- POST /saml-providers/delete (raw, sudo-gated) -------------------------

type deleteSAMLProviderBody struct {
	ID int64 `json:"id"`
}

func (s *Server) handleDeleteSAMLProviderHTTP(w http.ResponseWriter, r *http.Request) {
	var body deleteSAMLProviderBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	if body.ID == 0 {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}

	// Look up the entity_id first so we can include it in the audit record.
	sp, err := s.queries.GetSAMLSPByID(r.Context(), body.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeAuthErr(w, samlSPNotFound())
			return
		}
		writeAuthErr(w, fmt.Errorf("handleDeleteSAMLProvider: lookup: %w", err))
		return
	}

	// saml_sp_acs and saml_sp_key both have ON DELETE CASCADE, so deleting the
	// parent row is sufficient — child rows are removed by the DB.
	rows, err := s.queries.DeleteSAMLSP(r.Context(), body.ID)
	if err != nil {
		writeAuthErr(w, fmt.Errorf("handleDeleteSAMLProvider: delete: %w", err))
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
	_ = s.Audit.Record(r.Context(), audit.Record{
		AccountID: actorID,
		Factor:    audit.FactorSAMLSP,
		Event:     audit.EventRevoke,
		Detail:    map[string]any{"sp_id": body.ID, "entity_id": sp.EntityID},
	})

	w.WriteHeader(http.StatusNoContent)
}
