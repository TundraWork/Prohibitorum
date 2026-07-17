// Package server — handle_me_identities.go
//
// /me/identities surface for managing federated identities bound to the
// signed-in account:
//
//	GET  /api/prohibitorum/me/identities                       — list rows
//	POST /api/prohibitorum/me/identities/{id}/unlink           — sudo; delete row
//	GET  /api/prohibitorum/me/identities/link/{slug}/begin     — sudo; → upstream
//	GET  /api/prohibitorum/me/identities/link/{slug}/callback  — bind upstream
//
// The link/begin + link/callback pair uses the protocol-neutral federation
// service shared by public login and invite flows. Persisted state binds the
// current account and session so callback processing rejects a session swap.
// A successful callback links the verified identity without issuing a new
// session.

package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/sirupsen/logrus"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/federation"
	"prohibitorum/pkg/logx"
	sessstore "prohibitorum/pkg/session"
	"prohibitorum/pkg/weberr"
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
	ListAccountIdentitiesByAccount(ctx context.Context, accountID int32) ([]db.ListAccountIdentitiesByAccountRow, error)
	DeleteAccountIdentity(ctx context.Context, arg db.DeleteAccountIdentityParams) (int64, error)
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

// projectIdentityRow returns the shared secret-free identity projection. A
// malformed stored metadata object cannot block the account page: it is
// replaced with an empty object and logged without the raw value.
func projectIdentityRow(ctx context.Context, row db.ListAccountIdentitiesByAccountRow) contract.AccountIdentityView {
	data := make(map[string]string)
	if len(row.UpstreamData) > 0 {
		var decoded map[string]string
		if err := json.Unmarshal(row.UpstreamData, &decoded); err != nil || decoded == nil {
			logx.WithContext(ctx).WithFields(logrus.Fields{
				"event":       "identity_metadata_invalid",
				"identity_id": row.ID,
				"provider_id": row.UpstreamIdpID,
			}).Error("identity metadata invalid")
		} else {
			data = decoded
		}
	}
	return contract.AccountIdentityView{
		ID:                  row.ID,
		ProviderSlug:        row.IdpSlug,
		ProviderDisplayName: row.IdpDisplayName,
		Protocol:            row.Protocol,
		Subject:             row.UpstreamSub,
		Email:               textPtr(row.UpstreamEmail),
		Data:                data,
		LinkedAt:            row.LinkedAt.Time,
	}
}

// GET /api/prohibitorum/me/identities
//
// Returns a bare JSON array of the account's federated identities (no
// pagination envelope). Self-service identity listing is bounded by the
// small number of IdPs an account links, so keyset pagination is
// unnecessary here; the admin nested collections keep their own page
// contract. The empty case serializes as [] (never null) so JS clients
// can .map() without a nil-guard.
func (s *Server) handleMeIdentitiesListHTTP(w http.ResponseWriter, r *http.Request) {
	sess := authn.SessionFromContext(r.Context())
	rows, err := s.meIdentitiesQ().ListAccountIdentitiesByAccount(r.Context(), sess.Account.ID)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	out := make([]contract.AccountIdentityView, 0, len(rows))
	for _, row := range rows {
		out = append(out, projectIdentityRow(r.Context(), row))
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
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
	// the now-last-method removal. Audited race: M3 in the federation audit.
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

	// Capture identity metadata for audit enrichment before the delete.
	// ListAccountIdentitiesByAccount is scoped to sess.Account.ID, so we
	// find the row by id64 within the locked transaction. If the row is not
	// found (foreign id or already deleted), the subsequent delete will
	// return ErrNoRows and the handler will 404 — we don't need to handle
	// that case here.
	var (
		auditIdpSlug     string
		auditUpstreamIs  string
		auditUpstreamSub string
	)
	if identRows, _ := q.ListAccountIdentitiesByAccount(r.Context(), sess.Account.ID); identRows != nil {
		for _, row := range identRows {
			if row.ID == id64 {
				auditIdpSlug = row.IdpSlug
				auditUpstreamIs = row.UpstreamIss
				auditUpstreamSub = row.UpstreamSub
				break
			}
		}
	}

	// Delete-then-check: delete the identity row first (within the locked
	// tx), then call AvailableMethods to see how many usable sign-in methods
	// remain in the post-delete state. This is safer than check-then-delete
	// because it avoids the divergence between a raw identity-row count and
	// AvailableMethods' usable-federation count (which excludes identities
	// whose upstream IdP is DISABLED). With the old pattern, an account
	// holding one enabled-upstream identity + one disabled-upstream identity
	// would pass the raw len(identities)>1 gate and allow unlinking the
	// enabled one, leaving the account with zero usable sign-in methods
	// (admin recovery required). The delete-then-check pattern sees the
	// real post-delete usable count; if it's zero the deferred rollback()
	// undoes the delete and the unlink is refused.
	deletedID, err := q.DeleteAccountIdentity(r.Context(), db.DeleteAccountIdentityParams{
		ID:        id64,
		AccountID: sess.Account.ID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// The (id, account_id) pair didn't match a row — caller asked to
			// delete an identity that either doesn't exist or belongs to a
			// different account. 404 + no audit (avoid polluting the log
			// with no-op unlinks; audit M1-di finding). Nothing was changed,
			// so no lockout check is needed.
			writeAuthErr(w, authn.ErrCredentialNotFound())
			return
		}
		writeAuthErr(w, err)
		return
	}

	// Re-check available methods AFTER the delete. AvailableMethods uses
	// CountUsableSignInFederation (not a raw identity count), so disabled-
	// upstream links are excluded from the usable-federation tally. If the
	// deleted identity was the last usable sign-in method, len(methods)==0
	// and we refuse; the deferred rollback() undoes the delete.
	methods, err := authn.AvailableMethods(r.Context(), q, sess.Account.ID)
	if err != nil && !errors.Is(err, authn.ErrNoUsableMethod) {
		writeAuthErr(w, err)
		return
	}
	if len(methods) == 0 {
		writeAuthErr(w, authn.ErrLastSignInMethod())
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
	audit.RecordOrLog(r.Context(), s.Audit, audit.Record{
		AccountID: &acct,
		Factor:    audit.FactorFederationOIDC,
		Event:     audit.EventUnlink,
		Detail: map[string]any{
			"identity_id":  deletedID,
			"idp_slug":     auditIdpSlug,
			"upstream_iss": auditUpstreamIs,
			"upstream_sub": auditUpstreamSub,
		},
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
		redirectAuthErrToError(w, r, err)
		return
	}

	req, err := s.federationService.BeginLink(r.Context(), slug, returnTo, sess.Account.ID, sess.Data.SessionID)
	if err != nil {
		// returnTo is validated + same-origin (e.g. /connected) — forward it so
		// the /error "go back" link returns the user to where they started.
		if errors.Is(err, federation.ErrUnknownProvider) {
			// Collapse "no such slug" onto the generic state-invalid code —
			// mirrors handleFederationLoginHTTP so admins can't enumerate
			// configured IdPs via the link surface either.
			redirectAuthErrToErrorReturn(w, r, authn.ErrFederationStateInvalid(), returnTo)
			return
		}
		redirectAuthErrToErrorReturn(w, r, err, returnTo)
		return
	}
	destination, err := federationBeginDestination(req)
	if err != nil {
		redirectAuthErrToErrorReturn(w, r, err, returnTo)
		return
	}

	http.SetCookie(w, sessstore.FedStateCookie(s.config, r, req.BrowserToken))
	http.Redirect(w, r, destination, http.StatusFound)
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
		// embed it in the audit row. Generate a ref first so both the audit
		// Detail and the /error redirect carry the same correlation token.
		ref := weberr.NewRef()
		acct := sess.Account.ID
		audit.RecordOrLog(r.Context(), s.Audit, audit.Record{
			AccountID: &acct,
			Factor:    audit.FactorFederationOIDC,
			Event:     audit.EventFail,
			Detail: map[string]any{
				"reason":               "upstream_error",
				"upstream_code":        upstreamErr,
				"upstream_description": upstreamDesc,
				"ref":                  ref,
			},
		})
		weberr.RedirectToError(w, r, authn.ErrUpstreamError(upstreamErr, upstreamDesc).Code, ref)
		return
	}

	// OpenID 2.0 callbacks (Steam) carry no code — detect them by openid.mode
	// before applying the OIDC-style code guard. Mirrors the same pattern in
	// handle_federation.go handleFederationLoginCallbackHTTP.
	isSteamCallback := code == "" && q.Get("openid.mode") == "id_res"
	if state == "" || (code == "" && !isSteamCallback) {
		// Stray callbacks have no flow to attribute. Reject them without an
		// audit row; nonempty malformed or replayed state reaches the service,
		// which records the account-attributed structured failure.
		redirectAuthErrToError(w, r, authn.ErrFederationStateInvalid())
		return
	}

	browserToken := ""
	if cookie, cookieErr := r.Cookie(sessstore.FedStateCookieName); cookieErr == nil {
		browserToken = cookie.Value
	}
	result, err := s.federationService.AdvanceCallback(r.Context(), federation.AdvanceRequest{
		FlowID: state, BrowserToken: browserToken, ProviderSlug: chi.URLParam(r, "slug"),
		CallbackRoute: federation.CallbackRouteLink,
		AccountID:     new(sess.Account.ID), SessionID: sess.Data.SessionID,
		Input: federation.ActionInput{Kind: federation.ActionRedirect, Code: code, Issuer: iss, Params: r.URL.Query()},
	})
	if err != nil {
		// The federation service already emitted a fail audit row for each
		// structured failure mode (state_invalid, session_swap,
		// iss_mismatch_callback, code_exchange_failed, link_insert_failed,
		// link_conflict).
		// Redirect to /error — this is a full-page browser-navigated flow.
		redirectAuthErrToError(w, r, err)
		return
	}

	s.writeFederationCompletion(w, r, result, federationCompletionRedirect)
}
