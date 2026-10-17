package oidc

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"prohibitorum/pkg/appaccess"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/kv"
)

const requireIdentityRedirect = "https://rp.example.com/after-verify"

type requireIdentityQueries struct {
	db.Querier
	client      db.OidcClient
	clientErr   error
	providers   map[string]bool // enabled slugs
	satisfied   bool
	satisfyErr  error
	satisfyArgs []db.HasConfirmedIdentityForProvidersParams
	accounts    map[int32]db.Account
}

func (q *requireIdentityQueries) GetOIDCClient(context.Context, string) (db.OidcClient, error) {
	return q.client, q.clientErr
}

func (q *requireIdentityQueries) GetUpstreamIDPBySlug(_ context.Context, slug string) (db.UpstreamIdp, error) {
	if !q.providers[slug] {
		return db.UpstreamIdp{}, pgx.ErrNoRows
	}
	return db.UpstreamIdp{Slug: slug}, nil
}

func (q *requireIdentityQueries) HasConfirmedIdentityForProviders(_ context.Context, arg db.HasConfirmedIdentityForProvidersParams) (bool, error) {
	q.satisfyArgs = append(q.satisfyArgs, arg)
	return q.satisfied, q.satisfyErr
}

func (q *requireIdentityQueries) GetAccountByID(_ context.Context, id int32) (db.Account, error) {
	account, ok := q.accounts[id]
	if !ok {
		return db.Account{}, pgx.ErrNoRows
	}
	return account, nil
}

type requireIdentityHarness struct {
	p      *Provider
	q      *requireIdentityQueries
	audit  *recordingAudit
	access *fakeOIDCAuthorizer
}

func newRequireIdentityHarness(t *testing.T) *requireIdentityHarness {
	t.Helper()
	row, _ := testSigningKeyRow(t)
	q := &requireIdentityQueries{
		client: db.OidcClient{
			ClientID: "rp-1", DisplayName: "Team wiki",
			RedirectUris: []string{"https://rp.example.com/callback", requireIdentityRedirect},
		},
		providers: map[string]bool{"vrchat": true, "steam": true},
		accounts:  map[int32]db.Account{42: {ID: 42}, 43: {ID: 43}},
	}
	access := &fakeOIDCAuthorizer{}
	ra := &recordingAudit{}
	p := &Provider{
		cfg:     &configx.Config{OIDC: configx.OIDCConfig{Issuer: testIssuer}},
		queries: q,
		kv:      kv.NewMemoryStore(),
		deks:    oidcTestDEKs,
		audit:   ra,
		rl:      authn.NewRateLimiter(),
		keys:    newKeyCache(&fakeSigningKeyQueries{rows: []db.SigningKey{row}}, oidcTestDEKs),
		access:  access,
	}
	return &requireIdentityHarness{p: p, q: q, audit: ra, access: access}
}

func requireIdentityParams() url.Values {
	return url.Values{
		"client_id":    {"rp-1"},
		"redirect_uri": {requireIdentityRedirect},
		"providers":    {"vrchat", "steam"},
	}
}

func (h *requireIdentityHarness) serve(v url.Values, signedIn bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/prohibitorum/require-identity?"+v.Encode(), nil)
	if signedIn {
		req = req.WithContext(authn.WithSession(req.Context(), &authn.Session{
			Account: &db.Account{ID: 42},
			Data:    &authn.SessionData{SessionID: "sid-1", AccountID: 42},
		}))
	}
	rec := httptest.NewRecorder()
	h.p.HandleRequireIdentity(rec, req)
	return rec
}

func (h *requireIdentityHarness) hint(t *testing.T, aud any, accountID int32) string {
	t.Helper()
	token, err := h.p.signJWT(context.Background(), map[string]any{
		"iss": testIssuer, "sub": "subject", "aud": aud,
		internalAccountIDClaim: accountID,
		"exp":                  time.Now().Add(-time.Hour).Unix(),
	}, "JWT")
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func location(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 (body %s)", rec.Code, rec.Body)
	}
	return rec.Header().Get("Location")
}

// Every refusal ends on /error with its code and never on the application's
// redirect_uri.
func TestRequireIdentityRefusalsStayOnTheErrorPage(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*requireIdentityHarness, url.Values)
		code   string
	}{
		{"unknown client", func(h *requireIdentityHarness, _ url.Values) { h.q.clientErr = pgx.ErrNoRows }, errCodeRequireIdentityInvalidClient},
		{"client read fails", func(h *requireIdentityHarness, _ url.Values) { h.q.clientErr = errors.New("db down") }, errCodeServerError},
		{"redirect_uri missing", func(_ *requireIdentityHarness, v url.Values) { v.Del("redirect_uri") }, errCodeRequireIdentityInvalidRedirectURI},
		{"redirect_uri not registered", func(_ *requireIdentityHarness, v url.Values) { v.Set("redirect_uri", requireIdentityRedirect+"/x") }, errCodeRequireIdentityInvalidRedirectURI},
		{"no providers", func(_ *requireIdentityHarness, v url.Values) { v.Del("providers") }, errCodeRequireIdentityInvalidProviders},
		{"too many providers", func(_ *requireIdentityHarness, v url.Values) {
			v.Del("providers")
			for i := 0; i <= maxRequireIdentityProviders; i++ {
				v.Add("providers", "p"+string(rune('a'+i)))
			}
		}, errCodeRequireIdentityInvalidProviders},
		{"unknown or disabled provider", func(_ *requireIdentityHarness, v url.Values) { v.Add("providers", "gone") }, errCodeRequireIdentityInvalidProviders},
		{"hint for another client", func(h *requireIdentityHarness, v url.Values) { v.Set("id_token_hint", h.hint(t, "other-rp", 42)) }, errCodeRequireIdentityInvalidIDToken},
		{"hint is not a token", func(_ *requireIdentityHarness, v url.Values) { v.Set("id_token_hint", "garbage") }, errCodeRequireIdentityInvalidIDToken},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newRequireIdentityHarness(t)
			v := requireIdentityParams()
			tc.mutate(h, v)
			loc := location(t, h.serve(v, true))
			if !strings.HasPrefix(loc, "/error?error="+tc.code+"&") {
				t.Fatalf("Location = %q, want /error with %s", loc, tc.code)
			}
			if strings.Contains(loc, "rp.example.com") {
				t.Fatalf("refusal leaked toward the application: %q", loc)
			}
		})
	}
}

func TestRequireIdentitySignedOutGoesToLoginAndBack(t *testing.T) {
	h := newRequireIdentityHarness(t)
	v := requireIdentityParams()
	loc := location(t, h.serve(v, false))
	want := testIssuer + "/login?return_to=" + url.QueryEscape(testIssuer+"/api/prohibitorum/require-identity?"+v.Encode())
	if loc != want {
		t.Fatalf("Location = %q, want %q", loc, want)
	}
}

func TestRequireIdentitySatisfiedReturnsToTheApplication(t *testing.T) {
	h := newRequireIdentityHarness(t)
	h.q.satisfied = true
	v := requireIdentityParams()
	v.Add("providers", "vrchat") // duplicates collapse
	if loc := location(t, h.serve(v, true)); loc != requireIdentityRedirect {
		t.Fatalf("Location = %q, want the bare redirect_uri", loc)
	}
	if got := h.q.satisfyArgs; len(got) != 1 || got[0].AccountID != 42 || !slices.Equal(got[0].Slugs, []string{"vrchat", "steam"}) {
		t.Fatalf("satisfaction query = %+v", got)
	}
}

func TestRequireIdentityUnsatisfiedOpensTheTicketPage(t *testing.T) {
	h := newRequireIdentityHarness(t)
	v := requireIdentityParams()
	// An expired hint for this client and this account is accepted.
	v.Set("id_token_hint", h.hint(t, []any{"other", "rp-1"}, 42))
	loc := location(t, h.serve(v, true))
	id, ok := strings.CutPrefix(loc, testIssuer+"/require-identity/")
	if !ok || id == "" {
		t.Fatalf("Location = %q, want the require-identity page", loc)
	}
	ticket, err := h.p.LoadRequireIdentityTicket(context.Background(), id, 42, "sid-1")
	if err != nil {
		t.Fatal(err)
	}
	if ticket.ClientID != "rp-1" || ticket.RedirectURI != requireIdentityRedirect || ticket.Mismatch ||
		!slices.Equal(ticket.Providers, []string{"vrchat", "steam"}) || !strings.HasPrefix(ticket.Entry, testIssuer+"/api/prohibitorum/require-identity?") {
		t.Fatalf("ticket = %+v", ticket)
	}
	for _, other := range []struct {
		account int32
		session string
	}{{43, "sid-1"}, {42, "sid-2"}} {
		if _, err := h.p.LoadRequireIdentityTicket(context.Background(), id, other.account, other.session); !errors.Is(err, ErrRequireIdentityTicketNotFound) {
			t.Fatalf("ticket readable by account %d session %s: %v", other.account, other.session, err)
		}
	}
	if _, err := h.p.LoadRequireIdentityTicket(context.Background(), "missing", 42, "sid-1"); !errors.Is(err, ErrRequireIdentityTicketNotFound) {
		t.Fatalf("missing ticket: %v", err)
	}
}

func TestRequireIdentityHintForAnotherAccountIsAMismatch(t *testing.T) {
	for name, accountID := range map[string]int32{"another account": 43, "an account that no longer exists": 999} {
		t.Run(name, func(t *testing.T) {
			h := newRequireIdentityHarness(t)
			h.q.satisfied = true
			v := requireIdentityParams()
			v.Set("id_token_hint", h.hint(t, "rp-1", accountID))
			id, ok := strings.CutPrefix(location(t, h.serve(v, true)), testIssuer+"/require-identity/")
			if !ok {
				t.Fatal("a mismatch must open the page, not return to the application")
			}
			ticket, err := h.p.LoadRequireIdentityTicket(context.Background(), id, 42, "sid-1")
			if err != nil || !ticket.Mismatch {
				t.Fatalf("ticket = %+v, err = %v; want a mismatch", ticket, err)
			}
			if len(h.q.satisfyArgs) != 0 {
				t.Fatal("a mismatch must not check or act on this account's identities")
			}
		})
	}
}

func TestRequireIdentityAccessDeniedUsesTheDeniedPage(t *testing.T) {
	h := newRequireIdentityHarness(t)
	h.q.satisfied = true
	h.access.byClient = map[string]appaccess.Decision{"rp-1": {Source: appaccess.SourceManualDeny}}
	loc := location(t, h.serve(requireIdentityParams(), true))
	if loc != testIssuer+"/error?reason=app_access_denied&app="+url.QueryEscape("Team wiki") {
		t.Fatalf("Location = %q", loc)
	}
	if len(h.audit.records) != 1 || h.audit.records[0].Event != "access_denied" {
		t.Fatalf("audit = %+v", h.audit.records)
	}
}

func TestAudienceIncludes(t *testing.T) {
	for _, tc := range []struct {
		aud  any
		want bool
	}{
		{"rp-1", true}, {"rp-2", false}, {[]any{"x", "rp-1"}, true}, {[]any{"x"}, false}, {nil, false}, {42, false},
	} {
		if got := audienceIncludes(tc.aud, "rp-1"); got != tc.want {
			t.Errorf("audienceIncludes(%v) = %v, want %v", tc.aud, got, tc.want)
		}
	}
}

// ttlRecordingKV notes the lifetime each SetEx asked for.
type ttlRecordingKV struct {
	kv.Store
	ttls map[string]time.Duration
}

func (s *ttlRecordingKV) SetEx(ctx context.Context, key, value string, ttl time.Duration) error {
	s.ttls[key] = ttl
	return s.Store.SetEx(ctx, key, value, ttl)
}

func TestRequireIdentityTicketExpires(t *testing.T) {
	h := newRequireIdentityHarness(t)
	store := &ttlRecordingKV{Store: kv.NewMemoryStore(), ttls: map[string]time.Duration{}}
	h.p.kv = store
	id, ok := strings.CutPrefix(location(t, h.serve(requireIdentityParams(), true)), testIssuer+"/require-identity/")
	if !ok {
		t.Fatal("expected the require-identity page")
	}
	if got := store.ttls[requireIdentityKVPrefix+id]; got != 30*time.Minute {
		t.Fatalf("ticket lifetime = %v, want 30m", got)
	}
}
