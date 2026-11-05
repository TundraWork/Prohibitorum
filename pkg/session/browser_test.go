package session

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/kv"
	"prohibitorum/pkg/weberr"
)

func TestDecodeCookie_Formats(t *testing.T) {
	got := DecodeCookie("2:" + "a.12.tokA~b.40.tok-B_")
	if len(got) != 2 || got[0].Seq != 10 || got[0].AccountID != 12 || got[0].Token != "tokA" ||
		got[1].Seq != 11 || got[1].AccountID != 40 || got[1].Token != "tok-B_" {
		t.Fatalf("new format: %+v", got)
	}
	legacy := DecodeCookie("42.tok")
	if len(legacy) != 1 || !legacy[0].legacy || legacy[0].AccountID != 42 || legacy[0].Token != "tok" {
		t.Fatalf("legacy format: %+v", legacy)
	}
	for _, bad := range []string{"", ".", ".tok", "42.", "x.tok", "-1.tok", "0.tok", "42.t~k"} {
		if got := DecodeCookie(bad); len(got) != 0 {
			t.Errorf("legacy %q decoded to %+v", bad, got)
		}
	}
	// Broken entries are dropped; the rest are kept.
	mixed := DecodeCookie("2:a.12.tokA~zz~0.5.tok~b.-1.tok~c.9.~d.9.t.k~e.7.ok")
	if len(mixed) != 2 || mixed[0].AccountID != 12 || mixed[1].AccountID != 7 {
		t.Fatalf("mixed: %+v", mixed)
	}
}

func TestEncodeCookie_RoundtripSorted(t *testing.T) {
	in := []Entry{{Seq: 99, AccountID: 2, Token: "b"}, {Seq: 5, AccountID: 1, Token: "a"}}
	v := EncodeCookie(in)
	if !strings.HasPrefix(v, "2:5.1.a~") {
		t.Fatalf("not sorted by seq: %q", v)
	}
	out := DecodeCookie(v)
	if len(out) != 2 || out[0].AccountID != 1 || out[1].AccountID != 2 {
		t.Fatalf("roundtrip: %+v", out)
	}
}

func TestEncodeCookie_TwentyEntriesFit(t *testing.T) {
	var entries []Entry
	seq := time.Now().UnixMilli()
	for i := range 20 {
		tok, _ := newToken()
		entries = append(entries, Entry{Seq: seq + int64(i), AccountID: int32(1000000 + i), Token: tok})
	}
	if n := len(EncodeCookie(entries)); n >= 1500 {
		t.Fatalf("20 entries encode to %d bytes, want < 1500", n)
	}
}

// ----- Browser ---------------------------------------------------------------

type browserAccounts struct {
	db.Querier
	disabled map[int32]bool
	missing  map[int32]bool
}

func (q *browserAccounts) GetAccountByID(_ context.Context, id int32) (db.Account, error) {
	if q.missing[id] {
		return db.Account{}, context.Canceled
	}
	return db.Account{ID: id, Username: "user", Disabled: q.disabled[id]}, nil
}

type browserFixture struct {
	t     *testing.T
	cfg   *configx.Config
	store *SessionStore
	q     *browserAccounts
	m     *Manager
	jar   map[string]string
}

func newBrowserFixture(t *testing.T, limit int) *browserFixture {
	t.Helper()
	mem := kv.NewMemoryStore()
	t.Cleanup(func() { _ = mem.Close() })
	cfg := devCfg()
	cfg.SessionMaxAccounts = limit
	f := &browserFixture{
		t:     t,
		cfg:   cfg,
		store: NewSessionStore(mem, noopSessionQueries{}, time.Hour, 24*time.Hour),
		q:     &browserAccounts{disabled: map[int32]bool{}, missing: map[int32]bool{}},
		jar:   map[string]string{},
	}
	f.m = NewManager(cfg, f.q, f.store, nil)
	return f
}

func (f *browserFixture) request(method, path string) *http.Request {
	r := httptest.NewRequest(method, path, nil)
	for name, v := range f.jar {
		r.AddCookie(&http.Cookie{Name: name, Value: v})
	}
	return r
}

// absorb applies a response's Set-Cookie headers to the jar.
func (f *browserFixture) absorb(h http.Header) {
	for _, c := range (&http.Response{Header: h}).Cookies() {
		if c.MaxAge < 0 || c.Value == "" {
			delete(f.jar, c.Name)
		} else {
			f.jar[c.Name] = c.Value
		}
	}
}

func (f *browserFixture) signIn(id int32, remember bool) *authn.Session {
	f.t.Helper()
	rec := httptest.NewRecorder()
	b := f.m.BrowserFor(f.request(http.MethodPost, "/api/prohibitorum/auth/login/complete"))
	sess, err := b.Attach(rec, AttachParams{AccountID: id, AMR: []string{"hwk"}, Remember: remember})
	if err != nil {
		f.t.Fatalf("Attach(%d): %v", id, err)
	}
	f.absorb(rec.Header())
	return sess
}

func (f *browserFixture) browser() *Browser {
	return f.m.BrowserFor(f.request(http.MethodGet, "/api/prohibitorum/auth/accounts"))
}

func ids(b *Browser) []int32 { return b.AccountIDs() }

func TestBrowser_AttachAppendsAndKeepsOrder(t *testing.T) {
	f := newBrowserFixture(t, 10)
	f.signIn(12, true)
	f.signIn(40, false)
	f.signIn(7, true)
	if got := ids(f.browser()); len(got) != 3 || got[0] != 12 || got[1] != 40 || got[2] != 7 {
		t.Fatalf("order = %v, want [12 40 7]", got)
	}
	if !strings.HasPrefix(f.jar[SessionCookieName], "2:") || !strings.HasPrefix(f.jar[TempSessionCookieName], "2:") {
		t.Fatalf("cookies = %v", f.jar)
	}
	if d := f.browser().Default(); d == nil || d.Account.ID != 12 {
		t.Fatalf("default = %+v, want 12", d)
	}
}

func TestBrowser_ReauthKeepsSeqAndRevokesOld(t *testing.T) {
	f := newBrowserFixture(t, 10)
	first := f.signIn(12, false)
	f.signIn(40, false)
	again := f.signIn(12, true) // remember changed: moves to the persistent cookie
	if got := ids(f.browser()); got[0] != 12 || got[1] != 40 {
		t.Fatalf("re-auth moved the account: %v", got)
	}
	if _, _, err := f.store.Load(context.Background(), 12, first.Token, "", ""); err == nil {
		t.Fatal("old session must be revoked")
	}
	if !strings.Contains(f.jar[SessionCookieName], ".12."+again.Token) {
		t.Fatalf("account 12 must now live in the persistent cookie: %v", f.jar)
	}
	if strings.Contains(f.jar[TempSessionCookieName], ".12.") {
		t.Fatalf("account 12 must have left the temp cookie: %v", f.jar)
	}
}

func TestBrowser_LimitCountsBothCookies(t *testing.T) {
	f := newBrowserFixture(t, 2)
	f.signIn(1, true)
	f.signIn(2, false)
	b := f.browser()
	if !b.AtLimit() || b.CanAttach(3) || !b.CanAttach(1) {
		t.Fatal("cap must count entries of both cookies")
	}
	_, err := b.Attach(httptest.NewRecorder(), AttachParams{AccountID: 3, AMR: []string{"hwk"}})
	if err != ErrAccountLimit {
		t.Fatalf("Attach over the cap: err = %v", err)
	}
	if recs, _ := f.store.ListByAccount(context.Background(), 3); len(recs) != 0 {
		t.Fatal("nothing may be issued over the cap")
	}
	// Re-authenticating a signed-in account is not blocked by the cap.
	f.signIn(2, false)
}

func TestBrowser_ExpiredEntryDroppedAndCookieRewritten(t *testing.T) {
	f := newBrowserFixture(t, 10)
	a := f.signIn(1, false)
	f.signIn(2, false)
	_ = f.store.Revoke(context.Background(), 1, a.Token)

	rec := httptest.NewRecorder()
	h := f.m.LoadSession()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s := authn.SessionFromContext(r.Context()); s == nil || s.Account.ID != 2 {
			t.Errorf("default after the first expired = %+v, want 2", s)
		}
	}))
	h.ServeHTTP(rec, f.request(http.MethodGet, "/"))
	f.absorb(rec.Header())
	if strings.Contains(f.jar[TempSessionCookieName], ".1.") || !strings.Contains(f.jar[TempSessionCookieName], ".2.") {
		t.Fatalf("expired entry must be dropped: %v", f.jar)
	}
}

func TestBrowser_DuplicateAccountKeepsEarliest(t *testing.T) {
	f := newBrowserFixture(t, 10)
	ctx := context.Background()
	tokA, _, _ := f.store.Issue(ctx, 5, "", "", []string{"hwk"}, nil, false)
	tokB, _, _ := f.store.Issue(ctx, 5, "", "", []string{"hwk"}, nil, true)
	f.jar[TempSessionCookieName] = EncodeCookie([]Entry{{Seq: 10, AccountID: 5, Token: tokA}})
	f.jar[SessionCookieName] = EncodeCookie([]Entry{{Seq: 20, AccountID: 5, Token: tokB}})
	b := f.browser()
	if got := b.Select(5); got == nil || got.Token != tokA {
		t.Fatalf("earliest entry must win, got %+v", got)
	}
	if _, _, err := f.store.Load(ctx, 5, tokB, "", ""); err == nil {
		t.Fatal("the duplicate's session must be revoked")
	}
}

func TestBrowser_LegacyCookieMigratesWithoutSignOut(t *testing.T) {
	for _, remember := range []bool{true, false} {
		f := newBrowserFixture(t, 10)
		tok, data, err := f.store.Issue(context.Background(), 42, "", "", []string{"hwk"}, nil, remember)
		if err != nil {
			t.Fatal(err)
		}
		f.jar[SessionCookieName] = "42." + tok
		rec := httptest.NewRecorder()
		var got *authn.Session
		f.m.LoadSession()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got = authn.SessionFromContext(r.Context())
		})).ServeHTTP(rec, f.request(http.MethodGet, "/"))
		if got == nil || got.Account.ID != 42 {
			t.Fatalf("remember=%v: legacy cookie must still sign in, got %+v", remember, got)
		}
		f.absorb(rec.Header())
		want := TempSessionCookieName
		if remember {
			want = SessionCookieName
		}
		entries := DecodeCookie(f.jar[want])
		if len(entries) != 1 || entries[0].AccountID != 42 || entries[0].Seq != data.IssuedAt.UnixMilli() {
			t.Fatalf("remember=%v: migrated cookie %q = %+v", remember, want, f.jar)
		}
		if !remember {
			if _, ok := f.jar[SessionCookieName]; ok {
				t.Fatalf("legacy value must leave the persistent cookie: %v", f.jar)
			}
		}
	}
}

func TestBrowser_RemoveAndRemoveAll(t *testing.T) {
	f := newBrowserFixture(t, 10)
	f.signIn(1, true)
	f.signIn(2, false)
	f.signIn(3, false)
	b := f.browser()
	if !b.Remove(2) || b.Remove(99) {
		t.Fatal("Remove reports whether the account was signed in")
	}
	rec := httptest.NewRecorder()
	b.Flush(rec.Header())
	f.absorb(rec.Header())
	if got := ids(f.browser()); len(got) != 2 || got[0] != 1 || got[1] != 3 {
		t.Fatalf("after Remove(2): %v", got)
	}
	b = f.browser()
	if got := b.RemoveAll(); len(got) != 2 {
		t.Fatalf("RemoveAll = %v", got)
	}
	rec = httptest.NewRecorder()
	b.Flush(rec.Header())
	cleared := 0
	for _, c := range (&http.Response{Header: rec.Header()}).Cookies() {
		if c.MaxAge < 0 {
			cleared++
		}
	}
	if cleared != 2 {
		t.Fatalf("RemoveAll must clear both cookies: %v", rec.Header().Values("Set-Cookie"))
	}
}

func TestBrowser_ResetAccountKeepsSeq(t *testing.T) {
	f := newBrowserFixture(t, 10)
	f.signIn(1, false)
	f.signIn(2, false)
	// An admin-issued reset revokes every session of account 1 before the
	// new one is issued; it comes back in its old place.
	_, _ = f.store.RevokeAllForAccount(context.Background(), 1)
	f.signIn(1, false)
	if got := ids(f.browser()); got[0] != 1 || got[1] != 2 {
		t.Fatalf("reset account must keep its place: %v", got)
	}
}

// ----- LoadSession selection -------------------------------------------------

func serveSelect(t *testing.T, f *browserFixture, path, header string) (*httptest.ResponseRecorder, *authn.Session, bool) {
	t.Helper()
	rec := httptest.NewRecorder()
	var got *authn.Session
	var missing bool
	reached := false
	f.m.LoadSession()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		got = authn.SessionFromContext(r.Context())
		missing = authn.AccountNotSignedIn(r.Context())
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(rec, func() *http.Request {
		r := f.request(http.MethodGet, path)
		if header != "" {
			r.Header.Set(AccountHeader, header)
		}
		return r
	}())
	if !reached {
		return rec, nil, false
	}
	return rec, got, missing
}

func TestLoadSession_SelectsByHeader(t *testing.T) {
	f := newBrowserFixture(t, 10)
	f.signIn(12, false)
	f.signIn(40, true)

	rec, got, _ := serveSelect(t, f, "/api/prohibitorum/me", "")
	if got == nil || got.Account.ID != 12 || rec.Header().Get(AccountHeader) != "12" {
		t.Fatalf("no header: got %+v, response header %q", got, rec.Header().Get(AccountHeader))
	}
	rec, got, _ = serveSelect(t, f, "/api/prohibitorum/me", "40")
	if got == nil || got.Account.ID != 40 || rec.Header().Get(AccountHeader) != "40" {
		t.Fatalf("header 40: got %+v", got)
	}
	_, got, missing := serveSelect(t, f, "/api/prohibitorum/me", "77")
	if got != nil || !missing {
		t.Fatalf("header for an account not here: got %+v missing=%v", got, missing)
	}
	if err := authn.CheckContext(authn.WithAccountNotSignedIn(context.Background()), nil, sessionReq()); authn.AsAuthError(err).Code != "account_not_signed_in" {
		t.Fatalf("CheckContext = %v", err)
	}
	// Protocol endpoints ignore the header.
	_, got, _ = serveSelect(t, f, "/oauth/authorize", "40")
	if got == nil || got.Account.ID != 12 {
		t.Fatalf("header must be ignored outside /api/prohibitorum/: %+v", got)
	}
}

func TestLoadSession_MalformedHeaderIsBadRequest(t *testing.T) {
	f := newBrowserFixture(t, 10)
	f.signIn(12, false)
	for _, bad := range []string{"abc", "0", "-4", "3fa9", "1.5"} {
		rec, _, _ := serveSelect(t, f, "/api/prohibitorum/me", bad)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("header %q: status %d", bad, rec.Code)
		}
		var body struct{ Code string }
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body.Code != "bad_request" {
			t.Fatalf("header %q: body %s", bad, rec.Body.String())
		}
	}
}

func TestLoadSession_DisabledAccountDropsOnlyItself(t *testing.T) {
	f := newBrowserFixture(t, 10)
	f.signIn(1, false)
	f.signIn(2, false)
	f.q.disabled[1] = true

	rec, got, _ := serveSelect(t, f, "/api/prohibitorum/me", "1")
	if got == nil || !got.Account.Disabled || got.Data != nil {
		t.Fatalf("selected disabled account must get the sentinel: %+v", got)
	}
	if err := authn.Check(got, sessionReq()); authn.AsAuthError(err).Code != "account_disabled" {
		t.Fatalf("Check = %v", err)
	}
	f.absorb(rec.Header())
	if got := ids(f.browser()); len(got) != 1 || got[0] != 2 {
		t.Fatalf("only the disabled entry goes: %v", got)
	}
	_, got, _ = serveSelect(t, f, "/api/prohibitorum/me", "")
	if got == nil || got.Account.ID != 2 {
		t.Fatalf("next account becomes default: %+v", got)
	}
}

func TestLoadSession_DeletedAccountDropped(t *testing.T) {
	f := newBrowserFixture(t, 10)
	f.signIn(1, false)
	f.signIn(2, false)
	f.q.missing[1] = true
	rec, got, _ := serveSelect(t, f, "/", "")
	if got == nil || got.Account.ID != 2 {
		t.Fatalf("default = %+v", got)
	}
	f.absorb(rec.Header())
	if got := ids(f.browser()); len(got) != 1 {
		t.Fatalf("deleted account's entry must go: %v", got)
	}
}

func TestLoadSession_NoCookieNoSession(t *testing.T) {
	f := newBrowserFixture(t, 10)
	rec, got, _ := serveSelect(t, f, "/api/prohibitorum/me", "")
	if got != nil || rec.Header().Get("Set-Cookie") != "" || rec.Header().Get(AccountHeader) != "" {
		t.Fatalf("no cookie: %+v %v", got, rec.Header())
	}
}

func TestErrorEnvelopeCodesRegistered(t *testing.T) {
	for _, code := range []string{"account_not_signed_in", "account_limit_reached", "account_chooser_expired"} {
		if _, ok := weberr.DefinitionFor(code); !ok {
			t.Errorf("%s not registered", code)
		}
	}
}

func sessionReq() contract.AuthRequirement {
	return contract.AuthRequirement{Kind: contract.AuthSession}
}

// flakyGetKV fails Get for one account's session keys, as a KV timeout would.
type flakyGetKV struct {
	kv.Store
	failAccount int32
}

func (s *flakyGetKV) Get(ctx context.Context, key string) (string, error) {
	if strings.HasPrefix(key, "session:"+strconv.Itoa(int(s.failAccount))+":") {
		return "", errors.New("kv: i/o timeout")
	}
	return s.Store.Get(ctx, key)
}

func TestBrowser_UnreadableEntryIsNotSkipped(t *testing.T) {
	f := newBrowserFixture(t, 2)
	f.signIn(1, false)
	f.signIn(2, false)
	f.store.kv = &flakyGetKV{Store: f.store.kv, failAccount: 1}

	b := f.browser()
	if d := b.Default(); d != nil {
		t.Fatalf("default must not fall through to account %d while account 1 is unreadable", d.Account.ID)
	}
	if !b.Unsettled() || b.Count() != 1 {
		t.Fatalf("unsettled=%v count=%d, want true 1", b.Unsettled(), b.Count())
	}
	if b.CanAttach(3) || !b.AtLimit() {
		t.Fatal("an unreadable entry still counts toward the cap")
	}
	// It stays in the cookie: nothing is rewritten for it.
	rec := httptest.NewRecorder()
	b.Flush(rec.Header())
	if rec.Header().Get("Set-Cookie") != "" {
		t.Fatalf("unreadable entry must not be dropped: %v", rec.Header().Values("Set-Cookie"))
	}
}
