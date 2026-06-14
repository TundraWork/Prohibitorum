<script setup lang="ts">
/**
 * EnrollView — the enrollment ceremony (/enroll/:token).
 *
 * Contract (pkg/server/handle_enrollment.go + handle_invite_federation.go,
 * verified — the old EnrollView was advisory only):
 *
 *   GET  /api/prohibitorum/enrollments/{token}
 *        → { intent: 'bootstrap'|'invite'|'reset', target?{username,displayName}, expiresAt }
 *        invalid/expired/consumed → an AuthError → we route to /error.
 *
 *   POST /api/prohibitorum/enrollments/{token}/register/begin
 *        body { username, displayName } for bootstrap/invite; empty for reset
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
 * Per-intent form: bootstrap & invite collect username + displayName; reset
 * shows the read-only target username (identity is fixed, only the passkey is
 * replaced).
 */
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { api, type ApiError } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { useWebauthn } from '@/composables/useWebauthn'
import { hardRedirect } from '@/lib/navigate'
import CenteredLayout from '@/pages/CenteredLayout.vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Alert, AlertDescription } from '@/components/ui/alert'
import CodeField from '@/components/custom/CodeField.vue'

interface EnrollmentTarget {
  username: string
  displayName: string
}
interface EnrollmentPreview {
  intent: 'bootstrap' | 'invite' | 'reset'
  target?: EnrollmentTarget
  expiresAt: string
}
interface EnrollCompleteResponse {
  session: { id: number; username: string; displayName: string; role: string }
  newCredentialId: number
}

const route = useRoute()
const router = useRouter()
const { t, te } = useI18n()

const token = String(route.params.token ?? '')

const { busy: netBusy, error: netError, run } = useApi()
const { busy: waBusy, error: waError, register } = useWebauthn()
const busy = computed(() => netBusy.value || waBusy.value)
const error = computed<ApiError | null>(() => netError.value ?? waError.value)

const preview = ref<EnrollmentPreview | null>(null)
const loading = ref(true)

// bootstrap/invite collect these; reset leaves them untouched.
const username = ref('')
const displayName = ref('')

const collectsIdentity = computed(
  () => preview.value?.intent === 'bootstrap' || preview.value?.intent === 'invite',
)

const heading = computed(() => {
  switch (preview.value?.intent) {
    case 'invite':
      return t('enroll.titleInvite')
    case 'reset':
      return t('enroll.titleReset')
    default:
      return t('enroll.title')
  }
})

const errorText = computed(() => {
  const e = error.value
  if (!e) return ''
  const key = `errors.${e.code}`
  return te(key) ? t(key) : e.message || t('common.error')
})

function startFederationURL(): string {
  return (
    `/api/prohibitorum/enrollments/${encodeURIComponent(token)}/start-federation` +
    `?return_to=${encodeURIComponent('/')}`
  )
}

onMounted(async () => {
  try {
    preview.value = await api.get<EnrollmentPreview>(
      `/api/prohibitorum/enrollments/${encodeURIComponent(token)}`,
    )
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
    // Federation-bound invite → hand off to the upstream IdP instead of passkey.
    if (netError.value?.code === 'enrollment_federation_required') {
      hardRedirect(startFederationURL())
    }
    return // other errors render via errorText
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

    <form v-else-if="preview" class="flex flex-col gap-4" @submit.prevent="enroll">
      <!-- bootstrap / invite: choose identity -->
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
            required
          />
          <p class="text-xs text-muted">{{ t('enroll.displayNameDesc') }}</p>
        </div>
      </template>

      <!-- reset: identity is fixed, show the target as a read-only identifier -->
      <template v-else-if="preview.target">
        <div class="flex flex-col gap-1.5">
          <Label>{{ t('enroll.targetAccountLabel') }}</Label>
          <CodeField :value="t('enroll.targetAccount', { username: preview.target.username })" />
        </div>
      </template>

      <Alert v-if="errorText" variant="destructive" role="alert" aria-live="polite">
        <AlertDescription>{{ errorText }}</AlertDescription>
      </Alert>

      <Button type="submit" size="lg" class="w-full" :disabled="busy">
        {{ t('enroll.registerButton') }}
      </Button>
    </form>
  </CenteredLayout>
</template>
