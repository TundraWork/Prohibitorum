// Package wallpaper fetches the sign-in page's wallpapers: Bing's picture of the
// day and a batch of random Unsplash photos. It only returns picture addresses and
// credits — the browser loads the pictures straight from Bing and Unsplash, so
// the server never downloads an image.
//
// Answers are cached for an hour. When a refresh fails, the last answer keeps
// being served (stale-if-error), and the failure itself is remembered for a
// minute so a down upstream is not asked again on every page load.
package wallpaper

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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

	unsplashImageOrigin = "https://images.unsplash.com/"
	unsplashSiteOrigin  = "https://unsplash.com/"
	unsplashBatch       = 30
	referral            = "utm_source=prohibitorum&utm_medium=referral"

	freshFor   = time.Hour
	retryAfter = time.Minute
)

var (
	// ErrUnavailable means the upstream could not be reached or answered with
	// something unusable, and there is no earlier answer to fall back on.
	ErrUnavailable = errors.New("wallpaper: upstream unavailable")
	// ErrKeyRejected means Unsplash refused the access key (401/403).
	ErrKeyRejected = errors.New("wallpaper: Unsplash rejected the access key")
)

// Picture is one wallpaper and its credit. Bing fills Title, Copyright and
// CopyrightURL; Unsplash fills the photographer and photo links.
type Picture struct {
	ImageURL        string
	Title           string
	Copyright       string
	CopyrightURL    string
	Photographer    string
	PhotographerURL string
	PhotoURL        string
}

// Options configure a Service. Zero values use the real endpoints and clock.
type Options struct {
	Client      *http.Client
	BingURL     string
	UnsplashURL string
	Now         func() time.Time
}

// Service fetches and caches wallpapers. It is safe for concurrent use.
type Service struct {
	client      *http.Client
	bingURL     string
	unsplashURL string
	now         func() time.Time

	flight   singleflight.Group
	mu       sync.Mutex
	bing     map[string]*entry[Picture]
	unsplash map[string]*entry[[]Picture]
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
		client:      o.Client,
		bingURL:     o.BingURL,
		unsplashURL: o.UnsplashURL,
		now:         o.Now,
		bing:        map[string]*entry[Picture]{},
		unsplash:    map[string]*entry[[]Picture]{},
	}
	if s.client == nil {
		s.client = http.DefaultClient
	}
	if s.bingURL == "" {
		s.bingURL = BingOrigin
	}
	if s.unsplashURL == "" {
		s.unsplashURL = UnsplashAPI
	}
	if s.now == nil {
		s.now = time.Now
	}
	return s
}

// Bing returns today's picture for market.
func (s *Service) Bing(ctx context.Context, market string) (Picture, error) {
	return cached(s, s.bing, "bing:"+market, func() (Picture, error) {
		return s.fetchBing(context.WithoutCancel(ctx), market)
	})
}

// Unsplash returns the cached batch of photos for key and query, in the order
// Unsplash sent them. Picking one at random or rotating through them is left
// to the browser.
func (s *Service) Unsplash(ctx context.Context, key, query string) ([]Picture, error) {
	sum := sha256.Sum256([]byte(key))
	batch, err := cached(s, s.unsplash, "unsplash:"+hex.EncodeToString(sum[:])+":"+query, func() ([]Picture, error) {
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

// Forget drops every cached Unsplash batch, so a new key or keyword applies
// on the next request.
func (s *Service) Forget() {
	s.mu.Lock()
	clear(s.unsplash)
	s.mu.Unlock()
}

// cached answers from m[key] while it is fresh, refreshes it otherwise, and
// falls back to the stale value when the refresh fails. Fetches run detached
// from the caller's cancellation, so a visitor closing the page does not
// record a failure for everyone else.
func cached[T any](s *Service, m map[string]*entry[T], key string, fetch func() (T, error)) (T, error) {
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

func (s *Service) get(ctx context.Context, u string, header http.Header, out any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, err
	}
	for k, v := range header {
		req.Header[k] = v
	}
	req.Header.Set("Accept", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return 0, err
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
		User struct {
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
		out = append(out, Picture{
			ImageURL:        withQuery(p.URLs.Raw, "w=2560&q=80&auto=format"),
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

func withQuery(u, extra string) string {
	if strings.Contains(u, "?") {
		return u + "&" + extra
	}
	return u + "?" + extra
}
