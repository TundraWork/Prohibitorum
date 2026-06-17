package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
)

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
	client, err := s.queries.GetOIDCClient(r.Context(), ticket.ClientID)
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
		Account: contract.ConsentUser{DisplayName: sess.Account.DisplayName},
		Scopes:  ticket.Scopes,
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

