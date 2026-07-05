package session

import (
	"net/http"
	"strings"
	"time"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/db"
)

// Cookie names — referenced by handlers + tests.
const (
	SessionCookieName  = "prohibitorum_session"
	CeremonyCookieName = "prohibitorum_ceremony"
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

// LoadSession returns a chi middleware that:
//  1. Reads the prohibitorum_session cookie.
//  2. Validates it against the SessionStore.
//  3. Fetches the live db.Account.
//  4. Attaches *authn.Session to the request context.
//
// Does NOT reject missing/invalid sessions — that's per-route via registerOp's
// Check. DOES reject disabled accounts with 403 account_disabled (and revokes
// the session) because a disabled account never has authority regardless of
// what route is being hit. Failures in cookie parsing or session lookup are
// silently swallowed — the request continues unauthenticated, and downstream
// auth checks will produce the appropriate 401.
func LoadSession(cfg *configx.Config, q db.Querier, store *SessionStore, ipOf func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, err := r.Cookie(SessionCookieNameFor(cfg))
			if err != nil || c.Value == "" {
				next.ServeHTTP(w, r)
				return
			}
			accountID, token, ok := ParseCookieValue(c.Value)
			if !ok {
				http.SetCookie(w, ClearedSessionCookie(cfg, r))
				next.ServeHTTP(w, r)
				return
			}
			ip := ipOf(r)
			data, refreshed, err := store.Load(r.Context(), accountID, token, ip, r.UserAgent())
			if err != nil {
				// Invalid/expired session: clear the cookie, continue unauthenticated.
				http.SetCookie(w, ClearedSessionCookie(cfg, r))
				next.ServeHTTP(w, r)
				return
			}
			account, err := q.GetAccountByID(r.Context(), accountID)
			if err != nil {
				// Account vanished (e.g. deleted while session was live).
				http.SetCookie(w, ClearedSessionCookie(cfg, r))
				_ = store.Revoke(r.Context(), accountID, token)
				next.ServeHTTP(w, r)
				return
			}
			if account.Disabled {
				// Disabled mid-session — kill the persistent session in KV and clear the
				// cookie so the user is forcibly logged out. We still attach a "disabled
				// sentinel" session to the context so per-route Check can return
				// account_disabled (machine-readable JSON) instead of no_session.
				//
				// Public routes (auth/logout, enrollment consume, etc.) read the sentinel
				// via SessionFromContext but ignore it — their AuthRequirement is AuthPublic,
				// which Check returns nil for before inspecting the session.
				_ = store.Revoke(r.Context(), accountID, token)
				http.SetCookie(w, ClearedSessionCookie(cfg, r))
				ctx := authn.WithSession(r.Context(), &authn.Session{Account: &account, Token: "", Data: nil})
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			if refreshed {
				http.SetCookie(w, FreshSessionCookie(cfg, r, accountID, token, store.TTL()))
			}
			ctx := authn.WithSession(r.Context(), &authn.Session{Account: &account, Token: token, Data: data})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ----- cookie helpers -------------------------------------------------------

// FreshSessionCookie constructs the Set-Cookie value for issuing or refreshing
// a session. Path=/ so a real browser sends it to the root-level OIDC/SAML
// protocol endpoints (/oauth/authorize, /saml/sso, …) — it is an opaque
// HttpOnly token, so being sent on all paths is exactly what mainstream IdPs
// do. Name + Secure derive from the deployment scheme (see secureCookies).
// The *http.Request is no longer needed (Secure comes from cfg) but is kept
// for signature stability with the call sites.
func FreshSessionCookie(cfg *configx.Config, _ *http.Request, accountID int32, token string, ttl time.Duration) *http.Cookie {
	return &http.Cookie{
		Name:     SessionCookieNameFor(cfg),
		Value:    CookieValue(accountID, token),
		Path:     "/",
		HttpOnly: true,
		Secure:   secureCookies(cfg),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(ttl.Seconds()),
	}
}

// ClearedSessionCookie expires the session cookie. Name + Path + attributes
// MUST match FreshSessionCookie or browsers create a new empty cookie rather
// than clearing the existing one.
func ClearedSessionCookie(cfg *configx.Config, _ *http.Request) *http.Cookie {
	return &http.Cookie{
		Name:     SessionCookieNameFor(cfg),
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   secureCookies(cfg),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	}
}

// CeremonyCookie carries the random key that links /auth/login/begin and
// /auth/login/complete. Path is scoped to /api/prohibitorum/auth so the cookie
// isn't sent on unrelated API requests. SameSite=Strict because the
// ceremony is always same-origin and we want maximum tightness during the
// ~5 minute window. Max-Age 300 mirrors the KV TTL.
func CeremonyCookie(cfg *configx.Config, _ *http.Request, value string) *http.Cookie {
	return &http.Cookie{
		Name:     CeremonyCookieName,
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

// ClearedCeremonyCookie expires the ceremony cookie. Path matches the issue path.
func ClearedCeremonyCookie(cfg *configx.Config, r *http.Request) *http.Cookie {
	c := CeremonyCookie(cfg, r, "")
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

