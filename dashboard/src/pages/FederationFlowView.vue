<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { Clock3, ExternalLink, Info } from 'lucide-vue-next'
import CenteredLayout from '@/pages/CenteredLayout.vue'
import CodeField from '@/components/custom/CodeField.vue'
import ErrorPanel from '@/components/custom/ErrorPanel.vue'
import NumberedSteps from '@/components/custom/NumberedSteps.vue'
import SectionTitle from '@/components/custom/SectionTitle.vue'
import StatusMessage from '@/components/custom/StatusMessage.vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Skeleton } from '@/components/ui/skeleton'
import { api } from '@/lib/api'
import { isApiError } from '@/lib/errors'
import type { ApiError } from '@/lib/errors'
import { hardRedirect } from '@/lib/navigate'
import { useBrandingStore } from '@/stores/branding'

interface FederationProvider {
  slug: string
  displayName: string
  protocol: string
}

interface FederationFlow {
  provider: FederationProvider
  intent: string
  step: 'identify' | 'proof'
  profileUrl?: string
  proofUrl?: string
  expiresAt: string
}

interface VerifyResponse {
  redirect: string
}

const route = useRoute()
const { locale, t } = useI18n()
const branding = useBrandingStore()
const flowToken = String(route.params.flow ?? '')
const flowPath = `/api/prohibitorum/auth/federation/flows/${encodeURIComponent(flowToken)}`

const flow = ref<FederationFlow | null>(null)
const loading = ref(true)
const preparing = ref(false)
const verifying = ref(false)
const error = ref<ApiError | null>(null)
const terminal = ref(false)
const identity = ref('')
const redirect = ref('')

const succeeded = computed(() => redirect.value !== '')
const heading = computed(() => {
  if (!flow.value) return t('title.federationFlow')
  return t('federationFlow.title', { provider: flow.value.provider.displayName })
})
const retryAfter = computed(() => error.value?.retryAfterSeconds)
const expiry = computed(() => {
  if (!flow.value?.expiresAt) return ''
  const date = new Date(flow.value.expiresAt)
  if (Number.isNaN(date.getTime())) return flow.value.expiresAt
  return new Intl.DateTimeFormat(locale.value, {
    dateStyle: 'medium',
    timeStyle: 'short',
  }).format(date)
})
const profileIdentifier = computed(() => {
  const raw = flow.value?.profileUrl ?? ''
  const parts = raw.split('/').filter(Boolean)
  return parts.at(-1) || raw
})

function normalizeError(value: unknown): ApiError {
  return isApiError(value) ? value : { code: 'server_error' }
}

function isTerminalError(code: string): boolean {
  return [
    'federation_state_invalid',
    'federation_identity_conflict',
    'invite_required',
    'link_required',
  ].includes(code)
}

async function focusElement(id: string): Promise<void> {
  await nextTick()
  document.getElementById(id)?.focus()
}

async function loadFlow(options: { preserve?: boolean } = {}): Promise<boolean> {
  if (!options.preserve) loading.value = true
  try {
    flow.value = await api.get<FederationFlow>(flowPath)
    terminal.value = false
    return true
  } catch (value) {
    error.value = normalizeError(value)
    terminal.value = true
    return false
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  void loadFlow()
})

function dismissError(): void {
  error.value = null
}

async function prepareProof(): Promise<void> {
  if (preparing.value) return
  preparing.value = true
  error.value = null
  try {
    flow.value = await api.post<FederationFlow>(`${flowPath}/prepare`, {
      identity: identity.value,
    })
    await focusElement('proof-heading')
  } catch (value) {
    error.value = normalizeError(value)
    terminal.value = isTerminalError(error.value.code)
    if (!terminal.value) await focusElement('federation-identity')
  } finally {
    preparing.value = false
  }
}

async function reloadChangedFlow(): Promise<void> {
  const loaded = await loadFlow({ preserve: true })
  if (!loaded) return
  if (flow.value?.step === 'proof') {
    await focusElement('verify-profile')
  } else {
    await focusElement('federation-identity')
  }
}

async function verifyProfile(): Promise<void> {
  if (verifying.value || !flow.value || flow.value.step !== 'proof') return
  verifying.value = true
  error.value = null
  try {
    const result = await api.post<VerifyResponse>(`${flowPath}/verify`)
    redirect.value = result.redirect
    await focusElement('success-heading')
  } catch (value) {
    verifying.value = false
    error.value = normalizeError(value)
    const code = error.value.code
    terminal.value = isTerminalError(code)
    if (terminal.value) return

    if (code === 'federation_action_invalid') {
      await reloadChangedFlow()
    } else {
      await focusElement('verify-profile')
    }
  } finally {
    verifying.value = false
  }
}

function continueFlow(): void {
  if (redirect.value) hardRedirect(redirect.value)
}
</script>

<template>
  <CenteredLayout large-interactive-targets>
    <template #title>
      <h1 class="text-xl font-semibold tracking-tight text-ink">{{ heading }}</h1>
    </template>

    <div
      v-if="loading"
      data-test="flow-loading"
      class="flex flex-col gap-4"
      role="status"
      aria-busy="true"
    >
      <span class="sr-only">{{ t('common.loading') }}</span>
      <Skeleton class="h-5 w-3/4 animate-none" />
      <Skeleton class="h-11 w-full animate-none" />
      <Skeleton class="h-11 w-full animate-none" />
    </div>

    <ErrorPanel
      v-else-if="terminal"
      :error="error"
      :dismissible="false"
    />

    <form
      v-else-if="flow?.step === 'identify'"
      class="flex flex-col gap-5"
      @submit.prevent="prepareProof"
    >
      <div
        v-if="flow.intent === 'enroll'"
        data-test="account-handoff-notice"
        role="note"
        class="flex min-w-0 items-start gap-3 rounded-lg border border-info-border bg-info p-3 text-info-foreground"
      >
        <Info class="mt-0.5 size-4 shrink-0" aria-hidden="true" />
        <div class="min-w-0 space-y-1">
          <p class="text-sm font-medium leading-5">
            {{ t('federationFlow.accountNoticePrimary') }}
          </p>
          <p class="text-sm leading-5">
            {{ t('federationFlow.accountNoticeSupporting', { instance: branding.instanceName }) }}
          </p>
        </div>
      </div>

      <p class="text-sm leading-5 text-muted">{{ t('federationFlow.identifyIntro') }}</p>

      <section
        data-test="identify-guide"
        class="flex flex-col gap-3"
        aria-labelledby="identify-guide-title"
      >
        <SectionTitle id="identify-guide-title" as="h2">
          {{ t('federationFlow.identifyGuideTitle') }}
        </SectionTitle>
        <NumberedSteps
          :steps="[
            { text: t('federationFlow.identifyStepOpen'), href: 'https://vrchat.com/home', test: 'open-vrchat' },
            { text: t('federationFlow.identifyStepProfile') },
            { text: t('federationFlow.identifyStepCopy') },
          ]"
        />
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
          <p class="break-all font-mono text-xs text-muted">
            {{ t('federationFlow.identityExample') }}
          </p>
          <p class="text-xs leading-4 text-muted">{{ t('federationFlow.noCredentials') }}</p>
        </div>
      </div>

      <div id="federation-identify-error">
        <ErrorPanel :error="error" @dismiss="dismissError" />
      </div>

      <Button type="submit" size="lg" class="min-h-11 w-full" :disabled="preparing">
        {{ t('federationFlow.prepare') }}
      </Button>
    </form>

    <form
      v-else-if="flow?.step === 'proof'"
      class="flex min-w-0 flex-col gap-5"
      @submit.prevent="verifyProfile"
    >
      <section class="flex min-w-0 flex-col gap-4" aria-labelledby="proof-heading">
        <SectionTitle
          id="proof-heading"
          data-test="proof-heading"
          as="h2"
          tabindex="-1"
          class="outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
        >
          {{ t('federationFlow.proofTitle') }}
        </SectionTitle>

        <div
          data-test="profile-context"
          class="flex min-w-0 flex-col gap-3 rounded-md border border-border bg-sunken/60 p-3 sm:flex-row sm:items-center sm:justify-between"
        >
          <div class="min-w-0">
            <p class="text-xs text-muted">{{ t('federationFlow.profileLabel') }}</p>
            <code class="block truncate font-mono text-xs text-ink">{{ profileIdentifier }}</code>
          </div>
          <Button
            as="a"
            data-test="profile-link"
            :href="flow.profileUrl"
            target="_blank"
            rel="noopener noreferrer"
            variant="outline"
            size="sm"
            class="min-h-11 w-full sm:w-auto"
            :aria-label="`${t('federationFlow.openProfile')}: ${profileIdentifier}`"
          >
            {{ t('federationFlow.openProfile') }}
            <ExternalLink class="size-4" aria-hidden="true" />
          </Button>
        </div>

        <CodeField
          v-if="flow.proofUrl"
          data-test="proof-url"
          :value="flow.proofUrl"
          :label="t('federationFlow.proofUrlLabel')"
          :copy-label="t('federationFlow.copyProofUrl')"
          wrap
        />

        <NumberedSteps
          data-test="proof-steps"
          :label="t('federationFlow.instructionsLabel')"
          :steps="[
            { text: t('federationFlow.stepCopy') },
            { text: t('federationFlow.stepAdd') },
            { text: t('federationFlow.stepReturn') },
          ]"
        />

        <p data-test="proof-expiry" role="status" class="flex items-center gap-2 text-xs text-muted">
          <Clock3 class="size-4 shrink-0" aria-hidden="true" />
          {{ t('federationFlow.expires', { time: expiry }) }}
        </p>
      </section>

      <div id="federation-proof-error">
        <ErrorPanel :error="error" @dismiss="dismissError" />
      </div>

      <p v-if="retryAfter !== undefined" role="status" class="text-sm text-amber-700">
        {{ t('federationFlow.retryAfter', { seconds: retryAfter }) }}
      </p>

      <template v-if="succeeded">
        <div class="flex flex-col gap-3">
          <SectionTitle
            id="success-heading"
            data-test="success-heading"
            as="h2"
            tabindex="-1"
            class="outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
          >
            {{ t('federationFlow.success') }}
          </SectionTitle>
          <StatusMessage show data-test="verification-status">
            {{ t('federationFlow.success') }}
          </StatusMessage>
        </div>
        <Button
          type="button"
          size="lg"
          class="min-h-11 w-full"
          data-test="continue"
          @click="continueFlow"
        >
          {{ t('federationFlow.continue') }}
        </Button>
      </template>

      <Button
        v-else
        id="verify-profile"
        type="submit"
        size="lg"
        class="min-h-11 w-full"
        data-test="verify-profile"
        :disabled="verifying"
      >
        {{ t('federationFlow.verify') }}
      </Button>
    </form>
  </CenteredLayout>
</template>
