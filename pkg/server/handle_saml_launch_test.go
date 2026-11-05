package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	"prohibitorum/pkg/appaccess"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/kv"
	sessstore "prohibitorum/pkg/session"
)

const launchSP = "https://ghe.example/saml"

// perAccountApps allows each account only the apps listed for it.
type perAccountApps map[int32][]appaccess.AppSummary

func (p perAccountApps) ListAllowedApps(_ context.Context, accountID int32) ([]appaccess.AppSummary, error) {
	return p[accountID], nil
}

// The launcher's SAML launch binds an account choice to the account the
// request header selects, not the browser's default account, and refuses an
// account the SP does not admit.
func TestSAMLLaunchBindsHeaderSelectedAccount(t *testing.T) {
	ghe := appaccess.AppSummary{Ref: appaccess.AppRef{Kind: appaccess.KindSAML, SAMLSPID: 7}, DisplayName: "GitHub", EntityID: launchSP}
	cases := []struct {
		name     string
		selected int32
		wantCode int
		wantErr  string
	}{
		{name: "second account allowed", selected: 43, wantCode: http.StatusOK},
		{name: "default account refused", selected: 42, wantCode: http.StatusForbidden, wantErr: "app_access_denied"},
		{name: "account not signed in here", selected: 44, wantCode: http.StatusUnauthorized, wantErr: "account_not_signed_in"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := context.Background()
			cfg := &configx.Config{PublicOrigins: []string{"http://id.example.test"}}
			memKV := kv.NewMemoryStore()
			store := sessstore.NewSessionStore(memKV, noopSessionQueriesForServer{}, time.Hour, time.Hour)
			var entries []sessstore.Entry
			for i, id := range []int32{42, 43} {
				token, _, err := store.Issue(ctx, id, "192.0.2.1", "ua", []string{"hwk"}, nil, false)
				if err != nil {
					t.Fatal(err)
				}
				entries = append(entries, sessstore.Entry{Seq: int64(i + 1), AccountID: id, Token: token})
			}
			s := &Server{
				config:        cfg,
				kvStore:       memKV,
				sessionStore:  store,
				accountLookup: signOutAccounts{},
				appLister:     perAccountApps{43: {ghe}},
			}
			router := chi.NewMux()
			router.Use(s.browserSessions().LoadSession())
			s.router = router
			s.api = humachi.New(router, humaConfig())
			registerSecurityScheme(s.api, sessstore.SessionCookieName)
			s.registerOperations()

			req := httptest.NewRequest(http.MethodPost, "/api/prohibitorum/me/apps/saml-launch?sp="+url.QueryEscape(launchSP), nil)
			req.AddCookie(&http.Cookie{Name: sessstore.TempCookieName(cfg), Value: sessstore.EncodeCookie(entries)})
			req.Header.Set(sessstore.AccountHeader, strconv.Itoa(int(c.selected)))
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != c.wantCode {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, c.wantCode, rec.Body.String())
			}
			if c.wantErr != "" {
				var body struct{ Code string }
				_ = json.Unmarshal(rec.Body.Bytes(), &body)
				if body.Code != c.wantErr {
					t.Errorf("code = %q, want %q", body.Code, c.wantErr)
				}
				return
			}
			var out contract.SAMLLaunchResult
			if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
			redirect, err := url.Parse(out.Redirect)
			if err != nil || redirect.Path != "/saml/sso/init" || redirect.Query().Get("sp") != launchSP {
				t.Fatalf("redirect = %q, want /saml/sso/init for %s", out.Redirect, launchSP)
			}
			nonce := redirect.Query().Get("account_choice")
			if got := authn.ChosenAccount(ctx, memKV, nonce, authn.ChooserKindSAMLInit, launchSP); got != c.selected {
				t.Errorf("choice bound to account %d, want %d", got, c.selected)
			}
		})
	}
}
