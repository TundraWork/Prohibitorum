<script setup lang="ts">
/**
 * AdminInvitationsView (/admin/invitations) — list/create/revoke enrollment
 * invitations. Create is an inline form (not a ConfirmDialog — creating isn't
 * destructive). The list returns the full URL, so it stays copyable per row.
 */
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import StatusMessage from '@/components/custom/StatusMessage.vue'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { useCursorPage } from '@/composables/useCursorPage'
import { type Page, buildPagePath, unwrap } from '@/lib/pagination'
import { useTransientFlag } from '@/composables/useTransientFlag'
import { withSudo } from '@/lib/sudo'
import { relativeTime, formatDateTime } from '@/lib/time'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/table'
import { Select, SelectTrigger, SelectValue, SelectContent, SelectItem } from '@/components/ui/select'
import SegmentedControl from '@/components/custom/SegmentedControl.vue'
import TableSkeleton from '@/components/custom/TableSkeleton.vue'
import StatusBadge from '@/components/custom/StatusBadge.vue'
import ConfirmDialog from '@/components/custom/ConfirmDialog.vue'
import CodeField from '@/components/custom/CodeField.vue'
import EmptyState from '@/components/custom/EmptyState.vue'
import ErrorPanel from '@/components/custom/ErrorPanel.vue'
import PaginationControls from '@/components/custom/PaginationControls.vue'
import { Mail } from 'lucide-vue-next'

interface Invitation { token: string; url: string; role: string; attributes?: Record<string, unknown>; createdAt: string; expiresAt: string; expectedUpstreamIdpSlug?: string }
interface Idp { slug: string; displayName: string; disabled: boolean }
const { t } = useI18n()
const { busy, run, error, clear } = useApi()
const IDP_NONE = '__none__'
const page = useCursorPage<Invitation>((cursor) =>
  api.get<Page<Invitation>>(buildPagePath('/api/prohibitorum/invitations', { cursor })),
)
const rows = page.items
const idps = ref<Idp[]>([])
const createOpen = ref(false)
const newRole = ref<'admin' | 'user'>('user')
const newIdp = ref(IDP_NONE)
const { flag: created, trigger: triggerCreated } = useTransientFlag()
const revokeToken = ref<string | null>(null)
const displayError = computed(() => page.error.value ?? error.value)
function clearError(): void { page.clear(); clear() }
function idpDisplayName(slug: string | undefined): string {
  if (!slug) return '—'
  const found = idps.value.find((i) => i.slug === slug)
  return found ? found.displayName : slug
}
async function loadIdps(): Promise<void> {
  try {
    const res = await api.get<Page<Idp>>(buildPagePath('/api/prohibitorum/identity-providers', { limit: 100 }))
    idps.value = unwrap(res).items.filter((i) => !i.disabled)
  } catch {
    idps.value = []
  }
}
async function create(): Promise<void> {
  const body: Record<string, unknown> = { role: newRole.value }
  const idpSlug = newIdp.value && newIdp.value !== IDP_NONE ? newIdp.value : ''
  if (idpSlug) body.expectedUpstreamIdpSlug = idpSlug
  const ok = await run(() => withSudo(async () => {
    await api.post('/api/prohibitorum/invitations', body)
    return true as const
  }))
  if (ok) { createOpen.value = false; triggerCreated(); newIdp.value = IDP_NONE; await page.reload() }
}
async function revoke(): Promise<void> {
  const token = revokeToken.value
  if (token == null) return
  const ok = await run(() => withSudo(async () => {
    await api.post('/api/prohibitorum/invitations/revoke', { token })
    return true as const
  }))
  revokeToken.value = null
  if (ok) await page.reload()
}

onMounted(loadIdps)
</script>
<template>
  <div class="flex max-w-4xl flex-col gap-6">
    <div class="flex items-center justify-between gap-4">
      <h1 class="text-2xl font-semibold tracking-tight text-ink">{{ t('admin.invitations.title') }}</h1>
      <Button type="button" data-test="create" @click="createOpen = true">{{ t('admin.invitations.create') }}</Button>
    </div>
    <ErrorPanel :error="displayError" @dismiss="clearError" :is-admin="true" />
    <StatusMessage :show="created">{{ t('admin.invitations.created') }}</StatusMessage>

    <Card v-if="createOpen">
      <CardHeader><CardTitle>{{ t('admin.invitations.createTitle') }}</CardTitle></CardHeader>
      <CardContent class="flex flex-col gap-3 py-4">
        <div class="flex flex-col gap-1.5">
          <Label>{{ t('admin.invitations.role') }}</Label>
          <SegmentedControl v-model="newRole" :aria-label="t('admin.invitations.role')"
            :options="[{value:'user',label:t('admin.invitations.roleUser')},{value:'admin',label:t('admin.invitations.roleAdmin')}]" />
          <p class="text-xs text-muted">{{ t('admin.invitations.roleDesc') }}</p>
        </div>
        <div class="flex flex-col gap-1.5">
          <Label for="newIdp">{{ t('admin.invitations.requireMethod') }}</Label>
          <Select v-model="newIdp">
            <SelectTrigger id="newIdp" name="idp" data-test="idp" class="w-full" :aria-label="t('admin.invitations.requireMethod')"><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem :value="IDP_NONE">{{ t('admin.invitations.anyMethod') }}</SelectItem>
              <SelectItem v-for="idp in idps" :key="idp.slug" :value="idp.slug">{{ idp.displayName }}</SelectItem>
            </SelectContent>
          </Select>
          <p class="text-xs text-muted">{{ t('admin.invitations.requireMethodDesc') }}</p>
        </div>
        <div class="flex gap-2">
          <Button type="button" :disabled="busy" data-test="create-confirm" @click="create">{{ t('admin.invitations.create') }}</Button>
          <Button type="button" variant="outline" :disabled="busy" data-test="create-cancel" @click="createOpen = false; newIdp = IDP_NONE">{{ t('common.cancel') }}</Button>
        </div>
      </CardContent>
    </Card>

    <TableSkeleton v-if="page.busy.value && !rows.length" :rows="5" :cols="6" />
    <Table v-else-if="rows.length">
      <TableHeader>
        <TableRow>
          <TableHead>{{ t('admin.invitations.colRole') }}</TableHead>
          <TableHead>{{ t('admin.invitations.colMethod') }}</TableHead>
          <TableHead>{{ t('admin.invitations.colCreated') }}</TableHead>
          <TableHead>{{ t('admin.invitations.colExpires') }}</TableHead>
          <TableHead>{{ t('admin.invitations.colLink') }}</TableHead>
          <TableHead></TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <TableRow v-for="inv in rows" :key="inv.token">
          <TableCell><StatusBadge :variant="inv.role === 'admin' ? 'caution' : 'neutral'">{{ inv.role === 'admin' ? t('admin.invitations.roleAdmin') : t('admin.invitations.roleUser') }}</StatusBadge></TableCell>
          <TableCell class="max-w-[12rem] truncate text-muted">{{ idpDisplayName(inv.expectedUpstreamIdpSlug) }}</TableCell>
          <TableCell class="text-muted">{{ relativeTime(inv.createdAt) }}</TableCell>
          <TableCell class="text-muted">{{ formatDateTime(inv.expiresAt) }}</TableCell>
          <TableCell><CodeField :value="inv.url" /></TableCell>
          <TableCell><Button type="button" variant="outline" size="sm" :disabled="busy" :data-test="`revoke-${inv.token}`" @click="revokeToken = inv.token">{{ t('admin.invitations.revoke') }}</Button></TableCell>
        </TableRow>
      </TableBody>
    </Table>
    <EmptyState v-else-if="!displayError" :icon="Mail" :title="t('admin.invitations.empty')" />

    <ConfirmDialog :open="revokeToken !== null" :title="t('admin.invitations.revokeConfirmTitle')" :confirm-label="t('admin.invitations.revoke')" :busy="busy"
      @update:open="(v) => { if (!v) revokeToken = null }" @cancel="revokeToken = null" @confirm="revoke">
      {{ t('admin.invitations.revokeConfirmBody') }}
    </ConfirmDialog>
    <PaginationControls
      v-if="rows.length || page.pageIndex.value > 0"
      :page-index="page.pageIndex.value"
      :has-more="page.hasMore.value"
      :busy="page.busy.value"
      :has-items="rows.length > 0"
      @next="page.next"
      @previous="page.previous"
    />
  </div>
</template>
