<script setup lang="ts">
/**
 * LoginView — the method-selection login page (/login?return_to=…).
 *
 * Layout: a centered card over the Drenched backdrop. Passkey is the primary
 * (prominent) method; password→TOTP and federation are the secondary fallbacks.
 * Any successful flow lands on the same-origin-guarded return_to.
 *
 * Bootstrap branch: GET /auth/status tells us whether any admin account exists.
 * Before the first account is enrolled, passkey/password sign-in is impossible,
 * so we replace those with the enroll-admin instruction (federation, if any
 * upstream is configured, stays available since it can bootstrap via invite).
 */
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth'
import { useBrandingStore } from '@/stores/branding'
import { useReturnTo } from '@/composables/useReturnTo'
import { useSessionExpiry } from '@/composables/useSessionExpiry'
import { hardRedirect } from '@/lib/navigate'
import CenteredLayout from '@/pages/CenteredLayout.vue'
import PasskeyButton from '@/components/custom/PasskeyButton.vue'
import PasswordTotpForm from '@/components/custom/PasswordTotpForm.vue'
import FederationButtons from '@/components/custom/FederationButtons.vue'
import OrDivider from '@/components/custom/OrDivider.vue'
import CardSkeleton from '@/components/custom/CardSkeleton.vue'
import { Alert, AlertDescription } from '@/components/ui/alert'

const { t } = useI18n()
const route = useRoute()
const auth = useAuthStore()
const branding = useBrandingStore()
const { rawReturnTo, goReturnTo } = useReturnTo()
const sessionExpired = computed(() => route.query.reason === 'session_expired')

// Default to bootstrapped (show the sign-in methods); flip to the instruction
// only when /auth/status explicitly says no admin exists.
const bootstrapped = ref(true)
// True while the status check is in flight — hides the auth-method section
// until we know which methods to render, preventing a visible flash.
const checking = ref(true)

onMounted(async () => {
  // Clear the global session-expired banner flag now that the user is on the
  // login page — the reason query param carries the context forward instead.
  useSessionExpiry().reset()

  // Already signed in? Don't show the login form — resume the return_to (e.g.
  // the OIDC /oauth/authorize that bounced an unauthenticated user here) or land
  // on the dashboard. Leaves `checking` true so the skeleton shows during the
  // redirect rather than a flash of the sign-in methods.
  try {
    await auth.ensureLoaded()
    if (auth.me) {
      goReturnTo()
      return
    }
  } catch {
    // /me unreachable — fall through to the sign-in methods.
  }

  try {
    const status = await api.get<{ bootstrapped: boolean }>('/api/prohibitorum/auth/status')
    bootstrapped.value = status.bootstrapped
  } catch {
    // If status can't be read, fail open to the sign-in methods.
    bootstrapped.value = true
  } finally {
    checking.value = false
  }
})

function onSuccess(redirect?: string): void {
  // The login ceremony returns a server-validated redirect — follow it
  // verbatim (mirrors ConsentView; the server is the authoritative validator).
  // The recovery sub-flow has no server redirect → fall back to the client
  // goReturnTo (safeReturnTo defense-in-depth), as does the already-signed-in
  // on-mount branch below.
  if (redirect) {
    hardRedirect(redirect)
  } else {
    goReturnTo()
  }
}
</script>

<template>
  <CenteredLayout>
    <template #title>
      <h1 class="text-xl font-semibold tracking-tight text-ink">{{ t('login.title') }}</h1>
    </template>

    <div class="flex flex-col gap-6">
      <Alert v-if="branding.maintenanceMode" role="status" aria-live="polite">
        <AlertDescription>{{ t('login.maintenanceNotice') }}</AlertDescription>
      </Alert>

      <Alert
        v-if="sessionExpired"
        variant="destructive"
        class="border-destructive/20 bg-destructive/10"
        role="status"
        aria-live="polite"
      >
        <AlertDescription>{{ t('login.sessionExpired') }}</AlertDescription>
      </Alert>

      <template v-if="checking">
        <CardSkeleton :lines="3" />
      </template>
      <template v-else-if="bootstrapped">
        <PasskeyButton :return-to="rawReturnTo" @success="onSuccess" />

        <OrDivider :label="t('login.orDivider')" />

        <PasswordTotpForm :return-to="rawReturnTo" @success="onSuccess" />
      </template>

      <p v-else class="rounded-md bg-sunken px-4 py-3 text-center text-sm text-muted">
        {{ t('login.noBootstrap') }}
      </p>

      <FederationButtons />

      <RouterLink to="/pair" class="cursor-pointer text-center text-sm text-muted underline underline-offset-4">
        {{ t('login.pairDevice') }}
      </RouterLink>
    </div>
  </CenteredLayout>
</template>
