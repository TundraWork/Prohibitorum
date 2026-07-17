<script setup lang="ts">
/** AdminUpstreamIdpDetailView (/admin/identity-providers/:slug) — edit, rotate secret, delete. */
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import StatusMessage from '@/components/custom/StatusMessage.vue'
import { useRoute, useRouter } from 'vue-router'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { useTransientFlag } from '@/composables/useTransientFlag'
import { withSudo } from '@/lib/sudo'
import { formatDateTime } from '@/lib/time'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Separator } from '@/components/ui/separator'
import { Alert, AlertDescription } from '@/components/ui/alert'
import ConfirmDialog from '@/components/custom/ConfirmDialog.vue'
import SectionTitle from '@/components/custom/SectionTitle.vue'
import RadioCardGroup from '@/components/custom/RadioCardGroup.vue'
import ScopeSelector from '@/components/custom/ScopeSelector.vue'
import ListInput from '@/components/custom/ListInput.vue'
import StatusBadge from '@/components/custom/StatusBadge.vue'
import { UPSTREAM_SCOPE_SUGGESTIONS } from '@/lib/scopes'
import SettingRow from '@/components/custom/SettingRow.vue'
import FormSection from '@/components/custom/FormSection.vue'
import CardSkeleton from '@/components/custom/CardSkeleton.vue'
import BackLink from '@/components/custom/BackLink.vue'
import EntityIconUpload from '@/components/custom/EntityIconUpload.vue'
import ErrorPanel from '@/components/custom/ErrorPanel.vue'
import type { IdentityProvider, OIDCProviderConfig, ProviderMode, ProviderProtocol } from './AdminUpstreamIdpsView.vue'

const { t, locale } = useI18n()
const route = useRoute()
const router = useRouter()
const { busy, error, run, clear } = useApi()

const slug = String(route.params.slug)
const idp = ref<IdentityProvider | null>(null)
const notFound = ref(false)

const displayName = ref(''); const issuerUrl = ref(''); const clientId = ref('')
const mode = ref<ProviderMode>('auto_provision'); const scopes = ref<string[]>([]); const allowedDomains = ref<string[]>([])
const usernameClaim = ref(''); const displayNameClaim = ref(''); const emailClaim = ref(''); const pictureClaim = ref('')
const requireVerifiedEmail = ref(false); const disabled = ref(false); const allowPrivateNetwork = ref(false)
const { flag: saved, trigger: triggerSaved } = useTransientFlag()

const isOIDC = computed(() => idp.value?.protocol === 'oidc')
const isSteam = computed(() => idp.value?.protocol === 'steam')
const isVRChat = computed(() => idp.value?.protocol === 'vrchat')
const newSecret = ref(''); const { flag: rotated, trigger: triggerRotated } = useTransientFlag()
const confirmDelete = ref(false)

type OperatorMethod = 'totp' | 'emailOtp' | 'otp'
type OperatorSessionResult =
  | { status: 'challenge'; challenge: string; methods: string[]; expiresAt: string }
  | { status: 'valid'; provider: IdentityProvider }
const providerProtocols: readonly ProviderProtocol[] = ['oidc', 'steam', 'vrchat']
const providerModes: readonly ProviderMode[] = ['auto_provision', 'invite_only', 'link_only']

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null
}
function hasErrorCode(value: unknown, code: string): boolean {
  return isRecord(value) && value.code === code
}

function isIdentityProvider(value: unknown): value is IdentityProvider {
  if (!isRecord(value)) return false
  return typeof value.slug === 'string'
    && typeof value.displayName === 'string'
    && providerProtocols.includes(value.protocol as ProviderProtocol)
    && providerModes.includes(value.mode as ProviderMode)
    && isRecord(value.config)
    && typeof value.disabled === 'boolean'
    && typeof value.secretConfigured === 'boolean'
    && ['unconfigured', 'configured', 'valid', 'invalid'].includes(String(value.secretStatus))
    && (typeof value.secretValidatedAt === 'string' || value.secretValidatedAt === null)
    && typeof value.ready === 'boolean'
    && typeof value.supportsOperator === 'boolean'
    && Array.isArray(value.searchFields)
    && typeof value.createdAt === 'string'
}

function parseOperatorSessionResult(value: unknown): OperatorSessionResult {
  if (!isRecord(value)) throw { code: 'server_error' }
  if (value.status === 'challenge'
    && typeof value.challenge === 'string'
    && Array.isArray(value.methods)
    && value.methods.every((method) => typeof method === 'string')
    && typeof value.expiresAt === 'string') {
    return {
      status: 'challenge',
      challenge: value.challenge,
      methods: value.methods,
      expiresAt: value.expiresAt,
    }
  }
  if (value.status === 'valid' && isIdentityProvider(value.provider) && value.provider.protocol === 'vrchat') {
    return { status: 'valid', provider: value.provider }
  }
  throw { code: 'server_error' }
}

const supportedOperatorMethods: readonly OperatorMethod[] = ['totp', 'emailOtp', 'otp']
const operatorUsername = ref('')
const operatorPassword = ref('')
const operatorCode = ref('')
const operatorChallenge = ref<string | null>(null)
const operatorMethods = ref<OperatorMethod[]>([])
const operatorMethod = ref<OperatorMethod | ''>('')
const operatorSetupActive = ref(false)
const operatorUsernameInput = ref<{ $el: HTMLInputElement } | null>(null)
const operatorCodeInput = ref<{ $el: HTMLInputElement } | null>(null)
const operatorStatusRegion = ref<HTMLElement | null>(null)
let operatorOperationGeneration = 0
let active = true

function beginOperatorOperation(): number {
  operatorOperationGeneration += 1
  return operatorOperationGeneration
}

function isOperatorOperationCurrent(generation: number): boolean {
  return active && generation === operatorOperationGeneration
}

async function focusOperatorUsername(generation: number = operatorOperationGeneration): Promise<void> {
  await nextTick()
  if (isOperatorOperationCurrent(generation)) operatorUsernameInput.value?.$el.focus()
}

async function focusOperatorCode(generation: number): Promise<void> {
  await nextTick()
  if (isOperatorOperationCurrent(generation)) operatorCodeInput.value?.$el.focus()
}

async function focusOperatorStatus(generation: number): Promise<void> {
  await nextTick()
  if (isOperatorOperationCurrent(generation)) operatorStatusRegion.value?.focus()
}

const operatorMethodOptions = computed(() => operatorMethods.value.map((method) => ({
  value: method,
  title: t(`admin.upstream.operatorMethod.${method}`),
})))
const operatorStatusLabel = computed(() => {
  if (idp.value?.secretStatus === 'valid') return t('admin.upstream.operatorStatusValid')
  if (idp.value?.secretStatus === 'invalid') return t('admin.upstream.operatorStatusInvalid')
  return t('admin.upstream.operatorStatusUnconfigured')
})
const operatorStatusVariant = computed(() => {
  if (idp.value?.secretStatus === 'valid') return 'success'
  if (idp.value?.secretStatus === 'invalid') return 'danger'
  return 'caution'
})
const operatorProgress = computed(() => operatorChallenge.value
  ? t('admin.upstream.operatorProgressCode')
  : operatorSetupActive.value
    ? t('admin.upstream.operatorProgressCredentials')
    : t('admin.upstream.operatorProgressReady'))
const operatorEnableBlocked = computed(() =>
  Boolean(isVRChat.value && disabled.value && idp.value?.secretStatus !== 'valid'))

function validateDomain(s: string): string | null { return /^[a-z0-9.-]+\.[a-z]{2,}$/i.test(s) ? null : t('admin.upstream.domainInvalid') }

const upstreamScopesKnown = computed(() => UPSTREAM_SCOPE_SUGGESTIONS.map((s) => ({ value: s.value, description: t(s.descKey) })))

async function load(): Promise<void> {
  const i = await run(() => api.get<IdentityProvider>(`/api/prohibitorum/identity-providers/${slug}`))
  if (!i) { if (error.value?.code === 'upstream_idp_not_found') notFound.value = true; return }
  idp.value = i
  displayName.value = i.displayName
  mode.value = i.mode
  disabled.value = i.disabled
  operatorSetupActive.value = i.protocol === 'vrchat' && i.secretStatus !== 'valid'
  if (i.protocol === 'oidc') {
    const config = i.config as unknown as OIDCProviderConfig
    issuerUrl.value = config.issuerUrl
    clientId.value = config.clientId
    scopes.value = [...config.scopes]
    allowedDomains.value = [...config.allowedDomains]
    usernameClaim.value = config.usernameClaim
    displayNameClaim.value = config.displayNameClaim
    emailClaim.value = config.emailClaim
    pictureClaim.value = config.pictureClaim
    requireVerifiedEmail.value = config.requireVerifiedEmail
    allowPrivateNetwork.value = config.allowPrivateNetwork
  } else {
    issuerUrl.value = ''
    clientId.value = ''
    scopes.value = []
    allowedDomains.value = []
    usernameClaim.value = ''
    displayNameClaim.value = ''
    emailClaim.value = ''
    pictureClaim.value = ''
    requireVerifiedEmail.value = false
    allowPrivateNetwork.value = false
  }
}

async function save(): Promise<void> {
  const config: OIDCProviderConfig | Record<string, never> = isOIDC.value
    ? {
        issuerUrl: issuerUrl.value,
        clientId: clientId.value,
        scopes: scopes.value,
        allowedDomains: allowedDomains.value,
        usernameClaim: usernameClaim.value,
        displayNameClaim: displayNameClaim.value,
        emailClaim: emailClaim.value,
        pictureClaim: pictureClaim.value,
        requireVerifiedEmail: requireVerifiedEmail.value,
        allowPrivateNetwork: allowPrivateNetwork.value,
      }
    : {}
  const updated = await run(() => withSudo(() => api.put<IdentityProvider>(`/api/prohibitorum/identity-providers/${slug}`, {
    displayName: displayName.value,
    mode: mode.value,
    config,
  }), t('sudo.reason.saveChanges')))
  if (updated) { idp.value = updated; triggerSaved() }
}

async function rotate(): Promise<void> {
  const ok = await run(() => withSudo(async () => {
    await api.post('/api/prohibitorum/identity-providers/rotate-secret', { slug, secret: newSecret.value })
    return true as const
  }, t('sudo.reason.rotateSecret')))
  if (ok) { triggerRotated(); newSecret.value = '' }
}
function clearOperatorChallenge(): void {
  operatorChallenge.value = null
  operatorMethods.value = []
  operatorMethod.value = ''
  operatorCode.value = ''
}

function acceptValidOperatorProvider(provider: IdentityProvider, generation: number): void {
  if (!isOperatorOperationCurrent(generation)) return
  idp.value = provider
  disabled.value = provider.disabled
  operatorUsername.value = ''
  operatorPassword.value = ''
  clearOperatorChallenge()
  operatorSetupActive.value = false
  void focusOperatorStatus(generation)
}

function acceptOperatorChallenge(result: Extract<OperatorSessionResult, { status: 'challenge' }>, generation: number): void {
  if (!isOperatorOperationCurrent(generation)) return
  operatorUsername.value = ''
  operatorChallenge.value = result.challenge
  operatorMethods.value = [...new Set(result.methods.filter(
    (method): method is OperatorMethod =>
      supportedOperatorMethods.includes(method as OperatorMethod),
  ))]
  operatorMethod.value = operatorMethods.value[0] ?? ''
  operatorCode.value = ''
  void focusOperatorCode(generation)
}

function projectInvalidOperatorSession(generation: number): void {
  if (!isOperatorOperationCurrent(generation)) return
  if (idp.value) {
    idp.value = {
      ...idp.value,
      secretConfigured: true,
      secretStatus: 'invalid',
      ready: false,
    }
  }
  operatorUsername.value = ''
  operatorPassword.value = ''
  clearOperatorChallenge()
  operatorSetupActive.value = true
  void focusOperatorUsername(generation)
}

async function startOperatorSession(): Promise<void> {
  const generation = beginOperatorOperation()
  const credentials = {
    username: operatorUsername.value,
    password: operatorPassword.value,
  }
  try {
    const result = await run(() => withSudo(async () => {
      if (!isOperatorOperationCurrent(generation)) return undefined
      const response = await api.post<unknown>(
        `/api/prohibitorum/identity-providers/${slug}/operator-session/start`,
        { username: credentials.username, password: credentials.password },
      )
      return parseOperatorSessionResult(response)
    }, t('sudo.reason.startOperatorSession')))
    if (!result || !isOperatorOperationCurrent(generation)) return
    if (result.status === 'valid') {
      acceptValidOperatorProvider(result.provider, generation)
      return
    }
    acceptOperatorChallenge(result, generation)
  } finally {
    credentials.username = ''
    credentials.password = ''
    operatorPassword.value = ''
  }
}

async function verifyOperatorSession(): Promise<void> {
  if (!operatorChallenge.value || !operatorMethod.value) return
  const generation = beginOperatorOperation()
  const verification: { challenge: string; method: OperatorMethod | ''; code: string } = {
    challenge: operatorChallenge.value,
    method: operatorMethod.value,
    code: operatorCode.value,
  }
  try {
    const result = await run(() => withSudo(async () => {
      if (!isOperatorOperationCurrent(generation)) return undefined
      const response = await api.post<unknown>(
        `/api/prohibitorum/identity-providers/${slug}/operator-session/verify`,
        {
          challenge: verification.challenge,
          method: verification.method,
          code: verification.code,
        },
      )
      return parseOperatorSessionResult(response)
    }, t('sudo.reason.verifyOperatorSession')))
    if (!isOperatorOperationCurrent(generation)) return
    if (!result) {
      if (error.value?.code === 'vrchat_operator_challenge_invalid') {
        clearOperatorChallenge()
        operatorSetupActive.value = true
        void focusOperatorUsername(generation)
      }
      return
    }
    if (result.status === 'valid') acceptValidOperatorProvider(result.provider, generation)
    else acceptOperatorChallenge(result, generation)
  } finally {
    verification.challenge = ''
    verification.method = ''
    verification.code = ''
    operatorCode.value = ''
  }
}

async function validateOperatorSession(): Promise<void> {
  const generation = beginOperatorOperation()
  const result = await run(async () => {
    try {
      return await withSudo(async () => {
        if (!isOperatorOperationCurrent(generation)) return undefined
        const response = await api.post<unknown>(
          `/api/prohibitorum/identity-providers/${slug}/operator-session/validate`,
        )
        return parseOperatorSessionResult(response)
      }, t('sudo.reason.validateOperatorSession'))
    } catch (validationError: unknown) {
      if (isOperatorOperationCurrent(generation)
        && hasErrorCode(validationError, 'vrchat_operator_credentials_invalid')) {
        projectInvalidOperatorSession(generation)
        try {
          const provider = await api.get<IdentityProvider>(
            `/api/prohibitorum/identity-providers/${slug}`,
          )
          if (isOperatorOperationCurrent(generation)) {
            idp.value = provider
            disabled.value = provider.disabled
            operatorSetupActive.value = provider.secretStatus !== 'valid'
          }
        } catch {
          // The projected invalid state remains authoritative for this failure.
        }
      }
      throw validationError
    }
  })
  if (!result || !isOperatorOperationCurrent(generation)) return
  if (result.status === 'valid') acceptValidOperatorProvider(result.provider, generation)
  else acceptOperatorChallenge(result, generation)
}

function replaceOperatorSession(): void {
  clear()
  const generation = beginOperatorOperation()
  operatorUsername.value = ''
  operatorPassword.value = ''
  clearOperatorChallenge()
  operatorSetupActive.value = true
  void focusOperatorUsername(generation)
}

// Flip the disabled flag on its own (independent of the config Save), via the
// dedicated set-disabled endpoint.
async function toggleDisabled(): Promise<void> {
  const next = !disabled.value
  const updated = await run(() => withSudo(() =>
    api.post<IdentityProvider>('/api/prohibitorum/identity-providers/set-disabled', { slug, disabled: next }),
    t('sudo.reason.disableApp')))
  if (updated) { idp.value = updated; disabled.value = updated.disabled }
}

async function destroy(): Promise<void> {
  const ok = await run(() => withSudo(async () => {
    await api.post('/api/prohibitorum/identity-providers/delete', { slug })
    return true as const
  }, t('sudo.reason.deleteApp')))
  confirmDelete.value = false
  if (ok) router.push('/admin/identity-providers')
}

onBeforeUnmount(() => {
  active = false
  operatorOperationGeneration += 1
  operatorUsername.value = ''
  operatorPassword.value = ''
  operatorCode.value = ''
  clearOperatorChallenge()
})

onMounted(load)
</script>
<template>
  <div class="flex max-w-2xl flex-col gap-6">
    <BackLink to="/admin/identity-providers" :label="t('admin.upstream.back')" />
    <ErrorPanel v-if="error && !notFound" :error="error" @dismiss="clear" :is-admin="true" />
    <p v-if="notFound" class="text-sm text-muted" role="status">{{ t('admin.upstream.notFound') }}</p>

    <CardSkeleton v-else-if="busy && !idp" />

    <template v-else-if="idp">
      <h1 class="text-2xl font-semibold tracking-tight text-ink">{{ idp.displayName }}</h1>

      <Card>
        <CardHeader><CardTitle>{{ t('admin.upstream.configTitle') }}</CardTitle></CardHeader>
        <CardContent class="flex flex-col gap-5">
          <FormSection :title="t('admin.upstream.sectionConnection')">
            <div class="flex flex-col gap-1.5">
              <Label>{{ t('admin.upstream.slug') }}</Label>
              <p class="font-mono text-sm text-muted" data-test="idp-slug">{{ idp.slug }}</p>
              <p class="text-xs text-muted">{{ t('admin.upstream.slugDesc') }}</p>
            </div>
            <div class="flex flex-col gap-1.5">
              <Label>{{ t('admin.upstream.protocol') }}</Label>
              <p class="font-mono text-sm text-muted" data-test="idp-protocol">{{ idp.protocol ?? 'oidc' }}</p>
            </div>
            <div class="flex flex-col gap-1.5">
              <Label for="displayName">{{ t('admin.upstream.displayName') }}</Label>
              <Input id="displayName" name="displayName" v-model="displayName" autocomplete="off" />
            </div>
            <template v-if="isOIDC">
              <div class="flex flex-col gap-1.5">
                <Label for="issuerUrl">{{ t('admin.upstream.issuerUrl') }}</Label>
                <Input id="issuerUrl" name="issuerUrl" v-model="issuerUrl" autocomplete="off" />
                <p class="text-xs text-muted">{{ t('admin.upstream.issuerUrlDesc') }}</p>
              </div>
              <div class="flex flex-col gap-1.5">
                <Label for="clientId">{{ t('admin.upstream.clientId') }}</Label>
                <Input id="clientId" name="clientId" v-model="clientId" autocomplete="off" />
              </div>
              <div class="flex flex-col gap-1.5">
                <Label for="scopes">{{ t('admin.upstream.scopes') }}</Label>
                <ScopeSelector :known="upstreamScopesKnown" :allow-custom="true" v-model="scopes" />
                <p class="text-xs text-muted">{{ t('admin.upstream.scopesDesc') }}</p>
              </div>
            </template>
          </FormSection>
          <FormSection :title="t('admin.upstream.sectionProvisioning')">
            <div class="flex flex-col gap-1.5">
              <Label>{{ t('admin.upstream.mode') }}</Label>
              <RadioCardGroup v-model="mode" :aria-label="t('admin.upstream.mode')" :options="[
                {value:'auto_provision',title:t('admin.upstream.modeAutoProvision'),description:t('admin.upstream.modeAutoProvisionDesc')},
                {value:'invite_only',title:t('admin.upstream.modeInviteOnly'),description:t('admin.upstream.modeInviteOnlyDesc')},
                {value:'link_only',title:t('admin.upstream.modeLinkOnly'),description:t('admin.upstream.modeLinkOnlyDesc')}]" />
            </div>
            <div v-if="isOIDC" class="flex flex-col gap-1.5">
              <Label>{{ t('admin.upstream.allowedDomains') }}</Label>
              <ListInput v-model="allowedDomains" name="allowedDomains"
                :add-label="t('admin.upstream.addDomain')" :placeholder="t('admin.upstream.domainPlaceholder')" :validate="validateDomain" />
              <p class="text-xs text-muted">{{ t('admin.upstream.domainsHint') }}</p>
            </div>
            <SettingRow v-if="isOIDC" :label="t('admin.upstream.requireVerifiedEmail')" :description="t('admin.upstream.requireVerifiedEmailDesc')" for="requireVerifiedEmail">
              <Switch id="requireVerifiedEmail" v-model="requireVerifiedEmail" data-test="requireVerifiedEmail" />
            </SettingRow>
          </FormSection>
          <FormSection v-if="isOIDC" :title="t('admin.upstream.sectionSecurity')">
            <Alert variant="destructive" data-test="private-network-warning">
              <AlertDescription>{{ t('admin.upstream.allowPrivateNetworkWarning') }}</AlertDescription>
            </Alert>
            <SettingRow :label="t('admin.upstream.allowPrivateNetwork')" :description="t('admin.upstream.allowPrivateNetworkDesc')" for="allowPrivateNetwork">
              <Switch id="allowPrivateNetwork" v-model="allowPrivateNetwork" data-test="allowPrivateNetwork" />
            </SettingRow>
          </FormSection>
          <FormSection v-if="isOIDC" :title="t('admin.upstream.sectionClaims')">
            <div class="grid grid-cols-[minmax(7rem,auto)_1fr] items-center gap-x-3 gap-y-2">
              <Label class="text-sm" for="usernameClaim">{{ t('admin.upstream.usernameClaim') }}</Label>
              <Input id="usernameClaim" name="usernameClaim" class="h-8" v-model="usernameClaim" placeholder="preferred_username" autocomplete="off" data-test="claim-username" />
              <Label class="text-sm" for="displayNameClaim">{{ t('admin.upstream.displayNameClaim') }}</Label>
              <Input id="displayNameClaim" name="displayNameClaim" class="h-8" v-model="displayNameClaim" placeholder="name" autocomplete="off" data-test="claim-displayName" />
              <Label class="text-sm" for="emailClaim">{{ t('admin.upstream.emailClaim') }}</Label>
              <Input id="emailClaim" name="emailClaim" class="h-8" v-model="emailClaim" placeholder="email" autocomplete="off" data-test="claim-email" />
              <Label class="text-sm" for="pictureClaim">{{ t('admin.upstream.pictureClaim') }}</Label>
              <Input id="pictureClaim" name="pictureClaim" class="h-8" v-model="pictureClaim" placeholder="picture" autocomplete="off" data-test="claim-avatar" />
            </div>
            <p class="text-xs text-muted">{{ t('admin.upstream.claimsHint') }}</p>
          </FormSection>
          <div class="flex items-center gap-3">
            <Button type="button" :disabled="busy" data-test="save" @click="save">{{ t('admin.upstream.save') }}</Button>
            <StatusMessage :show="saved">{{ t('admin.upstream.saved') }}</StatusMessage>
          </div>
        </CardContent>
      </Card>

      <EntityIconUpload
        :base-path="`/api/prohibitorum/identity-providers/${slug}`"
        :name="idp?.displayName ?? slug"
        :icon-url="idp?.iconUrl"
        @changed="load"
      />

      <Card v-if="isVRChat" data-test="operator-session-card">
        <CardHeader><CardTitle>{{ t('admin.upstream.operatorSessionTitle') }}</CardTitle></CardHeader>
        <CardContent class="flex flex-col gap-5">
          <div ref="operatorStatusRegion" tabindex="-1" class="flex flex-col gap-1 rounded-sm outline-none focus-visible:ring-2 focus-visible:ring-ring" role="status" aria-live="polite" data-test="operator-live-region">
            <div class="flex flex-wrap items-center gap-2">
              <span class="text-sm font-medium text-ink">{{ t('admin.upstream.operatorStatusLabel') }}</span>
              <StatusBadge :variant="operatorStatusVariant" data-test="operator-status-badge">
                {{ operatorStatusLabel }}
              </StatusBadge>
            </div>
            <p class="text-sm text-muted">{{ operatorProgress }}</p>
            <p class="text-xs text-muted" data-test="operator-last-validation">
              {{ t('admin.upstream.operatorLastValidation') }}:
              {{ idp.secretValidatedAt ? formatDateTime(idp.secretValidatedAt, locale) : t('admin.upstream.operatorLastValidationNever') }}
            </p>
          </div>

          <Alert role="note" data-test="operator-risk-warning">
            <AlertDescription class="max-w-[75ch]">
              {{ t('admin.upstream.vrchatCreateWarning') }}
            </AlertDescription>
          </Alert>

          <Alert role="note" data-test="operator-session-notice">
            <AlertDescription class="max-w-[75ch]">{{ t('admin.upstream.operatorSessionNotice') }}</AlertDescription>
          </Alert>

          <form
            v-if="operatorSetupActive && !operatorChallenge"
            class="flex flex-col gap-4"
            @submit.prevent="startOperatorSession"
            data-test="operator-credentials-form"
          >
            <div class="flex flex-col gap-1.5">
              <Label for="operatorUsername">{{ t('admin.upstream.operatorUsername') }}</Label>
              <Input
                ref="operatorUsernameInput"
                id="operatorUsername"
                v-model="operatorUsername"
                name="operatorUsername"
                autocomplete="username"
                required
              />
            </div>
            <div class="flex flex-col gap-1.5">
              <Label for="operatorPassword">{{ t('admin.upstream.operatorPassword') }}</Label>
              <Input
                id="operatorPassword"
                v-model="operatorPassword"
                name="operatorPassword"
                type="password"
                autocomplete="current-password"
                required
              />
            </div>
            <Button
              type="submit"
              class="w-fit"
              :disabled="busy || !operatorUsername || !operatorPassword"
              data-test="operator-start"
            >
              {{ t('admin.upstream.operatorStart') }}
            </Button>
          </form>

          <form
            v-else-if="operatorChallenge"
            class="flex flex-col gap-4"
            @submit.prevent="verifyOperatorSession"
            data-test="operator-code-form"
          >
            <div class="flex flex-col gap-1.5">
              <Label>{{ t('admin.upstream.operatorMethodLabel') }}</Label>
              <RadioCardGroup
                v-model="operatorMethod"
                :aria-label="t('admin.upstream.operatorMethodLabel')"
                :options="operatorMethodOptions"
              />
            </div>
            <div class="flex flex-col gap-1.5">
              <Label for="operatorCode">{{ t('admin.upstream.operatorCode') }}</Label>
              <Input
                ref="operatorCodeInput"
                id="operatorCode"
                v-model="operatorCode"
                name="operatorCode"
                autocomplete="one-time-code"
                required
              />
            </div>
            <Button
              type="submit"
              class="w-fit"
              :disabled="busy || !operatorMethod || !operatorCode"
              data-test="operator-verify"
            >
              {{ t('admin.upstream.operatorVerify') }}
            </Button>
          </form>

          <div v-else class="flex flex-wrap gap-2">
            <Button
              type="button"
              :disabled="busy"
              data-test="operator-validate"
              @click="validateOperatorSession"
            >
              {{ t('admin.upstream.operatorValidate') }}
            </Button>
            <Button
              type="button"
              variant="outline"
              :disabled="busy"
              data-test="operator-replace"
              @click="replaceOperatorSession"
            >
              {{ t('admin.upstream.operatorReplace') }}
            </Button>
          </div>
        </CardContent>
      </Card>

      <!-- Danger zone: sensitive operations (disable, rotate secret, delete) grouped together. -->
      <Card class="border-destructive/30 bg-destructive/[0.02]">
        <CardHeader><CardTitle class="text-destructive">{{ t('admin.upstream.dangerTitle') }}</CardTitle></CardHeader>
        <CardContent class="flex flex-col gap-4">
          <div class="flex flex-col gap-2">
            <div class="flex items-center gap-2">
              <SectionTitle as="h3">{{ t('admin.upstream.statusLabel') }}</SectionTitle>
              <StatusBadge :variant="disabled ? 'danger' : 'success'" data-test="status-badge">
                {{ disabled ? t('admin.upstream.disabled') : t('admin.upstream.active') }}
              </StatusBadge>
            </div>
            <p class="text-xs text-muted">{{ t('admin.upstream.disabledDesc') }}</p>
            <p v-if="operatorEnableBlocked" class="text-xs text-muted">
              {{ t('admin.upstream.operatorEnableRequiresValid') }}
            </p>
            <Button type="button" variant="outline" class="w-fit" :disabled="busy || operatorEnableBlocked" data-test="disable-toggle" @click="toggleDisabled">
              {{ disabled ? t('admin.upstream.enable') : t('admin.upstream.disable') }}
            </Button>
          </div>

          <template v-if="isOIDC || isSteam">
            <Separator />
            <div class="flex flex-col gap-2">
              <SectionTitle as="h3">{{ isSteam ? t('admin.upstream.rotateTitleSteam') : t('admin.upstream.rotateTitle') }}</SectionTitle>
              <p class="text-xs text-muted">{{ isSteam ? t('admin.upstream.rotateBodySteam') : t('admin.upstream.rotateBody') }}</p>
              <div class="flex flex-col gap-1.5">
                <Label for="newSecret">{{ isSteam ? t('admin.upstream.steamApiKey') : t('admin.upstream.clientSecret') }}</Label>
                <Input id="newSecret" name="newSecret" type="password" v-model="newSecret" autocomplete="off" />
              </div>
              <StatusMessage :show="rotated">{{ t('admin.upstream.rotated') }}</StatusMessage>
              <Button type="button" variant="outline" class="w-fit" :disabled="busy || !newSecret" data-test="rotate" @click="rotate">{{ isSteam ? t('admin.upstream.rotateConfirmSteam') : t('admin.upstream.rotateConfirm') }}</Button>
            </div>
          </template>

          <Separator />
          <div class="flex flex-col gap-2">
            <SectionTitle as="h3">{{ t('admin.upstream.deleteTitle') }}</SectionTitle>
            <p class="text-xs text-muted">{{ t('admin.upstream.deleteHelp') }}</p>
            <Button type="button" variant="destructive" class="w-fit" :disabled="busy" data-test="delete" @click="confirmDelete = true">{{ t('admin.upstream.delete') }}</Button>
          </div>
        </CardContent>
      </Card>
    </template>

    <ConfirmDialog :open="confirmDelete" :title="t('admin.upstream.deleteConfirmTitle')" :confirm-label="t('admin.upstream.delete')" :busy="busy"
      @update:open="(v) => { if (!v) confirmDelete = false }" @cancel="confirmDelete = false" @confirm="destroy">
      {{ t('admin.upstream.deleteConfirmBody') }}
    </ConfirmDialog>
  </div>
</template>
