package oidc

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/db"
	"prohibitorum/pkg/kv"
)

const ForwardAuthPathPrefix = "/.prohibitorum-forward-auth"
const forwardAuthCookieBase = "prohibitorum_forward_auth"

// faSession is the payload stored in KV for a forward-auth session cookie.
type faSession struct {
	AccountID int32  `json:"account_id"`
	ClientID  string `json:"client_id"`
}

func faSessionKey(token string) string { return "fa:session:" + token }

// mintFASession stores a forward-auth session in KV and returns the opaque token.
func mintFASession(ctx context.Context, store kv.Store, s faSession, ttl time.Duration) (string, error) {
	tok, err := faRandToken()
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	if err := store.SetEx(ctx, faSessionKey(tok), string(payload), ttl); err != nil {
		return "", err
	}
	return tok, nil
}

// loadFASession retrieves a forward-auth session from KV. Returns nil when the
// token is absent, expired, or malformed.
func loadFASession(ctx context.Context, store kv.Store, token string) *faSession {
	if token == "" {
		return nil
	}
	raw, err := store.Get(ctx, faSessionKey(token))
	if err != nil || raw == "" {
		return nil
	}
	var s faSession
	if json.Unmarshal([]byte(raw), &s) != nil {
		return nil
	}
	return &s
}

// faState is the payload stored in KV for a single-use OAuth2 state parameter,
// carrying the original request URL plus the PKCE verifier.
type faState struct {
	OriginalURL string `json:"original_url"`
	ClientID    string `json:"client_id"`
	Verifier    string `json:"verifier"`
}

func faStateKey(id string) string { return "fa:state:" + id }

// mintFAState stores a single-use forward-auth state in KV and returns the
// opaque state ID.
func mintFAState(ctx context.Context, store kv.Store, s faState, ttl time.Duration) (string, error) {
	id, err := faRandToken()
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	if err := store.SetEx(ctx, faStateKey(id), string(payload), ttl); err != nil {
		return "", err
	}
	return id, nil
}

// popFAState atomically retrieves and removes a forward-auth state from KV,
// enforcing single-use. Returns nil when the state is absent or malformed.
func popFAState(ctx context.Context, store kv.Store, id string) *faState {
	if id == "" {
		return nil
	}
	raw, err := store.Pop(ctx, faStateKey(id))
	if err != nil || raw == "" {
		return nil
	}
	var s faState
	if json.Unmarshal([]byte(raw), &s) != nil {
		return nil
	}
	return &s
}

// faRandToken returns 32 bytes of cryptographic randomness encoded as
// base64url without padding — the same approach used by mintCode in codes.go.
func faRandToken() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf[:]), nil
}

// pkceChallengeS256 computes the RFC 7636 S256 code challenge for a verifier.
// The vector in Appendix B:
//
//	verifier  = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
//	challenge = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
func pkceChallengeS256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// faCookieName returns the cookie name for forward-auth. When secure is true
// the __Host- prefix is used (host-only binding per RFC 6265bis).
func faCookieName(secure bool) string {
	if secure {
		return "__Host-" + forwardAuthCookieBase
	}
	return forwardAuthCookieBase
}

// faCookie builds a forward-auth session cookie. When secure is true the
// __Host- prefix and Secure flag are set; Domain is always empty (host-only).
func faCookie(secure bool, token string) *http.Cookie {
	return &http.Cookie{
		Name:     faCookieName(secure),
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		// Domain intentionally omitted for host-only binding.
	}
}

// writeIdentityHeaders sets the Traefik/nginx ForwardAuth identity headers on w.
// Remote-Email is omitted when email is empty.
func writeIdentityHeaders(w http.ResponseWriter, user, name, email string, groups []string) {
	w.Header().Set("Remote-User", user)
	w.Header().Set("Remote-Name", name)
	if email != "" {
		w.Header().Set("Remote-Email", email)
	}
	w.Header().Set("Remote-Groups", strings.Join(groups, ","))
}

// HandleForwardAuthVerify is the Traefik ForwardAuth target. Traefik forwards
// X-Forwarded-* + the original (protected-domain) cookies. 200 = allow (+
// identity headers); 302 = bootstrap auth via /oauth/authorize; 403 = the host
// is not a registered forward-auth app.
func (p *Provider) HandleForwardAuthVerify(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	host := r.Header.Get("X-Forwarded-Host")
	proto := r.Header.Get("X-Forwarded-Proto")
	if proto == "" {
		proto = "https"
	}
	client, err := p.queries.GetForwardAuthClientByHost(ctx, pgtype.Text{String: host, Valid: host != ""})
	if err != nil || client.Disabled {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	secure := proto == "https"

	if c, cerr := r.Cookie(faCookieName(secure)); cerr == nil {
		if sess := loadFASession(ctx, p.kv, c.Value); sess != nil && sess.ClientID == client.ClientID {
			ok, aerr := p.queries.IsAccountAuthorizedForOIDCClient(ctx, db.IsAccountAuthorizedForOIDCClientParams{
				AccountID: pgtype.Int4{Int32: sess.AccountID, Valid: true}, ClientID: client.ClientID,
			})
			if aerr == nil && ok.Bool {
				if acct, gerr := p.queries.GetAccountByID(ctx, sess.AccountID); gerr == nil && !acct.Disabled {
					groups, _ := p.queries.ListExposedGroupSlugsByAccount(ctx, acct.ID)
					writeIdentityHeaders(w, acct.Username, acct.DisplayName, accountEmail(acct), groups)
					w.WriteHeader(http.StatusOK)
					return
				}
			}
		}
	}

	original := proto + "://" + host + r.Header.Get("X-Forwarded-Uri")
	verifier, verr := faRandToken()
	if verr != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	stateID, serr := mintFAState(ctx, p.kv, faState{
		OriginalURL: original, ClientID: client.ClientID, Verifier: verifier,
	}, 5*time.Minute)
	if serr != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	redirectURI := proto + "://" + host + ForwardAuthPathPrefix + "/callback"
	q := url.Values{}
	q.Set("client_id", client.ClientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("response_type", "code")
	q.Set("scope", "openid email groups")
	q.Set("code_challenge", pkceChallengeS256(verifier))
	q.Set("code_challenge_method", "S256")
	q.Set("state", stateID)
	http.Redirect(w, r, p.cfg.OIDC.Issuer+"/oauth/authorize?"+q.Encode(), http.StatusFound)
}

// accountEmail returns the account email string if set and valid, else "".
// db.Account.Email is pgtype.Text (confirmed from pkg/protocol/oidc/claims.go
// emailClaims, which reads a.Email.String and a.Email.Valid).
func accountEmail(a db.Account) string {
	if a.Email.Valid {
		return a.Email.String
	}
	return ""
}
