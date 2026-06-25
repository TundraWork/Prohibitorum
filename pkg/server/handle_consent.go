package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/avatar"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
)

// consentUser projects the session account into the consent screens' user view,
// carrying the public avatar URL when the account has one (empty → field
// omitted; the SPA then falls back to initials). Shared by the OIDC and SAML
// consent handlers so both screens render the same identity.
func (s *Server) consentUser(a *db.Account) contract.ConsentUser {
	u := contract.ConsentUser{DisplayName: a.DisplayName}
	origin := ""
	if s.config != nil && len(s.config.PublicOrigins) > 0 {
		origin = s.config.PublicOrigins[0]
	}
	if av := avatar.AccountURL(*a, origin); av != "" {
		u.AvatarURL = &av
	}
	return u
}

// oidcConsentQueries is the narrow DB surface the OIDC consent handlers need.
// Declared here so tests can stub it without constructing *db.Queries.
// Production wiring leaves oidcConsentOverride nil; handlers fall back to s.queries.
type oidcConsentQueries interface {
	GetOIDCClient(ctx context.Context, clientID string) (db.OidcClient, error)
	GetConsent(ctx context.Context, arg db.GetConsentParams) ([]string, error)
}

func (s *Server) getOIDCConsentQueries() oidcConsentQueries {
	if s.oidcConsentOverride != nil {
		return s.oidcConsentOverride
	}
	return s.queries
}

// GET /api/prohibitorum/consent?ticket=
func (s *Server) handleConsentContextHTTP(w http.ResponseWriter, r *http.Request) {
	sess := authn.SessionFromContext(r.Context())
	if sess == nil || sess.Data == nil {
		writeAuthErr(w, authn.ErrNoSession())
		return
	}
	ticket, ok, err := authn.PeekConsent(r.Context(), s.kvStore, r.URL.Query().Get("ticket"), sess.Data.AccountID)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	if !ok {
		writeAuthErr(w, authn.ErrInvalidConsentTicket())
		return
	}
	q := s.getOIDCConsentQueries()
	client, err := q.GetOIDCClient(r.Context(), ticket.ClientID)
	if err != nil {
		writeAuthErr(w, authn.ErrInvalidConsentTicket())
		return
	}
	out := contract.ConsentContext{
		Client: contract.ConsentClient{
			ClientID:    client.ClientID,
			DisplayName: client.DisplayName,
			LogoURI:     textOrEmpty(client.LogoUri),
			PolicyURI:   textOrEmpty(client.PolicyUri),
			TosURI:      textOrEmpty(client.TosUri),
		},
		Account: s.consentUser(sess.Account),
		Scopes:  ticket.Scopes,
	}
	granted, gerr := q.GetConsent(r.Context(), db.GetConsentParams{
		AccountID: sess.Data.AccountID, ClientID: ticket.ClientID,
	})
	if gerr != nil && !errors.Is(gerr, pgx.ErrNoRows) {
		writeAuthErr(w, gerr)
		return
	}
	// Report only the REQUESTED scopes the user has already granted (the
	// contract's "subset of Scopes"), in requested order, so the UI can mark the
	// genuinely new ones. nil on a first-time consent (ErrNoRows → granted nil).
	if len(granted) > 0 {
		have := make(map[string]struct{}, len(granted))
		for _, s := range granted {
			have[s] = struct{}{}
		}
		for _, s := range ticket.Scopes {
			if _, ok := have[s]; ok {
				out.AlreadyGranted = append(out.AlreadyGranted, s)
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// POST /api/prohibitorum/consent?return_to=...
func (s *Server) handleConsentDecisionHTTP(w http.ResponseWriter, r *http.Request) {
	sess := authn.SessionFromContext(r.Context())
	if sess == nil || sess.Data == nil {
		writeAuthErr(w, authn.ErrNoSession())
		return
	}
	var in contract.ConsentDecision
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	ticket, ok, err := authn.ConsumeConsent(r.Context(), s.kvStore, in.Ticket, sess.Data.AccountID)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	if !ok {
		writeAuthErr(w, authn.ErrInvalidConsentTicket())
		return
	}

	var redirect string
	switch in.Decision {
	case "approve":
		rt := validateReturnTo(r.URL.Query().Get("return_to"), s.config)
		granted, gerr := s.queries.GetConsent(r.Context(), db.GetConsentParams{
			AccountID: sess.Data.AccountID, ClientID: ticket.ClientID,
		})
		if gerr != nil && !errors.Is(gerr, pgx.ErrNoRows) {
			writeAuthErr(w, gerr)
			return
		}
		if uerr := s.queries.UpsertConsent(r.Context(), db.UpsertConsentParams{
			AccountID: sess.Data.AccountID, ClientID: ticket.ClientID, GrantedScopes: unionScopes(granted, ticket.Scopes),
		}); uerr != nil {
			writeAuthErr(w, uerr)
			return
		}
		redirect = rt
	case "deny":
		u, perr := url.Parse(ticket.RedirectURI)
		if perr != nil {
			writeAuthErr(w, authn.ErrBadRequest())
			return
		}
		q := u.Query()
		q.Set("error", "access_denied")
		if ticket.State != "" {
			q.Set("state", ticket.State)
		}
		// RFC 9207 §2: the iss parameter MUST be included in authorization
		// responses, INCLUDING error responses. The OP advertises
		// authorization_response_iss_parameter_supported, and the success +
		// other error paths set it (authorize.go) — the deny path must too, or a
		// strict mix-up-checking RP rejects the deny. (T2.2)
		q.Set("iss", s.config.OIDC.Issuer)
		u.RawQuery = q.Encode()
		redirect = u.String()
	default:
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(contract.ConsentResult{Redirect: redirect})
}

func textOrEmpty(t pgtype.Text) string {
	if t.Valid {
		return t.String
	}
	return ""
}

func unionScopes(a, b []string) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	for _, s := range append(append([]string{}, a...), b...) {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

