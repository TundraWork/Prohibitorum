<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useResource } from '@/composables/useResource'
import { useApi } from '@/composables/useApi'
import { accessQuery } from '@/queries/access'
import { api } from '@/lib/api'
import type { AppAccessWorkspace, AppGroup, AppKind } from '@/lib/appAccess'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import ErrorPanel from '@/components/custom/ErrorPanel.vue'
import ProtocolBadge from '@/components/custom/ProtocolBadge.vue'
import StatusBadge from '@/components/custom/StatusBadge.vue'

const props = defineProps<{ kind: AppKind; appId: string; displayName: string; isAdmin: boolean }>()
const { t } = useI18n()
const mutation = useApi('access')
const search = ref('')
const selected = ref<number[]>([])
const initializedKey = ref('')
const basePath = computed(() => `/api/prohibitorum/managed-applications/${encodeURIComponent(props.kind)}/${encodeURIComponent(props.appId)}`)
const workspaceQuery = useResource(computed(() => accessQuery<AppAccessWorkspace>(props.kind, props.appId, 'access')))
const catalogQuery = useResource(computed(() => ({ queryKey: ['session', 'access', 'global-groups'], staleTime: 0, queryFn: ({ signal }: { signal: AbortSignal }) => api.get<AppGroup[]>('/api/prohibitorum/groups', { signal }) })))
const workspace = computed(() => workspaceQuery.data.value ?? null)
const catalog = computed(() => {
  const byId = new Map<number, AppGroup>()
  for (const group of catalogQuery.data.value ?? []) byId.set(group.id, group)
  for (const group of workspace.value?.groups ?? []) byId.set(group.id, group)
  return [...byId.values()].sort((a, b) => a.displayName.localeCompare(b.displayName) || a.id - b.id)
})
const notFound = computed(() => workspaceQuery.error.value?.code === 'client_not_found')
const filtered = computed(() => {
  const query = search.value.trim().toLocaleLowerCase()
  if (!query) return catalog.value
  return catalog.value.filter(group => [group.displayName, group.slug, group.kind, String(group.id)].some(value => value.toLocaleLowerCase().includes(query)))
})
const selectedSet = computed(() => new Set(selected.value))
const dirty = computed(() => {
  const saved = (workspace.value?.groups ?? []).map(group => group.id).sort((a, b) => a - b)
  const draft = [...selected.value].sort((a, b) => a - b)
  return saved.length !== draft.length || saved.some((id, index) => id !== draft[index])
})
watch(workspace, value => { const key = `${props.kind}:${props.appId}`; if (!value || initializedKey.value === key) return; selected.value = (value.groups ?? []).map(group => group.id); initializedKey.value = key }, { immediate: true })
watch(() => [props.kind, props.appId], () => { initializedKey.value = ''; selected.value = []; search.value = '' })
function toggleGroup(id: number, checked: boolean): void { selected.value = checked ? [...selected.value, id] : selected.value.filter(value => value !== id) }
async function saveGroups(): Promise<void> {
  const groups = await mutation.run(() => api.put<AppGroup[]>(`${basePath.value}/groups`, { groupIds: selected.value }))
  if (!groups) return
  await workspaceQuery.refetch()
}
async function setRestricted(restricted: boolean): Promise<void> {
  const app = await mutation.run(() => api.post<AppAccessWorkspace['app']>(`${basePath.value}/access/set-restricted`, { restricted }))
  if (!app) return
  await workspaceQuery.refetch()
}
</script>

<template>
  <section class="flex flex-col gap-5">
    <div v-if="workspace" class="flex flex-wrap items-start justify-between gap-3">
      <div><div class="mb-2 flex items-center gap-2"><ProtocolBadge :kind="kind" /><StatusBadge :variant="workspace.accessRestricted ? 'caution' : 'success'">{{ workspace.accessRestricted ? t('manage.applications.restricted') : t('manage.applications.open') }}</StatusBadge></div><h1 class="text-2xl font-semibold tracking-tight text-ink">{{ workspace.app.displayName || displayName }}</h1><p class="mt-1 text-sm text-muted">{{ t('manage.applications.detailSubtitle') }}</p></div>
    </div>
    <ErrorPanel v-if="!notFound" :error="workspaceQuery.error.value ?? catalogQuery.error.value ?? mutation.error.value" :is-admin="isAdmin" @dismiss="workspaceQuery.clear(); catalogQuery.clear(); mutation.clear()" />
    <Card v-if="notFound"><CardContent class="py-6 text-sm text-muted">{{ t('manage.applications.notFound') }}</CardContent></Card>
    <template v-else-if="workspace">
      <Card><CardHeader><CardTitle>{{ t('manage.applications.accessTitle') }}</CardTitle><CardDescription>{{ workspace.accessRestricted ? t('manage.applications.restrictedDescription') : t('manage.applications.openDescription') }}</CardDescription></CardHeader><CardContent class="flex items-center justify-between gap-4 py-4"><span class="text-sm font-medium">{{ t('manage.applications.restricted') }}</span><Switch :model-value="workspace.accessRestricted" :disabled="mutation.busy.value" @update:model-value="setRestricted" /></CardContent></Card>
      <Card><CardHeader><CardTitle>{{ t('manage.applications.globalGroups') }}</CardTitle><CardDescription>{{ t('manage.applications.globalGroupsDescription') }}</CardDescription></CardHeader><CardContent class="flex flex-col gap-4 py-4">
        <Input v-model="search" type="search" :placeholder="t('manage.applications.searchGroups')" />
        <div v-if="filtered.length" class="grid gap-2" data-test="group-selector"><label v-for="group in filtered" :key="group.id" class="flex cursor-pointer items-start gap-3 rounded-md border border-border p-3 hover:bg-subtle"><input class="mt-1 size-4" type="checkbox" :checked="selectedSet.has(group.id)" @change="toggleGroup(group.id, ($event.target as HTMLInputElement).checked)" /><span class="min-w-0 flex-1"><span class="flex flex-wrap items-center gap-2"><strong class="text-sm text-ink">{{ group.displayName }}</strong><StatusBadge variant="neutral">{{ group.kind }}</StatusBadge></span><span class="block text-xs text-muted">{{ group.slug }} · #{{ group.id }}</span><span v-if="group.description" class="mt-1 block text-sm text-muted">{{ group.description }}</span></span><RouterLink v-if="isAdmin" :to="`/admin/groups/${group.id}`" class="text-sm text-ember hover:underline" @click.stop>{{ t('common.edit') }}</RouterLink></label></div>
        <p v-else class="text-sm text-muted">{{ t('manage.applications.noGlobalGroups') }}</p><div class="flex justify-end"><Button type="button" data-test="save-groups" :disabled="!dirty || mutation.busy.value" @click="saveGroups">{{ t('manage.applications.saveGroups') }}</Button></div>
      </CardContent></Card>
    </template>
  </section>
</template>
