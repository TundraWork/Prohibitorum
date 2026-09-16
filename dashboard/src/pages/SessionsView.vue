<script setup lang="ts">
/**
 * SessionsView (/sessions) — list active sessions; revoke non-current ones.
 * GET /me/sessions → SessionListItem[]; POST /me/sessions/revoke {id} (not
 * sudo-gated). The current session has no revoke control.
 */
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useResource } from '@/composables/useResource'
import { memberQuery } from '@/queries/resources'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { relativeTime, formatDateTime } from '@/lib/time'
import { CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import StatusBadge from '@/components/custom/StatusBadge.vue'
import BareCard from '@/components/custom/BareCard.vue'
import UserAgentDisplay from '@/components/custom/UserAgentDisplay.vue'
import ConfirmDialog from '@/components/custom/ConfirmDialog.vue'
import TableSkeleton from '@/components/custom/TableSkeleton.vue'
import EmptyState from '@/components/custom/EmptyState.vue'
import ErrorPanel from '@/components/custom/ErrorPanel.vue'
import { MonitorSmartphone } from 'lucide-vue-next'

interface SessionListItem {
  id: string
  isCurrent: boolean
  issuedAt: string
  expiresAt: string
  lastSeenIp: string
  userAgent?: string
}

const { t } = useI18n()
const { busy: mutationBusy, run, error: mutationError, clear: clearMutation } = useApi('sessions')

const query = useResource(memberQuery<SessionListItem[]>('sessions'))
const rows = computed(() => query.data.value ?? [])
const busy = computed(() => mutationBusy.value || query.busy.value)
const error = computed(() => mutationError.value ?? query.error.value)
function clear(): void { clearMutation(); query.clear() }
const confirmRevokeId = ref<string | null>(null)

async function revoke(): Promise<void> {
  const id = confirmRevokeId.value
  if (id == null) return
  await run(async () => {
    await api.post('/api/prohibitorum/me/sessions/revoke', { id })
    return true as const
  })
  confirmRevokeId.value = null
}
</script>

<template>
  <div class="flex max-w-2xl flex-col gap-6">
    <h1 class="text-2xl font-semibold tracking-tight text-ink">{{ t('sessions.title') }}</h1>
    <ErrorPanel :error="error" @dismiss="clear" />
    <TableSkeleton v-if="busy && !rows.length" :rows="3" :cols="1" />
    <template v-else-if="rows.length">
      <BareCard v-for="r in rows" :key="r.id">
        <CardContent class="flex items-center justify-between gap-4 py-4">
          <div class="flex min-w-0 flex-1 flex-col gap-1 text-sm">
            <UserAgentDisplay :ua="r.userAgent" class="min-w-0 text-ink">
              <template #badge>
                <StatusBadge v-if="r.isCurrent" variant="success" class="shrink-0">{{ t('sessions.current') }}</StatusBadge>
              </template>
            </UserAgentDisplay>
            <span class="truncate text-muted">{{ t('sessions.ipAddress') }}: <span class="font-mono">{{ r.lastSeenIp }}</span></span>
            <span v-if="r.issuedAt" class="truncate text-muted">{{ t('sessions.issued') }}: {{ relativeTime(r.issuedAt) }}</span>
            <span v-if="r.expiresAt" class="truncate text-muted">{{ t('sessions.expires') }}: {{ formatDateTime(r.expiresAt) }}</span>
          </div>
          <Button v-if="!r.isCurrent" variant="outline" size="sm" class="shrink-0" :disabled="busy"
                  data-test="revoke" @click="confirmRevokeId = r.id">
            {{ t('sessions.revoke') }}
          </Button>
        </CardContent>
      </BareCard>
    </template>
    <EmptyState v-else-if="!error" :icon="MonitorSmartphone" :title="t('sessions.empty')" />

    <ConfirmDialog
      :open="confirmRevokeId !== null"
      :title="t('sessions.revokeConfirmTitle')"
      :confirm-label="t('sessions.revoke')"
      :busy="busy"
      @update:open="(v) => { if (!v) confirmRevokeId = null }"
      @cancel="confirmRevokeId = null"
      @confirm="revoke"
    >
      {{ t('sessions.revokeConfirmBody') }}
    </ConfirmDialog>
  </div>
</template>
