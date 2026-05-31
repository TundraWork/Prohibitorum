<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { api } from '../lib/api'

const { t } = useI18n()
const route = useRoute()
const done = ref(false)
// post_logout_redirect_uri is pre-validated by the backend /oidc/logout (exact-match
// against the client's registered post_logout_redirect_uris) before the browser is
// redirected here; no same-origin guard — the RP target is legitimately external.
const postLogout = typeof route.query.post_logout_redirect_uri === 'string'
  ? route.query.post_logout_redirect_uri
  : ''

onMounted(async () => {
  try { await api.post('/api/prohibitorum/auth/logout') } catch { /* idempotent: ignore */ }
  done.value = true
})

function navigateTo(url: string) {
  window.location.assign(url)
}
</script>

<template>
  <UCard v-if="done" class="w-full max-w-md">
    <div class="flex flex-col items-center gap-4 text-center">
      <UIcon name="i-lucide-circle-check" class="size-10 text-success" />
      <h1 class="text-xl font-semibold">{{ t('logout.done') }}</h1>
      <UButton
        v-if="postLogout"
        type="button"
        @click="navigateTo(postLogout)"
      >
        {{ t('logout.returnTo', { app: t('app.name') }) }}
      </UButton>
    </div>
  </UCard>
  <div v-else class="flex justify-center mt-24">
    <UIcon name="i-lucide-loader-2" class="size-8 animate-spin text-muted" />
  </div>
</template>
