<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ExternalLink } from 'lucide-vue-next'
import CenteredLayout from '@/pages/CenteredLayout.vue'
import CodeField from '@/components/custom/CodeField.vue'
import ErrorPanel from '@/components/custom/ErrorPanel.vue'
import StatusMessage from '@/components/custom/StatusMessage.vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Skeleton } from '@/components/ui/skeleton'
import { api } from '@/lib/api'
import { isApiError } from '@/lib/errors'
import type { ApiError } from '@/lib/errors'
import { hardRedirect } from '@/lib/navigate'

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
  requiresLocalUsername: boolean
  expiresAt: string
}

interface VerifyResponse {
  redirect: string
}

const route = useRoute()
const { locale, t } = useI18n()
const flowToken = String(route.params.flow ?? '')
const flowPath = `/api/prohibitorum/auth/federation/flows/${encodeURIComponent(flowToken)}`

const flow = ref<FederationFlow | null>(null)
const loading = ref(true)
const preparing = ref(false)
const verifying = ref(false)
const error = ref<ApiError | null>(null)
const terminal = ref(false)
const identity = ref('')
const localUsername = ref('')
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
  if (flow.value?.requiresLocalUsername) {
    await focusElement('local-username')
  } else if (flow.value?.step === 'proof') {
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
    const result = flow.value.requiresLocalUsername
      ? await api.post<VerifyResponse>(`${flowPath}/verify`, {
          localUsername: localUsername.value,
        })
      : await api.post<VerifyResponse>(`${flowPath}/verify`)
    redirect.value = result.redirect
    await focusElement('success-heading')
  } catch (value) {
    verifying.value = false
    error.value = normalizeError(value)
    const code = error.value.code
    terminal.value = isTerminalError(code)
    if (terminal.value) return

    if (code === 'local_username_required' || code === 'federation_action_invalid') {
      await reloadChangedFlow()
    } else if (code === 'username_collision' || code === 'invalid_username') {
      await focusElement('local-username')
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
  <CenteredLayout>
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
      @dismiss="dismissError"
    />

    <form
      v-else-if="flow?.step === 'identify'"
      class="flex flex-col gap-5"
      @submit.prevent="prepareProof"
    >
      <p class="text-sm text-muted">{{ t('federationFlow.identifyIntro') }}</p>

      <div class="flex flex-col gap-1.5">
        <Label for="federation-identity">{{ t('federationFlow.identityLabel') }}</Label>
        <Input
          id="federation-identity"
          v-model="identity"
          name="identity"
          class="min-h-11"
          autocomplete="off"
          autocapitalize="none"
          spellcheck="false"
          required
          :aria-describedby="error ? 'federation-identify-error' : 'federation-identity-example'"
        />
        <p id="federation-identity-example" class="break-all font-mono text-xs text-muted">
          {{ t('federationFlow.identityExample') }}
        </p>
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
        <h2
          id="proof-heading"
          data-test="proof-heading"
          tabindex="-1"
          class="text-lg font-semibold text-ink outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
        >
          {{ t('federationFlow.proofTitle') }}
        </h2>

        <div class="flex flex-col gap-1">
          <span class="text-xs text-muted">{{ t('federationFlow.profileLabel') }}</span>
          <a
            data-test="profile-link"
            :href="flow.profileUrl"
            target="_blank"
            rel="noopener noreferrer"
            class="inline-flex min-h-11 w-fit max-w-full items-center gap-2 rounded-md text-sm font-medium text-tide-strong underline underline-offset-4 outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
          >
            <span>{{ t('federationFlow.openProfile') }}</span>
            <ExternalLink class="size-4 shrink-0" aria-hidden="true" />
          </a>
        </div>

        <CodeField
          v-if="flow.proofUrl"
          :value="flow.proofUrl"
          :label="t('federationFlow.proofUrlLabel')"
          :copy-label="t('federationFlow.copyProofUrl')"
          wrap
        />

        <ol
          class="list-decimal space-y-2 ps-5 text-sm text-ink"
          :aria-label="t('federationFlow.instructionsLabel')"
        >
          <li>{{ t('federationFlow.stepCopy') }}</li>
          <li>{{ t('federationFlow.stepAdd') }}</li>
          <li>{{ t('federationFlow.stepReturn') }}</li>
        </ol>

        <p class="text-sm text-muted">{{ t('federationFlow.expires', { time: expiry }) }}</p>
      </section>

      <div v-if="flow.requiresLocalUsername" class="flex flex-col gap-1.5">
        <Label for="local-username">{{ t('federationFlow.usernameLabel') }}</Label>
        <Input
          id="local-username"
          v-model="localUsername"
          name="localUsername"
          class="min-h-11"
          autocomplete="username"
          autocapitalize="none"
          spellcheck="false"
          required
          :aria-describedby="error ? 'federation-proof-error' : 'local-username-description'"
        />
        <p id="local-username-description" class="text-xs text-muted">
          {{ t('federationFlow.usernameDescription') }}
        </p>
      </div>

      <div id="federation-proof-error">
        <ErrorPanel :error="error" @dismiss="dismissError" />
      </div>

      <p v-if="retryAfter !== undefined" role="status" class="text-sm text-amber-700">
        {{ t('federationFlow.retryAfter', { seconds: retryAfter }) }}
      </p>

      <template v-if="succeeded">
        <div class="flex flex-col gap-3">
          <h2
            id="success-heading"
            data-test="success-heading"
            tabindex="-1"
            class="text-lg font-semibold text-ink outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
          >
            {{ t('federationFlow.success') }}
          </h2>
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
