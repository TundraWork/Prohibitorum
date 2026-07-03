<script setup lang="ts">
/**
 * MaintenanceView — shown when the instance is in maintenance mode.
 *
 * Warm and reassuring: the user is not blocked forever, just briefly paused.
 * Uses CenteredLayout (the threshold idiom) so it inherits the ember brand mark
 * and the drenched backdrop — the same visual language as the login page.
 *
 * Admins are never redirected here (the router guard exempts isAdmin).
 * Non-admins land here either via:
 *   a) the router guard (on nav while maintenanceMode is already true), or
 *   b) the maintenance handler in api.ts (after a 503 maintenance_mode response).
 */
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import CenteredLayout from '@/pages/CenteredLayout.vue'
import { Button } from '@/components/ui/button'
import { useBrandingStore } from '@/stores/branding'
import { useAuthStore } from '@/stores/auth'

const { t } = useI18n()
const branding = useBrandingStore()
const auth = useAuthStore()

const hasSession = ref(false)

onMounted(async () => {
  // Public route — a 401 here is fine; we just want to know if there is
  // already a session so we can offer "Sign out" (mirrors ErrorView).
  try { await auth.ensureLoaded() } catch { /* ignore */ }
  hasSession.value = !!auth.me
})

function reload(): void {
  window.location.reload()
}
</script>

<template>
  <CenteredLayout>
    <template #title>
      <h1 class="text-lg font-semibold tracking-tight text-ink">{{ t('maintenance.heading') }}</h1>
    </template>

    <div class="flex flex-col items-center gap-5 text-center">
      <p class="text-sm text-muted">{{ t('maintenance.body', { instance: branding.instanceName }) }}</p>

      <!-- Optional note from the admin -->
      <div
        v-if="branding.maintenanceMessage"
        class="w-full rounded-md bg-accent px-4 py-3 text-left text-sm text-ink"
      >
        <p class="mb-1 text-xs text-muted">{{ t('maintenance.noteLabel') }}</p>
        <p>{{ branding.maintenanceMessage }}</p>
      </div>

      <Button variant="outline" class="w-full" @click="reload">
        {{ t('maintenance.retry') }}
      </Button>

      <!-- Authenticated users who are stuck here can sign out; unauthenticated
           visitors get the deliberate admin-login entry (the form is otherwise
           unreachable during maintenance). -->
      <RouterLink
        v-if="hasSession"
        to="/logout"
        class="text-xs text-muted underline underline-offset-4 hover:text-ink"
      >
        {{ t('maintenance.signOut') }}
      </RouterLink>
      <RouterLink
        v-else
        to="/login?admin=1"
        class="text-xs text-muted underline underline-offset-4 hover:text-ink"
      >
        {{ t('maintenance.adminSignIn') }}
      </RouterLink>
    </div>
  </CenteredLayout>
</template>
