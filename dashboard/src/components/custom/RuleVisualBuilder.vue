<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Plus, Undo2 } from 'lucide-vue-next'
import type { Condition, ProviderDescriptor, Rule } from '@/lib/appAccess'
import {
  addNestedGroup,
  addPredicate,
  conditionAtPath,
  moveNode,
  removeNode,
  restoreNode,
  type RulePath,
  type RuleValidationIssue,
} from '@/lib/ruleDraft'
import RuleGroupEditor from './RuleGroupEditor.vue'
import RulePredicateRow from './RulePredicateRow.vue'
import { Button } from '@/components/ui/button'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'

interface RemovedNode {
  parent: RulePath
  index: number
  condition: Condition
  focusPath: RulePath
}

const props = defineProps<{
  modelValue: Rule
  providers: ProviderDescriptor[]
  issues: RuleValidationIssue[]
  maxDepth: number
  maxNodes: number
  maxChildren: number
}>()

const emit = defineEmits<{
  'update:modelValue': [Rule]
  'announce': [string]
}>()
const pendingFocus = ref<{ path: RulePath; rule: Rule } | null>(null)

const { t } = useI18n()
const liveMessage = ref('')
const removedNode = ref<RemovedNode | null>(null)
const root = computed(() => props.modelValue.condition)
const rootIsGroup = computed(() =>
  (root.value.op === 'all' || root.value.op === 'any') && Array.isArray(root.value.children),
)
const rootIsPredicate = computed(() => {
  if (root.value.op === undefined) return root.value.children === undefined && root.value.child === undefined
  return root.value.op === 'not' && root.value.child !== undefined && root.value.child.op === undefined
})
const nodeCount = computed(() => countNodes(root.value))
const rootDepth = computed(() => conditionDepth(root.value))
const rootAddDisabledReason = computed(() => {
  if (nodeCount.value + 2 > props.maxNodes) {
    return t('manage.policy.rule.limitNodes', { max: props.maxNodes })
  }
  if (rootDepth.value + 1 > props.maxDepth) {
    return t('manage.policy.rule.limitDepth', { max: props.maxDepth })
  }
  if (props.maxChildren < 2) {
    return t('manage.policy.rule.limitChildren', { max: props.maxChildren })
  }
  return ''
})
const rootIssue = computed(() => props.issues.find((issue) => issue.path.startsWith('$.condition')))

function countNodes(condition: Condition): number {
  if (condition.op === 'all' || condition.op === 'any') {
    return 1 + (condition.children ?? []).reduce((count, child) => count + countNodes(child), 0)
  }
  if (condition.op === 'not') return 1 + (condition.child ? countNodes(condition.child) : 0)
  return 1
}

function conditionDepth(condition: Condition): number {
  if (condition.op === 'all' || condition.op === 'any') {
    return 1 + Math.max(0, ...(condition.children ?? []).map(conditionDepth))
  }
  if (condition.op === 'not') return 1 + (condition.child ? conditionDepth(condition.child) : 0)
  return 1
}

function isGroup(condition: Condition | undefined): boolean {
  return condition?.op === 'all' || condition?.op === 'any'
}

function pathKey(path: RulePath): string {
  return path.length ? `root-${path.join('-')}` : 'root'
}

function announce(message: string): void {
  liveMessage.value = message
  emit('announce', message)
}

async function focusNode(path: RulePath, rule: Rule): Promise<void> {
  await nextTick()
  await nextTick()
  const target = conditionAtPath(rule, path)
  const key = pathKey(path)
  const selector = isGroup(target)
    ? `[data-test="group-mode-${target?.op}-${key}"]`
    : `[data-test="predicate-fact-${key}"]`
  document.querySelector<HTMLElement>(selector)?.focus()
}

function applyRule(rule: Rule): void {
  emit('update:modelValue', rule)
}
async function completeActionFocus(event: Event): Promise<void> {
  if (!pendingFocus.value) return
  event.preventDefault()
  const pending = pendingFocus.value
  pendingFocus.value = null
  await focusNode(pending.path, pending.rule)
}


async function updateRule(rule: Rule): Promise<void> {
  removedNode.value = null
  applyRule(rule)
}

async function addCondition(parent: RulePath): Promise<void> {
  const nextRule = addPredicate(props.modelValue, parent)
  const target = conditionAtPath(nextRule, parent)
  const index = target?.op === 'all' || target?.op === 'any'
    ? Math.max(0, (target.children?.length ?? 1) - 1)
    : 1
  const focusPath = [...parent, index]
  removedNode.value = null
  applyRule(nextRule)
  announce(t('manage.policy.rule.conditionAdded'))
  await focusNode(focusPath, nextRule)
}

async function addGroup(parent: RulePath): Promise<void> {
  const nextRule = addNestedGroup(props.modelValue, parent)
  const target = conditionAtPath(nextRule, parent)
  const index = target?.op === 'all' || target?.op === 'any'
    ? Math.max(0, (target.children?.length ?? 1) - 1)
    : 1
  const focusPath = [...parent, index, 0]
  removedNode.value = null
  applyRule(nextRule)
  announce(t('manage.policy.rule.groupAdded'))
  await focusNode(focusPath, nextRule)
}

async function remove(path: RulePath): Promise<void> {
  const before = conditionAtPath(props.modelValue, path)
  if (!before || path.length === 0 || typeof path.at(-1) !== 'number') return
  const result = removeNode(props.modelValue, path)
  removedNode.value = {
    parent: path.slice(0, -1),
    index: path.at(-1) as number,
    condition: result.removed,
    focusPath: result.focusPath,
  }
  applyRule(result.rule)
  announce(t(isGroup(before) ? 'manage.policy.rule.groupRemoved' : 'manage.policy.rule.conditionRemoved'))
  pendingFocus.value = { path: result.focusPath, rule: result.rule }
}

async function move(path: RulePath, delta: -1 | 1): Promise<void> {
  const before = conditionAtPath(props.modelValue, path)
  const index = path.at(-1)
  if (!before || typeof index !== 'number') return
  const nextRule = moveNode(props.modelValue, path, delta)
  const focusPath = [...path.slice(0, -1), index + delta]
  removedNode.value = null
  applyRule(nextRule)
  const key = isGroup(before)
    ? delta < 0 ? 'manage.policy.rule.groupMovedUp' : 'manage.policy.rule.groupMovedDown'
    : delta < 0 ? 'manage.policy.rule.conditionMovedUp' : 'manage.policy.rule.conditionMovedDown'
  announce(t(key))
  pendingFocus.value = { path: focusPath, rule: nextRule }
}

async function undoRemoval(): Promise<void> {
  if (!removedNode.value) return
  const removal = removedNode.value
  const nextRule = restoreNode(
    props.modelValue,
    removal.parent,
    removal.index,
    removal.condition,
  )
  removedNode.value = null
  applyRule(nextRule)
  announce(t('manage.policy.rule.removalRestored'))
  await focusNode([...removal.parent, removal.index], nextRule)
}
</script>

<template>
  <section class="min-w-0" data-test="rule-visual-builder">
    <RuleGroupEditor
      v-if="rootIsGroup"
      :rule="modelValue"
      :path="[]"
      :providers="providers"
      :issues="issues"
      :max-depth="maxDepth"
      :max-nodes="maxNodes"
      :max-children="maxChildren"
      @update:rule="updateRule"
      @add-predicate="addCondition"
      @add-group="addGroup"
      @move="move"
      @remove="remove"
      @actions-closed="completeActionFocus"
    />

    <template v-else-if="rootIsPredicate">
      <RulePredicateRow
        :rule="modelValue"
        :path="[]"
        :providers="providers"
        :issues="issues"
        :max-depth="maxDepth"
        :max-nodes="maxNodes"
        @update:rule="updateRule"
      />

      <div class="mt-2 border-t border-border pt-3">
        <TooltipProvider>
          <Tooltip>
            <TooltipTrigger as-child>
              <span class="inline-flex">
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  class="shadow-none"
                  :disabled="Boolean(rootAddDisabledReason)"
                  :aria-describedby="rootAddDisabledReason ? 'root-add-condition-reason' : undefined"
                  data-test="root-add-condition"
                  @click="addCondition([])"
                >
                  <Plus class="size-4" aria-hidden="true" />
                  {{ t('manage.policy.rule.addCondition') }}
                </Button>
              </span>
            </TooltipTrigger>
            <TooltipContent v-if="rootAddDisabledReason">{{ rootAddDisabledReason }}</TooltipContent>
          </Tooltip>
        </TooltipProvider>
        <span v-if="rootAddDisabledReason" id="root-add-condition-reason" class="sr-only">
          {{ rootAddDisabledReason }}
        </span>
      </div>
    </template>

    <p
      v-else
      role="alert"
      class="py-3 text-sm text-rose-700"
      data-test="invalid-node-root"
    >
      {{ rootIssue ? t(rootIssue.messageKey) : t('manage.policy.rule.validation.invalid_shape') }}
    </p>

    <div v-if="removedNode" class="mt-4 border-t border-border pt-3">
      <Button
        type="button"
        variant="outline"
        size="sm"
        :aria-label="t('manage.policy.rule.undoRemovalLabel')"
        data-test="rule-builder-undo"
        @click="undoRemoval"
      >
        <Undo2 class="size-4" aria-hidden="true" />
        {{ t('manage.policy.rule.undo') }}
      </Button>
    </div>

    <p
      aria-live="polite"
      aria-atomic="true"
      class="sr-only"
      data-test="rule-builder-live"
    >
      {{ liveMessage }}
    </p>
  </section>
</template>
