<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { relativeTime } from '@/lib/time'
import { Card, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import ConfirmDialog from '@/components/custom/ConfirmDialog.vue'
import TableSkeleton from '@/components/custom/TableSkeleton.vue'
import EmptyState from '@/components/custom/EmptyState.vue'
import AppIcon from '@/components/custom/AppIcon.vue'
import ErrorPanel from '@/components/custom/ErrorPanel.vue'

interface ConsentedApp { kind: 'oidc' | 'saml'; clientId: string; name: string; iconUrl?: string | null; scopes: string[]; grantedAt: string }

const { t } = useI18n()
const { busy, run, error, clear } = useApi()
const apps = ref<ConsentedApp[]>([])
const revokeTarget = ref<ConsentedApp | null>(null)

async function load(): Promise<void> {
  const res = await run(() => api.get<ConsentedApp[]>('/api/prohibitorum/me/consent'))
  if (res) apps.value = res
}
async function confirmRevoke(): Promise<void> {
  const app = revokeTarget.value
  if (!app) return
  const ok = await run(async () => { await api.post('/api/prohibitorum/me/consent/revoke', { kind: app.kind, clientId: app.clientId }); return true as const })
  revokeTarget.value = null
  if (ok) await load()
}
onMounted(load)
</script>

<template>
  <div class="flex max-w-2xl flex-col gap-6">
    <h1 class="text-2xl font-semibold tracking-tight text-ink">{{ t('appAccess.title') }}</h1>
    <p class="text-sm text-muted">{{ t('appAccess.help') }}</p>

    <ErrorPanel :error="error" @dismiss="clear" />

    <TableSkeleton v-if="busy && !apps.length" :rows="2" :cols="1" />
    <template v-else-if="apps.length">
      <Card v-for="app in apps" :key="`${app.kind}:${app.clientId}`">
        <CardContent class="flex items-center justify-between gap-4 py-4">
          <div class="flex min-w-0 flex-1 items-center gap-3">
            <AppIcon :src="app.iconUrl" :name="app.name" size="sm" />
            <div class="flex min-w-0 flex-col text-sm">
              <div class="flex min-w-0 items-center gap-2">
                <span class="min-w-0 truncate font-medium text-ink">{{ app.name }}</span>
                <span class="shrink-0 rounded px-1 py-0.5 text-xs text-muted ring-1 ring-border">{{ app.kind === 'saml' ? t('appAccess.kindSaml') : t('appAccess.kindOidc') }}</span>
              </div>
              <span v-if="app.kind === 'saml'" class="min-w-0 truncate text-muted">{{ t('appAccess.samlDescriptor') }}</span>
              <span v-else class="min-w-0 truncate text-muted">{{ t('appAccess.scopes') }}: {{ app.scopes.join(', ') }}</span>
              <span v-if="app.grantedAt" class="truncate text-muted">{{ t('appAccess.grantedOn', { date: relativeTime(app.grantedAt) }) }}</span>
            </div>
          </div>
          <Button variant="outline" size="sm" class="shrink-0" :disabled="busy" :data-test="`revoke-${app.kind}-${app.clientId}`" @click="revokeTarget = app">
            {{ t('appAccess.revoke') }}
          </Button>
        </CardContent>
      </Card>
    </template>
    <EmptyState v-else-if="!error" :title="t('appAccess.empty')" />

    <ConfirmDialog
      :open="revokeTarget !== null"
      :title="t('appAccess.revokeConfirmTitle')"
      :confirm-label="t('appAccess.revoke')"
      :busy="busy"
      @update:open="(v) => { if (!v) revokeTarget = null }"
      @cancel="revokeTarget = null"
      @confirm="confirmRevoke"
    >
      {{ t('appAccess.revokeConfirmBody', { name: revokeTarget?.name }) }}
    </ConfirmDialog>
  </div>
</template>
