package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
)

// samlConsentQueries is the narrow DB surface the SAML consent handlers need.
// Declared here so tests can stub it without constructing *db.Queries.
// Production wiring leaves samlConsentOverride nil; handlers fall back to s.queries.
type samlConsentQueries interface {
	GetEntityIconEtag(ctx context.Context, arg db.GetEntityIconEtagParams) (string, error)
	UpsertSAMLConsent(ctx context.Context, arg db.UpsertSAMLConsentParams) error
}

func (s *Server) getSAMLConsentQueries() samlConsentQueries {
	if s.samlConsentOverride != nil {
		return s.samlConsentOverride
	}
	return s.queries
}

// GET /api/prohibitorum/saml-consent?ticket=
func (s *Server) handleSAMLConsentContextHTTP(w http.ResponseWriter, r *http.Request) {
	sess := authn.SessionFromContext(r.Context())
	if sess == nil || sess.Data == nil {
		writeAuthErr(w, authn.ErrNoSession())
		return
	}
	ticket, ok, err := authn.PeekSAMLConsent(r.Context(), s.kvStore, r.URL.Query().Get("ticket"), sess.Data.AccountID)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	if !ok {
		writeAuthErr(w, authn.ErrInvalidConsentTicket())
		return
	}
	q := s.getSAMLConsentQueries()
	id := strconv.FormatInt(ticket.SPID, 10)
	var logo string
	if etag, e := q.GetEntityIconEtag(r.Context(), db.GetEntityIconEtagParams{OwnerKind: "saml_sp", OwnerID: id}); e == nil {
		if u := entityIconURLPtr("saml_sp", id, etag); u != nil {
			logo = *u
		}
	}
	out := contract.SAMLConsentContext{
		SP:         contract.SAMLConsentSP{ID: id, DisplayName: ticket.DisplayName, LogoURI: logo},
		Account:    contract.ConsentUser{DisplayName: sess.Account.DisplayName},
		Attributes: ticket.Attributes,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// POST /api/prohibitorum/saml-consent
func (s *Server) handleSAMLConsentDecisionHTTP(w http.ResponseWriter, r *http.Request) {
	sess := authn.SessionFromContext(r.Context())
	if sess == nil || sess.Data == nil {
		writeAuthErr(w, authn.ErrNoSession())
		return
	}
	var in contract.SAMLConsentDecision
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	ticket, ok, err := authn.ConsumeSAMLConsent(r.Context(), s.kvStore, in.Ticket, sess.Data.AccountID)
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
		if uerr := s.getSAMLConsentQueries().UpsertSAMLConsent(r.Context(), db.UpsertSAMLConsentParams{
			AccountID: sess.Data.AccountID, SpID: ticket.SPID,
		}); uerr != nil {
			writeAuthErr(w, uerr)
			return
		}
		// ReturnTo is server-minted (our own origin, exact SSO URL) — trusted.
		redirect = ticket.ReturnTo
	case "decline":
		// The user stays signed in to the IdP; they just don't enter the app.
		redirect = "/"
	default:
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(contract.ConsentResult{Redirect: redirect})
}
