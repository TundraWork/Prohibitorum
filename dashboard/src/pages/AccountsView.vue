<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '../lib/api'
import CopyableUrl from '../components/CopyableUrl.vue'

interface AccountView {
  id: number
  username: string
  displayName: string
  role: string
  disabled: boolean
  createdAt: string
  updatedAt: string
  lastSignInAt?: string | null
}

const { t, te } = useI18n()
const rows = ref<AccountView[]>([])
const error = ref('')
const busy = ref(false)
const armed = ref<number | null>(null)
const reissued = ref<{ id: number; url: string } | null>(null)

function show(e: any) {
  const code = e?.code as string | undefined
  error.value = code && te('errors.' + code) ? t('errors.' + code) : (e?.message ?? t('errors.server_error'))
}

async function load() {
  error.value = ''
  try {
    rows.value = await api.get<AccountView[]>('/api/prohibitorum/accounts')
  } catch (e) { show(e) }
}

async function toggle(a: AccountView) {
  if (busy.value) return
  busy.value = true
  error.value = ''
  try {
    await api.put(`/api/prohibitorum/accounts/${a.id}`, { displayName: a.displayName, role: a.role, disabled: !a.disabled })
    await load()
  } catch (e) { show(e) } finally { busy.value = false }
}

async function del(id: number) {
  if (busy.value) return
  busy.value = true
  error.value = ''
  try {
    await api.post('/api/prohibitorum/accounts/delete', { id })
    armed.value = null
    await load()
  } catch (e) { show(e); armed.value = null } finally { busy.value = false }
}

async function reissue(id: number) {
  if (busy.value) return
  busy.value = true
  error.value = ''
  reissued.value = null
  try {
    const r = await api.post<{ url: string }>('/api/prohibitorum/accounts/reissue-enrollment', { id })
    reissued.value = { id, url: r.url }
  } catch (e) { show(e) } finally { busy.value = false }
}

onMounted(load)
</script>

<template>
  <div class="space-y-4">
    <h1 class="text-lg font-semibold">{{ t('accounts.title') }}</h1>
    <p v-if="error" role="alert" aria-live="polite" class="text-error text-sm">{{ error }}</p>
    <table class="w-full text-sm border-collapse">
      <thead>
        <tr class="text-left text-muted border-b border-default">
          <th class="py-2 pr-4">{{ t('accounts.username') }}</th>
          <th class="py-2 pr-4">{{ t('accounts.displayName') }}</th>
          <th class="py-2 pr-4">{{ t('accounts.role') }}</th>
          <th class="py-2 pr-4">{{ t('accounts.status') }}</th>
          <th class="py-2 pr-4">{{ t('accounts.lastSignIn') }}</th>
          <th class="py-2 pr-4">{{ t('accounts.actions') }}</th>
        </tr>
      </thead>
      <tbody>
        <template v-for="a in rows" :key="a.id">
          <tr class="border-b border-default/50">
            <td class="py-2 pr-4 font-mono text-xs">
              <RouterLink :to="`/admin/accounts/${a.id}`" class="text-primary hover:underline">{{ a.username }}</RouterLink>
            </td>
            <td class="py-2 pr-4">{{ a.displayName }}</td>
            <td class="py-2 pr-4">{{ a.role }}</td>
            <td class="py-2 pr-4">
              <UBadge size="sm" :color="a.disabled ? 'error' : 'success'">
                {{ a.disabled ? t('accounts.disabled') : t('accounts.active') }}
              </UBadge>
            </td>
            <td class="py-2 pr-4">{{ a.lastSignInAt ? new Date(a.lastSignInAt).toLocaleString() : t('accounts.never') }}</td>
            <td class="py-2 pr-4">
              <div class="inline-flex items-center gap-1">
                <UButton data-test="toggle" type="button" size="xs" variant="soft" :disabled="busy" @click="toggle(a)">
                  {{ a.disabled ? t('common.enable') : t('common.disable') }}
                </UButton>
                <UButton data-test="reissue" type="button" size="xs" variant="soft" :disabled="busy" @click="reissue(a.id)">
                  {{ t('accounts.reissue') }}
                </UButton>
                <template v-if="armed === a.id">
                  <UButton type="button" size="xs" color="error" :disabled="busy" @click="del(a.id)">{{ t('common.confirm') }}</UButton>
                  <UButton type="button" size="xs" color="neutral" variant="ghost" @click="armed = null">{{ t('common.cancel') }}</UButton>
                </template>
                <UButton v-else type="button" size="xs" color="error" variant="soft" @click="armed = a.id">{{ t('common.delete') }}</UButton>
              </div>
            </td>
          </tr>
          <tr v-if="reissued && reissued.id === a.id">
            <td colspan="6" class="py-2 pr-4">
              <p class="text-xs text-muted mb-1">{{ t('accounts.reissued') }}</p>
              <CopyableUrl :url="reissued.url" />
            </td>
          </tr>
        </template>
      </tbody>
    </table>
  </div>
</template>
