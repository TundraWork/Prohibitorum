package oidc

import (
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/db"
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
			p.redirectToErrorPage(w, r, errCodeInvalidRequest)
		} else {
			// Transient (e.g. the DB is down): re-running the request is the
			// only remedy, so carry the authorize URI (server-built, relative —
			// same form validateFederationReturnTo produces) as return_to and
			// the /error page offers to load it again.
			p.redirectToErrorPageWithReturn(w, r, errCodeServerError, r.URL.RequestURI())
		}
		return
	}

	// redirect_uri MUST be present and an EXACT match against the registered
	// list. Still on the DIRECT-error side of the open-redirect guard.
	if redirectURI == "" || !slices.Contains(client.RedirectUris, redirectURI) {
		p.redirectToErrorPage(w, r, errCodeInvalidRequest)
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
		// Must be both allowed for this client AND in the OP's closed
		// vocabulary. The supported-set check is defense in depth: config-time
		// validation keeps allowed_scopes inside SupportedScopes, but this also
		// rejects any scope a legacy/CLI-written client may still carry.
		if !slices.Contains(client.AllowedScopes, s) || !IsSupportedScope(s) {
			redirectError(w, r, redirectURI, errCodeInvalidScope, "requested scope is not allowed for this client", state, p.cfg.OIDC.Issuer)
			return
		}
	}

	// PKCE per client policy (D6). A confidential client opts out only when
	// its require_pkce column says so; a public client (client_auth_method =
	// 'none') must always send a code_challenge — that rule is protocol-level
	// (OAuth 2.1 / RFC 9700), not operator configuration, so it is enforced
	// here regardless of what the column holds or what a write path allowed.
	// The requested method must be in allowed_code_challenge_methods; 'plain'
	// is forbidden entirely by the oidc_client DB CHECK (OAuth 2.1: S256
	// mandatory); the allowed set is S256-only. The membership check below is
	// the general gate.
	if (client.RequirePkce || client.ClientAuthMethod == "none") && codeChallenge == "" {
		redirectError(w, r, redirectURI, errCodeInvalidRequest, "PKCE code_challenge is required", state, p.cfg.OIDC.Issuer)
		return
	}
	if codeChallenge != "" {
		method := codeChallengeMethod
		if method == "" {
			method = "plain" // RFC 7636 default when method omitted — rejected unless explicitly allowed
		}
		if !slices.Contains(client.AllowedCodeChallengeMethods, method) {
			redirectError(w, r, redirectURI, errCodeInvalidRequest, "code_challenge_method not allowed for this client", state, p.cfg.OIDC.Issuer)
			return
		}
	}

	// OIDC Core §3.1.2.1: validate prompt tokens against the supported set
	// (advertised as prompt_values_supported in discovery) rather than silently
	// ignoring unknown values, and enforce that "none" is mutually exclusive with
	// every other value. Checked before the account is resolved, which reads
	// select_account and none. (T2.3)
	prompts := strings.Fields(prompt)
	for _, pr := range prompts {
		switch pr {
		case "none", "login", "consent", "select_account":
		default:
			redirectError(w, r, redirectURI, errCodeInvalidRequest, "unsupported prompt value", state, p.cfg.OIDC.Issuer)
			return
		}
	}
	wantLogin := slices.Contains(prompts, "login")
	wantNone := slices.Contains(prompts, "none")
	if wantNone && len(prompts) > 1 {
		redirectError(w, r, redirectURI, errCodeInvalidRequest, "prompt none must not be combined with other values", state, p.cfg.OIDC.Issuer)
		return
	}

	// (4) Fix the account (design 5.3). Every step after this acts on the
	// chosen account's session: access policy, re-auth, consent and the code,
	// whose SessionID becomes the ID token's sid. An id_token_hint that fails
	// verification is not an error here; it just names no account.
	var hintClaims map[string]any
	if hint := q.Get("id_token_hint"); hint != "" {
		hintClaims, _ = p.verifyIDTokenHint(r.Context(), hint)
	}
	resolved := p.resolveAccount(r, accountRequest{
		kind:          authn.ChooserKindOIDC,
		app:           client.ClientID,
		choice:        q.Get(authn.AccountChoiceParam),
		hint:          hintClaims,
		loginHint:     q.Get("login_hint"),
		selectAccount: slices.Contains(prompts, "select_account"),
	})
	switch resolved.outcome {
	case accountNeedsLogin:
		if wantNone {
			redirectError(w, r, redirectURI, errCodeLoginRequired, "authentication required", state, p.cfg.OIDC.Issuer)
			return
		}
		// Not an RP redirect: the login page returns to this exact URL.
		http.Redirect(w, r, p.loginURL(p.cfg.OIDC.Issuer+r.URL.RequestURI(), resolved.prefill), http.StatusFound)
		return
	case accountNeedsChoice:
		if wantNone {
			redirectError(w, r, redirectURI, errCodeAccountSelectionRequired, "several accounts are signed in", state, p.cfg.OIDC.Issuer)
			return
		}
		if err := p.redirectToChooser(w, r, authn.ChooserKindOIDC, client.ClientID, resolved.prefill); err != nil {
			redirectError(w, r, redirectURI, errCodeServerError, "could not start account selection", state, p.cfg.OIDC.Issuer)
		}
		return
	}
	sess := resolved.sess

	// The maintenance gate in front of this handler only sees the default
	// account; the account chosen here may be another one.
	if sess.Account != nil && p.maintenanceDenies(r.Context(), *sess.Account) {
		if wantNone {
			redirectError(w, r, redirectURI, errCodeTemporarilyUnavailable, "service under maintenance", state, p.cfg.OIDC.Issuer)
			return
		}
		http.Redirect(w, r, p.withAccountRef(r, p.cfg.OIDC.Issuer+"/", sess.Data.AccountID), http.StatusFound)
		return
	}

	// (4b) Per-app access gate (RBAC). The user is authenticated and enabled; a
	// restricted client requires a direct or via-group grant. There is NO admin
	// bypass — an admin who is not granted access is denied like any other user.
	// Placed immediately after the session/disabled gate and BEFORE any
	// prompt/re-auth/consent handling so an unauthorized user never reaches those
	// flows. Fail CLOSED: a predicate error denies (surfaced as server_error).
	decision, accessErr := p.evaluateOIDCAccess(r.Context(), sess.Data.AccountID, clientID)
	if accessErr != nil || !decision.Allowed {
		if accessErr != nil {
			// Fail closed, but surface as server_error (not access_denied): we
			// could not evaluate policy, so we make no authorization claim.
			redirectError(w, r, redirectURI, errCodeServerError, "could not evaluate access", state, p.cfg.OIDC.Issuer)
			return
		}
		acctID := sess.Data.AccountID
		audit.RecordOrLog(r.Context(), p.audit, audit.Record{
			AccountID: &acctID,
			Factor:    audit.FactorOIDCClient,
			Event:     audit.EventAccessDenied,
			IP:        audit.ParseIPOrNil(p.auditIP(r)),
			UserAgent: r.UserAgent(),
			Detail: map[string]any{
				"reason":    appAccessAuditReason(decision.Source),
				"client_id": clientID,
			},
		})
		p.appAccessDenied(w, r, redirectURI, client.DisplayName, state, r.URL.RequestURI(), wantNone, acctID)
		return
	}

	// (5a) Forced re-authentication (prompt=login / max_age). OIDC Core
	// §3.1.2.1. A pre-existing session may NOT satisfy a re-auth demand — the
	// user must authenticate again, producing an auth_time that post-dates the
	// demand (tracked via a single-use KV marker carried in &reauth=<nonce>).
	//
	// Snapshot the session NOW — moved ahead of the consent check and the
	// rate limit (steps 5/6) so auth_time is available for the re-auth demand
	// evaluation below. A deliberate ordering change.
	row, err := p.queries.GetSession(r.Context(), sess.Data.SessionID)
	if err != nil {
		redirectError(w, r, redirectURI, errCodeServerError, "could not load session", state, p.cfg.OIDC.Issuer)
		return
	}

	demand := wantLogin
	if maxAgeStr := q.Get("max_age"); maxAgeStr != "" {
		maxAge, perr := strconv.Atoi(maxAgeStr)
		if perr != nil || maxAge < 0 {
			redirectError(w, r, redirectURI, errCodeInvalidRequest, "invalid max_age", state, p.cfg.OIDC.Issuer)
			return
		}
		if time.Since(row.AuthTime.Time) > time.Duration(maxAge)*time.Second {
			demand = true
		}
	}
	if demand {
		reauthNonce := q.Get("reauth")
		satisfied := false
		if reauthNonce != "" {
			ok, cerr := authn.ConsumeReauth(r.Context(), p.kv, "oidc:reauth:", reauthNonce, sess.Data.AccountID, row.AuthTime.Time)
			if cerr != nil {
				redirectError(w, r, redirectURI, errCodeServerError, "reauth check failed", state, p.cfg.OIDC.Issuer)
				return
			}
			satisfied = ok
		}
		if !satisfied {
			if wantNone {
				redirectError(w, r, redirectURI, errCodeLoginRequired, "re-authentication required", state, p.cfg.OIDC.Issuer)
				return
			}
			renonce, derr := authn.DemandReauth(r.Context(), p.kv, "oidc:reauth:", sess.Data.AccountID)
			if derr != nil {
				redirectError(w, r, redirectURI, errCodeServerError, "could not start re-auth", state, p.cfg.OIDC.Issuer)
				return
			}
			// Rebuild from Path+Query (not RequestURI) so rq.Set replaces any
			// existing reauth nonce — a re-bounce must not preserve a stale one.
			ret := p.cfg.OIDC.Issuer + r.URL.Path
			rq := r.URL.Query()
			rq.Set("reauth", renonce)
			ret += "?" + rq.Encode()
			// The login page pre-fills the account being re-authenticated;
			// the URL keeps account_choice, so the same account is used
			// when the reader comes back.
			username := ""
			if sess.Account != nil {
				username = sess.Account.Username
			}
			http.Redirect(w, r, p.loginURL(ret, username), http.StatusFound)
			return
		}
	}

	// (5) Consent. Trusted clients (RequireConsent=false) skip entirely. Otherwise
	// a stored grant covering every requested scope satisfies consent — unless the
	// RP forced re-consent with prompt=consent. When consent is needed we mint a
	// single-use ticket and bounce to /consent (or, for prompt=none, error to RP).
	if client.RequireConsent {
		granted, gerr := p.queries.GetConsent(r.Context(), db.GetConsentParams{
			AccountID: sess.Data.AccountID,
			ClientID:  client.ClientID,
		})
		if gerr != nil && !errors.Is(gerr, pgx.ErrNoRows) {
			redirectError(w, r, redirectURI, errCodeServerError, "could not load consent", state, p.cfg.OIDC.Issuer)
			return
		}
		needConsent := slices.Contains(prompts, "consent")
		for _, s := range scopes {
			if !slices.Contains(granted, s) {
				needConsent = true
				break
			}
		}
		if needConsent {
			if wantNone {
				redirectError(w, r, redirectURI, errCodeConsentRequired, "user consent is required", state, p.cfg.OIDC.Issuer)
				return
			}
			nonce, derr := authn.DemandConsent(r.Context(), p.kv, authn.ConsentTicket{
				AccountID:   sess.Data.AccountID,
				ClientID:    client.ClientID,
				Scopes:      scopes,
				RedirectURI: redirectURI,
				State:       state,
			})
			if derr != nil {
				redirectError(w, r, redirectURI, errCodeServerError, "could not start consent", state, p.cfg.OIDC.Issuer)
				return
			}
			// Build return_to from Path+Query with any stale reauth nonce
			// stripped: a re-auth demand (prompt=login/max_age) is satisfied via
			// a single-use nonce that was already consumed above, so echoing it
			// here would be dead state. (Mirrors the re-auth block's own nonce
			// hygiene.) ACCEPTED LIMITATION: when prompt=login co-occurs with a
			// first-time (ungranted) consent, the user still sees one extra login
			// after approving — the single-use re-auth nonce cannot span the extra
			// consent round-trip. This is fail-safe (over-authentication) and rare;
			// not re-architected here.
			retQuery := r.URL.Query()
			retQuery.Del("reauth")
			returnTo := p.cfg.OIDC.Issuer + r.URL.Path
			if enc := retQuery.Encode(); enc != "" {
				returnTo += "?" + enc
			}
			consentURL := p.cfg.OIDC.Issuer + "/consent?ticket=" + url.QueryEscape(nonce) +
				"&return_to=" + url.QueryEscape(returnTo)
			http.Redirect(w, r, p.withAccountRef(r, consentURL, sess.Data.AccountID), http.StatusFound)
			return
		}
	}

	// (6) Per-account rate limit. The user is authenticated, so a direct 429 is
	// appropriate (no point redirecting an over-limit caller to the RP).
	rlKey := "oidc:authorize:acct:" + strconv.Itoa(int(sess.Data.AccountID))
	if !p.rl.Allow(rlKey, authorizeRateMax, authorizeRateWindow) {
		if ra := p.rl.RetryAfter(rlKey); ra > 0 {
			w.Header().Set("Retry-After", strconv.Itoa(int(ra.Seconds())+1))
		}
		writeOIDCError(w, r, http.StatusTooManyRequests, errCodeServerError, "rate limit exceeded")
		return
	}

	// (8) Build the authorization-code state. row (the GetSession snapshot) was
	// fetched in step (5a) and carries the authentication context.
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
	code, err := mintCode(r.Context(), p.kv, ac, p.authCodeTTL())
	if err != nil {
		redirectError(w, r, redirectURI, errCodeServerError, "could not issue authorization code", state, p.cfg.OIDC.Issuer)
		return
	}

	// (11) Audit the successful authorization. Best-effort.
	accountID := sess.Data.AccountID
	audit.RecordOrLog(r.Context(), p.audit, audit.Record{
		AccountID: &accountID,
		Factor:    audit.FactorOIDCClient,
		Event:     audit.EventUse,
		IP:        audit.ParseIPOrNil(p.auditIP(r)),
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
