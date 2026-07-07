package oidc

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/db"
)

// HandleRevoke implements OAuth 2.0 Token Revocation (RFC 7009) at
// POST /oauth/revoke. The caller authenticates as a client and may revoke only
// tokens issued to itself. An access token is added to the jti denylist (with
// its own exp so the row self-prunes); a refresh token has its whole family
// revoked. Per RFC 7009 §2.2 the endpoint always responds 200 — for unknown,
// malformed, or already-invalid tokens, and for tokens belonging to a
// different client (which are silently left untouched). Only an actual
// revocation is audited.
func (p *Provider) HandleRevoke(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	client, err := authenticateClient(ctx, p.queries, r)
	if err != nil {
		// Revocation requires client authentication (RFC 7009 §2.1).
		writeInvalidClient(w, r, "client authentication failed")
		return
	}

	token := r.PostForm.Get("token")
	if token == "" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Try the token as an access JWT first. token_type_hint is advisory only.
	if claims, err := p.validateAccessToken(ctx, token); err == nil {
		cid, _ := claims["client_id"].(string)
		jti, _ := claims["jti"].(string)
		expf, _ := claims["exp"].(float64)
		if cid == client.ClientID && jti != "" {
			// Use the token's own exp as the denylist row's expiry so it prunes
			// itself once the token would have expired anyway.
			_ = p.queries.InsertRevokedJTI(ctx, db.InsertRevokedJTIParams{
				Jti:       jti,
				ExpiresAt: pgtype.Timestamptz{Time: time.Unix(int64(expf), 0), Valid: true},
				Reason:    pgtype.Text{String: "revoke", Valid: true},
			})
			// Resolve the AccountID from the sub claim for attribution.
			var acctID *int32
			if sub, _ := claims["sub"].(string); sub != "" {
				var u pgtype.UUID
				if scanErr := u.Scan(sub); scanErr == nil {
					if acct, qErr := p.queries.GetAccountByOIDCSubject(ctx, u); qErr == nil {
						id := acct.ID
						acctID = &id
					}
				}
			}
			p.auditRevoked(ctx, r, client.ClientID, "access_token", acctID)
		}
		// If the token doesn't belong to the caller, do nothing — still 200.
		w.WriteHeader(http.StatusOK)
		return
	}

	// Not a live access token: try it as a refresh token.
	if fam, ok := lookupRefresh(ctx, p.kv, token); ok && fam.ClientID == client.ClientID {
		_ = revokeFamily(ctx, p.kv, fam.FamilyID)
		famAcctID := fam.AccountID
		p.auditRevoked(ctx, r, client.ClientID, "refresh_token", &famAcctID)
	}

	// Unknown / invalid / not-owned: RFC 7009 — still 200.
	w.WriteHeader(http.StatusOK)
}

// auditRevoked records a best-effort revoke audit event under the oidc_client
// factor, mirroring auditTokenEvent's convention. accountID may be nil when
// the revoking actor's account cannot be determined (e.g. a garbage token).
func (p *Provider) auditRevoked(ctx context.Context, r *http.Request, clientID, tokenType string, accountID *int32) {
	_ = p.audit.Record(ctx, audit.Record{
		AccountID: accountID,
		Factor:    audit.FactorOIDCClient,
		Event:     audit.EventRevoke,
		IP:        audit.ParseIPOrNil(p.auditIP(r)),
		UserAgent: r.UserAgent(),
		Detail: map[string]any{
			"reason":     "revoked",
			"client_id":  clientID,
			"token_type": tokenType,
		},
	})
}
