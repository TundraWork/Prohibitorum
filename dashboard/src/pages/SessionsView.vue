<script setup lang="ts">
/**
 * SessionsView (/sessions) — list active sessions; revoke non-current ones.
 * GET /me/sessions → SessionListItem[]; POST /me/sessions/revoke {id} (not
 * sudo-gated). The current session has no revoke control.
 */
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { Card, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Alert, AlertDescription } from '@/components/ui/alert'
import StatusBadge from '@/components/custom/StatusBadge.vue'

interface SessionListItem {
  id: string
  isCurrent: boolean
  issuedAt: string
  expiresAt: string
  lastSeenIp: string
  userAgent?: string
}

const { t, te } = useI18n()
const { busy, error, run } = useApi()

const fmt = (d: string) => { const t = Date.parse(d); return Number.isNaN(t) ? '' : new Date(t).toLocaleString() }

const errorText = computed(() => {
  const e = error.value
  if (!e) return ''
  const key = `errors.${e.code}`
  return te(key) ? t(key) : e.message || t('common.error')
})
const rows = ref<SessionListItem[]>([])

async function load(): Promise<void> {
  const res = await run(() => api.get<SessionListItem[]>('/api/prohibitorum/me/sessions'))
  if (res) rows.value = res
}
async function revoke(id: string): Promise<void> {
  const ok = await run(async () => {
    await api.post('/api/prohibitorum/me/sessions/revoke', { id })
    return true as const
  })
  if (ok) await load()
}
onMounted(load)
</script>

<template>
  <div class="flex max-w-2xl flex-col gap-4">
    <h1 class="text-lg font-semibold tracking-tight text-ink">{{ t('sessions.title') }}</h1>
    <Alert v-if="errorText" variant="destructive" role="alert" aria-live="polite">
      <AlertDescription>{{ errorText }}</AlertDescription>
    </Alert>
    <Card v-for="r in rows" :key="r.id">
      <CardContent class="flex items-center justify-between gap-4 py-4">
        <div class="flex flex-col gap-1 text-sm">
          <div class="flex items-center gap-2">
            <span class="min-w-0 truncate text-ink">{{ r.userAgent || r.lastSeenIp }}</span>
            <StatusBadge v-if="r.isCurrent" variant="success" class="shrink-0">{{ t('sessions.current') }}</StatusBadge>
          </div>
          <span class="text-muted">{{ t('sessions.lastSeen') }}: {{ r.lastSeenIp }}</span>
          <span v-if="fmt(r.issuedAt)" class="text-muted">{{ t('sessions.issued') }}: {{ fmt(r.issuedAt) }}</span>
          <span v-if="fmt(r.expiresAt)" class="text-muted">{{ t('sessions.expires') }}: {{ fmt(r.expiresAt) }}</span>
        </div>
        <Button v-if="!r.isCurrent" variant="outline" size="sm" :disabled="busy"
                data-test="revoke" @click="revoke(r.id)">
          {{ t('sessions.revoke') }}
        </Button>
      </CardContent>
    </Card>
    <p v-if="!busy && rows.length === 0 && !errorText" class="text-sm text-muted">{{ t('sessions.empty') }}</p>
  </div>
</template>
