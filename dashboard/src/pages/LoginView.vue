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
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api'
import { useReturnTo } from '@/composables/useReturnTo'
import CenteredLayout from '@/pages/CenteredLayout.vue'
import PasskeyButton from '@/components/custom/PasskeyButton.vue'
import PasswordTotpForm from '@/components/custom/PasswordTotpForm.vue'
import FederationButtons from '@/components/custom/FederationButtons.vue'
import OrDivider from '@/components/custom/OrDivider.vue'

const { t } = useI18n()
const { goReturnTo } = useReturnTo()

// Default to bootstrapped (show the sign-in methods); flip to the instruction
// only when /auth/status explicitly says no admin exists.
const bootstrapped = ref(true)

onMounted(async () => {
  try {
    const status = await api.get<{ bootstrapped: boolean }>('/api/prohibitorum/auth/status')
    bootstrapped.value = status.bootstrapped
  } catch {
    // If status can't be read, fail open to the sign-in methods.
    bootstrapped.value = true
  }
})

function onSuccess(): void {
  goReturnTo()
}
</script>

<template>
  <CenteredLayout>
    <template #title>
      <h1 class="text-xl font-semibold tracking-tight text-ink">{{ t('login.title') }}</h1>
    </template>

    <div class="flex flex-col gap-6">
      <template v-if="bootstrapped">
        <PasskeyButton @success="onSuccess" />

        <OrDivider :label="t('login.orDivider')" />

        <PasswordTotpForm @success="onSuccess" />
      </template>

      <p v-else class="rounded-md bg-sunken px-4 py-3 text-center text-sm text-muted">
        {{ t('login.noBootstrap') }}
      </p>

      <FederationButtons />

      <RouterLink to="/pair" class="text-center text-sm text-muted underline-offset-4 hover:underline">
        {{ t('login.pairDevice') }}
      </RouterLink>
    </div>
  </CenteredLayout>
</template>
