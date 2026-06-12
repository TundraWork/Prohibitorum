# Account Dropdown + Security Default — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers-extended-cc:subagent-driven-development (recommended) or superpowers-extended-cc:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move the sparse Profile page into a sidebar account dropdown (display-name editing in a dialog), make Security the default dashboard page, and remove the Profile page.

**Architecture:** A newly-vendored shadcn-vue `DropdownMenu` primitive backs a `NavUser` footer control in the sidebar. `NavUser` shows identity + opens a dropdown (header, "Edit display name…", "Sign out"); the edit dialog is a sibling that opens on `nextTick` after select (avoids Reka's menu→dialog focus bug). The `/` index route redirects to `/security`; `ProfileView` and its route/nav-link are deleted.

**Tech Stack:** Vue 3 (`<script setup>`), Vite, Tailwind v4 (+ `@theme inline` token bridge), vendored shadcn-vue / Reka UI 2.9, Pinia, vue-i18n 10, Vitest + Vue Test Utils (jsdom). Embedded SPA: `dashboard/` builds into `pkg/webui/dist` (committed, `//go:embed`).

**Spec:** `docs/superpowers/specs/2026-06-12-account-dropdown-security-default-design.md`

---

## Conventions confirmed from the codebase (read once)

- **Icons:** vendored `ui/` primitives import from `@lucide/vue`; custom/app components import from `lucide-vue-next`. Follow that split.
- **Vendored-primitive style:** wrappers use `useForwardProps` / `useForwardPropsEmits` from `reka-ui`, `reactiveOmit` from `@vueuse/core`, `cn` from `@/lib/utils`, and a `data-slot` attr. Model new files on `src/components/ui/dialog/*`.
- **Theme tokens** (all bridged in `src/assets/main.css`): `bg-popover`, `text-popover-foreground`, `bg-accent`, `text-accent-foreground`, `border` (border-color), `text-muted`, `bg-sidebar-accent`, `text-sidebar-accent-foreground`, `text-ink`.
- **Server `displayName` rule** (`pkg/account/account.go`): length 1–128, reject control chars (`< 0x20 || === 0x7f`), **no trim**. Bad input → `invalid_display_name` (400). i18n key `errors.invalid_display_name` already exists.
- **Error display pattern:** `const key = 'errors.' + e.code; return te(key) ? t(key) : e.message || t('common.error')`.
- **Test idiom for portaled overlays:** mount with `attachTo: document.body`, query `document.body.querySelector(...)`, drive native `<input>` with `el.value = x; el.dispatchEvent(new Event('input'))`, click with `el.click()`, `await flushPromises()`. jsdom lacks `matchMedia` — polyfill inline (see `AppSidebar.test.ts`).
- **Build/typecheck:** `cd dashboard && npm run test` (vitest) and `npm run build` (= `vue-tsc -b && vite build`; `vue-tsc -b` is the real typecheck and catches `noUnusedLocals`). `vite build` writes `pkg/webui/dist`.

---

### Task 1: Vendor the `DropdownMenu` primitive

**Goal:** Add a `ui/dropdown-menu/` primitive (Reka wrappers) so `NavUser` can use the shadcn account-menu pattern.

**Files:**
- Create: `dashboard/src/components/ui/dropdown-menu/DropdownMenu.vue`
- Create: `dashboard/src/components/ui/dropdown-menu/DropdownMenuTrigger.vue`
- Create: `dashboard/src/components/ui/dropdown-menu/DropdownMenuContent.vue`
- Create: `dashboard/src/components/ui/dropdown-menu/DropdownMenuItem.vue`
- Create: `dashboard/src/components/ui/dropdown-menu/DropdownMenuLabel.vue`
- Create: `dashboard/src/components/ui/dropdown-menu/DropdownMenuSeparator.vue`
- Create: `dashboard/src/components/ui/dropdown-menu/DropdownMenuGroup.vue`
- Create: `dashboard/src/components/ui/dropdown-menu/index.ts`

**Acceptance Criteria:**
- [ ] All seven wrappers + barrel exist and compile.
- [ ] `vue-tsc -b` reports 0 errors.

**Verify:** `cd dashboard && npx vue-tsc -b` → no errors.

**Steps:**

- [ ] **Step 1: Create `DropdownMenu.vue` (Root)**

```vue
<script setup lang="ts">
import type { DropdownMenuRootEmits, DropdownMenuRootProps } from "reka-ui"
import { DropdownMenuRoot, useForwardPropsEmits } from "reka-ui"

const props = defineProps<DropdownMenuRootProps>()
const emits = defineEmits<DropdownMenuRootEmits>()

const forwarded = useForwardPropsEmits(props, emits)
</script>

<template>
  <DropdownMenuRoot data-slot="dropdown-menu" v-bind="forwarded">
    <slot />
  </DropdownMenuRoot>
</template>
```

- [ ] **Step 2: Create `DropdownMenuTrigger.vue`**

```vue
<script setup lang="ts">
import type { DropdownMenuTriggerProps } from "reka-ui"
import { DropdownMenuTrigger, useForwardProps } from "reka-ui"

const props = defineProps<DropdownMenuTriggerProps>()

const forwarded = useForwardProps(props)
</script>

<template>
  <DropdownMenuTrigger data-slot="dropdown-menu-trigger" v-bind="forwarded">
    <slot />
  </DropdownMenuTrigger>
</template>
```

- [ ] **Step 3: Create `DropdownMenuContent.vue`**

```vue
<script setup lang="ts">
import type { DropdownMenuContentEmits, DropdownMenuContentProps } from "reka-ui"
import type { HTMLAttributes } from "vue"
import { reactiveOmit } from "@vueuse/core"
import { DropdownMenuContent, DropdownMenuPortal, useForwardPropsEmits } from "reka-ui"
import { cn } from "@/lib/utils"

defineOptions({ inheritAttrs: false })

const props = withDefaults(defineProps<DropdownMenuContentProps & { class?: HTMLAttributes["class"] }>(), {
  sideOffset: 4,
})
const emits = defineEmits<DropdownMenuContentEmits>()

const delegatedProps = reactiveOmit(props, "class")
const forwarded = useForwardPropsEmits(delegatedProps, emits)
</script>

<template>
  <DropdownMenuPortal>
    <DropdownMenuContent
      data-slot="dropdown-menu-content"
      v-bind="{ ...forwarded, ...$attrs }"
      :class="cn(
        'bg-popover text-popover-foreground data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95 data-[side=bottom]:slide-in-from-top-2 data-[side=left]:slide-in-from-right-2 data-[side=right]:slide-in-from-left-2 data-[side=top]:slide-in-from-bottom-2 z-50 min-w-[8rem] overflow-hidden rounded-md border p-1 shadow-md',
        props.class,
      )"
    >
      <slot />
    </DropdownMenuContent>
  </DropdownMenuPortal>
</template>
```

- [ ] **Step 4: Create `DropdownMenuItem.vue`** (forwards `@select`)

```vue
<script setup lang="ts">
import type { DropdownMenuItemEmits, DropdownMenuItemProps } from "reka-ui"
import type { HTMLAttributes } from "vue"
import { reactiveOmit } from "@vueuse/core"
import { DropdownMenuItem, useForwardPropsEmits } from "reka-ui"
import { cn } from "@/lib/utils"

const props = defineProps<DropdownMenuItemProps & { class?: HTMLAttributes["class"], inset?: boolean }>()
const emits = defineEmits<DropdownMenuItemEmits>()

const delegatedProps = reactiveOmit(props, "class", "inset")
const forwarded = useForwardPropsEmits(delegatedProps, emits)
</script>

<template>
  <DropdownMenuItem
    data-slot="dropdown-menu-item"
    :data-inset="inset ? '' : undefined"
    v-bind="forwarded"
    :class="cn(
      'focus:bg-accent focus:text-accent-foreground relative flex cursor-default items-center gap-2 rounded-sm px-2 py-1.5 text-sm outline-hidden select-none data-[disabled]:pointer-events-none data-[disabled]:opacity-50 data-[inset]:pl-8 [&>svg]:size-4 [&>svg]:shrink-0',
      props.class,
    )"
  >
    <slot />
  </DropdownMenuItem>
</template>
```

- [ ] **Step 5: Create `DropdownMenuLabel.vue`**

```vue
<script setup lang="ts">
import type { DropdownMenuLabelProps } from "reka-ui"
import type { HTMLAttributes } from "vue"
import { reactiveOmit } from "@vueuse/core"
import { DropdownMenuLabel, useForwardProps } from "reka-ui"
import { cn } from "@/lib/utils"

const props = defineProps<DropdownMenuLabelProps & { class?: HTMLAttributes["class"], inset?: boolean }>()

const delegatedProps = reactiveOmit(props, "class", "inset")
const forwarded = useForwardProps(delegatedProps)
</script>

<template>
  <DropdownMenuLabel
    data-slot="dropdown-menu-label"
    :data-inset="inset ? '' : undefined"
    v-bind="forwarded"
    :class="cn('px-2 py-1.5 text-sm font-medium data-[inset]:pl-8', props.class)"
  >
    <slot />
  </DropdownMenuLabel>
</template>
```

- [ ] **Step 6: Create `DropdownMenuSeparator.vue`**

```vue
<script setup lang="ts">
import type { DropdownMenuSeparatorProps } from "reka-ui"
import type { HTMLAttributes } from "vue"
import { reactiveOmit } from "@vueuse/core"
import { DropdownMenuSeparator, useForwardProps } from "reka-ui"
import { cn } from "@/lib/utils"

const props = defineProps<DropdownMenuSeparatorProps & { class?: HTMLAttributes["class"] }>()

const delegatedProps = reactiveOmit(props, "class")
const forwarded = useForwardProps(delegatedProps)
</script>

<template>
  <DropdownMenuSeparator
    data-slot="dropdown-menu-separator"
    v-bind="forwarded"
    :class="cn('bg-border -mx-1 my-1 h-px', props.class)"
  />
</template>
```

- [ ] **Step 7: Create `DropdownMenuGroup.vue`**

```vue
<script setup lang="ts">
import type { DropdownMenuGroupProps } from "reka-ui"
import { DropdownMenuGroup, useForwardProps } from "reka-ui"

const props = defineProps<DropdownMenuGroupProps>()

const forwarded = useForwardProps(props)
</script>

<template>
  <DropdownMenuGroup data-slot="dropdown-menu-group" v-bind="forwarded">
    <slot />
  </DropdownMenuGroup>
</template>
```

- [ ] **Step 8: Create `index.ts` barrel**

```ts
export { default as DropdownMenu } from "./DropdownMenu.vue"
export { default as DropdownMenuContent } from "./DropdownMenuContent.vue"
export { default as DropdownMenuGroup } from "./DropdownMenuGroup.vue"
export { default as DropdownMenuItem } from "./DropdownMenuItem.vue"
export { default as DropdownMenuLabel } from "./DropdownMenuLabel.vue"
export { default as DropdownMenuSeparator } from "./DropdownMenuSeparator.vue"
export { default as DropdownMenuTrigger } from "./DropdownMenuTrigger.vue"
```

- [ ] **Step 9: Typecheck**

Run: `cd dashboard && npx vue-tsc -b`
Expected: 0 errors.

- [ ] **Step 10: Commit**

```bash
git add dashboard/src/components/ui/dropdown-menu
git commit -m "feat(ui): vendor shadcn-vue DropdownMenu primitive"
```

---

### Task 2: `UserAvatar` component

**Goal:** A reusable initials avatar (displayName → username → generic-icon fallback), used by the trigger and the menu header.

**Files:**
- Create: `dashboard/src/components/custom/UserAvatar.vue`
- Test: `dashboard/src/components/custom/UserAvatar.test.ts`

**Acceptance Criteria:**
- [ ] Two-word name → two uppercase initials (`"Alex Smith"` → `AS`).
- [ ] One-word name → first two letters (`"Alex"` → `AL`).
- [ ] Empty/whitespace displayName → username initials; both empty → generic icon, no text.

**Verify:** `cd dashboard && npx vitest run src/components/custom/UserAvatar.test.ts` → all pass.

**Steps:**

- [ ] **Step 1: Write the failing test** — `dashboard/src/components/custom/UserAvatar.test.ts`

```ts
import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import UserAvatar from './UserAvatar.vue'

describe('UserAvatar', () => {
  it('derives two initials from a multi-word display name', () => {
    const w = mount(UserAvatar, { props: { displayName: 'Alex Smith' } })
    expect(w.text()).toBe('AS')
  })

  it('uses the first two letters of a single-word display name', () => {
    const w = mount(UserAvatar, { props: { displayName: 'Alex' } })
    expect(w.text()).toBe('AL')
  })

  it('falls back to the username initials when displayName is blank', () => {
    const w = mount(UserAvatar, { props: { displayName: '   ', username: 'bob' } })
    expect(w.text()).toBe('BO')
  })

  it('renders a generic icon (no text) when both are empty', () => {
    const w = mount(UserAvatar, { props: { displayName: '', username: '' } })
    expect(w.text()).toBe('')
    expect(w.find('svg').exists()).toBe(true)
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd dashboard && npx vitest run src/components/custom/UserAvatar.test.ts`
Expected: FAIL — cannot resolve `./UserAvatar.vue`.

- [ ] **Step 3: Implement** — `dashboard/src/components/custom/UserAvatar.vue`

```vue
<script setup lang="ts">
/** UserAvatar — initials box (displayName → username), generic-icon fallback. */
import { computed } from 'vue'
import { User } from 'lucide-vue-next'
import { cn } from '@/lib/utils'

const props = withDefaults(defineProps<{
  displayName?: string | null
  username?: string | null
  size?: 'sm' | 'md'
}>(), { size: 'md' })

const initials = computed(() => {
  const name = (props.displayName ?? '').trim()
  if (name) {
    const parts = name.split(/\s+/).filter(Boolean)
    const chars = parts.length >= 2 ? parts[0][0] + parts[parts.length - 1][0] : parts[0].slice(0, 2)
    return chars.toUpperCase()
  }
  const u = (props.username ?? '').trim()
  if (u) return u.slice(0, 2).toUpperCase()
  return ''
})

const sizeClass = computed(() => (props.size === 'sm' ? 'size-6 text-[0.625rem]' : 'size-8 text-xs'))
</script>

<template>
  <span
    aria-hidden="true"
    :class="cn('inline-flex shrink-0 items-center justify-center rounded-md bg-sidebar-accent font-medium text-sidebar-accent-foreground', sizeClass)"
  >
    <template v-if="initials">{{ initials }}</template>
    <User v-else class="size-4" />
  </span>
</template>
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd dashboard && npx vitest run src/components/custom/UserAvatar.test.ts`
Expected: PASS (4 tests).

- [ ] **Step 5: Commit**

```bash
git add dashboard/src/components/custom/UserAvatar.vue dashboard/src/components/custom/UserAvatar.test.ts
git commit -m "feat(ui): add UserAvatar initials component"
```

---

### Task 3: `EditDisplayNameDialog` + `accountMenu` i18n keys

**Goal:** A dialog that edits `displayName` via `PUT /me`, mirroring server validation, surfacing errors inline.

**Files:**
- Create: `dashboard/src/components/custom/EditDisplayNameDialog.vue`
- Test: `dashboard/src/components/custom/EditDisplayNameDialog.test.ts`
- Modify: `dashboard/src/locales/en.ts` (add `accountMenu` block — additive)

**Acceptance Criteria:**
- [ ] Opening the dialog prefills the input with the current `displayName`.
- [ ] Save is disabled when unchanged, empty, or >128 chars; enabled on a valid change.
- [ ] Save calls `PUT /api/prohibitorum/me` with `{ displayName }`, patches the store from the **response**, and emits `update:open=false`.
- [ ] An `invalid_display_name` error keeps the dialog open, shows the mapped message, and preserves input.

**Verify:** `cd dashboard && npx vitest run src/components/custom/EditDisplayNameDialog.test.ts` → all pass.

**Steps:**

- [ ] **Step 1: Add the `accountMenu` i18n block** — `dashboard/src/locales/en.ts`

Find the `nav` block (it ends with `signOut: 'Sign out',` then `},`) and insert the `accountMenu` block immediately after it. Do NOT type apostrophes in these strings.

Locate:
```ts
  nav: {
    account: 'Account',
    profile: 'Profile',
    security: 'Security',
    sessions: 'Sessions',
    connected: 'Connected',
    devices: 'Devices',
    signOut: 'Sign out',
  },
```
Replace with (note: `profile` stays for now — it is removed in Task 5):
```ts
  nav: {
    account: 'Account',
    profile: 'Profile',
    security: 'Security',
    sessions: 'Sessions',
    connected: 'Connected',
    devices: 'Devices',
    signOut: 'Sign out',
  },
  accountMenu: {
    trigger: 'Account menu',
    editName: 'Edit display name',
    editTitle: 'Edit display name',
    editDescription: 'This name is shown across your account.',
    displayNameLabel: 'Display name',
  },
```

- [ ] **Step 2: Verify no curly-quote corruption in en.ts**

Run: `cd dashboard && grep -nP "[\x{2018}\x{2019}]" src/locales/en.ts || echo "clean"`
Expected: `clean` (no curly apostrophes). If any appear, fix them back to straight `'`.

- [ ] **Step 3: Write the failing test** — `dashboard/src/components/custom/EditDisplayNameDialog.test.ts`

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { createPinia, setActivePinia } from 'pinia'
import en from '@/locales/en'
import EditDisplayNameDialog from './EditDisplayNameDialog.vue'
import { useAuthStore } from '@/stores/auth'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'
const put = vi.mocked(api.put)

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })

function seedUser(displayName = 'Alex Smith') {
  const auth = useAuthStore()
  auth.me = { id: 1, username: 'alex', displayName, role: 'user' }
  return auth
}

function mountOpen() {
  return mount(EditDisplayNameDialog, {
    props: { open: true }, attachTo: document.body, global: { plugins: [i18n()] },
  })
}

function input() {
  return document.body.querySelector('[data-test="edit-displayname-input"]') as HTMLInputElement
}
function setInput(v: string) {
  const el = input(); el.value = v; el.dispatchEvent(new Event('input'))
}
function saveBtn() {
  return document.body.querySelector('[data-test="edit-save"]') as HTMLButtonElement
}

beforeEach(() => { setActivePinia(createPinia()); put.mockReset(); document.body.innerHTML = '' })

describe('EditDisplayNameDialog', () => {
  it('prefills the input with the current displayName', async () => {
    seedUser('Alex Smith'); mountOpen(); await flushPromises()
    expect(input().value).toBe('Alex Smith')
  })

  it('disables Save when unchanged, empty, or too long; enables on a valid change', async () => {
    seedUser('Alex Smith'); mountOpen(); await flushPromises()
    expect(saveBtn().disabled).toBe(true)            // unchanged
    setInput(''); await flushPromises()
    expect(saveBtn().disabled).toBe(true)            // empty
    setInput('x'.repeat(129)); await flushPromises()
    expect(saveBtn().disabled).toBe(true)            // >128
    setInput('Alexander'); await flushPromises()
    expect(saveBtn().disabled).toBe(false)           // valid change
  })

  it('saves: PUT /me, patches store from RESPONSE, emits close', async () => {
    const auth = seedUser('Alex Smith')
    put.mockResolvedValue({ id: 1, username: 'alex', displayName: 'ALEXANDER', role: 'user' })
    const w = mountOpen(); await flushPromises()
    setInput('Alexander'); await flushPromises()
    saveBtn().click(); await flushPromises()
    expect(put).toHaveBeenCalledWith('/api/prohibitorum/me', { displayName: 'Alexander' })
    expect(auth.me?.displayName).toBe('ALEXANDER')
    expect(w.emitted('update:open')?.some((e) => e[0] === false)).toBe(true)
  })

  it('keeps the dialog open and shows the mapped error on invalid_display_name', async () => {
    seedUser('Alex Smith')
    put.mockRejectedValue({ code: 'invalid_display_name', message: 'zh' })
    const w = mountOpen(); await flushPromises()
    setInput('Bad'); await flushPromises()
    saveBtn().click(); await flushPromises()
    expect(document.body.textContent).toContain(en.errors.invalid_display_name)
    expect(input().value).toBe('Bad')
    expect(w.emitted('update:open')?.some((e) => e[0] === false)).toBe(false)
  })
})
```

- [ ] **Step 4: Run test to verify it fails**

Run: `cd dashboard && npx vitest run src/components/custom/EditDisplayNameDialog.test.ts`
Expected: FAIL — cannot resolve `./EditDisplayNameDialog.vue`.

- [ ] **Step 5: Implement** — `dashboard/src/components/custom/EditDisplayNameDialog.vue`

```vue
<script setup lang="ts">
/**
 * EditDisplayNameDialog — edits the current account's displayName via PUT /me.
 * Client validation mirrors the server (1–128, no control chars, NO trim) for
 * the disabled state; the server stays the source of truth and its error
 * surfaces inline. Sudo-free (PUT /me self-edit).
 */
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { useAuthStore } from '@/stores/auth'
import type { SessionView } from '@/stores/auth'
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter,
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Alert, AlertDescription } from '@/components/ui/alert'

const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ 'update:open': [boolean] }>()

const { t, te } = useI18n()
const auth = useAuthStore()
const { busy, error, run } = useApi()

const draft = ref('')

// Reset the draft to the current value each time the dialog opens — no stale carry-over.
watch(() => props.open, (o) => {
  if (o) { draft.value = auth.me?.displayName ?? ''; error.value = null }
})

const hasControlChar = (s: string) =>
  [...s].some((c) => { const n = c.codePointAt(0) ?? 0; return n < 0x20 || n === 0x7f })

const valid = computed(() => {
  const v = draft.value
  return v.length >= 1 && v.length <= 128 && !hasControlChar(v)
})
const dirty = computed(() => draft.value !== (auth.me?.displayName ?? ''))
const canSave = computed(() => valid.value && dirty.value && !busy.value)

const errorText = computed(() => {
  const e = error.value
  if (!e) return ''
  const key = `errors.${e.code}`
  return te(key) ? t(key) : e.message || t('common.error')
})

function onOpenChange(v: boolean): void { emit('update:open', v) }

async function save(): Promise<void> {
  if (!canSave.value) return
  const result = await run(() =>
    api.put<SessionView>('/api/prohibitorum/me', { displayName: draft.value }),
  )
  if (result) {
    auth.setDisplayName(result.displayName)
    emit('update:open', false)
  }
}
</script>

<template>
  <Dialog :open="open" @update:open="onOpenChange">
    <DialogContent class="sm:max-w-md">
      <DialogHeader>
        <DialogTitle>{{ t('accountMenu.editTitle') }}</DialogTitle>
        <DialogDescription>{{ t('accountMenu.editDescription') }}</DialogDescription>
      </DialogHeader>
      <form class="flex flex-col gap-3" @submit.prevent="save">
        <div class="flex flex-col gap-1.5">
          <Label for="edit-displayName">{{ t('accountMenu.displayNameLabel') }}</Label>
          <Input
            id="edit-displayName"
            v-model="draft"
            data-test="edit-displayname-input"
            :maxlength="128"
            autofocus
          />
        </div>
        <Alert v-if="errorText" variant="destructive" role="alert" aria-live="polite">
          <AlertDescription>{{ errorText }}</AlertDescription>
        </Alert>
        <DialogFooter class="gap-2">
          <Button type="button" variant="ghost" :disabled="busy" data-test="edit-cancel" @click="onOpenChange(false)">
            {{ t('common.cancel') }}
          </Button>
          <Button type="submit" :disabled="!canSave" data-test="edit-save">
            {{ t('common.save') }}
          </Button>
        </DialogFooter>
      </form>
    </DialogContent>
  </Dialog>
</template>
```

- [ ] **Step 6: Run test to verify it passes**

Run: `cd dashboard && npx vitest run src/components/custom/EditDisplayNameDialog.test.ts`
Expected: PASS (4 tests).

- [ ] **Step 7: Commit**

```bash
git add dashboard/src/components/custom/EditDisplayNameDialog.vue dashboard/src/components/custom/EditDisplayNameDialog.test.ts dashboard/src/locales/en.ts
git commit -m "feat(account): add EditDisplayNameDialog + accountMenu i18n keys"
```

---

### Task 4: `NavUser` sidebar account control

**Goal:** Footer dropdown over `DropdownMenu`: identity header, "Edit display name…" (opens dialog), "Sign out" (→ `/logout`); skeleton while loading; sibling edit dialog opened on `nextTick`.

**Files:**
- Create: `dashboard/src/components/custom/NavUser.vue`
- Test: `dashboard/src/components/custom/NavUser.test.ts`

**Acceptance Criteria:**
- [ ] Renders a skeleton (no trigger) while `auth.me` is null; renders the trigger with displayName, role, and initials when loaded.
- [ ] `signOut()` navigates to `/logout`.
- [ ] `openEdit()` opens the edit dialog (input appears in the DOM) after `nextTick`.

**Verify:** `cd dashboard && npx vitest run src/components/custom/NavUser.test.ts` → all pass.

**Steps:**

- [ ] **Step 1: Write the failing test** — `dashboard/src/components/custom/NavUser.test.ts`

> The dropdown's `@select` handlers are exposed via `defineExpose` and tested directly — this is deterministic in jsdom and does not depend on Reka's pointer-driven menu-open behavior. The trivial `@select="openEdit"` / `@select="signOut"` template wiring is verified live in Task 7.

```ts
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent, nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory, type Router } from 'vue-router'
import en from '@/locales/en'
import NavUser from './NavUser.vue'
import { SidebarProvider } from '@/components/ui/sidebar'
import { useAuthStore } from '@/stores/auth'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))

if (!window.matchMedia) {
  // @ts-expect-error jsdom lacks matchMedia
  window.matchMedia = () => ({ matches: false, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {} })
}

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
const stub = defineComponent({ template: '<div/>' })
function makeRouter(): Router {
  return createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/', component: stub }, { path: '/security', component: stub }, { path: '/logout', component: stub }],
  })
}
const Host = defineComponent({
  components: { SidebarProvider, NavUser },
  template: '<SidebarProvider><NavUser ref="nav" /></SidebarProvider>',
})

beforeEach(() => { setActivePinia(createPinia()); document.body.innerHTML = '' })

async function mountHost(router: Router) {
  router.push('/security'); await router.isReady()
  const w = mount(Host, { attachTo: document.body, global: { plugins: [router, i18n()] } })
  await flushPromises()
  return w
}

describe('NavUser', () => {
  it('shows a skeleton (no trigger) while the session is loading', async () => {
    const w = await mountHost(makeRouter()) // auth.me is null
    expect(w.find('[data-test="account-trigger"]').exists()).toBe(false)
  })

  it('renders displayName, role, and initials in the trigger when loaded', async () => {
    const auth = useAuthStore()
    auth.me = { id: 1, username: 'alex', displayName: 'Alex Smith', role: 'user' }
    const w = await mountHost(makeRouter())
    expect(w.find('[data-test="account-trigger"]').exists()).toBe(true)
    expect(w.text()).toContain('Alex Smith')
    expect(w.text()).toContain('user')
    expect(w.text()).toContain('AS')
  })

  it('signOut navigates to /logout', async () => {
    const auth = useAuthStore()
    auth.me = { id: 1, username: 'alex', displayName: 'Alex Smith', role: 'user' }
    const router = makeRouter()
    const push = vi.spyOn(router, 'push')
    const w = await mountHost(router)
    const nav = (w.vm.$refs as Record<string, { signOut: () => void }>).nav
    nav.signOut()
    expect(push).toHaveBeenCalledWith('/logout')
  })

  it('openEdit opens the edit dialog after nextTick', async () => {
    const auth = useAuthStore()
    auth.me = { id: 1, username: 'alex', displayName: 'Alex Smith', role: 'user' }
    const w = await mountHost(makeRouter())
    const nav = (w.vm.$refs as Record<string, { openEdit: () => void }>).nav
    nav.openEdit()
    await nextTick(); await flushPromises()
    expect(document.body.querySelector('[data-test="edit-displayname-input"]')).not.toBeNull()
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd dashboard && npx vitest run src/components/custom/NavUser.test.ts`
Expected: FAIL — cannot resolve `./NavUser.vue`.

- [ ] **Step 3: Implement** — `dashboard/src/components/custom/NavUser.vue`

```vue
<script setup lang="ts">
/**
 * NavUser — sidebar footer account control. A dropdown over the vendored
 * DropdownMenu: identity header, edit-display-name, sign out. The edit dialog
 * is a SIBLING (not nested in the menu) and opens on nextTick after select,
 * avoiding Reka's menu→dialog focus / lingering-pointer-events bug.
 */
import { nextTick, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { ChevronsUpDown, LogOut, Pencil } from 'lucide-vue-next'
import { useAuthStore } from '@/stores/auth'
import {
  SidebarMenu, SidebarMenuItem, SidebarMenuButton, useSidebar,
} from '@/components/ui/sidebar'
import { Skeleton } from '@/components/ui/skeleton'
import {
  DropdownMenu, DropdownMenuTrigger, DropdownMenuContent,
  DropdownMenuLabel, DropdownMenuItem, DropdownMenuSeparator,
} from '@/components/ui/dropdown-menu'
import StatusBadge from '@/components/custom/StatusBadge.vue'
import UserAvatar from '@/components/custom/UserAvatar.vue'
import EditDisplayNameDialog from '@/components/custom/EditDisplayNameDialog.vue'

const { t } = useI18n()
const auth = useAuthStore()
const router = useRouter()
const { isMobile } = useSidebar()

const editOpen = ref(false)

// Open the dialog on the next tick so the menu finishes closing / restoring
// focus to the trigger first — prevents Reka's lingering pointer-events:none.
function openEdit(): void { void nextTick(() => { editOpen.value = true }) }
function signOut(): void { void router.push('/logout') }

defineExpose({ openEdit, signOut, editOpen })
</script>

<template>
  <SidebarMenu>
    <SidebarMenuItem>
      <div v-if="!auth.me" class="flex items-center gap-2 p-2">
        <Skeleton class="size-8 rounded-md" />
        <div class="flex flex-1 flex-col gap-1">
          <Skeleton class="h-3.5 w-24" />
          <Skeleton class="h-3 w-12" />
        </div>
      </div>

      <DropdownMenu v-else>
        <DropdownMenuTrigger as-child>
          <SidebarMenuButton
            size="lg"
            data-test="account-trigger"
            :aria-label="t('accountMenu.trigger')"
            class="data-[state=open]:bg-sidebar-accent data-[state=open]:text-sidebar-accent-foreground"
          >
            <UserAvatar :display-name="auth.me.displayName" :username="auth.me.username" />
            <div class="grid flex-1 text-left text-sm leading-tight">
              <span class="truncate font-medium text-ink">{{ auth.me.displayName }}</span>
              <span class="truncate text-xs capitalize text-muted">{{ auth.me.role }}</span>
            </div>
            <ChevronsUpDown class="ml-auto size-4 text-muted" />
          </SidebarMenuButton>
        </DropdownMenuTrigger>

        <DropdownMenuContent
          class="min-w-56"
          :side="isMobile ? 'bottom' : 'right'"
          align="end"
          :side-offset="4"
        >
          <DropdownMenuLabel class="font-normal">
            <div class="flex items-center gap-2">
              <UserAvatar :display-name="auth.me.displayName" :username="auth.me.username" />
              <div class="grid flex-1 text-left text-sm leading-tight">
                <span class="truncate font-medium text-ink">{{ auth.me.displayName }}</span>
                <span class="truncate text-xs text-muted">@{{ auth.me.username }}</span>
              </div>
              <StatusBadge :variant="auth.me.role === 'admin' ? 'caution' : 'neutral'" class="capitalize">
                {{ auth.me.role }}
              </StatusBadge>
            </div>
          </DropdownMenuLabel>
          <DropdownMenuSeparator />
          <DropdownMenuItem data-test="account-edit" @select="openEdit">
            <Pencil />
            <span>{{ t('accountMenu.editName') }}</span>
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem data-test="account-signout" @select="signOut">
            <LogOut />
            <span>{{ t('nav.signOut') }}</span>
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </SidebarMenuItem>
  </SidebarMenu>

  <EditDisplayNameDialog v-model:open="editOpen" />
</template>
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd dashboard && npx vitest run src/components/custom/NavUser.test.ts`
Expected: PASS (4 tests).

- [ ] **Step 5: Commit**

```bash
git add dashboard/src/components/custom/NavUser.vue dashboard/src/components/custom/NavUser.test.ts
git commit -m "feat(account): add NavUser sidebar dropdown control"
```

---

### Task 5: Wire `AppSidebar` to `NavUser`; drop the Profile nav item

**Goal:** Replace the static footer (identity text + standalone Sign out link) with `<NavUser />`, remove the Profile nav link, and update the sidebar test.

**Files:**
- Modify: `dashboard/src/components/custom/AppSidebar.vue`
- Modify: `dashboard/src/components/custom/AppSidebar.test.ts`
- Modify: `dashboard/src/locales/en.ts` (remove `nav.profile`)

**Acceptance Criteria:**
- [ ] Account nav no longer contains a `/` (Profile) link; first item is Security.
- [ ] Footer renders the `NavUser` trigger (shows displayName); no standalone `/logout` anchor.
- [ ] `AppSidebar.test.ts` passes with updated assertions.

**Verify:** `cd dashboard && npx vitest run src/components/custom/AppSidebar.test.ts` → all pass.

**Steps:**

- [ ] **Step 1: Update the icon imports** in `AppSidebar.vue` (line 12). Remove `User` and `LogOut` (now unused — `vue-tsc -b` enforces `noUnusedLocals`):

Replace:
```ts
import { ShieldCheck, User, MonitorSmartphone, LogOut, KeyRound, Link2, TabletSmartphone, Users, Ticket, AppWindow, Building2, Network, KeySquare, ScrollText } from 'lucide-vue-next'
```
with:
```ts
import { ShieldCheck, MonitorSmartphone, KeyRound, Link2, TabletSmartphone, Users, Ticket, AppWindow, Building2, Network, KeySquare, ScrollText } from 'lucide-vue-next'
import NavUser from '@/components/custom/NavUser.vue'
```

- [ ] **Step 2: Remove the Profile entry from `accountItems`** (lines 27-33):

Replace:
```ts
const accountItems = computed(() => [
  { to: '/', label: t('nav.profile'), icon: User },
  { to: '/security', label: t('nav.security'), icon: KeyRound },
  { to: '/sessions', label: t('nav.sessions'), icon: MonitorSmartphone },
  { to: '/connected', label: t('nav.connected'), icon: Link2 },
  { to: '/devices', label: t('nav.devices'), icon: TabletSmartphone },
])
```
with:
```ts
const accountItems = computed(() => [
  { to: '/security', label: t('nav.security'), icon: KeyRound },
  { to: '/sessions', label: t('nav.sessions'), icon: MonitorSmartphone },
  { to: '/connected', label: t('nav.connected'), icon: Link2 },
  { to: '/devices', label: t('nav.devices'), icon: TabletSmartphone },
])
```

- [ ] **Step 3: Replace the `SidebarFooter` block** (lines 126-143):

Replace:
```vue
    <SidebarFooter>
      <div class="flex flex-col gap-1 border-t border-sidebar-border pt-2">
        <div v-if="auth.me" class="flex min-w-0 flex-col px-2 py-1">
          <span class="truncate text-sm font-medium text-ink">{{ auth.me.displayName }}</span>
          <span class="truncate text-xs capitalize text-muted">{{ auth.me.role }}</span>
        </div>
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
```
with:
```vue
    <SidebarFooter class="border-t border-sidebar-border">
      <NavUser />
    </SidebarFooter>
```

> `auth` (used by `v-if="auth.isAdmin"`) and `SidebarMenu`/`SidebarMenuItem`/`SidebarMenuButton` (still used by the nav groups) remain imported — do not remove them.

- [ ] **Step 4: Remove `nav.profile`** from `dashboard/src/locales/en.ts`:

Replace:
```ts
  nav: {
    account: 'Account',
    profile: 'Profile',
    security: 'Security',
```
with:
```ts
  nav: {
    account: 'Account',
    security: 'Security',
```

- [ ] **Step 5: Update `AppSidebar.test.ts`** — replace the first two tests (lines 34-56):

Replace the body of `it('renders the built Account links and a footer sign-out', ...)` and `it('marks only the current route link as active', ...)` with:

```ts
  it('renders the built Account links and the account control (no Profile, no footer sign-out link)', async () => {
    const auth = useAuthStore()
    auth.me = { id: 1, username: 'alex', displayName: 'Alex Smith', role: 'user' }
    const router = makeRouter(); router.push('/security'); await router.isReady()
    const wrapper = mount(Host, { global: { plugins: [router, makeI18n()], components: { AppSidebar } } })
    const links = wrapper.findAll('a').map((a) => a.attributes('href'))
    expect(links).toContain('/security')
    expect(links).toContain('/sessions')
    expect(links).toContain('/connected')
    expect(links).toContain('/devices')
    expect(links).not.toContain('/')        // Profile link removed
    expect(links).not.toContain('/logout')  // sign-out is now inside the account menu
    expect(wrapper.text()).toContain('Alex Smith') // NavUser trigger shows displayName
  })

  it('marks only the current route link as active', async () => {
    const auth = useAuthStore()
    auth.me = { id: 1, username: 'alex', displayName: 'Alex Smith', role: 'user' }
    const router = makeRouter(); router.push('/security'); await router.isReady()
    const wrapper = mount(Host, { global: { plugins: [router, makeI18n()], components: { AppSidebar } } })
    // Exactly one element should carry data-active="true" — the Security nav item
    const activeEls = wrapper.findAll('[data-active="true"]')
    expect(activeEls.length).toBe(1)
  })
```

> The two admin tests (lines 58-78) keep `router.push('/')` — unchanged; the local test router still has a `/` route and they assert only admin-link presence. The `matchMedia` polyfill at the top of the file already covers `NavUser`/`SidebarProvider`.

- [ ] **Step 6: Run the test**

Run: `cd dashboard && npx vitest run src/components/custom/AppSidebar.test.ts`
Expected: PASS (4 tests).

- [ ] **Step 7: Verify no curly-quote corruption in en.ts**

Run: `cd dashboard && grep -nP "[\x{2018}\x{2019}]" src/locales/en.ts || echo "clean"`
Expected: `clean`.

- [ ] **Step 8: Commit**

```bash
git add dashboard/src/components/custom/AppSidebar.vue dashboard/src/components/custom/AppSidebar.test.ts dashboard/src/locales/en.ts
git commit -m "feat(account): mount NavUser in sidebar footer; drop Profile nav link"
```

---

### Task 6: Make Security the default page; remove the Profile page

**Goal:** Redirect `/` → `/security`, delete `ProfileView` (+ test), and prune the now-dead `profile` i18n block.

**Files:**
- Modify: `dashboard/src/router/index.ts`
- Delete: `dashboard/src/pages/ProfileView.vue`
- Delete: `dashboard/src/pages/ProfileView.test.ts`
- Modify: `dashboard/src/locales/en.ts` (remove the `profile` block)

**Acceptance Criteria:**
- [ ] `/` redirects to the `security` route; `ProfileView` no longer exists or is referenced.
- [ ] The `profile` i18n block is removed; `vue-tsc -b` reports 0 errors (no dangling references).

**Verify:** `cd dashboard && npx vue-tsc -b && npx vitest run` → 0 type errors, all tests pass.

**Steps:**

- [ ] **Step 1: Delete the Profile page and its test**

```bash
git rm dashboard/src/pages/ProfileView.vue dashboard/src/pages/ProfileView.test.ts
```

- [ ] **Step 2: Update the router index child** — `dashboard/src/router/index.ts` (line 80):

Replace:
```ts
      { path: '', name: 'profile', component: () => import('../pages/ProfileView.vue') },
```
with:
```ts
      { path: '', redirect: { name: 'security' } },
```

> The lazy `import('../pages/ProfileView.vue')` lived only on this line, so removing it drops the only reference. `returnTo` (defaults to `/`) and `PairDeviceView`'s `router.push('/')` now flow through this redirect to `/security` — intentionally left untouched.

- [ ] **Step 3: Remove the dead `profile` i18n block** — `dashboard/src/locales/en.ts`:

Delete the entire block:
```ts
  profile: {
    title: 'Profile',
    username: 'Username',
    displayName: 'Display name',
    role: 'Role',
    edit: 'Edit',
    save: 'Save',
    cancel: 'Cancel',
  },
```

(Confirmed: after ProfileView is deleted, nothing references `profile.*` — the dialog uses `accountMenu.*` + `common.*`.)

- [ ] **Step 4: Typecheck (catches any dangling i18n/route reference)**

Run: `cd dashboard && npx vue-tsc -b`
Expected: 0 errors. (If a `profile.*` or `name: 'profile'` reference were missed, this fails here.)

- [ ] **Step 5: Run the full FE test suite**

Run: `cd dashboard && npx vitest run`
Expected: all tests pass (ProfileView.test.ts is gone; the rest are green).

- [ ] **Step 6: Verify no curly-quote corruption in en.ts**

Run: `cd dashboard && grep -nP "[\x{2018}\x{2019}]" src/locales/en.ts || echo "clean"`
Expected: `clean`.

- [ ] **Step 7: Commit**

```bash
git add dashboard/src/router/index.ts dashboard/src/locales/en.ts
git commit -m "feat(dashboard): default to /security; remove Profile page"
```

---

### Task 7: Done-gate — full verification, rebuild + commit `dist`

**Goal:** Prove the whole change green across FE + Go gates, regenerate the embedded SPA, and verify the live click-paths the unit tests deferred.

**Files:**
- Modify (generated): `pkg/webui/dist/**` (rebuilt by `vite build`)

**Acceptance Criteria:**
- [ ] `npx vitest run` — all green; `vue-tsc -b` — 0 errors.
- [ ] `go build ./... && go vet ./... && go test ./...` — 0 failures.
- [ ] Smoke `SMOKE_EXIT=0` (backend invariant; this FE-only change cannot affect the API-level smoke).
- [ ] `pkg/webui/dist` rebuilt and committed.
- [ ] Live: account dropdown opens; "Edit display name…" opens the dialog and a save updates the trigger; "Sign out" logs out; visiting `/` lands on Security.

**Verify:** commands below, each with its expected result.

**Steps:**

- [ ] **Step 1: Frontend tests + typecheck-build (regenerates `pkg/webui/dist`)**

Run: `cd dashboard && npm run test && npm run build`
Expected: vitest all pass; `vue-tsc -b` 0 errors; `vite build` writes `../pkg/webui/dist`.

- [ ] **Step 2: Go gate**

Run: `cd /home/tundra/projects/tundra/prohibitorum && go build ./... && go vet ./... && go test ./...`
Expected: 0 failures (no Go files changed).

- [ ] **Step 3: Smoke**

Run the smoke per the project runbook in `docs/superpowers/notes/2026-06-09-tier1-self-service-admin-reads-DONE-handoff.md` (detached runner → poll for `SMOKE_EXIT=`; requires `. ./scripts/dev-env.sh`-style env + `PROHIBITORUM_FEDERATION_ALLOW_PRIVATE_NETWORK=true`; never bare `pkill -f prohibitorum`).
Expected: `SMOKE_EXIT=0`. (FE-only change; the smoke drives `/api/prohibitorum/*` ceremonies, not the SPA, so this is a regression guard.)

- [ ] **Step 4: Live verification** (`mise dev-server`, then `mise enroll-admin -- --new`, open `http://localhost:8080`)

Confirm in the browser:
- Visiting `/` lands on the Security page.
- The bottom-left account control opens a dropdown (identity header, Edit, Sign out).
- "Edit display name…" opens the dialog; saving a new name updates the trigger immediately; an empty/unchanged name disables Save.
- "Sign out" ends the session.
- Collapse the sidebar (toggle): the trigger becomes icon-only with a name tooltip.

- [ ] **Step 5: Commit the rebuilt dist**

```bash
git add pkg/webui/dist
git commit -m "build(webui): rebuild embedded SPA for account dropdown + Security default"
```

---

## Self-Review

**Spec coverage:**
- Vendored `DropdownMenu` → Task 1. `UserAvatar` → Task 2. `EditDisplayNameDialog` (validation, error, store patch) → Task 3. `NavUser` (skeleton, trigger, dropdown, sign-out, edit handoff, collapsed/mobile side) → Task 4. `AppSidebar` rewire + drop Profile item → Task 5. Routing redirect + delete ProfileView + i18n prune → Task 6. Done-gate (FE/Go/smoke/dist + live) → Task 7. All spec sections map to a task.
- i18n: `accountMenu` added (Task 3); `nav.profile` removed (Task 5); `profile` block removed (Task 6); reuse of `nav.signOut`/`common.save`/`common.cancel`/`errors.invalid_display_name` confirmed. Apostrophe grep after every en.ts edit.

**Type consistency:** `auth.me` / `SessionView` (`id,username,displayName,role`), `auth.setDisplayName(name)`, `useApi().{busy,error,run}`, `api.put<SessionView>`, `useSidebar().isMobile`, `DropdownMenuItem` `@select`, `EditDisplayNameDialog` `v-model:open`, `NavUser` `defineExpose({openEdit,signOut,editOpen})` — all consistent across tasks.

**Placeholder scan:** none — every step has full code or an exact old→new edit and a concrete command + expected output.

**Risk note (honest):** Task 4's automated coverage tests the `@select` handlers via `defineExpose` rather than driving Reka's pointer-based menu-open in jsdom (which is brittle there). The one-token `@select` template wiring is verified live in Task 7, Step 4.
