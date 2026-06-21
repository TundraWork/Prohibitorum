# App & Provider Icons — Design

Date: 2026-06-22
Status: Approved (brainstorming) — pending spec review

## Problem

Admins cannot set a per-app or per-provider icon. Downstream OIDC apps carry
only a remote `logo_uri` URL (never displayed, with SSRF/mixed-content/privacy
downsides if loaded); SAML SPs and forward-auth apps have no icon; upstream IdPs
have **no icon field at all**, so the `/login` "Sign in with…" buttons and the
`/connected` "Connect…" buttons render text-only.

This feature lets an admin **upload an icon for any app or provider**, stored and
served by the IdP itself (the project's established pattern for the instance icon
and account avatars), and displays those icons on the federation buttons today —
and is the prerequisite for icons on the **end-user app launchpad** (next cycle;
see "Sequencing" below).

## Goals

- One reusable icon capability across the three entity families that own an
  identity-bearing record: **OIDC apps**, **SAML apps** (`saml_sp`), and
  **upstream IdPs** (`upstream_idp`). (Forward-auth apps are `oidc_client` rows,
  so they are covered by the OIDC path.)
- Admin upload + remove per entity; public, cacheable serve; self-hosted (no
  remote-URL load).
- Display upstream-IdP icons on the `/login` "Sign in with…" and `/connected`
  "Connect…" buttons now; expose app icons on the admin views and the wire
  contracts so the launchpad (next cycle) and optionally the consent screen can
  show them.

## Non-goals

- The end-user launchpad itself (separate spec/cycle — see Sequencing).
- A crop UI: icons are uploaded and **center-cropped server-side** (mirrors the
  instance-icon `SettingsView`, not the avatar crop flow).
- Replacing `logo_uri`: it remains stored OIDC client metadata; it is simply not
  the display source.

## Established patterns reused (mirror, don't reinvent)

- **`branding.ProcessIcon(raw) → (png, etag, err)`** — decode → center-crop
  square → PNG 256² → sha256 etag, 5 MiB cap + decode validation. Reused verbatim
  for entity icons. (`pkg/branding/branding.go`.)
- **Instance-icon admin upload** (`pkg/server/handle_admin_settings.go`): raw
  image `PUT` via `registerOpHTTP(admin)` + **in-handler `requireFreshSudo`**
  (the sudo wrapper rejects non-JSON bodies and caps at 64 KiB, so icon uploads
  cannot use it); `DELETE` via `registerSudoOpHTTP` (admin + sudo).
- **Public image serve** (`/branding/icon`, `/avatar/{subject}`): `ETag` + `304`
  + `Cache-Control: public, max-age=300`.
- **`SettingsView` icon block** (preview chip + Upload/Remove via `withSudo`) —
  extracted into a reusable component for the four detail views.

---

## §1 — Storage + processing

New migration **`019_entity_icon.sql`**:

```sql
CREATE TABLE entity_icon (
  owner_kind text   NOT NULL,   -- 'oidc_client' | 'saml_sp' | 'upstream_idp'
  owner_id   text   NOT NULL,   -- client_id | saml_sp.id::text | upstream_idp.slug
  png        bytea  NOT NULL,
  etag       text   NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (owner_kind, owner_id)
);
```

One table (not three column-sets) → one storage path, one serve handler, one
upload handler, all parameterized by `(owner_kind, owner_id)`. A cross-table FK
is impossible (mixed PK types), so the **three entity-delete handlers**
(`DeleteOIDCClient`, the SAML delete, the upstream-IdP delete) also delete the
matching `entity_icon` row (a new `DeleteEntityIcon` call), preventing orphans.

`owner_id` uses each entity's stable key: OIDC/forward-auth = `client_id`; SAML =
`saml_sp.id` as text; upstream IdP = `slug`.

sqlc queries (`db/queries/entity_icon.sql`):
- `SetEntityIcon(owner_kind, owner_id, png, etag)` — upsert (`ON CONFLICT … DO UPDATE`).
- `GetEntityIcon(owner_kind, owner_id) → (png, etag)` — for the serve handler.
- `DeleteEntityIcon(owner_kind, owner_id)` — remove / clean up on owner delete.
- `ListEntityIconEtags(owner_kind) → [(owner_id, etag)]` — lets list endpoints
  (e.g. the federation list) attach `iconUrl` without N fetches.

Processing reuses `branding.ProcessIcon` (it is exported; called from the new
handlers). No new image code.

---

## §2 — Admin upload + remove

A new file `pkg/server/handle_admin_entity_icon.go` with a generic core
(`putEntityIcon(kind, id, w, r)` / `deleteEntityIcon(...)`) wrapped by three thin
per-entity route handlers so each can validate the owner exists (404 otherwise):

| Route | Auth | Behavior |
|---|---|---|
| `PUT /api/prohibitorum/oidc-applications/{clientId}/icon` | admin + in-handler fresh sudo | read ≤5 MiB → `ProcessIcon` → `SetEntityIcon('oidc_client', clientId, …)`; 404 if the client doesn't exist; audit. |
| `DELETE /api/prohibitorum/oidc-applications/{clientId}/icon` | admin + sudo (`registerSudoOpHTTP`) | `DeleteEntityIcon('oidc_client', clientId)`; audit. |
| `PUT/DELETE …/saml-applications/{id}/icon` | same | `owner_kind='saml_sp'`, `owner_id=id`. |
| `PUT/DELETE …/identity-providers/{slug}/icon` | same | `owner_kind='upstream_idp'`, `owner_id=slug`. |

Errors mirror the instance-icon handler: `branding.ErrTooLarge` →
`avatar_too_large`; decode failure → `avatar_invalid_image`. Audit uses the
existing factor for each entity (`FactorOIDCClient` / `FactorSAMLSP` /
`FactorUpstreamIdP`) with `EventUpdate` + a `reason: "icon_updated|icon_removed"`
detail (the instance-icon precedent).

**Frontend — `components/custom/EntityIconUpload.vue`** (extracted from the
`SettingsView` icon block): props `{ kind, id, iconUrl }`; renders the current
icon (or the `AppIcon` fallback) + Upload/Remove buttons via `withSudo`; emits
`changed` so the parent refetches. Added as a card/section to all four detail
views: `AdminOidcClientDetailView`, `AdminSamlProviderDetailView`,
`AdminForwardAuthAppDetailView`, `AdminUpstreamIdpDetailView`. The detail-view
data already loads via the existing GET; it gains an `iconUrl` field (§4).

---

## §3 — Public serve

`GET /icon/{kind}/{id}` (public; `pkg/server/handle_entity_icon.go`):

- Validate `kind ∈ {oidc_client, saml_sp, upstream_idp}` (else 400).
- `GetEntityIcon(kind, id)`; `pgx.ErrNoRows` → 404.
- `If-None-Match` matches the quoted etag → `304`; else `Content-Type: image/png`
  + `ETag` + `Cache-Control: public, max-age=300` + the PNG bytes.

Public because the `/login` page is unauthenticated. Icons are not sensitive
(IdPs are already listed publicly via `/auth/federation`), so enumeration is
acceptable. Registered in `server.go` next to `/branding/icon`.

---

## §4 — Display wiring + contracts

- **`contract.FederationProvider` gains `IconURL *string`** (`json:"iconUrl,omitempty"`):
  the cache-busted `/icon/upstream_idp/{slug}?v=<etag8>`, nil when no icon. The
  `GET /auth/federation` handler joins `ListEntityIconEtags('upstream_idp')` to
  populate it. `FederationButtons.vue` renders the icon before the label on the
  **"Sign in with…"** (`/login`) and **"Connect…"** (`/connected`) buttons.
- **Shared `components/custom/AppIcon.vue`** — given `{ src, name }`, shows the
  icon `<img>` when `src` is set, else an initial-letter-on-tint fallback (the
  `UserAvatar` fallback idiom). Used by `EntityIconUpload`, `FederationButtons`,
  and (next cycle) the launchpad tiles.
- **Admin app views gain `iconUrl`** (`OIDCApplicationView`, `ForwardAuthAppView`,
  `SAMLApplicationView`): the per-entity GET handlers attach
  `/icon/{kind}/{id}?v=<etag8>` (nil when none) so the detail view's
  `EntityIconUpload` shows the current icon and the launchpad (next cycle) and
  optionally the OIDC consent screen can display it. The list/detail GET handlers
  read the etag via `GetEntityIcon`/`ListEntityIconEtags`.

i18n: `entityIcon.*` (upload/remove/hint/alt) in **en + zh**. No new error codes
(reuse `avatar_too_large` / `avatar_invalid_image`).

---

## Security

- Upload is admin + fresh sudo; delete is admin + sudo. Serve is public,
  read-only, of admin-uploaded bytes that already passed `ProcessIcon`'s 5 MiB
  cap + decode validation — no new image-handling surface, no remote fetch (the
  SSRF/mixed-content risk of loading `logo_uri` is avoided entirely).
- `{kind}` is validated against the fixed allowlist; `{id}` is a lookup key only.
- `entity_icon` rows are removed with their owner, so a deleted app/IdP can't
  leave a servable icon.

## Testing & verification

- **Go:** `ProcessIcon` is already tested. Add: serve-handler tests (200 + ETag,
  304 on `If-None-Match`, 404 when absent, 400 on bad `kind`); view-projection
  tests that `iconUrl` is set/nil correctly; an owner-delete-cascades-icon test.
  Add the new sudo routes (`PUT`/`DELETE …/icon`) to `admin_route_policy_test.go`
  where applicable (the `DELETE`s are sudo-gated; the `PUT`s use in-handler sudo,
  matching the instance-icon precedent already in that test's accounting).
- **Frontend:** vitest for `EntityIconUpload` (upload/remove via mocked withSudo)
  and `FederationButtons` (renders `AppIcon` with icon vs fallback) and `AppIcon`.
- **Runtime (fully verifiable — `/login` is public):** upload an icon for a
  seeded upstream IdP via the admin API, then chromium-verify the `/login`
  "Sign in with…" button shows it; `curl /icon/upstream_idp/{slug}` returns the
  PNG + a `304` on re-request with `If-None-Match`.
- Full gate (`go vet`/`build -tags nodynamic`/`go test ./...`; FE `vue-tsc -b`,
  `npm run test`, `check-contrast.mjs`) + **dist rebuild + commit**. No
  `Co-Authored-By`. Work on `master`.

## Task order

1. Migration `019_entity_icon` + `entity_icon.sql` queries (sqlc regen).
2. Upload/delete handlers (generic core + 3 per-entity wrappers) + routes +
   owner-delete cleanup in the 3 delete handlers + audit.
3. Public serve `GET /icon/{kind}/{id}` + route.
4. `contract.FederationProvider.IconURL` + the `/auth/federation` join; admin app
   views gain `iconUrl`. `AppIcon.vue`.
5. `EntityIconUpload.vue` on the four detail views + en/zh i18n.
6. Wire `FederationButtons.vue` (login + connect) to render `AppIcon`.
7. Runtime-verify on `/login` + final gate + dist.

---

## Sequencing — what comes after (the launchpad, next cycle)

This is cycle 1 of two. The **end-user app launchpad** is the next spec/cycle and
will consume these icons. Its already-decided shape (from the same brainstorm):

- The launchpad becomes the **end-user home** (`/` → launchpad; today it redirects
  to `/security`) + an "Apps" sidebar item.
- It lists **all authorized + enabled launchable apps** (RBAC predicate reused):
  OIDC (launch via an optional admin **Launch URL**, defaulting to the first
  `redirect_uri`'s origin), forward-auth (`https://<host>/`), and SAML
  (`/saml/sso/init?sp=<entityId>`, only when `allow_idp_initiated`).
- **"Self-manage access" = revoke OIDC consent**: a `/me/consent` list + revoke
  (`oidc_consent` + the existing `DeleteConsent`), shown as an "Apps with access
  to your account" section (distinct from the upstream-link "Connected" page).
- New end-user endpoints: `GET /me/apps`, `GET /me/consent`,
  `POST /me/consent/revoke`; new query to list authorized apps for an account
  (no such query exists today); the launchpad tiles use this cycle's `AppIcon`.

These are recorded here so the launchpad cycle starts from the settled decisions;
they are **out of scope for this spec**.
