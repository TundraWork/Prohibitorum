package saml

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/db"
	"prohibitorum/pkg/session"
)

// SLO revokes by session ID and is unchanged by multi-account browsers
// (design 5.7): the browser that held the session drops its entry on the next
// request, and the other account stays signed in.
func TestSLORevokedSessionDropsBrowserEntry(t *testing.T) {
	h := newSLOHarness(t, sloSP())
	ctx := context.Background()
	const nameID = "user-nameid-browser"

	token42, data42, err := h.store.Issue(ctx, 42, "", "", []string{"hwk"}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	token43, _, err := h.store.Issue(ctx, 43, "", "", []string{"hwk"}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	h.q.mu.Lock()
	h.q.sessions = append(h.q.sessions, db.SamlSession{
		ID: 1, SessionID: data42.SessionID, SpID: h.q.sp.ID, NameID: nameID,
		NotOnOrAfter: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
	})
	h.q.pgSess[data42.SessionID] = db.Session{ID: data42.SessionID, AccountID: 42}
	h.q.mu.Unlock()

	rec := httptest.NewRecorder()
	h.idp.HandleSLO(rec, buildLogoutRedirect(t, sloReqOpts{
		id: "_slo-browser", destination: testSLOURL, nameID: nameID, sign: true, signKey: h.spKey,
	}))
	if rec.Code != http.StatusFound {
		t.Fatalf("SLO status = %d, want 302; body=%s", rec.Code, rec.Body.String())
	}

	cfg := h.idp.cfg
	mgr := session.NewManager(cfg, browserAccounts{42: {ID: 42}, 43: {ID: 43}}, h.store, nil)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: session.TempCookieName(cfg), Value: session.EncodeCookie([]session.Entry{
		{Seq: 1, AccountID: 42, Token: token42},
		{Seq: 2, AccountID: 43, Token: token43},
	})})
	b := mgr.BrowserFor(req)
	if ids := b.AccountIDs(); len(ids) != 1 || ids[0] != 43 {
		t.Fatalf("browser accounts after SLO = %v, want [43]", ids)
	}
	if d := b.Default(); d == nil || d.Account.ID != 43 {
		t.Errorf("default account after SLO = %v, want 43", d)
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
	if len(rewritten) != 1 || rewritten[0].AccountID != 43 {
		t.Errorf("rewritten cookie entries = %+v, want only account 43", rewritten)
	}
}
