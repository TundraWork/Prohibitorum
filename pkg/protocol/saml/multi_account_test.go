package saml

import (
	"bytes"
	"context"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/kv"
	"prohibitorum/pkg/session"
)

// Several accounts signed in on one browser (PHB-285, design 5.4): the SSO
// endpoints settle which account a request acts as before anything is issued.

type browserAccounts map[int32]db.Account

func (a browserAccounts) GetAccountByID(_ context.Context, id int32) (db.Account, error) {
	if acct, ok := a[id]; ok {
		return acct, nil
	}
	return db.Account{}, pgx.ErrNoRows
}

// testBrowser is a browser with real sessions for its accounts, in the order
// given (the first is the default account).
type testBrowser struct {
	h        *ssoHarness
	mgr      *session.Manager
	store    *session.SessionStore
	entries  []session.Entry
	sessions map[int32]string // account → SessionID
}

var (
	octocat = db.Account{ID: 42, Username: "octocat", Role: "user"}
	hubot   = db.Account{ID: 43, Username: "hubot", Role: "user"}
)

func newTestBrowser(t *testing.T, h *ssoHarness, accts ...db.Account) *testBrowser {
	t.Helper()
	all := browserAccounts{}
	for _, a := range accts {
		all[a.ID] = a
	}
	store := session.NewSessionStore(kv.NewMemoryStore(), fakeSessionQueries{}, time.Hour, time.Hour)
	b := &testBrowser{h: h, store: store, mgr: session.NewManager(h.idp.cfg, all, store, nil), sessions: map[int32]string{}}
	for n, a := range accts {
		token, data, err := store.Issue(context.Background(), a.ID, "", "", []string{"hwk"}, nil, false)
		if err != nil {
			t.Fatalf("issue session: %v", err)
		}
		b.entries = append(b.entries, session.Entry{Seq: int64(n + 1), AccountID: a.ID, Token: token})
		b.sessions[a.ID] = data.SessionID
	}
	h.idp.SetAccountRefs(session.NewAccountRefs([]byte("saml-multi-account-test-key")))
	return b
}

// attach puts the browser's cookies on r and attaches it the way LoadSession
// does for a protocol endpoint: the Browser, and the default account's session.
func (b *testBrowser) attach(r *http.Request) *http.Request {
	r.AddCookie(&http.Cookie{Name: session.TempCookieName(b.h.idp.cfg), Value: session.EncodeCookie(b.entries)})
	br := b.mgr.BrowserFor(r)
	ctx := session.WithBrowser(r.Context(), br)
	if d := br.Default(); d != nil {
		ctx = authn.WithSession(ctx, d)
	}
	return r.WithContext(ctx)
}

func (b *testBrowser) get(target string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.RemoteAddr = "203.0.113.7:54321"
	req.Header.Set("User-Agent", "test-agent/1.0")
	return b.attach(req)
}

// chooserTicket follows a redirect to the account chooser and returns its
// ticket nonce and contents.
func chooserTicket(t *testing.T, h *ssoHarness, rec *httptest.ResponseRecorder) (string, *authn.AccountChooserTicket) {
	t.Helper()
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 to the account chooser; body=%s", rec.Code, rec.Body.String())
	}
	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil || loc.Path != "/choose-account" || !strings.HasPrefix(rec.Header().Get("Location"), testIdPOrigin+"/choose-account?") {
		t.Fatalf("Location = %q, want the account chooser", rec.Header().Get("Location"))
	}
	nonce := loc.Query().Get("ticket")
	ticket, err := authn.GetAccountChooser(context.Background(), h.idp.kv, nonce)
	if err != nil || ticket == nil {
		t.Fatalf("chooser ticket %q: %v, %v", nonce, ticket, err)
	}
	return nonce, ticket
}

// choose records the reader's choice the way the chooser page does and
// returns the resume address with account_choice appended.
func choose(t *testing.T, h *ssoHarness, nonce string, ticket *authn.AccountChooserTicket, accountID int32) string {
	t.Helper()
	if _, err := authn.BindAccountChooser(context.Background(), h.idp.kv, nonce, accountID); err != nil {
		t.Fatal(err)
	}
	return testIdPOrigin + ticket.ResumeURL + "&" + accountChoiceParam + "=" + nonce
}

// boundChoice creates a ticket already bound to accountID.
func boundChoice(t *testing.T, h *ssoHarness, kind, app string, accountID int32) string {
	t.Helper()
	nonce, err := authn.CreateAccountChooser(context.Background(), h.idp.kv, authn.AccountChooserTicket{Kind: kind, App: app, AccountID: accountID})
	if err != nil {
		t.Fatal(err)
	}
	return nonce
}

// assertIssuedTo checks an auto-POSTed assertion was issued from accountID's
// session and names that account.
func assertIssuedTo(t *testing.T, b *testBrowser, rec *httptest.ResponseRecorder, acct db.Account) {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want the auto-POST; Location=%q body=%s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	_, respXML := decodeAutoPost(t, rec.Body.String())
	if !bytes.Contains(respXML, []byte(">"+acct.Username+"<")) {
		t.Errorf("assertion does not name %s:\n%s", acct.Username, respXML)
	}
	rows := b.h.q.sessions()
	if len(rows) != 1 {
		t.Fatalf("saml_session rows = %d, want 1", len(rows))
	}
	if rows[0].SessionID != b.sessions[acct.ID] {
		t.Errorf("saml_session bound to session %q, want %s's %q", rows[0].SessionID, acct.Username, b.sessions[acct.ID])
	}
}

func TestSSOMultiAccountRedirectBindingGoesToChooser(t *testing.T) {
	h := newSSOHarness(t, ssoSP())
	b := newTestBrowser(t, h, octocat, hubot)
	req := b.attach(h.request(t, "_multi-redirect", nil))
	rawQuery := req.URL.RawQuery

	rec := httptest.NewRecorder()
	h.idp.HandleSSO(rec, req)
	nonce, ticket := chooserTicket(t, h, rec)
	if ticket.Kind != authn.ChooserKindSAML || ticket.App != testSPEntityID || ticket.AccountID != 0 {
		t.Errorf("ticket = %+v, want an unchosen saml ticket for %s", ticket, testSPEntityID)
	}
	if ticket.ResumeURL != "/saml/sso?"+rawQuery {
		t.Errorf("ResumeURL = %q, want the request's own address with its signed query intact", ticket.ResumeURL)
	}
	if len(h.q.sessions()) != 0 {
		t.Fatal("an assertion was issued before an account was chosen")
	}

	// The signed request is not spent by the detour: choosing the second
	// account answers it as that account.
	rec = httptest.NewRecorder()
	h.idp.HandleSSO(rec, b.get(choose(t, h, nonce, ticket, hubot.ID)))
	assertIssuedTo(t, b, rec, hubot)
}

func TestSSOMultiAccountPostBindingGoesToChooser(t *testing.T) {
	h := newSSOHarness(t, ssoSP())
	b := newTestBrowser(t, h, octocat, hubot)

	rec := httptest.NewRecorder()
	h.idp.HandleSSO(rec, b.attach(h.signedPost(t, "_multi-post", nil)))
	nonce, ticket := chooserTicket(t, h, rec)
	resume, err := url.Parse(ticket.ResumeURL)
	if err != nil || resume.Path != "/saml/sso" || resume.Query().Get("post") == "" || len(resume.Query()) != 1 {
		t.Fatalf("ResumeURL = %q, want /saml/sso with only the kept form's key", ticket.ResumeURL)
	}

	rec = httptest.NewRecorder()
	h.idp.HandleSSO(rec, b.get(choose(t, h, nonce, ticket, hubot.ID)))
	assertIssuedTo(t, b, rec, hubot)
	if m := relayValueRe.FindStringSubmatch(rec.Body.String()); m == nil || html.UnescapeString(m[1]) != "rs-post+state/1" {
		t.Errorf("the SP's RelayState was lost through the chooser; body=%s", rec.Body.String())
	}
}

func TestSSOMultiAccountIsPassiveAnswersNoPassive(t *testing.T) {
	h := newSSOHarness(t, ssoSP())
	b := newTestBrowser(t, h, octocat, hubot)
	req := b.attach(h.request(t, "_multi-passive", nil, func(o *authnReqOpts) { o.isPassive = true }))

	rec := httptest.NewRecorder()
	h.idp.HandleSSO(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want the NoPassive auto-POST; Location=%q", rec.Code, rec.Header().Get("Location"))
	}
	_, respXML := decodeAutoPost(t, rec.Body.String())
	if !bytes.Contains(respXML, []byte(statusNoPassive)) {
		t.Errorf("Response is not NoPassive:\n%s", respXML)
	}
	if len(h.q.sessions()) != 0 {
		t.Error("a passive request with several accounts issued an assertion")
	}
}

// A passive request whose account was already chosen is answered as that
// account rather than NoPassive.
func TestSSOMultiAccountIsPassiveWithChoiceIssues(t *testing.T) {
	h := newSSOHarness(t, ssoSP())
	b := newTestBrowser(t, h, octocat, hubot)
	nonce := boundChoice(t, h, authn.ChooserKindSAML, testSPEntityID, hubot.ID)
	req := h.request(t, "_multi-passive-chosen", nil, func(o *authnReqOpts) { o.isPassive = true })
	req.URL.RawQuery += "&" + accountChoiceParam + "=" + nonce

	rec := httptest.NewRecorder()
	h.idp.HandleSSO(rec, b.attach(req))
	assertIssuedTo(t, b, rec, hubot)
}

func TestSSOAccountChoiceForAnotherAppIsIgnored(t *testing.T) {
	cases := []struct{ name, kind, app string }{
		{"another SP", authn.ChooserKindSAML, "https://other.example.test/saml/metadata"},
		{"the IdP-initiated flow", authn.ChooserKindSAMLInit, testSPEntityID},
		{"an OIDC client", authn.ChooserKindOIDC, testSPEntityID},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newSSOHarness(t, ssoSP())
			b := newTestBrowser(t, h, octocat, hubot)
			req := h.request(t, "_multi-foreign-choice", nil)
			req.URL.RawQuery += "&" + accountChoiceParam + "=" + boundChoice(t, h, c.kind, c.app, hubot.ID)

			rec := httptest.NewRecorder()
			h.idp.HandleSSO(rec, b.attach(req))
			_, ticket := chooserTicket(t, h, rec)
			if strings.Contains(ticket.ResumeURL, accountChoiceParam) {
				t.Errorf("ResumeURL %q kept the ignored account_choice", ticket.ResumeURL)
			}
		})
	}
}

// A choice naming an account that has since signed out of this browser no
// longer settles anything.
func TestSSOAccountChoiceSignedOutAccountIsIgnored(t *testing.T) {
	h := newSSOHarness(t, ssoSP())
	b := newTestBrowser(t, h, octocat, hubot, db.Account{ID: 44, Username: "monalisa"})
	b.entries = b.entries[:2]
	req := h.request(t, "_multi-gone-choice", nil)
	req.URL.RawQuery += "&" + accountChoiceParam + "=" + boundChoice(t, h, authn.ChooserKindSAML, testSPEntityID, 44)

	rec := httptest.NewRecorder()
	h.idp.HandleSSO(rec, b.attach(req))
	chooserTicket(t, h, rec)
}

func TestSSOForceAuthnAfterChoiceCarriesLoginHint(t *testing.T) {
	h := newSSOHarness(t, ssoSP())
	b := newTestBrowser(t, h, octocat, hubot)
	nonce := boundChoice(t, h, authn.ChooserKindSAML, testSPEntityID, hubot.ID)
	req := h.request(t, "_multi-force", nil, func(o *authnReqOpts) { o.forceAuthn = true })
	req.URL.RawQuery += "&" + accountChoiceParam + "=" + nonce

	rec := httptest.NewRecorder()
	h.idp.HandleSSO(rec, b.attach(req))
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 to the sign-in page", rec.Code)
	}
	loc, _ := url.Parse(rec.Header().Get("Location"))
	if loc.Path != "/login" || loc.Query().Get("login_hint") != hubot.Username {
		t.Fatalf("Location = %q, want the sign-in page for %s", rec.Header().Get("Location"), hubot.Username)
	}
	rt, _ := url.Parse(loc.Query().Get("return_to"))
	if rt.Query().Get(accountChoiceParam) != nonce || rt.Query().Get("reauth") == "" {
		t.Errorf("return_to = %q, want it to keep the account choice and carry a reauth nonce", rt)
	}
	if len(h.q.sessions()) != 0 {
		t.Error("ForceAuthn issued before the chosen account re-authenticated")
	}
}

// One account signed in: no chooser, behaviour as before.
func TestSSOSingleAccountBrowserSkipsChooser(t *testing.T) {
	h := newSSOHarness(t, ssoSP())
	b := newTestBrowser(t, h, hubot)

	rec := httptest.NewRecorder()
	h.idp.HandleSSO(rec, b.attach(h.request(t, "_single", nil)))
	assertIssuedTo(t, b, rec, hubot)
}

func TestSSOMaintenanceChecksChosenAccount(t *testing.T) {
	admin := db.Account{ID: 50, Username: "admin", Role: "admin"}
	cases := []struct {
		name   string
		chosen db.Account
		issued bool
	}{
		{"member chosen", hubot, false},
		{"admin chosen", admin, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newSSOHarness(t, ssoSP())
			h.idp.SetMaintenanceChecker(func(context.Context) bool { return true })
			b := newTestBrowser(t, h, admin, hubot)
			req := h.request(t, "_multi-maint", nil)
			req.URL.RawQuery += "&" + accountChoiceParam + "=" + boundChoice(t, h, authn.ChooserKindSAML, testSPEntityID, c.chosen.ID)

			rec := httptest.NewRecorder()
			h.idp.HandleSSO(rec, b.attach(req))
			if c.issued {
				assertIssuedTo(t, b, rec, c.chosen)
				return
			}
			if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/" {
				t.Fatalf("status = %d Location = %q, want the maintenance bounce to /", rec.Code, rec.Header().Get("Location"))
			}
			if len(h.q.sessions()) != 0 {
				t.Error("issued to a non-admin during maintenance")
			}
		})
	}
}

func TestSSOMultiAccountAccessDeniedPageCarriesAccountRef(t *testing.T) {
	sp := ssoSP()
	sp.DisplayName = "Acme"
	h := newSSOHarness(t, sp)
	h.q.denied = true
	b := newTestBrowser(t, h, octocat, hubot)
	req := h.request(t, "_multi-denied", nil)
	req.URL.RawQuery += "&" + accountChoiceParam + "=" + boundChoice(t, h, authn.ChooserKindSAML, testSPEntityID, hubot.ID)

	rec := httptest.NewRecorder()
	h.idp.HandleSSO(rec, b.attach(req))
	loc, _ := url.Parse(rec.Header().Get("Location"))
	want := h.idp.refs.Shorts([]int32{octocat.ID, hubot.ID})[hubot.ID]
	if loc.Path != "/error" || loc.Query().Get("reason") != "app_access_denied" || loc.Query().Get("u") != want {
		t.Errorf("Location = %q, want the access page for %s's u %q", rec.Header().Get("Location"), hubot.Username, want)
	}
}

func TestSSOInitMultiAccountGoesToChooser(t *testing.T) {
	sp := ssoSP()
	sp.AllowIdpInitiated = true
	h := newSSOHarness(t, sp)
	b := newTestBrowser(t, h, octocat, hubot)

	rec := httptest.NewRecorder()
	h.idp.HandleIdPInitiated(rec, b.get(testIdPOrigin+"/saml/sso/init?sp="+url.QueryEscape(testSPEntityID)+"&RelayState=deep"))
	nonce, ticket := chooserTicket(t, h, rec)
	if ticket.Kind != authn.ChooserKindSAMLInit || ticket.App != testSPEntityID {
		t.Errorf("ticket = %+v, want a saml_init ticket for %s", ticket, testSPEntityID)
	}
	if ticket.ResumeURL != "/saml/sso/init?sp="+url.QueryEscape(testSPEntityID)+"&RelayState=deep" {
		t.Errorf("ResumeURL = %q", ticket.ResumeURL)
	}

	rec = httptest.NewRecorder()
	h.idp.HandleIdPInitiated(rec, b.get(choose(t, h, nonce, ticket, hubot.ID)))
	assertIssuedTo(t, b, rec, hubot)
}

// The launcher arrives with a choice already bound to the tab's account.
func TestSSOInitLauncherChoiceIssuesForBoundAccount(t *testing.T) {
	sp := ssoSP()
	sp.AllowIdpInitiated = true
	h := newSSOHarness(t, sp)
	b := newTestBrowser(t, h, octocat, hubot)
	nonce := boundChoice(t, h, authn.ChooserKindSAMLInit, testSPEntityID, hubot.ID)

	rec := httptest.NewRecorder()
	h.idp.HandleIdPInitiated(rec, b.get(testIdPOrigin+"/saml/sso/init?sp="+url.QueryEscape(testSPEntityID)+"&"+accountChoiceParam+"="+nonce))
	assertIssuedTo(t, b, rec, hubot)
}

// armMultiConsent runs an SP-initiated request as accountID with the consent
// gate armed, returning the consent ticket the /saml-consent redirect names.
func armMultiConsent(t *testing.T, b *testBrowser, accountID int32) string {
	t.Helper()
	h := b.h
	h.q.noConsent = true
	req := h.request(t, "_multi-consent", nil)
	req.URL.RawQuery += "&" + accountChoiceParam + "=" + boundChoice(t, h, authn.ChooserKindSAML, testSPEntityID, accountID)
	rec := httptest.NewRecorder()
	h.idp.HandleSSO(rec, b.attach(req))
	if rec.Code != http.StatusFound {
		t.Fatalf("arm: status = %d, want 302 to the consent page", rec.Code)
	}
	loc, _ := url.Parse(rec.Header().Get("Location"))
	if loc.Path != "/saml-consent" {
		t.Fatalf("arm: Location = %q, want the consent page", rec.Header().Get("Location"))
	}
	if want := h.idp.refs.Shorts(b.accountIDs())[accountID]; loc.Query().Get("u") != want {
		t.Errorf("consent page u = %q, want %q", loc.Query().Get("u"), want)
	}
	return loc.Query().Get("ticket")
}

func (b *testBrowser) accountIDs() []int32 {
	ids := make([]int32, len(b.entries))
	for i, e := range b.entries {
		ids[i] = e.AccountID
	}
	return ids
}

// The resume answers as the account the consent ticket was made for, not the
// browser's default account.
func TestHandleConsentResumeUsesTicketAccount(t *testing.T) {
	h := newSSOHarness(t, ssoSP())
	b := newTestBrowser(t, h, octocat, hubot)
	consent := armMultiConsent(t, b, hubot.ID)

	rec := httptest.NewRecorder()
	h.idp.HandleConsentResume(rec, b.get(testIdPOrigin+"/saml/sso/resume?ticket="+url.QueryEscape(consent)))
	assertIssuedTo(t, b, rec, hubot)
	if acks := h.q.consents(); len(acks) != 1 || acks[0].AccountID != hubot.ID {
		t.Errorf("consent acks = %+v, want one for %s", acks, hubot.Username)
	}
}

// "Use another account" on the consent page comes back to the resume with a
// choice naming another account: the original ticket is spent and the request
// continues as the chosen account, which is refused, asked for its own
// acknowledgement, or answered.
func TestHandleConsentResumeSwitchedAccount(t *testing.T) {
	cases := []struct {
		name      string
		denied    bool
		noConsent bool
	}{
		{name: "no access", denied: true},
		{name: "needs consent", noConsent: true},
		{name: "issues directly"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newSSOHarness(t, ssoSP())
			b := newTestBrowser(t, h, octocat, hubot)
			consent := armMultiConsent(t, b, octocat.ID)
			h.q.denied, h.q.noConsent = c.denied, c.noConsent
			choice := boundChoice(t, h, authn.ChooserKindSAML, testSPEntityID, hubot.ID)

			rec := httptest.NewRecorder()
			h.idp.HandleConsentResume(rec, b.get(testIdPOrigin+"/saml/sso/resume?ticket="+url.QueryEscape(consent)+"&"+accountChoiceParam+"="+choice))

			if left, _ := authn.ReadSAMLConsent(context.Background(), h.idp.kv, consent); left != nil {
				t.Error("the original consent ticket was not spent")
			}
			u := h.idp.refs.Shorts(b.accountIDs())[hubot.ID]
			loc, _ := url.Parse(rec.Header().Get("Location"))
			switch {
			case c.denied:
				if loc == nil || loc.Path != "/error" || loc.Query().Get("reason") != "app_access_denied" || loc.Query().Get("u") != u {
					t.Fatalf("Location = %q, want the access page for %s", rec.Header().Get("Location"), hubot.Username)
				}
				if len(h.q.sessions()) != 0 {
					t.Error("a refused account got an assertion")
				}
			case c.noConsent:
				if loc == nil || loc.Path != "/saml-consent" || loc.Query().Get("u") != u {
					t.Fatalf("Location = %q, want the consent page for %s", rec.Header().Get("Location"), hubot.Username)
				}
				next, _ := authn.ReadSAMLConsent(context.Background(), h.idp.kv, loc.Query().Get("ticket"))
				if next == nil || next.AccountID != hubot.ID || next.ACSURL != testACSURL || next.InResponseTo != "_multi-consent" {
					t.Errorf("new consent ticket = %+v, want one for %s answering the same request", next, hubot.Username)
				}
			default:
				assertIssuedTo(t, b, rec, hubot)
			}
		})
	}
}

// A request that demanded ForceAuthn has the switched-to account sign in
// again before the original ticket is spent.
func TestHandleConsentResumeSwitchedAccountForceAuthn(t *testing.T) {
	h := newSSOHarness(t, ssoSP())
	b := newTestBrowser(t, h, octocat, hubot)
	consent, err := authn.DemandSAMLConsent(context.Background(), h.idp.kv, authn.SAMLConsentTicket{
		AccountID: octocat.ID, SPID: ssoSP().ID, EntityID: testSPEntityID, ACSURL: testACSURL, InResponseTo: "_force", ForceAuthn: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	choice := boundChoice(t, h, authn.ChooserKindSAML, testSPEntityID, hubot.ID)

	rec := httptest.NewRecorder()
	h.idp.HandleConsentResume(rec, b.get(testIdPOrigin+"/saml/sso/resume?ticket="+url.QueryEscape(consent)+"&"+accountChoiceParam+"="+choice))
	loc, _ := url.Parse(rec.Header().Get("Location"))
	if loc == nil || loc.Path != "/login" || loc.Query().Get("login_hint") != hubot.Username {
		t.Fatalf("Location = %q, want the sign-in page for %s", rec.Header().Get("Location"), hubot.Username)
	}
	rt, _ := url.Parse(loc.Query().Get("return_to"))
	if rt.Path != "/saml/sso/resume" || rt.Query().Get("reauth") == "" || rt.Query().Get(accountChoiceParam) != choice {
		t.Errorf("return_to = %q, want the resume address with the choice and a reauth nonce", rt)
	}
	if left, _ := authn.ReadSAMLConsent(context.Background(), h.idp.kv, consent); left == nil {
		t.Error("the original ticket was spent before the re-authentication")
	}
}
