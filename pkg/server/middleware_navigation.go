package server

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/kv"
	sessstore "prohibitorum/pkg/session"
	"prohibitorum/pkg/weberr"
)

// navigationHeader asks a protocol hop to name where it leads instead of
// redirecting there. The dashboard sends it when it continues a sign-in, so a
// hop that ends on one of its own pages (consent, sign-in, error) opens that
// page in place rather than loading the whole app again.
const navigationHeader = "X-Prohibitorum-Navigation"

// navigationHopPaths are the hops the dashboard follows itself: every outcome
// of each is a redirect. SAML SSO is not one of them, because its outcome can
// be a form the browser has to post.
var navigationHopPaths = map[string]bool{
	"/oauth/authorize":         true,
	accountChooserContinuePath: true,
}

// navigationPagePaths are the destinations a hop may name to the page: the
// dashboard pages the hops lead to, and the hops themselves. Anything else —
// an application's redirect_uri above all, whose query carries the
// authorization code — is held for the browser instead (handoffPath), so no
// script on the page ever reads it. A page missing here only costs a full
// load.
var navigationPagePaths = []string{
	"/", "/login", "/login/", "/consent", "/choose-account", "/error",
	"/maintenance", "/require-identity/",
}

// handoffPath is where the browser collects a redirect a hop held for it.
const handoffPath = "/oauth/handoff"

const (
	handoffKeyPrefix = "nav_handoff:"
	handoffTTL       = time.Minute
)

// navigationLocation is the answer a hop gives in place of its redirect.
type navigationLocation struct {
	Location string `json:"location"`
}

// navigationMW answers a hop's redirect as 200 {"location": …} when the
// request asks for it. A destination that is one of the dashboard's pages is
// named; any other is kept server-side under a key in an HttpOnly cookie and
// the answer names handoffPath, which the browser loads to be sent on. The
// hop itself runs once either way, and everything else it does — the session
// it reads, the cookies it sets, what it audits — is unchanged, as is any
// answer that is not a redirect.
//
// The custom header keeps the answer to this origin: another site cannot send
// it without a preflight, and these paths answer none. A browser that says
// the request came from elsewhere is served the redirect regardless. It wraps
// the maintenance gate, whose redirect away from authorize is named the same
// way.
func navigationMW(store kv.Store, cfg *configx.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !navigationHopPaths[r.URL.Path] {
				next.ServeHTTP(w, r)
				return
			}
			// The same address answers differently with the header, so no
			// cache may hand one answer to the other request.
			w.Header().Add("Vary", navigationHeader)
			if r.Method != http.MethodGet || r.Header.Get(navigationHeader) != "follow" {
				next.ServeHTTP(w, r)
				return
			}
			if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" {
				next.ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(&navigationWriter{ResponseWriter: w, r: r, store: store, cfg: cfg}, r)
		})
	}
}

// navigationWriter turns a redirect into its JSON answer and drops the
// redirect's own body.
type navigationWriter struct {
	http.ResponseWriter
	r     *http.Request
	store kv.Store
	cfg   *configx.Config
	named bool
}

func (w *navigationWriter) WriteHeader(code int) {
	location := w.Header().Get("Location")
	if code < 300 || code >= 400 || location == "" {
		w.ResponseWriter.WriteHeader(code)
		return
	}
	named := location
	if !navigationPage(location, w.cfg) {
		key, err := holdRedirect(w.r, w.store, location)
		if err != nil {
			// The browser cannot be sent there from here; the plain
			// redirect is what the page falls back from.
			w.ResponseWriter.WriteHeader(code)
			return
		}
		http.SetCookie(w.ResponseWriter, sessstore.HandoffCookie(w.cfg, handoffPath, key))
		named = handoffPath
	}
	w.named = true
	h := w.Header()
	h.Del("Location")
	h.Del("Content-Length")
	h.Set("Content-Type", "application/json")
	h.Set("Cache-Control", "no-store")
	w.ResponseWriter.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w.ResponseWriter).Encode(navigationLocation{Location: named})
}

func (w *navigationWriter) Write(b []byte) (int, error) {
	if w.named {
		return len(b), nil
	}
	return w.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (w *navigationWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// navigationPage reports whether location is one of navigationPagePaths on
// this site: a plain path, or an address on one of the public origins.
func navigationPage(location string, cfg *configx.Config) bool {
	u, err := url.Parse(location)
	// A code is never named, whatever address carries it.
	if err != nil || u.Opaque != "" || u.User != nil || u.Query().Has("code") {
		return false
	}
	if u.Scheme != "" || u.Host != "" {
		if cfg == nil || !isPublicOrigin(u.Scheme+"://"+u.Host, cfg.PublicOrigins) {
			return false
		}
	} else if !strings.HasPrefix(location, "/") || strings.HasPrefix(location, "//") || strings.Contains(location, "\\") {
		return false
	}
	if navigationHopPaths[u.Path] {
		return true
	}
	for _, page := range navigationPagePaths {
		if u.Path == page || (strings.HasSuffix(page, "/") && page != "/" && strings.HasPrefix(u.Path, page)) {
			return true
		}
	}
	return false
}

func isPublicOrigin(origin string, origins []string) bool {
	for _, o := range origins {
		if strings.EqualFold(strings.TrimSuffix(o, "/"), origin) {
			return true
		}
	}
	return false
}

// holdRedirect keeps location for the browser that asked and returns its key.
func holdRedirect(r *http.Request, store kv.Store, location string) (string, error) {
	if store == nil {
		return "", errors.New("navigation: no store")
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	key := base64.RawURLEncoding.EncodeToString(buf)
	if err := store.SetEx(r.Context(), handoffKeyPrefix+key, location, handoffTTL); err != nil {
		return "", err
	}
	return key, nil
}

// handleNavigationHandoff sends the browser on to the redirect a hop held for
// it, once. Without the cookie, or after the minute is up, there is nothing to
// continue, and the error page says so.
func (s *Server) handleNavigationHandoff(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	http.SetCookie(w, sessstore.ClearedHandoffCookie(s.config, handoffPath))
	c, err := r.Cookie(sessstore.HandoffCookieName)
	if err != nil || c.Value == "" {
		weberr.RedirectToError(w, r, "invalid_request", weberr.NewRef())
		return
	}
	location, err := s.kvStore.Pop(r.Context(), handoffKeyPrefix+c.Value)
	if err != nil {
		weberr.RedirectToError(w, r, "invalid_request", weberr.NewRef())
		return
	}
	http.Redirect(w, r, location, http.StatusFound)
}
