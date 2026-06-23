package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
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

	// The ticket is consumed ONLY on decline; on approve it is merely peeked
	// (validated, not popped) and the SAML resume endpoint records the ack and
	// issues the assertion — that endpoint owns the single use of the nonce.
	var redirect string
	switch in.Decision {
	case "approve":
		// Validate the ticket still belongs to this session, then hand off to the
		// SAML resume endpoint, which records the ack and issues the assertion.
		if _, ok, perr := authn.PeekSAMLConsent(r.Context(), s.kvStore, in.Ticket, sess.Data.AccountID); perr != nil {
			writeAuthErr(w, perr)
			return
		} else if !ok {
			writeAuthErr(w, authn.ErrInvalidConsentTicket())
			return
		}
		redirect = "/saml/sso/resume?ticket=" + url.QueryEscape(in.Ticket)
	case "decline":
		// The user stays signed in to the IdP; they just don't enter the app.
		if _, ok, cerr := authn.ConsumeSAMLConsent(r.Context(), s.kvStore, in.Ticket, sess.Data.AccountID); cerr != nil {
			writeAuthErr(w, cerr)
			return
		} else if !ok {
			writeAuthErr(w, authn.ErrInvalidConsentTicket())
			return
		}
		redirect = "/"
	default:
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(contract.ConsentResult{Redirect: redirect})
}
