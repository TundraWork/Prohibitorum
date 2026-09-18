<script setup lang="ts">
/**
 * OfflineBanner — a persistent top strip shown while the browser reports no
 * network connectivity (navigator.onLine === false). Mirrors the amber-toned
 * caution palette of MaintenanceBanner. Mounted once app-wide in App.vue, above
 * <RouterView/>.
 *
 * navigator.onLine is reactive only via the window online/offline events, so we
 * seed from it and keep it in sync with listeners scoped to the component's
 * lifetime.
 */
import { ref, onMounted, onUnmounted } from 'vue'
import { WifiOff } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const online = ref(navigator.onLine)

function handleOnline(): void {
  online.value = true
}
function handleOffline(): void {
  online.value = false
}

onMounted(() => {
  window.addEventListener('online', handleOnline)
  window.addEventListener('offline', handleOffline)
})

onUnmounted(() => {
  window.removeEventListener('online', handleOnline)
  window.removeEventListener('offline', handleOffline)
})
</script>

<template>
  <div
    v-if="!online"
    class="flex items-center gap-2 border-b border-amber-200 bg-amber-50 px-4 py-2 text-sm text-amber-700 dark:border-amber-900/60 dark:bg-amber-950/30 dark:text-amber-400"
    role="status"
    aria-live="polite"
  >
    <WifiOff class="size-4 shrink-0" aria-hidden="true" />
    {{ t('offline.message') }}
  </div>
</template>
