// Package server — multi_account_test.go
//
// Handler tests for several accounts signed in on one browser (PHB-285): the
// account list, signing out one or all accounts, every sign-in entry point
// adding, replacing or refusing an entry, and the full-page navigations that
// must act as a particular account rather than the default one.
package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/credential/enrollment"
	"prohibitorum/pkg/credential/pairing"
	"prohibitorum/pkg/credential/totp"
	"prohibitorum/pkg/db"
	sessstore "prohibitorum/pkg/session"
)

// testBrowser keeps one browser's two session cookies across requests.
type testBrowser struct {
	t       *testing.T
	s       *Server
	cookies map[string]string
	nextSeq int64
}

func newTestBrowser(t *testing.T, s *Server) *testBrowser {
	return &testBrowser{t: t, s: s, cookies: map[string]string{}, nextSeq: 1000}
}

func (b *testBrowser) cookieName(remember bool) string {
	if remember {
		return sessstore.PersistentCookieName(b.s.config)
	}
	return sessstore.TempCookieName(b.s.config)
}

// signIn issues a session for accountID and adds its entry at the end, as an
// earlier sign-in on this browser would have. It returns the session token.
func (b *testBrowser) signIn(accountID int32, remember bool) string {
	b.t.Helper()
	token, _, err := b.s.sessionStore.Issue(context.Background(), accountID, "127.0.0.1", "ua/test", []string{"hwk"}, nil, remember)
	if err != nil {
		b.t.Fatalf("issue session for %d: %v", accountID, err)
	}
	name := b.cookieName(remember)
	entries := append(sessstore.DecodeCookie(b.cookies[name]), sessstore.Entry{Seq: b.nextSeq, AccountID: accountID, Token: token})
	b.nextSeq++
	b.cookies[name] = sessstore.EncodeCookie(entries)
	return token
}

func (b *testBrowser) httpCookies() []*http.Cookie {
	var out []*http.Cookie
	for name, value := range b.cookies {
		out = append(out, &http.Cookie{Name: name, Value: value, Path: "/"})
	}
	return out
}

// serve runs h behind the session middleware as this browser and keeps the
// cookies the response sets.
func (b *testBrowser) serve(h http.HandlerFunc, req *http.Request) *httptest.ResponseRecorder {
	b.t.Helper()
	for _, c := range b.httpCookies() {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	b.s.browserSessions().LoadSession()(h).ServeHTTP(rec, req)
	b.absorb(rec.Result().Cookies())
	return rec
}

func (b *testBrowser) absorb(cookies []*http.Cookie) {
	for _, c := range cookies {
		if c.Name != b.cookieName(true) && c.Name != b.cookieName(false) {
			continue
		}
		if c.MaxAge < 0 || c.Value == "" {
			delete(b.cookies, c.Name)
			continue
		}
		b.cookies[c.Name] = c.Value
	}
}

type testBrowserEntry struct {
	sessstore.Entry
	remembered bool
}

// entries returns the browser's entries in sign-in order.
func (b *testBrowser) entries() []testBrowserEntry {
	var out []testBrowserEntry
	for _, remember := range []bool{true, false} {
		for _, e := range sessstore.DecodeCookie(b.cookies[b.cookieName(remember)]) {
			out = append(out, testBrowserEntry{Entry: e, remembered: remember})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })
	return out
}

func (b *testBrowser) accountIDs() []int32 {
	var ids []int32
	for _, e := range b.entries() {
		ids = append(ids, e.AccountID)
	}
	return ids
}

func (b *testBrowser) entryFor(accountID int32) (testBrowserEntry, bool) {
	for _, e := range b.entries() {
		if e.AccountID == accountID {
			return e, true
		}
	}
	return testBrowserEntry{}, false
}

func sessionLive(s *Server, accountID int32, token string) bool {
	_, _, err := s.sessionStore.Load(context.Background(), accountID, token, "127.0.0.1", "ua/test")
	return err == nil
}

func assertAccounts(t *testing.T, b *testBrowser, want ...int32) {
	t.Helper()
	if got := b.accountIDs(); !slices.Equal(got, want) {
		t.Fatalf("browser accounts = %v, want %v", got, want)
	}
}

func assertErrorCode(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, status, rec.Body.String())
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Code != code {
		t.Fatalf("code = %q (err %v), want %q", body.Code, err, code)
	}
}

func assertNoSessionCookieSet(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if isSessionCookie(c) {
			t.Fatalf("response set session cookie %s=%q", c.Name, c.Value)
		}
	}
}

func sessionEndEvents(events []db.InsertCredentialEventParams) map[int32]string {
	out := map[int32]string{}
	for _, ev := range events {
		if ev.Event != audit.EventSessionEnd || ev.AccountID == nil {
			continue
		}
		var detail map[string]any
		_ = json.Unmarshal(ev.Detail, &detail)
		reason, _ := detail["reason"].(string)
		out[*ev.AccountID] = reason
	}
	return out
}

func withAccountHeader(req *http.Request, accountID int32) *http.Request {
	req.Header.Set(sessstore.AccountHeader, strconv.FormatInt(int64(accountID), 10))
	return req
}

// ----- account list and sign-out ---------------------------------------------

func TestBrowserAccountsListsAccountsInSignInOrder(t *testing.T) {
	s, f, _ := newTestServer(t)
	s.accountLookup = f
	s.config.SessionMaxAccounts = 5
	s.config.PublicOrigins = []string{"https://id.example.com"}
	f.accounts[30] = db.Account{ID: 30, Username: "lin", DisplayName: "Lin"}
	f.accounts[10] = db.Account{ID: 10, Username: "zhao", DisplayName: "Zhao", AvatarEtag: pgtype.Text{String: "abc", Valid: true}, OidcSubject: pgtype.UUID{Bytes: [16]byte{1}, Valid: true}}
	f.accounts[20] = db.Account{ID: 20, Username: "wu", DisplayName: "Wu"}
	b := newTestBrowser(t, s)
	b.signIn(30, true)
	b.signIn(10, false)
	b.signIn(20, true)

	list := func(w http.ResponseWriter, r *http.Request) {
		out, err := s.handleListBrowserAccounts(r.Context(), nil)
		if err != nil {
			t.Fatal(err)
		}
		writeJSON(w, out.Body)
	}
	// The header selects no account for the list: it is the browser's.
	rec := b.serve(list, withAccountHeader(httptest.NewRequest(http.MethodGet, "/api/prohibitorum/auth/accounts", nil), 20))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var got contract.BrowserAccounts
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Limit != 5 {
		t.Errorf("limit = %d, want 5", got.Limit)
	}
	shorts := s.refs().Shorts([]int32{30, 10, 20})
	want := []struct {
		id                    int32
		username, displayName string
	}{{30, "lin", "Lin"}, {10, "zhao", "Zhao"}, {20, "wu", "Wu"}}
	if len(got.Accounts) != len(want) {
		t.Fatalf("accounts = %+v", got.Accounts)
	}
	for i, w := range want {
		a := got.Accounts[i]
		if a.AccountID != w.id || a.Username != w.username || a.DisplayName != w.displayName {
			t.Errorf("accounts[%d] = %+v, want %d %s %s", i, a, w.id, w.username, w.displayName)
		}
		if a.U == "" || a.U != shorts[w.id] {
			t.Errorf("accounts[%d].u = %q, want %q", i, a.U, shorts[w.id])
		}
	}
	if got.Accounts[1].AvatarURL == nil || !strings.HasPrefix(*got.Accounts[1].AvatarURL, "https://id.example.com/") {
		t.Errorf("avatarUrl = %v, want the public avatar address", got.Accounts[1].AvatarURL)
	}
}

func TestLogoutSignsOutOnlyTheSelectedAccount(t *testing.T) {
	t.Run("header names the account", func(t *testing.T) {
		s, f, _ := newTestServer(t)
		b := newTestBrowser(t, s)
		tokA := b.signIn(1, false)
		tokB := b.signIn(2, true)

		rec := b.serve(s.handleLogoutHTTP, withAccountHeader(httptest.NewRequest(http.MethodPost, "/api/prohibitorum/auth/logout", nil), 2))
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d", rec.Code)
		}
		assertAccounts(t, b, 1)
		if sessionLive(s, 2, tokB) || !sessionLive(s, 1, tokA) {
			t.Fatalf("live: account 1 %v, account 2 %v; want only account 1", sessionLive(s, 1, tokA), sessionLive(s, 2, tokB))
		}
		if ends := sessionEndEvents(f.events); len(ends) != 1 || ends[2] != "logout" {
			t.Fatalf("session_end audits = %v, want account 2 / logout", ends)
		}
	})
	t.Run("no header signs out the default account", func(t *testing.T) {
		s, f, _ := newTestServer(t)
		b := newTestBrowser(t, s)
		tokA := b.signIn(1, false)
		tokB := b.signIn(2, false)

		rec := b.serve(s.handleLogoutHTTP, httptest.NewRequest(http.MethodPost, "/api/prohibitorum/auth/logout", nil))
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d", rec.Code)
		}
		assertAccounts(t, b, 2)
		if sessionLive(s, 1, tokA) || !sessionLive(s, 2, tokB) {
			t.Fatal("default account still signed in, or the other account was signed out")
		}
		if ends := sessionEndEvents(f.events); len(ends) != 1 || ends[1] != "logout" {
			t.Fatalf("session_end audits = %v", ends)
		}
	})
	t.Run("account not signed in here", func(t *testing.T) {
		s, f, _ := newTestServer(t)
		b := newTestBrowser(t, s)
		tok := b.signIn(1, false)

		rec := b.serve(s.handleLogoutHTTP, withAccountHeader(httptest.NewRequest(http.MethodPost, "/api/prohibitorum/auth/logout", nil), 99))
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d", rec.Code)
		}
		assertNoSessionCookieSet(t, rec)
		assertAccounts(t, b, 1)
		if !sessionLive(s, 1, tok) || len(sessionEndEvents(f.events)) != 0 {
			t.Fatal("signing out an account that is not here changed the browser")
		}

		empty := newTestBrowser(t, s)
		rec = empty.serve(s.handleLogoutHTTP, httptest.NewRequest(http.MethodPost, "/api/prohibitorum/auth/logout", nil))
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status without any session = %d, want 204", rec.Code)
		}
	})
}

func TestLogoutAllClearsBothCookiesAndAuditsEachAccount(t *testing.T) {
	s, f, _ := newTestServer(t)
	b := newTestBrowser(t, s)
	tokA := b.signIn(1, true)
	tokB := b.signIn(2, false)

	rec := b.serve(s.handleLogoutAllHTTP, httptest.NewRequest(http.MethodPost, "/api/prohibitorum/auth/logout-all", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rec.Code)
	}
	cleared := map[string]bool{}
	for _, c := range rec.Result().Cookies() {
		if isSessionCookie(c) && c.MaxAge < 0 && c.Value == "" {
			cleared[c.Name] = true
		}
	}
	if !cleared[b.cookieName(true)] || !cleared[b.cookieName(false)] {
		t.Fatalf("cleared cookies = %v, want both session cookies", cleared)
	}
	if sessionLive(s, 1, tokA) || sessionLive(s, 2, tokB) {
		t.Fatal("a session survived logout-all")
	}
	ends := sessionEndEvents(f.events)
	if len(ends) != 2 || ends[1] != "logout_all" || ends[2] != "logout_all" {
		t.Fatalf("session_end audits = %v, want one logout_all per account", ends)
	}
}

// ----- sign-in entry points ----------------------------------------------------

// totpVerifyRequest prepares a password+TOTP second step for accountID.
func totpVerifyRequest(t *testing.T, s *Server, f *fakeAuthQueries, dek []byte, accountID int32, remember bool, returnTo string) *http.Request {
	t.Helper()
	_ = seedConfirmedTOTP(t, s, f, dek, accountID)
	token := mustToken(t)
	stashPartialSessionOpt(t, s, token, accountID, remember)
	at := (time.Now().Unix()/30+1)*30 + 15
	code := totp.ComputeCodeForTesting(decryptTOTPSecret(t, dek, *f.totpRow, accountID), at, 6)
	path := "/api/prohibitorum/auth/totp/verify"
	if returnTo != "" {
		path += "?return_to=" + url.QueryEscape(returnTo)
	}
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(fmt.Sprintf(`{"partial_session_token":%q,"code":%q}`, token, code)))
	req.RemoteAddr = "127.0.0.1:5555"
	return req
}

func decodeLoginResult(t *testing.T, rec *httptest.ResponseRecorder) contract.LoginResult {
	t.Helper()
	var out contract.LoginResult
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
	return out
}

func TestTOTPVerifySecondAccountKeepsBoth(t *testing.T) {
	s, f, dek := newTestServer(t)
	b := newTestBrowser(t, s)
	tokFirst := b.signIn(7, false)

	rec := b.serve(s.handleTOTPVerifyHTTP, totpVerifyRequest(t, s, f, dek, 42, false, "/apps"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertAccounts(t, b, 7, 42)
	if !sessionLive(s, 7, tokFirst) {
		t.Fatal("the first account was signed out by the second sign-in")
	}
	u := s.refs().Shorts([]int32{7, 42})[42]
	result := decodeLoginResult(t, rec)
	if result.Redirect != "/apps?u="+u || result.Account == nil || result.Account.AccountID != 42 || result.Account.U != u {
		t.Fatalf("result = %+v (account %+v), want /apps?u=%s for account 42", result, result.Account, u)
	}
}

// The passkey completion signs in through issueBrowserSession like every
// other entry point; its own ceremony needs a real authenticator and the
// database, so the browser behaviour is exercised here directly.
func TestPasskeySignInSecondAccountKeepsBoth(t *testing.T) {
	s, f, _ := newTestServer(t)
	b := newTestBrowser(t, s)
	tokFirst := b.signIn(7, false)
	sessionsBefore := len(f.sessions)

	rec := b.serve(func(w http.ResponseWriter, r *http.Request) {
		if _, err := s.issueBrowserSession(w, r, sessstore.AttachParams{AccountID: 42, AMR: []string{"hwk"}}, ""); err != nil {
			writeAuthErr(w, err)
			return
		}
		writeJSON(w, s.loginResult(r, 42, ""))
	}, httptest.NewRequest(http.MethodPost, "/api/prohibitorum/auth/login/complete", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertAccounts(t, b, 7, 42)
	if !sessionLive(s, 7, tokFirst) || len(f.sessions) != sessionsBefore+1 {
		t.Fatal("second passkey sign-in did not add exactly one session beside the first")
	}
}

func TestTOTPVerifySameAccountKeepsPlaceAndRevokesOldSession(t *testing.T) {
	s, f, dek := newTestServer(t)
	b := newTestBrowser(t, s)
	oldToken := b.signIn(42, false)
	b.signIn(7, false)
	before, _ := b.entryFor(42)

	rec := b.serve(s.handleTOTPVerifyHTTP, totpVerifyRequest(t, s, f, dek, 42, false, ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertAccounts(t, b, 42, 7)
	after, _ := b.entryFor(42)
	if after.Seq != before.Seq {
		t.Errorf("seq = %d, want the original %d", after.Seq, before.Seq)
	}
	if after.Token == oldToken || !sessionLive(s, 42, after.Token) {
		t.Fatal("the entry does not carry the new session")
	}
	if sessionLive(s, 42, oldToken) {
		t.Fatal("the replaced session is still live")
	}
}

func TestTOTPVerifyRememberChangeMovesEntryToOtherCookie(t *testing.T) {
	s, f, dek := newTestServer(t)
	b := newTestBrowser(t, s)
	b.signIn(42, false)
	b.signIn(7, false)
	before, _ := b.entryFor(42)

	rec := b.serve(s.handleTOTPVerifyHTTP, totpVerifyRequest(t, s, f, dek, 42, true, ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	after, ok := b.entryFor(42)
	if !ok || !after.remembered || after.Seq != before.Seq {
		t.Fatalf("entry = %+v (found %v), want it in the remembered cookie at seq %d", after, ok, before.Seq)
	}
	for _, e := range sessstore.DecodeCookie(b.cookies[b.cookieName(false)]) {
		if e.AccountID == 42 {
			t.Fatal("the entry is still in the temporary cookie")
		}
	}
	assertAccounts(t, b, 42, 7)
}

// fullBrowser returns a browser at a cap of two accounts, holding 1 and 2.
func fullBrowser(t *testing.T, s *Server) *testBrowser {
	t.Helper()
	s.config.SessionMaxAccounts = 2
	b := newTestBrowser(t, s)
	b.signIn(1, false)
	b.signIn(2, false)
	return b
}

func TestSignInAtAccountLimitIssuesNothing(t *testing.T) {
	t.Run("TOTP verify", func(t *testing.T) {
		s, f, dek := newTestServer(t)
		b := fullBrowser(t, s)
		req := totpVerifyRequest(t, s, f, dek, 42, false, "")
		sessionsBefore := len(f.sessions)

		rec := b.serve(s.handleTOTPVerifyHTTP, req)
		assertErrorCode(t, rec, http.StatusConflict, "account_limit_reached")
		assertNoSessionCookieSet(t, rec)
		if len(f.sessions) != sessionsBefore {
			t.Fatalf("sessions issued = %d, want 0", len(f.sessions)-sessionsBefore)
		}
		assertAccounts(t, b, 1, 2)
	})
	t.Run("recovery-code verify", func(t *testing.T) {
		s, f, dek := newTestServer(t)
		b := fullBrowser(t, s)
		codes := seedConfirmedTOTP(t, s, f, dek, 42)
		token := mustToken(t)
		stashPartialSession(t, s, token, 42)
		sessionsBefore := len(f.sessions)

		req := httptest.NewRequest(http.MethodPost, "/api/prohibitorum/auth/recovery-code/verify",
			strings.NewReader(fmt.Sprintf(`{"partial_session_token":%q,"code":%q,"reset_authenticator":false}`, token, codes[0])))
		rec := b.serve(s.handleRecoveryCodeVerifyHTTP, req)
		assertErrorCode(t, rec, http.StatusConflict, "account_limit_reached")
		assertNoSessionCookieSet(t, rec)
		if len(f.sessions) != sessionsBefore {
			t.Fatalf("sessions issued = %d, want 0", len(f.sessions)-sessionsBefore)
		}
	})
	t.Run("passkey sign-in", func(t *testing.T) {
		s, f, _ := newTestServer(t)
		b := fullBrowser(t, s)
		sessionsBefore := len(f.sessions)

		rec := b.serve(func(w http.ResponseWriter, r *http.Request) {
			if _, err := s.issueBrowserSession(w, r, sessstore.AttachParams{AccountID: 42, AMR: []string{"hwk"}}, ""); err != nil {
				writeAuthErr(w, err)
				return
			}
			w.WriteHeader(http.StatusOK)
		}, httptest.NewRequest(http.MethodPost, "/api/prohibitorum/auth/login/complete", nil))
		assertErrorCode(t, rec, http.StatusConflict, "account_limit_reached")
		assertNoSessionCookieSet(t, rec)
		if len(f.sessions) != sessionsBefore {
			t.Fatalf("sessions issued = %d, want 0", len(f.sessions)-sessionsBefore)
		}
	})
	t.Run("pairing complete keeps the pairing", func(t *testing.T) {
		s, f := newPairingTestServer(t)
		b := fullBrowser(t, s)
		pairingID := approvedPairing(t, s, 42)
		sessionsBefore := len(f.sessions)

		rec := b.serve(s.handlePairCompleteHTTP, pairCompleteRequest(t, pairingID, ""))
		assertErrorCode(t, rec, http.StatusConflict, "account_limit_reached")
		assertNoSessionCookieSet(t, rec)
		if len(f.sessions) != sessionsBefore {
			t.Fatalf("sessions issued = %d, want 0", len(f.sessions)-sessionsBefore)
		}
		p, err := s.pairingStore.GetByID(context.Background(), pairingID)
		if err != nil || p.Status != pairing.PairingApproved {
			t.Fatalf("pairing after refusal = %+v (err %v), want still approved", p, err)
		}
	})
	t.Run("re-authenticating an account already here is not refused", func(t *testing.T) {
		s, f, dek := newTestServer(t)
		s.config.SessionMaxAccounts = 2
		b := newTestBrowser(t, s)
		b.signIn(42, false)
		b.signIn(2, false)

		rec := b.serve(s.handleTOTPVerifyHTTP, totpVerifyRequest(t, s, f, dek, 42, false, ""))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
		}
		assertAccounts(t, b, 42, 2)
	})
}

func approvedPairing(t *testing.T, s *Server, accountID int32) string {
	t.Helper()
	ctx := context.Background()
	p, err := s.pairingStore.New(ctx, "ua/test", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := s.pairingStore.LookupByCode(ctx, p.Code)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.pairingStore.Approve(ctx, loaded, accountID); err != nil {
		t.Fatal(err)
	}
	return p.ID
}

func TestPairCompleteAddsAccountBesideSignedInOne(t *testing.T) {
	s, _ := newPairingTestServer(t)
	b := newTestBrowser(t, s)
	b.signIn(5, false)
	pairingID := approvedPairing(t, s, 42)

	rec := b.serve(s.handlePairCompleteHTTP, pairCompleteRequest(t, pairingID, "/devices"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertAccounts(t, b, 5, 42)
	var resp pairCompleteResp
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	u := s.refs().Shorts([]int32{5, 42})[42]
	if resp.Redirect != "/devices?u="+u || resp.Account == nil || resp.Account.AccountID != 42 {
		t.Fatalf("response = %+v, want /devices?u=%s for account 42", resp, u)
	}
}

func TestLoginResultCarriesUOnlyForConsolePathsWithSeveralAccounts(t *testing.T) {
	s, _, _ := newTestServer(t)
	results := func(b *testBrowser) map[string]string {
		got := map[string]string{}
		b.serve(func(w http.ResponseWriter, r *http.Request) {
			for _, rt := range []string{"/apps", "/oauth/authorize?client_id=wiki", "/saml/sso?post=k", "/api/prohibitorum/me"} {
				got[rt] = s.loginResult(r, 42, rt).Redirect
			}
		}, httptest.NewRequest(http.MethodPost, "/api/prohibitorum/auth/totp/verify", nil))
		return got
	}

	one := newTestBrowser(t, s)
	one.signIn(42, false)
	for rt, got := range results(one) {
		if got != rt {
			t.Errorf("one account: %s → %q, want unchanged", rt, got)
		}
	}

	two := newTestBrowser(t, s)
	two.signIn(7, false)
	two.signIn(42, false)
	u := s.refs().Shorts([]int32{7, 42})[42]
	got := results(two)
	if got["/apps"] != "/apps?u="+u {
		t.Errorf("console path = %q, want /apps?u=%s", got["/apps"], u)
	}
	for _, rt := range []string{"/oauth/authorize?client_id=wiki", "/saml/sso?post=k", "/api/prohibitorum/me"} {
		if got[rt] != rt {
			t.Errorf("protocol/API path %s → %q, want unchanged", rt, got[rt])
		}
	}
}

// ----- enrollment ---------------------------------------------------------------

func enrollmentConsumed(q *vrchatEnrollmentQueries, token string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.enrollments[token].ConsumedAt.Valid
}

func enrollmentAccountCount(q *vrchatEnrollmentQueries) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.accounts)
}

func TestEnrollmentCompleteAtLimitKeepsInvitation(t *testing.T) {
	t.Run("passkey", func(t *testing.T) {
		e := verifiedInviteEnrollment("limit-passkey")
		s, q := newAtomicVRChatEnrollmentServer(t, e, nil)
		s.config.SessionMaxAccounts = 1
		b := newTestBrowser(t, s)
		b.signIn(500, false)
		beginVerifiedInvite(t, s, e, "limit-user", "Limit User")

		rec := b.serve(s.handleEnrollmentCompleteHTTP, completeEnrollmentRequest(e.Token))
		assertErrorCode(t, rec, http.StatusConflict, "account_limit_reached")
		assertNoSessionCookieSet(t, rec)
		if enrollmentConsumed(q, e.Token) || enrollmentAccountCount(q) != 0 {
			t.Fatalf("refused completion consumed=%v accounts=%d, want the invitation untouched", enrollmentConsumed(q, e.Token), enrollmentAccountCount(q))
		}

		// The same invitation completes once the browser has room.
		empty := newTestBrowser(t, s)
		rec = empty.serve(s.handleEnrollmentCompleteHTTP, completeEnrollmentRequest(e.Token))
		if rec.Code != http.StatusOK {
			t.Fatalf("retry status = %d body=%s", rec.Code, rec.Body.String())
		}
		if !enrollmentConsumed(q, e.Token) {
			t.Fatal("retry did not consume the invitation")
		}
	})
	t.Run("password and TOTP", func(t *testing.T) {
		e := verifiedInviteEnrollment("limit-password-totp")
		s, q := newAtomicVRChatEnrollmentServer(t, e, nil)
		s.config.SessionMaxAccounts = 1
		b := newTestBrowser(t, s)
		b.signIn(500, false)
		secret, code := passwordTOTPCandidate(t, s)

		rec := b.serve(s.handleEnrollmentPasswordTOTPVerifyHTTP, passwordTOTPVerifyRequest(e.Token, "limit-user", "trustno1pass!", secret, code))
		assertErrorCode(t, rec, http.StatusConflict, "account_limit_reached")
		assertNoSessionCookieSet(t, rec)
		if enrollmentConsumed(q, e.Token) || enrollmentAccountCount(q) != 0 {
			t.Fatal("refused completion consumed the invitation or created an account")
		}
		// Re-read the row: it is still redeemable.
		if _, err := enrollment.LoadEnrollment(context.Background(), q, e.Token); err != nil {
			t.Fatalf("invitation after refusal: %v", err)
		}
	})
}

type enrollmentCompletionBody struct {
	Redirect string                    `json:"redirect"`
	Account  *contract.SignedInAccount `json:"account"`
}

func TestEnrollmentCompleteWhileSignedInAppendsAccount(t *testing.T) {
	e := verifiedInviteEnrollment("signed-in-invite")
	s, q := newAtomicVRChatEnrollmentServer(t, e, nil)
	b := newTestBrowser(t, s)
	tokFirst := b.signIn(500, false)
	beginVerifiedInvite(t, s, e, "second-user", "Second User")

	rec := b.serve(s.handleEnrollmentCompleteHTTP, completeEnrollmentRequest(e.Token))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var newID int32
	q.mu.Lock()
	for id := range q.accounts {
		newID = id
	}
	q.mu.Unlock()
	assertAccounts(t, b, 500, newID)
	if !sessionLive(s, 500, tokFirst) {
		t.Fatal("enrolling signed the existing account out")
	}
	var body enrollmentCompletionBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	u := s.refs().Shorts([]int32{500, newID})[newID]
	if body.Redirect != "/?u="+u || body.Account == nil || body.Account.AccountID != newID || body.Account.U != u {
		t.Fatalf("body = %+v (account %+v), want / with the new account's u %s", body, body.Account, u)
	}

	// A browser that held no account gets / without u.
	e2 := verifiedInviteEnrollment("signed-out-invite")
	s2, _ := newAtomicVRChatEnrollmentServer(t, e2, nil)
	beginVerifiedInvite(t, s2, e2, "only-user", "Only User")
	rec = newTestBrowser(t, s2).serve(s2.handleEnrollmentCompleteHTTP, completeEnrollmentRequest(e2.Token))
	var single enrollmentCompletionBody
	if err := json.Unmarshal(rec.Body.Bytes(), &single); err != nil || single.Redirect != "/" {
		t.Fatalf("single-account body = %+v (err %v), want redirect /", single, err)
	}
}

func TestEnrollmentResetOfSignedInAccountKeepsPlaceAtLimit(t *testing.T) {
	account := db.Account{ID: 73, Username: "reset-user", DisplayName: "Reset User", WebauthnUserHandle: []byte("handle")}
	e := pendingEnrollment("reset-signed-in", enrollment.IntentReset)
	e.TargetAccountID = pgtype.Int4{Int32: account.ID, Valid: true}
	s, _ := newAtomicVRChatEnrollmentServer(t, e, &account)
	s.config.SessionMaxAccounts = 2
	b := newTestBrowser(t, s)
	oldToken := b.signIn(73, false)
	b.signIn(80, false)
	before, _ := b.entryFor(73)

	w := httptest.NewRecorder()
	s.handleEnrollmentBeginHTTP(w, beginEnrollmentRequest(e.Token, `{}`))
	if w.Code != http.StatusOK {
		t.Fatalf("begin status = %d body=%s", w.Code, w.Body.String())
	}
	rec := b.serve(s.handleEnrollmentCompleteHTTP, completeEnrollmentRequest(e.Token))
	if rec.Code != http.StatusOK {
		t.Fatalf("complete status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertAccounts(t, b, 73, 80)
	after, _ := b.entryFor(73)
	if after.Seq != before.Seq || after.Token == oldToken || !sessionLive(s, 73, after.Token) {
		t.Fatalf("reset entry = %+v, want a new session at seq %d", after, before.Seq)
	}
}

func TestEnrollmentPreviewReportsAtLimit(t *testing.T) {
	account := db.Account{ID: 73, Username: "reset-user", DisplayName: "Reset User"}
	reset := pendingEnrollment("preview-reset", enrollment.IntentReset)
	reset.TargetAccountID = pgtype.Int4{Int32: account.ID, Valid: true}
	invite := pendingEnrollment("preview-invite", enrollment.IntentInvite)
	s, q := newAtomicVRChatEnrollmentServer(t, reset, &account)
	q.enrollments[invite.Token] = invite

	preview := func(b *testBrowser, token string) bool {
		t.Helper()
		var atLimit bool
		rec := b.serve(func(w http.ResponseWriter, r *http.Request) {
			out, err := s.handlePreviewEnrollment(r.Context(), &previewIn{Token: token})
			if err != nil {
				t.Fatalf("preview %s: %v", token, err)
			}
			atLimit = out.Body.AtLimit
		}, httptest.NewRequest(http.MethodGet, "/api/prohibitorum/enrollments/"+token, nil))
		_ = rec
		return atLimit
	}

	s.config.SessionMaxAccounts = 2
	roomy := newTestBrowser(t, s)
	roomy.signIn(80, false)
	if preview(roomy, invite.Token) || preview(roomy, reset.Token) {
		t.Fatal("atLimit with room for another account")
	}

	full := newTestBrowser(t, s)
	full.signIn(80, false)
	full.signIn(81, false)
	if !preview(full, invite.Token) || !preview(full, reset.Token) {
		t.Fatal("atLimit false on a full browser without the target account")
	}

	holdsTarget := newTestBrowser(t, s)
	holdsTarget.signIn(73, false)
	holdsTarget.signIn(80, false)
	if preview(holdsTarget, reset.Token) {
		t.Fatal("a reset of an account signed in here reports atLimit")
	}
	if !preview(holdsTarget, invite.Token) {
		t.Fatal("an invitation on the same full browser does not report atLimit")
	}
}

// ----- federation ---------------------------------------------------------------

// fillFederationBrowser signs accounts 1 and 2 into the federation harness's
// browser and caps it at two.
func fillFederationBrowser(t *testing.T, h *fedTestHarness) {
	t.Helper()
	h.s.config.SessionMaxAccounts = 2
	b := newTestBrowser(t, h.s)
	b.signIn(1, false)
	b.signIn(2, false)
	origin, _ := url.Parse(h.srvTS.URL)
	h.client.Jar.SetCookies(origin, b.httpCookies())
}

func (h *fedTestHarness) completeUpstreamLogin(t *testing.T) *http.Response {
	t.Helper()
	loc, resp := h.driveLogin(t, "mockop", "/me")
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("login status = %d", resp.StatusCode)
	}
	code, state, iss := driveAuthorize(t, loc)
	return h.hitCallback(t, "mockop", url.Values{"code": {code}, "state": {state}, "iss": {iss}})
}

func assertLimitErrorRedirect(t *testing.T, resp *http.Response) {
	t.Helper()
	if resp.StatusCode != http.StatusFound || !strings.HasPrefix(resp.Header.Get("Location"), "/error?error=account_limit_reached") {
		t.Fatalf("callback = %d %q, want /error?error=account_limit_reached", resp.StatusCode, resp.Header.Get("Location"))
	}
	for _, c := range resp.Cookies() {
		if isSessionCookie(c) {
			t.Fatalf("refused callback set %s", c.Name)
		}
	}
}

func TestFederationCallbackAtLimitRedirectsToError(t *testing.T) {
	t.Run("existing account", func(t *testing.T) {
		h := newFederationTestServer(t)
		seedConfirmedIdentity(h)
		fillFederationBrowser(t, h)
		sessionsBefore := len(h.q.sessions)

		assertLimitErrorRedirect(t, h.completeUpstreamLogin(t))
		if len(h.q.sessions) != sessionsBefore {
			t.Fatal("a session was issued at the cap")
		}
	})
	t.Run("existing unconfirmed identity gets no welcome grant", func(t *testing.T) {
		h := newFederationTestServer(t)
		seedConfirmedIdentity(h)
		h.q.identityResult.ConfirmedAt = pgtype.Timestamptz{}
		fillFederationBrowser(t, h)

		assertLimitErrorRedirect(t, h.completeUpstreamLogin(t))
	})
}

func TestFederationFirstLoginAtLimitCreatesNoAccount(t *testing.T) {
	h := newFederationTestServer(t)
	fillFederationBrowser(t, h)

	assertLimitErrorRedirect(t, h.completeUpstreamLogin(t))
	if len(h.q.insertedAccounts) != 0 || len(h.q.insertIdentitys) != 0 {
		t.Fatalf("accounts=%d identities=%d created at the cap, want none", len(h.q.insertedAccounts), len(h.q.insertIdentitys))
	}

	// With room on the browser the same upstream identity provisions.
	h.client.Jar, _ = cookiejar.New(nil)
	resp := h.completeUpstreamLogin(t)
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/welcome" || len(h.q.insertedAccounts) != 1 {
		t.Fatalf("callback with room = %d %q accounts=%d, want /welcome and one account", resp.StatusCode, resp.Header.Get("Location"), len(h.q.insertedAccounts))
	}
}

func TestFederationConfirmAtLimitKeepsGrant(t *testing.T) {
	h := newFederationTestServer(t)
	fx := seedConfirmGrant(t, h)
	h.s.config.SessionMaxAccounts = 1
	b := newTestBrowser(t, h.s)
	b.signIn(1, false)
	confirm := func() *http.Request {
		req := httptest.NewRequest(http.MethodPost, confirmPath, nil)
		req.AddCookie(&http.Cookie{Name: sessstore.FedStateCookieName, Value: fx.cookie})
		return req
	}

	rec := b.serve(h.s.handleFederationConfirmPost, confirm())
	assertErrorCode(t, rec, http.StatusConflict, "account_limit_reached")
	assertNoSessionCookieSet(t, rec)
	if h.q.confirmedIdentityID != 0 || len(h.q.sessions) != 1 {
		t.Fatalf("refused confirm confirmed identity %d, sessions %d", h.q.confirmedIdentityID, len(h.q.sessions))
	}

	// The grant survived: once the browser has room it confirms, and the
	// redirect names the new account beside the one already here.
	h.s.config.SessionMaxAccounts = 2
	h.s.sessions = nil
	rec = b.serve(h.s.handleFederationConfirmPost, confirm())
	if rec.Code != http.StatusOK {
		t.Fatalf("confirm with room = %d body=%s", rec.Code, rec.Body.String())
	}
	assertAccounts(t, b, 1, fx.accountID)
	var body struct {
		Redirect string                    `json:"redirect"`
		Account  *contract.SignedInAccount `json:"account"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	u := h.s.refs().Shorts([]int32{1, fx.accountID})[fx.accountID]
	if body.Redirect != "/me?u="+u || body.Account == nil || body.Account.AccountID != fx.accountID {
		t.Fatalf("body = %+v, want /me?u=%s", body, u)
	}
}

// ----- identity link ------------------------------------------------------------

// The link callback is a navigation from the upstream with no account header;
// it must link to the account the flow was started from, not the default.
func TestLinkCallbackActsOnInitiatingAccount(t *testing.T) {
	h := newLinkTestHarness(t)
	const defaultAccount int32 = 800
	h.q.accountByIDResults[defaultAccount] = db.Account{ID: defaultAccount, Username: "first"}
	h.q.accountByIDResults[h.linkAccountID] = db.Account{ID: h.linkAccountID, Username: "alice"}
	h.grantSudoOnSession(t)
	defaultToken, _, err := h.s.sessionStore.Issue(context.Background(), defaultAccount, "127.0.0.1", "ua/test", []string{"hwk"}, nil, false)
	if err != nil {
		t.Fatal(err)
	}

	r := chi.NewRouter()
	r.Use(h.s.browserSessions().LoadSession())
	r.Post("/api/prohibitorum/me/identities/link/{slug}/begin", h.s.handleMeIdentitiesLinkBeginHTTP)
	r.Get("/api/prohibitorum/me/identities/link/{slug}/callback", h.s.handleMeIdentitiesLinkCallbackHTTP)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	h.s.federationService = newTestFederationService(t, h.q, h.s.kvStore, h.s.Audit, map[int][]byte{1: fedTestDEK}, srv.URL, fedTestFedCfg().StateTTL)
	origin, _ := url.Parse(srv.URL)
	h.client.Jar.SetCookies(origin, []*http.Cookie{{
		Name: sessstore.TempCookieName(h.s.config), Path: "/",
		Value: sessstore.EncodeCookie([]sessstore.Entry{
			{Seq: 1, AccountID: defaultAccount, Token: defaultToken},
			{Seq: 2, AccountID: h.linkAccountID, Token: h.linkToken},
		}),
	}})

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/prohibitorum/me/identities/link/mockop/begin", bytes.NewReader([]byte(`{"returnTo":"/security"}`)))
	req.Header.Set("Content-Type", "application/json")
	withAccountHeader(req, h.linkAccountID)
	resp, err := h.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var begin contract.LoginResult
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&begin) != nil {
		t.Fatalf("begin status = %d", resp.StatusCode)
	}
	code, state, iss := driveAuthorize(t, begin.Redirect)

	callback, err := h.client.Get(srv.URL + "/api/prohibitorum/me/identities/link/mockop/callback?" + url.Values{"code": {code}, "state": {state}, "iss": {iss}}.Encode())
	if err != nil {
		t.Fatal(err)
	}
	defer callback.Body.Close()
	u := h.s.refs().Shorts([]int32{defaultAccount, h.linkAccountID})[h.linkAccountID]
	if callback.StatusCode != http.StatusFound || callback.Header.Get("Location") != "/security?u="+u {
		body, _ := readAll(callback.Body)
		t.Fatalf("callback = %d %q (%s), want /security?u=%s", callback.StatusCode, callback.Header.Get("Location"), body, u)
	}
	if len(h.q.insertIdentitys) != 1 || h.q.insertIdentitys[0].AccountID != h.linkAccountID {
		t.Fatalf("linked identities = %+v, want one for account %d", h.q.insertIdentitys, h.linkAccountID)
	}
}

// ----- maintenance --------------------------------------------------------------

func TestMaintenanceGateWithSeveralAccounts(t *testing.T) {
	const admin, member int32 = 1, 2
	newBrowser := func(t *testing.T, accounts ...int32) (*Server, *testBrowser) {
		s, f, _ := newTestServer(t)
		s.accountLookup = f
		f.accounts[admin] = db.Account{ID: admin, Username: "admin", Role: roleAdmin}
		f.accounts[member] = db.Account{ID: member, Username: "member", Role: "user"}
		b := newTestBrowser(t, s)
		for _, id := range accounts {
			b.signIn(id, false)
		}
		return s, b
	}
	gate := maintenanceGateMW(maintenanceResolver(true))(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	t.Run("API request is judged as the account its header names", func(t *testing.T) {
		_, b := newBrowser(t, admin, member)
		rec := b.serve(gate.ServeHTTP, withAccountHeader(httptest.NewRequest(http.MethodPost, "/api/prohibitorum/me/tokens", nil), member))
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("member by header = %d, want 503", rec.Code)
		}
		rec = b.serve(gate.ServeHTTP, withAccountHeader(httptest.NewRequest(http.MethodPost, "/api/prohibitorum/me/tokens", nil), admin))
		if rec.Code != http.StatusOK {
			t.Fatalf("admin by header = %d, want 200", rec.Code)
		}
	})
	t.Run("navigation passes when any account here is an admin", func(t *testing.T) {
		_, b := newBrowser(t, member, admin)
		for _, path := range []string{"/oauth/authorize", "/api/prohibitorum/require-identity"} {
			if rec := b.serve(gate.ServeHTTP, httptest.NewRequest(http.MethodGet, path, nil)); rec.Code != http.StatusOK {
				t.Fatalf("%s with an admin beside the default member = %d, want 200", path, rec.Code)
			}
		}
	})
	t.Run("navigation is blocked when no account here is an admin", func(t *testing.T) {
		_, b := newBrowser(t, member)
		if rec := b.serve(gate.ServeHTTP, httptest.NewRequest(http.MethodGet, "/oauth/authorize", nil)); rec.Code != http.StatusFound {
			t.Fatalf("member-only navigation = %d, want 302 to the maintenance screen", rec.Code)
		}
	})
}
