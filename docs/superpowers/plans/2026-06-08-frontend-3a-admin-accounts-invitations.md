# Frontend Spec 3a — Admin shell + Accounts + Invitations Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers-extended-cc:subagent-driven-development (recommended) or superpowers-extended-cc:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the admin section of the rebuilt dashboard — an `isAdmin`-gated sidebar group, `requiresAdmin` routes, and the Accounts (list + detail) and Invitations pages — wired to the existing backend Admin Management API.

**Architecture:** Vue 3 `<script setup>` views on the established 2a/2b/2c patterns (`useApi`, `withSudo`, `errors.<code>` i18n, `Card`/`Alert`/`StatusBadge`/`ConfirmDialog`/`CodeField`, `data-test`). Views are built first and unit-tested standalone (mocked `api` + `vue-router`); routes/sidebar are wired last so no lazy-import points at a missing file mid-plan. A shadcn-vue `Table` primitive and a `lib/time.ts` helper are vendored/added in the first task.

**Tech Stack:** Vue 3, Vue Router, vue-i18n, Vitest + @vue/test-utils, Tailwind v4 + shadcn-vue, Pinia.

**Spec:** `docs/superpowers/specs/2026-06-08-frontend-3a-admin-accounts-invitations-design.md`

**Conventions that bite (from prior slices):**
- Run frontend tooling from `dashboard/` with `mise exec -- npm …`. cwd can reset to repo root between tool calls — `cd` explicitly.
- The binary embeds the **committed** `pkg/webui/dist`; Vite chunk hashes are non-deterministic. For source-only commits, after any verify build run `git checkout -- pkg/webui/dist && git clean -fq pkg/webui/dist`. Rebuild + commit dist **once** at the done-gate (Task 6).
- **`en.ts` Edit hazard:** the Edit tool has corrupted en.ts straight-quote delimiters into curly U+2018 before. After ANY en.ts edit, run `grep -nP "\x{2018}" dashboard/src/locales/en.ts` (must be empty) and `grep -nP ":\s*\x{2019}" dashboard/src/locales/en.ts` (must be empty). Keep in-text apostrophes curly (U+2019).
- Tests do NOT mock `withSudo` for happy paths — they let `api` resolve so `withSudo` passes straight through. The admin credential force-revoke is the only sudo-gated op; its test still just lets the post resolve.
- `ConfirmDialog` confirm button is a `variant="destructive"` `Button` showing `confirmLabel`, teleported to `document.body`. Tests click it via a `document.body` destructive-button query (see the helper in Task 3's test).

---

### Task 1: Vendor `Table` primitive + `lib/time.ts` + admin i18n

**Goal:** Add the shared building blocks every later task needs: a token-styled `Table`, past/absolute time helpers, and the full `admin.*` i18n namespace + admin error codes.

**Files:**
- Create: `dashboard/src/components/ui/table/{Table,TableHeader,TableBody,TableRow,TableHead,TableCell}.vue` + `dashboard/src/components/ui/table/index.ts`
- Create: `dashboard/src/lib/time.ts`, `dashboard/src/lib/time.test.ts`
- Modify: `dashboard/src/locales/en.ts`

**Acceptance Criteria:**
- [ ] `@/components/ui/table` exports Table/TableHeader/TableBody/TableRow/TableHead/TableCell.
- [ ] `relativeTime(iso, now?)` → "just now"/"Nm ago"/"Nh ago"/"Nd ago"/"—"; `formatDateTime(iso)` → locale string or "—"; both handle null/invalid.
- [ ] `admin.*` namespace + the seven admin `errors.*` codes + `errors.forbidden` present, apostrophes curly.

**Verify:** `cd dashboard && mise exec -- npm run test -- time` → pass; `mise exec -- npm run build` → clean.

**Steps:**

- [ ] **Step 1: Vendor the Table primitive.** Create the six files (token-styled — uses the project's `text-muted`/`bg-sunken` tokens, not shadcn defaults):

`dashboard/src/components/ui/table/Table.vue`:
```vue
<script setup lang="ts">
import type { HTMLAttributes } from "vue"
import { cn } from "@/lib/utils"
const props = defineProps<{ class?: HTMLAttributes["class"] }>()
</script>
<template>
  <div data-slot="table-container" class="relative w-full overflow-x-auto">
    <table data-slot="table" :class="cn('w-full caption-bottom text-sm', props.class)"><slot /></table>
  </div>
</template>
```
`dashboard/src/components/ui/table/TableHeader.vue`:
```vue
<script setup lang="ts">
import type { HTMLAttributes } from "vue"
import { cn } from "@/lib/utils"
const props = defineProps<{ class?: HTMLAttributes["class"] }>()
</script>
<template>
  <thead data-slot="table-header" :class="cn('[&_tr]:border-b', props.class)"><slot /></thead>
</template>
```
`dashboard/src/components/ui/table/TableBody.vue`:
```vue
<script setup lang="ts">
import type { HTMLAttributes } from "vue"
import { cn } from "@/lib/utils"
const props = defineProps<{ class?: HTMLAttributes["class"] }>()
</script>
<template>
  <tbody data-slot="table-body" :class="cn('[&_tr:last-child]:border-0', props.class)"><slot /></tbody>
</template>
```
`dashboard/src/components/ui/table/TableRow.vue`:
```vue
<script setup lang="ts">
import type { HTMLAttributes } from "vue"
import { cn } from "@/lib/utils"
const props = defineProps<{ class?: HTMLAttributes["class"] }>()
</script>
<template>
  <tr data-slot="table-row" :class="cn('hover:bg-sunken/60 border-b transition-colors', props.class)"><slot /></tr>
</template>
```
`dashboard/src/components/ui/table/TableHead.vue`:
```vue
<script setup lang="ts">
import type { HTMLAttributes } from "vue"
import { cn } from "@/lib/utils"
const props = defineProps<{ class?: HTMLAttributes["class"] }>()
</script>
<template>
  <th data-slot="table-head" :class="cn('text-muted h-10 px-2 text-left align-middle font-medium whitespace-nowrap', props.class)"><slot /></th>
</template>
```
`dashboard/src/components/ui/table/TableCell.vue`:
```vue
<script setup lang="ts">
import type { HTMLAttributes } from "vue"
import { cn } from "@/lib/utils"
const props = defineProps<{ class?: HTMLAttributes["class"] }>()
</script>
<template>
  <td data-slot="table-cell" :class="cn('p-2 align-middle', props.class)"><slot /></td>
</template>
```
`dashboard/src/components/ui/table/index.ts`:
```ts
export { default as Table } from "./Table.vue"
export { default as TableBody } from "./TableBody.vue"
export { default as TableCell } from "./TableCell.vue"
export { default as TableHead } from "./TableHead.vue"
export { default as TableHeader } from "./TableHeader.vue"
export { default as TableRow } from "./TableRow.vue"
```

- [ ] **Step 2: Write the failing time-helper test.** Create `dashboard/src/lib/time.test.ts`:
```ts
import { describe, it, expect } from 'vitest'
import { relativeTime, formatDateTime } from './time'

const NOW = Date.parse('2026-06-08T12:00:00Z')

describe('relativeTime', () => {
  it('returns — for null/invalid', () => {
    expect(relativeTime(null, NOW)).toBe('—')
    expect(relativeTime('nonsense', NOW)).toBe('—')
  })
  it('formats recent past', () => {
    expect(relativeTime('2026-06-08T11:59:30Z', NOW)).toBe('just now')
    expect(relativeTime('2026-06-08T11:30:00Z', NOW)).toBe('30m ago')
    expect(relativeTime('2026-06-08T09:00:00Z', NOW)).toBe('3h ago')
    expect(relativeTime('2026-06-05T12:00:00Z', NOW)).toBe('3d ago')
  })
  it('clamps future timestamps to just now', () => {
    expect(relativeTime('2026-06-09T12:00:00Z', NOW)).toBe('just now')
  })
})
describe('formatDateTime', () => {
  it('returns — for null/invalid', () => {
    expect(formatDateTime(null)).toBe('—')
    expect(formatDateTime('nonsense')).toBe('—')
  })
  it('returns a locale string for a valid date', () => {
    expect(formatDateTime('2026-06-08T12:00:00Z')).toContain('2026')
  })
})
```
Run `cd dashboard && mise exec -- npm run test -- time` → FAIL (cannot resolve `./time`).

- [ ] **Step 3: Implement `lib/time.ts`.** Create `dashboard/src/lib/time.ts`:
```ts
/**
 * Time formatting helpers for admin lists.
 * - relativeTime: compact past-relative ("3h ago"); future clamps to "just now".
 *   English-literal (English-first; i18n of relative units deferred with zh).
 * - formatDateTime: absolute locale string — use for FUTURE times (expiries),
 *   where relative reads wrong.
 */
export function relativeTime(iso: string | null | undefined, now: number = Date.now()): string {
  if (!iso) return '—'
  const t = Date.parse(iso)
  if (Number.isNaN(t)) return '—'
  const s = Math.floor(Math.max(0, now - t) / 1000)
  if (s < 60) return 'just now'
  const m = Math.floor(s / 60)
  if (m < 60) return `${m}m ago`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h}h ago`
  const d = Math.floor(h / 24)
  if (d < 30) return `${d}d ago`
  const mo = Math.floor(d / 30)
  if (mo < 12) return `${mo}mo ago`
  return `${Math.floor(d / 365)}y ago`
}

export function formatDateTime(iso: string | null | undefined): string {
  if (!iso) return '—'
  const t = Date.parse(iso)
  if (Number.isNaN(t)) return '—'
  return new Date(t).toLocaleString()
}
```
Run the test again → PASS.

- [ ] **Step 4: Add the admin i18n namespace + error codes.** In `dashboard/src/locales/en.ts`, add an `admin:` block (after the `pair:` block) — keep apostrophes CURLY (U+2019):
```ts
  admin: {
    nav: { title: 'Admin', accounts: 'Accounts', invitations: 'Invitations' },
    accounts: {
      title: 'Accounts',
      invite: 'Invite',
      colUser: 'User', colRole: 'Role', colState: 'State', colLastSeen: 'Last seen',
      active: 'Active', disabled: 'Disabled',
      empty: 'No accounts yet.',
    },
    account: {
      back: 'Back to accounts',
      notFound: 'That account no longer exists.',
      identityTitle: 'Identity & role',
      username: 'Username', displayName: 'Display name', role: 'Role',
      roleAdmin: 'Admin', roleUser: 'User',
      disabledLabel: 'Account disabled',
      attributes: 'Attributes',
      save: 'Save changes', saved: 'Saved.',
      passkeysTitle: 'Passkeys',
      passkeysEmpty: 'This account has no passkeys.',
      created: 'Added', lastUsed: 'last used',
      forceRevoke: 'Force-revoke',
      forceRevokeConfirmTitle: 'Force-revoke this passkey?',
      forceRevokeConfirmBody: 'This account will no longer be able to sign in with this passkey.',
      sessionsTitle: 'Sessions',
      revokeAllSessions: 'Revoke all sessions',
      revokeAllConfirmTitle: 'Revoke all sessions?',
      revokeAllConfirmBody: 'This signs the account out of every device immediately.',
      sessionsRevoked: 'Revoked {count} session(s).',
      resetTitle: 'Reset access',
      resetHelp: 'Issue a fresh enrollment link. The account re-enrolls its passkeys; existing credentials keep working until then.',
      reissue: 'Reissue enrollment link',
      reissueExpires: 'Link expires {when}.',
      dangerTitle: 'Danger zone',
      deleteHelp: 'Permanently delete this account and all of its credentials, sessions, and connected identities.',
      delete: 'Delete account',
      deleteConfirmTitle: 'Delete this account?',
      deleteConfirmBody: 'This permanently removes the account and everything attached to it. This cannot be undone.',
    },
    invitations: {
      title: 'Invitations',
      create: 'Create invitation',
      role: 'Role', roleAdmin: 'Admin', roleUser: 'User',
      colRole: 'Role', colCreated: 'Created', colExpires: 'Expires', colLink: 'Enrollment link',
      empty: 'No outstanding invitations.',
      created: 'Invitation created — share the link below.',
      revoke: 'Revoke',
      revokeConfirmTitle: 'Revoke this invitation?',
      revokeConfirmBody: 'The enrollment link will stop working immediately.',
    },
  },
```
In the `errors:` object add:
```ts
    last_admin: 'You can’t remove the last administrator.',
    admin_cannot_be_disabled: 'An administrator can’t be disabled — change the role to user first.',
    cannot_delete_self: 'You can’t delete your own account.',
    invalid_role: 'Choose a valid role.',
    username_immutable: 'Usernames can’t be changed.',
    account_not_found: 'That account no longer exists.',
    invitation_not_found: 'That invitation no longer exists.',
    forbidden: 'You don’t have permission to view that.',
```

- [ ] **Step 5: Verify en.ts integrity + build.**
```bash
cd dashboard
grep -nP "\x{2018}" src/locales/en.ts && echo "BAD: U+2018 present" || echo "ok: no U+2018"
grep -nP ":\s*\x{2019}" src/locales/en.ts && echo "BAD: curly delimiter" || echo "ok: no curly delimiter"
mise exec -- npm run test -- time
mise exec -- npm run build
```
Expected: greps print "ok", time tests pass, build clean. Then discard dist:
```bash
cd /home/tundra/projects/tundra/prohibitorum && git checkout -- pkg/webui/dist 2>/dev/null; git clean -fq pkg/webui/dist 2>/dev/null
```

- [ ] **Step 6: Commit.**
```bash
cd /home/tundra/projects/tundra/prohibitorum
git add dashboard/src/components/ui/table dashboard/src/lib/time.ts dashboard/src/lib/time.test.ts dashboard/src/locales/en.ts
git commit -m "feat(web): admin foundation — Table primitive, time helpers, admin i18n

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: AdminAccountsView (`/admin/accounts`)

**Goal:** A Table of all accounts; row click navigates to the detail page; an Invite button routes to invitations.

**Files:**
- Create: `dashboard/src/pages/admin/AdminAccountsView.vue`, `dashboard/src/pages/admin/AdminAccountsView.test.ts`

**Acceptance Criteria:**
- [ ] `GET /accounts` → table rows (displayName + @username, role badge, state badge, relative last-seen).
- [ ] Row click → `router.push('/admin/accounts/{id}')`; Invite → `router.push('/admin/invitations')`.
- [ ] Empty + error states.

**Verify:** `cd dashboard && mise exec -- npm run test -- AdminAccountsView` → pass.

**Steps:**

- [ ] **Step 1: Write the failing test.** Create `dashboard/src/pages/admin/AdminAccountsView.test.ts`:
```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'
const get = vi.mocked(api.get)
const { push } = vi.hoisted(() => ({ push: vi.fn() }))
vi.mock('vue-router', () => ({ useRouter: () => ({ push }) }))
import AdminAccountsView from './AdminAccountsView.vue'

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
const mountView = () => mount(AdminAccountsView, { global: { plugins: [i18n()] } })
const ACCOUNTS = [
  { id: 1, username: 'alice', displayName: 'Alice Smith', role: 'admin', disabled: false, lastSignInAt: '2026-06-08T00:00:00Z' },
  { id: 2, username: 'bob', displayName: 'Bob Lee', role: 'user', disabled: true },
]
beforeEach(() => { get.mockReset(); push.mockReset() })

describe('AdminAccountsView', () => {
  it('lists accounts with role and state', async () => {
    get.mockResolvedValue(ACCOUNTS)
    const w = mountView(); await flushPromises()
    expect(get).toHaveBeenCalledWith('/api/prohibitorum/accounts')
    expect(w.text()).toContain('Alice Smith')
    expect(w.text()).toContain('@bob')
    expect(w.text()).toContain(en.admin.accounts.disabled)
  })
  it('row click navigates to the detail page', async () => {
    get.mockResolvedValue(ACCOUNTS)
    const w = mountView(); await flushPromises()
    await w.find('[data-test="account-row-2"]').trigger('click')
    expect(push).toHaveBeenCalledWith('/admin/accounts/2')
  })
  it('invite navigates to invitations', async () => {
    get.mockResolvedValue([])
    const w = mountView(); await flushPromises()
    await w.find('[data-test="invite"]').trigger('click')
    expect(push).toHaveBeenCalledWith('/admin/invitations')
  })
  it('shows empty state', async () => {
    get.mockResolvedValue([])
    const w = mountView(); await flushPromises()
    expect(w.text()).toContain(en.admin.accounts.empty)
  })
  it('surfaces error', async () => {
    get.mockRejectedValue({ code: 'server_error', message: 'boom' })
    const w = mountView(); await flushPromises()
    expect(w.text()).toContain(en.errors.server_error)
  })
})
```
Run `cd dashboard && mise exec -- npm run test -- AdminAccountsView` → FAIL (cannot resolve the view).

- [ ] **Step 2: Implement the view.** Create `dashboard/src/pages/admin/AdminAccountsView.vue`:
```vue
<script setup lang="ts">
/** AdminAccountsView (/admin/accounts) — table of accounts; row → detail. */
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { relativeTime } from '@/lib/time'
import { Button } from '@/components/ui/button'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/table'
import StatusBadge from '@/components/custom/StatusBadge.vue'

interface Account {
  id: number; username: string; displayName: string; role: string
  disabled: boolean; lastSignInAt?: string
}
const { t, te } = useI18n()
const router = useRouter()
const { busy, error, run } = useApi()
const rows = ref<Account[]>([])
const errorText = computed(() => {
  const e = error.value
  if (!e) return ''
  const key = `errors.${e.code}`
  return te(key) ? t(key) : e.message || t('common.error')
})
async function load(): Promise<void> {
  const res = await run(() => api.get<Account[]>('/api/prohibitorum/accounts'))
  if (res) rows.value = res
}
onMounted(load)
</script>
<template>
  <div class="flex max-w-4xl flex-col gap-6">
    <div class="flex items-center justify-between gap-4">
      <h1 class="text-2xl font-semibold tracking-tight text-ink">{{ t('admin.accounts.title') }}</h1>
      <Button type="button" data-test="invite" @click="router.push('/admin/invitations')">{{ t('admin.accounts.invite') }}</Button>
    </div>
    <Alert v-if="errorText" variant="destructive" role="alert" aria-live="polite"><AlertDescription>{{ errorText }}</AlertDescription></Alert>
    <Table v-if="rows.length">
      <TableHeader>
        <TableRow>
          <TableHead>{{ t('admin.accounts.colUser') }}</TableHead>
          <TableHead>{{ t('admin.accounts.colRole') }}</TableHead>
          <TableHead>{{ t('admin.accounts.colState') }}</TableHead>
          <TableHead>{{ t('admin.accounts.colLastSeen') }}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <TableRow v-for="a in rows" :key="a.id" class="cursor-pointer" :data-test="`account-row-${a.id}`" @click="router.push(`/admin/accounts/${a.id}`)">
          <TableCell>
            <div class="flex min-w-0 flex-col">
              <span class="truncate font-medium text-ink">{{ a.displayName }}</span>
              <span class="truncate text-muted">@{{ a.username }}</span>
            </div>
          </TableCell>
          <TableCell><StatusBadge :variant="a.role === 'admin' ? 'caution' : 'neutral'">{{ a.role === 'admin' ? t('admin.account.roleAdmin') : t('admin.account.roleUser') }}</StatusBadge></TableCell>
          <TableCell><StatusBadge :variant="a.disabled ? 'danger' : 'success'">{{ a.disabled ? t('admin.accounts.disabled') : t('admin.accounts.active') }}</StatusBadge></TableCell>
          <TableCell class="text-muted">{{ relativeTime(a.lastSignInAt) }}</TableCell>
        </TableRow>
      </TableBody>
    </Table>
    <p v-else-if="!busy && !errorText" class="text-sm text-muted">{{ t('admin.accounts.empty') }}</p>
  </div>
</template>
```
Run the test → PASS (5 cases).

- [ ] **Step 3: Build + discard dist + commit.**
```bash
cd dashboard && mise exec -- npm run build && cd /home/tundra/projects/tundra/prohibitorum && git checkout -- pkg/webui/dist 2>/dev/null; git clean -fq pkg/webui/dist 2>/dev/null
git add dashboard/src/pages/admin/AdminAccountsView.vue dashboard/src/pages/admin/AdminAccountsView.test.ts
git commit -m "feat(web): AdminAccountsView (/admin/accounts) — accounts table

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: AdminAccountDetailView (`/admin/accounts/:id`)

**Goal:** Per-account admin page: edit identity/role/disabled (PUT, round-tripping attributes), force-revoke passkeys (sudo), revoke all sessions, reissue enrollment, delete.

**Files:**
- Create: `dashboard/src/pages/admin/AdminAccountDetailView.vue`, `dashboard/src/pages/admin/AdminAccountDetailView.test.ts`

**Acceptance Criteria:**
- [ ] Loads `GET /accounts/{id}` + `GET /accounts/{id}/credentials`; `account_not_found` → not-found state.
- [ ] Save → `PUT /accounts/{id}` with the account's **existing attributes round-tripped** (asserted in test); surfaces `last_admin`/`admin_cannot_be_disabled`.
- [ ] Force-revoke credential → ConfirmDialog + `POST /accounts/credentials/delete {accountId, credentialId}` → refresh.
- [ ] Revoke all sessions → `POST /accounts/revoke-sessions {id}` → shows count.
- [ ] Reissue → `POST /accounts/reissue-enrollment {id}` → shows URL in CodeField.
- [ ] Delete → ConfirmDialog + `POST /accounts/delete {id}` → `router.push('/admin/accounts')`.

**Verify:** `cd dashboard && mise exec -- npm run test -- AdminAccountDetailView` → pass.

**Steps:**

- [ ] **Step 1: Write the failing test.** Create `dashboard/src/pages/admin/AdminAccountDetailView.test.ts`:
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
vi.mock('vue-router', () => ({ useRouter: () => ({ push }), useRoute: () => ({ params: { id: '7' } }) }))
import AdminAccountDetailView from './AdminAccountDetailView.vue'

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
const mountView = () => mount(AdminAccountDetailView, {
  global: { plugins: [i18n()], stubs: { RouterLink: { props: ['to'], template: '<a :href="to"><slot/></a>' } } },
  attachTo: document.body,
})
const ACCOUNT = {
  id: 7, username: 'carol', displayName: 'Carol Ng', role: 'user',
  attributes: { team: 'security' }, disabled: false,
  createdAt: '2026-01-01T00:00:00Z', updatedAt: '2026-02-01T00:00:00Z', lastSignInAt: '2026-06-01T00:00:00Z',
}
const CREDS = [
  { id: 11, credentialIdSuffix: 'ab12', nickname: 'Laptop', transports: ['internal'], backupState: true, attestationType: 'none', createdAt: '2026-01-02T00:00:00Z', lastUsedAt: '2026-06-01T00:00:00Z' },
]
// GET router: /accounts/7 → account; /accounts/7/credentials → creds
function mockGets(account = ACCOUNT, creds = CREDS) {
  get.mockImplementation(async (p: string) => p.endsWith('/credentials') ? creds : account)
}
// ConfirmDialog confirm = destructive button (teleported to body) with the given label.
function clickConfirm(label: string) {
  const btns = Array.from(document.body.querySelectorAll('button'))
    .filter((b) => b.getAttribute('data-variant') === 'destructive' && b.textContent?.includes(label))
  btns[btns.length - 1]!.click()
}
beforeEach(() => { get.mockReset(); post.mockReset(); put.mockReset(); push.mockReset() })

describe('AdminAccountDetailView', () => {
  it('loads the account and its credentials', async () => {
    mockGets()
    const w = mountView(); await flushPromises()
    expect(get).toHaveBeenCalledWith('/api/prohibitorum/accounts/7')
    expect(get).toHaveBeenCalledWith('/api/prohibitorum/accounts/7/credentials')
    expect(w.text()).toContain('Carol Ng')
    expect(w.text()).toContain('Laptop')
    expect(w.text()).toContain('team')
  })
  it('shows not-found when the account is missing', async () => {
    get.mockRejectedValue({ code: 'account_not_found', message: 'zh' })
    const w = mountView(); await flushPromises()
    expect(w.text()).toContain(en.admin.account.notFound)
  })
  it('saves identity, round-tripping existing attributes', async () => {
    mockGets()
    put.mockResolvedValue({ ...ACCOUNT, role: 'admin' })
    const w = mountView(); await flushPromises()
    await w.find<HTMLSelectElement>('select[name="role"]').setValue('admin')
    await w.find('[data-test="save"]').trigger('click'); await flushPromises()
    expect(put).toHaveBeenCalledWith('/api/prohibitorum/accounts/7', {
      username: '', displayName: 'Carol Ng', role: 'admin', disabled: false, attributes: { team: 'security' },
    })
    expect(w.text()).toContain(en.admin.account.saved)
  })
  it('surfaces last_admin on save failure', async () => {
    mockGets()
    put.mockRejectedValue({ code: 'last_admin', message: 'zh' })
    const w = mountView(); await flushPromises()
    await w.find('[data-test="save"]').trigger('click'); await flushPromises()
    expect(w.text()).toContain(en.errors.last_admin)
  })
  it('force-revokes a passkey (confirm → post → refresh)', async () => {
    mockGets(); post.mockResolvedValue(undefined)
    const w = mountView(); await flushPromises()
    await w.find('[data-test="revoke-cred-11"]').trigger('click'); await flushPromises()
    clickConfirm(en.admin.account.forceRevoke); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/accounts/credentials/delete', { accountId: 7, credentialId: 11 })
    expect(get.mock.calls.filter((c) => String(c[0]).endsWith('/credentials')).length).toBe(2)
  })
  it('revokes all sessions and shows the count', async () => {
    mockGets(); post.mockResolvedValue({ revoked: 3 })
    const w = mountView(); await flushPromises()
    await w.find('[data-test="revoke-all"]').trigger('click'); await flushPromises()
    clickConfirm(en.admin.account.revokeAllSessions); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/accounts/revoke-sessions', { id: 7 })
    expect(w.text()).toContain('Revoked 3')
  })
  it('reissues an enrollment link and reveals the URL', async () => {
    mockGets(); post.mockResolvedValue({ url: 'https://x/enroll/tok', expiresAt: '2026-06-09T00:00:00Z' })
    const w = mountView(); await flushPromises()
    await w.find('[data-test="reissue"]').trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/accounts/reissue-enrollment', { id: 7 })
    expect(w.text()).toContain('https://x/enroll/tok')
  })
  it('deletes the account and navigates to the list', async () => {
    mockGets(); post.mockResolvedValue(undefined)
    const w = mountView(); await flushPromises()
    await w.find('[data-test="delete"]').trigger('click'); await flushPromises()
    clickConfirm(en.admin.account.delete); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/accounts/delete', { id: 7 })
    expect(push).toHaveBeenCalledWith('/admin/accounts')
  })
})
```
Run `cd dashboard && mise exec -- npm run test -- AdminAccountDetailView` → FAIL (cannot resolve the view).

- [ ] **Step 2: Implement the view.** Create `dashboard/src/pages/admin/AdminAccountDetailView.vue`:
```vue
<script setup lang="ts">
/**
 * AdminAccountDetailView (/admin/accounts/:id) — per-account admin actions.
 * Edit identity/role/disabled (PUT round-trips attributes — the backend REPLACES
 * them, so omitting would clear them); force-revoke passkeys (sudo); revoke all
 * sessions; reissue an enrollment link; delete. Attribute EDITING is out of scope
 * (shown read-only). All mutations go through withSudo (no-op unless the server
 * demands sudo — only credential force-revoke does today).
 */
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { withSudo } from '@/lib/sudo'
import { relativeTime, formatDateTime } from '@/lib/time'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Alert, AlertDescription } from '@/components/ui/alert'
import ConfirmDialog from '@/components/custom/ConfirmDialog.vue'
import CodeField from '@/components/custom/CodeField.vue'

interface Account {
  id: number; username: string; displayName: string; role: string
  attributes?: Record<string, unknown>; disabled: boolean
  createdAt: string; updatedAt: string; lastSignInAt?: string
}
interface Credential {
  id: number; credentialIdSuffix: string; nickname?: string; transports: string[]
  backupState: boolean; attestationType: string; createdAt: string; lastUsedAt?: string
}

const { t, te } = useI18n()
const route = useRoute()
const router = useRouter()
const { busy, error, run } = useApi()

const id = Number(route.params.id)
const account = ref<Account | null>(null)
const credentials = ref<Credential[]>([])
const notFound = ref(false)

const displayName = ref('')
const role = ref<'admin' | 'user'>('user')
const disabled = ref(false)
const saved = ref(false)

const revokeCredId = ref<number | null>(null)
const confirmRevokeAll = ref(false)
const confirmDelete = ref(false)
const revokedCount = ref<number | null>(null)
const reissueUrl = ref('')
const reissueExpires = ref('')

const errorText = computed(() => {
  const e = error.value
  if (!e) return ''
  const key = `errors.${e.code}`
  return te(key) ? t(key) : e.message || t('common.error')
})
const attributeEntries = computed(() =>
  Object.entries(account.value?.attributes ?? {}).map(([k, v]) => [k, String(v)] as [string, string]))

async function loadCredentials(): Promise<void> {
  const creds = await run(() => api.get<Credential[]>(`/api/prohibitorum/accounts/${id}/credentials`))
  if (creds) credentials.value = creds
}
async function load(): Promise<void> {
  const acc = await run(() => api.get<Account>(`/api/prohibitorum/accounts/${id}`))
  if (!acc) { if (error.value?.code === 'account_not_found') notFound.value = true; return }
  account.value = acc
  displayName.value = acc.displayName
  role.value = acc.role === 'admin' ? 'admin' : 'user'
  disabled.value = acc.disabled
  await loadCredentials()
}
async function save(): Promise<void> {
  saved.value = false
  const updated = await run(() => withSudo(() => api.put<Account>(`/api/prohibitorum/accounts/${id}`, {
    username: '',
    displayName: displayName.value,
    role: role.value,
    disabled: disabled.value,
    attributes: account.value?.attributes ?? {},
  })))
  if (updated) { account.value = updated; saved.value = true }
}
async function forceRevoke(): Promise<void> {
  const credentialId = revokeCredId.value
  if (credentialId == null) return
  const ok = await run(() => withSudo(async () => {
    await api.post('/api/prohibitorum/accounts/credentials/delete', { accountId: id, credentialId })
    return true as const
  }))
  revokeCredId.value = null
  if (ok) await loadCredentials()
}
async function revokeAllSessions(): Promise<void> {
  const res = await run(() => withSudo(() =>
    api.post<{ revoked: number }>('/api/prohibitorum/accounts/revoke-sessions', { id })))
  confirmRevokeAll.value = false
  if (res) revokedCount.value = res.revoked
}
async function reissue(): Promise<void> {
  const res = await run(() => withSudo(() =>
    api.post<{ url: string; expiresAt: string }>('/api/prohibitorum/accounts/reissue-enrollment', { id })))
  if (res) { reissueUrl.value = res.url; reissueExpires.value = res.expiresAt }
}
async function destroy(): Promise<void> {
  const ok = await run(() => withSudo(async () => {
    await api.post('/api/prohibitorum/accounts/delete', { id })
    return true as const
  }))
  confirmDelete.value = false
  if (ok) router.push('/admin/accounts')
}
onMounted(load)
</script>
<template>
  <div class="flex max-w-2xl flex-col gap-6">
    <RouterLink to="/admin/accounts" class="text-sm text-muted underline-offset-4 hover:underline">{{ t('admin.account.back') }}</RouterLink>
    <p v-if="notFound" class="text-sm text-muted" role="status">{{ t('admin.account.notFound') }}</p>

    <template v-else-if="account">
      <h1 class="text-2xl font-semibold tracking-tight text-ink">{{ account.displayName }}</h1>
      <Alert v-if="errorText" variant="destructive" role="alert" aria-live="polite"><AlertDescription>{{ errorText }}</AlertDescription></Alert>

      <Card>
        <CardHeader><CardTitle>{{ t('admin.account.identityTitle') }}</CardTitle></CardHeader>
        <CardContent class="flex flex-col gap-4">
          <div class="flex flex-col gap-1.5">
            <Label>{{ t('admin.account.username') }}</Label>
            <p class="text-sm text-muted">@{{ account.username }}</p>
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="displayName">{{ t('admin.account.displayName') }}</Label>
            <Input id="displayName" name="displayName" v-model="displayName" />
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="role">{{ t('admin.account.role') }}</Label>
            <select id="role" name="role" v-model="role" class="bg-sunken border-input h-9 w-fit rounded-md border px-3 text-sm text-ink">
              <option value="user">{{ t('admin.account.roleUser') }}</option>
              <option value="admin">{{ t('admin.account.roleAdmin') }}</option>
            </select>
          </div>
          <label class="flex items-center gap-2 text-sm text-ink">
            <input type="checkbox" name="disabled" v-model="disabled" />
            {{ t('admin.account.disabledLabel') }}
          </label>
          <div v-if="attributeEntries.length" class="flex flex-col gap-1">
            <Label>{{ t('admin.account.attributes') }}</Label>
            <div v-for="[k, v] in attributeEntries" :key="k" class="flex min-w-0 gap-2 text-sm text-muted">
              <span class="font-mono">{{ k }}</span><span>=</span><span class="min-w-0 truncate">{{ v }}</span>
            </div>
          </div>
          <div class="flex items-center gap-3">
            <Button type="button" :disabled="busy" data-test="save" @click="save">{{ t('admin.account.save') }}</Button>
            <span v-if="saved" class="text-sm text-sage" role="status">{{ t('admin.account.saved') }}</span>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader><CardTitle>{{ t('admin.account.passkeysTitle') }}</CardTitle></CardHeader>
        <CardContent class="flex flex-col gap-3">
          <p v-if="!credentials.length" class="text-sm text-muted">{{ t('admin.account.passkeysEmpty') }}</p>
          <div v-for="c in credentials" :key="c.id" class="flex items-center justify-between gap-4">
            <div class="flex min-w-0 flex-col text-sm">
              <span class="truncate text-ink">{{ c.nickname || ('····' + c.credentialIdSuffix) }}</span>
              <span class="truncate text-muted">{{ t('admin.account.created') }} {{ relativeTime(c.createdAt) }} · {{ t('admin.account.lastUsed') }} {{ relativeTime(c.lastUsedAt) }}</span>
            </div>
            <Button type="button" variant="outline" size="sm" class="shrink-0" :disabled="busy" :data-test="`revoke-cred-${c.id}`" @click="revokeCredId = c.id">{{ t('admin.account.forceRevoke') }}</Button>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader><CardTitle>{{ t('admin.account.sessionsTitle') }}</CardTitle></CardHeader>
        <CardContent class="flex flex-col gap-3">
          <p v-if="revokedCount !== null" class="text-sm text-sage" role="status">{{ t('admin.account.sessionsRevoked', { count: revokedCount }) }}</p>
          <Button type="button" variant="outline" class="w-fit" :disabled="busy" data-test="revoke-all" @click="confirmRevokeAll = true">{{ t('admin.account.revokeAllSessions') }}</Button>
        </CardContent>
      </Card>

      <Card>
        <CardHeader><CardTitle>{{ t('admin.account.resetTitle') }}</CardTitle></CardHeader>
        <CardContent class="flex flex-col gap-3">
          <p class="text-sm text-muted">{{ t('admin.account.resetHelp') }}</p>
          <CodeField v-if="reissueUrl" :value="reissueUrl" />
          <p v-if="reissueUrl" class="text-xs text-muted">{{ t('admin.account.reissueExpires', { when: formatDateTime(reissueExpires) }) }}</p>
          <Button type="button" variant="outline" class="w-fit" :disabled="busy" data-test="reissue" @click="reissue">{{ t('admin.account.reissue') }}</Button>
        </CardContent>
      </Card>

      <Card class="border-destructive/30 bg-destructive/[0.02]">
        <CardHeader><CardTitle class="text-destructive">{{ t('admin.account.dangerTitle') }}</CardTitle></CardHeader>
        <CardContent class="flex flex-col gap-3">
          <p class="text-sm text-muted">{{ t('admin.account.deleteHelp') }}</p>
          <Button type="button" variant="destructive" class="w-fit" :disabled="busy" data-test="delete" @click="confirmDelete = true">{{ t('admin.account.delete') }}</Button>
        </CardContent>
      </Card>
    </template>

    <ConfirmDialog :open="revokeCredId !== null" :title="t('admin.account.forceRevokeConfirmTitle')" :confirm-label="t('admin.account.forceRevoke')" :busy="busy"
      @update:open="(v) => { if (!v) revokeCredId = null }" @cancel="revokeCredId = null" @confirm="forceRevoke">
      {{ t('admin.account.forceRevokeConfirmBody') }}
    </ConfirmDialog>
    <ConfirmDialog :open="confirmRevokeAll" :title="t('admin.account.revokeAllConfirmTitle')" :confirm-label="t('admin.account.revokeAllSessions')" :busy="busy"
      @update:open="(v) => { if (!v) confirmRevokeAll = false }" @cancel="confirmRevokeAll = false" @confirm="revokeAllSessions">
      {{ t('admin.account.revokeAllConfirmBody') }}
    </ConfirmDialog>
    <ConfirmDialog :open="confirmDelete" :title="t('admin.account.deleteConfirmTitle')" :confirm-label="t('admin.account.delete')" :busy="busy"
      @update:open="(v) => { if (!v) confirmDelete = false }" @cancel="confirmDelete = false" @confirm="destroy">
      {{ t('admin.account.deleteConfirmBody') }}
    </ConfirmDialog>
  </div>
</template>
```
Run the test → PASS (8 cases). Note: the three ConfirmDialogs are mutually exclusive (separate refs), so only one is open at a time and `clickConfirm(label)` finds the right destructive button by its label.

- [ ] **Step 3: Build + discard dist + commit.**
```bash
cd dashboard && mise exec -- npm run build && cd /home/tundra/projects/tundra/prohibitorum && git checkout -- pkg/webui/dist 2>/dev/null; git clean -fq pkg/webui/dist 2>/dev/null
git add dashboard/src/pages/admin/AdminAccountDetailView.vue dashboard/src/pages/admin/AdminAccountDetailView.test.ts
git commit -m "feat(web): AdminAccountDetailView (/admin/accounts/:id) — edit, force-revoke, sessions, reissue, delete

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: AdminInvitationsView (`/admin/invitations`)

**Goal:** List outstanding invitations with copyable enrollment URLs; create one (inline form); revoke one.

**Files:**
- Create: `dashboard/src/pages/admin/AdminInvitationsView.vue`, `dashboard/src/pages/admin/AdminInvitationsView.test.ts`

**Acceptance Criteria:**
- [ ] `GET /invitations` → table (role badge, created relative, expires absolute, URL in CodeField).
- [ ] Create (inline form, role select) → `POST /invitations {role}` → refresh + "created" notice.
- [ ] Revoke → ConfirmDialog + `POST /invitations/revoke {token}` → refresh.
- [ ] Empty + error states.

**Verify:** `cd dashboard && mise exec -- npm run test -- AdminInvitationsView` → pass.

**Steps:**

- [ ] **Step 1: Write the failing test.** Create `dashboard/src/pages/admin/AdminInvitationsView.test.ts`:
```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'
vi.mock('@/lib/sudo', () => ({ withSudo: (fn: () => Promise<unknown>) => fn() }))
const get = vi.mocked(api.get); const post = vi.mocked(api.post)
import AdminInvitationsView from './AdminInvitationsView.vue'

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
const mountView = () => mount(AdminInvitationsView, { global: { plugins: [i18n()] }, attachTo: document.body })
const INVITES = [
  { token: 'tok1', url: 'https://x/enroll/tok1', role: 'user', createdAt: '2026-06-01T00:00:00Z', expiresAt: '2026-06-09T00:00:00Z' },
]
function clickConfirm(label: string) {
  const btns = Array.from(document.body.querySelectorAll('button'))
    .filter((b) => b.getAttribute('data-variant') === 'destructive' && b.textContent?.includes(label))
  btns[btns.length - 1]!.click()
}
beforeEach(() => { get.mockReset(); post.mockReset() })

describe('AdminInvitationsView', () => {
  it('lists outstanding invitations with their URL', async () => {
    get.mockResolvedValue(INVITES)
    const w = mountView(); await flushPromises()
    expect(get).toHaveBeenCalledWith('/api/prohibitorum/invitations')
    expect(w.text()).toContain('https://x/enroll/tok1')
  })
  it('shows empty state', async () => {
    get.mockResolvedValue([])
    const w = mountView(); await flushPromises()
    expect(w.text()).toContain(en.admin.invitations.empty)
  })
  it('creates an invitation then refreshes', async () => {
    get.mockResolvedValue([]); post.mockResolvedValue({ url: 'https://x/enroll/new', expiresAt: '2026-06-10T00:00:00Z' })
    const w = mountView(); await flushPromises()
    await w.find('[data-test="create"]').trigger('click')         // open inline form
    await w.find<HTMLSelectElement>('select[name="newRole"]').setValue('admin')
    await w.find('[data-test="create-confirm"]').trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/invitations', { role: 'admin' })
    expect(w.text()).toContain(en.admin.invitations.created)
    expect(get).toHaveBeenCalledTimes(2)
  })
  it('revokes an invitation (confirm → post → refresh)', async () => {
    get.mockResolvedValue(INVITES); post.mockResolvedValue(undefined)
    const w = mountView(); await flushPromises()
    await w.find('[data-test="revoke-tok1"]').trigger('click'); await flushPromises()
    clickConfirm(en.admin.invitations.revoke); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/invitations/revoke', { token: 'tok1' })
    expect(get).toHaveBeenCalledTimes(2)
  })
})
```
Run `cd dashboard && mise exec -- npm run test -- AdminInvitationsView` → FAIL.

- [ ] **Step 2: Implement the view.** Create `dashboard/src/pages/admin/AdminInvitationsView.vue`:
```vue
<script setup lang="ts">
/**
 * AdminInvitationsView (/admin/invitations) — list/create/revoke enrollment
 * invitations. Create is an inline form (not a ConfirmDialog — creating isn't
 * destructive). The list returns the full URL, so it stays copyable per row.
 */
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { withSudo } from '@/lib/sudo'
import { relativeTime, formatDateTime } from '@/lib/time'
import { Card, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/table'
import StatusBadge from '@/components/custom/StatusBadge.vue'
import ConfirmDialog from '@/components/custom/ConfirmDialog.vue'
import CodeField from '@/components/custom/CodeField.vue'

interface Invitation { token: string; url: string; role: string; attributes?: Record<string, unknown>; createdAt: string; expiresAt: string }
const { t, te } = useI18n()
const { busy, error, run } = useApi()
const rows = ref<Invitation[]>([])
const createOpen = ref(false)
const newRole = ref<'admin' | 'user'>('user')
const created = ref(false)
const revokeToken = ref<string | null>(null)
const errorText = computed(() => {
  const e = error.value
  if (!e) return ''
  const key = `errors.${e.code}`
  return te(key) ? t(key) : e.message || t('common.error')
})
async function load(): Promise<void> {
  const res = await run(() => api.get<Invitation[]>('/api/prohibitorum/invitations'))
  if (res) rows.value = res
}
async function create(): Promise<void> {
  created.value = false
  const ok = await run(() => withSudo(async () => {
    await api.post('/api/prohibitorum/invitations', { role: newRole.value })
    return true as const
  }))
  createOpen.value = false
  if (ok) { created.value = true; await load() }
}
async function revoke(): Promise<void> {
  const token = revokeToken.value
  if (token == null) return
  const ok = await run(() => withSudo(async () => {
    await api.post('/api/prohibitorum/invitations/revoke', { token })
    return true as const
  }))
  revokeToken.value = null
  if (ok) await load()
}
onMounted(load)
</script>
<template>
  <div class="flex max-w-4xl flex-col gap-6">
    <div class="flex items-center justify-between gap-4">
      <h1 class="text-2xl font-semibold tracking-tight text-ink">{{ t('admin.invitations.title') }}</h1>
      <Button type="button" data-test="create" @click="createOpen = true">{{ t('admin.invitations.create') }}</Button>
    </div>
    <Alert v-if="errorText" variant="destructive" role="alert" aria-live="polite"><AlertDescription>{{ errorText }}</AlertDescription></Alert>
    <p v-if="created" class="text-sm text-sage" role="status">{{ t('admin.invitations.created') }}</p>

    <Card v-if="createOpen">
      <CardContent class="flex flex-col gap-3 py-4">
        <div class="flex flex-col gap-1.5">
          <label for="newRole" class="text-sm font-medium text-ink">{{ t('admin.invitations.role') }}</label>
          <select id="newRole" name="newRole" v-model="newRole" class="bg-sunken border-input h-9 w-fit rounded-md border px-3 text-sm text-ink">
            <option value="user">{{ t('admin.invitations.roleUser') }}</option>
            <option value="admin">{{ t('admin.invitations.roleAdmin') }}</option>
          </select>
        </div>
        <div class="flex gap-2">
          <Button type="button" :disabled="busy" data-test="create-confirm" @click="create">{{ t('admin.invitations.create') }}</Button>
          <Button type="button" variant="outline" :disabled="busy" data-test="create-cancel" @click="createOpen = false">{{ t('common.cancel') }}</Button>
        </div>
      </CardContent>
    </Card>

    <Table v-if="rows.length">
      <TableHeader>
        <TableRow>
          <TableHead>{{ t('admin.invitations.colRole') }}</TableHead>
          <TableHead>{{ t('admin.invitations.colCreated') }}</TableHead>
          <TableHead>{{ t('admin.invitations.colExpires') }}</TableHead>
          <TableHead>{{ t('admin.invitations.colLink') }}</TableHead>
          <TableHead></TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <TableRow v-for="inv in rows" :key="inv.token">
          <TableCell><StatusBadge :variant="inv.role === 'admin' ? 'caution' : 'neutral'">{{ inv.role === 'admin' ? t('admin.invitations.roleAdmin') : t('admin.invitations.roleUser') }}</StatusBadge></TableCell>
          <TableCell class="text-muted">{{ relativeTime(inv.createdAt) }}</TableCell>
          <TableCell class="text-muted">{{ formatDateTime(inv.expiresAt) }}</TableCell>
          <TableCell class="min-w-0"><CodeField :value="inv.url" /></TableCell>
          <TableCell><Button type="button" variant="outline" size="sm" :disabled="busy" :data-test="`revoke-${inv.token}`" @click="revokeToken = inv.token">{{ t('admin.invitations.revoke') }}</Button></TableCell>
        </TableRow>
      </TableBody>
    </Table>
    <p v-else-if="!busy && !errorText" class="text-sm text-muted">{{ t('admin.invitations.empty') }}</p>

    <ConfirmDialog :open="revokeToken !== null" :title="t('admin.invitations.revokeConfirmTitle')" :confirm-label="t('admin.invitations.revoke')" :busy="busy"
      @update:open="(v) => { if (!v) revokeToken = null }" @cancel="revokeToken = null" @confirm="revoke">
      {{ t('admin.invitations.revokeConfirmBody') }}
    </ConfirmDialog>
  </div>
</template>
```
Run the test → PASS (4 cases).

- [ ] **Step 3: Build + discard dist + commit.**
```bash
cd dashboard && mise exec -- npm run build && cd /home/tundra/projects/tundra/prohibitorum && git checkout -- pkg/webui/dist 2>/dev/null; git clean -fq pkg/webui/dist 2>/dev/null
git add dashboard/src/pages/admin/AdminInvitationsView.vue dashboard/src/pages/admin/AdminInvitationsView.test.ts
git commit -m "feat(web): AdminInvitationsView (/admin/invitations) — list, create, revoke

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: Wire routes + admin sidebar group + guard test

**Goal:** Mount the three admin pages as `requiresAdmin` children, add the `isAdmin`-gated Admin sidebar group, and prove the guard.

**Files:**
- Modify: `dashboard/src/router/index.ts`
- Modify: `dashboard/src/components/custom/AppSidebar.vue`, `dashboard/src/components/custom/AppSidebar.test.ts`
- Create: `dashboard/src/router/guard.test.ts`

**Acceptance Criteria:**
- [ ] `/admin/accounts`, `/admin/accounts/:id`, `/admin/invitations` resolve as `requiresAdmin` children of `DashboardLayout`.
- [ ] Admin sidebar group renders only when `auth.isAdmin`, with Accounts + Invitations links.
- [ ] Guard: non-admin hitting a `requiresAdmin` route → redirect to `error?error=forbidden`; admin passes.

**Verify:** `cd dashboard && mise exec -- npm run test -- AppSidebar guard` → pass; `mise exec -- npm run build` → clean.

**Steps:**

- [ ] **Step 1: Add routes.** In `dashboard/src/router/index.ts`, add three children to the `DashboardLayout` route's `children` array (after the `devices` child):
```ts
      { path: 'admin/accounts', name: 'admin-accounts', component: () => import('../pages/admin/AdminAccountsView.vue'), meta: { requiresAdmin: true } },
      { path: 'admin/accounts/:id', name: 'admin-account-detail', component: () => import('../pages/admin/AdminAccountDetailView.vue'), meta: { requiresAdmin: true } },
      { path: 'admin/invitations', name: 'admin-invitations', component: () => import('../pages/admin/AdminInvitationsView.vue'), meta: { requiresAdmin: true } },
```
(The parent already carries `meta: { requiresAuth: true }`; vue-router merges matched-record meta, so these children require both auth and admin.)

- [ ] **Step 2: Write the guard test (fails until... it already passes — the guard logic exists; this is a regression test for the new routes).** Create `dashboard/src/router/guard.test.ts`:
```ts
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { createRouter, createMemoryHistory } from 'vue-router'
import { createPinia, setActivePinia } from 'pinia'
import { defineComponent } from 'vue'
import { installGuard } from './index'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn() } }))
import { api } from '@/lib/api'
const get = vi.mocked(api.get)

const stub = defineComponent({ template: '<div/>' })
function makeRouter() {
  const r = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/login', name: 'login', component: stub, meta: { public: true } },
      { path: '/error', name: 'error', component: stub, meta: { public: true } },
      { path: '/admin', name: 'admin-accounts', component: stub, meta: { requiresAuth: true, requiresAdmin: true } },
    ],
  })
  installGuard(r)
  return r
}
beforeEach(() => { setActivePinia(createPinia()); get.mockReset() })

describe('router guard (requiresAdmin)', () => {
  it('redirects a non-admin to error?error=forbidden', async () => {
    get.mockResolvedValue({ id: 1, username: 'u', displayName: 'U', role: 'user' })
    const r = makeRouter()
    await r.push('/admin'); await r.isReady()
    expect(r.currentRoute.value.name).toBe('error')
    expect(r.currentRoute.value.query.error).toBe('forbidden')
  })
  it('lets an admin through', async () => {
    get.mockResolvedValue({ id: 1, username: 'a', displayName: 'A', role: 'admin' })
    const r = makeRouter()
    await r.push('/admin'); await r.isReady()
    expect(r.currentRoute.value.name).toBe('admin-accounts')
  })
})
```
Run `cd dashboard && mise exec -- npm run test -- guard` → both pass (the guard already enforces `requiresAdmin`; this locks it in).

- [ ] **Step 3: Add the admin sidebar group.** In `dashboard/src/components/custom/AppSidebar.vue`:
  - Add `Users` and `Ticket` to the `lucide-vue-next` import:
```ts
import { ShieldCheck, User, MonitorSmartphone, LogOut, KeyRound, Link2, TabletSmartphone, Users, Ticket } from 'lucide-vue-next'
```
  - Add an `adminItems` computed next to `accountItems`:
```ts
const adminItems = computed(() => [
  { to: '/admin/accounts', label: t('admin.nav.accounts'), icon: Users },
  { to: '/admin/invitations', label: t('admin.nav.invitations'), icon: Ticket },
])
```
  - In the template, immediately after the closing `</SidebarGroup>` of the Account group (replacing the `<!-- Admin group (Spec 3) ... -->` comment), add:
```vue
      <SidebarGroup v-if="auth.isAdmin">
        <SidebarGroupLabel>{{ t('admin.nav.title') }}</SidebarGroupLabel>
        <SidebarGroupContent>
          <SidebarMenu>
            <SidebarMenuItem v-for="item in adminItems" :key="item.to">
              <SidebarMenuButton as-child :tooltip="item.label" :is-active="isActive(item.to)">
                <RouterLink :to="item.to">
                  <component :is="item.icon" aria-hidden="true" />
                  <span>{{ item.label }}</span>
                </RouterLink>
              </SidebarMenuButton>
            </SidebarMenuItem>
          </SidebarMenu>
        </SidebarGroupContent>
      </SidebarGroup>
```
  - If `Ticket` is not exported by `lucide-vue-next`, fall back to `Mail` (verify with the build in Step 5). `Users` is a stable export.

- [ ] **Step 4: Extend the sidebar test.** In `dashboard/src/components/custom/AppSidebar.test.ts`:
  - Add the three admin paths to `makeRouter()`'s routes array:
```ts
{ path: '/admin/accounts', component: stub }, { path: '/admin/invitations', component: stub },
```
  - Add a test asserting the admin group renders for admins and not for non-admins:
```ts
  it('renders the admin group only for admins', async () => {
    const auth = useAuthStore()
    auth.me = { id: 1, username: 'alex', displayName: 'Alex Smith', role: 'admin' }
    const router = makeRouter(); router.push('/'); await router.isReady()
    const wrapper = mount(Host, { global: { plugins: [router, makeI18n()], components: { AppSidebar } } })
    const links = wrapper.findAll('a').map((a) => a.attributes('href'))
    expect(links).toContain('/admin/accounts')
    expect(links).toContain('/admin/invitations')
  })

  it('hides the admin group for non-admins', async () => {
    const auth = useAuthStore()
    auth.me = { id: 2, username: 'bob', displayName: 'Bob Lee', role: 'user' }
    const router = makeRouter(); router.push('/'); await router.isReady()
    const wrapper = mount(Host, { global: { plugins: [router, makeI18n()], components: { AppSidebar } } })
    const links = wrapper.findAll('a').map((a) => a.attributes('href'))
    expect(links).not.toContain('/admin/accounts')
  })
```
Run `cd dashboard && mise exec -- npm run test -- AppSidebar` → pass (existing + 2 new).

- [ ] **Step 5: Build (verifies routes + icon exports) + discard dist + commit.**
```bash
cd dashboard && mise exec -- npm run build && cd /home/tundra/projects/tundra/prohibitorum && git checkout -- pkg/webui/dist 2>/dev/null; git clean -fq pkg/webui/dist 2>/dev/null
git add dashboard/src/router/index.ts dashboard/src/router/guard.test.ts dashboard/src/components/custom/AppSidebar.vue dashboard/src/components/custom/AppSidebar.test.ts
git commit -m "feat(web): wire admin routes + isAdmin sidebar group + guard test

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: Done-gate — full suite, Go, smoke, commit dist

**Goal:** Prove the slice green end-to-end and commit the rebuilt embed.

**Files:**
- Modify: `pkg/webui/dist/**` (rebuilt embed, committed here)

**Acceptance Criteria:**
- [ ] Full vitest suite passes.
- [ ] `go build ./... && go vet ./...` exit 0.
- [ ] Smoke `SMOKE_EXIT=0`.
- [ ] `pkg/webui/dist` rebuilt + committed.

**Verify:** commands below.

**Steps:**

- [ ] **Step 1: Full frontend suite.** `cd dashboard && mise exec -- npm run test` → all suites pass (prior 91 + time + AdminAccountsView + AdminAccountDetailView + AdminInvitationsView + guard + AppSidebar additions). Do NOT pipe through `tail` in a gating chain.
- [ ] **Step 2: Lint/typecheck (if defined) + build.** `cd dashboard && mise exec -- npm run build` → clean.
- [ ] **Step 3: Go gate.** `cd /home/tundra/projects/tundra/prohibitorum && mise exec -- go build ./... && mise exec -- go vet ./...` → exit 0.
- [ ] **Step 4: Smoke.** `cd /home/tundra/projects/tundra/prohibitorum && setsid bash /tmp/run_v06.sh`; poll `/tmp/v06.result` for `SMOKE_EXIT=0` (full log `/tmp/smoke-v06.log`). NEVER bare `pkill -f 'prohibitorum'` (kills dev PG). If `/tmp/run_v06.sh` is absent, locate the slice's smoke runner; report rather than skip.
- [ ] **Step 5: Rebuild + commit dist.**
```bash
cd /home/tundra/projects/tundra/prohibitorum/dashboard && mise exec -- npm run build
cd /home/tundra/projects/tundra/prohibitorum && git add pkg/webui/dist
git commit -m "build(web): rebuild embedded dist for Spec 3a (admin accounts + invitations)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```
- [ ] **Step 6: Final state.** Confirm `git status` clean; report gate evidence (vitest count, go exit 0, `SMOKE_EXIT=0`).

---

## Self-Review notes (author)

- **Spec coverage:** shell (Table + sidebar group + routes + guard) → Tasks 1+5; AdminAccountsView → Task 2; AdminAccountDetailView (identity PUT w/ attribute round-trip, passkey force-revoke [sudo], revoke-all-sessions, reissue, delete) → Task 3; AdminInvitationsView (list/create/revoke) → Task 4; done-gate + dist → Task 6. All spec sections mapped.
- **Contract fidelity:** endpoints are the authoritative POST-with-body shapes from `pkg/contract/auth.go` (`/accounts/delete`, `/accounts/revoke-sessions`, `/accounts/reissue-enrollment`, `/invitations/revoke`); PUT round-trips `attributes` (asserted by a test); only credential force-revoke is sudo-gated (others wrapped in `withSudo` harmlessly). Error codes mapped: `last_admin`, `admin_cannot_be_disabled`, `cannot_delete_self`, `invalid_role`, `username_immutable`, `account_not_found`, `invitation_not_found`, `forbidden`.
- **Ordering:** views (Tasks 2–4) are built and unit-tested standalone (mocked `api`/`vue-router`) before routes/sidebar (Task 5), so no lazy-import points at a missing file mid-plan; dist committed once (Task 6).
- **Type consistency:** `relativeTime`/`formatDateTime` signatures match across tasks; `Account`/`Credential`/`Invitation` interfaces match the verified JSON; `clickConfirm(label)` helper (Task 3/4) mirrors the proven 2c ConfirmDialog interaction.
- **Risks (flagged, not placeholders):** `Ticket` lucide export (Task 5 falls back to `Mail`, build verifies); the `<select v-model>` `.setValue()` test interaction is standard @vue/test-utils. en.ts edits guarded by the U+2018 grep at each touch.
