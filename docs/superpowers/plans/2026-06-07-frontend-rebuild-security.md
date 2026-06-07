# Frontend Rebuild — Spec 2b (Security) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers-extended-cc:subagent-driven-development (recommended) or superpowers-extended-cc:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the `/security` page and its four cards (Passkeys, Password, TOTP, Recovery codes) on shadcn-vue, behind the Spec-2a sudo gate, with reusable building blocks (`CodeField`, `ConfirmDialog`, `TotpQr`, `RecoveryCodesDisplay`) to a research-backed quality bar.

**Architecture:** A `/security` child route under `DashboardLayout` (`requiresAuth`) renders `SecurityView`, which stacks four cards plus a bottom destructive "remove password & authenticator" action. Sensitive calls are wrapped in `withSudo()` (no-op unless the server returns `sudo_required`). New presentational components live in `components/custom/`; cards in `pages/security/`. Frontend-only — every `/me/*` endpoint already exists.

**Tech Stack:** Vue 3.5 + TS strict, Tailwind v4 tokens, shadcn-vue/Reka UI (vendored `dialog`/`button`/`input`/`label`/`card`/`alert` primitives), the `qrcode` package (QR as a `data:` PNG), `@simplewebauthn/browser` via `lib/webauthn`, vitest + `@vue/test-utils`.

**Spec:** `docs/superpowers/specs/2026-06-07-frontend-rebuild-security-design.md`

**Conventions:**
- Commit directly to `master` (no remote, no worktree). Run frontend tooling from `dashboard/` (npm via mise — prefix `mise exec --` if npm isn't on PATH).
- After a verify build, restore the non-deterministic dist for source-only commits: `git checkout -- pkg/webui/dist && git clean -fq pkg/webui/dist` (from repo root). Rebuild + commit dist once in the final task.
- Go gate authoritative: `mise exec -- go build ./... && go vet ./...` exit 0.
- Smoke: `setsid bash /tmp/run_v06.sh`, poll `/tmp/v06.result` for `SMOKE_EXIT=0`. Never bare `pkill -f 'prohibitorum'`.
- **Quality bar (review lesson):** badges/labels never wrap (`whitespace-nowrap`); long nicknames/UA-like text truncates (`min-w-0 truncate`); aligned spacing.
- **Existing code to reuse (do NOT recreate):** `@/lib/api` (`api.{get,post}`, throws `{code,message}`, 204→undefined, `ApiError`); `@/composables/useApi` (`{busy,error,run}`); `@/composables/useWebauthn` (`{busy,error,authenticate,register}`; `register(options)`→`RegistrationResponseJSON|undefined`, silent on cancel); `@/lib/sudo` (`withSudo`, `ensureSudo`, `sudoState`, `_resolveSudo`); `@/components/custom/StatusBadge.vue` (`variant` neutral|success|caution|danger, `class` prop, has `whitespace-nowrap`); `@/components/ui/{dialog,button,input,label,card,alert}`; `@/lib/utils` `cn`. Error idiom: ``const key = `errors.${e.code}`; te(key) ? t(key) : e.message || t('common.error')``.

---

### Task 1: Reusable primitives — `CodeField`, `ConfirmDialog`, `TotpQr` (+ `qrcode` dep)

**Goal:** Three small, independently-tested building blocks the cards compose: a copy-to-clipboard mono field, a destructive-confirmation dialog, and a QR renderer.

**Files:**
- Create: `dashboard/src/components/custom/CodeField.vue`, `.../ConfirmDialog.vue`, `.../TotpQr.vue`
- Create: `dashboard/src/components/custom/CodeField.test.ts`, `.../ConfirmDialog.test.ts`, `.../TotpQr.test.ts`
- Modify: `dashboard/package.json` (add `qrcode` + `@types/qrcode`)
- Modify: `dashboard/src/locales/en.ts` (add `common.copy`, `common.copied`, `confirm.cancel`)

**Acceptance Criteria:**
- [ ] `CodeField` renders `value` in mono and copies it via `navigator.clipboard.writeText`, showing a transient "Copied" state.
- [ ] `ConfirmDialog` (v-model `open`) renders `title` + consequences slot + a **destructive** confirm button (`confirmLabel`) and a Cancel that receives initial focus; emits `confirm`/`cancel`; closing via the Dialog emits `cancel`.
- [ ] `TotpQr` renders an `<img>` with a non-empty `data:` `src` and descriptive `alt` from `QRCode.toDataURL(uri)`; renders nothing if generation fails.
- [ ] `cd dashboard && npm run test` passes (existing + new).

**Verify:** `cd dashboard && npm run test` → PASS; `npm run build` → exit 0.

**Steps:**

- [ ] **Step 1: Add the QR dependency.**
```bash
cd dashboard && mise exec -- npm install qrcode && mise exec -- npm install -D @types/qrcode
```
Commit `package.json` + `package-lock.json` with this task's commit.

- [ ] **Step 2: i18n keys** — add to `dashboard/src/locales/en.ts`: in `common` add `copy: 'Copy'` and `copied: 'Copied'`; add a new namespace after `sudo`:
```ts
  confirm: {
    cancel: 'Cancel',
  },
```

- [ ] **Step 3: `CodeField.vue`.**
```vue
<script setup lang="ts">
/** CodeField — a monospace value with a copy-to-clipboard button. */
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Copy, Check } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'

const props = defineProps<{ value: string; label?: string }>()
const { t } = useI18n()
const copied = ref(false)

async function copy(): Promise<void> {
  try {
    await navigator.clipboard.writeText(props.value)
    copied.value = true
    setTimeout(() => { copied.value = false }, 1500)
  } catch {
    /* clipboard blocked — no-op; value is visible for manual copy */
  }
}
</script>

<template>
  <div class="flex flex-col gap-1">
    <span v-if="label" class="text-xs text-muted">{{ label }}</span>
    <div class="flex items-center gap-2 rounded-md border border-border bg-sunken px-3 py-2">
      <code class="min-w-0 flex-1 truncate font-mono text-sm text-ink">{{ value }}</code>
      <Button type="button" variant="ghost" size="sm" class="shrink-0" :aria-label="t('common.copy')" @click="copy">
        <component :is="copied ? Check : Copy" class="size-4" aria-hidden="true" />
        <span>{{ copied ? t('common.copied') : t('common.copy') }}</span>
      </Button>
    </div>
  </div>
</template>
```

- [ ] **Step 4: `ConfirmDialog.vue`.**
```vue
<script setup lang="ts">
/**
 * ConfirmDialog — reusable destructive confirmation over the vendored Dialog
 * primitive. Restates the action (title) + itemized consequences (default
 * slot), a red descriptive confirm button, and a Cancel that gets initial
 * focus and is spatially separated. Closing the dialog = cancel.
 */
import { nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter,
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'

const props = defineProps<{ open: boolean; title: string; confirmLabel: string; busy?: boolean }>()
const emit = defineEmits<{ 'update:open': [boolean]; confirm: []; cancel: [] }>()

const { t } = useI18n()
const cancelRef = ref<{ $el?: HTMLElement }>()

function onCancel(): void {
  emit('update:open', false)
  emit('cancel')
}

// Closing via the Dialog (X / Esc / overlay) routes through here too.
function onOpenChange(v: boolean): void {
  emit('update:open', v)
  if (!v) emit('cancel')
}

// Give Cancel initial focus when the dialog opens (safe default per NN/g).
watch(() => props.open, async (o) => {
  if (o) { await nextTick(); cancelRef.value?.$el?.focus() }
})
</script>

<template>
  <Dialog :open="open" @update:open="onOpenChange">
    <DialogContent>
      <DialogHeader>
        <DialogTitle>{{ title }}</DialogTitle>
      </DialogHeader>
      <div class="text-sm text-ink"><slot /></div>
      <DialogFooter class="gap-2">
        <Button ref="cancelRef" type="button" variant="ghost" :disabled="busy" @click="onCancel">
          {{ t('confirm.cancel') }}
        </Button>
        <Button type="button" variant="destructive" :disabled="busy" @click="emit('confirm')">
          {{ confirmLabel }}
        </Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>
</template>
```

- [ ] **Step 5: `TotpQr.vue`.**
```vue
<script setup lang="ts">
/** TotpQr — renders an otpauth URI as a scannable QR (data: PNG img). */
import { ref, watchEffect } from 'vue'
import QRCode from 'qrcode'

const props = defineProps<{ uri: string; alt: string }>()
const src = ref('')

watchEffect(async () => {
  if (!props.uri) { src.value = ''; return }
  try {
    src.value = await QRCode.toDataURL(props.uri, { width: 200, margin: 1 })
  } catch {
    src.value = ''
  }
})
</script>

<template>
  <img v-if="src" :src="src" :alt="alt" width="200" height="200"
       class="rounded-md border border-border bg-bg p-2" />
</template>
```

- [ ] **Step 6: tests.** `CodeField.test.ts`:
```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import CodeField from './CodeField.vue'

const writeText = vi.fn(async () => {})
beforeEach(() => {
  writeText.mockClear()
  Object.assign(navigator, { clipboard: { writeText } })
})
const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })

describe('CodeField', () => {
  it('renders the value and copies it', async () => {
    const w = mount(CodeField, { props: { value: 'ABC123' }, global: { plugins: [i18n()] } })
    expect(w.find('code').text()).toBe('ABC123')
    await w.find('button').trigger('click')
    expect(writeText).toHaveBeenCalledWith('ABC123')
  })
})
```
`ConfirmDialog.test.ts`:
```ts
import { describe, it, expect, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import ConfirmDialog from './ConfirmDialog.vue'

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })

describe('ConfirmDialog', () => {
  it('emits confirm and cancel; confirm button is destructive', async () => {
    const w = mount(ConfirmDialog, {
      props: { open: true, title: 'Remove X', confirmLabel: 'Remove X' },
      slots: { default: 'This cannot be undone.' },
      attachTo: document.body,
      global: { plugins: [i18n()] },
    })
    await flushPromises()
    const buttons = Array.from(document.body.querySelectorAll('button'))
    const confirm = buttons.find((b) => b.textContent?.includes('Remove X'))!
    const cancel = buttons.find((b) => b.textContent?.includes(en.confirm.cancel))!
    expect(confirm.getAttribute('data-variant')).toBe('destructive')
    confirm.click(); await flushPromises()
    expect(w.emitted('confirm')).toBeTruthy()
    cancel.click(); await flushPromises()
    expect(w.emitted('cancel')).toBeTruthy()
  })
})
```
> The `Button` primitive sets `:data-variant="variant"` (confirmed in `ui/button/Button.vue`), so `data-variant="destructive"` is assertable.
`TotpQr.test.ts`:
```ts
import { describe, it, expect, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
vi.mock('qrcode', () => ({ default: { toDataURL: vi.fn(async () => 'data:image/png;base64,AAAA') } }))
import TotpQr from './TotpQr.vue'

describe('TotpQr', () => {
  it('renders an img with a data: src and the provided alt', async () => {
    const w = mount(TotpQr, { props: { uri: 'otpauth://totp/x', alt: 'Scan to add' } })
    await flushPromises()
    const img = w.find('img')
    expect(img.exists()).toBe(true)
    expect(img.attributes('src')).toContain('data:image/png')
    expect(img.attributes('alt')).toBe('Scan to add')
  })
})
```

- [ ] **Step 7: run + commit (source only).**
```bash
cd dashboard && mise exec -- npm run test && mise exec -- npm run build && cd ..
git checkout -- pkg/webui/dist && git clean -fq pkg/webui/dist
git add dashboard/src/components/custom/CodeField.vue dashboard/src/components/custom/CodeField.test.ts dashboard/src/components/custom/ConfirmDialog.vue dashboard/src/components/custom/ConfirmDialog.test.ts dashboard/src/components/custom/TotpQr.vue dashboard/src/components/custom/TotpQr.test.ts dashboard/src/locales/en.ts dashboard/package.json dashboard/package-lock.json
git commit -m "feat(web): security primitives — CodeField, ConfirmDialog, TotpQr (+ qrcode)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: `RecoveryCodesDisplay`

**Goal:** The recovery-codes screen done to the research bar: mono grid, Copy-all + Download .txt, secure-storage microcopy, and an "I've saved my codes" gate that must be satisfied before the parent can dismiss.

**Files:**
- Create: `dashboard/src/components/custom/RecoveryCodesDisplay.vue`, `.../RecoveryCodesDisplay.test.ts`
- Modify: `dashboard/src/locales/en.ts` (add `recoveryCodes` namespace)

**Acceptance Criteria:**
- [ ] Renders `codes: string[]` in a mono, non-wrapping grid.
- [ ] **Copy all** writes the newline-joined codes to the clipboard; **Download** builds a `text/plain` Blob and triggers a `recovery-codes.txt` download.
- [ ] A checkbox gates a "Done" button; clicking Done emits `confirmed` only after the box is checked.
- [ ] Shows the "old codes invalidated" warning when `regenerated` is true; always shows secure-storage guidance.
- [ ] `npm run test` passes.

**Verify:** `cd dashboard && npm run test` → PASS.

**Steps:**

- [ ] **Step 1: i18n** — add to `dashboard/src/locales/en.ts` after `confirm`:
```ts
  recoveryCodes: {
    heading: 'Save your recovery codes',
    intro: 'Each code can be used once if you lose access to your authenticator app.',
    regeneratedWarning: 'Your previous recovery codes no longer work.',
    storage: 'Store them in a safe place — a password manager or printed copy. Don’t keep them next to your password, and don’t rely on a single screenshot.',
    copyAll: 'Copy all',
    download: 'Download .txt',
    savedConfirm: 'I’ve saved my recovery codes',
    done: 'Done',
  },
```

- [ ] **Step 2: `RecoveryCodesDisplay.vue`.**
```vue
<script setup lang="ts">
/**
 * RecoveryCodesDisplay — present one-time recovery codes safely: mono grid,
 * copy-all + download, secure-storage guidance, and a save-confirmation gate
 * the parent honours before dismissing.
 */
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Copy, Check, Download } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { Alert, AlertDescription } from '@/components/ui/alert'

const props = defineProps<{ codes: string[]; regenerated?: boolean }>()
const emit = defineEmits<{ confirmed: [] }>()
const { t } = useI18n()

const copied = ref(false)
const saved = ref(false)

async function copyAll(): Promise<void> {
  try {
    await navigator.clipboard.writeText(props.codes.join('\n'))
    copied.value = true
    setTimeout(() => { copied.value = false }, 1500)
  } catch { /* no-op */ }
}

function download(): void {
  const blob = new Blob([props.codes.join('\n') + '\n'], { type: 'text/plain' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = 'recovery-codes.txt'
  document.body.appendChild(a)
  a.click()
  a.remove()
  URL.revokeObjectURL(url)
}
</script>

<template>
  <div class="flex flex-col gap-4">
    <div class="flex flex-col gap-1">
      <h3 class="text-sm font-semibold text-ink">{{ t('recoveryCodes.heading') }}</h3>
      <p class="text-sm text-muted">{{ t('recoveryCodes.intro') }}</p>
    </div>

    <Alert v-if="regenerated" role="status">
      <AlertDescription>{{ t('recoveryCodes.regeneratedWarning') }}</AlertDescription>
    </Alert>

    <ul class="grid grid-cols-2 gap-2 rounded-md border border-border bg-sunken p-3">
      <li v-for="c in codes" :key="c" class="whitespace-nowrap font-mono text-sm text-ink">{{ c }}</li>
    </ul>

    <div class="flex flex-wrap gap-2">
      <Button type="button" variant="outline" size="sm" @click="copyAll">
        <component :is="copied ? Check : Copy" class="size-4" aria-hidden="true" />
        <span>{{ copied ? t('common.copied') : t('recoveryCodes.copyAll') }}</span>
      </Button>
      <Button type="button" variant="outline" size="sm" @click="download">
        <Download class="size-4" aria-hidden="true" />
        <span>{{ t('recoveryCodes.download') }}</span>
      </Button>
    </div>

    <p class="text-xs text-muted">{{ t('recoveryCodes.storage') }}</p>

    <label class="flex items-center gap-2 text-sm text-ink">
      <input v-model="saved" type="checkbox" data-test="saved" />
      <span>{{ t('recoveryCodes.savedConfirm') }}</span>
    </label>

    <Button type="button" class="w-full" :disabled="!saved" data-test="done" @click="emit('confirmed')">
      {{ t('recoveryCodes.done') }}
    </Button>
  </div>
</template>
```

- [ ] **Step 3: `RecoveryCodesDisplay.test.ts`.**
```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import RecoveryCodesDisplay from './RecoveryCodesDisplay.vue'

const writeText = vi.fn(async () => {})
beforeEach(() => {
  writeText.mockClear()
  Object.assign(navigator, { clipboard: { writeText } })
  // jsdom lacks createObjectURL
  // @ts-expect-error test stub
  URL.createObjectURL = vi.fn(() => 'blob:x')
  // @ts-expect-error test stub
  URL.revokeObjectURL = vi.fn()
})
const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
const CODES = ['aaaa-bbbb', 'cccc-dddd']

describe('RecoveryCodesDisplay', () => {
  it('renders codes and copies all', async () => {
    const w = mount(RecoveryCodesDisplay, { props: { codes: CODES }, global: { plugins: [i18n()] } })
    expect(w.findAll('li').map((l) => l.text())).toEqual(CODES)
    await w.findAll('button')[0].trigger('click') // Copy all
    expect(writeText).toHaveBeenCalledWith('aaaa-bbbb\ncccc-dddd')
  })

  it('gates the Done emit behind the saved checkbox', async () => {
    const w = mount(RecoveryCodesDisplay, { props: { codes: CODES }, global: { plugins: [i18n()] } })
    const done = w.find('[data-test=done]')
    expect((done.element as HTMLButtonElement).disabled).toBe(true)
    await w.find('[data-test=saved]').setValue(true)
    expect((done.element as HTMLButtonElement).disabled).toBe(false)
    await done.trigger('click')
    expect(w.emitted('confirmed')).toBeTruthy()
  })

  it('shows the regenerated warning when flagged', () => {
    const w = mount(RecoveryCodesDisplay, { props: { codes: CODES, regenerated: true }, global: { plugins: [i18n()] } })
    expect(w.text()).toContain(en.recoveryCodes.regeneratedWarning)
  })
})
```

- [ ] **Step 4: run + commit (source only).**
```bash
cd dashboard && mise exec -- npm run test && cd ..
git checkout -- pkg/webui/dist && git clean -fq pkg/webui/dist 2>/dev/null || true
git add dashboard/src/components/custom/RecoveryCodesDisplay.vue dashboard/src/components/custom/RecoveryCodesDisplay.test.ts dashboard/src/locales/en.ts
git commit -m "feat(web): RecoveryCodesDisplay — copy-all/download/save-gate + storage guidance

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: `PasskeysCard`

**Goal:** List, add, rename, and delete passkeys (session-only endpoints; delete via `ConfirmDialog`, last-passkey rejection surfaced).

**Files:**
- Create: `dashboard/src/pages/security/PasskeysCard.vue`, `.../PasskeysCard.test.ts`
- Modify: `dashboard/src/locales/en.ts` (`security.passkeys` + `errors.last_passkey`)

**Acceptance Criteria:**
- [ ] Lists `GET /me/credentials`; each row shows nickname (fallback "Passkey ····<suffix>"), created/last-used, a backup `StatusBadge`; long nicknames truncate.
- [ ] Add → `register/begin` → `useWebauthn.register` → `register/complete?nickname=` → reload.
- [ ] Rename → `POST /me/credentials/rename {id, nickname}` → reload. Delete → `ConfirmDialog` → `POST /me/credentials/delete {id}` → reload; `last_passkey` error renders.
- [ ] `npm run test` passes.

**Verify:** `cd dashboard && npm run test` → PASS.

**Steps:**

- [ ] **Step 1: i18n** — add `errors.last_passkey: 'You can’t remove your only passkey. Add another first.'` to the `errors` block, and after the `sessions` namespace add:
```ts
  security: {
    title: 'Security',
    passkeys: {
      title: 'Passkeys',
      add: 'Add passkey',
      rename: 'Rename',
      save: 'Save',
      remove: 'Remove',
      removeTitle: 'Remove this passkey?',
      removeBody: 'You’ll sign in with your other passkeys. This passkey will stop working on its device.',
      created: 'Added',
      lastUsed: 'Last used',
      synced: 'Synced',
      deviceBound: 'This device',
      defaultName: 'Passkey',
    },
  },
```
> Tasks 4–6 extend the `security` namespace; the implementer adds the sub-objects their card needs.

- [ ] **Step 2: `PasskeysCard.vue`.**
```vue
<script setup lang="ts">
/**
 * PasskeysCard — manage the account's WebAuthn passkeys. List/add/rename/delete
 * are session-only (not sudo-gated). The backend sends excludeCredentials on
 * begin (no duplicate passkeys) and rejects deleting the last passkey.
 */
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PublicKeyCredentialCreationOptionsJSON } from '@simplewebauthn/browser'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { useWebauthn } from '@/composables/useWebauthn'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Alert, AlertDescription } from '@/components/ui/alert'
import StatusBadge from '@/components/custom/StatusBadge.vue'
import ConfirmDialog from '@/components/custom/ConfirmDialog.vue'
import { Trash2, Plus } from 'lucide-vue-next'

interface CredentialView {
  id: number
  credentialIdSuffix: string
  nickname?: string
  transports: string[]
  backupState: boolean
  attestationType: string
  createdAt: string
  lastUsedAt?: string
}

const { t, te } = useI18n()
const { busy: netBusy, error: netError, run } = useApi()
const { busy: waBusy, error: waError, register } = useWebauthn()

const busy = computed(() => netBusy.value || waBusy.value)
const errorText = computed(() => {
  const e = netError.value ?? waError.value
  if (!e) return ''
  const key = `errors.${e.code}`
  return te(key) ? t(key) : e.message || t('common.error')
})

const rows = ref<CredentialView[]>([])
const editingId = ref<number | null>(null)
const draftName = ref('')
const confirmId = ref<number | null>(null)

const fmt = (d?: string) => { if (!d) return ''; const n = Date.parse(d); return Number.isNaN(n) ? '' : new Date(n).toLocaleDateString() }
const displayName = (c: CredentialView) => c.nickname || `${t('security.passkeys.defaultName')} ····${c.credentialIdSuffix}`

async function load(): Promise<void> {
  const res = await run(() => api.get<CredentialView[]>('/api/prohibitorum/me/credentials'))
  if (res) rows.value = res
}

async function add(): Promise<void> {
  const options = await run(() =>
    api.post<PublicKeyCredentialCreationOptionsJSON>('/api/prohibitorum/me/credentials/register/begin'))
  if (!options) return
  const attestation = await register(options)
  if (!attestation) return
  const ok = await run(async () => {
    await api.post('/api/prohibitorum/me/credentials/register/complete', attestation)
    return true as const
  })
  if (ok) await load()
}

function startRename(c: CredentialView): void { editingId.value = c.id; draftName.value = c.nickname ?? '' }
async function saveRename(id: number): Promise<void> {
  const ok = await run(async () => {
    await api.post('/api/prohibitorum/me/credentials/rename', { id, nickname: draftName.value || null })
    return true as const
  })
  if (ok) { editingId.value = null; await load() }
}

async function confirmDelete(): Promise<void> {
  const id = confirmId.value
  if (id == null) return
  const ok = await run(async () => {
    await api.post('/api/prohibitorum/me/credentials/delete', { id })
    return true as const
  })
  confirmId.value = null
  if (ok) await load()
}

onMounted(load)
</script>

<template>
  <Card>
    <CardHeader class="flex flex-row items-center justify-between gap-2">
      <CardTitle>{{ t('security.passkeys.title') }}</CardTitle>
      <Button type="button" size="sm" :disabled="busy" @click="add">
        <Plus class="size-4" aria-hidden="true" /><span>{{ t('security.passkeys.add') }}</span>
      </Button>
    </CardHeader>
    <CardContent class="flex flex-col gap-3">
      <Alert v-if="errorText" variant="destructive" role="alert" aria-live="polite">
        <AlertDescription>{{ errorText }}</AlertDescription>
      </Alert>

      <div v-for="c in rows" :key="c.id" class="flex items-center justify-between gap-3 border-b border-border pb-3 last:border-0 last:pb-0">
        <div class="flex min-w-0 flex-col gap-1">
          <div class="flex items-center gap-2">
            <template v-if="editingId === c.id">
              <Input v-model="draftName" name="nickname" class="h-8 w-48" :placeholder="displayName(c)" />
              <Button type="button" size="sm" :disabled="busy" @click="saveRename(c.id)">{{ t('security.passkeys.save') }}</Button>
            </template>
            <template v-else>
              <span class="min-w-0 truncate text-sm text-ink">{{ displayName(c) }}</span>
              <StatusBadge :variant="c.backupState ? 'success' : 'neutral'" class="shrink-0">
                {{ c.backupState ? t('security.passkeys.synced') : t('security.passkeys.deviceBound') }}
              </StatusBadge>
            </template>
          </div>
          <span class="text-xs text-muted">
            {{ t('security.passkeys.created') }}: {{ fmt(c.createdAt) }}
            <template v-if="fmt(c.lastUsedAt)"> · {{ t('security.passkeys.lastUsed') }}: {{ fmt(c.lastUsedAt) }}</template>
          </span>
        </div>
        <div class="flex shrink-0 items-center gap-1">
          <Button v-if="editingId !== c.id" type="button" variant="ghost" size="sm" @click="startRename(c)">{{ t('security.passkeys.rename') }}</Button>
          <Button type="button" variant="ghost" size="icon-sm" :aria-label="t('security.passkeys.remove')" @click="confirmId = c.id">
            <Trash2 class="size-4" aria-hidden="true" />
          </Button>
        </div>
      </div>
    </CardContent>
  </Card>

  <ConfirmDialog
    :open="confirmId !== null"
    :title="t('security.passkeys.removeTitle')"
    :confirm-label="t('security.passkeys.remove')"
    :busy="busy"
    @update:open="(v) => { if (!v) confirmId = null }"
    @cancel="confirmId = null"
    @confirm="confirmDelete"
  >
    {{ t('security.passkeys.removeBody') }}
  </ConfirmDialog>
</template>
```

- [ ] **Step 3: `PasskeysCard.test.ts`.**
```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import PasskeysCard from './PasskeysCard.vue'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'
vi.mock('@/lib/webauthn', () => ({
  passkeyGet: vi.fn(), isUserCancel: () => false,
  passkeyRegister: vi.fn(async () => ({ id: 'newcred', response: {} })),
}))
const get = vi.mocked(api.get); const post = vi.mocked(api.post)
const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
const CREDS = [
  { id: 1, credentialIdSuffix: 'ab12', nickname: 'Laptop', transports: ['internal'], backupState: true, attestationType: 'none', createdAt: '2026-01-01T00:00:00Z' },
  { id: 2, credentialIdSuffix: 'cd34', transports: ['usb'], backupState: false, attestationType: 'none', createdAt: '2026-01-02T00:00:00Z' },
]
beforeEach(() => { get.mockReset(); post.mockReset() })
const mountCard = () => mount(PasskeysCard, { global: { plugins: [i18n()] }, attachTo: document.body })

describe('PasskeysCard', () => {
  it('lists passkeys with a fallback name and backup badge', async () => {
    get.mockResolvedValue(CREDS)
    const w = mountCard(); await flushPromises()
    expect(w.text()).toContain('Laptop')
    expect(w.text()).toContain('····cd34') // fallback name for the unnamed cred
  })

  it('adds a passkey (begin → register → complete) then reloads', async () => {
    get.mockResolvedValue(CREDS)
    post.mockImplementation(async (p: string) => p.endsWith('/register/begin') ? { challenge: 'c' } : undefined)
    const w = mountCard(); await flushPromises()
    const addBtn = w.findAll('button').find((b) => b.text().includes(en.security.passkeys.add))!
    await addBtn.trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/credentials/register/begin')
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/credentials/register/complete', expect.objectContaining({ id: 'newcred' }))
    expect(get).toHaveBeenCalledTimes(2)
  })

  it('renders the last_passkey error from a failed delete', async () => {
    get.mockResolvedValue([CREDS[0]])
    post.mockRejectedValue({ code: 'last_passkey', message: '…' })
    const w = mountCard(); await flushPromises()
    // open confirm + confirm
    await w.find('[aria-label="' + en.security.passkeys.remove + '"]').trigger('click')
    await flushPromises()
    const confirmBtn = Array.from(document.body.querySelectorAll('button')).find((b) => b.getAttribute('data-variant') === 'destructive')!
    confirmBtn.click(); await flushPromises()
    expect(w.text()).toContain(en.errors.last_passkey)
  })
})
```

- [ ] **Step 4: run + commit (source only).**
```bash
cd dashboard && mise exec -- npm run test && cd ..
git checkout -- pkg/webui/dist && git clean -fq pkg/webui/dist 2>/dev/null || true
git add dashboard/src/pages/security/PasskeysCard.vue dashboard/src/pages/security/PasskeysCard.test.ts dashboard/src/locales/en.ts
git commit -m "feat(web): PasskeysCard — list/add/rename/delete with confirm + last-passkey guard

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: `PasswordCard`

**Goal:** Set/replace the account password behind the sudo gate, with client-side length + match validation.

**Files:**
- Create: `dashboard/src/pages/security/PasswordCard.vue`, `.../PasswordCard.test.ts`
- Modify: `dashboard/src/locales/en.ts` (`security.password`)

**Acceptance Criteria:**
- [ ] New-password + confirm fields; client mirror of 8–1024 length + match; submit → `withSudo(POST /me/password/set {password})`; success feedback; errors via `errors.<code>`.
- [ ] `npm run test` passes (test mocks `@/lib/sudo` so `withSudo` runs the fn directly).

**Verify:** `cd dashboard && npm run test` → PASS.

**Steps:**

- [ ] **Step 1: i18n** — add under `security` (sibling of `passkeys`):
```ts
    password: {
      title: 'Password',
      help: 'Set a password you can use with a one-time code if a passkey isn’t available.',
      newLabel: 'New password',
      confirmLabel: 'Confirm password',
      submit: 'Save password',
      tooShort: 'Use at least 8 characters.',
      mismatch: 'Passwords don’t match.',
      saved: 'Password updated.',
    },
```

- [ ] **Step 2: `PasswordCard.vue`.**
```vue
<script setup lang="ts">
/** PasswordCard — set/replace the password (always sudo-gated server-side). */
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { withSudo } from '@/lib/sudo'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Alert, AlertDescription } from '@/components/ui/alert'

const { t, te } = useI18n()
const { busy, error, run } = useApi()
const pw = ref('')
const confirm = ref('')
const localError = ref('')
const done = ref(false)

const errorText = computed(() => {
  if (localError.value) return localError.value
  const e = error.value
  if (!e) return ''
  const key = `errors.${e.code}`
  return te(key) ? t(key) : e.message || t('common.error')
})

async function submit(): Promise<void> {
  localError.value = ''
  done.value = false
  if (pw.value.length < 8) { localError.value = t('security.password.tooShort'); return }
  if (pw.value !== confirm.value) { localError.value = t('security.password.mismatch'); return }
  const ok = await run(() => withSudo(async () => {
    await api.post('/api/prohibitorum/me/password/set', { password: pw.value })
    return true as const
  }))
  if (ok) { done.value = true; pw.value = ''; confirm.value = '' }
}
</script>

<template>
  <Card>
    <CardHeader><CardTitle>{{ t('security.password.title') }}</CardTitle></CardHeader>
    <CardContent>
      <form class="flex max-w-sm flex-col gap-4" @submit.prevent="submit">
        <p class="text-sm text-muted">{{ t('security.password.help') }}</p>
        <div class="flex flex-col gap-1.5">
          <Label for="pw-new">{{ t('security.password.newLabel') }}</Label>
          <Input id="pw-new" v-model="pw" name="new_password" type="password" autocomplete="new-password" required />
        </div>
        <div class="flex flex-col gap-1.5">
          <Label for="pw-confirm">{{ t('security.password.confirmLabel') }}</Label>
          <Input id="pw-confirm" v-model="confirm" name="confirm_password" type="password" autocomplete="new-password" required />
        </div>
        <Alert v-if="errorText" variant="destructive" role="alert" aria-live="polite">
          <AlertDescription>{{ errorText }}</AlertDescription>
        </Alert>
        <p v-if="done" class="text-sm text-sage" role="status">{{ t('security.password.saved') }}</p>
        <Button type="submit" :disabled="busy">{{ t('security.password.submit') }}</Button>
      </form>
    </CardContent>
  </Card>
</template>
```

- [ ] **Step 3: `PasswordCard.test.ts`.**
```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import PasswordCard from './PasswordCard.vue'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'
// withSudo runs fn directly in tests (the gate itself is unit-tested in sudo.test.ts).
vi.mock('@/lib/sudo', () => ({ withSudo: (fn: () => unknown) => fn(), ensureSudo: vi.fn(), sudoState: { value: { open: false, resolve: null } }, _resolveSudo: vi.fn() }))
const post = vi.mocked(api.post)
const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
beforeEach(() => post.mockReset())
const mountCard = () => mount(PasswordCard, { global: { plugins: [i18n()] } })

describe('PasswordCard', () => {
  it('rejects a short password before calling the API', async () => {
    const w = mountCard()
    await w.find('input[name=new_password]').setValue('short')
    await w.find('input[name=confirm_password]').setValue('short')
    await w.find('form').trigger('submit'); await flushPromises()
    expect(w.text()).toContain(en.security.password.tooShort)
    expect(post).not.toHaveBeenCalled()
  })

  it('flags a mismatch', async () => {
    const w = mountCard()
    await w.find('input[name=new_password]').setValue('longenough1')
    await w.find('input[name=confirm_password]').setValue('different12')
    await w.find('form').trigger('submit'); await flushPromises()
    expect(w.text()).toContain(en.security.password.mismatch)
    expect(post).not.toHaveBeenCalled()
  })

  it('posts the password and shows success', async () => {
    post.mockResolvedValue(undefined)
    const w = mountCard()
    await w.find('input[name=new_password]').setValue('longenough1')
    await w.find('input[name=confirm_password]').setValue('longenough1')
    await w.find('form').trigger('submit'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/password/set', { password: 'longenough1' })
    expect(w.text()).toContain(en.security.password.saved)
  })
})
```

- [ ] **Step 4: run + commit (source only).**
```bash
cd dashboard && mise exec -- npm run test && cd ..
git checkout -- pkg/webui/dist && git clean -fq pkg/webui/dist 2>/dev/null || true
git add dashboard/src/pages/security/PasswordCard.vue dashboard/src/pages/security/PasswordCard.test.ts dashboard/src/locales/en.ts
git commit -m "feat(web): PasswordCard — set/replace password via sudo gate + client validation

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: `TotpCard`

**Goal:** Enroll a TOTP authenticator — sudo-gated begin → QR + secret → verify → show recovery codes on first enrollment.

**Files:**
- Create: `dashboard/src/pages/security/TotpCard.vue`, `.../TotpCard.test.ts`
- Modify: `dashboard/src/locales/en.ts` (`security.totp`)

**Acceptance Criteria:**
- [ ] "Set up" → `withSudo(begin)` renders `TotpQr(otpauth_uri)` + `CodeField(secret_base32)` + a code input; verify → `withSudo(verify {code})`; first-enrollment `recovery_codes` → `RecoveryCodesDisplay`.
- [ ] `npm run test` passes (mock `@/lib/sudo`, `qrcode`).

**Verify:** `cd dashboard && npm run test` → PASS.

**Steps:**

- [ ] **Step 1: i18n** — add under `security`:
```ts
    totp: {
      title: 'Authenticator app',
      help: 'Use a TOTP app (Google Authenticator, 1Password, …) for one-time codes.',
      setup: 'Set up authenticator',
      scan: 'Scan this QR code with your authenticator app',
      secretLabel: 'Or enter this key manually',
      codeLabel: 'Enter the 6-digit code to confirm',
      verify: 'Verify & enable',
      enabled: 'Authenticator enabled.',
    },
```

- [ ] **Step 2: `TotpCard.vue`.**
```vue
<script setup lang="ts">
/**
 * TotpCard — enroll a TOTP authenticator. begin (sudo-gated when re-enrolling)
 * returns secret+otpauth; the backend persists only on verify. First
 * enrollment returns recovery codes.
 */
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { withSudo } from '@/lib/sudo'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Alert, AlertDescription } from '@/components/ui/alert'
import CodeField from '@/components/custom/CodeField.vue'
import TotpQr from '@/components/custom/TotpQr.vue'
import RecoveryCodesDisplay from '@/components/custom/RecoveryCodesDisplay.vue'

const { t, te } = useI18n()
const { busy, error, run } = useApi()
const secret = ref('')
const otpauth = ref('')
const code = ref('')
const recovery = ref<string[]>([])
const enabled = ref(false)

const errorText = computed(() => {
  const e = error.value
  if (!e) return ''
  const key = `errors.${e.code}`
  return te(key) ? t(key) : e.message || t('common.error')
})

async function setup(): Promise<void> {
  const r = await run(() => withSudo(() =>
    api.post<{ secret_base32: string; otpauth_uri: string }>('/api/prohibitorum/me/totp/begin')))
  if (!r) return
  secret.value = r.secret_base32
  otpauth.value = r.otpauth_uri
  recovery.value = []
  enabled.value = false
}

async function verify(): Promise<void> {
  const r = await run(() => withSudo(() =>
    api.post<{ recovery_codes?: string[] } | undefined>('/api/prohibitorum/me/totp/verify', { code: code.value })))
  // 204 (re-enroll) → undefined; first enrollment → { recovery_codes }
  if (error.value) return
  enabled.value = true
  secret.value = ''; otpauth.value = ''; code.value = ''
  if (r && r.recovery_codes) recovery.value = r.recovery_codes
}
</script>

<template>
  <Card>
    <CardHeader><CardTitle>{{ t('security.totp.title') }}</CardTitle></CardHeader>
    <CardContent class="flex flex-col gap-4">
      <p class="text-sm text-muted">{{ t('security.totp.help') }}</p>

      <RecoveryCodesDisplay v-if="recovery.length" :codes="recovery" @confirmed="recovery = []" />

      <template v-else-if="!secret">
        <p v-if="enabled" class="text-sm text-sage" role="status">{{ t('security.totp.enabled') }}</p>
        <Button type="button" class="w-fit" :disabled="busy" @click="setup">{{ t('security.totp.setup') }}</Button>
      </template>

      <template v-else>
        <p class="text-sm text-ink">{{ t('security.totp.scan') }}</p>
        <TotpQr :uri="otpauth" :alt="t('security.totp.scan')" />
        <CodeField :value="secret" :label="t('security.totp.secretLabel')" />
        <form class="flex max-w-xs flex-col gap-2" @submit.prevent="verify">
          <Label for="totp-code">{{ t('security.totp.codeLabel') }}</Label>
          <Input id="totp-code" v-model="code" name="code" inputmode="numeric" autocomplete="one-time-code" required />
          <Button type="submit" :disabled="busy">{{ t('security.totp.verify') }}</Button>
        </form>
      </template>

      <Alert v-if="errorText" variant="destructive" role="alert" aria-live="polite">
        <AlertDescription>{{ errorText }}</AlertDescription>
      </Alert>
    </CardContent>
  </Card>
</template>
```

- [ ] **Step 3: `TotpCard.test.ts`.**
```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import TotpCard from './TotpCard.vue'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'
vi.mock('@/lib/sudo', () => ({ withSudo: (fn: () => unknown) => fn(), ensureSudo: vi.fn(), sudoState: { value: { open: false, resolve: null } }, _resolveSudo: vi.fn() }))
vi.mock('qrcode', () => ({ default: { toDataURL: vi.fn(async () => 'data:image/png;base64,AAAA') } }))
const post = vi.mocked(api.post)
const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
beforeEach(() => { post.mockReset(); Object.assign(navigator, { clipboard: { writeText: vi.fn(async () => {}) } }) })
const mountCard = () => mount(TotpCard, { global: { plugins: [i18n()] }, attachTo: document.body })

describe('TotpCard', () => {
  it('enrolls: setup shows QR+secret, verify shows recovery codes', async () => {
    post.mockImplementation(async (p: string) =>
      p.endsWith('/totp/begin') ? { secret_base32: 'SECRET', otpauth_uri: 'otpauth://totp/x' }
      : p.endsWith('/totp/verify') ? { recovery_codes: ['c1', 'c2'] } : undefined)
    const w = mountCard()
    await w.findAll('button').find((b) => b.text().includes(en.security.totp.setup))!.trigger('click')
    await flushPromises()
    expect(w.find('img').exists()).toBe(true)        // QR
    expect(w.text()).toContain('SECRET')             // manual key
    await w.find('input[name=code]').setValue('123456')
    await w.find('form').trigger('submit'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/totp/verify', { code: '123456' })
    expect(w.text()).toContain(en.recoveryCodes.heading) // recovery codes shown
  })
})
```

- [ ] **Step 4: run + commit (source only).**
```bash
cd dashboard && mise exec -- npm run test && cd ..
git checkout -- pkg/webui/dist && git clean -fq pkg/webui/dist 2>/dev/null || true
git add dashboard/src/pages/security/TotpCard.vue dashboard/src/pages/security/TotpCard.test.ts dashboard/src/locales/en.ts
git commit -m "feat(web): TotpCard — sudo-gated enrollment, QR + secret, recovery codes on first enroll

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: `RecoveryCodesCard` + `SecurityView` + route + nav + revoke action

**Goal:** The standalone regenerate card, the page that stacks all four cards, the bottom destructive "remove password & authenticator" action, and the route/nav wiring.

**Files:**
- Create: `dashboard/src/pages/security/RecoveryCodesCard.vue`, `.../RecoveryCodesCard.test.ts`
- Create: `dashboard/src/pages/SecurityView.vue`, `dashboard/src/pages/SecurityView.test.ts`
- Modify: `dashboard/src/router/index.ts` (add `/security` child), `dashboard/src/components/custom/AppSidebar.vue` (nav link), `dashboard/src/locales/en.ts` (`security.recovery`, `security.revoke`, `nav.security`)

**Acceptance Criteria:**
- [ ] `RecoveryCodesCard`: "Regenerate" → `withSudo(regenerate)` → `RecoveryCodesDisplay regenerated`; no-TOTP `bad_request` → plain-language hint.
- [ ] `SecurityView` stacks the four cards + a bottom `ConfirmDialog`-gated revoke (`withSudo(revoke-password-totp)`); reachable at `/security` with a sidebar link; `requiresAuth`.
- [ ] `npm run test` passes; `npm run build` exit 0.

**Verify:** `cd dashboard && npm run test` → PASS; `npm run build` → exit 0.

**Steps:**

- [ ] **Step 1: i18n** — add `nav.security: 'Security'` to the `nav` namespace; add under `security`:
```ts
    recovery: {
      title: 'Recovery codes',
      help: 'One-time codes to sign in if you lose your authenticator. Requires an authenticator app.',
      regenerate: 'Regenerate codes',
      needTotp: 'Set up an authenticator app first.',
    },
    revoke: {
      title: 'Password & authenticator',
      help: 'Remove your password, authenticator app, and recovery codes. You’ll sign in with passkeys only.',
      button: 'Remove password & authenticator',
      confirmTitle: 'Remove password & authenticator?',
      confirmBody: 'This deletes your password, authenticator app, and recovery codes. You’ll be able to sign in with your passkeys only. You can set them up again later.',
      done: 'Removed. You now sign in with passkeys only.',
    },
```

- [ ] **Step 2: `RecoveryCodesCard.vue`.**
```vue
<script setup lang="ts">
/** RecoveryCodesCard — regenerate recovery codes (sudo-gated; needs confirmed TOTP). */
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { withSudo } from '@/lib/sudo'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Alert, AlertDescription } from '@/components/ui/alert'
import RecoveryCodesDisplay from '@/components/custom/RecoveryCodesDisplay.vue'

const { t, te } = useI18n()
const { busy, error, run } = useApi()
const codes = ref<string[]>([])

const errorText = computed(() => {
  const e = error.value
  if (!e) return ''
  if (e.code === 'bad_request') return t('security.recovery.needTotp')
  const key = `errors.${e.code}`
  return te(key) ? t(key) : e.message || t('common.error')
})

async function regenerate(): Promise<void> {
  const r = await run(() => withSudo(() =>
    api.post<{ recovery_codes: string[] }>('/api/prohibitorum/me/recovery-codes/regenerate')))
  if (r) codes.value = r.recovery_codes ?? []
}
</script>

<template>
  <Card>
    <CardHeader><CardTitle>{{ t('security.recovery.title') }}</CardTitle></CardHeader>
    <CardContent class="flex flex-col gap-4">
      <RecoveryCodesDisplay v-if="codes.length" :codes="codes" regenerated @confirmed="codes = []" />
      <template v-else>
        <p class="text-sm text-muted">{{ t('security.recovery.help') }}</p>
        <Button type="button" variant="outline" class="w-fit" :disabled="busy" @click="regenerate">
          {{ t('security.recovery.regenerate') }}
        </Button>
      </template>
      <Alert v-if="errorText" variant="destructive" role="alert" aria-live="polite">
        <AlertDescription>{{ errorText }}</AlertDescription>
      </Alert>
    </CardContent>
  </Card>
</template>
```

- [ ] **Step 3: `SecurityView.vue`.**
```vue
<script setup lang="ts">
/** SecurityView (/security) — stacks the factor cards + the coarse revoke action. */
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { withSudo } from '@/lib/sudo'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Alert, AlertDescription } from '@/components/ui/alert'
import ConfirmDialog from '@/components/custom/ConfirmDialog.vue'
import PasskeysCard from '@/pages/security/PasskeysCard.vue'
import PasswordCard from '@/pages/security/PasswordCard.vue'
import TotpCard from '@/pages/security/TotpCard.vue'
import RecoveryCodesCard from '@/pages/security/RecoveryCodesCard.vue'

const { t, te } = useI18n()
const { busy, error, run } = useApi()
const confirmOpen = ref(false)
const done = ref(false)

const errorText = computed(() => {
  const e = error.value
  if (!e) return ''
  const key = `errors.${e.code}`
  return te(key) ? t(key) : e.message || t('common.error')
})

async function revoke(): Promise<void> {
  const ok = await run(() => withSudo(async () => {
    await api.post('/api/prohibitorum/me/auth/revoke-password-totp')
    return true as const
  }))
  confirmOpen.value = false
  if (ok) done.value = true
}
</script>

<template>
  <div class="flex max-w-2xl flex-col gap-6">
    <h1 class="text-lg font-semibold tracking-tight text-ink">{{ t('security.title') }}</h1>
    <PasskeysCard />
    <PasswordCard />
    <TotpCard />
    <RecoveryCodesCard />

    <Card>
      <CardHeader><CardTitle>{{ t('security.revoke.title') }}</CardTitle></CardHeader>
      <CardContent class="flex flex-col gap-3">
        <p class="text-sm text-muted">{{ t('security.revoke.help') }}</p>
        <Alert v-if="errorText" variant="destructive" role="alert" aria-live="polite">
          <AlertDescription>{{ errorText }}</AlertDescription>
        </Alert>
        <p v-if="done" class="text-sm text-sage" role="status">{{ t('security.revoke.done') }}</p>
        <Button type="button" variant="destructive" class="w-fit" :disabled="busy" @click="confirmOpen = true">
          {{ t('security.revoke.button') }}
        </Button>
      </CardContent>
    </Card>

    <ConfirmDialog
      :open="confirmOpen"
      :title="t('security.revoke.confirmTitle')"
      :confirm-label="t('security.revoke.button')"
      :busy="busy"
      @update:open="(v) => { if (!v) confirmOpen = false }"
      @cancel="confirmOpen = false"
      @confirm="revoke"
    >
      {{ t('security.revoke.confirmBody') }}
    </ConfirmDialog>
  </div>
</template>
```

- [ ] **Step 4: wire route + nav.** In `dashboard/src/router/index.ts`, add a child to the `/` layout route's `children` array (after `sessions`):
```ts
      { path: 'security', name: 'security', component: () => import('../pages/SecurityView.vue') },
```
In `dashboard/src/components/custom/AppSidebar.vue`, import `KeyRound` from `lucide-vue-next` and add to `accountItems` (after Profile, before Sessions):
```ts
  { to: '/security', label: t('nav.security'), icon: KeyRound },
```

- [ ] **Step 5: tests.** `RecoveryCodesCard.test.ts`:
```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import RecoveryCodesCard from './RecoveryCodesCard.vue'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'
vi.mock('@/lib/sudo', () => ({ withSudo: (fn: () => unknown) => fn(), ensureSudo: vi.fn(), sudoState: { value: { open: false, resolve: null } }, _resolveSudo: vi.fn() }))
const post = vi.mocked(api.post)
const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
beforeEach(() => { post.mockReset(); Object.assign(navigator, { clipboard: { writeText: vi.fn(async () => {}) } }) })
const mountCard = () => mount(RecoveryCodesCard, { global: { plugins: [i18n()] } })

describe('RecoveryCodesCard', () => {
  it('regenerates and displays codes', async () => {
    post.mockResolvedValue({ recovery_codes: ['x1', 'x2'] })
    const w = mountCard()
    await w.find('button').trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/recovery-codes/regenerate')
    expect(w.findAll('li').map((l) => l.text())).toEqual(['x1', 'x2'])
  })

  it('shows the need-TOTP hint on bad_request', async () => {
    post.mockRejectedValue({ code: 'bad_request', message: '…' })
    const w = mountCard()
    await w.find('button').trigger('click'); await flushPromises()
    expect(w.text()).toContain(en.security.recovery.needTotp)
  })
})
```
`SecurityView.test.ts`:
```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import SecurityView from './SecurityView.vue'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn(async () => []), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'
vi.mock('@/lib/sudo', () => ({ withSudo: (fn: () => unknown) => fn(), ensureSudo: vi.fn(), sudoState: { value: { open: false, resolve: null } }, _resolveSudo: vi.fn() }))
vi.mock('qrcode', () => ({ default: { toDataURL: vi.fn(async () => 'data:image/png;base64,AAAA') } }))
const post = vi.mocked(api.post)
const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
beforeEach(() => { post.mockReset(); Object.assign(navigator, { clipboard: { writeText: vi.fn(async () => {}) } }) })

describe('SecurityView', () => {
  it('renders the four cards and the revoke action; revoke opens confirm → posts', async () => {
    const w = mount(SecurityView, { global: { plugins: [i18n()] }, attachTo: document.body })
    await flushPromises()
    expect(w.text()).toContain(en.security.passkeys.title)
    expect(w.text()).toContain(en.security.password.title)
    expect(w.text()).toContain(en.security.totp.title)
    expect(w.text()).toContain(en.security.recovery.title)
    // open the revoke confirm and confirm it
    await w.findAll('button').find((b) => b.text() === en.security.revoke.button)!.trigger('click')
    await flushPromises()
    const confirmBtn = Array.from(document.body.querySelectorAll('button')).find((b) => b.getAttribute('data-variant') === 'destructive' && b.textContent?.includes(en.security.revoke.button))!
    confirmBtn.click(); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/auth/revoke-password-totp')
  })
})
```

- [ ] **Step 6: run + commit (source only).**
```bash
cd dashboard && mise exec -- npm run test && mise exec -- npm run build && cd ..
git checkout -- pkg/webui/dist && git clean -fq pkg/webui/dist
git add dashboard/src/pages/security/RecoveryCodesCard.vue dashboard/src/pages/security/RecoveryCodesCard.test.ts dashboard/src/pages/SecurityView.vue dashboard/src/pages/SecurityView.test.ts dashboard/src/router/index.ts dashboard/src/components/custom/AppSidebar.vue dashboard/src/locales/en.ts
git commit -m "feat(web): /security — RecoveryCodesCard + SecurityView (4 cards + revoke), route + nav

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 7: Done-gate — CSP/embed check, rebuild dist, smoke

**Goal:** Confirm the Security surface adds no inline assets, rebuild + commit the dist, and verify the full gate.

**Files:**
- Modify: `pkg/webui/dist/*` (rebuilt, committed)

**Acceptance Criteria:**
- [ ] `pkg/webui/dist/index.html` has zero inline `<script>` (only `src=`) and zero inline `<style>`.
- [ ] `mise exec -- go build ./... && go vet ./... && go test ./...` exit 0; `npm run test` green; `npm run build` clean.
- [ ] `cmd/smoke` `SMOKE_EXIT=0` (the SPA shell still serves; `/security` is a real route).

**Verify:** `setsid bash /tmp/run_v06.sh`; `cat /tmp/v06.result` → `SMOKE_EXIT=0`.

**Steps:**

- [ ] **Step 1: rebuild + inline-asset check.**
```bash
cd dashboard && mise exec -- npm run build && cd ..
grep -oE '<script[^>]*>' pkg/webui/dist/index.html   # only src=… module
grep -c '<style' pkg/webui/dist/index.html           # expect 0
```

- [ ] **Step 2: Go gate.** `mise exec -- go build ./... && mise exec -- go vet ./... && mise exec -- go test ./...` → exit 0.

- [ ] **Step 3: Frontend gate.** `cd dashboard && mise exec -- npm run test && cd ..` → all green.

- [ ] **Step 4: Smoke.**
```bash
rm -f /tmp/v06.result
setsid bash /tmp/run_v06.sh >/dev/null 2>&1 < /dev/null &
# poll until SMOKE_EXIT= appears:
grep 'SMOKE_EXIT=' /tmp/v06.result   # expect SMOKE_EXIT=0
```

- [ ] **Step 5: Runtime spot-check (per always-verify-fixes).** `mise dev-server` + `mise enroll-admin` → `/security`: add a passkey, set a password, enroll TOTP (scan QR + verify → recovery codes), regenerate codes, then the revoke action. Confirm no layout wrap on passkey rows / badges.

- [ ] **Step 6: commit dist.**
```bash
git add pkg/webui/dist
git commit -m "feat(web): Spec 2b done-gate — rebuild+commit dist; CSP unchanged; smoke green

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Self-Review

- **Spec coverage:** §4 routing/nav → Task 6; §5 building blocks → Tasks 1 (CodeField/ConfirmDialog/TotpQr) + 2 (RecoveryCodesDisplay); §6 cards → Tasks 3 (Passkeys), 4 (Password), 5 (TOTP), 6 (Recovery + SecurityView + revoke); §7 i18n → split across tasks; §8 testing → each task; §9 CSP/done-gate → Task 7; §10 limitations → documented in the spec (no code). All mapped.
- **Placeholder scan:** every component + test has concrete, copy-paste-ready code (`ConfirmDialog.vue` is a single correct `<script setup>` with `useI18n()` inside). No TBD/"similar to"/prose-only steps.
- **Type/name consistency:** `withSudo`/`ensureSudo` mock shape consistent across Tasks 4/5/6 tests; `CredentialView` fields match the contract; `RecoveryCodesDisplay` props (`codes`, `regenerated`) + `confirmed` emit consistent between Tasks 2/5/6; `errors.last_passkey` added in Task 3 and asserted there; `nav.security` added in Task 6 (used by AppSidebar) — the `security` namespace is grown additively across Tasks 3→6 (each adds its sub-object), no key collisions.
- **Risk note:** riskiest is Task 6 (SecurityView wiring + the ConfirmDialog/destructive-button test selector). Mitigated by `data-variant="destructive"` being a real attribute on the Button primitive and the smoke/runtime check in Task 7. The `ConfirmDialog` Cancel-initial-focus relies on `$el.focus()` of the Button component — same `$el` pattern proven in Spec-2a's PasswordTotpForm.
