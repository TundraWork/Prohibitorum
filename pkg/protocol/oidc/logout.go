package oidc

import (
	"context"
	"net/http"
	"net/url"
	"slices"

	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/audit"
)

// HandleLogout implements OpenID Connect RP-Initiated Logout 1.0 at the
// end_session_endpoint (/oidc/logout). It is a GET endpoint.
//
// Flow:
//   - id_token_hint (optional but recommended): validated for signature only.
//     We deliberately TOLERATE an expired hint — logout must work after the ID
//     token expires. verifyJWT checks the RS256 signature + kid but not exp, so
//     a signature-valid-but-expired hint passes. We additionally require that
//     the hint was issued by THIS provider (iss == configured Issuer); a hint
//     for some other issuer is rejected.
//   - When the hint identifies a live IdP session (sub + sid), that single
//     session is revoked and the logout is audited.
//   - post_logout_redirect_uri (optional): exact-matched against the hint
//     client's registered PostLogoutRedirectUris allowlist. A mismatch — or a
//     post_logout_redirect_uri presented without a hint to identify the client
//     — is a DIRECT error (no redirect), mirroring the /authorize open-redirect
//     guard: we never 302 to an unregistered URI. On an exact match we 302 to
//     it, echoing state.
//   - With no post_logout_redirect_uri we 302 to the default logged-out
//     landing (the Issuer root).
func (p *Provider) HandleLogout(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	hint := r.URL.Query().Get("id_token_hint")
	postLogout := r.URL.Query().Get("post_logout_redirect_uri")
	state := r.URL.Query().Get("state")

	var clientID string
	if hint != "" {
		// Signature + RS256 + kid only — NOT exp. Expiry tolerance is required:
		// logout commonly happens after the ID token has expired.
		claims, _, err := p.verifyJWT(ctx, hint)
		if err != nil {
			writeOIDCError(w, http.StatusBadRequest, errCodeInvalidRequest, "invalid id_token_hint")
			return
		}
		if iss, _ := claims["iss"].(string); iss != p.cfg.OIDC.Issuer {
			writeOIDCError(w, http.StatusBadRequest, errCodeInvalidRequest, "id_token_hint not issued here")
			return
		}

		sub, _ := claims["sub"].(string)
		sid, _ := claims["sid"].(string)
		// Task 4 encodes a single audience as a bare string.
		clientID, _ = claims["aud"].(string)

		if sub != "" && sid != "" {
			var u pgtype.UUID
			if err := u.Scan(sub); err == nil {
				if acct, err := p.queries.GetAccountByOIDCSubject(ctx, u); err == nil {
					if _, err := p.sessions.RevokeBySessionID(ctx, acct.ID, sid); err == nil {
						p.auditLogout(ctx, r, acct.ID, clientID)
					}
				}
			}
		}
	}

	if postLogout != "" {
		// A post_logout_redirect_uri can only be honored when a hint identifies
		// the client whose allowlist we check. Without it we cannot safely
		// validate the URI, so this is a direct error rather than a redirect.
		if clientID == "" {
			writeOIDCError(w, http.StatusBadRequest, errCodeInvalidRequest, "post_logout_redirect_uri requires id_token_hint")
			return
		}
		client, err := loadClient(ctx, p.queries, clientID)
		if err != nil {
			writeOIDCError(w, http.StatusBadRequest, errCodeInvalidRequest, "unknown client")
			return
		}
		if !slices.Contains(client.PostLogoutRedirectUris, postLogout) {
			// DIRECT error — never redirect to an unregistered URI.
			writeOIDCError(w, http.StatusBadRequest, errCodeInvalidRequest, "post_logout_redirect_uri not registered")
			return
		}
		u, err := url.Parse(postLogout)
		if err != nil {
			writeOIDCError(w, http.StatusBadRequest, errCodeInvalidRequest, "post_logout_redirect_uri not registered")
			return
		}
		q := u.Query()
		if state != "" {
			q.Set("state", state)
		}
		u.RawQuery = q.Encode()
		http.Redirect(w, r, u.String(), http.StatusFound)
		return
	}

	// No post_logout_redirect_uri: send the user agent to the default landing.
	http.Redirect(w, r, p.cfg.OIDC.Issuer, http.StatusFound)
}

// auditLogout records a best-effort RP-initiated-logout event under the
// oidc_client factor, mirroring the auditRevoked / auditTokenEvent convention.
func (p *Provider) auditLogout(ctx context.Context, r *http.Request, accountID int32, clientID string) {
	id := accountID
	_ = p.audit.Record(ctx, audit.Record{
		AccountID: &id,
		Factor:    audit.FactorOIDCClient,
		Event:     audit.EventUse,
		IP:        audit.ParseIPOrNil(r.RemoteAddr),
		UserAgent: r.UserAgent(),
		Detail: map[string]any{
			"reason":    "logout",
			"client_id": clientID,
		},
	})
}
