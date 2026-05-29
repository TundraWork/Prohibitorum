package oidc

import (
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
)

// authorizeRateLimit caps authorization-code mints per authenticated account.
// 60/min is generous for any legitimate SSO flow (each is a single redirect)
// while bounding a compromised session's ability to spray codes at many RPs.
const (
	authorizeRateMax    = 60
	authorizeRateWindow = time.Minute
)

// HandleAuthorize implements the OAuth 2.0 / OIDC authorization endpoint
// (RFC 6749 §4.1.1, OIDC Core §3.1.2) at GET /oauth/authorize.
//
// SECURITY — error-channel ordering: until the redirect_uri is confirmed to be
// an EXACT match against the client's registered list it is UNTRUSTED, so any
// failure up to and including that check is rendered as a DIRECT error
// (writeOIDCError) and never redirected — this is the open-redirect guard.
// Only once client + redirect_uri are validated do subsequent errors travel
// back to the RP via redirectError.
func (p *Provider) HandleAuthorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	clientID := q.Get("client_id")
	redirectURI := q.Get("redirect_uri")
	responseType := q.Get("response_type")
	scopeParam := q.Get("scope")
	state := q.Get("state")
	nonce := q.Get("nonce")
	prompt := q.Get("prompt")
	codeChallenge := q.Get("code_challenge")
	codeChallengeMethod := q.Get("code_challenge_method")

	// (2) Load + validate the client. Unknown/disabled collapses to the
	// errInvalidClient sentinel → a generic DIRECT 400 (so we neither redirect
	// to an untrusted URI nor leak unknown-vs-disabled). Any OTHER error is a
	// transient failure (e.g. the DB is down) → a DIRECT 500 server_error.
	client, err := loadClient(r.Context(), p.queries, clientID)
	if err != nil {
		if errors.Is(err, errInvalidClient) {
			writeOIDCError(w, http.StatusBadRequest, errCodeInvalidRequest, "invalid client")
		} else {
			writeOIDCError(w, http.StatusInternalServerError, errCodeServerError, "internal error")
		}
		return
	}

	// redirect_uri MUST be present and an EXACT match against the registered
	// list. Still on the DIRECT-error side of the open-redirect guard.
	if redirectURI == "" || !slices.Contains(client.RedirectUris, redirectURI) {
		writeOIDCError(w, http.StatusBadRequest, errCodeInvalidRequest, "invalid redirect_uri")
		return
	}

	// (3) redirect_uri is now trusted — every further error goes back to the RP
	// via redirectError.
	if responseType != "code" {
		redirectError(w, r, redirectURI, errCodeUnsupportedResponseType, "only response_type=code is supported", state, p.cfg.OIDC.Issuer)
		return
	}

	scopes := strings.Fields(scopeParam)
	if !slices.Contains(scopes, "openid") {
		redirectError(w, r, redirectURI, errCodeInvalidScope, "the openid scope is required", state, p.cfg.OIDC.Issuer)
		return
	}
	for _, s := range scopes {
		if !slices.Contains(client.AllowedScopes, s) {
			redirectError(w, r, redirectURI, errCodeInvalidScope, "requested scope is not allowed for this client", state, p.cfg.OIDC.Issuer)
			return
		}
	}

	// PKCE is mandatory (S256 only). Treat an empty method as invalid and
	// reject "plain" explicitly.
	if codeChallenge == "" {
		redirectError(w, r, redirectURI, errCodeInvalidRequest, "PKCE code_challenge is required", state, p.cfg.OIDC.Issuer)
		return
	}
	if codeChallengeMethod != "S256" {
		redirectError(w, r, redirectURI, errCodeInvalidRequest, "code_challenge_method must be S256", state, p.cfg.OIDC.Issuer)
		return
	}

	// (4) Session gate. A nil session, the disabled-mid-session sentinel
	// (non-nil Session with Data == nil, attached by LoadSession when an
	// account is disabled), or an explicitly-disabled account all count as
	// "not authenticated" — bounce to login (or login_required for
	// prompt=none). Widening this guard also keeps the sess.Data deref below
	// safe, matching the pattern in handle_sudo.go.
	sess := authn.SessionFromContext(r.Context())
	if sess == nil || sess.Data == nil || (sess.Account != nil && sess.Account.Disabled) {
		if prompt == "none" {
			// The RP forbade an interactive login bounce.
			redirectError(w, r, redirectURI, errCodeLoginRequired, "authentication required", state, p.cfg.OIDC.Issuer)
			return
		}
		// Send the user to the login page; on success they return to this exact
		// authorize URL. This is NOT an RP redirect, so use a plain redirect.
		fullAuthorizeURL := p.cfg.OIDC.Issuer + r.URL.RequestURI()
		loginURL := p.cfg.OIDC.Issuer + "/login?return_to=" + url.QueryEscape(fullAuthorizeURL)
		http.Redirect(w, r, loginURL, http.StatusFound)
		return
	}

	// (5) Consent is not yet implemented (v0.6). A client that requires it
	// cannot complete the flow yet.
	if client.RequireConsent {
		redirectError(w, r, redirectURI, errCodeConsentRequired, "user consent is required but not yet supported", state, p.cfg.OIDC.Issuer)
		return
	}

	// (6) Per-account rate limit. The user is authenticated, so a direct 429 is
	// appropriate (no point redirecting an over-limit caller to the RP).
	rlKey := "oidc:authorize:acct:" + strconv.Itoa(int(sess.Data.AccountID))
	if !p.rl.Allow(rlKey, authorizeRateMax, authorizeRateWindow) {
		if ra := p.rl.RetryAfter(rlKey); ra > 0 {
			w.Header().Set("Retry-After", strconv.Itoa(int(ra.Seconds())+1))
		}
		writeOIDCError(w, http.StatusTooManyRequests, errCodeServerError, "rate limit exceeded")
		return
	}

	// (7) Recover the authentication context snapshot for this session.
	row, err := p.queries.GetSession(r.Context(), sess.Data.SessionID)
	if err != nil {
		redirectError(w, r, redirectURI, errCodeServerError, "could not load session", state, p.cfg.OIDC.Issuer)
		return
	}

	// (8) Build the authorization-code state.
	ac := authCode{
		ClientID:            clientID,
		AccountID:           sess.Data.AccountID,
		SessionID:           sess.Data.SessionID,
		RedirectURI:         redirectURI,
		Scope:               scopes,
		Nonce:               nonce,
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: codeChallengeMethod,
		AuthTime:            row.AuthTime.Time,
		AMR:                 row.Amr,
		ACR:                 row.Acr.String,
	}

	// (9) Mint the single-use code into KV.
	code, err := mintCode(r.Context(), p.kv, ac)
	if err != nil {
		redirectError(w, r, redirectURI, errCodeServerError, "could not issue authorization code", state, p.cfg.OIDC.Issuer)
		return
	}

	// (11) Audit the successful authorization. Best-effort.
	accountID := sess.Data.AccountID
	_ = p.audit.Record(r.Context(), audit.Record{
		AccountID: &accountID,
		Factor:    audit.FactorOIDCClient,
		Event:     audit.EventUse,
		IP:        audit.ParseIPOrNil(r.RemoteAddr),
		UserAgent: r.UserAgent(),
		Detail: map[string]any{
			"reason":    "authorize",
			"client_id": clientID,
			"scope":     scopes,
		},
	})

	// (10) Success redirect back to the RP with code + state + iss (RFC 9207).
	u, err := url.Parse(redirectURI)
	if err != nil {
		// Defensive guard: redirectURI was already an exact registered match
		// (and parsed once in redirectError on the error paths), so this is
		// practically unreachable. If it ever did fail, the URI cannot be
		// parsed to redirect to, so redirectError falls through to a direct
		// server_error JSON response rather than redirecting.
		redirectError(w, r, redirectURI, errCodeServerError, "invalid redirect_uri", state, p.cfg.OIDC.Issuer)
		return
	}
	rq := u.Query()
	rq.Set("code", code)
	if state != "" {
		rq.Set("state", state)
	}
	rq.Set("iss", p.cfg.OIDC.Issuer)
	u.RawQuery = rq.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}
