# Frontend Spec 2c — Connected Accounts + Devices Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers-extended-cc:subagent-driven-development (recommended) or superpowers-extended-cc:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the `/connected` (federated-identity management), `/devices` (device approval), and public `/pair` (new-device pairing) pages, wired to existing backend endpoints, plus a `/login` entry link, sidebar nav, and i18n.

**Architecture:** Three Vue 3 `<script setup>` views following the established 2a/2b patterns — `useApi` for busy/error, `withSudo`/`ensureSudo` for the sudo gate, `errors.<code>` i18n mapping, `data-test` hooks. No backend changes; all contracts are already live. `/connected` + `/devices` are `requiresAuth` children of `DashboardLayout`; `/pair` is a public threshold page on `CenteredLayout` with a begin→poll→complete state machine.

**Tech Stack:** Vue 3, Vue Router, vue-i18n, Vitest + @vue/test-utils, Tailwind v4 + shadcn-vue primitives, `@simplewebauthn/browser` (via `useWebauthn`).

**Spec:** `docs/superpowers/specs/2026-06-08-frontend-2c-connected-devices-design.md`

**Conventions that bite (from prior slices):**
- Run frontend tooling from `dashboard/` with `mise exec -- npm …`. cwd can reset to repo root between tool calls.
- The binary embeds the **committed** `pkg/webui/dist` via go:embed; Vite chunk hashes are non-deterministic. For source-only intermediate commits, after any verify build run `git checkout -- pkg/webui/dist && git clean -fq pkg/webui/dist`. Rebuild + commit dist **once** at the done-gate (Task 5).
- Keep apostrophes curly (U+2019) in `en.ts`.
- Tests do **not** mock `withSudo` for happy paths — they let `api.post` resolve, so `withSudo` passes straight through. Only the proactive-`ensureSudo` link flow mocks `@/lib/sudo`.

---

### Task 1: ConnectedAccountsView (`/connected`)

**Goal:** List linked federated identities, unlink one (sudo-gated, confirmed), and link a new provider (proactive sudo → hard redirect).

**Files:**
- Create: `dashboard/src/pages/ConnectedAccountsView.vue`
- Create: `dashboard/src/pages/ConnectedAccountsView.test.ts`
- Modify: `dashboard/src/locales/en.ts` (add `connected:` block + `errors.last_sign_in_method`, `errors.credential_not_found`)

**Acceptance Criteria:**
- [ ] Lists identities from `GET /api/prohibitorum/me/identities` (provider name, optional upstream email, linked date).
- [ ] Empty state when the list is `[]`.
- [ ] Unlink → `ConfirmDialog` → `withSudo(POST /me/identities/{id}/unlink)` → list refreshes; `last_sign_in_method` / `credential_not_found` surface via `errors.<code>`.
- [ ] Link picker lists `GET /auth/federation` providers, disables already-linked slugs, and on select calls `ensureSudo()` then `hardRedirect('/api/prohibitorum/me/identities/link/{slug}/begin?return_to=/connected')` (no redirect when sudo is cancelled).

**Verify:** `cd dashboard && mise exec -- npm run test -- ConnectedAccountsView` → all pass.

**Steps:**

- [ ] **Step 1: Add i18n strings.** In `dashboard/src/locales/en.ts`, add a `connected:` block (insert after the `sessions:` block) and two `errors` entries (inside the existing `errors:` object):

```ts
  connected: {
    title: 'Connected accounts',
    help: 'Sign in using accounts from other identity providers.',
    empty: 'You haven’t connected any accounts yet.',
    linked: 'Linked',
    unlink: 'Disconnect',
    unlinkConfirmTitle: 'Disconnect this account?',
    unlinkConfirmBody: 'You will no longer be able to sign in using this provider.',
    linkHeading: 'Connect an account',
    linkHelp: 'Add another identity provider you can sign in with.',
    alreadyLinked: 'Connected',
    noProviders: 'No identity providers are available to connect.',
  },
```

In the `errors:` object add:

```ts
    last_sign_in_method: 'You can’t remove your last sign-in method. Add another first.',
    credential_not_found: 'That connection no longer exists.',
```

- [ ] **Step 2: Write the failing test.** Create `dashboard/src/pages/ConnectedAccountsView.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'
const get = vi.mocked(api.get)
const post = vi.mocked(api.post)

const { ensureSudo, hardRedirect } = vi.hoisted(() => ({
  ensureSudo: vi.fn(async () => true),
  hardRedirect: vi.fn(),
}))
// Real withSudo passthrough so the unlink happy path calls api.post directly.
vi.mock('@/lib/sudo', () => ({
  ensureSudo,
  withSudo: (fn: () => Promise<unknown>) => fn(),
}))
vi.mock('@/lib/navigate', () => ({ hardRedirect }))

import ConnectedAccountsView from './ConnectedAccountsView.vue'

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
const mountView = () => mount(ConnectedAccountsView, { global: { plugins: [i18n()] }, attachTo: document.body })

const IDENTITIES = [
  { id: 1, idpSlug: 'okta', idpDisplayName: 'Okta', upstreamEmail: 'a@example.com', linkedAt: '2026-01-01T00:00:00Z' },
  { id: 2, idpSlug: 'ad', idpDisplayName: 'Azure AD', upstreamEmail: null, linkedAt: '2026-02-01T00:00:00Z' },
]
const PROVIDERS = [
  { slug: 'okta', displayName: 'Okta' },
  { slug: 'google', displayName: 'Google' },
]

// /me/identities and /auth/federation are both GETs; route by path.
function mockGets(identities = IDENTITIES, providers = PROVIDERS) {
  get.mockImplementation(async (p: string) =>
    p.includes('/me/identities') ? identities : providers)
}

beforeEach(() => {
  get.mockReset(); post.mockReset(); ensureSudo.mockReset(); hardRedirect.mockReset()
  ensureSudo.mockResolvedValue(true)
})

// ConfirmDialog (reka-ui) teleports to document.body and its confirm button has
// no data-test hook — it's the destructive-variant button carrying the unlink
// label. The row's own unlink button is variant="outline", so filtering by
// data-variant="destructive" uniquely finds the dialog confirm. Mirrors
// SecurityView.test.ts.
function clickConfirm() {
  const confirmBtns = Array.from(document.body.querySelectorAll('button'))
    .filter((b) => b.getAttribute('data-variant') === 'destructive'
      && b.textContent?.includes(en.connected.unlink))
  confirmBtns[confirmBtns.length - 1]!.click()
}

describe('ConnectedAccountsView', () => {
  it('lists linked identities with provider name and upstream email', async () => {
    mockGets()
    const w = mountView(); await flushPromises()
    expect(w.text()).toContain('Okta')
    expect(w.text()).toContain('a@example.com')
    expect(w.text()).toContain('Azure AD')
  })

  it('shows empty state when no identities are linked', async () => {
    mockGets([], PROVIDERS)
    const w = mountView(); await flushPromises()
    expect(w.text()).toContain(en.connected.empty)
  })

  it('unlinks an identity (confirm → post → refresh)', async () => {
    mockGets()
    post.mockResolvedValue(undefined)
    const w = mountView(); await flushPromises()
    await w.find('[data-test="unlink-1"]').trigger('click'); await flushPromises()
    clickConfirm(); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/identities/1/unlink')
    // refresh: identities fetched on mount + after unlink
    expect(get.mock.calls.filter((c) => String(c[0]).includes('/me/identities'))).toHaveLength(2)
  })

  it('surfaces last_sign_in_method on unlink failure', async () => {
    mockGets()
    post.mockRejectedValue({ code: 'last_sign_in_method', message: 'zh' })
    const w = mountView(); await flushPromises()
    await w.find('[data-test="unlink-1"]').trigger('click'); await flushPromises()
    clickConfirm(); await flushPromises()
    expect(w.text()).toContain(en.errors.last_sign_in_method)
  })

  it('disables already-linked providers in the link picker', async () => {
    mockGets()
    const w = mountView(); await flushPromises()
    // okta is linked → disabled; google is not → enabled
    expect(w.find('[data-test="link-okta"]').attributes('disabled')).toBeDefined()
    expect(w.find('[data-test="link-google"]').attributes('disabled')).toBeUndefined()
  })

  it('link → ensureSudo then hardRedirect to begin', async () => {
    mockGets()
    const w = mountView(); await flushPromises()
    await w.find('[data-test="link-google"]').trigger('click'); await flushPromises()
    expect(ensureSudo).toHaveBeenCalledOnce()
    expect(hardRedirect).toHaveBeenCalledWith(
      '/api/prohibitorum/me/identities/link/google/begin?return_to=/connected')
  })

  it('does not redirect when sudo is cancelled', async () => {
    mockGets()
    ensureSudo.mockResolvedValue(false)
    const w = mountView(); await flushPromises()
    await w.find('[data-test="link-google"]').trigger('click'); await flushPromises()
    expect(hardRedirect).not.toHaveBeenCalled()
  })
})
```

- [ ] **Step 2b: Run the test to confirm it fails.**

Run: `cd dashboard && mise exec -- npm run test -- ConnectedAccountsView`
Expected: FAIL — cannot resolve `./ConnectedAccountsView.vue`.

- [ ] **Step 3: Implement the view.** Create `dashboard/src/pages/ConnectedAccountsView.vue`:

```vue
<script setup lang="ts">
/**
 * ConnectedAccountsView (/connected) — manage federated identities.
 * GET /me/identities lists links; unlink is sudo-gated + confirmed; linking a
 * new provider needs a PROACTIVE sudo step (the begin endpoint is a sudo-gated
 * 302 that withSudo's XHR-retry can't replay), then a hard redirect upstream.
 */
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { withSudo, ensureSudo } from '@/lib/sudo'
import { hardRedirect } from '@/lib/navigate'
import { Link2 } from 'lucide-vue-next'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Alert, AlertDescription } from '@/components/ui/alert'
import ConfirmDialog from '@/components/custom/ConfirmDialog.vue'

interface Identity {
  id: number
  idpSlug: string
  idpDisplayName: string
  upstreamEmail: string | null
  linkedAt: string
}
interface Provider { slug: string; displayName: string }

const { t, te } = useI18n()
const { busy, error, run } = useApi()

const identities = ref<Identity[]>([])
const providers = ref<Provider[]>([])
const confirmId = ref<number | null>(null)

const fmt = (d: string) => { const ms = Date.parse(d); return Number.isNaN(ms) ? '' : new Date(ms).toLocaleDateString() }
const errorText = computed(() => {
  const e = error.value
  if (!e) return ''
  const key = `errors.${e.code}`
  return te(key) ? t(key) : e.message || t('common.error')
})
const linkedSlugs = computed(() => new Set(identities.value.map((i) => i.idpSlug)))

async function loadIdentities(): Promise<void> {
  const res = await run(() => api.get<Identity[]>('/api/prohibitorum/me/identities'))
  if (res) identities.value = res
}
async function loadProviders(): Promise<void> {
  try {
    providers.value = await api.get<Provider[]>('/api/prohibitorum/auth/federation')
  } catch {
    providers.value = []
  }
}
async function confirmUnlink(): Promise<void> {
  const id = confirmId.value
  if (id == null) return
  const ok = await run(() => withSudo(async () => {
    await api.post(`/api/prohibitorum/me/identities/${id}/unlink`)
    return true as const
  }))
  confirmId.value = null
  if (ok) await loadIdentities()
}
async function link(slug: string): Promise<void> {
  const elevated = await ensureSudo()
  if (!elevated) return
  hardRedirect(
    `/api/prohibitorum/me/identities/link/${encodeURIComponent(slug)}/begin?return_to=/connected`)
}

onMounted(async () => { await Promise.all([loadIdentities(), loadProviders()]) })
</script>

<template>
  <div class="flex max-w-2xl flex-col gap-6">
    <h1 class="text-2xl font-semibold tracking-tight text-ink">{{ t('connected.title') }}</h1>
    <p class="text-sm text-muted">{{ t('connected.help') }}</p>

    <Alert v-if="errorText" variant="destructive" role="alert" aria-live="polite">
      <AlertDescription>{{ errorText }}</AlertDescription>
    </Alert>

    <Card v-for="ident in identities" :key="ident.id">
      <CardContent class="flex items-center justify-between gap-4 py-4">
        <div class="flex min-w-0 flex-1 flex-col gap-1 text-sm">
          <span class="min-w-0 truncate font-medium text-ink">{{ ident.idpDisplayName }}</span>
          <span v-if="ident.upstreamEmail" class="min-w-0 truncate text-muted">{{ ident.upstreamEmail }}</span>
          <span v-if="fmt(ident.linkedAt)" class="truncate text-muted">{{ t('connected.linked') }}: {{ fmt(ident.linkedAt) }}</span>
        </div>
        <Button variant="outline" size="sm" class="shrink-0" :disabled="busy"
                :data-test="`unlink-${ident.id}`" @click="confirmId = ident.id">
          {{ t('connected.unlink') }}
        </Button>
      </CardContent>
    </Card>

    <p v-if="!busy && identities.length === 0 && !errorText" class="text-sm text-muted">
      {{ t('connected.empty') }}
    </p>

    <Card>
      <CardHeader>
        <CardTitle class="flex items-center gap-2">
          <Link2 class="size-4 shrink-0" aria-hidden="true" />
          {{ t('connected.linkHeading') }}
        </CardTitle>
      </CardHeader>
      <CardContent class="flex flex-col gap-3">
        <p class="text-sm text-muted">{{ t('connected.linkHelp') }}</p>
        <p v-if="providers.length === 0" class="text-sm text-muted">{{ t('connected.noProviders') }}</p>
        <div v-else class="flex flex-col gap-2">
          <Button v-for="p in providers" :key="p.slug" type="button" variant="outline" class="w-full justify-between"
                  :disabled="linkedSlugs.has(p.slug) || busy" :data-test="`link-${p.slug}`" @click="link(p.slug)">
            <span>{{ p.displayName }}</span>
            <span v-if="linkedSlugs.has(p.slug)" class="text-xs text-muted">{{ t('connected.alreadyLinked') }}</span>
          </Button>
        </div>
      </CardContent>
    </Card>

    <ConfirmDialog
      :open="confirmId !== null"
      :title="t('connected.unlinkConfirmTitle')"
      :confirm-label="t('connected.unlink')"
      :busy="busy"
      @update:open="(v) => { if (!v) confirmId = null }"
      @cancel="confirmId = null"
      @confirm="confirmUnlink"
    >
      {{ t('connected.unlinkConfirmBody') }}
    </ConfirmDialog>
  </div>
</template>
```

Note for the implementer: `ConfirmDialog` (read `components/custom/ConfirmDialog.vue`) renders its confirm action as a `variant="destructive"` `Button` showing `confirmLabel`, teleported into `document.body`. The test's `clickConfirm()` helper finds it there. The row's own unlink button is `variant="outline"`, so the destructive filter is unambiguous. Do NOT add a `data-test` to the shared `ConfirmDialog`.

- [ ] **Step 4: Run the test to confirm it passes.**

Run: `cd dashboard && mise exec -- npm run test -- ConnectedAccountsView`
Expected: PASS (all cases).

- [ ] **Step 5: Commit (source only — keep dist out).**

```bash
cd /home/tundra/projects/tundra/prohibitorum
git checkout -- pkg/webui/dist 2>/dev/null; git clean -fq pkg/webui/dist 2>/dev/null
git add dashboard/src/pages/ConnectedAccountsView.vue dashboard/src/pages/ConnectedAccountsView.test.ts dashboard/src/locales/en.ts
git commit -m "feat(web): ConnectedAccountsView (/connected) — list, unlink, link"
```

---

### Task 2: DevicesView (`/devices`, approver side)

**Goal:** Enter a pairing code, look it up, review initiator context, and approve (sudo) or cancel.

**Files:**
- Create: `dashboard/src/pages/DevicesView.vue`
- Create: `dashboard/src/pages/DevicesView.test.ts`
- Modify: `dashboard/src/locales/en.ts` (add `devices:` block + `errors.pairing_not_found`, `errors.pairing_expired`, `errors.pairing_not_approved`, `errors.pairing_state`)

**Acceptance Criteria:**
- [ ] Code input → `GET /me/devices/pair/lookup?code=` → confirmation card with initiator UA / IP / created / expires + echoed `displayCode`.
- [ ] Approve → `withSudo(POST /me/devices/pair/approve {code})` → success state.
- [ ] Cancel → `POST /me/devices/pair/cancel {code}` → back to entry.
- [ ] `alreadyBound` shows an "already approved" note with no Approve button.
- [ ] `pairing_not_found` / `rate_limited` map via `errors.<code>`.

**Verify:** `cd dashboard && mise exec -- npm run test -- DevicesView` → all pass.

**Steps:**

- [ ] **Step 1: Add i18n strings.** In `dashboard/src/locales/en.ts`, add a `devices:` block (after `connected:`) and the four `errors` entries:

```ts
  devices: {
    title: 'Devices',
    help: 'Approve a new device that’s trying to sign in to your account.',
    codeLabel: 'Pairing code',
    codePlaceholder: 'XXXX-XXXX',
    lookup: 'Look up',
    confirmTitle: 'Approve this device?',
    requestedFrom: 'Requested from',
    ipAddress: 'IP address',
    started: 'Started',
    expires: 'Expires',
    approve: 'Approve device',
    cancel: 'Cancel',
    approved: 'Device approved. It will be signed in shortly.',
    alreadyBound: 'You’ve already approved this device.',
  },
```

In the `errors:` object add:

```ts
    pairing_not_found: 'That code is invalid, used, or expired.',
    pairing_expired: 'That code has expired. Ask the device to generate a new one.',
    pairing_not_approved: 'This device hasn’t been approved yet.',
    pairing_state: 'That pairing can’t be changed right now.',
```

- [ ] **Step 2: Write the failing test.** Create `dashboard/src/pages/DevicesView.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'
vi.mock('@/lib/sudo', () => ({ withSudo: (fn: () => Promise<unknown>) => fn() }))
const get = vi.mocked(api.get)
const post = vi.mocked(api.post)

import DevicesView from './DevicesView.vue'

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
const mountView = () => mount(DevicesView, { global: { plugins: [i18n()] }, attachTo: document.body })

const LOOKUP = {
  pairingId: 'p1', displayCode: 'AB12-CD34', initiatorUa: 'Chrome on Mac', initiatorIp: '10.0.0.9',
  createdAt: '2026-01-01T00:00:00Z', expiresAt: '2026-01-01T00:10:00Z', alreadyBound: false,
}
beforeEach(() => { get.mockReset(); post.mockReset() })

async function enterCodeAndLookup(w: ReturnType<typeof mountView>, code = 'AB12-CD34') {
  await w.find('input[name="code"]').setValue(code)
  await w.find('[data-test="lookup"]').trigger('click')
  await flushPromises()
}

describe('DevicesView', () => {
  it('looks up a code and shows the confirmation card', async () => {
    get.mockResolvedValue(LOOKUP)
    const w = mountView()
    await enterCodeAndLookup(w)
    expect(get).toHaveBeenCalledWith('/api/prohibitorum/me/devices/pair/lookup?code=AB12-CD34')
    expect(w.text()).toContain('Chrome on Mac')
    expect(w.text()).toContain('10.0.0.9')
    expect(w.text()).toContain('AB12-CD34')
    expect(w.find('[data-test="approve"]').exists()).toBe(true)
  })

  it('approves the device', async () => {
    get.mockResolvedValue(LOOKUP)
    post.mockResolvedValue(undefined)
    const w = mountView()
    await enterCodeAndLookup(w)
    await w.find('[data-test="approve"]').trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/devices/pair/approve', { code: 'AB12-CD34' })
    expect(w.text()).toContain(en.devices.approved)
  })

  it('cancels the pairing and returns to entry', async () => {
    get.mockResolvedValue(LOOKUP)
    post.mockResolvedValue(undefined)
    const w = mountView()
    await enterCodeAndLookup(w)
    await w.find('[data-test="cancel"]').trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/devices/pair/cancel', { code: 'AB12-CD34' })
    expect(w.find('[data-test="approve"]').exists()).toBe(false)
    expect(w.find('input[name="code"]').exists()).toBe(true)
  })

  it('shows an already-approved note with no approve button', async () => {
    get.mockResolvedValue({ ...LOOKUP, alreadyBound: true })
    const w = mountView()
    await enterCodeAndLookup(w)
    expect(w.text()).toContain(en.devices.alreadyBound)
    expect(w.find('[data-test="approve"]').exists()).toBe(false)
  })

  it('surfaces pairing_not_found on a bad code', async () => {
    get.mockRejectedValue({ code: 'pairing_not_found', message: 'zh' })
    const w = mountView()
    await enterCodeAndLookup(w, 'ZZZZ-ZZZZ')
    expect(w.text()).toContain(en.errors.pairing_not_found)
  })
})
```

- [ ] **Step 2b: Run the test to confirm it fails.**

Run: `cd dashboard && mise exec -- npm run test -- DevicesView`
Expected: FAIL — cannot resolve `./DevicesView.vue`.

- [ ] **Step 3: Implement the view.** Create `dashboard/src/pages/DevicesView.vue`:

```vue
<script setup lang="ts">
/**
 * DevicesView (/devices) — approve a new device by its pairing code.
 * lookup (not sudo) shows the initiator context; approve is sudo-gated; cancel
 * drops the pairing. The lookup → confirm → approve sequence IS the
 * confirmation, so there is no extra ConfirmDialog here.
 */
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { withSudo } from '@/lib/sudo'
import { MonitorSmartphone } from 'lucide-vue-next'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Alert, AlertDescription } from '@/components/ui/alert'
import CodeField from '@/components/custom/CodeField.vue'

interface Lookup {
  pairingId: string
  displayCode: string
  initiatorUa: string
  initiatorIp: string
  createdAt: string
  expiresAt: string
  alreadyBound: boolean
}

const { t, te } = useI18n()
const { busy, error, run } = useApi()

const code = ref('')
const found = ref<Lookup | null>(null)
const approved = ref(false)

const fmt = (d: string) => { const ms = Date.parse(d); return Number.isNaN(ms) ? '' : new Date(ms).toLocaleString() }
const errorText = computed(() => {
  const e = error.value
  if (!e) return ''
  const key = `errors.${e.code}`
  return te(key) ? t(key) : e.message || t('common.error')
})

async function lookup(): Promise<void> {
  approved.value = false
  const c = code.value.trim()
  if (!c) return
  const res = await run(() => api.get<Lookup>(
    `/api/prohibitorum/me/devices/pair/lookup?code=${encodeURIComponent(c)}`))
  if (res) found.value = res
}
async function approve(): Promise<void> {
  const c = code.value.trim()
  const ok = await run(() => withSudo(async () => {
    await api.post('/api/prohibitorum/me/devices/pair/approve', { code: c })
    return true as const
  }))
  if (ok) { approved.value = true; found.value = null }
}
async function cancel(): Promise<void> {
  const c = code.value.trim()
  await run(async () => {
    await api.post('/api/prohibitorum/me/devices/pair/cancel', { code: c })
    return true as const
  })
  found.value = null
  code.value = ''
}
</script>

<template>
  <div class="flex max-w-2xl flex-col gap-6">
    <h1 class="text-2xl font-semibold tracking-tight text-ink">{{ t('devices.title') }}</h1>
    <p class="text-sm text-muted">{{ t('devices.help') }}</p>

    <Alert v-if="errorText" variant="destructive" role="alert" aria-live="polite">
      <AlertDescription>{{ errorText }}</AlertDescription>
    </Alert>

    <p v-if="approved" class="text-sm text-sage" role="status">{{ t('devices.approved') }}</p>

    <!-- Entry -->
    <Card v-if="!found">
      <CardContent class="flex flex-col gap-3 py-4">
        <label class="text-sm font-medium text-ink" for="code">{{ t('devices.codeLabel') }}</label>
        <div class="flex items-center gap-2">
          <Input id="code" name="code" v-model="code" :placeholder="t('devices.codePlaceholder')"
                 autocomplete="off" class="font-mono uppercase" @keydown.enter.prevent="lookup" />
          <Button type="button" class="shrink-0" :disabled="busy || !code.trim()" data-test="lookup" @click="lookup">
            {{ t('devices.lookup') }}
          </Button>
        </div>
      </CardContent>
    </Card>

    <!-- Confirmation -->
    <Card v-else>
      <CardHeader>
        <CardTitle class="flex items-center gap-2">
          <MonitorSmartphone class="size-4 shrink-0" aria-hidden="true" />
          {{ t('devices.confirmTitle') }}
        </CardTitle>
      </CardHeader>
      <CardContent class="flex flex-col gap-3 text-sm">
        <CodeField :value="found.displayCode" />
        <div class="flex min-w-0 flex-col gap-1">
          <span class="truncate text-muted">{{ t('devices.requestedFrom') }}: {{ found.initiatorUa }}</span>
          <span class="truncate text-muted">{{ t('devices.ipAddress') }}: {{ found.initiatorIp }}</span>
          <span v-if="fmt(found.createdAt)" class="truncate text-muted">{{ t('devices.started') }}: {{ fmt(found.createdAt) }}</span>
          <span v-if="fmt(found.expiresAt)" class="truncate text-muted">{{ t('devices.expires') }}: {{ fmt(found.expiresAt) }}</span>
        </div>
        <p v-if="found.alreadyBound" class="text-sm text-sage" role="status">{{ t('devices.alreadyBound') }}</p>
        <div class="flex gap-2">
          <Button v-if="!found.alreadyBound" type="button" :disabled="busy" data-test="approve" @click="approve">
            {{ t('devices.approve') }}
          </Button>
          <Button type="button" variant="outline" :disabled="busy" data-test="cancel" @click="cancel">
            {{ t('devices.cancel') }}
          </Button>
        </div>
      </CardContent>
    </Card>
  </div>
</template>
```

- [ ] **Step 4: Run the test to confirm it passes.**

Run: `cd dashboard && mise exec -- npm run test -- DevicesView`
Expected: PASS (all cases).

- [ ] **Step 5: Commit (source only).**

```bash
cd /home/tundra/projects/tundra/prohibitorum
git checkout -- pkg/webui/dist 2>/dev/null; git clean -fq pkg/webui/dist 2>/dev/null
git add dashboard/src/pages/DevicesView.vue dashboard/src/pages/DevicesView.test.ts dashboard/src/locales/en.ts
git commit -m "feat(web): DevicesView (/devices) — approve a device by pairing code"
```

---

### Task 3: PairDeviceView (`/pair`, new-device side)

**Goal:** On a new device, begin a pairing, poll for approval, complete to get a session, then offer a skippable passkey registration before entering the dashboard.

**Files:**
- Create: `dashboard/src/pages/PairDeviceView.vue`
- Create: `dashboard/src/pages/PairDeviceView.test.ts`
- Modify: `dashboard/src/locales/en.ts` (add `pair:` block)

**Acceptance Criteria:**
- [ ] On mount, `POST /auth/devices/pair/begin` → shows `displayCode` + instructions.
- [ ] Polls `GET /auth/devices/pair/status?id=` every `POLL_MS`; stops on unmount/approved/expired.
- [ ] `expired` → expired state + "Generate a new code" (re-begins).
- [ ] `approved` → `POST /auth/devices/pair/complete {pairingId}` → success step.
- [ ] Success step offers passkey registration (`register/begin` → `useWebauthn().register()` → `register/complete`) and a Skip; both `router.push('/')`.

**Verify:** `cd dashboard && mise exec -- npm run test -- PairDeviceView` → all pass.

**Steps:**

- [ ] **Step 1: Add i18n strings.** In `dashboard/src/locales/en.ts`, add a `pair:` block (after `devices:`):

```ts
  pair: {
    title: 'Pair this device',
    intro: 'On a device where you’re already signed in, open Devices and enter this code.',
    waiting: 'Waiting for approval…',
    expiresIn: 'Expires in {seconds}s',
    expired: 'This code has expired.',
    regenerate: 'Generate a new code',
    success: 'This device is now signed in.',
    addPasskey: 'Add a passkey to this device',
    addPasskeyHelp: 'So you can sign in directly next time, without pairing.',
    skip: 'Continue to dashboard',
  },
```

- [ ] **Step 2: Write the failing test.** Create `dashboard/src/pages/PairDeviceView.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'
const get = vi.mocked(api.get)
const post = vi.mocked(api.post)

const { push } = vi.hoisted(() => ({ push: vi.fn() }))
vi.mock('vue-router', () => ({ useRouter: () => ({ push }) }))
vi.mock('@/lib/webauthn', () => ({
  passkeyGet: vi.fn(), isUserCancel: () => false,
  passkeyRegister: vi.fn(async () => ({ id: 'newcred', response: {} })),
}))

import PairDeviceView from './PairDeviceView.vue'

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
const mountView = () => mount(PairDeviceView, { global: { plugins: [i18n()] }, attachTo: document.body })

const BEGIN = { pairingId: 'p1', code: 'AB12CD34', displayCode: 'AB12-CD34', expiresAt: '2999-01-01T00:00:00Z' }

beforeEach(() => { get.mockReset(); post.mockReset(); push.mockReset(); vi.useFakeTimers() })
afterEach(() => { vi.useRealTimers() })

describe('PairDeviceView', () => {
  it('begins on mount and shows the display code', async () => {
    post.mockResolvedValue(BEGIN)
    get.mockResolvedValue({ status: 'pending' })
    const w = mountView(); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/auth/devices/pair/begin')
    expect(w.text()).toContain('AB12-CD34')
    expect(w.text()).toContain(en.pair.waiting)
  })

  it('polls; on approved it completes and shows success', async () => {
    post.mockImplementation(async (p: string) =>
      p.endsWith('/pair/begin') ? BEGIN : { session: { role: 'user' } })
    // first poll pending, second poll approved
    get.mockResolvedValueOnce({ status: 'pending' }).mockResolvedValue({ status: 'approved' })
    const w = mountView(); await flushPromises()
    await vi.advanceTimersByTimeAsync(2600) // first poll
    await vi.advanceTimersByTimeAsync(2600) // second poll → approved → complete
    await flushPromises()
    expect(get).toHaveBeenCalledWith('/api/prohibitorum/auth/devices/pair/status?id=p1')
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/auth/devices/pair/complete', { pairingId: 'p1' })
    expect(w.text()).toContain(en.pair.success)
  })

  it('shows expired state and regenerates on click', async () => {
    post.mockResolvedValue(BEGIN)
    get.mockResolvedValue({ status: 'expired' })
    const w = mountView(); await flushPromises()
    await vi.advanceTimersByTimeAsync(2600)
    await flushPromises()
    expect(w.text()).toContain(en.pair.expired)
    post.mockClear()
    await w.find('[data-test="regenerate"]').trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/auth/devices/pair/begin')
  })

  it('skip on success navigates to dashboard', async () => {
    post.mockImplementation(async (p: string) =>
      p.endsWith('/pair/begin') ? BEGIN : { session: { role: 'user' } })
    get.mockResolvedValue({ status: 'approved' })
    const w = mountView(); await flushPromises()
    await vi.advanceTimersByTimeAsync(2600); await flushPromises()
    await w.find('[data-test="skip"]').trigger('click'); await flushPromises()
    expect(push).toHaveBeenCalledWith('/')
  })

  it('add-passkey registers then navigates to dashboard', async () => {
    post.mockImplementation(async (p: string) => {
      if (p.endsWith('/pair/begin')) return BEGIN
      if (p.endsWith('/pair/complete')) return { session: { role: 'user' } }
      if (p.endsWith('/register/begin')) return { challenge: 'c' }
      return undefined // register/complete
    })
    get.mockResolvedValue({ status: 'approved' })
    const w = mountView(); await flushPromises()
    await vi.advanceTimersByTimeAsync(2600); await flushPromises()
    await w.find('[data-test="add-passkey"]').trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/credentials/register/begin')
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/credentials/register/complete',
      expect.objectContaining({ id: 'newcred' }))
    expect(push).toHaveBeenCalledWith('/')
  })
})
```

- [ ] **Step 2b: Run the test to confirm it fails.**

Run: `cd dashboard && mise exec -- npm run test -- PairDeviceView`
Expected: FAIL — cannot resolve `./PairDeviceView.vue`.

- [ ] **Step 3: Implement the view.** Create `dashboard/src/pages/PairDeviceView.vue`:

```vue
<script setup lang="ts">
/**
 * PairDeviceView (/pair) — the NEW-device side of device pairing (public).
 * begin → show display code → poll status → on approval complete (gets a
 * session cookie) → offer a skippable local-passkey registration → dashboard.
 * The poll timer is cleared on unmount, approval, and expiry.
 */
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { useWebauthn } from '@/composables/useWebauthn'
import type { PublicKeyCredentialCreationOptionsJSON } from '@simplewebauthn/browser'
import CenteredLayout from '@/pages/CenteredLayout.vue'
import CodeField from '@/components/custom/CodeField.vue'
import { Button } from '@/components/ui/button'
import { Alert, AlertDescription } from '@/components/ui/alert'

const POLL_MS = 2500

interface Begin { pairingId: string; code: string; displayCode: string; expiresAt: string }
interface Status { status: 'pending' | 'approved' | 'expired'; expiresAt?: string }

const { t, te } = useI18n()
const router = useRouter()
const { busy, error, run } = useApi()
const { register } = useWebauthn()

type Phase = 'pending' | 'expired' | 'success'
const phase = ref<Phase>('pending')
const displayCode = ref('')
const pairingId = ref('')
const expiresAt = ref('')
const now = ref(Date.now())
let timer: ReturnType<typeof setInterval> | null = null
let polling = false

const errorText = computed(() => {
  const e = error.value
  if (!e) return ''
  const key = `errors.${e.code}`
  return te(key) ? t(key) : e.message || t('common.error')
})
const secondsLeft = computed(() => {
  const ms = Date.parse(expiresAt.value)
  if (Number.isNaN(ms)) return 0
  return Math.max(0, Math.round((ms - now.value) / 1000))
})

function stopTimer(): void { if (timer) { clearInterval(timer); timer = null } }

async function begin(): Promise<void> {
  stopTimer()
  phase.value = 'pending'
  const res = await run(() => api.post<Begin>('/api/prohibitorum/auth/devices/pair/begin'))
  if (!res) return
  pairingId.value = res.pairingId
  displayCode.value = res.displayCode
  expiresAt.value = res.expiresAt
  now.value = Date.now()
  timer = setInterval(poll, POLL_MS)
}

async function poll(): Promise<void> {
  if (polling || phase.value !== 'pending') return
  polling = true
  now.value = Date.now()
  try {
    const s = await api.get<Status>(
      `/api/prohibitorum/auth/devices/pair/status?id=${encodeURIComponent(pairingId.value)}`)
    if (s.status === 'expired') { stopTimer(); phase.value = 'expired'; return }
    if (s.status === 'approved') { stopTimer(); await complete() }
  } catch {
    // Transient poll failure — keep polling; a terminal state will resolve it.
  } finally {
    polling = false
  }
}

async function complete(): Promise<void> {
  const ok = await run(async () => {
    await api.post('/api/prohibitorum/auth/devices/pair/complete', { pairingId: pairingId.value })
    return true as const
  })
  if (ok) phase.value = 'success'
}

async function addPasskey(): Promise<void> {
  const options = await run(() => api.post<PublicKeyCredentialCreationOptionsJSON>(
    '/api/prohibitorum/me/credentials/register/begin'))
  if (!options) return
  const attestation = await register(options)
  if (!attestation) return
  const ok = await run(async () => {
    await api.post('/api/prohibitorum/me/credentials/register/complete', attestation)
    return true as const
  })
  if (ok) router.push('/')
}

function skip(): void { router.push('/') }

onMounted(begin)
onUnmounted(stopTimer)
</script>

<template>
  <CenteredLayout>
    <template #title>
      <h1 class="text-xl font-semibold tracking-tight text-ink">{{ t('pair.title') }}</h1>
    </template>

    <div class="flex flex-col gap-6">
      <Alert v-if="errorText" variant="destructive" role="alert" aria-live="polite">
        <AlertDescription>{{ errorText }}</AlertDescription>
      </Alert>

      <template v-if="phase === 'pending'">
        <p class="text-center text-sm text-muted">{{ t('pair.intro') }}</p>
        <CodeField v-if="displayCode" :value="displayCode" />
        <p class="text-center text-sm text-muted" role="status">
          {{ t('pair.waiting') }}
          <span v-if="secondsLeft > 0"> · {{ t('pair.expiresIn', { seconds: secondsLeft }) }}</span>
        </p>
      </template>

      <template v-else-if="phase === 'expired'">
        <p class="text-center text-sm text-muted" role="status">{{ t('pair.expired') }}</p>
        <Button type="button" class="w-full" :disabled="busy" data-test="regenerate" @click="begin">
          {{ t('pair.regenerate') }}
        </Button>
      </template>

      <template v-else>
        <p class="text-center text-sm text-sage" role="status">{{ t('pair.success') }}</p>
        <div class="flex flex-col gap-2">
          <Button type="button" class="w-full" :disabled="busy" data-test="add-passkey" @click="addPasskey">
            {{ t('pair.addPasskey') }}
          </Button>
          <p class="text-center text-xs text-muted">{{ t('pair.addPasskeyHelp') }}</p>
          <Button type="button" variant="ghost" class="w-full" data-test="skip" @click="skip">
            {{ t('pair.skip') }}
          </Button>
        </div>
      </template>
    </div>
  </CenteredLayout>
</template>
```

- [ ] **Step 4: Run the test to confirm it passes.**

Run: `cd dashboard && mise exec -- npm run test -- PairDeviceView`
Expected: PASS (all cases). If a fake-timer test flakes on microtask ordering, ensure each `advanceTimersByTimeAsync` is followed by `await flushPromises()`.

- [ ] **Step 5: Commit (source only).**

```bash
cd /home/tundra/projects/tundra/prohibitorum
git checkout -- pkg/webui/dist 2>/dev/null; git clean -fq pkg/webui/dist 2>/dev/null
git add dashboard/src/pages/PairDeviceView.vue dashboard/src/pages/PairDeviceView.test.ts dashboard/src/locales/en.ts
git commit -m "feat(web): PairDeviceView (/pair) — new-device pairing + skippable passkey"
```

---

### Task 4: Routes, sidebar nav, and login entry link

**Goal:** Mount the three new pages, add the two sidebar items in the agreed order, and add the `/login` → `/pair` link.

**Files:**
- Modify: `dashboard/src/router/index.ts`
- Modify: `dashboard/src/components/custom/AppSidebar.vue`
- Modify: `dashboard/src/components/custom/AppSidebar.test.ts`
- Modify: `dashboard/src/pages/LoginView.vue`
- Modify: `dashboard/src/locales/en.ts` (add `nav.connected`, `nav.devices`, `login.pairDevice`)

**Acceptance Criteria:**
- [ ] `/connected` + `/devices` resolve as `requiresAuth` children of `DashboardLayout`; `/pair` resolves as a public route.
- [ ] Sidebar shows Profile · Security · Sessions · Connected · Devices in that order.
- [ ] `/login` renders a "New device? Pair it" link pointing at `/pair`.

**Verify:** `cd dashboard && mise exec -- npm run test -- AppSidebar` → pass; `mise exec -- npm run build` → builds clean.

**Steps:**

- [ ] **Step 1: Add i18n strings.** In `dashboard/src/locales/en.ts`:
  - In the `nav:` block, add `connected: 'Connected',` and `devices: 'Devices',`.
  - In the `login:` block, add `pairDevice: 'New device? Pair it',`.

- [ ] **Step 2: Add routes.** In `dashboard/src/router/index.ts`, add the public `/pair` route after the `/enroll/:token` route:

```ts
  {
    path: '/pair',
    name: 'pair',
    component: () => import('../pages/PairDeviceView.vue'),
    meta: { public: true },
  },
```

And add the two children to the `DashboardLayout` `children` array (after the `security` child):

```ts
      { path: 'connected', name: 'connected', component: () => import('../pages/ConnectedAccountsView.vue') },
      { path: 'devices', name: 'devices', component: () => import('../pages/DevicesView.vue') },
```

- [ ] **Step 3: Update the sidebar test (failing first).** In `dashboard/src/components/custom/AppSidebar.test.ts`:
  - Add the two new routes to `makeRouter()` so `RouterLink` resolution doesn't warn. The routes array becomes:

```ts
    routes: [{ path: '/', component: stub }, { path: '/security', component: stub }, { path: '/sessions', component: stub }, { path: '/connected', component: stub }, { path: '/devices', component: stub }, { path: '/logout', component: stub }],
```

  - In the "renders the built Account links" test, add two assertions after the existing `links` checks (the test asserts membership via `toContain`, not order):

```ts
    expect(links).toContain('/connected')
    expect(links).toContain('/devices')
```

Run: `cd dashboard && mise exec -- npm run test -- AppSidebar`
Expected: FAIL — `/connected` and `/devices` links not yet rendered.

- [ ] **Step 4: Update the sidebar.** In `dashboard/src/components/custom/AppSidebar.vue`:
  - Add `Link2` and `TabletSmartphone` to the existing `lucide-vue-next` import (both verified exported; keep Sessions on `MonitorSmartphone`). The import line becomes:

```ts
import { ShieldCheck, User, MonitorSmartphone, LogOut, KeyRound, Link2, TabletSmartphone } from 'lucide-vue-next'
```

  - Extend `accountItems` to the agreed order (Connected → `Link2`, Devices → `TabletSmartphone`):

```ts
const accountItems = computed(() => [
  { to: '/', label: t('nav.profile'), icon: User },
  { to: '/security', label: t('nav.security'), icon: KeyRound },
  { to: '/sessions', label: t('nav.sessions'), icon: MonitorSmartphone },
  { to: '/connected', label: t('nav.connected'), icon: Link2 },
  { to: '/devices', label: t('nav.devices'), icon: TabletSmartphone },
])
```

Run: `cd dashboard && mise exec -- npm run test -- AppSidebar`
Expected: PASS.

- [ ] **Step 5: Add the login entry link.** In `dashboard/src/pages/LoginView.vue`, add a `RouterLink` to `/pair` below the `<FederationButtons />`, inside the existing `<div class="flex flex-col gap-6">` (still within `CenteredLayout`):

```vue
      <FederationButtons />

      <RouterLink to="/pair" class="text-center text-sm text-muted underline-offset-4 hover:underline">
        {{ t('login.pairDevice') }}
      </RouterLink>
```

`LoginView.test.ts` currently mounts with only the i18n plugin (no router), so `RouterLink` would fail to resolve. Add a stub to its `mountView()` global config:

```ts
function mountView() {
  return mount(LoginView, {
    global: {
      plugins: [makeI18n()],
      stubs: { RouterLink: { props: ['to'], template: '<a :href="to"><slot/></a>' } },
    },
  })
}
```

This keeps the existing LoginView assertions intact while letting the new link render.

- [ ] **Step 6: Build to verify routes + icon exports compile.**

Run: `cd dashboard && mise exec -- npm run build`
Expected: build succeeds. Then discard dist (committed at the gate):

```bash
cd /home/tundra/projects/tundra/prohibitorum
git checkout -- pkg/webui/dist 2>/dev/null; git clean -fq pkg/webui/dist 2>/dev/null
```

- [ ] **Step 7: Commit (source only).**

```bash
cd /home/tundra/projects/tundra/prohibitorum
git add dashboard/src/router/index.ts dashboard/src/components/custom/AppSidebar.vue dashboard/src/components/custom/AppSidebar.test.ts dashboard/src/pages/LoginView.vue dashboard/src/locales/en.ts
git commit -m "feat(web): wire /connected /devices /pair routes + sidebar nav + login link"
```

---

### Task 5: Done-gate — full suite, Go, smoke, commit dist

**Goal:** Prove the slice green end-to-end and embed the built UI.

**Files:**
- Modify: `pkg/webui/dist/**` (rebuilt embed artifact — committed here)

**Acceptance Criteria:**
- [ ] Full vitest suite passes (not just the new files).
- [ ] `go build ./... && go vet ./...` exit 0.
- [ ] Smoke run reports `SMOKE_EXIT=0`.
- [ ] `pkg/webui/dist` rebuilt and committed.

**Verify:** see commands below.

**Steps:**

- [ ] **Step 1: Full frontend suite.**

Run: `cd dashboard && mise exec -- npm run test`
Expected: all suites pass (the 68 prior + the new ConnectedAccountsView / DevicesView / PairDeviceView + updated AppSidebar). Do NOT pipe through `tail` in a gating chain.

- [ ] **Step 2: Lint/typecheck if the project defines it.**

Run: `cd dashboard && mise exec -- npm run lint` (if the script exists) and `mise exec -- npm run build`
Expected: clean. (`build` doubles as a typecheck via vue-tsc if configured.)

- [ ] **Step 3: Go build + vet.**

```bash
cd /home/tundra/projects/tundra/prohibitorum
mise exec -- go build ./... && mise exec -- go vet ./...
```
Expected: exit 0, no output.

- [ ] **Step 4: Smoke.**

```bash
cd /home/tundra/projects/tundra/prohibitorum
setsid bash /tmp/run_v06.sh
```
Then poll `/tmp/v06.result` for `SMOKE_EXIT=0` (full log `/tmp/smoke-v06.log`). NEVER run a bare `pkill -f 'prohibitorum'` (kills dev PG). If `/tmp/run_v06.sh` is absent in this environment, locate the slice's smoke runner used by prior tasks and use that; report if none exists rather than skipping silently.

- [ ] **Step 5: Rebuild + commit dist.**

```bash
cd /home/tundra/projects/tundra/prohibitorum/dashboard
mise exec -- npm run build
cd /home/tundra/projects/tundra/prohibitorum
git add pkg/webui/dist
git commit -m "build(web): rebuild embedded dist for Spec 2c (connected + devices + pair)"
```

- [ ] **Step 6: Final state.** Confirm `git status` clean and report the gate evidence (vitest pass count, go exit 0, `SMOKE_EXIT=0`).

---

## Self-Review notes (author)

- **Spec coverage:** `/connected` (Task 1), `/devices` (Task 2), `/pair` + skippable passkey (Task 3), routes/sidebar/login-link/i18n (Task 4), done-gate + dist (Task 5). All spec sections mapped.
- **i18n keys** are introduced once and reused: `connected.*` (T1), `devices.*` + pairing errors (T2), `pair.*` (T3), `nav.connected`/`nav.devices`/`login.pairDevice` (T4). `errors.last_sign_in_method`/`credential_not_found` (T1); `errors.pairing_*` (T2). `rate_limited` already exists — not re-added.
- **Type/shape consistency:** `Identity`, `Provider`, `Lookup`, `Begin`, `Status` interfaces match the verified handler JSON. Endpoint paths are byte-for-byte the ones in the spec's contracts section.
- **Known adaptation point (flagged inline, not a placeholder):** the `ConfirmDialog` confirm-button selector in Task 1's test — locate by `data-test` if present, else by button text, matching `ConfirmDialog.vue`'s actual markup. The implementer reads that file and picks the working selector.
- **Risk:** fake-timer polling in Task 3 — mitigated by `advanceTimersByTimeAsync` + `flushPromises`. Icon export names in Task 4 — mitigated by the build step and a named fallback.
