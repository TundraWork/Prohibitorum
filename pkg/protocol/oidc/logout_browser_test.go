package oidc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"prohibitorum/pkg/db"
	"prohibitorum/pkg/session"
)

type logoutBrowserAccounts struct{}

func (logoutBrowserAccounts) GetAccountByID(_ context.Context, id int32) (db.Account, error) {
	return db.Account{ID: id}, nil
}

// RP-initiated logout revokes the hint's session by ID and is unchanged by
// multi-account browsers (design 5.7): the browser that held it drops the
// entry on its next request and keeps its other account.
func TestLogoutRevokedSessionDropsBrowserEntry(t *testing.T) {
	h := newEndpointHarness(t)
	ctx := context.Background()
	store := session.NewSessionStore(h.p.kv, fakeSessionQueries{}, time.Hour, time.Hour)
	h.p.sessions = store
	token7, data7, err := store.Issue(ctx, 7, "", "", []string{"pwd"}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	token8, _, err := store.Issue(ctx, 8, "", "", []string{"pwd"}, nil, false)
	if err != nil {
		t.Fatal(err)
	}

	q := url.Values{}
	q.Set("id_token_hint", h.mintHint(t, data7.SessionID, time.Now().Add(time.Hour)))
	rec := httptest.NewRecorder()
	h.p.HandleLogout(rec, logoutReq(q))
	if rec.Code != http.StatusFound {
		t.Fatalf("logout status = %d, want 302; body=%s", rec.Code, rec.Body.String())
	}

	cfg := h.p.cfg
	mgr := session.NewManager(cfg, logoutBrowserAccounts{}, store, nil)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: session.TempCookieName(cfg), Value: session.EncodeCookie([]session.Entry{
		{Seq: 1, AccountID: 7, Token: token7},
		{Seq: 2, AccountID: 8, Token: token8},
	})})
	b := mgr.BrowserFor(req)
	if ids := b.AccountIDs(); len(ids) != 1 || ids[0] != 8 {
		t.Fatalf("browser accounts after logout = %v, want [8]", ids)
	}
	out := http.Header{}
	b.Flush(out)
	resp := http.Response{Header: out}
	var rewritten []session.Entry
	for _, c := range resp.Cookies() {
		if c.Name == session.TempCookieName(cfg) {
			rewritten = session.DecodeCookie(c.Value)
		}
	}
	if len(rewritten) != 1 || rewritten[0].AccountID != 8 {
		t.Errorf("rewritten cookie entries = %+v, want only account 8", rewritten)
	}
}
