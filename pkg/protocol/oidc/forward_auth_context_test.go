package oidc

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/kv"
)

type contextQueries struct {
	*fakeFAQueries
	client db.OidcClient
	err    error
}

func (q *contextQueries) GetOIDCClientAny(_ context.Context, id string) (db.OidcClient, error) {
	if q.err != nil {
		return db.OidcClient{}, q.err
	}
	if id != q.client.ClientID {
		return db.OidcClient{}, pgx.ErrNoRows
	}
	return q.client, nil
}

// Exposes reads only: embedding a nil Store makes every mutation panic.
type contextReadStore struct {
	kv.Store
	source kv.Store
	err    error
}

func (s contextReadStore) Get(ctx context.Context, key string) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	return s.source.Get(ctx, key)
}

func contextFixture(t *testing.T) (*Provider, *contextQueries, kv.Store, *url.URL) {
	t.Helper()
	host := "app.example:8443"
	q := &contextQueries{fakeFAQueries: &fakeFAQueries{faClient: db.GetForwardAuthClientByHostRow{ClientID: "svc"}},
		client: db.OidcClient{ClientID: "svc", DisplayName: "Application", ForwardAuthEnabled: true,
			ForwardAuthHost: pgtype.Text{String: host, Valid: true}, RedirectUris: []string{ForwardAuthCallbackURI(host)}}}
	p, store := newFAProvider(q)
	rec := httptest.NewRecorder()
	p.HandleForwardAuthVerify(rec, faRequest("https", host, "/private?document=one", nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("verify status %d: %s", rec.Code, rec.Body.String())
	}
	u, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	p.kv = contextReadStore{source: store}
	return p, q, store, u
}
func readContext(p *Provider, raw string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	p.HandleForwardAuthLoginContext(rec, httptest.NewRequest("GET", "/api/prohibitorum/forward-auth/login-context?return_to="+url.QueryEscape(raw), nil))
	return rec
}
func TestForwardAuthContext_GatewayStateRemainsUsable(t *testing.T) {
	for _, name := range []string{"Application", "", "  untrimmed  ", "<script>text</script>"} {
		t.Run(name, func(t *testing.T) {
			p, q, store, u := contextFixture(t)
			q.client.DisplayName = name
			key := faStateKey(u.Query().Get("state"))
			ctx := context.Background()
			raw, _ := store.Get(ctx, key)
			// Use a shorter lifetime so any accidental five-minute renewal is visible.
			if err := store.SetEx(ctx, key, raw, 45*time.Second); err != nil {
				t.Fatal(err)
			}
			before, _ := store.TTL(ctx, key)
			for range 2 {
				rec := readContext(p, u.String())
				if rec.Code != 200 || rec.Header().Get("Cache-Control") != "no-store" {
					t.Fatalf("response: %d %v %s", rec.Code, rec.Header(), rec.Body.String())
				}
				var body forwardAuthLoginContext
				if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				expected := name
				if expected == "" {
					expected = q.client.ForwardAuthHost.String
				}
				if body.Application == nil || body.Application.Label != expected {
					t.Fatalf("context %+v", body)
				}
				var fields map[string]json.RawMessage
				_ = json.Unmarshal(rec.Body.Bytes(), &fields)
				if len(fields) != 1 {
					t.Fatalf("unexpected data: %s", rec.Body.String())
				}
			}
			after, _ := store.Get(ctx, key)
			ttl, _ := store.TTL(ctx, key)
			if raw != after || ttl > before || ttl < before-1 {
				t.Fatalf("state or TTL changed: %d -> %d", before, ttl)
			}
			if st := popFAState(ctx, store, u.Query().Get("state")); st == nil || st.ClientID != "svc" {
				t.Fatal("callback cannot consume state")
			}
			if st := popFAState(ctx, store, u.Query().Get("state")); st != nil {
				t.Fatal("state is not single-use")
			}
		})
	}
}
func TestForwardAuthContext_NoMisleadingApplication(t *testing.T) {
	cases := []string{"ordinary", "no-state", "no-pkce", "unknown", "disabled", "not-forward-auth", "wrong-callback", "unregistered-callback", "wrong-challenge", "wrong-method", "missing-state", "expired-state", "wrong-client-state", "wrong-host-state", "malformed-state"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			p, q, store, u := contextFixture(t)
			v := u.Query()
			key := faStateKey(v.Get("state"))
			ctx := context.Background()
			switch name {
			case "ordinary":
				v.Del("state")
				v.Del("code_challenge")
				v.Del("code_challenge_method")
			case "no-state":
				v.Del("state")
			case "no-pkce":
				v.Del("code_challenge")
			case "unknown":
				v.Set("client_id", "unknown")
			case "disabled":
				q.client.Disabled = true
			case "not-forward-auth":
				q.client.ForwardAuthEnabled = false
			case "wrong-callback":
				v.Set("redirect_uri", "https://evil.example/callback")
			case "unregistered-callback":
				q.client.RedirectUris = nil
			case "wrong-challenge":
				v.Set("code_challenge", "incorrect")
			case "wrong-method":
				v.Set("code_challenge_method", "plain")
			case "missing-state":
				v.Set("state", "not-stored")
			case "expired-state":
				_ = store.SetEx(ctx, key, "{}", time.Nanosecond)
				time.Sleep(time.Millisecond)
			case "wrong-client-state", "wrong-host-state":
				raw, _ := store.Get(ctx, key)
				var st faState
				_ = json.Unmarshal([]byte(raw), &st)
				if name == "wrong-client-state" {
					st.ClientID = "other"
				} else {
					st.OriginalURL = "https://evil.example/private"
				}
				payload, _ := json.Marshal(st)
				_ = store.SetEx(ctx, key, string(payload), time.Minute)
			case "malformed-state":
				_ = store.SetEx(ctx, key, "broken", time.Minute)
			}
			u.RawQuery = v.Encode()
			rec := readContext(p, u.String())
			if rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != `{"application":null}` {
				t.Fatalf("response %d %s", rec.Code, rec.Body.String())
			}
		})
	}
}
func TestForwardAuthContext_RejectsAmbiguousInput(t *testing.T) {
	p, _, _, u := contextFixture(t)
	cases := []string{"", "/oauth/authorize?client_id=x", strings.Replace(u.String(), testIssuer, "https://evil.example", 1), u.String() + "#", u.String() + "#fragment", u.String() + "&state=other", u.String() + "&client_id=other", u.String() + "&redirect_uri=other", u.String() + "&scope=other", u.String() + "&code_challenge=other", u.String() + "&broken=%zz", u.String() + ";extra=one", strings.Replace(u.String(), "/oauth/authorize", "/oauth/%61uthorize", 1), strings.Repeat("x", 17000)}
	for _, field := range []string{"client_id", "redirect_uri", "response_type"} {
		v := u.Query()
		v.Del(field)
		copy := *u
		copy.RawQuery = v.Encode()
		cases = append(cases, copy.String())
	}
	withUser := *u
	withUser.User = url.User("user")
	cases = append(cases, withUser.String())
	for i, raw := range cases {
		rec := readContext(p, raw)
		if rec.Code != 400 {
			t.Errorf("case %d: %d %s", i, rec.Code, rec.Body.String())
		}
	}
	for _, query := range []string{"return_to=%zz", "return_to=" + url.QueryEscape(u.String()) + "&return_to=" + url.QueryEscape(u.String())} {
		rec := httptest.NewRecorder()
		p.HandleForwardAuthLoginContext(rec, httptest.NewRequest("GET", "/context?"+query, nil))
		if rec.Code != 400 {
			t.Errorf("outer query: %d", rec.Code)
		}
	}
}
func TestForwardAuthContext_StorageFailure(t *testing.T) {
	for _, kind := range []string{"database", "kv"} {
		t.Run(kind, func(t *testing.T) {
			p, q, _, u := contextFixture(t)
			if kind == "database" {
				q.err = errors.New("private database detail")
			} else {
				p.kv = contextReadStore{err: errors.New("private KV detail")}
			}
			rec := readContext(p, u.String())
			if rec.Code != 503 || !strings.Contains(rec.Body.String(), kind+"_unavailable") || strings.Contains(rec.Body.String(), "private") {
				t.Fatalf("response %d %s", rec.Code, rec.Body.String())
			}
		})
	}
}
