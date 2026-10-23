// Package wallpaper fetches the sign-in page's wallpapers: Bing's picture of
// the day, a batch of random Unsplash photos, and a batch of Pixabay photos
// for a keyword. Bing and Unsplash allow the browser to load their pictures
// directly, so those answers are only addresses and credits. Pixabay's terms
// forbid permanent hotlinking and its picture addresses expire after a day, so
// the server downloads each picture itself and serves it from this site
// through Image — the browser never talks to Pixabay for a picture.
//
// Answers are cached (an hour for Bing and Unsplash, a day for Pixabay, as
// Pixabay's terms ask). When a refresh fails, the last answer keeps being
// served (stale-if-error), and the failure itself is remembered for a minute
// so a down upstream is not asked again on every page load.
package wallpaper

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

const (
	// BingOrigin serves both Bing's API and its pictures; CSP allows it.
	BingOrigin = "https://www.bing.com"
	// UnsplashAPI is Unsplash's JSON API. Its pictures come from
	// https://images.unsplash.com, which CSP allows.
	UnsplashAPI = "https://api.unsplash.com"
	// PixabayAPI is Pixabay's JSON API; its pictures are downloaded from the
	// same host, because the addresses it hands out elsewhere (the CDN) are
	// not the ones that last.
	PixabayAPI = "https://pixabay.com/api/"

	unsplashImageOrigin = "https://images.unsplash.com/"
	unsplashSiteOrigin  = "https://unsplash.com/"
	unsplashBatch       = 30
	referral            = "utm_source=prohibitorum&utm_medium=referral"

	pixabayBatch = 20

	// imageCacheBytes caps the bytes of downloaded pictures; the least
	// recently used picture is dropped when a new one would pass it.
	imageCacheBytes = 64 << 20
	// defaultImageMaxBytes caps one downloaded picture when a source does not
	// set its own limit.
	defaultImageMaxBytes = 10 << 20

	freshFor   = time.Hour
	pixabayFor = 24 * time.Hour
	retryAfter = time.Minute
)

var (
	// ErrUnavailable means the upstream could not be reached or answered with
	// something unusable, and there is no earlier answer to fall back on.
	ErrUnavailable = errors.New("wallpaper: upstream unavailable")
	// ErrKeyRejected means Unsplash refused the access key (401/403).
	ErrKeyRejected = errors.New("wallpaper: Unsplash rejected the access key")
	// ErrPixabayKeyRejected means Pixabay refused the API key (400/401/403).
	ErrPixabayKeyRejected = errors.New("wallpaper: Pixabay rejected the API key")
	// ErrImageNotFound means no source knows id: either the source does not
	// exist, or the id is not in the batch this build has cached.
	ErrImageNotFound = errors.New("wallpaper: picture not in a cached batch")
)

// Picture is one wallpaper and its credit. Bing fills Title, Copyright and
// CopyrightURL; Unsplash fills Title from the photo's description, when it
// has one, and the photographer and photo links; Pixabay fills the
// photographer and the photo page link, whose URL stays inside the service
// (the picture itself is served by Image).
type Picture struct {
	ImageURL        string
	Title           string
	Copyright       string
	CopyrightURL    string
	Photographer    string
	PhotographerURL string
	PhotoURL        string
}

// Image is a picture this site serves for a source: its bytes, as downloaded,
// the content type read from those bytes, and the ETag (the content's
// sha256).
type Image struct {
	Data        []byte
	ContentType string
	ETag        string
}

// imageSource is one picture service Image can download from. idPattern
// decides which ids the source answers; lookup finds id's upstream address in
// the source's current cached batch; hosts is the allow-list the address and
// every redirect hop must be on (https only); maxBytes caps one download.
type imageSource struct {
	idPattern func(id string) bool
	lookup    func(id string) (string, bool)
	hosts     []string
	maxBytes  int64
}

// Options configure a Service. Zero values use the real endpoints and clock.
type Options struct {
	Client      *http.Client
	ImageClient *http.Client
	BingURL     string
	UnsplashURL string
	PixabayURL  string
	Now         func() time.Time
	// ImageHosts overrides pixabay's download allow-list (tests point it at
	// their own server).
	ImageHosts []string
}

// Service fetches and caches wallpapers. It is safe for concurrent use.
type Service struct {
	client      *http.Client
	imageClient *http.Client
	bingURL     string
	unsplashURL string
	pixabayURL  string
	hosts       []string
	// imageMaxBytes overrides the per-source download cap when set (tests).
	imageMaxBytes int64
	now           func() time.Time

	flight   singleflight.Group
	mu       sync.Mutex
	bing     map[string]*entry[Picture]
	unsplash map[string]*entry[[]Picture]
	pixabay  map[string]*entry[[]cachedPicture]

	// images caches downloaded picture bytes, keyed by source and id.
	images *lruCache
	// imageFailures remembers, per source and id, when a failed download may
	// be retried; until then the failure is served as-is so an anonymous
	// visitor cannot make the server re-fetch a dead upstream address on
	// every request.
	imageFailures map[string]time.Time
}

type entry[T any] struct {
	value      T
	ok         bool
	freshUntil time.Time
	retryAt    time.Time
	err        error
}

// New builds a Service.
func New(o Options) *Service {
	s := &Service{
		client:        o.Client,
		imageClient:   o.ImageClient,
		bingURL:       o.BingURL,
		unsplashURL:   o.UnsplashURL,
		pixabayURL:    o.PixabayURL,
		hosts:         o.ImageHosts,
		now:           o.Now,
		bing:          map[string]*entry[Picture]{},
		unsplash:      map[string]*entry[[]Picture]{},
		pixabay:       map[string]*entry[[]cachedPicture]{},
		images:        newLRUCache(imageCacheBytes),
		imageFailures: map[string]time.Time{},
	}
	if s.client == nil {
		s.client = http.DefaultClient
	}
	if s.imageClient == nil {
		s.imageClient = s.client
	}
	if s.bingURL == "" {
		s.bingURL = BingOrigin
	}
	if s.unsplashURL == "" {
		s.unsplashURL = UnsplashAPI
	}
	if s.pixabayURL == "" {
		s.pixabayURL = PixabayAPI
	}
	if s.now == nil {
		s.now = time.Now
	}
	return s
}

// Bing returns today's picture for market.
func (s *Service) Bing(ctx context.Context, market string) (Picture, error) {
	return cached(s, s.bing, freshFor, "bing:"+market, func() (Picture, error) {
		return s.fetchBing(context.WithoutCancel(ctx), market)
	})
}

// Unsplash returns the cached batch of photos for key and query, in the order
// Unsplash sent them. Picking one at random or rotating through them is left
// to the browser.
func (s *Service) Unsplash(ctx context.Context, key, query string) ([]Picture, error) {
	sum := sha256.Sum256([]byte(key))
	batch, err := cached(s, s.unsplash, freshFor, "unsplash:"+hex.EncodeToString(sum[:])+":"+query, func() ([]Picture, error) {
		return s.fetchUnsplash(context.WithoutCancel(ctx), key, query, unsplashBatch)
	})
	if err != nil {
		return nil, err
	}
	return slices.Clone(batch), nil
}

// Verify asks Unsplash for one photo with key, to check the key before it is
// saved. It returns ErrKeyRejected or ErrUnavailable.
func (s *Service) Verify(ctx context.Context, key string) error {
	_, err := s.fetchUnsplash(ctx, key, "", 1)
	return err
}

// Pixabay returns the cached batch of pictures for key, query and lang, in
// the order Pixabay sent them. Each picture's ImageURL points at this site's
// /branding/wallpaper/images/pixabay/{id}; Pixabay's own picture address
// stays inside the service for Image to download. A batch is held for a day,
// as Pixabay's API terms ask, so each (key, lang, query) searches at most
// once a day.
func (s *Service) Pixabay(ctx context.Context, key, query, lang string) ([]Picture, error) {
	sum := sha256.Sum256([]byte(key))
	cacheKey := "pixabay:" + hex.EncodeToString(sum[:]) + ":" + lang + ":" + query
	batch, err := cached(s, s.pixabay, pixabayFor, cacheKey, func() ([]cachedPicture, error) {
		return s.fetchPixabay(context.WithoutCancel(ctx), key, query, lang)
	})
	if err != nil {
		return nil, err
	}
	out := make([]Picture, len(batch))
	for i, p := range batch {
		out[i] = p.Picture
	}
	return out, nil
}

// VerifyPixabay asks Pixabay for three pictures with key, to check the key
// before it is saved. The answer is not cached.
func (s *Service) VerifyPixabay(ctx context.Context, key string) error {
	_, err := s.fetchPixabay(ctx, key, "", "en")
	return err
}

// Forget drops every cached Unsplash batch, so a new key or keyword applies
// on the next request.
func (s *Service) Forget() {
	s.mu.Lock()
	clear(s.unsplash)
	s.mu.Unlock()
}

// Image serves the picture id from source's current batch: the bytes are
// downloaded once from the address the batch holds (on the source's
// allow-list, https only, every redirect hop re-checked), then cached and
// served byte-for-byte with the content type read from the bytes and an ETag
// of their sha256. PHB-153's Openverse source registers beside Pixabay.
func (s *Service) Image(ctx context.Context, source, id string) (Image, error) {
	src := s.imageSource(source)
	if src == nil || !src.idPattern(id) {
		return Image{}, ErrImageNotFound
	}
	key := source + "/" + id
	if img, ok := s.images.get(key); ok {
		return img, nil
	}
	// A download that failed recently is remembered: until retryAt the same
	// failure answers without touching the upstream again, so repeated
	// anonymous requests cannot make the server re-fetch a dead address.
	s.mu.Lock()
	retryAt, failed := s.imageFailures[key]
	s.mu.Unlock()
	if failed && s.now().Before(retryAt) {
		return Image{}, ErrUnavailable
	}
	v, err, _ := s.flight.Do("image:"+key, func() (any, error) {
		if img, ok := s.images.get(key); ok {
			return img, nil
		}
		upstream, ok := src.lookup(id)
		if !ok {
			return Image{}, ErrImageNotFound
		}
		img, err := s.downloadImage(ctx, source, upstream, src)
		if err != nil {
			s.mu.Lock()
			s.imageFailures[key] = s.now().Add(retryAfter)
			s.mu.Unlock()
			return Image{}, err
		}
		s.mu.Lock()
		delete(s.imageFailures, key)
		s.mu.Unlock()
		s.images.put(key, img)
		return img, nil
	})
	if err != nil {
		return Image{}, err
	}
	return v.(Image), nil
}

// imageSources lists the sources Image serves. Pixabay's ids are 1–20 digits
// and its pictures live on pixabay.com (measured: page and image addresses
// are all that host); a full-HD address on another host would be dropped at
// download time.
func (s *Service) imageSource(source string) *imageSource {
	hosts := s.imageHosts()
	var maxBytes int64 = defaultImageMaxBytes
	if s.imageMaxBytes > 0 {
		maxBytes = s.imageMaxBytes
	}
	switch source {
	case "pixabay":
		return &imageSource{
			idPattern: func(id string) bool { return len(id) >= 1 && len(id) <= 20 && allDigits(id) },
			lookup:    s.lookupPixabay,
			hosts:     hosts,
			maxBytes:  maxBytes,
		}
	default:
		return nil
	}
}

func allDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return len(s) > 0
}

func (s *Service) imageHosts() []string {
	if len(s.hosts) == 0 {
		return []string{"pixabay.com"}
	}
	return s.hosts
}

// lookupPixabay finds id's upstream address across the cached batches whose
// addresses are still valid: Pixabay's expire 24 hours after the search, so a
// batch past its freshness names an address that can no longer be downloaded.
// Among the valid ones the newest wins.
func (s *Service) lookupPixabay(id string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	type candidate struct {
		url        string
		freshUntil time.Time
	}
	var best *candidate
	now := s.now()
	for _, e := range s.pixabay {
		if !e.ok || now.After(e.freshUntil) {
			continue
		}
		for _, p := range e.value {
			if p.id == id && p.upstream != "" {
				if best == nil || e.freshUntil.After(best.freshUntil) {
					best = &candidate{url: p.upstream, freshUntil: e.freshUntil}
				}
			}
		}
	}
	if best == nil {
		return "", false
	}
	return best.url, true
}

// cachedPicture is what fetchPixabay stores: the public Picture plus the id
// and the upstream address Image downloads from.
type cachedPicture struct {
	Picture
	id       string
	upstream string
}

// downloadImage fetches one picture: the address must be https and on the
// source's allow-list, every redirect hop must stay on it, and the body may
// not pass the source's cap. Only jpeg, png and webp bytes are accepted.
func (s *Service) downloadImage(ctx context.Context, source, upstream string, src *imageSource) (Image, error) {
	if err := checkImageAddress(upstream, src.hosts); err != nil {
		return Image{}, fmt.Errorf("%w: %s: %v", ErrUnavailable, source, err)
	}
	// The base client keeps its own redirect policy (the shared outbound
	// client screens schemes and dials); the wallpaper policy is applied
	// first and lets the base one run by calling the saved hook.
	client := *s.imageClient
	base := s.imageClient.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("wallpaper: too many redirects")
		}
		if err := checkImageAddress(req.URL.String(), src.hosts); err != nil {
			return err
		}
		if base != nil {
			return base(req, via)
		}
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, upstream, nil)
	if err != nil {
		return Image{}, scrubURLError(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return Image{}, fmt.Errorf("%w: %s: %v", ErrUnavailable, source, scrubURLError(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Image{}, fmt.Errorf("%w: %s: status %d", ErrUnavailable, source, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, src.maxBytes+1))
	if err != nil {
		return Image{}, fmt.Errorf("%w: %s: %v", ErrUnavailable, source, scrubURLError(err))
	}
	if int64(len(data)) > src.maxBytes {
		return Image{}, fmt.Errorf("%w: %s: picture over %d bytes", ErrUnavailable, source, src.maxBytes)
	}
	contentType := http.DetectContentType(data)
	switch contentType {
	case "image/jpeg", "image/png", "image/webp":
	default:
		return Image{}, fmt.Errorf("%w: %s: %s", ErrUnavailable, source, contentType)
	}
	sum := sha256.Sum256(data)
	return Image{Data: data, ContentType: contentType, ETag: hex.EncodeToString(sum[:])}, nil
}

// checkImageAddress reports whether u is an https address on hosts.
func checkImageAddress(u string, hosts []string) error {
	parsed, err := url.Parse(u)
	if err != nil {
		return fmt.Errorf("invalid address: %w", err)
	}
	if parsed.Scheme != "https" {
		return fmt.Errorf("address is not https")
	}
	if !slices.Contains(hosts, parsed.Hostname()) {
		return fmt.Errorf("host %q is not on the allow-list", parsed.Hostname())
	}
	return nil
}

// cached answers from m[key] while it is fresh, refreshes it otherwise, and
// falls back to the stale value when the refresh fails. Fetches run detached
// from the caller's cancellation, so a visitor closing the page does not
// record a failure for everyone else. freshFor is how long a good answer
// holds; onStore, when set, runs after a successful write under the lock, so
// a source can prune its own cache.
func cached[T any](s *Service, m map[string]*entry[T], freshFor time.Duration, key string, fetch func() (T, error)) (T, error) {
	now := s.now()
	s.mu.Lock()
	e := m[key]
	if e != nil {
		switch {
		case e.ok && now.Before(e.freshUntil):
			s.mu.Unlock()
			return e.value, nil
		case now.Before(e.retryAt):
			value, ok, err := e.value, e.ok, e.err
			s.mu.Unlock()
			if ok {
				return value, nil
			}
			return value, err
		}
	}
	s.mu.Unlock()

	v, err, _ := s.flight.Do(key, func() (any, error) {
		value, err := fetch()
		s.mu.Lock()
		defer s.mu.Unlock()
		e := m[key]
		if e == nil {
			e = &entry[T]{}
			m[key] = e
		}
		if err == nil {
			*e = entry[T]{value: value, ok: true, freshUntil: now.Add(freshFor)}
			// Pixabay's batches hold their upstream addresses for a day;
			// once those have been stale for another day they can never be
			// downloaded again, so drop them instead of pinning memory.
			for k, e := range m {
				if e.ok && now.After(e.freshUntil.Add(pixabayFor)) {
					delete(m, k)
				}
			}
			return value, nil
		}
		e.retryAt, e.err = now.Add(retryAfter), err
		if e.ok {
			return e.value, nil
		}
		return value, err
	})
	value, _ := v.(T)
	return value, err
}

// get turns an error from the client into one that cannot quote the request
// URL, which carries the API key for Pixabay (and is nobody's business for
// the others either).
func scrubURLError(err error) error {
	var uerr *url.Error
	if errors.As(err, &uerr) {
		return fmt.Errorf("%s %s: %w", uerr.Op, "request", uerr.Err)
	}
	return err
}

func (s *Service) get(ctx context.Context, u string, header http.Header, out any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, scrubURLError(err)
	}
	for k, v := range header {
		req.Header[k] = v
	}
	req.Header.Set("Accept", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return 0, scrubURLError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, fmt.Errorf("status %d", resp.StatusCode)
	}
	return resp.StatusCode, json.NewDecoder(resp.Body).Decode(out)
}

func (s *Service) fetchBing(ctx context.Context, market string) (Picture, error) {
	q := url.Values{"format": {"js"}, "idx": {"0"}, "n": {"1"}, "mkt": {market}}
	var body struct {
		Images []struct {
			URLBase       string `json:"urlbase"`
			Title         string `json:"title"`
			Copyright     string `json:"copyright"`
			CopyrightLink string `json:"copyrightlink"`
		} `json:"images"`
	}
	if _, err := s.get(ctx, s.bingURL+"/HPImageArchive.aspx?"+q.Encode(), nil, &body); err != nil {
		return Picture{}, fmt.Errorf("%w: bing: %v", ErrUnavailable, err)
	}
	if len(body.Images) == 0 {
		return Picture{}, fmt.Errorf("%w: bing: no picture", ErrUnavailable)
	}
	img := body.Images[0]
	if !strings.HasPrefix(img.URLBase, "/th?id=") || !strings.HasPrefix(img.CopyrightLink, BingOrigin+"/") {
		return Picture{}, fmt.Errorf("%w: bing: unexpected picture or copyright address", ErrUnavailable)
	}
	return Picture{
		ImageURL:     BingOrigin + img.URLBase + "_UHD.jpg&w=2560",
		Title:        img.Title,
		Copyright:    img.Copyright,
		CopyrightURL: img.CopyrightLink,
	}, nil
}

func (s *Service) fetchUnsplash(ctx context.Context, key, query string, count int) ([]Picture, error) {
	q := url.Values{
		"count":          {fmt.Sprint(count)},
		"orientation":    {"landscape"},
		"content_filter": {"high"},
	}
	if query != "" {
		q.Set("query", query)
	}
	header := http.Header{
		"Authorization":  {"Client-ID " + key},
		"Accept-Version": {"v1"},
	}
	var body []struct {
		URLs struct {
			Raw string `json:"raw"`
		} `json:"urls"`
		Links struct {
			HTML string `json:"html"`
		} `json:"links"`
		Description    string `json:"description"`
		AltDescription string `json:"alt_description"`
		User           struct {
			Name  string `json:"name"`
			Links struct {
				HTML string `json:"html"`
			} `json:"links"`
		} `json:"user"`
	}
	status, err := s.get(ctx, s.unsplashURL+"/photos/random?"+q.Encode(), header, &body)
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return nil, ErrKeyRejected
	}
	if err != nil {
		return nil, fmt.Errorf("%w: unsplash: %v", ErrUnavailable, err)
	}
	var out []Picture
	for _, p := range body {
		if !strings.HasPrefix(p.URLs.Raw, unsplashImageOrigin) ||
			!strings.HasPrefix(p.Links.HTML, unsplashSiteOrigin) ||
			!strings.HasPrefix(p.User.Links.HTML, unsplashSiteOrigin) {
			continue
		}
		// The photographer's own description, else Unsplash's alt text.
		title := strings.TrimSpace(p.Description)
		if title == "" {
			title = strings.TrimSpace(p.AltDescription)
		}
		out = append(out, Picture{
			ImageURL:        withQuery(p.URLs.Raw, "w=2560&q=80&auto=format"),
			Title:           title,
			Photographer:    p.User.Name,
			PhotographerURL: withQuery(p.User.Links.HTML, referral),
			PhotoURL:        withQuery(p.Links.HTML, referral),
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: unsplash: no usable photo", ErrUnavailable)
	}
	return out, nil
}

func (s *Service) fetchPixabay(ctx context.Context, key, query, lang string) ([]cachedPicture, error) {
	q := url.Values{
		"key":         {key},
		"lang":        {lang},
		"image_type":  {"photo"},
		"orientation": {"horizontal"},
		"min_width":   {"1920"},
		"safesearch":  {"true"},
		"order":       {"popular"},
		"per_page":    {fmt.Sprint(pixabayBatch)},
	}
	if query != "" {
		q.Set("q", query)
	}
	var body struct {
		Hits []struct {
			ID            int    `json:"id"`
			PageURL       string `json:"pageURL"`
			LargeImageURL string `json:"largeImageURL"`
			FullHDURL     string `json:"fullHDURL"`
			User          string `json:"user"`
		} `json:"hits"`
	}
	status, err := s.get(ctx, s.pixabayURL+"?"+q.Encode(), nil, &body)
	if status == http.StatusBadRequest || status == http.StatusUnauthorized || status == http.StatusForbidden {
		return nil, ErrPixabayKeyRejected
	}
	if err != nil {
		return nil, fmt.Errorf("%w: pixabay: %v", ErrUnavailable, err)
	}
	hosts := s.imageHosts()
	var out []cachedPicture
	for _, hit := range body.Hits {
		if !httpsHost(hit.PageURL, hosts) {
			continue
		}
		// A full-HD address appears only for approved accounts; a future one
		// on a host outside the allow-list is dropped here.
		image := hit.LargeImageURL
		if hit.FullHDURL != "" && httpsHost(hit.FullHDURL, hosts) {
			image = hit.FullHDURL
		}
		if !httpsHost(image, hosts) {
			continue
		}
		out = append(out, cachedPicture{
			Picture: Picture{
				// The credit shows the author linked to the photo page; the
				// platform is named by the link, not in words.
				ImageURL:     "/branding/wallpaper/images/pixabay/" + itoa(hit.ID),
				Photographer: hit.User,
				PhotoURL:     hit.PageURL,
			},
			id:       itoa(hit.ID),
			upstream: image,
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: pixabay: no usable picture", ErrUnavailable)
	}
	return out, nil
}

// httpsHost reports whether u is an https address on hosts.
func httpsHost(u string, hosts []string) bool {
	return checkImageAddress(u, hosts) == nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

func withQuery(u, extra string) string {
	if strings.Contains(u, "?") {
		return u + "&" + extra
	}
	return u + "?" + extra
}
