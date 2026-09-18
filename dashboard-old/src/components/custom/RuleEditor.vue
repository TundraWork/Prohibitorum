<script lang="ts">
import type { Rule as DraftRule } from '@/lib/appAccess'

export interface RuleEditorDraft {
  slug: string
  displayName: string
  description: string
  exposedToDownstream: boolean
  rule: DraftRule
}
</script>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, useId, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ChevronDown } from 'lucide-vue-next'
import type { ProviderDescriptor } from '@/lib/appAccess'
import type { ApiError } from '@/lib/api'
import {
  cloneRule,
  formatRuleJSON,
  parseRuleJSON,
  ruleExpression,
  slugFromDisplayName,
  validateRule,
  type InvalidRuleJSON,
} from '@/lib/ruleDraft'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import SegmentedControl from './SegmentedControl.vue'
import RuleVisualBuilder from './RuleVisualBuilder.vue'
import RuleJsonEditor from './RuleJsonEditor.vue'
import RuleMeaning from './RuleMeaning.vue'
import RuleImpactPreview from './RuleImpactPreview.vue'
import StatusMessage from './StatusMessage.vue'

const props = defineProps<{
  initialDraft: RuleEditorDraft
  providers: ProviderDescriptor[]
  previewEndpoint: string
  busy: boolean
  serverError?: ApiError
  mode: 'create' | 'edit'
}>()

const emit = defineEmits<{
  save: [RuleEditorDraft]
  cancel: []
  'dirty-change': [boolean]
}>()

const { t } = useI18n()
const advancedId = `rule-editor-advanced-${useId()}`
const initial = closedDraft(props.initialDraft)
const baseline = ref(editorSnapshot(initial, 'visual', formatRuleJSON(initial.rule)))
const draft = ref<RuleEditorDraft>(closedDraft(initial))
const lastValidRule = ref(cloneRule(initial.rule))
const editorMode = ref<'visual' | 'json'>('visual')
const jsonSource = ref(formatRuleJSON(initial.rule))
const jsonError = ref<InvalidRuleJSON>()
const advancedOpen = ref(false)
const reviewing = ref(false)
const dirtyDialogOpen = ref(false)
const previewConfirmOpen = ref(false)
const previewState = ref<'idle' | 'loading' | 'current' | 'stale' | 'error'>('idle')
const slugEdited = ref(props.mode === 'edit' || initial.slug !== '')
const saveRequested = ref(false)
const saveBecameBusy = ref(false)
const saved = ref(false)
const visualBuilderKey = ref(0)
const jsonTextarea = ref<HTMLElement>()
const keepEditingButton = ref<{ $el?: HTMLElement }>()
const cancelSaveButton = ref<{ $el?: HTMLElement }>()
const saveStatus = ref<{ $el?: HTMLElement }>()

const providerSlugs = computed(() => new Set(props.providers.map((provider) => provider.slug)))
const issues = computed(() => validateRule(draft.value.rule, providerSlugs.value))
const valid = computed(() => (
  draft.value.displayName.trim() !== '' &&
  draft.value.slug.trim() !== '' &&
  issues.value.length === 0 &&
  jsonError.value === undefined
))
const dirty = computed(() => JSON.stringify(editorSnapshot(draft.value, editorMode.value, jsonSource.value)) !== JSON.stringify(baseline.value))
const title = computed(() => props.mode === 'create'
  ? t('manage.policy.rule.createTitle')
  : t('manage.policy.rule.editTitle', { name: initial.displayName }))
const modes = computed(() => [
  { value: 'visual', label: t('manage.policy.rule.visualMode') },
  { value: 'json', label: t('manage.policy.rule.jsonMode') },
])
const serverErrorText = computed(() => {
  if (!props.serverError) return ''
  const key = `errors.codes.${props.serverError.code}`
  return t(key, props.serverError.code)
})

function editorSnapshot(source: RuleEditorDraft, mode: 'visual' | 'json', json: string) {
  return { draft: closedDraft(source), editorMode: mode, jsonSource: json }
}

function closedDraft(source: RuleEditorDraft): RuleEditorDraft {
  return {
    slug: source.slug,
    displayName: source.displayName,
    description: source.description,
    exposedToDownstream: source.exposedToDownstream,
    rule: cloneRule(source.rule),
  }
}

function updateDisplayName(event: Event): void {
  const value = (event.target as HTMLInputElement).value
  draft.value.displayName = value
  if (!slugEdited.value) draft.value.slug = slugFromDisplayName(value)
}

function updateSlug(event: Event): void {
  slugEdited.value = true
  draft.value.slug = (event.target as HTMLInputElement).value
}

function updateRule(rule: DraftRule): void {
  const next = cloneRule(rule)
  draft.value.rule = next
  lastValidRule.value = next
  jsonSource.value = formatRuleJSON(next)
  jsonError.value = undefined
}

function updateJSON(source: string): void {
  jsonSource.value = source
  const parsed = parseRuleJSON(source, providerSlugs.value)
  if (!parsed.ok) {
    jsonError.value = parsed
    return
  }
  jsonError.value = undefined
  const next = cloneRule(parsed.rule)
  if (JSON.stringify(next) !== JSON.stringify(draft.value.rule)) visualBuilderKey.value += 1
  lastValidRule.value = next
  draft.value.rule = next
}

async function changeMode(value: string): Promise<void> {
  if (value !== 'visual' && value !== 'json') return
  if (value === 'visual') {
    const parsed = parseRuleJSON(jsonSource.value, providerSlugs.value)
    if (!parsed.ok) {
      jsonError.value = parsed
      await nextTick()
      document.querySelector<HTMLElement>('[data-test="rule-json-source"]')?.focus()
      return
    }
    updateRule(parsed.rule)
  } else {
    jsonSource.value = formatRuleJSON(lastValidRule.value)
    jsonError.value = undefined
  }
  editorMode.value = value
}

function updatePreviewState(state: 'idle' | 'loading' | 'current' | 'stale' | 'error'): void {
  previewState.value = state
}

function formatJSON(): void {
  jsonSource.value = formatRuleJSON(lastValidRule.value)
  jsonError.value = undefined
}

async function copy(value: string): Promise<void> {
  try {
    await navigator.clipboard?.writeText(value)
  } catch {
    // The source remains selected and readable when clipboard permission is denied.
  }
}

function startReview(): void {
  if (!valid.value || jsonError.value) return
  previewState.value = 'idle'
  reviewing.value = true
  saved.value = false
}

function requestSave(): void {
  if (!reviewing.value || props.busy) return
  if (previewState.value === 'error') {
    previewConfirmOpen.value = true
    return
  }
  if (previewState.value !== 'current') return
  emitSave()
}

function emitSave(): void {
  previewConfirmOpen.value = false
  saveRequested.value = true
  saveBecameBusy.value = props.busy
  saved.value = false
  emit('save', closedDraft(draft.value))
}

function requestCancel(): void {
  if (!dirty.value) {
    emit('cancel')
    return
  }
  dirtyDialogOpen.value = true
}

function discardDraft(): void {
  dirtyDialogOpen.value = false
  emit('cancel')
}

function beforeUnload(event: BeforeUnloadEvent): void {
  event.preventDefault()
  event.returnValue = ''
}

watch(dirty, async (isDirty) => {
  emit('dirty-change', isDirty)
  if (isDirty) window.addEventListener('beforeunload', beforeUnload)
  else window.removeEventListener('beforeunload', beforeUnload)
})

watch(dirtyDialogOpen, async (open) => {
  if (!open) return
  await nextTick()
  keepEditingButton.value?.$el?.focus()
})

watch(previewConfirmOpen, async (open) => {
  if (!open) return
  await nextTick()
  cancelSaveButton.value?.$el?.focus()
})

watch(() => props.busy, async (isBusy, wasBusy) => {
  if (!saveRequested.value) return
  if (isBusy) {
    saveBecameBusy.value = true
    return
  }
  if (!wasBusy || !saveBecameBusy.value || props.serverError) return
  saveRequested.value = false
  saveBecameBusy.value = false
  saved.value = true
  baseline.value = editorSnapshot(draft.value, editorMode.value, jsonSource.value)
  await nextTick()
  saveStatus.value?.$el?.focus()
})

watch(() => props.serverError, (error) => {
  if (!error) return
  saveRequested.value = false
  saveBecameBusy.value = false
  saved.value = false
})

onBeforeUnmount(() => window.removeEventListener('beforeunload', beforeUnload))
</script>

<template>
  <section class="rounded-lg border border-border bg-surface" data-test="rule-editor">
    <header class="border-b border-border px-4 py-4 sm:px-6">
      <h2 class="text-lg font-semibold text-ink">{{ title }}</h2>
      <p class="mt-1 max-w-2xl text-sm text-muted">{{ t('manage.policy.rule.editorDescription') }}</p>
    </header>

    <StatusMessage
      ref="saveStatus"
      :show="saved"
      tabindex="-1"
      class="mx-4 mt-4 rounded-md bg-sage-50 px-3 py-2 font-medium outline-none focus-visible:ring-2 focus-visible:ring-ring sm:mx-6"
      data-test="rule-save-status"
    >
      {{ t('manage.policy.rule.saved') }}
    </StatusMessage>

    <div v-if="serverError" class="mx-4 mt-4 rounded-md border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700 sm:mx-6" role="alert" data-test="rule-server-error">
      <p class="font-medium">{{ serverErrorText }}</p>
      <p class="mt-1">{{ t('manage.policy.rule.serverErrorHint') }}</p>
    </div>

    <template v-if="!reviewing">
      <div class="space-y-5 px-4 py-5 sm:px-6 sm:py-6">
        <div class="max-w-xl space-y-2">
          <Label for="rule-display-name">{{ t('manage.policy.workspace.groupDisplayName') }}</Label>
          <Input
            id="rule-display-name"
            :model-value="draft.displayName"
            data-test="rule-display-name"
            autocomplete="off"
            @input="updateDisplayName"
          />
        </div>

        <SegmentedControl
          :model-value="editorMode"
          :options="modes"
          :aria-label="t('manage.policy.rule.editorMode')"
          class="max-w-xs"
          @update:model-value="changeMode"
        />

        <div class="flex min-w-0 flex-col gap-5" data-test="rule-editor-layout">
          <div class="min-w-0 max-w-[790px]" data-test="editor-builder-column">
            <RuleVisualBuilder
              v-show="editorMode === 'visual'"
              :key="visualBuilderKey"
              :model-value="draft.rule"
              :providers="providers"
              :issues="issues"
              :max-depth="8"
              :max-nodes="64"
              :max-children="32"
              @update:model-value="updateRule"
            />
            <RuleJsonEditor
              v-show="editorMode === 'json'"
              ref="jsonTextarea"
              :source="jsonSource"
              :parsed-rule="lastValidRule"
              :error="jsonError"
              :expression="ruleExpression(lastValidRule)"
              @update:source="updateJSON"
              @format="formatJSON"
              @copy-json="copy(jsonSource)"
              @copy-expression="copy(ruleExpression(lastValidRule))"
            />
          </div>

          <section
            class="max-w-[790px] rounded-[10px] border border-border bg-surface px-[17px] py-[15px]"
            aria-labelledby="rule-meaning-heading"
            data-test="editor-meaning-strip"
          >
            <h3 id="rule-meaning-heading" class="text-sm font-semibold text-ink">
              {{ t('manage.policy.rule.plainMeaning') }}
            </h3>
            <RuleMeaning class="mt-2" :rule="lastValidRule" :providers="providers" />
          </section>
        </div>

        <div class="max-w-2xl space-y-2">
          <Label for="rule-description">{{ t('manage.policy.workspace.groupDescription') }}</Label>
          <Textarea id="rule-description" v-model="draft.description" data-test="rule-description" />
        </div>

        <div class="border-t border-border pt-4">
          <Button
            type="button"
            variant="ghost"
            class="px-0 shadow-none hover:bg-transparent"
            :aria-expanded="advancedOpen"
            :aria-controls="advancedId"
            data-test="advanced-toggle"
            @click="advancedOpen = !advancedOpen"
          >
            {{ t('manage.policy.rule.advancedOptions') }}
            <ChevronDown class="size-4 transition-transform" :class="advancedOpen ? 'rotate-180' : ''" aria-hidden="true" />
          </Button>
          <div v-if="advancedOpen" :id="advancedId" class="mt-4 max-w-2xl space-y-5">
            <div class="space-y-2">
              <Label for="rule-slug">{{ t('manage.policy.workspace.groupSlug') }}</Label>
              <Input id="rule-slug" :model-value="draft.slug" data-test="rule-slug" autocomplete="off" @input="updateSlug" />
              <p class="text-sm text-muted">{{ t('manage.policy.rule.slugHint') }}</p>
            </div>
            <div class="flex items-start justify-between gap-4">
              <div>
                <Label for="rule-exposed">{{ t('manage.policy.rule.exposed') }}</Label>
                <p class="mt-1 max-w-xl text-sm text-muted">{{ t('manage.policy.rule.exposedHint') }}</p>
              </div>
              <Switch id="rule-exposed" v-model="draft.exposedToDownstream" data-test="rule-exposed" />
            </div>
          </div>
        </div>
      </div>

      <footer class="flex flex-col-reverse gap-2 border-t border-border px-4 py-4 sm:flex-row sm:justify-end sm:px-6">
        <Button type="button" variant="ghost" :disabled="busy" data-test="cancel-rule" @click="requestCancel">
          {{ t('common.cancel') }}
        </Button>
        <Button type="button" :disabled="!valid || busy" data-test="review-rule" @click="startReview">
          {{ t('manage.policy.rule.reviewAndSave') }}
        </Button>
      </footer>
    </template>

    <template v-else>
      <div class="space-y-6 px-4 py-5 sm:px-6 sm:py-6" data-test="rule-review">
        <div>
          <p class="text-sm font-medium text-muted">{{ t('manage.policy.rule.reviewName') }}</p>
          <p class="mt-1 text-lg font-semibold text-ink">{{ draft.displayName }}</p>
        </div>
        <section class="space-y-2" aria-labelledby="review-meaning-heading">
          <h3 id="review-meaning-heading" class="text-sm font-semibold text-ink">{{ t('manage.policy.rule.plainMeaning') }}</h3>
          <RuleMeaning :rule="draft.rule" :providers="providers" />
        </section>
        <RuleImpactPreview
          :rule="draft.rule"
          :providers="providers"
          :draft-valid="true"
          :endpoint="previewEndpoint"
          @state-change="updatePreviewState"
        />
        <p class="text-sm text-ink">{{ t('manage.policy.rule.accessConsequence') }}</p>
        <p v-if="draft.exposedToDownstream" class="text-sm text-ink">{{ t('manage.policy.rule.claimConsequence', { slug: draft.slug }) }}</p>
      </div>
      <footer class="flex flex-col-reverse gap-2 border-t border-border px-4 py-4 sm:flex-row sm:justify-between sm:px-6">
        <Button type="button" variant="ghost" :disabled="busy" @click="reviewing = false">{{ t('manage.policy.rule.backToEditing') }}</Button>
        <div class="flex flex-col-reverse gap-2 sm:flex-row">
          <Button type="button" variant="ghost" :disabled="busy" data-test="cancel-rule" @click="requestCancel">{{ t('common.cancel') }}</Button>
          <Button type="button" :disabled="busy || (previewState !== 'current' && previewState !== 'error')" :aria-busy="busy ? 'true' : undefined" data-test="save-rule" @click="requestSave">
            {{ busy ? t('manage.policy.rule.saving') : t('common.save') }}
          </Button>
        </div>
      </footer>
    </template>

    <Dialog :open="dirtyDialogOpen" @update:open="dirtyDialogOpen = $event">
      <DialogContent data-test="dirty-dialog">
        <DialogHeader>
          <DialogTitle>{{ t('manage.policy.rule.discardTitle') }}</DialogTitle>
          <DialogDescription>{{ t('manage.policy.rule.discardBody') }}</DialogDescription>
        </DialogHeader>
        <DialogFooter class="gap-2">
          <Button ref="keepEditingButton" type="button" variant="ghost" data-test="keep-editing" @click="dirtyDialogOpen = false">
            {{ t('manage.policy.rule.keepEditing') }}
          </Button>
          <Button type="button" variant="destructive" data-test="discard-draft" @click="discardDraft">
            {{ t('manage.policy.rule.discard') }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <Dialog :open="previewConfirmOpen" @update:open="previewConfirmOpen = $event">
      <DialogContent data-test="save-without-preview-dialog">
        <DialogHeader>
          <DialogTitle>{{ t('manage.policy.rule.saveWithoutPreviewTitle') }}</DialogTitle>
          <DialogDescription>{{ t('manage.policy.rule.saveWithoutPreviewBody') }}</DialogDescription>
        </DialogHeader>
        <DialogFooter class="gap-2">
          <Button ref="cancelSaveButton" type="button" variant="ghost" @click="previewConfirmOpen = false">
            {{ t('manage.policy.rule.keepReviewing') }}
          </Button>
          <Button type="button" data-test="confirm-save-without-preview" @click="emitSave">
            {{ t('manage.policy.rule.saveWithoutPreview') }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </section>
</template>
