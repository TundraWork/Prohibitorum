<script setup lang="ts">
/**
 * AdminInvitationsView (/admin/invitations) — list/create/revoke enrollment
 * invitations. Create is an inline form (not a ConfirmDialog — creating isn't
 * destructive). The list returns the full URL, so it stays copyable per row.
 */
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { withSudo } from '@/lib/sudo'
import { relativeTime, formatDateTime } from '@/lib/time'
import { Card, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/table'
import StatusBadge from '@/components/custom/StatusBadge.vue'
import ConfirmDialog from '@/components/custom/ConfirmDialog.vue'
import CodeField from '@/components/custom/CodeField.vue'

interface Invitation { token: string; url: string; role: string; attributes?: Record<string, unknown>; createdAt: string; expiresAt: string }
const { t, te } = useI18n()
const { busy, error, run } = useApi()
const rows = ref<Invitation[]>([])
const createOpen = ref(false)
const newRole = ref<'admin' | 'user'>('user')
const created = ref(false)
const revokeToken = ref<string | null>(null)
const errorText = computed(() => {
  const e = error.value
  if (!e) return ''
  const key = `errors.${e.code}`
  return te(key) ? t(key) : e.message || t('common.error')
})
async function load(): Promise<void> {
  const res = await run(() => api.get<Invitation[]>('/api/prohibitorum/invitations'))
  if (res) rows.value = res
}
async function create(): Promise<void> {
  created.value = false
  const ok = await run(() => withSudo(async () => {
    await api.post('/api/prohibitorum/invitations', { role: newRole.value })
    return true as const
  }))
  if (ok) { createOpen.value = false; created.value = true; await load() }
}
async function revoke(): Promise<void> {
  const token = revokeToken.value
  if (token == null) return
  const ok = await run(() => withSudo(async () => {
    await api.post('/api/prohibitorum/invitations/revoke', { token })
    return true as const
  }))
  revokeToken.value = null
  if (ok) await load()
}
onMounted(load)
</script>
<template>
  <div class="flex max-w-4xl flex-col gap-6">
    <div class="flex items-center justify-between gap-4">
      <h1 class="text-2xl font-semibold tracking-tight text-ink">{{ t('admin.invitations.title') }}</h1>
      <Button type="button" data-test="create" @click="createOpen = true">{{ t('admin.invitations.create') }}</Button>
    </div>
    <Alert v-if="errorText" variant="destructive" role="alert" aria-live="polite"><AlertDescription>{{ errorText }}</AlertDescription></Alert>
    <p v-if="created" class="text-sm text-sage" role="status">{{ t('admin.invitations.created') }}</p>

    <Card v-if="createOpen">
      <CardContent class="flex flex-col gap-3 py-4">
        <div class="flex flex-col gap-1.5">
          <label for="newRole" class="text-sm font-medium text-ink">{{ t('admin.invitations.role') }}</label>
          <select id="newRole" name="newRole" v-model="newRole" class="bg-sunken border-input h-9 w-fit rounded-md border px-3 text-sm text-ink">
            <option value="user">{{ t('admin.invitations.roleUser') }}</option>
            <option value="admin">{{ t('admin.invitations.roleAdmin') }}</option>
          </select>
        </div>
        <div class="flex gap-2">
          <Button type="button" :disabled="busy" data-test="create-confirm" @click="create">{{ t('admin.invitations.create') }}</Button>
          <Button type="button" variant="outline" :disabled="busy" data-test="create-cancel" @click="createOpen = false">{{ t('common.cancel') }}</Button>
        </div>
      </CardContent>
    </Card>

    <Table v-if="rows.length">
      <TableHeader>
        <TableRow>
          <TableHead>{{ t('admin.invitations.colRole') }}</TableHead>
          <TableHead>{{ t('admin.invitations.colCreated') }}</TableHead>
          <TableHead>{{ t('admin.invitations.colExpires') }}</TableHead>
          <TableHead>{{ t('admin.invitations.colLink') }}</TableHead>
          <TableHead></TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <TableRow v-for="inv in rows" :key="inv.token">
          <TableCell><StatusBadge :variant="inv.role === 'admin' ? 'caution' : 'neutral'">{{ inv.role === 'admin' ? t('admin.invitations.roleAdmin') : t('admin.invitations.roleUser') }}</StatusBadge></TableCell>
          <TableCell class="text-muted">{{ relativeTime(inv.createdAt) }}</TableCell>
          <TableCell class="text-muted">{{ formatDateTime(inv.expiresAt) }}</TableCell>
          <TableCell><CodeField :value="inv.url" /></TableCell>
          <TableCell><Button type="button" variant="outline" size="sm" :disabled="busy" :data-test="`revoke-${inv.token}`" @click="revokeToken = inv.token">{{ t('admin.invitations.revoke') }}</Button></TableCell>
        </TableRow>
      </TableBody>
    </Table>
    <p v-else-if="!busy && !errorText" class="text-sm text-muted">{{ t('admin.invitations.empty') }}</p>

    <ConfirmDialog :open="revokeToken !== null" :title="t('admin.invitations.revokeConfirmTitle')" :confirm-label="t('admin.invitations.revoke')" :busy="busy"
      @update:open="(v) => { if (!v) revokeToken = null }" @cancel="revokeToken = null" @confirm="revoke">
      {{ t('admin.invitations.revokeConfirmBody') }}
    </ConfirmDialog>
  </div>
</template>
