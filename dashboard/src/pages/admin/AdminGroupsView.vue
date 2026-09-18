<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { api } from '@/lib/api'
import type { AppGroup, ProviderDescriptor, Rule } from '@/lib/appAccess'
import { useApi } from '@/composables/useApi'
import { useResource } from '@/composables/useResource'
import { withSudo } from '@/lib/sudo'
import RuleEditor, { type RuleEditorDraft } from '@/components/custom/RuleEditor.vue'
import ErrorPanel from '@/components/custom/ErrorPanel.vue'
import EmptyState from '@/components/custom/EmptyState.vue'
import StatusBadge from '@/components/custom/StatusBadge.vue'
import TableSkeleton from '@/components/custom/TableSkeleton.vue'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'

const { t } = useI18n()
const router = useRouter()
const mutation = useApi('access')
const search = ref('')
const kind = ref<'all' | 'manual' | 'rule'>('all')
const createKind = ref<'manual' | 'rule' | null>(null)
const manualDraft = ref({ slug: '', displayName: '', description: '', exposedToDownstream: true })
const defaultRule: Rule = { version: 1, condition: { op: 'all', children: [{}] } }

const groupsQuery = useResource(computed(() => ({
  queryKey: ['admin', 'groups'],
  staleTime: 0,
  queryFn: ({ signal }: { signal: AbortSignal }) => api.get<AppGroup[]>('/api/prohibitorum/groups', { signal }),
})))
const providersQuery = useResource(computed(() => ({ queryKey: ['admin', 'groups', 'providers'], staleTime: 60_000, queryFn: ({ signal }: { signal: AbortSignal }) => api.get<ProviderDescriptor[]>('/api/prohibitorum/groups/providers', { signal }) })))
const groups = computed(() => {
  const query = search.value.trim().toLocaleLowerCase()
  return (groupsQuery.data.value ?? []).filter(group => {
    if (kind.value !== 'all' && group.kind !== kind.value) return false
    if (!query) return true
    return [group.displayName, group.slug, group.kind, String(group.id)]
      .some(value => value.toLocaleLowerCase().includes(query))
  })
})

function resetCreate(): void {
  createKind.value = null
  manualDraft.value = { slug: '', displayName: '', description: '', exposedToDownstream: true }
}
async function createManual(): Promise<void> {
  const result = await mutation.run(() => withSudo(() => api.post<AppGroup>('/api/prohibitorum/groups', { kind: 'manual', ...manualDraft.value })))
  if (!result) return
  resetCreate()
  await router.push(`/admin/groups/${result.id}`)
}
async function createRule(draft: RuleEditorDraft): Promise<void> {
  const result = await mutation.run(() => withSudo(() => api.post<AppGroup>('/api/prohibitorum/groups', { kind: 'rule', ...draft })))
  if (!result) return
  resetCreate()
  await router.push(`/admin/groups/${result.id}`)
}

function openGroup(id: number): void {
  router.push(`/admin/groups/${id}`)
}
</script>

<template>
  <div class="flex max-w-5xl flex-col gap-6">
    <div class="flex flex-wrap items-start justify-between gap-3">
      <div><h1 class="text-2xl font-semibold tracking-tight text-ink">{{ t('admin.groups.title') }}</h1><p class="mt-1 text-sm text-muted">{{ t('admin.groups.subtitle') }}</p></div>
      <div class="flex gap-2"><Button variant="outline" @click="createKind = 'manual'">{{ t('admin.groups.createManual') }}</Button><Button @click="createKind = 'rule'">{{ t('admin.groups.createRule') }}</Button></div>
    </div>
    <ErrorPanel :error="groupsQuery.error.value ?? providersQuery.error.value ?? mutation.error.value" is-admin @dismiss="groupsQuery.clear(); providersQuery.clear(); mutation.clear()" />

    <Card v-if="createKind === 'manual'">
      <CardHeader><CardTitle>{{ t('admin.groups.createManual') }}</CardTitle><CardDescription>{{ t('admin.groups.manualDescription') }}</CardDescription></CardHeader>
      <CardContent class="grid gap-4 sm:grid-cols-2">
        <div><Label for="manual-name">{{ t('admin.groups.name') }}</Label><Input id="manual-name" v-model="manualDraft.displayName" /></div>
        <div><Label for="manual-slug">{{ t('admin.groups.slug') }}</Label><Input id="manual-slug" v-model="manualDraft.slug" /></div>
        <div class="sm:col-span-2"><Label for="manual-description">{{ t('admin.groups.description') }}</Label><Textarea id="manual-description" v-model="manualDraft.description" /></div>
        <label class="flex items-center justify-between gap-3 sm:col-span-2"><span class="text-sm font-medium">{{ t('admin.groups.exposed') }}</span><Switch v-model="manualDraft.exposedToDownstream" /></label>
        <div class="flex justify-end gap-2 sm:col-span-2"><Button variant="outline" @click="resetCreate">{{ t('common.cancel') }}</Button><Button :disabled="!manualDraft.displayName || !manualDraft.slug || mutation.busy.value" @click="createManual">{{ t('common.save') }}</Button></div>
      </CardContent>
    </Card>
    <RuleEditor v-else-if="createKind === 'rule'" :initial-draft="{ slug: '', displayName: '', description: '', exposedToDownstream: true, rule: defaultRule }" :providers="providersQuery.data.value ?? []" preview-endpoint="/api/prohibitorum/groups/rule-preview" :busy="mutation.busy.value" :server-error="mutation.error.value ?? undefined" mode="create" @save="createRule" @cancel="resetCreate" />

    <div class="grid gap-3 sm:grid-cols-[1fr_auto]"><Input v-model="search" type="search" :placeholder="t('admin.groups.search')" /><select v-model="kind" class="h-9 rounded-md border border-input bg-surface px-3 text-sm"><option value="all">{{ t('admin.groups.allKinds') }}</option><option value="manual">{{ t('admin.groups.manual') }}</option><option value="rule">{{ t('admin.groups.rule') }}</option></select></div>
    <TableSkeleton v-if="groupsQuery.busy.value && !groups.length" :rows="5" :cols="4" />
    <EmptyState v-else-if="groups.length === 0" :title="t('admin.groups.empty')" :description="t('admin.groups.emptyDescription')" />
    <Table v-else class="min-w-[46rem]" data-test="groups-table">
      <TableHeader>
        <TableRow>
          <TableHead>{{ t('admin.groups.colName') }}</TableHead>
          <TableHead>{{ t('admin.groups.colType') }}</TableHead>
          <TableHead>{{ t('admin.groups.colDescription') }}</TableHead>
          <TableHead class="text-right">{{ t('admin.groups.colApplications') }}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <TableRow
          v-for="group in groups"
          :key="group.id"
          tabindex="0"
          class="cursor-pointer"
          :aria-label="t('admin.groups.openGroup', { name: group.displayName })"
          :data-test="`group-row-${group.id}`"
          @click="openGroup(group.id)"
          @keydown.enter="openGroup(group.id)"
          @keydown.space.prevent="openGroup(group.id)"
        >
          <TableCell>
            <span class="block max-w-72 truncate font-medium text-ink">{{ group.displayName }}</span>
            <span class="font-mono text-xs text-muted">{{ group.slug }} · #{{ group.id }}</span>
          </TableCell>
          <TableCell><StatusBadge variant="neutral">{{ t(`admin.groups.${group.kind}`) }}</StatusBadge></TableCell>
          <TableCell class="max-w-96 text-sm text-muted">{{ group.description || t('admin.groups.noDescription') }}</TableCell>
          <TableCell class="text-right tabular-nums text-muted">{{ group.applicationCount ?? 0 }}</TableCell>
        </TableRow>
      </TableBody>
    </Table>
  </div>
</template>
