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
	loads  int
}

func (q *contextQueries) GetOIDCClientAny(_ context.Context, id string) (db.OidcClient, error) {
	q.loads++
	if q.err != nil {
		return db.OidcClient{}, q.err
	}
	if id != q.client.ClientID {
		return db.OidcClient{}, pgx.ErrNoRows
	}
	return q.client, nil
}

func ordinaryAuthorizeURL(redirectURI string, mutations ...func(url.Values)) string {
	v := url.Values{}
	v.Set("client_id", "svc")
	v.Set("redirect_uri", redirectURI)
	v.Set("response_type", "code")
	v.Set("scope", "openid")
	for _, mutate := range mutations {
		mutate(v)
	}
	return testIssuer + "/oauth/authorize?" + v.Encode()
}

func decodeContext(t *testing.T, rec *httptest.ResponseRecorder) forwardAuthLoginContext {
	t.Helper()
	var body forwardAuthLoginContext
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body
}

func TestForwardAuthContext_OrdinaryOIDC(t *testing.T) {
	cases := []struct {
		name        string
		client      db.OidcClient
		redirectURI string
		label       string
	}{
		{"named client", db.OidcClient{ClientID: "svc", DisplayName: "Wiki", RedirectUris: []string{"https://rp.example.test/callback"}}, "https://rp.example.test/callback", "Wiki"},
		{"empty name falls back to the request's redirect host", db.OidcClient{ClientID: "svc", RedirectUris: []string{"https://internal.example.test/callback", "https://public.example.test/cb"}}, "https://public.example.test/cb", "public.example.test"},
		{"host includes port", db.OidcClient{ClientID: "svc", RedirectUris: []string{"https://rp.example.test:8443/callback"}}, "https://rp.example.test:8443/callback", "rp.example.test:8443"},
		{"name kept as registered, not trimmed", db.OidcClient{ClientID: "svc", DisplayName: "  spaced  ", RedirectUris: []string{"https://rp.example.test/callback"}}, "https://rp.example.test/callback", "  spaced  "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, q, _, _ := contextFixture(t)
			q.client = tc.client
			// Without state/PKCE, and with them: a non-forward-auth client
			// takes the ordinary branch either way.
			requests := []string{ordinaryAuthorizeURL(tc.redirectURI), ordinaryAuthorizeURL(tc.redirectURI, func(v url.Values) {
				v.Set("state", "st")
				v.Set("code_challenge", "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM")
				v.Set("code_challenge_method", "S256")
			})}
			for _, raw := range requests {
				rec := readContext(p, raw)
				if rec.Code != 200 {
					t.Fatalf("response %d %s", rec.Code, rec.Body.String())
				}
				body := decodeContext(t, rec)
				if body.Application == nil || body.Application.Label != tc.label {
					t.Fatalf("context %+v, want label %q", body, tc.label)
				}
			}
		})
	}
}

func TestForwardAuthContext_OrdinaryOIDCNoMisleadingLabel(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*contextQueries, url.Values)
		code   int
		body   string
	}{
		{"unknown client", func(_ *contextQueries, v url.Values) { v.Set("client_id", "nobody") }, 200, `{"application":null}`},
		{"unregistered redirect", func(_ *contextQueries, v url.Values) { v.Set("redirect_uri", "https://evil.example.test/cb") }, 200, `{"application":null}`},
		{"disabled client", func(q *contextQueries, _ url.Values) { q.client.Disabled = true }, 200, `{"application":null}`},
		{"unsupported response type", func(_ *contextQueries, v url.Values) { v.Set("response_type", "token") }, 200, `{"application":null}`},
		{"database failure", func(q *contextQueries, _ url.Values) { q.err = errors.New("down") }, 503, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, q, _, _ := contextFixture(t)
			q.client = db.OidcClient{ClientID: "svc", DisplayName: "Wiki", RedirectUris: []string{"https://rp.example.test/callback"}}
			request, err := url.Parse(ordinaryAuthorizeURL(q.client.RedirectUris[0]))
			if err != nil {
				t.Fatal(err)
			}
			query := request.Query()
			tc.mutate(q, query)
			request.RawQuery = query.Encode()
			rec := readContext(p, request.String())
			if rec.Code != tc.code {
				t.Fatalf("response %d %s", rec.Code, rec.Body.String())
			}
			if tc.body != "" && strings.TrimSpace(rec.Body.String()) != tc.body {
				t.Fatalf("body %s, want %s", rec.Body.String(), tc.body)
			}
			if tc.code == 503 && !strings.Contains(rec.Body.String(), "database_unavailable") {
				t.Fatalf("body %s", rec.Body.String())
			}
		})
	}
}

func TestForwardAuthContext_SAMLDelegates(t *testing.T) {
	for _, path := range []string{"/saml/sso?SAMLRequest=abc", "/saml/sso/init?sp=https%3A%2F%2Fsp.example.test"} {
		t.Run(path, func(t *testing.T) {
			p, _, _, _ := contextFixture(t)
			var seen string
			p.samlContext = func(_ context.Context, u *url.URL) (string, bool, error) {
				seen = u.EscapedPath()
				return "Smoke SP", true, nil
			}
			rec := readContext(p, testIssuer+path)
			if rec.Code != 200 {
				t.Fatalf("response %d %s", rec.Code, rec.Body.String())
			}
			body := decodeContext(t, rec)
			if body.Application == nil || body.Application.Label != "Smoke SP" {
				t.Fatalf("context %+v", body)
			}
			if seen == "" {
				t.Fatal("resolver was not consulted")
			}
		})
	}
	// No resolver wired (bare provider): no context, not an error.
	t.Run("no resolver", func(t *testing.T) {
		p, _, _, _ := contextFixture(t)
		rec := readContext(p, testIssuer+"/saml/sso?SAMLRequest=abc")
		if rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != `{"application":null}` {
			t.Fatalf("response %d %s", rec.Code, rec.Body.String())
		}
	})
}

func TestForwardAuthContext_SAMLStorageFailure(t *testing.T) {
	p, _, _, _ := contextFixture(t)
	p.samlContext = func(context.Context, *url.URL) (string, bool, error) {
		return "", false, errors.New("database down")
	}
	rec := readContext(p, testIssuer+"/saml/sso?SAMLRequest=abc")
	if rec.Code != 503 || !strings.Contains(rec.Body.String(), "database_unavailable") {
		t.Fatalf("response %d %s", rec.Code, rec.Body.String())
	}
}

// The number of client loads must not depend on what the request names:
// an extra load on any branch would let a caller time the difference and
// learn which client_ids exist.
func TestForwardAuthContext_SingleClientLoad(t *testing.T) {
	cases := []struct {
		name string
		set  func(q *contextQueries, v url.Values)
	}{
		{"unknown client", func(_ *contextQueries, v url.Values) { v.Set("client_id", "nobody") }},
		{"ordinary request", func(_ *contextQueries, v url.Values) { v.Del("state"); v.Del("code_challenge"); v.Del("code_challenge_method") }},
		{"not forward-auth bound", func(q *contextQueries, _ url.Values) { q.client.ForwardAuthEnabled = false }},
		{"disabled client", func(q *contextQueries, _ url.Values) { q.client.Disabled = true }},
		{"valid forward-auth", func(_ *contextQueries, _ url.Values) {}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, q, _, u := contextFixture(t)
			v := u.Query()
			tc.set(q, v)
			u.RawQuery = v.Encode()
			q.loads = 0
			readContext(p, u.String())
			if q.loads != 1 {
				t.Fatalf("%s: %d client loads, want exactly 1", tc.name, q.loads)
			}
		})
	}
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
	// The "ordinary"/"no-state"/"no-pkce"/"not-forward-auth"/"wrong-method"
	// shapes are ordinary OIDC requests now, which earn the registered
	// client's label (TestForwardAuthContext_OrdinaryOIDC); what remains are
	// forward-auth failures that must never leak a label.
	cases := []string{"unknown", "disabled", "wrong-callback", "unregistered-callback", "wrong-challenge", "missing-state", "expired-state", "wrong-client-state", "wrong-host-state", "malformed-state"}
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
