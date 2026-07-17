// Package server — handle_federation.go
//
// Public HTTP entrypoints for protocol-neutral upstream federation login.
//
//	GET /api/prohibitorum/auth/federation/{slug}/login?return_to=…
//	    → 302 to the upstream OP's /authorize URL.
//
//	GET /api/prohibitorum/auth/federation/{slug}/callback?code=…&state=…&iss=…
//	    → on success: issues a session cookie and 302s to the original return_to.
//	    → on upstream error= : audits + writes ErrUpstreamError.
//	    → on missing/bad state : writes ErrFederationStateInvalid (no audit row —
//	      stray browser hits should not flood the audit log).
//
// The opaque flow ID is sent as the upstream state parameter. Its browser-bound
// FlowState is stored at FlowKey(flowID), then acquired under an owner-fenced
// lease for callback processing. Replay, issuer-mismatch, and protocol
// verification failures collapse onto the single federation_state_invalid code
// to avoid leaking a side channel into the federation pipeline.
//
// Routes are mounted by server.go's registerOperations. NewServer populates
// Server.federationService; this file defines the handlers and return_to
// validator.
package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/federation"
	sessstore "prohibitorum/pkg/session"
	"prohibitorum/pkg/weberr"
)

// listFedQueries is the narrow query surface for handleListFederationProvidersHTTP.
// Tests inject a fake via Server.listFedOverride; production falls back to
// s.queries.
type listFedQueries interface {
	ListUpstreamIDPs(ctx context.Context) ([]db.UpstreamIdp, error)
	ListEntityIconEtags(ctx context.Context, ownerKind string) ([]db.ListEntityIconEtagsRow, error)
}

func (s *Server) listFedQ() listFedQueries {
	if s.listFedOverride != nil {
		return s.listFedOverride
	}
	return s.queries
}

// handleFederationLoginHTTP serves
// GET /api/prohibitorum/auth/federation/{slug}/login.
func (s *Server) handleFederationLoginHTTP(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")

	returnTo, err := s.validateFederationReturnTo(r.URL.Query().Get("return_to"))
	if err != nil {
		redirectAuthErrToError(w, r, err)
		return
	}

	req, err := s.federationService.BeginLogin(r.Context(), slug, returnTo)
	if err != nil {
		// returnTo is validated + same-origin — forward it so the /error
		// "go back" link can resume where the user started.
		if errors.Is(err, federation.ErrUnknownProvider) {
			// Collapse "no such slug" onto the generic state-invalid code so
			// callers can't enumerate configured upstream IdP slugs.
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

	// Bind the flow to this browser (N4): the anti-forgery cookie must come
	// back on the cross-site callback navigation, where it is matched against
	// the state's BrowserBinding.
	http.SetCookie(w, sessstore.FedStateCookie(s.config, r, req.BrowserToken))
	http.Redirect(w, r, destination, http.StatusFound)
}

// handleFederationCallbackHTTP serves
// GET /api/prohibitorum/auth/federation/{slug}/callback.
func (s *Server) handleFederationCallbackHTTP(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	upstreamErr := q.Get("error")
	upstreamDesc := q.Get("error_description")
	state := q.Get("state")
	code := q.Get("code")
	iss := q.Get("iss")

	if upstreamErr != "" {
		// Upstream OP refused the user. Emit an audit row (no account_id — we
		// never reached the resolve step), stamp a correlation ref, then
		// redirect to the SPA /error page.
		ref := weberr.NewRef()
		audit.RecordOrLog(r.Context(), s.Audit, audit.Record{
			Factor: audit.FactorFederationOIDC,
			Event:  audit.EventFail,
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

	// A valid callback carries either an OAuth2 authorization code (OIDC) or an
	// OpenID 2.0 id_res response (Steam). Both require a non-empty state token.
	// Stray requests (back-button replay, link previews) have neither — reject
	// them before touching the KV store. No audit: these are benign hits.
	isSteamCallback := code == "" && q.Get("openid.mode") == "id_res"
	if state == "" || (code == "" && !isSteamCallback) {
		redirectAuthErrToError(w, r, authn.ErrFederationStateInvalid())
		return
	}

	// Anti-forgery binding (N4): the cookie set at /login must come back here.
	// Absent/mismatched → HandleCallback rejects via the state's BrowserBinding.
	browserToken := ""
	if c, cerr := r.Cookie(sessstore.FedStateCookieName); cerr == nil {
		browserToken = c.Value
	}

	result, err := s.federationService.AdvanceCallback(r.Context(), federation.AdvanceRequest{
		FlowID: state, BrowserToken: browserToken, ProviderSlug: chi.URLParam(r, "slug"),
		CallbackRoute: federation.CallbackRoutePublic,
		Input:         federation.ActionInput{Kind: federation.ActionRedirect, Code: code, Issuer: iss, Params: r.URL.Query()},
	})
	if err != nil {
		// HandleCallback returns structured *authn.AuthError for every
		// expected failure (federation_state_invalid, bad_credentials,
		// email_not_verified, username_collision, …). Redirect to /error
		// instead of JSON — this is a full-page browser-navigated flow.
		redirectAuthErrToError(w, r, err)
		return
	}

	s.writeFederationCompletion(w, r, result, federationCompletionRedirect)
}

type federationCompletionMode uint8

const (
	federationCompletionRedirect federationCompletionMode = iota
	federationCompletionJSON
)

func (s *Server) writeFederationCompletion(w http.ResponseWriter, r *http.Request, result *federation.CompletionResult, mode federationCompletionMode) {
	if result == nil {
		s.writeFederationCompletionError(w, r, errors.New("federation: missing completion result"), mode)
		return
	}
	if result.Intent == federation.IntentLink {
		http.SetCookie(w, sessstore.ClearedFedStateCookie(s.config, r))
		s.writeFederationCompletionDestination(w, r, "/connected", mode)
		return
	}
	if !result.Confirmed {
		token, antiForgery, err := s.federationService.CreateConfirmGrant(
			r.Context(), result.AccountID, result.IdentityID, result.ProviderID,
			result.ProviderSlug, result.ReturnTo, result.AMR,
		)
		if err != nil {
			s.writeFederationCompletionError(w, r, err, mode)
			return
		}
		http.SetCookie(w, sessstore.FedStateCookie(s.config, r, token+"."+antiForgery))
		s.writeFederationCompletionDestination(w, r, "/welcome", mode)
		return
	}

	http.SetCookie(w, sessstore.ClearedFedStateCookie(s.config, r))
	if s.maintenanceLockout(r.Context(), result.AccountID) != nil {
		s.writeFederationCompletionDestination(w, r, "/", mode)
		return
	}
	amr := result.AMR
	if len(amr) == 0 {
		amr = []string{"federated"}
	}
	ip := r.RemoteAddr
	if s.clientIP != nil {
		ip = s.clientIP.IP(r)
	}
	idpID := result.ProviderID
	token, _, err := s.sessionStore.Issue(r.Context(), result.AccountID, ip, r.UserAgent(), amr, &idpID)
	if err != nil {
		s.writeFederationCompletionError(w, r, err, mode)
		return
	}
	http.SetCookie(w, sessstore.FreshSessionCookie(s.config, r, result.AccountID, token, s.config.SessionTTL))
	s.writeFederationCompletionDestination(w, r, result.ReturnTo, mode)
}

func (s *Server) writeFederationCompletionError(w http.ResponseWriter, r *http.Request, err error, mode federationCompletionMode) {
	if mode == federationCompletionRedirect {
		redirectAuthErrToError(w, r, err)
		return
	}
	writeAuthErr(w, err)
}

func (s *Server) writeFederationCompletionDestination(w http.ResponseWriter, r *http.Request, destination string, mode federationCompletionMode) {
	if mode == federationCompletionRedirect {
		http.Redirect(w, r, destination, http.StatusFound)
		return
	}
	writeJSON(w, contract.LoginResult{Redirect: destination})
}

// GET /api/prohibitorum/auth/federation — public list of enabled upstream IdPs
// for the login page's "sign in with" buttons. ListUpstreamIDPs already filters
// disabled rows and orders by display_name.
func (s *Server) handleListFederationProvidersHTTP(w http.ResponseWriter, r *http.Request) {
	idps, err := s.listFedQ().ListUpstreamIDPs(r.Context())
	if err != nil {
		writeAuthErr(w, fmt.Errorf("list federation providers: %w", err))
		return
	}
	// Best-effort: icon metadata is decorative — a lookup failure must not fail
	// the provider list (the buttons just fall back to the initial).
	icons, _ := s.listFedQ().ListEntityIconEtags(r.Context(), "upstream_idp")
	etagBySlug := make(map[string]string, len(icons))
	for _, ic := range icons {
		etagBySlug[ic.OwnerID] = ic.Etag
	}
	out := make([]contract.FederationProvider, 0, len(idps))
	for _, idp := range idps {
		// invite_only IdPs are reachable only via an invite link, never a
		// generic "sign in with" button — a plain login on one is rejected
		// pre-auth in begin(). Omit them so the login page never offers a
		// doomed button.
		if idp.Mode == federation.ModeInviteOnly {
			continue
		}
		out = append(out, contract.FederationProvider{
			Slug:        idp.Slug,
			DisplayName: idp.DisplayName,
			Protocol:    idp.Protocol,
			IconURL:     entityIconURLPtr("upstream_idp", idp.Slug, etagBySlug[idp.Slug]),
		})
	}
	writeJSON(w, out)
}

// validateFederationReturnTo is the fail-closed return_to policy for
// federation handlers. It delegates to resolveReturnTo (the shared same-origin
// core) and returns the safe relative path, or ErrInvalidReturnTo on any
// unsafe/off-origin input. Empty input → "/". Also accepts a same-origin
// absolute URL (normalised to relative), in addition to the original
// path-absolute relative ref. With a nil config, absolute URLs are rejected
// and relative paths pass — preserving the nil-config test behaviour. The
// returned path is built from parsed URL components (EscapedPath + RawQuery),
// not returned verbatim, so raw input is normalized (e.g. "/path with space"
// → "/path%20with%20space").
func (s *Server) validateFederationReturnTo(rt string) (string, error) {
	if p, ok := resolveReturnTo(rt, s.config); ok {
		return p, nil
	}
	return "", authn.ErrInvalidReturnTo()
}
