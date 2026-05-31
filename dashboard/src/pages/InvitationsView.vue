<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '../lib/api'
import CopyableUrl from '../components/CopyableUrl.vue'

interface InvitationView {
  token: string
  url: string
  role: string
  createdAt: string
  expiresAt: string
}

const { t, te } = useI18n()
const rows = ref<InvitationView[]>([])
const error = ref('')
const busy = ref(false)
const newRole = ref('user')
const armed = ref<string | null>(null)
const created = ref<string>('')

function show(e: any) {
  const code = e?.code as string | undefined
  error.value = code && te('errors.' + code) ? t('errors.' + code) : (e?.message ?? t('errors.server_error'))
}

async function load() {
  error.value = ''
  try {
    rows.value = await api.get<InvitationView[]>('/api/prohibitorum/invitations')
  } catch (e) { show(e) }
}

async function create() {
  if (busy.value) return
  busy.value = true
  error.value = ''
  created.value = ''
  try {
    const r = await api.post<{ url: string }>('/api/prohibitorum/invitations', { role: newRole.value })
    created.value = r.url
    await load()
  } catch (e) { show(e) } finally { busy.value = false }
}

async function revoke(token: string) {
  if (busy.value) return
  busy.value = true
  error.value = ''
  try {
    await api.post('/api/prohibitorum/invitations/revoke', { token })
    armed.value = null
    await load()
  } catch (e) { show(e); armed.value = null } finally { busy.value = false }
}

onMounted(load)
</script>

<template>
  <div class="space-y-4">
    <h1 class="text-lg font-semibold">{{ t('invitations.title') }}</h1>
    <p v-if="error" role="alert" aria-live="polite" class="text-error text-sm">{{ error }}</p>

    <div class="flex items-end gap-2">
      <USelect v-model="newRole" :items="[t('invitations.roleUser'), t('invitations.roleAdmin')]" class="w-40" />
      <UButton data-test="create" type="button" :disabled="busy" @click="create">{{ t('invitations.create') }}</UButton>
    </div>
    <div v-if="created" class="space-y-1">
      <p class="text-xs text-muted">{{ t('invitations.created') }}</p>
      <CopyableUrl :url="created" />
    </div>

    <table class="w-full text-sm border-collapse">
      <thead>
        <tr class="text-left text-muted border-b border-default">
          <th class="py-2 pr-4">{{ t('invitations.role') }}</th>
          <th class="py-2 pr-4">{{ t('invitations.url') }}</th>
          <th class="py-2 pr-4">{{ t('invitations.expiresAt') }}</th>
          <th class="py-2 pr-4">{{ t('invitations.actions') }}</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="inv in rows" :key="inv.token" class="border-b border-default/50">
          <td class="py-2 pr-4">{{ inv.role }}</td>
          <td class="py-2 pr-4 w-1/2"><CopyableUrl :url="inv.url" /></td>
          <td class="py-2 pr-4">{{ new Date(inv.expiresAt).toLocaleDateString() }}</td>
          <td class="py-2 pr-4">
            <div class="inline-flex items-center gap-1">
              <template v-if="armed === inv.token">
                <UButton data-test="revoke" type="button" size="xs" color="error" :disabled="busy" @click="revoke(inv.token)">{{ t('common.confirm') }}</UButton>
                <UButton type="button" size="xs" color="neutral" variant="ghost" @click="armed = null">{{ t('common.cancel') }}</UButton>
              </template>
              <UButton v-else data-test="revoke" type="button" size="xs" color="error" variant="soft" @click="armed = inv.token">{{ t('common.revoke') }}</UButton>
            </div>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
