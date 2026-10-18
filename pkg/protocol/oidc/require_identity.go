package oidc

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/kv"
	"prohibitorum/pkg/weberr"
)

// The public codes a require-identity request can end on. Every one goes to
// the /error page: none of them sends the browser to an unchecked redirect_uri.
const (
	errCodeRequireIdentityInvalidClient      = "require_identity_invalid_client"
	errCodeRequireIdentityInvalidRedirectURI = "require_identity_invalid_redirect_uri"
	errCodeRequireIdentityInvalidProviders   = "require_identity_invalid_providers"
	errCodeRequireIdentityInvalidIDToken     = "require_identity_invalid_id_token"
)

const (
	// maxRequireIdentityProviders bounds the providers one request may list.
	maxRequireIdentityProviders = 10
	// requireIdentityTicketTTL leaves time for a slow proof, such as editing a
	// VRChat profile bio.
	requireIdentityTicketTTL = 30 * time.Minute
	requireIdentityKVPrefix  = "oidc:require-identity:"
)

// ErrRequireIdentityTicketNotFound is a ticket that does not exist, has
// expired, or belongs to another account or session.
var ErrRequireIdentityTicketNotFound = errors.New("oidc: require-identity ticket not found")

// RequireIdentityTicket is a require-identity request the user has not yet
// satisfied, kept while they link a provider on the in-between page.
type RequireIdentityTicket struct {
	ClientID    string   `json:"client_id"`
	RedirectURI string   `json:"redirect_uri"`
	Providers   []string `json:"providers"`
	AccountID   int32    `json:"account_id"`
	SessionID   string   `json:"session_id"`
	// Mismatch marks a request whose id_token_hint names another account.
	Mismatch bool `json:"mismatch"`
	// Entry is the request's own address, for signing in as another account
	// and coming back to it.
	Entry string `json:"entry"`
}

// HandleRequireIdentity serves GET /api/prohibitorum/require-identity: an
// application sends the user here when it needs an identity from one of the
// listed providers. A user who already has one goes straight back to
// redirect_uri; otherwise the request becomes a ticket and the browser goes to
// the in-between page, where the user links one.
func (p *Provider) HandleRequireIdentity(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	clientID := q.Get("client_id")
	redirectURI := q.Get("redirect_uri")

	client, err := loadClient(ctx, p.queries, clientID)
	if err != nil {
		if errors.Is(err, errInvalidClient) {
			p.requireIdentityError(w, r, errCodeRequireIdentityInvalidClient, "client_id", clientID)
		} else {
			p.requireIdentityError(w, r, errCodeServerError, "error", err.Error())
		}
		return
	}
	if redirectURI == "" || !slices.Contains(client.RedirectUris, redirectURI) {
		p.requireIdentityError(w, r, errCodeRequireIdentityInvalidRedirectURI, "client_id", clientID)
		return
	}

	var providers []string
	for _, slug := range q["providers"] {
		if !slices.Contains(providers, slug) {
			providers = append(providers, slug)
		}
	}
	if len(providers) == 0 || len(providers) > maxRequireIdentityProviders {
		p.requireIdentityError(w, r, errCodeRequireIdentityInvalidProviders, "providers", len(providers))
		return
	}
	for _, slug := range providers {
		if _, err := p.queries.GetUpstreamIDPBySlug(ctx, slug); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				p.requireIdentityError(w, r, errCodeRequireIdentityInvalidProviders, "provider", slug)
			} else {
				p.requireIdentityError(w, r, errCodeServerError, "error", err.Error())
			}
			return
		}
	}

	var hintClaims map[string]any
	if hint := q.Get("id_token_hint"); hint != "" {
		claims, err := p.verifyIDTokenHint(ctx, hint)
		if err != nil || !audienceIncludes(claims["aud"], client.ClientID) {
			p.requireIdentityError(w, r, errCodeRequireIdentityInvalidIDToken, "client_id", clientID)
			return
		}
		hintClaims = claims
	}

	entry := p.cfg.OIDC.Issuer + r.URL.RequestURI()
	sess := authn.SessionFromContext(ctx)
	if sess == nil || sess.Data == nil || (sess.Account != nil && sess.Account.Disabled) {
		http.Redirect(w, r, p.cfg.OIDC.Issuer+"/login?return_to="+url.QueryEscape(entry), http.StatusFound)
		return
	}
	accountID := sess.Data.AccountID

	ticket := RequireIdentityTicket{
		ClientID: client.ClientID, RedirectURI: redirectURI, Providers: providers,
		AccountID: accountID, SessionID: sess.Data.SessionID, Entry: entry,
	}
	if hintClaims != nil {
		hinted, err := accountFromTokenClaims(ctx, p.queries, hintClaims)
		switch {
		case errors.Is(err, pgx.ErrNoRows), errors.Is(err, errPrincipalUnavailable):
			// The hint names no account we can find: not proof it is this one.
			ticket.Mismatch = true
		case err != nil:
			p.requireIdentityError(w, r, errCodeServerError, "error", err.Error())
			return
		case hinted.ID != accountID:
			ticket.Mismatch = true
		}
		if ticket.Mismatch {
			p.redirectToRequireIdentityPage(w, r, ticket)
			return
		}
	}

	decision, err := p.evaluateOIDCAccess(ctx, accountID, client.ClientID)
	if err != nil {
		p.requireIdentityError(w, r, errCodeServerError, "error", err.Error())
		return
	}
	if !decision.Allowed {
		audit.RecordOrLog(ctx, p.audit, audit.Record{
			AccountID: &accountID,
			Factor:    audit.FactorOIDCClient,
			Event:     audit.EventAccessDenied,
			IP:        audit.ParseIPOrNil(p.auditIP(r)),
			UserAgent: r.UserAgent(),
			Detail: map[string]any{
				"reason":    appAccessAuditReason(decision.Source),
				"client_id": client.ClientID,
			},
		})
		p.appAccessDenied(w, r, redirectURI, client.DisplayName, "", entry, false)
		return
	}

	satisfied, err := p.queries.HasConfirmedIdentityForProviders(ctx, db.HasConfirmedIdentityForProvidersParams{
		AccountID: accountID, Slugs: providers,
	})
	if err != nil {
		p.requireIdentityError(w, r, errCodeServerError, "error", err.Error())
		return
	}
	if satisfied {
		http.Redirect(w, r, redirectURI, http.StatusFound)
		return
	}
	p.redirectToRequireIdentityPage(w, r, ticket)
}

// redirectToRequireIdentityPage stores the ticket and sends the browser to the
// in-between page that reads it.
func (p *Provider) redirectToRequireIdentityPage(w http.ResponseWriter, r *http.Request, ticket RequireIdentityTicket) {
	id, err := storeRequireIdentityTicket(r.Context(), p.kv, ticket)
	if err != nil {
		p.requireIdentityError(w, r, errCodeServerError, "error", err.Error())
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, p.cfg.OIDC.Issuer+"/require-identity/"+id, http.StatusFound)
}

// requireIdentityError ends the request on the /error page and logs why, with
// the same ref the page shows.
func (p *Provider) requireIdentityError(w http.ResponseWriter, r *http.Request, code string, attrs ...any) {
	ref := weberr.NewRef()
	slog.Warn("require-identity request refused", append([]any{"code", code, "ref", ref}, attrs...)...)
	weberr.RedirectToError(w, r, code, ref)
}

// audienceIncludes reports whether an aud claim, a single string or a list,
// names clientID.
func audienceIncludes(aud any, clientID string) bool {
	switch v := aud.(type) {
	case string:
		return v == clientID
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok && s == clientID {
				return true
			}
		}
	}
	return false
}

func storeRequireIdentityTicket(ctx context.Context, store kv.Store, ticket RequireIdentityTicket) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	id := base64.RawURLEncoding.EncodeToString(raw)
	value, err := json.Marshal(ticket)
	if err != nil {
		return "", err
	}
	if err := store.SetEx(ctx, requireIdentityKVPrefix+id, string(value), requireIdentityTicketTTL); err != nil {
		return "", err
	}
	return id, nil
}

// LoadRequireIdentityTicket reads a ticket for the account and session that
// created it. Anyone else gets ErrRequireIdentityTicketNotFound, as if it did
// not exist.
func (p *Provider) LoadRequireIdentityTicket(ctx context.Context, id string, accountID int32, sessionID string) (RequireIdentityTicket, error) {
	value, err := p.kv.Get(ctx, requireIdentityKVPrefix+id)
	if errors.Is(err, kv.ErrKeyNotFound) {
		return RequireIdentityTicket{}, ErrRequireIdentityTicketNotFound
	}
	if err != nil {
		return RequireIdentityTicket{}, err
	}
	var ticket RequireIdentityTicket
	if err := json.Unmarshal([]byte(value), &ticket); err != nil {
		return RequireIdentityTicket{}, err
	}
	if ticket.AccountID != accountID || ticket.SessionID != sessionID {
		return RequireIdentityTicket{}, ErrRequireIdentityTicketNotFound
	}
	return ticket, nil
}
