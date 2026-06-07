# Frontend Rebuild — Spec 2a (Shell + Sudo Gate + Profile + Sessions) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers-extended-cc:subagent-driven-development (recommended) or superpowers-extended-cc:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stand up the authenticated dashboard shell (DashboardLayout + AppSidebar over the vendored shadcn-vue Sidebar primitive), the reusable sudo step-up gate, and the first two real pages (read-only Profile, Sessions list+revoke), restoring the `login → dashboard → logout` loop on the new stack.

**Architecture:** A `/` nested layout route (`DashboardLayout`, `meta.requiresAuth`) renders the persistent sidebar + `<RouterView/>`; children are Profile (`''`) and Sessions (`sessions`). The existing `installGuard` redirects unauthenticated users to `/login?return_to=`. The sudo gate is a module-singleton (`lib/sudo.ts`) driving a single `SudoModal` mounted in the layout. Frontend-only — no backend change; new routes ride the chi `NotFound` SPA fallback.

**Tech Stack:** Vue 3.5 + TS strict, Tailwind v4 `@theme` tokens, shadcn-vue/Reka UI (vendored `Sidebar` primitive — the capability floor, enhanced not stripped), vue-router 4, vue-i18n (English-first), Pinia, `@simplewebauthn/browser` (via `lib/webauthn`), vitest + `@vue/test-utils`.

**Spec:** `docs/superpowers/specs/2026-06-07-frontend-rebuild-dashboard-shell-design.md`

**Conventions:**
- Commit directly to `master` (no remote, no worktree — project convention).
- Run frontend tooling from `dashboard/` (npm; node via mise).
- After any Vue change that must reach the binary: `cd dashboard && npm run build` then `git add pkg/webui/dist`. Vite chunk hashes are non-deterministic — for per-task source-only commits, `git checkout -- pkg/webui/dist && git clean -fq pkg/webui/dist` after a verify build; rebuild + commit dist once in Task 5.
- Go gate authoritative: `mise exec -- go build ./... && mise exec -- go vet ./...` exit 0.
- Smoke: `setsid bash /tmp/run_v06.sh`, poll `/tmp/v06.result` for `SMOKE_EXIT=0`. Never bare `pkill -f 'prohibitorum'`.
- Backend contracts are canonical and already verified in the spec (§8). Re-derive FE cleanly; the old `dashboard/` (git `e45f356`) is advisory only.

---

### Task 1: Vendor the shadcn-vue Sidebar primitive (+ deps) into `ui/`

**Goal:** Add the `sidebar` primitive and its dependencies under `components/ui/` via the CLI fence, reconcile the CLI-added CSS variables onto our Welcoming Vault tokens, and confirm the build is clean — keeping the primitive pristine (no markup hand-edits).

**Files:**
- Create (via CLI): `dashboard/src/components/ui/sidebar/*`, `dashboard/src/components/ui/sheet/*`, `dashboard/src/components/ui/tooltip/*`, `dashboard/src/components/ui/separator/*`, `dashboard/src/components/ui/skeleton/*`
- Modify: `dashboard/src/assets/main.css` (map `--sidebar-*` vars onto tokens)
- Modify: `dashboard/src/components/ui/README.md` (note the added primitives)

**Acceptance Criteria:**
- [ ] `npx shadcn-vue@latest add sidebar` writes only under `src/components/ui/` (alias fence holds); `git status` shows no files outside `ui/` except the `main.css`/README edits.
- [ ] The CLI-appended `--sidebar-*` CSS variables are mapped onto our tokens (Sunken surface, Ink text, Tide ring) in `main.css`; our existing OKLCH `@theme` values and the `--accent`→sunken (off-Ember) mapping are preserved.
- [ ] `cd dashboard && npm run build` exit 0; `dist/index.html` still has zero inline `<style>` tags.

**Verify:** `cd dashboard && npm run build` → exit 0; `grep -c '<style' ../pkg/webui/dist/index.html` → `0`.

**Steps:**

- [ ] **Step 1: Add the primitive via the CLI.**
```bash
cd dashboard
npx shadcn-vue@latest add sidebar
```
Accept any prompted dependencies (`sheet`, `tooltip`, `separator`, `skeleton`, `button` already present). Confirm everything landed under `src/components/ui/` only:
```bash
git status --porcelain src/components | grep -v '/ui/' || echo "fence OK — only ui/ touched"
```

- [ ] **Step 2: Inspect what the CLI generated.** The sidebar barrel typically exports `SidebarProvider`, `Sidebar`, `SidebarHeader`, `SidebarContent`, `SidebarFooter`, `SidebarGroup`, `SidebarGroupLabel`, `SidebarGroupContent`, `SidebarMenu`, `SidebarMenuItem`, `SidebarMenuButton`, `SidebarInset`, `SidebarRail`, `SidebarTrigger`, and `useSidebar`. Confirm the exact export names from the generated `src/components/ui/sidebar/index.ts` — later tasks import these names; if the installed version differs, use the actual names.
```bash
sed -n '1,40p' src/components/ui/sidebar/index.ts
```

- [ ] **Step 3: Reconcile CSS variables in `main.css`.** The CLI may append a `:root`/`.dark` block defining `--sidebar`, `--sidebar-foreground`, `--sidebar-primary`, `--sidebar-primary-foreground`, `--sidebar-accent`, `--sidebar-accent-foreground`, `--sidebar-border`, `--sidebar-ring`. Keep the variable NAMES but point them at our tokens (the sidebar sits on the Sunken neutral layer per DESIGN.md). Ensure this block lives in the existing `:root` mapping (merge, don't duplicate), immediately after the existing structural vars:
```css
  /* Sidebar (shadcn-vue Sidebar primitive) — mapped onto Welcoming Vault tokens.
     The sidebar is the Sunken second neutral layer; active item = Tide. */
  --sidebar:                    var(--color-sunken);
  --sidebar-foreground:         var(--color-ink);
  --sidebar-primary:            var(--color-tide-strong);
  --sidebar-primary-foreground: var(--color-bg);
  --sidebar-accent:             var(--color-border);      /* hover tint — neutral, NOT Ember */
  --sidebar-accent-foreground:  var(--color-ink);
  --sidebar-border:             var(--color-border);
  --sidebar-ring:               var(--color-tide);
```
If the CLI appended its own `--sidebar-*` definitions elsewhere (e.g. a second `:root` or a `.dark` block with values), delete those CLI value lines (keep our mapping above as the single source); leave the `.dark` block's TODO comment intact.

- [ ] **Step 4: Build + verify no inline style elements.**
```bash
npm run build
grep -c '<style' ../pkg/webui/dist/index.html   # expect 0
```

- [ ] **Step 5: README + restore dist + commit (source only).** Append to `src/components/ui/README.md`: "Added Spec 2a: sidebar (+ sheet/tooltip/separator/skeleton). The Sidebar primitive is the capability floor — keep collapse/drawer/tooltip/a11y; enhance via tokens only." Then restore the non-deterministic dist and commit source:
```bash
cd ..
git checkout -- pkg/webui/dist && git clean -fq pkg/webui/dist
git add dashboard/src/components/ui dashboard/src/assets/main.css
git commit -m "feat(web): vendor shadcn-vue Sidebar primitive (+ sheet/tooltip/separator/skeleton), token-mapped

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: Sudo step-up gate — `lib/sudo.ts` + `SudoModal.vue`

**Goal:** The reusable elevation gate: a module singleton (`withSudo`/`ensureSudo`) and the ceremony modal (passkey + password/TOTP), driven by the verified `/me/sudo/*` contract, with unit + flow tests.

**Files:**
- Create: `dashboard/src/lib/sudo.ts`, `dashboard/src/lib/sudo.test.ts`
- Create: `dashboard/src/components/custom/SudoModal.vue`, `dashboard/src/components/custom/SudoModal.test.ts`
- Modify: `dashboard/src/locales/en.ts` (add `sudo.*` namespace + `errors.sudo_method_unavailable`)

**Acceptance Criteria:**
- [ ] `withSudo(fn)`: returns `fn()` on success; on a thrown `{code:'sudo_required'}` opens the modal and retries `fn()` exactly once if the user completed it; rethrows if cancelled; rethrows non-sudo errors immediately without opening the modal.
- [ ] `SudoModal` runs the passkey path (`begin{webauthn}` → `passkeyGet` → `complete`) and the password+TOTP path (`begin{password_totp}` → `complete{current_password,totp_code}`), resolving `true` on `204`; cancel/close resolves `false`; empty `methods` shows a terminal "no method" message.
- [ ] `cd dashboard && npm run test` passes (existing 35 + new).

**Verify:** `cd dashboard && npm run test` → PASS.

**Steps:**

- [ ] **Step 1: Write `lib/sudo.ts`.**
```ts
import { ref } from 'vue'

/**
 * Sudo step-up gate (singleton). The SudoModal — mounted once in
 * DashboardLayout — watches `sudoState`; withSudo()/ensureSudo() open it and
 * await the user's ceremony. Backend contract: sensitive /me actions return
 * {code:'sudo_required'} until the session has a fresh (one-shot) sudo grant.
 */
export interface SudoState {
  open: boolean
  resolve: ((ok: boolean) => void) | null
}
export const sudoState = ref<SudoState>({ open: false, resolve: null })

/** Open the step-up modal; resolves true (elevated) / false (cancelled). */
export function ensureSudo(): Promise<boolean> {
  return new Promise<boolean>((resolve) => {
    sudoState.value = { open: true, resolve }
  })
}

/** Test/internal hook: resolve the pending sudo promise and close the modal. */
export function _resolveSudo(ok: boolean): void {
  const r = sudoState.value.resolve
  sudoState.value = { open: false, resolve: null }
  r?.(ok)
}

/** Run fn(); if it fails with sudo_required, step up and retry once. */
export async function withSudo<T>(fn: () => Promise<T>): Promise<T> {
  try {
    return await fn()
  } catch (e: unknown) {
    if ((e as { code?: string })?.code !== 'sudo_required') throw e
    const ok = await ensureSudo()
    if (!ok) throw e
    return await fn()
  }
}
```

- [ ] **Step 2: Write `lib/sudo.test.ts` (run → expect FAIL until Step 1 saved, then PASS).**
```ts
import { describe, it, expect, vi } from 'vitest'
import { withSudo, sudoState, _resolveSudo } from './sudo'

describe('withSudo', () => {
  it('passes through on success without opening the modal', async () => {
    const fn = vi.fn(async () => 'ok')
    expect(await withSudo(fn)).toBe('ok')
    expect(sudoState.value.open).toBe(false)
    expect(fn).toHaveBeenCalledOnce()
  })

  it('steps up and retries once on sudo_required', async () => {
    const fn = vi.fn()
      .mockRejectedValueOnce({ code: 'sudo_required' })
      .mockResolvedValueOnce('done')
    const p = withSudo(fn as () => Promise<string>)
    await Promise.resolve() // let the first fn() reject + ensureSudo open
    expect(sudoState.value.open).toBe(true)
    _resolveSudo(true)
    expect(await p).toBe('done')
    expect(fn).toHaveBeenCalledTimes(2)
  })

  it('rethrows when the user cancels the step-up', async () => {
    const err = { code: 'sudo_required' }
    const fn = vi.fn().mockRejectedValue(err)
    const p = withSudo(fn as () => Promise<unknown>)
    await Promise.resolve()
    _resolveSudo(false)
    await expect(p).rejects.toBe(err)
    expect(fn).toHaveBeenCalledOnce() // not retried
  })

  it('rethrows non-sudo errors immediately', async () => {
    const err = { code: 'bad_request' }
    const fn = vi.fn().mockRejectedValue(err)
    await expect(withSudo(fn as () => Promise<unknown>)).rejects.toBe(err)
    expect(sudoState.value.open).toBe(false)
  })
})
```
Run: `npm run test src/lib/sudo.test.ts`.

- [ ] **Step 3: Add i18n in `locales/en.ts`.** Add a `sudo` namespace (place after `enroll`) and one error code (in `errors`, after `factor_locked`):
```ts
  sudo: {
    title: 'Confirm it’s you',
    prompt: 'For your security, re-verify before making this change.',
    passkeyButton: 'Verify with passkey',
    usePassword: 'Use password and code instead',
    passwordLabel: 'Current password',
    codeLabel: 'One-time code',
    verify: 'Verify',
    cancel: 'Cancel',
    noMethod: 'No verification method is available on this account. Contact an administrator.',
  },
```
```ts
    sudo_method_unavailable: 'That verification method isn’t available on your account.',
```

- [ ] **Step 4: Write `components/custom/SudoModal.vue`.**
```vue
<script setup lang="ts">
/**
 * SudoModal — the sudo step-up ceremony. Mounted ONCE in DashboardLayout;
 * watches the lib/sudo singleton. Opening fetches the account's elevation
 * methods; the user re-proves a factor (passkey, or password + TOTP); a 204
 * from /me/sudo/complete resolves the pending withSudo()/ensureSudo() promise.
 *
 * Contract (verified, pkg/server/handle_sudo.go):
 *   GET  /me/sudo/methods          → { methods: ('webauthn'|'password_totp')[] }
 *   POST /me/sudo/begin {method}   → webauthn: 200 options / password_totp: 204
 *   POST /me/sudo/complete         → webauthn: assertion / pwd: {current_password,totp_code} → 204
 */
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PublicKeyCredentialRequestOptionsJSON } from '@simplewebauthn/browser'
import { api, type ApiError } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { useWebauthn } from '@/composables/useWebauthn'
import { sudoState, _resolveSudo } from '@/lib/sudo'
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter,
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Alert, AlertDescription } from '@/components/ui/alert'

const { t, te } = useI18n()
const { busy: netBusy, error: netError, run } = useApi()
const { busy: waBusy, error: waError, authenticate } = useWebauthn()

const open = computed({
  get: () => sudoState.value.open,
  set: (v) => { if (!v) _resolveSudo(false) }, // closing via X/escape = cancel
})

const methods = ref<string[]>([])
const showPwForm = ref(false)
const password = ref('')
const code = ref('')

const busy = computed(() => netBusy.value || waBusy.value)
const error = computed<ApiError | null>(() => netError.value ?? waError.value)
const errorText = computed(() => {
  const e = error.value
  if (!e) return ''
  const key = `errors.${e.code}`
  return te(key) ? t(key) : e.message || t('common.error')
})
const hasPasskey = computed(() => methods.value.includes('webauthn'))
const hasPwTotp = computed(() => methods.value.includes('password_totp'))

// Each open: reset + fetch methods.
watch(() => sudoState.value.open, async (isOpen) => {
  if (!isOpen) return
  methods.value = []
  showPwForm.value = false
  password.value = ''
  code.value = ''
  netError.value = null
  try {
    const res = await api.get<{ methods: string[] }>('/api/prohibitorum/me/sudo/methods')
    methods.value = res.methods ?? []
    showPwForm.value = !hasPasskey.value && hasPwTotp.value
  } catch {
    methods.value = []
  }
})

async function doPasskey(): Promise<void> {
  const options = await run(() =>
    api.post<PublicKeyCredentialRequestOptionsJSON>('/api/prohibitorum/me/sudo/begin', { method: 'webauthn' }),
  )
  if (!options) return
  const assertion = await authenticate(options)
  if (!assertion) return
  const ok = await run(async () => {
    await api.post('/api/prohibitorum/me/sudo/complete', assertion)
    return true as const
  })
  if (ok) _resolveSudo(true)
}

async function doPasswordTotp(): Promise<void> {
  const began = await run(async () => {
    await api.post('/api/prohibitorum/me/sudo/begin', { method: 'password_totp' })
    return true as const
  })
  if (!began) return
  const ok = await run(async () => {
    await api.post('/api/prohibitorum/me/sudo/complete', {
      current_password: password.value,
      totp_code: code.value,
    })
    return true as const
  })
  if (ok) _resolveSudo(true)
}
</script>

<template>
  <Dialog v-model:open="open">
    <DialogContent>
      <DialogHeader>
        <DialogTitle>{{ t('sudo.title') }}</DialogTitle>
        <DialogDescription>{{ t('sudo.prompt') }}</DialogDescription>
      </DialogHeader>

      <p v-if="methods.length === 0" class="text-sm text-muted">{{ t('sudo.noMethod') }}</p>

      <div v-else class="flex flex-col gap-4">
        <Button v-if="hasPasskey && !showPwForm" class="w-full" :disabled="busy" @click="doPasskey">
          {{ t('sudo.passkeyButton') }}
        </Button>

        <button
          v-if="hasPasskey && hasPwTotp && !showPwForm"
          type="button"
          class="text-sm text-tide-strong underline-offset-4 hover:underline"
          @click="showPwForm = true"
        >
          {{ t('sudo.usePassword') }}
        </button>

        <form v-if="showPwForm" class="flex flex-col gap-3" @submit.prevent="doPasswordTotp">
          <div class="flex flex-col gap-1.5">
            <Label for="sudo-password">{{ t('sudo.passwordLabel') }}</Label>
            <Input id="sudo-password" v-model="password" name="current_password" type="password"
                   autocomplete="current-password" required />
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="sudo-code">{{ t('sudo.codeLabel') }}</Label>
            <Input id="sudo-code" v-model="code" name="totp_code" inputmode="numeric"
                   autocomplete="one-time-code" required />
          </div>
          <Button type="submit" class="w-full" :disabled="busy">{{ t('sudo.verify') }}</Button>
        </form>

        <Alert v-if="errorText" variant="destructive" role="alert" aria-live="polite">
          <AlertDescription>{{ errorText }}</AlertDescription>
        </Alert>
      </div>

      <DialogFooter>
        <Button variant="ghost" :disabled="busy" @click="_resolveSudo(false)">{{ t('sudo.cancel') }}</Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>
</template>
```

- [ ] **Step 5: Write `components/custom/SudoModal.test.ts`.**
```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import SudoModal from './SudoModal.vue'
import { sudoState } from '@/lib/sudo'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'
vi.mock('@/lib/webauthn', () => ({
  passkeyGet: vi.fn(async () => ({ id: 'assert', response: {} })),
  passkeyRegister: vi.fn(),
  isUserCancel: () => false,
}))

const get = vi.mocked(api.get)
const post = vi.mocked(api.post)

function makeI18n() {
  return createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
}
function mountModal() {
  return mount(SudoModal, { global: { plugins: [makeI18n()] }, attachTo: document.body })
}
async function openAndSettle() {
  sudoState.value = { open: true, resolve: vi.fn() }
  await flushPromises()
}

beforeEach(() => { get.mockReset(); post.mockReset(); sudoState.value = { open: false, resolve: null } })

describe('SudoModal', () => {
  it('completes the passkey path and resolves true', async () => {
    get.mockResolvedValue({ methods: ['webauthn'] })
    post.mockImplementation(async (p: string) =>
      p.endsWith('/begin') ? { challenge: 'c' } : undefined)
    const resolve = vi.fn()
    mountModal()
    sudoState.value = { open: true, resolve }
    await flushPromises()
    // The single passkey button:
    const btn = document.querySelector('button')!
    ;(btn as HTMLButtonElement).click()
    await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/sudo/begin', { method: 'webauthn' })
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/sudo/complete', expect.objectContaining({ id: 'assert' }))
    expect(resolve).toHaveBeenCalledWith(true)
  })

  it('shows a terminal message when no method is available', async () => {
    get.mockResolvedValue({ methods: [] })
    mountModal()
    await openAndSettle()
    expect(document.body.textContent).toContain(en.sudo.noMethod)
  })
})
```
> Note: SudoModal renders into a teleported Dialog, so query `document.body` (mount with `attachTo: document.body`). If the installed Dialog teleports to a portal root, assert via `document.body.textContent` / `document.querySelector` as above rather than the wrapper.

- [ ] **Step 6: Run + commit (source only).**
```bash
cd dashboard && npm run test && cd ..
git checkout -- pkg/webui/dist 2>/dev/null; git clean -fq pkg/webui/dist 2>/dev/null || true
git add dashboard/src/lib/sudo.ts dashboard/src/lib/sudo.test.ts dashboard/src/components/custom/SudoModal.vue dashboard/src/components/custom/SudoModal.test.ts dashboard/src/locales/en.ts
git commit -m "feat(web): sudo step-up gate — lib/sudo (withSudo/ensureSudo) + SudoModal

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: Shell — `DashboardLayout` + `AppSidebar` + `StatusBadge`

**Goal:** The persistent authenticated shell: `DashboardLayout` composing the vendored Sidebar primitive + `<RouterView/>` + the single `SudoModal`; a config-driven `AppSidebar` (brand header, Account nav, footer identity+logout); and the `StatusBadge` token component.

**Files:**
- Create: `dashboard/src/components/custom/StatusBadge.vue`
- Create: `dashboard/src/components/custom/AppSidebar.vue`, `dashboard/src/components/custom/AppSidebar.test.ts`
- Create: `dashboard/src/pages/DashboardLayout.vue`
- Modify: `dashboard/src/locales/en.ts` (add `nav.*`)

**Acceptance Criteria:**
- [ ] `DashboardLayout` renders `SidebarProvider` + `Sidebar` (via `AppSidebar`) + `SidebarInset` containing `<RouterView/>`, and mounts `<SudoModal/>` once. The primitive's collapse/drawer/tooltip/a11y behavior is intact (not re-implemented or stripped).
- [ ] `AppSidebar` is config-driven: a `navItems` array → `SidebarMenu`; brand mark is the only Ember element; footer shows `auth.me.displayName` + a logout item that routes to `/logout`; the admin group is rendered only when `auth.isAdmin` (empty in 2a).
- [ ] `StatusBadge` renders a token-styled pill with `variant` ∈ `neutral|success|caution|danger` (sage/amber/rose/neutral).
- [ ] `cd dashboard && npm run test` passes.

**Verify:** `cd dashboard && npm run test` → PASS; `npm run build` → exit 0.

**Steps:**

- [ ] **Step 1: `components/custom/StatusBadge.vue`.**
```vue
<script setup lang="ts">
/** StatusBadge — small token-styled status pill (State-Has-a-Color, DESIGN.md). */
import { cva, type VariantProps } from 'class-variance-authority'
import { cn } from '@/lib/utils'

const badge = cva(
  'inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium',
  {
    variants: {
      variant: {
        neutral: 'bg-sunken text-muted',
        success: 'bg-sage/12 text-sage',
        caution: 'bg-amber/15 text-amber',
        danger: 'bg-rose/12 text-rose',
      },
    },
    defaultVariants: { variant: 'neutral' },
  },
)
type Props = { variant?: VariantProps<typeof badge>['variant']; class?: string }
const props = defineProps<Props>()
</script>

<template>
  <span :class="cn(badge({ variant: props.variant }), props.class)"><slot /></span>
</template>
```

- [ ] **Step 2: Add `nav.*` i18n in `locales/en.ts`** (after `common`):
```ts
  nav: {
    account: 'Account',
    profile: 'Profile',
    sessions: 'Sessions',
    signOut: 'Sign out',
  },
```

- [ ] **Step 3: `components/custom/AppSidebar.vue`** (config-driven; uses the vendored primitive — confirm the export names against Task 1 Step 2):
```vue
<script setup lang="ts">
/**
 * AppSidebar — config-driven navigation over the vendored shadcn-vue Sidebar
 * primitive (the capability floor: collapse/drawer/tooltip/a11y come from it).
 * Header = brand mark (the single Ember moment). Content = Account nav group
 * (built links only for Spec 2a). Footer = identity + logout (utility tier).
 * An admin group renders only when auth.isAdmin (lands in Spec 3).
 */
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { ShieldCheck, User, MonitorSmartphone, LogOut } from 'lucide-vue-next'
import { useAuthStore } from '@/stores/auth'
import {
  Sidebar, SidebarHeader, SidebarContent, SidebarFooter,
  SidebarGroup, SidebarGroupLabel, SidebarGroupContent,
  SidebarMenu, SidebarMenuItem, SidebarMenuButton,
} from '@/components/ui/sidebar'

const { t } = useI18n()
const auth = useAuthStore()

// Config-driven nav — built links only for 2a; Security/Connected/Devices land in 2b/2c.
const accountItems = computed(() => [
  { to: '/', label: t('nav.profile'), icon: User },
  { to: '/sessions', label: t('nav.sessions'), icon: MonitorSmartphone },
])
</script>

<template>
  <Sidebar>
    <SidebarHeader>
      <div class="flex items-center gap-2 px-2 py-1.5">
        <span class="inline-flex size-7 items-center justify-center rounded-md bg-ember/10 text-ember">
          <ShieldCheck class="size-5" aria-hidden="true" />
        </span>
        <span class="font-semibold tracking-tight text-ink">Prohibitorum</span>
      </div>
    </SidebarHeader>

    <SidebarContent>
      <SidebarGroup>
        <SidebarGroupLabel>{{ t('nav.account') }}</SidebarGroupLabel>
        <SidebarGroupContent>
          <SidebarMenu>
            <SidebarMenuItem v-for="item in accountItems" :key="item.to">
              <SidebarMenuButton as-child :tooltip="item.label">
                <RouterLink :to="item.to">
                  <component :is="item.icon" aria-hidden="true" />
                  <span>{{ item.label }}</span>
                </RouterLink>
              </SidebarMenuButton>
            </SidebarMenuItem>
          </SidebarMenu>
        </SidebarGroupContent>
      </SidebarGroup>
      <!-- Admin group (Spec 3): rendered only for admins. Empty in 2a. -->
    </SidebarContent>

    <SidebarFooter>
      <div class="flex flex-col gap-1 px-2 py-1.5">
        <span v-if="auth.me" class="truncate text-sm text-muted">{{ auth.me.displayName }}</span>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton as-child :tooltip="t('nav.signOut')">
              <RouterLink to="/logout">
                <LogOut aria-hidden="true" />
                <span>{{ t('nav.signOut') }}</span>
              </RouterLink>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </div>
    </SidebarFooter>
  </Sidebar>
</template>
```
> `RouterLink` active styling: the primitive's `SidebarMenuButton` supports an `isActive` prop; vue-router applies `router-link-active`/`router-link-exact-active` classes to the rendered `<a>` automatically, which is sufficient for 2a. (Wiring `isActive` to the route can be a 2b polish.)

- [ ] **Step 4: `pages/DashboardLayout.vue`.**
```vue
<script setup lang="ts">
/**
 * DashboardLayout — the authenticated shell. SidebarProvider keeps the
 * sidebar's collapse/drawer state across route changes; SidebarInset holds the
 * routed page. SudoModal is mounted ONCE here so any page's withSudo() can
 * drive it. The auth store is loaded by the router guard before this renders;
 * we ensureLoaded() defensively for direct mounts.
 */
import { onMounted } from 'vue'
import { useAuthStore } from '@/stores/auth'
import { SidebarProvider, SidebarInset, SidebarTrigger } from '@/components/ui/sidebar'
import AppSidebar from '@/components/custom/AppSidebar.vue'
import SudoModal from '@/components/custom/SudoModal.vue'

const auth = useAuthStore()
onMounted(() => { void auth.ensureLoaded() })
</script>

<template>
  <SidebarProvider>
    <AppSidebar />
    <SidebarInset>
      <header class="flex h-14 items-center gap-2 border-b border-border px-4">
        <SidebarTrigger />
      </header>
      <main class="flex-1 p-6">
        <RouterView />
      </main>
    </SidebarInset>
    <SudoModal />
  </SidebarProvider>
</template>
```

- [ ] **Step 5: `components/custom/AppSidebar.test.ts`.**
```ts
import { describe, it, expect, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import { defineComponent } from 'vue'
import en from '@/locales/en'
import AppSidebar from './AppSidebar.vue'
import { useAuthStore } from '@/stores/auth'

const stub = defineComponent({ template: '<div/>' })
function makeRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/', component: stub }, { path: '/sessions', component: stub }, { path: '/logout', component: stub }],
  })
}
function makeI18n() {
  return createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
}

beforeEach(() => setActivePinia(createPinia()))

describe('AppSidebar', () => {
  it('renders the built Account links and a footer sign-out', async () => {
    const auth = useAuthStore()
    auth.me = { id: 1, username: 'alex', displayName: 'Alex Smith', role: 'user' }
    const router = makeRouter(); router.push('/'); await router.isReady()
    const wrapper = mount(AppSidebar, { global: { plugins: [router, makeI18n(), createPinia()] } })
    const links = wrapper.findAll('a').map((a) => a.attributes('href'))
    expect(links).toContain('/')
    expect(links).toContain('/sessions')
    expect(links).toContain('/logout')
    expect(wrapper.text()).toContain('Alex Smith')
  })
})
```
> If `SidebarProvider` context is required for `AppSidebar` to mount, wrap it: mount a small host component with `<SidebarProvider><AppSidebar/></SidebarProvider>`. Adjust per the primitive's requirement discovered in Task 1.

- [ ] **Step 6: Run + commit (source only).**
```bash
cd dashboard && npm run test && npm run build && cd ..
git checkout -- pkg/webui/dist && git clean -fq pkg/webui/dist
git add dashboard/src/components/custom/StatusBadge.vue dashboard/src/components/custom/AppSidebar.vue dashboard/src/components/custom/AppSidebar.test.ts dashboard/src/pages/DashboardLayout.vue dashboard/src/locales/en.ts
git commit -m "feat(web): authenticated shell — DashboardLayout + config-driven AppSidebar + StatusBadge

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: Pages — `ProfileView` + `SessionsView` + route wiring

**Goal:** The two real pages (read-only Profile; Sessions list+revoke) and the nested `/` layout route under `DashboardLayout` with `meta.requiresAuth`.

**Files:**
- Create: `dashboard/src/pages/ProfileView.vue`, `dashboard/src/pages/ProfileView.test.ts`
- Create: `dashboard/src/pages/SessionsView.vue`, `dashboard/src/pages/SessionsView.test.ts`
- Modify: `dashboard/src/router/index.ts` (add the nested layout route)
- Modify: `dashboard/src/locales/en.ts` (add `profile.*`, `sessions.*`)

**Acceptance Criteria:**
- [ ] `/` renders Profile (username/displayName/role from `auth.me`, read-only); `/sessions` lists `GET /me/sessions` with a current-session `StatusBadge`; revoke posts `{id}` and refreshes; the current row has no revoke control.
- [ ] The `/` layout route carries `meta.requiresAuth`; an unauthenticated visit is redirected to `/login?return_to=` by `installGuard`.
- [ ] `cd dashboard && npm run test` passes.

**Verify:** `cd dashboard && npm run test` → PASS; `npm run build` → exit 0.

**Steps:**

- [ ] **Step 1: i18n in `locales/en.ts`** (after `nav`):
```ts
  profile: {
    title: 'Profile',
    username: 'Username',
    displayName: 'Display name',
    role: 'Role',
  },
  sessions: {
    title: 'Active sessions',
    current: 'This device',
    revoke: 'Sign out',
    issued: 'Signed in',
    expires: 'Expires',
    lastSeen: 'Last seen',
    empty: 'No other active sessions.',
  },
```

- [ ] **Step 2: `pages/ProfileView.vue`** (read-only — no self-update endpoint exists):
```vue
<script setup lang="ts">
/** ProfileView (/) — read-only account profile from the auth store. */
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'

const { t } = useI18n()
const auth = useAuthStore()
</script>

<template>
  <Card class="max-w-xl">
    <CardHeader><CardTitle>{{ t('profile.title') }}</CardTitle></CardHeader>
    <CardContent>
      <dl v-if="auth.me" class="grid grid-cols-[8rem_1fr] gap-y-3 text-sm">
        <dt class="text-muted">{{ t('profile.username') }}</dt>
        <dd class="font-mono text-ink">{{ auth.me.username }}</dd>
        <dt class="text-muted">{{ t('profile.displayName') }}</dt>
        <dd class="text-ink">{{ auth.me.displayName }}</dd>
        <dt class="text-muted">{{ t('profile.role') }}</dt>
        <dd class="text-ink">{{ auth.me.role }}</dd>
      </dl>
    </CardContent>
  </Card>
</template>
```

- [ ] **Step 3: `pages/SessionsView.vue`.**
```vue
<script setup lang="ts">
/**
 * SessionsView (/sessions) — list active sessions; revoke non-current ones.
 * GET /me/sessions → SessionListItem[]; POST /me/sessions/revoke {id} (not
 * sudo-gated). The current session has no revoke control.
 */
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { Card, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import StatusBadge from '@/components/custom/StatusBadge.vue'

interface SessionListItem {
  id: string
  isCurrent: boolean
  issuedAt: string
  expiresAt: string
  lastSeenIp: string
  userAgent?: string
}

const { t } = useI18n()
const { busy, run } = useApi()
const rows = ref<SessionListItem[]>([])

async function load(): Promise<void> {
  const res = await run(() => api.get<SessionListItem[]>('/api/prohibitorum/me/sessions'))
  if (res) rows.value = res
}
async function revoke(id: string): Promise<void> {
  const ok = await run(async () => {
    await api.post('/api/prohibitorum/me/sessions/revoke', { id })
    return true as const
  })
  if (ok) await load()
}
onMounted(load)
</script>

<template>
  <div class="flex max-w-2xl flex-col gap-4">
    <h1 class="text-lg font-semibold tracking-tight text-ink">{{ t('sessions.title') }}</h1>
    <Card v-for="r in rows" :key="r.id">
      <CardContent class="flex items-center justify-between gap-4 py-4">
        <div class="flex flex-col gap-1 text-sm">
          <div class="flex items-center gap-2">
            <span class="text-ink">{{ r.userAgent || r.lastSeenIp }}</span>
            <StatusBadge v-if="r.isCurrent" variant="success">{{ t('sessions.current') }}</StatusBadge>
          </div>
          <span class="text-muted">{{ t('sessions.lastSeen') }}: {{ r.lastSeenIp }}</span>
        </div>
        <Button v-if="!r.isCurrent" variant="outline" size="sm" :disabled="busy"
                data-test="revoke" @click="revoke(r.id)">
          {{ t('sessions.revoke') }}
        </Button>
      </CardContent>
    </Card>
  </div>
</template>
```

- [ ] **Step 4: Wire the nested route in `router/index.ts`.** Add this entry to the `routes` array immediately BEFORE the catch-all (`/:pathMatch(.*)*`) entry:
```ts
  // Authenticated dashboard shell (Spec 2a). requiresAuth → installGuard
  // redirects to /login?return_to= when not signed in.
  {
    path: '/',
    component: () => import('../pages/DashboardLayout.vue'),
    meta: { requiresAuth: true },
    children: [
      { path: '', name: 'profile', component: () => import('../pages/ProfileView.vue') },
      { path: 'sessions', name: 'sessions', component: () => import('../pages/SessionsView.vue') },
    ],
  },
```

- [ ] **Step 5: `pages/ProfileView.test.ts`.**
```ts
import { describe, it, expect, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { createPinia, setActivePinia } from 'pinia'
import en from '@/locales/en'
import ProfileView from './ProfileView.vue'
import { useAuthStore } from '@/stores/auth'

function makeI18n() {
  return createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
}
beforeEach(() => setActivePinia(createPinia()))

describe('ProfileView', () => {
  it('renders the profile fields read-only', () => {
    const auth = useAuthStore()
    auth.me = { id: 1, username: 'alex', displayName: 'Alex Smith', role: 'admin' }
    const wrapper = mount(ProfileView, { global: { plugins: [makeI18n()] } })
    expect(wrapper.text()).toContain('alex')
    expect(wrapper.text()).toContain('Alex Smith')
    expect(wrapper.text()).toContain('admin')
    // Read-only: no inputs, no buttons.
    expect(wrapper.find('input').exists()).toBe(false)
    expect(wrapper.find('button').exists()).toBe(false)
  })
})
```

- [ ] **Step 6: `pages/SessionsView.test.ts`.**
```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import SessionsView from './SessionsView.vue'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'
const get = vi.mocked(api.get)
const post = vi.mocked(api.post)

function makeI18n() {
  return createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
}
const SESSIONS = [
  { id: 's1', isCurrent: true, issuedAt: '', expiresAt: '', lastSeenIp: '10.0.0.1', userAgent: 'Firefox' },
  { id: 's2', isCurrent: false, issuedAt: '', expiresAt: '', lastSeenIp: '10.0.0.2', userAgent: 'Safari' },
]
beforeEach(() => { get.mockReset(); post.mockReset() })

describe('SessionsView', () => {
  it('lists sessions; only non-current rows have a revoke control', async () => {
    get.mockResolvedValue(SESSIONS)
    const wrapper = mount(SessionsView, { global: { plugins: [makeI18n()] } })
    await flushPromises()
    expect(wrapper.text()).toContain('Firefox')
    expect(wrapper.text()).toContain('Safari')
    expect(wrapper.findAll('[data-test=revoke]')).toHaveLength(1) // only s2
  })

  it('revoke posts {id} and refreshes', async () => {
    get.mockResolvedValue(SESSIONS)
    post.mockResolvedValue(undefined)
    const wrapper = mount(SessionsView, { global: { plugins: [makeI18n()] } })
    await flushPromises()
    await wrapper.find('[data-test=revoke]').trigger('click')
    await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/sessions/revoke', { id: 's2' })
    expect(get).toHaveBeenCalledTimes(2) // initial + post-revoke refresh
  })
})
```

- [ ] **Step 7: Run + commit (source only).**
```bash
cd dashboard && npm run test && npm run build && cd ..
git checkout -- pkg/webui/dist && git clean -fq pkg/webui/dist
git add dashboard/src/pages/ProfileView.vue dashboard/src/pages/ProfileView.test.ts dashboard/src/pages/SessionsView.vue dashboard/src/pages/SessionsView.test.ts dashboard/src/router/index.ts dashboard/src/locales/en.ts
git commit -m "feat(web): /(Profile, read-only) + /sessions (list+revoke); nested dashboard route

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: Done-gate — CSP/embed check, rebuild dist, smoke

**Goal:** Confirm the shell adds no inline `<style>`, rebuild + commit the dist, and verify the full gate green (Go build/vet/test, vitest, smoke `SMOKE_EXIT=0`).

**Files:**
- Modify: `dashboard/pkg/webui/dist/*` (rebuilt, committed)

**Acceptance Criteria:**
- [ ] `pkg/webui/dist/index.html` has zero inline `<script>` (only `src=` module) and zero inline `<style>` elements.
- [ ] `mise exec -- go build ./... && go vet ./... && go test ./...` exit 0; `cd dashboard && npm run test` green; `npm run build` clean.
- [ ] `cmd/smoke` `SMOKE_EXIT=0` (step 5b: `/` and `/sessions` serve the SPA shell — now real routes).

**Verify:** `setsid bash /tmp/run_v06.sh`; `cat /tmp/v06.result` → `SMOKE_EXIT=0`.

**Steps:**

- [ ] **Step 1: Rebuild dist + inline-asset check.**
```bash
cd dashboard && npm run build && cd ..
grep -oE '<script[^>]*>' pkg/webui/dist/index.html   # only src=… module, no inline JS
grep -c '<style' pkg/webui/dist/index.html           # expect 0
```
If any inline `<style>` appears (a dependency regressed), apply the CSP fallback noted in `pkg/webui/webui.go` (revert to `style-src 'self' 'unsafe-inline'`) and document why. Otherwise no CSP change.

- [ ] **Step 2: Go gate.**
```bash
mise exec -- go build ./... && mise exec -- go vet ./... && mise exec -- go test ./...
```
Expected: exit 0 (the SPA is embedded fresh by `go run`/`go build`).

- [ ] **Step 3: Frontend gate.**
```bash
cd dashboard && npm run test && cd ..   # all suites green
```

- [ ] **Step 4: Smoke.**
```bash
rm -f /tmp/v06.result
setsid bash /tmp/run_v06.sh >/dev/null 2>&1 < /dev/null &
# poll until SMOKE_EXIT= appears, then:
grep 'SMOKE_EXIT=' /tmp/v06.result   # expect SMOKE_EXIT=0
```
Step 5b asserts `/` and `/sessions` serve the shell (`id="app"`) — they still do (the chi `NotFound` fallback serves index.html for any path; now they are real Vue routes too).

- [ ] **Step 5: Runtime spot-check (per always-verify-fixes).** Boot the server and confirm the authenticated loop renders:
```bash
# (server boot per /tmp/run_v06.sh env); then in a browser via mise dev-server:
# mise dev-server ; mise enroll-admin → visit /enroll/<token> → land on / (Profile)
# → /sessions → sign out (footer) → /logout. Sidebar collapse + mobile drawer work.
```

- [ ] **Step 6: Commit dist + gate result.**
```bash
git add pkg/webui/dist
git commit -m "feat(web): Spec 2a done-gate — rebuild+commit dist; CSP unchanged; smoke green

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Self-Review

- **Spec coverage:** §5 routing → Task 4; §5 component taxonomy → Tasks 1 (ui/sidebar), 2 (sudo), 3 (shell+StatusBadge), 4 (pages); §6 sudo gate → Task 2; §7 pages → Task 4; §8 contracts → consumed in Tasks 2/4; §9 i18n → split across Tasks 2/3/4; §10 testing → each task's tests; §11 embed/CSP/done-gate → Task 5. All mapped.
- **Placeholder scan:** crux code (sudo.ts, SudoModal, AppSidebar, DashboardLayout, Profile, Sessions, router edit, tests) is concrete. The two "confirm against the generated index / primitive requirement" notes (Task 1 Step 2; Task 3 Steps 3/5) are legitimate verify-against-generated-code steps (the shadcn-vue version isn't pinned), not placeholders.
- **Type/name consistency:** `sudoState`/`ensureSudo`/`withSudo`/`_resolveSudo` (Task 2) reused in Task 3's `SudoModal` mount + Task 2 tests; `SessionListItem` shape matches the contract; `StatusBadge` `variant` values (neutral/success/caution/danger) consistent between Tasks 3 and 4; nav/profile/sessions/sudo i18n keys consistent across Tasks 2–4.
- **Risk note:** the riskiest task is 1 (the exact shadcn-vue Sidebar export names + CSS-var reconciliation) — mitigated by the Step 2 inspection and the build/grep verify. Task 5's smoke is the integration backstop.
