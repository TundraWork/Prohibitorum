<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { api } from '@/lib/api'
import type { AppGroup, GroupApplication, ManualDecision, ManualEffect, ProviderDescriptor } from '@/lib/appAccess'
import { buildPagePath, type Page } from '@/lib/pagination'
import { useApi } from '@/composables/useApi'
import { useResource } from '@/composables/useResource'
import { withSudo } from '@/lib/sudo'
import BackLink from '@/components/custom/BackLink.vue'
import AppIcon from '@/components/custom/AppIcon.vue'
import ErrorPanel from '@/components/custom/ErrorPanel.vue'
import ManualDecisionEditor from '@/components/custom/ManualDecisionEditor.vue'
import ProtocolBadge from '@/components/custom/ProtocolBadge.vue'
import RuleEditor, { type RuleEditorDraft } from '@/components/custom/RuleEditor.vue'
import StatusBadge from '@/components/custom/StatusBadge.vue'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'

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
const decisionsQuery = useResource(computed(() => ({
  queryKey: ['admin', 'groups', id.value, 'decisions'],
  staleTime: 0,
  enabled: detailQuery.data.value?.kind === 'manual',
  queryFn: async ({ signal }: { signal: AbortSignal }): Promise<Page<ManualDecision>> => {
    const items: ManualDecision[] = []
    const seen = new Set<string>()
    let cursor = ''
    do {
      seen.add(cursor)
      const page = await api.get<Page<ManualDecision>>(
        buildPagePath(`/api/prohibitorum/groups/${id.value}/decisions`, { limit: 100, cursor }),
        { signal },
      )
      items.push(...page.items)
      cursor = page.nextCursor
    } while (cursor && !seen.has(cursor))
    return { items, nextCursor: '' }
  },
})))
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
    <ErrorPanel :error="detailQuery.error.value ?? appsQuery.error.value ?? providersQuery.error.value ?? decisionsQuery.error.value ?? mutation.error.value" is-admin @dismiss="detailQuery.clear(); appsQuery.clear(); providersQuery.clear(); decisionsQuery.clear(); mutation.clear()" />
    <template v-if="group">
      <div class="flex flex-wrap items-start justify-between gap-3">
        <div class="min-w-0">
          <h1 class="break-words text-2xl font-semibold tracking-tight text-ink">{{ group.displayName }}</h1>
          <div class="mt-2 flex flex-wrap items-center gap-2">
            <span class="font-mono text-xs text-muted">{{ group.slug }} · #{{ group.id }}</span>
            <StatusBadge variant="neutral">{{ t(`admin.groups.${group.kind}`) }}</StatusBadge>
            <StatusBadge :variant="group.exposedToDownstream ? 'success' : 'neutral'">{{ group.exposedToDownstream ? t('admin.groups.exposed') : t('admin.groups.hidden') }}</StatusBadge>
          </div>
        </div>
        <div class="flex gap-2"><Button variant="outline" @click="editing = !editing">{{ editing ? t('common.cancel') : t('common.edit') }}</Button><Button variant="destructive" :disabled="applications.length > 0 || mutation.busy.value" @click="remove">{{ t('admin.groups.delete') }}</Button></div>
      </div>
      <RuleEditor v-if="editing && group.kind === 'rule' && group.rule" :initial-draft="{ slug: group.slug, displayName: group.displayName, description: group.description ?? '', exposedToDownstream: group.exposedToDownstream, rule: group.rule }" :providers="providersQuery.data.value ?? []" preview-endpoint="/api/prohibitorum/groups/rule-preview" :busy="mutation.busy.value" :server-error="mutation.error.value ?? undefined" mode="edit" @save="saveRule" @cancel="editing = false" />
      <Card v-else-if="editing"><CardHeader><CardTitle>{{ t('admin.groups.edit') }}</CardTitle></CardHeader><CardContent class="grid gap-4 sm:grid-cols-2"><div><Label for="group-name">{{ t('admin.groups.name') }}</Label><Input id="group-name" v-model="manualDraft.displayName" /></div><div><Label for="group-slug">{{ t('admin.groups.slug') }}</Label><Input id="group-slug" v-model="manualDraft.slug" /></div><div class="sm:col-span-2"><Label for="group-description">{{ t('admin.groups.description') }}</Label><Textarea id="group-description" v-model="manualDraft.description" /></div><label class="flex items-center justify-between gap-3 sm:col-span-2"><span class="text-sm font-medium">{{ t('admin.groups.exposed') }}</span><Switch v-model="manualDraft.exposedToDownstream" /></label><div class="flex justify-end sm:col-span-2"><Button :disabled="mutation.busy.value" @click="saveManual">{{ t('common.save') }}</Button></div></CardContent></Card>
      <Card v-if="group.kind === 'manual'"><CardHeader><CardTitle>{{ t('admin.groups.decisions') }}</CardTitle><CardDescription>{{ t('admin.groups.decisionsDescription') }}</CardDescription></CardHeader><CardContent><ManualDecisionEditor :decisions="decisionsQuery.data.value?.items ?? []" :busy="mutation.busy.value" @set-decision="setDecision" @clear-decision="clearDecision" /></CardContent></Card>

      <Card>
        <CardHeader><CardTitle>{{ t('admin.groups.sharedImpact') }}</CardTitle><CardDescription>{{ applications.length ? t('admin.groups.sharedImpactCount', { count: applications.length }) : t('admin.groups.unused') }}</CardDescription></CardHeader>
        <CardContent>
          <div v-if="applications.length" class="grid gap-3 sm:grid-cols-2" data-test="group-applications">
            <div v-for="app in applications" :key="`${app.kind}:${app.appId}`" class="flex min-w-0 items-center gap-3 rounded-lg border border-border bg-surface p-4">
              <AppIcon :src="app.iconUrl" :name="app.displayName" />
              <div class="min-w-0 flex-1">
                <strong class="block truncate text-sm text-ink">{{ app.displayName }}</strong>
                <span class="block truncate font-mono text-xs text-muted">{{ app.appId }}</span>
              </div>
              <ProtocolBadge :kind="app.kind" />
            </div>
          </div>
          <p v-else class="text-sm text-muted">{{ t('admin.groups.deleteReady') }}</p>
        </CardContent>
      </Card>
    </template>
  </div>
</template>
