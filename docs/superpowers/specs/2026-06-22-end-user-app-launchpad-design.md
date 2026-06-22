# End-user app launchpad — design

> Launch authorized apps from a "My apps" home, and self-manage the apps that
> have access to your account.

Status: approved (brainstorm 2026-06-22). Implements the README TODO
*"End-user app launchpad — launch authorized apps and self-manage access."*
This is **cycle 2 of two**; cycle 1 (app & provider icons,
`2026-06-22-app-provider-icons-design.md`) shipped the icons the tiles consume
and recorded the launchpad's settled shape in its *Sequencing* section.

## Context & motivation

Today an end user signs in and lands on `/security`; there is no place that
answers *"which apps may I open, and which apps can see my account?"* The
authorization data already exists — the RBAC feature
(`2026-06-16-rbac-design.md`) added a per-account "may this account access this
app?" predicate, and OIDC consent records what a user has explicitly approved —
but nothing surfaces it to the end user. Mature IdP portals (Microsoft *My
Apps*, Okta dashboard, Cloudflare Access App Launcher, Keycloak Account Console,
Authentik) all converge on the same shape: a **policy-generated grid of app
tiles** that shows only the apps the signed-in user is authorized for, launched
single-click via SSO, plus a place to **review and revoke** the apps that have
access. Cloudflare frames the launcher as an **anti-phishing trusted launch
point** — one known origin to start from rather than hand-typed URLs.

This design adds that surface for Prohibitorum, scaled to a small, single-tenant,
first-party IdP (so the enterprise-scale features — user collections, favorites,
recently-used, self-service request-access, bookmarks, browser plugins — are
deliberately out of scope; see *Non-goals*).

## Decisions (settled in the brainstorm)

1. **Launch URL for OIDC apps:** an optional admin-editable `launch_url` column,
   falling back to the **origin of the first `redirect_uri`** when unset. Apps
   with no resolvable target are simply not launchable.
2. **Revoke is not sudo-gated.** Revoking app access (deleting an OIDC consent)
   is low-risk and self-correcting — the next sign-in re-prompts for consent.
3. **The launchpad is the end-user home.** A new minimal **`LauncherLayout`**
   hosts `/` (the "My apps" page) and becomes the post-login landing; the
   existing sidebar shell becomes the **settings/admin** area, reached from the
   launcher's avatar menu. (Today `/` → `/security`.)
4. **Self-manage access lives in both places.** The launcher expresses consent
   state *on the tile* (a subtle glyph + a kebab "Revoke"), and a concise
   **"App access"** settings page is the full management list.
5. **Naming:** the homepage gets a natural, human label — **"My apps"** (with a
   *"Welcome back, {firstName}"* greeting). Settings/admin pages keep their
   existing concise style (Security / Sessions / Connected / Devices / **App
   access**). "Launchpad" stays an internal/feature/route name only.

## Information architecture

```
LauncherLayout  (new; minimal chrome: brand left, avatar menu right)
  /                         → "My apps" (the launchpad; post-login landing)
                              avatar menu → Settings, Admin (if admin), Sign out

DashboardLayout (existing sidebar shell; brand/logo → "/" home)
  /security  /sessions  /connected  /devices   (Account/Settings group)
  /app-access                                   (new — "App access")
  /admin/*                                      (Admin group)
```

- Root redirect changes from `/` → `/security` to `/` rendering the launchpad.
- Existing settings routes keep their paths (no URL churn); only the default
  landing and the chrome around the launchpad change.
- The router's post-login `return_to` default becomes `/`.

## "My apps" — the launchpad page

A responsive grid of uniform tiles. The page lists **only authorized + enabled +
launchable** apps for the signed-in account.

**What's listed (three sources, one unified list):**
- **OIDC clients** (non-forward-auth) that resolve a launch URL.
- **Forward-auth apps** (OIDC rows with `forward_auth_enabled`) → `https://<forward_auth_host>/`.
- **SAML SPs** with `allow_idp_initiated = true` → `/saml/sso/init?sp=<entity_id>`.

For every source: `disabled = false`, and the **RBAC grant predicate** holds
(open app → allow; `access_restricted` → direct account grant OR via-group
membership). This reuses the exact predicate the protocol endpoints enforce, so
the launchpad can never show an app the user would be denied at launch.

**Launch-URL resolution (OIDC):** `launch_url` if set → else origin of
`redirect_uris[0]` → else not launchable (omitted). Forward-auth and SAML have
intrinsic targets (host / sso-init) and ignore `launch_url`.

**The tile (the "express consent + launch" unit):**
- `AppIcon` (cycle-1 component) + display name + a small **type chip** that
  labels the app's protocol — **OIDC**, **SAML**, or **Forward-auth** — driven by
  the item's `kind` (`oidc` / `saml` / `forward_auth`). Every tile shows its
  type so the user can tell the apps apart (and it explains why each launches
  differently). The chip uses the same category names as the admin UI ("OIDC
  applications", "SAML applications", "Forward-auth apps") for consistency.
- **Click anywhere → launch in a new tab** (`target="_blank" rel="noopener"`),
  so the launcher persists. (SSO means the app round-trips back to the IdP and
  returns authenticated; no credentials re-entered.)
- **Consent glyph:** a subtle indicator shown **only** when the user has an
  explicit consent record for that app (i.e. `require_consent` clients).
- **Kebab (⋯) menu** (keyboard-reachable):
  - **Details** — popover: display name, type, and *what the app receives*
    (OIDC: granted scopes; SAML: configured attributes), plus the consent date
    when present.
  - **Revoke access** — present **only** when a consent record exists; one click
    → `POST /me/consent/revoke`. (Trusted apps have no consent to revoke; their
    kebab shows Details only.)
- **Empty state** as an onboarding moment, not a dead end: a friendly card —
  *"No apps yet. When an admin grants you access to an app, it'll show up here."*

**Why the consent glyph is often absent:** consent is recorded only for clients
with `require_consent = true` (trusted/first-party clients skip the consent step
entirely — `pkg/protocol/oidc/authorize.go`). So for most first-party apps the
tile is a pure launcher with a Details-only kebab; the glyph/Revoke appear for
the apps a user actually approved. This is correct: the launchpad never implies a
revoke that wouldn't do anything.

**Deferred UX (YAGNI for a handful of first-party apps):** search/filter (Okta
only surfaces it at ~80+ apps), user collections/sections, favorites,
recently-used, self-service request-access + approval, user bookmarks /
"add-a-site", auto-launch. All are easy to add later if app counts grow.

## "App access" — the settings page

The canonical management view that the tile kebab mirrors. Lists the apps the
user has **granted access to** (consent records): `AppIcon` + name + granted
scopes + granted date + **Revoke**. Concise empty state: *"Apps you've approved
will appear here."* Sits in the settings sidebar next to "Connected" (which
remains upstream **identity providers** — left unchanged).

## Backend

### Data model
- **Migration** (next number in `db/migrations/`): `ALTER TABLE oidc_client ADD
  COLUMN launch_url text;` (nullable; no backfill — resolution falls back to the
  redirect-uri origin at read time).

### Queries (`db/queries/`, sqlc)
- `ListAuthorizedOIDCClientsForAccount(account_id)` — enabled, **not**
  forward-auth, RBAC-authorized; returns columns needed to resolve the launch
  URL (`client_id, display_name, launch_url, redirect_uris, access_restricted`).
- `ListAuthorizedForwardAuthAppsForAccount(account_id)` — enabled,
  `forward_auth_enabled`, RBAC-authorized; returns `client_id, display_name,
  forward_auth_host`.
- `ListAuthorizedSAMLSPsForAccount(account_id)` — enabled, `allow_idp_initiated`,
  RBAC-authorized; returns `id, entity_id, display_name`.
- `ListConsentsByAccount(account_id)` — join `oidc_client` for display name;
  returns `client_id, display_name, granted_scopes, updated_at`.

Each "authorized" query embeds the same `NOT access_restricted OR <direct grant>
OR <via-group grant>` predicate used by `IsAccountAuthorizedFor{OIDCClient,
SAMLSP}` so behavior stays identical to launch-time enforcement.

### Endpoints (session-gated `/me`, registered in `pkg/server/server.go`)
- `GET  /api/prohibitorum/me/apps` → unified launchable list. Each item:
  `{ kind: "oidc"|"forward_auth"|"saml", id, name, iconUrl, launchUrl,
  hasConsent }`. The handler resolves launch URLs and icon URLs
  (`entityIconURL`), merges the three sources, and sorts by name.
- `GET  /api/prohibitorum/me/consent` → `[{ clientId, name, iconUrl, scopes,
  grantedAt }]` from `ListConsentsByAccount`.
- `POST /api/prohibitorum/me/consent/revoke` `{ clientId }` → `DeleteConsent`
  (account from session; 204/empty on success; idempotent).

### Contracts (`pkg/contract/`)
- `LaunchpadApp`, `LaunchpadAppsView` (the `/me/apps` response).
- `ConsentedApp`, `ConsentListView` (the `/me/consent` response).
- `RevokeConsentInput` (`{ clientId }`).
- Follow the existing `/me` DTO + operation-registration pattern
  (`pkg/contract/auth.go`, `handle_me*.go`).

### Handlers (`pkg/server/`)
- New `handle_me_apps.go` (launchpad + consent reads) and the revoke handler
  (co-located or `handle_me_consent.go`). Reuse the session-account accessor and
  `s.config.PublicOrigins[0]` for absolute SAML sso-init URLs.

## Admin

- Add an optional **Launch URL** field to the OIDC application update path:
  contract field, admin handler, and the admin OIDC detail form
  (`dashboard/src/pages/...OIDC...`), with placeholder text showing the derived
  default (origin of the first redirect URI). Forward-auth and SAML need no admin
  change (intrinsic targets).

## Frontend (`dashboard/`)

- **`LauncherLayout.vue`** — minimal chrome (instance brand from the branding
  store; avatar menu reusing the existing account-dropdown with Settings / Admin
  / Sign out). Logo → `/`.
- **`MyAppsView.vue`** — greeting + responsive tile grid + empty state; fetches
  `GET /me/apps`.
- **`AppTile.vue`** — `AppIcon` + name + type chip; click-to-launch (new tab);
  consent glyph; kebab → Details popover / Revoke.
- **`AppAccessView.vue`** — settings list from `GET /me/consent` + revoke;
  added to the settings sidebar group.
- **Router** (`dashboard/src/router/index.ts`): `/` → `MyAppsView` under
  `LauncherLayout`; `/app-access` under the sidebar shell; default landing/return
  → `/`; guard unchanged (`requiresAuth`).
- **API module** for launchpad/consent calls; small Pinia store if state needs
  sharing (otherwise per-view fetch, matching existing pages).
- **i18n** (`locales/en.ts` + `zh.ts`): `nav.*`, `title.*`, the "My apps"
  greeting, the **type-chip labels** (OIDC / SAML / Forward-auth), tile menu
  labels, App-access strings, empty states — kept at parity
  (`locales.parity.test.ts`).

## Security considerations

- The launchpad is **discovery only**; every launch still hits the protocol
  endpoint, which independently enforces the RBAC predicate (OIDC authorize,
  SAML sso-init) and session validity. The launchpad reusing the same predicate
  is a UX nicety, not the security boundary.
- New tabs use `rel="noopener"` to prevent reverse-tabnabbing of the launcher.
- Revoke deletes only the **caller's own** consent (account from session), is
  idempotent, and is intentionally not sudo-gated (re-consent on next sign-in).
- No new data is exposed: scopes/attributes shown in Details are what the app
  already receives.

## Testing & gate

- **Go (unit):** the three `ListAuthorized…ForAccount` queries across the matrix
  (open app → listed; restricted + direct grant → listed; restricted + via-group
  → listed; restricted + non-member → omitted; `disabled` → omitted; OIDC with no
  launch URL → omitted; SAML without `allow_idp_initiated` → omitted).
  `ListConsentsByAccount`. Handler tests for `/me/apps`, `/me/consent`, and
  revoke (incl. idempotent re-revoke and cross-account isolation).
- **Smoke (`cmd/smoke`):** a new `launchpad` arc — grant an account access to an
  app → `GET /me/apps` includes it (and a denied app does not) → drive a
  `require_consent` authorize to record consent → `GET /me/consent` shows it →
  `POST /me/consent/revoke` → `GET /me/consent` empty. Numbered in the existing
  per-arc local-count style (`launchpad N/M`).
- **Frontend (vitest + vue-tsc):** `AppTile` (launch link target, consent glyph
  presence/absence, kebab Details/Revoke), `MyAppsView` empty state,
  `AppAccessView` list + revoke; en/zh parity.
- **Green gate:** `go build -tags nodynamic ./... && go vet ./... && go test
  ./...`; `cd dashboard && npm test` + `npm run build`; live smoke
  `SMOKE_EXIT=0`; rebuild + commit `pkg/webui/dist`.

## Non-goals (explicit)

- Per-app **token/session** revocation (only OIDC consent revoke ships; the
  protocol's existing token revocation/refresh-family logic is unchanged).
- User-created collections/sections, favorites, recently-used, search/filter.
- Self-service **request access** + approval workflows.
- User-added bookmarks / "add a site"; auto-launch; browser extensions.
- Renaming the existing "Connected" (upstream identity) page.
