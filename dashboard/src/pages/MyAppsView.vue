<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { useAuthStore } from '@/stores/auth'
import AppTile, { type LaunchpadApp } from '@/components/custom/AppTile.vue'
import EmptyState from '@/components/custom/EmptyState.vue'
import TableSkeleton from '@/components/custom/TableSkeleton.vue'
import { Alert, AlertDescription } from '@/components/ui/alert'
import ConfirmDialog from '@/components/custom/ConfirmDialog.vue'

interface Consent { clientId: string; scopes: string[] }

const { t } = useI18n()
const { busy, run, errorText } = useApi()
const auth = useAuthStore()

const apps = ref<LaunchpadApp[]>([])
const consents = ref<Map<string, Consent>>(new Map())
const revokeTarget = ref<LaunchpadApp | null>(null)

const firstName = computed(() => (auth.me?.displayName ?? '').split(' ')[0] || auth.me?.username || '')

// Only OIDC apps can carry a consent record — forward-auth and SAML apps never
// traverse the consent flow — so the `kind === 'oidc'` gate is intentional.
function consentFor(app: LaunchpadApp): Consent | null {
  return app.kind === 'oidc' ? consents.value.get(app.id) ?? null : null
}

async function load(): Promise<void> {
  const [a, c] = await Promise.all([
    run(() => api.get<LaunchpadApp[]>('/api/prohibitorum/me/apps')),
    api.get<Consent[]>('/api/prohibitorum/me/consent').catch(() => [] as Consent[]),
  ])
  if (a) apps.value = a
  consents.value = new Map(c.map((x) => [x.clientId, x]))
}

async function confirmRevoke(): Promise<void> {
  const app = revokeTarget.value
  if (!app) return
  const ok = await run(async () => {
    await api.post('/api/prohibitorum/me/consent/revoke', { clientId: app.id })
    return true as const
  })
  revokeTarget.value = null
  if (ok) await load()
}

onMounted(load)
</script>

<template>
  <div class="flex flex-col gap-6">
    <div>
      <p class="text-sm text-muted">{{ t('myApps.greeting', { name: firstName }) }}</p>
      <h1 class="text-2xl font-semibold tracking-tight text-ink">{{ t('myApps.title') }}</h1>
    </div>

    <Alert v-if="errorText" variant="destructive" role="alert" aria-live="polite">
      <AlertDescription>{{ errorText }}</AlertDescription>
    </Alert>

    <TableSkeleton v-if="busy && !apps.length" :rows="2" :cols="3" />
    <div v-else-if="apps.length" class="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4">
      <AppTile v-for="app in apps" :key="`${app.kind}:${app.id}`" :app="app" :consent="consentFor(app)" @revoke="revokeTarget = $event" />
    </div>
    <EmptyState v-else-if="!errorText" :title="t('myApps.empty')" :description="t('myApps.emptyHelp')" />

    <ConfirmDialog
      :open="revokeTarget !== null"
      :title="t('myApps.revokeConfirmTitle')"
      :confirm-label="t('myApps.revoke')"
      :busy="busy"
      @update:open="(v) => { if (!v) revokeTarget = null }"
      @cancel="revokeTarget = null"
      @confirm="confirmRevoke"
    >
      {{ t('myApps.revokeConfirmBody', { name: revokeTarget?.name }) }}
    </ConfirmDialog>
  </div>
</template>
