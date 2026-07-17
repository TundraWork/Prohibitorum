// Package server — handle_federation_confirm.go
//
// The /welcome federated-identity confirmation step. When the callback resolves
// a first-time (unconfirmed) federated identity it WITHHOLDS the durable session
// and instead mints a single-use, browser-bound confirmation grant (handled in
// handle_federation.go). These three public endpoints drive the /welcome page:
//
//	GET  /api/prohibitorum/auth/federation/confirm          → pending identity + avatar status
//	POST /api/prohibitorum/auth/federation/confirm          → YES: confirm + issue session
//	POST /api/prohibitorum/auth/federation/confirm/decline  → NO: invalidate the grant
//
// The grant lives in KV (ConfirmKey); the fed-state cookie carries
// "<grant-token>.<anti-forgery>". The GET peeks (non-consuming) so the page can
// render; the POST pops (single-use) so a confirmation cannot be replayed. Every
// invalid-grant branch collapses onto federation_state_invalid.
package server

import (
	"context"
	"net/http"
	"strings"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/avatar"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
	sessstore "prohibitorum/pkg/session"
)

// confirmFedQueries is the narrow query surface the /welcome confirm endpoints
// need: the GET reads the pending account + IdP display name; the POST stamps
// confirmed_at. Declared so tests can stub it (via Server.confirmFedOverride)
// without standing up *db.Queries; production leaves the override nil and
// confirmFedQ falls back to s.queries.
type confirmFedQueries interface {
	GetAccountByID(ctx context.Context, id int32) (db.Account, error)
	GetUpstreamIDPBySlug(ctx context.Context, slug string) (db.UpstreamIdp, error)
	ConfirmAccountIdentity(ctx context.Context, id int64) error
}

func (s *Server) confirmFedQ() confirmFedQueries {
	if s.confirmFedOverride != nil {
		return s.confirmFedOverride
	}
	return s.queries
}

// handleFederationConfirmGet serves
// GET /api/prohibitorum/auth/federation/confirm — grant-scoped (fed-state
// cookie), no session. Reads the pending identity for the /welcome page.
func (s *Server) handleFederationConfirmGet(w http.ResponseWriter, r *http.Request) {
	token, anti := splitConfirmCookie(cookieValue(r, sessstore.FedStateCookieName))
	grant, err := s.federationService.PeekConfirmGrant(r.Context(), token, anti)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	acct, err := s.confirmFedQ().GetAccountByID(r.Context(), grant.AccountID)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	idp, err := s.confirmFedQ().GetUpstreamIDPBySlug(r.Context(), grant.ProviderSlug)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	view := contract.FederationConfirmView{
		IDPDisplayName: idp.DisplayName,
		DisplayName:    acct.DisplayName,
		Username:       acct.Username,
		Email:          acct.Email.String,
		AvatarPending:  s.federationService.AvatarPending(r.Context(), grant.AccountID),
	}
	if len(s.config.PublicOrigins) > 0 {
		if u := avatar.AccountURL(acct, s.config.PublicOrigins[0]); u != "" {
			view.AvatarURL = &u
		}
	}
	writeJSON(w, view)
}

// handleFederationConfirmPost serves
// POST /api/prohibitorum/auth/federation/confirm — YES: single-use-consume the
// grant, stamp confirmed_at, and issue the durable session. A second POST with
// the same (now-popped) grant fails closed (federation_state_invalid).
func (s *Server) handleFederationConfirmPost(w http.ResponseWriter, r *http.Request) {
	token, anti := splitConfirmCookie(cookieValue(r, sessstore.FedStateCookieName))
	grant, err := s.federationService.PopConfirmGrant(r.Context(), token, anti)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	// ConfirmAccountIdentity is idempotent (WHERE confirmed_at IS NULL): if the
	// identity was already confirmed by a concurrent request, this is a no-op and
	// the missing rows-affected check is intentional — the session is bound to
	// the grant's validated AccountID regardless.
	if err := s.confirmFedQ().ConfirmAccountIdentity(r.Context(), grant.IdentityID); err != nil {
		writeAuthErr(w, err)
		return
	}
	http.SetCookie(w, sessstore.ClearedFedStateCookie(s.config, r))

	ip := s.clientIP.IP(r)
	// Carry the upstream AMR through the grant so a first-login user who
	// completed MFA at the upstream IdP keeps the "mfa" amr claim in their
	// session. Backfill with the generic RFC 8176 "federated" value only when the
	// upstream did not assert any methods (common for OPs that omit the amr claim).
	amr := grant.AMR
	if len(amr) == 0 {
		amr = []string{"federated"}
	}
	if me := s.maintenanceLockout(r.Context(), grant.AccountID); me != nil {
		writeAuthErr(w, me)
		return
	}
	// H1-sch: stamp the upstream IdP onto the session row (federated discriminator).
	idpID := grant.ProviderID
	sess, _, err := s.sessionStore.Issue(r.Context(), grant.AccountID, ip, r.UserAgent(), amr, &idpID)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	accountID := grant.AccountID
	audit.RecordOrLog(r.Context(), s.Audit, audit.Record{
		AccountID: &accountID,
		Factor:    audit.FactorFederationOIDC,
		Event:     audit.EventUse,
		Detail:    map[string]any{"reason": "confirm", "idp_id": grant.ProviderID},
	})
	audit.RecordOrLog(r.Context(), s.Audit, audit.Record{
		AccountID: &accountID,
		Factor:    audit.FactorSession,
		Event:     audit.EventSessionStart,
		Detail:    map[string]any{"via": "federation"},
	})
	http.SetCookie(w, sessstore.FreshSessionCookie(s.config, r, grant.AccountID, sess, s.config.SessionTTL))
	writeJSON(w, map[string]string{"redirect": grant.ReturnTo})
}

// handleFederationConfirmDecline serves
// POST /api/prohibitorum/auth/federation/confirm/decline — NO: invalidate the
// grant (best-effort single-use consume) and clear the cookie. No session.
func (s *Server) handleFederationConfirmDecline(w http.ResponseWriter, r *http.Request) {
	token, anti := splitConfirmCookie(cookieValue(r, sessstore.FedStateCookieName))
	grant, _ := s.federationService.PopConfirmGrant(r.Context(), token, anti) // best-effort consume
	http.SetCookie(w, sessstore.ClearedFedStateCookie(s.config, r))
	// Emit the decline audit even on an invalid/missing grant (best-effort).
	var accountID *int32
	if grant != nil {
		id := grant.AccountID
		accountID = &id
	}
	audit.RecordOrLog(r.Context(), s.Audit, audit.Record{
		AccountID: accountID,
		Factor:    audit.FactorFederationOIDC,
		Event:     audit.EventFail,
		Detail:    map[string]any{"reason": "confirm_declined"},
	})
	w.WriteHeader(http.StatusNoContent)
}

// cookieValue returns the named cookie's value, or "" when absent.
func cookieValue(r *http.Request, name string) string {
	if c, err := r.Cookie(name); err == nil {
		return c.Value
	}
	return ""
}

// splitConfirmCookie splits the "<grant-token>.<anti-forgery>" fed-state cookie
// value the confirm flow sets. A value with no '.' yields the whole string as
// the token and an empty anti-forgery (which fails the browser-binding check).
func splitConfirmCookie(v string) (token, antiForgery string) {
	if i := strings.IndexByte(v, '.'); i >= 0 {
		return v[:i], v[i+1:]
	}
	return v, ""
}
