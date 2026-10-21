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
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"

	"github.com/go-chi/chi/v5"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/federation"
	sessstore "prohibitorum/pkg/session"
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

	req, err := s.federationService.BeginPublic(r.Context(), slug, returnTo, rememberRequested(r))
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

// enabledFederationName returns the display name of the enabled provider with
// this slug, or "" when it is unknown, disabled or cannot be read. Public
// errors are named only through it, so they never reveal a provider the public
// list at GET /auth/federation does not show.
func (s *Server) enabledFederationName(ctx context.Context, slug string) string {
	if s.federationService == nil {
		return ""
	}
	return s.federationService.EnabledProviderName(ctx, slug)
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
	idToken := q.Get("id_token")
	accessToken := q.Get("access_token")

	if upstreamErr != "" {
		// Upstream OP refused the user. Emit an audit row (no account_id — we
		// never reached the resolve step), stamp a correlation ref, then
		// redirect to the SPA /error page.
		// The /error redirect names the provider when it is enabled, and its
		// ref goes on the audit row so the two correlate.
		named := authn.WithFederationName(authn.ErrUpstreamError(upstreamErr, upstreamDesc), s.enabledFederationName(r.Context(), chi.URLParam(r, "slug")))
		ref := redirectAuthErrToError(w, r, named)
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
		return
	}

	// An implicit-flow response (public-client OIDC) puts every parameter in
	// the URI fragment, which the browser never sends, so it arrives with an
	// empty query. The relay page moves the fragment into the query and
	// requests this URL again.
	if r.URL.RawQuery == "" {
		writeFragmentRelay(w)
		return
	}
	// A valid callback carries an OAuth2 authorization code (OIDC), a relayed
	// id_token (implicit flow), or an OpenID 2.0 id_res response (Steam). All
	// require a non-empty state token. Stray requests (back-button replay,
	// link previews) have none — reject them before touching the KV store.
	// No audit: these are benign hits.
	isSteamCallback := code == "" && q.Get("openid.mode") == "id_res"
	if state == "" || (code == "" && idToken == "" && !isSteamCallback) {
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
		Input:         federation.ActionInput{Kind: federation.ActionRedirect, Code: code, Issuer: iss, IDToken: idToken, AccessToken: accessToken, Params: r.URL.Query()},
	})
	if err != nil {
		// HandleCallback returns structured *authn.AuthError for every
		// expected failure (federation_state_invalid, bad_credentials,
		// email_not_verified, username_collision, …). Redirect to /error
		// instead of JSON — this is a full-page browser-navigated flow. The
		// flow's validated return_to rides along, so the error page can name
		// the application and offer the way back to it.
		var callbackErr *federation.CallbackError
		if errors.As(err, &callbackErr) {
			redirectAuthErrToErrorReturn(w, r, err, callbackErr.ReturnTo)
			return
		}
		redirectAuthErrToError(w, r, err)
		return
	}

	s.writeFederationCompletion(w, r, result, federationCompletionRedirect)
}

// fragmentRelayScript copies the fragment into the query and loads the URL
// again. An empty fragment becomes relay=empty, so the second request has a
// query and fails the callback's own checks instead of serving this page in
// a loop.
//
// The relayed id_token then travels in the query string, so a reverse proxy
// that logs request URLs records it, with the email and name claims it
// carries. It cannot be replayed: the nonce belongs to a flow state that the
// first callback consumes, and the token expires within minutes. Keeping it
// out of URLs altogether needs response_mode=form_post, which arrives as a
// cross-site POST that carries no SameSite=Lax federation cookie, so the
// callback would have to accept POST and re-post same-site before it can
// check the browser binding.
const fragmentRelayScript = `var p = new URLSearchParams(location.hash.slice(1));
if (!p.toString()) p.set("relay", "empty");
var t = new URL(location.href);
t.hash = "";
t.search = "?" + p.toString();
location.replace(t.href);`

// fragmentRelayCSP admits only the relay script, so the page stays inert even
// where no proxy adds a policy for API routes.
var fragmentRelayCSP = func() string {
	sum := sha256.Sum256([]byte(fragmentRelayScript))
	return "default-src 'none'; script-src 'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"
}()

// writeFragmentRelay serves the page an implicit-flow callback lands on before
// its parameters reach the server.
func writeFragmentRelay(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", fragmentRelayCSP)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, `<!doctype html><meta charset="utf-8"><title>Signing in</title><script>`+fragmentRelayScript+`</script><noscript><p>Turn on JavaScript in this browser, then sign in again.</p></noscript>`)
}

type federationCompletionMode uint8

const (
	federationCompletionRedirect federationCompletionMode = iota
	federationCompletionJSON
)

func (s *Server) writeFederationCompletion(w http.ResponseWriter, r *http.Request, result *federation.CompletionResult, mode federationCompletionMode) {
	if err := result.Validate(); err != nil {
		s.writeFederationCompletionError(w, r, err, mode)
		return
	}
	if result.Enrollment != nil {
		// Verify-then-create invite: the snapshot landed on the invitation,
		// no account exists yet; clear the fed-state cookie and continue at
		// the enrollment page.
		http.SetCookie(w, sessstore.ClearedFedStateCookie(s.config, r))
		s.writeFederationCompletionDestination(w, r, "/enroll/"+url.PathEscape(result.Enrollment.Token), mode)
		return
	}
	if result.Intent == federation.IntentLink {
		http.SetCookie(w, sessstore.ClearedFedStateCookie(s.config, r))
		s.writeFederationCompletionDestination(w, r, linkCompletionDestination(result.ReturnTo), mode)
		return
	}
	if !result.Confirmed {
		token, antiForgery, err := s.federationService.CreateConfirmGrant(
			r.Context(), result.AccountID, result.IdentityID, result.ProviderID,
			result.ProviderSlug, result.ReturnTo, result.App, result.AMR, result.OfferLocalSignin, result.Remember,
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
	token, data, err := s.sessionStore.Issue(r.Context(), result.AccountID, ip, r.UserAgent(), amr, &idpID, result.Remember)
	if err != nil {
		s.writeFederationCompletionError(w, r, err, mode)
		return
	}
	http.SetCookie(w, s.sessionStore.SessionCookie(s.config, r, result.AccountID, token, data))
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
		// invite_only IdPs are listed like any other: an unknown identity
		// hitting one is rejected at resolve time (no_account_invite_only),
		// while existing users sign in through it directly. The login page
		// filters on allowSignIn; "linked identities" reads the same list
		// unfiltered.
		out = append(out, contract.FederationProvider{
			Slug:        idp.Slug,
			DisplayName: idp.DisplayName,
			Protocol:    idp.Protocol,
			IconURL:     entityIconURLPtr("upstream_idp", idp.Slug, etagBySlug[idp.Slug]),
			AllowSignIn: idp.AllowSignIn,
		})
	}
	writeJSON(w, out)
}

// requireIdentityPagePattern is the in-between page an application's
// require-identity request sends the browser to.
var requireIdentityPagePattern = regexp.MustCompile(`^/require-identity/[A-Za-z0-9_-]+$`)

// linkCompletionDestination is where a finished link returns: the
// require-identity page that started it, or Security. Any other return_to the
// flow carries is ignored, so a link never ends on a page of the starter's
// choosing.
func linkCompletionDestination(returnTo string) string {
	if requireIdentityPagePattern.MatchString(returnTo) {
		return returnTo
	}
	return "/security"
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
