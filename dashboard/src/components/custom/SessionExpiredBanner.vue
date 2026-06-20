<script setup lang="ts">
/**
 * SessionExpiredBanner — shown when a session expires during an in-page
 * MUTATION (the read path redirects directly). Persistent + non-dismissable so
 * the user notices, but non-modal so they can copy any unsaved input before
 * re-authenticating. Mounted once app-wide in App.vue.
 */
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useSessionExpiry } from '@/composables/useSessionExpiry'
import { Button } from '@/components/ui/button'

const router = useRouter()
const { t } = useI18n()
const { expired, reset } = useSessionExpiry()

function signInAgain(): void {
  const returnTo = router.currentRoute.value.fullPath
  reset()
  void router.push({ name: 'login', query: { return_to: returnTo, reason: 'session_expired' } })
}
</script>

<template>
  <div
    v-if="expired"
    role="alert"
    class="fixed inset-x-0 top-0 z-50 flex items-center justify-center gap-4 px-4 py-3 text-sm shadow bg-destructive text-destructive-foreground"
  >
    <span>{{ t('sessionExpiry.message') }}</span>
    <Button size="sm" variant="outline" @click="signInAgain">
      {{ t('sessionExpiry.signInAgain') }}
    </Button>
  </div>
</template>
