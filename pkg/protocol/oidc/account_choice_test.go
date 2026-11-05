package oidc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/appaccess"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/kv"
	"prohibitorum/pkg/session"
)

// multiQueries backs a browser with several accounts signed in: the session
// store writes sessions here and authorize reads them back by ID.
type multiQueries struct {
	db.Querier
	client   db.OidcClient
	accounts map[int32]db.Account
	sessions map[string]db.Session
	// emails maps account → address → verified.
	emails  map[int32]map[string]bool
	granted map[int32][]string
	identOK map[int32]bool
}

func (q *multiQueries) GetOIDCClient(context.Context, string) (db.OidcClient, error) {
	return q.client, nil
}

func (q *multiQueries) GetAccountByID(_ context.Context, id int32) (db.Account, error) {
	a, ok := q.accounts[id]
	if !ok {
		return db.Account{}, pgx.ErrNoRows
	}
	return a, nil
}

func (q *multiQueries) GetSession(_ context.Context, id string) (db.Session, error) {
	s, ok := q.sessions[id]
	if !ok {
		return db.Session{}, pgx.ErrNoRows
	}
	return s, nil
}

func (q *multiQueries) GetConsent(_ context.Context, arg db.GetConsentParams) ([]string, error) {
	g, ok := q.granted[arg.AccountID]
	if !ok {
		return nil, pgx.ErrNoRows
	}
	return g, nil
}

func (q *multiQueries) GetAccountEmailByAddress(_ context.Context, arg db.GetAccountEmailByAddressParams) (db.GetAccountEmailByAddressRow, error) {
	for addr, verified := range q.emails[arg.AccountID] {
		if strings.EqualFold(addr, arg.Lower) {
			row := db.GetAccountEmailByAddressRow{AccountID: arg.AccountID, Address: addr}
			if verified {
				row.VerifiedAt = pgtype.Timestamptz{Time: time.Now(), Valid: true}
			}
			return row, nil
		}
	}
	return db.GetAccountEmailByAddressRow{}, pgx.ErrNoRows
}

func (q *multiQueries) GetUpstreamIDPBySlug(_ context.Context, slug string) (db.UpstreamIdp, error) {
	return db.UpstreamIdp{Slug: slug}, nil
}

func (q *multiQueries) HasConfirmedIdentityForProviders(_ context.Context, arg db.HasConfirmedIdentityForProvidersParams) (bool, error) {
	return q.identOK[arg.AccountID], nil
}

func (q *multiQueries) InsertSession(_ context.Context, arg db.InsertSessionParams) (db.Session, error) {
	row := db.Session{ID: arg.ID, AccountID: arg.AccountID, AuthTime: arg.AuthTime, Amr: arg.Amr}
	q.sessions[arg.ID] = row
	return row, nil
}

func (q *multiQueries) RevokeSession(context.Context, string) error             { return nil }
func (q *multiQueries) RevokeAllSessionsByAccount(context.Context, int32) error { return nil }

// multiHarness is a Provider behind the real session middleware, with a
// browser cookie naming the accounts signIn added.
type multiHarness struct {
	p       *Provider
	q       *multiQueries
	cfg     *configx.Config
	store   *session.SessionStore
	m       *session.Manager
	access  *fakeOIDCAuthorizer
	refs    *session.AccountRefs
	entries []session.Entry
	sids    map[int32]string
}

const (
	alice int32 = 42
	bob   int32 = 43
	carol int32 = 44
	// dave has an account but is never signed in on the test browser.
	dave int32 = 45
)

func newMultiHarness(t *testing.T) *multiHarness {
	t.Helper()
	row, _ := testSigningKeyRow(t)
	cfg := &configx.Config{OIDC: configx.OIDCConfig{Issuer: testIssuer}}
	q := &multiQueries{
		client: validClient(),
		accounts: map[int32]db.Account{
			alice: {ID: alice, Username: "alice"},
			bob:   {ID: bob, Username: "bob"},
			carol: {ID: carol, Username: "carol", Role: "admin"},
			dave:  {ID: dave, Username: "dave"},
		},
		sessions: map[string]db.Session{},
		emails:   map[int32]map[string]bool{},
		granted:  map[int32][]string{},
		identOK:  map[int32]bool{},
	}
	q.client.RedirectUris = append(q.client.RedirectUris, requireIdentityRedirect)
	store := kv.NewMemoryStore()
	t.Cleanup(func() { _ = store.Close() })
	sessions := session.NewSessionStore(store, q, time.Hour, time.Hour)
	access := &fakeOIDCAuthorizer{}
	refs := session.NewAccountRefs([]byte("oidc-account-choice-test-key"))
	p := &Provider{
		cfg:      cfg,
		queries:  q,
		kv:       store,
		deks:     oidcTestDEKs,
		sessions: sessions,
		audit:    &recordingAudit{},
		rl:       authn.NewRateLimiter(),
		keys:     newKeyCache(&fakeSigningKeyQueries{rows: []db.SigningKey{row}}, oidcTestDEKs),
		access:   access,
	}
	p.SetAccountRefs(refs)
	return &multiHarness{
		p: p, q: q, cfg: cfg, store: sessions, access: access, refs: refs,
		m:    session.NewManager(cfg, q, sessions, nil),
		sids: map[int32]string{},
	}
}

// signIn issues a session for id and adds (or replaces) its cookie entry,
// keeping the account's place.
func (h *multiHarness) signIn(t *testing.T, id int32) {
	t.Helper()
	token, data, err := h.store.Issue(context.Background(), id, "", "", []string{"hwk"}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	h.sids[id] = data.SessionID
	for i, e := range h.entries {
		if e.AccountID == id {
			h.entries[i].Token = token
			return
		}
	}
	h.entries = append(h.entries, session.Entry{Seq: int64(len(h.entries) + 1), AccountID: id, Token: token})
}

func (h *multiHarness) serve(t *testing.T, target string, handler http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if len(h.entries) > 0 {
		req.AddCookie(&http.Cookie{Name: session.TempCookieName(h.cfg), Value: session.EncodeCookie(h.entries)})
	}
	rec := httptest.NewRecorder()
	h.m.LoadSession()(handler).ServeHTTP(rec, req)
	return rec
}

func (h *multiHarness) authorize(t *testing.T, v url.Values) *httptest.ResponseRecorder {
	t.Helper()
	return h.serve(t, "/oauth/authorize?"+v.Encode(), h.p.HandleAuthorize)
}

// u is the short reference of id among the accounts signed in.
func (h *multiHarness) u(id int32) string {
	ids := make([]int32, len(h.entries))
	for i, e := range h.entries {
		ids[i] = e.AccountID
	}
	return h.refs.Shorts(ids)[id]
}

// choose stores a chooser ticket that already chose id for (kind, app).
func (h *multiHarness) choose(t *testing.T, kind, app string, id int32) string {
	t.Helper()
	nonce, err := authn.CreateAccountChooser(context.Background(), h.p.kv, authn.AccountChooserTicket{
		Kind: kind, App: app, ResumeURL: "/oauth/authorize", AccountID: id,
	})
	if err != nil {
		t.Fatal(err)
	}
	return nonce
}

// hint signs an ID token for accountID with session sid, expired an hour ago.
func (h *multiHarness) hint(t *testing.T, accountID int32, sid string) string {
	t.Helper()
	claims := map[string]any{
		"iss": testIssuer, "sub": "subject", "aud": "rp-1",
		internalAccountIDClaim: accountID,
		"exp":                  time.Now().Add(-time.Hour).Unix(),
	}
	if sid != "" {
		claims["sid"] = sid
	}
	token, err := h.p.signJWT(context.Background(), claims, "JWT")
	if err != nil {
		t.Fatal(err)
	}
	return token
}

// codeFor asserts the response is a code for accountID minted on that
// account's session, and returns it.
func (h *multiHarness) codeFor(t *testing.T, rec *httptest.ResponseRecorder, accountID int32) *authCode {
	t.Helper()
	if rec.Code != http.StatusFound {
		t.Fatalf("want 302, got %d (%s)", rec.Code, rec.Body.String())
	}
	loc, _ := url.Parse(rec.Header().Get("Location"))
	code := loc.Query().Get("code")
	if !strings.HasPrefix(loc.String(), "https://rp.example.com/callback") || code == "" {
		t.Fatalf("want a code at the RP, got Location=%q", loc)
	}
	ac, err := consumeCode(context.Background(), h.p.kv, code)
	if err != nil {
		t.Fatal(err)
	}
	if ac.AccountID != accountID || ac.SessionID != h.sids[accountID] {
		t.Fatalf("code for account %d session %q, want account %d session %q", ac.AccountID, ac.SessionID, accountID, h.sids[accountID])
	}
	return ac
}

// chooserTicket asserts the response opens the chooser page and returns its
// ticket.
func (h *multiHarness) chooserTicket(t *testing.T, rec *httptest.ResponseRecorder) *authn.AccountChooserTicket {
	t.Helper()
	loc := rec.Header().Get("Location")
	if rec.Code != http.StatusFound || !strings.HasPrefix(loc, testIssuer+chooserPagePath+"?ticket=") {
		t.Fatalf("want the chooser page, got %d Location=%q", rec.Code, loc)
	}
	u, _ := url.Parse(loc)
	ticket, err := authn.GetAccountChooser(context.Background(), h.p.kv, u.Query().Get("ticket"))
	if err != nil || ticket == nil {
		t.Fatalf("chooser ticket: %v %v", ticket, err)
	}
	return ticket
}

func locationQuery(t *testing.T, rec *httptest.ResponseRecorder) (string, url.Values) {
	t.Helper()
	if rec.Code != http.StatusFound {
		t.Fatalf("want 302, got %d (%s)", rec.Code, rec.Body.String())
	}
	u, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	return u.Scheme + "://" + u.Host + u.Path, u.Query()
}

// Step 1: a valid account_choice picks its account even though the default
// account is another one.
func TestAuthorizeAccount_ChoiceWins(t *testing.T) {
	h := newMultiHarness(t)
	h.signIn(t, alice)
	h.signIn(t, bob)
	v := baseParams()
	v.Set("account_choice", h.choose(t, authn.ChooserKindOIDC, "rp-1", bob))
	h.codeFor(t, h.authorize(t, v), bob)
}

// A choice made for another client, for another flow, for an account not
// signed in here, or not made yet is ignored; resolution goes on.
func TestAuthorizeAccount_ChoiceIgnoredWhenNotForThisRequest(t *testing.T) {
	h := newMultiHarness(t)
	h.signIn(t, alice)
	h.signIn(t, bob)
	for name, nonce := range map[string]string{
		"other client":      h.choose(t, authn.ChooserKindOIDC, "rp-2", bob),
		"other flow":        h.choose(t, authn.ChooserKindSAML, "rp-1", bob),
		"account not here":  h.choose(t, authn.ChooserKindOIDC, "rp-1", dave),
		"not chosen yet":    h.choose(t, authn.ChooserKindOIDC, "rp-1", 0),
		"no such ticket":    "missing",
		"expired or forged": "Zm9yZ2Vk",
	} {
		t.Run(name, func(t *testing.T) {
			v := baseParams()
			v.Set("account_choice", nonce)
			ticket := h.chooserTicket(t, h.authorize(t, v))
			if strings.Contains(ticket.ResumeURL, "account_choice") {
				t.Fatalf("resume URL keeps the stale choice: %q", ticket.ResumeURL)
			}
		})
	}
}

// Step 2: id_token_hint's sid is looked up first, then the account the hint
// names; an expired hint is still a hint.
func TestAuthorizeAccount_IDTokenHint(t *testing.T) {
	h := newMultiHarness(t)
	h.signIn(t, alice)
	h.signIn(t, bob)
	h.signIn(t, carol)

	v := baseParams()
	v.Set("id_token_hint", h.hint(t, bob, ""))
	h.codeFor(t, h.authorize(t, v), bob)

	// The sid wins over the account claim.
	v.Set("id_token_hint", h.hint(t, alice, h.sids[carol]))
	h.codeFor(t, h.authorize(t, v), carol)

	// A hint naming an account not signed in here picks nothing; the chooser
	// pre-fills it.
	v.Set("id_token_hint", h.hint(t, dave, ""))
	if ticket := h.chooserTicket(t, h.authorize(t, v)); ticket.LoginHint != "dave" {
		t.Fatalf("chooser login hint = %q, want dave", ticket.LoginHint)
	}

	// A hint that does not verify names nobody.
	v.Set("id_token_hint", "not-a-token")
	h.chooserTicket(t, h.authorize(t, v))
}

// Step 2: login_hint matches a username or a verified email, without case,
// only among this browser's accounts.
func TestAuthorizeAccount_LoginHint(t *testing.T) {
	h := newMultiHarness(t)
	h.signIn(t, alice)
	h.signIn(t, bob)
	h.q.emails[bob] = map[string]bool{"Bob@Example.com": true}
	h.q.emails[alice] = map[string]bool{"shared@example.com": false}
	h.q.emails[dave] = map[string]bool{"dave@example.com": true}

	for _, hint := range []string{"BOB", "bob@example.COM"} {
		v := baseParams()
		v.Set("login_hint", hint)
		h.codeFor(t, h.authorize(t, v), bob)
	}
	for _, hint := range []string{"shared@example.com", "dave", "dave@example.com"} {
		v := baseParams()
		v.Set("login_hint", hint)
		if ticket := h.chooserTicket(t, h.authorize(t, v)); ticket.LoginHint != hint {
			t.Fatalf("login_hint %q: chooser pre-fill = %q", hint, ticket.LoginHint)
		}
	}
}

// Step 3: prompt=select_account opens the chooser even for one account and
// even when a hint would pick one; with no account it signs in.
func TestAuthorizeAccount_SelectAccount(t *testing.T) {
	h := newMultiHarness(t)
	v := baseParams()
	v.Set("prompt", "select_account")
	if base, _ := locationQuery(t, h.authorize(t, v)); base != testIssuer+"/login" {
		t.Fatalf("no account: want the login page, got %q", base)
	}

	h.signIn(t, alice)
	v.Set("login_hint", "alice")
	ticket := h.chooserTicket(t, h.authorize(t, v))
	if ticket.Kind != authn.ChooserKindOIDC || ticket.App != "rp-1" || ticket.LoginHint != "alice" {
		t.Fatalf("ticket = %+v", ticket)
	}
	if !strings.HasPrefix(ticket.ResumeURL, "/oauth/authorize?") || !strings.Contains(ticket.ResumeURL, "prompt=select_account") {
		t.Fatalf("resume URL = %q", ticket.ResumeURL)
	}
}

// select_account with a valid choice is satisfied by it: the chooser does not
// send the reader back to itself.
func TestAuthorizeAccount_SelectAccountWithChoiceDoesNotLoop(t *testing.T) {
	h := newMultiHarness(t)
	h.signIn(t, alice)
	h.signIn(t, bob)
	v := baseParams()
	v.Set("prompt", "select_account")
	first := h.chooserTicket(t, h.authorize(t, v))
	resume, err := url.Parse(authn.ResumeWithChoice(first.ResumeURL, h.choose(t, authn.ChooserKindOIDC, "rp-1", bob)))
	if err != nil {
		t.Fatal(err)
	}
	h.codeFor(t, h.serve(t, resume.String(), h.p.HandleAuthorize), bob)
}

// Step 4: no account signs in, pre-filling login_hint; prompt=none answers
// login_required.
func TestAuthorizeAccount_NoAccount(t *testing.T) {
	h := newMultiHarness(t)
	v := baseParams()
	v.Set("login_hint", "bob")
	base, q := locationQuery(t, h.authorize(t, v))
	if base != testIssuer+"/login" || q.Get("login_hint") != "bob" || !strings.Contains(q.Get("return_to"), "/oauth/authorize?") {
		t.Fatalf("login redirect = %q %v", base, q)
	}
	v.Set("prompt", "none")
	if _, q := locationQuery(t, h.authorize(t, v)); q.Get("error") != errCodeLoginRequired {
		t.Fatalf("prompt=none error = %q", q.Get("error"))
	}
}

// Step 5: one account is used as before.
func TestAuthorizeAccount_OneAccount(t *testing.T) {
	h := newMultiHarness(t)
	h.signIn(t, bob)
	h.codeFor(t, h.authorize(t, baseParams()), bob)
}

// Step 6: several accounts open the chooser; prompt=none answers
// account_selection_required.
func TestAuthorizeAccount_SeveralAccounts(t *testing.T) {
	h := newMultiHarness(t)
	h.signIn(t, alice)
	h.signIn(t, bob)
	rec := h.authorize(t, baseParams())
	ticket := h.chooserTicket(t, rec)
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("the chooser redirect must not be cached")
	}
	if ticket.AccountID != 0 || ticket.ResumeURL != "/oauth/authorize?"+baseParams().Encode() {
		t.Fatalf("ticket = %+v", ticket)
	}

	v := baseParams()
	v.Set("prompt", "none")
	_, q := locationQuery(t, h.authorize(t, v))
	if q.Get("error") != errCodeAccountSelectionRequired || q.Get("state") != "xyz-state" || q.Get("iss") != testIssuer {
		t.Fatalf("prompt=none answer = %v", q)
	}
}

// A re-auth demand pre-fills the chosen account and keeps account_choice in
// the address the login page returns to, so the same account is used after
// signing in again.
func TestAuthorizeAccount_ChoiceSurvivesReauth(t *testing.T) {
	h := newMultiHarness(t)
	h.signIn(t, alice)
	h.signIn(t, bob)
	v := baseParams()
	v.Set("prompt", "login")
	v.Set("account_choice", h.choose(t, authn.ChooserKindOIDC, "rp-1", bob))
	base, q := locationQuery(t, h.authorize(t, v))
	if base != testIssuer+"/login" || q.Get("login_hint") != "bob" {
		t.Fatalf("re-auth redirect = %q %v", base, q)
	}
	back, err := url.Parse(q.Get("return_to"))
	if err != nil || back.Query().Get("account_choice") != v.Get("account_choice") || back.Query().Get("reauth") == "" {
		t.Fatalf("return_to = %q", q.Get("return_to"))
	}

	time.Sleep(time.Millisecond)
	h.signIn(t, bob)
	h.codeFor(t, h.serve(t, back.RequestURI(), h.p.HandleAuthorize), bob)
}

// Consent opens for the chosen account, carrying its `u`, and the consent
// page's return address comes back to the same account.
func TestAuthorizeAccount_ConsentRoundTrip(t *testing.T) {
	h := newMultiHarness(t)
	h.q.client.RequireConsent = true
	h.signIn(t, alice)
	h.signIn(t, bob)
	h.q.granted[alice] = []string{"openid", "profile"}
	v := baseParams()
	v.Set("account_choice", h.choose(t, authn.ChooserKindOIDC, "rp-1", bob))

	base, q := locationQuery(t, h.authorize(t, v))
	if base != testIssuer+"/consent" || q.Get("u") != h.u(bob) || q.Get("u") == "" {
		t.Fatalf("consent redirect = %q %v (want u=%s)", base, q, h.u(bob))
	}
	ticket, ok, err := authn.PeekConsent(context.Background(), h.p.kv, q.Get("ticket"), bob)
	if err != nil || !ok || ticket.ClientID != "rp-1" {
		t.Fatalf("consent ticket for bob: %v %v %v", ticket, ok, err)
	}
	back, _ := url.Parse(q.Get("return_to"))
	if back.Query().Get("account_choice") != v.Get("account_choice") {
		t.Fatalf("consent return_to lost the choice: %q", back)
	}

	h.q.granted[bob] = []string{"openid", "profile"}
	h.codeFor(t, h.serve(t, back.RequestURI(), h.p.HandleAuthorize), bob)
}

// An access denial for the chosen account names it on the error page.
func TestAuthorizeAccount_AccessDeniedCarriesU(t *testing.T) {
	h := newMultiHarness(t)
	h.signIn(t, alice)
	h.signIn(t, bob)
	h.access.evaluate = func(_ context.Context, id int32, _ string) (appaccess.Decision, error) {
		return appaccess.Decision{Allowed: id != bob, Source: appaccess.SourceManualDeny}, nil
	}
	v := baseParams()
	v.Set("account_choice", h.choose(t, authn.ChooserKindOIDC, "rp-1", bob))
	base, q := locationQuery(t, h.authorize(t, v))
	if base != testIssuer+"/error" || q.Get("reason") != "app_access_denied" || q.Get("u") != h.u(bob) {
		t.Fatalf("denial = %q %v", base, q)
	}

	// With one account no `u` appears.
	single := newMultiHarness(t)
	single.access.evaluate = h.access.evaluate
	single.signIn(t, bob)
	if _, q := locationQuery(t, single.authorize(t, baseParams())); q.Has("u") {
		t.Fatalf("single account denial carries u: %v", q)
	}
}

// Maintenance is checked for the account chosen, not the default one.
func TestAuthorizeAccount_MaintenanceChecksChosenAccount(t *testing.T) {
	h := newMultiHarness(t)
	h.p.SetMaintenanceChecker(func(context.Context) bool { return true })
	h.signIn(t, carol) // admin, the default account
	h.signIn(t, bob)

	v := baseParams()
	v.Set("account_choice", h.choose(t, authn.ChooserKindOIDC, "rp-1", bob))
	base, q := locationQuery(t, h.authorize(t, v))
	if base != testIssuer+"/" || q.Get("u") != h.u(bob) {
		t.Fatalf("maintenance redirect = %q %v", base, q)
	}
	v.Set("prompt", "none")
	if _, q := locationQuery(t, h.authorize(t, v)); q.Get("error") != errCodeTemporarilyUnavailable {
		t.Fatalf("prompt=none maintenance answer = %v", q)
	}

	v = baseParams()
	v.Set("account_choice", h.choose(t, authn.ChooserKindOIDC, "rp-1", carol))
	h.codeFor(t, h.authorize(t, v), carol)
}

func (h *multiHarness) requireIdentity(t *testing.T, v url.Values) *httptest.ResponseRecorder {
	t.Helper()
	return h.serve(t, "/api/prohibitorum/require-identity?"+v.Encode(), h.p.HandleRequireIdentity)
}

// require-identity resolves the account with the same rules: several
// accounts open the chooser, a choice or a hint for an account here picks it,
// and a hint for an account not here is a mismatch that names it.
func TestRequireIdentityAccount(t *testing.T) {
	h := newMultiHarness(t)
	h.signIn(t, alice)
	h.signIn(t, bob)

	ticket := h.chooserTicket(t, h.requireIdentity(t, requireIdentityParams()))
	if ticket.Kind != authn.ChooserKindRequireIdentity || ticket.App != "rp-1" ||
		!strings.HasPrefix(ticket.ResumeURL, "/api/prohibitorum/require-identity?") {
		t.Fatalf("ticket = %+v", ticket)
	}

	loadTicket := func(rec *httptest.ResponseRecorder, accountID int32) RequireIdentityTicket {
		t.Helper()
		loc, _ := url.Parse(rec.Header().Get("Location"))
		if rec.Code != http.StatusFound || !strings.HasPrefix(loc.Path, "/require-identity/") || loc.Query().Get("u") != h.u(accountID) {
			t.Fatalf("want the require-identity page for %d, got %d %q", accountID, rec.Code, loc)
		}
		got, err := h.p.LoadRequireIdentityTicket(context.Background(), strings.TrimPrefix(loc.Path, "/require-identity/"), accountID, h.sids[accountID])
		if err != nil {
			t.Fatal(err)
		}
		return got
	}

	v := requireIdentityParams()
	v.Set("account_choice", h.choose(t, authn.ChooserKindRequireIdentity, "rp-1", bob))
	if got := loadTicket(h.requireIdentity(t, v), bob); got.Mismatch || strings.Contains(got.Entry, "account_choice") {
		t.Fatalf("chosen ticket = %+v", got)
	}

	v = requireIdentityParams()
	v.Set("id_token_hint", h.hint(t, bob, ""))
	if got := loadTicket(h.requireIdentity(t, v), bob); got.Mismatch {
		t.Fatalf("hint for a signed-in account is not a mismatch: %+v", got)
	}

	v.Set("id_token_hint", h.hint(t, dave, ""))
	v.Set("account_choice", h.choose(t, authn.ChooserKindRequireIdentity, "rp-1", alice))
	if got := loadTicket(h.requireIdentity(t, v), alice); !got.Mismatch || got.LoginHint != "dave" {
		t.Fatalf("mismatch ticket = %+v", got)
	}
}
