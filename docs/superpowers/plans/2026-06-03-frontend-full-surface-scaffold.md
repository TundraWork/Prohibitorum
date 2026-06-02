# Frontend Full-Surface Scaffold Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers-extended-cc:subagent-driven-development (recommended) or superpowers-extended-cc:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make every planned core user interaction visible and navigable in the dashboard SPA — a consolidated Security page (passkeys/password/TOTP/recovery), Connected accounts, Devices, an Admin account-detail page, and greyed "planned" placeholder pages for the deferred protocol/system-admin areas — wiring the real `/me/*` and admin endpoints (which already exist), including the sudo step-up ceremony.

**Architecture:** Vue 3 + Nuxt UI v4 SPA (`dashboard/`), embedded via `pkg/webui` `go:embed`. New routes ride the existing nested-layout router; a module-singleton `useSudo` composable + a globally-mounted `SudoModal` implement the `sudo_required`→step-up→retry gate reused by every sensitive action. **No Go changes** (deferred areas are placeholders, not stubbed endpoints).

**Tech Stack:** Vue 3 `<script setup lang="ts">`, Nuxt UI v4, vue-router, Pinia, `@simplewebauthn/browser`, Vitest + `@vue/test-utils`. English-literal copy for new pages (per spec; `// TODO(i18n)`).

---

## Confirmed backend contracts (verified against the Go handlers)

All paths are under `/api/prohibitorum`. Errors throw `{code, message}`; a sudo-gated endpoint returns `{code:"sudo_required"}` (HTTP 401) when fresh sudo is missing.

| Endpoint | Method | Request | Response | Sudo |
|---|---|---|---|---|
| `/me/credentials/register/begin` | POST | `?nickname=` (query, optional), no body | flat WebAuthn creation options | no |
| `/me/credentials/register/complete` | POST | attestation (`?nickname=` query optional) | `CredentialView` | no |
| `/me/password/set` | POST | `{password}` (8–1024) | 204 | **yes** |
| `/me/totp/begin` | POST | none | `{secret_base32, otpauth_uri}` | conditional* |
| `/me/totp/verify` | POST | `{code}` | 204 **or** `{recovery_codes:[...]}` | conditional* |
| `/me/recovery-codes/regenerate` | POST | none | `{recovery_codes:[...]}` | **yes** (needs confirmed TOTP) |
| `/me/auth/revoke-password-totp` | POST | none | 204 | **yes** |
| `/me/identities` | GET | — | array `{id:number, idpSlug, idpDisplayName, upstreamEmail, linkedAt}` | no |
| `/me/identities/{id}/unlink` | POST | none | 204 | **yes** |
| `/me/identities/link/{slug}/begin` | GET | `?return_to=` | **302 redirect** to upstream | **yes** |
| `/me/devices/pair/lookup` | GET | `?code=` (required) | `{pairingId, displayCode, initiatorUa, initiatorIp, createdAt, expiresAt, alreadyBound}` | no |
| `/me/devices/pair/approve` | POST | `{code}` | 204 | **yes** |
| `/me/devices/pair/cancel` | POST | `{code}` | 204 | no |
| `/me/sudo/methods` | GET | — | `{methods:[...]}` (`webauthn`/`password_totp`) | no |
| `/me/sudo/begin` | POST | `{method}` | webauthn→flat assertion options; password_totp→204 | no |
| `/me/sudo/complete` | POST | webauthn→assertion; password_totp→`{current_password, totp_code}` | 204 | no |
| `/accounts/{id}` | GET | — | `AccountView` | admin |
| `/accounts/credentials/delete` | POST | `{accountId, credentialId}` | 200 | admin |
| `/accounts/revoke-sessions` | POST | `{id}` | `{revoked:number}` | admin |

\* **conditional sudo (TOTP):** first-time enrollment is NOT gated; re-enrollment (a confirmed TOTP already exists) IS gated. Handle by wrapping the verify/begin calls in `withSudo` — it only steps up if the server actually returns `sudo_required`, so first-enroll just works.

## Dist rebuild policy (same as prior chunks)

New pages aren't imported by the router until **Task 10**, so they don't affect the served bundle until then. **Do NOT rebuild `pkg/webui/dist` in Tasks 1–9** (vitest verifies in isolation). **Rebuild + commit `pkg/webui/dist` in Task 10** (router/sidebar wiring) and re-verify in Task 11.

## Conventions (reused across tasks)

- Error display: `const show = (e:any) => { const c=e?.code; error.value = c && te('errors.'+c) ? t('errors.'+c) : (e?.message ?? 'Something went wrong') }`. New pages use **English literals** and `te()` only for backend error codes (the locale `errors.*` keys already exist). Render in `<p v-if="error" role="alert" aria-live="polite" class="text-error text-sm">`.
- `busy` ref re-entrancy guard on every mutation; `:disabled="busy"`; `type="button"` on all buttons.
- Destructive actions: inline two-step confirm (arm → confirm), matching existing views.
- Each new page starts with `// TODO(i18n): English-literal scaffold copy; key + translate after the design-system pass.`

---

### Task 1: `useSudo` composable + `SudoModal` (the reusable step-up gate)

**Goal:** A module-singleton sudo controller: `withSudo(fn)` retries `fn` once after a step-up; `ensureSudo()` runs a proactive step-up (for the redirect-based identity link). A `SudoModal` mounted once globally runs the methods→begin→complete ceremony.

**Files:**
- Create: `dashboard/src/lib/sudo.ts`
- Create: `dashboard/src/components/SudoModal.vue`
- Test: `dashboard/src/lib/sudo.test.ts` (create)

**Acceptance Criteria:**
- [ ] `withSudo(fn)` returns `fn()`'s result directly when it succeeds; on `{code:'sudo_required'}` it opens the modal and retries once after success; if the modal is cancelled, it rethrows the original error.
- [ ] `ensureSudo()` resolves `true`/`false` from one modal run.
- [ ] `SudoModal` runs webauthn (assertion) and password_totp (`{current_password, totp_code}`) ceremonies and resolves the pending promise.

**Verify:** `cd dashboard && npx vitest run src/lib/sudo.test.ts` → PASS

**Steps:**

- [ ] **Step 1: Write the failing test** — create `dashboard/src/lib/sudo.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { withSudo, sudoState, _resolveSudo } from './sudo'

beforeEach(() => { sudoState.value.open = false; sudoState.value.resolve = null })

describe('withSudo', () => {
  it('returns the result when fn succeeds (no step-up)', async () => {
    const r = await withSudo(async () => 'ok')
    expect(r).toBe('ok')
    expect(sudoState.value.open).toBe(false)
  })

  it('on sudo_required opens the modal, then retries once after success', async () => {
    let calls = 0
    const p = withSudo(async () => {
      calls++
      if (calls === 1) throw { code: 'sudo_required', message: 'x' }
      return 'done'
    })
    // modal opened; simulate the user completing the ceremony
    await Promise.resolve()
    expect(sudoState.value.open).toBe(true)
    _resolveSudo(true)
    expect(await p).toBe('done')
    expect(calls).toBe(2)
  })

  it('rethrows the original error if the ceremony is cancelled', async () => {
    const p = withSudo(async () => { throw { code: 'sudo_required', message: 'x' } })
    await Promise.resolve()
    _resolveSudo(false)
    await expect(p).rejects.toMatchObject({ code: 'sudo_required' })
  })

  it('does not intercept non-sudo errors', async () => {
    await expect(withSudo(async () => { throw { code: 'boom' } })).rejects.toMatchObject({ code: 'boom' })
    expect(sudoState.value.open).toBe(false)
  })
})
```

- [ ] **Step 2: Run the test, confirm it FAILS**

Run: `cd dashboard && npx vitest run src/lib/sudo.test.ts`
Expected: FAIL — `./sudo` does not exist.

- [ ] **Step 3: Implement `dashboard/src/lib/sudo.ts`:**

```ts
import { ref } from 'vue'

// Module singleton: the SudoModal (mounted once in DashboardLayout) watches this
// state; withSudo()/ensureSudo() open it and await the user's ceremony.
export interface SudoState { open: boolean; resolve: ((ok: boolean) => void) | null }
export const sudoState = ref<SudoState>({ open: false, resolve: null })

// Open the step-up modal and resolve true (succeeded) / false (cancelled).
export function ensureSudo(): Promise<boolean> {
  return new Promise<boolean>((resolve) => {
    sudoState.value = { open: true, resolve }
  })
}

// Test/internal hook: resolve the pending sudo promise and close the modal.
export function _resolveSudo(ok: boolean) {
  const r = sudoState.value.resolve
  sudoState.value = { open: false, resolve: null }
  r?.(ok)
}

// Run fn(); if it fails with sudo_required, step up and retry once.
export async function withSudo<T>(fn: () => Promise<T>): Promise<T> {
  try {
    return await fn()
  } catch (e: any) {
    if (e?.code !== 'sudo_required') throw e
    const ok = await ensureSudo()
    if (!ok) throw e
    return await fn()
  }
}
```

- [ ] **Step 4: Run the test, confirm it PASSES**

Run: `cd dashboard && npx vitest run src/lib/sudo.test.ts`
Expected: PASS (4 tests). (The `SudoModal` resolves via the same `resolve` callback in the app; the test drives it through `_resolveSudo`.)

- [ ] **Step 5: Implement `dashboard/src/components/SudoModal.vue`** (mounted once; watches `sudoState`):

```vue
<script setup lang="ts">
// TODO(i18n): English-literal scaffold copy; key + translate after the design-system pass.
import { ref, watch } from 'vue'
import { api } from '../lib/api'
import { startAuthentication } from '@simplewebauthn/browser'
import { sudoState } from '../lib/sudo'

const methods = ref<string[]>([])
const chosen = ref<string>('')
const password = ref('')
const totp = ref('')
const error = ref('')
const busy = ref(false)
const loading = ref(false)

function finish(ok: boolean) {
  const r = sudoState.value.resolve
  sudoState.value = { open: false, resolve: null }
  // reset transient state
  methods.value = []; chosen.value = ''; password.value = ''; totp.value = ''; error.value = ''
  r?.(ok)
}

watch(() => sudoState.value.open, async (open) => {
  if (!open) return
  error.value = ''; chosen.value = ''
  loading.value = true
  try {
    const r = await api.get<{ methods: string[] }>('/api/prohibitorum/me/sudo/methods')
    methods.value = r.methods ?? []
    if (methods.value.length === 1) chosen.value = methods.value[0]
  } catch (e: any) {
    error.value = e?.message ?? 'Could not load step-up methods'
  } finally {
    loading.value = false
  }
})

async function runWebauthn() {
  const options = await api.post<any>('/api/prohibitorum/me/sudo/begin', { method: 'webauthn' })
  const assertion = await startAuthentication({ optionsJSON: options.publicKey ?? options })
  await api.post('/api/prohibitorum/me/sudo/complete', assertion)
}

async function runPasswordTotp() {
  await api.post('/api/prohibitorum/me/sudo/begin', { method: 'password_totp' })
  await api.post('/api/prohibitorum/me/sudo/complete', { current_password: password.value, totp_code: totp.value })
}

async function submit() {
  if (busy.value || !chosen.value) return
  busy.value = true; error.value = ''
  try {
    if (chosen.value === 'webauthn') await runWebauthn()
    else await runPasswordTotp()
    finish(true)
  } catch (e: any) {
    error.value = e?.message ?? 'Step-up failed'
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <UModal :open="sudoState.open" @update:open="(v) => { if (!v) finish(false) }">
    <template #content>
      <div class="p-6 space-y-4 w-full max-w-sm">
        <h2 class="text-lg font-semibold">Confirm it's you</h2>
        <p class="text-sm text-muted">This action needs a fresh identity check.</p>
        <p v-if="error" role="alert" aria-live="polite" class="text-error text-sm">{{ error }}</p>

        <div v-if="loading" class="text-sm text-muted">Loading…</div>
        <div v-else-if="methods.length === 0" class="text-sm text-muted">
          No step-up methods are enrolled. Add a passkey or password+2FA first.
        </div>
        <div v-else class="space-y-3">
          <div class="flex gap-2">
            <UButton v-for="m in methods" :key="m" type="button" size="sm"
              :variant="chosen === m ? 'solid' : 'soft'" @click="chosen = m">
              {{ m === 'webauthn' ? 'Passkey' : 'Password + 2FA' }}
            </UButton>
          </div>
          <div v-if="chosen === 'password_totp'" class="space-y-2">
            <UInput v-model="password" type="password" placeholder="Current password" autocomplete="current-password" />
            <UInput v-model="totp" type="text" inputmode="numeric" placeholder="Authenticator code" />
          </div>
        </div>

        <div class="flex justify-end gap-2 pt-2">
          <UButton type="button" color="neutral" variant="ghost" @click="finish(false)">Cancel</UButton>
          <UButton type="button" :disabled="busy || !chosen" :loading="busy" @click="submit">Confirm</UButton>
        </div>
      </div>
    </template>
  </UModal>
</template>
```

- [ ] **Step 6: Commit**

```bash
cd /home/tundra/projects/tundra/prohibitorum
git add dashboard/src/lib/sudo.ts dashboard/src/lib/sudo.test.ts dashboard/src/components/SudoModal.vue
git commit -m "feat(webui): useSudo step-up gate + SudoModal

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

> Note: `SudoModal` is wired into `DashboardLayout` in Task 10 (mounted once so any page's `withSudo`/`ensureSudo` triggers it). The `UModal #content` slot API is Nuxt UI v4; if the installed version differs, render a fixed-overlay `<div>` instead — the logic is unchanged.

---

### Task 2: `StatusBadge` + `PlaceholderView` + `passkeyAddCredential`

**Goal:** The maturity-marker badge, the reusable "planned" page for deferred admin routes, and the add-a-passkey WebAuthn helper.

**Files:**
- Create: `dashboard/src/components/StatusBadge.vue`
- Create: `dashboard/src/pages/PlaceholderView.vue`
- Modify: `dashboard/src/lib/webauthn.ts`
- Test: `dashboard/src/components/StatusBadge.test.ts`, `dashboard/src/lib/webauthn.test.ts` (extend)

**Acceptance Criteria:**
- [ ] `StatusBadge` renders a labelled pill for `planned`/`stub`/`beta`.
- [ ] `PlaceholderView` reads `title`/`summary` from route `meta` and shows a `planned` badge + "Not yet implemented."
- [ ] `passkeyAddCredential(nickname?)` posts begin (nickname as query), runs `startRegistration`, posts complete, returns the `CredentialView`.

**Verify:** `cd dashboard && npx vitest run src/components/StatusBadge.test.ts src/lib/webauthn.test.ts` → PASS

**Steps:**

- [ ] **Step 1: Write the failing tests.** Create `dashboard/src/components/StatusBadge.test.ts`:

```ts
import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import StatusBadge from './StatusBadge.vue'

describe('StatusBadge', () => {
  it('renders the kind label', () => {
    const w = mount(StatusBadge, { props: { kind: 'planned' } })
    expect(w.text().toLowerCase()).toContain('planned')
  })
})
```

Append to `dashboard/src/lib/webauthn.test.ts` (inside the existing file, add a new describe block; the existing mocks for `./api` and `@simplewebauthn/browser` already cover `startRegistration`):

```ts
import { passkeyAddCredential } from './webauthn'

describe('passkeyAddCredential', () => {
  it('begins (nickname query), runs startRegistration, completes, returns the credential', async () => {
    post.mockResolvedValueOnce({ challenge: 'abc' })            // begin
    startRegistration.mockResolvedValueOnce({ id: 'cred' })     // attestation
    post.mockResolvedValueOnce({ id: 7, credentialIdSuffix: 'ab12', transports: [] }) // complete
    const cred = await passkeyAddCredential('Laptop')
    expect(post).toHaveBeenNthCalledWith(1, '/api/prohibitorum/me/credentials/register/begin?nickname=Laptop')
    expect(startRegistration).toHaveBeenCalledWith({ optionsJSON: { challenge: 'abc' } })
    expect(post).toHaveBeenNthCalledWith(2, '/api/prohibitorum/me/credentials/register/complete?nickname=Laptop', { id: 'cred' })
    expect(cred.id).toBe(7)
  })
})
```

- [ ] **Step 2: Run, confirm FAIL**

Run: `cd dashboard && npx vitest run src/components/StatusBadge.test.ts src/lib/webauthn.test.ts`
Expected: FAIL — `StatusBadge`, `passkeyAddCredential` missing.

- [ ] **Step 3: Implement `dashboard/src/components/StatusBadge.vue`:**

```vue
<script setup lang="ts">
const props = defineProps<{ kind: 'planned' | 'stub' | 'beta' }>()
const color: Record<string, string> = { planned: 'neutral', stub: 'warning', beta: 'primary' }
</script>

<template>
  <UBadge :color="color[props.kind]" variant="subtle" size="sm">{{ props.kind }}</UBadge>
</template>
```

- [ ] **Step 4: Implement `dashboard/src/pages/PlaceholderView.vue`:**

```vue
<script setup lang="ts">
// TODO(i18n): English-literal scaffold copy; key + translate after the design-system pass.
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import StatusBadge from '../components/StatusBadge.vue'

const route = useRoute()
const title = computed(() => (route.meta.title as string) ?? 'Planned')
const summary = computed(() => (route.meta.summary as string) ?? '')
</script>

<template>
  <UCard class="max-w-xl">
    <template #header>
      <div class="flex items-center gap-2">
        <h1 class="text-lg font-semibold">{{ title }}</h1>
        <StatusBadge kind="planned" />
      </div>
    </template>
    <p class="text-sm text-muted">{{ summary }}</p>
    <p class="text-sm text-muted mt-2">Not yet implemented.</p>
  </UCard>
</template>
```

- [ ] **Step 5: Add `passkeyAddCredential` to `dashboard/src/lib/webauthn.ts`** (append; `startRegistration`, `api`, `SessionView` already imported). First add an exported type near `EnrollFields`:

```ts
export interface CredentialView {
  id: number
  credentialIdSuffix: string
  nickname?: string | null
  transports: string[]
  backupState?: boolean
  attestationType?: string
  createdAt?: string
  lastUsedAt?: string | null
}

// Add another passkey to the CURRENT account. begin/complete take the optional
// nickname as a QUERY param (see handle_me.go); begin returns flat creation
// options. Not sudo-gated.
export async function passkeyAddCredential(nickname?: string): Promise<CredentialView> {
  const q = nickname ? `?nickname=${encodeURIComponent(nickname)}` : ''
  const options = await api.post<any>(`/api/prohibitorum/me/credentials/register/begin${q}`)
  const attestation = await startRegistration({ optionsJSON: options.publicKey ?? options })
  return await api.post<CredentialView>(`/api/prohibitorum/me/credentials/register/complete${q}`, attestation)
}
```

- [ ] **Step 6: Run tests, confirm PASS; then full suite**

Run: `cd dashboard && npx vitest run src/components/StatusBadge.test.ts src/lib/webauthn.test.ts` → PASS
Run: `cd dashboard && npx vitest run` → all PASS

- [ ] **Step 7: Commit**

```bash
git add dashboard/src/components/StatusBadge.vue dashboard/src/components/StatusBadge.test.ts dashboard/src/pages/PlaceholderView.vue dashboard/src/lib/webauthn.ts dashboard/src/lib/webauthn.test.ts
git commit -m "feat(webui): StatusBadge + PlaceholderView + passkeyAddCredential

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: `SecurityView` shell + Passkeys card

**Goal:** The consolidated Security page with its first section — Passkeys (list/rename/delete moved from `CredentialsView`, plus Add passkey).

**Files:**
- Create: `dashboard/src/pages/SecurityView.vue`
- Create: `dashboard/src/pages/security/PasskeysCard.vue`
- Test: `dashboard/src/pages/security/PasskeysCard.test.ts`

**Acceptance Criteria:**
- [ ] `SecurityView` renders a page title and the `PasskeysCard` (later tasks add more cards).
- [ ] `PasskeysCard` lists `GET /me/credentials`, supports rename (`/me/credentials/rename {id,nickname}`), two-step delete (`/me/credentials/delete {id}`), and **Add passkey** via `passkeyAddCredential`, refetching after each.

**Verify:** `cd dashboard && npx vitest run src/pages/security/PasskeysCard.test.ts` → PASS

**Steps:**

- [ ] **Step 1: Write the failing test** — create `dashboard/src/pages/security/PasskeysCard.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import PasskeysCard from './PasskeysCard.vue'

const get = vi.fn(); const post = vi.fn()
vi.mock('../../lib/api', () => ({ api: { get: (...a: unknown[]) => get(...a), post: (...a: unknown[]) => post(...a) } }))
const add = vi.fn()
vi.mock('../../lib/webauthn', () => ({ passkeyAddCredential: (...a: unknown[]) => add(...a) }))

beforeEach(() => { get.mockReset(); post.mockReset(); add.mockReset() })
const creds = [{ id: 1, credentialIdSuffix: 'ab12', nickname: 'Laptop', transports: ['internal'], createdAt: '2026-01-01T00:00:00Z' }]

describe('PasskeysCard', () => {
  it('lists credentials', async () => {
    get.mockResolvedValueOnce(creds)
    const w = mount(PasskeysCard); await flushPromises()
    expect(w.findAll('tbody tr').length).toBe(1)
    expect(w.text()).toContain('Laptop')
  })
  it('adds a passkey then refetches', async () => {
    get.mockResolvedValueOnce(creds)
    add.mockResolvedValueOnce({ id: 2 })
    get.mockResolvedValueOnce([...creds, { id: 2, credentialIdSuffix: 'cd34', nickname: null, transports: [], createdAt: '2026-01-02T00:00:00Z' }])
    const w = mount(PasskeysCard); await flushPromises()
    await w.find('[data-test="add-passkey"]').trigger('click'); await flushPromises()
    expect(add).toHaveBeenCalled()
    expect(get).toHaveBeenCalledTimes(2)
  })
})
```

- [ ] **Step 2: Run, confirm FAIL**

Run: `cd dashboard && npx vitest run src/pages/security/PasskeysCard.test.ts` → FAIL (missing components).

- [ ] **Step 3: Implement `dashboard/src/pages/security/PasskeysCard.vue`:**

```vue
<script setup lang="ts">
// TODO(i18n): English-literal scaffold copy; key + translate after the design-system pass.
import { ref, onMounted } from 'vue'
import { api } from '../../lib/api'
import { passkeyAddCredential, type CredentialView } from '../../lib/webauthn'

const rows = ref<CredentialView[]>([])
const error = ref(''); const busy = ref(false)
const armed = ref<number | null>(null); const editing = ref<number | null>(null); const draft = ref('')

function show(e: any) { error.value = e?.message ?? 'Something went wrong' }
async function load() { error.value = ''; try { rows.value = await api.get<CredentialView[]>('/api/prohibitorum/me/credentials') } catch (e) { show(e) } }

async function add() {
  if (busy.value) return; busy.value = true; error.value = ''
  try { await passkeyAddCredential(); await load() } catch (e) { show(e) } finally { busy.value = false }
}
function startRename(c: CredentialView) { editing.value = c.id; draft.value = c.nickname ?? '' }
async function saveRename(id: number) {
  if (busy.value) return; busy.value = true; error.value = ''
  try { await api.post('/api/prohibitorum/me/credentials/rename', { id, nickname: draft.value || null }); editing.value = null; await load() } catch (e) { show(e) } finally { busy.value = false }
}
async function del(id: number) {
  if (busy.value) return; busy.value = true; error.value = ''
  try { await api.post('/api/prohibitorum/me/credentials/delete', { id }); armed.value = null; await load() } catch (e) { show(e); armed.value = null } finally { busy.value = false }
}
onMounted(load)
</script>

<template>
  <UCard>
    <template #header>
      <div class="flex items-center justify-between">
        <h2 class="font-medium">Passkeys</h2>
        <UButton data-test="add-passkey" type="button" size="xs" :loading="busy" :disabled="busy" @click="add">Add passkey</UButton>
      </div>
    </template>
    <p v-if="error" role="alert" aria-live="polite" class="text-error text-sm mb-2">{{ error }}</p>
    <table class="w-full text-sm border-collapse">
      <thead><tr class="text-left text-muted border-b border-default">
        <th class="py-2 pr-4">Nickname</th><th class="py-2 pr-4">ID</th><th class="py-2 pr-4">Added</th><th class="py-2 pr-4">Actions</th>
      </tr></thead>
      <tbody>
        <tr v-for="c in rows" :key="c.id" class="border-b border-default/50">
          <td class="py-2 pr-4">
            <UInput v-if="editing === c.id" v-model="draft" size="xs" placeholder="New nickname" />
            <template v-else>{{ c.nickname || '(unnamed)' }}</template>
          </td>
          <td class="py-2 pr-4 font-mono text-xs">…{{ c.credentialIdSuffix }}</td>
          <td class="py-2 pr-4">{{ c.createdAt ? new Date(c.createdAt).toLocaleDateString() : '—' }}</td>
          <td class="py-2 pr-4">
            <div class="inline-flex items-center gap-1">
              <template v-if="editing === c.id">
                <UButton type="button" size="xs" :disabled="busy" @click="saveRename(c.id)">Save</UButton>
                <UButton type="button" size="xs" color="neutral" variant="ghost" @click="editing = null">Cancel</UButton>
              </template>
              <template v-else>
                <UButton type="button" size="xs" variant="soft" @click="startRename(c)">Rename</UButton>
                <template v-if="armed === c.id">
                  <UButton data-test="del" type="button" size="xs" color="error" :disabled="busy" @click="del(c.id)">Confirm</UButton>
                  <UButton type="button" size="xs" color="neutral" variant="ghost" @click="armed = null">Cancel</UButton>
                </template>
                <UButton v-else data-test="del" type="button" size="xs" color="error" variant="soft" @click="armed = c.id">Delete</UButton>
              </template>
            </div>
          </td>
        </tr>
      </tbody>
    </table>
  </UCard>
</template>
```

- [ ] **Step 4: Implement `dashboard/src/pages/SecurityView.vue`** (shell; later tasks add cards):

```vue
<script setup lang="ts">
// TODO(i18n): English-literal scaffold copy; key + translate after the design-system pass.
import PasskeysCard from './security/PasskeysCard.vue'
</script>

<template>
  <div class="space-y-6 max-w-3xl">
    <h1 class="text-lg font-semibold">Security</h1>
    <PasskeysCard />
  </div>
</template>
```

- [ ] **Step 5: Run the test, confirm PASS**

Run: `cd dashboard && npx vitest run src/pages/security/PasskeysCard.test.ts` → PASS

- [ ] **Step 6: Commit**

```bash
git add dashboard/src/pages/SecurityView.vue dashboard/src/pages/security/PasskeysCard.vue dashboard/src/pages/security/PasskeysCard.test.ts
git commit -m "feat(webui): SecurityView shell + Passkeys card (incl. add-passkey)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: Password card

**Goal:** Set/change password, sudo-gated via `withSudo`.

**Files:**
- Create: `dashboard/src/pages/security/PasswordCard.vue`
- Modify: `dashboard/src/pages/SecurityView.vue` (add the card)
- Test: `dashboard/src/pages/security/PasswordCard.test.ts`

**Acceptance Criteria:**
- [ ] Submitting a password posts `/me/password/set {password}` wrapped in `withSudo`.
- [ ] A success message shows after a 204; errors surface inline.

**Verify:** `cd dashboard && npx vitest run src/pages/security/PasswordCard.test.ts` → PASS

**Steps:**

- [ ] **Step 1: Write the failing test** — `dashboard/src/pages/security/PasswordCard.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import PasswordCard from './PasswordCard.vue'

const post = vi.fn()
vi.mock('../../lib/api', () => ({ api: { post: (...a: unknown[]) => post(...a) } }))
// withSudo passes through (no sudo_required in this test)
vi.mock('../../lib/sudo', () => ({ withSudo: (fn: any) => fn() }))

beforeEach(() => post.mockReset())

describe('PasswordCard', () => {
  it('sets the password via withSudo', async () => {
    post.mockResolvedValueOnce(undefined)
    const w = mount(PasswordCard)
    await w.find('input[type="password"]').setValue('hunter2hunter2')
    await w.find('[data-test="save-password"]').trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/password/set', { password: 'hunter2hunter2' })
    expect(w.text().toLowerCase()).toContain('updated')
  })
})
```

- [ ] **Step 2: Run, confirm FAIL** — `cd dashboard && npx vitest run src/pages/security/PasswordCard.test.ts` → FAIL.

- [ ] **Step 3: Implement `dashboard/src/pages/security/PasswordCard.vue`:**

```vue
<script setup lang="ts">
// TODO(i18n): English-literal scaffold copy; key + translate after the design-system pass.
import { ref } from 'vue'
import { api } from '../../lib/api'
import { withSudo } from '../../lib/sudo'

const password = ref(''); const busy = ref(false); const error = ref(''); const done = ref(false)

async function save() {
  if (busy.value || password.value.length < 8) { if (password.value.length < 8) error.value = 'Password must be at least 8 characters'; return }
  busy.value = true; error.value = ''; done.value = false
  try {
    await withSudo(() => api.post('/api/prohibitorum/me/password/set', { password: password.value }))
    done.value = true; password.value = ''
  } catch (e: any) { error.value = e?.message ?? 'Could not set password' } finally { busy.value = false }
}
</script>

<template>
  <UCard>
    <template #header><h2 class="font-medium">Password</h2></template>
    <div class="flex flex-col gap-2 max-w-sm">
      <UInput v-model="password" type="password" placeholder="New password" autocomplete="new-password" />
      <p v-if="error" role="alert" aria-live="polite" class="text-error text-sm">{{ error }}</p>
      <p v-if="done" class="text-success text-sm">Password updated.</p>
      <UButton data-test="save-password" type="button" class="self-start" :loading="busy" :disabled="busy" @click="save">Set password</UButton>
    </div>
  </UCard>
</template>
```

- [ ] **Step 4: Add to `SecurityView.vue`** — import and render after `PasskeysCard`:

```vue
<script setup lang="ts">
// TODO(i18n): English-literal scaffold copy; key + translate after the design-system pass.
import PasskeysCard from './security/PasskeysCard.vue'
import PasswordCard from './security/PasswordCard.vue'
</script>

<template>
  <div class="space-y-6 max-w-3xl">
    <h1 class="text-lg font-semibold">Security</h1>
    <PasskeysCard />
    <PasswordCard />
  </div>
</template>
```

- [ ] **Step 5: Run, confirm PASS** — `cd dashboard && npx vitest run src/pages/security/PasswordCard.test.ts` → PASS

- [ ] **Step 6: Commit**

```bash
git add dashboard/src/pages/security/PasswordCard.vue dashboard/src/pages/security/PasswordCard.test.ts dashboard/src/pages/SecurityView.vue
git commit -m "feat(webui): Security Password card (sudo-gated)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: Two-factor (TOTP) card

**Goal:** TOTP enrollment (begin → show secret/otpauth → verify), with conditional-sudo handled by `withSudo`, and surfacing any returned recovery codes; plus "Revoke password & 2FA".

**Files:**
- Create: `dashboard/src/pages/security/TotpCard.vue`
- Modify: `dashboard/src/pages/SecurityView.vue`
- Test: `dashboard/src/pages/security/TotpCard.test.ts`

**Acceptance Criteria:**
- [ ] "Set up" posts `/me/totp/begin` (via `withSudo`) and shows `secret_base32` + `otpauth_uri`.
- [ ] Entering a code posts `/me/totp/verify {code}` (via `withSudo`); if the response contains `recovery_codes`, they are shown once.
- [ ] "Revoke password & 2FA" posts `/me/auth/revoke-password-totp` (via `withSudo`, confirm).

**Verify:** `cd dashboard && npx vitest run src/pages/security/TotpCard.test.ts` → PASS

**Steps:**

- [ ] **Step 1: Write the failing test** — `dashboard/src/pages/security/TotpCard.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import TotpCard from './TotpCard.vue'

const post = vi.fn()
vi.mock('../../lib/api', () => ({ api: { post: (...a: unknown[]) => post(...a) } }))
vi.mock('../../lib/sudo', () => ({ withSudo: (fn: any) => fn() }))
beforeEach(() => post.mockReset())

describe('TotpCard', () => {
  it('begins enrollment and shows the secret', async () => {
    post.mockResolvedValueOnce({ secret_base32: 'JBSWY3DP', otpauth_uri: 'otpauth://totp/x' })
    const w = mount(TotpCard)
    await w.find('[data-test="totp-begin"]').trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/totp/begin')
    expect(w.text()).toContain('JBSWY3DP')
  })
  it('verifies and shows recovery codes when returned', async () => {
    post.mockResolvedValueOnce({ secret_base32: 'JBSWY3DP', otpauth_uri: 'otpauth://totp/x' }) // begin
    post.mockResolvedValueOnce({ recovery_codes: ['aaaa-bbbb', 'cccc-dddd'] }) // verify
    const w = mount(TotpCard)
    await w.find('[data-test="totp-begin"]').trigger('click'); await flushPromises()
    await w.find('[data-test="totp-code"]').setValue('123456')
    await w.find('[data-test="totp-verify"]').trigger('click'); await flushPromises()
    expect(post).toHaveBeenLastCalledWith('/api/prohibitorum/me/totp/verify', { code: '123456' })
    expect(w.text()).toContain('aaaa-bbbb')
  })
})
```

- [ ] **Step 2: Run, confirm FAIL.**

- [ ] **Step 3: Implement `dashboard/src/pages/security/TotpCard.vue`:**

```vue
<script setup lang="ts">
// TODO(i18n): English-literal scaffold copy; key + translate after the design-system pass.
import { ref } from 'vue'
import { api } from '../../lib/api'
import { withSudo } from '../../lib/sudo'

const secret = ref(''); const otpauth = ref(''); const code = ref('')
const recovery = ref<string[]>([]); const busy = ref(false); const error = ''
const err = ref(''); const done = ref(false); const armedRevoke = ref(false)

async function begin() {
  if (busy.value) return; busy.value = true; err.value = ''; recovery.value = []; done.value = false
  try {
    const r = await withSudo(() => api.post<{ secret_base32: string; otpauth_uri: string }>('/api/prohibitorum/me/totp/begin'))
    secret.value = r.secret_base32; otpauth.value = r.otpauth_uri
  } catch (e: any) { err.value = e?.message ?? 'Could not start TOTP setup' } finally { busy.value = false }
}
async function verify() {
  if (busy.value) return; busy.value = true; err.value = ''
  try {
    const r = await withSudo(() => api.post<{ recovery_codes?: string[] } | undefined>('/api/prohibitorum/me/totp/verify', { code: code.value }))
    done.value = true; secret.value = ''; otpauth.value = ''; code.value = ''
    if (r && r.recovery_codes) recovery.value = r.recovery_codes
  } catch (e: any) { err.value = e?.message ?? 'Invalid code' } finally { busy.value = false }
}
async function revoke() {
  if (busy.value) return; busy.value = true; err.value = ''
  try { await withSudo(() => api.post('/api/prohibitorum/me/auth/revoke-password-totp')); armedRevoke.value = false; done.value = false } catch (e: any) { err.value = e?.message ?? 'Could not revoke'; armedRevoke.value = false } finally { busy.value = false }
}
</script>

<template>
  <UCard>
    <template #header><h2 class="font-medium">Two-factor (TOTP)</h2></template>
    <p v-if="err" role="alert" aria-live="polite" class="text-error text-sm mb-2">{{ err }}</p>

    <div v-if="!secret" class="flex items-center gap-2">
      <UButton data-test="totp-begin" type="button" size="sm" :loading="busy" :disabled="busy" @click="begin">Set up authenticator</UButton>
      <span v-if="done" class="text-success text-sm">2FA configured.</span>
    </div>

    <div v-else class="space-y-3">
      <p class="text-sm text-muted">Add this secret to your authenticator app, then enter a code to confirm.</p>
      <div class="text-sm">Secret: <code class="font-mono">{{ secret }}</code></div>
      <div class="text-xs text-muted break-all font-mono">{{ otpauth }}</div>
      <div class="flex items-center gap-2">
        <UInput data-test="totp-code" v-model="code" type="text" inputmode="numeric" placeholder="6-digit code" class="w-40" />
        <UButton data-test="totp-verify" type="button" size="sm" :loading="busy" :disabled="busy" @click="verify">Verify</UButton>
      </div>
    </div>

    <div v-if="recovery.length" class="mt-4 space-y-1">
      <p class="text-sm font-medium">Recovery codes (save now — shown once):</p>
      <ul class="font-mono text-sm grid grid-cols-2 gap-x-6">
        <li v-for="rc in recovery" :key="rc">{{ rc }}</li>
      </ul>
    </div>

    <template #footer>
      <div class="inline-flex items-center gap-1">
        <template v-if="armedRevoke">
          <UButton type="button" size="xs" color="error" :disabled="busy" @click="revoke">Confirm revoke</UButton>
          <UButton type="button" size="xs" color="neutral" variant="ghost" @click="armedRevoke = false">Cancel</UButton>
        </template>
        <UButton v-else type="button" size="xs" color="error" variant="soft" @click="armedRevoke = true">Revoke password &amp; 2FA</UButton>
      </div>
    </template>
  </UCard>
</template>
```

> Note: the stray `const error = ''` in the original sketch is removed; the component uses `err`. (Kept distinct from other cards' `error` to avoid confusion — both are fine; `err` here.) QR rendering is intentionally omitted (no new dep) — the secret + otpauth URI are shown as copyable text; a QR image is a design-phase enhancement.

- [ ] **Step 4: Add `<TotpCard />` to `SecurityView.vue`** after `PasswordCard` (import + render).

- [ ] **Step 5: Run, confirm PASS** — `cd dashboard && npx vitest run src/pages/security/TotpCard.test.ts`

- [ ] **Step 6: Commit**

```bash
git add dashboard/src/pages/security/TotpCard.vue dashboard/src/pages/security/TotpCard.test.ts dashboard/src/pages/SecurityView.vue
git commit -m "feat(webui): Security TOTP card (enroll/verify/revoke, sudo-gated)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: Recovery codes card

**Goal:** Regenerate recovery codes (sudo-gated; requires confirmed TOTP) and show them once.

**Files:**
- Create: `dashboard/src/pages/security/RecoveryCodesCard.vue`
- Modify: `dashboard/src/pages/SecurityView.vue`
- Test: `dashboard/src/pages/security/RecoveryCodesCard.test.ts`

**Acceptance Criteria:**
- [ ] "Regenerate" (confirm) posts `/me/recovery-codes/regenerate` via `withSudo` and shows the returned codes once.
- [ ] A backend error (e.g. no TOTP) surfaces inline.

**Verify:** `cd dashboard && npx vitest run src/pages/security/RecoveryCodesCard.test.ts` → PASS

**Steps:**

- [ ] **Step 1: Write the failing test** — `dashboard/src/pages/security/RecoveryCodesCard.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import RecoveryCodesCard from './RecoveryCodesCard.vue'

const post = vi.fn()
vi.mock('../../lib/api', () => ({ api: { post: (...a: unknown[]) => post(...a) } }))
vi.mock('../../lib/sudo', () => ({ withSudo: (fn: any) => fn() }))
beforeEach(() => post.mockReset())

describe('RecoveryCodesCard', () => {
  it('regenerates after confirm and shows codes', async () => {
    post.mockResolvedValueOnce({ recovery_codes: ['aaaa-bbbb', 'cccc-dddd'] })
    const w = mount(RecoveryCodesCard)
    await w.find('[data-test="regen"]').trigger('click') // arm
    await w.find('[data-test="regen"]').trigger('click') // confirm
    await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/recovery-codes/regenerate')
    expect(w.text()).toContain('aaaa-bbbb')
  })
})
```

- [ ] **Step 2: Run, confirm FAIL.**

- [ ] **Step 3: Implement `dashboard/src/pages/security/RecoveryCodesCard.vue`:**

```vue
<script setup lang="ts">
// TODO(i18n): English-literal scaffold copy; key + translate after the design-system pass.
import { ref } from 'vue'
import { api } from '../../lib/api'
import { withSudo } from '../../lib/sudo'

const codes = ref<string[]>([]); const busy = ref(false); const error = ref(''); const armed = ref(false)

async function regen() {
  if (busy.value) return; busy.value = true; error.value = ''
  try {
    const r = await withSudo(() => api.post<{ recovery_codes: string[] }>('/api/prohibitorum/me/recovery-codes/regenerate'))
    codes.value = r.recovery_codes ?? []; armed.value = false
  } catch (e: any) { error.value = e?.message ?? 'Could not regenerate'; armed.value = false } finally { busy.value = false }
}
</script>

<template>
  <UCard>
    <template #header><h2 class="font-medium">Recovery codes</h2></template>
    <p class="text-sm text-muted mb-2">Single-use codes to sign in if you lose your other factors. Regenerating invalidates any previous set.</p>
    <p v-if="error" role="alert" aria-live="polite" class="text-error text-sm mb-2">{{ error }}</p>
    <div class="inline-flex items-center gap-1">
      <template v-if="armed">
        <UButton data-test="regen" type="button" size="xs" color="error" :disabled="busy" @click="regen">Confirm regenerate</UButton>
        <UButton type="button" size="xs" color="neutral" variant="ghost" @click="armed = false">Cancel</UButton>
      </template>
      <UButton v-else data-test="regen" type="button" size="xs" variant="soft" @click="armed = true">Regenerate</UButton>
    </div>
    <div v-if="codes.length" class="mt-3 space-y-1">
      <p class="text-sm font-medium">Save these now — shown once:</p>
      <ul class="font-mono text-sm grid grid-cols-2 gap-x-6"><li v-for="c in codes" :key="c">{{ c }}</li></ul>
    </div>
  </UCard>
</template>
```

- [ ] **Step 4: Add `<RecoveryCodesCard />` to `SecurityView.vue`** after `TotpCard`.

- [ ] **Step 5: Run, confirm PASS; then full suite** — `cd dashboard && npx vitest run`

- [ ] **Step 6: Commit**

```bash
git add dashboard/src/pages/security/RecoveryCodesCard.vue dashboard/src/pages/security/RecoveryCodesCard.test.ts dashboard/src/pages/SecurityView.vue
git commit -m "feat(webui): Security Recovery codes card (regenerate, sudo-gated)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 7: `ConnectedAccountsView` (federation identities)

**Goal:** List linked upstream identities; unlink (sudo); link (proactive sudo → navigate to the redirect begin endpoint).

**Files:**
- Create: `dashboard/src/pages/ConnectedAccountsView.vue`
- Test: `dashboard/src/pages/ConnectedAccountsView.test.ts`

**Acceptance Criteria:**
- [ ] Lists `GET /me/identities` and the available providers from `GET /auth/federation`.
- [ ] Unlink posts `/me/identities/{id}/unlink` via `withSudo`, then refetches.
- [ ] Link calls `ensureSudo()`; on success navigates to `/api/prohibitorum/me/identities/link/{slug}/begin?return_to=/connected`.

**Verify:** `cd dashboard && npx vitest run src/pages/ConnectedAccountsView.test.ts` → PASS

**Steps:**

- [ ] **Step 1: Write the failing test** — `dashboard/src/pages/ConnectedAccountsView.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ConnectedAccountsView from './ConnectedAccountsView.vue'

const get = vi.fn(); const post = vi.fn()
vi.mock('../lib/api', () => ({ api: { get: (...a: unknown[]) => get(...a), post: (...a: unknown[]) => post(...a) } }))
vi.mock('../lib/sudo', () => ({ withSudo: (fn: any) => fn(), ensureSudo: vi.fn().mockResolvedValue(true) }))
beforeEach(() => { get.mockReset(); post.mockReset() })

const idents = [{ id: 5, idpSlug: 'mockop', idpDisplayName: 'Mock OP', upstreamEmail: 'a@x', linkedAt: '2026-01-01T00:00:00Z' }]
const providers = [{ slug: 'mockop', displayName: 'Mock OP' }, { slug: 'other', displayName: 'Other' }]

describe('ConnectedAccountsView', () => {
  it('lists identities and providers', async () => {
    get.mockResolvedValueOnce(idents)     // /me/identities
    get.mockResolvedValueOnce(providers)  // /auth/federation
    const w = mount(ConnectedAccountsView); await flushPromises()
    expect(w.text()).toContain('Mock OP')
    expect(w.findAll('tbody tr').length).toBe(1)
  })
  it('unlinks via withSudo then refetches', async () => {
    get.mockResolvedValueOnce(idents); get.mockResolvedValueOnce(providers)
    post.mockResolvedValueOnce(undefined)
    get.mockResolvedValueOnce([]); get.mockResolvedValueOnce(providers)
    const w = mount(ConnectedAccountsView); await flushPromises()
    await w.find('[data-test="unlink"]').trigger('click')
    await w.find('[data-test="unlink"]').trigger('click') // arm+confirm
    await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/identities/5/unlink')
  })
})
```

- [ ] **Step 2: Run, confirm FAIL.**

- [ ] **Step 3: Implement `dashboard/src/pages/ConnectedAccountsView.vue`:**

```vue
<script setup lang="ts">
// TODO(i18n): English-literal scaffold copy; key + translate after the design-system pass.
import { ref, onMounted, computed } from 'vue'
import { api } from '../lib/api'
import { withSudo, ensureSudo } from '../lib/sudo'

interface Identity { id: number; idpSlug: string; idpDisplayName: string; upstreamEmail?: string | null; linkedAt: string }
interface Provider { slug: string; displayName: string }

const idents = ref<Identity[]>([]); const providers = ref<Provider[]>([])
const error = ref(''); const busy = ref(false); const armed = ref<number | null>(null)

function show(e: any) { error.value = e?.message ?? 'Something went wrong' }
async function load() {
  error.value = ''
  try { idents.value = await api.get<Identity[]>('/api/prohibitorum/me/identities') } catch (e) { show(e) }
  try { providers.value = await api.get<Provider[]>('/api/prohibitorum/auth/federation') } catch { /* providers optional */ }
}
const linkedSlugs = computed(() => new Set(idents.value.map(i => i.idpSlug)))

async function unlink(id: number) {
  if (busy.value) return; busy.value = true; error.value = ''
  try { await withSudo(() => api.post(`/api/prohibitorum/me/identities/${id}/unlink`)); armed.value = null; await load() } catch (e) { show(e); armed.value = null } finally { busy.value = false }
}
async function link(slug: string) {
  // The begin endpoint is a sudo-gated 302 redirect; step up proactively, then navigate.
  const ok = await ensureSudo()
  if (!ok) return
  window.location.assign(`/api/prohibitorum/me/identities/link/${encodeURIComponent(slug)}/begin?return_to=${encodeURIComponent('/connected')}`)
}
onMounted(load)
</script>

<template>
  <div class="space-y-4 max-w-3xl">
    <h1 class="text-lg font-semibold">Connected accounts</h1>
    <p v-if="error" role="alert" aria-live="polite" class="text-error text-sm">{{ error }}</p>

    <table class="w-full text-sm border-collapse">
      <thead><tr class="text-left text-muted border-b border-default">
        <th class="py-2 pr-4">Provider</th><th class="py-2 pr-4">Email</th><th class="py-2 pr-4">Linked</th><th class="py-2 pr-4">Actions</th>
      </tr></thead>
      <tbody>
        <tr v-for="i in idents" :key="i.id" class="border-b border-default/50">
          <td class="py-2 pr-4">{{ i.idpDisplayName }}</td>
          <td class="py-2 pr-4">{{ i.upstreamEmail || '—' }}</td>
          <td class="py-2 pr-4">{{ new Date(i.linkedAt).toLocaleDateString() }}</td>
          <td class="py-2 pr-4">
            <div class="inline-flex items-center gap-1">
              <template v-if="armed === i.id">
                <UButton data-test="unlink" type="button" size="xs" color="error" :disabled="busy" @click="unlink(i.id)">Confirm</UButton>
                <UButton type="button" size="xs" color="neutral" variant="ghost" @click="armed = null">Cancel</UButton>
              </template>
              <UButton v-else data-test="unlink" type="button" size="xs" color="error" variant="soft" @click="armed = i.id">Unlink</UButton>
            </div>
          </td>
        </tr>
        <tr v-if="!idents.length"><td colspan="4" class="py-3 text-muted">No connected accounts.</td></tr>
      </tbody>
    </table>

    <div v-if="providers.length" class="space-y-2">
      <h2 class="text-sm font-medium">Link a provider</h2>
      <div class="flex flex-wrap gap-2">
        <UButton v-for="p in providers" :key="p.slug" type="button" size="sm" variant="soft"
          :disabled="linkedSlugs.has(p.slug)" @click="link(p.slug)">
          {{ p.displayName }}{{ linkedSlugs.has(p.slug) ? ' (linked)' : '' }}
        </UButton>
      </div>
      <p class="text-xs text-muted">Linking redirects to the provider; it completes only with a live upstream.</p>
    </div>
  </div>
</template>
```

- [ ] **Step 4: Run, confirm PASS** — `cd dashboard && npx vitest run src/pages/ConnectedAccountsView.test.ts`

- [ ] **Step 5: Commit**

```bash
git add dashboard/src/pages/ConnectedAccountsView.vue dashboard/src/pages/ConnectedAccountsView.test.ts
git commit -m "feat(webui): ConnectedAccountsView (identities list/unlink/link)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 8: `DevicesView` (device pairing approvals)

**Goal:** Look up a pending device-pairing by its code, then approve (sudo) or cancel.

**Files:**
- Create: `dashboard/src/pages/DevicesView.vue`
- Test: `dashboard/src/pages/DevicesView.test.ts`

**Acceptance Criteria:**
- [ ] Entering a code + "Look up" calls `GET /me/devices/pair/lookup?code=` and shows the pending request.
- [ ] Approve posts `/me/devices/pair/approve {code}` via `withSudo`; Cancel posts `/me/devices/pair/cancel {code}`.

**Verify:** `cd dashboard && npx vitest run src/pages/DevicesView.test.ts` → PASS

**Steps:**

- [ ] **Step 1: Write the failing test** — `dashboard/src/pages/DevicesView.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import DevicesView from './DevicesView.vue'

const get = vi.fn(); const post = vi.fn()
vi.mock('../lib/api', () => ({ api: { get: (...a: unknown[]) => get(...a), post: (...a: unknown[]) => post(...a) } }))
vi.mock('../lib/sudo', () => ({ withSudo: (fn: any) => fn() }))
beforeEach(() => { get.mockReset(); post.mockReset() })

const pending = { pairingId: 'p1', displayCode: 'ABCD-1234', initiatorUa: 'CLI', initiatorIp: '1.1.1.1', createdAt: '2026-01-01T00:00:00Z', expiresAt: '2026-01-01T00:10:00Z', alreadyBound: false }

describe('DevicesView', () => {
  it('looks up a pending pairing then approves', async () => {
    get.mockResolvedValueOnce(pending)
    post.mockResolvedValueOnce(undefined)
    const w = mount(DevicesView)
    await w.find('[data-test="code"]').setValue('ABCD-1234')
    await w.find('[data-test="lookup"]').trigger('click'); await flushPromises()
    expect(get).toHaveBeenCalledWith('/api/prohibitorum/me/devices/pair/lookup?code=ABCD-1234')
    expect(w.text()).toContain('CLI')
    await w.find('[data-test="approve"]').trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/devices/pair/approve', { code: 'ABCD-1234' })
  })
})
```

- [ ] **Step 2: Run, confirm FAIL.**

- [ ] **Step 3: Implement `dashboard/src/pages/DevicesView.vue`:**

```vue
<script setup lang="ts">
// TODO(i18n): English-literal scaffold copy; key + translate after the design-system pass.
import { ref } from 'vue'
import { api } from '../lib/api'
import { withSudo } from '../lib/sudo'

interface Pairing { pairingId: string; displayCode: string; initiatorUa: string; initiatorIp: string; createdAt: string; expiresAt: string; alreadyBound: boolean }
const code = ref(''); const pending = ref<Pairing | null>(null)
const error = ref(''); const busy = ref(false); const done = ref('')

function show(e: any) { error.value = e?.message ?? 'Something went wrong' }
async function lookup() {
  if (busy.value || !code.value) return; busy.value = true; error.value = ''; done.value = ''; pending.value = null
  try { pending.value = await api.get<Pairing>(`/api/prohibitorum/me/devices/pair/lookup?code=${encodeURIComponent(code.value)}`) } catch (e) { show(e) } finally { busy.value = false }
}
async function approve() {
  if (busy.value) return; busy.value = true; error.value = ''
  try { await withSudo(() => api.post('/api/prohibitorum/me/devices/pair/approve', { code: code.value })); done.value = 'Device approved.'; pending.value = null } catch (e) { show(e) } finally { busy.value = false }
}
async function cancel() {
  if (busy.value) return; busy.value = true; error.value = ''
  try { await api.post('/api/prohibitorum/me/devices/pair/cancel', { code: code.value }); done.value = 'Pairing cancelled.'; pending.value = null } catch (e) { show(e) } finally { busy.value = false }
}
</script>

<template>
  <div class="space-y-4 max-w-2xl">
    <h1 class="text-lg font-semibold">Devices</h1>
    <p class="text-sm text-muted">A new device starts pairing and shows a code. Enter that code here to approve it.</p>
    <p v-if="error" role="alert" aria-live="polite" class="text-error text-sm">{{ error }}</p>
    <p v-if="done" class="text-success text-sm">{{ done }}</p>

    <div class="flex items-center gap-2">
      <UInput data-test="code" v-model="code" type="text" placeholder="Pairing code (e.g. ABCD-1234)" class="w-64" />
      <UButton data-test="lookup" type="button" size="sm" :loading="busy" :disabled="busy" @click="lookup">Look up</UButton>
    </div>

    <UCard v-if="pending">
      <dl class="grid grid-cols-3 gap-y-2 text-sm">
        <dt class="text-muted">Code</dt><dd class="col-span-2 font-mono">{{ pending.displayCode }}</dd>
        <dt class="text-muted">Device</dt><dd class="col-span-2">{{ pending.initiatorUa }}</dd>
        <dt class="text-muted">IP</dt><dd class="col-span-2 font-mono">{{ pending.initiatorIp }}</dd>
        <dt class="text-muted">Expires</dt><dd class="col-span-2">{{ new Date(pending.expiresAt).toLocaleString() }}</dd>
      </dl>
      <template #footer>
        <div class="flex gap-2">
          <UButton data-test="approve" type="button" size="sm" :disabled="busy" @click="approve">Approve</UButton>
          <UButton type="button" size="sm" color="error" variant="soft" :disabled="busy" @click="cancel">Cancel pairing</UButton>
        </div>
      </template>
    </UCard>
  </div>
</template>
```

- [ ] **Step 4: Run, confirm PASS** — `cd dashboard && npx vitest run src/pages/DevicesView.test.ts`

- [ ] **Step 5: Commit**

```bash
git add dashboard/src/pages/DevicesView.vue dashboard/src/pages/DevicesView.test.ts
git commit -m "feat(webui): DevicesView (pairing lookup/approve/cancel)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 9: `AccountDetailView` (`/admin/accounts/:id`)

**Goal:** Admin per-account page: edit role + enable/disable (PUT), revoke sessions, reissue enrollment, delete; the per-credential force-revoke is shown as a `stub` (no admin list-credentials endpoint exists).

**Files:**
- Create: `dashboard/src/pages/AccountDetailView.vue`
- Test: `dashboard/src/pages/AccountDetailView.test.ts`

**Acceptance Criteria:**
- [ ] Fetches `GET /accounts/{id}` and renders the account.
- [ ] Save posts `PUT /accounts/{id} {displayName, role, disabled}`; Revoke sessions posts `/accounts/revoke-sessions {id}` and shows the count; Reissue posts `/accounts/reissue-enrollment {id}` → `CopyableUrl`; Delete posts `/accounts/delete {id}` (confirm).
- [ ] The credential force-revoke area renders a `stub` StatusBadge noting it needs a backend list endpoint.

**Verify:** `cd dashboard && npx vitest run src/pages/AccountDetailView.test.ts` → PASS

**Steps:**

- [ ] **Step 1: Write the failing test** — `dashboard/src/pages/AccountDetailView.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'
import AccountDetailView from './AccountDetailView.vue'

const get = vi.fn(); const post = vi.fn(); const put = vi.fn()
vi.mock('../lib/api', () => ({ api: { get: (...a:unknown[])=>get(...a), post: (...a:unknown[])=>post(...a), put: (...a:unknown[])=>put(...a) } }))
beforeEach(() => { get.mockReset(); post.mockReset(); put.mockReset() })

const acct = { id: 2, username: 'bob', displayName: 'Bob', role: 'user', disabled: false, createdAt: '2026-01-01T00:00:00Z', updatedAt: '2026-01-01T00:00:00Z' }
function makeRouter() { return createRouter({ history: createMemoryHistory(), routes: [
  { path: '/admin/accounts/:id', component: AccountDetailView }, { path: '/admin/accounts', component: { template: '<div/>' } } ] }) }
async function mountAt() { const r = makeRouter(); r.push('/admin/accounts/2'); await r.isReady(); return mount(AccountDetailView, { global: { plugins: [r] } }) }

describe('AccountDetailView', () => {
  it('loads and saves role/disabled', async () => {
    get.mockResolvedValueOnce(acct)
    put.mockResolvedValueOnce({ ...acct, role: 'admin' })
    const w = await mountAt(); await flushPromises()
    expect(w.text()).toContain('bob')
    await w.find('[data-test="save"]').trigger('click'); await flushPromises()
    expect(put).toHaveBeenCalledWith('/api/prohibitorum/accounts/2', { displayName: 'Bob', role: 'user', disabled: false })
  })
  it('revoke sessions shows the count', async () => {
    get.mockResolvedValueOnce(acct)
    post.mockResolvedValueOnce({ revoked: 3 })
    const w = await mountAt(); await flushPromises()
    await w.find('[data-test="revoke-sessions"]').trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/accounts/revoke-sessions', { id: 2 })
    expect(w.text()).toContain('3')
  })
})
```

- [ ] **Step 2: Run, confirm FAIL.**

- [ ] **Step 3: Implement `dashboard/src/pages/AccountDetailView.vue`:**

```vue
<script setup lang="ts">
// TODO(i18n): English-literal scaffold copy; key + translate after the design-system pass.
import { ref, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api } from '../lib/api'
import CopyableUrl from '../components/CopyableUrl.vue'
import StatusBadge from '../components/StatusBadge.vue'

interface AccountView { id: number; username: string; displayName: string; role: string; disabled: boolean; createdAt: string; updatedAt: string; lastSignInAt?: string | null }
const route = useRoute(); const router = useRouter()
const id = computed(() => Number(route.params.id))
const acct = ref<AccountView | null>(null)
const role = ref('user'); const disabled = ref(false)
const error = ref(''); const busy = ref(false); const done = ref('')
const reissued = ref(''); const revoked = ref<number | null>(null); const armedDelete = ref(false)

function show(e: any) { error.value = e?.message ?? 'Something went wrong' }
async function load() {
  error.value = ''
  try { const a = await api.get<AccountView>(`/api/prohibitorum/accounts/${id.value}`); acct.value = a; role.value = a.role; disabled.value = a.disabled } catch (e) { show(e) }
}
async function save() {
  if (busy.value || !acct.value) return; busy.value = true; error.value = ''; done.value = ''
  try { await api.put(`/api/prohibitorum/accounts/${id.value}`, { displayName: acct.value.displayName, role: role.value, disabled: disabled.value }); done.value = 'Saved.'; await load() } catch (e) { show(e) } finally { busy.value = false }
}
async function revokeSessions() {
  if (busy.value) return; busy.value = true; error.value = ''
  try { const r = await api.post<{ revoked: number }>('/api/prohibitorum/accounts/revoke-sessions', { id: id.value }); revoked.value = r.revoked } catch (e) { show(e) } finally { busy.value = false }
}
async function reissue() {
  if (busy.value) return; busy.value = true; error.value = ''; reissued.value = ''
  try { const r = await api.post<{ url: string }>('/api/prohibitorum/accounts/reissue-enrollment', { id: id.value }); reissued.value = r.url } catch (e) { show(e) } finally { busy.value = false }
}
async function del() {
  if (busy.value) return; busy.value = true; error.value = ''
  try { await api.post('/api/prohibitorum/accounts/delete', { id: id.value }); router.push('/admin/accounts') } catch (e) { show(e); armedDelete.value = false } finally { busy.value = false }
}
onMounted(load)
</script>

<template>
  <div class="space-y-4 max-w-2xl">
    <RouterLink to="/admin/accounts" class="text-sm text-primary hover:underline">← Accounts</RouterLink>
    <h1 class="text-lg font-semibold">Account</h1>
    <p v-if="error" role="alert" aria-live="polite" class="text-error text-sm">{{ error }}</p>

    <UCard v-if="acct">
      <dl class="grid grid-cols-3 gap-y-2 text-sm">
        <dt class="text-muted">Username</dt><dd class="col-span-2 font-mono">{{ acct.username }}</dd>
        <dt class="text-muted">Display name</dt><dd class="col-span-2">{{ acct.displayName }}</dd>
        <dt class="text-muted">Role</dt>
        <dd class="col-span-2"><USelect v-model="role" :items="['user', 'admin']" class="w-40" /></dd>
        <dt class="text-muted">Disabled</dt>
        <dd class="col-span-2"><USwitch v-model="disabled" /></dd>
      </dl>
      <template #footer>
        <div class="flex items-center gap-2">
          <UButton data-test="save" type="button" size="sm" :loading="busy" :disabled="busy" @click="save">Save</UButton>
          <span v-if="done" class="text-success text-sm">{{ done }}</span>
        </div>
      </template>
    </UCard>

    <UCard v-if="acct">
      <template #header><h2 class="font-medium">Sessions & enrollment</h2></template>
      <div class="flex flex-wrap items-center gap-2">
        <UButton data-test="revoke-sessions" type="button" size="sm" variant="soft" :disabled="busy" @click="revokeSessions">Revoke all sessions</UButton>
        <span v-if="revoked !== null" class="text-sm text-muted">Revoked {{ revoked }} session(s).</span>
        <UButton type="button" size="sm" variant="soft" :disabled="busy" @click="reissue">Reissue enrollment</UButton>
      </div>
      <div v-if="reissued" class="mt-2"><CopyableUrl :url="reissued" /></div>
    </UCard>

    <UCard v-if="acct">
      <template #header>
        <div class="flex items-center gap-2"><h2 class="font-medium">Credentials</h2><StatusBadge kind="stub" /></div>
      </template>
      <p class="text-sm text-muted">Force-revoke a specific credential needs an admin "list account credentials" API (not yet implemented). <code>POST /accounts/credentials/delete {accountId, credentialId}</code> exists but there is no way to enumerate IDs yet. TODO(backend): add a list endpoint.</p>
    </UCard>

    <UCard v-if="acct">
      <template #header><h2 class="font-medium text-error">Danger zone</h2></template>
      <div class="inline-flex items-center gap-1">
        <template v-if="armedDelete">
          <UButton type="button" size="xs" color="error" :disabled="busy" @click="del">Confirm delete</UButton>
          <UButton type="button" size="xs" color="neutral" variant="ghost" @click="armedDelete = false">Cancel</UButton>
        </template>
        <UButton v-else type="button" size="xs" color="error" variant="soft" @click="armedDelete = true">Delete account</UButton>
      </div>
    </UCard>
  </div>
</template>
```

> Note: if `USwitch`/`USelect` differ in the installed Nuxt UI v4, substitute a checkbox/`<select>` — behavior (binding `role`/`disabled`) is unchanged.

- [ ] **Step 4: Run, confirm PASS; full suite** — `cd dashboard && npx vitest run`

- [ ] **Step 5: Commit**

```bash
git add dashboard/src/pages/AccountDetailView.vue dashboard/src/pages/AccountDetailView.test.ts
git commit -m "feat(webui): AccountDetailView (role/disable/revoke-sessions/reissue/delete)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 10: Router + AppSidebar wiring + planned placeholders + dev console + rebuild dist

**Goal:** Wire all new pages into the router and the grouped sidebar; add the greyed "planned" admin routes (via `PlaceholderView` + route meta); redirect `/credentials`→`/security`; mount `SudoModal` globally; link `AccountsView` rows to detail; extend the dev console; rebuild + commit dist.

**Files:**
- Modify: `dashboard/src/router.ts`, `dashboard/src/components/AppSidebar.vue`, `dashboard/src/pages/DashboardLayout.vue`, `dashboard/src/pages/DevIndexView.vue`, `dashboard/src/pages/AccountsView.vue`
- Test: `dashboard/src/router.test.ts` (extend)
- Rebuild + commit: `pkg/webui/dist`

**Acceptance Criteria:**
- [ ] New routes resolve: `/security`, `/connected`, `/devices`, `/admin/accounts/:id`, and the five `/admin/*` planned routes (each `requiresAdmin`, rendering `PlaceholderView`).
- [ ] `/credentials` redirects to `/security`.
- [ ] Sidebar shows the Account group (Profile/Security/Sessions/Connected accounts/Devices) and Admin group (Accounts/Invitations + greyed planned entries).
- [ ] `SudoModal` is mounted once in `DashboardLayout`.
- [ ] `AccountsView` rows link to `/admin/accounts/:id`.
- [ ] `npm run build` succeeds; `pkg/webui/dist` rebuilt + committed.

**Verify:** `cd dashboard && npx vitest run src/router.test.ts && npm run build` → PASS + build OK

**Steps:**

- [ ] **Step 1: Extend the guard test** — add to `dashboard/src/router.test.ts` (the existing `devMode` mock + `buildRouter` are present): add planned + security routes to `buildRouter`'s route table and a test that a non-admin is bounced from `/admin/oidc-clients`:

```ts
// add to buildRouter() routes array:
      { path: '/security', component: { template: '<div/>' }, meta: { requiresAuth: true } },
      { path: '/admin/oidc-clients', component: { template: '<div/>' }, meta: { requiresAuth: true, requiresAdmin: true } },
```

```ts
  it('bounces non-admin from a planned admin route', async () => {
    get.mockResolvedValue({ id: 1, username: 'u', displayName: 'U', role: 'user' })
    const router = buildRouter()
    await router.push('/admin/oidc-clients')
    expect(router.currentRoute.value.path).toBe('/')
  })
```

- [ ] **Step 2: Run, confirm the new test FAILS** (route not in the table yet) then add it as above so it passes against the guard logic. Run: `cd dashboard && npx vitest run src/router.test.ts`.

- [ ] **Step 3: Update `dashboard/src/router.ts`** — add the new children to the DashboardLayout parent and the redirect; full new `routes` for the `/` parent + the standalone redirect:

```ts
  routes: [
    {
      path: '/',
      component: () => import('./pages/DashboardLayout.vue'),
      children: [
        { path: '', name: 'profile', component: () => import('./pages/ProfileView.vue'), meta: { requiresAuth: true } },
        { path: 'security', name: 'security', component: () => import('./pages/SecurityView.vue'), meta: { requiresAuth: true } },
        { path: 'sessions', name: 'sessions', component: () => import('./pages/SessionsView.vue'), meta: { requiresAuth: true } },
        { path: 'connected', name: 'connected', component: () => import('./pages/ConnectedAccountsView.vue'), meta: { requiresAuth: true } },
        { path: 'devices', name: 'devices', component: () => import('./pages/DevicesView.vue'), meta: { requiresAuth: true } },
        { path: 'admin/accounts', name: 'admin-accounts', component: () => import('./pages/AccountsView.vue'), meta: { requiresAuth: true, requiresAdmin: true } },
        { path: 'admin/accounts/:id', name: 'admin-account-detail', component: () => import('./pages/AccountDetailView.vue'), meta: { requiresAuth: true, requiresAdmin: true } },
        { path: 'admin/invitations', name: 'admin-invitations', component: () => import('./pages/InvitationsView.vue'), meta: { requiresAuth: true, requiresAdmin: true } },
        // Planned (deferred) — greyed in nav; render the shared placeholder.
        { path: 'admin/oidc-clients', component: () => import('./pages/PlaceholderView.vue'), meta: { requiresAuth: true, requiresAdmin: true, title: 'OIDC clients', summary: 'Register and manage downstream OIDC relying parties (client IDs, secrets, redirect URIs, rotation).' } },
        { path: 'admin/saml-providers', component: () => import('./pages/PlaceholderView.vue'), meta: { requiresAuth: true, requiresAdmin: true, title: 'SAML service providers', summary: 'Register SAML SPs, manage metadata, ACS URLs, and signing certificates.' } },
        { path: 'admin/signing-keys', component: () => import('./pages/PlaceholderView.vue'), meta: { requiresAuth: true, requiresAdmin: true, title: 'Signing keys', summary: 'View, generate, and rotate the OIDC/SAML signing keys.' } },
        { path: 'admin/audit', component: () => import('./pages/PlaceholderView.vue'), meta: { requiresAuth: true, requiresAdmin: true, title: 'Audit log', summary: 'Browse and export credential and security events.' } },
        { path: 'admin/settings', component: () => import('./pages/PlaceholderView.vue'), meta: { requiresAuth: true, requiresAdmin: true, title: 'Settings', summary: 'Issuer, WebAuthn RP, TOTP issuer, allowed origins (read-only for now).' } },
        // /credentials folded into /security.
        { path: 'credentials', redirect: '/security' },
      ],
    },
    {
      path: '',
      component: () => import('./pages/CenteredLayout.vue'),
      children: [
        { path: '/login', name: 'login', component: () => import('./pages/LoginView.vue') },
        { path: '/consent', name: 'consent', component: () => import('./pages/ConsentView.vue') },
        { path: '/logout', name: 'logout', component: () => import('./pages/LogoutView.vue') },
        { path: '/error', name: 'error', component: () => import('./pages/ErrorView.vue') },
      ],
    },
    { path: '/enroll/:token', name: 'enroll', component: () => import('./pages/EnrollView.vue') },
    { path: '/dev', name: 'dev', component: () => import('./pages/DevIndexView.vue') },
    { path: '/:pathMatch(.*)*', redirect: '/' },
  ],
```

(The `installGuard` function is unchanged.)

- [ ] **Step 4: Update `dashboard/src/components/AppSidebar.vue`** — expand the link lists + add a greyed planned group. Replace the `<script setup>` link arrays and template:

```vue
<script setup lang="ts">
import { useSessionStore } from '../stores/session'
const session = useSessionStore()

const userLinks = [
  { to: '/', label: 'Profile', icon: 'i-lucide-user' },
  { to: '/security', label: 'Security', icon: 'i-lucide-shield' },
  { to: '/sessions', label: 'Sessions', icon: 'i-lucide-monitor' },
  { to: '/connected', label: 'Connected accounts', icon: 'i-lucide-link' },
  { to: '/devices', label: 'Devices', icon: 'i-lucide-smartphone' },
]
const adminLinks = [
  { to: '/admin/accounts', label: 'Accounts', icon: 'i-lucide-users' },
  { to: '/admin/invitations', label: 'Invitations', icon: 'i-lucide-mail-plus' },
]
const plannedLinks = [
  { to: '/admin/oidc-clients', label: 'OIDC clients' },
  { to: '/admin/saml-providers', label: 'SAML providers' },
  { to: '/admin/signing-keys', label: 'Signing keys' },
  { to: '/admin/audit', label: 'Audit log' },
  { to: '/admin/settings', label: 'Settings' },
]
</script>

<template>
  <nav class="flex flex-col gap-1 p-3 w-56 shrink-0 border-r border-default min-h-screen">
    <RouterLink v-for="l in userLinks" :key="l.to" :to="l.to"
      class="flex items-center gap-2 px-3 py-2 rounded text-sm hover:bg-elevated"
      active-class="bg-elevated font-medium" exact-active-class="bg-elevated font-medium">
      <UIcon :name="l.icon" class="size-4" />{{ l.label }}
    </RouterLink>

    <template v-if="session.isAdmin">
      <div class="mt-4 mb-1 px-3 text-xs uppercase tracking-wide text-muted">Admin</div>
      <RouterLink v-for="l in adminLinks" :key="l.to" :to="l.to"
        class="flex items-center gap-2 px-3 py-2 rounded text-sm hover:bg-elevated"
        active-class="bg-elevated font-medium">
        <UIcon :name="l.icon" class="size-4" />{{ l.label }}
      </RouterLink>
      <div class="mt-3 mb-1 px-3 text-xs uppercase tracking-wide text-muted/60">Planned</div>
      <RouterLink v-for="l in plannedLinks" :key="l.to" :to="l.to"
        class="flex items-center gap-2 px-3 py-2 rounded text-sm text-muted/60 hover:bg-elevated"
        active-class="bg-elevated">
        <UIcon name="i-lucide-circle-dashed" class="size-4" />{{ l.label }}
      </RouterLink>
    </template>
  </nav>
</template>
```

> Note: this drops the old `nav.*` i18n keys for sidebar labels in favor of English literals (per the scaffold i18n decision). The labels are now literal; `useI18n` import removed.

- [ ] **Step 5: Mount `SudoModal` in `dashboard/src/pages/DashboardLayout.vue`** — import it and render once inside the root div (e.g. right after `<AppSidebar />` or before `</div>`):

```vue
// add to <script setup>: import SudoModal from '../components/SudoModal.vue'
// add to template, inside the outer <div class="min-h-screen flex ...">:
    <SudoModal />
```

- [ ] **Step 6: Link `AccountsView` rows to detail** — in `dashboard/src/pages/AccountsView.vue`, wrap the username cell in a RouterLink. Change the username `<td>` to:

```vue
            <td class="py-2 pr-4 font-mono text-xs">
              <RouterLink :to="`/admin/accounts/${a.id}`" class="text-primary hover:underline">{{ a.username }}</RouterLink>
            </td>
```

- [ ] **Step 7: Extend the dev console route list** — in `dashboard/src/pages/DevIndexView.vue`, update `routeGroups` to include the new pages:

```ts
  { group: 'User · requires session', items: [
    { to: '/', label: 'Profile' },
    { to: '/security', label: 'Security' },
    { to: '/sessions', label: 'Sessions' },
    { to: '/connected', label: 'Connected accounts' },
    { to: '/devices', label: 'Devices' },
  ]},
  { group: 'Admin · requires admin', items: [
    { to: '/admin/accounts', label: 'Accounts' },
    { to: '/admin/invitations', label: 'Invitations' },
  ]},
  { group: 'Admin · planned', items: [
    { to: '/admin/oidc-clients', label: 'OIDC clients' },
    { to: '/admin/saml-providers', label: 'SAML providers' },
    { to: '/admin/signing-keys', label: 'Signing keys' },
    { to: '/admin/audit', label: 'Audit log' },
    { to: '/admin/settings', label: 'Settings' },
  ]},
```

- [ ] **Step 8: Run guard test + full suite + build**

Run: `cd dashboard && npx vitest run` → all PASS (incl. the new guard test).
Run: `cd dashboard && npm run build` → succeeds; confirm `git status pkg/webui/dist` shows changes.
Run: `go build ./...` → exit 0 (embed intact).

- [ ] **Step 9: Commit (source + rebuilt dist)**

```bash
git add dashboard/src/router.ts dashboard/src/router.test.ts dashboard/src/components/AppSidebar.vue dashboard/src/pages/DashboardLayout.vue dashboard/src/pages/AccountsView.vue dashboard/src/pages/DevIndexView.vue pkg/webui/dist
git commit -m "feat(webui): wire Security/Connected/Devices/account-detail + planned admin placeholders; mount SudoModal; rebuild dist

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 11: Final gate + manual walkthrough note

**Goal:** Confirm the full gate is green and record the manual click-through.

**Files:** none (verification only); optionally update the handoff note.

**Acceptance Criteria:**
- [ ] `go build ./... && go vet ./...` exit 0; `go test ./...` pass (no Go changes — must stay green).
- [ ] `cd dashboard && npx vitest run` all pass; `npm run build` clean; `pkg/webui/dist` committed.
- [ ] Manual: `mise dev-server` + `mise enroll-admin` → sign in → every new sidebar entry renders; a sudo-gated action (e.g. Set password) triggers `SudoModal`; planned entries show the placeholder.

**Verify:** `go build ./... && go vet ./... && go test ./...` → exit 0; `cd dashboard && npx vitest run` → all PASS

**Steps:**

- [ ] **Step 1: Run the Go gate** — `go build ./... && go vet ./... && go test ./...` (ignore gopls editor diagnostics; trust CLI exit 0).
- [ ] **Step 2: Run the frontend gate** — `cd dashboard && npx vitest run && npm run build`. If `pkg/webui/dist` changed since Task 10, `git add pkg/webui/dist` and commit.
- [ ] **Step 3: Manual walkthrough** (report to the user, don't automate): `mise dev-server`; `mise enroll-admin`; open the enroll URL → register → dashboard. Click Profile / Security (add passkey, set password → SudoModal, set up TOTP, regenerate recovery) / Sessions / Connected accounts / Devices; Admin → Accounts → a row → detail (edit role, revoke sessions, reissue); open a Planned entry (placeholder). Confirm `/dev` lists every page.
- [ ] **Step 4: Commit** any final dist delta (if Step 2 produced one); otherwise nothing to commit.

---

## Self-review

- **Spec coverage:** IA + routes → Task 10; Security (passkeys+add / password / TOTP / recovery) → Tasks 3–6; Connected accounts → Task 7; Devices → Task 8; Admin account detail → Task 9; sudo `useSudo`/`SudoModal` → Task 1; `StatusBadge`/`PlaceholderView` + `passkeyAddCredential` → Task 2; planned placeholders + dev console + `/credentials` redirect → Task 10; English-literal copy + `TODO(i18n)` → every page; testing/gate → Tasks per-page + Task 11. Spec's "per-account credential force-revoke" → honestly downgraded to a `stub` in Task 9 (no backend list endpoint; documented).
- **Placeholder scan:** no TBD/“add error handling”/uncoded steps; every step has concrete code/commands. The `TODO(i18n)` / `TODO(backend)` markers are intentional in-code scaffold notes, not plan gaps.
- **Type consistency:** `withSudo`/`ensureSudo`/`sudoState`/`_resolveSudo` (Task 1) used consistently in Tasks 4–9; `passkeyAddCredential` + `CredentialView` (Task 2) used in Task 3; `StatusBadge`/`PlaceholderView` (Task 2) used in Tasks 9–10; admin `PUT /accounts/{id}` body `{displayName, role, disabled}` matches the prior AccountsView toggle; route names/paths in Task 10 match the sidebar + dev console lists.
- **Dist policy:** only Task 10 (+ Task 11 fallback) rebuilds/commits `pkg/webui/dist`; Tasks 1–9 are vitest-verified.
