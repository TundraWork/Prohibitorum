package wallpaper

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// newOpenverseService points the search and the image allow-list at the test
// server, so Openverse's whole flow runs against it. client lets a TLS test
// server be trusted.
func newOpenverseService(c *clock, api, imgHost string, client *http.Client) *Service {
	return New(Options{Now: c.now, OpenverseURL: api, ImageHosts: []string{imgHost}, Client: client, ImageClient: client})
}

// ovResult is one Openverse search result. Only the fields the mapping reads
// are filled; every address is on the test server's host so the allow-list
// passes.
func ovResult(id, imgBase string, mutate func(map[string]any)) string {
	r := map[string]any{
		"id":                  id,
		"title":               "Lake " + id,
		"url":                 imgBase + "/" + id + ".jpg",
		"foreign_landing_url": "https://www.flickr.com/photos/ok/" + id + "/",
		"creator":             "Author " + id,
		"creator_url":         "https://www.flickr.com/people/ok/" + id + "/",
		"license":             "by-sa",
		"license_version":     "4.0",
		"license_url":         "https://creativecommons.org/licenses/by-sa/4.0/",
		"width":               1600,
		"height":              900,
	}
	if mutate != nil {
		mutate(r)
	}
	parts := make([]string, 0, len(r))
	for k, v := range r {
		switch v := v.(type) {
		case string:
			parts = append(parts, fmt.Sprintf("%q:%q", k, v))
		case int:
			parts = append(parts, fmt.Sprintf("%q:%d", k, v))
		default:
			panic(fmt.Sprintf("unsupported value %T", v))
		}
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func ovBody(imgBase string, results ...string) string {
	return `{"results":[` + strings.Join(results, ",") + `]}`
}

// flickrID and wikiID are stable ids the tests reuse.
const (
	flickrID = "0f0e8d7c-6b5a-4321-9def-0123456789ab"
	wikiID   = "97341896-5bde-42c8-a094-1aa0e6d4e59f"
)

func TestOpenverseRequestAndMapping(t *testing.T) {
	up := newUpstream(t, respond(http.StatusOK, ovBody("https://images.test", ovResult(flickrID, "https://images.test", nil))))
	c := &clock{t: time.Unix(1_700_000_000, 0)}
	s := newOpenverseService(c, up.URL, "images.test", nil)
	got, err := s.Openverse(context.Background(), "lake")
	if err != nil {
		t.Fatal(err)
	}
	r := up.request()
	q := r.URL.Query()
	if r.URL.Path != "/v1/images/" || q.Get("q") != "lake" || q.Get("page_size") != "20" ||
		q.Get("license") != "by,by-sa,cc0,pdm" || q.Get("source") != "flickr,wikimedia" ||
		q.Get("aspect_ratio") != "wide" || q.Get("mature") != "false" {
		t.Fatalf("request = %s", r.URL)
	}
	// Only the first page is ever asked for: paging would spend the anonymous
	// daily quota, and the terms forbid scraping around the quota.
	if q.Has("page") {
		t.Fatalf("request = %s: only the first page may be asked for", r.URL)
	}
	if h := r.Header.Get("Authorization"); h != "" {
		t.Fatalf("Authorization = %q, want none: the API is anonymous", h)
	}
	want := []Picture{{
		ImageURL:        "/branding/wallpaper/images/openverse/" + flickrID,
		Title:           "Lake " + flickrID,
		Photographer:    "Author " + flickrID,
		PhotographerURL: "https://www.flickr.com/people/ok/" + flickrID + "/",
		PhotoURL:        "https://www.flickr.com/photos/ok/" + flickrID + "/",
		License:         "CC BY-SA 4.0",
		LicenseURL:      "https://creativecommons.org/licenses/by-sa/4.0/",
	}}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("Openverse() = %+v, want %+v", got, want)
	}
}

func TestOpenverseLicenseNames(t *testing.T) {
	cases := map[string]struct {
		license string
		version string
		want    string
	}{
		"by":        {"by", "3.0", "CC BY 3.0"},
		"by-sa":     {"by-sa", "2.0", "CC BY-SA 2.0"},
		"cc0":       {"cc0", "1.0", "CC0 1.0"},
		"pdm":       {"pdm", "", "Public Domain Mark"},
		"by-novver": {"by", "", "CC BY"},
	}
	for name, tc := range cases {
		if got := openverseLicense(tc.license, tc.version); got != tc.want {
			t.Fatalf("%s: openverseLicense(%q, %q) = %q, want %q", name, tc.license, tc.version, got, tc.want)
		}
	}
	if openverseLicense("by-nc", "4.0") != "" {
		t.Fatal("openverseLicense(by-nc) is not empty")
	}
}

// TestOpenverseImageSourceDefaults proves the openverse source registers with
// exactly the production allow-list, the shared 10 MiB cap and the lowercase
// UUID id pattern, without Options overriding any of them.
func TestOpenverseImageSourceDefaults(t *testing.T) {
	s := New(Options{})
	src := s.imageSource("openverse")
	if src == nil {
		t.Fatal("openverse source not registered")
	}
	if len(src.hosts) != 2 || src.hosts[0] != openverseHostFlickr || src.hosts[1] != openverseHostWikimedia {
		t.Fatalf("hosts = %v, want exactly [%s %s]", src.hosts, openverseHostFlickr, openverseHostWikimedia)
	}
	if src.maxBytes != 10<<20 {
		t.Fatalf("maxBytes = %d, want 10 MiB", src.maxBytes)
	}
	for _, id := range []string{flickrID, wikiID} {
		if !src.idPattern(id) {
			t.Fatalf("idPattern(%q) = false, want a lowercase UUID accepted", id)
		}
	}
	for _, id := range []string{
		"0F0E8D7C-6B5A-4321-9DEF-0123456789AB", // uppercase
		"not-a-uuid",
		"0f0e8d7c-6b5a-4321-9def-0123456789a",   // short
		"0f0e8d7c-6b5a-4321-9def-0123456789ab/", // a path glued on
	} {
		if src.idPattern(id) {
			t.Fatalf("idPattern(%q) = true, want refused", id)
		}
	}
	// The allow-list is what downloadImage checks addresses against: an
	// address on any other host is refused even though it is https.
	for _, u := range []string{
		"https://" + openverseHostFlickr + "/x.jpg",
		"https://" + openverseHostWikimedia + "/wikipedia/commons/a/ab/X.jpg",
	} {
		if err := checkImageAddress(u, src.hosts); err != nil {
			t.Fatalf("checkImageAddress(%q) = %v, want the allow-list to accept it", u, err)
		}
	}
	for _, u := range []string{
		"https://wikimedia.org/wikipedia/commons/a/ab/X.jpg", // a shorter host
		"https://staticflickr.com/x.jpg",                     // a shorter host
		"https://evil.test/x.jpg",
		"http://" + openverseHostFlickr + "/x.jpg", // not https
	} {
		if err := checkImageAddress(u, src.hosts); err == nil {
			t.Fatalf("checkImageAddress(%q) accepted an address off the allow-list", u)
		}
	}
}

func TestOpenverseFilters(t *testing.T) {
	base := "https://images.test"
	// Every case keeps a valid UUID id, so only the condition under test can
	// drop the result; the plain control proves a result passes when nothing
	// is wrong with it.
	cases := map[string]func(map[string]any){
		"id not a uuid":      func(r map[string]any) { r["id"] = "not-a-uuid" },
		"uppercase id":       func(r map[string]any) { r["id"] = "0F0E8D7C-6B5A-4321-9DEF-0123456789AB" },
		"license off-list":   func(r map[string]any) { r["license"] = "by-nc" },
		"license url prefix": func(r map[string]any) { r["license_url"] = "https://example.org/licenses/by/4.0/" },
		"image host":         func(r map[string]any) { r["url"] = "https://foreign.test/x.jpg" },
		"image not https":    func(r map[string]any) { r["url"] = "http://images.test/x.jpg" },
		"image extension":    func(r map[string]any) { r["url"] = base + "/x.gif" },
		"landing page":       func(r map[string]any) { r["foreign_landing_url"] = "https://example.org/x" },
		"too narrow":         func(r map[string]any) { r["width"], r["height"] = 800, 600 },
		"portrait":           func(r map[string]any) { r["width"], r["height"] = 1600, 2000 },
	}
	for name, changes := range cases {
		up := newUpstream(t, respond(http.StatusOK, ovBody(base, ovResult(flickrID, base, changes))))
		c := &clock{t: time.Unix(1_700_000_000, 0)}
		s := newOpenverseService(c, up.URL, "images.test", nil)
		if _, err := s.Openverse(context.Background(), "q"); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("%s: err = %v, want ErrUnavailable for a batch with nothing usable", name, err)
		}
	}
	// The control: the same result untouched is kept, so the cases above are
	// about their own condition and not about the id or the harness.
	up := newUpstream(t, respond(http.StatusOK, ovBody(base, ovResult(flickrID, base, nil))))
	c := &clock{t: time.Unix(1_700_000_000, 0)}
	s := newOpenverseService(c, up.URL, "images.test", nil)
	got, err := s.Openverse(context.Background(), "q")
	if err != nil || len(got) != 1 || got[0].ImageURL != "/branding/wallpaper/images/openverse/"+flickrID {
		t.Fatalf("control: got %v, err %v; want the untouched result kept", got, err)
	}
}

func TestOpenverseKeepsPictureWhenCreatorLinkIsOffList(t *testing.T) {
	base := "https://images.test"
	up := newUpstream(t, respond(http.StatusOK, ovBody(base,
		ovResult(flickrID, base, func(r map[string]any) { r["creator_url"] = "https://en.wikipedia.org/wiki/user:X" }))))
	c := &clock{t: time.Unix(1_700_000_000, 0)}
	s := newOpenverseService(c, up.URL, "images.test", nil)
	got, err := s.Openverse(context.Background(), "q")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("pictures = %d, want the picture kept", len(got))
	}
	if got[0].PhotographerURL != "" {
		t.Fatalf("photographerUrl = %q, want empty: an off-list creator link is dropped, not the picture", got[0].PhotographerURL)
	}
	if got[0].Photographer == "" {
		t.Fatal("photographer dropped with the link")
	}
}

func TestOpenverseTruncatesLongCredits(t *testing.T) {
	base := "https://images.test"
	long := strings.Repeat("山", 250)
	up := newUpstream(t, respond(http.StatusOK, ovBody(base,
		ovResult(flickrID, base, func(r map[string]any) { r["title"], r["creator"] = long, long }))))
	c := &clock{t: time.Unix(1_700_000_000, 0)}
	s := newOpenverseService(c, up.URL, "images.test", nil)
	got, err := s.Openverse(context.Background(), "q")
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Title != strings.Repeat("山", 200) || got[0].Photographer != strings.Repeat("山", 200) {
		t.Fatalf("title %d chars, photographer %d chars; want both cut at 200", len(got[0].Title), len(got[0].Photographer))
	}
}

func TestOpenverseWikimediaThumbRewrite(t *testing.T) {
	original := "https://upload.wikimedia.org/wikipedia/commons/9/91/Peyto_Lake-Banff_NP-Canada.jpg"
	thumb := "https://upload.wikimedia.org/wikipedia/commons/thumb/9/91/Peyto_Lake-Banff_NP-Canada.jpg/1920px-Peyto_Lake-Banff_NP-Canada.jpg"

	// A wide original larger than 1920px is downloaded as the 1920px thumb.
	up := newUpstream(t, respond(http.StatusOK, ovBody("https://upload.wikimedia.org",
		ovResult(wikiID, "https://upload.wikimedia.org", func(r map[string]any) {
			r["url"] = original
			r["width"], r["height"] = 4150, 2467
			r["foreign_landing_url"] = "https://commons.wikimedia.org/w/index.php?curid=3064142"
			r["creator_url"] = "https://commons.wikimedia.org/wiki/User:Tobi_87"
		}))))
	c := &clock{t: time.Unix(1_700_000_000, 0)}
	s := newOpenverseService(c, up.URL, "upload.wikimedia.org", nil)
	if _, err := s.Openverse(context.Background(), "lake"); err != nil {
		t.Fatal(err)
	}
	if got, ok := s.lookupOpenverse(wikiID); !ok || got != thumb {
		t.Fatalf("lookup = %q (ok %v), want the rewritten thumb %q", got, ok, thumb)
	}

	// Width at or below 1920 keeps the original, as does an address that is
	// not the original-file path (a thumb or a scaled name already).
	for name, tc := range map[string]struct {
		url   string
		width int
	}{
		"at the thumb width": {original, 1920},
		"already a thumb":    {"https://upload.wikimedia.org/wikipedia/commons/thumb/9/91/Peyto.jpg/1280px-Peyto.jpg", 4150},
		"has a query":        {original + "?uselang=zh", 4150},
	} {
		up := newUpstream(t, respond(http.StatusOK, ovBody("https://upload.wikimedia.org",
			ovResult(wikiID, "https://upload.wikimedia.org", func(r map[string]any) {
				r["url"] = tc.url
				r["width"], r["height"] = tc.width, 1080
				r["foreign_landing_url"] = "https://commons.wikimedia.org/w/index.php?curid=1"
			}))))
		s := newOpenverseService(c, up.URL, "upload.wikimedia.org", nil)
		if _, err := s.Openverse(context.Background(), "lake"); err != nil {
			t.Fatal(err)
		}
		if got, ok := s.lookupOpenverse(wikiID); !ok || got != tc.url {
			t.Fatalf("%s: lookup = %q (ok %v), want the address unchanged %q", name, got, ok, tc.url)
		}
	}
}

func TestOpenverseCachesForAnHour(t *testing.T) {
	up := newUpstream(t, respond(http.StatusOK, ovBody("https://images.test", ovResult(flickrID, "https://images.test", nil))))
	c := &clock{t: time.Unix(1_700_000_000, 0)}
	s := newOpenverseService(c, up.URL, "images.test", nil)
	ctx := context.Background()
	if _, err := s.Openverse(ctx, "lake"); err != nil {
		t.Fatal(err)
	}
	c.advance(30 * time.Minute)
	if _, err := s.Openverse(ctx, "lake"); err != nil || up.hits.Load() != 1 {
		t.Fatalf("within the hour: hits = %d, err %v; want the cached batch", up.hits.Load(), err)
	}
	c.advance(31 * time.Minute)
	if _, err := s.Openverse(ctx, "lake"); err != nil || up.hits.Load() != 2 {
		t.Fatalf("past the hour: hits = %d, err %v; want a fresh search", up.hits.Load(), err)
	}
}

func TestOpenverseServesStaleOnError(t *testing.T) {
	up := newUpstream(t, respond(http.StatusOK, ovBody("https://images.test", ovResult(flickrID, "https://images.test", nil))))
	c := &clock{t: time.Unix(1_700_000_000, 0)}
	s := newOpenverseService(c, up.URL, "images.test", nil)
	ctx := context.Background()
	first, err := s.Openverse(ctx, "lake")
	if err != nil {
		t.Fatal(err)
	}
	// A failed refresh serves the stale batch, and a 429 (the daily quota
	// spent) is no exception: the batch keeps serving until it returns.
	for _, status := range []int{http.StatusInternalServerError, http.StatusTooManyRequests} {
		up.set(respond(status, `{"detail":"no"}`))
		c.advance(time.Hour + time.Minute)
		got, err := s.Openverse(ctx, "lake")
		if err != nil || len(got) != len(first) || got[0] != first[0] {
			t.Fatalf("status %d: got %v, err %v; want the stale batch", status, got, err)
		}
	}
	// Each failed refresh searched once; the minute after a failure answers
	// from the remembered error without another search.
	if up.hits.Load() != 3 {
		t.Fatalf("hits = %d, want one search per failed refresh plus the first", up.hits.Load())
	}
}

func TestOpenverseUnavailableWithoutCache(t *testing.T) {
	up := newUpstream(t, respond(http.StatusBadGateway, `{"detail":"down"}`))
	c := &clock{t: time.Unix(1_700_000_000, 0)}
	s := newOpenverseService(c, up.URL, "images.test", nil)
	if _, err := s.Openverse(context.Background(), "lake"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
	if strings.Contains(errText(t, s, "lake"), "openverseURL") {
		t.Fatal("error text names the request URL")
	}
}

func errText(t *testing.T, s *Service, query string) string {
	t.Helper()
	_, err := s.Openverse(context.Background(), query)
	if err == nil {
		t.Fatal("no error")
	}
	return err.Error()
}

func TestOpenverseForgetKeepsTheCache(t *testing.T) {
	up := newUpstream(t, respond(http.StatusOK, ovBody("https://images.test", ovResult(flickrID, "https://images.test", nil))))
	c := &clock{t: time.Unix(1_700_000_000, 0)}
	s := newOpenverseService(c, up.URL, "images.test", nil)
	ctx := context.Background()
	if _, err := s.Openverse(ctx, "lake"); err != nil {
		t.Fatal(err)
	}
	s.Forget()
	if _, err := s.Openverse(ctx, "lake"); err != nil || up.hits.Load() != 1 {
		t.Fatalf("after Forget: hits = %d, err %v; want the cached batch kept", up.hits.Load(), err)
	}
	// A different keyword is its own cache entry.
	if _, err := s.Openverse(ctx, "mountain"); err != nil || up.hits.Load() != 2 {
		t.Fatalf("other keyword: hits = %d, err %v; want a separate search", up.hits.Load(), err)
	}
}

/* --------------------------------------------------------------- images -- */

// ovWorld is an Openverse API and its picture host on one TLS test server
// (the download path is https-only), with every address on the server's own
// host so it stays on the allow-list.
type ovWorld struct {
	*upstream
	imageHits atomic.Int32
}

func newOvWorld(t *testing.T, ids ...string) (*ovWorld, *Service, *clock) {
	t.Helper()
	w := &ovWorld{}
	w.upstream = newTLSUpstream(t, http.HandlerFunc(func(wr http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/images/" {
			wr.Header().Set("Content-Type", "application/json")
			results := make([]string, len(ids))
			for i, id := range ids {
				results[i] = ovResult(id, "https://"+r.Host, nil)
			}
			fmt.Fprintf(wr, `{"results":[%s]}`, strings.Join(results, ","))
			return
		}
		w.imageHits.Add(1)
		wr.Header().Set("Content-Type", "image/png")
		wr.Write(pngBytes(t))
	}))
	c := &clock{t: time.Unix(1_700_000_000, 0)}
	return w, newOpenverseService(c, w.URL, hostOf(w.URL), w.Client()), c
}

func TestOpenverseImageServesFromTheBatch(t *testing.T) {
	w, s, _ := newOvWorld(t, flickrID)
	ctx := context.Background()
	if _, err := s.Openverse(ctx, "lake"); err != nil {
		t.Fatal(err)
	}
	img, err := s.Image(ctx, "openverse", flickrID)
	if err != nil {
		t.Fatal(err)
	}
	if img.ContentType != "image/png" || !bytes.Equal(img.Data, pngBytes(t)) {
		t.Fatalf("image = %s (%d bytes), want the PNG as downloaded", img.ContentType, len(img.Data))
	}
	// The second call is served from the byte cache: one download only.
	if _, err := s.Image(ctx, "openverse", flickrID); err != nil || w.imageHits.Load() != 1 {
		t.Fatalf("second call: hits = %d, err %v; want the cached bytes", w.imageHits.Load(), err)
	}
}

func TestOpenverseImageRejectsUnknownAndMalformedIds(t *testing.T) {
	w, s, _ := newOvWorld(t, flickrID)
	ctx := context.Background()
	if _, err := s.Openverse(ctx, "lake"); err != nil {
		t.Fatal(err)
	}
	// Not in the cached batch.
	other := "11111111-2222-3333-4444-555555555555"
	if _, err := s.Image(ctx, "openverse", other); !errors.Is(err, ErrImageNotFound) {
		t.Fatalf("unknown id: err = %v, want ErrImageNotFound", err)
	}
	// Not a lowercase UUID: refused before the batch is consulted.
	for _, id := range []string{"NOT-A-UUID", "0f0e8d7c6b5a43219def0123456789ab", "0f0e8d7c-6b5a-4321-9def-0123456789ab/evil"} {
		if _, err := s.Image(ctx, "openverse", id); !errors.Is(err, ErrImageNotFound) {
			t.Fatalf("id %q: err = %v, want ErrImageNotFound", id, err)
		}
	}
	// An unknown source is not served either.
	if _, err := s.Image(ctx, "openversе", flickrID); !errors.Is(err, ErrImageNotFound) {
		t.Fatalf("unknown source: err = %v, want ErrImageNotFound", err)
	}
	if w.imageHits.Load() != 0 {
		t.Fatalf("image hits = %d, want no downloads", w.imageHits.Load())
	}
}

func TestOpenverseImageSkipsForeignAddresses(t *testing.T) {
	// The search is answered with a picture on an off-list host; the batch
	// holds nothing usable, so Openverse answers ErrUnavailable and no image
	// request can happen at all.
	up := newUpstream(t, respond(http.StatusOK, ovBody("https://images.test",
		ovResult(flickrID, "https://foreign.test", nil))))
	c := &clock{t: time.Unix(1_700_000_000, 0)}
	s := newOpenverseService(c, up.URL, "images.test", nil)
	if _, err := s.Openverse(context.Background(), "lake"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable: the off-list host is filtered at search time", err)
	}
}

// wrOpenverse answers the search with one result whose addresses are the
// server itself, so downloads stay on the allow-list.
func wrOpenverse(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"results":[%s]}`, ovResult(flickrID, "https://"+r.Host, nil))
}

// TestOpenverseImageChecksEveryRedirectHop proves an Openverse download that
// leaves the allow-list mid-redirect is refused by the host check alone: one
// throwaway CA signs both the picture server and the off-list one, so a
// followed redirect would complete its TLS handshake and reach the off-list
// server.
func TestOpenverseImageChecksEveryRedirectHop(t *testing.T) {
	ca := newTestCA(t)
	var foreign *upstream
	up := newTLSUpstreamWithCA(t, "127.0.0.1", ca, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/images/" {
			wrOpenverse(w, r)
			return
		}
		if r.URL.Query().Get("hop") == "" {
			http.Redirect(w, r, "/"+flickrID+".jpg?hop=1", http.StatusFound)
			return
		}
		http.Redirect(w, r, foreign.URL+"/evil.jpg", http.StatusFound)
	}))
	foreign = newTLSUpstreamWithCA(t, "127.0.0.2", ca, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	}))
	c := &clock{t: time.Unix(1_700_000_000, 0)}
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: ca.pool}}}
	t.Cleanup(client.CloseIdleConnections)
	s := newOpenverseService(c, up.URL, hostOf(up.URL), client)
	ctx := context.Background()
	if _, err := s.Openverse(ctx, "lake"); err != nil {
		t.Fatal(err)
	}
	_, err := s.Image(ctx, "openverse", flickrID)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable for a redirect off the allow-list", err)
	}
	if !strings.Contains(err.Error(), "not on the allow-list") {
		t.Fatalf("err = %v, want the allow-list check to be what refused it", err)
	}
	if foreign.hits.Load() != 0 {
		t.Fatal("a redirect off the allow-list was followed")
	}
}
