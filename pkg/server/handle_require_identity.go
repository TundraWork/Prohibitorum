package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
	oidc "prohibitorum/pkg/protocol/oidc"
)

// requireIdentityQueries is what the require-identity page's read needs.
type requireIdentityQueries interface {
	GetOIDCClient(ctx context.Context, clientID string) (db.OidcClient, error)
	GetUpstreamIDPBySlug(ctx context.Context, slug string) (db.UpstreamIdp, error)
	HasConfirmedIdentityForProviders(ctx context.Context, arg db.HasConfirmedIdentityForProvidersParams) (bool, error)
	GetEntityIconEtag(ctx context.Context, arg db.GetEntityIconEtagParams) (string, error)
}

type requireIdentityTickets interface {
	LoadRequireIdentityTicket(ctx context.Context, id string, accountID int32, sessionID string) (oidc.RequireIdentityTicket, error)
}

func (s *Server) requireIdentityDeps() (requireIdentityQueries, requireIdentityTickets) {
	var q requireIdentityQueries = s.queries
	if s.requireIdentityOverride != nil {
		q = s.requireIdentityOverride
	}
	var tickets requireIdentityTickets = s.oidcOP
	if s.requireIdentityTicketsOverride != nil {
		tickets = s.requireIdentityTicketsOverride
	}
	return q, tickets
}

// GET /api/prohibitorum/require-identity/{ticket} — the request an application
// sent this account to the require-identity page with. Only the session that
// opened it can read it; anyone else, and an expired ticket, is told it is gone.
func (s *Server) handleRequireIdentityRequestHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sess := authn.SessionFromContext(ctx)
	if sess == nil || sess.Data == nil {
		writeAuthErr(w, authn.ErrNoSession())
		return
	}
	q, tickets := s.requireIdentityDeps()
	ticket, err := tickets.LoadRequireIdentityTicket(ctx, chi.URLParam(r, "ticket"), sess.Data.AccountID, sess.Data.SessionID)
	if errors.Is(err, oidc.ErrRequireIdentityTicketNotFound) {
		writeAuthErr(w, authn.ErrRequireIdentityTicketExpired())
		return
	}
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	// The application may have been turned off, or the address it asked to
	// return to unregistered, since: its request is gone too, so the page never
	// sends the browser somewhere the application no longer accepts.
	client, err := q.GetOIDCClient(ctx, ticket.ClientID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeAuthErr(w, authn.ErrRequireIdentityTicketExpired())
		return
	}
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	if !slices.Contains(client.RedirectUris, ticket.RedirectURI) {
		writeAuthErr(w, authn.ErrRequireIdentityTicketExpired())
		return
	}

	out := contract.RequireIdentityRequest{
		Client: contract.RequireIdentityClient{
			DisplayName: client.DisplayName,
			IconURL:     uploadedIconOr(ctx, q, "oidc_client", client.ClientID, textOrEmpty(client.LogoUri)),
		},
		RedirectURI: ticket.RedirectURI,
		EntryURL:    ticket.Entry,
		Mismatch:    ticket.Mismatch,
		Providers:   []contract.RequireIdentityProvider{},
	}
	for _, slug := range ticket.Providers {
		provider, err := q.GetUpstreamIDPBySlug(ctx, slug)
		if errors.Is(err, pgx.ErrNoRows) {
			continue // turned off or deleted since the request was made
		}
		if err != nil {
			writeAuthErr(w, err)
			return
		}
		out.Providers = append(out.Providers, contract.RequireIdentityProvider{
			Slug: provider.Slug, DisplayName: provider.DisplayName,
			IconURL: uploadedIconOr(ctx, q, "upstream_idp", provider.Slug, ""),
		})
	}
	if !ticket.Mismatch {
		out.Satisfied, err = q.HasConfirmedIdentityForProviders(ctx, db.HasConfirmedIdentityForProvidersParams{
			AccountID: sess.Data.AccountID, Slugs: ticket.Providers,
		})
		if err != nil {
			writeAuthErr(w, err)
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(out)
}

// uploadedIconOr is the uploaded icon's URL, or fallback when there is
// none. A failed lookup only costs the icon.
// entityIconEtags reads an entity's uploaded icon version.
type entityIconEtags interface {
	GetEntityIconEtag(ctx context.Context, arg db.GetEntityIconEtagParams) (string, error)
}

// uploadedIconOr is an application's uploaded icon URL, or fallback (its
// registered logo_uri) when it has none: the order every public page draws
// an application's icon in.
func uploadedIconOr(ctx context.Context, q entityIconEtags, kind, id, fallback string) string {
	etag, err := q.GetEntityIconEtag(ctx, db.GetEntityIconEtagParams{OwnerKind: kind, OwnerID: id})
	if err != nil || etag == "" {
		return fallback
	}
	if u := entityIconURLPtr(kind, id, etag); u != nil {
		return *u
	}
	return fallback
}
