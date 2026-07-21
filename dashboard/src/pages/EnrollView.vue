<script setup lang="ts">
/**
 * EnrollView — the enrollment ceremony (/enroll/:token).
 *
 * Contract (pkg/server/handle_enrollment.go + handle_invite_federation.go,
 * verified — the old EnrollView was advisory only):
 *
 *   GET  /api/prohibitorum/enrollments/{token}
 *        → { intent: 'bootstrap'|'invite'|'reset'|'federated_register',
 *            target?{username,displayName}, suggestedDisplayName?, expiresAt }
 *        invalid/expired/consumed → an AuthError → we route to /error.
 *
 *   POST /api/prohibitorum/enrollments/{token}/register/begin
 *        body { username, displayName } for bootstrap/invite/federated_register; empty for reset
 *        → WebAuthn creation options
 *   POST /api/prohibitorum/enrollments/{token}/register/complete
 *        body = attestation → { session, newCredentialId } (+ session cookie)
 *        → auto-login → hardRedirect('/').
 *
 *   GET  /api/prohibitorum/enrollments/{token}/start-federation?return_to=/
 *        → 302 to the upstream OP (federation-bound invites).
 *
 * Federation detection — IMPORTANT: the preview carries NO federation hint
 * (EnrollmentPreview has only intent/target/expiresAt). A federation-bound
 * invite is revealed ONLY by register/begin returning the
 * `enrollment_federation_required` code; that is our signal to hand off to
 * start-federation. (The username/displayName the invitee typed is discarded —
 * federation derives identity from the upstream IdP's claims.)
 *
 * Per-intent form: bootstrap, invite, and federated_register collect username +
 * displayName; federated_register may initialize the editable display name from
 * the verified profile. Reset shows a read-only username only when previewed.
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
interface EnrollmentPreview {
  intent: 'bootstrap' | 'invite' | 'reset' | 'federated_register'
  target?: EnrollmentTarget
  suggestedDisplayName?: string
  expiresAt: string
  // Which credential methods this enrollment permits: 'passkey' and/or
  // 'password_totp'. Bootstrap is passkey-only; every other intent offers both.
  allowedMethods?: string[]
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

const preview = ref<EnrollmentPreview | null>(null)
const loading = ref(true)
const federationRedirectUrl = ref('')

// New-account intents collect these; reset leaves them untouched.
const username = ref('')
const displayName = ref('')

const collectsIdentity = computed(
  () =>
    preview.value?.intent === 'bootstrap' ||
    preview.value?.intent === 'invite' ||
    preview.value?.intent === 'federated_register',
)

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

function onFederationRequired(): void {
  // A federation-bound invite rejects local methods; hand off to the provider.
  method.value = 'choose'
  federationRedirectUrl.value = startFederationURL()
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

function startFederationURL(): string {
  return (
    `/api/prohibitorum/enrollments/${encodeURIComponent(token)}/start-federation` +
    `?return_to=${encodeURIComponent('/')}`
  )
}

onMounted(async () => {
  try {
    const loaded = await api.get<EnrollmentPreview>(
      `/api/prohibitorum/enrollments/${encodeURIComponent(token)}`,
    )
    preview.value = loaded
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

  // 1) begin — fetch WebAuthn creation options (or discover this is federation-bound).
  const options = await run(() =>
    api.post(`/api/prohibitorum/enrollments/${encodeURIComponent(token)}/register/begin`, body),
  )
  if (!options) {
    // Federation-bound invite → show an interstitial instead of an instant bounce.
    if (netError.value?.code === 'enrollment_federation_required') {
      federationRedirectUrl.value = startFederationURL()
    }
    return // other errors render via ErrorPanel
  }

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
  // sent on the next request. (In Spec 1 the authenticated home is not built
  // yet; Spec 2 adds the '/' dashboard route this lands on.)
  hardRedirect('/')
}
</script>

<template>
  <CenteredLayout>
    <template #title>
      <h1 class="text-xl font-semibold tracking-tight text-ink">{{ heading }}</h1>
    </template>

    <p v-if="loading" class="text-center text-sm text-muted">{{ t('common.loading') }}</p>

    <!-- Federation interstitial: shown when begin returns enrollment_federation_required -->
    <div v-else-if="federationRedirectUrl" class="flex flex-col gap-4">
      <p class="text-sm text-muted">{{ t('enroll.federationBody') }}</p>
      <Button
        type="button"
        size="lg"
        class="w-full"
        data-test="federation-continue"
        @click="hardRedirect(federationRedirectUrl)"
      >
        {{ t('enroll.federationContinue') }}
      </Button>
    </div>

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
        @federation-required="onFederationRequired"
      />

      <!-- Otherwise: the method chooser (or the passkey-only bootstrap button). -->
      <template v-else>
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
    </form>
  </CenteredLayout>
</template>
