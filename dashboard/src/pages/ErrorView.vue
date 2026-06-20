<script setup lang="ts">
/**
 * ErrorView — the plain-language error landing (/error?error=…&error_description=…).
 *
 * Renders a human message from the query (PRODUCT.md: no jargon, no stack
 * traces, no codes shown to the user). Resolution order:
 *   1. errors.<error> i18n copy (the known backend / OIDC code)
 *   2. the raw error_description (only if it looks human — see below)
 *   3. a generic fallback
 *
 * `error` here can be a backend AuthError code, an OIDC error (e.g.
 * access_denied, server_error), or our own routed codes (e.g. forbidden from
 * the router guard). error_description is shown only as a fallback because
 * upstream descriptions are sometimes terse/technical.
 */
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import CenteredLayout from '@/pages/CenteredLayout.vue'
import { Button } from '@/components/ui/button'
import { useAuthStore } from '@/stores/auth'

const route = useRoute()
const { t, te } = useI18n()
const auth = useAuthStore()

const code = computed(() => String(route.query.error ?? ''))
const description = computed(() => {
  const d = route.query.error_description
  return typeof d === 'string' ? d : ''
})

// reason is our own routed signal (distinct from the OIDC `error` code). The
// per-app access denial (?reason=app_access_denied&app=…) is sent here by the
// OIDC authorize and SAML SSO handlers when an authenticated user is not
// authorized for a restricted application.
const reason = computed(() => String(route.query.reason ?? ''))
const app = computed(() => {
  const a = route.query.app
  return typeof a === 'string' ? a : ''
})
const appAccessDenied = computed(() => reason.value === 'app_access_denied')

const title = computed(() =>
  appAccessDenied.value ? t('error.appAccessDeniedTitle') : t('error.title'),
)

const message = computed(() => {
  if (appAccessDenied.value) return t('error.appAccessDenied', { app: app.value })
  const key = `errors.${code.value}`
  if (code.value && te(key)) return t(key)
  if (description.value) return description.value
  return t('error.defaultMessage')
})

const reference = computed(() => String(route.query.ref ?? ''))
const hasSession = ref(false)

onMounted(async () => {
  // Public route — a 401 here is fine; the global handler no-ops on /error.
  try { await auth.ensureLoaded() } catch { /* ignore */ }
  hasSession.value = !!auth.me
})
</script>

<template>
  <CenteredLayout>
    <template #title>
      <h1 class="text-xl font-semibold tracking-tight text-ink">{{ title }}</h1>
    </template>

    <div class="flex flex-col items-center gap-6 text-center">
      <p role="alert" class="text-sm text-ink">{{ message }}</p>
      <p v-if="reference" class="text-xs text-muted">{{ t('error.reference', { ref: reference }) }}</p>
      <Button as-child variant="outline" class="w-full">
        <RouterLink v-if="hasSession" to="/security">{{ t('error.backToDashboard') }}</RouterLink>
        <RouterLink v-else to="/login">{{ t('error.returnToLogin') }}</RouterLink>
      </Button>
    </div>
  </CenteredLayout>
</template>
