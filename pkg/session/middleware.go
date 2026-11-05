package session

import (
	"net/http"
	"strconv"
	"strings"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/credential/pat"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/weberr"
)

// Cookie names — referenced by handlers + tests.
const (
	SessionCookieName  = "prohibitorum_session"
	CeremonyCookieName = "prohibitorum_ceremony"
	// ConditionalCeremonyCookieName carries the login ceremony started for
	// username-field autofill (mediation=conditional), so that ceremony and a
	// button-started one can be pending at the same time.
	ConditionalCeremonyCookieName = "prohibitorum_ceremony_conditional"
	// FedStateCookieName carries the upstream-federation anti-forgery token
	// (audit follow-up N4). See FedStateCookie for the SameSite rationale.
	FedStateCookieName = "prohibitorum_fed_state"
)

// secureCookies reports whether session cookies should be hardened for a
// secure (HTTPS) deployment. Derived from the canonical public origin's scheme
// so the cookie identity is deployment-stable (not per-request) — required
// because the __Host- name must match between the set and read paths, and a
// TLS-terminating proxy must not flip it per request.
func secureCookies(cfg *configx.Config) bool {
	return len(cfg.PublicOrigins) > 0 &&
		strings.HasPrefix(strings.ToLower(strings.TrimSpace(cfg.PublicOrigins[0])), "https://")
}

// sessionCookieName returns the deployment-appropriate session cookie name:
// the browser-hardened __Host- prefix in secure deployments, the plain base
// name otherwise. The __Host- prefix REQUIRES Secure + Path=/ + no Domain
// (all satisfied below) and gives browser-enforced session-fixation /
// subdomain-injection defense.
func sessionCookieName(secure bool) string {
	if secure {
		return "__Host-" + SessionCookieName
	}
	return SessionCookieName
}

// SessionCookieNameFor resolves the session cookie name for cfg. Exported so
// out-of-package readers (the logout handler, the OpenAPI security scheme) name
// the cookie identically to this package's set/clear/read paths.
func SessionCookieNameFor(cfg *configx.Config) string {
	return sessionCookieName(secureCookies(cfg))
}

// LoadSession returns a chi middleware that reads the browser's signed-in
// accounts from the two session cookies, selects the session this request
// acts as, and attaches it with authn.WithSession. The Browser itself is
// attached too (BrowserFromContext) for handlers that list, add or remove
// accounts.
//
// Selection: under /api/prohibitorum/ an X-Prohibitorum-Account header names
// the account by ID; a malformed value is 400 bad_request, and an account not
// signed in here leaves the request without a session, marked so
// authn.CheckContext answers account_not_signed_in. Everywhere else, and
// without the header, the default account is used: the first one signed in.
// The URL's `u` is never read here — the dashboard resolves it to an ID.
//
// Does NOT reject missing/invalid sessions — that's per-route via
// registerOp's Check. A disabled account's entry is revoked and dropped; when
// it is the selected account a sentinel session (no token, no data) is
// attached so Check answers account_disabled.
func LoadSession(cfg *configx.Config, q db.Querier, store *SessionStore, ipOf func(*http.Request) string) func(http.Handler) http.Handler {
	return NewManager(cfg, q, store, ipOf).LoadSession()
}

// LoadSession is the middleware form of the manager; see the package-level
// LoadSession.
func (m *Manager) LoadSession() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// A management API request carrying a PAT is authenticated by the
			// token alone (see patAuthMW); the cookie is not consulted.
			if pat.SelectsManagementAuth(r) {
				next.ServeHTTP(w, r)
				return
			}
			b := m.newBrowser(r)
			ctx := WithBrowser(r.Context(), b)
			b.ctx = ctx

			var sess *authn.Session
			raw := r.Header.Get(AccountHeader)
			if raw != "" && strings.HasPrefix(r.URL.Path, "/api/prohibitorum/") {
				id, ok := parseAccountID(raw)
				if !ok {
					weberr.WriteJSON(w, "bad_request", nil, weberr.RequestIDFromContext(r.Context()))
					return
				}
				if sess = b.Select(id); sess == nil {
					ctx = authn.WithAccountNotSignedIn(ctx)
				}
			} else if len(b.entries) > 0 {
				sess = b.Default()
			}
			if sess != nil {
				if sess.Data != nil {
					w.Header().Set(AccountHeader, strconv.FormatInt(int64(sess.Account.ID), 10))
					// A request that names its account, or one from a browser
					// holding a single account, is that account's activity. A
					// header-less request on a multi-account browser (a page or
					// asset load, a protocol endpoint before it settles on an
					// account) is no particular account's, so it slides none.
					if raw != "" || len(b.entries) == 1 {
						b.Touch(sess.Account.ID)
					}
				}
				ctx = authn.WithSession(ctx, sess)
			}
			r = r.WithContext(ctx)
			b.r = r
			bw := &browserWriter{ResponseWriter: w, b: b}
			next.ServeHTTP(bw, r)
			if !bw.wrote {
				// Nothing written yet: the server will send an implicit 200,
				// so the cookies still fit in the header.
				b.Flush(w.Header())
			}
		})
	}
}

// browserWriter rewrites the session cookies, when an entry changed during
// the request, just before the response header goes out.
type browserWriter struct {
	http.ResponseWriter
	b     *Browser
	wrote bool
}

func (w *browserWriter) WriteHeader(code int) {
	if !w.wrote {
		w.wrote = true
		w.b.Flush(w.Header())
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *browserWriter) Write(p []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(p)
}

func (w *browserWriter) Flush() {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (w *browserWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// ----- cookie helpers -------------------------------------------------------

// ClearedSessionCookies expires both session cookies (sign out of every
// account). Name + Path + attributes match the set path or browsers would
// create a new empty cookie rather than clearing the existing one.
func ClearedSessionCookies(cfg *configx.Config) []*http.Cookie {
	return []*http.Cookie{
		browserCookie(cfg, true, "", 0),
		browserCookie(cfg, false, "", 0),
	}
}

// CeremonyCookie carries the random key that links /auth/login/begin and
// /auth/login/complete. name is CeremonyCookieName for a button-started (modal)
// ceremony and ConditionalCeremonyCookieName for an autofill (conditional) one;
// each mediation keeps its own cookie so neither begin overwrites the other's
// pending ceremony. Path is scoped to /api/prohibitorum/auth so the cookie
// isn't sent on unrelated API requests. SameSite=Strict because the
// ceremony is always same-origin and we want maximum tightness during the
// ~5 minute window. Max-Age 300 mirrors the KV TTL.
func CeremonyCookie(cfg *configx.Config, _ *http.Request, name, value string) *http.Cookie {
	return &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/api/prohibitorum/auth",
		HttpOnly: true,
		// Secure derives from the deployment-stable public-origin scheme
		// (secureCookies), matching FreshSessionCookie/FedStateCookie — NOT a
		// per-request isSecure(r) probe, which returns false behind a
		// TLS-terminating proxy and would ship this cookie without Secure on an
		// https deployment (audit SESS-2).
		Secure:   secureCookies(cfg),
		SameSite: http.SameSiteStrictMode,
		MaxAge:   300,
	}
}

// ClearedCeremonyCookie expires the named ceremony cookie. Path matches the
// issue path.
func ClearedCeremonyCookie(cfg *configx.Config, r *http.Request, name string) *http.Cookie {
	c := CeremonyCookie(cfg, r, name, "")
	c.MaxAge = -1
	return c
}

// FedStateCookie carries the anti-forgery token that binds an upstream-OIDC
// federation login (and invite) flow to the initiating browser (audit
// follow-up N4). It is set at /federation/{slug}/login (and
// /enrollments/{token}/start-federation) and required to match at the
// /callback.
//
// SameSite=Lax — NOT Strict — is deliberate and load-bearing: the OIDC callback
// is a cross-site, top-level GET navigation initiated by the upstream IdP, and
// SameSite=Strict suppresses cookies on cross-site-initiated navigations, which
// would break the legitimate callback. Lax IS sent on top-level GET navigations
// (exactly like the session cookie, which is Lax for the same reason). The
// local WebAuthn CeremonyCookie can afford Strict only because that ceremony is
// fully same-origin. Path is scoped to /api/prohibitorum so the cookie reaches
// the federation, invite, and link callbacks but not unrelated origins. Secure
// is deployment-derived (secureCookies) so it is stable across the set/read
// requests. MaxAge tracks the federation state TTL.
func FedStateCookie(cfg *configx.Config, _ *http.Request, value string) *http.Cookie {
	maxAge := int(cfg.Federation.StateTTL.Seconds())
	if maxAge <= 0 {
		maxAge = 600
	}
	return &http.Cookie{
		Name:     FedStateCookieName,
		Value:    value,
		Path:     "/api/prohibitorum",
		HttpOnly: true,
		Secure:   secureCookies(cfg),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAge,
	}
}

// ClearedFedStateCookie expires the federation anti-forgery cookie. Attributes
// MUST match FedStateCookie or the browser creates a new empty cookie instead.
func ClearedFedStateCookie(cfg *configx.Config, r *http.Request) *http.Cookie {
	c := FedStateCookie(cfg, r, "")
	c.MaxAge = -1
	return c
}
