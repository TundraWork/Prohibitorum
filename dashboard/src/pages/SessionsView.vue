<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '../lib/api'

interface SessionListItem {
  id: string
  isCurrent: boolean
  issuedAt: string
  expiresAt: string
  lastSeenIp: string
  userAgent?: string
}

const { t, te } = useI18n()
const rows = ref<SessionListItem[]>([])
const error = ref('')
const busy = ref(false)

function show(e: any) {
  const code = e?.code as string | undefined
  error.value = code && te('errors.' + code) ? t('errors.' + code) : (e?.message ?? t('errors.server_error'))
}

async function load() {
  error.value = ''
  try {
    rows.value = await api.get<SessionListItem[]>('/api/prohibitorum/me/sessions')
  } catch (e) { show(e) }
}

async function revoke(id: string) {
  if (busy.value) return
  busy.value = true
  error.value = ''
  try {
    await api.post('/api/prohibitorum/me/sessions/revoke', { id })
    await load()
  } catch (e) { show(e) } finally { busy.value = false }
}

onMounted(load)
</script>

<template>
  <div class="space-y-4">
    <h1 class="text-lg font-semibold">{{ t('sessions.title') }}</h1>
    <p v-if="error" role="alert" aria-live="polite" class="text-error text-sm">{{ error }}</p>
    <table class="w-full text-sm border-collapse">
      <thead>
        <tr class="text-left text-muted border-b border-default">
          <th class="py-2 pr-4">{{ t('sessions.device') }}</th>
          <th class="py-2 pr-4">{{ t('sessions.ip') }}</th>
          <th class="py-2 pr-4">{{ t('sessions.issuedAt') }}</th>
          <th class="py-2 pr-4">{{ t('sessions.expiresAt') }}</th>
          <th class="py-2 pr-4">{{ t('sessions.actions') }}</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="r in rows" :key="r.id" class="border-b border-default/50">
          <td class="py-2 pr-4">
            {{ r.userAgent || '—' }}
            <UBadge v-if="r.isCurrent" size="sm" color="primary" class="ml-2">{{ t('sessions.current') }}</UBadge>
          </td>
          <td class="py-2 pr-4 font-mono text-xs">{{ r.lastSeenIp }}</td>
          <td class="py-2 pr-4">{{ new Date(r.issuedAt).toLocaleString() }}</td>
          <td class="py-2 pr-4">{{ new Date(r.expiresAt).toLocaleString() }}</td>
          <td class="py-2 pr-4">
            <UButton
              v-if="!r.isCurrent"
              data-test="revoke"
              type="button"
              size="xs"
              color="error"
              variant="soft"
              :disabled="busy"
              @click="revoke(r.id)"
            >
              {{ t('sessions.revoke') }}
            </UButton>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
