# Configurable client-IP resolution (CDN/proxy aware) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers-extended-cc:subagent-driven-development (recommended) or superpowers-extended-cc:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the spoofable, inconsistent client-IP handling with one peer-validated resolver used by every IP acquisition site, configured from the dashboard Admin → Settings screen.

**Architecture:** A new leaf package `pkg/clientip` provides a pure `Extract(r, Config)` (peer-validated: forwarding headers trusted only when the TCP peer is inside admin-configured trusted-proxy CIDRs) and a cached `Resolver` backed by three new `instance_settings` columns. The resolver's `IP(r)` method is injected into the session middleware, OIDC provider, SAML IdP, and every server audit site, retiring the static `trust_proxy` config.

**Tech Stack:** Go (net/http, pgx, goose migrations, chi, huma), Vue 3 + Vite + Tailwind v4 + shadcn-vue, vitest, vue-tsc.

**User decisions (already made):**
- Trust model: **peer-validated** — a header is trusted only when `RemoteAddr` is inside the configured trusted-proxy CIDRs; empty list ⇒ headers never trusted (fail-safe to peer).
- CIDR source: **manual only** — no compiled-in presets, no auto-fetch.
- `trust_proxy`: **removed** — DB config is the sole source; default `direct`; startup warning if the legacy key persists.
- Scope: **all in one** — full unification of every audit/rate-limit/middleware IP site in this feature.
- CIDR input UX: **textarea + on-save validation** (server validates; no inline client-side CIDR parsing).

Spec: `docs/superpowers/specs/2026-07-05-configurable-client-ip-resolution-design.md`

---

## File Structure

**New files**
- `pkg/clientip/clientip.go` — `Strategy`, `Config`, `Extract`, host/XFF helpers (pure).
- `pkg/clientip/clientip_test.go` — `Extract` table tests.
- `pkg/clientip/resolver.go` — `Stored`, `Store`, `ParseStored`, `Resolver` (cache/invalidate).
- `pkg/clientip/resolver_test.go` — `ParseStored` + resolver cache tests (fake store).
- `pkg/clientip/store_pg.go` — `PGStore` over `instance_settings`.
- `db/migrations/027_client_ip_config.sql` — three new columns.
- `pkg/server/handle_admin_client_ip.go` — admin GET/PUT handlers + JSON body type.
- `pkg/server/handle_admin_client_ip_test.go` — handler tests.

**Modified files**
- `pkg/configx/configx.go` — remove `TrustProxy` field + default; add deprecation warning.
- `pkg/session/middleware.go` — `LoadSession` takes an IP func; delete `ClientIP`.
- `pkg/server/server.go` — build resolver; struct field; wire into middleware/OIDC/SAML; routes.
- `pkg/server/*.go` — replace `sessstore.ClientIP(r, s.config.TrustProxy)` → `s.clientIP.IP(r)`.
- `pkg/protocol/oidc/oidc.go` + OIDC audit sites — inject IP seam; replace `r.RemoteAddr`.
- `pkg/protocol/saml/saml.go` + SAML audit sites — inject IP seam; replace `r.RemoteAddr`.
- `dashboard/src/pages/admin/SettingsView.vue` — new "Client IP / Proxy" card.
- `dashboard/src/pages/admin/SettingsView.test.ts` — card tests.
- `dashboard/src/locales/en.ts`, `dashboard/src/locales/zh.ts` — i18n keys.
- `cmd/smoke/main.go` — client-IP smoke block.

---

### Task 1: `pkg/clientip` extraction core

**Goal:** A pure, spoof-resistant `Extract(r, Config)` that returns the effective client IP host, with exhaustive table tests.

**Files:**
- Create: `pkg/clientip/clientip.go`
- Test: `pkg/clientip/clientip_test.go`

**Acceptance Criteria:**
- [ ] `Direct` (and empty/unknown strategy) returns the peer host, ignoring all headers.
- [ ] `Header` returns the header IP only when the peer is inside a trusted CIDR; otherwise the peer; missing/invalid header ⇒ peer.
- [ ] `Forwarded` returns the first non-trusted entry walking `X-Forwarded-For` right-to-left when the peer is trusted; ignores the header when the peer is untrusted.
- [ ] Spoof case: peer trusted, `X-Forwarded-For: 9.9.9.9, 203.0.113.5` with `203.0.113.5` NOT trusted ⇒ `203.0.113.5` (not the injected `9.9.9.9`).
- [ ] IPv6 peers with brackets/ports parse; malformed XFF entries are skipped.
- [ ] `go test ./pkg/clientip/` passes.

**Verify:** `go test ./pkg/clientip/ -run TestExtract -v` → PASS

**Steps:**

- [ ] **Step 1: Write `pkg/clientip/clientip.go`**

```go
// Package clientip resolves the effective client IP from an *http.Request under a
// configurable, spoof-resistant trust policy. A forwarding header (X-Forwarded-For
// or a single named header such as CF-Connecting-IP) is honored ONLY when the direct
// TCP peer is inside the configured trusted-proxy CIDR set; otherwise the peer address
// is used. This defeats header spoofing by clients that can reach the origin directly.
package clientip

import (
	"net"
	"net/http"
	"strings"
)

// Strategy selects how the client IP is extracted.
type Strategy string

const (
	// Direct ignores all forwarding headers and uses the TCP peer. Default.
	Direct Strategy = "direct"
	// Forwarded reads X-Forwarded-For, walking right-to-left past trusted proxies.
	Forwarded Strategy = "forwarded"
	// Header reads a single named header (e.g. CF-Connecting-IP) holding one IP.
	Header Strategy = "header"
)

// Config is the parsed, ready-to-use trust policy.
type Config struct {
	Strategy       Strategy
	Header         string       // header name for the Header strategy
	TrustedProxies []*net.IPNet // ranges permitted to set forwarding headers
}

// Extract returns the effective client IP as a bare host string (no port). It never
// panics; on any ambiguity it returns the direct peer host.
func Extract(r *http.Request, cfg Config) string {
	peer := peerHost(r.RemoteAddr)
	switch cfg.Strategy {
	case Header:
		if !trusted(peer, cfg.TrustedProxies) {
			return peer
		}
		if ip := parseHost(r.Header.Get(cfg.Header)); ip != "" {
			return ip
		}
		return peer
	case Forwarded:
		if !trusted(peer, cfg.TrustedProxies) {
			return peer
		}
		if c := clientFromXFF(r.Header.Get("X-Forwarded-For"), cfg.TrustedProxies); c != "" {
			return c
		}
		return peer
	default: // Direct and anything unrecognized
		return peer
	}
}

// peerHost strips the port from a RemoteAddr ("1.2.3.4:5678" -> "1.2.3.4",
// "[::1]:5678" -> "::1"). If there is no port it returns s trimmed of brackets.
func peerHost(s string) string {
	if host, _, err := net.SplitHostPort(s); err == nil {
		return host
	}
	return strings.Trim(s, "[]")
}

// trusted reports whether ip (bare host) parses and falls inside any CIDR.
func trusted(ip string, cidrs []*net.IPNet) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	for _, n := range cidrs {
		if n != nil && n.Contains(parsed) {
			return true
		}
	}
	return false
}

// parseHost validates a single-IP value (tolerating an "ip:port" form) and returns
// the canonical IP string, or "" if empty/invalid.
func parseHost(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(v); err == nil {
		v = host
	}
	if ip := net.ParseIP(strings.Trim(v, "[]")); ip != nil {
		return ip.String()
	}
	return ""
}

// clientFromXFF walks X-Forwarded-For right-to-left and returns the first entry that
// is a valid IP and NOT inside a trusted CIDR — the real client behind the trusted
// proxies. If every entry is trusted, it returns the leftmost valid entry. Returns
// "" when no valid entry exists.
func clientFromXFF(xff string, cidrs []*net.IPNet) string {
	parts := strings.Split(xff, ",")
	var leftmost string
	for i := len(parts) - 1; i >= 0; i-- {
		ip := parseHost(parts[i])
		if ip == "" {
			continue
		}
		leftmost = ip // walking left, the last valid one we see is the leftmost
		if !trusted(ip, cidrs) {
			return ip
		}
	}
	return leftmost
}
```

- [ ] **Step 2: Write `pkg/clientip/clientip_test.go`**

```go
package clientip

import (
	"net"
	"net/http"
	"testing"
)

func cidrs(t *testing.T, ss ...string) []*net.IPNet {
	t.Helper()
	var out []*net.IPNet
	for _, s := range ss {
		_, n, err := net.ParseCIDR(s)
		if err != nil {
			t.Fatalf("bad test CIDR %q: %v", s, err)
		}
		out = append(out, n)
	}
	return out
}

func TestExtract(t *testing.T) {
	tests := []struct {
		name       string
		cfg        Config
		remoteAddr string
		headers    map[string]string
		want       string
	}{
		{
			name:       "direct ignores headers",
			cfg:        Config{Strategy: Direct},
			remoteAddr: "203.0.113.7:5000",
			headers:    map[string]string{"X-Forwarded-For": "9.9.9.9", "CF-Connecting-IP": "8.8.8.8"},
			want:       "203.0.113.7",
		},
		{
			name:       "empty strategy behaves as direct",
			cfg:        Config{},
			remoteAddr: "203.0.113.7:5000",
			want:       "203.0.113.7",
		},
		{
			name:       "header trusted peer",
			cfg:        Config{Strategy: Header, Header: "CF-Connecting-IP", TrustedProxies: cidrs(t, "203.0.113.0/24")},
			remoteAddr: "203.0.113.7:5000",
			headers:    map[string]string{"CF-Connecting-IP": "198.51.100.23"},
			want:       "198.51.100.23",
		},
		{
			name:       "header untrusted peer falls back to peer",
			cfg:        Config{Strategy: Header, Header: "CF-Connecting-IP", TrustedProxies: cidrs(t, "203.0.113.0/24")},
			remoteAddr: "192.0.2.9:5000",
			headers:    map[string]string{"CF-Connecting-IP": "198.51.100.23"},
			want:       "192.0.2.9",
		},
		{
			name:       "header missing falls back to peer",
			cfg:        Config{Strategy: Header, Header: "CF-Connecting-IP", TrustedProxies: cidrs(t, "203.0.113.0/24")},
			remoteAddr: "203.0.113.7:5000",
			want:       "203.0.113.7",
		},
		{
			name:       "header empty trusted list never trusts",
			cfg:        Config{Strategy: Header, Header: "CF-Connecting-IP"},
			remoteAddr: "203.0.113.7:5000",
			headers:    map[string]string{"CF-Connecting-IP": "198.51.100.23"},
			want:       "203.0.113.7",
		},
		{
			name:       "forwarded skips trusted from right",
			cfg:        Config{Strategy: Forwarded, TrustedProxies: cidrs(t, "203.0.113.0/24")},
			remoteAddr: "203.0.113.7:5000",
			headers:    map[string]string{"X-Forwarded-For": "198.51.100.23, 203.0.113.9"},
			want:       "198.51.100.23",
		},
		{
			name:       "forwarded spoof attempt from trusted peer",
			cfg:        Config{Strategy: Forwarded, TrustedProxies: cidrs(t, "203.0.113.0/24")},
			remoteAddr: "203.0.113.7:5000",
			headers:    map[string]string{"X-Forwarded-For": "9.9.9.9, 198.51.100.23"},
			want:       "198.51.100.23",
		},
		{
			name:       "forwarded untrusted peer ignores header",
			cfg:        Config{Strategy: Forwarded, TrustedProxies: cidrs(t, "203.0.113.0/24")},
			remoteAddr: "192.0.2.9:5000",
			headers:    map[string]string{"X-Forwarded-For": "198.51.100.23"},
			want:       "192.0.2.9",
		},
		{
			name:       "forwarded all trusted returns leftmost",
			cfg:        Config{Strategy: Forwarded, TrustedProxies: cidrs(t, "203.0.113.0/24")},
			remoteAddr: "203.0.113.7:5000",
			headers:    map[string]string{"X-Forwarded-For": "203.0.113.1, 203.0.113.2"},
			want:       "203.0.113.1",
		},
		{
			name:       "forwarded skips malformed entries",
			cfg:        Config{Strategy: Forwarded, TrustedProxies: cidrs(t, "203.0.113.0/24")},
			remoteAddr: "203.0.113.7:5000",
			headers:    map[string]string{"X-Forwarded-For": "198.51.100.23, garbage, 203.0.113.9"},
			want:       "198.51.100.23",
		},
		{
			name:       "ipv6 peer trusted header",
			cfg:        Config{Strategy: Header, Header: "CF-Connecting-IP", TrustedProxies: cidrs(t, "2001:db8::/32")},
			remoteAddr: "[2001:db8::1]:5000",
			headers:    map[string]string{"CF-Connecting-IP": "198.51.100.23"},
			want:       "198.51.100.23",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := &http.Request{RemoteAddr: tc.remoteAddr, Header: http.Header{}}
			for k, v := range tc.headers {
				r.Header.Set(k, v)
			}
			if got := Extract(r, tc.cfg); got != tc.want {
				t.Fatalf("Extract() = %q, want %q", got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./pkg/clientip/ -run TestExtract -v`
Expected: PASS (all subtests).

- [ ] **Step 4: Commit**

```bash
git add pkg/clientip/clientip.go pkg/clientip/clientip_test.go
git commit -m "feat(clientip): peer-validated client IP extraction core"
```

---

### Task 2: `pkg/clientip` resolver, store, and migration

**Goal:** DB-backed cached resolver (`instance_settings` columns) with shared string→Config validation.

**Files:**
- Create: `pkg/clientip/resolver.go`, `pkg/clientip/store_pg.go`, `db/migrations/027_client_ip_config.sql`
- Test: `pkg/clientip/resolver_test.go`

**Acceptance Criteria:**
- [ ] `ParseStored` accepts `direct`/`forwarded`/`header`/empty; rejects unknown strategy, header strategy with empty/invalid header name, non-CIDR entries, and lists > 64.
- [ ] `Resolver.Config` caches after first load; `Invalidate`/`Set` clear the cache; a store read error degrades to `Direct`.
- [ ] Migration adds `client_ip_strategy`/`client_ip_header`/`client_ip_trusted_proxies` and reverses cleanly.
- [ ] `go test ./pkg/clientip/` passes; migration applies against the dev DB.

**Verify:** `go test ./pkg/clientip/ -v` → PASS; `mise run db migrate` → applies 027 with no error

**Steps:**

- [ ] **Step 1: Write `db/migrations/027_client_ip_config.sql`**

```sql
-- +goose Up
-- Client-IP resolution policy: how the effective remote/user IP is extracted from
-- forwarding headers behind a CDN/reverse proxy. strategy is 'direct' (peer only),
-- 'forwarded' (X-Forwarded-For), or 'header' (a single named header, e.g.
-- CF-Connecting-IP). A header is trusted only when the direct peer is inside one of
-- client_ip_trusted_proxies (CIDRs); an empty set means headers are never trusted.
ALTER TABLE instance_settings
  ADD COLUMN IF NOT EXISTS client_ip_strategy        text   NOT NULL DEFAULT 'direct',
  ADD COLUMN IF NOT EXISTS client_ip_header          text   NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS client_ip_trusted_proxies text[] NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE instance_settings
  DROP COLUMN IF EXISTS client_ip_strategy,
  DROP COLUMN IF EXISTS client_ip_header,
  DROP COLUMN IF EXISTS client_ip_trusted_proxies;
```

- [ ] **Step 2: Write `pkg/clientip/resolver.go`**

```go
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
func (r *Resolver) IP(req *http.Request) string {
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
```

- [ ] **Step 3: Write `pkg/clientip/store_pg.go`**

```go
package clientip

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PGStore is the production store backed by the instance_settings singleton row.
type PGStore struct{ pool *pgxpool.Pool }

// NewPGStore creates a PGStore backed by the given connection pool.
func NewPGStore(pool *pgxpool.Pool) *PGStore { return &PGStore{pool: pool} }

func (s *PGStore) Get(ctx context.Context) (Stored, error) {
	var out Stored
	row := s.pool.QueryRow(ctx,
		`SELECT client_ip_strategy, client_ip_header, client_ip_trusted_proxies
		   FROM instance_settings WHERE id = 1`)
	if err := row.Scan(&out.Strategy, &out.Header, &out.TrustedProxies); err != nil {
		return Stored{}, err
	}
	return out, nil
}

func (s *PGStore) Set(ctx context.Context, in Stored) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE instance_settings
		    SET client_ip_strategy = $1, client_ip_header = $2, client_ip_trusted_proxies = $3, updated_at = now()
		  WHERE id = 1`,
		in.Strategy, in.Header, in.TrustedProxies)
	return err
}
```

- [ ] **Step 4: Write `pkg/clientip/resolver_test.go`**

```go
package clientip

import (
	"context"
	"errors"
	"testing"
)

type fakeStore struct {
	get  Stored
	err  error
	sets []Stored
}

func (f *fakeStore) Get(context.Context) (Stored, error) { return f.get, f.err }
func (f *fakeStore) Set(_ context.Context, s Stored) error {
	f.sets = append(f.sets, s)
	return nil
}

func TestParseStored(t *testing.T) {
	ok := []Stored{
		{Strategy: "direct"},
		{Strategy: ""},
		{Strategy: "forwarded", TrustedProxies: []string{"203.0.113.0/24", "2001:db8::/32"}},
		{Strategy: "header", Header: "CF-Connecting-IP", TrustedProxies: []string{"10.0.0.0/8"}},
	}
	for _, s := range ok {
		if _, err := ParseStored(s); err != nil {
			t.Fatalf("ParseStored(%+v) unexpected error: %v", s, err)
		}
	}
	bad := []Stored{
		{Strategy: "bogus"},
		{Strategy: "header", Header: ""},
		{Strategy: "header", Header: "bad header!"},
		{Strategy: "forwarded", TrustedProxies: []string{"not-a-cidr"}},
		{Strategy: "forwarded", TrustedProxies: make([]string, maxTrustedProxies+1)},
	}
	for _, s := range bad {
		if _, err := ParseStored(s); err == nil {
			t.Fatalf("ParseStored(%+v) expected error, got nil", s)
		}
	}
}

func TestResolverCacheAndInvalidate(t *testing.T) {
	fs := &fakeStore{get: Stored{Strategy: "header", Header: "CF-Connecting-IP", TrustedProxies: []string{"203.0.113.0/24"}}}
	r := NewResolver(fs)
	if got := r.Config(context.Background()).Strategy; got != Header {
		t.Fatalf("strategy = %q, want header", got)
	}
	// Mutate the underlying store; cache must still return the old value.
	fs.get = Stored{Strategy: "direct"}
	if got := r.Config(context.Background()).Strategy; got != Header {
		t.Fatalf("cache broken: strategy = %q, want header", got)
	}
	r.Invalidate()
	if got := r.Config(context.Background()).Strategy; got != Direct {
		t.Fatalf("post-invalidate strategy = %q, want direct", got)
	}
}

func TestResolverReadErrorFailsSafe(t *testing.T) {
	r := NewResolver(&fakeStore{err: errors.New("db down")})
	if got := r.Config(context.Background()).Strategy; got != Direct {
		t.Fatalf("read-error strategy = %q, want direct", got)
	}
}

func TestResolverSetValidates(t *testing.T) {
	fs := &fakeStore{}
	r := NewResolver(fs)
	if err := r.Set(context.Background(), Stored{Strategy: "bogus"}); err == nil {
		t.Fatal("Set with bad strategy should error")
	}
	if len(fs.sets) != 0 {
		t.Fatal("invalid Set must not reach the store")
	}
	if err := r.Set(context.Background(), Stored{Strategy: "direct"}); err != nil {
		t.Fatalf("valid Set errored: %v", err)
	}
	if len(fs.sets) != 1 {
		t.Fatalf("valid Set stored %d times, want 1", len(fs.sets))
	}
}
```

- [ ] **Step 5: Run tests + migration**

Run: `go test ./pkg/clientip/ -v`
Expected: PASS.
Run: `mise run db migrate`
Expected: migration `027` applies with no error (verify with `mise run db status`).

- [ ] **Step 6: Commit**

```bash
git add pkg/clientip/resolver.go pkg/clientip/store_pg.go pkg/clientip/resolver_test.go db/migrations/027_client_ip_config.sql
git commit -m "feat(clientip): DB-backed cached resolver + instance_settings columns"
```

---

### Task 3: Wire resolver into server + session middleware; retire `trust_proxy`

**Goal:** Build the resolver once, inject its `IP` into the session middleware and every `pkg/server` IP site, and remove the static `trust_proxy` config with a startup deprecation warning.

**Files:**
- Modify: `pkg/configx/configx.go` (remove field + default; add warning)
- Modify: `pkg/session/middleware.go` (`LoadSession` signature; delete `ClientIP`)
- Modify: `pkg/server/server.go` (build resolver, struct field, middleware wiring)
- Modify: all `pkg/server/*.go` using `sessstore.ClientIP(r, s.config.TrustProxy)`

**Acceptance Criteria:**
- [ ] `configx.Config` no longer has `TrustProxy`; `trust_proxy` default removed; startup logs a warning when the legacy key is set.
- [ ] `session.ClientIP` is deleted; `LoadSession` takes `ipOf func(*http.Request) string` and uses it.
- [ ] Every `sessstore.ClientIP(r, s.config.TrustProxy)` in `pkg/server` becomes `s.clientIP.IP(r)`.
- [ ] `go build ./...` and `go vet ./...` clean; `go test ./pkg/session/ ./pkg/server/` build+run.

**Verify:** `go build ./... && go vet ./... && go test ./pkg/session/` → PASS

**Steps:**

- [ ] **Step 1: Remove `TrustProxy` from `pkg/configx/configx.go`**

Delete the field line (currently `pkg/configx/configx.go:30`):

```go
	TrustProxy    bool          `mapstructure:"trust_proxy"`
```

Delete the default line (currently `:204`):

```go
	viper.SetDefault("trust_proxy", false)
```

- [ ] **Step 2: Add the deprecation warning in `configx.Parse`**

Add `"prohibitorum/pkg/logx"` to the import block. Immediately after `_ = viper.Unmarshal(&config)` (currently `:282`), add:

```go
	if viper.IsSet("trust_proxy") {
		logx.New().Warn("config: 'trust_proxy' is removed — configure client IP handling in Admin → Settings → Client IP; the direct connection IP is used until then")
	}
```

- [ ] **Step 3: Update `LoadSession` in `pkg/session/middleware.go` and delete `ClientIP`**

Change the signature (currently `:63`):

```go
func LoadSession(cfg *configx.Config, q db.Querier, store *SessionStore, ipOf func(*http.Request) string) func(http.Handler) http.Handler {
```

Change line 77 from `ip := ClientIP(r, cfg.TrustProxy)` to:

```go
			ip := ipOf(r)
```

Delete the entire `ClientIP` function (currently `:222-243`) and drop the now-unused `strings` import if nothing else in the file uses it (run `go build ./pkg/session/` to confirm; keep it if still referenced).

- [ ] **Step 4: Build the resolver and wire it in `pkg/server/server.go`**

Add the import `"prohibitorum/pkg/clientip"`. After the branding resolver block (currently ends `:187`), add:

```go
	clientIPResolver := clientip.NewResolver(clientip.NewPGStore(conn))
```

Change the middleware mount (currently `:190`) to pass the IP func:

```go
	router.Use(sessstore.LoadSession(config, queries, sessionStore, clientIPResolver.IP))
```

Add the struct field (in the `Server` struct near `branding`, `:129`):

```go
	// clientIP resolves the effective client IP under the DB-stored, peer-validated
	// policy. Admin PUT handlers call Invalidate() after writes.
	clientIP *clientip.Resolver
```

Add to the struct literal (near `branding: brandingResolver,`, `:235`):

```go
		clientIP: clientIPResolver,
```

- [ ] **Step 5: Replace `ClientIP` call sites in `pkg/server`**

Run to list them:

```bash
grep -rln "sessstore.ClientIP(r, s.config.TrustProxy)" pkg/server/
```

In every match (handle_auth.go, handle_auth_recovery.go, handle_pairing.go, handle_sudo.go, handle_me.go, handle_forward_auth_signout.go, handle_auth_password.go, handle_enrollment.go), replace each occurrence of:

```go
sessstore.ClientIP(r, s.config.TrustProxy)
```

with:

```go
s.clientIP.IP(r)
```

(`handle_sudo.go:348` and `:374` and every `"client_ip": ...` map entry included.) Then:

```bash
grep -rn "TrustProxy\|sessstore.ClientIP\|session.ClientIP" pkg/ | grep -v _test.go
```

must return nothing outside comments.

- [ ] **Step 6: Fix any test references**

```bash
grep -rln "TrustProxy\|\.ClientIP(" pkg/session/ pkg/server/ | grep _test.go
```

For each, update `LoadSession(...)` test calls to pass a stub IP func (e.g. `func(r *http.Request) string { return "127.0.0.1" }`) and remove `TrustProxy:` fields from any `configx.Config{...}` literals. If a test asserted the old leftmost-XFF behavior of `session.ClientIP`, move/rewrite it as a `clientip.Extract` case (already covered in Task 1 — delete the stale assertion).

- [ ] **Step 7: Build, vet, test**

Run: `go build ./... && go vet ./... && go test ./pkg/session/ ./pkg/server/ 2>&1 | tail -20`
Expected: build+vet clean; session tests PASS. (The `pkg/server` suite is known-flaky under shared-DB parallelism — re-run any flake in isolation.)

- [ ] **Step 8: Commit**

```bash
git add pkg/configx/configx.go pkg/session/middleware.go pkg/server/
git commit -m "refactor(server): route all session/rate-limit IP through clientip resolver; remove trust_proxy"
```

---

### Task 4: Migrate OIDC audit IP sites through the resolver seam

**Goal:** Inject an IP function into `oidc.Provider` and replace every `audit.ParseIPOrNil(r.RemoteAddr)` audit site with it (leaving forward-auth `X-Forwarded-*` resource resolution untouched).

**Files:**
- Modify: `pkg/protocol/oidc/oidc.go` (struct field + `New` param + `auditIP` helper)
- Modify: `pkg/protocol/oidc/{logout.go,revoke.go,authorize.go,token.go}` (audit sites)
- Modify: `pkg/server/server.go` (pass `clientIPResolver.IP` to `oidcop.New`)
- Modify: OIDC test constructors calling `oidc.New(...)`

**Acceptance Criteria:**
- [ ] `oidc.Provider` has a `clientIP func(*http.Request) string`; `New` takes it as the final param.
- [ ] All 5 OIDC `audit.ParseIPOrNil(r.RemoteAddr)` sites use `p.auditIP(r)`; a nil func falls back to `r.RemoteAddr` (test-safe).
- [ ] `forward_auth.go` `X-Forwarded-Host/Proto/Uri` logic is unchanged.
- [ ] `go build ./... && go test ./pkg/protocol/oidc/` PASS.

**Verify:** `go test ./pkg/protocol/oidc/ 2>&1 | tail -20` → PASS

**Steps:**

- [ ] **Step 1: Add the seam to `pkg/protocol/oidc/oidc.go`**

Add the field to `Provider` (after `maintenance`, `:40`):

```go
	// clientIP resolves the effective client IP for audit records. nil in bare-Config
	// unit tests, where auditIP falls back to the request peer.
	clientIP func(*http.Request) string
```

Change `New` (currently `:52`) to accept it as the final parameter and store it:

```go
func New(cfg *configx.Config, queries db.Querier, kvStore kv.Store, sessions *session.SessionStore, auditW audit.Writer, rl *authn.RateLimiter, clientIP func(*http.Request) string) *Provider {
	return &Provider{
		cfg:      cfg,
		queries:  queries,
		kv:       kvStore,
		sessions: sessions,
		audit:    auditW,
		rl:       rl,
		keys:     newKeyCache(queries, cfg.DataEncryptionKeys),
		clientIP: clientIP,
	}
}
```

Add the nil-safe helper (place near the bottom of `oidc.go`). It needs `net/http`; add the import if absent:

```go
// auditIP returns the effective client IP for audit records: the injected resolver
// when wired, otherwise the raw request peer (unit tests build Providers without it).
func (p *Provider) auditIP(r *http.Request) string {
	if p.clientIP != nil {
		return p.clientIP(r)
	}
	return r.RemoteAddr
}
```

- [ ] **Step 2: Replace the OIDC audit sites**

In each of `logout.go:122`, `revoke.go:74`, `authorize.go:155`, `authorize.go:342`, `token.go:338`, replace:

```go
		IP:        audit.ParseIPOrNil(r.RemoteAddr),
```

with:

```go
		IP:        audit.ParseIPOrNil(p.auditIP(r)),
```

Confirm each handler is a `*Provider` method (receiver `p`); they are. Do NOT touch `forward_auth.go` (its `X-Forwarded-*` reads resolve the protected resource, not the client IP).

- [ ] **Step 3: Update the server wiring**

In `pkg/server/server.go`, change the `oidcOP` construction (currently `:228`) to pass the IP func:

```go
		oidcOP:        oidcop.New(config, queries, kvStore, sessionStore, auditWriter, rateLimiter, clientIPResolver.IP),
```

- [ ] **Step 4: Update OIDC test constructors**

```bash
grep -rln "oidc.New(\|oidcop.New(" pkg/ | grep _test.go
```

For each `oidc.New(...)` in tests, append `, nil` as the final argument (nil ⇒ `auditIP` falls back to the peer).

- [ ] **Step 5: Build + test**

Run: `go build ./... && go test ./pkg/protocol/oidc/ 2>&1 | tail -20`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add pkg/protocol/oidc/ pkg/server/server.go
git commit -m "refactor(oidc): route audit client IP through clientip resolver"
```

---

### Task 5: Migrate SAML audit IP sites through the resolver seam

**Goal:** Same seam for `saml.IdP` and its 5 `audit.ParseIPOrNil(r.RemoteAddr)` audit sites.

**Files:**
- Modify: `pkg/protocol/saml/saml.go` (struct field + `NewIdP` param + `auditIP` helper)
- Modify: `pkg/protocol/saml/{sso.go,assertion.go,slo.go,consent_saml.go,sso_init.go}`
- Modify: `pkg/server/server.go` (pass `clientIPResolver.IP` to `samlidp.NewIdP`)
- Modify: SAML test constructors calling `NewIdP(...)`

**Acceptance Criteria:**
- [ ] `saml.IdP` has `clientIP func(*http.Request) string`; `NewIdP` takes it as the final param.
- [ ] All 5 SAML audit sites use `idp.auditIP(r)` with a nil-safe peer fallback (match the actual receiver name in `saml.go`).
- [ ] `go build ./... && go test ./pkg/protocol/saml/` PASS.

**Verify:** `go test ./pkg/protocol/saml/ 2>&1 | tail -20` → PASS

**Steps:**

- [ ] **Step 1: Inspect the `IdP` struct + receiver name**

Run: `sed -n '1,60p' pkg/protocol/saml/saml.go` — note the `IdP` struct fields and the method receiver name used across the package (assume `idp` below; match whatever `saml.go` uses).

- [ ] **Step 2: Add the seam to `pkg/protocol/saml/saml.go`**

Add to the `IdP` struct:

```go
	// clientIP resolves the effective client IP for audit records. nil in unit tests,
	// where auditIP falls back to the request peer.
	clientIP func(*http.Request) string
```

Change `NewIdP` (currently `:42`) to accept + store it as the final param:

```go
func NewIdP(cfg *configx.Config, queries db.Querier, kvStore kv.Store, sessions *session.SessionStore, auditW audit.Writer, rl *authn.RateLimiter, clientIP func(*http.Request) string) *IdP {
```

(assign `clientIP: clientIP` in the returned literal). Add the helper (add `net/http` import if absent; use the real receiver name):

```go
// auditIP returns the effective client IP for audit records: the injected resolver
// when wired, otherwise the raw request peer.
func (idp *IdP) auditIP(r *http.Request) string {
	if idp.clientIP != nil {
		return idp.clientIP(r)
	}
	return r.RemoteAddr
}
```

- [ ] **Step 3: Replace the SAML audit sites**

In `sso.go:166`, `assertion.go:293`, `slo.go:241`, `consent_saml.go:125`, `sso_init.go:126`, replace `audit.ParseIPOrNil(r.RemoteAddr)` with `audit.ParseIPOrNil(idp.auditIP(r))` (use the real receiver name). Verify each is an `*IdP` method; if any audit site lives in a non-method function, thread the IdP/func through its caller instead.

- [ ] **Step 4: Update the server wiring**

In `pkg/server/server.go` (`:229`):

```go
		samlIdP:       samlidp.NewIdP(config, queries, kvStore, sessionStore, auditWriter, rateLimiter, clientIPResolver.IP),
```

- [ ] **Step 5: Update SAML test constructors**

```bash
grep -rln "NewIdP(" pkg/ | grep _test.go
```

Append `, nil` as the final argument to each `NewIdP(...)` test call.

- [ ] **Step 6: Build + test**

Run: `go build ./... && go test ./pkg/protocol/saml/ 2>&1 | tail -20`
Expected: PASS. Then a final full check: `grep -rn "audit.ParseIPOrNil(r.RemoteAddr)" pkg/ | grep -v _test.go` should return nothing (all sites migrated; the admin `auditBranding` site is handled in Task 6).

- [ ] **Step 7: Commit**

```bash
git add pkg/protocol/saml/ pkg/server/server.go
git commit -m "refactor(saml): route audit client IP through clientip resolver"
```

---

### Task 6: Admin GET/PUT client-IP API

**Goal:** Admin read + sudo-gated write of the client-IP policy, with server-side validation and an audit record; also migrate the `auditBranding` RemoteAddr site.

**Files:**
- Create: `pkg/server/handle_admin_client_ip.go`
- Test: `pkg/server/handle_admin_client_ip_test.go`
- Modify: `pkg/server/server.go` (register routes)
- Modify: `pkg/server/handle_admin_settings.go` (`auditBranding` uses `s.clientIP.IP(r)`)

**Acceptance Criteria:**
- [ ] `GET /api/prohibitorum/admin/settings/client-ip` (admin) returns `{strategy, header, trustedProxies}`.
- [ ] `PUT` (admin + fresh sudo) validates via `clientip.ParseStored`, returns 400 on bad strategy/header/CIDR/oversize, 204 on success, invalidates the cache, and writes an audit record.
- [ ] `auditBranding` no longer uses raw `r.RemoteAddr`.
- [ ] `go test ./pkg/server/ -run ClientIP` PASS.

**Verify:** `go test ./pkg/server/ -run ClientIP -v 2>&1 | tail -30` → PASS

**Steps:**

- [ ] **Step 1: Write `pkg/server/handle_admin_client_ip.go`**

```go
// Package server — handle_admin_client_ip.go
//
// Admin read/write of the client-IP resolution policy (how the effective remote/user
// IP is extracted behind a CDN/reverse proxy). GET is a plain admin read; PUT goes
// through registerSudoOpHTTP (admin role + fresh sudo enforced by the wrapper).
package server

import (
	"encoding/json"
	"net/http"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/clientip"
)

type clientIPBody struct {
	Strategy       string   `json:"strategy"`
	Header         string   `json:"header"`
	TrustedProxies []string `json:"trustedProxies"`
}

// GET /api/prohibitorum/admin/settings/client-ip
func (s *Server) handleGetClientIPHTTP(w http.ResponseWriter, r *http.Request) {
	raw := s.clientIP.Stored(r.Context())
	writeJSON(w, clientIPBody{
		Strategy:       raw.Strategy,
		Header:         raw.Header,
		TrustedProxies: raw.TrustedProxies,
	})
}

// PUT /api/prohibitorum/admin/settings/client-ip — registerSudoOpHTTP (admin + sudo).
func (s *Server) handlePutClientIPHTTP(w http.ResponseWriter, r *http.Request) {
	var body clientIPBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	stored := clientip.Stored{
		Strategy:       body.Strategy,
		Header:         body.Header,
		TrustedProxies: body.TrustedProxies,
	}
	// clientip.ParseStored (invoked by Set) validates strategy/header/CIDRs/length.
	if err := s.clientIP.Set(r.Context(), stored); err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	s.auditBranding(r, "client_ip_config_updated")
	w.WriteHeader(http.StatusNoContent)
}
```

_Note: `audit` is imported for parity with sibling settings handlers; if `goimports` flags it as unused, drop it — `auditBranding` lives in `handle_admin_settings.go`._

- [ ] **Step 2: Migrate `auditBranding` off raw RemoteAddr**

In `pkg/server/handle_admin_settings.go:166`, change:

```go
		IP:        audit.ParseIPOrNil(r.RemoteAddr),
```

to:

```go
		IP:        audit.ParseIPOrNil(s.clientIP.IP(r)),
```

Also migrate `pkg/server/handle_admin_entity_icon.go:95` the same way (it is the remaining `pkg/server` raw-RemoteAddr audit site).

- [ ] **Step 3: Register the routes in `pkg/server/server.go`**

After the existing `admin/settings` routes (currently ending `:503`), add:

```go
	registerOpHTTP(s.router, "GET", "/api/prohibitorum/admin/settings/client-ip", admin, s.handleGetClientIPHTTP)
	s.registerSudoOpHTTP(s.router, "PUT", "/api/prohibitorum/admin/settings/client-ip", admin, s.handlePutClientIPHTTP)
```

- [ ] **Step 4: Write `pkg/server/handle_admin_client_ip_test.go`**

Mirror the setup idiom in `pkg/server/handle_admin_settings_test.go` (build a `Server` with a `clientip.Resolver` over an in-memory fake store, invoke handlers directly). Minimal shape:

```go
package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"prohibitorum/pkg/clientip"
)

type memClientIPStore struct{ s clientip.Stored }

func (m *memClientIPStore) Get(context.Context) (clientip.Stored, error) { return m.s, nil }
func (m *memClientIPStore) Set(_ context.Context, s clientip.Stored) error {
	m.s = s
	return nil
}

func TestClientIPPutValidation(t *testing.T) {
	s := &Server{clientIP: clientip.NewResolver(&memClientIPStore{})}
	bad := `{"strategy":"header","header":"","trustedProxies":[]}`
	req := httptest.NewRequest(http.MethodPut, "/api/prohibitorum/admin/settings/client-ip", strings.NewReader(bad))
	rec := httptest.NewRecorder()
	s.handlePutClientIPHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad header strategy: status = %d, want 400", rec.Code)
	}

	badCIDR := `{"strategy":"forwarded","trustedProxies":["nope"]}`
	req2 := httptest.NewRequest(http.MethodPut, "/x", strings.NewReader(badCIDR))
	rec2 := httptest.NewRecorder()
	s.handlePutClientIPHTTP(rec2, req2)
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("bad CIDR: status = %d, want 400", rec2.Code)
	}
}

func TestClientIPPutGetRoundTrip(t *testing.T) {
	s := &Server{clientIP: clientip.NewResolver(&memClientIPStore{})}
	good := `{"strategy":"header","header":"CF-Connecting-IP","trustedProxies":["203.0.113.0/24"]}`
	req := httptest.NewRequest(http.MethodPut, "/x", strings.NewReader(good))
	rec := httptest.NewRecorder()
	s.handlePutClientIPHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("valid PUT: status = %d, want 204", rec.Code)
	}
	getRec := httptest.NewRecorder()
	s.handleGetClientIPHTTP(getRec, httptest.NewRequest(http.MethodGet, "/x", nil))
	body := getRec.Body.String()
	for _, want := range []string{`"strategy":"header"`, `"CF-Connecting-IP"`, `"203.0.113.0/24"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("GET body %q missing %q", body, want)
		}
	}
}
```

_If `handlePutClientIPHTTP` on the `auditBranding` path nil-derefs when `s.Audit` is nil in these bare-Server tests, guard the test by setting a no-op `audit.Writer` the way `handle_admin_settings_test.go` does, or split the audit call so validation returns before it. Match whatever the sibling settings test does._

- [ ] **Step 5: Build + test**

Run: `go build ./... && go test ./pkg/server/ -run ClientIP -v 2>&1 | tail -30`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add pkg/server/handle_admin_client_ip.go pkg/server/handle_admin_client_ip_test.go pkg/server/server.go pkg/server/handle_admin_settings.go pkg/server/handle_admin_entity_icon.go
git commit -m "feat(server): admin GET/PUT client-IP policy API"
```

---

### Task 7: Dashboard "Client IP / Proxy" settings card

**Goal:** A new card in `SettingsView.vue` to view/edit the strategy, header, and trusted-proxy CIDRs, saved through sudo, with i18n (en + zh) and vitest coverage.

**Files:**
- Modify: `dashboard/src/pages/admin/SettingsView.vue`
- Modify: `dashboard/src/pages/admin/SettingsView.test.ts`
- Modify: `dashboard/src/locales/en.ts`, `dashboard/src/locales/zh.ts`

**Acceptance Criteria:**
- [ ] Card loads current policy via `GET /api/prohibitorum/admin/settings/client-ip` on mount.
- [ ] Strategy `Select` (Direct / X-Forwarded-For / Custom header); header `Input` shown only for `header`; CIDR `Textarea` shown for non-`direct`.
- [ ] Fail-safe warning renders when strategy ≠ direct and the CIDR box is empty.
- [ ] Save issues `PUT` through `withSudo` with `{strategy, header, trustedProxies}` (CIDRs split by newline, trimmed, blanks dropped).
- [ ] `vitest run` green for `SettingsView`; `vue-tsc -b` 0 errors.

**Verify:** `cd dashboard && npx vitest run src/pages/admin/SettingsView.test.ts && npx vue-tsc -b` → PASS

**Steps:**

- [ ] **Step 1: Add i18n keys**

In `dashboard/src/locales/en.ts`, inside the `settings: { ... }` block (before the closing `}` at `:580`), add:

```ts
      clientIpLabel: 'Client IP / Proxy',
      clientIpHelp: 'How the real visitor IP is read when this instance runs behind a CDN or reverse proxy. Used for audit logs and rate limiting.',
      clientIpStrategyLabel: 'Source',
      clientIpStrategyDirect: 'Direct connection (no proxy)',
      clientIpStrategyForwarded: 'X-Forwarded-For header',
      clientIpStrategyHeader: 'Custom header (e.g. Cloudflare)',
      clientIpHeaderLabel: 'Header name',
      clientIpHeaderHint: 'Common values: CF-Connecting-IP (Cloudflare), True-Client-IP, X-Real-IP, Fastly-Client-IP.',
      clientIpTrustedLabel: 'Trusted proxy ranges (CIDR, one per line)',
      clientIpTrustedHint: 'The IP ranges that connect directly to this server (your CDN edge or local proxy). A forwarding header is trusted only when the request comes from one of these ranges.',
      clientIpEmptyWarning: 'No trusted ranges set — forwarding headers are ignored and the direct connection IP is used until you add ranges.',
      clientIpSave: 'Save client IP settings',
```

In `dashboard/src/locales/zh.ts`, add the same keys inside its `settings` block with translations consistent with the file's register (你-register / hybrid terms):

```ts
      clientIpLabel: '客户端 IP / 代理',
      clientIpHelp: '当本实例运行在 CDN 或反向代理之后时，如何获取访客的真实 IP。用于审计日志和限流。',
      clientIpStrategyLabel: '来源',
      clientIpStrategyDirect: '直接连接（无代理）',
      clientIpStrategyForwarded: 'X-Forwarded-For 头',
      clientIpStrategyHeader: '自定义头（如 Cloudflare）',
      clientIpHeaderLabel: '头名称',
      clientIpHeaderHint: '常见取值：CF-Connecting-IP（Cloudflare）、True-Client-IP、X-Real-IP、Fastly-Client-IP。',
      clientIpTrustedLabel: '受信任的代理范围（CIDR，每行一个）',
      clientIpTrustedHint: '直接连接到本服务器的 IP 范围（你的 CDN 边缘或本地代理）。只有来自这些范围的请求，转发头才会被信任。',
      clientIpEmptyWarning: '未设置受信任范围 —— 在添加范围前，转发头将被忽略，使用直接连接的 IP。',
      clientIpSave: '保存客户端 IP 设置',
```

After editing, verify no apostrophe/`@` hazards: `grep -n "clientIp" dashboard/src/locales/en.ts` and confirm straight quotes (per the en.ts apostrophe + vue-i18n `@` memory notes).

- [ ] **Step 2: Add the card logic to `SettingsView.vue` `<script setup>`**

Add to the imports:

```ts
import { Select, SelectTrigger, SelectContent, SelectItem, SelectValue } from '@/components/ui/select'
```

Add state + load/save (place after the maintenance block, before `onPickFile`):

```ts
const clientIpStrategy = ref<'direct' | 'forwarded' | 'header'>('direct')
const clientIpHeader = ref('CF-Connecting-IP')
const clientIpTrusted = ref('') // textarea, one CIDR per line
const { flag: clientIpSavedFlag, trigger: triggerClientIpSaved } = useTransientFlag(2000)

async function loadClientIp(): Promise<void> {
  const cfg = await run(() =>
    api.get<{ strategy: string; header: string; trustedProxies: string[] }>(
      '/api/prohibitorum/admin/settings/client-ip',
    ),
  )
  if (!cfg) return
  clientIpStrategy.value = (cfg.strategy as 'direct' | 'forwarded' | 'header') || 'direct'
  if (cfg.header) clientIpHeader.value = cfg.header
  clientIpTrusted.value = (cfg.trustedProxies || []).join('\n')
}

async function saveClientIp(): Promise<void> {
  const trustedProxies = clientIpTrusted.value
    .split('\n')
    .map((s) => s.trim())
    .filter((s) => s.length > 0)
  const ok = await run(() =>
    withSudo(async () => {
      await api.put('/api/prohibitorum/admin/settings/client-ip', {
        strategy: clientIpStrategy.value,
        header: clientIpStrategy.value === 'header' ? clientIpHeader.value : '',
        trustedProxies,
      })
      return true as const
    }),
  )
  if (ok) triggerClientIpSaved()
}
```

Add `loadClientIp()` to the existing `onMounted` (after the maintenance init):

```ts
  await loadClientIp()
```

- [ ] **Step 3: Add the card to the template**

Insert after the maintenance `</Card>` (currently `:165`):

```html
    <Card>
      <CardHeader><CardTitle>{{ t('admin.settings.clientIpLabel') }}</CardTitle></CardHeader>
      <CardContent class="flex flex-col gap-3">
        <p class="text-xs text-muted">{{ t('admin.settings.clientIpHelp') }}</p>
        <div class="flex flex-col gap-1.5">
          <Label for="client-ip-strategy">{{ t('admin.settings.clientIpStrategyLabel') }}</Label>
          <Select :model-value="clientIpStrategy" @update:model-value="(v) => (clientIpStrategy = v as 'direct' | 'forwarded' | 'header')">
            <SelectTrigger id="client-ip-strategy" data-test="client-ip-strategy" :aria-label="t('admin.settings.clientIpStrategyLabel')">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="direct">{{ t('admin.settings.clientIpStrategyDirect') }}</SelectItem>
              <SelectItem value="forwarded">{{ t('admin.settings.clientIpStrategyForwarded') }}</SelectItem>
              <SelectItem value="header">{{ t('admin.settings.clientIpStrategyHeader') }}</SelectItem>
            </SelectContent>
          </Select>
        </div>
        <div v-if="clientIpStrategy === 'header'" class="flex flex-col gap-1.5">
          <Label for="client-ip-header">{{ t('admin.settings.clientIpHeaderLabel') }}</Label>
          <Input id="client-ip-header" v-model="clientIpHeader" :disabled="busy" data-test="client-ip-header" />
          <p class="text-xs text-muted">{{ t('admin.settings.clientIpHeaderHint') }}</p>
        </div>
        <div v-if="clientIpStrategy !== 'direct'" class="flex flex-col gap-1.5">
          <Label for="client-ip-trusted">{{ t('admin.settings.clientIpTrustedLabel') }}</Label>
          <Textarea id="client-ip-trusted" v-model="clientIpTrusted" :disabled="busy" rows="4" data-test="client-ip-trusted" />
          <p class="text-xs text-muted">{{ t('admin.settings.clientIpTrustedHint') }}</p>
          <p v-if="clientIpTrusted.trim() === ''" class="text-xs text-amber-700 dark:text-amber-400" data-test="client-ip-warning">
            {{ t('admin.settings.clientIpEmptyWarning') }}
          </p>
        </div>
        <div class="flex items-center gap-3">
          <Button :disabled="busy" data-test="save-client-ip" @click="saveClientIp">{{ t('admin.settings.clientIpSave') }}</Button>
          <span v-if="clientIpSavedFlag" class="text-sm text-muted" role="status">{{ t('admin.settings.saved') }}</span>
        </div>
      </CardContent>
    </Card>
```

- [ ] **Step 4: Add vitest coverage in `SettingsView.test.ts`**

Mirror the existing mocking idiom in that file (it already mocks `@/lib/api` and `@/lib/sudo`). Add cases:
1. Mount resolves `api.get` for `/client-ip` returning `{strategy:'header',header:'CF-Connecting-IP',trustedProxies:['203.0.113.0/24']}`; assert the header input and CIDR textarea render with those values.
2. Set strategy to `direct` and assert the header + CIDR blocks are hidden.
3. Set strategy `header` with an empty CIDR box and assert `[data-test="client-ip-warning"]` renders.
4. Click `[data-test="save-client-ip"]` and assert `api.put` was called with `/api/prohibitorum/admin/settings/client-ip` and a body whose `trustedProxies` is the split/trimmed array, wrapped in `withSudo`.

Follow the Reka test idioms (interact via `@update:model-value`/DOM events, not `setValue`) per the reka-primitive-idioms memory.

- [ ] **Step 5: Run FE checks**

Run: `cd dashboard && npx vitest run src/pages/admin/SettingsView.test.ts && npx vue-tsc -b 2>&1 | tail -20`
Expected: vitest green; `vue-tsc` 0 errors.

- [ ] **Step 6: Commit** (dist is rebuilt in Task 8)

```bash
git add dashboard/src/pages/admin/SettingsView.vue dashboard/src/pages/admin/SettingsView.test.ts dashboard/src/locales/en.ts dashboard/src/locales/zh.ts
git commit -m "feat(dashboard): client IP / proxy settings card"
```

---

### Task 8: Smoke coverage, full gate, and dist rebuild

**Goal:** Prove the end-to-end behavior with a live smoke block, run the full gate, rebuild + commit the embedded dashboard.

**Files:**
- Modify: `cmd/smoke/main.go`
- Modify: `pkg/webui/dist/**` (rebuilt)

**Acceptance Criteria:**
- [ ] Smoke sets `strategy=header, header=CF-Connecting-IP, trustedProxies=[127.0.0.1/32]`, then a request from loopback with `CF-Connecting-IP: 203.0.113.9` records/echoes `203.0.113.9`; a control (untrusted peer OR `strategy=direct`) records the peer, not the header.
- [ ] Full gate green: `go build -tags nodynamic ./...`, `go vet ./...`, `go test ./...`, `vitest`, `vue-tsc`, `check-contrast`, live smoke `SMOKE_EXIT=0`.
- [ ] `pkg/webui/dist` rebuilt and committed.

**Verify:** run the full gate below → all green, `SMOKE_EXIT=0`

**Steps:**

- [ ] **Step 1: Add a client-IP smoke block to `cmd/smoke/main.go`**

Find an existing admin-authenticated block that already holds a sudo grant + admin session (e.g. the maintenance or branding smoke block) and mirror its request idiom. The block must:
1. `PUT /api/prohibitorum/admin/settings/client-ip` with body `{"strategy":"header","header":"CF-Connecting-IP","trustedProxies":["127.0.0.1/32"]}` (expect 204).
2. `GET /api/prohibitorum/admin/settings/client-ip` and assert the JSON echoes the values.
3. Make a request that produces an audit/echo of the client IP (an action that logs `client_ip`) with header `CF-Connecting-IP: 203.0.113.9`; assert the recorded IP is `203.0.113.9` (peer is loopback ∈ trusted).
4. Reset with `PUT ... {"strategy":"direct"}` (expect 204) and assert a follow-up records the loopback peer, not `203.0.113.9`.

Match the smoke's existing assertion/logging helpers; gate the block behind the same section-print pattern the file uses (search for an existing `SMOKE:` / section header).

- [ ] **Step 2: Rebuild the dashboard dist**

Run: `cd dashboard && npm run build`
(Vite hashes are non-deterministic — the whole `dist` gets re-emitted; that's expected.)

- [ ] **Step 3: Run the full gate**

```bash
go build -tags nodynamic ./... && go vet ./... && go test ./... 2>&1 | tail -20
cd dashboard && npx vitest run && npx vue-tsc -b && node scripts/check-contrast.mjs && cd ..
```
Then run the live smoke per the project's smoke runbook (its own server + a throwaway `prohibitorum_smoke` DB, with `PROHIBITORUM_FEDERATION_ALLOW_PRIVATE_NETWORK=true` and the sudo-before-add-passkey ordering per the preship-smoke-gate memory). Expected: `SMOKE_EXIT=0`.

- [ ] **Step 4: Commit**

```bash
git add cmd/smoke/main.go pkg/webui/dist
git commit -m "test(smoke): client-IP resolution coverage; rebuild dashboard dist"
```

---

## Self-Review

**Spec coverage:**
- `pkg/clientip` Extract + peer-validation → Task 1. ✓
- Resolver + caching + `instance_settings` columns (migration 027) → Task 2. ✓
- Unify session/rate-limit/audit + delete `session.ClientIP` + remove `trust_proxy` + deprecation warning → Task 3. ✓
- OIDC + SAML audit RemoteAddr migration → Tasks 4, 5. ✓ (admin `auditBranding`/`entity_icon` sites → Task 6.)
- Admin GET/PUT API + validation + audit → Task 6. ✓
- Dashboard card + i18n (en+zh) → Task 7. ✓
- Tests (Extract table, resolver, handler, vitest) + smoke + gate + dist → Tasks 1,2,6,7,8. ✓
- Out-of-scope items (presets, auto-fetch, RFC 7239) → not planned. ✓

**Placeholder scan:** No TBD/TODO; every code step has real code; test bodies are concrete. The one deliberately open detail (smoke "action that logs client_ip") is scoped to "mirror an existing admin block" with explicit assertions — an execution detail, not a placeholder.

**Type consistency:** `Strategy`/`Config`/`Stored`/`Store`/`Resolver`/`ParseStored`/`NewResolver`/`NewPGStore` names match across Tasks 1–2 and their consumers (Tasks 3–6). The injected seam is uniformly `func(*http.Request) string` (`clientIPResolver.IP`) across middleware, OIDC (`New` final param), and SAML (`NewIdP` final param). JSON field names `strategy`/`header`/`trustedProxies` match between the Go `clientIPBody`, the resolver `Stored`, and the Vue payload.
