package server

// maintenance.go — maintenance-mode enforcement.
//
// When maintenance mode is on, every non-admin principal is denied across all
// surfaces; admins are unaffected so they can finish the work and lift the mode.
// Two server-side mechanisms cooperate, plus a forward-auth gateway check:
//
//   - maintenanceGateMW blocks authenticated non-admin requests to the gated
//     surfaces (dashboard API, OIDC authorize, SAML SSO). The session cookie is
//     left intact, so users are simply restored when maintenance ends. With
//     several accounts signed in on one browser, an API request that names
//     its account (X-Prohibitorum-Account) is judged as that account; a
//     request that names none — a page navigation — passes when any account
//     on the browser is an admin, since it cannot say which one it acts as.
//     Whatever then picks an account for such a navigation checks that
//     account itself with maintenanceLockout: the link callback here, and the
//     OIDC and SAML endpoints once they have resolved the account (through
//     the Provider's maintenance checker and the account's role).
//   - maintenanceLockout blocks non-admins at every session-issuance point, so a
//     non-admin cannot even sign in (the gate alone can't enforce that, since
//     GET /me stays open to render the maintenance screen).
//   - the OIDC Provider carries its own check for the requests that name an
//     account without the main session, so the gate sees no principal on
//     them: the forward-auth gateway (a PAT or a per-domain cookie) and the
//     token, userinfo and introspection endpoints (client credentials or a
//     bearer token) — see SetMaintenanceChecker, wired in server.go. Tokens
//     already issued stay valid wherever they are verified locally until they
//     expire.

import (
	"context"
	"net/http"
	"strings"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/branding"
	sessstore "prohibitorum/pkg/session"
)

const roleAdmin = "admin"

// maintenanceGateMW denies authenticated non-admin requests while maintenance
// mode is on. Unauthenticated requests, admins, and the small allowlist needed
// to render the maintenance screen + sign out pass through.
func maintenanceGateMW(b *branding.Resolver) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sess := authn.PrincipalFromContext(r.Context())
			if sess == nil || sess.Account == nil || sess.Account.Role == roleAdmin {
				next.ServeHTTP(w, r)
				return
			}
			if on, _ := b.Maintenance(r.Context()); !on {
				next.ServeHTTP(w, r)
				return
			}
			if maintenanceAllowedPath(r) {
				next.ServeHTTP(w, r)
				return
			}
			if sess.PAT == nil && !namesAccount(r) && browserHasAdmin(r) {
				next.ServeHTTP(w, r)
				return
			}
			// Browser-navigation SSO surfaces bounce to the SPA (which renders the
			// maintenance screen); XHR/API callers get a JSON 503 they can map.
			if maintenanceBrowserNav(r.URL.Path) {
				http.Redirect(w, r, "/", http.StatusFound)
				return
			}
			writeAuthErr(w, authn.ErrMaintenanceMode())
		})
	}
}

// namesAccount reports whether the request selects its account by header,
// which only API requests can do.
func namesAccount(r *http.Request) bool {
	return r.Header.Get(sessstore.AccountHeader) != "" && strings.HasPrefix(r.URL.Path, "/api/prohibitorum/")
}

// browserHasAdmin reports whether any account signed in on the request's
// browser is an admin.
func browserHasAdmin(r *http.Request) bool {
	b := sessstore.BrowserFromContext(r.Context())
	if b == nil {
		return false
	}
	for _, sess := range b.Accounts() {
		if sess.Account != nil && sess.Account.Role == roleAdmin {
			return true
		}
	}
	return false
}

// maintenanceAllowedPath reports the requests a blocked non-admin may still make
// during maintenance: the SPA shell + assets, the read-only endpoints the
// maintenance screen needs (public config, own identity, instance icon, own
// avatar), and sign-out.
func maintenanceAllowedPath(r *http.Request) bool {
	p := r.URL.Path
	switch {
	case p == "/api/prohibitorum/config":
		return true
	case p == "/api/prohibitorum/me" && r.Method == http.MethodGet:
		return true
	case p == "/api/prohibitorum/auth/logout", p == "/api/prohibitorum/auth/logout-all",
		p == "/api/prohibitorum/auth/accounts":
		return true
	case p == "/api/prohibitorum/account-chooser", p == accountChooserContinuePath:
		// The account picked here may be an admin's; the protocol endpoint
		// it resumes checks maintenance for that account.
		return true
	case strings.HasPrefix(p, "/branding/"), strings.HasPrefix(p, "/avatar/"):
		return true
	}
	// Anything outside the API/protocol surfaces is the static SPA shell.
	return !maintenanceGatedPath(p)
}

func maintenanceGatedPath(p string) bool {
	return strings.HasPrefix(p, "/api/prohibitorum/") ||
		strings.HasPrefix(p, "/oauth/") ||
		strings.HasPrefix(p, "/saml/")
}

// maintenanceBrowserNav reports the top-level browser-navigation surfaces a
// denied non-admin is redirected away from (rather than shown a raw JSON 503):
// OIDC authorize and SAML SSO/SLO.
func maintenanceBrowserNav(p string) bool {
	return p == "/oauth/authorize" ||
		strings.HasPrefix(p, "/saml/sso") ||
		p == "/saml/slo"
}

// maintenanceLockout returns ErrMaintenanceMode when maintenance mode is on and
// the account being signed in is not an admin. Called at every session-issuance
// point so non-admins cannot establish a session during maintenance, and by
// navigations that act as an account other than the one the gate judged. The DB
// role lookup runs only while maintenance is on (a cached bool check otherwise),
// so normal logins are unaffected.
func (s *Server) maintenanceLockout(ctx context.Context, accountID int32) *authn.AuthError {
	if s.branding == nil {
		return nil // no branding resolver wired (minimal test servers) → never in maintenance
	}
	if on, _ := s.branding.Maintenance(ctx); !on {
		return nil
	}
	if acct, err := s.queries.GetAccountByID(ctx, accountID); err == nil && acct.Role == roleAdmin {
		return nil
	}
	return authn.ErrMaintenanceMode()
}
