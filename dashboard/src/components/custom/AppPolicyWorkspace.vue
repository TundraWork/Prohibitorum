<script setup lang="ts">
import { computed, inject, nextTick, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { matchedRouteKey, onBeforeRouteLeave, onBeforeRouteUpdate, routerKey } from 'vue-router'
import { Eye, MoreHorizontal, Pencil, Plus, Trash2, X } from 'lucide-vue-next'
import { api } from '@/lib/api'
import type {
  AccountSummary,
  AppAccessWorkspace,
  AppGroup,
  AppKind,
  ExplanationNode,
  GroupExplanation,
  GroupPreview,
  ManualDecision,
  ManualEffect,
} from '@/lib/appAccess'
import { buildPagePath, type Page } from '@/lib/pagination'
import { useApi } from '@/composables/useApi'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import ConfirmDialog from '@/components/custom/ConfirmDialog.vue'
import ErrorPanel from '@/components/custom/ErrorPanel.vue'
import ManualDecisionEditor from '@/components/custom/ManualDecisionEditor.vue'
import PaginationControls from '@/components/custom/PaginationControls.vue'
import ProtocolBadge from '@/components/custom/ProtocolBadge.vue'
import RuleEditor, { type RuleEditorDraft } from '@/components/custom/RuleEditor.vue'
import RuleMeaning from '@/components/custom/RuleMeaning.vue'
import StatusBadge from '@/components/custom/StatusBadge.vue'
import { cloneRule, makeEmptyRule } from '@/lib/ruleDraft'

interface WorkspaceIdentity {
  kind: AppKind
  appId: string
  displayName: string
}

type ActiveRuleEditor =
  | { mode: 'create'; groupId: null; initialDraft: RuleEditorDraft }
  | { mode: 'edit'; groupId: number; initialDraft: RuleEditorDraft }

type RuleEditorDestination =
  | { type: 'create' }
  | { type: 'edit'; group: AppGroup }
  | { type: 'preview'; groupId: number }
  | { type: 'navigate'; path: string }
  | { type: 'identity'; identity: WorkspaceIdentity }
interface ExplanationRow {
  key: string
  depth: number
  node: ExplanationNode
}

const props = defineProps<{
  kind: AppKind
  appId: string
  displayName: string
  mode: 'manager' | 'admin'
}>()

const { t } = useI18n()

const router = inject(routerKey, null)
const activeRouteRecord = inject(matchedRouteKey, null)
const workspaceApi = useApi()
const accountsApi = useApi()
const decisionsApi = useApi()
const previewApi = useApi()
const explanationApi = useApi()
const policyMutationApi = useApi()
const workspaceIdentity = ref<WorkspaceIdentity>({
  kind: props.kind,
  appId: props.appId,
  displayName: props.displayName,
})
const deferredWorkspaceIdentity = ref<WorkspaceIdentity | null>(null)

const workspace = ref<AppAccessWorkspace | null>(null)
const notFound = ref(false)
const loadedManualGroupId = ref<number | null>(null)

const manualCreateOpen = ref(false)
const manualDraft = reactive({ slug: '', displayName: '', description: '' })
const accounts = ref<AccountSummary[]>([])
const decisions = ref<ManualDecision[]>([])
const allDecisions = ref<ManualDecision[]>([])
const decisionIndexReady = ref(false)
const decisionsNextCursor = ref('')
const decisionsPageIndex = ref(0)
const decisionPageCursors = ref<string[]>([''])

const activeRuleEditor = ref<ActiveRuleEditor | null>(null)
const activeRuleEditorRevision = ref(0)
const ruleEditorDirty = ref(false)
const pendingRuleEditorDestination = ref<RuleEditorDestination | null>(null)
const confirmDiscardRuleEditor = ref(false)
const savedRuleGroupId = ref<number | null>(null)
const confirmDeleteRuleId = ref<number | null>(null)
const ruleSaveFinalizing = ref(false)

const confirmEmptyRestriction = ref(false)

const previewGroupId = ref<number | null>(null)
const previewItems = ref<GroupPreview[]>([])
const previewNextCursor = ref('')
const previewPageIndex = ref(0)
const previewPageCursors = ref<string[]>([''])

const explanationTarget = ref<{ groupId: number; accountId: number } | null>(null)
const explanation = ref<GroupExplanation | null>(null)
let allowRuleEditorNavigation = false
let workspaceInitialized = false
let workspaceIdentityVersion = 0
let workspaceLoadActive = false
let workspaceReloadPending = false

const basePath = computed(
  () =>
    `/api/prohibitorum/managed-applications/${encodeURIComponent(workspaceIdentity.value.kind)}/${encodeURIComponent(workspaceIdentity.value.appId)}`,
)
const accessEndpoint = computed(() => `${basePath.value}/access`)
const groupsEndpoint = computed(() => `${basePath.value}/groups`)
const accountsEndpoint = computed(() => `${basePath.value}/accounts`)

const appName = computed(() => workspace.value?.app.displayName || workspaceIdentity.value.displayName)
const manualGroup = computed(() => workspace.value?.manualGroup)
const ruleGroups = computed(() => workspace.value?.ruleGroups ?? [])
const rulePreviewEndpoint = computed(() => `${basePath.value}/rule-preview`)
const hasAnyPolicyGroup = computed(
  () => manualGroup.value !== undefined || ruleGroups.value.length > 0,
)
const ruleGroupCountLabel = computed(() =>
  t('manage.applications.ruleGroups', ruleGroups.value.length),
)
const manualFormValid = computed(
  () => manualDraft.slug.trim() !== '' && manualDraft.displayName.trim() !== '',
)
const ruleToDelete = computed(() =>
  ruleGroups.value.find((group) => group.id === confirmDeleteRuleId.value),
)
const ruleEditorBusy = computed(() => policyMutationApi.busy.value || ruleSaveFinalizing.value)
const previewHasMore = computed(() => previewNextCursor.value !== '')
const decisionsHaveMore = computed(() => decisionsNextCursor.value !== '')
const explanationRows = computed(() =>
  explanation.value ? flattenExplanation(explanation.value.explanation) : [],
)

function makeRuleDraft(group?: AppGroup): RuleEditorDraft {
  return {
    slug: group?.slug ?? '',
    displayName: group?.displayName ?? '',
    description: group?.description ?? '',
    exposedToDownstream: group?.exposedToDownstream ?? true,
    rule: cloneRule(group?.rule ?? makeEmptyRule()),
  }
}

function flattenExplanation(
  node: ExplanationNode,
  key = 'root',
  depth = 0,
  rows: ExplanationRow[] = [],
): ExplanationRow[] {
  rows.push({ key, depth, node })
  for (const [index, child] of (node.children ?? []).entries()) {
    flattenExplanation(child, `${key}-${index}`, depth + 1, rows)
  }
  return rows
}

const explanationIndentClasses = [
  'ps-0',
  'ps-4',
  'ps-8',
  'ps-12',
  'ps-16',
  'ps-20',
  'ps-24',
  'ps-28',
] as const

function explanationIndent(depth: number): string {
  return explanationIndentClasses[Math.min(depth, explanationIndentClasses.length - 1)]!
}

function accountName(account: AccountSummary): string {
  return account.displayName.trim() || account.username
}

function clearManualData(): void {
  loadedManualGroupId.value = null
  accounts.value = []
  decisions.value = []
  allDecisions.value = []
  decisionIndexReady.value = false
  decisionsNextCursor.value = ''
  decisionsPageIndex.value = 0
  decisionPageCursors.value = ['']
  accountsApi.clear()
  decisionsApi.clear()
}

function resetWorkspaceState(): void {
  workspace.value = null
  notFound.value = false
  clearManualData()
  manualCreateOpen.value = false
  manualDraft.slug = ''
  manualDraft.displayName = ''
  manualDraft.description = ''
  activeRuleEditor.value = null
  ruleEditorDirty.value = false
  pendingRuleEditorDestination.value = null
  ruleSaveFinalizing.value = false
  confirmDiscardRuleEditor.value = false
  savedRuleGroupId.value = null
  confirmDeleteRuleId.value = null
  confirmEmptyRestriction.value = false
  previewGroupId.value = null
  previewItems.value = []
  previewNextCursor.value = ''
  previewPageIndex.value = 0
  previewPageCursors.value = ['']
  explanationTarget.value = null
  explanation.value = null
  workspaceApi.clear()
  previewApi.clear()
  explanationApi.clear()
  policyMutationApi.clear()
}

function sameWorkspaceIdentity(left: WorkspaceIdentity, right: WorkspaceIdentity): boolean {
  return left.kind === right.kind && left.appId === right.appId
}

function applyWorkspaceIdentity(identity: WorkspaceIdentity): void {
  workspaceIdentity.value = identity
  deferredWorkspaceIdentity.value = null
  workspaceIdentityVersion += 1
  resetWorkspaceState()
  void loadWorkspace(workspaceIdentityVersion)
}

function applyDeferredWorkspaceIdentity(): boolean {
  const identity = deferredWorkspaceIdentity.value
  if (!identity) return false
  applyWorkspaceIdentity(identity)
  return true
}

async function loadWorkspace(version = workspaceIdentityVersion): Promise<void> {
  if (workspaceLoadActive) {
    workspaceReloadPending = true
    return
  }

  workspaceLoadActive = true
  workspaceReloadPending = false
  notFound.value = false
  const endpoint = accessEndpoint.value

  try {
    const result = await workspaceApi.run(() => api.get<AppAccessWorkspace>(endpoint))

    if (version !== workspaceIdentityVersion || endpoint !== accessEndpoint.value) {
      workspaceApi.clear()
      return
    }

    if (!result) {
      if (props.mode === 'manager' && workspaceApi.error.value?.code === 'client_not_found') {
        workspace.value = null
        notFound.value = true
        clearManualData()
      }
      return
    }

    workspace.value = result
    const group = result.manualGroup
    if (!group) {
      clearManualData()
      return
    }

    if (loadedManualGroupId.value === group.id) return
    loadedManualGroupId.value = group.id
    accounts.value = []
    decisions.value = []
    allDecisions.value = []
    decisionIndexReady.value = false
    decisionsNextCursor.value = ''
    decisionsPageIndex.value = 0
    decisionPageCursors.value = ['']
    await Promise.all([loadAllAccounts(), resetDecisionPage()])
  } finally {
    workspaceLoadActive = false
    if (workspaceReloadPending || version !== workspaceIdentityVersion) {
      workspaceReloadPending = false
      void loadWorkspace(workspaceIdentityVersion)
    }
  }
}

async function loadAllAccounts(): Promise<void> {
  const groupId = manualGroup.value?.id
  if (groupId === undefined) return

  const result = await accountsApi.run(async () => {
    const items: AccountSummary[] = []
    const visitedCursors = new Set<string>()
    let cursor = ''

    while (!visitedCursors.has(cursor)) {
      visitedCursors.add(cursor)
      const page = await api.get<Page<AccountSummary>>(
        buildPagePath(accountsEndpoint.value, { cursor }),
      )
      items.push(...(page.items ?? []))
      cursor = page.nextCursor ?? ''
      if (cursor === '') break
    }

    return items
  })

  if (result && manualGroup.value?.id === groupId) accounts.value = result
}

function decisionsEndpoint(groupId: number): string {
  return `${groupsEndpoint.value}/${groupId}/decisions`
}

async function loadDecisionPage(cursor: string): Promise<boolean> {
  const groupId = manualGroup.value?.id
  if (groupId === undefined) return false

  const result = await decisionsApi.run(() =>
    api.get<Page<ManualDecision>>(
      buildPagePath(decisionsEndpoint(groupId), { cursor }),
    ),
  )
  if (!result || manualGroup.value?.id !== groupId) return false

  decisions.value = result.items ?? []
  decisionsNextCursor.value = result.nextCursor ?? ''
  return true
}

async function loadAllDecisions(): Promise<boolean> {
  const groupId = manualGroup.value?.id
  if (groupId === undefined) return false
  decisionIndexReady.value = false

  const result = await decisionsApi.run(async () => {
    const byAccount = new Map<number, ManualDecision>()
    const visitedCursors = new Set<string>()
    let cursor = ''

    while (!visitedCursors.has(cursor)) {
      visitedCursors.add(cursor)
      const page = await api.get<Page<ManualDecision>>(
        buildPagePath(decisionsEndpoint(groupId), { cursor }),
      )
      for (const decision of page.items ?? []) {
        byAccount.set(decision.account.id, decision)
      }
      cursor = page.nextCursor ?? ''
      if (cursor === '') break
    }

    return [...byAccount.values()]
  })

  if (!result || manualGroup.value?.id !== groupId) return false
  allDecisions.value = result
  decisionIndexReady.value = true
  return true
}

async function resetDecisionPage(): Promise<void> {
  decisionPageCursors.value = ['']
  decisionsPageIndex.value = 0
  if (await loadDecisionPage('')) await loadAllDecisions()
}

async function nextDecisionPage(): Promise<void> {
  if (decisionsApi.busy.value || decisionsNextCursor.value === '') return
  const cursor = decisionsNextCursor.value
  if (!(await loadDecisionPage(cursor))) return
  decisionPageCursors.value = [
    ...decisionPageCursors.value.slice(0, decisionsPageIndex.value + 1),
    cursor,
  ]
  decisionsPageIndex.value += 1
}

async function previousDecisionPage(): Promise<void> {
  if (decisionsApi.busy.value || decisionsPageIndex.value <= 0) return
  const targetIndex = decisionsPageIndex.value - 1
  const cursor = decisionPageCursors.value[targetIndex] ?? ''
  if (await loadDecisionPage(cursor)) decisionsPageIndex.value = targetIndex
}

async function reloadDecisionPage(): Promise<void> {
  const cursor = decisionPageCursors.value[decisionsPageIndex.value] ?? ''
  if (!(await loadDecisionPage(cursor))) return
  if (decisions.value.length === 0 && decisionsPageIndex.value > 0) {
    const targetIndex = decisionsPageIndex.value - 1
    const previousCursor = decisionPageCursors.value[targetIndex] ?? ''
    if (await loadDecisionPage(previousCursor)) decisionsPageIndex.value = targetIndex
  }
  await loadAllDecisions()
}

function openManualCreate(): void {
  manualDraft.slug = ''
  manualDraft.displayName = ''
  manualDraft.description = ''
  manualCreateOpen.value = true
}

async function createManualGroup(): Promise<void> {
  if (!manualFormValid.value || policyMutationApi.busy.value || manualGroup.value) return

  const created = await policyMutationApi.run(() =>
    api.post<AppGroup>(groupsEndpoint.value, {
      kind: 'manual',
      slug: manualDraft.slug.trim(),
      displayName: manualDraft.displayName.trim(),
      description: manualDraft.description.trim(),
      exposedToDownstream: false,
    }),
  )
  if (created === undefined) return

  manualCreateOpen.value = false
  await loadWorkspace()
}

async function setManualDecision(payload: {
  accountId: number
  effect: ManualEffect
}): Promise<void> {
  const groupId = manualGroup.value?.id
  if (groupId === undefined || policyMutationApi.busy.value) return

  const result = await policyMutationApi.run(() =>
    api.post<ManualDecision>(decisionsEndpoint(groupId), payload),
  )
  if (result !== undefined) await reloadDecisionPage()
}

async function clearManualDecision(payload: { accountId: number }): Promise<void> {
  const groupId = manualGroup.value?.id
  if (groupId === undefined || policyMutationApi.busy.value) return

  const result = await policyMutationApi.run(() =>
    api.post<object>(`${decisionsEndpoint(groupId)}/clear`, payload),
  )
  if (result !== undefined) await reloadDecisionPage()
}

function openRuleCreate(): void {
  requestRuleEditorDestination({ type: 'create' })
}

function editRuleGroup(group: AppGroup): void {
  requestRuleEditorDestination({ type: 'edit', group })
}

function requestRuleEditorDestination(destination: RuleEditorDestination): void {
  if (activeRuleEditor.value && ruleEditorDirty.value) {
    pendingRuleEditorDestination.value = destination
    confirmDiscardRuleEditor.value = true
    return
  }
  void applyRuleEditorDestination(destination)
}

async function applyRuleEditorDestination(destination: RuleEditorDestination): Promise<void> {
  pendingRuleEditorDestination.value = null
  confirmDiscardRuleEditor.value = false

  if (destination.type === 'navigate') {
    activeRuleEditor.value = null
    ruleEditorDirty.value = false
    allowRuleEditorNavigation = true
    await router?.push(destination.path)
    return
  }

  if (destination.type === 'identity') {
    applyWorkspaceIdentity(destination.identity)
    return
  }


  if (destination.type === 'create') {
    closePreview()
    activeRuleEditor.value = { mode: 'create', groupId: null, initialDraft: makeRuleDraft() }
    ruleEditorDirty.value = false
    return
  }

  if (destination.type === 'edit') {
    closePreview()
    activeRuleEditor.value = {
      mode: 'edit',
      groupId: destination.group.id,
      initialDraft: makeRuleDraft(destination.group),
    }
    ruleEditorDirty.value = false
    return
  }

  activeRuleEditor.value = null
  ruleEditorDirty.value = false
  await openSavedPreview(destination.groupId)
}

function cancelRuleEditor(): void {
  activeRuleEditor.value = null
  ruleEditorDirty.value = false
  policyMutationApi.clear()
  applyDeferredWorkspaceIdentity()
}

function cancelDiscardRuleEditor(): void {
  confirmDiscardRuleEditor.value = false
  pendingRuleEditorDestination.value = null
}

function discardRuleEditorAndContinue(): void {
  const destination = pendingRuleEditorDestination.value
  activeRuleEditor.value = null
  ruleEditorDirty.value = false
  pendingRuleEditorDestination.value = null
  confirmDiscardRuleEditor.value = false
  activeRuleEditorRevision.value += 1
  if (destination) void applyRuleEditorDestination(destination)
}

function guardRuleEditorNavigation(path: string): boolean {
  if (allowRuleEditorNavigation) {
    allowRuleEditorNavigation = false
    return true
  }
  if (!activeRuleEditor.value || !ruleEditorDirty.value) return true
  pendingRuleEditorDestination.value = { type: 'navigate', path }
  confirmDiscardRuleEditor.value = true
  return false
}

function ruleWritePayload(draft: RuleEditorDraft): RuleEditorDraft {
  return {
    slug: draft.slug.trim(),
    displayName: draft.displayName.trim(),
    description: draft.description.trim(),
    exposedToDownstream: draft.exposedToDownstream,
    rule: cloneRule(draft.rule),
  }
}

async function saveRuleGroup(draft: RuleEditorDraft): Promise<void> {
  const editor = activeRuleEditor.value
  if (!editor || ruleEditorBusy.value) return

  ruleSaveFinalizing.value = true
  const saved = await policyMutationApi.run(() => editor.mode === 'create'
    ? api.post<AppGroup>(groupsEndpoint.value, { kind: 'rule', ...ruleWritePayload(draft) })
    : api.put<AppGroup>(`${groupsEndpoint.value}/${editor.groupId}`, ruleWritePayload(draft)),
  )
  if (!saved) {
    ruleSaveFinalizing.value = false
    return
  }

  await loadWorkspace()
  activeRuleEditor.value = null
  ruleEditorDirty.value = false
  savedRuleGroupId.value = saved.id
  await nextTick()
  document.querySelector<HTMLElement>(`[data-test="rule-group-row-${saved.id}"]`)?.focus()
  ruleSaveFinalizing.value = false
}

async function deleteRuleGroup(): Promise<void> {
  const groupId = confirmDeleteRuleId.value
  if (groupId === null || policyMutationApi.busy.value) return

  const result = await policyMutationApi.run(() =>
    api.post<object>(`${groupsEndpoint.value}/${groupId}/delete`),
  )
  confirmDeleteRuleId.value = null
  if (result !== undefined) await loadWorkspace()
}

async function onRestrictionChange(restricted: boolean): Promise<void> {
  if (policyMutationApi.busy.value) return
  if (restricted && !hasAnyPolicyGroup.value) {
    confirmEmptyRestriction.value = true
    return
  }
  await setRestricted(restricted)
}

async function confirmEnableEmptyRestriction(): Promise<void> {
  confirmEmptyRestriction.value = false
  await setRestricted(true)
}

async function setRestricted(restricted: boolean): Promise<void> {
  const result = await policyMutationApi.run(() =>
    api.post<object>(`${accessEndpoint.value}/set-restricted`, { restricted }),
  )
  if (result !== undefined) await loadWorkspace()
}

function previewEndpoint(groupId: number): string {
  return `${groupsEndpoint.value}/${groupId}/preview`
}

async function loadPreviewPage(groupId: number, cursor: string): Promise<boolean> {
  const result = await previewApi.run(() =>
    api.get<Page<GroupPreview>>(
      buildPagePath(previewEndpoint(groupId), { cursor }),
    ),
  )
  if (!result || previewGroupId.value !== groupId) return false

  previewItems.value = result.items ?? []
  previewNextCursor.value = result.nextCursor ?? ''
  return true
}

async function openPreview(groupId: number): Promise<void> {
  if (previewApi.busy.value) return
  if (!activeRuleEditor.value && previewGroupId.value === groupId) {
    closePreview()
    return
  }
  requestRuleEditorDestination({ type: 'preview', groupId })
}

async function openSavedPreview(groupId: number): Promise<void> {
  previewGroupId.value = groupId
  previewItems.value = []
  previewNextCursor.value = ''
  previewPageIndex.value = 0
  previewPageCursors.value = ['']
  closeExplanation()
  await loadPreviewPage(groupId, '')
}

function closePreview(): void {
  previewGroupId.value = null
  previewItems.value = []
  previewNextCursor.value = ''
  previewPageIndex.value = 0
  previewPageCursors.value = ['']
  previewApi.clear()
  closeExplanation()
}

async function nextPreviewPage(): Promise<void> {
  const groupId = previewGroupId.value
  if (groupId === null || previewApi.busy.value || previewNextCursor.value === '') return
  const cursor = previewNextCursor.value
  closeExplanation()
  if (!(await loadPreviewPage(groupId, cursor))) return
  previewPageCursors.value = [
    ...previewPageCursors.value.slice(0, previewPageIndex.value + 1),
    cursor,
  ]
  previewPageIndex.value += 1
}

async function previousPreviewPage(): Promise<void> {
  const groupId = previewGroupId.value
  if (groupId === null || previewApi.busy.value || previewPageIndex.value <= 0) return
  const targetIndex = previewPageIndex.value - 1
  const cursor = previewPageCursors.value[targetIndex] ?? ''
  closeExplanation()
  if (await loadPreviewPage(groupId, cursor)) previewPageIndex.value = targetIndex
}

async function reloadPreview(): Promise<void> {
  const groupId = previewGroupId.value
  if (groupId === null) return
  const cursor = previewPageCursors.value[previewPageIndex.value] ?? ''
  await loadPreviewPage(groupId, cursor)
}

function explanationEndpoint(groupId: number, accountId: number): string {
  return `${groupsEndpoint.value}/${groupId}/explain/${accountId}`
}

async function openExplanation(groupId: number, accountId: number): Promise<void> {
  if (explanationApi.busy.value) return
  explanationTarget.value = { groupId, accountId }
  explanation.value = null
  explanationApi.clear()
  await reloadExplanation()
}

async function reloadExplanation(): Promise<void> {
  const target = explanationTarget.value
  if (!target) return

  const result = await explanationApi.run(() =>
    api.get<GroupExplanation>(explanationEndpoint(target.groupId, target.accountId)),
  )
  if (
    result &&
    explanationTarget.value?.groupId === target.groupId &&
    explanationTarget.value.accountId === target.accountId
  ) {
    explanation.value = result
  }
}

function closeExplanation(): void {
  explanationTarget.value = null
  explanation.value = null
  explanationApi.clear()
}

if (router && activeRouteRecord) {
  onBeforeRouteLeave((to) => guardRuleEditorNavigation(to.fullPath))
  onBeforeRouteUpdate((to) => guardRuleEditorNavigation(to.fullPath))
}

watch(
  () => [props.kind, props.appId, props.displayName] as const,
  ([kind, appId, displayName]) => {
    const identity: WorkspaceIdentity = { kind, appId, displayName }
    if (!workspaceInitialized) {
      workspaceInitialized = true
      applyWorkspaceIdentity(identity)
      return
    }
    if (sameWorkspaceIdentity(identity, workspaceIdentity.value)) {
      workspaceIdentity.value = identity
      deferredWorkspaceIdentity.value = null
      if (pendingRuleEditorDestination.value?.type === 'identity') {
        pendingRuleEditorDestination.value = null
        confirmDiscardRuleEditor.value = false
      }
      return
    }
    if (activeRuleEditor.value && ruleEditorDirty.value) {
      deferredWorkspaceIdentity.value = identity
      pendingRuleEditorDestination.value = { type: 'identity', identity }
      confirmDiscardRuleEditor.value = true
      return
    }
    applyWorkspaceIdentity(identity)
  },
  { immediate: true },
)
</script>

<template>
  <section data-test="app-policy-workspace" class="flex min-w-0 flex-col gap-6">
    <div
      v-if="workspaceApi.busy.value && !workspace"
      role="status"
      aria-live="polite"
      data-test="policy-loading"
      class="rounded-lg border border-border bg-sunken px-4 py-6 text-sm text-muted"
    >
      {{ t('common.loading') }}
    </div>

    <p
      v-else-if="notFound"
      role="status"
      class="rounded-lg border border-border bg-sunken px-4 py-6 text-sm text-muted"
    >
      {{ t('manage.applications.notFound') }}
    </p>

    <div v-else-if="!workspace" class="flex flex-col items-start gap-3">
      <ErrorPanel
        :error="workspaceApi.error.value"
        :is-admin="mode === 'admin'"
        :dismissible="false"
        @recovery="loadWorkspace"
      />
      <Button
        type="button"
        variant="outline"
        class="shadow-none"
        :disabled="workspaceApi.busy.value"
        data-test="policy-retry"
        @click="loadWorkspace"
      >
        {{ t('common.tryAgain') }}
      </Button>
    </div>

    <template v-else>
      <header
        v-if="mode === 'manager'"
        data-test="managed-application-detail"
        class="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between"
      >
        <div class="min-w-0">
          <h1 class="truncate text-2xl font-semibold tracking-tight text-ink">
            {{ workspace.app.displayName }}
          </h1>
          <p class="truncate font-mono text-xs text-muted">{{ workspace.app.appId }}</p>
          <p class="mt-2 max-w-2xl text-sm leading-relaxed text-muted">
            {{ t('manage.applications.detailSubtitle') }}
          </p>
        </div>
        <ProtocolBadge
          :kind="workspace.app.kind"
          class="size-9 shrink-0 rounded-md border border-border bg-sunken text-muted"
        />
      </header>
      <span v-else class="sr-only">{{ appName }}</span>

      <ErrorPanel
        v-if="workspaceApi.error.value"
        :error="workspaceApi.error.value"
        :is-admin="mode === 'admin'"
        @dismiss="workspaceApi.clear"
        @recovery="loadWorkspace"
      />
      <ErrorPanel
        v-if="policyMutationApi.error.value"
        :error="policyMutationApi.error.value"
        :is-admin="mode === 'admin'"
        @dismiss="policyMutationApi.clear"
      />

      <Card class="shadow-none">
        <CardHeader>
          <CardTitle>{{ t('manage.applications.accessTitle') }}</CardTitle>
        </CardHeader>
        <CardContent>
          <div class="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
            <div class="flex min-w-0 flex-col items-start gap-2">
              <div data-test="restriction-state">
                <StatusBadge :variant="workspace.accessRestricted ? 'caution' : 'success'">
                  {{
                    workspace.accessRestricted
                      ? t('manage.applications.restricted')
                      : t('manage.applications.open')
                  }}
                </StatusBadge>
              </div>
              <p data-test="restriction-hint" class="max-w-2xl text-sm leading-relaxed text-muted">
                {{
                  workspace.accessRestricted
                    ? t('manage.applications.restrictedDescription')
                    : t('manage.applications.openDescription')
                }}
              </p>
            </div>
            <div class="flex shrink-0 items-center gap-3">
              <Label for="access-restricted-toggle" class="text-sm text-ink">
                {{ t('manage.policy.workspace.restrictionToggle') }}
              </Label>
              <Switch
                id="access-restricted-toggle"
                :model-value="workspace.accessRestricted"
                :disabled="policyMutationApi.busy.value"
                :aria-label="t('manage.policy.workspace.restrictionToggle')"
                data-test="access-restricted-toggle"
                @update:model-value="onRestrictionChange"
              />
            </div>
          </div>
        </CardContent>
      </Card>

      <Card class="shadow-none">
        <CardHeader>
          <CardTitle>{{ t('manage.applications.manualGroup') }}</CardTitle>
          <CardDescription>{{ t('manage.policy.workspace.manualDescription') }}</CardDescription>
        </CardHeader>
        <CardContent class="flex flex-col gap-4">
          <div class="flex flex-col items-start gap-3 sm:flex-row sm:items-center sm:justify-between">
            <StatusBadge :variant="manualGroup ? 'info' : 'neutral'">
              {{
                manualGroup
                  ? t('manage.applications.manualGroupReady', { name: manualGroup.displayName })
                  : t('manage.applications.noManualGroup')
              }}
            </StatusBadge>
            <Button
              v-if="!manualGroup && !manualCreateOpen"
              type="button"
              variant="outline"
              class="w-full shadow-none sm:w-auto"
              data-test="manual-group-create"
              @click="openManualCreate"
            >
              <Plus class="size-4" aria-hidden="true" />
              {{ t('manage.policy.workspace.createManual') }}
            </Button>
          </div>

          <form
            v-if="!manualGroup && manualCreateOpen"
            data-test="manual-group-form"
            class="flex flex-col gap-4 rounded-lg border border-border bg-sunken p-4"
            @submit.prevent="createManualGroup"
          >
            <h3 class="text-base font-semibold text-ink">
              {{ t('manage.policy.workspace.createManualTitle') }}
            </h3>
            <div class="grid gap-4 sm:grid-cols-2">
              <div class="flex flex-col gap-1.5">
                <Label for="manual-group-slug">{{ t('manage.policy.workspace.groupSlug') }}</Label>
                <Input
                  id="manual-group-slug"
                  v-model="manualDraft.slug"
                  name="slug"
                  autocomplete="off"
                  required
                  class="bg-surface shadow-none"
                />
              </div>
              <div class="flex flex-col gap-1.5">
                <Label for="manual-group-display-name">
                  {{ t('manage.policy.workspace.groupDisplayName') }}
                </Label>
                <Input
                  id="manual-group-display-name"
                  v-model="manualDraft.displayName"
                  name="displayName"
                  autocomplete="off"
                  required
                  class="bg-surface shadow-none"
                />
              </div>
            </div>
            <div class="flex flex-col gap-1.5">
              <Label for="manual-group-description">
                {{ t('manage.policy.workspace.groupDescription') }}
              </Label>
              <Textarea
                id="manual-group-description"
                v-model="manualDraft.description"
                name="description"
                class="bg-surface shadow-none"
              />
            </div>
            <div class="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
              <Button
                type="button"
                variant="ghost"
                :disabled="policyMutationApi.busy.value"
                @click="manualCreateOpen = false"
              >
                {{ t('common.cancel') }}
              </Button>
              <Button
                type="submit"
                class="w-full sm:w-auto"
                :disabled="!manualFormValid || policyMutationApi.busy.value"
                data-test="manual-group-save"
                @click.prevent="createManualGroup"
              >
                {{ t('common.save') }}
              </Button>
            </div>
          </form>

          <template v-if="manualGroup">
            <div v-if="accountsApi.error.value" class="flex flex-col items-start gap-2">
              <ErrorPanel
                :error="accountsApi.error.value"
                :is-admin="mode === 'admin'"
                @dismiss="accountsApi.clear"
                @recovery="loadAllAccounts"
              />
              <Button type="button" variant="outline" class="shadow-none" @click="loadAllAccounts">
                {{ t('common.tryAgain') }}
              </Button>
            </div>
            <div v-if="decisionsApi.error.value" class="flex flex-col items-start gap-2">
              <ErrorPanel
                :error="decisionsApi.error.value"
                :is-admin="mode === 'admin'"
                :dismissible="decisionIndexReady"
                @dismiss="decisionsApi.clear"
                @recovery="reloadDecisionPage"
              />
              <Button type="button" variant="outline" class="shadow-none" @click="reloadDecisionPage">
                {{ t('common.tryAgain') }}
              </Button>
            </div>

            <p
              v-if="accountsApi.busy.value || decisionsApi.busy.value"
              role="status"
              aria-live="polite"
              class="text-sm text-muted"
            >
              {{ t('common.loading') }}
            </p>

            <ManualDecisionEditor
              :decisions="decisions"
              :all-decisions="allDecisions"
              :accounts="accounts"
              :busy="
                policyMutationApi.busy.value ||
                accountsApi.busy.value ||
                decisionsApi.busy.value ||
                !decisionIndexReady ||
                Boolean(accountsApi.error.value) ||
                Boolean(decisionsApi.error.value)
              "
              @set-decision="setManualDecision"
              @clear-decision="clearManualDecision"
            />
            <PaginationControls
              :page-index="decisionsPageIndex"
              :has-more="decisionsHaveMore"
              :busy="
                decisionsApi.busy.value ||
                Boolean(decisionsApi.error.value) ||
                !decisionIndexReady
              "
              :has-items="decisions.length > 0"
              @next="nextDecisionPage"
              @previous="previousDecisionPage"
            />
          </template>
        </CardContent>
      </Card>

      <Card class="shadow-none">
        <CardHeader class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
          <div class="flex min-w-0 flex-col gap-1.5">
            <CardTitle>{{ t('manage.policy.rule.sectionTitle') }}</CardTitle>
            <CardDescription>
              {{ ruleGroupCountLabel }} · {{ t('manage.policy.rule.sectionDescription') }}
            </CardDescription>
          </div>
          <Button
            type="button"
            variant="outline"
            class="w-full shrink-0 shadow-none sm:w-auto"
            data-test="rule-group-create"
            @click="openRuleCreate"
          >
            <Plus class="size-4" aria-hidden="true" />
            {{ t('manage.policy.rule.create') }}
          </Button>
        </CardHeader>

        <CardContent class="flex flex-col gap-4">
          <p
            v-if="savedRuleGroupId !== null"
            role="status"
            aria-live="polite"
            data-test="rule-save-announcement"
            class="rounded-md bg-sage-50 px-3 py-2 text-sm font-medium text-sage-700"
          >
            {{ t('manage.policy.rule.saved') }}
          </p>


          <p
            v-if="ruleGroups.length === 0 && activeRuleEditor?.mode !== 'create'"
            role="status"
            class="rounded-lg border border-border bg-sunken px-4 py-6 text-sm text-muted"
          >
            {{ ruleGroupCountLabel }}
          </p>

          <ul
            v-if="ruleGroups.length > 0 || activeRuleEditor?.mode === 'create'"
            data-test="rule-groups-list"
            class="divide-y divide-border overflow-hidden rounded-lg border border-border"
          >
            <li
              v-if="activeRuleEditor?.mode === 'create'"
              data-test="rule-group-draft-row"
              class="bg-surface"
            >
              <RuleEditor
                :key="`create-new-${activeRuleEditorRevision}`"
                :initial-draft="activeRuleEditor.initialDraft"
                :providers="workspace.providers"
                :preview-endpoint="rulePreviewEndpoint"
                :busy="ruleEditorBusy"
                :server-error="policyMutationApi.error.value ?? undefined"
                mode="create"
                @save="saveRuleGroup"
                @cancel="cancelRuleEditor"
                @dirty-change="ruleEditorDirty = $event"
              />
            </li>
            <li
              v-for="group in ruleGroups"
              :key="group.id"
              :data-test="`rule-group-row-${group.id}`"
              :tabindex="savedRuleGroupId === group.id ? -1 : undefined"
              class="bg-surface outline-none focus-visible:ring-2 focus-visible:ring-ring"
            >
              <div v-if="activeRuleEditor?.groupId !== group.id" class="flex flex-col gap-4 p-4 sm:flex-row sm:items-start sm:justify-between">
                <div class="min-w-0">
                  <h3 class="font-semibold text-ink">{{ group.displayName }}</h3>
                  <p class="truncate font-mono text-xs text-muted">{{ group.slug }}</p>
                  <p v-if="group.description" class="mt-2 text-sm leading-relaxed text-muted">
                    {{ group.description }}
                  </p>
                  <RuleMeaning
                    v-if="group.rule"
                    compact
                    :rule="group.rule"
                    :providers="workspace.providers"
                    class="mt-3"
                  />
                  <details
                    v-if="group.rule"
                    :data-test="`rule-meaning-disclosure-${group.id}`"
                    class="mt-3 max-w-2xl rounded-md border border-border bg-sunken px-3 py-2"
                  >
                    <summary class="cursor-pointer text-sm font-medium text-ink">
                      {{ t('manage.policy.rule.plainMeaning') }}
                    </summary>
                    <RuleMeaning :rule="group.rule" :providers="workspace.providers" class="mt-3" />
                  </details>
                  <div class="mt-3 flex flex-wrap gap-2">
                    <StatusBadge :variant="group.exposedToDownstream ? 'info' : 'neutral'">
                      {{
                        group.exposedToDownstream
                          ? t('manage.policy.rule.exposedStatus')
                          : t('manage.policy.rule.privateStatus')
                      }}
                    </StatusBadge>
                  </div>
                </div>

                <div class="flex flex-wrap gap-2 sm:shrink-0 sm:justify-end">
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    class="shadow-none"
                    :disabled="previewApi.busy.value || policyMutationApi.busy.value"
                    :data-test="`rule-group-preview-${group.id}`"
                    @click="openPreview(group.id)"
                  >
                    <Eye class="size-4" aria-hidden="true" />
                    {{ t('manage.policy.rule.preview') }}
                  </Button>
                  <DropdownMenu>
                    <DropdownMenuTrigger as-child>
                      <Button
                        type="button"
                        variant="ghost"
                        size="icon-sm"
                        :aria-label="t('manage.policy.rule.savedActions', { name: group.displayName })"
                        :title="t('manage.policy.rule.savedActions', { name: group.displayName })"
                        :data-test="`rule-group-actions-${group.id}`"
                      >
                        <MoreHorizontal class="size-4" aria-hidden="true" />
                      </Button>
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="end">
                      <DropdownMenuItem
                        :disabled="policyMutationApi.busy.value"
                        :data-test="`rule-group-edit-${group.id}`"
                        @select="editRuleGroup(group)"
                      >
                        <Pencil class="size-4" aria-hidden="true" />
                        {{ t('manage.policy.rule.edit') }}
                      </DropdownMenuItem>
                      <DropdownMenuItem
                        class="text-destructive focus:text-destructive"
                        :disabled="policyMutationApi.busy.value"
                        :data-test="`rule-group-delete-${group.id}`"
                        @select="confirmDeleteRuleId = group.id"
                      >
                        <Trash2 class="size-4" aria-hidden="true" />
                        {{ t('manage.policy.rule.delete') }}
                      </DropdownMenuItem>
                    </DropdownMenuContent>
                  </DropdownMenu>
                </div>
              </div>

              <RuleEditor
                v-if="activeRuleEditor?.mode === 'edit' && activeRuleEditor.groupId === group.id"
                :key="`edit-${group.id}-${activeRuleEditorRevision}`"
                :initial-draft="activeRuleEditor.initialDraft"
                :providers="workspace.providers"
                :preview-endpoint="rulePreviewEndpoint"
                :busy="ruleEditorBusy"
                :server-error="policyMutationApi.error.value ?? undefined"
                mode="edit"
                @save="saveRuleGroup"
                @cancel="cancelRuleEditor"
                @dirty-change="ruleEditorDirty = $event"
              />

              <div
                v-if="previewGroupId === group.id"
                :data-test="`preview-panel-${group.id}`"
                class="flex flex-col gap-4 border-t border-border bg-sunken p-4"
              >
                <div class="flex flex-wrap items-center justify-between gap-3">
                  <h4 class="text-base font-semibold text-ink">
                    {{ t('manage.policy.rule.previewTitle', { name: group.displayName }) }}
                  </h4>
                  <Button type="button" variant="ghost" size="sm" @click="closePreview">
                    <X class="size-4" aria-hidden="true" />
                    {{ t('manage.policy.rule.closePreview') }}
                  </Button>
                </div>

                <ErrorPanel
                  v-if="previewApi.error.value"
                  :error="previewApi.error.value"
                  :is-admin="mode === 'admin'"
                  @dismiss="previewApi.clear"
                  @recovery="reloadPreview"
                />
                <p
                  v-if="previewApi.busy.value && previewItems.length === 0"
                  role="status"
                  aria-live="polite"
                  class="text-sm text-muted"
                >
                  {{ t('common.loading') }}
                </p>
                <p
                  v-else-if="!previewApi.error.value && previewItems.length === 0"
                  role="status"
                  class="text-sm text-muted"
                >
                  {{ t('manage.policy.rule.previewEmpty') }}
                </p>

                <ul
                  v-else-if="previewItems.length > 0"
                  class="divide-y divide-border overflow-hidden rounded-lg border border-border bg-surface"
                >
                  <li
                    v-for="item in previewItems"
                    :key="item.account.id"
                    :data-test="`preview-row-${item.account.id}`"
                    class="p-4"
                  >
                    <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
                      <div class="min-w-0">
                        <p class="truncate text-sm font-medium text-ink">
                          {{ accountName(item.account) }}
                        </p>
                        <p class="truncate font-mono text-xs text-muted">
                          {{ item.account.username }}
                        </p>
                      </div>
                      <div class="flex flex-wrap items-center gap-2 sm:shrink-0">
                        <StatusBadge :variant="item.matched ? 'success' : 'neutral'">
                          {{
                            item.matched
                              ? t('manage.policy.rule.matches')
                              : t('manage.policy.rule.doesNotMatch')
                          }}
                        </StatusBadge>
                        <Button
                          type="button"
                          variant="outline"
                          size="sm"
                          class="shadow-none"
                          :disabled="explanationApi.busy.value"
                          :data-test="`preview-explain-${group.id}-${item.account.id}`"
                          @click="openExplanation(group.id, item.account.id)"
                        >
                          {{ t('manage.policy.rule.explain') }}
                        </Button>
                      </div>
                    </div>

                    <div
                      v-if="
                        explanationTarget?.groupId === group.id &&
                        explanationTarget.accountId === item.account.id
                      "
                      :data-test="`explanation-panel-${group.id}-${item.account.id}`"
                      class="mt-4 flex flex-col gap-3 rounded-lg border border-border bg-sunken p-4"
                    >
                      <div class="flex flex-wrap items-center justify-between gap-3">
                        <h5 class="text-sm font-semibold text-ink">
                          {{
                            t('manage.policy.rule.explanationTitle', {
                              name: explanation
                                ? accountName(explanation.account)
                                : accountName(item.account),
                            })
                          }}
                        </h5>
                        <Button
                          type="button"
                          variant="ghost"
                          size="icon-sm"
                          :aria-label="t('manage.policy.rule.closeExplanation')"
                          @click="closeExplanation"
                        >
                          <X class="size-4" aria-hidden="true" />
                        </Button>
                      </div>

                      <ErrorPanel
                        v-if="explanationApi.error.value"
                        :error="explanationApi.error.value"
                        :is-admin="mode === 'admin'"
                        @dismiss="explanationApi.clear"
                        @recovery="reloadExplanation"
                      />
                      <p
                        v-if="explanationApi.busy.value"
                        role="status"
                        aria-live="polite"
                        class="text-sm text-muted"
                      >
                        {{ t('common.loading') }}
                      </p>
                      <ul
                        v-else-if="explanationRows.length > 0"
                        role="tree"
                        class="flex flex-col gap-2"
                      >
                        <li
                          v-for="row in explanationRows"
                          :key="row.key"
                          role="treeitem"
                          :aria-level="row.depth + 1"
                          :data-test="`explanation-node-${row.key}`"
                          :class="explanationIndent(row.depth)"
                        >
                          <div class="flex flex-col gap-2 rounded-md border border-border bg-surface px-3 py-2 sm:flex-row sm:items-center sm:justify-between">
                            <div class="min-w-0">
                              <p class="break-words text-sm font-medium text-ink">
                                {{ row.node.label }}
                              </p>
                              <p class="break-all font-mono text-xs text-muted">
                                {{ row.node.path }}
                              </p>
                            </div>
                            <StatusBadge :variant="row.node.result ? 'success' : 'neutral'">
                              {{
                                row.node.result
                                  ? t('manage.policy.rule.matches')
                                  : t('manage.policy.rule.doesNotMatch')
                              }}
                            </StatusBadge>
                          </div>
                        </li>
                      </ul>
                    </div>
                  </li>
                </ul>

                <PaginationControls
                  :page-index="previewPageIndex"
                  :has-more="previewHasMore"
                  :busy="previewApi.busy.value"
                  :has-items="previewItems.length > 0"
                  @next="nextPreviewPage"
                  @previous="previousPreviewPage"
                />
              </div>
            </li>
          </ul>
        </CardContent>
      </Card>
    </template>

    <ConfirmDialog
      :open="confirmEmptyRestriction"
      :title="t('manage.policy.workspace.enableEmptyTitle')"
      :confirm-label="t('manage.policy.workspace.enableEmptyConfirm')"
      :busy="policyMutationApi.busy.value"
      @update:open="confirmEmptyRestriction = $event"
      @cancel="confirmEmptyRestriction = false"
      @confirm="confirmEnableEmptyRestriction"
    >
      {{ t('manage.policy.workspace.enableEmptyBody') }}
    </ConfirmDialog>

    <ConfirmDialog
      :open="confirmDiscardRuleEditor"
      :title="t('manage.policy.rule.discardTitle')"
      :confirm-label="t('manage.policy.rule.discard')"
      @update:open="confirmDiscardRuleEditor = $event"
      @cancel="cancelDiscardRuleEditor"
      @confirm="discardRuleEditorAndContinue"
    >
      {{ t('manage.policy.rule.discardBody') }}
    </ConfirmDialog>

    <ConfirmDialog
      :open="confirmDeleteRuleId !== null"
      :title="t('manage.policy.rule.deleteTitle', { name: ruleToDelete?.displayName ?? '' })"
      :confirm-label="t('manage.policy.rule.deleteConfirm')"
      :busy="policyMutationApi.busy.value"
      @update:open="confirmDeleteRuleId = $event ? confirmDeleteRuleId : null"
      @cancel="confirmDeleteRuleId = null"
      @confirm="deleteRuleGroup"
    >
      {{ t('manage.policy.rule.deleteBody') }}
    </ConfirmDialog>
  </section>
</template>
