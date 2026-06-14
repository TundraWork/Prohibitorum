// Package server — handle_admin_saml_sps.go
//
// Admin SAML application endpoints:
//   GET  /saml-applications              — list all SPs (admin role, no sudo)
//   GET  /saml-applications/{id}         — get one SP with ACS + key summaries (admin role, no sudo)
//   POST /saml-applications              — create SP + ACS + keys in one tx (admin + sudo)
//   PUT  /saml-applications/{id}         — update mutable SP flags/fields (admin + sudo)
//   POST /saml-applications/{id}/reingest-metadata — replace ACS + keys from fresh metadata (admin + sudo)
//   POST /saml-applications/delete       — hard-delete SP + children in one tx (admin + sudo)
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

// samlApplicationView projects a db.SamlSp row + its ACS + key rows into the
// wire-safe contract view. Raw cert PEM is deliberately excluded — SAMLKeyView
// carries only the lifecycle summary fields.
func samlApplicationView(sp db.SamlSp, acs []db.SamlSpAc, keys []db.SamlSpKey) contract.SAMLApplicationView {
	attrMap := json.RawMessage(sp.AttributeMap)
	if len(attrMap) == 0 {
		attrMap = json.RawMessage("[]")
	}
	v := contract.SAMLApplicationView{
		ID:                        sp.ID,
		EntityID:                  sp.EntityID,
		DisplayName:               sp.DisplayName,
		NameIDFormat:              sp.NameIDFormat,
		AttributeMap:              attrMap,
		RequireSignedAuthnRequest: sp.RequireSignedAuthnRequest,
		AllowIdpInitiated:         sp.AllowIdpInitiated,
		Disabled:                  sp.Disabled,
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

// ----- GET /saml-applications (typed, role-only) --------------------------------

type listSAMLApplicationsOut struct {
	Body []contract.SAMLApplicationView
}

func (s *Server) handleListSAMLApplications(ctx context.Context, _ *struct{}) (*listSAMLApplicationsOut, error) {
	rows, err := s.queries.ListSAMLSPs(ctx)
	if err != nil {
		return nil, fmt.Errorf("handleListSAMLApplications: query: %w", err)
	}
	views := make([]contract.SAMLApplicationView, 0, len(rows))
	for _, sp := range rows {
		// List omits ACS + key details (summary only — id, entityId, displayName, flags).
		views = append(views, samlApplicationView(sp, nil, nil))
	}
	return &listSAMLApplicationsOut{Body: views}, nil
}

// ----- GET /saml-applications/{id} (typed, role-only) ---------------------------

type getSAMLApplicationIn struct {
	ID int64 `path:"id"`
}

type getSAMLApplicationOut struct {
	Body contract.SAMLApplicationView
}

func (s *Server) handleGetSAMLApplication(ctx context.Context, in *getSAMLApplicationIn) (*getSAMLApplicationOut, error) {
	sp, err := s.queries.GetSAMLSPByID(ctx, in.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, authErrToHuma(samlSPNotFound())
		}
		return nil, fmt.Errorf("handleGetSAMLApplication: query: %w", err)
	}
	acs, err := s.queries.ListSAMLSPACSEndpoints(ctx, sp.ID)
	if err != nil {
		return nil, fmt.Errorf("handleGetSAMLApplication: acs: %w", err)
	}
	keys, err := s.queries.ListSAMLSPKeys(ctx, db.ListSAMLSPKeysParams{SpID: sp.ID, Use: "signing"})
	if err != nil {
		return nil, fmt.Errorf("handleGetSAMLApplication: keys: %w", err)
	}
	return &getSAMLApplicationOut{Body: samlApplicationView(sp, acs, keys)}, nil
}

// ----- POST /saml-applications (raw, sudo-gated) --------------------------------

type createSAMLApplicationBody struct {
	// Metadata path: supply MetadataXML + Kind (+ optional overrides).
	MetadataXML  string `json:"metadataXml,omitempty"`
	Kind         string `json:"kind"` // "ghes" | "generic" | "" (defaults to generic)
	DisplayName  string `json:"displayName,omitempty"`
	EntityID     string `json:"entityId,omitempty"`
	NameIDFormat string `json:"nameIdFormat,omitempty"`

	// Override flags (also respected on the manual path).
	RequireSignedAuthnRequest bool `json:"requireSignedAuthnRequest"`
	AllowIdpInitiated         bool `json:"allowIdpInitiated"`

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

func (s *Server) handleCreateSAMLApplicationHTTP(w http.ResponseWriter, r *http.Request) {
	var body createSAMLApplicationBody
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

	// NameID-format default: an unset request field falls back to the
	// deployment-wide saml.default_nameid_format (C11) instead of a value
	// hardcoded in BuildSPParams, so the global default flows into new SPs.
	nameIDFormat := body.NameIDFormat
	if nameIDFormat == "" {
		nameIDFormat = s.config.SAML.DefaultNameIDFormat
	}

	opts := saml.SPOptions{
		MetadataXML:               []byte(body.MetadataXML),
		Kind:                      body.Kind,
		DisplayName:               body.DisplayName,
		EntityID:                  body.EntityID,
		NameIDFormat:              nameIDFormat,
		RequireSignedAuthnRequest: body.RequireSignedAuthnRequest,
		AllowIdpInitiated:         body.AllowIdpInitiated,
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
		writeAuthErr(w, fmt.Errorf("handleCreateSAMLApplication: begin tx: %w", err))
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck

	qtx := s.queries.WithTx(tx)
	sp, err := qtx.InsertSAMLSP(r.Context(), params)
	if err != nil {
		if isUniqueViolation(err) {
			writeAuthErr(w, authn.ErrSAMLApplicationAlreadyExists())
			return
		}
		writeAuthErr(w, fmt.Errorf("handleCreateSAMLApplication: insert sp: %w", err))
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
			writeAuthErr(w, fmt.Errorf("handleCreateSAMLApplication: insert acs: %w", err))
			return
		}
	}
	for _, certPEM := range certPEMs {
		if err := qtx.InsertSAMLSPKey(r.Context(), db.InsertSAMLSPKeyParams{
			SpID:    sp.ID,
			Use:     "signing",
			CertPem: certPEM,
		}); err != nil {
			writeAuthErr(w, fmt.Errorf("handleCreateSAMLApplication: insert key: %w", err))
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeAuthErr(w, fmt.Errorf("handleCreateSAMLApplication: commit: %w", err))
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
	_ = json.NewEncoder(w).Encode(samlApplicationView(sp, spACS, spKeys))
}

// ----- PUT /saml-applications/{id} (raw, sudo-gated) ----------------------------

type updateSAMLApplicationBody struct {
	DisplayName               string          `json:"displayName"`
	NameIDFormat              string          `json:"nameIdFormat"`
	AttributeMap              json.RawMessage `json:"attributeMap"`
	RequireSignedAuthnRequest bool            `json:"requireSignedAuthnRequest"`
	AllowIdpInitiated         bool            `json:"allowIdpInitiated"`
	SessionLifetimeSecs       *int64          `json:"sessionLifetimeSecs,omitempty"`
}

func (s *Server) handleUpdateSAMLApplicationHTTP(w http.ResponseWriter, r *http.Request) {
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

	var body updateSAMLApplicationBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	if body.DisplayName == "" {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}

	// Default attribute_map to an empty array so the NOT NULL jsonb column is
	// never set to NULL.  The outer json.NewDecoder already rejects structurally-
	// malformed JSON before we reach here, so no inner Unmarshal guard is needed.
	// Normalise an explicit JSON null to [] so callers can pass null to mean
	// "reset to default" without causing a 500 on the NOT NULL column.
	attrMapBytes := []byte("[]")
	if len(body.AttributeMap) > 0 && string(body.AttributeMap) != "null" {
		attrMapBytes = body.AttributeMap
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
		AllowIdpInitiated:         body.AllowIdpInitiated,
		SessionLifetime:           sessionLifetime,
		AttributeMap:              attrMapBytes,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeAuthErr(w, samlSPNotFound())
			return
		}
		writeAuthErr(w, fmt.Errorf("handleUpdateSAMLApplication: update: %w", err))
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
	writeJSON(w, samlApplicationView(sp, acs, keys))
}

// ----- POST /saml-applications/{id}/reingest-metadata (raw, sudo-gated) ---------

type reingestSAMLApplicationBody struct {
	MetadataXML string `json:"metadataXml"`
}

func (s *Server) handleReingestSAMLApplicationHTTP(w http.ResponseWriter, r *http.Request) {
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

	var body reingestSAMLApplicationBody
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
		writeAuthErr(w, fmt.Errorf("handleReingestSAMLApplication: lookup: %w", err))
		return
	}

	// Parse the new metadata, preserving the existing entity_id.
	opts := saml.SPOptions{
		MetadataXML: []byte(body.MetadataXML),
		EntityID:    sp.EntityID, // keep the canonical entity_id; metadata entityID is ignored
		DisplayName: sp.DisplayName,
		Kind:        "", // re-derive kind from SpKind if valid
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
		writeAuthErr(w, fmt.Errorf("handleReingestSAMLApplication: begin tx: %w", err))
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck

	qtx := s.queries.WithTx(tx)
	// Delete children first, then re-insert from fresh metadata.
	if err := qtx.DeleteSAMLSPACSByID(r.Context(), id); err != nil {
		writeAuthErr(w, fmt.Errorf("handleReingestSAMLApplication: delete acs: %w", err))
		return
	}
	if err := qtx.DeleteSAMLSPKeysByID(r.Context(), id); err != nil {
		writeAuthErr(w, fmt.Errorf("handleReingestSAMLApplication: delete keys: %w", err))
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
			writeAuthErr(w, fmt.Errorf("handleReingestSAMLApplication: insert acs: %w", err))
			return
		}
	}
	for _, certPEM := range certPEMs {
		if err := qtx.InsertSAMLSPKey(r.Context(), db.InsertSAMLSPKeyParams{
			SpID:    id,
			Use:     "signing",
			CertPem: certPEM,
		}); err != nil {
			writeAuthErr(w, fmt.Errorf("handleReingestSAMLApplication: insert key: %w", err))
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeAuthErr(w, fmt.Errorf("handleReingestSAMLApplication: commit: %w", err))
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
	writeJSON(w, samlApplicationView(sp, newACS, newKeys))
}

// ----- POST /saml-applications/delete (raw, sudo-gated) -------------------------

type deleteSAMLApplicationBody struct {
	ID int64 `json:"id"`
}

func (s *Server) handleDeleteSAMLApplicationHTTP(w http.ResponseWriter, r *http.Request) {
	var body deleteSAMLApplicationBody
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
		writeAuthErr(w, fmt.Errorf("handleDeleteSAMLApplication: lookup: %w", err))
		return
	}

	// saml_sp_acs and saml_sp_key both have ON DELETE CASCADE, so deleting the
	// parent row is sufficient — child rows are removed by the DB.
	rows, err := s.queries.DeleteSAMLSP(r.Context(), body.ID)
	if err != nil {
		writeAuthErr(w, fmt.Errorf("handleDeleteSAMLApplication: delete: %w", err))
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

// ----- POST /saml-applications/set-disabled (raw, sudo-gated) -------------------

type setSAMLApplicationDisabledBody struct {
	ID       int64 `json:"id"`
	Disabled bool  `json:"disabled"`
}

// handleSetSAMLApplicationDisabledHTTP flips ONLY the disabled flag, independent
// of the config PUT. A disabled SP is rejected by the SP-initiated AuthnRequest,
// SLO, and IdP-initiated SSO flows (treated as an unregistered SP). Returns the
// updated application view.
func (s *Server) handleSetSAMLApplicationDisabledHTTP(w http.ResponseWriter, r *http.Request) {
	var body setSAMLApplicationDisabledBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	if body.ID <= 0 {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}

	sp, err := s.queries.SetSAMLSPDisabled(r.Context(), db.SetSAMLSPDisabledParams{
		ID:       body.ID,
		Disabled: body.Disabled,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeAuthErr(w, samlSPNotFound())
			return
		}
		writeAuthErr(w, fmt.Errorf("handleSetSAMLApplicationDisabled: update: %w", err))
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
		Detail:    map[string]any{"sp_id": body.ID, "disabled": body.Disabled},
	})

	acs, _ := s.queries.ListSAMLSPACSEndpoints(r.Context(), sp.ID)
	keys, _ := s.queries.ListSAMLSPKeys(r.Context(), db.ListSAMLSPKeysParams{SpID: sp.ID, Use: "signing"})
	writeJSON(w, samlApplicationView(sp, acs, keys))
}
