# Instance branding (name + icon) & dynamic page titles — Design

Date: 2026-06-20
Status: Approved (brainstorming) — pending spec review

## Problem

Two related gaps:

1. **No dynamic page titles.** `dashboard/index.html` has a static `<title>Prohibitorum</title>`; nothing sets `document.title` per route, so every page/tab reads "Prohibitorum" regardless of where the user is.

2. **The instance identity is hardcoded.** The product name "Prohibitorum" is hardcoded in two Vue components (`AppSidebar.vue` sidebar header, `CenteredLayout.vue` login card), the brand "icon" is a fixed lucide `ShieldCheck`, and the favicon (`<link rel="icon" href="/favicon.ico">`) points at a file that does not exist (404). An operator cannot rebrand the instance (e.g. to "Acme SSO") without editing source.

## Goals

- Each route sets a meaningful document title: **`<page> · <instance name>`** (just `<instance name>` when a route has no page name).
- An operator can set the **instance name** and an **icon** that customize both the **browser-tab favicon** and the **in-app brand mark** (sidebar + login card), on authenticated *and* unauthenticated pages.
- **Hybrid config model:** a deploy-time config default, overridable at runtime by an admin via the dashboard (stored in the DB).
- Sensible fallbacks: unset name → `"Prohibitorum"`; unset icon → the existing styled `ShieldCheck` in-app and a bundled default favicon.

## Non-goals

- The branding instance name is **purely cosmetic**. It does NOT change the WebAuthn `RPDisplayName`, the OIDC `Issuer`, or the TOTP issuer — those are security/stability-sensitive and remain config-only. (`branding.instance_name` MAY share the literal default "Prohibitorum" but is an independent value.)
- No per-route custom titles beyond the page name; no title template configurability.
- No multi-size / SVG / `.ico` favicon set — a single PNG serves both favicon and brand mark.
- No icon cropper UI this cycle — a simple file picker + server-side square-crop. (A cropper, like the avatar flow, can be added later.)
- No theming/color customization — name + icon only.

## Standards / precedent in this repo

- DB-backed image upload + processing + public serving already exists for **avatars** (`pkg/avatar` decode→square-crop→resize; `account_avatar`; public `GET /avatar/{subject}`; `PUT`/`DELETE /me/avatar`). The instance icon mirrors this, but encodes **PNG** (favicon compatibility) instead of WebP.
- Deploy-time config lives in `configx` (e.g. `PublicOrigins`, `OIDC.Issuer`, `WebAuthn.RPDisplayName`).
- Admin mutations go through `registerSudoOpHTTP` (admin role + fresh sudo); a cache-with-invalidation pattern is established for signing keys.

---

## Design

### A. Config + storage (the hybrid)

**Config defaults (`pkg/configx`):** a new `Branding` substruct:
- `branding.instance_name` (string, default `"Prohibitorum"`).
- `branding.icon_path` (string, optional; an absolute/relative path to a PNG/JPEG/WebP the operator drops at deploy. Empty = no config icon).

**DB override (migration `017_instance_settings.sql`):** a singleton `instance_settings` table — exactly one row, enforced by a `CHECK (id = 1)` single-row constraint (insert id=1 in the migration with NULLs):

```
instance_settings(
  id             smallint PRIMARY KEY DEFAULT 1 CHECK (id = 1),
  instance_name  text NULL,          -- NULL = fall back to config/default
  icon_png       bytea NULL,         -- NULL = fall back to config-file/built-in
  icon_etag      text NULL,
  updated_at     timestamptz NOT NULL DEFAULT now()
)
```
The migration seeds `(1, NULL, NULL, NULL, now())`.

**Effective resolution** — a `pkg/branding` resolver (the single source of truth):
- `InstanceName()` = DB.instance_name (if non-NULL/non-empty) ELSE config.branding.instance_name ELSE `"Prohibitorum"`.
- icon: DB.icon_png (if present) ELSE config.icon_path file (if set & readable) ELSE built-in default PNG (bundled asset). `HasCustomIcon()` is true only when DB or config provides one (NOT for the built-in default).
- The resolver caches the loaded DB row + processed config icon; admin mutations call `Invalidate()` so changes apply immediately (mirrors the signing-key cache).

### B. Icon pipeline + serving

- **Processing:** add `branding.ProcessIcon(raw []byte) (png []byte, etag string, err error)` reusing avatar's decode (`image.Decode` — jpeg/png/webp registered) + `cropSquare` + resize, but encoding **PNG at 256×256** via stdlib `image/png` (no WASM/cgo). Refactor the shared crop/resize out of `pkg/avatar` into a small internal helper if cheap; otherwise duplicate the ~15 lines (avatar stays WebP). 5 MiB upload cap + decode-config dimension guard, matching the avatar hardening.
- **Bundled default:** commit a default icon PNG (a ShieldCheck-on-canvas render, Ember tint) at `pkg/branding/default_icon.png`, `go:embed`-ed, so `/branding/icon` always returns a valid image.
- **Public serve — `GET /branding/icon`:** returns the effective icon PNG with `Content-Type: image/png`, `ETag: "<icon_etag-or-default>"`, `Cache-Control: public, max-age=300` (short, so a rebrand propagates fast). Honors `If-None-Match` → 304. No auth (it's public branding, like the favicon).

### C. Public delivery + index.html

- **Public config — `GET /api/prohibitorum/config`** (no auth) → `{ instanceName: string, hasCustomIcon: bool, iconUrl: "/branding/icon", iconEtag: string }`. (`iconEtag` lets the SPA cache-bust the brand-mark `<img>` after an admin change.)
- **SPA branding store** (`stores/branding.ts`, pinia): `load()` fetches `/config` once; `App.vue` calls it on boot (alongside `useTheme`/`useLocale`). Exposes `instanceName` + `hasCustomIcon` + `iconSrc` (= `/branding/icon?v=<etag>`). All brand surfaces read from it.
- **`index.html`:**
  - `<link rel="icon" href="/branding/icon">` — static href; immediately correct (custom-or-default), no templating.
  - `<title>` is templated **server-side from the config default** instance name by the webui handler. The handler (`pkg/webui`) gains a small dependency: it receives the config instance name at construction and string-replaces a `__INSTANCE_NAME__` placeholder in `index.html` (kept decoupled from the DB — a DB override only briefly shows the config name pre-boot, then the SPA corrects it). Existing `webui.Handler()` call site in `server.go` is updated to pass the name.

### D. Dynamic page titles

- Each route record gets `meta.titleKey` (an i18n key, e.g. `'title.security'`). New `title.*` keys in `en.ts` + `zh.ts` for every route (login/consent/logout/error/enroll/pair/welcome + dashboard + admin pages).
- A router `afterEach((to) => { ... })` (installed in `router/index.ts`) sets:
  `document.title = key ? \`${t(key)} · ${instanceName}\` : instanceName`
  where `instanceName` comes from the branding store (falls back to the config-templated value already in the tag until the store loads). Lives in a tiny tested unit `lib/pageTitle.ts` (`buildTitle(pageName, instanceName)`), called by the guard.

### E. Admin Settings page (the DB override)

- **Route `/admin/settings`** (`pages/admin/SettingsView.vue`, `requiresAdmin`), new **Settings** item in the Admin sidebar group (`Settings` lucide icon).
- **Backend (mirrors avatar handlers):**
  - `GET /api/prohibitorum/admin/settings` (admin role) → `{ instanceName, instanceNameSource: 'db'|'config'|'default', hasCustomIcon, iconEtag }`.
  - `PUT /api/prohibitorum/admin/settings` (admin + sudo) `{ instanceName: string }` → upsert DB row (empty string → set NULL = revert to config/default). Validates length (e.g. 1–64).
  - `PUT /api/prohibitorum/admin/settings/icon` (admin + sudo) — raw image body (like `PUT /me/avatar`) → `ProcessIcon` → store PNG + etag.
  - `DELETE /api/prohibitorum/admin/settings/icon` (admin + sudo) → NULL the icon (revert to config/default).
  - Each mutation records an audit row (factor `credential_event`-style, reason `instance_branding_updated`) and calls `branding.Invalidate()`.
- **SettingsView UI:** an instance-name field (text, save → PUT, transient "Saved"); an icon section showing the current effective icon, a file picker → upload (PUT), and Remove (DELETE, shown only when a custom icon exists). Sudo handled by `withSudo` on the mutations.

---

## Data flow

**Boot / any page:** `App.vue` → `branding.load()` (`GET /config`) → store holds name + icon flag → sidebar/login mark + every brand surface render from it; router `afterEach` sets `document.title = <page> · <name>`. Favicon loads from `/branding/icon` independently.

**Admin change:** SettingsView → `withSudo(PUT /admin/settings[/icon])` → backend upserts DB row + audits + `branding.Invalidate()` → SettingsView re-fetches; other clients pick it up within the 300s icon cache / on their next `/config` load.

**Icon request:** browser/SPA → `GET /branding/icon` → resolver returns DB PNG | config-file PNG | built-in default PNG, with ETag → 304 on revisit.

---

## Components / interfaces

| Unit | Responsibility | Interface |
|---|---|---|
| `pkg/configx` Branding | deploy-time defaults | `InstanceName string`, `IconPath string` |
| migration `017` | singleton DB override row | `instance_settings` |
| `pkg/branding` resolver | effective name/icon + cache | `InstanceName()`, `Icon() (png,etag)`, `HasCustomIcon()`, `Invalidate()`, `ProcessIcon()` |
| `GET /branding/icon` | public icon serve | PNG + ETag/304 |
| `GET /api/prohibitorum/config` | public branding for SPA | `{instanceName,hasCustomIcon,iconUrl,iconEtag}` |
| admin settings handlers | DB override CRUD (admin+sudo) | GET/PUT/PUT-icon/DELETE-icon |
| `pkg/webui` handler | template `<title>` from config name | constructed with the config instance name |
| `stores/branding.ts` | SPA branding state | `load()`, `instanceName`, `hasCustomIcon`, `iconSrc` |
| `lib/pageTitle.ts` | title string builder | `buildTitle(pageName, instanceName)` |
| router `afterEach` | set document.title per route | reads `meta.titleKey` + store |
| brand-mark components | render name + ShieldCheck/`<img>` | `AppSidebar.vue`, `CenteredLayout.vue` |
| `SettingsView.vue` | admin edit name + icon | `/admin/settings` |

## Testing

**Go:**
- `branding` resolver precedence (DB→config→default) for name and icon; `HasCustomIcon` correctness; `Invalidate` refresh.
- `ProcessIcon`: PNG output, square 256×256, rejects oversize/garbage.
- `GET /branding/icon`: custom vs default, `Content-Type image/png`, ETag + 304 on `If-None-Match`.
- `GET /config`: shape + reflects DB override.
- admin settings: GET/PUT/PUT-icon/DELETE-icon happy paths + **sudo gating** (covered by `TestAdminMutationRoutesRequireSudo`) + name length validation + empty-name-reverts-to-config.
- `webui` handler: `<title>` contains the configured instance name (placeholder replaced).

**Frontend (vitest):**
- branding store: `load()` populates state; brand mark renders `ShieldCheck` when `hasCustomIcon=false`, `<img :src=iconSrc>` when true; name text reflects the store.
- `lib/pageTitle.buildTitle`: `<page> · <instance>`, and `<instance>` alone when no page.
- router title guard sets `document.title` on navigation (unit test the guard with a fake route + store).
- SettingsView: name save (PUT + Saved flag), icon upload (PUT), remove (DELETE shown only when custom), sudo path.
- i18n parity/compile for all new `title.*` + `admin.settings.*` keys (en + zh).

**Runtime (chromium via a subagent-launched server, per the env note that the harness kills my own Bash servers):**
- tab title differs per page (`Security · Prohibitorum`, `Sign in · Prohibitorum`); `/branding/icon` returns a PNG; set a custom name + icon via the API and confirm the sidebar/login text + favicon + brand `<img>` update.

## Risks

- **webui ↔ config coupling for the title placeholder.** Mitigated by injecting only the config string at construction (no DB dependency in `pkg/webui`); the SPA owns the dynamic/DB-override title.
- **Favicon caching staleness** after a rebrand. Mitigated by a short `max-age=300` + ETag; the in-app `<img>` cache-busts via `?v=<etag>`.
- **Singleton-row contention.** Trivial (one row, admin-only writes); upsert on id=1.
- **Reusing avatar crop/resize.** If extracting a shared helper is awkward, duplicate the ~15 lines into `pkg/branding` rather than entangle the two packages — keep avatar WebP and branding PNG independent.
