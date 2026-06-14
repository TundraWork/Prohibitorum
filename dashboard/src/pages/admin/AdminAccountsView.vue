<script setup lang="ts">
/** AdminAccountsView (/admin/accounts) — table of accounts; row → detail. */
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { relativeTime } from '@/lib/time'
import { Button } from '@/components/ui/button'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/table'
import StatusBadge from '@/components/custom/StatusBadge.vue'
import UserAvatar from '@/components/custom/UserAvatar.vue'
import TableSkeleton from '@/components/custom/TableSkeleton.vue'
import EmptyState from '@/components/custom/EmptyState.vue'
import { Users } from 'lucide-vue-next'

interface Account {
  id: number; username: string; displayName: string; role: string
  disabled: boolean; lastSignInAt?: string; avatarUrl?: string
}
const { t } = useI18n()
const router = useRouter()
const { busy, run, errorText } = useApi()
const rows = ref<Account[]>([])
async function load(): Promise<void> {
  const res = await run(() => api.get<Account[]>('/api/prohibitorum/accounts'))
  if (res) rows.value = res
}
function go(id: number): void { router.push(`/admin/accounts/${id}`) }
onMounted(load)
</script>
<template>
  <div class="flex max-w-4xl flex-col gap-6">
    <div class="flex items-center justify-between gap-4">
      <h1 class="text-2xl font-semibold tracking-tight text-ink">{{ t('admin.accounts.title') }}</h1>
      <Button type="button" data-test="invite" @click="router.push('/admin/invitations')">{{ t('admin.accounts.invite') }}</Button>
    </div>
    <Alert v-if="errorText" variant="destructive" role="alert" aria-live="polite"><AlertDescription>{{ errorText }}</AlertDescription></Alert>
    <TableSkeleton v-if="busy && !rows.length" :rows="5" :cols="4" />
    <Table v-else-if="rows.length">
      <TableHeader>
        <TableRow>
          <TableHead>{{ t('admin.accounts.colUser') }} · {{ t('admin.accounts.colUsername') }}</TableHead>
          <TableHead>{{ t('admin.accounts.colRole') }}</TableHead>
          <TableHead>{{ t('admin.accounts.colState') }}</TableHead>
          <TableHead>{{ t('admin.accounts.colLastSeen') }}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <TableRow v-for="a in rows" :key="a.id" class="cursor-pointer" tabindex="0"
                  :data-test="`account-row-${a.id}`"
                  @click="go(a.id)" @keydown.enter="go(a.id)" @keydown.space.prevent="go(a.id)">
          <TableCell>
            <div class="flex min-w-0 items-center gap-2">
              <UserAvatar :display-name="a.displayName" :username="a.username" :src="a.avatarUrl" size="sm" />
              <div class="flex min-w-0 flex-col">
                <span class="truncate font-medium text-ink">{{ a.displayName }}</span>
                <span class="truncate text-muted">@{{ a.username }}</span>
              </div>
            </div>
          </TableCell>
          <TableCell><StatusBadge :variant="a.role === 'admin' ? 'caution' : 'neutral'">{{ a.role === 'admin' ? t('admin.account.roleAdmin') : t('admin.account.roleUser') }}</StatusBadge></TableCell>
          <TableCell><StatusBadge :variant="a.disabled ? 'danger' : 'success'">{{ a.disabled ? t('admin.accounts.disabled') : t('admin.accounts.active') }}</StatusBadge></TableCell>
          <TableCell class="text-muted">{{ relativeTime(a.lastSignInAt) }}</TableCell>
        </TableRow>
      </TableBody>
    </Table>
    <EmptyState v-else-if="!errorText" :icon="Users" :title="t('admin.accounts.empty')">
      <Button type="button" variant="outline" @click="router.push('/admin/invitations')">{{ t('admin.accounts.invite') }}</Button>
    </EmptyState>
  </div>
</template>
