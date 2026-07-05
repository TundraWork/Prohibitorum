# Configurable client-IP resolution (CDN/proxy aware)

Date: 2026-07-05
Status: Design approved (pending spec review)

## Problem

Prohibitorum acquires the remote/user IP in two inconsistent ways:

1. `session.ClientIP(r, cfg.TrustProxy)` (`pkg/session/middleware.go:224`) — used by the
   session middleware, rate limiting, and most `pkg/server` audit records. When the static
   `trust_proxy` config is on, it takes the **leftmost** `X-Forwarded-For` value, else
   `X-Real-IP`, else the `RemoteAddr` host.
2. Raw `audit.ParseIPOrNil(r.RemoteAddr)` — used directly by the OIDC/SAML protocol handlers
   and a couple of admin handlers (e.g. `pkg/protocol/oidc/authorize.go:155`,
   `pkg/protocol/saml/sso.go:166`), **bypassing `ClientIP`**. These log the proxy's IP even
   when `trust_proxy` is on.

This has three defects:

- **No CDN support.** Cloudflare and similar CDNs deliver the real client IP in
  `CF-Connecting-IP` (or `True-Client-IP`), which nothing reads.
- **Spoofable.** Leftmost-`X-Forwarded-For` is client-controlled: a client sends
  `X-Forwarded-For: <evil>`, the proxy appends the real hop, and the leftmost (`<evil>`) wins.
  There is no check that the request actually arrived through a trusted proxy.
- **Inconsistent.** The proxy IP lands in protocol-flow audit logs while the client IP lands
  in session/rate-limit logs.

`trust_proxy` is compile/env-only, not operator-editable at runtime.

## Goals

- One spoof-resistant client-IP resolver used by **every** IP acquisition site.
- CDN/proxy awareness that covers Cloudflare and any reverse-proxy topology, configured from
  the dashboard Admin → Settings screen.
- Remove the static `trust_proxy` setting in favour of the dashboard config.

## Decisions (locked during brainstorming)

- **Trust model: peer-validated.** A forwarding header is trusted only when the direct TCP
  peer (`RemoteAddr`) is inside an admin-configured set of trusted proxy/CDN CIDRs. Empty
  CIDR list → headers are never trusted (fail-safe to the peer).
- **CIDR source: manual only.** No compiled-in provider presets and no auto-fetch. The admin
  pastes the trusted ranges. (A preset/auto-refresh could be a future task.)
- **`trust_proxy`: removed.** The DB config is the sole source of truth; default behaviour is
  `direct` (equivalent to today's `trust_proxy=false`). A startup warning is logged if the
  legacy key is still present.
- **Scope: all in one.** Full unification of every audit/rate-limit/middleware IP site in this
  feature — no half-migrated state.
- **CIDR input UX: textarea + on-save validation.** One CIDR per line; the PUT validates and
  returns a clear error. No inline client-side CIDR parsing.

## Architecture

### `pkg/clientip` (new, leaf package)

Self-contained; depends only on `net`/`net/http`. No dependency on `branding` or `session`,
so `session` may import it without a cycle.

```go
type Strategy string

const (
    Direct    Strategy = "direct"    // always RemoteAddr peer; ignore headers (default)
    Forwarded Strategy = "forwarded" // X-Forwarded-For chain
    Header    Strategy = "header"    // single named header (e.g. CF-Connecting-IP)
)

type Config struct {
    Strategy       Strategy
    Header         string       // for Header strategy, e.g. "CF-Connecting-IP"
    TrustedProxies []*net.IPNet // parsed CIDRs of whatever connects directly to us
}

// Extract returns the effective client IP (bare host, no port). Never panics; on any
// ambiguity it returns the direct peer host.
func Extract(r *http.Request, cfg Config) string
```

**`Extract` algorithm:**

1. Compute `peer` = host portion of `r.RemoteAddr` (strip port; unwrap IPv6 brackets).
2. `Direct` (and unknown/empty strategy) → return `peer`.
3. `Header`:
   - If `peer` is **not** inside `TrustedProxies` → return `peer` (untrusted source, ignore header).
   - Read `r.Header.Get(cfg.Header)`; if it parses to a valid IP → return it; else → `peer`.
4. `Forwarded`:
   - If `peer` is **not** inside `TrustedProxies` → return `peer`.
   - Parse `X-Forwarded-For` into entries; walk **right-to-left**, skipping entries whose IP is
     inside `TrustedProxies`; return the first entry that is a valid IP and not trusted.
   - If every entry is trusted (or the list is empty), return the leftmost valid entry, else `peer`.

Invalid/malformed entries are skipped. This right-to-left, skip-trusted walk is the standard
spoof-resistant algorithm; the old leftmost-first behaviour is intentionally removed.

**"Trusted proxies"** = the IP ranges of whatever connects *directly* to Prohibitorum:
Cloudflare's published ranges when CF→origin, `127.0.0.1/32` when a local nginx forwards, or
both when chained. This single concept covers every "popular CDN" without provider-specific code.

### `clientip.Resolver` (cached, mirrors `branding.Resolver`)

```go
type Store interface {
    Get(ctx context.Context) (Stored, error)               // raw strings from DB
    Set(ctx context.Context, s Stored) error
}
type Stored struct {
    Strategy       string
    Header         string
    TrustedProxies []string // raw CIDR strings
}

type Resolver struct { /* store + RWMutex cache of parsed Config */ }

func NewResolver(st Store) *Resolver
func (r *Resolver) IP(req *http.Request) string        // hot path; parses CIDRs once at load
func (r *Resolver) Config(ctx) Config                  // parsed, cached
func (r *Resolver) Set(ctx, Stored) error              // validates, writes, Invalidate()
func (r *Resolver) Invalidate()
```

CIDRs are parsed once when the row loads and cached; `IP` does no allocation-heavy parsing per
request. A read error behaves as `Direct` (fail-safe), matching `branding.load`.

### Persistence

Migration `db/migrations/027_client_ip_config.sql` adds three columns to the `instance_settings`
singleton:

```sql
ALTER TABLE instance_settings
    ADD COLUMN client_ip_strategy        text   NOT NULL DEFAULT 'direct',
    ADD COLUMN client_ip_header          text   NOT NULL DEFAULT '',
    ADD COLUMN client_ip_trusted_proxies text[] NOT NULL DEFAULT '{}';
```

`clientip.PGStore` reads/writes these columns on `WHERE id = 1`, alongside the existing
branding/maintenance columns.

## Unifying all IP acquisition

- **Delete** `session.ClientIP` and the `configx.Config.TrustProxy` field + its
  `viper.SetDefault("trust_proxy", false)`.
- **Session middleware:** `LoadSession(...)` gains the resolver's IP func (or the `*Resolver`);
  line 77 uses it instead of `ClientIP(r, cfg.TrustProxy)`.
- **`pkg/server`:** every `sessstore.ClientIP(r, s.config.TrustProxy)` → `s.clientIP.IP(r)`;
  every `audit.ParseIPOrNil(r.RemoteAddr)` → `audit.ParseIPOrNil(s.clientIP.IP(r))`.
- **OIDC / SAML:** inject a `func(*http.Request) string` seam (trivially stubbable in tests)
  into `oidc.New(...)` (`oidc.Provider`) and `saml.NewIdP(...)` (`saml.IdP`); replace their
  `r.RemoteAddr` audit sites. The plain-function seam avoids importing `clientip` into the
  protocol packages.
- **`trust_proxy` deprecation:** at startup, if the legacy `PROHIBITORUM_TRUST_PROXY` env or
  `trust_proxy` config key is present, log a warning: the setting is removed; configure
  Admin → Settings → Client IP. Default `direct` == old `trust_proxy=false`, so
  `trust_proxy=false` deployments are unaffected; `trust_proxy=true` deployments reconfigure
  via the dashboard (accepted breaking change).

The full set of call sites to migrate (from grep, excluding tests): `pkg/protocol/oidc/`
{logout, revoke, authorize×2, token}, `pkg/protocol/saml/`
{sso, assertion, slo, consent_saml, sso_init}, `pkg/server/`
{handle_admin_entity_icon, handle_admin_settings.auditBranding, handle_auth×N,
handle_auth_recovery, handle_pairing×N, handle_sudo×N, handle_me, handle_forward_auth_signout,
handle_auth_password×N, handle_enrollment×N}, and `pkg/session/middleware.go`. The exact list
is re-grepped during implementation to guarantee none are missed.

## Admin API

- `GET /api/prohibitorum/admin/settings/client-ip` (admin role; no sudo — a read).
  Response: `{ "strategy": "...", "header": "...", "trustedProxies": ["...", ...] }`.
- `PUT /api/prohibitorum/admin/settings/client-ip` (admin + fresh **sudo**, via
  `registerSudoOpHTTP`). Body identical shape. Validation:
  - `strategy` ∈ {`direct`, `forwarded`, `header`}.
  - `header`: required and header-name-charset-safe when `strategy == header`; ignored/empty otherwise.
  - each `trustedProxies` entry parses via `net.ParseCIDR`; list length ≤ 64.
  - Invalid input → `authn.ErrBadRequest()`.
  - On success: `Resolver.Set` (writes + invalidates), audit record
    (`FactorSigningKey` / `EventUpdate`, reason `client_ip_config_updated`), `204`.

Handlers live in `pkg/server/handle_admin_settings.go` next to the existing settings handlers;
routes registered in `server.go` alongside the other `admin/settings` routes.

## Dashboard

New **"Client IP / Proxy"** card in `dashboard/src/pages/admin/SettingsView.vue`, matching the
existing card idiom (mirror the maintenance card structure):

- Strategy `Select`: Direct connection / X-Forwarded-For / Custom header.
- Header `Input` (shown only for the header strategy) with common suggestions surfaced as help
  text or a datalist: `CF-Connecting-IP`, `True-Client-IP`, `X-Real-IP`, `Fastly-Client-IP`.
- Trusted-proxies `Textarea` (shown for non-`direct` strategies), one CIDR per line, with hint
  text explaining "ranges that connect directly to this server".
- Fail-safe warning when strategy ≠ direct and the CIDR list is empty: forwarding headers are
  ignored and the direct connection IP is used until ranges are added.
- Save `Button` wrapped in `withSudo`, `PUT`s the config; server-side validation errors surface
  via the existing `useApi().errorText` alert. Loads current values via the admin GET on mount.
- New `en.ts` + `zh.ts` i18n keys under `admin.settings.*`.

## Testing

- **`clientip.Extract` table tests** (security core): direct; header with trusted peer /
  untrusted peer / missing header / invalid header; forwarded right-to-left skip; spoof attempt
  (`X-Forwarded-For: <evil>, <realclient>` from a trusted peer → `<realclient>`); all-entries-trusted;
  malformed entries; IPv6 with brackets and ports; empty-CIDR fail-safe for both header/forwarded.
- **Resolver** cache/invalidate with a fake store; read-error → Direct.
- **Handler tests:** PUT validation (bad CIDR, bad strategy, header required for header strategy,
  oversize list), GET shape, sudo gate on PUT, audit record emitted.
- **Frontend vitest:** strategy switch shows/hides header + CIDR fields; save issues the PUT
  through sudo with the entered values; empty-CIDR warning renders.
- **Smoke:** PUT `{strategy:"header", header:"CF-Connecting-IP", trustedProxies:["127.0.0.1/32"]}`;
  send a request from loopback with `CF-Connecting-IP: 203.0.113.9` and assert the recorded/echoed
  IP is `203.0.113.9`; then a control with the peer outside the trusted range (or `strategy=direct`)
  and assert the header is ignored.

## Gate (Definition of Done)

`go build -tags nodynamic ./...`, `go vet ./...`, `go test ./...` all clean; `vitest` green;
`vue-tsc` 0; `check-contrast` unchanged; live smoke `SMOKE_EXIT=0` including the client-IP block;
`dist` rebuilt + committed.

## Out of scope

- Provider presets / auto-fetch of CDN ranges (possible future task).
- RFC 7239 `Forwarded:` header parsing (CDNs use `X-Forwarded-For` / `CF-Connecting-IP`).
- Per-route or per-listener IP policy (single instance-wide config).
```
