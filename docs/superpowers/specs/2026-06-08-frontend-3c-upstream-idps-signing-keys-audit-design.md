# Spec — Frontend 3c: Upstream IdPs + Signing keys + Audit log

**Date:** 2026-06-08
**Branch:** `master` (commit directly; no remote, no worktree).
**Status:** approved design, ready for plan.

The final admin slice of the frontend rebuild. Spec 3a shipped the admin shell +
Accounts + Invitations; the combined cycle shipped RP management (OIDC clients +
SAML SPs) + account recovery + federated invites. **3c closes the last three admin
areas — all backend-ready — so the rebuilt dashboard reaches admin parity with the
backend.** After this, the only remaining gaps are the deliberately deferred
backend-heavy features (D: OTP/password invitations; E: SAML-as-login).

**Frontend-only — NO Go changes.** All three backends are complete (Admin Management
API, commits `ce2fdf4`..`459c505`). New views are `requiresAdmin` children of the
existing `DashboardLayout`; new nav items extend `adminItems` in `AppSidebar.vue`.

Contracts below are verified against `pkg/contract/auth.go` + the handlers
(`handle_admin_upstream_idps.go`, `handle_admin_signing_keys.go`,
`handle_admin_audit.go`, `operations.go`). **`api.md` is STALE — do not trust it.**

---

## Cross-cutting (applies to all three areas)

- **Routes:** three `requiresAdmin` children of `DashboardLayout`:
  `/admin/upstream-idps` (+ `/admin/upstream-idps/:slug` detail), `/admin/signing-keys`,
  `/admin/audit`. Extend the guard regression test to cover them.
- **Sidebar:** append to `adminItems` in `AppSidebar.vue` (after SAML providers):
  Upstream IdPs · Signing keys · Audit log. Icons from `lucide-vue-next`
  (`Network`, `KeySquare`, `ScrollText` — pick what reads cleanly; not the brand `ShieldCheck`).
- **Reads** (admin role, no sudo): `useApi()` + `api.get` + vendored `Table` + `StatusBadge`.
- **Mutations** (admin + fresh sudo): `withSudo(() => api.post/put(...))` — retry-once on
  `sudo_required`; `SudoModal` is already mounted once in `DashboardLayout`. No new sudo plumbing.
- **Destructive / state-changing:** `ConfirmDialog` with itemized consequences.
- **Errors:** `errors.<code>` i18n → `Alert variant="destructive"`; fall back to
  `e.message` then `common.error` (the established `errorText` computed). New codes:
  `upstream_idp_not_found`, `upstream_idp_already_exists`, `active_key_no_replacement`.
  (`credential_not_found` already mapped; reused for signing-key not-found.)
- **i18n:** new namespaces `admin.upstream.*`, `admin.signingKeys.*`, `admin.audit.*`,
  plus `admin.nav.*` labels. English-first. **After EVERY en.ts edit run the apostrophe
  guard** `grep -nP "\x{2018}" dashboard/src/locales/en.ts` and
  `grep -nP ":\s*\x{2019}" dashboard/src/locales/en.ts` (see project memory: the Edit
  tool corrupts en.ts delimiters).
- **No new ui/ primitives needed** — Table, Card, Input, Label, Textarea, Button, Badge,
  Alert, Dialog are all already vendored. (Audit datetime filters use native
  `<input type="datetime-local">`, consistent with the existing checkbox/native-select usage.)

---

## Section A — Upstream IdPs (federation config CRUD)

List + detail, mirroring the OIDC-clients pattern (`AdminOidcClientsView` +
`AdminOidcClientDetailView`).

**Contract** (all `/api/prohibitorum/upstream-idps`):
- `GET` → `UpstreamIDPView[]`; `GET /{slug}` → one (404 `upstream_idp_not_found`).
- `POST` (sudo): `{slug, displayName, issuerUrl, clientId, clientSecret, mode,
  scopes?, allowedDomains?, usernameClaim?, displayNameClaim?, emailClaim?,
  requireVerifiedEmail?}` → 201 `UpstreamIDPView`. 409 `upstream_idp_already_exists`.
- `PUT /{slug}` (sudo): same fields **minus `clientSecret`**, **plus `disabled`** → 200 view.
- `POST /rotate-secret` (sudo): `{slug, clientSecret}` → **204** (no reveal — server
  returns nothing).
- `POST /delete` (sudo): `{slug}` → 204.
- `UpstreamIDPView` = `{slug, displayName, issuerUrl, clientId, scopes[], mode,
  allowedDomains[], usernameClaim, displayNameClaim, emailClaim, requireVerifiedEmail,
  disabled, createdAt}`. **No secret field is ever returned.**
- `mode` ∈ `{auto_provision, invite_only, link_only}` (constants in
  `pkg/federation/oidc/modes.go`).

**`AdminUpstreamIdpsView`** (`/admin/upstream-idps`): `Table` (Display name + slug ·
mode badge · status active/disabled). Inline **create** card (sudo `POST`):
- Required: `slug`, `displayName`, `issuerUrl`, `clientId`, **`clientSecret`**
  (write-only — `type="password"`/`autocomplete="off"`, never echoed back), `mode`
  (native `<select>` of the three modes).
- Claim/advanced fields with defaults pre-filled: `scopes` (textarea, newline-split,
  default `openid`/`email`/`profile`), `usernameClaim` (`preferred_username`),
  `displayNameClaim` (`name`), `emailClaim` (`email`), `allowedDomains` (textarea,
  newline-split, optional), `requireVerifiedEmail` (checkbox).
- `upstream_idp_already_exists` → inline Alert; form stays open (matches OIDC dup handling).

**`AdminUpstreamIdpDetailView`** (`/admin/upstream-idps/:slug`): `GET /{slug}` with
not-found + load-error states (model on `AdminSamlProviderDetailView`). **Edit** = all
view fields incl. `disabled` via `PUT` (no `clientSecret`). **Rotate secret** = a small
write-only input + button → `POST /rotate-secret` (204; success status text, no reveal).
**Delete** via `ConfirmDialog` → `POST /delete`. A muted note states these providers
power the `/login` + `/connected` provider lists.

## Section B — Signing keys (lifecycle)

Single list view, **no detail page** — the resource is small and action-oriented.

**Contract** (all `/api/prohibitorum/signing-keys`):
- `GET` → `SigningKeyView[]`.
- `POST /generate` (sudo, **no body**) → 201 new `pending` `SigningKeyView`.
- `POST /{kid}/activate` (sudo) → 200 view (now `active`; prior active →
  `decommissioning`). 404 `credential_not_found`.
- `POST /{kid}/retire` (sudo) → 200 view (`decommissioning` → `retired`). 404
  `credential_not_found`; **409 `active_key_no_replacement`** if retiring the active key
  with no active replacement.
- `SigningKeyView` = `{kid, algorithm, use, status, publicJwk,
  notBefore?, activatedAt?, decommissionedAt?, retireAfter?}`. `status` ∈
  `{pending, active, decommissioning, retired}`. **`privatePem` is never serialized.**

**`AdminSigningKeysView`** (`/admin/signing-keys`): `Table` — kid (truncate) · algorithm ·
use · **status badge** (`pending`→neutral, `active`→success, `decommissioning`→caution,
`retired`→neutral; `StatusBadge` has only neutral/success/caution/danger) · relevant
timestamp(s) via `lib/time`. `publicJwk` in an
expandable `<pre>` per row (pretty JSON).

Actions — all sudo-gated, each behind `ConfirmDialog` with consequence text, availability
gated by row `status`:
- **Generate** (toolbar, always available): `POST /generate`. Consequence note: published
  immediately (appears in JWKS / SAML metadata) but does **not** sign until activated.
- **Activate** (only on `pending` rows): consequences spell out "the current active key
  is demoted to decommissioning."
- **Retire** (only on `decommissioning` rows): handles `active_key_no_replacement` (409)
  with a clear inline message. (Active keys are not directly retirable from the UI; this
  is a backend invariant we surface, not bypass.)

## Section C — Audit log (read-only, filterable, keyset-paginated)

**`AdminAuditView`** (`/admin/audit`): **load-more + expandable detail**.

**Contract** `GET /api/prohibitorum/audit-events` (admin role, **no sudo**):
- Query (all optional): `factor`, `event`, `accountId` (int32), `since` (RFC3339),
  `until` (RFC3339), `before` (int64 keyset cursor — return rows with `id < before`),
  `limit` (int32; default 50, clamped 1..200).
- Response: `AuditEventView[]`, **newest-first** (descending id). **No `nextCursor`
  field** — the client uses the last row's `id` as the next `before`.
- `AuditEventView` = `{id, at, accountId?, factor, event, ip?, userAgent?, detail?}`;
  `detail` is an arbitrary `map[string]any` (write-site redacted — never contains secrets).

UI:
- **Filters bar:** `factor`, `event` (text inputs — server matches exact strings, so no
  dropdowns), `accountId` (number), `since`/`until` (`datetime-local` → RFC3339).
  An **Apply** action resets the list + cursor and reloads from newest. A **Clear** action
  empties filters.
- **Table** newest-first: time (`formatDateTime`) · factor · event · accountId · ip.
  Each row **expands** (click/keyboard) to show pretty-printed `detail`
  (`JSON.stringify(detail, null, 2)` in `<pre>`) + `userAgent`. Empty/missing detail → "—".
- **Load more:** sends `before=<id of last loaded row>` with the current filters and
  **appends**. Hidden when a page returns fewer than `limit` rows (end reached).
  `limit=50`.

---

## Testing & done-gate

Each view gets a vitest spec in the established style (mock `@/lib/api`; assert
load / empty / error-Alert; mutations route through `withSudo`; `ConfirmDialog` confirm
paths; signing-key actions gated by status; audit **load-more appends** + filter query
params are sent; `active_key_no_replacement` + `upstream_idp_already_exists` surface).
Extend the router-guard regression test for the three new `requiresAdmin` routes.

**Done-gate (all GREEN, run from repo root `/home/tundra/projects/tundra/prohibitorum`):**
- `go build ./... && go vet ./...` exit 0 (authoritative over gopls "undefined" noise).
- Full `dashboard` vitest suite green.
- `cmd/smoke` `SMOKE_EXIT=0`.
- `cd dashboard && npm run build` then `git add pkg/webui/dist` — **commit dist once at the
  done-gate** (Vite chunk hashes are non-deterministic; discard reviewers' build dirt with
  `git checkout -- pkg/webui/dist` between source-only commits).

**Folded-in live visual review (after the gate):** prep `mise dev-server` +
`mise enroll-admin` + `mise dev-seed` (seeds upstream-IdPs/accounts/invitations so lists
render) and hand the user a reload-and-react checklist covering the three 3c pages + a
sweep of the earlier rebuilt surface (login/recovery, dashboard, security, connected,
devices, admin accounts/invitations/OIDC/SAML). The user is the visual verifier (no
screenshot tool).

## Plan shape (~9–10 tasks, subagent-driven)

sonnet impl + spec-then-quality review per task + opus final review. Rough grouping:
1. i18n + error codes + sidebar items + three routes + guard test (foundation).
2. `AdminUpstreamIdpsView` (list + create).
3. `AdminUpstreamIdpDetailView` (edit + rotate + delete).
4. `AdminSigningKeysView` (list + generate/activate/retire lifecycle).
5. `AdminAuditView` (filters + load-more + expandable detail).
6. Final whole-cycle opus review + fixes.
7. Done-gate (build/vet/vitest/smoke/dist) + handoff + memory.
(Steps may split; the per-task review bar is non-negotiable.)

## Out of scope (deferred, tracked — NOT this cycle)
- **D — OTP/password invitations** (BACKEND: enrollment credential-requirements column +
  new ceremony endpoints + frontend).
- **E — SAML-as-login subsystem** (MAJOR backend: ACS callback + assertion validation +
  account linking + upstream-SAML config; enables SAML invites).
- Migration `009` (drop legacy signing-key columns) after a soak; Playwright e2e;
  v0.7+ hardening (HSM/KMS, etc.).
