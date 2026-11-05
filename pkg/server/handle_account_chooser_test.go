package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"prohibitorum/pkg/appaccess"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/credential/totp"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/kv"
	sessstore "prohibitorum/pkg/session"
	"prohibitorum/pkg/weberr"
)

type fakeChooserQ struct {
	accounts map[int32]db.Account
	clients  map[string]db.OidcClient
	sps      map[string]db.SamlSp
}

func (f *fakeChooserQ) GetAccountByID(_ context.Context, id int32) (db.Account, error) {
	if a, ok := f.accounts[id]; ok {
		return a, nil
	}
	return db.Account{}, pgx.ErrNoRows
}

func (f *fakeChooserQ) GetOIDCClient(_ context.Context, id string) (db.OidcClient, error) {
	if c, ok := f.clients[id]; ok {
		return c, nil
	}
	return db.OidcClient{}, pgx.ErrNoRows
}

func (f *fakeChooserQ) GetSAMLSPByEntityID(_ context.Context, id string) (db.SamlSp, error) {
	if sp, ok := f.sps[id]; ok {
		return sp, nil
	}
	return db.SamlSp{}, pgx.ErrNoRows
}

func (f *fakeChooserQ) GetEntityIconEtag(context.Context, db.GetEntityIconEtagParams) (string, error) {
	return "", pgx.ErrNoRows
}

func (f *fakeChooserQ) InsertSession(_ context.Context, arg db.InsertSessionParams) (db.Session, error) {
	return db.Session{ID: arg.ID, AccountID: arg.AccountID}, nil
}
func (f *fakeChooserQ) RevokeSession(context.Context, string) error             { return nil }
func (f *fakeChooserQ) RevokeAllSessionsByAccount(context.Context, int32) error { return nil }

// fakeAppAccess denies the (account, app) pairs listed.
type fakeAppAccess struct {
	deniedOIDC map[int32]bool
	deniedSAML map[int32]bool
}

func (f *fakeAppAccess) EvaluateOIDC(_ context.Context, id int32, _ string) (appaccess.Decision, error) {
	return appaccess.Decision{Allowed: !f.deniedOIDC[id]}, nil
}

func (f *fakeAppAccess) EvaluateSAML(_ context.Context, id int32, _ int64) (appaccess.Decision, error) {
	return appaccess.Decision{Allowed: !f.deniedSAML[id]}, nil
}

type chooserHarness struct {
	s       *Server
	q       *fakeChooserQ
	access  *fakeAppAccess
	entries []sessstore.Entry
}

func newChooserHarness(t *testing.T) *chooserHarness {
	t.Helper()
	store := kv.NewMemoryStore()
	t.Cleanup(func() { _ = store.Close() })
	q := &fakeChooserQ{
		accounts: map[int32]db.Account{
			1: {ID: 1, Username: "lin", DisplayName: "Lin"},
			2: {ID: 2, Username: "mo", DisplayName: "Mo"},
			3: {ID: 3, Username: "kai", DisplayName: "Kai"},
		},
		clients: map[string]db.OidcClient{"wiki": {ClientID: "wiki", DisplayName: "Wiki"}},
		sps:     map[string]db.SamlSp{"https://sp.example/meta": {ID: 9, EntityID: "https://sp.example/meta", DisplayName: "Files"}},
	}
	access := &fakeAppAccess{deniedOIDC: map[int32]bool{}, deniedSAML: map[int32]bool{}}
	s := &Server{
		config:                 &configx.Config{OIDC: configx.OIDCConfig{Issuer: "https://idp.example"}},
		kvStore:                store,
		sessionStore:           sessstore.NewSessionStore(store, q, time.Hour, time.Hour),
		accountLookup:          q,
		appAccess:              access,
		accountChooserOverride: q,
	}
	return &chooserHarness{s: s, q: q, access: access}
}

func (h *chooserHarness) signIn(t *testing.T, ids ...int32) {
	t.Helper()
	for _, id := range ids {
		token, _, err := h.s.sessionStore.Issue(context.Background(), id, "", "", []string{"hwk"}, nil, false)
		if err != nil {
			t.Fatal(err)
		}
		h.entries = append(h.entries, sessstore.Entry{Seq: int64(len(h.entries) + 1), AccountID: id, Token: token})
	}
}

// request runs r through the session middleware and returns the context the
// handler saw.
func (h *chooserHarness) withBrowser(r *http.Request) *http.Request {
	if len(h.entries) > 0 {
		r.AddCookie(&http.Cookie{Name: sessstore.TempCookieName(h.s.config), Value: sessstore.EncodeCookie(h.entries)})
	}
	var seen *http.Request
	h.s.browserSessions().LoadSession()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = r
	})).ServeHTTP(httptest.NewRecorder(), r)
	return seen
}

func (h *chooserHarness) ctx(accountHeader int32) context.Context {
	r := httptest.NewRequest(http.MethodGet, "/api/prohibitorum/account-chooser", nil)
	if accountHeader != 0 {
		r.Header.Set(sessstore.AccountHeader, fmt.Sprint(accountHeader))
	}
	return h.withBrowser(r).Context()
}

func (h *chooserHarness) ticket(t *testing.T, tk authn.AccountChooserTicket) string {
	t.Helper()
	nonce, err := authn.CreateAccountChooser(context.Background(), h.s.kvStore, tk)
	if err != nil {
		t.Fatal(err)
	}
	return nonce
}

func wantPublicErr(t *testing.T, err error, status int, code string) {
	t.Helper()
	var pe *weberr.PublicError
	if !errors.As(err, &pe) || pe.Code != code || pe.GetStatus() != status {
		t.Fatalf("err = %v, want %d %s", err, status, code)
	}
}

func TestAccountChooserGet_ListsAccountsWithAccess(t *testing.T) {
	h := newChooserHarness(t)
	h.signIn(t, 1, 2)
	h.access.deniedOIDC[2] = true
	nonce := h.ticket(t, authn.AccountChooserTicket{Kind: authn.ChooserKindOIDC, App: "wiki", ResumeURL: "/oauth/authorize?client_id=wiki", LoginHint: "kai"})

	out, err := h.s.handleGetAccountChooser(h.ctx(0), &accountChooserInput{Ticket: nonce})
	if err != nil {
		t.Fatal(err)
	}
	b := out.Body
	if b.App.Kind != "oidc" || b.App.Key != "oidc:wiki" || b.App.Name != "Wiki" || b.LoginHint != "kai" {
		t.Fatalf("chooser = %+v", b)
	}
	shorts := h.s.refs().Shorts([]int32{1, 2})
	if len(b.Accounts) != 2 || b.Accounts[0].AccountID != 1 || !b.Accounts[0].Allowed || b.Accounts[0].Username != "lin" ||
		b.Accounts[0].U != shorts[1] || b.Accounts[1].AccountID != 2 || b.Accounts[1].Allowed {
		t.Fatalf("accounts = %+v", b.Accounts)
	}
	want := "/login?return_to=" + url.QueryEscape(accountChooserContinuePath+"?ticket="+nonce) + "&login_hint=kai"
	if b.OtherAccountURL != want {
		t.Fatalf("otherAccountUrl = %q, want %q", b.OtherAccountURL, want)
	}

	// SAML tickets name the SP by entity ID and use the SAML policy.
	h.access.deniedSAML[1] = true
	samlNonce := h.ticket(t, authn.AccountChooserTicket{Kind: authn.ChooserKindSAMLInit, App: "https://sp.example/meta", ResumeURL: "/saml/sso/init"})
	out, err = h.s.handleGetAccountChooser(h.ctx(0), &accountChooserInput{Ticket: samlNonce})
	if err != nil {
		t.Fatal(err)
	}
	if out.Body.App.Kind != "saml" || out.Body.App.Key != "saml:https://sp.example/meta" || out.Body.App.Name != "Files" ||
		out.Body.Accounts[0].Allowed || !out.Body.Accounts[1].Allowed {
		t.Fatalf("saml chooser = %+v", out.Body)
	}
}

func TestAccountChooserGet_Expired(t *testing.T) {
	h := newChooserHarness(t)
	_, err := h.s.handleGetAccountChooser(h.ctx(0), &accountChooserInput{Ticket: "missing"})
	wantPublicErr(t, err, http.StatusNotFound, "account_chooser_expired")

	gone := h.ticket(t, authn.AccountChooserTicket{Kind: authn.ChooserKindOIDC, App: "deleted", ResumeURL: "/oauth/authorize"})
	_, err = h.s.handleGetAccountChooser(h.ctx(0), &accountChooserInput{Ticket: gone})
	wantPublicErr(t, err, http.StatusNotFound, "account_chooser_expired")
}

func TestAccountChooserPost(t *testing.T) {
	h := newChooserHarness(t)
	h.signIn(t, 1, 2)
	h.access.deniedOIDC[2] = true
	nonce := h.ticket(t, authn.AccountChooserTicket{Kind: authn.ChooserKindOIDC, App: "wiki", ResumeURL: "/oauth/authorize?client_id=wiki&account_choice=old"})
	choose := func(id int32) (*accountChooserRedirectOutput, error) {
		return h.s.handleChooseAccount(h.ctx(0), &chooseAccountInput{Body: contract.ChooseAccountInput{Ticket: nonce, AccountID: id}})
	}

	_, err := choose(3)
	wantPublicErr(t, err, http.StatusUnauthorized, "account_not_signed_in")
	_, err = choose(2)
	wantPublicErr(t, err, http.StatusForbidden, "app_access_denied")
	if id := authn.ChosenAccount(context.Background(), h.s.kvStore, nonce, authn.ChooserKindOIDC, "wiki"); id != 0 {
		t.Fatalf("a refused choice was recorded: %d", id)
	}

	out, err := choose(1)
	if err != nil {
		t.Fatal(err)
	}
	if want := "/oauth/authorize?client_id=wiki&account_choice=" + url.QueryEscape(nonce); out.Body.Redirect != want {
		t.Fatalf("redirect = %q, want %q", out.Body.Redirect, want)
	}
	if id := authn.ChosenAccount(context.Background(), h.s.kvStore, nonce, authn.ChooserKindOIDC, "wiki"); id != 1 {
		t.Fatalf("chosen = %d", id)
	}

	// The choice can be changed while the ticket lives.
	h.access.deniedOIDC[2] = false
	if _, err := choose(2); err != nil {
		t.Fatal(err)
	}
	if id := authn.ChosenAccount(context.Background(), h.s.kvStore, nonce, authn.ChooserKindOIDC, "wiki"); id != 2 {
		t.Fatalf("re-chosen = %d", id)
	}

	_, err = h.s.handleChooseAccount(h.ctx(0), &chooseAccountInput{Body: contract.ChooseAccountInput{Ticket: "missing", AccountID: 1}})
	wantPublicErr(t, err, http.StatusNotFound, "account_chooser_expired")
}

func (h *chooserHarness) continueTo(t *testing.T, nonce string) string {
	t.Helper()
	r := h.withBrowser(httptest.NewRequest(http.MethodGet, accountChooserContinuePath+"?ticket="+url.QueryEscape(nonce), nil))
	rec := httptest.NewRecorder()
	h.s.handleAccountChooserContinueHTTP(rec, r)
	if rec.Code != http.StatusFound {
		t.Fatalf("continue: %d", rec.Code)
	}
	return rec.Header().Get("Location")
}

func TestAccountChooserContinue(t *testing.T) {
	h := newChooserHarness(t)
	h.signIn(t, 1)
	unbound := h.ticket(t, authn.AccountChooserTicket{Kind: authn.ChooserKindOIDC, App: "wiki", ResumeURL: "/oauth/authorize?client_id=wiki"})
	if got := h.continueTo(t, unbound); got != "/choose-account?ticket="+url.QueryEscape(unbound) {
		t.Fatalf("unbound ticket: %q", got)
	}
	signedOut := h.ticket(t, authn.AccountChooserTicket{Kind: authn.ChooserKindOIDC, App: "wiki", ResumeURL: "/oauth/authorize?client_id=wiki", AccountID: 2})
	if got := h.continueTo(t, signedOut); got != "/choose-account?ticket="+url.QueryEscape(signedOut) {
		t.Fatalf("account not signed in: %q", got)
	}
	bound := h.ticket(t, authn.AccountChooserTicket{Kind: authn.ChooserKindOIDC, App: "wiki", ResumeURL: "/oauth/authorize?client_id=wiki", AccountID: 1})
	if got := h.continueTo(t, bound); got != "/oauth/authorize?client_id=wiki&account_choice="+url.QueryEscape(bound) {
		t.Fatalf("bound ticket: %q", got)
	}
	if got := h.continueTo(t, "missing"); got != "/error?reason=account_chooser_expired" {
		t.Fatalf("missing ticket: %q", got)
	}
}

func TestAccountChooserSwitch(t *testing.T) {
	h := newChooserHarness(t)
	h.signIn(t, 1, 2)
	ctx := context.Background()
	consent, err := authn.DemandConsent(ctx, h.s.kvStore, authn.ConsentTicket{AccountID: 2, ClientID: "wiki"})
	if err != nil {
		t.Fatal(err)
	}
	authorize := "https://idp.example/oauth/authorize?client_id=wiki&scope=openid&account_choice=old"
	sw := func(as int32, in contract.SwitchConsentAccountInput) (*accountChooserRedirectOutput, error) {
		return h.s.handleSwitchConsentAccount(h.ctx(as), &switchConsentAccountInput{Body: in})
	}

	out, err := sw(2, contract.SwitchConsentAccountInput{Kind: "oidc", ConsentTicket: consent, ReturnTo: authorize})
	if err != nil {
		t.Fatal(err)
	}
	nonce := strings.TrimPrefix(out.Body.Redirect, "/choose-account?ticket=")
	ticket, _ := authn.GetAccountChooser(ctx, h.s.kvStore, nonce)
	if ticket == nil || ticket.Kind != authn.ChooserKindOIDC || ticket.App != "wiki" ||
		ticket.ResumeURL != "/oauth/authorize?client_id=wiki&scope=openid" || ticket.AccountID != 0 {
		t.Fatalf("switch ticket = %+v (redirect %q)", ticket, out.Body.Redirect)
	}
	if got, _ := authn.LookupConsent(ctx, h.s.kvStore, consent); got == nil {
		t.Fatal("switching must not consume the consent ticket")
	}

	// The consent ticket belongs to the account the page acts as.
	_, err = sw(1, contract.SwitchConsentAccountInput{Kind: "oidc", ConsentTicket: consent, ReturnTo: authorize})
	wantPublicErr(t, err, http.StatusUnauthorized, "account_not_signed_in")
	_, err = sw(2, contract.SwitchConsentAccountInput{Kind: "oidc", ConsentTicket: "missing", ReturnTo: authorize})
	wantPublicErr(t, err, http.StatusBadRequest, "invalid_consent_ticket")
	for _, rt := range []string{
		"https://idp.example/oauth/authorize?client_id=other",
		"https://idp.example/consent?client_id=wiki",
		"https://evil.example/oauth/authorize?client_id=wiki",
	} {
		_, err = sw(2, contract.SwitchConsentAccountInput{Kind: "oidc", ConsentTicket: consent, ReturnTo: rt})
		wantPublicErr(t, err, http.StatusBadRequest, "bad_request")
	}

	samlConsent, err := authn.DemandSAMLConsent(ctx, h.s.kvStore, authn.SAMLConsentTicket{AccountID: 2, SPID: 9, EntityID: "https://sp.example/meta"})
	if err != nil {
		t.Fatal(err)
	}
	out, err = sw(2, contract.SwitchConsentAccountInput{Kind: "saml", ConsentTicket: samlConsent})
	if err != nil {
		t.Fatal(err)
	}
	ticket, _ = authn.GetAccountChooser(ctx, h.s.kvStore, strings.TrimPrefix(out.Body.Redirect, "/choose-account?ticket="))
	if ticket == nil || ticket.Kind != authn.ChooserKindSAML || ticket.App != "https://sp.example/meta" ||
		ticket.ResumeURL != "/saml/sso/resume?ticket="+url.QueryEscape(samlConsent) {
		t.Fatalf("saml switch ticket = %+v", ticket)
	}
	_, err = sw(1, contract.SwitchConsentAccountInput{Kind: "saml", ConsentTicket: samlConsent})
	wantPublicErr(t, err, http.StatusUnauthorized, "account_not_signed_in")
}

// "Use another account" signs in with return_to at the continue address; the
// new account is bound to the ticket, so continuing resumes as it. Shown here
// with the password + TOTP sign-in, which shares issueBrowserSession with
// the passkey one.
func TestAccountChooserContinue_BindsPasswordSignIn(t *testing.T) {
	s, f, dek := newTestServer(t)
	s.config.OIDC.Issuer = "https://idp.example"
	const accountID int32 = 77
	_ = seedConfirmedTOTP(t, s, f, dek, accountID)
	nonce, err := authn.CreateAccountChooser(context.Background(), s.kvStore, authn.AccountChooserTicket{
		Kind: authn.ChooserKindOIDC, App: "wiki", ResumeURL: "/oauth/authorize?client_id=wiki",
	})
	if err != nil {
		t.Fatal(err)
	}
	token := mustToken(t)
	stashPartialSession(t, s, token, accountID)
	at := (time.Now().Unix()/30+1)*30 + 15
	code := totp.ComputeCodeForTesting(decryptTOTPSecret(t, dek, *f.totpRow, accountID), at, 6)
	returnTo := "https://idp.example" + accountChooserContinuePath + "?ticket=" + url.QueryEscape(nonce)
	req := httptest.NewRequest(http.MethodPost, "/api/prohibitorum/auth/totp/verify?return_to="+url.QueryEscape(returnTo),
		strings.NewReader(fmt.Sprintf(`{"partial_session_token":%q,"code":%q}`, token, code)))
	req.RemoteAddr = "127.0.0.1:5555"
	rec := httptest.NewRecorder()
	s.handleTOTPVerifyHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("totp verify: %d %s", rec.Code, rec.Body.String())
	}
	var result contract.LoginResult
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.Redirect != accountChooserContinuePath+"?ticket="+url.QueryEscape(nonce) {
		t.Fatalf("redirect = %q", result.Redirect)
	}
	if id := authn.ChosenAccount(context.Background(), s.kvStore, nonce, authn.ChooserKindOIDC, "wiki"); id != accountID {
		t.Fatalf("ticket bound to %d, want %d", id, accountID)
	}

	cont := httptest.NewRequest(http.MethodGet, result.Redirect, nil)
	for _, c := range rec.Result().Cookies() {
		cont.AddCookie(c)
	}
	out := httptest.NewRecorder()
	s.browserSessions().LoadSession()(http.HandlerFunc(s.handleAccountChooserContinueHTTP)).ServeHTTP(out, cont)
	if got := out.Header().Get("Location"); got != "/oauth/authorize?client_id=wiki&account_choice="+url.QueryEscape(nonce) {
		t.Fatalf("continue Location = %q", got)
	}
}

// The same through an upstream sign-in (mock OP): the federation callback
// lands on the continue address with the account bound.
func TestAccountChooserContinue_BindsFederatedSignIn(t *testing.T) {
	h := newFederationTestServer(t)
	seedConfirmedIdentity(h)
	nonce, err := authn.CreateAccountChooser(context.Background(), h.s.kvStore, authn.AccountChooserTicket{
		Kind: authn.ChooserKindSAML, App: "https://sp.example/meta", ResumeURL: "/saml/sso/resume?ticket=c",
	})
	if err != nil {
		t.Fatal(err)
	}
	continueURL := accountChooserContinuePath + "?ticket=" + url.QueryEscape(nonce)
	loc, resp := h.driveLogin(t, "mockop", continueURL)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("login status %d", resp.StatusCode)
	}
	code, state, iss := driveAuthorize(t, loc)
	resp = h.hitCallback(t, "mockop", url.Values{"code": {code}, "state": {state}, "iss": {iss}})
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != continueURL {
		t.Fatalf("callback: %d Location=%q", resp.StatusCode, resp.Header.Get("Location"))
	}
	if id := authn.ChosenAccount(context.Background(), h.s.kvStore, nonce, authn.ChooserKindSAML, "https://sp.example/meta"); id != 777 {
		t.Fatalf("ticket bound to %d, want 777", id)
	}

	cont := httptest.NewRequest(http.MethodGet, continueURL, nil)
	for _, c := range resp.Cookies() {
		cont.AddCookie(c)
	}
	out := httptest.NewRecorder()
	h.s.browserSessions().LoadSession()(http.HandlerFunc(h.s.handleAccountChooserContinueHTTP)).ServeHTTP(out, cont)
	if got := out.Header().Get("Location"); got != "/saml/sso/resume?ticket=c&account_choice="+url.QueryEscape(nonce) {
		t.Fatalf("continue Location = %q", got)
	}
}

// A sign-in that does not return to the continue address binds nothing.
func TestBindChooserTicket_OnlyForContinueAddress(t *testing.T) {
	h := newChooserHarness(t)
	nonce := h.ticket(t, authn.AccountChooserTicket{Kind: authn.ChooserKindOIDC, App: "wiki", ResumeURL: "/oauth/authorize"})
	r := httptest.NewRequest(http.MethodPost, "/api/prohibitorum/auth/login/complete", nil)
	for _, rt := range []string{
		"/me?ticket=" + nonce,
		"https://evil.example" + accountChooserContinuePath + "?ticket=" + nonce,
		accountChooserContinuePath,
	} {
		h.s.bindChooserTicket(r, rt, 1)
	}
	if id := authn.ChosenAccount(context.Background(), h.s.kvStore, nonce, authn.ChooserKindOIDC, "wiki"); id != 0 {
		t.Fatalf("bound to %d", id)
	}
}
