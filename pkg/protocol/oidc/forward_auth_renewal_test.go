package oidc

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"prohibitorum/pkg/db"
	"prohibitorum/pkg/kv"
)

func TestForwardAuthVerify_SlidingRenewal(t *testing.T) {
	for _, proto := range []string{"https", "http"} {
		t.Run(proto, func(t *testing.T) {
			ctx := context.Background()
			q := &fakeFAQueries{faClient: db.GetForwardAuthClientByHostRow{ClientID: "svc"}, authorized: true, acct: db.Account{ID: 42, Username: "alice"}}
			p, store := newFAProvider(q)
			// Legacy JSON with different whitespace must still be conditionally renewed.
			const token = "legacy-session"
			const raw = "{ \"client_id\": \"svc\", \"account_id\": 42 }"
			for i := 0; i < 2; i++ {
				if err := store.SetEx(ctx, faSessionKey(token), raw, 10*time.Minute); err != nil {
					t.Fatal(err)
				}
				rec := httptest.NewRecorder()
				p.HandleForwardAuthVerify(rec, faRequest(proto, "app.acme.io", "/", faCookie(proto == "https", token)))
				if rec.Code != http.StatusOK || rec.Header().Get("Remote-User") != "alice" {
					t.Fatalf("renewal: status %d", rec.Code)
				}
				cookies := rec.Result().Cookies()
				if len(cookies) != 1 {
					t.Fatalf("renewal cookies: %d", len(cookies))
				}
				c := cookies[0]
				if c.Value != token || c.Name != faCookieName(proto == "https") || c.Secure != (proto == "https") || !c.HttpOnly || c.Domain != "" || c.Path != "/" || c.SameSite != http.SameSiteLaxMode || c.MaxAge != 0 {
					t.Fatalf("cookie attributes: %+v", c)
				}
				ttl, err := store.TTL(ctx, faSessionKey(token))
				if err != nil || ttl < 3599 {
					t.Fatalf("renewed TTL=%d err=%v", ttl, err)
				}
				// Immediately following requests keep the session, without more Set-Cookie.
				rec = httptest.NewRecorder()
				p.HandleForwardAuthVerify(rec, faRequest(proto, "app.acme.io", "/", c))
				if rec.Code != http.StatusOK || len(rec.Result().Cookies()) != 0 {
					t.Fatalf("fresh request: status=%d cookies=%d", rec.Code, len(rec.Result().Cookies()))
				}
			}
		})
	}
}

func TestForwardAuthVerify_DeniedRequestsDoNotRenew(t *testing.T) {
	for _, scenario := range []string{"access", "account", "app", "maintenance", "wrong-client", "pat", "expired"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			q := &fakeFAQueries{faClient: db.GetForwardAuthClientByHostRow{ClientID: "svc"}, authorized: true, acct: db.Account{ID: 42, Username: "alice"}}
			p, store := newFAProvider(q)
			client := "svc"
			want := http.StatusFound
			switch scenario {
			case "access":
				q.authorized = false
			case "account":
				q.acct.Disabled = true
			case "app":
				q.faClient.Disabled = true
				want = http.StatusForbidden
			case "maintenance":
				p.maintenance = func(context.Context) bool { return true }
				want = http.StatusServiceUnavailable
			case "wrong-client":
				client = "other"
			case "pat":
				q.patErr = pgx.ErrNoRows
				want = http.StatusUnauthorized
			}
			token, err := mintFASession(ctx, store, faSession{AccountID: 42, ClientID: client}, 10*time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "expired" {
				if err := store.Del(ctx, faSessionKey(token)); err != nil {
					t.Fatal(err)
				}
			}
			req := faRequest("https", "app.acme.io", "/", faCookie(true, token))
			if scenario == "pat" {
				req.Header.Set("X-Prohibitorum-PAT", "invalid")
			}
			rec := httptest.NewRecorder()
			p.HandleForwardAuthVerify(rec, req)
			if rec.Code != want || len(rec.Result().Cookies()) != 0 {
				t.Fatalf("status=%d want=%d cookies=%d", rec.Code, want, len(rec.Result().Cookies()))
			}
			ttl, err := store.TTL(ctx, faSessionKey(token))
			if err != nil || ttl > 600 {
				t.Fatalf("denied request extended TTL=%d err=%v", ttl, err)
			}
		})
	}
}

type renewalRaceStore struct {
	kv.Store
	beforeCAS func(context.Context, string)
	casErr    error
}

func (s *renewalRaceStore) CompareAndSwap(ctx context.Context, key, old, next string, ttl time.Duration) (bool, error) {
	if s.beforeCAS != nil {
		s.beforeCAS(ctx, key)
	}
	if s.casErr != nil {
		return false, s.casErr
	}
	return s.Store.CompareAndSwap(ctx, key, old, next, ttl)
}

func TestForwardAuthVerify_RenewalCannotResurrectSession(t *testing.T) {
	for _, scenario := range []string{"sign-out", "changed", "store-error"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			q := &fakeFAQueries{faClient: db.GetForwardAuthClientByHostRow{ClientID: "svc"}, authorized: true, acct: db.Account{ID: 42}}
			p, store := newFAProvider(q)
			token, err := mintFASession(ctx, store, faSession{AccountID: 42, ClientID: "svc"}, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			race := &renewalRaceStore{Store: store}
			switch scenario {
			case "sign-out":
				race.beforeCAS = func(ctx context.Context, key string) {
					_, err := store.Pop(ctx, key)
					if err != nil {
						t.Fatal(err)
					}
				}
			case "changed":
				race.beforeCAS = func(ctx context.Context, key string) {
					if err := store.SetEx(ctx, key, "changed", time.Minute); err != nil {
						t.Fatal(err)
					}
				}
			case "store-error":
				race.casErr = errors.New("unavailable")
			}
			p.kv = race
			rec := httptest.NewRecorder()
			p.HandleForwardAuthVerify(rec, faRequest("https", "app.acme.io", "/", faCookie(true, token)))
			if rec.Code != http.StatusServiceUnavailable || len(rec.Result().Cookies()) != 0 || rec.Header().Get("Remote-User") != "" {
				t.Fatalf("failed renewal allowed: status=%d", rec.Code)
			}
			ttl, err := store.TTL(ctx, faSessionKey(token))
			if err != nil || ttl > 60 {
				t.Fatalf("failed renewal extended TTL=%d err=%v", ttl, err)
			}
			if scenario == "sign-out" && ttl != -2 {
				t.Fatalf("deleted session resurrected: TTL=%d", ttl)
			}
		})
	}
}

func TestForwardAuth_ConcurrentRenewalAndSignOut(t *testing.T) {
	ctx := context.Background()
	store := kv.NewMemoryStore()
	token, err := mintFASession(ctx, store, faSession{AccountID: 42, ClientID: "svc"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	s := loadFASession(ctx, store, token)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Go(func() { _, _ = renewFASession(ctx, store, token, s, time.Hour) })
	}
	wg.Go(func() { _, _ = store.Pop(ctx, faSessionKey(token)) })
	wg.Wait()
	if loadFASession(ctx, store, token) != nil {
		t.Fatal("renewal resurrected signed-out session")
	}
}

// Run against an isolated Redis instance with PROHIBITORUM_TEST_REDIS_ADDR.
// Uses unique tokens and deletes only this test's keys.
func TestForwardAuth_RedisRenewalIntegration(t *testing.T) {
	addr := os.Getenv("PROHIBITORUM_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("set PROHIBITORUM_TEST_REDIS_ADDR for Redis integration")
	}
	store, err := kv.NewRedisStore(kv.RedisConfig{Addr: addr})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	q := &fakeFAQueries{faClient: db.GetForwardAuthClientByHostRow{ClientID: "svc"}, authorized: true, acct: db.Account{ID: 42}}
	p, _ := newFAProvider(q)
	p.kv = store
	p.cfg.ForwardAuth.SessionTTL = 8 * time.Second
	token, err := mintFASession(ctx, store, faSession{AccountID: 42, ClientID: "svc"}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Del(ctx, faSessionKey(token))
	verify := func() *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		p.HandleForwardAuthVerify(rec, faRequest("https", "app.acme.io", "/", faCookie(true, token)))
		return rec
	}
	rec := verify()
	if rec.Code != http.StatusOK || hasFACookie(rec, true) != token {
		t.Fatalf("renewal: status %d", rec.Code)
	}
	time.Sleep(1200 * time.Millisecond)
	// Beyond the original TTL: renewed session still works, without another cookie.
	rec = verify()
	if rec.Code != http.StatusOK || len(rec.Result().Cookies()) != 0 {
		t.Fatalf("after original expiry: status %d", rec.Code)
	}
	signout := httptest.NewRecorder()
	p.HandleForwardAuthSignOut(signout, faRequest("https", "app.acme.io", ForwardAuthPathPrefix+"/sign_out", faCookie(true, token)))
	if rec = verify(); rec.Code != http.StatusFound {
		t.Fatalf("after sign-out: status %d", rec.Code)
	}
	// CAS against a genuinely expired key must not recreate it.
	expired, err := mintFASession(ctx, store, faSession{AccountID: 42, ClientID: "svc"}, 100*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Del(ctx, faSessionKey(expired))
	s := loadFASession(ctx, store, expired)
	if s == nil {
		t.Fatal("could not load expiring fixture")
	}
	time.Sleep(150 * time.Millisecond)
	if renewed, err := renewFASession(ctx, store, expired, s, time.Hour); renewed || err == nil {
		t.Fatalf("expired renewal: renewed=%v err=%v", renewed, err)
	}
	if loadFASession(ctx, store, expired) != nil {
		t.Fatal("expired key recreated")
	}
}
