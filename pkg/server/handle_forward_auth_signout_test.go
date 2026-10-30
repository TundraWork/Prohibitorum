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

	"prohibitorum/pkg/authn"
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

// sso-logout ends the SSO session only for a pass naming the session's
// account; any other request with a session goes to the confirmation page and
// leaves the session alone.
func TestForwardAuthSSOLogout(t *testing.T) {
	const app = "https://app.example.test/"
	const evil = "https://evil.example/"
	confirm := "/sign-out?rd=" + url.QueryEscape(app)
	cases := []struct {
		name        string
		session     bool
		disabled    bool // LoadSession's stand-in for a disabled account: no Data
		token       string
		passes      fakeSignOutPasses
		rd          string
		wantLoc     string
		wantRevoked bool
	}{
		{name: "no session", token: "", rd: app, wantLoc: app},
		{name: "no session, with pass", token: "p42", passes: fakeSignOutPasses{"p42": 42}, rd: app, wantLoc: app},
		{name: "disabled account", disabled: true, token: "p42", passes: fakeSignOutPasses{"p42": 42}, rd: app, wantLoc: app},
		{name: "valid pass", session: true, token: "p42", passes: fakeSignOutPasses{"p42": 42}, rd: app, wantLoc: app, wantRevoked: true},
		{name: "valid pass, bad rd", session: true, token: "p42", passes: fakeSignOutPasses{"p42": 42}, rd: evil, wantLoc: "/", wantRevoked: true},
		{name: "no pass", session: true, rd: app, wantLoc: confirm},
		{name: "pass for another account", session: true, token: "p43", passes: fakeSignOutPasses{"p43": 43}, rd: app, wantLoc: confirm},
		{name: "unknown pass", session: true, token: "nope", rd: app, wantLoc: confirm},
		{name: "no pass, bad rd", session: true, rd: evil, wantLoc: "/sign-out"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := context.Background()
			cfg := &configx.Config{PublicOrigins: []string{"https://id.example.test"}}
			store := sessstore.NewSessionStore(kv.NewMemoryStore(), noopSessionQueriesForServer{}, time.Hour, time.Hour)
			token, data, err := store.Issue(ctx, 42, "192.0.2.1", "ua", []string{"hwk"}, nil, false)
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
				forwardAuthSignOutOverride:       ssoLogoutHosts{},
				forwardAuthSignOutPassesOverride: passes,
			}

			q := url.Values{"rd": {c.rd}}
			if c.token != "" {
				q.Set("token", c.token)
			}
			req := httptest.NewRequest(http.MethodGet, "/api/prohibitorum/forward-auth/sso-logout?"+q.Encode(), nil)
			if c.session || c.disabled {
				req.AddCookie(&http.Cookie{Name: sessstore.SessionCookieNameFor(cfg), Value: sessstore.CookieValue(42, token)})
			}
			switch {
			case c.session:
				req = req.WithContext(authn.WithSession(req.Context(), &authn.Session{Account: &db.Account{ID: 42}, Token: token, Data: data}))
			case c.disabled:
				req = req.WithContext(authn.WithSession(req.Context(), &authn.Session{Account: &db.Account{ID: 42, Disabled: true}}))
			}
			rec := httptest.NewRecorder()
			s.handleForwardAuthSSOLogoutHTTP(rec, req)

			if rec.Code != http.StatusFound {
				t.Fatalf("status = %d, want 302", rec.Code)
			}
			if loc := rec.Header().Get("Location"); loc != c.wantLoc {
				t.Errorf("Location = %q, want %q", loc, c.wantLoc)
			}
			if len(passes) != 0 {
				t.Errorf("passes left unconsumed: %v", passes)
			}
			if !c.session {
				if cookies := rec.Result().Cookies(); len(cookies) != 0 {
					t.Errorf("Set-Cookie without a live session: %v", cookies)
				}
				return
			}
			_, _, loadErr := store.Load(ctx, 42, token, "192.0.2.1", "ua")
			if revoked := loadErr != nil; revoked != c.wantRevoked {
				t.Errorf("session revoked = %v, want %v", revoked, c.wantRevoked)
			}
			cleared := false
			for _, ck := range rec.Result().Cookies() {
				if ck.Name == sessstore.SessionCookieNameFor(cfg) && ck.MaxAge < 0 {
					cleared = true
				}
			}
			if cleared != c.wantRevoked {
				t.Errorf("session cookie cleared = %v, want %v", cleared, c.wantRevoked)
			}
		})
	}
}
