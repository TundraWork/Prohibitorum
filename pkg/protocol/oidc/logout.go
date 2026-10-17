package oidc

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"

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
		claims, err := p.verifyIDTokenHint(ctx, hint)
		if err != nil {
			p.redirectToErrorPage(w, r, errCodeInvalidRequest)
			return
		}

		sid, _ := claims["sid"].(string)
		// Task 4 encodes a single audience as a bare string.
		clientID, _ = claims["aud"].(string)

		if sid != "" {
			if acct, err := accountFromTokenClaims(ctx, p.queries, claims); err == nil {
				if _, err := p.sessions.RevokeBySessionID(ctx, acct.ID, sid); err == nil {
					p.auditLogout(ctx, r, acct.ID, clientID)
				}
			}
		}
	}

	if postLogout != "" {
		// A post_logout_redirect_uri can only be honored when a hint identifies
		// the client whose allowlist we check. Without it we cannot safely
		// validate the URI, so this is a direct error rather than a redirect.
		if clientID == "" {
			p.redirectToErrorPage(w, r, errCodeInvalidRequest)
			return
		}
		client, err := loadClient(ctx, p.queries, clientID)
		if err != nil {
			p.redirectToErrorPage(w, r, errCodeInvalidRequest)
			return
		}
		if !slices.Contains(client.PostLogoutRedirectUris, postLogout) {
			// Never redirect to an unregistered URI — send the user to /error.
			p.redirectToErrorPage(w, r, errCodeInvalidRequest)
			return
		}
		u, err := url.Parse(postLogout)
		if err != nil {
			p.redirectToErrorPage(w, r, errCodeInvalidRequest)
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
	audit.RecordOrLog(ctx, p.audit, audit.Record{
		AccountID: &id,
		Factor:    audit.FactorOIDCClient,
		Event:     audit.EventSessionEnd,
		IP:        audit.ParseIPOrNil(p.auditIP(r)),
		UserAgent: r.UserAgent(),
		Detail: map[string]any{
			"reason":    "logout",
			"client_id": clientID,
		},
	})
}

// errInvalidIDTokenHint is any id_token_hint that is not an ID token this OP
// issued.
var errInvalidIDTokenHint = errors.New("oidc: invalid id_token_hint")

// verifyIDTokenHint checks that hint is an ID token this OP signed: signature
// and kid, not an access token, and this issuer. Expiry is deliberately not
// checked: a hint names the account, and an application usually holds an ID
// token that has long expired by the time it sends one (logout, a
// require-identity request).
func (p *Provider) verifyIDTokenHint(ctx context.Context, hint string) (map[string]any, error) {
	claims, typ, err := p.verifyJWT(ctx, hint)
	if err != nil {
		return nil, errInvalidIDTokenHint
	}
	// An access token carries JOSE typ "at+jwt"; an ID token carries "JWT"
	// (or nothing, for some signers).
	if typ == "at+jwt" {
		return nil, errInvalidIDTokenHint
	}
	if iss, _ := claims["iss"].(string); iss != p.cfg.OIDC.Issuer {
		return nil, errInvalidIDTokenHint
	}
	return claims, nil
}
