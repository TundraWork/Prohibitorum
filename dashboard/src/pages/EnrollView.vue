<script setup lang="ts">
import { useResource } from '@/composables/useResource'
import { enrollmentQuery } from '@/queries/ceremonies'
/**
 * EnrollView — the enrollment ceremony (/enroll/:token).
 *
 * Contract (pkg/server/handle_enrollment.go + handle_invite_federation.go):
 *
 *   GET  /api/prohibitorum/enrollments/{token}
 *        → { intent, target?, suggestedDisplayName?, expiresAt, allowedMethods?,
 *            expectedUpstreamIdpSlug?, providers? } — the last two are
 *            invite-only. An invalid/expired/consumed token → AuthError → /error.
 *
 *   POST /api/prohibitorum/enrollments/{token}/register/begin … complete
 *        → local passkey signup, auto-login → hardRedirect('/').
 *
 *   GET  /api/prohibitorum/enrollments/{token}/start-federation?provider=…
 *        → 302 to the upstream provider; provisioning happens on the callback
 *          and lands on /welcome (identity unconfirmed).
 *
 * Invite rendering: an invite bound to a provider shows exactly that
 * provider's button — no local-credential buttons and no username inputs (the
 * account is named from the upstream claims). An unbound invite offers both
 * local methods and its redeemable providers; provider buttons intentionally
 * skip reportValidity — the typed name only feeds the local ceremonies.
 */
import ErrorPanel from '@/components/custom/ErrorPanel.vue'
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { api, type ApiError } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { useWebauthn } from '@/composables/useWebauthn'
import { hardRedirect } from '@/lib/navigate'
import CenteredLayout from '@/pages/CenteredLayout.vue'
import EnrollPasswordTotp from '@/components/custom/EnrollPasswordTotp.vue'
import OrDivider from '@/components/custom/OrDivider.vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

interface EnrollmentTarget {
  username: string
  displayName: string
}
interface FederationProvider {
  slug: string
  displayName: string
  protocol: string
  iconUrl?: string | null
}
interface EnrollmentPreview {
  intent: 'bootstrap' | 'invite' | 'reset' | 'federated_register'
  target?: EnrollmentTarget
  suggestedDisplayName?: string
  expiresAt: string
  // Which credential methods this enrollment permits: 'passkey' and/or
  // 'password_totp'. Bootstrap is passkey-only; every other intent offers both.
  allowedMethods?: string[]
  // Invite-only: the bound provider slug and the redeemable provider list.
  expectedUpstreamIdpSlug?: string
  providers?: FederationProvider[]
}
interface EnrollCompleteResponse {
  session: { id: number; username: string; displayName: string; role: string }
  newCredentialId: number
}

const route = useRoute()
const router = useRouter()
const { t } = useI18n()

const token = String(route.params.token ?? '')

const { busy: netBusy, error: netError, run, clear: clearNetError } = useApi()
const { busy: waBusy, error: waError, register, clear: clearWebauthnError } = useWebauthn()
const busy = computed(() => netBusy.value || waBusy.value)
const error = computed<ApiError | null>(() => netError.value ?? waError.value)

function clearError(): void {
  clearNetError()
  clearWebauthnError()
}

const contextQuery = useResource({ ...enrollmentQuery<EnrollmentPreview>(token), enabled: false })
const preview = computed(() => contextQuery.data.value ?? null)
const loading = ref(true)

// New-account intents collect these; reset leaves them untouched.
const username = ref('')
const displayName = ref('')

// Whether this page collects a locally-chosen username/display name. A
// provider-bound invite does NOT: the account is named from the upstream
// claims, so no identity inputs (and no local credentials) are rendered.
const collectsIdentity = computed(
  () =>
    (preview.value?.intent === 'bootstrap' ||
      preview.value?.intent === 'invite' ||
      preview.value?.intent === 'federated_register') &&
    !providerBound.value,
)

const providerBound = computed(() => !!preview.value?.expectedUpstreamIdpSlug)
const providers = computed(() => preview.value?.providers ?? [])

// Method chooser. Bootstrap is passkey-only; every other intent may also set up
// password+TOTP. `method` toggles the identity form between the chooser and the
// inline password+TOTP ceremony.
const allowsPasswordTotp = computed(() => preview.value?.allowedMethods?.includes('password_totp') ?? false)
const method = ref<'choose' | 'password_totp'>('choose')
const formRef = ref<HTMLFormElement | null>(null)

function choosePasswordTotp(): void {
  clearError()
  // The password+TOTP button is type=button, so native `required` validation on
  // the identity fields doesn't fire on click — trigger it explicitly.
  if (collectsIdentity.value && !formRef.value?.reportValidity()) return
  method.value = 'password_totp'
}

const heading = computed(() => {
  switch (preview.value?.intent) {
    case 'invite':
      return t('enroll.titleInvite')
    case 'federated_register':
      return t('enroll.titleFederatedRegister')
    case 'reset':
      return preview.value?.target ? t('enroll.titleReset') : t('enroll.titleRecovery')
    default:
      return t('enroll.title')
  }
})

function startFederationURL(slug: string): string {
  return (
    `/api/prohibitorum/enrollments/${encodeURIComponent(token)}/start-federation` +
    `?provider=${encodeURIComponent(slug)}` +
    `&return_to=${encodeURIComponent('/')}`
  )
}

function continueToProvider(slug: string): void {
  hardRedirect(startFederationURL(slug))
}

onMounted(async () => {
  try {
    const loaded = (await contextQuery.refetch({ throwOnError: true })).data!
    if (loaded.intent === 'federated_register') {
      displayName.value = loaded.suggestedDisplayName ?? ''
    }
  } catch (e) {
    const code = (e as ApiError | undefined)?.code
    router.replace({ name: 'error', query: { error: code ?? 'enrollment_consumed' } })
  } finally {
    loading.value = false
  }
})

async function enroll(): Promise<void> {
  const body = collectsIdentity.value
    ? { username: username.value, displayName: displayName.value }
    : undefined

  // 1) begin — fetch WebAuthn creation options.
  const options = await run(() =>
    api.post(`/api/prohibitorum/enrollments/${encodeURIComponent(token)}/register/begin`, body),
  )
  if (!options) return // errors render via ErrorPanel

  // 2) ceremony — navigator.credentials.create. undefined = user-cancel / error.
  const attestation = await register(options as Parameters<typeof register>[0])
  if (!attestation) return

  // 3) complete — create/rotate the credential, issue the session, auto-login.
  const res = await run(() =>
    api.post<EnrollCompleteResponse>(
      `/api/prohibitorum/enrollments/${encodeURIComponent(token)}/register/complete`,
      attestation,
    ),
  )
  if (!res) return

  // Authenticated. Full-page nav to the app root so the new session cookie is
  // sent on the next request.
  hardRedirect('/')
}
</script>

<template>
  <CenteredLayout>
    <template #title>
      <h1 class="text-xl font-semibold tracking-tight text-ink">{{ heading }}</h1>
    </template>

    <p v-if="loading" class="text-center text-sm text-muted">{{ t('common.loading') }}</p>

    <form v-else-if="preview" ref="formRef" class="flex flex-col gap-4" @submit.prevent="enroll">
      <p
        v-if="preview.intent === 'federated_register'"
        data-test="federated-register-intro"
        class="text-sm leading-5 text-muted"
      >
        {{ t('enroll.federatedRegisterBody') }}
      </p>
      <p
        v-else-if="preview.intent === 'reset' && !preview.target"
        data-test="recovery-intro"
        class="text-sm leading-5 text-muted"
      >
        {{ t('enroll.recoveryBody') }}
      </p>

      <!-- New-account intents choose a local identity. Locked once the
           password+TOTP ceremony has started so the pending account is fixed. -->
      <template v-if="collectsIdentity">
        <div class="flex flex-col gap-1.5">
          <Label for="enroll-username">{{ t('enroll.usernameLabel') }}</Label>
          <Input
            id="enroll-username"
            v-model="username"
            name="username"
            :placeholder="t('enroll.usernamePlaceholder')"
            autocomplete="username"
            autocapitalize="none"
            spellcheck="false"
            :disabled="method !== 'choose'"
            required
          />
          <p class="text-xs text-muted">{{ t('enroll.usernameDesc') }}</p>
        </div>
        <div class="flex flex-col gap-1.5">
          <Label for="enroll-displayname">{{ t('enroll.displayNameLabel') }}</Label>
          <Input
            id="enroll-displayname"
            v-model="displayName"
            name="displayName"
            :placeholder="t('enroll.displayNamePlaceholder')"
            autocomplete="name"
            :disabled="method !== 'choose'"
            required
          />
          <p class="text-xs text-muted">{{ t('enroll.displayNameDesc') }}</p>
        </div>
      </template>

      <!-- A target-bearing reset may identify the fixed account as read-only text. -->
      <template v-else-if="preview.target">
        <div data-test="target-account" class="flex flex-col gap-1.5">
          <Label>{{ t('enroll.targetAccountLabel') }}</Label>
          <p class="font-mono text-sm text-ink">{{ preview.target.username }}</p>
        </div>
      </template>

      <!-- Password+TOTP ceremony (inline) once chosen. -->
      <EnrollPasswordTotp
        v-if="method === 'password_totp'"
        :token="token"
        :identity="collectsIdentity ? { username, displayName } : null"
        @back="method = 'choose'"
      />

      <!-- Otherwise — unless this invite is bound to a provider: the method
           chooser (or the passkey-only bootstrap button). A reset registers
           a new passkey without collecting an identity, so it renders here
           too; the account already exists. -->
      <template v-else-if="!providerBound">
        <ErrorPanel :error="error" @dismiss="clearError" />

        <p class="text-xs text-muted">{{ t('enroll.passkeyForeshadow') }}</p>

        <Button type="submit" size="lg" class="w-full" :disabled="busy">
          {{ t('enroll.registerButton') }}
        </Button>

        <template v-if="allowsPasswordTotp">
          <OrDivider :label="t('login.orDivider')" />
          <Button
            type="button"
            variant="outline"
            size="lg"
            class="w-full"
            data-test="choose-password-totp"
            @click="choosePasswordTotp"
          >
            {{ t('enroll.methodPasswordTotp') }}
          </Button>
        </template>

      </template>
      <!-- Providers the invite may be redeemed through — rendered for every
           invite, bound or not, including when no local identity is collected.
           Provider accounts are named from the upstream claims, so these
           buttons deliberately skip form validation — the typed inputs serve
           local signup only. -->
      <template v-if="providers.length">
        <OrDivider :label="t('enroll.providerDivider')" />
        <p class="text-xs text-muted">{{ t('enroll.providerHint') }}</p>
        <Button
          v-for="p in providers"
          :key="p.slug"
          type="button"
          variant="outline"
          size="lg"
          class="w-full"
          :data-test="`provider-${p.slug}`"
          @click="continueToProvider(p.slug)"
        >
          <img
            v-if="p.iconUrl"
            :src="p.iconUrl"
            :alt="p.displayName"
            class="mr-2 size-5 rounded-sm object-contain"
          />
          <span v-else class="mr-2 size-5 leading-5" aria-hidden="true">{{
            p.displayName.charAt(0).toUpperCase()
          }}</span>
          {{ t('enroll.providerButton', { provider: p.displayName }) }}
        </Button>
      </template>
    </form>
  </CenteredLayout>
</template>
