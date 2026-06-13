package oidc

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/db"
)

// AccessTokenTTL and IDTokenTTL are the DEFAULT lifetimes of the access and ID
// tokens minted at the token endpoint, used when oidc.access_token_ttl /
// oidc.id_token_ttl are unset. Ten minutes is a conventional short access-token
// lifetime; the refresh token (RefreshTokenTTL, refresh.go) carries the
// long-lived grant and is rotated on each use. Read the effective values via
// p.accessTokenTTL() / p.idTokenTTL(), never these consts directly.
const (
	AccessTokenTTL = 10 * time.Minute
	IDTokenTTL     = 10 * time.Minute
)

// tokenRateMax / tokenRateWindow cap token-endpoint calls per authenticated
// client. 120/min is generous for any legitimate RP (each user login is a
// single code exchange; refresh rotations are infrequent) while bounding a
// compromised client credential's blast radius.
const (
	tokenRateMax    = 120
	tokenRateWindow = time.Minute
)

// tokenResponse is the RFC 6749 §5.1 / OIDC token-endpoint success body.
// Shared by the authorization_code and (Task 10) refresh_token grants.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	IDToken      string `json:"id_token,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope"`
}

// writeTokenResponse renders a token-endpoint success body as JSON with the
// RFC 6749 §5.1-mandated Cache-Control: no-store.
func writeTokenResponse(w http.ResponseWriter, resp tokenResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(resp)
}

// verifyPKCE checks an S256 PKCE proof: base64url(SHA256(verifier)) must equal
// the challenge captured at /authorize. Empty inputs always fail. The compare
// is constant-time so a mismatching prefix leaks no timing signal.
func verifyPKCE(verifier, challenge string) bool {
	if verifier == "" || challenge == "" {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	computed := base64.RawURLEncoding.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(computed), []byte(challenge)) == 1
}

// randJTI returns 32 bytes of cryptographic randomness, base64url-encoded
// without padding, for use as an access-token jti (RFC 9068 §4: a unique
// identifier so resource servers can track / blacklist individual tokens).
func randJTI() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf[:]), nil
}

// HandleToken implements the OAuth 2.0 / OIDC token endpoint (RFC 6749 §3.2)
// at POST /oauth/token. It authenticates the client, rate-limits per client,
// and dispatches on grant_type. Client authentication failures map uniformly
// to invalid_client (401) so they cannot be used to enumerate clients.
func (p *Provider) HandleToken(w http.ResponseWriter, r *http.Request) {
	// authenticateClient calls r.ParseForm(); ParseForm again defensively in
	// case the auth path changes — it is idempotent and cheap.
	if err := r.ParseForm(); err != nil {
		writeOIDCError(w, http.StatusBadRequest, errCodeInvalidRequest, "could not parse request body")
		return
	}

	client, err := authenticateClient(r.Context(), p.queries, r)
	if err != nil {
		// Every client load/auth failure collapses to invalid_client (401) per
		// RFC 6749 §5.2; do not distinguish unknown client from bad secret.
		writeInvalidClient(w, r, "client authentication failed")
		return
	}

	rlKey := "oidc:token:client:" + client.ClientID
	if !p.rl.Allow(rlKey, tokenRateMax, tokenRateWindow) {
		if ra := p.rl.RetryAfter(rlKey); ra > 0 {
			w.Header().Set("Retry-After", strconv.Itoa(int(ra.Seconds())+1))
		}
		writeOIDCError(w, http.StatusTooManyRequests, errCodeTemporarilyUnavailable, "rate limit exceeded")
		return
	}

	switch r.PostForm.Get("grant_type") {
	case "authorization_code":
		p.grantAuthorizationCode(w, r, client)
	case "refresh_token":
		p.grantRefreshToken(w, r, client)
	default:
		writeOIDCError(w, http.StatusBadRequest, errCodeUnsupportedGrantType, "unsupported grant_type")
	}
}

// grantAuthorizationCode exchanges a single-use authorization code for an
// access token, ID token, and (when offline_access was granted) a refresh
// token. It enforces client binding, redirect_uri match, and PKCE, and trips
// reuse detection: a replayed code revokes the refresh family it had minted.
func (p *Provider) grantAuthorizationCode(w http.ResponseWriter, r *http.Request, client db.OidcClient) {
	ctx := r.Context()
	code := r.PostForm.Get("code")

	ac, err := consumeCode(ctx, p.kv, code)
	if err != nil {
		if errors.Is(err, errCodeNotFound) {
			// A miss may be a replay: if this code was previously consumed, it
			// recorded the refresh family it minted. Revoke that whole family
			// (defeating a stolen-code race) and audit a security event.
			if fid, ok := usedFamily(ctx, p.kv, code); ok {
				_ = revokeFamily(ctx, p.kv, fid)
				p.auditTokenEvent(ctx, r, audit.EventFail, nil, map[string]any{
					"reason":    "code_replay",
					"client_id": client.ClientID,
				})
				writeOIDCError(w, http.StatusBadRequest, errCodeInvalidGrant, "authorization code already used")
				return
			}
			writeOIDCError(w, http.StatusBadRequest, errCodeInvalidGrant, "invalid authorization code")
			return
		}
		// A decode/storage error is not a client fault, but surfacing it as
		// invalid_grant keeps the endpoint from leaking internal state and
		// matches the not-found handling — the code is unusable either way.
		writeOIDCError(w, http.StatusBadRequest, errCodeInvalidGrant, "invalid authorization code")
		return
	}

	// Bind the code to the authenticated client (RFC 6749 §4.1.3): a code
	// minted for one client must not be redeemable by another.
	if ac.ClientID != client.ClientID {
		writeOIDCError(w, http.StatusBadRequest, errCodeInvalidGrant, "client mismatch")
		return
	}

	// redirect_uri MUST match the one used at /authorize (RFC 6749 §4.1.3).
	if r.PostForm.Get("redirect_uri") != ac.RedirectURI {
		writeOIDCError(w, http.StatusBadRequest, errCodeInvalidGrant, "redirect_uri mismatch")
		return
	}

	// PKCE verification (RFC 7636). The challenge was bound to the code at
	// /authorize; only the holder of the verifier may redeem it. verifyPKCE
	// implements S256 only. Defense-in-depth: when a challenge IS present, the
	// method must be S256 — reject 'plain' and an omitted method ("" means plain
	// per RFC 7636 §4.3) rather than mis-verify. /authorize already rejects these
	// at mint time (the DB CHECK forbids 'plain' in the allowed set entirely), so
	// this only fires on a malformed/forged stored code. The guard is gated on a
	// non-empty challenge so a legitimate no-PKCE code (no challenge, empty
	// method) is not caught here.
	if ac.CodeChallenge != "" && (ac.CodeChallengeMethod == "plain" || ac.CodeChallengeMethod == "") {
		writeOIDCError(w, http.StatusBadRequest, errCodeInvalidGrant, "unsupported PKCE method")
		return
	}
	// Only run PKCE verification when a challenge was actually captured at
	// /authorize. verifyPKCE("", "") is false, so calling it unconditionally
	// would reject a legitimate no-PKCE code (require_pkce=false client that sent
	// no challenge) with "PKCE verification failed". This does NOT weaken PKCE: a
	// require_pkce=true client always has a stored challenge (HandleAuthorize
	// rejects an empty code_challenge at mint time), so a missing challenge is
	// only ever reachable for a require_pkce=false client that legitimately opted
	// out — exactly the case that must exchange without a verifier.
	if ac.CodeChallenge != "" {
		if !verifyPKCE(r.PostForm.Get("code_verifier"), ac.CodeChallenge) {
			writeOIDCError(w, http.StatusBadRequest, errCodeInvalidGrant, "PKCE verification failed")
			return
		}
	}

	acct, err := p.queries.GetAccountByID(ctx, ac.AccountID)
	if err != nil {
		writeOIDCError(w, http.StatusBadRequest, errCodeInvalidGrant, "account not found")
		return
	}
	if acct.Disabled {
		writeOIDCError(w, http.StatusBadRequest, errCodeInvalidGrant, "account disabled")
		return
	}

	// Re-bind the code to a still-live session. /authorize loaded the session,
	// but within the short code window the user may have logged out or had the
	// session revoked. GetSession filters revoked_at IS NULL, so a miss means the
	// originating session is gone — refuse the exchange rather than mint tokens
	// (including a long-lived refresh) tied to a dead session (audit OIDC-3).
	// Guarded on a non-empty SessionID so any non-session-bound code is unaffected.
	if ac.SessionID != "" {
		if _, serr := p.queries.GetSession(ctx, ac.SessionID); serr != nil {
			writeOIDCError(w, http.StatusBadRequest, errCodeInvalidGrant, "originating session is no longer valid")
			return
		}
	}

	now := time.Now()

	accessToken, idToken, err := p.mintAccessAndIDTokens(ctx, acct, client.ClientID, ac.Nonce, ac.SessionID, ac.ACR, ac.AMR, ac.Scope, ac.AuthTime, now)
	if err != nil {
		writeOIDCError(w, http.StatusInternalServerError, errCodeServerError, "could not mint tokens")
		return
	}

	// Refresh token only when offline_access was granted. We then record, on
	// the consumed code, which family it minted: a later replay of this code
	// revokes that family. issueRefresh returns the family id directly, so the
	// replay marker is recorded without a second lookup.
	var refreshToken string
	if hasScope(ac.Scope, "offline_access") {
		rt, fid, err := issueRefresh(ctx, p.kv, refreshFamily{
			ClientID:  client.ClientID,
			AccountID: acct.ID,
			SessionID: ac.SessionID,
			Scope:     ac.Scope,
			AuthTime:  ac.AuthTime,
			AMR:       ac.AMR,
			ACR:       ac.ACR,
		}, p.refreshTokenTTL())
		if err != nil {
			writeOIDCError(w, http.StatusInternalServerError, errCodeServerError, "could not issue refresh token")
			return
		}
		_ = markCodeUsed(ctx, p.kv, code, fid, p.authCodeTTL())
		refreshToken = rt
	}

	acctID := acct.ID
	p.auditTokenEvent(ctx, r, audit.EventUse, &acctID, map[string]any{
		"reason":    "token_issued",
		"client_id": client.ClientID,
		"scope":     ac.Scope,
	})

	writeTokenResponse(w, tokenResponse{
		AccessToken:  accessToken,
		TokenType:    "Bearer",
		ExpiresIn:    int(p.accessTokenTTL().Seconds()),
		IDToken:      idToken,
		RefreshToken: refreshToken,
		Scope:        strings.Join(ac.Scope, " "),
	})
}

// mintAccessAndIDTokens builds and signs the RFC 9068 access token and the
// OIDC ID token (with at_hash bound to the access token) for an authenticated
// grant. It is shared by the authorization_code and refresh_token grants.
//
// nonce is "" for the refresh grant (the refresh family snapshots no nonce, so
// the re-issued ID token correctly omits the nonce claim). The access token's
// aud is the issuer itself: the OP is its own resource server (notably for
// /userinfo). Returns (accessToken, idToken, err).
func (p *Provider) mintAccessAndIDTokens(ctx context.Context, acct db.Account, clientID, nonce, sid, acr string, amr, scope []string, authTime, now time.Time) (string, string, error) {
	issuer := p.cfg.OIDC.Issuer

	jti, err := randJTI()
	if err != nil {
		return "", "", err
	}
	atClaims := map[string]any{
		"iss":       issuer,
		"sub":       subjectOf(acct),
		"aud":       issuer,
		"client_id": clientID,
		"exp":       now.Add(p.accessTokenTTL()).Unix(),
		"iat":       now.Unix(),
		"jti":       jti,
		"scope":     strings.Join(scope, " "),
	}
	accessToken, err := p.signJWT(ctx, atClaims, "at+jwt")
	if err != nil {
		return "", "", err
	}

	avatarOrigin := issuer
	if len(p.cfg.PublicOrigins) > 0 {
		avatarOrigin = p.cfg.PublicOrigins[0]
	}
	idClaims := idTokenClaims(acct, idTokenInput{
		Issuer:       issuer,
		AvatarOrigin: avatarOrigin,
		Audience:     clientID,
		Nonce:        nonce,
		ACR:          acr,
		SID:          sid,
		AMR:          amr,
		AccessToken:  accessToken,
		Scope:        scope,
		IssuedAt:     now,
		Expiry:       now.Add(p.idTokenTTL()),
		AuthTime:     authTime,
	})
	idToken, err := p.signJWT(ctx, idClaims, "JWT")
	if err != nil {
		return "", "", err
	}
	return accessToken, idToken, nil
}

// auditTokenEvent records a token-endpoint audit event (best-effort) under the
// oidc_client factor, mirroring authorize.go's convention.
func (p *Provider) auditTokenEvent(ctx context.Context, r *http.Request, event string, accountID *int32, detail map[string]any) {
	_ = p.audit.Record(ctx, audit.Record{
		AccountID: accountID,
		Factor:    audit.FactorOIDCClient,
		Event:     event,
		IP:        audit.ParseIPOrNil(r.RemoteAddr),
		UserAgent: r.UserAgent(),
		Detail:    detail,
	})
}
