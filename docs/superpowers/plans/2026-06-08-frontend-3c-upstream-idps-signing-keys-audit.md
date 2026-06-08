# Frontend 3c — Upstream IdPs + Signing keys + Audit log — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers-extended-cc:subagent-driven-development (recommended) or superpowers-extended-cc:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the last three admin areas of the frontend rebuild — Upstream IdPs (federation config CRUD), Signing keys (lifecycle), and the Audit log viewer — reaching admin parity with the complete backend.

**Architecture:** Frontend-only; no Go changes. New views are `requiresAdmin` children of the existing `DashboardLayout`, wired into `adminItems` in `AppSidebar.vue`. Every view reuses the established admin patterns: `useApi()`+`api` for I/O, `withSudo()` for mutations (the `SudoModal` is already mounted in `DashboardLayout`), `Table`/`StatusBadge`/`ConfirmDialog`/`CodeField` primitives, and the `errors.<code>`→`Alert` idiom. The closest existing template is the OIDC clients pair (`AdminOidcClientsView.vue` + `AdminOidcClientDetailView.vue` + their tests) — read them before starting.

**Tech Stack:** Vue 3 (`<script setup lang="ts">`), Vite, Tailwind v4 + shadcn-vue/Reka UI, vue-i18n, vitest + @vue/test-utils. Backend contracts verified against `pkg/contract/auth.go` + handlers (`api.md` is STALE).

**Contracts (all under `/api/prohibitorum`, verified):**
- **Upstream IdPs:** `GET /upstream-idps`→`UpstreamIDPView[]`; `GET /upstream-idps/{slug}`→one (404 `upstream_idp_not_found`); `POST /upstream-idps` (sudo) `{slug,displayName,issuerUrl,clientId,clientSecret,mode,scopes?,allowedDomains?,usernameClaim?,displayNameClaim?,emailClaim?,requireVerifiedEmail?}`→201 view (409 `upstream_idp_already_exists`); `PUT /upstream-idps/{slug}` (sudo) same fields **minus `clientSecret`, plus `disabled`**→200 view; `POST /upstream-idps/rotate-secret` (sudo) `{slug,clientSecret}`→**204**; `POST /upstream-idps/delete` (sudo) `{slug}`→204. `UpstreamIDPView = {slug,displayName,issuerUrl,clientId,scopes[],mode,allowedDomains[],usernameClaim,displayNameClaim,emailClaim,requireVerifiedEmail,disabled,createdAt}` — **never any secret**. `mode ∈ {auto_provision,invite_only,link_only}`.
- **Signing keys:** `GET /signing-keys`→`SigningKeyView[]`; `POST /signing-keys/generate` (sudo, **no body**)→201 pending view; `POST /signing-keys/{kid}/activate` (sudo)→200 view (404 `credential_not_found`); `POST /signing-keys/{kid}/retire` (sudo)→200 view (404 `credential_not_found`; **409 `active_key_no_replacement`**). `SigningKeyView = {kid,algorithm,use,status,publicJwk,notBefore?,activatedAt?,decommissionedAt?,retireAfter?}`; `status ∈ {pending,active,decommissioning,retired}`; **no `privatePem`**.
- **Audit:** `GET /audit-events` (admin, **no sudo**) query `factor?,event?,accountId?(int),since?(RFC3339),until?(RFC3339),before?(int64 keyset),limit?(default 50, 1..200)`→`AuditEventView[]` **newest-first**, **no nextCursor** (use last row's `id` as next `before`). `AuditEventView = {id,at,accountId?,factor,event,ip?,userAgent?,detail?}`; `detail` arbitrary object.

**Per-task discipline (all tasks):** TDD where it fits the view (write the vitest spec alongside; the suite is the gate). After ANY `dashboard/src/locales/en.ts` edit, run the apostrophe guard (Step in Task 1; repeat after every en.ts touch):
```bash
grep -nP "\x{2018}" dashboard/src/locales/en.ts ; grep -nP ":\s*\x{2019}" dashboard/src/locales/en.ts
```
Both must print nothing (the Edit tool corrupts en.ts delimiters — project memory `reference_en_ts_apostrophe_edit_hazard`). Run git/dist commands from the repo root `/home/tundra/projects/tundra/prohibitorum`. Commit per task (source only); rebuild+commit `pkg/webui/dist` once at the finishing gate.

**Verify command for every frontend task:** `cd dashboard && npx vitest run` → all files pass. Type-check with `cd dashboard && npx vue-tsc --noEmit` when a task adds types.

---

### Task 1: Foundation — i18n, error codes, routes, sidebar, guard test

**Goal:** All scaffolding for the three areas in place so the four new views can be dropped in: i18n namespaces + labels + error codes, four `requiresAdmin` routes, three sidebar items, and a guard regression test asserting the new routes require admin.

**Files:**
- Modify: `dashboard/src/locales/en.ts` (add `admin.upstream.*`, `admin.signingKeys.*`, `admin.audit.*`; extend `admin.nav`; add 3 `errors.*` codes)
- Modify: `dashboard/src/components/custom/AppSidebar.vue:35-40` (extend `adminItems`, add icon imports)
- Modify: `dashboard/src/router/index.ts:88-91` (add 4 child routes after the SAML routes)
- Test: `dashboard/src/router/guard.test.ts` (add a regression test using the default router)

**Acceptance Criteria:**
- [ ] `en.ts` has complete English strings for all three namespaces + 3 new error codes; apostrophe guard prints nothing.
- [ ] Sidebar shows Upstream IdPs · Signing keys · Audit log under the admin group (gated on `isAdmin`).
- [ ] `/admin/upstream-idps`, `/admin/upstream-idps/:slug`, `/admin/signing-keys`, `/admin/audit` exist as `requiresAdmin` routes.
- [ ] Guard test asserts the three top-level new routes resolve with `meta.requiresAdmin === true`.

**Verify:** `cd dashboard && npx vitest run src/router/guard.test.ts` → PASS; `npx vue-tsc --noEmit` → no errors.

**Steps:**

- [ ] **Step 1: Add i18n keys to `dashboard/src/locales/en.ts`.**

Extend the `admin.nav` object (currently line 98) to add three labels:
```ts
    nav: { title: 'Admin', accounts: 'Accounts', invitations: 'Invitations', oidcClients: 'OIDC clients', samlProviders: 'SAML providers', upstreamIdps: 'Upstream IdPs', signingKeys: 'Signing keys', audit: 'Audit log' },
```

Inside the `admin: { ... }` object (after the `saml` block), add three namespaces. Use straight ASCII apostrophes in source — the guard catches curly ones:
```ts
    upstream: {
      title: 'Upstream IdPs',
      create: 'Add provider',
      created: 'Provider added.',
      back: 'Back to upstream IdPs',
      notFound: 'That provider no longer exists.',
      empty: 'No upstream identity providers configured yet.',
      poweredNote: 'These providers power the sign-in and connected-account options.',
      colName: 'Provider',
      colMode: 'Mode',
      colState: 'State',
      slug: 'Slug',
      displayName: 'Display name',
      issuerUrl: 'Issuer URL',
      clientId: 'Client ID',
      clientSecret: 'Client secret',
      mode: 'Provisioning mode',
      modeAutoProvision: 'Auto-provision',
      modeInviteOnly: 'Invite only',
      modeLinkOnly: 'Link only',
      scopes: 'Scopes',
      scopesHint: 'One scope per line.',
      allowedDomains: 'Allowed email domains',
      domainsHint: 'One domain per line. Leave empty to allow any.',
      usernameClaim: 'Username claim',
      displayNameClaim: 'Display-name claim',
      emailClaim: 'Email claim',
      requireVerifiedEmail: 'Require verified email',
      disabled: 'Disabled',
      active: 'Active',
      configTitle: 'Configuration',
      save: 'Save changes',
      saved: 'Changes saved.',
      rotate: 'Rotate secret',
      rotateTitle: 'Rotate client secret',
      rotateBody: 'Enter the new client secret issued by the provider. The old secret stops working immediately.',
      rotateConfirm: 'Rotate secret',
      rotated: 'Secret rotated.',
      dangerTitle: 'Danger zone',
      delete: 'Delete provider',
      deleteHelp: 'Removing a provider stops it from being offered for sign-in or account linking.',
      deleteConfirmTitle: 'Delete this provider?',
      deleteConfirmBody: 'This removes the provider configuration. Accounts already linked through it are not deleted, but can no longer sign in via this provider.',
    },
    signingKeys: {
      title: 'Signing keys',
      empty: 'No signing keys yet.',
      generate: 'Generate key',
      generateTitle: 'Generate a new signing key?',
      generateBody: 'A new key is created in the pending state. It is published immediately (appears in JWKS and SAML metadata) but does not sign tokens until you activate it.',
      generateConfirm: 'Generate key',
      generated: 'New key generated.',
      colKid: 'Key ID',
      colAlg: 'Algorithm',
      colUse: 'Use',
      colStatus: 'Status',
      colActivated: 'Activated',
      colDecommissioned: 'Decommissioned',
      colRetireAfter: 'Retire after',
      statusPending: 'Pending',
      statusActive: 'Active',
      statusDecommissioning: 'Decommissioning',
      statusRetired: 'Retired',
      publicJwk: 'Public JWK',
      activate: 'Activate',
      activateTitle: 'Activate this key?',
      activateBody: 'This key becomes the single active signing key. The current active key is demoted to decommissioning and kept only to verify previously issued signatures.',
      activateConfirm: 'Activate',
      activated: 'Key activated.',
      retire: 'Retire',
      retireTitle: 'Retire this key?',
      retireBody: 'A retired key stops appearing in JWKS and SAML metadata. Only decommissioning keys can be retired.',
      retireConfirm: 'Retire',
      retired: 'Key retired.',
    },
    audit: {
      title: 'Audit log',
      empty: 'No audit events match these filters.',
      loadMore: 'Load more',
      apply: 'Apply',
      clear: 'Clear',
      filterFactor: 'Factor',
      filterEvent: 'Event',
      filterAccount: 'Account ID',
      filterSince: 'Since',
      filterUntil: 'Until',
      colTime: 'Time',
      colFactor: 'Factor',
      colEvent: 'Event',
      colAccount: 'Account',
      colIp: 'IP',
      detail: 'Detail',
      userAgent: 'User agent',
      expand: 'Show detail',
    },
```

Add three error codes inside the `errors: { ... }` object (near the other admin codes around line 374):
```ts
    upstream_idp_not_found: 'That provider no longer exists.',
    upstream_idp_already_exists: 'A provider with that slug already exists.',
    active_key_no_replacement: 'Activate a replacement key before retiring the active key.',
```

- [ ] **Step 2: Run the apostrophe guard (MUST print nothing).**

Run from repo root:
```bash
grep -nP "\x{2018}" dashboard/src/locales/en.ts ; grep -nP ":\s*\x{2019}" dashboard/src/locales/en.ts
```
Expected: no output. If any line prints, the Edit tool flipped a `'` to a curly `‘`/`’` — fix that line back to ASCII `'`.

- [ ] **Step 3: Wire the sidebar.** In `dashboard/src/components/custom/AppSidebar.vue`, add icons to the lucide import (line 12) and three entries to `adminItems` (lines 35-40):
```ts
import { ShieldCheck, User, MonitorSmartphone, LogOut, KeyRound, Link2, TabletSmartphone, Users, Ticket, AppWindow, Building2, Network, KeySquare, ScrollText } from 'lucide-vue-next'
```
```ts
const adminItems = computed(() => [
  { to: '/admin/accounts', label: t('admin.nav.accounts'), icon: Users },
  { to: '/admin/invitations', label: t('admin.nav.invitations'), icon: Ticket },
  { to: '/admin/oidc-clients', label: t('admin.nav.oidcClients'), icon: AppWindow },
  { to: '/admin/saml-providers', label: t('admin.nav.samlProviders'), icon: Building2 },
  { to: '/admin/upstream-idps', label: t('admin.nav.upstreamIdps'), icon: Network },
  { to: '/admin/signing-keys', label: t('admin.nav.signingKeys'), icon: KeySquare },
  { to: '/admin/audit', label: t('admin.nav.audit'), icon: ScrollText },
])
```

- [ ] **Step 4: Add routes.** In `dashboard/src/router/index.ts`, after the SAML provider detail route (line 91), add four children:
```ts
      { path: 'admin/upstream-idps', name: 'admin-upstream-idps', component: () => import('../pages/admin/AdminUpstreamIdpsView.vue'), meta: { requiresAdmin: true } },
      { path: 'admin/upstream-idps/:slug', name: 'admin-upstream-idp-detail', component: () => import('../pages/admin/AdminUpstreamIdpDetailView.vue'), meta: { requiresAdmin: true } },
      { path: 'admin/signing-keys', name: 'admin-signing-keys', component: () => import('../pages/admin/AdminSigningKeysView.vue'), meta: { requiresAdmin: true } },
      { path: 'admin/audit', name: 'admin-audit', component: () => import('../pages/admin/AdminAuditView.vue'), meta: { requiresAdmin: true } },
```
NOTE: the four view files do not exist yet — they are created in Tasks 2–5. `vue-tsc`/vitest will not fail on a lazy `import()` of a missing file at type-check time (dynamic import), but the dev/prod build WILL fail until they exist. That is expected; the build runs at the finishing gate after all views exist. Per-task vitest does not import the router's lazy chunks, so it stays green.

- [ ] **Step 5: Add the guard regression test.** Append to `dashboard/src/router/guard.test.ts`:
```ts
import realRouter from './index'

describe('3c admin routes require admin', () => {
  it.each([
    '/admin/upstream-idps',
    '/admin/signing-keys',
    '/admin/audit',
  ])('%s is marked requiresAdmin', (path) => {
    const resolved = realRouter.resolve(path)
    expect(resolved.meta.requiresAdmin).toBe(true)
  })
})
```

- [ ] **Step 6: Verify + commit.**
```bash
cd dashboard && npx vitest run src/router/guard.test.ts && npx vue-tsc --noEmit
```
Expected: guard tests PASS; vue-tsc no errors. Then from repo root:
```bash
git add dashboard/src/locales/en.ts dashboard/src/components/custom/AppSidebar.vue dashboard/src/router/index.ts dashboard/src/router/guard.test.ts
git commit -m "feat(web): 3c foundation — i18n, routes, sidebar, guard test for upstream IdPs/signing keys/audit"
```

---

### Task 2: AdminUpstreamIdpsView — list + create

**Goal:** `/admin/upstream-idps` lists providers and creates new ones (sudo-gated, write-only secret), modeled on `AdminOidcClientsView.vue`.

**Files:**
- Create: `dashboard/src/pages/admin/AdminUpstreamIdpsView.vue`
- Test: `dashboard/src/pages/admin/AdminUpstreamIdpsView.test.ts`

**Acceptance Criteria:**
- [ ] Loads `GET /api/prohibitorum/upstream-idps`, renders a Table (provider/slug, mode badge, state badge); empty-state when no rows.
- [ ] Create card: required slug/displayName/issuerUrl/clientId/clientSecret + mode `<select>` (3 modes) + defaulted scopes/claims/domains/requireVerifiedEmail; posts via `withSudo`.
- [ ] `clientSecret` is a write-only password input, never displayed back.
- [ ] `upstream_idp_already_exists` surfaces inline; form stays open.
- [ ] On success: closes the form, shows `admin.upstream.created`, reloads the list.

**Verify:** `cd dashboard && npx vitest run src/pages/admin/AdminUpstreamIdpsView.test.ts` → PASS.

**Steps:**

- [ ] **Step 1: Write the failing test** `dashboard/src/pages/admin/AdminUpstreamIdpsView.test.ts`:
```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'
vi.mock('@/lib/sudo', () => ({ withSudo: (fn: () => Promise<unknown>) => fn() }))
const get = vi.mocked(api.get); const post = vi.mocked(api.post)
const { push } = vi.hoisted(() => ({ push: vi.fn() }))
vi.mock('vue-router', () => ({ useRouter: () => ({ push }) }))
import AdminUpstreamIdpsView from './AdminUpstreamIdpsView.vue'

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
const mountView = () => mount(AdminUpstreamIdpsView, { global: { plugins: [i18n()] }, attachTo: document.body })
const IDPS = [
  { slug: 'okta', displayName: 'Okta', issuerUrl: 'https://okta/', clientId: 'c1', scopes: ['openid'], mode: 'auto_provision', allowedDomains: [], usernameClaim: 'preferred_username', displayNameClaim: 'name', emailClaim: 'email', requireVerifiedEmail: false, disabled: false, createdAt: '2026-01-01T00:00:00Z' },
  { slug: 'entra', displayName: 'Entra', issuerUrl: 'https://entra/', clientId: 'c2', scopes: ['openid'], mode: 'invite_only', allowedDomains: [], usernameClaim: 'preferred_username', displayNameClaim: 'name', emailClaim: 'email', requireVerifiedEmail: true, disabled: true, createdAt: '2026-01-02T00:00:00Z' },
]
beforeEach(() => { get.mockReset(); post.mockReset(); push.mockReset() })

describe('AdminUpstreamIdpsView', () => {
  it('lists providers with mode + state', async () => {
    get.mockResolvedValue(IDPS)
    const w = mountView(); await flushPromises()
    expect(get).toHaveBeenCalledWith('/api/prohibitorum/upstream-idps')
    expect(w.text()).toContain('Okta'); expect(w.text()).toContain(en.admin.upstream.modeInviteOnly)
  })
  it('row click navigates to detail', async () => {
    get.mockResolvedValue(IDPS)
    const w = mountView(); await flushPromises()
    await w.find('[data-test="idp-row-okta"]').trigger('click')
    expect(push).toHaveBeenCalledWith('/admin/upstream-idps/okta')
  })
  it('creates a provider with mode + secret via withSudo', async () => {
    get.mockResolvedValue([])
    post.mockResolvedValue({ slug: 'new', displayName: 'New', mode: 'link_only' })
    const w = mountView(); await flushPromises()
    await w.find('[data-test="create"]').trigger('click')
    await w.find('input[name="slug"]').setValue('new')
    await w.find('input[name="displayName"]').setValue('New')
    await w.find('input[name="issuerUrl"]').setValue('https://new/')
    await w.find('input[name="clientId"]').setValue('cid')
    await w.find('input[name="clientSecret"]').setValue('sek')
    await w.find('select[name="mode"]').setValue('link_only')
    await w.find('[data-test="create-confirm"]').trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/upstream-idps', expect.objectContaining({
      slug: 'new', displayName: 'New', issuerUrl: 'https://new/', clientId: 'cid', clientSecret: 'sek', mode: 'link_only',
    }))
    expect(w.text()).toContain(en.admin.upstream.created)
  })
  it('surfaces upstream_idp_already_exists', async () => {
    get.mockResolvedValue([])
    post.mockRejectedValue({ code: 'upstream_idp_already_exists', message: 'zh' })
    const w = mountView(); await flushPromises()
    await w.find('[data-test="create"]').trigger('click')
    await w.find('input[name="slug"]').setValue('okta')
    await w.find('input[name="displayName"]').setValue('Dup')
    await w.find('input[name="issuerUrl"]').setValue('https://x/')
    await w.find('input[name="clientId"]').setValue('c')
    await w.find('input[name="clientSecret"]').setValue('s')
    await w.find('[data-test="create-confirm"]').trigger('click'); await flushPromises()
    expect(w.text()).toContain(en.errors.upstream_idp_already_exists)
  })
})
```
Run `cd dashboard && npx vitest run src/pages/admin/AdminUpstreamIdpsView.test.ts` → FAIL (component missing).

- [ ] **Step 2: Implement** `dashboard/src/pages/admin/AdminUpstreamIdpsView.vue`:
```vue
<script setup lang="ts">
/** AdminUpstreamIdpsView (/admin/upstream-idps) — list upstream IdPs; inline create (sudo). */
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { withSudo } from '@/lib/sudo'
import { Card, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/table'
import StatusBadge from '@/components/custom/StatusBadge.vue'

export interface UpstreamIdp {
  slug: string; displayName: string; issuerUrl: string; clientId: string
  scopes: string[]; mode: string; allowedDomains: string[]
  usernameClaim: string; displayNameClaim: string; emailClaim: string
  requireVerifiedEmail: boolean; disabled: boolean; createdAt: string
}

const { t, te } = useI18n()
const router = useRouter()
const { busy, error, run } = useApi()

const rows = ref<UpstreamIdp[]>([])
const createOpen = ref(false)
const created = ref(false)

const slug = ref(''); const displayName = ref(''); const issuerUrl = ref(''); const clientId = ref('')
const clientSecret = ref(''); const mode = ref('auto_provision')
const scopes = ref('openid\nemail\nprofile')
const allowedDomains = ref('')
const usernameClaim = ref('preferred_username'); const displayNameClaim = ref('name'); const emailClaim = ref('email')
const requireVerifiedEmail = ref(false)

const errorText = computed(() => {
  const e = error.value
  if (!e) return ''
  const key = `errors.${e.code}`
  return te(key) ? t(key) : e.message || t('common.error')
})

function modeLabel(m: string): string {
  if (m === 'invite_only') return t('admin.upstream.modeInviteOnly')
  if (m === 'link_only') return t('admin.upstream.modeLinkOnly')
  return t('admin.upstream.modeAutoProvision')
}
function lines(s: string): string[] { return s.split('\n').map((x) => x.trim()).filter(Boolean) }
function go(s: string): void { router.push(`/admin/upstream-idps/${s}`) }

async function load(): Promise<void> {
  const res = await run(() => api.get<UpstreamIdp[]>('/api/prohibitorum/upstream-idps'))
  if (res) rows.value = res
}

function openCreate(): void {
  slug.value = ''; displayName.value = ''; issuerUrl.value = ''; clientId.value = ''
  clientSecret.value = ''; mode.value = 'auto_provision'
  scopes.value = 'openid\nemail\nprofile'; allowedDomains.value = ''
  usernameClaim.value = 'preferred_username'; displayNameClaim.value = 'name'; emailClaim.value = 'email'
  requireVerifiedEmail.value = false; created.value = false; createOpen.value = true
}

async function create(): Promise<void> {
  created.value = false
  const res = await run(() => withSudo(() => api.post<UpstreamIdp>('/api/prohibitorum/upstream-idps', {
    slug: slug.value, displayName: displayName.value, issuerUrl: issuerUrl.value, clientId: clientId.value,
    clientSecret: clientSecret.value, mode: mode.value, scopes: lines(scopes.value),
    allowedDomains: lines(allowedDomains.value), usernameClaim: usernameClaim.value,
    displayNameClaim: displayNameClaim.value, emailClaim: emailClaim.value,
    requireVerifiedEmail: requireVerifiedEmail.value,
  })))
  if (res) { createOpen.value = false; created.value = true; await load() }
}

onMounted(load)
</script>
<template>
  <div class="flex max-w-4xl flex-col gap-6">
    <div class="flex items-center justify-between gap-4">
      <h1 class="text-2xl font-semibold tracking-tight text-ink">{{ t('admin.upstream.title') }}</h1>
      <Button type="button" data-test="create" @click="openCreate">{{ t('admin.upstream.create') }}</Button>
    </div>
    <p class="text-sm text-muted">{{ t('admin.upstream.poweredNote') }}</p>
    <Alert v-if="errorText" variant="destructive" role="alert" aria-live="polite"><AlertDescription>{{ errorText }}</AlertDescription></Alert>
    <p v-if="created" class="text-sm text-sage" role="status">{{ t('admin.upstream.created') }}</p>

    <Card v-if="createOpen">
      <CardContent class="flex flex-col gap-3 py-4">
        <div class="flex flex-col gap-1.5">
          <Label for="slug">{{ t('admin.upstream.slug') }}</Label>
          <Input id="slug" name="slug" v-model="slug" autocomplete="off" />
        </div>
        <div class="flex flex-col gap-1.5">
          <Label for="displayName">{{ t('admin.upstream.displayName') }}</Label>
          <Input id="displayName" name="displayName" v-model="displayName" />
        </div>
        <div class="flex flex-col gap-1.5">
          <Label for="issuerUrl">{{ t('admin.upstream.issuerUrl') }}</Label>
          <Input id="issuerUrl" name="issuerUrl" v-model="issuerUrl" autocomplete="off" />
        </div>
        <div class="flex flex-col gap-1.5">
          <Label for="clientId">{{ t('admin.upstream.clientId') }}</Label>
          <Input id="clientId" name="clientId" v-model="clientId" autocomplete="off" />
        </div>
        <div class="flex flex-col gap-1.5">
          <Label for="clientSecret">{{ t('admin.upstream.clientSecret') }}</Label>
          <Input id="clientSecret" name="clientSecret" type="password" v-model="clientSecret" autocomplete="off" />
        </div>
        <div class="flex flex-col gap-1.5">
          <Label for="mode">{{ t('admin.upstream.mode') }}</Label>
          <select id="mode" name="mode" v-model="mode" class="h-9 rounded-md border border-input bg-transparent px-3 text-sm text-ink">
            <option value="auto_provision">{{ t('admin.upstream.modeAutoProvision') }}</option>
            <option value="invite_only">{{ t('admin.upstream.modeInviteOnly') }}</option>
            <option value="link_only">{{ t('admin.upstream.modeLinkOnly') }}</option>
          </select>
        </div>
        <div class="flex flex-col gap-1.5">
          <Label for="scopes">{{ t('admin.upstream.scopes') }}</Label>
          <Textarea id="scopes" name="scopes" v-model="scopes" :placeholder="t('admin.upstream.scopesHint')" />
        </div>
        <div class="flex flex-col gap-1.5">
          <Label for="allowedDomains">{{ t('admin.upstream.allowedDomains') }}</Label>
          <Textarea id="allowedDomains" name="allowedDomains" v-model="allowedDomains" :placeholder="t('admin.upstream.domainsHint')" />
        </div>
        <div class="flex flex-col gap-1.5">
          <Label for="usernameClaim">{{ t('admin.upstream.usernameClaim') }}</Label>
          <Input id="usernameClaim" name="usernameClaim" v-model="usernameClaim" autocomplete="off" />
        </div>
        <div class="flex flex-col gap-1.5">
          <Label for="displayNameClaim">{{ t('admin.upstream.displayNameClaim') }}</Label>
          <Input id="displayNameClaim" name="displayNameClaim" v-model="displayNameClaim" autocomplete="off" />
        </div>
        <div class="flex flex-col gap-1.5">
          <Label for="emailClaim">{{ t('admin.upstream.emailClaim') }}</Label>
          <Input id="emailClaim" name="emailClaim" v-model="emailClaim" autocomplete="off" />
        </div>
        <label class="flex items-center gap-2 text-sm text-ink">
          <input type="checkbox" name="requireVerifiedEmail" v-model="requireVerifiedEmail" />
          {{ t('admin.upstream.requireVerifiedEmail') }}
        </label>
        <div class="flex gap-2">
          <Button type="button" :disabled="busy" data-test="create-confirm" @click="create">{{ t('admin.upstream.create') }}</Button>
          <Button type="button" variant="outline" :disabled="busy" data-test="create-cancel" @click="createOpen = false">{{ t('common.cancel') }}</Button>
        </div>
      </CardContent>
    </Card>

    <Table v-if="rows.length">
      <TableHeader>
        <TableRow>
          <TableHead>{{ t('admin.upstream.colName') }}</TableHead>
          <TableHead>{{ t('admin.upstream.colMode') }}</TableHead>
          <TableHead>{{ t('admin.upstream.colState') }}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <TableRow v-for="i in rows" :key="i.slug" class="cursor-pointer" tabindex="0"
                  :data-test="`idp-row-${i.slug}`"
                  @click="go(i.slug)" @keydown.enter="go(i.slug)" @keydown.space.prevent="go(i.slug)">
          <TableCell>
            <div class="flex min-w-0 flex-col">
              <span class="truncate font-medium text-ink">{{ i.displayName }}</span>
              <span class="truncate text-muted">{{ i.slug }}</span>
            </div>
          </TableCell>
          <TableCell><StatusBadge variant="neutral">{{ modeLabel(i.mode) }}</StatusBadge></TableCell>
          <TableCell>
            <StatusBadge :variant="i.disabled ? 'danger' : 'success'">
              {{ i.disabled ? t('admin.upstream.disabled') : t('admin.upstream.active') }}
            </StatusBadge>
          </TableCell>
        </TableRow>
      </TableBody>
    </Table>
    <p v-else-if="!busy && !errorText && !createOpen" class="text-sm text-muted">{{ t('admin.upstream.empty') }}</p>
  </div>
</template>
```

- [ ] **Step 3: Run the test → PASS.** `cd dashboard && npx vitest run src/pages/admin/AdminUpstreamIdpsView.test.ts`. Then re-run the apostrophe guard if en.ts was touched (it shouldn't be here).

- [ ] **Step 4: Commit.**
```bash
git add dashboard/src/pages/admin/AdminUpstreamIdpsView.vue dashboard/src/pages/admin/AdminUpstreamIdpsView.test.ts
git commit -m "feat(web): admin upstream-IdPs list + create"
```

---

### Task 3: AdminUpstreamIdpDetailView — edit + rotate secret + delete

**Goal:** `/admin/upstream-idps/:slug` edits config (PUT, no secret, +disabled), rotates the secret (POST, 204, no reveal), and deletes — modeled on `AdminOidcClientDetailView.vue`.

**Files:**
- Create: `dashboard/src/pages/admin/AdminUpstreamIdpDetailView.vue`
- Test: `dashboard/src/pages/admin/AdminUpstreamIdpDetailView.test.ts`

**Acceptance Criteria:**
- [ ] Loads `GET /upstream-idps/{slug}`; not-found state on `upstream_idp_not_found`; load-error Alert is visible.
- [ ] Edit form PUTs all view fields incl. `disabled`, **without `clientSecret`**, via `withSudo`; shows saved confirmation.
- [ ] Rotate card: write-only secret input → `POST /rotate-secret {slug,clientSecret}` (204) via `withSudo`; shows rotated confirmation; never reveals a value.
- [ ] Delete via `ConfirmDialog` → `POST /delete {slug}`; navigates to `/admin/upstream-idps`.

**Verify:** `cd dashboard && npx vitest run src/pages/admin/AdminUpstreamIdpDetailView.test.ts` → PASS.

**Steps:**

- [ ] **Step 1: Write the failing test** `dashboard/src/pages/admin/AdminUpstreamIdpDetailView.test.ts`:
```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'
vi.mock('@/lib/sudo', () => ({ withSudo: (fn: () => Promise<unknown>) => fn() }))
const get = vi.mocked(api.get); const post = vi.mocked(api.post); const put = vi.mocked(api.put)
const { push } = vi.hoisted(() => ({ push: vi.fn() }))
vi.mock('vue-router', () => ({ useRoute: () => ({ params: { slug: 'okta' } }), useRouter: () => ({ push }), RouterLink: { template: '<a><slot/></a>' } }))
import AdminUpstreamIdpDetailView from './AdminUpstreamIdpDetailView.vue'

const IDP = { slug: 'okta', displayName: 'Okta', issuerUrl: 'https://okta/', clientId: 'c1', scopes: ['openid','email'], mode: 'auto_provision', allowedDomains: ['ex.com'], usernameClaim: 'preferred_username', displayNameClaim: 'name', emailClaim: 'email', requireVerifiedEmail: false, disabled: false, createdAt: '2026-01-01T00:00:00Z' }
const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
const mountView = () => mount(AdminUpstreamIdpDetailView, { global: { plugins: [i18n()] }, attachTo: document.body })
beforeEach(() => { get.mockReset(); post.mockReset(); put.mockReset(); push.mockReset() })

describe('AdminUpstreamIdpDetailView', () => {
  it('shows not-found on upstream_idp_not_found', async () => {
    get.mockRejectedValue({ code: 'upstream_idp_not_found', message: 'zh' })
    const w = mountView(); await flushPromises()
    expect(w.text()).toContain(en.admin.upstream.notFound)
  })
  it('saves config via PUT without clientSecret', async () => {
    get.mockResolvedValue(IDP); put.mockResolvedValue({ ...IDP, displayName: 'Okta 2' })
    const w = mountView(); await flushPromises()
    await w.find('input[name="displayName"]').setValue('Okta 2')
    await w.find('[data-test="save"]').trigger('click'); await flushPromises()
    const [, body] = put.mock.calls[0]
    expect(put).toHaveBeenCalledWith('/api/prohibitorum/upstream-idps/okta', expect.objectContaining({ displayName: 'Okta 2', disabled: false }))
    expect(body).not.toHaveProperty('clientSecret')
    expect(w.text()).toContain(en.admin.upstream.saved)
  })
  it('rotates the secret without revealing a value', async () => {
    get.mockResolvedValue(IDP); post.mockResolvedValue(undefined)
    const w = mountView(); await flushPromises()
    await w.find('input[name="newSecret"]').setValue('newsek')
    await w.find('[data-test="rotate"]').trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/upstream-idps/rotate-secret', { slug: 'okta', clientSecret: 'newsek' })
    expect(w.text()).toContain(en.admin.upstream.rotated)
  })
  it('deletes and navigates back to the list', async () => {
    get.mockResolvedValue(IDP); post.mockResolvedValue(undefined)
    const w = mountView(); await flushPromises()
    await w.find('[data-test="delete"]').trigger('click'); await flushPromises()
    await w.find('[data-test="confirm-delete"] button[type="button"].bg-destructive, [data-test="confirm-delete"]').exists()
    // confirm via the ConfirmDialog confirm button
    const confirmBtn = w.findAll('button').find((b) => b.text() === en.admin.upstream.delete && b.classes().includes('bg-destructive'))
    await confirmBtn!.trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/upstream-idps/delete', { slug: 'okta' })
    expect(push).toHaveBeenCalledWith('/admin/upstream-idps')
  })
})
```
NOTE on the delete test: if locating the confirm button by class proves brittle, instead emit-trigger the dialog — call the component's `@confirm` by finding the `ConfirmDialog` stub. Simpler alternative used in existing tests: render real ConfirmDialog (no stub) and click the destructive button by its label text. Keep whichever is green; the existing OIDC detail test does not test delete, so this is new — prefer the label+variant approach above.

Run → FAIL (component missing).

- [ ] **Step 2: Implement** `dashboard/src/pages/admin/AdminUpstreamIdpDetailView.vue` (model on `AdminOidcClientDetailView.vue`):
```vue
<script setup lang="ts">
/** AdminUpstreamIdpDetailView (/admin/upstream-idps/:slug) — edit, rotate secret, delete. */
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { withSudo } from '@/lib/sudo'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { Alert, AlertDescription } from '@/components/ui/alert'
import ConfirmDialog from '@/components/custom/ConfirmDialog.vue'
import type { UpstreamIdp } from './AdminUpstreamIdpsView.vue'

const { t, te } = useI18n()
const route = useRoute()
const router = useRouter()
const { busy, error, run } = useApi()

const slug = String(route.params.slug)
const idp = ref<UpstreamIdp | null>(null)
const notFound = ref(false)

const displayName = ref(''); const issuerUrl = ref(''); const clientId = ref('')
const mode = ref('auto_provision'); const scopes = ref(''); const allowedDomains = ref('')
const usernameClaim = ref(''); const displayNameClaim = ref(''); const emailClaim = ref('')
const requireVerifiedEmail = ref(false); const disabled = ref(false)
const saved = ref(false)

const newSecret = ref(''); const rotated = ref(false)
const confirmDelete = ref(false)

const errorText = computed(() => {
  const e = error.value
  if (!e) return ''
  const key = `errors.${e.code}`
  return te(key) ? t(key) : e.message || t('common.error')
})
function lines(s: string): string[] { return s.split('\n').map((x) => x.trim()).filter(Boolean) }

async function load(): Promise<void> {
  const i = await run(() => api.get<UpstreamIdp>(`/api/prohibitorum/upstream-idps/${slug}`))
  if (!i) { if (error.value?.code === 'upstream_idp_not_found') notFound.value = true; return }
  idp.value = i
  displayName.value = i.displayName; issuerUrl.value = i.issuerUrl; clientId.value = i.clientId
  mode.value = i.mode; scopes.value = i.scopes.join('\n'); allowedDomains.value = i.allowedDomains.join('\n')
  usernameClaim.value = i.usernameClaim; displayNameClaim.value = i.displayNameClaim; emailClaim.value = i.emailClaim
  requireVerifiedEmail.value = i.requireVerifiedEmail; disabled.value = i.disabled
}

async function save(): Promise<void> {
  saved.value = false; rotated.value = false
  const updated = await run(() => withSudo(() => api.put<UpstreamIdp>(`/api/prohibitorum/upstream-idps/${slug}`, {
    displayName: displayName.value, issuerUrl: issuerUrl.value, clientId: clientId.value, mode: mode.value,
    scopes: lines(scopes.value), allowedDomains: lines(allowedDomains.value), usernameClaim: usernameClaim.value,
    displayNameClaim: displayNameClaim.value, emailClaim: emailClaim.value,
    requireVerifiedEmail: requireVerifiedEmail.value, disabled: disabled.value,
  })))
  if (updated) { idp.value = updated; saved.value = true }
}

async function rotate(): Promise<void> {
  saved.value = false; rotated.value = false
  const ok = await run(() => withSudo(async () => {
    await api.post('/api/prohibitorum/upstream-idps/rotate-secret', { slug, clientSecret: newSecret.value })
    return true as const
  }))
  if (ok) { rotated.value = true; newSecret.value = '' }
}

async function destroy(): Promise<void> {
  saved.value = false; rotated.value = false
  const ok = await run(() => withSudo(async () => {
    await api.post('/api/prohibitorum/upstream-idps/delete', { slug })
    return true as const
  }))
  confirmDelete.value = false
  if (ok) router.push('/admin/upstream-idps')
}

onMounted(load)
</script>
<template>
  <div class="flex max-w-2xl flex-col gap-6">
    <RouterLink to="/admin/upstream-idps" class="text-sm text-muted underline-offset-4 hover:underline">{{ t('admin.upstream.back') }}</RouterLink>
    <Alert v-if="errorText" variant="destructive" role="alert" aria-live="polite"><AlertDescription>{{ errorText }}</AlertDescription></Alert>
    <p v-if="notFound" class="text-sm text-muted" role="status">{{ t('admin.upstream.notFound') }}</p>

    <template v-else-if="idp">
      <h1 class="text-2xl font-semibold tracking-tight text-ink">{{ idp.displayName }}</h1>

      <Card>
        <CardHeader><CardTitle>{{ t('admin.upstream.configTitle') }}</CardTitle></CardHeader>
        <CardContent class="flex flex-col gap-4">
          <div class="flex flex-col gap-1.5">
            <Label for="displayName">{{ t('admin.upstream.displayName') }}</Label>
            <Input id="displayName" name="displayName" v-model="displayName" />
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="issuerUrl">{{ t('admin.upstream.issuerUrl') }}</Label>
            <Input id="issuerUrl" name="issuerUrl" v-model="issuerUrl" autocomplete="off" />
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="clientId">{{ t('admin.upstream.clientId') }}</Label>
            <Input id="clientId" name="clientId" v-model="clientId" autocomplete="off" />
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="mode">{{ t('admin.upstream.mode') }}</Label>
            <select id="mode" name="mode" v-model="mode" class="h-9 rounded-md border border-input bg-transparent px-3 text-sm text-ink">
              <option value="auto_provision">{{ t('admin.upstream.modeAutoProvision') }}</option>
              <option value="invite_only">{{ t('admin.upstream.modeInviteOnly') }}</option>
              <option value="link_only">{{ t('admin.upstream.modeLinkOnly') }}</option>
            </select>
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="scopes">{{ t('admin.upstream.scopes') }}</Label>
            <Textarea id="scopes" name="scopes" v-model="scopes" :placeholder="t('admin.upstream.scopesHint')" />
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="allowedDomains">{{ t('admin.upstream.allowedDomains') }}</Label>
            <Textarea id="allowedDomains" name="allowedDomains" v-model="allowedDomains" :placeholder="t('admin.upstream.domainsHint')" />
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="usernameClaim">{{ t('admin.upstream.usernameClaim') }}</Label>
            <Input id="usernameClaim" name="usernameClaim" v-model="usernameClaim" autocomplete="off" />
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="displayNameClaim">{{ t('admin.upstream.displayNameClaim') }}</Label>
            <Input id="displayNameClaim" name="displayNameClaim" v-model="displayNameClaim" autocomplete="off" />
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="emailClaim">{{ t('admin.upstream.emailClaim') }}</Label>
            <Input id="emailClaim" name="emailClaim" v-model="emailClaim" autocomplete="off" />
          </div>
          <label class="flex items-center gap-2 text-sm text-ink">
            <input type="checkbox" name="requireVerifiedEmail" v-model="requireVerifiedEmail" />
            {{ t('admin.upstream.requireVerifiedEmail') }}
          </label>
          <label class="flex items-center gap-2 text-sm text-ink">
            <input type="checkbox" name="disabled" v-model="disabled" />
            {{ t('admin.upstream.disabled') }}
          </label>
          <div class="flex items-center gap-3">
            <Button type="button" :disabled="busy" data-test="save" @click="save">{{ t('admin.upstream.save') }}</Button>
            <span v-if="saved" class="text-sm text-sage" role="status">{{ t('admin.upstream.saved') }}</span>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader><CardTitle>{{ t('admin.upstream.rotateTitle') }}</CardTitle></CardHeader>
        <CardContent class="flex flex-col gap-3">
          <p class="text-sm text-muted">{{ t('admin.upstream.rotateBody') }}</p>
          <div class="flex flex-col gap-1.5">
            <Label for="newSecret">{{ t('admin.upstream.clientSecret') }}</Label>
            <Input id="newSecret" name="newSecret" type="password" v-model="newSecret" autocomplete="off" />
          </div>
          <span v-if="rotated" class="text-sm text-sage" role="status">{{ t('admin.upstream.rotated') }}</span>
          <Button type="button" variant="outline" class="w-fit" :disabled="busy || !newSecret" data-test="rotate" @click="rotate">{{ t('admin.upstream.rotateConfirm') }}</Button>
        </CardContent>
      </Card>

      <Card class="border-destructive/30 bg-destructive/[0.02]">
        <CardHeader><CardTitle class="text-destructive">{{ t('admin.upstream.dangerTitle') }}</CardTitle></CardHeader>
        <CardContent class="flex flex-col gap-3">
          <p class="text-sm text-muted">{{ t('admin.upstream.deleteHelp') }}</p>
          <Button type="button" variant="destructive" class="w-fit" :disabled="busy" data-test="delete" @click="confirmDelete = true">{{ t('admin.upstream.delete') }}</Button>
        </CardContent>
      </Card>
    </template>

    <ConfirmDialog :open="confirmDelete" :title="t('admin.upstream.deleteConfirmTitle')" :confirm-label="t('admin.upstream.delete')" :busy="busy"
      @update:open="(v) => { if (!v) confirmDelete = false }" @cancel="confirmDelete = false" @confirm="destroy">
      {{ t('admin.upstream.deleteConfirmBody') }}
    </ConfirmDialog>
  </div>
</template>
```
NOTE: importing the `UpstreamIdp` type from the list view requires it to be `export interface` there (Task 2 does this). The `import type` is erased at runtime so it does not pull the list component into this chunk.

- [ ] **Step 3: Run the test → PASS.** Adjust the delete-confirm selector if needed (see the test NOTE). `cd dashboard && npx vitest run src/pages/admin/AdminUpstreamIdpDetailView.test.ts`.

- [ ] **Step 4: Commit.**
```bash
git add dashboard/src/pages/admin/AdminUpstreamIdpDetailView.vue dashboard/src/pages/admin/AdminUpstreamIdpDetailView.test.ts
git commit -m "feat(web): admin upstream-IdP detail — edit, rotate secret, delete"
```

---

### Task 4: AdminSigningKeysView — list + lifecycle (generate/activate/retire)

**Goal:** `/admin/signing-keys` lists keys with status badges + timestamps + expandable public JWK, and drives the lifecycle: generate (toolbar), activate (pending rows), retire (decommissioning rows) — each sudo-gated behind a `ConfirmDialog`.

**Files:**
- Create: `dashboard/src/pages/admin/AdminSigningKeysView.vue`
- Test: `dashboard/src/pages/admin/AdminSigningKeysView.test.ts`

**Acceptance Criteria:**
- [ ] Loads `GET /signing-keys`; Table shows kid/algorithm/use/status-badge/timestamps; empty-state when none.
- [ ] Status badges: pending→neutral, active→success, decommissioning→caution, retired→neutral.
- [ ] Generate → `POST /signing-keys/generate` (no body) via `withSudo`, behind a ConfirmDialog; reloads.
- [ ] Activate shown only on `pending` rows → `POST /signing-keys/{kid}/activate`; Retire shown only on `decommissioning` rows → `POST /signing-keys/{kid}/retire`; both via `withSudo`+ConfirmDialog; reload on success.
- [ ] `active_key_no_replacement` (from retire) surfaces via the `errors.*` Alert.
- [ ] Each row can expand to show `publicJwk` pretty-printed.

**Verify:** `cd dashboard && npx vitest run src/pages/admin/AdminSigningKeysView.test.ts` → PASS.

**Steps:**

- [ ] **Step 1: Write the failing test** `dashboard/src/pages/admin/AdminSigningKeysView.test.ts`:
```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'
vi.mock('@/lib/sudo', () => ({ withSudo: (fn: () => Promise<unknown>) => fn() }))
const get = vi.mocked(api.get); const post = vi.mocked(api.post)
import AdminSigningKeysView from './AdminSigningKeysView.vue'

const KEYS = [
  { kid: 'k-active', algorithm: 'RS256', use: 'sig', status: 'active', publicJwk: { kty: 'RSA', n: 'aaa' }, activatedAt: '2026-01-01T00:00:00Z' },
  { kid: 'k-pending', algorithm: 'RS256', use: 'sig', status: 'pending', publicJwk: { kty: 'RSA', n: 'bbb' } },
  { kid: 'k-decom', algorithm: 'RS256', use: 'sig', status: 'decommissioning', publicJwk: { kty: 'RSA', n: 'ccc' }, decommissionedAt: '2026-01-02T00:00:00Z' },
]
const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
const mountView = () => mount(AdminSigningKeysView, { global: { plugins: [i18n()] }, attachTo: document.body })
const clickConfirm = async (w: ReturnType<typeof mountView>, label: string) => {
  const btn = w.findAll('button').find((b) => b.text() === label && b.classes().includes('bg-destructive'))
  await btn!.trigger('click'); await flushPromises()
}
beforeEach(() => { get.mockReset(); post.mockReset() })

describe('AdminSigningKeysView', () => {
  it('lists keys with status badges', async () => {
    get.mockResolvedValue(KEYS)
    const w = mountView(); await flushPromises()
    expect(get).toHaveBeenCalledWith('/api/prohibitorum/signing-keys')
    expect(w.text()).toContain('k-active'); expect(w.text()).toContain(en.admin.signingKeys.statusActive)
    expect(w.text()).toContain(en.admin.signingKeys.statusPending); expect(w.text()).toContain(en.admin.signingKeys.statusDecommissioning)
  })
  it('generates a key via withSudo + confirm', async () => {
    get.mockResolvedValueOnce(KEYS).mockResolvedValueOnce(KEYS)
    post.mockResolvedValue({ kid: 'k-new', status: 'pending' })
    const w = mountView(); await flushPromises()
    await w.find('[data-test="generate"]').trigger('click')
    await clickConfirm(w, en.admin.signingKeys.generateConfirm)
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/signing-keys/generate')
  })
  it('activates only a pending key', async () => {
    get.mockResolvedValue(KEYS); post.mockResolvedValue({ kid: 'k-pending', status: 'active' })
    const w = mountView(); await flushPromises()
    expect(w.find('[data-test="activate-k-pending"]').exists()).toBe(true)
    expect(w.find('[data-test="activate-k-active"]').exists()).toBe(false)
    await w.find('[data-test="activate-k-pending"]').trigger('click')
    await clickConfirm(w, en.admin.signingKeys.activateConfirm)
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/signing-keys/k-pending/activate')
  })
  it('retires only a decommissioning key and surfaces active_key_no_replacement', async () => {
    get.mockResolvedValue(KEYS); post.mockRejectedValue({ code: 'active_key_no_replacement', message: 'zh' })
    const w = mountView(); await flushPromises()
    expect(w.find('[data-test="retire-k-decom"]').exists()).toBe(true)
    expect(w.find('[data-test="retire-k-pending"]').exists()).toBe(false)
    await w.find('[data-test="retire-k-decom"]').trigger('click')
    await clickConfirm(w, en.admin.signingKeys.retireConfirm)
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/signing-keys/k-decom/retire')
    expect(w.text()).toContain(en.errors.active_key_no_replacement)
  })
})
```
Run → FAIL (component missing).

- [ ] **Step 2: Implement** `dashboard/src/pages/admin/AdminSigningKeysView.vue`:
```vue
<script setup lang="ts">
/** AdminSigningKeysView (/admin/signing-keys) — list + lifecycle (generate/activate/retire). */
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { withSudo } from '@/lib/sudo'
import { Button } from '@/components/ui/button'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/table'
import StatusBadge from '@/components/custom/StatusBadge.vue'
import ConfirmDialog from '@/components/custom/ConfirmDialog.vue'
import { formatDateTime } from '@/lib/time'

interface SigningKey {
  kid: string; algorithm: string; use: string; status: string
  publicJwk: Record<string, unknown>
  notBefore?: string; activatedAt?: string; decommissionedAt?: string; retireAfter?: string
}
type Variant = 'neutral' | 'success' | 'caution' | 'danger'

const { t, te } = useI18n()
const { busy, error, run } = useApi()

const rows = ref<SigningKey[]>([])
const expanded = ref<Record<string, boolean>>({})
const confirmGenerate = ref(false)
const confirmActivate = ref('')   // kid or ''
const confirmRetire = ref('')     // kid or ''

const errorText = computed(() => {
  const e = error.value
  if (!e) return ''
  const key = `errors.${e.code}`
  return te(key) ? t(key) : e.message || t('common.error')
})

const STATUS_VARIANT: Record<string, Variant> = { pending: 'neutral', active: 'success', decommissioning: 'caution', retired: 'neutral' }
function statusVariant(s: string): Variant { return STATUS_VARIANT[s] ?? 'neutral' }
function statusLabel(s: string): string {
  if (s === 'active') return t('admin.signingKeys.statusActive')
  if (s === 'pending') return t('admin.signingKeys.statusPending')
  if (s === 'decommissioning') return t('admin.signingKeys.statusDecommissioning')
  if (s === 'retired') return t('admin.signingKeys.statusRetired')
  return s
}
function toggle(kid: string): void { expanded.value = { ...expanded.value, [kid]: !expanded.value[kid] } }
function jwk(k: SigningKey): string { return JSON.stringify(k.publicJwk, null, 2) }

async function load(): Promise<void> {
  const res = await run(() => api.get<SigningKey[]>('/api/prohibitorum/signing-keys'))
  if (res) rows.value = res
}
async function generate(): Promise<void> {
  const ok = await run(() => withSudo(async () => { await api.post('/api/prohibitorum/signing-keys/generate'); return true as const }))
  confirmGenerate.value = false
  if (ok) await load()
}
async function activate(kid: string): Promise<void> {
  const ok = await run(() => withSudo(async () => { await api.post(`/api/prohibitorum/signing-keys/${kid}/activate`); return true as const }))
  confirmActivate.value = ''
  if (ok) await load()
}
async function retire(kid: string): Promise<void> {
  const ok = await run(() => withSudo(async () => { await api.post(`/api/prohibitorum/signing-keys/${kid}/retire`); return true as const }))
  confirmRetire.value = ''
  if (ok) await load()
}

onMounted(load)
</script>
<template>
  <div class="flex max-w-4xl flex-col gap-6">
    <div class="flex items-center justify-between gap-4">
      <h1 class="text-2xl font-semibold tracking-tight text-ink">{{ t('admin.signingKeys.title') }}</h1>
      <Button type="button" data-test="generate" @click="confirmGenerate = true">{{ t('admin.signingKeys.generate') }}</Button>
    </div>
    <Alert v-if="errorText" variant="destructive" role="alert" aria-live="polite"><AlertDescription>{{ errorText }}</AlertDescription></Alert>

    <Table v-if="rows.length">
      <TableHeader>
        <TableRow>
          <TableHead>{{ t('admin.signingKeys.colKid') }}</TableHead>
          <TableHead>{{ t('admin.signingKeys.colAlg') }}</TableHead>
          <TableHead>{{ t('admin.signingKeys.colStatus') }}</TableHead>
          <TableHead>{{ t('admin.signingKeys.colActivated') }}</TableHead>
          <TableHead></TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <template v-for="k in rows" :key="k.kid">
          <TableRow>
            <TableCell><button type="button" class="max-w-[18rem] truncate font-mono text-sm text-ink underline-offset-4 hover:underline" :data-test="`expand-${k.kid}`" @click="toggle(k.kid)">{{ k.kid }}</button></TableCell>
            <TableCell class="text-sm text-muted">{{ k.algorithm }} · {{ k.use }}</TableCell>
            <TableCell><StatusBadge :variant="statusVariant(k.status)">{{ statusLabel(k.status) }}</StatusBadge></TableCell>
            <TableCell class="text-sm text-muted">{{ formatDateTime(k.activatedAt) }}</TableCell>
            <TableCell>
              <div class="flex justify-end gap-2">
                <Button v-if="k.status === 'pending'" type="button" variant="outline" size="sm" :disabled="busy" :data-test="`activate-${k.kid}`" @click="confirmActivate = k.kid">{{ t('admin.signingKeys.activate') }}</Button>
                <Button v-if="k.status === 'decommissioning'" type="button" variant="outline" size="sm" :disabled="busy" :data-test="`retire-${k.kid}`" @click="confirmRetire = k.kid">{{ t('admin.signingKeys.retire') }}</Button>
              </div>
            </TableCell>
          </TableRow>
          <TableRow v-if="expanded[k.kid]">
            <TableCell colspan="5">
              <span class="text-xs text-muted">{{ t('admin.signingKeys.publicJwk') }}</span>
              <pre class="mt-1 overflow-x-auto rounded-md bg-sunken p-3 font-mono text-xs text-ink">{{ jwk(k) }}</pre>
            </TableCell>
          </TableRow>
        </template>
      </TableBody>
    </Table>
    <p v-else-if="!busy && !errorText" class="text-sm text-muted">{{ t('admin.signingKeys.empty') }}</p>

    <ConfirmDialog :open="confirmGenerate" :title="t('admin.signingKeys.generateTitle')" :confirm-label="t('admin.signingKeys.generateConfirm')" :busy="busy"
      @update:open="(v) => { if (!v) confirmGenerate = false }" @cancel="confirmGenerate = false" @confirm="generate">
      {{ t('admin.signingKeys.generateBody') }}
    </ConfirmDialog>
    <ConfirmDialog :open="!!confirmActivate" :title="t('admin.signingKeys.activateTitle')" :confirm-label="t('admin.signingKeys.activateConfirm')" :busy="busy"
      @update:open="(v) => { if (!v) confirmActivate = '' }" @cancel="confirmActivate = ''" @confirm="activate(confirmActivate)">
      {{ t('admin.signingKeys.activateBody') }}
    </ConfirmDialog>
    <ConfirmDialog :open="!!confirmRetire" :title="t('admin.signingKeys.retireTitle')" :confirm-label="t('admin.signingKeys.retireConfirm')" :busy="busy"
      @update:open="(v) => { if (!v) confirmRetire = '' }" @cancel="confirmRetire = ''" @confirm="retire(confirmRetire)">
      {{ t('admin.signingKeys.retireBody') }}
    </ConfirmDialog>
  </div>
</template>
```
NOTE: `ConfirmDialog`'s confirm button uses `variant="destructive"` (the `bg-destructive` class the test keys on). That's fine for activate/retire/generate even though generate isn't destructive — it's the shared confirm primitive; consequence text carries the meaning. If a reviewer objects to a red Generate button, that's a polish note, not a blocker.

- [ ] **Step 3: Run the test → PASS.** `cd dashboard && npx vitest run src/pages/admin/AdminSigningKeysView.test.ts`.

- [ ] **Step 4: Commit.**
```bash
git add dashboard/src/pages/admin/AdminSigningKeysView.vue dashboard/src/pages/admin/AdminSigningKeysView.test.ts
git commit -m "feat(web): admin signing-keys list + lifecycle (generate/activate/retire)"
```

---

### Task 5: AdminAuditView — filters + load-more + expandable detail

**Goal:** `/admin/audit` shows a newest-first audit table with filter inputs, keyset "Load more" paging (via `before=<lastId>`), and per-row expandable pretty-printed `detail`.

**Files:**
- Create: `dashboard/src/pages/admin/AdminAuditView.vue`
- Test: `dashboard/src/pages/admin/AdminAuditView.test.ts`

**Acceptance Criteria:**
- [ ] Initial load `GET /audit-events?limit=50` (newest-first); renders rows (time/factor/event/account/ip).
- [ ] Filters (factor/event/accountId/since/until) → Apply re-queries from the top with the filter params; Clear empties them.
- [ ] "Load more" sends `before=<id of last row>` (+ active filters) and appends; hidden when a page returns `< limit` rows.
- [ ] Each row expands to show `detail` as pretty JSON + userAgent; empty detail → "—".
- [ ] Empty result → empty-state text.

**Verify:** `cd dashboard && npx vitest run src/pages/admin/AdminAuditView.test.ts` → PASS.

**Steps:**

- [ ] **Step 1: Write the failing test** `dashboard/src/pages/admin/AdminAuditView.test.ts`:
```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'
const get = vi.mocked(api.get)
import AdminAuditView from './AdminAuditView.vue'

const page = (startId: number, n: number) => Array.from({ length: n }, (_, i) => ({
  id: startId - i, at: '2026-01-01T00:00:00Z', accountId: 7, factor: 'signing_key', event: 'activate',
  ip: '10.0.0.1', userAgent: 'curl', detail: { kid: `k${startId - i}`, action: 'activate' },
}))
const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
const mountView = () => mount(AdminAuditView, { global: { plugins: [i18n()] }, attachTo: document.body })
beforeEach(() => { get.mockReset() })

describe('AdminAuditView', () => {
  it('loads newest-first with limit=50', async () => {
    get.mockResolvedValue(page(100, 3))
    const w = mountView(); await flushPromises()
    expect(get).toHaveBeenCalledWith('/api/prohibitorum/audit-events?limit=50')
    expect(w.text()).toContain('activate')
  })
  it('applies filters and re-queries from the top', async () => {
    get.mockResolvedValue(page(100, 2))
    const w = mountView(); await flushPromises()
    await w.find('input[name="factor"]').setValue('signing_key')
    await w.find('input[name="event"]').setValue('activate')
    await w.find('[data-test="apply"]').trigger('click'); await flushPromises()
    const lastCall = get.mock.calls.at(-1)![0] as string
    expect(lastCall).toContain('factor=signing_key'); expect(lastCall).toContain('event=activate'); expect(lastCall).toContain('limit=50')
    expect(lastCall).not.toContain('before=')
  })
  it('load-more sends before=<lastId> and appends; hides when short page', async () => {
    get.mockResolvedValueOnce(page(100, 50)).mockResolvedValueOnce(page(50, 3))
    const w = mountView(); await flushPromises()
    expect(w.find('[data-test="load-more"]').exists()).toBe(true)
    await w.find('[data-test="load-more"]').trigger('click'); await flushPromises()
    expect((get.mock.calls.at(-1)![0] as string)).toContain('before=51')  // last id of first page = 100-49 = 51
    expect(w.find('[data-test="load-more"]').exists()).toBe(false)         // 2nd page had 3 (<50)
  })
  it('expands a row to show detail JSON', async () => {
    get.mockResolvedValue(page(100, 1))
    const w = mountView(); await flushPromises()
    await w.find('[data-test="expand-100"]').trigger('click')
    expect(w.text()).toContain('"action": "activate"')
  })
  it('shows empty-state when no events', async () => {
    get.mockResolvedValue([])
    const w = mountView(); await flushPromises()
    expect(w.text()).toContain(en.admin.audit.empty)
  })
})
```
Run → FAIL (component missing).

- [ ] **Step 2: Implement** `dashboard/src/pages/admin/AdminAuditView.vue`:
```vue
<script setup lang="ts">
/** AdminAuditView (/admin/audit) — filterable, keyset-paginated audit log. */
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/table'
import { formatDateTime } from '@/lib/time'

interface AuditEvent {
  id: number; at: string; accountId?: number; factor: string; event: string
  ip?: string; userAgent?: string; detail?: Record<string, unknown>
}
const LIMIT = 50

const { t, te } = useI18n()
const { busy, error, run } = useApi()

const rows = ref<AuditEvent[]>([])
const hasMore = ref(false)
const expanded = ref<Record<number, boolean>>({})

const factor = ref(''); const event = ref(''); const accountId = ref('')
const since = ref(''); const until = ref('')

const errorText = computed(() => {
  const e = error.value
  if (!e) return ''
  const key = `errors.${e.code}`
  return te(key) ? t(key) : e.message || t('common.error')
})

function buildQuery(before?: number): string {
  const p = new URLSearchParams()
  if (factor.value.trim()) p.set('factor', factor.value.trim())
  if (event.value.trim()) p.set('event', event.value.trim())
  if (accountId.value.trim()) p.set('accountId', accountId.value.trim())
  if (since.value) p.set('since', new Date(since.value).toISOString())
  if (until.value) p.set('until', new Date(until.value).toISOString())
  if (before !== undefined) p.set('before', String(before))
  p.set('limit', String(LIMIT))
  return `/api/prohibitorum/audit-events?${p.toString()}`
}

async function fetchPage(before?: number): Promise<AuditEvent[] | undefined> {
  return run(() => api.get<AuditEvent[]>(buildQuery(before)))
}

async function reload(): Promise<void> {
  expanded.value = {}
  const res = await fetchPage()
  if (res) { rows.value = res; hasMore.value = res.length === LIMIT }
}
async function loadMore(): Promise<void> {
  const last = rows.value.at(-1)
  if (!last) return
  const res = await fetchPage(last.id)
  if (res) { rows.value = [...rows.value, ...res]; hasMore.value = res.length === LIMIT }
}
function clearFilters(): void {
  factor.value = ''; event.value = ''; accountId.value = ''; since.value = ''; until.value = ''
  void reload()
}
function toggle(id: number): void { expanded.value = { ...expanded.value, [id]: !expanded.value[id] } }
function detailText(e: AuditEvent): string {
  return e.detail && Object.keys(e.detail).length ? JSON.stringify(e.detail, null, 2) : '—'
}

onMounted(reload)
</script>
<template>
  <div class="flex max-w-5xl flex-col gap-6">
    <h1 class="text-2xl font-semibold tracking-tight text-ink">{{ t('admin.audit.title') }}</h1>
    <Alert v-if="errorText" variant="destructive" role="alert" aria-live="polite"><AlertDescription>{{ errorText }}</AlertDescription></Alert>

    <div class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-5">
      <div class="flex flex-col gap-1.5">
        <Label for="factor">{{ t('admin.audit.filterFactor') }}</Label>
        <Input id="factor" name="factor" v-model="factor" autocomplete="off" />
      </div>
      <div class="flex flex-col gap-1.5">
        <Label for="event">{{ t('admin.audit.filterEvent') }}</Label>
        <Input id="event" name="event" v-model="event" autocomplete="off" />
      </div>
      <div class="flex flex-col gap-1.5">
        <Label for="accountId">{{ t('admin.audit.filterAccount') }}</Label>
        <Input id="accountId" name="accountId" v-model="accountId" inputmode="numeric" autocomplete="off" />
      </div>
      <div class="flex flex-col gap-1.5">
        <Label for="since">{{ t('admin.audit.filterSince') }}</Label>
        <Input id="since" name="since" type="datetime-local" v-model="since" />
      </div>
      <div class="flex flex-col gap-1.5">
        <Label for="until">{{ t('admin.audit.filterUntil') }}</Label>
        <Input id="until" name="until" type="datetime-local" v-model="until" />
      </div>
    </div>
    <div class="flex gap-2">
      <Button type="button" :disabled="busy" data-test="apply" @click="reload">{{ t('admin.audit.apply') }}</Button>
      <Button type="button" variant="outline" :disabled="busy" data-test="clear" @click="clearFilters">{{ t('admin.audit.clear') }}</Button>
    </div>

    <Table v-if="rows.length">
      <TableHeader>
        <TableRow>
          <TableHead>{{ t('admin.audit.colTime') }}</TableHead>
          <TableHead>{{ t('admin.audit.colFactor') }}</TableHead>
          <TableHead>{{ t('admin.audit.colEvent') }}</TableHead>
          <TableHead>{{ t('admin.audit.colAccount') }}</TableHead>
          <TableHead>{{ t('admin.audit.colIp') }}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <template v-for="e in rows" :key="e.id">
          <TableRow class="cursor-pointer" tabindex="0" :data-test="`expand-${e.id}`"
                    @click="toggle(e.id)" @keydown.enter="toggle(e.id)" @keydown.space.prevent="toggle(e.id)">
            <TableCell class="text-sm text-muted">{{ formatDateTime(e.at) }}</TableCell>
            <TableCell class="text-sm text-ink">{{ e.factor }}</TableCell>
            <TableCell class="text-sm text-ink">{{ e.event }}</TableCell>
            <TableCell class="text-sm text-muted">{{ e.accountId ?? '—' }}</TableCell>
            <TableCell class="text-sm text-muted">{{ e.ip || '—' }}</TableCell>
          </TableRow>
          <TableRow v-if="expanded[e.id]">
            <TableCell colspan="5">
              <span class="text-xs text-muted">{{ t('admin.audit.detail') }}</span>
              <pre class="mt-1 overflow-x-auto rounded-md bg-sunken p-3 font-mono text-xs text-ink">{{ detailText(e) }}</pre>
              <p v-if="e.userAgent" class="mt-2 text-xs text-muted">{{ t('admin.audit.userAgent') }}: {{ e.userAgent }}</p>
            </TableCell>
          </TableRow>
        </template>
      </TableBody>
    </Table>
    <p v-else-if="!busy && !errorText" class="text-sm text-muted">{{ t('admin.audit.empty') }}</p>

    <Button v-if="hasMore" type="button" variant="outline" class="w-fit" :disabled="busy" data-test="load-more" @click="loadMore">{{ t('admin.audit.loadMore') }}</Button>
  </div>
</template>
```

- [ ] **Step 3: Run the test → PASS.** `cd dashboard && npx vitest run src/pages/admin/AdminAuditView.test.ts`.

- [ ] **Step 4: Commit.**
```bash
git add dashboard/src/pages/admin/AdminAuditView.vue dashboard/src/pages/admin/AdminAuditView.test.ts
git commit -m "feat(web): admin audit-log viewer — filters, load-more, expandable detail"
```

---

## Finishing (coordinator steps — handled by subagent-driven-development's review + finishing flow)

1. **Per-task review:** each task gets a spec-compliance review then a code-quality review (the established two-stage bar). Fix findings before moving on.
2. **Final whole-cycle review (opus):** read all five views together for cross-cutting issues (error handling, sudo coverage, status-gating, keyset paging edge cases, secret never echoed, a11y of expand rows). Fix any Critical/High.
3. **Done-gate (run from repo root, all GREEN):**
```bash
go build ./... && go vet ./...                 # exit 0 (authoritative over gopls)
cd dashboard && npx vue-tsc --noEmit && npx vitest run   # type-check + full suite
cd /home/tundra/projects/tundra/prohibitorum && go run ./cmd/smoke   # or the detached runner; expect SMOKE_EXIT=0
cd dashboard && npm run build && cd .. && git add pkg/webui/dist && git commit -m "build(web): rebuild embedded dist for 3c admin pages"
```
   (Discard reviewers' incidental dist dirt between source commits with `git checkout -- pkg/webui/dist`.)
4. **Folded-in live visual review:** `mise dev-server` + `mise enroll-admin` + `mise dev-seed`; hand the user a reload-and-react checklist (the three 3c pages + a sweep of login/recovery, dashboard, security, connected, devices, admin accounts/invitations/OIDC/SAML).
5. **Memory + handoff:** update `project_current_state.md` + `MEMORY.md` index line; write `docs/superpowers/notes/2026-06-08-frontend-3c-...-DONE-handoff.md`.

---

## Self-review (against the spec)

- **Spec coverage:** Section A (upstream IdPs) → Tasks 2–3; Section B (signing keys) → Task 4; Section C (audit) → Task 5; cross-cutting (routes/sidebar/i18n/errors/guard) → Task 1; testing + done-gate + visual review → Finishing. All covered.
- **Type consistency:** `UpstreamIdp` exported from `AdminUpstreamIdpsView.vue`, imported as `type` in the detail view. `SigningKey`/`AuditEvent` local to their views. `StatusBadge` variants limited to neutral/success/caution/danger (retired→neutral, matching the spec correction). i18n keys referenced in tests (`en.admin.upstream.*`, `en.admin.signingKeys.*`, `en.admin.audit.*`, `en.errors.*`) all defined in Task 1.
- **No placeholders:** every step ships complete code/commands. The one judgment call (delete-confirm selector in Task 3) is flagged with a concrete fallback.
- **Keyset edge case:** `hasMore` is `res.length === LIMIT`; `loadMore` uses `rows.at(-1).id` as `before`. Test asserts the exact `before=51` for a 50-row first page (ids 100..51).
```
