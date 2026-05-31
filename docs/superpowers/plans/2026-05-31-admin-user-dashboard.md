# Admin + User Dashboard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers-extended-cc:subagent-driven-development (recommended) or superpowers-extended-cc:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship a frontend-only admin+user dashboard (sidebar SPA) that makes the whole **login → dashboard → logout** flow manually browser-testable from a fresh database, consuming the backend APIs that already exist.

**Architecture:** Vue 3 + Vite + Nuxt UI v4 SPA (`dashboard/`), embedded same-origin in the Go binary via `pkg/webui` `go:embed`. New SPA routes ride the existing `NotFound`→`index.html` fallback — **no Go HTTP route changes**. A `router.beforeEach` guard gates `requiresAuth`/`requiresAdmin` routes against the Pinia session store. A passkey-enrollment page (`/enroll/:token`) drives the bootstrap ceremony and auto-logs-in, closing the loop. The only Go changes are a `mise dev-server` task and SPA-shell assertions appended to `cmd/smoke`.

**Tech Stack:** Vue 3 (`<script setup lang="ts">`), Nuxt UI v4.8.1, vue-router, Pinia, vue-i18n (zh+en), `@simplewebauthn/browser`, Vitest + `@vue/test-utils`. Backend: Go huma ops under `/api/prohibitorum` (already implemented).

---

## Critical facts confirmed against the code (read before starting)

These were verified by reading the handlers — **some correct the spec**:

1. **All consumed `/me/*` and admin endpoints are huma ops** (not raw-chi). Huma serializes the handler output's `Body` field at the JSON top level:
   - List endpoints return **top-level JSON arrays**: `GET /me/sessions` → `SessionListItem[]`, `GET /me/credentials` → `CredentialView[]`, `GET /accounts` → `AccountView[]`, `GET /invitations` → `InvitationView[]`.
   - Object endpoints return **top-level objects**: `GET /me` → `SessionView`, `PUT /accounts/{id}` → `AccountView`.
2. **`UpdateAccount` is `PUT /accounts/{id}`** with body `{displayName, role, disabled, attributes?}` (`username` is immutable / rejected if sent; `displayName` and `role` are **required**). So a Disable/Enable toggle must send the account's **current** `displayName` + `role` plus the flipped `disabled`. → `lib/api.ts` needs a `put` method (it currently only has `get`/`post`).
3. **Enrollment begin requires identity fields for bootstrap & invite** (`pkg/server/handle_enrollment.go:138-200`): bootstrap & invite validate `username` + `displayName` (bootstrap also checks uniqueness); only **reset** uses an empty body. **The manual-test path (`enroll-admin`) is a bootstrap intent with NO target.** So `EnrollView` must collect `username` + `displayName` inputs for bootstrap/invite, and only show a target name for reset. This corrects spec D5 (which assumed a `target.displayName` always exists).
4. **Enrollment `register/complete` returns `{ "session": SessionView, "newCredentialId": number }`** (`handle_enrollment.go:510-513`) — NOT a bare `SessionView`. It sets the session cookie (auto-login).
5. **Enrollment `register/begin` returns flat WebAuthn `PublicKeyCredentialCreationOptions`** (not wrapped in `{publicKey}` / `{options}`) — mirror `passkeyLogin`'s `options.publicKey ?? options` probe.
6. **Exact request bodies:** rename `{id:number, nickname:string|null}`; delete-credential `{id:number}`; revoke-session `{id:string}` (session id is a **string**); delete-account `{id:number}`; reissue-enrollment `{id:number}` → `{url, expiresAt}`; create-invitation `{role:string, attributes?}` → `{url, expiresAt}` (**no username/displayName** — invitee chooses at enroll time); revoke-invitation `{token:string}`.
7. **`App.vue` currently imposes a centered single-card chrome on every route** (`<main class="max-w-md">`). A sidebar dashboard cannot live inside that. So `App.vue` must be reduced to `<UApp><RouterView/></UApp>`, the old chrome extracted into a new `CenteredLayout.vue` used by the public auth pages, and `DashboardLayout.vue` provides the sidebar shell. (This refactor is not called out in the spec but is required; it lands in Task 11.)

## Dist rebuild policy (important)

The Go binary embeds the **committed** `pkg/webui/dist`. New views in Tasks 1–10 are **not imported by the router until Task 11**, so they don't affect the served bundle until then. Therefore: **do NOT rebuild `pkg/webui/dist` in Tasks 1–10** (vitest verifies those in isolation). **Rebuild + `git add pkg/webui/dist` in Task 11** (router wiring) and re-verify in Task 12. This avoids 10 redundant dist commits while keeping the committed dist correct.

## Verification commands (used throughout)

- Frontend unit tests: `cd dashboard && npx vitest run <file>`
- Frontend typecheck/build: `cd dashboard && npm run build`
- Go: `go build ./... && go vet ./...`
- Full smoke (Task 12): `setsid bash /tmp/run_v06.sh` then poll `/tmp/v06.result` for `DONE` / `SMOKE_EXIT=0`. **Never** `pkill -f 'prohibitorum'` bare (kills the dev PG); use `pkill -f 'go-build.*/prohibitorum'` + `pkill -f 'cmd/prohibitorum'`.
- Trust `go build ./...` exit 0 + `go vet`, NOT gopls `<new-diagnostics>` (false positives on `//go:embed all:dist` and sqlc types).

---

### Task 1: Session store extensions + `api.put`

**Goal:** Add the single source of truth the guard and layout read (`ensureLoaded`, `isAdmin`, `clear`) and the `put` HTTP verb needed by `UpdateAccount`.

**Files:**
- Modify: `dashboard/src/stores/session.ts`
- Modify: `dashboard/src/lib/api.ts`
- Test: `dashboard/src/stores/session.test.ts` (create)

**Acceptance Criteria:**
- [ ] `ensureLoaded()` calls `fetchMe` at most once across repeated calls (idempotent cache).
- [ ] `isAdmin` is true only when `me.role === 'admin'`.
- [ ] `clear()` resets `me` to null and resets the loaded flag so a later `ensureLoaded()` re-fetches.
- [ ] `api.put` issues a PUT with a JSON body.

**Verify:** `cd dashboard && npx vitest run src/stores/session.test.ts` → PASS

**Steps:**

- [ ] **Step 1: Write the failing test** — create `dashboard/src/stores/session.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useSessionStore } from './session'

const get = vi.fn()
vi.mock('../lib/api', () => ({ api: { get: (...a: unknown[]) => get(...a) } }))

beforeEach(() => {
  setActivePinia(createPinia())
  get.mockReset()
})

describe('session store', () => {
  it('ensureLoaded fetches once and caches', async () => {
    get.mockResolvedValue({ id: 1, username: 'a', displayName: 'A', role: 'user' })
    const s = useSessionStore()
    await s.ensureLoaded()
    await s.ensureLoaded()
    expect(get).toHaveBeenCalledTimes(1)
    expect(s.me?.username).toBe('a')
  })

  it('treats a rejected fetch as no session', async () => {
    get.mockRejectedValue({ code: 'no_session', message: 'x' })
    const s = useSessionStore()
    await s.ensureLoaded()
    expect(s.me).toBeNull()
  })

  it('isAdmin reflects the role', async () => {
    get.mockResolvedValue({ id: 1, username: 'a', displayName: 'A', role: 'admin' })
    const s = useSessionStore()
    await s.ensureLoaded()
    expect(s.isAdmin).toBe(true)
  })

  it('clear resets state and allows a refetch', async () => {
    get.mockResolvedValue({ id: 1, username: 'a', displayName: 'A', role: 'user' })
    const s = useSessionStore()
    await s.ensureLoaded()
    s.clear()
    expect(s.me).toBeNull()
    await s.ensureLoaded()
    expect(get).toHaveBeenCalledTimes(2)
  })
})
```

- [ ] **Step 2: Run the test, confirm it fails**

Run: `cd dashboard && npx vitest run src/stores/session.test.ts`
Expected: FAIL — `ensureLoaded`/`isAdmin`/`clear` are not defined.

- [ ] **Step 3: Implement the store** — replace `dashboard/src/stores/session.ts` with:

```ts
import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { api } from '../lib/api'

export interface SessionView {
  id: number
  username: string
  displayName: string
  role: string
}

export const useSessionStore = defineStore('session', () => {
  const me = ref<SessionView | null>(null)
  const loaded = ref(false)

  async function fetchMe(): Promise<SessionView | null> {
    try {
      me.value = await api.get<SessionView>('/api/prohibitorum/me')
    } catch {
      // 401 (or any error): treat as no live session.
      me.value = null
    }
    loaded.value = true
    return me.value
  }

  // Idempotent: fetch the session at most once. Used by the router guard and the
  // dashboard layout so they share one source of truth.
  async function ensureLoaded(): Promise<SessionView | null> {
    if (loaded.value) return me.value
    return fetchMe()
  }

  const isAdmin = computed(() => me.value?.role === 'admin')

  // Drop the cached session (after logout is initiated) so the next ensureLoaded refetches.
  function clear() {
    me.value = null
    loaded.value = false
  }

  return { me, fetchMe, ensureLoaded, isAdmin, clear }
})
```

- [ ] **Step 4: Add `put` to `dashboard/src/lib/api.ts`** — change the exported `api` object:

```ts
export const api = {
  get: <T>(p: string) => request<T>('GET', p),
  post: <T>(p: string, b?: unknown) => request<T>('POST', p, b),
  put: <T>(p: string, b?: unknown) => request<T>('PUT', p, b),
}
```

- [ ] **Step 5: Run the test, confirm it passes**

Run: `cd dashboard && npx vitest run src/stores/session.test.ts`
Expected: PASS (4 tests)

- [ ] **Step 6: Commit**

```bash
git add dashboard/src/stores/session.ts dashboard/src/stores/session.test.ts dashboard/src/lib/api.ts
git commit -m "feat(webui): session store ensureLoaded/isAdmin/clear + api.put"
```

---

### Task 2: `passkeyRegister` in `lib/webauthn.ts`

**Goal:** Add the enrollment registration ceremony helper that posts the begin body (identity fields), runs `startRegistration`, posts complete, and returns the auto-login `SessionView`.

**Files:**
- Modify: `dashboard/src/lib/webauthn.ts`
- Test: `dashboard/src/lib/webauthn.test.ts` (create)

**Acceptance Criteria:**
- [ ] `passkeyRegister(token, fields)` POSTs `fields` to `/enrollments/{token}/register/begin`.
- [ ] It calls `startRegistration({ optionsJSON })` with `options.publicKey ?? options`.
- [ ] It POSTs the attestation to `…/register/complete` and returns `result.session`.

**Verify:** `cd dashboard && npx vitest run src/lib/webauthn.test.ts` → PASS

**Steps:**

- [ ] **Step 1: Write the failing test** — create `dashboard/src/lib/webauthn.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'

const post = vi.fn()
vi.mock('./api', () => ({ api: { post: (...a: unknown[]) => post(...a) } }))

const startRegistration = vi.fn()
vi.mock('@simplewebauthn/browser', () => ({
  startRegistration: (...a: unknown[]) => startRegistration(...a),
}))

import { passkeyRegister } from './webauthn'

beforeEach(() => {
  post.mockReset()
  startRegistration.mockReset()
})

describe('passkeyRegister', () => {
  it('drives begin → ceremony → complete and returns the session', async () => {
    post.mockResolvedValueOnce({ challenge: 'abc' }) // begin (flat options)
    startRegistration.mockResolvedValueOnce({ id: 'cred' }) // attestation
    post.mockResolvedValueOnce({ session: { id: 1, username: 'a', displayName: 'A', role: 'admin' }, newCredentialId: 9 }) // complete

    const session = await passkeyRegister('tok', { username: 'a', displayName: 'A' })

    expect(post).toHaveBeenNthCalledWith(1, '/api/prohibitorum/enrollments/tok/register/begin', { username: 'a', displayName: 'A' })
    expect(startRegistration).toHaveBeenCalledWith({ optionsJSON: { challenge: 'abc' } })
    expect(post).toHaveBeenNthCalledWith(2, '/api/prohibitorum/enrollments/tok/register/complete', { id: 'cred' })
    expect(session.username).toBe('a')
  })
})
```

- [ ] **Step 2: Run the test, confirm it fails**

Run: `cd dashboard && npx vitest run src/lib/webauthn.test.ts`
Expected: FAIL — `passkeyRegister` not exported.

- [ ] **Step 3: Implement** — append to `dashboard/src/lib/webauthn.ts`:

```ts
import { startRegistration } from '@simplewebauthn/browser'

// Identity fields for the enrollment begin body. Bootstrap & invite intents
// REQUIRE username + displayName (server validates); reset ignores them (empty
// body). nickname is the optional first-passkey label. See
// pkg/server/handle_enrollment.go (enrollBeginBody).
export interface EnrollFields {
  username?: string
  displayName?: string
  nickname?: string
}

// Drives the WebAuthn registration ceremony for an enrollment token.
// /begin returns flat PublicKeyCredentialCreationOptions (probe .publicKey for
// forward-compat, mirroring passkeyLogin). /complete sets the session cookie and
// returns { session: SessionView, newCredentialId } — we return the session.
export async function passkeyRegister(token: string, fields: EnrollFields): Promise<SessionView> {
  const base = `/api/prohibitorum/enrollments/${encodeURIComponent(token)}`
  const options = await api.post<any>(`${base}/register/begin`, fields)
  const attestation = await startRegistration({ optionsJSON: options.publicKey ?? options })
  const result = await api.post<{ session: SessionView }>(`${base}/register/complete`, attestation)
  return result.session
}
```

> Note: `import { startAuthentication } ...`, `import { api } ...` and `import type { SessionView } ...` already exist at the top of the file from `passkeyLogin`. Add `startRegistration` to the existing `@simplewebauthn/browser` import line rather than duplicating it:
> `import { startAuthentication, startRegistration } from '@simplewebauthn/browser'` — and delete the standalone `import { startRegistration }` line shown above. The `api` and `SessionView` imports are already present.

- [ ] **Step 4: Run the test, confirm it passes**

Run: `cd dashboard && npx vitest run src/lib/webauthn.test.ts`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add dashboard/src/lib/webauthn.ts dashboard/src/lib/webauthn.test.ts
git commit -m "feat(webui): passkeyRegister enrollment ceremony helper"
```

---

### Task 3: Shared component — `CopyableUrl` + common i18n keys

**Goal:** A reusable copyable URL field (for reissue/invite URLs) plus the shared `common.*` i18n keys the views need. (The destructive-action confirm is implemented inline in each view as a two-step button so the `data-test` attribute lands on real `<button>`s — no separate `ConfirmButton` component; YAGNI.)

**Files:**
- Create: `dashboard/src/components/CopyableUrl.vue`
- Modify: `dashboard/src/locales/en.ts`, `dashboard/src/locales/zh.ts`
- Test: `dashboard/src/components/CopyableUrl.test.ts` (create)

**Acceptance Criteria:**
- [ ] `CopyableUrl` shows the URL read-only and copies it to the clipboard on click.
- [ ] The shared `common.*` keys (copy/copied/confirm/delete/rename/save/create/revoke/disable/enable/loading/empty) exist in both locales.

**Verify:** `cd dashboard && npx vitest run src/components/CopyableUrl.test.ts` → PASS

**Steps:**

- [ ] **Step 1: Add common i18n keys.** In `dashboard/src/locales/en.ts`, replace the `common:` line with:

```ts
  common: { continue: 'Continue', cancel: 'Cancel', signOut: 'Sign out', copy: 'Copy', copied: 'Copied', confirm: 'Confirm', delete: 'Delete', rename: 'Rename', save: 'Save', create: 'Create', revoke: 'Revoke', disable: 'Disable', enable: 'Enable', loading: 'Loading…', empty: 'Nothing here yet' },
```

In `dashboard/src/locales/zh.ts`, replace the `common:` line with:

```ts
  common: { continue: '继续', cancel: '取消', signOut: '退出登录', copy: '复制', copied: '已复制', confirm: '确认', delete: '删除', rename: '重命名', save: '保存', create: '创建', revoke: '撤销', disable: '禁用', enable: '启用', loading: '加载中…', empty: '暂无内容' },
```

- [ ] **Step 2: Write the failing tests** — create `dashboard/src/components/CopyableUrl.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import zh from '../locales/zh'
import en from '../locales/en'
import CopyableUrl from './CopyableUrl.vue'

const writeText = vi.fn().mockResolvedValue(undefined)
beforeEach(() => {
  writeText.mockClear()
  Object.assign(navigator, { clipboard: { writeText } })
})

function makeI18n() {
  return createI18n({ legacy: false, locale: 'en', fallbackLocale: 'zh', messages: { zh, en } })
}

describe('CopyableUrl', () => {
  it('copies the url to the clipboard', async () => {
    const wrapper = mount(CopyableUrl, { props: { url: 'http://x/enroll/t' }, global: { plugins: [makeI18n()] } })
    await wrapper.find('button').trigger('click')
    expect(writeText).toHaveBeenCalledWith('http://x/enroll/t')
  })
})
```

- [ ] **Step 3: Run the test, confirm it fails**

Run: `cd dashboard && npx vitest run src/components/CopyableUrl.test.ts`
Expected: FAIL — component does not exist.

- [ ] **Step 4: Implement `dashboard/src/components/CopyableUrl.vue`:**

```vue
<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{ url: string }>()
const { t } = useI18n()
const copied = ref(false)

async function copy() {
  try {
    await navigator.clipboard.writeText(props.url)
    copied.value = true
    setTimeout(() => { copied.value = false }, 1500)
  } catch {
    // Clipboard denied (e.g. insecure context): the read-only input still lets
    // the user select + copy manually.
  }
}
</script>

<template>
  <div class="flex items-center gap-2">
    <UInput :model-value="props.url" readonly class="flex-1 font-mono text-xs" />
    <UButton type="button" size="sm" variant="soft" @click="copy">
      {{ copied ? t('common.copied') : t('common.copy') }}
    </UButton>
  </div>
</template>
```

- [ ] **Step 5: Run the test, confirm it passes**

Run: `cd dashboard && npx vitest run src/components/CopyableUrl.test.ts`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add dashboard/src/components/CopyableUrl.vue dashboard/src/components/CopyableUrl.test.ts dashboard/src/locales/en.ts dashboard/src/locales/zh.ts
git commit -m "feat(webui): CopyableUrl shared component + common i18n keys"
```

---

### Task 4: `ProfileView` (`/`)

**Goal:** The dashboard landing card showing the current account, with a Logout action.

**Files:**
- Create: `dashboard/src/pages/ProfileView.vue`
- Modify: `dashboard/src/locales/en.ts`, `dashboard/src/locales/zh.ts`
- Test: `dashboard/src/pages/ProfileView.test.ts` (create)

**Acceptance Criteria:**
- [ ] Renders `username`, `displayName`, `role` from the session store.
- [ ] A Logout button navigates to `/logout`.

**Verify:** `cd dashboard && npx vitest run src/pages/ProfileView.test.ts` → PASS

**Steps:**

- [ ] **Step 1: Add i18n keys.** In `en.ts`, add a `profile` key (insert after the `logout:` line):

```ts
  profile: { title: 'Profile', username: 'Username', displayName: 'Display name', role: 'Role', logout: 'Log out' },
```

In `zh.ts`:

```ts
  profile: { title: '个人资料', username: '用户名', displayName: '显示名称', role: '角色', logout: '退出登录' },
```

- [ ] **Step 2: Write the failing test** — create `dashboard/src/pages/ProfileView.test.ts`:

```ts
import { describe, it, expect, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import zh from '../locales/zh'
import en from '../locales/en'
import { useSessionStore } from '../stores/session'
import ProfileView from './ProfileView.vue'

function makeI18n() {
  return createI18n({ legacy: false, locale: 'en', fallbackLocale: 'zh', messages: { zh, en } })
}
function makeRouter() {
  return createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: { template: '<div/>' } }, { path: '/logout', component: { template: '<div/>' } }] })
}

beforeEach(() => setActivePinia(createPinia()))

describe('ProfileView', () => {
  it('renders the current account', async () => {
    const s = useSessionStore()
    s.me = { id: 1, username: 'alice', displayName: 'Alice', role: 'admin' }
    const router = makeRouter()
    const wrapper = mount(ProfileView, { global: { plugins: [makeI18n(), router] } })
    expect(wrapper.text()).toContain('alice')
    expect(wrapper.text()).toContain('Alice')
    expect(wrapper.text()).toContain('admin')
  })
})
```

- [ ] **Step 3: Run the test, confirm it fails**

Run: `cd dashboard && npx vitest run src/pages/ProfileView.test.ts`
Expected: FAIL — component missing.

- [ ] **Step 4: Implement `dashboard/src/pages/ProfileView.vue`:**

```vue
<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { useSessionStore } from '../stores/session'

const { t } = useI18n()
const router = useRouter()
const session = useSessionStore()

function logout() {
  router.push('/logout')
}
</script>

<template>
  <UCard class="max-w-xl">
    <template #header>
      <h1 class="text-lg font-semibold">{{ t('profile.title') }}</h1>
    </template>
    <dl class="grid grid-cols-3 gap-y-3 text-sm">
      <dt class="text-muted">{{ t('profile.username') }}</dt>
      <dd class="col-span-2">{{ session.me?.username }}</dd>
      <dt class="text-muted">{{ t('profile.displayName') }}</dt>
      <dd class="col-span-2">{{ session.me?.displayName }}</dd>
      <dt class="text-muted">{{ t('profile.role') }}</dt>
      <dd class="col-span-2">{{ session.me?.role }}</dd>
    </dl>
    <template #footer>
      <UButton type="button" color="neutral" variant="soft" @click="logout">{{ t('profile.logout') }}</UButton>
    </template>
  </UCard>
</template>
```

- [ ] **Step 5: Run the test, confirm it passes**

Run: `cd dashboard && npx vitest run src/pages/ProfileView.test.ts`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add dashboard/src/pages/ProfileView.vue dashboard/src/pages/ProfileView.test.ts dashboard/src/locales/en.ts dashboard/src/locales/zh.ts
git commit -m "feat(webui): ProfileView dashboard landing"
```

---

### Task 5: `SessionsView` (`/sessions`)

**Goal:** List the caller's sessions and let them revoke any non-current one.

**Files:**
- Create: `dashboard/src/pages/SessionsView.vue`
- Modify: `dashboard/src/locales/en.ts`, `dashboard/src/locales/zh.ts`
- Test: `dashboard/src/pages/SessionsView.test.ts` (create)

**Acceptance Criteria:**
- [ ] Fetches `GET /api/prohibitorum/me/sessions` on mount and renders one row per session.
- [ ] The current session shows a badge and **no** revoke button; non-current rows have a working revoke that POSTs `{id}` then refetches.

**Verify:** `cd dashboard && npx vitest run src/pages/SessionsView.test.ts` → PASS

**Steps:**

- [ ] **Step 1: Add i18n keys.** In `en.ts`:

```ts
  sessions: { title: 'Sessions', current: 'Current', issuedAt: 'Signed in', lastSeen: 'Last seen', expiresAt: 'Expires', device: 'Device', ip: 'IP', actions: 'Actions', revoke: 'Revoke' },
```

In `zh.ts`:

```ts
  sessions: { title: '会话', current: '当前', issuedAt: '登录时间', lastSeen: '最近活动', expiresAt: '过期时间', device: '设备', ip: 'IP', actions: '操作', revoke: '撤销' },
```

- [ ] **Step 2: Write the failing test** — create `dashboard/src/pages/SessionsView.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import zh from '../locales/zh'
import en from '../locales/en'
import SessionsView from './SessionsView.vue'

const get = vi.fn()
const post = vi.fn()
vi.mock('../lib/api', () => ({ api: { get: (...a: unknown[]) => get(...a), post: (...a: unknown[]) => post(...a) } }))

function makeI18n() {
  return createI18n({ legacy: false, locale: 'en', fallbackLocale: 'zh', messages: { zh, en } })
}

beforeEach(() => { get.mockReset(); post.mockReset() })

const rows = [
  { id: 'cur', isCurrent: true, issuedAt: '2026-01-01T00:00:00Z', expiresAt: '2026-02-01T00:00:00Z', lastSeenIp: '1.1.1.1', userAgent: 'Cur' },
  { id: 'other', isCurrent: false, issuedAt: '2026-01-01T00:00:00Z', expiresAt: '2026-02-01T00:00:00Z', lastSeenIp: '2.2.2.2', userAgent: 'Other' },
]

describe('SessionsView', () => {
  it('lists sessions; only non-current rows are revocable', async () => {
    get.mockResolvedValueOnce(rows)
    const wrapper = mount(SessionsView, { global: { plugins: [makeI18n()] } })
    await flushPromises()
    expect(wrapper.findAll('tbody tr').length).toBe(2)
    // exactly one revoke button (for the non-current row)
    const revokeButtons = wrapper.findAll('[data-test="revoke"]')
    expect(revokeButtons.length).toBe(1)
  })

  it('revokes a non-current session then refetches', async () => {
    get.mockResolvedValueOnce(rows)        // initial
    post.mockResolvedValueOnce(undefined)  // revoke
    get.mockResolvedValueOnce([rows[0]])   // refetch
    const wrapper = mount(SessionsView, { global: { plugins: [makeI18n()] } })
    await flushPromises()
    await wrapper.find('[data-test="revoke"]').trigger('click')
    await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/sessions/revoke', { id: 'other' })
    expect(get).toHaveBeenCalledTimes(2)
  })
})
```

- [ ] **Step 3: Run the test, confirm it fails**

Run: `cd dashboard && npx vitest run src/pages/SessionsView.test.ts`
Expected: FAIL — component missing.

- [ ] **Step 4: Implement `dashboard/src/pages/SessionsView.vue`:**

```vue
<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '../lib/api'

interface SessionListItem {
  id: string
  isCurrent: boolean
  issuedAt: string
  expiresAt: string
  lastSeenIp: string
  userAgent?: string
}

const { t, te } = useI18n()
const rows = ref<SessionListItem[]>([])
const error = ref('')
const busy = ref(false)

function show(e: any) {
  const code = e?.code as string | undefined
  error.value = code && te('errors.' + code) ? t('errors.' + code) : (e?.message ?? t('errors.server_error'))
}

async function load() {
  error.value = ''
  try {
    rows.value = await api.get<SessionListItem[]>('/api/prohibitorum/me/sessions')
  } catch (e) { show(e) }
}

async function revoke(id: string) {
  if (busy.value) return
  busy.value = true
  error.value = ''
  try {
    await api.post('/api/prohibitorum/me/sessions/revoke', { id })
    await load()
  } catch (e) { show(e) } finally { busy.value = false }
}

onMounted(load)
</script>

<template>
  <div class="space-y-4">
    <h1 class="text-lg font-semibold">{{ t('sessions.title') }}</h1>
    <p v-if="error" role="alert" aria-live="polite" class="text-error text-sm">{{ error }}</p>
    <table class="w-full text-sm border-collapse">
      <thead>
        <tr class="text-left text-muted border-b border-default">
          <th class="py-2 pr-4">{{ t('sessions.device') }}</th>
          <th class="py-2 pr-4">{{ t('sessions.ip') }}</th>
          <th class="py-2 pr-4">{{ t('sessions.issuedAt') }}</th>
          <th class="py-2 pr-4">{{ t('sessions.expiresAt') }}</th>
          <th class="py-2 pr-4">{{ t('sessions.actions') }}</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="r in rows" :key="r.id" class="border-b border-default/50">
          <td class="py-2 pr-4">
            {{ r.userAgent || '—' }}
            <UBadge v-if="r.isCurrent" size="sm" color="primary" class="ml-2">{{ t('sessions.current') }}</UBadge>
          </td>
          <td class="py-2 pr-4 font-mono text-xs">{{ r.lastSeenIp }}</td>
          <td class="py-2 pr-4">{{ new Date(r.issuedAt).toLocaleString() }}</td>
          <td class="py-2 pr-4">{{ new Date(r.expiresAt).toLocaleString() }}</td>
          <td class="py-2 pr-4">
            <UButton
              v-if="!r.isCurrent"
              data-test="revoke"
              type="button"
              size="xs"
              color="error"
              variant="soft"
              :disabled="busy"
              @click="revoke(r.id)"
            >
              {{ t('sessions.revoke') }}
            </UButton>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
```

- [ ] **Step 5: Run the test, confirm it passes**

Run: `cd dashboard && npx vitest run src/pages/SessionsView.test.ts`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add dashboard/src/pages/SessionsView.vue dashboard/src/pages/SessionsView.test.ts dashboard/src/locales/en.ts dashboard/src/locales/zh.ts
git commit -m "feat(webui): SessionsView with revoke"
```

---

### Task 6: `CredentialsView` (`/credentials`)

**Goal:** List the caller's passkeys with inline rename and confirmed delete (surfacing the last-passkey error). No add-passkey (out of scope).

**Files:**
- Create: `dashboard/src/pages/CredentialsView.vue`
- Modify: `dashboard/src/locales/en.ts`, `dashboard/src/locales/zh.ts`
- Test: `dashboard/src/pages/CredentialsView.test.ts` (create)

**Acceptance Criteria:**
- [ ] Fetches `GET /api/prohibitorum/me/credentials` and renders one row per credential.
- [ ] Rename POSTs `{id, nickname}` then refetches.
- [ ] Delete (via `ConfirmButton`) POSTs `{id}`; a rejection (e.g. last passkey) surfaces in the `aria-live` region.

**Verify:** `cd dashboard && npx vitest run src/pages/CredentialsView.test.ts` → PASS

**Steps:**

- [ ] **Step 1: Add i18n keys.** In `en.ts`:

```ts
  credentials: { title: 'Passkeys', nickname: 'Nickname', suffix: 'ID suffix', transports: 'Transports', createdAt: 'Added', lastUsed: 'Last used', actions: 'Actions', unnamed: '(unnamed)', renamePrompt: 'New nickname' },
```

In `zh.ts`:

```ts
  credentials: { title: '通行密钥', nickname: '昵称', suffix: 'ID 后缀', transports: '传输方式', createdAt: '添加时间', lastUsed: '最近使用', actions: '操作', unnamed: '（未命名）', renamePrompt: '新昵称' },
```

- [ ] **Step 2: Write the failing test** — create `dashboard/src/pages/CredentialsView.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import zh from '../locales/zh'
import en from '../locales/en'
import CredentialsView from './CredentialsView.vue'

const get = vi.fn()
const post = vi.fn()
vi.mock('../lib/api', () => ({ api: { get: (...a: unknown[]) => get(...a), post: (...a: unknown[]) => post(...a) } }))

function makeI18n() {
  return createI18n({ legacy: false, locale: 'en', fallbackLocale: 'zh', messages: { zh, en } })
}
beforeEach(() => { get.mockReset(); post.mockReset() })

const creds = [
  { id: 1, credentialIdSuffix: 'ab12', nickname: 'Laptop', transports: ['internal'], backupState: false, attestationType: 'none', createdAt: '2026-01-01T00:00:00Z' },
  { id: 2, credentialIdSuffix: 'cd34', nickname: null, transports: ['usb'], backupState: false, attestationType: 'none', createdAt: '2026-01-01T00:00:00Z' },
]

describe('CredentialsView', () => {
  it('lists credentials', async () => {
    get.mockResolvedValueOnce(creds)
    const wrapper = mount(CredentialsView, { global: { plugins: [makeI18n()] } })
    await flushPromises()
    expect(wrapper.findAll('tbody tr').length).toBe(2)
    expect(wrapper.text()).toContain('Laptop')
  })

  it('surfaces a delete rejection (last passkey)', async () => {
    get.mockResolvedValueOnce(creds)
    post.mockRejectedValueOnce({ code: 'last_passkey', message: 'cannot remove last passkey' })
    const wrapper = mount(CredentialsView, { global: { plugins: [makeI18n()] } })
    await flushPromises()
    // arm + confirm the first row's delete
    await wrapper.findAll('[data-test="del"]')[0].trigger('click')
    await wrapper.findAll('[data-test="del"]')[0].trigger('click')
    await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/credentials/delete', { id: 1 })
    expect(wrapper.find('[role="alert"]').text()).toContain('cannot remove last passkey')
  })
})
```

> Note: `ConfirmButton` renders its trigger button with the same `data-test` we pass through; after arming, the confirm button takes the first slot. We forward `data-test="del"` onto the `ConfirmButton` root via an attribute on a wrapping element — see the implementation, which puts `data-test="del"` on the inner buttons by wrapping. To keep the test simple, the implementation wraps each delete in a `<span data-test-wrap>`; the test selects buttons by `[data-test="del"]`. Ensure the `ConfirmButton` buttons receive `data-test="del"` by setting it on the component (Vue forwards fallthrough attrs to the single root). Since `ConfirmButton`'s root is a `<div>`, not a button, instead select via the wrapper: replace the two `findAll('[data-test="del"]')` lines with `wrapper.findAll('button')` filtering is brittle — so the implementation below renders delete as a plain pair of buttons (not ConfirmButton) to keep `data-test="del"` on the actual `<button>`s. Follow the implementation exactly.

- [ ] **Step 3: Run the test, confirm it fails**

Run: `cd dashboard && npx vitest run src/pages/CredentialsView.test.ts`
Expected: FAIL — component missing.

- [ ] **Step 4: Implement `dashboard/src/pages/CredentialsView.vue`** (uses an inline two-step delete so `data-test="del"` sits on the real buttons):

```vue
<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '../lib/api'

interface CredentialView {
  id: number
  credentialIdSuffix: string
  nickname: string | null
  transports: string[]
  backupState: boolean
  attestationType: string
  createdAt: string
  lastUsedAt?: string | null
}

const { t, te } = useI18n()
const rows = ref<CredentialView[]>([])
const error = ref('')
const busy = ref(false)
const armed = ref<number | null>(null)
const editing = ref<number | null>(null)
const draft = ref('')

function show(e: any) {
  const code = e?.code as string | undefined
  error.value = code && te('errors.' + code) ? t('errors.' + code) : (e?.message ?? t('errors.server_error'))
}

async function load() {
  error.value = ''
  try {
    rows.value = await api.get<CredentialView[]>('/api/prohibitorum/me/credentials')
  } catch (e) { show(e) }
}

function startRename(c: CredentialView) {
  editing.value = c.id
  draft.value = c.nickname ?? ''
}

async function saveRename(id: number) {
  if (busy.value) return
  busy.value = true
  error.value = ''
  try {
    await api.post('/api/prohibitorum/me/credentials/rename', { id, nickname: draft.value || null })
    editing.value = null
    await load()
  } catch (e) { show(e) } finally { busy.value = false }
}

async function del(id: number) {
  if (busy.value) return
  busy.value = true
  error.value = ''
  try {
    await api.post('/api/prohibitorum/me/credentials/delete', { id })
    armed.value = null
    await load()
  } catch (e) { show(e); armed.value = null } finally { busy.value = false }
}

onMounted(load)
</script>

<template>
  <div class="space-y-4">
    <h1 class="text-lg font-semibold">{{ t('credentials.title') }}</h1>
    <p v-if="error" role="alert" aria-live="polite" class="text-error text-sm">{{ error }}</p>
    <table class="w-full text-sm border-collapse">
      <thead>
        <tr class="text-left text-muted border-b border-default">
          <th class="py-2 pr-4">{{ t('credentials.nickname') }}</th>
          <th class="py-2 pr-4">{{ t('credentials.suffix') }}</th>
          <th class="py-2 pr-4">{{ t('credentials.transports') }}</th>
          <th class="py-2 pr-4">{{ t('credentials.createdAt') }}</th>
          <th class="py-2 pr-4">{{ t('credentials.actions') }}</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="c in rows" :key="c.id" class="border-b border-default/50">
          <td class="py-2 pr-4">
            <template v-if="editing === c.id">
              <UInput v-model="draft" size="xs" :placeholder="t('credentials.renamePrompt')" />
            </template>
            <template v-else>
              {{ c.nickname || t('credentials.unnamed') }}
            </template>
          </td>
          <td class="py-2 pr-4 font-mono text-xs">…{{ c.credentialIdSuffix }}</td>
          <td class="py-2 pr-4 text-xs">{{ c.transports.join(', ') }}</td>
          <td class="py-2 pr-4">{{ new Date(c.createdAt).toLocaleDateString() }}</td>
          <td class="py-2 pr-4">
            <div class="inline-flex items-center gap-1">
              <template v-if="editing === c.id">
                <UButton type="button" size="xs" :disabled="busy" @click="saveRename(c.id)">{{ t('common.save') }}</UButton>
                <UButton type="button" size="xs" color="neutral" variant="ghost" @click="editing = null">{{ t('common.cancel') }}</UButton>
              </template>
              <template v-else>
                <UButton type="button" size="xs" variant="soft" @click="startRename(c)">{{ t('common.rename') }}</UButton>
                <template v-if="armed === c.id">
                  <UButton data-test="del" type="button" size="xs" color="error" :disabled="busy" @click="del(c.id)">{{ t('common.confirm') }}</UButton>
                  <UButton type="button" size="xs" color="neutral" variant="ghost" @click="armed = null">{{ t('common.cancel') }}</UButton>
                </template>
                <UButton v-else data-test="del" type="button" size="xs" color="error" variant="soft" @click="armed = c.id">{{ t('common.delete') }}</UButton>
              </template>
            </div>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
```

- [ ] **Step 5: Run the test, confirm it passes**

Run: `cd dashboard && npx vitest run src/pages/CredentialsView.test.ts`
Expected: PASS (the first `data-test="del"` click arms, the second confirms → POST + error surfaced)

- [ ] **Step 6: Commit**

```bash
git add dashboard/src/pages/CredentialsView.vue dashboard/src/pages/CredentialsView.test.ts dashboard/src/locales/en.ts dashboard/src/locales/zh.ts
git commit -m "feat(webui): CredentialsView with rename + delete"
```

---

### Task 7: `AccountsView` (`/admin/accounts`)

**Goal:** Admin account table with Disable/Enable (PUT), confirmed Delete, and Reissue-enrollment (→ copyable `/enroll/<token>` URL).

**Files:**
- Create: `dashboard/src/pages/AccountsView.vue`
- Modify: `dashboard/src/locales/en.ts`, `dashboard/src/locales/zh.ts`
- Test: `dashboard/src/pages/AccountsView.test.ts` (create)

**Acceptance Criteria:**
- [ ] Fetches `GET /api/prohibitorum/accounts` and renders one row per account.
- [ ] Disable/Enable issues `PUT /api/prohibitorum/accounts/{id}` with body `{displayName, role, disabled}` (current displayName+role, flipped disabled), then refetches.
- [ ] Reissue posts `{id}` and shows the returned URL via `CopyableUrl`.
- [ ] Delete (confirm) posts `{id}`; a rejection (last admin) surfaces inline.

**Verify:** `cd dashboard && npx vitest run src/pages/AccountsView.test.ts` → PASS

**Steps:**

- [ ] **Step 1: Add i18n keys.** In `en.ts`:

```ts
  accounts: { title: 'Accounts', username: 'Username', displayName: 'Display name', role: 'Role', status: 'Status', lastSignIn: 'Last sign-in', actions: 'Actions', active: 'Active', disabled: 'Disabled', reissue: 'Reissue enrollment', reissued: 'Enrollment URL (copy now — shown once):', never: 'Never' },
```

In `zh.ts`:

```ts
  accounts: { title: '账户', username: '用户名', displayName: '显示名称', role: '角色', status: '状态', lastSignIn: '最近登录', actions: '操作', active: '正常', disabled: '已禁用', reissue: '重新签发注册链接', reissued: '注册链接（仅显示一次，请立即复制）：', never: '从未' },
```

- [ ] **Step 2: Write the failing test** — create `dashboard/src/pages/AccountsView.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import zh from '../locales/zh'
import en from '../locales/en'
import AccountsView from './AccountsView.vue'

const get = vi.fn()
const post = vi.fn()
const put = vi.fn()
vi.mock('../lib/api', () => ({ api: {
  get: (...a: unknown[]) => get(...a),
  post: (...a: unknown[]) => post(...a),
  put: (...a: unknown[]) => put(...a),
} }))

function makeI18n() {
  return createI18n({ legacy: false, locale: 'en', fallbackLocale: 'zh', messages: { zh, en } })
}
beforeEach(() => { get.mockReset(); post.mockReset(); put.mockReset() })

const accts = [
  { id: 1, username: 'admin', displayName: 'Admin', role: 'admin', disabled: false, createdAt: '2026-01-01T00:00:00Z', updatedAt: '2026-01-01T00:00:00Z' },
  { id: 2, username: 'bob', displayName: 'Bob', role: 'user', disabled: false, createdAt: '2026-01-01T00:00:00Z', updatedAt: '2026-01-01T00:00:00Z' },
]

describe('AccountsView', () => {
  it('renders accounts', async () => {
    get.mockResolvedValueOnce(accts)
    const wrapper = mount(AccountsView, { global: { plugins: [makeI18n()] } })
    await flushPromises()
    expect(wrapper.findAll('tbody tr').length).toBe(2)
  })

  it('disable sends PUT with current displayName+role and flipped disabled', async () => {
    get.mockResolvedValueOnce(accts)
    put.mockResolvedValueOnce({ ...accts[1], disabled: true })
    get.mockResolvedValueOnce([accts[0], { ...accts[1], disabled: true }])
    const wrapper = mount(AccountsView, { global: { plugins: [makeI18n()] } })
    await flushPromises()
    // bob is row 2 (index 1); click its disable toggle
    await wrapper.findAll('[data-test="toggle"]')[1].trigger('click')
    await flushPromises()
    expect(put).toHaveBeenCalledWith('/api/prohibitorum/accounts/2', { displayName: 'Bob', role: 'user', disabled: true })
  })

  it('reissue shows the returned url', async () => {
    get.mockResolvedValueOnce(accts)
    post.mockResolvedValueOnce({ url: 'http://x/enroll/tok', expiresAt: '2026-02-01T00:00:00Z' })
    const wrapper = mount(AccountsView, { global: { plugins: [makeI18n()] } })
    await flushPromises()
    await wrapper.findAll('[data-test="reissue"]')[1].trigger('click')
    await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/accounts/reissue-enrollment', { id: 2 })
    expect(wrapper.text()).toContain('http://x/enroll/tok')
  })
})
```

- [ ] **Step 3: Run the test, confirm it fails**

Run: `cd dashboard && npx vitest run src/pages/AccountsView.test.ts`
Expected: FAIL — component missing.

- [ ] **Step 4: Implement `dashboard/src/pages/AccountsView.vue`:**

```vue
<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '../lib/api'
import CopyableUrl from '../components/CopyableUrl.vue'

interface AccountView {
  id: number
  username: string
  displayName: string
  role: string
  disabled: boolean
  createdAt: string
  updatedAt: string
  lastSignInAt?: string | null
}

const { t, te } = useI18n()
const rows = ref<AccountView[]>([])
const error = ref('')
const busy = ref(false)
const armed = ref<number | null>(null)
const reissued = ref<{ id: number; url: string } | null>(null)

function show(e: any) {
  const code = e?.code as string | undefined
  error.value = code && te('errors.' + code) ? t('errors.' + code) : (e?.message ?? t('errors.server_error'))
}

async function load() {
  error.value = ''
  try {
    rows.value = await api.get<AccountView[]>('/api/prohibitorum/accounts')
  } catch (e) { show(e) }
}

async function toggle(a: AccountView) {
  if (busy.value) return
  busy.value = true
  error.value = ''
  try {
    await api.put(`/api/prohibitorum/accounts/${a.id}`, { displayName: a.displayName, role: a.role, disabled: !a.disabled })
    await load()
  } catch (e) { show(e) } finally { busy.value = false }
}

async function del(id: number) {
  if (busy.value) return
  busy.value = true
  error.value = ''
  try {
    await api.post('/api/prohibitorum/accounts/delete', { id })
    armed.value = null
    await load()
  } catch (e) { show(e); armed.value = null } finally { busy.value = false }
}

async function reissue(id: number) {
  if (busy.value) return
  busy.value = true
  error.value = ''
  reissued.value = null
  try {
    const r = await api.post<{ url: string }>('/api/prohibitorum/accounts/reissue-enrollment', { id })
    reissued.value = { id, url: r.url }
  } catch (e) { show(e) } finally { busy.value = false }
}

onMounted(load)
</script>

<template>
  <div class="space-y-4">
    <h1 class="text-lg font-semibold">{{ t('accounts.title') }}</h1>
    <p v-if="error" role="alert" aria-live="polite" class="text-error text-sm">{{ error }}</p>
    <table class="w-full text-sm border-collapse">
      <thead>
        <tr class="text-left text-muted border-b border-default">
          <th class="py-2 pr-4">{{ t('accounts.username') }}</th>
          <th class="py-2 pr-4">{{ t('accounts.displayName') }}</th>
          <th class="py-2 pr-4">{{ t('accounts.role') }}</th>
          <th class="py-2 pr-4">{{ t('accounts.status') }}</th>
          <th class="py-2 pr-4">{{ t('accounts.lastSignIn') }}</th>
          <th class="py-2 pr-4">{{ t('accounts.actions') }}</th>
        </tr>
      </thead>
      <tbody>
        <template v-for="a in rows" :key="a.id">
          <tr class="border-b border-default/50">
            <td class="py-2 pr-4 font-mono text-xs">{{ a.username }}</td>
            <td class="py-2 pr-4">{{ a.displayName }}</td>
            <td class="py-2 pr-4">{{ a.role }}</td>
            <td class="py-2 pr-4">
              <UBadge size="sm" :color="a.disabled ? 'error' : 'success'">
                {{ a.disabled ? t('accounts.disabled') : t('accounts.active') }}
              </UBadge>
            </td>
            <td class="py-2 pr-4">{{ a.lastSignInAt ? new Date(a.lastSignInAt).toLocaleString() : t('accounts.never') }}</td>
            <td class="py-2 pr-4">
              <div class="inline-flex items-center gap-1">
                <UButton data-test="toggle" type="button" size="xs" variant="soft" :disabled="busy" @click="toggle(a)">
                  {{ a.disabled ? t('common.enable') : t('common.disable') }}
                </UButton>
                <UButton data-test="reissue" type="button" size="xs" variant="soft" :disabled="busy" @click="reissue(a.id)">
                  {{ t('accounts.reissue') }}
                </UButton>
                <template v-if="armed === a.id">
                  <UButton type="button" size="xs" color="error" :disabled="busy" @click="del(a.id)">{{ t('common.confirm') }}</UButton>
                  <UButton type="button" size="xs" color="neutral" variant="ghost" @click="armed = null">{{ t('common.cancel') }}</UButton>
                </template>
                <UButton v-else type="button" size="xs" color="error" variant="soft" @click="armed = a.id">{{ t('common.delete') }}</UButton>
              </div>
            </td>
          </tr>
          <tr v-if="reissued && reissued.id === a.id">
            <td colspan="6" class="py-2 pr-4">
              <p class="text-xs text-muted mb-1">{{ t('accounts.reissued') }}</p>
              <CopyableUrl :url="reissued.url" />
            </td>
          </tr>
        </template>
      </tbody>
    </table>
  </div>
</template>
```

- [ ] **Step 5: Run the test, confirm it passes**

Run: `cd dashboard && npx vitest run src/pages/AccountsView.test.ts`
Expected: PASS (3 tests)

- [ ] **Step 6: Commit**

```bash
git add dashboard/src/pages/AccountsView.vue dashboard/src/pages/AccountsView.test.ts dashboard/src/locales/en.ts dashboard/src/locales/zh.ts
git commit -m "feat(webui): AccountsView with disable/enable, delete, reissue"
```

---

### Task 8: `InvitationsView` (`/admin/invitations`)

**Goal:** Admin invitations list with Create (role select → copyable enroll URL) and Revoke.

**Files:**
- Create: `dashboard/src/pages/InvitationsView.vue`
- Modify: `dashboard/src/locales/en.ts`, `dashboard/src/locales/zh.ts`
- Test: `dashboard/src/pages/InvitationsView.test.ts` (create)

**Acceptance Criteria:**
- [ ] Fetches `GET /api/prohibitorum/invitations` and renders one row per invitation (the row exposes its URL via `CopyableUrl`).
- [ ] Create posts `{role}` and surfaces the returned URL, then refetches.
- [ ] Revoke posts `{token}` then refetches.

**Verify:** `cd dashboard && npx vitest run src/pages/InvitationsView.test.ts` → PASS

**Steps:**

- [ ] **Step 1: Add i18n keys.** In `en.ts`:

```ts
  invitations: { title: 'Invitations', role: 'Role', createdAt: 'Created', expiresAt: 'Expires', url: 'Enroll URL', actions: 'Actions', create: 'Create invitation', roleUser: 'user', roleAdmin: 'admin', created: 'Invitation created (copy the URL):' },
```

In `zh.ts`:

```ts
  invitations: { title: '邀请', role: '角色', createdAt: '创建时间', expiresAt: '过期时间', url: '注册链接', actions: '操作', create: '创建邀请', roleUser: 'user', roleAdmin: 'admin', created: '邀请已创建（请复制链接）：' },
```

- [ ] **Step 2: Write the failing test** — create `dashboard/src/pages/InvitationsView.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import zh from '../locales/zh'
import en from '../locales/en'
import InvitationsView from './InvitationsView.vue'

const get = vi.fn()
const post = vi.fn()
vi.mock('../lib/api', () => ({ api: { get: (...a: unknown[]) => get(...a), post: (...a: unknown[]) => post(...a) } }))

function makeI18n() {
  return createI18n({ legacy: false, locale: 'en', fallbackLocale: 'zh', messages: { zh, en } })
}
beforeEach(() => { get.mockReset(); post.mockReset() })

const invites = [
  { token: 'tok1', url: 'http://x/enroll/tok1', role: 'user', createdAt: '2026-01-01T00:00:00Z', expiresAt: '2026-02-01T00:00:00Z' },
]

describe('InvitationsView', () => {
  it('lists invitations with their urls', async () => {
    get.mockResolvedValueOnce(invites)
    const wrapper = mount(InvitationsView, { global: { plugins: [makeI18n()] } })
    await flushPromises()
    expect(wrapper.findAll('tbody tr').length).toBe(1)
    expect(wrapper.text()).toContain('http://x/enroll/tok1')
  })

  it('creates an invitation then refetches', async () => {
    get.mockResolvedValueOnce([])                                  // initial
    post.mockResolvedValueOnce({ url: 'http://x/enroll/new', expiresAt: '2026-02-01T00:00:00Z' })
    get.mockResolvedValueOnce(invites)                             // refetch
    const wrapper = mount(InvitationsView, { global: { plugins: [makeI18n()] } })
    await flushPromises()
    await wrapper.find('[data-test="create"]').trigger('click')
    await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/invitations', { role: 'user' })
    expect(get).toHaveBeenCalledTimes(2)
  })

  it('revokes by token', async () => {
    get.mockResolvedValueOnce(invites)
    post.mockResolvedValueOnce(undefined)
    get.mockResolvedValueOnce([])
    const wrapper = mount(InvitationsView, { global: { plugins: [makeI18n()] } })
    await flushPromises()
    await wrapper.find('[data-test="revoke"]').trigger('click')
    await wrapper.find('[data-test="revoke"]').trigger('click') // arm + confirm
    await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/invitations/revoke', { token: 'tok1' })
  })
})
```

- [ ] **Step 3: Run the test, confirm it fails**

Run: `cd dashboard && npx vitest run src/pages/InvitationsView.test.ts`
Expected: FAIL — component missing.

- [ ] **Step 4: Implement `dashboard/src/pages/InvitationsView.vue`:**

```vue
<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '../lib/api'
import CopyableUrl from '../components/CopyableUrl.vue'

interface InvitationView {
  token: string
  url: string
  role: string
  createdAt: string
  expiresAt: string
}

const { t, te } = useI18n()
const rows = ref<InvitationView[]>([])
const error = ref('')
const busy = ref(false)
const newRole = ref('user')
const armed = ref<string | null>(null)
const created = ref<string>('')

function show(e: any) {
  const code = e?.code as string | undefined
  error.value = code && te('errors.' + code) ? t('errors.' + code) : (e?.message ?? t('errors.server_error'))
}

async function load() {
  error.value = ''
  try {
    rows.value = await api.get<InvitationView[]>('/api/prohibitorum/invitations')
  } catch (e) { show(e) }
}

async function create() {
  if (busy.value) return
  busy.value = true
  error.value = ''
  created.value = ''
  try {
    const r = await api.post<{ url: string }>('/api/prohibitorum/invitations', { role: newRole.value })
    created.value = r.url
    await load()
  } catch (e) { show(e) } finally { busy.value = false }
}

async function revoke(token: string) {
  if (busy.value) return
  busy.value = true
  error.value = ''
  try {
    await api.post('/api/prohibitorum/invitations/revoke', { token })
    armed.value = null
    await load()
  } catch (e) { show(e); armed.value = null } finally { busy.value = false }
}

onMounted(load)
</script>

<template>
  <div class="space-y-4">
    <h1 class="text-lg font-semibold">{{ t('invitations.title') }}</h1>
    <p v-if="error" role="alert" aria-live="polite" class="text-error text-sm">{{ error }}</p>

    <div class="flex items-end gap-2">
      <USelect v-model="newRole" :items="[t('invitations.roleUser'), t('invitations.roleAdmin')]" class="w-40" />
      <UButton data-test="create" type="button" :disabled="busy" @click="create">{{ t('invitations.create') }}</UButton>
    </div>
    <div v-if="created" class="space-y-1">
      <p class="text-xs text-muted">{{ t('invitations.created') }}</p>
      <CopyableUrl :url="created" />
    </div>

    <table class="w-full text-sm border-collapse">
      <thead>
        <tr class="text-left text-muted border-b border-default">
          <th class="py-2 pr-4">{{ t('invitations.role') }}</th>
          <th class="py-2 pr-4">{{ t('invitations.url') }}</th>
          <th class="py-2 pr-4">{{ t('invitations.expiresAt') }}</th>
          <th class="py-2 pr-4">{{ t('invitations.actions') }}</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="inv in rows" :key="inv.token" class="border-b border-default/50">
          <td class="py-2 pr-4">{{ inv.role }}</td>
          <td class="py-2 pr-4 w-1/2"><CopyableUrl :url="inv.url" /></td>
          <td class="py-2 pr-4">{{ new Date(inv.expiresAt).toLocaleDateString() }}</td>
          <td class="py-2 pr-4">
            <div class="inline-flex items-center gap-1">
              <template v-if="armed === inv.token">
                <UButton data-test="revoke" type="button" size="xs" color="error" :disabled="busy" @click="revoke(inv.token)">{{ t('common.confirm') }}</UButton>
                <UButton type="button" size="xs" color="neutral" variant="ghost" @click="armed = null">{{ t('common.cancel') }}</UButton>
              </template>
              <UButton v-else data-test="revoke" type="button" size="xs" color="error" variant="soft" @click="armed = inv.token">{{ t('common.revoke') }}</UButton>
            </div>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
```

> Note on the `USelect` items: this uses plain string items (`'user'`/`'admin'`) so `newRole` holds the role string directly, which is exactly what the create body needs. The labels `roleUser`/`roleAdmin` are literally `'user'`/`'admin'` in both locales, so `newRole` equals the role value. The test asserts `{ role: 'user' }` (the default).

- [ ] **Step 5: Run the test, confirm it passes**

Run: `cd dashboard && npx vitest run src/pages/InvitationsView.test.ts`
Expected: PASS (3 tests)

- [ ] **Step 6: Commit**

```bash
git add dashboard/src/pages/InvitationsView.vue dashboard/src/pages/InvitationsView.test.ts dashboard/src/locales/en.ts dashboard/src/locales/zh.ts
git commit -m "feat(webui): InvitationsView with create + revoke"
```

---

### Task 9: `EnrollView` (`/enroll/:token`)

**Goal:** The passkey-enrollment page that previews the token, collects identity fields for bootstrap/invite (the manual-test path), drives the ceremony, and auto-logs-in by full-navigating to `/`.

**Files:**
- Create: `dashboard/src/pages/EnrollView.vue`
- Modify: `dashboard/src/locales/en.ts`, `dashboard/src/locales/zh.ts`
- Test: `dashboard/src/pages/EnrollView.test.ts` (create)

**Acceptance Criteria:**
- [ ] On mount, `GET /api/prohibitorum/enrollments/{token}` previews the intent; a GET error routes to `/error?code=…`.
- [ ] For `bootstrap`/`invite` intents, username + displayName inputs are shown and required; for `reset`, the target name is shown with no identity inputs.
- [ ] Clicking Register calls `passkeyRegister(token, fields)` and on success calls `window.location.assign('/')`.

**Verify:** `cd dashboard && npx vitest run src/pages/EnrollView.test.ts` → PASS

**Steps:**

- [ ] **Step 1: Add i18n keys.** In `en.ts`:

```ts
  enroll: { titleBootstrap: 'Set up the first admin', titleInvite: 'Create your account', titleReset: 'Re-register your passkey', forTarget: 'for {name}', username: 'Username', displayName: 'Display name', nickname: 'Passkey name (optional)', register: 'Register passkey', registering: 'Registering…' },
```

In `zh.ts`:

```ts
  enroll: { titleBootstrap: '设置首位管理员', titleInvite: '创建您的账户', titleReset: '重新注册通行密钥', forTarget: '为 {name}', username: '用户名', displayName: '显示名称', nickname: '通行密钥名称（可选）', register: '注册通行密钥', registering: '注册中…' },
```

- [ ] **Step 2: Write the failing test** — create `dashboard/src/pages/EnrollView.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { createRouter, createMemoryHistory } from 'vue-router'
import zh from '../locales/zh'
import en from '../locales/en'
import EnrollView from './EnrollView.vue'

const get = vi.fn()
vi.mock('../lib/api', () => ({ api: { get: (...a: unknown[]) => get(...a) } }))

const passkeyRegister = vi.fn()
vi.mock('../lib/webauthn', () => ({ passkeyRegister: (...a: unknown[]) => passkeyRegister(...a) }))

function makeI18n() {
  return createI18n({ legacy: false, locale: 'en', fallbackLocale: 'zh', messages: { zh, en } })
}
function makeRouter() {
  return createRouter({ history: createMemoryHistory(), routes: [
    { path: '/enroll/:token', component: EnrollView },
    { path: '/error', component: { template: '<div/>' } },
  ] })
}

beforeEach(() => { get.mockReset(); passkeyRegister.mockReset() })

async function mountAt(token: string) {
  const router = makeRouter()
  router.push(`/enroll/${token}`)
  await router.isReady()
  return mount(EnrollView, { global: { plugins: [makeI18n(), router] } })
}

describe('EnrollView', () => {
  it('bootstrap shows username + displayName inputs and registers', async () => {
    get.mockResolvedValueOnce({ intent: 'bootstrap', expiresAt: '2026-02-01T00:00:00Z' })
    passkeyRegister.mockResolvedValueOnce({ id: 1, username: 'admin', displayName: 'Admin', role: 'admin' })
    const assign = vi.fn()
    Object.defineProperty(window, 'location', { value: { assign }, writable: true })

    const wrapper = await mountAt('tok')
    await flushPromises()
    const inputs = wrapper.findAll('input')
    expect(inputs.length).toBeGreaterThanOrEqual(2) // username + displayName (+ optional nickname)
    await inputs[0].setValue('admin')
    await inputs[1].setValue('Admin')
    await wrapper.find('[data-test="register"]').trigger('click')
    await flushPromises()
    expect(passkeyRegister).toHaveBeenCalledWith('tok', expect.objectContaining({ username: 'admin', displayName: 'Admin' }))
    expect(assign).toHaveBeenCalledWith('/')
  })

  it('reset shows the target name and no identity inputs', async () => {
    get.mockResolvedValueOnce({ intent: 'reset', target: { username: 'bob', displayName: 'Bob' }, expiresAt: '2026-02-01T00:00:00Z' })
    const wrapper = await mountAt('tok')
    await flushPromises()
    expect(wrapper.text()).toContain('Bob')
    // No username/displayName text inputs for reset (nickname optional only).
    const textInputs = wrapper.findAll('input[type="text"]')
    expect(textInputs.length).toBeLessThanOrEqual(1)
  })
})
```

- [ ] **Step 3: Run the test, confirm it fails**

Run: `cd dashboard && npx vitest run src/pages/EnrollView.test.ts`
Expected: FAIL — component missing.

- [ ] **Step 4: Implement `dashboard/src/pages/EnrollView.vue`:**

```vue
<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { api } from '../lib/api'
import { passkeyRegister, type EnrollFields } from '../lib/webauthn'
import LocaleSwitcher from '../components/LocaleSwitcher.vue'

interface EnrollmentPreview {
  intent: string
  target?: { username: string; displayName: string } | null
  expiresAt: string
}

const { t, te } = useI18n()
const route = useRoute()
const router = useRouter()
const token = computed(() => String(route.params.token))

const preview = ref<EnrollmentPreview | null>(null)
const username = ref('')
const displayName = ref('')
const nickname = ref('')
const error = ref('')
const busy = ref(false)

// Bootstrap & invite require the user to choose username + displayName (server
// validates). Reset targets an existing account — no identity inputs.
const needsIdentity = computed(() => preview.value?.intent === 'bootstrap' || preview.value?.intent === 'invite')

const title = computed(() => {
  switch (preview.value?.intent) {
    case 'invite': return t('enroll.titleInvite')
    case 'reset': return t('enroll.titleReset')
    default: return t('enroll.titleBootstrap')
  }
})

function show(e: any) {
  const code = e?.code as string | undefined
  error.value = code && te('errors.' + code) ? t('errors.' + code) : (e?.message ?? t('errors.server_error'))
}

onMounted(async () => {
  try {
    preview.value = await api.get<EnrollmentPreview>(`/api/prohibitorum/enrollments/${encodeURIComponent(token.value)}`)
  } catch (e: any) {
    router.replace({ path: '/error', query: { code: e?.code ?? 'server_error' } })
  }
})

async function register() {
  if (busy.value) return
  busy.value = true
  error.value = ''
  try {
    const fields: EnrollFields = { nickname: nickname.value || undefined }
    if (needsIdentity.value) {
      fields.username = username.value
      fields.displayName = displayName.value
    }
    await passkeyRegister(token.value, fields)
    // Full navigation so the freshly-set session cookie is in play and the store reloads.
    window.location.assign('/')
  } catch (e) { show(e) } finally { busy.value = false }
}
</script>

<template>
  <div class="min-h-screen flex flex-col items-center justify-center gap-6 p-4 bg-default">
    <header class="w-full max-w-sm flex items-center justify-between">
      <span class="text-lg font-semibold">{{ t('app.name') }}</span>
      <LocaleSwitcher />
    </header>
    <UCard v-if="preview" class="w-full max-w-sm">
      <h1 class="text-xl font-semibold text-center">{{ title }}</h1>
      <p v-if="preview.target" class="text-center text-muted mt-1">
        {{ t('enroll.forTarget', { name: preview.target.displayName }) }}
      </p>

      <div class="mt-6 flex flex-col gap-3">
        <template v-if="needsIdentity">
          <UFormField :label="t('enroll.username')">
            <UInput v-model="username" type="text" autocomplete="username" />
          </UFormField>
          <UFormField :label="t('enroll.displayName')">
            <UInput v-model="displayName" type="text" autocomplete="name" />
          </UFormField>
        </template>
        <UFormField :label="t('enroll.nickname')">
          <UInput v-model="nickname" type="text" />
        </UFormField>

        <p v-if="error" role="alert" aria-live="polite" class="text-error text-sm">{{ error }}</p>

        <UButton data-test="register" type="button" block :loading="busy" :disabled="busy" @click="register">
          {{ busy ? t('enroll.registering') : t('enroll.register') }}
        </UButton>
      </div>
    </UCard>
    <div v-else class="flex justify-center mt-8">
      <UIcon name="i-lucide-loader-2" class="size-8 animate-spin text-muted" />
    </div>
  </div>
</template>
```

- [ ] **Step 5: Run the test, confirm it passes**

Run: `cd dashboard && npx vitest run src/pages/EnrollView.test.ts`
Expected: PASS (2 tests)

- [ ] **Step 6: Commit**

```bash
git add dashboard/src/pages/EnrollView.vue dashboard/src/pages/EnrollView.test.ts dashboard/src/locales/en.ts dashboard/src/locales/zh.ts
git commit -m "feat(webui): EnrollView passkey enrollment + auto-login"
```

---

### Task 10: `DashboardLayout` + `AppSidebar`

**Goal:** The sidebar shell (left nav + header with LocaleSwitcher, displayName, Logout) wrapping `<RouterView>`. The admin nav group renders only for admins.

**Files:**
- Create: `dashboard/src/pages/DashboardLayout.vue`
- Create: `dashboard/src/components/AppSidebar.vue`
- Modify: `dashboard/src/locales/en.ts`, `dashboard/src/locales/zh.ts`
- Test: `dashboard/src/components/AppSidebar.test.ts` (create)

**Acceptance Criteria:**
- [ ] `AppSidebar` shows the user nav links always and the admin group only when `session.isAdmin`.
- [ ] `DashboardLayout` renders the header (app name, LocaleSwitcher, displayName, Logout) and `<RouterView>`.

**Verify:** `cd dashboard && npx vitest run src/components/AppSidebar.test.ts` → PASS

**Steps:**

- [ ] **Step 1: Add nav i18n keys.** In `en.ts`:

```ts
  nav: { profile: 'Profile', sessions: 'Sessions', credentials: 'Passkeys', admin: 'Admin', accounts: 'Accounts', invitations: 'Invitations', logout: 'Log out' },
```

In `zh.ts`:

```ts
  nav: { profile: '个人资料', sessions: '会话', credentials: '通行密钥', admin: '管理', accounts: '账户', invitations: '邀请', logout: '退出登录' },
```

- [ ] **Step 2: Write the failing test** — create `dashboard/src/components/AppSidebar.test.ts`:

```ts
import { describe, it, expect, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import zh from '../locales/zh'
import en from '../locales/en'
import { useSessionStore } from '../stores/session'
import AppSidebar from './AppSidebar.vue'

function makeI18n() {
  return createI18n({ legacy: false, locale: 'en', fallbackLocale: 'zh', messages: { zh, en } })
}
function makeRouter() {
  return createRouter({ history: createMemoryHistory(), routes: [
    { path: '/', component: { template: '<div/>' } },
    { path: '/sessions', component: { template: '<div/>' } },
    { path: '/credentials', component: { template: '<div/>' } },
    { path: '/admin/accounts', component: { template: '<div/>' } },
    { path: '/admin/invitations', component: { template: '<div/>' } },
  ] })
}

beforeEach(() => setActivePinia(createPinia()))

describe('AppSidebar', () => {
  it('hides the admin group for non-admins', async () => {
    const s = useSessionStore()
    s.me = { id: 1, username: 'u', displayName: 'U', role: 'user' }
    const wrapper = mount(AppSidebar, { global: { plugins: [makeI18n(), makeRouter()] } })
    expect(wrapper.text()).toContain(en.nav.profile)
    expect(wrapper.text()).not.toContain(en.nav.accounts)
  })

  it('shows the admin group for admins', async () => {
    const s = useSessionStore()
    s.me = { id: 1, username: 'a', displayName: 'A', role: 'admin' }
    const wrapper = mount(AppSidebar, { global: { plugins: [makeI18n(), makeRouter()] } })
    expect(wrapper.text()).toContain(en.nav.accounts)
    expect(wrapper.text()).toContain(en.nav.invitations)
  })
})
```

- [ ] **Step 3: Run the test, confirm it fails**

Run: `cd dashboard && npx vitest run src/components/AppSidebar.test.ts`
Expected: FAIL — component missing.

- [ ] **Step 4: Implement `dashboard/src/components/AppSidebar.vue`:**

```vue
<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { useSessionStore } from '../stores/session'

const { t } = useI18n()
const session = useSessionStore()

const userLinks = [
  { to: '/', label: 'nav.profile', icon: 'i-lucide-user' },
  { to: '/sessions', label: 'nav.sessions', icon: 'i-lucide-monitor' },
  { to: '/credentials', label: 'nav.credentials', icon: 'i-lucide-key-round' },
]
const adminLinks = [
  { to: '/admin/accounts', label: 'nav.accounts', icon: 'i-lucide-users' },
  { to: '/admin/invitations', label: 'nav.invitations', icon: 'i-lucide-mail-plus' },
]
</script>

<template>
  <nav class="flex flex-col gap-1 p-3 w-56 shrink-0 border-r border-default min-h-screen">
    <RouterLink
      v-for="l in userLinks"
      :key="l.to"
      :to="l.to"
      class="flex items-center gap-2 px-3 py-2 rounded text-sm hover:bg-elevated"
      active-class="bg-elevated font-medium"
      exact-active-class="bg-elevated font-medium"
    >
      <UIcon :name="l.icon" class="size-4" />
      {{ t(l.label) }}
    </RouterLink>

    <template v-if="session.isAdmin">
      <div class="mt-4 mb-1 px-3 text-xs uppercase tracking-wide text-muted">{{ t('nav.admin') }}</div>
      <RouterLink
        v-for="l in adminLinks"
        :key="l.to"
        :to="l.to"
        class="flex items-center gap-2 px-3 py-2 rounded text-sm hover:bg-elevated"
        active-class="bg-elevated font-medium"
      >
        <UIcon :name="l.icon" class="size-4" />
        {{ t(l.label) }}
      </RouterLink>
    </template>
  </nav>
</template>
```

- [ ] **Step 5: Implement `dashboard/src/pages/DashboardLayout.vue`:**

```vue
<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { useSessionStore } from '../stores/session'
import AppSidebar from '../components/AppSidebar.vue'
import LocaleSwitcher from '../components/LocaleSwitcher.vue'

const { t } = useI18n()
const router = useRouter()
const session = useSessionStore()

function logout() {
  router.push('/logout')
}
</script>

<template>
  <div class="min-h-screen flex bg-default">
    <AppSidebar />
    <div class="flex-1 flex flex-col">
      <header class="flex items-center justify-between px-6 py-3 border-b border-default">
        <span class="text-lg font-semibold">{{ t('app.name') }}</span>
        <div class="flex items-center gap-3">
          <span class="text-sm text-muted">{{ session.me?.displayName }}</span>
          <LocaleSwitcher />
          <UButton type="button" size="sm" color="neutral" variant="ghost" @click="logout">
            {{ t('nav.logout') }}
          </UButton>
        </div>
      </header>
      <main class="flex-1 p-6">
        <RouterView />
      </main>
    </div>
  </div>
</template>
```

- [ ] **Step 6: Run the test, confirm it passes**

Run: `cd dashboard && npx vitest run src/components/AppSidebar.test.ts`
Expected: PASS (2 tests)

- [ ] **Step 7: Commit**

```bash
git add dashboard/src/components/AppSidebar.vue dashboard/src/components/AppSidebar.test.ts dashboard/src/pages/DashboardLayout.vue dashboard/src/locales/en.ts dashboard/src/locales/zh.ts
git commit -m "feat(webui): DashboardLayout sidebar shell + role-gated AppSidebar"
```

---

### Task 11: Router restructure + auth guard + App.vue/CenteredLayout refactor + rebuild dist

**Goal:** Wire everything: nested layout routes, `meta` flags, the `beforeEach` guard, the `App.vue` chrome refactor (so the dashboard isn't trapped in the centered card), and the **first dist rebuild + commit**.

**Files:**
- Modify: `dashboard/src/router.ts`
- Modify: `dashboard/src/App.vue`
- Create: `dashboard/src/pages/CenteredLayout.vue`
- Test: `dashboard/src/router.test.ts` (create)
- Rebuild + commit: `pkg/webui/dist`

**Acceptance Criteria:**
- [ ] Unauthenticated access to a `requiresAuth` route redirects to `/login?return_to=<path>`.
- [ ] A non-admin reaching `/admin/*` is redirected to `/`; an admin passes.
- [ ] Public routes (`/login`, `/consent`, `/logout`, `/error`, `/enroll/:token`) are reachable without a session.
- [ ] `npm run build` succeeds and `pkg/webui/dist` is rebuilt and committed.

**Verify:** `cd dashboard && npx vitest run src/router.test.ts && npm run build` → PASS + build succeeds

**Steps:**

- [ ] **Step 1: Write the failing guard test** — create `dashboard/src/router.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { installGuard } from './router'
import { createRouter, createMemoryHistory } from 'vue-router'
import { useSessionStore } from './stores/session'

const get = vi.fn()
vi.mock('./lib/api', () => ({ api: { get: (...a: unknown[]) => get(...a) } }))

function buildRouter() {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div/>' }, meta: { requiresAuth: true } },
      { path: '/admin/accounts', component: { template: '<div/>' }, meta: { requiresAuth: true, requiresAdmin: true } },
      { path: '/login', component: { template: '<div/>' } },
      { path: '/enroll/:token', component: { template: '<div/>' } },
    ],
  })
  installGuard(router)
  return router
}

beforeEach(() => { setActivePinia(createPinia()); get.mockReset() })

describe('router guard', () => {
  it('redirects unauthenticated users to /login with return_to', async () => {
    get.mockRejectedValue({ code: 'no_session', message: 'x' })
    const router = buildRouter()
    await router.push('/')
    expect(router.currentRoute.value.path).toBe('/login')
    expect(router.currentRoute.value.query.return_to).toBe('/')
  })

  it('redirects non-admins away from admin routes', async () => {
    get.mockResolvedValue({ id: 1, username: 'u', displayName: 'U', role: 'user' })
    const router = buildRouter()
    await router.push('/admin/accounts')
    expect(router.currentRoute.value.path).toBe('/')
  })

  it('lets admins into admin routes', async () => {
    get.mockResolvedValue({ id: 1, username: 'a', displayName: 'A', role: 'admin' })
    const router = buildRouter()
    await router.push('/admin/accounts')
    expect(router.currentRoute.value.path).toBe('/admin/accounts')
  })

  it('allows public routes without a session', async () => {
    get.mockRejectedValue({ code: 'no_session', message: 'x' })
    const router = buildRouter()
    await router.push('/enroll/tok')
    expect(router.currentRoute.value.path).toBe('/enroll/tok')
  })
})
```

> Note: the test imports a named `installGuard(router)` helper so the guard logic is verified against a minimal route table (avoids importing every real view/layout). The real router calls `installGuard(router)` on the production instance.

- [ ] **Step 2: Run the test, confirm it fails**

Run: `cd dashboard && npx vitest run src/router.test.ts`
Expected: FAIL — `installGuard` not exported.

- [ ] **Step 3: Implement `dashboard/src/router.ts`** (replace entirely):

```ts
import { createRouter, createWebHistory, type Router } from 'vue-router'
import { useSessionStore } from './stores/session'

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    // In-layout dashboard routes (sidebar shell).
    {
      path: '/',
      component: () => import('./pages/DashboardLayout.vue'),
      children: [
        { path: '', name: 'profile', component: () => import('./pages/ProfileView.vue'), meta: { requiresAuth: true } },
        { path: 'sessions', name: 'sessions', component: () => import('./pages/SessionsView.vue'), meta: { requiresAuth: true } },
        { path: 'credentials', name: 'credentials', component: () => import('./pages/CredentialsView.vue'), meta: { requiresAuth: true } },
        { path: 'admin/accounts', name: 'admin-accounts', component: () => import('./pages/AccountsView.vue'), meta: { requiresAuth: true, requiresAdmin: true } },
        { path: 'admin/invitations', name: 'admin-invitations', component: () => import('./pages/InvitationsView.vue'), meta: { requiresAuth: true, requiresAdmin: true } },
      ],
    },
    // Public auth pages (centered card chrome). Pathless parent + absolute child
    // paths is the canonical vue-router layout pattern (no '/' collision).
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
    // Public, no layout.
    { path: '/enroll/:token', name: 'enroll', component: () => import('./pages/EnrollView.vue') },
    // Unknown paths fall back to the dashboard; the guard bounces to /login if unauthenticated.
    { path: '/:pathMatch(.*)*', redirect: '/' },
  ],
})

// Installable guard (exported for tests). requiresAuth → ensure a session;
// requiresAdmin → also require role==='admin'.
export function installGuard(r: Router) {
  r.beforeEach(async (to) => {
    if (!to.meta.requiresAuth) return true
    const session = useSessionStore()
    await session.ensureLoaded()
    if (!session.me) return { path: '/login', query: { return_to: to.fullPath } }
    if (to.meta.requiresAdmin && !session.isAdmin) return { path: '/' }
    return true
  })
}

installGuard(router)
```

- [ ] **Step 4: Create `dashboard/src/pages/CenteredLayout.vue`** (the old `App.vue` chrome):

```vue
<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import LocaleSwitcher from '../components/LocaleSwitcher.vue'

const { t } = useI18n()
</script>

<template>
  <div class="min-h-screen flex flex-col items-center justify-center gap-6 p-4 bg-default">
    <header class="w-full max-w-md flex items-center justify-between">
      <span class="text-lg font-semibold">{{ t('app.name') }}</span>
      <LocaleSwitcher />
    </header>
    <main class="w-full max-w-md">
      <RouterView />
    </main>
  </div>
</template>
```

- [ ] **Step 5: Reduce `dashboard/src/App.vue`** to a bare shell (chrome now lives per-layout):

```vue
<script setup lang="ts">
</script>

<template>
  <UApp>
    <RouterView />
  </UApp>
</template>
```

- [ ] **Step 6: Run the guard test + full vitest + build**

Run: `cd dashboard && npx vitest run src/router.test.ts`
Expected: PASS (4 tests)

Run: `cd dashboard && npx vitest run`
Expected: PASS (all suites green)

Run: `cd dashboard && npm run build`
Expected: build completes, `pkg/webui/dist` regenerated (the vite config outputs to `pkg/webui/dist` — confirm the build prints that path; if it writes to `dashboard/dist`, the existing `vite.config.ts` `build.outDir` already targets `pkg/webui/dist` from the prior chunk).

- [ ] **Step 7: Commit (source + rebuilt dist together)**

```bash
git add dashboard/src/router.ts dashboard/src/router.test.ts dashboard/src/App.vue dashboard/src/pages/CenteredLayout.vue pkg/webui/dist
git commit -m "feat(webui): wire dashboard routes + auth guard; refactor chrome into layouts; rebuild dist"
```

---

### Task 12: `mise dev-server` task + smoke SPA-route assertions + final gate

**Goal:** A one-command dev runner and machine assertions that the new SPA routes serve the shell, then the full release gate.

**Files:**
- Modify: `mise.toml`
- Modify: `cmd/smoke/main.go`
- Possibly rebuild + commit: `pkg/webui/dist`

**Acceptance Criteria:**
- [ ] `mise dev-server` builds the SPA then runs the Go server (serving the embedded SPA at `http://localhost:8080`).
- [ ] `cmd/smoke` asserts that `/`, `/enroll/<token>`, `/admin/accounts`, `/sessions`, `/credentials` all return HTML containing `id="app"` (the SPA shell) and a 200.
- [ ] Full gate green: `go build/vet ./...`, `go test ./...`, `npm run build` + `vitest`, smoke `SMOKE_EXIT=0`.

**Verify:** `setsid bash /tmp/run_v06.sh` then poll `/tmp/v06.result` for `DONE` + `SMOKE_EXIT=0`.

**Steps:**

- [ ] **Step 1: Add the `dev-server` mise task.** Append to `mise.toml`:

```toml
[tasks.dev-server]
description = "Build the SPA into pkg/webui/dist, then run the Go server (embedded SPA at http://localhost:8080). Requires PROHIBITORUM_DATABASE_URL, PROHIBITORUM_DATA_ENCRYPTION_KEY_V1, PROHIBITORUM_PUBLIC_ORIGIN in the environment."
run = "cd dashboard && npm run build && cd .. && go run ./cmd/prohibitorum"
```

- [ ] **Step 2: Add the SPA-shell smoke assertions.** In `cmd/smoke/main.go`, locate a point after the bootstrap login is established (after `step("step 5/45 — GET /me")` succeeds, near line 116-166). Insert a new step block. Add `"io"` and `"net/http"` to the imports if not already present (check the import block first; `net/http` is already used via `httptest`, and `strings` is imported). Insert:

```go
	// --- SPA shell routes: the new dashboard paths must serve index.html
	// (id="app") via the NotFound fallback, not be shadowed by a backend route. ---
	step("step 5b/45 — SPA shell served for dashboard routes")
	for _, p := range []string{"/", "/sessions", "/credentials", "/admin/accounts", "/enroll/" + token} {
		resp, err := http.Get(*baseURL + p)
		if err != nil {
			log.Fatalf("SPA shell GET %s: %v", p, err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			log.Fatalf("SPA shell GET %s: status %d (want 200)", p, resp.StatusCode)
		}
		if !strings.Contains(string(body), `id="app"`) {
			log.Fatalf("SPA shell GET %s: body missing id=\"app\" (got %d bytes)", p, len(body))
		}
	}
	log.Printf("  /, /sessions, /credentials, /admin/accounts, /enroll/<token> all serve the SPA shell ✓")
```

> Note: `token` is in scope from step 1 (`token, err := mintEnrollmentToken(...)`). `*baseURL` is the flag value. Use a plain `http.Get` (unauthenticated) — the SPA fallback serves the shell regardless of auth; client-side guard enforcement is tested by vitest, not smoke. If `io` is not yet imported, add it; verify with `go build ./...`.

- [ ] **Step 3: Verify Go compiles + vets + tests**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: all exit 0. (Ignore any gopls `<new-diagnostics>` false positives on `//go:embed` / sqlc — trust the CLI exit codes.)

- [ ] **Step 4: Verify the frontend gate**

Run: `cd dashboard && npm run build && npx vitest run`
Expected: build succeeds; all vitest suites pass. If `pkg/webui/dist` changed since Task 11 (it shouldn't unless source changed), `git add pkg/webui/dist`.

- [ ] **Step 5: Run the full smoke** (the existing harness already mints a bootstrap token and exercises the enrollment + login surface; the new step 5b rides on it).

Run: `setsid bash /tmp/run_v06.sh`
Then poll: read `/tmp/v06.result` until it ends with `DONE`. Confirm it contains `SMOKE_EXIT=0` and the `SPA shell … ✓` line.
Expected: `SMOKE_EXIT=0`.

- [ ] **Step 6: Commit**

```bash
git add mise.toml cmd/smoke/main.go
# include pkg/webui/dist only if it changed this task
git commit -m "feat: mise dev-server task + smoke SPA-shell route assertions"
```

- [ ] **Step 7: Surface the manual-test script to the user** (the deliverable's acceptance — D9). Report these steps for the human to run in a browser:

```
1. Ensure dev env is exported (PROHIBITORUM_DATABASE_URL / DATA_ENCRYPTION_KEY_V1 / PUBLIC_ORIGIN=http://localhost:8080) against a fresh DB.
2. `mise dev-server`  (builds SPA + runs server at http://localhost:8080)
3. In another shell: `go run ./cmd/prohibitorum enroll-admin`  → copy the printed http://localhost:8080/enroll/<token> URL.
4. Open the URL → enter username + display name → "Register passkey" → passkey ceremony → auto-login lands on `/` (Profile).
5. Sidebar shows the Admin group (bootstrap account is admin). Visit Sessions, Credentials, Admin → Accounts, Admin → Invitations; exercise revoke / rename / disable / reissue / create-invitation.
6. Click Logout (header) → lands on the logout page → go to `/login`.
7. Visit `/` again → guard bounces to `/login?return_to=%2F` → sign in with the passkey → back to `/`.
```

---

## Self-review (completed by plan author)

- **Spec coverage:** D1 (sidebar) → Task 10; D2 (routes & meta) → Task 11; D3 (guard) → Task 11; D4 (store) → Task 1; D5 (enroll) → Task 9 *(corrected: bootstrap/invite need identity inputs)*; D6 (passkeyRegister) → Task 2; D7 sections → Tasks 4–8; D8 (mutations UX, i18n in both locales) → folded into each view task; D9 (run/manual test) → Task 12; D10 (embedded dist) → Task 11 + 12.
- **Spec corrections folded in:** huma list endpoints return top-level arrays; `UpdateAccount` is PUT (added `api.put`); enrollment begin requires username/displayName for bootstrap/invite; complete returns `{session,...}`; `App.vue` chrome refactor required.
- **Type consistency:** `ensureLoaded`/`isAdmin`/`clear` defined in Task 1 and used identically in Tasks 9–11; `passkeyRegister(token, fields)` + `EnrollFields` defined in Task 2 and consumed in Task 9; `CopyableUrl` defined in Task 3 and used in Tasks 7–8; destructive actions use inline two-step confirm buttons (no separate `ConfirmButton` component — YAGNI, and `data-test` lands on real `<button>`s for testability). `installGuard` defined and exported in Task 11, tested there.
- **No placeholders:** every code step contains complete, copy-paste-ready content.
