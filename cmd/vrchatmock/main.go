// Command vrchatmock serves the small, mutable subset of the VRChat API used by
// the end-to-end smoke test. API request records deliberately retain only
// method, path, User-Agent, and cookie names.
package main

import (
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

const controlBodyLimit = 2 << 20

type fixture struct {
	Username             string   `json:"username"`
	Password             string   `json:"password"`
	Code                 string   `json:"code"`
	AuthCookieValue      string   `json:"authCookieValue"`
	TwoFactorCookieValue string   `json:"twoFactorCookieValue"`
	RequireTwoFactor     bool     `json:"requireTwoFactor"`
	Methods              []string `json:"methods"`
	CurrentUserID        string   `json:"currentUserId"`
	PublicUserID         string   `json:"publicUserId"`
	DisplayName          string   `json:"displayName"`
	AvatarURL            string   `json:"avatarUrl"`
	BioLinks             []string `json:"bioLinks"`
	CurrentStatus        int      `json:"currentStatus"`
	PublicStatus         int      `json:"publicStatus"`
	RetryAfter           string   `json:"retryAfter"`
	PublicBodyMode       string   `json:"publicBodyMode"`
}

type requestRecord struct {
	Method      string   `json:"method"`
	Path        string   `json:"path"`
	UserAgent   string   `json:"userAgent"`
	CookieNames []string `json:"cookieNames"`
}

type mockState struct {
	mu      sync.Mutex
	fixture fixture
	records []requestRecord
}

func newMockState() *mockState {
	return &mockState{fixture: fixture{
		Username:             "vrchat-operator@example.test",
		Password:             "vrchat-mock-password",
		Code:                 "314159",
		AuthCookieValue:      "vrchat-auth-cookie",
		TwoFactorCookieValue: "vrchat-two-factor-cookie",
		Methods:              []string{"totp"},
		CurrentUserID:        "usr_00000000-0000-0000-0000-000000000001",
		PublicUserID:         "usr_00000000-0000-0000-0000-000000000001",
		DisplayName:          "VRChat Smoke User",
		AvatarURL:            "https://api.vrchat.cloud/avatar-smoke.png",
		BioLinks:             []string{},
	}}
}

func (s *mockState) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/1/auth/user", s.currentUser)
	mux.HandleFunc("POST /api/1/auth/twofactorauth/totp/verify", s.verify("totp"))
	mux.HandleFunc("POST /api/1/auth/twofactorauth/emailotp/verify", s.verify("emailOtp"))
	mux.HandleFunc("POST /api/1/auth/twofactorauth/otp/verify", s.verify("otp"))
	mux.HandleFunc("GET /api/1/users/{id}", s.publicUser)
	mux.HandleFunc("GET /control/ready", s.controlOnly(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	mux.HandleFunc("POST /control/state", s.controlOnly(s.setState))
	mux.HandleFunc("GET /control/requests", s.controlOnly(s.getRecords))
	mux.HandleFunc("DELETE /control/requests", s.controlOnly(s.clearRecords))
	return mux
}

func (s *mockState) snapshot() fixture {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fixture
}

func (s *mockState) requestRecords() []requestRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]requestRecord, len(s.records))
	copy(out, s.records)
	return out
}

func (s *mockState) record(r *http.Request) {
	names := make([]string, 0, len(r.Cookies()))
	seen := make(map[string]struct{}, len(r.Cookies()))
	for _, cookie := range r.Cookies() {
		if _, ok := seen[cookie.Name]; !ok {
			seen[cookie.Name] = struct{}{}
			names = append(names, cookie.Name)
		}
	}
	sort.Strings(names)
	s.mu.Lock()
	s.records = append(s.records, requestRecord{Method: r.Method, Path: r.URL.EscapedPath(), UserAgent: r.UserAgent(), CookieNames: names})
	s.mu.Unlock()
}

func (s *mockState) currentUser(w http.ResponseWriter, r *http.Request) {
	s.record(r)
	f := s.snapshot()
	if f.CurrentStatus != 0 {
		writeControlledStatus(w, f.CurrentStatus, f.RetryAfter)
		return
	}
	basicOK := r.Header.Get("Authorization") == "Basic "+base64.StdEncoding.EncodeToString([]byte(url.QueryEscape(f.Username)+":"+url.QueryEscape(f.Password)))
	authOK := cookieEquals(r, "auth", f.AuthCookieValue)
	if !basicOK && !authOK {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if basicOK {
		setMockCookie(w, "auth", f.AuthCookieValue)
	}
	if f.RequireTwoFactor && !cookieEquals(r, "twoFactorAuth", f.TwoFactorCookieValue) {
		methods := f.Methods
		if len(methods) == 0 {
			methods = []string{"totp"}
		}
		writeJSON(w, map[string]any{"requiresTwoFactorAuth": methods})
		return
	}
	writeJSON(w, map[string]any{"id": f.CurrentUserID, "displayName": f.DisplayName})
}

func (s *mockState) verify(method string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.record(r)
		f := s.snapshot()
		if !cookieEquals(r, "auth", f.AuthCookieValue) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var body struct {
			Code string `json:"code"`
		}
		decoder := json.NewDecoder(io.LimitReader(r.Body, 4097))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&body); err != nil || body.Code == "" || decoder.Decode(&struct{}{}) != io.EOF {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		allowed := false
		for _, candidate := range f.Methods {
			allowed = allowed || candidate == method
		}
		if !allowed || body.Code != f.Code {
			writeJSON(w, map[string]bool{"verified": false})
			return
		}
		setMockCookie(w, "twoFactorAuth", f.TwoFactorCookieValue)
		writeJSON(w, map[string]bool{"verified": true})
	}
}

func (s *mockState) publicUser(w http.ResponseWriter, r *http.Request) {
	s.record(r)
	f := s.snapshot()
	if f.PublicStatus != 0 {
		writeControlledStatus(w, f.PublicStatus, f.RetryAfter)
		return
	}
	if !cookieEquals(r, "auth", f.AuthCookieValue) || (f.RequireTwoFactor && !cookieEquals(r, "twoFactorAuth", f.TwoFactorCookieValue)) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	switch f.PublicBodyMode {
	case "malformed":
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, "{")
		return
	case "oversized":
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"padding":"`+strings.Repeat("x", (1<<20)+1)+`"}`)
		return
	case "":
	default:
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if f.BioLinks == nil {
		f.BioLinks = []string{}
	}
	writeJSON(w, map[string]any{"id": f.PublicUserID, "displayName": f.DisplayName, "bioLinks": f.BioLinks, "currentAvatarThumbnailImageUrl": f.AvatarURL})
}

func (s *mockState) controlOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

func (s *mockState) setState(w http.ResponseWriter, r *http.Request) {
	var next fixture
	decoder := json.NewDecoder(io.LimitReader(r.Body, controlBodyLimit+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&next); err != nil || decoder.Decode(&struct{}{}) != io.EOF || next.Username == "" || next.Password == "" || next.Code == "" || next.AuthCookieValue == "" || next.TwoFactorCookieValue == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	s.fixture = next
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (s *mockState) getRecords(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.requestRecords())
}
func (s *mockState) clearRecords(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	s.records = nil
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func cookieEquals(r *http.Request, name, value string) bool {
	cookie, err := r.Cookie(name)
	return err == nil && cookie.Value == value
}

func setMockCookie(w http.ResponseWriter, name, value string) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, Expires: time.Now().Add(time.Hour)})
}

func writeControlledStatus(w http.ResponseWriter, status int, retryAfter string) {
	if status < 400 || status > 599 {
		status = http.StatusInternalServerError
	}
	if status == http.StatusTooManyRequests && retryAfter != "" {
		w.Header().Set("Retry-After", retryAfter)
	}
	w.WriteHeader(status)
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func newLoopbackProxy(rawTarget string) (http.Handler, error) {
	target, err := url.Parse(rawTarget)
	if err != nil || target.Scheme != "http" || target.Host == "" || target.User != nil || target.Path != "" || target.RawQuery != "" || target.Fragment != "" {
		return nil, errors.New("vrchatmock: invalid proxy target")
	}
	ip := net.ParseIP(target.Hostname())
	if ip == nil || !ip.IsLoopback() {
		return nil, errors.New("vrchatmock: proxy target must be loopback")
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	director := proxy.Director
	proxy.Director = func(request *http.Request) {
		director(request)
		request.Header.Set("X-Forwarded-Proto", "https")
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) {
		w.WriteHeader(http.StatusBadGateway)
	}
	return proxy, nil
}

func main() {
	addr := flag.String("addr", "127.0.0.1:18100", "loopback HTTPS listen address")
	proxyAddr := flag.String("proxy-addr", "127.0.0.1:18443", "loopback HTTPS reverse-proxy listen address")
	proxyTarget := flag.String("proxy-target", "http://127.0.0.1:8080", "loopback HTTP reverse-proxy target")
	certFile := flag.String("cert", "", "TLS certificate PEM")
	keyFile := flag.String("key", "", "TLS private key PEM")
	flag.Parse()
	for _, listenAddr := range []string{*addr, *proxyAddr} {
		host, _, err := net.SplitHostPort(listenAddr)
		if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
			log.Fatal("vrchatmock: listen addresses must be loopback IPs and ports")
		}
	}
	if *certFile == "" || *keyFile == "" {
		log.Fatal("vrchatmock: -cert and -key are required")
	}
	proxy, err := newLoopbackProxy(*proxyTarget)
	if err != nil {
		log.Fatal("vrchatmock: invalid proxy target")
	}
	certificate, err := tls.LoadX509KeyPair(*certFile, *keyFile)
	if err != nil {
		log.Fatal("vrchatmock: invalid TLS key pair")
	}
	apiListener, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal("vrchatmock: API listen failed")
	}
	proxyListener, err := net.Listen("tcp", *proxyAddr)
	if err != nil {
		_ = apiListener.Close()
		log.Fatal("vrchatmock: proxy listen failed")
	}
	tlsConfig := &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12}
	apiServer := &http.Server{Handler: newMockState().routes(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second}
	proxyServer := &http.Server{Handler: proxy, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second}
	go func() {
		log.Printf("vrchatmock: HTTPS proxy listening on %s", *proxyAddr)
		if err := proxyServer.Serve(tls.NewListener(proxyListener, tlsConfig.Clone())); err != nil {
			log.Fatal("vrchatmock: proxy serve failed")
		}
	}()
	log.Printf("vrchatmock: HTTPS API listening on %s", *addr)
	if err := apiServer.Serve(tls.NewListener(apiListener, tlsConfig)); err != nil {
		log.Fatal("vrchatmock: API serve failed")
	}
}
