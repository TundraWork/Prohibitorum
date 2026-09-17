<script setup lang="ts">
/**
 * AdminInvitationsView (/admin/invitations) — list/create/revoke enrollment
 * invitations. Create is an inline form (not a ConfirmDialog — creating isn't
 * destructive). The list returns the full URL, so it stays copyable per row.
 */
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import StatusMessage from '@/components/custom/StatusMessage.vue'
import { useResource } from '@/composables/useResource'
import { collectionQuery } from '@/queries/resources'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { useCursorPage } from '@/composables/useCursorPage'
import { useTransientFlag } from '@/composables/useTransientFlag'
import { withSudo } from '@/lib/sudo'
import { relativeTime, formatDateTime } from '@/lib/time'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
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
import { Mail, X } from 'lucide-vue-next'

interface InvitationGroup { id: number; slug: string; displayName: string }
interface Invitation { token: string; url: string; role: string; attributes?: Record<string, unknown>; createdAt: string; expiresAt: string; expectedUpstreamIdpSlug?: string; username?: string; groupIds: number[]; groups: InvitationGroup[] }
interface Idp { slug: string; displayName: string; disabled: boolean; mode: string }
interface GroupPage { items: InvitationGroup[]; nextCursor: string }
const { t } = useI18n()
const { busy, run, error, clear } = useApi('invitations')
const IDP_NONE = '__none__'
const page = useCursorPage<Invitation>('invitations')
const rows = page.items
const providersQuery = useResource(collectionQuery<Idp>('identity-providers', { limit: 100 }))
const idps = computed(() => (providersQuery.data.value?.items ?? []).filter(i => !i.disabled && (i.mode === 'auto_provision' || i.mode === 'invite_only')))
const createOpen = ref(false)
const newRole = ref<'admin' | 'app_manager' | 'user'>('user')
const newIdp = ref(IDP_NONE)
const newUsername = ref('')
const groupSearch = ref('')
const selectedGroups = ref<InvitationGroup[]>([])
const groupsQuery = useResource(computed(() => ({
  queryKey: ['admin', 'invitation-groups', groupSearch.value.trim()],
  staleTime: 0,
  enabled: createOpen.value,
  queryFn: ({ signal }: { signal: AbortSignal }) => {
    const params = new URLSearchParams({ kind: 'manual', limit: '100' })
    if (groupSearch.value.trim()) params.set('q', groupSearch.value.trim())
    return api.get<GroupPage>(`/api/prohibitorum/groups?${params}`, { signal })
  },
})))
const availableGroups = computed(() => groupsQuery.data.value?.items ?? [])
const { flag: created, trigger: triggerCreated } = useTransientFlag()
const revokeToken = ref<string | null>(null)
const displayError = computed(() => page.error.value ?? error.value ?? providersQuery.error.value ?? groupsQuery.error.value)
function clearError(): void { page.clear(); clear(); providersQuery.clear(); groupsQuery.clear() }
function idpDisplayName(slug: string | undefined): string {
  if (!slug) return '—'
  const found = idps.value.find((i) => i.slug === slug)
  return found ? found.displayName : slug
}
async function create(): Promise<void> {
  const body: Record<string, unknown> = { role: newRole.value }
  const idpSlug = newIdp.value && newIdp.value !== IDP_NONE ? newIdp.value : ''
  if (idpSlug) body.expectedUpstreamIdpSlug = idpSlug
  if (newUsername.value.trim()) body.username = newUsername.value.trim()
  if (selectedGroups.value.length) body.groupIds = selectedGroups.value.map(group => group.id)
  const ok = await run(() => withSudo(async () => {
    await api.post('/api/prohibitorum/invitations', body)
    return true as const
  }))
  if (ok) { resetCreate(); triggerCreated() }
}
function resetCreate(): void {
  createOpen.value = false
  newIdp.value = IDP_NONE
  newUsername.value = ''
  groupSearch.value = ''
  selectedGroups.value = []
}
function isGroupSelected(id: number): boolean {
  return selectedGroups.value.some(group => group.id === id)
}
function setGroupSelected(group: InvitationGroup, checked: boolean | 'indeterminate'): void {
  if (checked === true && !isGroupSelected(group.id)) selectedGroups.value = [...selectedGroups.value, group]
  if (checked === false) selectedGroups.value = selectedGroups.value.filter(selected => selected.id !== group.id)
}
function invalidGroupIDs(invitation: Invitation): number[] {
  const resolved = new Set((invitation.groups ?? []).map(group => group.id))
  return (invitation.groupIds ?? []).filter(id => !resolved.has(id))
}
async function revoke(): Promise<void> {
  const token = revokeToken.value
  if (token == null) return
  await run(() => withSudo(async () => {
    await api.post('/api/prohibitorum/invitations/revoke', { token })
    return true as const
  }))
  revokeToken.value = null
}

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
            :options="[
              {value:'user',label:t('admin.invitations.roleUser')},
              {value:'app_manager',label:t('admin.invitations.roleAppManager')},
              {value:'admin',label:t('admin.invitations.roleAdmin')},
            ]" />
          <p class="text-xs text-muted">{{ t('admin.invitations.roleDesc') }}</p>
        </div>
        <div class="flex flex-col gap-1.5">
          <Label for="newUsername">{{ t('admin.invitations.username') }}</Label>
          <Input id="newUsername" v-model="newUsername" name="username" autocomplete="off" autocapitalize="none" spellcheck="false" :placeholder="t('admin.invitations.usernamePlaceholder')" />
          <p class="text-xs text-muted">{{ t('admin.invitations.usernameDesc') }}</p>
        </div>
        <div class="flex flex-col gap-2">
          <Label for="groupSearch">{{ t('admin.invitations.groups') }}</Label>
          <Input id="groupSearch" v-model="groupSearch" type="search" :placeholder="t('admin.invitations.groupSearch')" />
          <div v-if="selectedGroups.length" class="flex flex-wrap gap-2" data-test="selected-groups">
            <div v-for="group in selectedGroups" :key="group.id" class="inline-flex min-h-8 items-center gap-2 rounded-md bg-subtle px-2.5 text-sm text-ink">
              <span>{{ group.displayName }}</span><span class="font-mono text-xs text-muted">{{ group.slug }}</span>
              <button type="button" class="rounded-sm text-muted hover:text-ink focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring" :aria-label="t('admin.invitations.removeGroup', { group: group.displayName })" @click="setGroupSelected(group, false)"><X class="size-4" /></button>
            </div>
          </div>
          <div class="max-h-48 overflow-y-auto rounded-md border border-border bg-bg p-1" data-test="group-options">
            <p v-if="groupsQuery.busy.value" class="px-2 py-3 text-sm text-muted">{{ t('common.loading') }}</p>
            <label v-for="group in availableGroups" :key="group.id" class="flex min-h-10 cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 hover:bg-subtle">
              <Checkbox :model-value="isGroupSelected(group.id)" @update:model-value="(value: boolean | 'indeterminate') => setGroupSelected(group, value)" />
              <span class="min-w-0"><span class="block truncate text-sm text-ink">{{ group.displayName }}</span><span class="block font-mono text-xs text-muted">{{ group.slug }} · #{{ group.id }}</span></span>
            </label>
            <p v-if="!groupsQuery.busy.value && !availableGroups.length" class="px-2 py-3 text-sm text-muted">{{ t('admin.invitations.noGroups') }}</p>
          </div>
          <p class="text-xs text-muted">{{ t('admin.invitations.groupsDesc') }}</p>
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
          <Button type="button" variant="outline" :disabled="busy" data-test="create-cancel" @click="resetCreate">{{ t('common.cancel') }}</Button>
        </div>
      </CardContent>
    </Card>

    <TableSkeleton v-if="page.busy.value && !rows.length" :rows="5" :cols="7" />
    <Table v-else-if="rows.length">
      <TableHeader>
        <TableRow>
          <TableHead>{{ t('admin.invitations.colRole') }}</TableHead>
          <TableHead>{{ t('admin.invitations.colMethod') }}</TableHead>
          <TableHead>{{ t('admin.invitations.colAccount') }}</TableHead>
          <TableHead>{{ t('admin.invitations.colCreated') }}</TableHead>
          <TableHead>{{ t('admin.invitations.colExpires') }}</TableHead>
          <TableHead>{{ t('admin.invitations.colLink') }}</TableHead>
          <TableHead></TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <TableRow v-for="inv in rows" :key="inv.token">
          <TableCell>
            <StatusBadge :variant="inv.role === 'admin' ? 'caution' : inv.role === 'app_manager' ? 'info' : 'neutral'">
              {{ inv.role === 'admin' ? t('admin.invitations.roleAdmin') : inv.role === 'app_manager' ? t('admin.invitations.roleAppManager') : t('admin.invitations.roleUser') }}
            </StatusBadge>
          </TableCell>
          <TableCell class="max-w-[12rem] truncate text-muted">{{ idpDisplayName(inv.expectedUpstreamIdpSlug) }}</TableCell>
          <TableCell class="min-w-48">
            <p class="text-sm text-ink">{{ inv.username || t('admin.invitations.usernameChosenLater') }}</p>
            <div v-if="inv.groups?.length || invalidGroupIDs(inv).length" class="mt-1 flex flex-wrap gap-1">
              <span v-for="group in inv.groups" :key="group.id" class="rounded bg-subtle px-1.5 py-0.5 text-xs text-muted">{{ group.displayName }}</span>
              <span v-for="id in invalidGroupIDs(inv)" :key="id" class="rounded bg-destructive/10 px-1.5 py-0.5 text-xs text-destructive">{{ t('admin.invitations.groupUnavailable', { id }) }}</span>
            </div>
            <p v-else class="mt-1 text-xs text-muted">{{ t('admin.invitations.noAssignedGroups') }}</p>
          </TableCell>
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
