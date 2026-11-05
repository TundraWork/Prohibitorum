package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/kv"
	sessstore "prohibitorum/pkg/session"
)

// ssoLogoutHosts registers app.example.test as the one forward-auth host;
// embedding db.Querier keeps every other method a panic-on-use stub.
type ssoLogoutHosts struct {
	db.Querier
}

func (ssoLogoutHosts) GetForwardAuthClientByHost(_ context.Context, host pgtype.Text) (db.GetForwardAuthClientByHostRow, error) {
	if host.String == "app.example.test" {
		return db.GetForwardAuthClientByHostRow{ClientID: "fa"}, nil
	}
	return db.GetForwardAuthClientByHostRow{}, pgx.ErrNoRows
}

// fakeSignOutPasses maps a pass to the account it was minted for; a pass is
// gone once consumed.
type fakeSignOutPasses map[string]int32

func (f fakeSignOutPasses) ConsumeForwardAuthSignOut(_ context.Context, token string) (int32, bool) {
	id, ok := f[token]
	delete(f, token)
	return id, ok
}

type signOutAccounts struct {
	disabled bool
}

func (a signOutAccounts) GetAccountByID(_ context.Context, id int32) (db.Account, error) {
	return db.Account{ID: id, Disabled: a.disabled}, nil
}

// sso-logout ends the SSO session only for a pass naming an account signed in
// on this browser, and only that account's; any other request with a session
// goes to the confirmation page and leaves the sessions alone.
func TestForwardAuthSSOLogout(t *testing.T) {
	const app = "https://app.example.test/"
	const evil = "https://evil.example/"
	confirm := "/sign-out?rd=" + url.QueryEscape(app)
	cases := []struct {
		name        string
		session     bool
		second      bool // a second account (43) is signed in too
		disabled    bool // account 42 was disabled mid-session
		token       string
		done        bool
		passes      fakeSignOutPasses
		rd          string
		wantLoc     string
		wantRevoked bool // account 42's session
		want43Gone  bool
	}{
		{name: "no session", token: "", rd: app, wantLoc: app},
		{name: "no session, with pass", token: "p42", passes: fakeSignOutPasses{"p42": 42}, rd: app, wantLoc: app},
		{name: "disabled account", session: true, disabled: true, token: "p42", passes: fakeSignOutPasses{"p42": 42}, rd: app, wantLoc: app, wantRevoked: true},
		{name: "valid pass", session: true, token: "p42", passes: fakeSignOutPasses{"p42": 42}, rd: app, wantLoc: app, wantRevoked: true},
		{name: "valid pass, bad rd", session: true, token: "p42", passes: fakeSignOutPasses{"p42": 42}, rd: evil, wantLoc: "/", wantRevoked: true},
		{name: "no pass", session: true, rd: app, wantLoc: confirm},
		{name: "pass for an account not here", session: true, token: "p43", passes: fakeSignOutPasses{"p43": 43}, rd: app, wantLoc: confirm},
		{name: "unknown pass", session: true, token: "nope", rd: app, wantLoc: confirm},
		{name: "no pass, bad rd", session: true, rd: evil, wantLoc: "/sign-out"},
		{name: "pass for the second account signs out only it", session: true, second: true, token: "p43", passes: fakeSignOutPasses{"p43": 43}, rd: app, wantLoc: app, want43Gone: true},
		{name: "several accounts, no pass", session: true, second: true, rd: app, wantLoc: confirm},
		{name: "back to the app revokes nothing", session: true, second: true, done: true, rd: app, wantLoc: app},
		{name: "back to the app, bad rd", session: true, done: true, rd: evil, wantLoc: "/"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := context.Background()
			cfg := &configx.Config{PublicOrigins: []string{"http://id.example.test"}}
			store := sessstore.NewSessionStore(kv.NewMemoryStore(), noopSessionQueriesForServer{}, time.Hour, time.Hour)
			token, _, err := store.Issue(ctx, 42, "192.0.2.1", "ua", []string{"hwk"}, nil, false)
			if err != nil {
				t.Fatalf("issue session: %v", err)
			}
			token43, _, err := store.Issue(ctx, 43, "192.0.2.1", "ua", []string{"hwk"}, nil, false)
			if err != nil {
				t.Fatalf("issue session: %v", err)
			}
			passes := c.passes
			if passes == nil {
				passes = fakeSignOutPasses{}
			}
			s := &Server{
				config:                           cfg,
				sessionStore:                     store,
				accountLookup:                    signOutAccounts{disabled: c.disabled},
				forwardAuthSignOutOverride:       ssoLogoutHosts{},
				forwardAuthSignOutPassesOverride: passes,
			}

			q := url.Values{"rd": {c.rd}}
			if c.token != "" {
				q.Set("token", c.token)
			}
			if c.done {
				q.Set("done", "1")
			}
			req := httptest.NewRequest(http.MethodGet, "/api/prohibitorum/forward-auth/sso-logout?"+q.Encode(), nil)
			var entries []sessstore.Entry
			if c.session {
				entries = append(entries, sessstore.Entry{Seq: 1, AccountID: 42, Token: token})
			}
			if c.second {
				entries = append(entries, sessstore.Entry{Seq: 2, AccountID: 43, Token: token43})
			}
			if len(entries) > 0 {
				req.AddCookie(&http.Cookie{Name: sessstore.TempCookieName(cfg), Value: sessstore.EncodeCookie(entries)})
			}
			rec := httptest.NewRecorder()
			s.browserSessions().LoadSession()(http.HandlerFunc(s.handleForwardAuthSSOLogoutHTTP)).ServeHTTP(rec, req)

			if rec.Code != http.StatusFound {
				t.Fatalf("status = %d, want 302", rec.Code)
			}
			if loc := rec.Header().Get("Location"); loc != c.wantLoc {
				t.Errorf("Location = %q, want %q", loc, c.wantLoc)
			}
			if !c.done && len(passes) != 0 {
				t.Errorf("passes left unconsumed: %v", passes)
			}
			_, _, loadErr := store.Load(ctx, 42, token, "192.0.2.1", "ua")
			if revoked := loadErr != nil; c.session && revoked != c.wantRevoked {
				t.Errorf("session 42 revoked = %v, want %v", revoked, c.wantRevoked)
			}
			if !c.session && loadErr != nil {
				t.Errorf("session revoked without being signed in here: %v", loadErr)
			}
			_, _, load43 := store.Load(ctx, 43, token43, "192.0.2.1", "ua")
			if gone := load43 != nil; c.second && gone != c.want43Gone {
				t.Errorf("session 43 revoked = %v, want %v", gone, c.want43Gone)
			}
			changed := len(rec.Result().Cookies()) > 0
			if changed != (c.wantRevoked || c.want43Gone) {
				t.Errorf("cookie rewritten = %v, want %v", changed, c.wantRevoked || c.want43Gone)
			}
		})
	}
}
