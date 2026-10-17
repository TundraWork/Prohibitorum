package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
	oidc "prohibitorum/pkg/protocol/oidc"
)

type fakeRequireIdentityQueries struct {
	clients     map[string]db.OidcClient
	providers   map[string]string // enabled slug → display name
	icons       map[string]string // kind/id → etag
	satisfied   bool
	satisfyArgs []db.HasConfirmedIdentityForProvidersParams
}

func (f *fakeRequireIdentityQueries) GetOIDCClient(_ context.Context, id string) (db.OidcClient, error) {
	c, ok := f.clients[id]
	if !ok {
		return db.OidcClient{}, pgx.ErrNoRows
	}
	return c, nil
}

func (f *fakeRequireIdentityQueries) GetUpstreamIDPBySlug(_ context.Context, slug string) (db.UpstreamIdp, error) {
	name, ok := f.providers[slug]
	if !ok {
		return db.UpstreamIdp{}, pgx.ErrNoRows
	}
	return db.UpstreamIdp{Slug: slug, DisplayName: name}, nil
}

func (f *fakeRequireIdentityQueries) HasConfirmedIdentityForProviders(_ context.Context, arg db.HasConfirmedIdentityForProvidersParams) (bool, error) {
	f.satisfyArgs = append(f.satisfyArgs, arg)
	return f.satisfied, nil
}

func (f *fakeRequireIdentityQueries) GetEntityIconEtag(_ context.Context, arg db.GetEntityIconEtagParams) (string, error) {
	etag, ok := f.icons[arg.OwnerKind+"/"+arg.OwnerID]
	if !ok {
		return "", pgx.ErrNoRows
	}
	return etag, nil
}

type fakeRequireIdentityTickets map[string]oidc.RequireIdentityTicket

func (f fakeRequireIdentityTickets) LoadRequireIdentityTicket(_ context.Context, id string, accountID int32, sessionID string) (oidc.RequireIdentityTicket, error) {
	ticket, ok := f[id]
	if !ok || ticket.AccountID != accountID || ticket.SessionID != sessionID {
		return oidc.RequireIdentityTicket{}, oidc.ErrRequireIdentityTicketNotFound
	}
	return ticket, nil
}

func requireIdentityServer(q *fakeRequireIdentityQueries, tickets fakeRequireIdentityTickets) *Server {
	return &Server{requireIdentityOverride: q, requireIdentityTicketsOverride: tickets}
}

func readRequireIdentity(t *testing.T, s *Server, ticket string, sess *authn.Session) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/prohibitorum/require-identity/"+ticket, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("ticket", ticket)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	if sess != nil {
		ctx = authn.WithSession(ctx, sess)
	}
	rec := httptest.NewRecorder()
	s.handleRequireIdentityRequestHTTP(rec, req.WithContext(ctx))
	return rec
}

func requireIdentityFixture() (*fakeRequireIdentityQueries, fakeRequireIdentityTickets, *authn.Session) {
	q := &fakeRequireIdentityQueries{
		clients:   map[string]db.OidcClient{"rp-1": {ClientID: "rp-1", DisplayName: "Team wiki", RedirectUris: []string{"https://rp.example/after"}}},
		providers: map[string]string{"vrchat": "VRChat", "steam": "Steam"},
		icons:     map[string]string{"upstream_idp/vrchat": "etag-v"},
	}
	tickets := fakeRequireIdentityTickets{"t-1": {
		ClientID: "rp-1", RedirectURI: "https://rp.example/after", Providers: []string{"vrchat", "gone", "steam"},
		AccountID: 42, SessionID: "sid-1", Entry: "https://idp.example/api/prohibitorum/require-identity?client_id=rp-1",
	}}
	return q, tickets, &authn.Session{Account: &db.Account{ID: 42}, Data: &authn.SessionData{AccountID: 42, SessionID: "sid-1"}}
}

func TestRequireIdentityRequestRead(t *testing.T) {
	q, tickets, sess := requireIdentityFixture()
	rec := readRequireIdentity(t, requireIdentityServer(q, tickets), "t-1", sess)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	var out contract.RequireIdentityRequest
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Client.DisplayName != "Team wiki" || out.RedirectURI != "https://rp.example/after" || out.Mismatch || out.Satisfied ||
		out.EntryURL != tickets["t-1"].Entry {
		t.Fatalf("request = %+v", out)
	}
	// A provider turned off since the request is left out; order follows the request.
	if len(out.Providers) != 2 || out.Providers[0].Slug != "vrchat" || out.Providers[0].DisplayName != "VRChat" ||
		out.Providers[0].IconURL == "" || out.Providers[1].Slug != "steam" || out.Providers[1].IconURL != "" {
		t.Fatalf("providers = %+v", out.Providers)
	}
	if len(q.satisfyArgs) != 1 || q.satisfyArgs[0].AccountID != 42 {
		t.Fatalf("satisfaction query = %+v", q.satisfyArgs)
	}
}

func TestRequireIdentityRequestSatisfiedAndMismatch(t *testing.T) {
	q, tickets, sess := requireIdentityFixture()
	q.satisfied = true
	var out contract.RequireIdentityRequest
	_ = json.Unmarshal(readRequireIdentity(t, requireIdentityServer(q, tickets), "t-1", sess).Body.Bytes(), &out)
	if !out.Satisfied {
		t.Fatal("a linked provider must report satisfied")
	}

	mismatch := tickets["t-1"]
	mismatch.Mismatch = true
	tickets["t-1"] = mismatch
	q.satisfyArgs = nil
	_ = json.Unmarshal(readRequireIdentity(t, requireIdentityServer(q, tickets), "t-1", sess).Body.Bytes(), &out)
	if !out.Mismatch || out.Satisfied || len(q.satisfyArgs) != 0 {
		t.Fatalf("mismatch = %+v, queries = %v: a mismatch never reports or checks this account", out, q.satisfyArgs)
	}
}

func TestRequireIdentityRequestGone(t *testing.T) {
	q, tickets, sess := requireIdentityFixture()
	other := &authn.Session{Account: &db.Account{ID: 42}, Data: &authn.SessionData{AccountID: 42, SessionID: "sid-2"}}
	for name, tc := range map[string]struct {
		ticket string
		sess   *authn.Session
		setup  func()
	}{
		"unknown ticket":   {ticket: "nope", sess: sess},
		"another session":  {ticket: "t-1", sess: other},
		"application gone": {ticket: "t-1", sess: sess, setup: func() { delete(q.clients, "rp-1") }},
	} {
		if tc.setup != nil {
			tc.setup()
		}
		rec := readRequireIdentity(t, requireIdentityServer(q, tickets), tc.ticket, tc.sess)
		if rec.Code != http.StatusGone || !json.Valid(rec.Body.Bytes()) {
			t.Errorf("%s: status = %d body %s, want 410", name, rec.Code, rec.Body)
		}
	}
	if rec := readRequireIdentity(t, requireIdentityServer(q, tickets), "t-1", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("no session: status = %d, want 401", rec.Code)
	}
}

// The page never offers to return to an address the application has since
// unregistered.
func TestRequireIdentityRequestRedirectURIUnregistered(t *testing.T) {
	q, tickets, sess := requireIdentityFixture()
	c := q.clients["rp-1"]
	c.RedirectUris = []string{"https://rp.example/other"}
	q.clients["rp-1"] = c
	if rec := readRequireIdentity(t, requireIdentityServer(q, tickets), "t-1", sess); rec.Code != http.StatusGone {
		t.Fatalf("status = %d body %s, want 410", rec.Code, rec.Body)
	}
}
