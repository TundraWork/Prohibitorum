<script setup lang="ts">
import { onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { safeReturnTo } from '../lib/returnTo'
import { useSessionStore } from '../stores/session'
import PasskeyButton from '../components/PasskeyButton.vue'
import FederationButtons from '../components/FederationButtons.vue'
import PasswordTotpForm from '../components/PasswordTotpForm.vue'

const { t } = useI18n()

// Resolve the post-login target. safeReturnTo enforces same-origin; default '/'.
const rawReturnTo = new URLSearchParams(window.location.search).get('return_to')
const returnTo = safeReturnTo(rawReturnTo) ?? '/'

const returnToUrl = new URL(returnTo, window.location.origin)
// Forced re-auth: when the target carries ?reauth we must NOT skip the form even
// if a live session exists.
const hasReauth = returnToUrl.searchParams.has('reauth')
// Federation requires a RELATIVE return_to (path + query only).
const relativeReturnTo = returnToUrl.pathname + returnToUrl.search

const session = useSessionStore()

onMounted(async () => {
  await session.fetchMe()
  if (session.me && !hasReauth) window.location.assign(returnTo)
})

function go() {
  window.location.assign(returnTo)
}
</script>

<template>
  <UCard class="w-full max-w-sm mx-auto mt-16">
    <h1 class="text-xl font-semibold text-center">{{ t('login.title') }}</h1>

    <div class="mt-6 flex flex-col gap-4">
      <PasskeyButton @success="go" />

      <FederationButtons :relative-return-to="relativeReturnTo" />

      <div class="flex items-center gap-3 text-sm text-muted">
        <span class="h-px flex-1 bg-default" />
        <span>{{ t('login.or') }}</span>
        <span class="h-px flex-1 bg-default" />
      </div>

      <PasswordTotpForm @success="go" />
    </div>
  </UCard>
</template>
