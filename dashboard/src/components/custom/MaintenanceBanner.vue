<script setup lang="ts">
/**
 * MaintenanceBanner — a slim persistent warning strip shown only to admins
 * while maintenance mode is on. Mirrors the amber-toned trust Alert in
 * AdminForwardAuthAppDetailView. Mounted in both DashboardLayout and
 * LauncherLayout.
 */
import { RouterLink } from 'vue-router'
import { TriangleAlert } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import { useBrandingStore } from '@/stores/branding'
import { useAuthStore } from '@/stores/auth'

const { t } = useI18n()
const branding = useBrandingStore()
const auth = useAuthStore()
</script>

<template>
  <div
    v-if="branding.maintenanceMode && auth.isAdmin"
    class="flex items-center justify-between gap-3 border-b border-amber-200 bg-amber-50 px-4 py-2 text-sm text-amber-700 dark:border-amber-900/60 dark:bg-amber-950/30 dark:text-amber-400"
    role="status"
    aria-live="polite"
  >
    <span class="flex items-center gap-2">
      <TriangleAlert class="size-4 shrink-0" aria-hidden="true" />
      {{ t('maintenance.adminBanner') }}
    </span>
    <RouterLink
      to="/admin/settings"
      class="shrink-0 text-xs font-medium underline underline-offset-4 hover:opacity-75"
    >
      {{ t('maintenance.adminBannerAction') }}
    </RouterLink>
  </div>
</template>
