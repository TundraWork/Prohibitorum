// Package server — handle_me_identities.go
//
// /me/identities surface for managing federated identities bound to the
// signed-in account (v0.3, Task 8):
//
//	GET  /api/prohibitorum/me/identities                       — list rows
//	POST /api/prohibitorum/me/identities/{id}/unlink           — sudo; delete row
//	GET  /api/prohibitorum/me/identities/link/{slug}/begin     — sudo; → upstream
//	GET  /api/prohibitorum/me/identities/link/{slug}/callback  — bind upstream
//
// The link/begin + link/callback pair runs the same OIDC RP dance as the
// public /auth/federation/{slug}/* handlers, but stashed under LinkKey(state)
// and bound to the current account_id so the federator can refuse a
// session-swap mid-flow. The callback does NOT issue a new session — the
// user is already signed in; we only insert account_identity.
//
// Route mounting lives in Task 9 (server.go). This file defines only the
// handlers and a couple of narrowly-scoped helpers (last-sign-in-method
// check, response-shape projection).

package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/db"
	fedoidc "prohibitorum/pkg/federation/oidc"
)

// meIdentitiesQueries is the narrow query surface the /me/identities
// handlers need: AvailableMethods (via authn.FlowQueries) plus the two
// account_identity mutations the list+unlink endpoints touch. Production
// uses *db.Queries; tests inject a fake by widening s.revokeFlowOverride
// (which is the closest existing seam — Task 6 already typed it as
// authn.FlowQueries) to also satisfy this interface.
//
// GetAccountByIDForUpdate is the row-level lock used by the unlink handler
// to serialize concurrent /unlink requests against the same account. The
// FOR UPDATE clause is enforced by Postgres; in-memory test fakes simply
// record the call (no actual lock).
type meIdentitiesQueries interface {
	authn.FlowQueries
	DeleteAccountIdentity(ctx context.Context, arg db.DeleteAccountIdentityParams) error
	GetAccountByIDForUpdate(ctx context.Context, id int32) (db.Account, error)
}

// meIdentitiesQ resolves the meIdentitiesQueries surface. Reuses
// s.revokeFlowOverride when it widens far enough; otherwise falls back to
// the concrete s.queries (production path).
func (s *Server) meIdentitiesQ() meIdentitiesQueries {
	if s.revokeFlowOverride != nil {
		if q, ok := s.revokeFlowOverride.(meIdentitiesQueries); ok {
			return q
		}
	}
	return s.queries
}

// identityView is the JSON projection of ListAccountIdentitiesByAccountRow
// returned by GET /me/identities. UpstreamEmail is a *string so absent
// addresses serialize as null rather than the empty string — UI code that
// branches on "has the OP given us an email?" reads the null cleanly.
type identityView struct {
	ID             int64   `json:"id"`
	IdpSlug        string  `json:"idpSlug"`
	IdpDisplayName string  `json:"idpDisplayName"`
	UpstreamEmail  *string `json:"upstreamEmail"`
	LinkedAt       string  `json:"linkedAt"`
}

// GET /api/prohibitorum/me/identities
func (s *Server) handleMeIdentitiesListHTTP(w http.ResponseWriter, r *http.Request) {
	sess := authn.SessionFromContext(r.Context())
	rows, err := s.meIdentitiesQ().ListAccountIdentitiesByAccount(r.Context(), sess.Account.ID)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	// Empty result must be [], not null — JS clients .map() the response
	// without a nil-guard.
	out := make([]identityView, 0, len(rows))
	for _, row := range rows {
		out = append(out, projectIdentityRow(row))
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func projectIdentityRow(row db.ListAccountIdentitiesByAccountRow) identityView {
	v := identityView{
		ID:             row.ID,
		IdpSlug:        row.IdpSlug,
		IdpDisplayName: row.IdpDisplayName,
	}
	if row.UpstreamEmail.Valid {
		s := row.UpstreamEmail.String
		v.UpstreamEmail = &s
	}
	v.LinkedAt = row.LinkedAt.Time.UTC().Format(time.RFC3339)
	return v
}

// POST /api/prohibitorum/me/identities/{id}/unlink
func (s *Server) handleMeIdentitiesUnlinkHTTP(w http.ResponseWriter, r *http.Request) {
	sess := authn.SessionFromContext(r.Context())
	if s.requireFreshSudo(r.Context(), w, sess) {
		return
	}

	idStr := chi.URLParam(r, "id")
	id64, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}

	// Race-free check+delete: wrap the last-sign-in-method gate and the
	// DeleteAccountIdentity in a single transaction, with a row-level
	// SELECT … FOR UPDATE on the account row up front. Two concurrent
	// unlink requests against the same account_id serialize on this lock,
	// so the second one sees the post-delete state and (correctly) rejects
	// the now-last-method removal. Audited race: M3 in v0.3 audit.
	//
	// In tests, s.dbPool is nil and s.meIdentitiesQ() resolves to a fake
	// that satisfies the surface without a real Postgres lock (we exercise
	// the lock-acquisition ordering via the fake; the actual concurrency
	// guarantee is asserted at the cmd/smoke layer against real PG).
	q, commit, rollback, err := s.beginMeIdentitiesTx(r.Context())
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	defer rollback()

	// Acquire row-level lock on the account before any read. Concurrent
	// unlinks block here until the first one commits or rolls back.
	if _, err := q.GetAccountByIDForUpdate(r.Context(), sess.Account.ID); err != nil {
		writeAuthErr(w, err)
		return
	}

	// Re-do the last-sign-in-method check INSIDE the locked transaction.
	// federation_oidc remains iff >1 identity row existed before; webauthn
	// + password_totp survive unconditionally.
	methods, err := authn.AvailableMethods(r.Context(), q, sess.Account.ID)
	if err != nil && !errors.Is(err, authn.ErrNoUsableMethod) {
		writeAuthErr(w, err)
		return
	}
	identities, err := q.ListAccountIdentitiesByAccount(r.Context(), sess.Account.ID)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	federationStillAvailable := len(identities) > 1
	nonFederationMethods := 0
	for _, m := range methods {
		if m != authn.MethodFederationOIDC {
			nonFederationMethods++
		}
	}
	if !federationStillAvailable && nonFederationMethods == 0 {
		writeAuthErr(w, authn.ErrLastSignInMethod())
		return
	}

	if err := q.DeleteAccountIdentity(r.Context(), db.DeleteAccountIdentityParams{
		ID:        id64,
		AccountID: sess.Account.ID,
	}); err != nil {
		writeAuthErr(w, err)
		return
	}

	if err := commit(); err != nil {
		writeAuthErr(w, err)
		return
	}

	// Audit AFTER commit: we don't want to audit a delete that may have
	// been rolled back. The audit row lives in its own (separate) write,
	// so a missed audit here costs us nothing structural — the unlink is
	// already persisted.
	acct := sess.Account.ID
	_ = s.Audit.Record(r.Context(), audit.Record{
		AccountID: &acct,
		Factor:    audit.FactorFederationOIDC,
		Event:     audit.EventUnlink,
		Detail:    map[string]any{"identity_id": id64},
	})

	w.WriteHeader(http.StatusNoContent)
}

// beginMeIdentitiesTx returns the meIdentitiesQueries surface bound to a
// fresh transaction, plus commit/rollback closures. In production, this
// opens a real pgxpool transaction and wraps s.queries.WithTx(tx). In
// tests (when s.dbPool is nil), it returns the existing fake querier with
// no-op commit/rollback — the fake's job is to record the lock-acquisition
// ordering; the real concurrency guarantee is asserted at the smoke layer.
func (s *Server) beginMeIdentitiesTx(ctx context.Context) (meIdentitiesQueries, func() error, func(), error) {
	if s.dbPool == nil {
		// Test path: no real DB. The fake querier carries through, and the
		// commit/rollback closures are no-ops. The handler still calls
		// GetAccountByIDForUpdate first, which the fake records, so the
		// "lock is acquired in the right place" assertion works.
		q := s.meIdentitiesQ()
		return q, func() error { return nil }, func() {}, nil
	}
	tx, err := s.dbPool.Begin(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	qtx := s.queries.WithTx(tx)
	commit := func() error { return tx.Commit(ctx) }
	rollback := func() { _ = tx.Rollback(ctx) } // safe to call after commit; pgx returns ErrTxClosed which we ignore.
	return qtx, commit, rollback, nil
}

// GET /api/prohibitorum/me/identities/link/{slug}/begin
func (s *Server) handleMeIdentitiesLinkBeginHTTP(w http.ResponseWriter, r *http.Request) {
	sess := authn.SessionFromContext(r.Context())
	if s.requireFreshSudo(r.Context(), w, sess) {
		return
	}

	slug := chi.URLParam(r, "slug")
	returnTo, err := s.validateFederationReturnTo(r.URL.Query().Get("return_to"))
	if err != nil {
		writeAuthErr(w, err)
		return
	}

	req, err := s.federator.LinkBegin(r.Context(), sess.Account.ID, slug, returnTo)
	if err != nil {
		if errors.Is(err, fedoidc.ErrUnknownIDP) {
			// Collapse "no such slug" onto the generic state-invalid code —
			// mirrors handleFederationLoginHTTP so admins can't enumerate
			// configured IdPs via the link surface either.
			writeAuthErr(w, authn.ErrFederationStateInvalid())
			return
		}
		writeAuthErr(w, err)
		return
	}

	http.Redirect(w, r, req.AuthorizeURL, http.StatusFound)
}

// GET /api/prohibitorum/me/identities/link/{slug}/callback
func (s *Server) handleMeIdentitiesLinkCallbackHTTP(w http.ResponseWriter, r *http.Request) {
	sess := authn.SessionFromContext(r.Context())
	// No sudo here — the link callback completes a ceremony the user
	// initiated under sudo at /begin. Forcing sudo again after the upstream
	// round-trip would force a re-elevation in the same browser flow.

	q := r.URL.Query()
	upstreamErr := q.Get("error")
	upstreamDesc := q.Get("error_description")
	state := q.Get("state")
	code := q.Get("code")
	iss := q.Get("iss")

	if upstreamErr != "" {
		// The user is already authenticated, so we know the account_id —
		// embed it in the audit row (unlike the unauthenticated login
		// callback, where we have no account context at this point).
		acct := sess.Account.ID
		_ = s.Audit.Record(r.Context(), audit.Record{
			AccountID: &acct,
			Factor:    audit.FactorFederationOIDC,
			Event:     audit.EventFail,
			Detail: map[string]any{
				"reason":               "upstream_error",
				"upstream_code":        upstreamErr,
				"upstream_description": upstreamDesc,
			},
		})
		writeAuthErr(w, authn.ErrUpstreamError(upstreamErr, upstreamDesc))
		return
	}

	if state == "" || code == "" {
		// Stray / replayed callback. Federator's LinkCallback would also
		// reject this on the KV Pop, but short-circuiting here keeps the
		// audit log clean of accidental hits.
		writeAuthErr(w, authn.ErrFederationStateInvalid())
		return
	}

	result, err := s.federator.LinkCallback(r.Context(), state, code, iss, sess.Account.ID)
	if err != nil {
		// Federator already emitted a fail audit row for each structured
		// failure mode (state_invalid, session_swap, iss_mismatch_callback,
		// code_exchange_failed, link_insert_failed, link_conflict).
		// writeAuthErr collapses every expected variant onto
		// federation_state_invalid via authn.AuthError.
		writeAuthErr(w, err)
		return
	}

	// Federator's LinkCallback already emitted EventLink on success
	// (pkg/federation/oidc/federation.go ~line 368). Do NOT double-audit.
	// No session.Issue — the user is already logged in.
	http.Redirect(w, r, result.ReturnTo, http.StatusFound)
}
