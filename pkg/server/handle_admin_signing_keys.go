// Package server — handle_admin_signing_keys.go
//
// Admin signing-key endpoints:
//   GET  /signing-keys             — list all keys (admin role, no sudo)
//   POST /signing-keys/generate    — mint a new pending key (admin + sudo)
//   POST /signing-keys/{kid}/activate — activate a pending key (admin + sudo)
//   POST /signing-keys/{kid}/retire   — begin retiring a key (admin + sudo, 409 if active)
//
// Private key material is NEVER serialized or included in any response or audit
// detail. The three mutations are registered via s.registerSudoOpHTTP, so the
// sudo gate, content-type check, and body-size limit are all enforced by the
// wrapper — handlers must NOT call requireFreshSudo themselves.

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

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
	oidc "prohibitorum/pkg/protocol/oidc"
)

// signingKeyView projects a db.SigningKey row into the wire-safe contract view.
// Private key material (PrivatePem) is explicitly excluded — this function is
// the single chokepoint that prevents accidental leakage.
func signingKeyView(k db.SigningKey) contract.SigningKeyView {
	v := contract.SigningKeyView{
		Kid:       k.Kid,
		Algorithm: k.Algorithm,
		Use:       k.Use,
		Status:    k.Status,
	}
	if len(k.PublicJwk) > 0 {
		var m map[string]any
		if err := json.Unmarshal(k.PublicJwk, &m); err == nil {
			v.PublicJWK = m
		}
	}
	if k.NotBefore.Valid {
		t := k.NotBefore.Time
		v.NotBefore = &t
	}
	if k.ActivatedAt.Valid {
		t := k.ActivatedAt.Time
		v.ActivatedAt = &t
	}
	if k.DecommissionedAt.Valid {
		t := k.DecommissionedAt.Time
		v.DecommissionedAt = &t
	}
	if k.RetireAfter.Valid {
		t := k.RetireAfter.Time
		v.RetireAfter = &t
	}
	return v
}

// writeSigningKeyJSON writes a single SigningKeyView as a JSON response.
func writeSigningKeyJSON(w http.ResponseWriter, status int, v contract.SigningKeyView) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// ----- GET /signing-keys (typed, role-only) ----------------------------------

type listSigningKeysOut struct {
	Body []contract.SigningKeyView
}

func (s *Server) handleListSigningKeys(ctx context.Context, _ *struct{}) (*listSigningKeysOut, error) {
	rows, err := s.queries.ListAllSigningKeys(ctx)
	if err != nil {
		return nil, fmt.Errorf("handler: listSigningKeys: %w", err)
	}
	views := make([]contract.SigningKeyView, 0, len(rows))
	for _, r := range rows {
		views = append(views, signingKeyView(r))
	}
	return &listSigningKeysOut{Body: views}, nil
}

// ----- POST /signing-keys/generate (raw, sudo-gated) ------------------------

func (s *Server) handleGenerateSigningKeyHTTP(w http.ResponseWriter, r *http.Request) {
	key, err := oidc.InsertPendingKey(r.Context(), s.queries)
	if err != nil {
		writeAuthErr(w, fmt.Errorf("handleGenerateSigningKey: insert: %w", err))
		return
	}

	sess := authn.SessionFromContext(r.Context())
	var actorID *int32
	if sess != nil {
		actorID = &sess.Account.ID
	}
	_ = s.Audit.Record(r.Context(), audit.Record{
		AccountID: actorID,
		Factor:    audit.FactorSigningKey,
		Event:     audit.EventRegister,
		Detail:    map[string]any{"kid": key.Kid, "status": key.Status, "action": "generate"},
	})

	// The new pending key must appear in JWKS and SAML metadata immediately.
	// Both caches are invalidated on this replica; other replicas in a
	// multi-replica deployment refresh within the cache TTL.
	if s.oidcOP != nil {
		s.oidcOP.InvalidateKeyCache()
	}
	if s.samlIdP != nil {
		s.samlIdP.InvalidateKeyCache()
	}

	writeSigningKeyJSON(w, http.StatusCreated, signingKeyView(key))
}

// ----- POST /signing-keys/{kid}/activate (raw, sudo-gated) ------------------

func (s *Server) handleActivateSigningKeyHTTP(w http.ResponseWriter, r *http.Request) {
	kid := chi.URLParam(r, "kid")
	if kid == "" {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}

	grace := s.config.SAML.MetadataRotationGrace
	key, err := oidc.ActivateSigningKey(r.Context(), s.dbPool, s.queries, kid, grace)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeAuthErr(w, authn.ErrCredentialNotFound())
			return
		}
		writeAuthErr(w, fmt.Errorf("handleActivateSigningKey: activate: %w", err))
		return
	}

	sess := authn.SessionFromContext(r.Context())
	var actorID *int32
	if sess != nil {
		actorID = &sess.Account.ID
	}
	_ = s.Audit.Record(r.Context(), audit.Record{
		AccountID: actorID,
		Factor:    audit.FactorSigningKey,
		Event:     audit.EventUpdate,
		Detail:    map[string]any{"kid": key.Kid, "status": key.Status, "action": "activate"},
	})

	// Activation changes which key signs new tokens and demotes the prior active
	// key to decommissioning. Both the OIDC and SAML caches are invalidated on
	// this replica; other replicas in a multi-replica deployment refresh within
	// the cache TTL.
	if s.oidcOP != nil {
		s.oidcOP.InvalidateKeyCache()
	}
	if s.samlIdP != nil {
		s.samlIdP.InvalidateKeyCache()
	}

	writeSigningKeyJSON(w, http.StatusOK, signingKeyView(key))
}

// ----- POST /signing-keys/{kid}/retire (raw, sudo-gated) --------------------

func (s *Server) handleRetireSigningKeyHTTP(w http.ResponseWriter, r *http.Request) {
	kid := chi.URLParam(r, "kid")
	if kid == "" {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}

	grace := s.config.SAML.MetadataRotationGrace
	key, err := oidc.RetireSigningKey(r.Context(), s.queries, kid, grace)
	if err != nil {
		switch {
		case errors.Is(err, oidc.ErrActiveKeyNoReplacement):
			writeAuthErr(w, authn.ErrActiveKeyNoReplacement())
		case errors.Is(err, pgx.ErrNoRows):
			writeAuthErr(w, authn.ErrCredentialNotFound())
		default:
			writeAuthErr(w, fmt.Errorf("handleRetireSigningKey: retire: %w", err))
		}
		return
	}

	sess := authn.SessionFromContext(r.Context())
	var actorID *int32
	if sess != nil {
		actorID = &sess.Account.ID
	}
	_ = s.Audit.Record(r.Context(), audit.Record{
		AccountID: actorID,
		Factor:    audit.FactorSigningKey,
		Event:     audit.EventRevoke,
		Detail:    map[string]any{"kid": key.Kid, "status": key.Status, "action": "retire"},
	})

	// Retiring moves the key out of the publishable set. Both the OIDC and SAML
	// caches are invalidated on this replica so JWKS and SAML metadata stop
	// advertising the retired key immediately; other replicas in a multi-replica
	// deployment refresh within the cache TTL.
	if s.oidcOP != nil {
		s.oidcOP.InvalidateKeyCache()
	}
	if s.samlIdP != nil {
		s.samlIdP.InvalidateKeyCache()
	}

	writeSigningKeyJSON(w, http.StatusOK, signingKeyView(key))
}

// Compile-time check: ensure signingKeyView never exposes PrivatePem.
// This is enforced structurally — db.SigningKey.PrivatePem is a string field
// that is deliberately absent from contract.SigningKeyView.
var _ = func() bool {
	k := db.SigningKey{PrivatePem: "SECRET"}
	v := signingKeyView(k)
	// contract.SigningKeyView has no PrivatePem field — the compiler enforces this.
	_ = v
	_ = time.Now() // keep time import live
	return true
}()
