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
import { safeReturnTo } from '@/lib/returnTo'

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

const message = computed(() => {
  if (appAccessDenied.value) return t('error.appAccessDenied', { app: app.value })
  const key = `errors.codes.${code.value}`
  if (code.value && te(key)) return t(key)
  if (description.value) return description.value
  return t('error.defaultMessage')
})

const reference = computed(() => String(route.query.ref ?? ''))
const hasSession = ref(false)

// When the failing flow forwarded a server-validated return_to (e.g. an
// identity-link begin from /connected), the "go back" link returns the user
// there. Re-guarded through safeReturnTo (defense in depth); a root/invalid
// value falls through to the auth-aware default below.
const backTarget = computed(() => {
  const raw = route.query.return_to
  if (typeof raw !== 'string' || !raw) return ''
  const safe = safeReturnTo(raw)
  return safe !== '/' ? safe : ''
})

onMounted(async () => {
  // Public route — a 401 here is fine; the global handler no-ops on /error.
  try { await auth.ensureLoaded() } catch { /* ignore */ }
  hasSession.value = !!auth.me
})
</script>

<template>
  <CenteredLayout>
    <template #title>
      <h1 role="alert" class="text-balance text-xl font-semibold tracking-tight text-ink">{{ message }}</h1>
    </template>

    <div class="flex flex-col items-center gap-6 text-center">
      <p v-if="reference" class="text-xs text-muted">{{ t('error.reference', { ref: reference }) }}</p>
      <Button as-child variant="outline" class="w-full">
        <RouterLink v-if="backTarget" :to="backTarget">{{ t('error.goBack') }}</RouterLink>
        <RouterLink v-else-if="hasSession" to="/security">{{ t('error.backToDashboard') }}</RouterLink>
        <RouterLink v-else to="/login">{{ t('error.returnToLogin') }}</RouterLink>
      </Button>
    </div>
  </CenteredLayout>
</template>
