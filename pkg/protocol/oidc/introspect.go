package oidc

import (
	"encoding/json"
	"net/http"
	"strings"
)

// writeIntrospectionInactive renders the RFC 7662 §2.2 inactive response:
// `{"active":false}` with no other members. Every failure path (bad token,
// expired, not owned by the caller, unknown) collapses to this so the endpoint
// never leaks why a token is not active.
func writeIntrospectionInactive(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{"active": false})
}

// HandleIntrospect implements OAuth 2.0 Token Introspection (RFC 7662) at
// POST /oauth/introspect. The caller authenticates as a client; it may only
// introspect tokens issued to itself. An access JWT is verified and
// revocation-checked; a refresh token is resolved (without consuming it) via
// the family store. Anything unverifiable, expired, revoked, or owned by a
// different client returns `{"active":false}`.
func (p *Provider) HandleIntrospect(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	client, err := authenticateClient(ctx, p.queries, r)
	if err != nil {
		// Introspection requires client authentication (RFC 7662 §2.1).
		writeInvalidClient(w, r, "client authentication failed")
		return
	}

	token := r.PostForm.Get("token")
	if token == "" {
		writeIntrospectionInactive(w)
		return
	}

	// Try the token as an access JWT first. token_type_hint is advisory only;
	// we always fall through to the refresh-token path on any failure.
	if claims, err := p.validateAccessToken(ctx, token); err == nil {
		// Ownership: a client may only introspect its own tokens. A mismatch is
		// reported as inactive rather than as an error.
		if cid, _ := claims["client_id"].(string); cid == client.ClientID {
			resp := map[string]any{
				"active":     true,
				"token_type": "access_token",
				"client_id":  claims["client_id"],
			}
			if v, ok := claims["sub"]; ok {
				resp["sub"] = v
			}
			if v, ok := claims["scope"]; ok {
				resp["scope"] = v
			}
			if v, ok := claims["exp"]; ok {
				resp["exp"] = v
			}
			if v, ok := claims["iat"]; ok {
				resp["iat"] = v
			}
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		writeIntrospectionInactive(w)
		return
	}

	// Not a live access token: try it as a refresh token (read-only lookup).
	if fam, ok := lookupRefresh(ctx, p.kv, token); ok && fam.ClientID == client.ClientID {
		resp := map[string]any{
			"active":     true,
			"token_type": "refresh_token",
			"client_id":  fam.ClientID,
			"scope":      strings.Join(fam.Scope, " "),
		}
		// sub is optional for refresh tokens; include it when the account
		// resolves cheaply.
		if acct, err := p.queries.GetAccountByID(ctx, fam.AccountID); err == nil {
			resp["sub"] = subjectOf(acct)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(resp)
		return
	}

	writeIntrospectionInactive(w)
}
