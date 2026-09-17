<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { api } from '@/lib/api'
import type { AccountSummary, AppGroup, ManualDecision, ManualEffect, ProviderDescriptor } from '@/lib/appAccess'
import type { Page } from '@/lib/pagination'
import { useApi } from '@/composables/useApi'
import { useResource } from '@/composables/useResource'
import { withSudo } from '@/lib/sudo'
import BackLink from '@/components/custom/BackLink.vue'
import ErrorPanel from '@/components/custom/ErrorPanel.vue'
import ManualDecisionEditor from '@/components/custom/ManualDecisionEditor.vue'
import RuleEditor, { type RuleEditorDraft } from '@/components/custom/RuleEditor.vue'
import StatusBadge from '@/components/custom/StatusBadge.vue'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'

interface GroupApplication { kind: string; appId: string; displayName: string }
const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const id = computed(() => Number(route.params.id))
const mutation = useApi('access')
const editing = ref(false)
const manualDraft = ref({ slug: '', displayName: '', description: '', exposedToDownstream: true })
const detailQuery = useResource(computed(() => ({ queryKey: ['admin', 'groups', id.value], staleTime: 0, queryFn: ({ signal }: { signal: AbortSignal }) => api.get<AppGroup>(`/api/prohibitorum/groups/${id.value}`, { signal }) })))
const appsQuery = useResource(computed(() => ({ queryKey: ['admin', 'groups', id.value, 'applications'], staleTime: 0, queryFn: ({ signal }: { signal: AbortSignal }) => api.get<Page<GroupApplication>>(`/api/prohibitorum/groups/${id.value}/applications`, { signal }) })))
const providersQuery = useResource(computed(() => ({ queryKey: ['admin', 'groups', 'providers'], staleTime: 60_000, queryFn: ({ signal }: { signal: AbortSignal }) => api.get<ProviderDescriptor[]>('/api/prohibitorum/groups/providers', { signal }) })))
const decisionsQuery = useResource(computed(() => ({ queryKey: ['admin', 'groups', id.value, 'decisions'], staleTime: 0, enabled: detailQuery.data.value?.kind === 'manual', queryFn: ({ signal }: { signal: AbortSignal }) => api.get<Page<ManualDecision>>(`/api/prohibitorum/groups/${id.value}/decisions?limit=100`, { signal }) })))
const accountsQuery = useResource(computed(() => ({ queryKey: ['admin', 'groups', 'accounts'], staleTime: 30_000, enabled: detailQuery.data.value?.kind === 'manual', queryFn: ({ signal }: { signal: AbortSignal }) => api.get<Page<AccountSummary>>('/api/prohibitorum/accounts?limit=100', { signal }) })))
const group = computed(() => detailQuery.data.value ?? null)
const applications = computed(() => appsQuery.data.value?.items ?? [])
watch(group, value => { if (!value) return; manualDraft.value = { slug: value.slug, displayName: value.displayName, description: value.description ?? '', exposedToDownstream: value.exposedToDownstream } }, { immediate: true })

async function saveManual(): Promise<void> {
  const result = await mutation.run(() => withSudo(() => api.put<AppGroup>(`/api/prohibitorum/groups/${id.value}`, manualDraft.value)))
  if (!result) return
  editing.value = false
  await detailQuery.refetch()
}
async function saveRule(draft: RuleEditorDraft): Promise<void> {
  const result = await mutation.run(() => withSudo(() => api.put<AppGroup>(`/api/prohibitorum/groups/${id.value}`, draft)))
  if (!result) return
  editing.value = false
  await detailQuery.refetch()
}
async function setDecision(payload: { accountId: number; effect: ManualEffect }): Promise<void> {
  const result = await mutation.run(() => withSudo(() => api.post<ManualDecision>(`/api/prohibitorum/groups/${id.value}/decisions`, payload)))
  if (result) await decisionsQuery.refetch()
}
async function clearDecision(payload: { accountId: number }): Promise<void> {
  const result = await mutation.run(() => withSudo(async () => { await api.post(`/api/prohibitorum/groups/${id.value}/decisions/clear`, payload); return true }))
  if (result) await decisionsQuery.refetch()
}
async function remove(): Promise<void> {
  if (!confirm(t('admin.groups.deleteConfirm', { name: group.value?.displayName ?? '' }))) return
  const result = await mutation.run(() => withSudo(async () => { await api.post(`/api/prohibitorum/groups/${id.value}/delete`); return true }))
  if (result) await router.push('/admin/groups')
}
</script>

<template>
  <div class="flex max-w-5xl flex-col gap-6">
    <BackLink to="/admin/groups" :label="t('admin.groups.back')" />
    <ErrorPanel :error="detailQuery.error.value ?? appsQuery.error.value ?? providersQuery.error.value ?? decisionsQuery.error.value ?? accountsQuery.error.value ?? mutation.error.value" is-admin @dismiss="detailQuery.clear(); appsQuery.clear(); providersQuery.clear(); decisionsQuery.clear(); accountsQuery.clear(); mutation.clear()" />
    <template v-if="group">
      <div class="flex flex-wrap items-start justify-between gap-3"><div><div class="mb-2 flex gap-2"><StatusBadge variant="neutral">{{ t(`admin.groups.${group.kind}`) }}</StatusBadge><StatusBadge :variant="group.exposedToDownstream ? 'success' : 'neutral'">{{ group.exposedToDownstream ? t('admin.groups.exposed') : t('admin.groups.hidden') }}</StatusBadge></div><h1 class="text-2xl font-semibold tracking-tight text-ink">{{ group.displayName }}</h1><p class="font-mono text-xs text-muted">{{ group.slug }} · #{{ group.id }}</p></div><div class="flex gap-2"><Button variant="outline" @click="editing = !editing">{{ editing ? t('common.cancel') : t('common.edit') }}</Button><Button variant="destructive" :disabled="applications.length > 0 || mutation.busy.value" @click="remove">{{ t('admin.groups.delete') }}</Button></div></div>
      <Card><CardHeader><CardTitle>{{ t('admin.groups.sharedImpact') }}</CardTitle><CardDescription>{{ applications.length ? t('admin.groups.sharedImpactCount', { count: applications.length }) : t('admin.groups.unused') }}</CardDescription></CardHeader><CardContent><ul v-if="applications.length" class="divide-y divide-border"><li v-for="app in applications" :key="`${app.kind}:${app.appId}`" class="flex items-center justify-between gap-3 py-3"><span><strong class="block text-sm text-ink">{{ app.displayName }}</strong><span class="font-mono text-xs text-muted">{{ app.appId }}</span></span><StatusBadge variant="neutral">{{ app.kind }}</StatusBadge></li></ul><p v-else class="text-sm text-muted">{{ t('admin.groups.deleteReady') }}</p></CardContent></Card>
      <RuleEditor v-if="editing && group.kind === 'rule' && group.rule" :initial-draft="{ slug: group.slug, displayName: group.displayName, description: group.description ?? '', exposedToDownstream: group.exposedToDownstream, rule: group.rule }" :providers="providersQuery.data.value ?? []" preview-endpoint="/api/prohibitorum/groups/rule-preview" :busy="mutation.busy.value" :server-error="mutation.error.value ?? undefined" mode="edit" @save="saveRule" @cancel="editing = false" />
      <Card v-else-if="editing"><CardHeader><CardTitle>{{ t('admin.groups.edit') }}</CardTitle></CardHeader><CardContent class="grid gap-4 sm:grid-cols-2"><div><Label for="group-name">{{ t('admin.groups.name') }}</Label><Input id="group-name" v-model="manualDraft.displayName" /></div><div><Label for="group-slug">{{ t('admin.groups.slug') }}</Label><Input id="group-slug" v-model="manualDraft.slug" /></div><div class="sm:col-span-2"><Label for="group-description">{{ t('admin.groups.description') }}</Label><Textarea id="group-description" v-model="manualDraft.description" /></div><label class="flex items-center justify-between gap-3 sm:col-span-2"><span class="text-sm font-medium">{{ t('admin.groups.exposed') }}</span><Switch v-model="manualDraft.exposedToDownstream" /></label><div class="flex justify-end sm:col-span-2"><Button :disabled="mutation.busy.value" @click="saveManual">{{ t('common.save') }}</Button></div></CardContent></Card>
      <Card v-if="group.kind === 'manual'"><CardHeader><CardTitle>{{ t('admin.groups.decisions') }}</CardTitle><CardDescription>{{ t('admin.groups.decisionsDescription') }}</CardDescription></CardHeader><CardContent><ManualDecisionEditor :decisions="decisionsQuery.data.value?.items ?? []" :accounts="accountsQuery.data.value?.items ?? []" :busy="mutation.busy.value" @set-decision="setDecision" @clear-decision="clearDecision" /></CardContent></Card>
    </template>
  </div>
</template>
