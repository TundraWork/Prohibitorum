# Account recovery + RP management + Federated invites — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers-extended-cc:subagent-driven-development (recommended) or superpowers-extended-cc:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship three batched areas: inline account recovery on `/login`, admin management of OIDC clients + SAML service providers, and OIDC-federated invitations.

**Architecture:** Frontend-heavy on the established 2a/2b/2c/3a patterns (`useApi`/`errorText`, `withSudo`, `ConfirmDialog`, `StatusBadge`, `CodeField`, `Table`, `data-test`, the `isAdmin` Admin sidebar group + `requiresAdmin` routes). One small additive backend change (Section C: invitation federation slug). Admin views mirror the **shipped** 3a views (`AdminAccountsView`/`AdminAccountDetailView`/`AdminInvitationsView`) — implementers read those files; each task gives exact contracts + deltas + complete authoritative tests.

**Tech Stack:** Vue 3, Vue Router, vue-i18n, Vitest + @vue/test-utils, Tailwind v4 + shadcn-vue, Go (chi + Huma) for Section C.

**Spec:** `docs/superpowers/specs/2026-06-08-recovery-rp-management-federated-invites-design.md`

**Verified contracts (authoritative — from `pkg/contract/auth.go` + handlers; `api.md` is STALE):**
- Recovery: `POST /auth/recovery-code/verify {partial_session_token, code}` → `{recovery_session_token}`; `POST /auth/recovery/totp/begin {recovery_session_token}` → `{secret_base32, otpauth_uri}`; `POST /auth/recovery/totp/verify {recovery_session_token, code}` → `{recovery_codes:[]}` + sets cookie. (`POST /auth/password/begin {username,password}` → `{partial_session_token}` is already used by PasswordTotpForm.)
- OIDC clients: `GET /oidc-clients`→`[]OIDCClientView{clientId,displayName,redirectUris[],postLogoutRedirectUris[],allowedScopes[],tokenEndpointAuthMethod,requireConsent,disabled,createdAt}`; `GET /oidc-clients/{clientId}`; `POST /oidc-clients {clientId,displayName,redirectUris[],postLogoutRedirectUris[]?,scopes[]?,public,requireConsent}`→ view + `secret` (omitempty, confidential only); `PUT /oidc-clients/{clientId} {displayName,redirectUris[],postLogoutRedirectUris[]?,allowedScopes[]?,requireConsent,disabled}` (**PUT uses `allowedScopes`; create uses `scopes`**); `POST /oidc-clients/rotate-secret {clientId}`→`{clientId,secret}`; `POST /oidc-clients/delete {clientId}`→204. Errors: `client_not_found`(404), `oidc_client_already_exists`(409), `bad_request`.
- SAML SPs: `GET /saml-providers`→`[]SAMLProviderView{id,entityId,displayName,kind?,nameIdFormat,requireSignedAuthnRequest,wantAssertionsSigned,allowIdpInitiated,sessionLifetimeSecs?,acs[],keys[],createdAt}` (list omits acs/keys); `GET /saml-providers/{id}` (full, `acs:[{binding,location,index,isDefault}]`, `keys:[{use,notAfter?}]`); `POST /saml-providers` metadata `{metadataXml,kind?,displayName?,entityId?,nameIdFormat?,requireSignedAuthnRequest?,allowIdpInitiated?,wantAssertionsSigned?,sessionLifetimeSecs?}` OR manual `{displayName,entityId,nameIdFormat,...flags,acs:[{binding,location,index,isDefault}],sessionLifetimeSecs?}`; `PUT /saml-providers/{id} {displayName,nameIdFormat,requireSignedAuthnRequest,wantAssertionsSigned,allowIdpInitiated,sessionLifetimeSecs?}`; `POST /saml-providers/{id}/reingest-metadata {metadataXml}`; `POST /saml-providers/delete {id}`→204. Errors: `saml_provider_already_exists`(409), `credential_not_found`(404, = SP not found), `bad_request`. Bindings: `urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST` / `…HTTP-Redirect`.
- Upstream IdPs (for the invite picker): `GET /upstream-idps`→`[]{slug,displayName,…,disabled}` (filter `disabled`).
- `InvitationView` does NOT currently include the federation slug — Section C adds it.

**Conventions that bite:**
- Frontend tooling from `dashboard/` with `mise exec -- npm …`; cwd resets to repo root between tool calls — `cd` explicitly.
- Binary embeds committed `pkg/webui/dist`; Vite hashes non-deterministic → source-only commits do `git checkout -- pkg/webui/dist && git clean -fq pkg/webui/dist` after a verify build; rebuild + commit dist once at the done-gate. (Reviewers' `npm run build` dirties dist — discard before next commit.)
- **en.ts Edit hazard:** after ANY en.ts edit run `grep -nP "\x{2018}" dashboard/src/locales/en.ts` (must be empty) + `grep -nP ":\s*\x{2019}" dashboard/src/locales/en.ts` (must be empty); in-text apostrophes curly U+2019.
- Tests don't mock `withSudo` for happy paths (let api resolve). `ConfirmDialog` confirm = `variant="destructive"` button (teleported to body); `clickConfirm(label)` = last destructive button with that label.
- Admin views are built standalone (mocked `api`/`vue-router`) then routed in Task 9. Section C's Go change is verified with `go test` + the smoke.

---

### Task 1: Vendor `Textarea` + all i18n

**Goal:** Add the `Textarea` primitive and the full i18n for all three sections.

**Files:**
- Create: `dashboard/src/components/ui/textarea/Textarea.vue`, `dashboard/src/components/ui/textarea/index.ts`
- Modify: `dashboard/src/locales/en.ts`

**Acceptance Criteria:**
- [ ] `@/components/ui/textarea` exports `Textarea` (token-styled, `bg-sunken`, v-model via `useVModel`).
- [ ] i18n blocks present: `recovery.*`, `admin.oidc.*`, `admin.saml.*`; `admin.nav.{oidcClients,samlProviders}`; `admin.invitations.{requireMethod,anyMethod,colMethod}`; `login.{lostAuthenticator,recoveryRestart}`; errors `client_not_found`, `oidc_client_already_exists`, `saml_provider_already_exists`.

**Verify:** `cd dashboard && mise exec -- npm run build` → clean; en.ts greps empty.

**Steps:**

- [ ] **Step 1: Vendor Textarea.** Create `dashboard/src/components/ui/textarea/Textarea.vue`:
```vue
<script setup lang="ts">
import type { HTMLAttributes } from "vue"
import { useVModel } from "@vueuse/core"
import { cn } from "@/lib/utils"

const props = defineProps<{
  defaultValue?: string | number
  modelValue?: string | number
  class?: HTMLAttributes["class"]
}>()
const emits = defineEmits<{ (e: "update:modelValue", payload: string | number): void }>()
const modelValue = useVModel(props, "modelValue", emits, { passive: true, defaultValue: props.defaultValue })
</script>
<template>
  <textarea
    v-model="modelValue"
    data-slot="textarea"
    :class="cn(
      'placeholder:text-muted-foreground bg-sunken border-input flex min-h-20 w-full rounded-md border px-3 py-2 text-sm shadow-xs transition-[color,box-shadow] outline-none',
      'focus-visible:border-ring focus-visible:ring-ring/50 focus-visible:ring-3',
      'disabled:pointer-events-none disabled:cursor-not-allowed disabled:opacity-50',
      props.class,
    )"
  />
</template>
```
`dashboard/src/components/ui/textarea/index.ts`:
```ts
export { default as Textarea } from "./Textarea.vue"
```

- [ ] **Step 2: Add i18n.** In `dashboard/src/locales/en.ts`:
  - In the `login:` block add: `lostAuthenticator: 'Lost your authenticator?',` and `recoveryRestart: 'We couldn’t verify that recovery code. Please sign in again to try another.',`.
  - Add a top-level `recovery:` block (after `login:`):
```ts
  recovery: {
    title: 'Recover your account',
    codeLabel: 'Recovery code',
    codeHint: 'Enter one of the backup codes you saved when you set up your authenticator.',
    verify: 'Verify code',
    reenrollTitle: 'Set up a new authenticator',
    reenrollHint: 'Scan this with your authenticator app (or enter the key), then enter the 6-digit code.',
    secretLabel: 'Setup key',
    codeInputLabel: 'Authenticator code',
    confirm: 'Confirm',
  },
```
  - In the `admin.nav` object add: `oidcClients: 'OIDC clients',` and `samlProviders: 'SAML providers',`.
  - In the `admin.invitations` object add: `requireMethod: 'Require sign-up via', anyMethod: 'Any method', colMethod: 'Method',`.
  - Add `admin.oidc` and `admin.saml` blocks inside the `admin:` object:
```ts
    oidc: {
      title: 'OIDC clients', create: 'Register client',
      colClient: 'Client', colType: 'Type', colState: 'State',
      confidential: 'Confidential', public: 'Public', active: 'Active', disabled: 'Disabled',
      empty: 'No OIDC clients registered.',
      clientId: 'Client ID', displayName: 'Display name',
      redirectUris: 'Redirect URIs', postLogoutUris: 'Post-logout redirect URIs',
      urisHint: 'One URI per line.',
      scopes: 'Scopes', publicClient: 'Public client (no secret)', requireConsent: 'Always show consent',
      secretReveal: 'Client secret — copy it now; it is shown only once.',
      back: 'Back to clients', notFound: 'That client no longer exists.',
      save: 'Save changes', saved: 'Saved.',
      rotate: 'Rotate secret', rotateConfirmTitle: 'Rotate the client secret?',
      rotateConfirmBody: 'The current secret stops working immediately. The app must be updated with the new secret.',
      dangerTitle: 'Danger zone', deleteHelp: 'Permanently delete this client. Apps using it will stop working.',
      delete: 'Delete client', deleteConfirmTitle: 'Delete this client?',
      deleteConfirmBody: 'This permanently removes the client. This cannot be undone.',
    },
    saml: {
      title: 'SAML providers', create: 'Register provider',
      colEntity: 'Entity ID', colName: 'Name', colIdpInit: 'IdP-initiated',
      yes: 'Yes', no: 'No', empty: 'No SAML providers registered.',
      modeMetadata: 'Paste metadata XML', modeManual: 'Enter manually',
      metadataXml: 'SP metadata XML', metadataHint: 'Paste the service provider’s SAML metadata; we extract the ACS endpoints and certificates.',
      displayName: 'Display name', entityId: 'Entity ID', nameIdFormat: 'NameID format',
      requireSignedAuthn: 'Require signed AuthnRequests', wantAssertionsSigned: 'Sign assertions', allowIdpInitiated: 'Allow IdP-initiated SSO',
      sessionLifetime: 'Session lifetime (seconds)',
      acs: 'Assertion Consumer Services', acsBinding: 'Binding', acsLocation: 'Location (ACS URL)', acsIndex: 'Index', acsDefault: 'Default', acsAdd: 'Add ACS endpoint', acsRemove: 'Remove',
      bindingPost: 'HTTP-POST', bindingRedirect: 'HTTP-Redirect',
      back: 'Back to providers', notFound: 'That provider no longer exists.',
      save: 'Save changes', saved: 'Saved.',
      reingest: 'Re-ingest metadata', reingestDone: 'Metadata re-ingested.',
      keysTitle: 'Signing certificates', acsTitle: 'Assertion Consumer Services',
      dangerTitle: 'Danger zone', deleteHelp: 'Permanently delete this provider and its endpoints and certificates.',
      delete: 'Delete provider', deleteConfirmTitle: 'Delete this provider?',
      deleteConfirmBody: 'This permanently removes the provider. This cannot be undone.',
    },
```
  - In the `errors:` object add: `client_not_found: 'That client no longer exists.',`, `oidc_client_already_exists: 'A client with that ID already exists.',`, `saml_provider_already_exists: 'A provider with that entity ID already exists.',`.

- [ ] **Step 3: Verify + commit.**
```bash
cd dashboard
grep -nP "\x{2018}" src/locales/en.ts && echo BAD || echo ok
grep -nP ":\s*\x{2019}" src/locales/en.ts && echo BAD || echo ok
mise exec -- npm run build
cd /home/tundra/projects/tundra/prohibitorum && git checkout -- pkg/webui/dist 2>/dev/null; git clean -fq pkg/webui/dist 2>/dev/null
git add dashboard/src/components/ui/textarea dashboard/src/locales/en.ts
git commit -m "feat(web): Textarea primitive + i18n for recovery/RP-mgmt/federated-invites

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: AccountRecovery + PasswordTotpForm link (Section A)

**Goal:** Inline recovery sub-flow on `/login`: recovery code → re-enroll TOTP → new codes → session.

**Files:**
- Create: `dashboard/src/components/custom/AccountRecovery.vue`, `dashboard/src/components/custom/AccountRecovery.test.ts`
- Modify: `dashboard/src/components/custom/PasswordTotpForm.vue`, `dashboard/src/components/custom/PasswordTotpForm.test.ts`

**Acceptance Criteria:**
- [ ] Recovery code → `POST /auth/recovery-code/verify` → on success advance + auto-`begin`; on failure emit `restart`.
- [ ] Re-enroll: `begin` shows `TotpQr` + secret; `verify` → shows new codes via `RecoveryCodesDisplay`; confirm → emit `success`.
- [ ] PasswordTotpForm's TOTP step shows a "Lost your authenticator?" control that mounts AccountRecovery; `restart` returns to the password step with a message.

**Verify:** `cd dashboard && mise exec -- npm run test -- AccountRecovery PasswordTotpForm` → pass.

**Steps:**

- [ ] **Step 1: Failing test.** Create `dashboard/src/components/custom/AccountRecovery.test.ts`:
```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'
vi.mock('qrcode', () => ({ default: { toDataURL: vi.fn(async () => 'data:image/png;base64,AAAA') } }))
const post = vi.mocked(api.post)
import AccountRecovery from './AccountRecovery.vue'

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
const mountC = () => mount(AccountRecovery, { props: { partialToken: 'pt_1' }, global: { plugins: [i18n()] }, attachTo: document.body })
beforeEach(() => { post.mockReset() })

describe('AccountRecovery', () => {
  it('verifies a recovery code, re-enrolls TOTP, shows new codes, emits success', async () => {
    post.mockImplementation(async (p: string) => {
      if (p.endsWith('/recovery-code/verify')) return { recovery_session_token: 'rs_1' }
      if (p.endsWith('/recovery/totp/begin')) return { secret_base32: 'ABCD', otpauth_uri: 'otpauth://x' }
      if (p.endsWith('/recovery/totp/verify')) return { recovery_codes: ['c1', 'c2'] }
      return undefined
    })
    const w = mountC()
    await w.find('input[name="recovery-code"]').setValue('backup-1')
    await w.find('[data-test="verify-code"]').trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/auth/recovery-code/verify', { partial_session_token: 'pt_1', code: 'backup-1' })
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/auth/recovery/totp/begin', { recovery_session_token: 'rs_1' })
    expect(w.text()).toContain('ABCD') // secret shown
    await w.find('input[name="reenroll-code"]').setValue('123456')
    await w.find('[data-test="confirm-reenroll"]').trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/auth/recovery/totp/verify', { recovery_session_token: 'rs_1', code: '123456' })
    expect(w.text()).toContain(en.recoveryCodes.heading) // RecoveryCodesDisplay heading
    // confirm save → success
    const confirmBtn = w.findAll('button').find((b) => b.text() === en.recoveryCodes.savedConfirm)!
    await confirmBtn.trigger('click'); await flushPromises()
    expect(w.emitted('success')).toBeTruthy()
  })
  it('emits restart when the recovery code is rejected', async () => {
    post.mockRejectedValue({ code: 'bad_credentials', message: 'zh' })
    const w = mountC()
    await w.find('input[name="recovery-code"]').setValue('wrong')
    await w.find('[data-test="verify-code"]').trigger('click'); await flushPromises()
    expect(w.emitted('restart')).toBeTruthy()
  })
})
```
Run `cd dashboard && mise exec -- npm run test -- AccountRecovery` → FAIL.

- [ ] **Step 2: Implement AccountRecovery.** Create `dashboard/src/components/custom/AccountRecovery.vue`:
```vue
<script setup lang="ts">
/**
 * AccountRecovery — inline recovery for password+TOTP accounts that lost their
 * authenticator. Driven from PasswordTotpForm's TOTP step with the
 * partial_session_token in hand. code → re-enroll TOTP → new recovery codes → success.
 * A failed recovery code spends the partial token, so failure emits 'restart'.
 */
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Alert, AlertDescription } from '@/components/ui/alert'
import TotpQr from '@/components/custom/TotpQr.vue'
import CodeField from '@/components/custom/CodeField.vue'
import RecoveryCodesDisplay from '@/components/custom/RecoveryCodesDisplay.vue'

const props = defineProps<{ partialToken: string }>()
const emit = defineEmits<{ success: []; restart: [] }>()

const { t, te } = useI18n()
const { busy, error, run } = useApi()

const phase = ref<'code' | 'reenroll' | 'done'>('code')
const recoveryCode = ref('')
const recoveryToken = ref('')
const otpauthUri = ref('')
const secret = ref('')
const totpCode = ref('')
const newCodes = ref<string[]>([])

const errorText = computed(() => {
  const e = error.value
  if (!e) return ''
  const key = `errors.${e.code}`
  return te(key) ? t(key) : e.message || t('common.error')
})

async function verifyCode(): Promise<void> {
  const res = await run(() => api.post<{ recovery_session_token: string }>(
    '/api/prohibitorum/auth/recovery-code/verify',
    { partial_session_token: props.partialToken, code: recoveryCode.value }))
  if (!res) { emit('restart'); return } // partial token is spent on any failure → restart
  recoveryToken.value = res.recovery_session_token
  phase.value = 'reenroll'
  await beginReenroll()
}
async function beginReenroll(): Promise<void> {
  const res = await run(() => api.post<{ secret_base32: string; otpauth_uri: string }>(
    '/api/prohibitorum/auth/recovery/totp/begin',
    { recovery_session_token: recoveryToken.value }))
  if (res) { secret.value = res.secret_base32; otpauthUri.value = res.otpauth_uri }
}
async function verifyReenroll(): Promise<void> {
  const res = await run(() => api.post<{ recovery_codes: string[] }>(
    '/api/prohibitorum/auth/recovery/totp/verify',
    { recovery_session_token: recoveryToken.value, code: totpCode.value }))
  if (res) { newCodes.value = res.recovery_codes; phase.value = 'done' }
}
</script>
<template>
  <div class="flex flex-col gap-4">
    <Alert v-if="errorText" variant="destructive" role="alert" aria-live="polite"><AlertDescription>{{ errorText }}</AlertDescription></Alert>

    <template v-if="phase === 'code'">
      <h2 class="text-base font-semibold text-ink">{{ t('recovery.title') }}</h2>
      <div class="flex flex-col gap-1.5">
        <Label for="recovery-code">{{ t('recovery.codeLabel') }}</Label>
        <Input id="recovery-code" name="recovery-code" v-model="recoveryCode" autocomplete="one-time-code" @keydown.enter.prevent="verifyCode" />
        <p class="text-sm text-muted">{{ t('recovery.codeHint') }}</p>
      </div>
      <Button type="button" class="w-full" :disabled="busy || !recoveryCode" data-test="verify-code" @click="verifyCode">{{ t('recovery.verify') }}</Button>
    </template>

    <template v-else-if="phase === 'reenroll'">
      <h2 class="text-base font-semibold text-ink">{{ t('recovery.reenrollTitle') }}</h2>
      <p class="text-sm text-muted">{{ t('recovery.reenrollHint') }}</p>
      <TotpQr v-if="otpauthUri" :uri="otpauthUri" :alt="t('recovery.reenrollTitle')" />
      <CodeField v-if="secret" :value="secret" :label="t('recovery.secretLabel')" />
      <div class="flex flex-col gap-1.5">
        <Label for="reenroll-code">{{ t('recovery.codeInputLabel') }}</Label>
        <Input id="reenroll-code" name="reenroll-code" v-model="totpCode" inputmode="numeric" autocomplete="one-time-code" pattern="[0-9]*" maxlength="8" @keydown.enter.prevent="verifyReenroll" />
      </div>
      <Button type="button" class="w-full" :disabled="busy || !totpCode" data-test="confirm-reenroll" @click="verifyReenroll">{{ t('recovery.confirm') }}</Button>
    </template>

    <template v-else>
      <RecoveryCodesDisplay :codes="newCodes" regenerated @confirmed="emit('success')" />
    </template>
  </div>
</template>
```
Run the test → both cases PASS.

- [ ] **Step 3: PasswordTotpForm link.** In `dashboard/src/components/custom/PasswordTotpForm.vue`:
  - Add imports: `import AccountRecovery from '@/components/custom/AccountRecovery.vue'`.
  - Add refs: `const recovering = ref(false)` and `const recoveryNote = ref('')`.
  - Add handler: `function onRecoveryRestart(): void { recovering.value = false; phase.value = 'password'; code.value = ''; recoveryNote.value = t('login.recoveryRestart') }`.
  - In `submitPassword()`, clear the note at the top: `recoveryNote.value = ''`.
  - In the template's password phase, show the note when set (above the username field): `<p v-if="recoveryNote" class="text-sm text-muted" role="status">{{ recoveryNote }}</p>`.
  - In the template's TOTP phase (`<template v-else>`), wrap the existing TOTP input block in `<template v-if="!recovering">` and after it add the recovery branch:
```vue
      <template v-if="!recovering">
        <!-- existing TOTP input block stays here unchanged -->
        <button type="button" class="text-left text-sm text-muted underline-offset-4 hover:underline" data-test="lost-authenticator" @click="recovering = true">
          {{ t('login.lostAuthenticator') }}
        </button>
      </template>
      <AccountRecovery v-else :partial-token="partialToken" @success="emit('success')" @restart="onRecoveryRestart" />
```
  Note: the "lost authenticator?" control is a `<button type="button">` (not a submit) inside the form; clicking it must NOT submit. Keep the Alert + submit Button rendering only when `!recovering` (move them inside the `!recovering` template, or guard with `v-if="!recovering"`), so the recovery sub-flow owns the UI while active.

- [ ] **Step 4: PasswordTotpForm test.** In `dashboard/src/components/custom/PasswordTotpForm.test.ts`, add a case after the existing TOTP-phase test:
```ts
  it('reveals the recovery sub-flow from the TOTP step', async () => {
    post.mockResolvedValueOnce({ partial_session_token: 'pt_123' })
    const w = mountForm()
    await w.find('input[name=username]').setValue('alex')
    await w.find('input[name=password]').setValue('hunter2')
    await w.find('form').trigger('submit'); await flushPromises()
    expect(w.find('[data-test="lost-authenticator"]').exists()).toBe(true)
    await w.find('[data-test="lost-authenticator"]').trigger('click')
    expect(w.find('input[name="recovery-code"]').exists()).toBe(true) // AccountRecovery mounted
  })
```
Run `cd dashboard && mise exec -- npm run test -- AccountRecovery PasswordTotpForm` → all pass.

- [ ] **Step 5: Build + discard dist + commit.**
```bash
cd dashboard && mise exec -- npm run build && cd /home/tundra/projects/tundra/prohibitorum && git checkout -- pkg/webui/dist 2>/dev/null; git clean -fq pkg/webui/dist 2>/dev/null
git add dashboard/src/components/custom/AccountRecovery.vue dashboard/src/components/custom/AccountRecovery.test.ts dashboard/src/components/custom/PasswordTotpForm.vue dashboard/src/components/custom/PasswordTotpForm.test.ts
git commit -m "feat(web): inline account recovery (recovery code → re-enroll TOTP) on /login

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: AdminOidcClientsView — list + create (Section B1)

**Goal:** OIDC clients list (Table) + a create form that reveals the secret once.

**Files:**
- Create: `dashboard/src/pages/admin/AdminOidcClientsView.vue`, `dashboard/src/pages/admin/AdminOidcClientsView.test.ts`

**Pattern:** Mirror the shipped `dashboard/src/pages/admin/AdminAccountsView.vue` (Table list, keyboard-activatable rows → `/admin/oidc-clients/{clientId}`, `useApi`/`errorText`, `StatusBadge`) and the inline-create idiom from `dashboard/src/pages/admin/AdminInvitationsView.vue` (header "Register client" button toggles an inline create `Card`; create is NOT a ConfirmDialog). **Read both before writing.**

**Acceptance Criteria:**
- [ ] `GET /oidc-clients` → Table (clientId, displayName, Type badge confidential/public from `tokenEndpointAuthMethod !== 'none'`, State badge). Row → `router.push('/admin/oidc-clients/{clientId}')`, keyboard-activatable.
- [ ] Inline create form (clientId, displayName, redirectUris Textarea [one/line], postLogoutRedirectUris Textarea, scopes checkboxes openid[checked,required]/profile/email, public toggle, requireConsent toggle) → `withSudo(POST /oidc-clients {…, scopes, public, requireConsent})` → on success reveal `secret` (confidential) in a `CodeField` with the `admin.oidc.secretReveal` note; refresh list.
- [ ] `oidc_client_already_exists` surfaced.

**Verify:** `cd dashboard && mise exec -- npm run test -- AdminOidcClientsView` → pass.

**Steps:**

- [ ] **Step 1: Failing test.** Create `dashboard/src/pages/admin/AdminOidcClientsView.test.ts`:
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
import AdminOidcClientsView from './AdminOidcClientsView.vue'

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
const mountView = () => mount(AdminOidcClientsView, { global: { plugins: [i18n()] }, attachTo: document.body })
const CLIENTS = [
  { clientId: 'web', displayName: 'Web App', redirectUris: ['https://w/cb'], postLogoutRedirectUris: [], allowedScopes: ['openid'], tokenEndpointAuthMethod: 'client_secret_basic', requireConsent: true, disabled: false, createdAt: '2026-01-01T00:00:00Z' },
  { clientId: 'spa', displayName: 'SPA', redirectUris: ['https://s/cb'], postLogoutRedirectUris: [], allowedScopes: ['openid'], tokenEndpointAuthMethod: 'none', requireConsent: false, disabled: false, createdAt: '2026-01-02T00:00:00Z' },
]
beforeEach(() => { get.mockReset(); post.mockReset(); push.mockReset() })

describe('AdminOidcClientsView', () => {
  it('lists clients with type badges', async () => {
    get.mockResolvedValue(CLIENTS)
    const w = mountView(); await flushPromises()
    expect(get).toHaveBeenCalledWith('/api/prohibitorum/oidc-clients')
    expect(w.text()).toContain('Web App'); expect(w.text()).toContain(en.admin.oidc.confidential); expect(w.text()).toContain(en.admin.oidc.public)
  })
  it('row click navigates to detail', async () => {
    get.mockResolvedValue(CLIENTS)
    const w = mountView(); await flushPromises()
    await w.find('[data-test="client-row-spa"]').trigger('click')
    expect(push).toHaveBeenCalledWith('/admin/oidc-clients/spa')
  })
  it('creates a confidential client and reveals the secret', async () => {
    get.mockResolvedValue([])
    post.mockResolvedValue({ clientId: 'new', secret: 's3cr3t', tokenEndpointAuthMethod: 'client_secret_basic' })
    const w = mountView(); await flushPromises()
    await w.find('[data-test="create"]').trigger('click')
    await w.find('input[name="clientId"]').setValue('new')
    await w.find('input[name="displayName"]').setValue('New')
    await w.find('textarea[name="redirectUris"]').setValue('https://n/cb')
    await w.find('[data-test="create-confirm"]').trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/oidc-clients', expect.objectContaining({
      clientId: 'new', displayName: 'New', redirectUris: ['https://n/cb'],
    }))
    expect(w.text()).toContain('s3cr3t')
  })
  it('surfaces oidc_client_already_exists', async () => {
    get.mockResolvedValue([])
    post.mockRejectedValue({ code: 'oidc_client_already_exists', message: 'zh' })
    const w = mountView(); await flushPromises()
    await w.find('[data-test="create"]').trigger('click')
    await w.find('input[name="clientId"]').setValue('web')
    await w.find('input[name="displayName"]').setValue('Dup')
    await w.find('textarea[name="redirectUris"]').setValue('https://w/cb')
    await w.find('[data-test="create-confirm"]').trigger('click'); await flushPromises()
    expect(w.text()).toContain(en.errors.oidc_client_already_exists)
  })
})
```
Run → FAIL.

- [ ] **Step 2: Implement.** Create `dashboard/src/pages/admin/AdminOidcClientsView.vue` mirroring `AdminAccountsView` (Table + keyboard rows) + `AdminInvitationsView` (inline create). Key implementation details (write complete code following those siblings):
  - Interface `OidcClient { clientId; displayName; redirectUris: string[]; postLogoutRedirectUris: string[]; allowedScopes: string[]; tokenEndpointAuthMethod: string; requireConsent: boolean; disabled: boolean; createdAt: string }`.
  - `load()` → `api.get<OidcClient[]>('/api/prohibitorum/oidc-clients')`.
  - Row: `:data-test="`client-row-${c.clientId}`"`, `tabindex="0"`, `@click`/`@keydown.enter`/`@keydown.space.prevent` → `go(c.clientId)` where `go = (id) => router.push(`/admin/oidc-clients/${id}`)`. Type badge: `c.tokenEndpointAuthMethod !== 'none' ? confidential(caution) : public(neutral)`. State badge: disabled→danger else success.
  - Inline create `Card v-if="createOpen"`: `clientId` Input, `displayName` Input, `redirectUris` Textarea (name="redirectUris"), `postLogoutRedirectUris` Textarea, scopes checkboxes (openid checked+disabled-required, profile, email — collect into `scopes` array), `public` checkbox, `requireConsent` checkbox. Create handler:
    ```ts
    const lines = (s: string) => s.split('\n').map((x) => x.trim()).filter(Boolean)
    async function create() {
      created.value = false
      const res = await run(() => withSudo(() => api.post<{ secret?: string }>('/api/prohibitorum/oidc-clients', {
        clientId: clientId.value, displayName: displayName.value,
        redirectUris: lines(redirectUris.value), postLogoutRedirectUris: lines(postLogoutUris.value),
        scopes: scopes.value, public: isPublic.value, requireConsent: requireConsent.value,
      })))
      if (res) { createOpen.value = false; revealedSecret.value = res.secret ?? ''; created.value = true; await load() }
    }
    ```
  - On success show, if `revealedSecret`, a `CodeField :value="revealedSecret"` + the `admin.oidc.secretReveal` note (public clients return no secret → show a "created" note only).
  - Buttons `data-test="create"`, `data-test="create-confirm"`, `data-test="create-cancel"`. `errorText` Alert. Empty state.
  Run the test → 4 cases PASS.

- [ ] **Step 3: Build + discard dist + commit.**
```bash
cd dashboard && mise exec -- npm run build && cd /home/tundra/projects/tundra/prohibitorum && git checkout -- pkg/webui/dist 2>/dev/null; git clean -fq pkg/webui/dist 2>/dev/null
git add dashboard/src/pages/admin/AdminOidcClientsView.vue dashboard/src/pages/admin/AdminOidcClientsView.test.ts
git commit -m "feat(web): AdminOidcClientsView — list + create (reveal-once secret)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: AdminOidcClientDetailView — edit / rotate-secret / delete (Section B1)

**Goal:** Per-client detail: edit config (PUT), rotate secret (reveal-once), delete.

**Files:**
- Create: `dashboard/src/pages/admin/AdminOidcClientDetailView.vue`, `dashboard/src/pages/admin/AdminOidcClientDetailView.test.ts`

**Pattern:** Mirror shipped `dashboard/src/pages/admin/AdminAccountDetailView.vue` (load by route param, Card sections, `withSudo` mutations, ConfirmDialog, danger zone, `clickConfirm` test helper, error Alert hoisted above the `v-else-if`).

**Acceptance Criteria:**
- [ ] `GET /oidc-clients/{clientId}` → form (displayName, redirectUris Textarea, postLogoutRedirectUris Textarea, scopes checkboxes seeded from `allowedScopes`, requireConsent toggle, disabled toggle). `client_not_found` → not-found.
- [ ] Save → `withSudo(PUT /oidc-clients/{clientId} {displayName, redirectUris, postLogoutRedirectUris, allowedScopes, requireConsent, disabled})` (**note `allowedScopes`**).
- [ ] Rotate secret → ConfirmDialog + `withSudo(POST /oidc-clients/rotate-secret {clientId})` → reveal new `secret` in CodeField.
- [ ] Delete → ConfirmDialog + `withSudo(POST /oidc-clients/delete {clientId})` → `router.push('/admin/oidc-clients')`.

**Verify:** `cd dashboard && mise exec -- npm run test -- AdminOidcClientDetailView` → pass.

**Steps:**

- [ ] **Step 1: Failing test.** Create `dashboard/src/pages/admin/AdminOidcClientDetailView.test.ts`:
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
vi.mock('vue-router', () => ({ useRouter: () => ({ push }), useRoute: () => ({ params: { clientId: 'web' } }) }))
import AdminOidcClientDetailView from './AdminOidcClientDetailView.vue'

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
const mountView = () => mount(AdminOidcClientDetailView, { global: { plugins: [i18n()], stubs: { RouterLink: { props: ['to'], template: '<a :href="to"><slot/></a>' } } }, attachTo: document.body })
const CLIENT = { clientId: 'web', displayName: 'Web App', redirectUris: ['https://w/cb'], postLogoutRedirectUris: [], allowedScopes: ['openid', 'profile'], tokenEndpointAuthMethod: 'client_secret_basic', requireConsent: true, disabled: false, createdAt: '2026-01-01T00:00:00Z' }
function clickConfirm(label: string) {
  const b = Array.from(document.body.querySelectorAll('button')).filter((x) => x.getAttribute('data-variant') === 'destructive' && x.textContent?.includes(label))
  b[b.length - 1]!.click()
}
beforeEach(() => { get.mockReset(); post.mockReset(); put.mockReset(); push.mockReset() })

describe('AdminOidcClientDetailView', () => {
  it('loads the client and saves config via PUT (allowedScopes)', async () => {
    get.mockResolvedValue(CLIENT); put.mockResolvedValue({ ...CLIENT, displayName: 'Renamed' })
    const w = mountView(); await flushPromises()
    expect(get).toHaveBeenCalledWith('/api/prohibitorum/oidc-clients/web')
    await w.find('input[name="displayName"]').setValue('Renamed')
    await w.find('[data-test="save"]').trigger('click'); await flushPromises()
    expect(put).toHaveBeenCalledWith('/api/prohibitorum/oidc-clients/web', expect.objectContaining({
      displayName: 'Renamed', allowedScopes: ['openid', 'profile'], requireConsent: true, disabled: false,
    }))
    expect(w.text()).toContain(en.admin.oidc.saved)
  })
  it('not found', async () => {
    get.mockRejectedValue({ code: 'client_not_found', message: 'zh' })
    const w = mountView(); await flushPromises()
    expect(w.text()).toContain(en.admin.oidc.notFound)
  })
  it('rotates the secret and reveals it', async () => {
    get.mockResolvedValue(CLIENT); post.mockResolvedValue({ clientId: 'web', secret: 'newsecret' })
    const w = mountView(); await flushPromises()
    await w.find('[data-test="rotate"]').trigger('click'); await flushPromises()
    clickConfirm(en.admin.oidc.rotate); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/oidc-clients/rotate-secret', { clientId: 'web' })
    expect(w.text()).toContain('newsecret')
  })
  it('deletes and navigates to the list', async () => {
    get.mockResolvedValue(CLIENT); post.mockResolvedValue(undefined)
    const w = mountView(); await flushPromises()
    await w.find('[data-test="delete"]').trigger('click'); await flushPromises()
    clickConfirm(en.admin.oidc.delete); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/oidc-clients/delete', { clientId: 'web' })
    expect(push).toHaveBeenCalledWith('/admin/oidc-clients')
  })
})
```
Run → FAIL.

- [ ] **Step 2: Implement** `AdminOidcClientDetailView.vue` mirroring `AdminAccountDetailView.vue`. `const clientId = String(route.params.clientId)`. Load `GET /oidc-clients/{clientId}` → seed `displayName`, `redirectUris`/`postLogoutUris` (join arrays with `\n` for the Textareas), `scopes` (from `allowedScopes`), `requireConsent`, `disabled`. Sections: **Config** (Save → PUT with `allowedScopes: scopes.value`, redirectUris/postLogout via `lines()`), **Secret** (Rotate → ConfirmDialog → `rotate-secret` → reveal `secret` in CodeField; only meaningful for confidential clients — show a note for public), **Danger zone** (Delete → ConfirmDialog → `delete` → push list). Error Alert hoisted above the `v-else-if="client"`. Run the test → 4 cases PASS.

- [ ] **Step 3: Build + discard dist + commit** (message: `feat(web): AdminOidcClientDetailView — edit, rotate-secret, delete`).

---

### Task 5: AdminSamlProvidersView — list + create (metadata + manual ACS) (Section B2)

**Goal:** SAML SP list + a create form with both a metadata-XML-paste mode and a manual ACS-rows mode.

**Files:**
- Create: `dashboard/src/pages/admin/AdminSamlProvidersView.vue`, `dashboard/src/pages/admin/AdminSamlProvidersView.test.ts`

**Pattern:** Mirror `AdminAccountsView` (Table + rows) + `AdminInvitationsView` (inline create). The create form has a mode toggle.

**Acceptance Criteria:**
- [ ] `GET /saml-providers` → Table (entityId, displayName, IdP-initiated badge). Row → `/admin/saml-providers/{id}`, keyboard-activatable.
- [ ] Create — mode toggle: **metadata** (`metadataXml` Textarea + optional displayName + flags → `POST /saml-providers {metadataXml, displayName?, requireSignedAuthnRequest, allowIdpInitiated, wantAssertionsSigned}`); **manual** (displayName, entityId, nameIdFormat, flags, repeatable ACS rows [binding select POST/Redirect + location + index + isDefault] → `POST /saml-providers {displayName, entityId, nameIdFormat, …flags, acs:[…]}`). Refresh on success.
- [ ] `saml_provider_already_exists` surfaced.

**Verify:** `cd dashboard && mise exec -- npm run test -- AdminSamlProvidersView` → pass.

**Steps:**

- [ ] **Step 1: Failing test.** Create `dashboard/src/pages/admin/AdminSamlProvidersView.test.ts`:
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
import AdminSamlProvidersView from './AdminSamlProvidersView.vue'

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
const mountView = () => mount(AdminSamlProvidersView, { global: { plugins: [i18n()] }, attachTo: document.body })
const SPS = [{ id: 1, entityId: 'https://sp/meta', displayName: 'GHES', nameIdFormat: 'persistent', requireSignedAuthnRequest: false, wantAssertionsSigned: true, allowIdpInitiated: true, acs: [], keys: [], createdAt: '2026-01-01T00:00:00Z' }]
beforeEach(() => { get.mockReset(); post.mockReset(); push.mockReset() })

describe('AdminSamlProvidersView', () => {
  it('lists providers', async () => {
    get.mockResolvedValue(SPS)
    const w = mountView(); await flushPromises()
    expect(get).toHaveBeenCalledWith('/api/prohibitorum/saml-providers')
    expect(w.text()).toContain('GHES'); expect(w.text()).toContain('https://sp/meta')
  })
  it('row click navigates to detail', async () => {
    get.mockResolvedValue(SPS)
    const w = mountView(); await flushPromises()
    await w.find('[data-test="sp-row-1"]').trigger('click')
    expect(push).toHaveBeenCalledWith('/admin/saml-providers/1')
  })
  it('creates via metadata paste', async () => {
    get.mockResolvedValue([]); post.mockResolvedValue({ id: 2 })
    const w = mountView(); await flushPromises()
    await w.find('[data-test="create"]').trigger('click')
    // metadata mode is default
    await w.find('textarea[name="metadataXml"]').setValue('<xml/>')
    await w.find('[data-test="create-confirm"]').trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/saml-providers', expect.objectContaining({ metadataXml: '<xml/>' }))
  })
  it('creates via manual ACS', async () => {
    get.mockResolvedValue([]); post.mockResolvedValue({ id: 3 })
    const w = mountView(); await flushPromises()
    await w.find('[data-test="create"]').trigger('click')
    await w.find('[data-test="mode-manual"]').trigger('click')
    await w.find('input[name="entityId"]').setValue('https://manual/sp')
    await w.find('input[name="displayName"]').setValue('Manual SP')
    await w.find('[data-test="acs-add"]').trigger('click')
    await w.find('input[name="acs-location-0"]').setValue('https://manual/acs')
    await w.find('[data-test="create-confirm"]').trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/saml-providers', expect.objectContaining({
      entityId: 'https://manual/sp', displayName: 'Manual SP',
      acs: [expect.objectContaining({ location: 'https://manual/acs' })],
    }))
  })
  it('surfaces saml_provider_already_exists', async () => {
    get.mockResolvedValue([]); post.mockRejectedValue({ code: 'saml_provider_already_exists', message: 'zh' })
    const w = mountView(); await flushPromises()
    await w.find('[data-test="create"]').trigger('click')
    await w.find('textarea[name="metadataXml"]').setValue('<xml/>')
    await w.find('[data-test="create-confirm"]').trigger('click'); await flushPromises()
    expect(w.text()).toContain(en.errors.saml_provider_already_exists)
  })
})
```
Run → FAIL.

- [ ] **Step 2: Implement** `AdminSamlProvidersView.vue`. List mirrors `AdminAccountsView`. Inline create `Card v-if="createOpen"` with a mode toggle (`mode: 'metadata' | 'manual'`, `data-test="mode-metadata"`/`data-test="mode-manual"` buttons):
  - **metadata mode:** `metadataXml` `Textarea` (name="metadataXml") + optional `displayName` Input + the three flag checkboxes.
  - **manual mode:** `displayName` Input, `entityId` Input (name="entityId"), `nameIdFormat` Input/select, the three flags, and a repeatable ACS editor: `acsRows = ref<{binding;location;index;isDefault}[]>([])`, `data-test="acs-add"` button pushes `{binding: POST_URN, location:'', index: acsRows.length, isDefault: acsRows.length===0}`; each row renders a binding `<select>`, a location Input (name=`acs-location-{i}`), an index Input, an isDefault radio/checkbox, and a `data-test="acs-remove-{i}"` button.
  - The binding URN constants: `const POST_URN = 'urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST'`, `const REDIRECT_URN = 'urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect'`.
  - `create()`: build the body by mode — metadata: `{metadataXml, displayName: displayName || undefined, requireSignedAuthnRequest, wantAssertionsSigned, allowIdpInitiated}`; manual: `{displayName, entityId, nameIdFormat, requireSignedAuthnRequest, wantAssertionsSigned, allowIdpInitiated, acs: acsRows.value}`. `withSudo(POST /saml-providers, body)` → on success refresh + "created" note.
  Run the test → 5 cases PASS.

- [ ] **Step 3: Build + discard dist + commit** (message: `feat(web): AdminSamlProvidersView — list + create (metadata + manual ACS)`).

---

### Task 6: AdminSamlProviderDetailView — edit / reingest / delete (Section B2)

**Goal:** Per-SP detail: edit flags (PUT), re-ingest metadata, view ACS/keys, delete.

**Files:**
- Create: `dashboard/src/pages/admin/AdminSamlProviderDetailView.vue`, `dashboard/src/pages/admin/AdminSamlProviderDetailView.test.ts`

**Pattern:** Mirror `AdminAccountDetailView.vue`.

**Acceptance Criteria:**
- [ ] `GET /saml-providers/{id}` → shows entityId (read-only), editable form (displayName, nameIdFormat, requireSignedAuthnRequest, wantAssertionsSigned, allowIdpInitiated, sessionLifetimeSecs), and read-only **ACS** + **certificates** lists. `credential_not_found` → not-found.
- [ ] Save → `withSudo(PUT /saml-providers/{id} {displayName, nameIdFormat, requireSignedAuthnRequest, wantAssertionsSigned, allowIdpInitiated, sessionLifetimeSecs?})`.
- [ ] Re-ingest → a `Textarea` + button → `withSudo(POST /saml-providers/{id}/reingest-metadata {metadataXml})` → refresh, "re-ingested" note.
- [ ] Delete → ConfirmDialog + `withSudo(POST /saml-providers/delete {id})` → `router.push('/admin/saml-providers')`.

**Verify:** `cd dashboard && mise exec -- npm run test -- AdminSamlProviderDetailView` → pass.

**Steps:**

- [ ] **Step 1: Failing test.** Create `dashboard/src/pages/admin/AdminSamlProviderDetailView.test.ts` (route param `{ id: '5' }`; `GET /saml-providers/5` → an SP with `acs:[{binding,location,index,isDefault}]`, `keys:[{use,notAfter}]`):
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
vi.mock('vue-router', () => ({ useRouter: () => ({ push }), useRoute: () => ({ params: { id: '5' } }) }))
import AdminSamlProviderDetailView from './AdminSamlProviderDetailView.vue'

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
const mountView = () => mount(AdminSamlProviderDetailView, { global: { plugins: [i18n()], stubs: { RouterLink: { props: ['to'], template: '<a :href="to"><slot/></a>' } } }, attachTo: document.body })
const SP = { id: 5, entityId: 'https://sp/meta', displayName: 'GHES', nameIdFormat: 'persistent', requireSignedAuthnRequest: false, wantAssertionsSigned: true, allowIdpInitiated: true, sessionLifetimeSecs: 3600, acs: [{ binding: 'urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST', location: 'https://sp/acs', index: 0, isDefault: true }], keys: [{ use: 'signing', notAfter: '2027-01-01T00:00:00Z' }], createdAt: '2026-01-01T00:00:00Z' }
function clickConfirm(label: string) { const b = Array.from(document.body.querySelectorAll('button')).filter((x) => x.getAttribute('data-variant') === 'destructive' && x.textContent?.includes(label)); b[b.length - 1]!.click() }
beforeEach(() => { get.mockReset(); post.mockReset(); put.mockReset(); push.mockReset() })

describe('AdminSamlProviderDetailView', () => {
  it('loads the SP, shows ACS, saves flags via PUT', async () => {
    get.mockResolvedValue(SP); put.mockResolvedValue({ ...SP, displayName: 'GHES 2' })
    const w = mountView(); await flushPromises()
    expect(get).toHaveBeenCalledWith('/api/prohibitorum/saml-providers/5')
    expect(w.text()).toContain('https://sp/acs')
    await w.find('input[name="displayName"]').setValue('GHES 2')
    await w.find('[data-test="save"]').trigger('click'); await flushPromises()
    expect(put).toHaveBeenCalledWith('/api/prohibitorum/saml-providers/5', expect.objectContaining({ displayName: 'GHES 2', allowIdpInitiated: true }))
    expect(w.text()).toContain(en.admin.saml.saved)
  })
  it('not found', async () => {
    get.mockRejectedValue({ code: 'credential_not_found', message: 'zh' })
    const w = mountView(); await flushPromises()
    expect(w.text()).toContain(en.admin.saml.notFound)
  })
  it('re-ingests metadata', async () => {
    get.mockResolvedValue(SP); post.mockResolvedValue(SP)
    const w = mountView(); await flushPromises()
    await w.find('textarea[name="reingestXml"]').setValue('<xml2/>')
    await w.find('[data-test="reingest"]').trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/saml-providers/5/reingest-metadata', { metadataXml: '<xml2/>' })
  })
  it('deletes and navigates to the list', async () => {
    get.mockResolvedValue(SP); post.mockResolvedValue(undefined)
    const w = mountView(); await flushPromises()
    await w.find('[data-test="delete"]').trigger('click'); await flushPromises()
    clickConfirm(en.admin.saml.delete); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/saml-providers/delete', { id: 5 })
    expect(push).toHaveBeenCalledWith('/admin/saml-providers')
  })
})
```
Run → FAIL.

- [ ] **Step 2: Implement** `AdminSamlProviderDetailView.vue` mirroring `AdminAccountDetailView.vue`. `const id = Number(route.params.id)`. Load → seed form. Sections: **Config** (entityId read-only; editable displayName/nameIdFormat/flags/sessionLifetime → PUT; `sessionLifetimeSecs` sent as a number or omitted if blank), **ACS** (read-only list of `acs` rows), **Certificates** (read-only `keys` list: use + notAfter via `formatDateTime`), **Re-ingest metadata** (`Textarea` name="reingestXml" → reingest-metadata → refresh + note), **Danger zone** (Delete → ConfirmDialog → delete → push list). Error Alert hoisted above `v-else-if`. `credential_not_found` → notFound. Run the test → 4 cases PASS.

- [ ] **Step 3: Build + discard dist + commit** (message: `feat(web): AdminSamlProviderDetailView — edit, reingest, delete`).

---

### Task 7: Federated invites — backend (Section C)

**Goal:** Let the create-invitation endpoint bind an invite to an upstream IdP, and surface the binding in the list.

**Files:**
- Modify: `pkg/contract/auth.go` (createInvitation input body + `InvitationView`), `pkg/server/handle_account.go` (pass slug into template; populate view)
- Test: `pkg/server/handle_account_test.go` (or the existing invitations test file)

**Acceptance Criteria:**
- [ ] `POST /invitations` accepts an optional `expectedUpstreamIdpSlug` and writes it into the enrollment.
- [ ] `GET /invitations` items include `expectedUpstreamIdpSlug` (omitempty) when set.
- [ ] `go build ./... && go vet ./...` exit 0; a Go test proves a slug-bound invite round-trips.

**Verify:** `cd /home/tundra/projects/tundra/prohibitorum && mise exec -- go test ./pkg/server/ -run Invitation` → pass.

**Steps:**

- [ ] **Step 1: Read** `pkg/server/handle_account.go` (`createInvitationIn`, `handleCreateInvitation`, `handleListInvitations`) and `pkg/contract/auth.go` (`InvitationView`, the create-invitation request struct). Find the existing invitations test for the pattern.
- [ ] **Step 2: Contract.** In `pkg/contract/auth.go`: add `ExpectedUpstreamIdpSlug *string` json `expectedUpstreamIdpSlug,omitempty` to `InvitationView`. (The create request body type lives in `handle_account.go`'s `createInvitationIn` — see next step.)
- [ ] **Step 3: Handler.** In `pkg/server/handle_account.go`:
  - Add to `createInvitationIn.Body`: `ExpectedUpstreamIdpSlug *string \`json:"expectedUpstreamIdpSlug,omitempty"\``.
  - In `handleCreateInvitation`, set `tpl.ExpectedUpstreamIDPSlug = in.Body.ExpectedUpstreamIdpSlug` (the `EnrollmentTemplate` field already exists and `IssueEnrollment` writes it). If a non-nil/non-empty slug is provided, optionally validate it exists via `s.queries` (look up `GetUpstreamIDP`/equivalent); if validation is non-trivial, skip it (the federation begin will reject an unknown slug) — keep this task minimal.
  - In `handleListInvitations`, populate `ExpectedUpstreamIdpSlug` from the enrollment row's `ExpectedUpstreamIdpSlug` pgtype.Text (`if r.ExpectedUpstreamIdpSlug.Valid { s := r.ExpectedUpstreamIdpSlug.String; view.ExpectedUpstreamIdpSlug = &s }`).
- [ ] **Step 4: Go test.** Add a test (mirror the existing invitations handler test) that creates an invitation with `ExpectedUpstreamIdpSlug` set and asserts the persisted enrollment carries it (and the list view returns it). Run `mise exec -- go test ./pkg/server/ -run Invitation` → pass. Then `mise exec -- go build ./... && go vet ./...` → exit 0.
- [ ] **Step 5: Commit.**
```bash
cd /home/tundra/projects/tundra/prohibitorum
git add pkg/contract/auth.go pkg/server/handle_account.go pkg/server/handle_account_test.go
git commit -m "feat(api): create-invitation accepts expectedUpstreamIdpSlug; list returns it

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```
(If the invitations test lives in a different file, adjust the `git add` path.)

---

### Task 8: Federated invites — frontend (Section C)

**Goal:** The invitations create form gains an optional "Require sign-up via [upstream IdP]" select; the list shows the bound IdP.

**Files:**
- Modify: `dashboard/src/pages/admin/AdminInvitationsView.vue`, `dashboard/src/pages/admin/AdminInvitationsView.test.ts`

**Acceptance Criteria:**
- [ ] On open, the create form loads `GET /upstream-idps` and renders a select (default "Any method" + each non-disabled IdP's displayName, value = slug).
- [ ] Create includes `expectedUpstreamIdpSlug` only when an IdP is chosen.
- [ ] The list gains a Method column showing the bound IdP's displayName (or "—").

**Verify:** `cd dashboard && mise exec -- npm run test -- AdminInvitationsView` → pass.

**Steps:**

- [ ] **Step 1: Extend the test.** In `dashboard/src/pages/admin/AdminInvitationsView.test.ts`:
  - The list `get` mock must now answer both `/invitations` and `/upstream-idps`. Update the existing `get.mockResolvedValue(...)` calls to a `get.mockImplementation((p) => p.includes('/upstream-idps') ? IDPS : INVITES)` where `const IDPS = [{ slug: 'okta', displayName: 'Okta', disabled: false }]` and INVITES items may include `expectedUpstreamIdpSlug`.
  - Add a test:
```ts
  it('creates a federation-bound invitation when an IdP is chosen', async () => {
    get.mockImplementation(async (p: string) => p.includes('/upstream-idps') ? [{ slug: 'okta', displayName: 'Okta', disabled: false }] : [])
    post.mockResolvedValue({ url: 'https://x/enroll/n', expiresAt: '2026-06-10T00:00:00Z' })
    const w = mountView(); await flushPromises()
    await w.find('[data-test="create"]').trigger('click'); await flushPromises()
    await w.find<HTMLSelectElement>('select[name="idp"]').setValue('okta')
    await w.find('[data-test="create-confirm"]').trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/invitations', { role: 'user', expectedUpstreamIdpSlug: 'okta' })
  })
```
  - Confirm the existing "creates an invitation then refreshes" test still passes (no IdP chosen → body is just `{ role }`, no `expectedUpstreamIdpSlug` key). Keep that assertion as `{ role: 'admin' }` (the picker defaults to "Any method").
- [ ] **Step 2: Implement.** In `AdminInvitationsView.vue`:
  - Add `interface Idp { slug: string; displayName: string; disabled: boolean }`, `const idps = ref<Idp[]>([])`, `const newIdp = ref('')` (empty = any method).
  - In `load()` (or when opening create), fetch idps: `idps.value = (await api.get<Idp[]>('/api/prohibitorum/upstream-idps')).filter((i) => !i.disabled)` (guard with try/catch → `[]` so a federation-less deployment still works).
  - In the create form, add a `<select name="idp" v-model="newIdp">` with a default `<option value="">{{ t('admin.invitations.anyMethod') }}</option>` + `v-for` over `idps`.
  - `create()`: build body `const body: Record<string, unknown> = { role: newRole.value }; if (newIdp.value) body.expectedUpstreamIdpSlug = newIdp.value;` → `withSudo(() => api.post('/api/prohibitorum/invitations', body))`.
  - Add `expectedUpstreamIdpSlug?: string` to the `Invitation` interface; add a **Method** column to the table showing the matching IdP displayName (look up in `idps`) or "—".
  Run the test → all (existing + 2 new) PASS.
- [ ] **Step 3: Build + discard dist + commit** (message: `feat(web): federation-bound invitations — IdP picker + method column`).

---

### Task 9: Wire routes + admin sidebar

**Goal:** Route the four new admin pages and add their sidebar items.

**Files:**
- Modify: `dashboard/src/router/index.ts`, `dashboard/src/components/custom/AppSidebar.vue`, `dashboard/src/components/custom/AppSidebar.test.ts`

**Acceptance Criteria:**
- [ ] `/admin/oidc-clients`, `/admin/oidc-clients/:clientId`, `/admin/saml-providers`, `/admin/saml-providers/:id` resolve as `requiresAdmin` children of `DashboardLayout`.
- [ ] Admin sidebar group shows (admins only): Accounts · Invitations · OIDC clients · SAML providers.

**Verify:** `cd dashboard && mise exec -- npm run test -- AppSidebar` → pass; `mise exec -- npm run build` → clean.

**Steps:**

- [ ] **Step 1:** In `dashboard/src/router/index.ts`, add four children after the existing `admin/invitations` child:
```ts
      { path: 'admin/oidc-clients', name: 'admin-oidc-clients', component: () => import('../pages/admin/AdminOidcClientsView.vue'), meta: { requiresAdmin: true } },
      { path: 'admin/oidc-clients/:clientId', name: 'admin-oidc-client-detail', component: () => import('../pages/admin/AdminOidcClientDetailView.vue'), meta: { requiresAdmin: true } },
      { path: 'admin/saml-providers', name: 'admin-saml-providers', component: () => import('../pages/admin/AdminSamlProvidersView.vue'), meta: { requiresAdmin: true } },
      { path: 'admin/saml-providers/:id', name: 'admin-saml-provider-detail', component: () => import('../pages/admin/AdminSamlProviderDetailView.vue'), meta: { requiresAdmin: true } },
```
- [ ] **Step 2:** In `dashboard/src/components/custom/AppSidebar.vue`, add icons to the lucide import (`AppWindow`, `Building2` — verify exports at build; fall back to `Boxes`/`Shield` if missing) and extend `adminItems`:
```ts
const adminItems = computed(() => [
  { to: '/admin/accounts', label: t('admin.nav.accounts'), icon: Users },
  { to: '/admin/invitations', label: t('admin.nav.invitations'), icon: Ticket },
  { to: '/admin/oidc-clients', label: t('admin.nav.oidcClients'), icon: AppWindow },
  { to: '/admin/saml-providers', label: t('admin.nav.samlProviders'), icon: Building2 },
])
```
- [ ] **Step 3:** In `dashboard/src/components/custom/AppSidebar.test.ts`, add `/admin/oidc-clients` + `/admin/saml-providers` to `makeRouter()` routes, and extend the "renders the admin group only for admins" test to also assert `expect(links).toContain('/admin/oidc-clients')` and `toContain('/admin/saml-providers')`. Run `cd dashboard && mise exec -- npm run test -- AppSidebar` → pass.
- [ ] **Step 4:** `cd dashboard && mise exec -- npm run build` → clean (verifies the four lazy imports + icon exports). Discard dist; commit (message: `feat(web): wire OIDC/SAML admin routes + sidebar items`).

---

### Task 10: Done-gate — full suite, Go, smoke, commit dist

**Goal:** Prove the whole cycle green and commit the rebuilt embed.

**Files:** Modify `pkg/webui/dist/**`.

**Acceptance Criteria:**
- [ ] Full vitest passes; `go build ./... && go vet ./...` exit 0; smoke `SMOKE_EXIT=0`; dist rebuilt + committed.

**Steps:**
- [ ] **Step 1:** `cd dashboard && mise exec -- npm run test` → all suites pass (prior 121 + recovery + 4 admin views + AppSidebar/InvitationsView additions). No `tail`-piping in a gating chain.
- [ ] **Step 2:** `cd dashboard && mise exec -- npm run build` → clean.
- [ ] **Step 3:** `cd /home/tundra/projects/tundra/prohibitorum && mise exec -- go build ./... && mise exec -- go vet ./...` → exit 0 (Section C touched Go).
- [ ] **Step 4:** Smoke — `setsid bash /tmp/run_v06.sh`; poll `/tmp/v06.result` for `SMOKE_EXIT=0`. NEVER bare `pkill -f 'prohibitorum'`. If absent, locate the runner or report.
- [ ] **Step 5:** Rebuild + commit dist:
```bash
cd /home/tundra/projects/tundra/prohibitorum/dashboard && mise exec -- npm run build
cd /home/tundra/projects/tundra/prohibitorum && git add pkg/webui/dist
git commit -m "build(web): rebuild embedded dist for recovery + RP-mgmt + federated invites

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```
- [ ] **Step 6:** Confirm `git status` clean; report gate evidence (vitest count, go exit 0, SMOKE_EXIT=0).

---

## Self-Review notes (author)

- **Spec coverage:** Section A → Task 2 (+ Textarea/i18n in 1); Section B1 → Tasks 3–4; Section B2 → Tasks 5–6; Section C → Tasks 7 (backend) + 8 (frontend); routing/sidebar → Task 9; done-gate → Task 10. All spec sections mapped.
- **Contract fidelity:** endpoints + field names taken from the verified `pkg/contract/auth.go` shapes; the create-vs-PUT `scopes`/`allowedScopes` split is called out explicitly (Tasks 3 vs 4); SAML binding URNs given; `InvitationView` slug addition is in Task 7.
- **Type consistency:** `lines()` helper, `clickConfirm(label)` helper, the `withSudo` wrapping, and the `data-test`/route-path conventions match across tasks and the shipped 3a siblings the implementers read.
- **Ordering:** views are built standalone (Tasks 2–8) before routes/sidebar (Task 9); Section C backend (7) precedes its frontend (8); dist committed once (10). No lazy-import points at a missing file mid-plan.
- **Risks (flagged, not placeholders):** lucide icon exports (`AppWindow`/`Building2`) — Task 9 build verifies, fallbacks named; the SAML manual-ACS repeatable editor is the most intricate new UI (Task 5) — its test pins the body shape; Section C optional slug-validation is deliberately minimal (federation begin rejects unknown slugs).
- **Patterned-view note:** Tasks 3–6 give complete authoritative tests + exact contracts + explicit deltas and name the shipped sibling to mirror, rather than re-deriving full component code that duplicates `AdminAccountsView`/`AdminAccountDetailView`/`AdminInvitationsView`. Implementers read those files; the per-task two-stage review covers correctness.
