package wallpaper

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newPixabayService points the search and the image allow-list at the test
// server, so Pixabay's whole flow runs against it. client lets a TLS test
// server be trusted.
func newPixabayService(c *clock, api, imgHost string, client *http.Client) *Service {
	return New(Options{Now: c.now, PixabayURL: api, ImageHosts: []string{imgHost}, Client: client, ImageClient: client})
}

// newTLSUpstream serves https, so the image path — https only — can run
// against it.
func newTLSUpstream(t *testing.T, h http.HandlerFunc) *upstream {
	t.Helper()
	u := &upstream{}
	u.handler.Store(h)
	u.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.hits.Add(1)
		u.last.Store(r.Clone(context.Background()))
		u.handler.Load().(http.HandlerFunc)(w, r)
	}))
	t.Cleanup(u.Close)
	return u
}

// newTLSUpstreamOnHost serves https bound to a specific loopback address, so
// its hostname differs from 127.0.0.1's and a host-allow-list check can be
// told apart from the scheme check.
func newTLSUpstreamOnHost(t *testing.T, host string, h http.HandlerFunc) *upstream {
	t.Helper()
	listener, err := net.Listen("tcp", host+":0")
	if err != nil {
		t.Skipf("cannot bind %s: %v", host, err)
	}
	u := &upstream{}
	u.handler.Store(h)
	u.Server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.hits.Add(1)
		u.last.Store(r.Clone(context.Background()))
		u.handler.Load().(http.HandlerFunc)(w, r)
	}))
	u.Server.Listener = listener
	u.Server.StartTLS()
	t.Cleanup(u.Close)
	return u
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		panic(err)
	}
	return u.Hostname()
}

func pixabayHit(i int, imgBase string) string {
	return fmt.Sprintf(`{"id":%d,"pageURL":"https://pixabay.com/photos/ridge-%d/","largeImageURL":"%s/Img%d.jpg","fullHDURL":null,"user":"Author %d"}`, i, i, imgBase, i, i)
}

func pixabayBody(imgBase string, ids ...int) string {
	hits := make([]string, len(ids))
	for i, id := range ids {
		hits[i] = pixabayHit(id, imgBase)
	}
	return `{"hits":[` + strings.Join(hits, ",") + `]}`
}

func TestPixabayRequestAndCredit(t *testing.T) {
	up := newUpstream(t, respond(http.StatusOK, pixabayBody("https://pixabay.com/get", 11, 12)))
	c := &clock{t: time.Unix(1_700_000_000, 0)}
	s := newPixabayService(c, up.URL, "pixabay.com", nil)
	got, err := s.Pixabay(context.Background(), "key-1", "雪山", "zh")
	if err != nil {
		t.Fatal(err)
	}
	r := up.request()
	q := r.URL.Query()
	if r.URL.Path != "/" || q.Get("key") != "key-1" || q.Get("q") != "雪山" || q.Get("lang") != "zh" ||
		q.Get("image_type") != "photo" || q.Get("orientation") != "horizontal" ||
		q.Get("min_width") != "1920" || q.Get("safesearch") != "true" ||
		q.Get("order") != "popular" || q.Get("per_page") != "20" {
		t.Fatalf("request = %s", r.URL)
	}
	want := []Picture{
		{ImageURL: "/branding/wallpaper/images/pixabay/11", Photographer: "Author 11", PhotoURL: "https://pixabay.com/photos/ridge-11/"},
		{ImageURL: "/branding/wallpaper/images/pixabay/12", Photographer: "Author 12", PhotoURL: "https://pixabay.com/photos/ridge-12/"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("Pixabay() = %+v, want %+v", got, want)
	}
	for _, p := range got {
		if p.Title != "" || p.PhotographerURL != "" {
			t.Fatalf("credit carries more than the author: %+v", p)
		}
	}

	// Another language is another cache entry and another search.
	if _, err := s.Pixabay(context.Background(), "key-1", "雪山", "en"); err != nil || up.hits.Load() != 2 {
		t.Fatalf("other lang: hits = %d, err %v; want a separate fetch", up.hits.Load(), err)
	}
	if got := up.request().URL.Query().Get("lang"); got != "en" {
		t.Fatalf("lang = %q, want en", got)
	}
}

func TestPixabayCachesForADay(t *testing.T) {
	up := newUpstream(t, respond(http.StatusOK, pixabayBody("https://pixabay.com/get", 1)))
	c := &clock{t: time.Unix(1_700_000_000, 0)}
	s := newPixabayService(c, up.URL, "pixabay.com", nil)
	ctx := context.Background()
	if _, err := s.Pixabay(ctx, "key", "", "zh"); err != nil {
		t.Fatal(err)
	}
	c.advance(23 * time.Hour)
	if _, err := s.Pixabay(ctx, "key", "", "zh"); err != nil || up.hits.Load() != 1 {
		t.Fatalf("within a day: hits = %d, err %v; want the cached batch", up.hits.Load(), err)
	}
	c.advance(2 * time.Hour)
	if _, err := s.Pixabay(ctx, "key", "", "zh"); err != nil || up.hits.Load() != 2 {
		t.Fatalf("after a day: hits = %d, err %v; want a refresh", up.hits.Load(), err)
	}
}

func TestPixabayServesStaleOnError(t *testing.T) {
	up := newUpstream(t, respond(http.StatusOK, pixabayBody("https://pixabay.com/get", 1, 2)))
	c := &clock{t: time.Unix(1_700_000_000, 0)}
	s := newPixabayService(c, up.URL, "pixabay.com", nil)
	ctx := context.Background()
	first, err := s.Pixabay(ctx, "key", "", "zh")
	if err != nil {
		t.Fatal(err)
	}
	up.set(respond(http.StatusInternalServerError, ``))
	c.advance(25 * time.Hour)
	got, err := s.Pixabay(ctx, "key", "", "zh")
	if err != nil || !slices.Equal(got, first) {
		t.Fatalf("got %+v, err %v; want the stale batch", got, err)
	}
}

func TestPixabayUnavailableWithoutCache(t *testing.T) {
	up := newUpstream(t, respond(http.StatusTooManyRequests, ``))
	c := &clock{t: time.Unix(1_700_000_000, 0)}
	s := newPixabayService(c, up.URL, "pixabay.com", nil)
	if _, err := s.Pixabay(context.Background(), "key", "", "zh"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
	if _, err := s.Pixabay(context.Background(), "key", "", "zh"); !errors.Is(err, ErrUnavailable) || up.hits.Load() != 1 {
		t.Fatalf("second call: hits = %d, err %v; want the remembered failure", up.hits.Load(), err)
	}
}

func TestPixabayKeyRejected(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden} {
		up := newUpstream(t, respond(status, `[ERROR 400] Invalid or missing API key`))
		c := &clock{t: time.Unix(1_700_000_000, 0)}
		s := newPixabayService(c, up.URL, "pixabay.com", nil)
		if _, err := s.Pixabay(context.Background(), "bad", "", "zh"); !errors.Is(err, ErrPixabayKeyRejected) {
			t.Fatalf("%d: Pixabay err = %v, want ErrPixabayKeyRejected", status, err)
		}
		if err := s.VerifyPixabay(context.Background(), "bad"); !errors.Is(err, ErrPixabayKeyRejected) {
			t.Fatalf("%d: VerifyPixabay err = %v, want ErrPixabayKeyRejected", status, err)
		}
	}
}

func TestPixabayVerify(t *testing.T) {
	up := newUpstream(t, respond(http.StatusOK, pixabayBody("https://pixabay.com/get", 1)))
	c := &clock{t: time.Unix(1_700_000_000, 0)}
	s := newPixabayService(c, up.URL, "pixabay.com", nil)
	if err := s.VerifyPixabay(context.Background(), "good"); err != nil {
		t.Fatalf("VerifyPixabay: %v", err)
	}
	if up.request().URL.Query().Has("q") {
		t.Fatalf("VerifyPixabay sent a keyword: %s", up.request().URL)
	}
	// The verification answer does not enter the cache.
	if _, err := s.Pixabay(context.Background(), "good", "", "en"); err != nil || up.hits.Load() != 2 {
		t.Fatalf("after verify: hits = %d, err %v; want a fresh search", up.hits.Load(), err)
	}
}

func TestPixabaySkipsForeignAddresses(t *testing.T) {
	body := `{"hits":[{"id":1,"pageURL":"http://pixabay.com/photos/x/","largeImageURL":"https://pixabay.com/get/a.jpg","user":"A"},{"id":2,"pageURL":"https://pixabay.com/photos/y/","largeImageURL":"https://evil.test/y.jpg","user":"B"},{"id":3,"pageURL":"https://evil.test/y/","largeImageURL":"https://pixabay.com/get/c.jpg","user":"C"}]}`
	up := newUpstream(t, respond(http.StatusOK, body))
	c := &clock{t: time.Unix(1_700_000_000, 0)}
	s := newPixabayService(c, up.URL, "pixabay.com", nil)
	if _, err := s.Pixabay(context.Background(), "key", "", "zh"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable once every hit is dropped", err)
	}
}

func TestPixabayErrorTextHasNoKey(t *testing.T) {
	const key = "secret-key-value"
	up := newUpstream(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	// Point the service at a closed server: the URL carries the key.
	up.Close()
	c := &clock{t: time.Unix(1_700_000_000, 0)}
	s := newPixabayService(c, up.URL, "pixabay.com", nil)
	_, err := s.Pixabay(context.Background(), key, "", "zh")
	if err == nil {
		t.Fatal("want an error")
	}
	if strings.Contains(err.Error(), key) {
		t.Fatalf("error quotes the key: %v", err)
	}
}

func TestPixabayForgetKeepsTheCache(t *testing.T) {
	up := newUpstream(t, respond(http.StatusOK, pixabayBody("https://pixabay.com/get", 1)))
	c := &clock{t: time.Unix(1_700_000_000, 0)}
	s := newPixabayService(c, up.URL, "pixabay.com", nil)
	ctx := context.Background()
	if _, err := s.Pixabay(ctx, "key", "", "zh"); err != nil {
		t.Fatal(err)
	}
	s.Forget()
	if _, err := s.Pixabay(ctx, "key", "", "zh"); err != nil || up.hits.Load() != 1 {
		t.Fatalf("after Forget: hits = %d, err %v; want the cached batch kept", up.hits.Load(), err)
	}
}

/* --------------------------------------------------------------- images -- */

// pngBytes is a real picture for the image path: DetectContentType reads its
// magic bytes.
func pngBytes(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 3))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// imageWorld is a Pixabay API and its picture host on one TLS test server
// (the download path is https-only), with the page and picture addresses on
// the server's own host so they stay on the allow-list.
type imageWorld struct {
	*upstream
	imageHits atomic.Int32
}

func newImageWorld(t *testing.T, ids ...int) (*imageWorld, *Service, *clock) {
	t.Helper()
	w := &imageWorld{}
	w.upstream = newTLSUpstream(t, http.HandlerFunc(func(wr http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			wr.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(wr, `{"hits":[%s]}`, strings.Join(pixabayHitIDs(r.Host, ids), ","))
			return
		}
		w.imageHits.Add(1)
		wr.Header().Set("Content-Type", "image/png")
		wr.Write(pngBytes(t))
	}))
	c := &clock{t: time.Unix(1_700_000_000, 0)}
	return w, newPixabayService(c, w.URL, hostOf(w.URL), w.Client()), c
}

// pixabayHitIDs writes hits whose page and picture addresses are the test
// server itself, so downloads stay on the allow-list.
func pixabayHitIDs(host string, ids []int) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = fmt.Sprintf(`{"id":%d,"pageURL":"https://%s/photos/p%d/","largeImageURL":"https://%s/Img%d.jpg","user":"Author %d"}`, id, host, id, host, id, id)
	}
	return out
}

func TestImageServesFromTheBatch(t *testing.T) {
	w, s, _ := newImageWorld(t, 7)
	ctx := context.Background()
	if _, err := s.Pixabay(ctx, "key", "", "zh"); err != nil {
		t.Fatal(err)
	}
	img, err := s.Image(ctx, "pixabay", "7")
	if err != nil {
		t.Fatal(err)
	}
	if img.ContentType != "image/png" || !bytes.Equal(img.Data, pngBytes(t)) {
		t.Fatalf("image = %s (%d bytes), want the PNG as downloaded", img.ContentType, len(img.Data))
	}
	sum := sha256.Sum256(img.Data)
	if img.ETag != hex.EncodeToString(sum[:]) {
		t.Fatalf("etag = %s, want the sha256", img.ETag)
	}
	if w.imageHits.Load() != 1 {
		t.Fatalf("image hits = %d, want one download", w.imageHits.Load())
	}
	// The second call is served from the byte cache.
	if _, err := s.Image(ctx, "pixabay", "7"); err != nil || w.imageHits.Load() != 1 {
		t.Fatalf("second call: hits = %d, err %v; want the cached bytes", w.imageHits.Load(), err)
	}
}

func TestImageRejectsUnknownIdsAndSources(t *testing.T) {
	w, s, _ := newImageWorld(t, 7)
	ctx := context.Background()
	if _, err := s.Pixabay(ctx, "key", "", "zh"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ source, id string }{
		{"pixabay", "999"}, {"pixabay", "abc"}, {"pixabay", ""}, {"pixabay", "123456789012345678901"},
		{"openverse", "7"}, {"", "7"},
	} {
		if _, err := s.Image(ctx, tc.source, tc.id); !errors.Is(err, ErrImageNotFound) {
			t.Fatalf("Image(%q, %q) err = %v, want ErrImageNotFound", tc.source, tc.id, err)
		}
	}
	if w.imageHits.Load() != 0 {
		t.Fatalf("nothing should have been downloaded, hits = %d", w.imageHits.Load())
	}
}

// TestImageIdPatternGatesLookup proves the id's shape is checked before the
// batch is consulted at all: a malformed id must never reach lookup, even
// when a cached batch holds something at that key.
func TestImageIdPatternGatesLookup(t *testing.T) {
	w, s, c := newImageWorld(t, 7)
	ctx := context.Background()
	if _, err := s.Pixabay(ctx, "key", "", "zh"); err != nil {
		t.Fatal(err)
	}
	// Poison the cache with a hand-made entry whose picture ids are not
	// digits; without the pattern gate, lookup would find "notanumber" and
	// the download would start (and hit the server).
	s.mu.Lock()
	s.pixabay["pixabay:key::zh"] = &entry[[]cachedPicture]{
		ok:         true,
		freshUntil: c.t.Add(pixabayFor),
		value: []cachedPicture{{
			Picture:  Picture{ImageURL: "/branding/wallpaper/images/pixabay/notanumber"},
			id:       "notanumber",
			upstream: "https://" + urlMustHost(w.URL) + "/ImgX.jpg",
		}},
	}
	s.mu.Unlock()
	if _, err := s.Image(ctx, "pixabay", "notanumber"); !errors.Is(err, ErrImageNotFound) {
		t.Fatalf("err = %v, want ErrImageNotFound from the pattern gate", err)
	}
	if w.imageHits.Load() != 0 {
		t.Fatalf("a malformed id reached the download path, hits = %d", w.imageHits.Load())
	}
}

func TestImageRefusesForeignHosts(t *testing.T) {
	// The batch's picture address points at a host outside the allow-list,
	// so the hit is dropped at search time and nothing is fetched.
	up := newUpstream(t, respond(http.StatusOK, `{"hits":[{"id":5,"pageURL":"https://pixabay.com/photos/p5/","largeImageURL":"https://foreign.test/x.jpg","user":"A"}]}`))
	c := &clock{t: time.Unix(1_700_000_000, 0)}
	s := newPixabayService(c, up.URL, "pixabay.com", nil)
	ctx := context.Background()
	if _, err := s.Pixabay(ctx, "key", "", "zh"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("search err = %v, want ErrUnavailable with every hit dropped", err)
	}
	if up.hits.Load() != 1 {
		t.Fatalf("only the search may be fetched, hits = %d", up.hits.Load())
	}
}

func TestImageChecksEveryRedirectHop(t *testing.T) {
	foreign := newTLSUpstreamOnHost(t, "127.0.0.2", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	}))
	var handler atomic.Value
	handler.Store(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			wr2(w, r)
			return
		}
		// The picture "redirects" one hop at a time; the second hop leaves
		// the allow-list for a different https host, so the scheme check
		// passes and only the host check can refuse it.
		if r.URL.Query().Get("hop") == "" {
			http.Redirect(w, r, "/Img5.jpg?hop=1", http.StatusFound)
			return
		}
		http.Redirect(w, r, foreign.URL+"/evil.jpg", http.StatusFound)
	}))
	up := newTLSUpstream(t, handler.Load().(http.HandlerFunc))
	c := &clock{t: time.Unix(1_700_000_000, 0)}
	s := newPixabayService(c, up.URL, hostOf(up.URL), up.Client())
	ctx := context.Background()
	if _, err := s.Pixabay(ctx, "key", "", "zh"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Image(ctx, "pixabay", "5"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable for a redirect off the allow-list", err)
	}
	if foreign.hits.Load() != 0 {
		t.Fatal("a redirect off the allow-list was followed")
	}
}

// wr2 answers the search with one hit whose addresses are the server itself.
func wr2(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"hits":[{"id":5,"pageURL":"https://%s/photos/p5/","largeImageURL":"https://%s/Img5.jpg","user":"A"}]}`, r.Host, r.Host)
}

// TestImageRejectsOversizedAndNonPictureBodies exercises both refusals with a
// body that passes the earlier checks: the oversized one is a real PNG header
// followed by padding, so only the size cap rejects it, and the test counts
// the bytes the server actually read to prove the cap bounded the read.
func TestImageRejectsOversizedAndNonPictureBodies(t *testing.T) {
	var mode atomic.Value
	mode.Store("huge")
	var host atomic.Value
	var readBytes atomic.Int64
	var handler atomic.Value
	handler.Store(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"hits":[{"id":5,"pageURL":"https://%s/photos/p5/","largeImageURL":"https://%s/Img5.jpg","user":"A"},{"id":6,"pageURL":"https://%s/photos/p6/","largeImageURL":"https://%s/Img6.jpg","user":"B"}]}`, host.Load(), host.Load(), host.Load(), host.Load())
			return
		}
		switch mode.Load() {
		case "huge":
			// A valid PNG signature with padding: sniffing reads image/png,
			// so the size cap is the only check that can refuse it.
			w.Header().Set("Content-Type", "image/png")
			w.Write(append(pngBytes(t), bytes.Repeat([]byte{0x00}, 64<<10)...))
		default:
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, "<html>")
		}
	}))
	up := newTLSUpstream(t, handler.Load().(http.HandlerFunc))
	host.Store(urlMustHost(up.URL))
	c := &clock{t: time.Unix(1_700_000_000, 0)}
	// Wrap the transport so the test sees exactly how many body bytes the
	// service pulled from the wire.
	trustworthy := up.Client()
	counting := *trustworthy
	counting.Transport = readCountingTransport{base: trustworthy.Transport, read: &readBytes}
	s := newPixabayService(c, up.URL, hostOf(up.URL), &counting)
	// A small cap keeps the count deterministic: the PNG header (a few
	// hundred bytes) plus one buffered read at most.
	const cap = 512
	s.imageMaxBytes = cap
	ctx := context.Background()
	if _, err := s.Pixabay(ctx, "key", "", "zh"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Image(ctx, "pixabay", "5"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("oversized: err = %v, want ErrUnavailable", err)
	}
	// The service must stop reading at the cap, not pull the whole body in
	// and then measure: LimitReader(maxBytes+1) is what bounds this. One
	// bufio buffer (4 KiB) of read-ahead can arrive before the reader stops.
	if read := readBytes.Load(); read > cap+1+4096 {
		t.Fatalf("service read %d bytes from upstream, want at most the cap+1 (%d) plus one buffered read", read, cap+1)
	}
	// A body that is not a picture at all is refused by sniffing.
	mode.Store("text")
	if _, err := s.Image(ctx, "pixabay", "6"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("non-picture: err = %v, want ErrUnavailable", err)
	}
}

// readCountingTransport wraps a transport and counts the body bytes its
// responses hand to the caller.
type readCountingTransport struct {
	base http.RoundTripper
	read *atomic.Int64
}

func (t readCountingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	resp.Body = &countingReadCloser{r: resp.Body, read: t.read}
	return resp, nil
}

type countingReadCloser struct {
	r    io.ReadCloser
	read *atomic.Int64
}

func (c *countingReadCloser) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.read.Add(int64(n))
	return n, err
}

func (c *countingReadCloser) Close() error { return c.r.Close() }

func TestImageConcurrentRequestsDownloadOnce(t *testing.T) {
	var started atomic.Int32
	release := make(chan struct{})
	var handler atomic.Value
	handler.Store(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			wr2(w, r)
			return
		}
		started.Add(1)
		<-release
		w.Header().Set("Content-Type", "image/png")
		w.Write(pngBytes(t))
	}))
	up := newTLSUpstream(t, handler.Load().(http.HandlerFunc))
	c := &clock{t: time.Unix(1_700_000_000, 0)}
	s := newPixabayService(c, up.URL, hostOf(up.URL), up.Client())
	ctx := context.Background()
	if _, err := s.Pixabay(ctx, "key", "", "zh"); err != nil {
		t.Fatal(err)
	}
	const callers = 8
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.Image(ctx, "pixabay", "5"); err != nil {
				t.Errorf("Image: %v", err)
			}
		}()
	}
	// Wait until the one download has started, then let everyone through.
	for started.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	close(release)
	wg.Wait()
	if started.Load() != 1 {
		t.Fatalf("downloads = %d, want a single flight", started.Load())
	}
}

func TestImageLRUEvictsTheOldest(t *testing.T) {
	// Two pictures big enough that both cannot fit the byte cache (each is
	// 3/4 of its cap) but well under the download cap.
	const per = 24 << 20
	pic := append(pngBytes(t), bytes.Repeat([]byte{0x00}, per)...)
	if ct := http.DetectContentType(pic); ct != "image/png" {
		t.Fatalf("placeholder reads as %s", ct)
	}
	var host atomic.Value
	var handler atomic.Value
	handler.Store(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"hits":[{"id":1,"pageURL":"https://%s/photos/p1/","largeImageURL":"https://%s/Img1.jpg","user":"A"},{"id":2,"pageURL":"https://%s/photos/p2/","largeImageURL":"https://%s/Img2.jpg","user":"B"}]}`, host.Load(), host.Load(), host.Load(), host.Load())
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Write(pic)
	}))
	up := newTLSUpstream(t, handler.Load().(http.HandlerFunc))
	host.Store(urlMustHost(up.URL))
	c := &clock{t: time.Unix(1_700_000_000, 0)}
	s := newPixabayService(c, up.URL, hostOf(up.URL), up.Client())
	s.imageMaxBytes = per + 1<<20
	s.images.max = 2*per - per/2
	ctx := context.Background()
	if _, err := s.Pixabay(ctx, "key", "", "zh"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Image(ctx, "pixabay", "1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Image(ctx, "pixabay", "2"); err != nil {
		t.Fatal(err)
	}
	// Picture 1 was evicted; serving it again downloads it once more.
	hits := up.hits.Load()
	if _, err := s.Image(ctx, "pixabay", "1"); err != nil {
		t.Fatal(err)
	}
	if up.hits.Load() != hits+1 {
		t.Fatalf("after eviction: hits %d → %d, want a re-download", hits, up.hits.Load())
	}
}

// urlMustHost is the host:port of a test server's URL.
func urlMustHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		panic(err)
	}
	return u.Host
}

// TestImageRemembersAFailedDownload proves a failed download is remembered per
// source and id for a short while: repeated requests answer the same failure
// without touching the upstream again, and the retry happens after the TTL.
func TestImageRemembersAFailedDownload(t *testing.T) {
	var mode atomic.Value
	mode.Store("fail")
	var host atomic.Value
	var handler atomic.Value
	handler.Store(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"hits":[{"id":5,"pageURL":"https://%s/photos/p5/","largeImageURL":"https://%s/Img5.jpg","user":"A"}]}`, host.Load(), host.Load())
			return
		}
		if mode.Load() == "fail" {
			// The upstream refuses the picture, as Pixabay does once an
			// address has expired.
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Write(pngBytes(t))
	}))
	up := newTLSUpstream(t, handler.Load().(http.HandlerFunc))
	host.Store(urlMustHost(up.URL))
	c := &clock{t: time.Unix(1_700_000_000, 0)}
	s := newPixabayService(c, up.URL, hostOf(up.URL), up.Client())
	ctx := context.Background()
	if _, err := s.Pixabay(ctx, "key", "", "zh"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Image(ctx, "pixabay", "5"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("first: err = %v, want ErrUnavailable", err)
	}
	hits := up.hits.Load() // search + one failed picture download
	// Many visitors ask for the same dead picture: only the remembered
	// failure answers, and the upstream sees none of them.
	for range 50 {
		if _, err := s.Image(ctx, "pixabay", "5"); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("retry within TTL: err = %v, want ErrUnavailable", err)
		}
	}
	if got := up.hits.Load(); got != hits {
		t.Fatalf("upstream hits %d → %d within the TTL; the failure must be remembered", hits, got)
	}
	// After the TTL the upstream is asked again, and now it serves the bytes.
	mode.Store("serve")
	c.advance(retryAfter + time.Second)
	img, err := s.Image(ctx, "pixabay", "5")
	if err != nil {
		t.Fatalf("after TTL: %v", err)
	}
	if img.ContentType != "image/png" {
		t.Fatalf("after TTL contentType = %s", img.ContentType)
	}
	// And the bytes are cached from then on.
	hits = up.hits.Load()
	if _, err := s.Image(ctx, "pixabay", "5"); err != nil || up.hits.Load() != hits {
		t.Fatalf("after success: err %v, hits %d → %d; the bytes must be cached", err, hits, up.hits.Load())
	}
}

// TestImageIgnoresExpiredBatches proves an address is not served from a batch
// whose freshness has passed: Pixabay's addresses die with the batch, so an
// expired batch's id reads as not found rather than a doomed download.
func TestImageIgnoresExpiredBatches(t *testing.T) {
	w, s, c := newImageWorld(t, 5)
	ctx := context.Background()
	if _, err := s.Pixabay(ctx, "key", "", "zh"); err != nil {
		t.Fatal(err)
	}
	// The batch is now past its 24-hour freshness; the search cache may
	// still hold it (stale-if-error), but its addresses are dead.
	c.advance(pixabayFor + time.Minute)
	if _, err := s.Image(ctx, "pixabay", "5"); !errors.Is(err, ErrImageNotFound) {
		t.Fatalf("err = %v, want ErrImageNotFound from an expired batch", err)
	}
	if w.imageHits.Load() != 0 {
		t.Fatalf("a dead address was downloaded, hits = %d", w.imageHits.Load())
	}
}
