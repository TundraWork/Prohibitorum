package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/federation"
	"prohibitorum/pkg/pagination"
)

const providerConfigLimit = 8 * 1024

type providerWriteBody struct {
	Slug        string          `json:"slug,omitempty"`
	DisplayName string          `json:"displayName"`
	Protocol    string          `json:"protocol,omitempty"`
	Mode        string          `json:"mode"`
	Config      json.RawMessage `json:"config"`
	Secret      string          `json:"secret,omitempty"`
}

type providerStateQueries interface {
	GetUpstreamIDPBySlugAny(context.Context, string) (db.UpstreamIdp, error)
	SetUpstreamIDPDisabled(context.Context, db.SetUpstreamIDPDisabledParams) (db.UpstreamIdp, error)
}

func (s *Server) providerStateQ() providerStateQueries {
	if s.providerStateQueriesOverride != nil {
		return s.providerStateQueriesOverride
	}
	return s.queries
}

func providerFromDB(row db.UpstreamIdp) federation.Provider {
	provider := federation.Provider{
		ID:           row.ID,
		Slug:         row.Slug,
		DisplayName:  row.DisplayName,
		Protocol:     row.Protocol,
		Mode:         row.Mode,
		Config:       append(json.RawMessage(nil), row.ProviderConfig...),
		SecretStatus: row.SecretStatus,
		Disabled:     row.Disabled,
	}
	if row.SecretValidatedAt.Valid {
		validatedAt := row.SecretValidatedAt.Time
		provider.SecretValidatedAt = &validatedAt
	}
	if row.KeyVersion.Valid {
		provider.Secret = &federation.SealedSecret{
			Ciphertext: append([]byte(nil), row.SecretEnc...),
			Nonce:      append([]byte(nil), row.SecretNonce...),
			KeyVersion: row.KeyVersion.Int32,
		}
	}
	return provider
}

func (s *Server) identityProviderView(row db.UpstreamIdp) (contract.IdentityProviderView, error) {
	if s.federationRegistry == nil {
		return contract.IdentityProviderView{}, errors.New("identity provider registry is not configured")
	}
	definition, err := s.federationRegistry.Definition(row.Protocol)
	if err != nil {
		return contract.IdentityProviderView{}, err
	}
	descriptor := definition.Descriptor()
	searchFields := make([]contract.IdentitySearchFieldView, len(descriptor.SearchFields))
	for i, field := range descriptor.SearchFields {
		operators := make([]string, len(field.Operators))
		for j, operator := range field.Operators {
			operators[j] = string(operator)
		}
		searchFields[i] = contract.IdentitySearchFieldView{Key: field.Key, Operators: operators}
	}
	provider := providerFromDB(row)
	view := contract.IdentityProviderView{
		Slug:              row.Slug,
		DisplayName:       row.DisplayName,
		Protocol:          row.Protocol,
		Mode:              row.Mode,
		Config:            append(json.RawMessage(nil), row.ProviderConfig...),
		Disabled:          row.Disabled,
		SecretConfigured:  provider.Secret != nil,
		SecretStatus:      row.SecretStatus,
		SecretValidatedAt: provider.SecretValidatedAt,
		Ready:             definition.Ready(provider),
		SupportsOperator:  descriptor.SupportsOperator,
		SearchFields:      searchFields,
	}
	if row.CreatedAt.Valid {
		view.CreatedAt = row.CreatedAt.Time
	}
	return view, nil
}

func (s *Server) validateProviderWrite(body providerWriteBody, existing *db.UpstreamIdp, creating bool) (federation.Definition, error) {
	if body.DisplayName == "" || body.Mode == "" || len(body.Config) == 0 || len(body.Config) > providerConfigLimit {
		return nil, authn.ErrBadRequest()
	}
	switch body.Mode {
	case federation.ModeAutoProvision, federation.ModeInviteOnly, federation.ModeLinkOnly:
	default:
		return nil, authn.ErrBadRequest()
	}

	protocol := body.Protocol
	if creating {
		if body.Slug == "" || protocol == "" {
			return nil, authn.ErrBadRequest()
		}
	} else {
		if existing == nil || body.Secret != "" {
			return nil, authn.ErrBadRequest()
		}
		if body.Slug != "" && body.Slug != existing.Slug {
			return nil, authn.ErrBadRequest()
		}
		if protocol != "" && protocol != existing.Protocol {
			return nil, authn.ErrBadRequest()
		}
		protocol = existing.Protocol
	}
	if s.federationRegistry == nil {
		return nil, authn.ErrBadRequest()
	}
	definition, err := s.federationRegistry.Definition(protocol)
	if err != nil || definition.ValidateConfig(body.Config) != nil {
		return nil, authn.ErrBadRequest()
	}
	if creating {
		if definition.Descriptor().RequiresSecret && body.Secret == "" {
			return nil, authn.ErrBadRequest()
		}
		if err := definition.ValidateSecret([]byte(body.Secret)); err != nil {
			return nil, authn.ErrBadRequest()
		}
	}
	return definition, nil
}

func providerInsertParams(body providerWriteBody) db.InsertUpstreamIDPParams {
	return db.InsertUpstreamIDPParams{
		Slug:           body.Slug,
		DisplayName:    body.DisplayName,
		Protocol:       body.Protocol,
		Mode:           body.Mode,
		ProviderConfig: append([]byte(nil), body.Config...),
		SecretStatus:   "unconfigured",
		Disabled:       body.Protocol == "vrchat",
	}
}

func providerWriteAuditDetail(body providerWriteBody) map[string]any {
	return map[string]any{"slug": body.Slug, "protocol": body.Protocol, "mode": body.Mode}
}

func (s *Server) currentDEK() (int32, []byte, error) {
	if s.config == nil || len(s.config.DataEncryptionKeys) == 0 {
		return 0, nil, errors.New("handle_admin_upstream_idps: no data encryption keys configured")
	}
	var maxVersion int
	for version := range s.config.DataEncryptionKeys {
		if version > maxVersion {
			maxVersion = version
		}
	}
	return int32(maxVersion), s.config.DataEncryptionKeys[maxVersion], nil
}

type listIdentityProvidersIn struct {
	pageInput
}

type listIdentityProvidersOut struct {
	Body contract.Page[contract.IdentityProviderView]
}

func (s *Server) handleListIdentityProviders(ctx context.Context, in *listIdentityProvidersIn) (*listIdentityProvidersOut, error) {
	limit := pagination.Limit(in.Limit)
	const collection = "identity_providers"
	const sort = "created_at"
	filters := map[string]string{}
	payload, err := s.decodeCursor(in.Cursor, collection, sort, filters)
	if err != nil {
		return nil, cursorInvalidErr(err)
	}
	params := db.ListAllUpstreamIDPsParams{Limit: int32(limit + 1)}
	if len(payload.Keys) == 2 {
		if createdAt, parseErr := time.Parse(time.RFC3339Nano, payload.Keys[0]); parseErr == nil {
			params.AfterCreatedAt = tsToPgType(createdAt)
		}
		var id int64
		if _, scanErr := fmt.Sscanf(payload.Keys[1], "%d", &id); scanErr == nil {
			params.AfterID = pgtype.Int8{Int64: id, Valid: true}
		}
	}
	rows, err := s.listQ().ListAllUpstreamIDPs(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("handleListIdentityProviders: query: %w", err)
	}
	more := hasMore(len(rows), limit)
	if more {
		rows = rows[:limit]
	}
	views := make([]contract.IdentityProviderView, 0, len(rows))
	for _, row := range rows {
		view, viewErr := s.identityProviderView(row)
		if viewErr != nil {
			return nil, fmt.Errorf("handleListIdentityProviders: view: %w", viewErr)
		}
		views = append(views, view)
	}
	var nextCursor string
	if more && len(rows) > 0 {
		last := rows[len(rows)-1]
		nextCursor = s.encodeNextCursor(collection, sort, filters, []string{
			last.CreatedAt.Time.Format(time.RFC3339Nano), fmt.Sprintf("%d", last.ID),
		})
	}
	return &listIdentityProvidersOut{Body: buildPage(views, nextCursor)}, nil
}

type getIdentityProviderIn struct {
	Slug string `path:"slug"`
}

type identityProviderOut struct {
	Body contract.IdentityProviderView
}

func (s *Server) handleGetIdentityProvider(ctx context.Context, in *getIdentityProviderIn) (*identityProviderOut, error) {
	row, err := s.queries.GetUpstreamIDPBySlugAny(ctx, in.Slug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, authErrToHuma(authn.ErrUpstreamIDPNotFound())
		}
		return nil, fmt.Errorf("handleGetIdentityProvider: query: %w", err)
	}
	view, err := s.identityProviderView(row)
	if err != nil {
		return nil, fmt.Errorf("handleGetIdentityProvider: view: %w", err)
	}
	view.IconURL = entityIconURLPtr("upstream_idp", row.Slug, s.lookupEntityIconEtag(ctx, "upstream_idp", row.Slug))
	return &identityProviderOut{Body: view}, nil
}

func (s *Server) handleCreateIdentityProviderHTTP(w http.ResponseWriter, r *http.Request) {
	var body providerWriteBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	if _, err := s.validateProviderWrite(body, nil, true); err != nil {
		writeAuthErr(w, err)
		return
	}

	tx, err := s.dbPool.Begin(r.Context())
	if err != nil {
		writeAuthErr(w, fmt.Errorf("handleCreateIdentityProvider: begin tx: %w", err))
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck
	qtx := s.queries.WithTx(tx)
	row, err := qtx.InsertUpstreamIDP(r.Context(), providerInsertParams(body))
	if err != nil {
		if isUniqueViolation(err) {
			writeAuthErr(w, authn.ErrUpstreamIDPAlreadyExists())
			return
		}
		writeAuthErr(w, fmt.Errorf("handleCreateIdentityProvider: insert: %w", err))
		return
	}
	if body.Secret != "" {
		keyVersion, dek, keyErr := s.currentDEK()
		if keyErr != nil {
			writeAuthErr(w, keyErr)
			return
		}
		sealed, sealErr := federation.SealProviderSecret(dek, []byte(body.Secret), row.ID, keyVersion)
		if sealErr != nil {
			writeAuthErr(w, fmt.Errorf("handleCreateIdentityProvider: seal: %w", sealErr))
			return
		}
		row, err = qtx.UpdateUpstreamIDPSecret(r.Context(), db.UpdateUpstreamIDPSecretParams{
			Slug: row.Slug, SecretEnc: sealed.Ciphertext, SecretNonce: sealed.Nonce,
			KeyVersion: pgtype.Int4{Int32: keyVersion, Valid: true}, SecretStatus: "configured",
		})
		if err != nil {
			writeAuthErr(w, fmt.Errorf("handleCreateIdentityProvider: secret update: %w", err))
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeAuthErr(w, fmt.Errorf("handleCreateIdentityProvider: commit: %w", err))
		return
	}

	body.Protocol = row.Protocol
	audit.RecordOrLog(r.Context(), s.Audit, audit.Record{
		AccountID: sessionAccountID(r.Context()), Factor: audit.FactorUpstreamIDP,
		Event: audit.EventRegister, Detail: providerWriteAuditDetail(body),
	})
	view, err := s.identityProviderView(row)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(view)
}

func (s *Server) handleUpdateIdentityProviderHTTP(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	if slug == "" {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	var body providerWriteBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	existing, err := s.queries.GetUpstreamIDPBySlugAny(r.Context(), slug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeAuthErr(w, authn.ErrUpstreamIDPNotFound())
			return
		}
		writeAuthErr(w, fmt.Errorf("handleUpdateIdentityProvider: lookup: %w", err))
		return
	}
	if _, err := s.validateProviderWrite(body, &existing, false); err != nil {
		writeAuthErr(w, err)
		return
	}
	updated, err := s.queries.UpdateUpstreamIDPConfig(r.Context(), db.UpdateUpstreamIDPConfigParams{
		Slug: slug, DisplayName: body.DisplayName, Mode: body.Mode, ProviderConfig: append([]byte(nil), body.Config...),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeAuthErr(w, authn.ErrUpstreamIDPNotFound())
			return
		}
		writeAuthErr(w, fmt.Errorf("handleUpdateIdentityProvider: update: %w", err))
		return
	}
	if s.federationOIDCAdapter != nil && existing.Protocol == "oidc" {
		s.federationOIDCAdapter.InvalidateClientCache(slug)
	}
	body.Slug = existing.Slug
	body.Protocol = existing.Protocol
	audit.RecordOrLog(r.Context(), s.Audit, audit.Record{
		AccountID: sessionAccountID(r.Context()), Factor: audit.FactorUpstreamIDP,
		Event: audit.EventUpdate, Detail: providerWriteAuditDetail(body),
	})
	view, err := s.identityProviderView(updated)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	writeJSON(w, view)
}

type setIdentityProviderDisabledBody struct {
	Slug     string `json:"slug"`
	Disabled bool   `json:"disabled"`
}

func (s *Server) handleSetIdentityProviderDisabledHTTP(w http.ResponseWriter, r *http.Request) {
	var body setIdentityProviderDisabledBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Slug == "" {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	queries := s.providerStateQ()
	if !body.Disabled {
		row, err := queries.GetUpstreamIDPBySlugAny(r.Context(), body.Slug)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeAuthErr(w, authn.ErrUpstreamIDPNotFound())
				return
			}
			writeAuthErr(w, fmt.Errorf("handleSetIdentityProviderDisabled: lookup: %w", err))
			return
		}
		definition, err := s.federationRegistry.Definition(row.Protocol)
		if err != nil || !definition.Ready(providerFromDB(row)) {
			writeAuthErr(w, authn.ErrProviderNotReady())
			return
		}
	}
	updated, err := queries.SetUpstreamIDPDisabled(r.Context(), db.SetUpstreamIDPDisabledParams{Slug: body.Slug, Disabled: body.Disabled})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeAuthErr(w, authn.ErrUpstreamIDPNotFound())
			return
		}
		writeAuthErr(w, fmt.Errorf("handleSetIdentityProviderDisabled: update: %w", err))
		return
	}
	audit.RecordOrLog(r.Context(), s.Audit, audit.Record{
		AccountID: sessionAccountID(r.Context()), Factor: audit.FactorUpstreamIDP,
		Event: audit.EventUpdate, Detail: map[string]any{"slug": body.Slug, "disabled": body.Disabled},
	})
	view, err := s.identityProviderView(updated)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	writeJSON(w, view)
}

type rotateIdentityProviderSecretBody struct {
	Slug   string `json:"slug"`
	Secret string `json:"secret"`
}

func (s *Server) handleRotateIdentityProviderSecretHTTP(w http.ResponseWriter, r *http.Request) {
	var body rotateIdentityProviderSecretBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Slug == "" || body.Secret == "" {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	row, err := s.queries.GetUpstreamIDPBySlugAny(r.Context(), body.Slug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeAuthErr(w, authn.ErrUpstreamIDPNotFound())
			return
		}
		writeAuthErr(w, fmt.Errorf("handleRotateIdentityProviderSecret: lookup: %w", err))
		return
	}
	definition, err := s.federationRegistry.Definition(row.Protocol)
	if err != nil || definition.ValidateSecret([]byte(body.Secret)) != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	keyVersion, dek, err := s.currentDEK()
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	sealed, err := federation.SealProviderSecret(dek, []byte(body.Secret), row.ID, keyVersion)
	if err != nil {
		writeAuthErr(w, fmt.Errorf("handleRotateIdentityProviderSecret: seal: %w", err))
		return
	}
	if _, err := s.queries.UpdateUpstreamIDPSecret(r.Context(), db.UpdateUpstreamIDPSecretParams{
		Slug: body.Slug, SecretEnc: sealed.Ciphertext, SecretNonce: sealed.Nonce,
		KeyVersion: pgtype.Int4{Int32: keyVersion, Valid: true}, SecretStatus: "configured",
	}); err != nil {
		writeAuthErr(w, fmt.Errorf("handleRotateIdentityProviderSecret: update: %w", err))
		return
	}
	audit.RecordOrLog(r.Context(), s.Audit, audit.Record{
		AccountID: sessionAccountID(r.Context()), Factor: audit.FactorUpstreamIDP,
		Event: audit.EventRotate, Detail: map[string]any{"slug": body.Slug, "action": "rotate_secret"},
	})
	w.WriteHeader(http.StatusNoContent)
}

type deleteIdentityProviderBody struct {
	Slug string `json:"slug"`
}

func (s *Server) handleDeleteIdentityProviderHTTP(w http.ResponseWriter, r *http.Request) {
	var body deleteIdentityProviderBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Slug == "" {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
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
	_ = s.queries.DeleteEntityIcon(r.Context(), db.DeleteEntityIconParams{OwnerKind: "upstream_idp", OwnerID: body.Slug})
	audit.RecordOrLog(r.Context(), s.Audit, audit.Record{
		AccountID: sessionAccountID(r.Context()), Factor: audit.FactorUpstreamIDP,
		Event: audit.EventRevoke, Detail: map[string]any{"slug": body.Slug},
	})
	w.WriteHeader(http.StatusNoContent)
}

func sessionAccountID(ctx context.Context) *int32 {
	session := authn.SessionFromContext(ctx)
	if session == nil {
		return nil
	}
	return &session.Account.ID
}
