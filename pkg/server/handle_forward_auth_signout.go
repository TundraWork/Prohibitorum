// Package server — handle_forward_auth_signout.go
//
// The IdP-domain side of forward-auth sign-out. The protected-domain
// /.prohibitorum-forward-auth/sign_out handler (oidc.Provider) clears the
// per-domain cookie + KV session, then 302s here. This handler terminates the
// Prohibitorum SSO session (mirroring handleLogoutHTTP) and redirects back to a
// validated forward-auth host (fail-closed open-redirect guard).
package server

import (
	"net/http"

	"github.com/sirupsen/logrus"

	"prohibitorum/pkg/logx"
	oidc "prohibitorum/pkg/protocol/oidc"
	sessstore "prohibitorum/pkg/session"
)

func (s *Server) handleForwardAuthSSOLogoutHTTP(w http.ResponseWriter, r *http.Request) {
	// Terminate the SSO session (same path as POST /auth/logout).
	if c, err := r.Cookie(sessstore.SessionCookieNameFor(s.config)); err == nil && c.Value != "" {
		if id, tok, ok := sessstore.ParseCookieValue(c.Value); ok {
			_ = s.sessionStore.Revoke(r.Context(), id, tok)
			logx.WithContext(r.Context()).WithFields(logrus.Fields{
				"event":      "auth.logout",
				"account_id": id,
				"client_ip":  sessstore.ClientIP(r, s.config.TrustProxy),
				"via":        "forward_auth",
			}).Info("auth")
		}
	}
	http.SetCookie(w, sessstore.ClearedSessionCookie(s.config, r))

	// Open-redirect guard: only bounce back to a registered forward-auth host.
	if dest, ok := oidc.ValidatedForwardAuthReturnURL(r.Context(), s.queries, r.URL.Query().Get("rd")); ok {
		http.Redirect(w, r, dest, http.StatusFound)
		return
	}
	http.Redirect(w, r, "/", http.StatusFound)
}
