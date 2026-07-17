# VRChat Linking UX Polish Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Polish the shared error surface and VRChat linking journey so errors typeset cleanly, users can find their profile, proof steps are obvious, public visitors understand bio links, and VRChat login uses recognizable branding.

**Architecture:** Keep existing APIs and state machines intact. Refine the shared `ErrorPanel`, reshape the two existing `FederationFlowView` states, enrich the static `VRChatProofView`, and add one protocol-specific login button beside the existing Steam button. English and Simplified Chinese copy remain structurally identical.

**Tech Stack:** Vue 3, TypeScript, Tailwind CSS v4, reka-ui primitives, lucide-vue-next, vue-i18n, Vitest, Vue Test Utils, Vite, mise.

## Global Constraints

- Frontend-only changes. Federation APIs, proof behavior, account linking, token privacy, and provider configuration remain unchanged.
- The public proof page makes no API request and renders identical content for valid, expired, malformed, and unknown proof tokens.
- The linking flow never requests or displays VRChat passwords, verification codes, cookies, or operator credentials.
- Error details contain only the existing public code, curated public fields, and optional request ID.
- English and Simplified Chinese locale structure remains in parity.
- All controls remain keyboard reachable with visible focus; error disclosure IDs are unique per component instance.
- Layouts must not overflow at a 390px viewport.
- VRChat button colors are `#00A2E8` background and `#0B1A21` foreground. Do not use white text on VRChat blue.
- Preserve Steam and generic OIDC login-button behavior.
- Do not stage or commit the unrelated root `package-lock.json`.

## File Responsibilities

- `dashboard/src/components/custom/ErrorPanel.vue`: shared error summary, actions, semantic details, and diagnostics layout.
- `dashboard/src/components/custom/ErrorPanel.test.ts`: error-code disclosure, unique IDs, semantics, and retained behavior.
- `dashboard/src/pages/FederationFlowView.vue`: VRChat identify and proof screen hierarchy without API changes.
- `dashboard/src/pages/FederationFlowView.test.ts`: visible guidance, profile action, ordered proof flow, expiry, and existing state behavior.
- `dashboard/src/pages/VRChatProofView.vue`: guest-first public explanation and owner instructions.
- `dashboard/src/pages/VRChatProofView.test.ts`: static privacy behavior, configured instance name, visitor copy, and owner sequence.
- `dashboard/src/components/custom/VRChatButton.vue`: protocol-specific branded login control.
- `dashboard/src/assets/vrchat-logo.svg`: built-in predefined VRChat mark.
- `dashboard/src/components/custom/FederationButtons.vue`: protocol routing to Steam, VRChat, or generic controls.
- `dashboard/src/components/custom/FederationButtons.test.ts`: branded routing and generic-provider regression coverage.
- `dashboard/src/locales/en.ts`, `dashboard/src/locales/zh.ts`: all new visible copy.
- `pkg/webui/dist/**`: generated embedded dashboard bundle, refreshed only after source verification.

---

### Task 1: Structured shared error panel

**Files:**
- Modify: `dashboard/src/components/custom/ErrorPanel.vue`
- Test: `dashboard/src/components/custom/ErrorPanel.test.ts`

**Interfaces:**
- Consumes: existing `ApiError`, `codeDefinition`, `localizedDetailEntries`, `Alert`, and diagnostic API.
- Produces: unchanged component props/events with `data-test="error-code"`, `data-test="error-actions"`, and a unique details content ID.

- [ ] **Step 1: Write failing behavior and structure tests**

Add or update focused tests to require the selected design:

```ts
it('always discloses the public error code in semantic details', async () => {
  const w = mount(ErrorPanel, {
    props: { error: { code: 'bad_request' } },
    global: { plugins: [makeI18n()] },
  })
  const trigger = w.get('[data-test="error-details-trigger"]')
  expect(trigger.exists()).toBe(true)
  await trigger.trigger('click')
  await nextTick()

  expect(w.get('[data-test="error-details"]').element.tagName).toBe('DL')
  expect(w.get('[data-test="error-code"]').text()).toBe('bad_request')
  expect(w.get('[data-test="error-code"]').element.tagName).toBe('DD')
  expect(w.get('[data-test="error-code-label"]').text()).toBe(en.errors.diagnosticField_code)
})

it('uses unique disclosure ids for multiple panels', () => {
  const host = defineComponent({
    components: { ErrorPanel },
    template: `
      <div>
        <ErrorPanel :error="{ code: 'bad_request' }" />
        <ErrorPanel :error="{ code: 'forbidden' }" />
      </div>
    `,
  })
  const w = mount(host, { global: { plugins: [makeI18n()] } })
  const controls = w.findAll('[data-test="error-details-trigger"]').map((node) => node.attributes('aria-controls'))
  expect(new Set(controls).size).toBe(2)
})

it('positions dismiss independently and groups secondary actions', () => {
  const w = mount(ErrorPanel, {
    props: { error: KNOWN_ERROR, isAdmin: true },
    global: { plugins: [makeI18n()] },
  })
  expect(w.get('[data-test="error-dismiss"]').classes()).toEqual(
    expect.arrayContaining(['absolute', 'top-0', '-end-1', 'size-11']),
  )
  expect(w.get('[data-test="error-actions"]').find('[data-test="error-details-trigger"]').exists()).toBe(true)
  expect(w.get('[data-test="error-actions"]').find('[data-test="error-diagnostic"]').exists()).toBe(true)
})
```

Import `defineComponent` from Vue in the test. Replace the old assertion that every secondary control has `min-h-11`: dismiss and recovery retain 44px targets; details, diagnostic, and request-ID copy use compact 32px controls, which remain above WCAG 2.2's 24px minimum.

- [ ] **Step 2: Run the focused RED tests**

Run from `dashboard/`:

```bash
npm test -- --run src/components/custom/ErrorPanel.test.ts -t 'public error code|unique disclosure|positions dismiss'
```

Expected: FAIL because the code is not rendered, the ID is fixed, and actions are in unrelated blocks.

- [ ] **Step 3: Add shared canonical details and unique IDs**

Import `useId` and create an instance-specific content ID:

```ts
import { ref, computed, watch, onBeforeUnmount, getCurrentInstance, useId } from 'vue'

const detailsContentId = `error-details-${useId()}`
```

The details trigger always renders while `hasError` is true. Its `aria-controls` points to `detailsContentId`. The expanded semantic block begins with the public code:

```vue
<dl
  v-if="detailsOpen"
  :id="detailsContentId"
  data-test="error-details"
  class="mt-2 grid min-w-0 grid-cols-[max-content_minmax(0,1fr)] gap-x-3 gap-y-2 border-t border-destructive/15 pt-3 text-xs"
>
  <dt data-test="error-code-label" class="font-medium text-muted">
    {{ t('errors.diagnosticField_code') }}
  </dt>
  <dd data-test="error-code" class="break-all font-mono text-ink">
    {{ error?.code }}
  </dd>

  <template v-for="entry in detailEntries" :key="entry.field">
    <dt class="font-medium text-muted">{{ t(entry.labelKey) }}</dt>
    <dd class="min-w-0 break-words text-ink">
      {{ entry.reasonKey && te(entry.reasonKey)
        ? t(entry.reasonKey)
        : Array.isArray(entry.value) ? entry.value.join(', ') : String(entry.value) }}
    </dd>
  </template>

  <template v-if="showRequestId">
    <dt class="font-medium text-muted">{{ t('errors.requestId') }}</dt>
    <dd data-test="error-request-id-row" class="flex min-w-0 items-center gap-1">
      <code data-test="error-request-id" class="min-w-0 flex-1 break-all font-mono text-ink">{{ error?.requestId }}</code>
      <button
        type="button"
        data-test="error-copy-request-id"
        class="inline-flex size-8 shrink-0 items-center justify-center rounded text-muted hover:text-ink focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        :aria-label="t('errors.copyRequestId')"
        @click="copyRequestId"
      >
        <Copy class="size-3.5" aria-hidden="true" />
      </button>
    </dd>
  </template>
</dl>
```

- [ ] **Step 4: Implement the selected structured panel layout**

Use `Alert` only as the semantic/error-color shell. Replace the current template hierarchy with:

```vue
<Alert
  v-if="hasError"
  variant="destructive"
  role="alert"
  aria-live="polite"
  class="relative flex flex-col gap-0 border-destructive/25 bg-destructive/[0.05] px-4 py-3.5 pe-12"
>
  <AlertDescription data-test="error-summary" class="leading-5 text-destructive">
    {{ message }}
  </AlertDescription>

  <button
    v-if="dismissible"
    type="button"
    data-test="error-dismiss"
    class="absolute -end-1 top-0 inline-flex size-11 items-center justify-center rounded text-destructive hover:text-destructive/80 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
    :aria-label="t('errors.dismiss')"
    @click="onDismiss"
  >
    <X class="size-4" aria-hidden="true" />
  </button>

  <div v-if="showRecoveryGuidance" class="mt-2">
    <span v-if="!showRecoveryButton" class="text-xs leading-4 text-muted">{{ recoveryLabel }}</span>
    <Button v-else type="button" data-test="error-recovery" variant="outline" size="sm" class="min-h-11" @click="onRecovery">
      <component :is="recoveryIcon" class="size-3.5" aria-hidden="true" />
      {{ recoveryLabel }}
    </Button>
  </div>

  <div data-test="error-actions" class="mt-3 flex min-h-8 flex-wrap items-center gap-x-2 gap-y-1">
    <button
      type="button"
      data-test="error-details-trigger"
      class="inline-flex h-8 items-center gap-1 rounded px-1 text-xs font-medium text-destructive/85 hover:text-destructive focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      :aria-expanded="detailsOpen"
      :aria-controls="detailsContentId"
      @click="detailsOpen = !detailsOpen"
    >
      <ChevronDown class="size-3.5 transition-transform" :class="{ 'rotate-180': detailsOpen }" aria-hidden="true" />
      {{ t('errors.detailsLabel') }}
    </button>
    <Button v-if="showDiagnostic" type="button" data-test="error-diagnostic" variant="ghost" size="sm" class="h-8 px-2 text-xs" @click="fetchDiagnostic">
      <Stethoscope class="size-3.5" aria-hidden="true" />
      {{ t('errors.diagnostic') }}
    </Button>
  </div>

  <!-- semantic details block from Step 3 -->
  <!-- existing diagnostic states, each with mt-3 and no nested arbitrary mt-2 chain -->
</Alert>
```

Render the loaded diagnostic record as a `dl` with the same label/value grid instead of nested flex rows. Do not change API calls, staleness guards, or recovery behavior.

- [ ] **Step 5: Run the full ErrorPanel suite**

```bash
npm test -- --run src/components/custom/ErrorPanel.test.ts
```

Expected: all ErrorPanel tests pass with no Vue warnings.

- [ ] **Step 6: Commit the shared error redesign**

```bash
git add dashboard/src/components/custom/ErrorPanel.vue dashboard/src/components/custom/ErrorPanel.test.ts
git diff --cached --check
git commit -m "fix: refine shared error panel layout"
```

---

### Task 2: Guided VRChat profile identification

**Files:**
- Modify: `dashboard/src/pages/FederationFlowView.vue`
- Test: `dashboard/src/pages/FederationFlowView.test.ts`
- Modify: `dashboard/src/locales/en.ts`
- Modify: `dashboard/src/locales/zh.ts`

**Interfaces:**
- Consumes: unchanged identify flow and `POST .../prepare` payload `{ identity: string }`.
- Produces: visible guidance selectors `identify-guide`, `open-vrchat`, and a URL-first field without changing submitted values.

- [ ] **Step 1: Write the failing identify guidance test**

Replace the existing label/example-only test with:

```ts
it('shows how to find a profile URL and submits the unchanged identity value', async () => {
  get.mockResolvedValue(identifyFlow)
  post.mockResolvedValue(proofFlow)
  const wrapper = await mountView()

  const guide = wrapper.get('[data-test="identify-guide"]')
  expect(guide.get('h2').text()).toBe(en.federationFlow.identifyGuideTitle)
  expect(guide.findAll('ol li')).toHaveLength(3)
  expect(guide.text()).toContain(en.federationFlow.identifyStepOpen)
  expect(guide.text()).toContain(en.federationFlow.identifyStepProfile)
  expect(guide.text()).toContain(en.federationFlow.identifyStepCopy)
  expect(wrapper.get('[data-test="open-vrchat"]').attributes('href')).toBe('https://vrchat.com/home')
  expect(wrapper.get('[data-test="open-vrchat"]').attributes('target')).toBe('_blank')
  expect(wrapper.text()).toContain(en.federationFlow.noCredentials)
  expect(wrapper.get('label[for="federation-identity"]').text()).toBe(en.federationFlow.identityLabel)
  expect(wrapper.get('input[name="identity"]').attributes('placeholder')).toBe(en.federationFlow.identityPlaceholder)

  const value = 'https://vrchat.com/home/user/usr_12345678-1234-1234-1234-123456789abc'
  await wrapper.get('input[name="identity"]').setValue(value)
  await wrapper.get('form').trigger('submit')
  await flushPromises()
  expect(post).toHaveBeenCalledWith(`${basePath}/prepare`, { identity: value })
})
```

- [ ] **Step 2: Run the identify test and verify RED**

```bash
npm test -- --run src/pages/FederationFlowView.test.ts -t 'shows how to find a profile URL'
```

Expected: FAIL because the guide, link, placeholder, and copy do not exist.

- [ ] **Step 3: Add exact English and Chinese identify copy**

Add these keys under `federationFlow` in both locale files:

```ts
// en.ts
identifyIntro: 'Paste your VRChat profile URL. You can also enter a user ID beginning with usr_.',
identifyGuideTitle: 'Find your VRChat profile URL',
identifyStepOpen: 'Open the VRChat website and sign in.',
identifyStepProfile: 'Open your profile.',
identifyStepCopy: 'Copy the page address ending in /user/usr_…, then paste it below.',
openVrchatWebsite: 'Open VRChat website',
identityLabel: 'VRChat profile URL or user ID',
identityPlaceholder: 'https://vrchat.com/home/user/usr_…',
identityExample: 'User ID example: usr_12345678-1234-1234-1234-123456789abc',
noCredentials: 'Do not enter your VRChat password or verification code here.',

// zh.ts
identifyIntro: '粘贴你的 VRChat 个人资料网址，也可以输入以 usr_ 开头的用户 ID。',
identifyGuideTitle: '查找你的 VRChat 个人资料网址',
identifyStepOpen: '打开 VRChat 网站并登录。',
identifyStepProfile: '打开你的个人资料。',
identifyStepCopy: '复制以 /user/usr_… 结尾的页面地址，然后粘贴到下方。',
openVrchatWebsite: '打开 VRChat 网站',
identityLabel: 'VRChat 个人资料网址或用户 ID',
identityPlaceholder: 'https://vrchat.com/home/user/usr_…',
identityExample: '用户 ID 示例：usr_12345678-1234-1234-1234-123456789abc',
noCredentials: '请勿在此输入 VRChat 密码或验证码。',
```

- [ ] **Step 4: Implement visible guidance without hiding it in a disclosure**

Import `ExternalLink` is already available. Replace the identify intro/input block with:

```vue
<p class="text-sm leading-5 text-muted">{{ t('federationFlow.identifyIntro') }}</p>

<section
  data-test="identify-guide"
  class="flex flex-col gap-3 rounded-md border border-tide/20 bg-info/60 p-4"
  aria-labelledby="identify-guide-title"
>
  <h2 id="identify-guide-title" class="text-sm font-semibold text-ink">
    {{ t('federationFlow.identifyGuideTitle') }}
  </h2>
  <ol class="list-decimal space-y-1.5 ps-5 text-sm leading-5 text-ink">
    <li>{{ t('federationFlow.identifyStepOpen') }}</li>
    <li>{{ t('federationFlow.identifyStepProfile') }}</li>
    <li>{{ t('federationFlow.identifyStepCopy') }}</li>
  </ol>
  <a
    data-test="open-vrchat"
    href="https://vrchat.com/home"
    target="_blank"
    rel="noopener noreferrer"
    class="inline-flex min-h-11 w-fit items-center gap-2 rounded-md font-medium text-tide-strong underline underline-offset-4 outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
  >
    {{ t('federationFlow.openVrchatWebsite') }}
    <ExternalLink class="size-4" aria-hidden="true" />
  </a>
</section>

<div class="flex flex-col gap-1.5">
  <Label for="federation-identity">{{ t('federationFlow.identityLabel') }}</Label>
  <Input
    id="federation-identity"
    v-model="identity"
    name="identity"
    class="min-h-11"
    :placeholder="t('federationFlow.identityPlaceholder')"
    autocomplete="off"
    autocapitalize="none"
    spellcheck="false"
    required
    :aria-describedby="error ? 'federation-identify-error' : 'federation-identity-help'"
  />
  <div id="federation-identity-help" class="space-y-1">
    <p class="break-all font-mono text-xs text-muted">{{ t('federationFlow.identityExample') }}</p>
    <p class="text-xs leading-4 text-muted">{{ t('federationFlow.noCredentials') }}</p>
  </div>
</div>
```

- [ ] **Step 5: Run flow and locale parity tests**

```bash
npm test -- --run src/pages/FederationFlowView.test.ts src/locales/locales.parity.test.ts
```

Expected: both files pass in English and Chinese without changed API expectations.

- [ ] **Step 6: Commit identify-screen guidance**

```bash
git add dashboard/src/pages/FederationFlowView.vue dashboard/src/pages/FederationFlowView.test.ts dashboard/src/locales/en.ts dashboard/src/locales/zh.ts
git diff --cached --check
git commit -m "fix: guide VRChat profile identification"
```

---

### Task 3: Structured VRChat proof screen

**Files:**
- Modify: `dashboard/src/pages/FederationFlowView.vue`
- Test: `dashboard/src/pages/FederationFlowView.test.ts`
- Modify: `dashboard/src/locales/en.ts`
- Modify: `dashboard/src/locales/zh.ts`

**Interfaces:**
- Consumes: unchanged proof flow fields `profileUrl`, `proofUrl`, `requiresLocalUsername`, and `expiresAt`.
- Produces: computed `profileIdentifier`, selectors `profile-context`, `proof-steps`, and `proof-expiry`; API behavior remains unchanged.

- [ ] **Step 1: Write the failing proof hierarchy test**

Add:

```ts
it('presents profile context, proof steps, and expiry as one ordered task', async () => {
  get.mockResolvedValue(proofFlow)
  const wrapper = await mountView()

  const context = wrapper.get('[data-test="profile-context"]')
  expect(context.text()).toContain('usr_12345678-1234-1234-1234-123456789abc')
  const profileLink = context.get('[data-test="profile-link"]')
  expect(profileLink.text()).toContain(en.federationFlow.openProfile)
  expect(profileLink.attributes('href')).toBe(proofFlow.profileUrl)

  expect(wrapper.get('[data-test="copy-code"]').attributes('aria-label')).toBe(en.federationFlow.copyProofUrl)
  const steps = wrapper.get('[data-test="proof-steps"]')
  expect(steps.element.tagName).toBe('OL')
  expect(steps.findAll('li')).toHaveLength(3)
  expect(wrapper.get('[data-test="proof-expiry"]').text()).toContain('Expires')
  expect(wrapper.get('[data-test="verify-profile"]').text()).toBe(en.federationFlow.verify)
})
```

Extend the local-username test to require `[data-test="local-username-section"]` after the proof instruction section and before the error/action area.

- [ ] **Step 2: Run the proof hierarchy test and verify RED**

```bash
npm test -- --run src/pages/FederationFlowView.test.ts -t 'profile context, proof steps, and expiry'
```

Expected: FAIL because the selectors and structured profile row do not exist.

- [ ] **Step 3: Add the profile identifier and clock import**

Change the icon import and add the computed identifier:

```ts
import { Clock3, ExternalLink } from 'lucide-vue-next'

const profileIdentifier = computed(() => {
  const raw = flow.value?.profileUrl ?? ''
  const parts = raw.split('/').filter(Boolean)
  return parts.at(-1) || raw
})
```

- [ ] **Step 4: Reshape the proof section**

Use one section with deliberate 12/16px spacing and no nested card:

```vue
<section class="flex min-w-0 flex-col gap-4" aria-labelledby="proof-heading">
  <h2 id="proof-heading" data-test="proof-heading" tabindex="-1" class="text-lg font-semibold text-ink outline-none focus-visible:ring-3 focus-visible:ring-ring/50">
    {{ t('federationFlow.proofTitle') }}
  </h2>

  <div data-test="profile-context" class="flex min-w-0 flex-col gap-3 rounded-md border border-border bg-sunken/60 p-3 sm:flex-row sm:items-center sm:justify-between">
    <div class="min-w-0">
      <p class="text-xs text-muted">{{ t('federationFlow.profileLabel') }}</p>
      <code class="block truncate font-mono text-xs text-ink">{{ profileIdentifier }}</code>
    </div>
    <Button as="a" data-test="profile-link" :href="flow.profileUrl" target="_blank" rel="noopener noreferrer" variant="outline" size="sm" class="min-h-11 w-full sm:w-auto" :aria-label="`${t('federationFlow.openProfile')}: ${profileIdentifier}`">
      {{ t('federationFlow.openProfile') }}
      <ExternalLink class="size-4" aria-hidden="true" />
    </Button>
  </div>

  <CodeField v-if="flow.proofUrl" :value="flow.proofUrl" :label="t('federationFlow.proofUrlLabel')" :copy-label="t('federationFlow.copyProofUrl')" wrap />

  <ol data-test="proof-steps" class="grid gap-3" :aria-label="t('federationFlow.instructionsLabel')">
    <li v-for="(step, index) in [t('federationFlow.stepCopy'), t('federationFlow.stepAdd'), t('federationFlow.stepReturn')]" :key="step" class="grid grid-cols-[1.75rem_minmax(0,1fr)] items-start gap-2 text-sm leading-5 text-ink">
      <span aria-hidden="true" class="inline-flex size-7 items-center justify-center rounded-full bg-tide/10 text-xs font-semibold text-tide-strong">{{ index + 1 }}</span>
      <span class="pt-1">{{ step }}</span>
    </li>
  </ol>

  <p data-test="proof-expiry" role="status" class="flex items-center gap-2 text-xs text-muted">
    <Clock3 class="size-4 shrink-0" aria-hidden="true" />
    {{ t('federationFlow.expires', { time: expiry }) }}
  </p>
</section>
```

Add `data-test="local-username-section"` and `class="flex flex-col gap-1.5 border-t border-border pt-4"` to the optional username block. Leave the error panel, retry status, success state, and verify button after that block in their current behavioral order.

- [ ] **Step 5: Run the complete flow suite**

```bash
npm test -- --run src/pages/FederationFlowView.test.ts
```

Expected: all existing prepare, verify, retry, focus, copy, success, terminal, and privacy tests pass.

- [ ] **Step 6: Commit the proof-screen hierarchy**

```bash
git add dashboard/src/pages/FederationFlowView.vue dashboard/src/pages/FederationFlowView.test.ts
git diff --cached --check
git commit -m "fix: structure VRChat profile proof steps"
```

---

### Task 4: Guest-first public proof explanation

**Files:**
- Modify: `dashboard/src/pages/VRChatProofView.vue`
- Test: `dashboard/src/pages/VRChatProofView.test.ts`
- Modify: `dashboard/src/locales/en.ts`
- Modify: `dashboard/src/locales/zh.ts`

**Interfaces:**
- Consumes: `useBrandingStore().instanceName` and existing static route.
- Produces: selectors `proof-guest-context` and `proof-owner-instructions`; no API or route changes.

- [ ] **Step 1: Write failing guest and configured-brand tests**

Add:

```ts
it('explains the bio link to guests before owner instructions', async () => {
  const wrapper = await mountProof('proof_live')
  const guest = wrapper.get('[data-test="proof-guest-context"]')
  const owner = wrapper.get('[data-test="proof-owner-instructions"]')

  expect(guest.text()).toContain(en.vrchatProof.guestTitle)
  expect(guest.text()).toContain(en.vrchatProof.guestBody)
  expect(owner.get('h2').text()).toBe(en.vrchatProof.ownerTitle)
  expect(owner.get('ol').element.compareDocumentPosition(guest.element) & Node.DOCUMENT_POSITION_PRECEDING).not.toBe(0)
  expect(owner.findAll('ol li')).toHaveLength(3)
})

it('uses the configured instance name without checking the proof token', async () => {
  const wrapper = await mountProof('unknown_or_expired', 'Northstar ID')
  expect(wrapper.text()).toContain('Northstar ID')
  expect(wrapper.text()).not.toContain('Prohibitorum')
  expect(api.get).not.toHaveBeenCalled()
  expect(api.post).not.toHaveBeenCalled()
})
```

Change `mountProof` to accept `instanceName = 'Prohibitorum'`, create Pinia, set `useBrandingStore().instanceName = instanceName`, and then mount.

- [ ] **Step 2: Run guest tests and verify RED**

```bash
npm test -- --run src/pages/VRChatProofView.test.ts -t 'guests|configured instance'
```

Expected: FAIL because visitor/owner sections and branding interpolation do not exist.

- [ ] **Step 3: Replace public proof copy in both locales**

```ts
// en.ts
vrchatProof: {
  title: 'VRChat verification link',
  explanation: 'The owner of this VRChat profile temporarily added this link to prove they control the profile to {instance}.',
  guestTitle: "Visiting someone else's profile?",
  guestBody: 'You do not need to do anything. Opening this page does not verify the person, sign you in, approve access, or give them access to your account.',
  ownerTitle: 'If this is your profile',
  instructionsLabel: 'What the profile owner should do',
  return: 'Return to {instance} and select Verify profile.',
  remove: 'Remove this link after {instance} confirms verification.',
  close: 'You can close this page.',
},

// zh.ts
vrchatProof: {
  title: 'VRChat 验证链接',
  explanation: '此 VRChat 个人资料的所有者临时添加了该链接，用于向 {instance} 证明其拥有此个人资料的控制权。',
  guestTitle: '你是从他人的个人资料进入此页面吗？',
  guestBody: '你无需进行任何操作。打开此页面不会验证对方的身份、让你登录、批准访问，也不会让对方访问你的账户。',
  ownerTitle: '如果这是你的个人资料',
  instructionsLabel: '个人资料所有者应执行的操作',
  return: '返回 {instance} 并选择“验证个人资料”。',
  remove: '{instance} 确认验证后，请移除此链接。',
  close: '你可以关闭此页面。',
},
```

- [ ] **Step 4: Implement guest-first hierarchy**

```vue
<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import CenteredLayout from '@/pages/CenteredLayout.vue'
import { useBrandingStore } from '@/stores/branding'

const { t } = useI18n()
const branding = useBrandingStore()
</script>

<template>
  <CenteredLayout large-interactive-targets>
    <template #title>
      <h1 class="text-xl font-semibold tracking-tight text-ink">{{ t('vrchatProof.title') }}</h1>
    </template>

    <div class="flex flex-col gap-5">
      <p class="text-sm leading-5 text-ink">
        {{ t('vrchatProof.explanation', { instance: branding.instanceName }) }}
      </p>

      <section data-test="proof-guest-context" class="rounded-md border border-tide/20 bg-info/60 p-4" aria-labelledby="proof-guest-title">
        <h2 id="proof-guest-title" class="text-sm font-semibold text-ink">{{ t('vrchatProof.guestTitle') }}</h2>
        <p class="mt-2 text-sm leading-5 text-muted">{{ t('vrchatProof.guestBody') }}</p>
      </section>

      <section data-test="proof-owner-instructions" class="flex flex-col gap-3 border-t border-border pt-5" aria-labelledby="proof-owner-title">
        <h2 id="proof-owner-title" class="text-sm font-semibold text-ink">{{ t('vrchatProof.ownerTitle') }}</h2>
        <ol class="list-decimal space-y-2 ps-5 text-sm leading-5 text-ink" :aria-label="t('vrchatProof.instructionsLabel')">
          <li>{{ t('vrchatProof.return', { instance: branding.instanceName }) }}</li>
          <li>{{ t('vrchatProof.remove', { instance: branding.instanceName }) }}</li>
          <li>{{ t('vrchatProof.close') }}</li>
        </ol>
      </section>
    </div>
  </CenteredLayout>
</template>
```

- [ ] **Step 5: Run proof-page and locale tests**

```bash
npm test -- --run src/pages/VRChatProofView.test.ts src/locales/locales.parity.test.ts
```

Expected: both files pass; API mocks remain untouched for every proof value.

- [ ] **Step 6: Commit the public explanation**

```bash
git add dashboard/src/pages/VRChatProofView.vue dashboard/src/pages/VRChatProofView.test.ts dashboard/src/locales/en.ts dashboard/src/locales/zh.ts
git diff --cached --check
git commit -m "fix: explain VRChat proof links to visitors"
```

---

### Task 5: Branded VRChat login button

**Files:**
- Create: `dashboard/src/assets/vrchat-logo.svg`
- Create: `dashboard/src/components/custom/VRChatButton.vue`
- Modify: `dashboard/src/components/custom/FederationButtons.vue`
- Test: `dashboard/src/components/custom/FederationButtons.test.ts`

**Interfaces:**
- Produces: `VRChatButton` props `{ label: string }`, event `click`, selector `data-test="vrchat-login"`.
- Consumes: provider `protocol === 'vrchat'` from the existing public provider list.

- [ ] **Step 1: Write the failing branded routing test**

Replace the generic VRChat test with:

```ts
it('renders VRChat with the predefined branded button', async () => {
  get.mockResolvedValue([{ slug: 'vrchat', displayName: 'VRChat', protocol: 'vrchat', iconUrl: '/ignored-admin-icon' }])
  const w = mountComp()
  await flushPromises()

  const button = w.get('[data-test="vrchat-login"]')
  expect(button.text()).toContain('VRChat')
  expect(button.classes()).toEqual(expect.arrayContaining(['bg-[#00A2E8]', 'text-[#0B1A21]']))
  expect(button.find('img').attributes('src')).toContain('vrchat-logo')
  expect(button.find('img').attributes('alt')).toBe('')
  expect(button.text()).not.toContain('VVRChat')
})
```

Extend the mixed-provider test to include Steam, VRChat, and OIDC and assert one `steam-login`, one `vrchat-login`, and one generic outline button.

- [ ] **Step 2: Run the branded button test and verify RED**

```bash
npm test -- --run src/components/custom/FederationButtons.test.ts -t 'predefined branded button'
```

Expected: FAIL because VRChat still uses the generic initial-letter path.

- [ ] **Step 3: Add the predefined VRChat mark**

Create `dashboard/src/assets/vrchat-logo.svg` using the predefined Simple Icons VRChat mark (CC0), preserving its `viewBox="0 0 24 24"` and complete path data from:

```text
https://raw.githubusercontent.com/simple-icons/simple-icons/develop/icons/vrchat.svg
```

Remove the source `<title>` because the imported image is decorative and receives `alt=""` at the use site. Do not recolor the path; its default black fill supplies the approved dark foreground.

- [ ] **Step 4: Create `VRChatButton.vue`**

```vue
<script setup lang="ts">
import { Button } from '@/components/ui/button'
import VRChatLogo from '@/assets/vrchat-logo.svg'

defineProps<{ label: string }>()
defineEmits<{ (event: 'click'): void }>()
</script>

<template>
  <Button
    type="button"
    data-test="vrchat-login"
    class="w-full justify-start gap-2 border-0 bg-[#00A2E8] text-[#0B1A21] hover:bg-[#0092D1] active:bg-[#0084BD] focus-visible:ring-[#00A2E8]/50"
    @click="$emit('click')"
  >
    <img :src="VRChatLogo" alt="" aria-hidden="true" class="size-5" />
    <span>{{ label }}</span>
  </Button>
</template>
```

- [ ] **Step 5: Route VRChat providers to the dedicated component**

Import `VRChatButton` and add the branch between Steam and generic:

```vue
<SteamButton
  v-if="p.protocol === 'steam'"
  :label="p.displayName"
  @click="startFederation(p.slug)"
/>
<VRChatButton
  v-else-if="p.protocol === 'vrchat'"
  :label="p.displayName"
  @click="startFederation(p.slug)"
/>
<Button v-else type="button" variant="outline" class="w-full justify-start gap-2" @click="startFederation(p.slug)">
  <AppIcon :src="p.iconUrl" :name="p.displayName" size="sm" />
  <span>{{ p.displayName }}</span>
</Button>
```

- [ ] **Step 6: Run federation button tests**

```bash
npm test -- --run src/components/custom/FederationButtons.test.ts
```

Expected: Steam, VRChat, OIDC, icon, fallback, loading, and redirect tests all pass.

- [ ] **Step 7: Commit the branded button**

```bash
git add dashboard/src/assets/vrchat-logo.svg dashboard/src/components/custom/VRChatButton.vue dashboard/src/components/custom/FederationButtons.vue dashboard/src/components/custom/FederationButtons.test.ts
git diff --cached --check
git commit -m "fix: brand VRChat federation login"
```

---

### Task 6: Browser verification and embedded delivery

**Files:**
- Modify (generated): `pkg/webui/dist/**`
- Modify: `STATUS.md` only if an existing statement is contradicted by the verified UX.

**Interfaces:**
- Consumes: Tasks 1 through 5.
- Produces: verified desktop/mobile UX and current embedded dashboard assets.

- [ ] **Step 1: Run all focused frontend suites**

From `dashboard/`:

```bash
npm test -- --run \
  src/components/custom/ErrorPanel.test.ts \
  src/components/custom/FederationButtons.test.ts \
  src/pages/FederationFlowView.test.ts \
  src/pages/VRChatProofView.test.ts \
  src/locales/locales.parity.test.ts \
  src/locales/locales.errors.parity.test.ts
```

Expected: all six test files pass with no unhandled errors or Vue warnings.

- [ ] **Step 2: Build and refresh the embedded dashboard**

From the repository root:

```bash
mise run build:web
```

Expected: `vue-tsc -b` and Vite production build pass; `pkg/webui/dist` contains current hashed assets.

- [ ] **Step 3: Run the complete CI gate**

```bash
mise run ci
```

Expected: Go vet/build/tests, frontend tests/typecheck/build, and embedded-dist drift check all pass.

- [ ] **Step 4: Browser-check the shared ErrorPanel at desktop and mobile widths**

Use the existing Vite development server with deterministic API interception. At 1440x900 and 390x844, verify:

- dismiss mark is visually aligned with the first message line and sits at the right edge;
- message, recovery, action row, details divider, and diagnostic region follow an even 8/12px rhythm;
- expanded details show `Error code` before fields and request ID;
- no horizontal overflow or clipped controls;
- focus rings remain visible for dismiss, details, diagnostic, and copy controls.

- [ ] **Step 5: Browser-check all VRChat user-facing screens**

At 1440x900 and 390x844, intercept the existing flow APIs to render both states and verify:

- identify screen shows all three profile-finding steps, VRChat website link, URL placeholder, credential warning, and primary action;
- proof screen shows compact profile context, copyable proof URL, three ordered steps, clock expiry, optional local username separation, contextual error, and verify action;
- public proof page shows guest explanation before owner instructions and uses a configured instance name;
- login screen renders Steam and VRChat branded buttons plus a generic OIDC button; VRChat uses the predefined mark and blue/dark palette;
- keyboard focus order follows visual order and no surface overflows 390px.

- [ ] **Step 6: Commit generated assets**

```bash
git add -A pkg/webui/dist
git diff --cached --check
git commit -m "build: refresh polished dashboard bundle"
```

If the generated bundle is already byte-identical, skip this commit. Do not add the root `package-lock.json`.

- [ ] **Step 7: Request final code review and resolve findings**

Use the `requesting-code-review` skill over the complete implementation range. Fix every Critical and Important finding, rerun the focused tests for each fix, and request re-review until the result is ready to merge.

- [ ] **Step 8: Re-run final evidence and push master**

```bash
mise run ci
git push origin master
```

Expected: CI exits 0 and remote `origin/master` advances without force-push.
