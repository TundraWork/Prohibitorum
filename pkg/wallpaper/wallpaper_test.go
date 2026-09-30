package wallpaper

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time          { return c.t }
func (c *clock) advance(d time.Duration) { c.t = c.t.Add(d) }

// upstream is an httptest server whose answer a test can swap mid-way.
type upstream struct {
	*httptest.Server
	hits    atomic.Int32
	handler atomic.Value // http.HandlerFunc
	last    atomic.Value // *http.Request
}

func newUpstream(t *testing.T, h http.HandlerFunc) *upstream {
	t.Helper()
	u := &upstream{}
	u.handler.Store(h)
	u.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.hits.Add(1)
		u.last.Store(r.Clone(context.Background()))
		u.handler.Load().(http.HandlerFunc)(w, r)
	}))
	t.Cleanup(u.Close)
	return u
}

func (u *upstream) set(h http.HandlerFunc) { u.handler.Store(h) }
func (u *upstream) request() *http.Request { return u.last.Load().(*http.Request) }

func respond(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}
}

const bingBody = `{"images":[{"urlbase":"/th?id=OHR.Reedling_ZH-CN123","title":"一张脸","copyright":"文须雀 (© A/B)","copyrightlink":"https://www.bing.com/search?q=x"}]}`

func newService(c *clock, bing, unsplash *upstream) *Service {
	o := Options{Now: c.now}
	if bing != nil {
		o.BingURL = bing.URL
	}
	if unsplash != nil {
		o.UnsplashURL = unsplash.URL
	}
	return New(o)
}

func TestBingParsesAndCaches(t *testing.T) {
	up := newUpstream(t, respond(http.StatusOK, bingBody))
	c := &clock{t: time.Unix(1_700_000_000, 0)}
	s := newService(c, up, nil)
	ctx := context.Background()

	got, err := s.Bing(ctx, "zh-CN")
	if err != nil {
		t.Fatal(err)
	}
	want := Picture{
		ImageURL:     "https://www.bing.com/th?id=OHR.Reedling_ZH-CN123_UHD.jpg&w=2560",
		Title:        "一张脸",
		Copyright:    "文须雀 (© A/B)",
		CopyrightURL: "https://www.bing.com/search?q=x",
	}
	if got != want {
		t.Fatalf("Bing() = %+v, want %+v", got, want)
	}
	q := up.request().URL.Query()
	if up.request().URL.Path != "/HPImageArchive.aspx" || q.Get("format") != "js" || q.Get("idx") != "0" || q.Get("n") != "1" || q.Get("mkt") != "zh-CN" {
		t.Fatalf("request = %s", up.request().URL)
	}

	c.advance(59 * time.Minute)
	if _, err := s.Bing(ctx, "zh-CN"); err != nil || up.hits.Load() != 1 {
		t.Fatalf("within the hour: hits = %d, err %v; want the cached answer", up.hits.Load(), err)
	}
	if _, err := s.Bing(ctx, "en-US"); err != nil || up.hits.Load() != 2 {
		t.Fatalf("another market: hits = %d, err %v; want a separate fetch", up.hits.Load(), err)
	}
	c.advance(2 * time.Minute)
	if _, err := s.Bing(ctx, "zh-CN"); err != nil || up.hits.Load() != 3 {
		t.Fatalf("after the hour: hits = %d, err %v; want a refresh", up.hits.Load(), err)
	}
}

func TestBingRejectsUnexpectedAddresses(t *testing.T) {
	for name, body := range map[string]string{
		"foreign picture":   `{"images":[{"urlbase":"//evil.test/x","title":"t","copyright":"c","copyrightlink":"https://www.bing.com/search?q=x"}]}`,
		"foreign copyright": `{"images":[{"urlbase":"/th?id=OHR.X","title":"t","copyright":"c","copyrightlink":"https://evil.test/"}]}`,
		"script copyright":  `{"images":[{"urlbase":"/th?id=OHR.X","title":"t","copyright":"c","copyrightlink":"javascript:alert(1)"}]}`,
		"no pictures":       `{"images":[]}`,
		"not json":          `<html>`,
	} {
		t.Run(name, func(t *testing.T) {
			up := newUpstream(t, respond(http.StatusOK, body))
			s := newService(&clock{t: time.Unix(1_700_000_000, 0)}, up, nil)
			if _, err := s.Bing(context.Background(), "zh-CN"); !errors.Is(err, ErrUnavailable) {
				t.Fatalf("err = %v, want ErrUnavailable", err)
			}
		})
	}
}

func TestBingServesStaleOnError(t *testing.T) {
	up := newUpstream(t, respond(http.StatusOK, bingBody))
	c := &clock{t: time.Unix(1_700_000_000, 0)}
	s := newService(c, up, nil)
	ctx := context.Background()
	first, err := s.Bing(ctx, "zh-CN")
	if err != nil {
		t.Fatal(err)
	}

	up.set(respond(http.StatusBadGateway, ``))
	c.advance(2 * time.Hour)
	got, err := s.Bing(ctx, "zh-CN")
	if err != nil || got != first {
		t.Fatalf("refresh failure: got %+v, err %v; want the stale picture", got, err)
	}
	// The failure is remembered for a minute rather than retried per request.
	hits := up.hits.Load()
	if _, err := s.Bing(ctx, "zh-CN"); err != nil || up.hits.Load() != hits {
		t.Fatalf("retry within a minute: hits %d → %d, err %v", hits, up.hits.Load(), err)
	}
}

func TestBingUnavailableWithoutCache(t *testing.T) {
	up := newUpstream(t, respond(http.StatusInternalServerError, ``))
	c := &clock{t: time.Unix(1_700_000_000, 0)}
	s := newService(c, up, nil)
	if _, err := s.Bing(context.Background(), "zh-CN"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
	if _, err := s.Bing(context.Background(), "zh-CN"); !errors.Is(err, ErrUnavailable) || up.hits.Load() != 1 {
		t.Fatalf("second call: hits = %d, err %v; want the remembered failure", up.hits.Load(), err)
	}
	up.set(respond(http.StatusOK, bingBody))
	c.advance(time.Minute + time.Second)
	if _, err := s.Bing(context.Background(), "zh-CN"); err != nil {
		t.Fatalf("after the retry delay: %v", err)
	}
}

func unsplashPhoto(i int) string {
	return fmt.Sprintf(`{"urls":{"raw":"https://images.unsplash.com/photo-%d?ixid=abc"},"links":{"html":"https://unsplash.com/photos/p%d"},"user":{"name":"Person %d","links":{"html":"https://unsplash.com/@person%d"}}}`, i, i, i, i)
}

func unsplashBody(n int) string {
	var photos []string
	for i := range n {
		photos = append(photos, unsplashPhoto(i))
	}
	return "[" + strings.Join(photos, ",") + "]"
}

func TestUnsplashRequestAndCredit(t *testing.T) {
	up := newUpstream(t, respond(http.StatusOK, unsplashBody(1)))
	s := newService(&clock{t: time.Unix(1_700_000_000, 0)}, nil, up)
	got, err := s.Unsplash(context.Background(), "key-1", "snowy mountains")
	if err != nil {
		t.Fatal(err)
	}
	r := up.request()
	q := r.URL.Query()
	if r.URL.Path != "/photos/random" || q.Get("count") != "30" || q.Get("orientation") != "landscape" ||
		q.Get("content_filter") != "high" || q.Get("query") != "snowy mountains" {
		t.Fatalf("request = %s", r.URL)
	}
	if r.Header.Get("Authorization") != "Client-ID key-1" || r.Header.Get("Accept-Version") != "v1" {
		t.Fatalf("headers = %v", r.Header)
	}
	want := Picture{
		ImageURL:        "https://images.unsplash.com/photo-0?ixid=abc&w=2560&q=80&auto=format",
		Photographer:    "Person 0",
		PhotographerURL: "https://unsplash.com/@person0?utm_source=prohibitorum&utm_medium=referral",
		PhotoURL:        "https://unsplash.com/photos/p0?utm_source=prohibitorum&utm_medium=referral",
	}
	if got != want {
		t.Fatalf("Unsplash() = %+v, want %+v", got, want)
	}

	if _, err := s.Unsplash(context.Background(), "key-1", ""); err != nil {
		t.Fatal(err)
	}
	if up.request().URL.Query().Has("query") {
		t.Fatalf("an empty keyword sent query=: %s", up.request().URL)
	}
}

func TestUnsplashBatchIsCachedAndShuffled(t *testing.T) {
	up := newUpstream(t, respond(http.StatusOK, unsplashBody(30)))
	s := newService(&clock{t: time.Unix(1_700_000_000, 0)}, nil, up)
	seen := map[string]bool{}
	for range 50 {
		p, err := s.Unsplash(context.Background(), "key-1", "")
		if err != nil {
			t.Fatal(err)
		}
		seen[p.ImageURL] = true
	}
	if up.hits.Load() != 1 {
		t.Fatalf("hits = %d, want one batch fetch", up.hits.Load())
	}
	if len(seen) < 2 {
		t.Fatalf("50 requests returned %d distinct photos, want a random pick from the batch", len(seen))
	}
	// A different key has its own batch.
	if _, err := s.Unsplash(context.Background(), "key-2", ""); err != nil || up.hits.Load() != 2 {
		t.Fatalf("other key: hits = %d, err %v", up.hits.Load(), err)
	}
}

func TestUnsplashKeyRejected(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		up := newUpstream(t, respond(status, `{"errors":["OAuth error"]}`))
		s := newService(&clock{t: time.Unix(1_700_000_000, 0)}, nil, up)
		if _, err := s.Unsplash(context.Background(), "bad", ""); !errors.Is(err, ErrKeyRejected) {
			t.Fatalf("%d: Unsplash err = %v, want ErrKeyRejected", status, err)
		}
		if err := s.Verify(context.Background(), "bad"); !errors.Is(err, ErrKeyRejected) {
			t.Fatalf("%d: Verify err = %v, want ErrKeyRejected", status, err)
		}
	}
}

func TestUnsplashVerify(t *testing.T) {
	up := newUpstream(t, respond(http.StatusOK, unsplashBody(1)))
	s := newService(&clock{t: time.Unix(1_700_000_000, 0)}, nil, up)
	if err := s.Verify(context.Background(), "good"); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got := up.request().URL.Query().Get("count"); got != "1" {
		t.Fatalf("Verify asked for %s photos, want 1", got)
	}
	up.set(respond(http.StatusServiceUnavailable, ``))
	if err := s.Verify(context.Background(), "good"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Verify on outage: err = %v, want ErrUnavailable", err)
	}
}

func TestUnsplashForgetRefetches(t *testing.T) {
	up := newUpstream(t, respond(http.StatusOK, unsplashBody(3)))
	s := newService(&clock{t: time.Unix(1_700_000_000, 0)}, nil, up)
	ctx := context.Background()
	if _, err := s.Unsplash(ctx, "key", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Unsplash(ctx, "key", ""); err != nil || up.hits.Load() != 1 {
		t.Fatalf("hits = %d, err %v; want cached", up.hits.Load(), err)
	}
	s.Forget()
	if _, err := s.Unsplash(ctx, "key", ""); err != nil || up.hits.Load() != 2 {
		t.Fatalf("after Forget: hits = %d, err %v; want a fresh fetch", up.hits.Load(), err)
	}
}

func TestUnsplashSkipsForeignAddresses(t *testing.T) {
	body := `[{"urls":{"raw":"https://evil.test/p"},"links":{"html":"https://unsplash.com/photos/p"},"user":{"name":"N","links":{"html":"https://unsplash.com/@n"}}}]`
	up := newUpstream(t, respond(http.StatusOK, body))
	s := newService(&clock{t: time.Unix(1_700_000_000, 0)}, nil, up)
	if _, err := s.Unsplash(context.Background(), "key", ""); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
}

func TestUnsplashServesStaleOnError(t *testing.T) {
	up := newUpstream(t, respond(http.StatusOK, unsplashBody(1)))
	c := &clock{t: time.Unix(1_700_000_000, 0)}
	s := newService(c, nil, up)
	first, err := s.Unsplash(context.Background(), "key", "")
	if err != nil {
		t.Fatal(err)
	}
	up.set(respond(http.StatusInternalServerError, ``))
	c.advance(2 * time.Hour)
	if got, err := s.Unsplash(context.Background(), "key", ""); err != nil || got != first {
		t.Fatalf("got %+v, err %v; want the stale photo", got, err)
	}
}
