package clientip

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
)

// maxTrustedProxies caps the CIDR list: defends the admin PUT and keeps the parsed
// set small on the hot path.
const maxTrustedProxies = 64

// Stored is the raw DB representation (strings) of the client-IP policy.
type Stored struct {
	Strategy       string
	Header         string
	TrustedProxies []string
}

// Store is the persistence seam (real impl in store_pg.go; fakes in tests).
type Store interface {
	Get(ctx context.Context) (Stored, error)
	Set(ctx context.Context, s Stored) error
}

// Resolver resolves the effective client IP under the DB-stored policy. The parsed
// Config is cached after first load; Set()/Invalidate() clear it.
type Resolver struct {
	st Store

	mu     sync.RWMutex
	cached *Config // nil = not loaded
}

// NewResolver builds a resolver over the given store.
func NewResolver(st Store) *Resolver { return &Resolver{st: st} }

// Config returns the parsed policy, loading+caching on first use. A read or parse
// error degrades to Direct (fail-safe: never trust unvalidated headers when the
// policy can't be read).
func (r *Resolver) Config(ctx context.Context) Config {
	r.mu.RLock()
	c := r.cached
	r.mu.RUnlock()
	if c != nil {
		return *c
	}
	cfg := Config{Strategy: Direct}
	if raw, err := r.st.Get(ctx); err == nil {
		if parsed, perr := ParseStored(raw); perr == nil {
			cfg = parsed
		}
	}
	r.mu.Lock()
	r.cached = &cfg
	r.mu.Unlock()
	return cfg
}

// IP is the hot-path helper: parsed policy + Extract.
// Safe to call on a nil *Resolver: falls back to the direct (RemoteAddr)
// strategy, which is the same safe default Config.go returns on store error.
// This allows test scaffolding that builds *Server without wiring clientIP.
func (r *Resolver) IP(req *http.Request) string {
	if r == nil {
		return Extract(req, Config{Strategy: Direct})
	}
	return Extract(req, r.Config(req.Context()))
}

// Stored returns the raw stored policy for the admin GET form. Bypasses the parsed
// cache (admin reads are rare) and returns direct-defaults on a store error.
func (r *Resolver) Stored(ctx context.Context) Stored {
	raw, err := r.st.Get(ctx)
	if err != nil {
		return Stored{Strategy: string(Direct), TrustedProxies: []string{}}
	}
	if raw.TrustedProxies == nil {
		raw.TrustedProxies = []string{}
	}
	return raw
}

// Set validates + persists the policy, then invalidates the cache.
func (r *Resolver) Set(ctx context.Context, raw Stored) error {
	if _, err := ParseStored(raw); err != nil {
		return err
	}
	if err := r.st.Set(ctx, raw); err != nil {
		return err
	}
	r.Invalidate()
	return nil
}

// Invalidate clears the cached parsed policy, forcing the next read to reload.
func (r *Resolver) Invalidate() {
	r.mu.Lock()
	r.cached = nil
	r.mu.Unlock()
}

// ParseStored validates raw strings and returns a ready Config. Single validation
// point shared by the resolver load path and the admin PUT handler.
func ParseStored(raw Stored) (Config, error) {
	cfg := Config{Header: strings.TrimSpace(raw.Header)}
	switch Strategy(strings.TrimSpace(raw.Strategy)) {
	case "", Direct:
		cfg.Strategy = Direct
	case Forwarded:
		cfg.Strategy = Forwarded
	case Header:
		cfg.Strategy = Header
		if cfg.Header == "" {
			return Config{}, fmt.Errorf("clientip: header name required for header strategy")
		}
		if !validHeaderName(cfg.Header) {
			return Config{}, fmt.Errorf("clientip: invalid header name %q", cfg.Header)
		}
	default:
		return Config{}, fmt.Errorf("clientip: unknown strategy %q", raw.Strategy)
	}
	if len(raw.TrustedProxies) > maxTrustedProxies {
		return Config{}, fmt.Errorf("clientip: too many trusted proxies (max %d)", maxTrustedProxies)
	}
	for _, s := range raw.TrustedProxies {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		_, ipnet, err := net.ParseCIDR(s)
		if err != nil {
			return Config{}, fmt.Errorf("clientip: invalid CIDR %q: %w", s, err)
		}
		cfg.TrustedProxies = append(cfg.TrustedProxies, ipnet)
	}
	return cfg, nil
}

// validHeaderName allows RFC 7230 token characters used by real HTTP header names.
func validHeaderName(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '-' || c == '_':
		default:
			return false
		}
	}
	return true
}
