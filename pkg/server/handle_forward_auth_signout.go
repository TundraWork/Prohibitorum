// Package server — handle_forward_auth_signout.go
//
// The IdP-domain side of forward-auth sign-out. The protected-domain
// /.prohibitorum-forward-auth/sign_out handler (oidc.Provider) clears the
// per-domain cookie + KV session, then 302s here, with a single-use sign-out
// pass when the app's own page asked. Anyone can link here, so this handler
// terminates the Prohibitorum SSO session (mirroring handleLogoutHTTP) only for
// a pass naming the session's account; any other request with a session goes
// to the confirmation page (/sign-out), whose own sign-out comes back here
// without a session. Redirects go back only to a validated forward-auth host
// (fail-closed open-redirect guard).
package server

import (
	"context"
	"net/http"
	"net/url"

	"github.com/sirupsen/logrus"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/logx"
	oidc "prohibitorum/pkg/protocol/oidc"
	sessstore "prohibitorum/pkg/session"
)

// forwardAuthSignOutPasses redeems the single-use pass the protected-domain
// sign_out mints.
type forwardAuthSignOutPasses interface {
	ConsumeForwardAuthSignOut(ctx context.Context, token string) (accountID int32, ok bool)
}

func (s *Server) forwardAuthSignOutDeps() (db.Querier, forwardAuthSignOutPasses) {
	var q db.Querier = s.queries
	if s.forwardAuthSignOutOverride != nil {
		q = s.forwardAuthSignOutOverride
	}
	var passes forwardAuthSignOutPasses = s.oidcOP
	if s.forwardAuthSignOutPassesOverride != nil {
		passes = s.forwardAuthSignOutPassesOverride
	}
	return q, passes
}

func (s *Server) handleForwardAuthSSOLogoutHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q, passes := s.forwardAuthSignOutDeps()
	// Open-redirect guard: only bounce back to a registered forward-auth host.
	dest, _ := oidc.ValidatedForwardAuthReturnURL(ctx, q, r.URL.Query().Get("rd"))
	back := dest
	if back == "" {
		back = "/"
	}
	// The pass is used up whichever way the request goes.
	var passAccount int32
	passOK := false
	if token := r.URL.Query().Get("token"); token != "" {
		passAccount, passOK = passes.ConsumeForwardAuthSignOut(ctx, token)
	}

	sess := authn.SessionFromContext(ctx)
	if sess == nil || sess.Data == nil {
		// No SSO session to end (a disabled account's was ended on load).
		http.Redirect(w, r, back, http.StatusFound)
		return
	}
	if !passOK || passAccount != sess.Data.AccountID {
		confirm := "/sign-out"
		if dest != "" {
			confirm += "?" + url.Values{"rd": {dest}}.Encode()
		}
		http.Redirect(w, r, confirm, http.StatusFound)
		return
	}

	// Terminate the SSO session (same path as POST /auth/logout).
	if c, err := r.Cookie(sessstore.SessionCookieNameFor(s.config)); err == nil && c.Value != "" {
		if id, tok, ok := sessstore.ParseCookieValue(c.Value); ok {
			_ = s.sessionStore.Revoke(ctx, id, tok)
			logx.WithContext(ctx).WithFields(logrus.Fields{
				"event":      "auth.logout",
				"account_id": id,
				"client_ip":  s.clientIP.IP(r),
				"via":        "forward_auth",
			}).Info("auth")
			audit.RecordOrLog(ctx, s.Audit, audit.Record{
				AccountID: &id,
				Factor:    audit.FactorSession,
				Event:     audit.EventSessionEnd,
				Detail:    map[string]any{"reason": "forward_auth_signout"},
			})
		}
	}
	http.SetCookie(w, sessstore.ClearedSessionCookie(s.config, r))
	http.Redirect(w, r, back, http.StatusFound)
}
