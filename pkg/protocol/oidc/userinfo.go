package oidc

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// userinfoRateMax / userinfoRateWindow cap /userinfo calls per subject. The
// access token is short-lived and a legitimate client polls /userinfo at most
// once per login; 120/min is generous while bounding abuse of a leaked token.
const (
	userinfoRateMax    = 120
	userinfoRateWindow = time.Minute
)

// validateAccessToken verifies an access-token JWT and confirms it is live:
//   - signature verifies against a cached signing key (verifyJWT),
//   - the JOSE typ header is at+jwt (RFC 9068) — an ID token (typ JWT) is rejected,
//   - iss matches the configured issuer,
//   - aud matches the configured issuer (the OP is its own resource server),
//   - exp is in the future (JSON numbers decode as float64),
//   - the jti is not on the revocation denylist.
//
// It returns the decoded claims on success, or an error describing the first
// failing check. Callers map any error uniformly to invalid_token / inactive;
// the specific reason is never leaked on the wire.
func (p *Provider) validateAccessToken(ctx context.Context, token string) (map[string]any, error) {
	claims, typ, err := p.verifyJWT(ctx, token)
	if err != nil {
		return nil, err
	}
	if typ != "at+jwt" {
		return nil, errors.New("oidc: not an access token")
	}
	if iss, _ := claims["iss"].(string); iss != p.cfg.OIDC.Issuer {
		return nil, errors.New("oidc: issuer mismatch")
	}
	if aud, _ := claims["aud"].(string); aud != p.cfg.OIDC.Issuer {
		return nil, errors.New("oidc: audience mismatch")
	}
	expf, ok := claims["exp"].(float64)
	if !ok || time.Now().Unix() >= int64(expf) {
		return nil, errors.New("oidc: token expired")
	}
	if jti, _ := claims["jti"].(string); jti != "" {
		revoked, err := p.queries.IsJTIRevoked(ctx, jti)
		if err != nil {
			return nil, err
		}
		if revoked {
			return nil, errors.New("oidc: token revoked")
		}
	}
	return claims, nil
}

// writeBearerError renders an RFC 6750 §3 bearer-token error: a
// WWW-Authenticate challenge naming the invalid_token error plus a JSON body
// carrying the same code. The description is echoed in both. Used only by
// /userinfo, where the caller presents a bearer access token rather than
// client credentials.
func writeBearerError(w http.ResponseWriter, status int, desc string) {
	w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token", error_description="`+desc+`"`)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":             errCodeInvalidToken,
		"error_description": desc,
	})
}

// bearerToken extracts the access token from an Authorization: Bearer header,
// falling back to a POST `access_token` form field (RFC 6750 §2.2). Returns ""
// when neither is present. The header is the norm; the form field is accepted
// for completeness.
func bearerToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); h != "" {
		if tok, ok := strings.CutPrefix(h, "Bearer "); ok {
			return strings.TrimSpace(tok)
		}
	}
	_ = r.ParseForm()
	return r.PostForm.Get("access_token")
}

// HandleUserinfo implements the OIDC UserInfo endpoint (Core §5.3) at
// /oauth/userinfo. It authenticates the caller by bearer access token,
// rate-limits per subject, loads the account, and returns the scope-gated
// claim set. Any verification failure maps to a 401 invalid_token with a
// WWW-Authenticate challenge (RFC 6750); the body never reveals which check
// failed.
func (p *Provider) HandleUserinfo(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	token := bearerToken(r)
	if token == "" {
		writeBearerError(w, http.StatusUnauthorized, "missing bearer token")
		return
	}

	claims, err := p.validateAccessToken(ctx, token)
	if err != nil {
		writeBearerError(w, http.StatusUnauthorized, "invalid access token")
		return
	}

	sub, _ := claims["sub"].(string)
	if sub == "" {
		writeBearerError(w, http.StatusUnauthorized, "invalid access token")
		return
	}

	rlKey := "oidc:userinfo:sub:" + sub
	if !p.rl.Allow(rlKey, userinfoRateMax, userinfoRateWindow) {
		if ra := p.rl.RetryAfter(rlKey); ra > 0 {
			w.Header().Set("Retry-After", strconv.Itoa(int(ra.Seconds())+1))
		}
		writeOIDCError(w, http.StatusTooManyRequests, errCodeTemporarilyUnavailable, "rate limit exceeded")
		return
	}

	var u pgtype.UUID
	if err := u.Scan(sub); err != nil {
		writeBearerError(w, http.StatusUnauthorized, "invalid access token")
		return
	}
	acct, err := p.queries.GetAccountByOIDCSubject(ctx, u)
	if err != nil {
		writeBearerError(w, http.StatusUnauthorized, "invalid access token")
		return
	}
	if acct.Disabled {
		writeBearerError(w, http.StatusUnauthorized, "invalid access token")
		return
	}

	var scope []string
	if s, ok := claims["scope"].(string); ok {
		scope = strings.Fields(s)
	}

	var groups []string
	if hasScope(scope, "groups") {
		gs, gerr := p.queries.ListExposedGroupSlugsByAccount(ctx, acct.ID)
		if gerr != nil {
			writeBearerError(w, http.StatusInternalServerError, "could not load groups")
			return
		}
		groups = gs
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	avatarOrigin := p.cfg.OIDC.Issuer
	if len(p.cfg.PublicOrigins) > 0 {
		avatarOrigin = p.cfg.PublicOrigins[0]
	}
	_ = json.NewEncoder(w).Encode(userinfoClaims(acct, scope, avatarOrigin, groups))
}
