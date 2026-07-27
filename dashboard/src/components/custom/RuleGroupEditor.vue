<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ChevronDown, MoreHorizontal, Plus } from 'lucide-vue-next'
import type { Condition, ProviderDescriptor, Rule } from '@/lib/appAccess'
import {
  conditionAtPath,
  setGroupMode,
  type GroupMode,
  type RulePath,
  type RuleValidationIssue,
} from '@/lib/ruleDraft'
import RulePredicateRow from './RulePredicateRow.vue'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'

const props = withDefaults(defineProps<{
  rule: Rule
  path: RulePath
  providers: ProviderDescriptor[]
  issues: RuleValidationIssue[]
  maxDepth: number
  maxNodes: number
  maxChildren: number
  canMoveUp?: boolean
  canMoveDown?: boolean
  canRemove?: boolean
}>(), {
  canMoveUp: false,
  canMoveDown: false,
  canRemove: false,
})
const emit = defineEmits<{
  'update:rule': [Rule]
  'add-predicate': [RulePath]
  'add-group': [RulePath]
  'move': [RulePath, -1 | 1]
  'remove': [RulePath]
  'actions-closed': [Event]
}>()

const { t } = useI18n()
const actionsTrigger = ref<InstanceType<typeof Button> | null>(null)
const pathKey = computed(() => props.path.length ? `root-${props.path.join('-')}` : 'root')
const jsonPath = computed(() => props.path.reduce<string>(
  (path, segment) => segment === 'child' ? `${path}.child` : `${path}.children[${segment}]`,
  '$.condition',
))
const node = computed(() => conditionAtPath(props.rule, props.path))
const mode = computed<GroupMode | undefined>(() =>
  node.value?.op === 'all' || node.value?.op === 'any' ? node.value.op : undefined,
)
const children = computed(() => mode.value && Array.isArray(node.value?.children) ? node.value.children : [])
const depth = computed(() => props.path.length + 1)
const nodes = computed(() => countNodes(props.rule.condition))
const atChildLimit = computed(() => children.value.length >= props.maxChildren)
const atNodeLimitForPredicate = computed(() => nodes.value + 1 > props.maxNodes)
const atNodeLimitForGroup = computed(() => nodes.value + 2 > props.maxNodes)
const atDepthLimitForPredicate = computed(() => depth.value + 1 > props.maxDepth)
const atDepthLimitForGroup = computed(() => depth.value + 2 > props.maxDepth)
const predicateDisabledReason = computed(() => {
  if (atChildLimit.value) return t('manage.policy.rule.limitChildren', { max: props.maxChildren })
  if (atNodeLimitForPredicate.value) return t('manage.policy.rule.limitNodes', { max: props.maxNodes })
  if (atDepthLimitForPredicate.value) return t('manage.policy.rule.limitDepth', { max: props.maxDepth })
  return ''
})
const groupDisabledReason = computed(() => {
  if (atChildLimit.value) return t('manage.policy.rule.limitChildren', { max: props.maxChildren })
  if (atNodeLimitForGroup.value) return t('manage.policy.rule.limitNodes', { max: props.maxNodes })
  if (atDepthLimitForGroup.value) return t('manage.policy.rule.limitDepth', { max: props.maxDepth })
  return ''
})
const nodeIssue = computed(() => issueForJSONPath(jsonPath.value))
const modeLabel = computed(() => mode.value === 'any'
  ? t('manage.policy.rule.operatorAny')
  : t('manage.policy.rule.operatorAll'))

function countNodes(condition: Condition): number {
  if (condition.op === 'all' || condition.op === 'any') {
    return 1 + (condition.children ?? []).reduce((count, child) => count + countNodes(child), 0)
  }
  if (condition.op === 'not') return 1 + (condition.child ? countNodes(condition.child) : 0)
  return 1
}

function issueForJSONPath(path: string): RuleValidationIssue | undefined {
  return props.issues.find((issue) => issue.path === path || issue.path.startsWith(`${path}.`))
}

function childJSONPath(index: number): string {
  return `${jsonPath.value}.children[${index}]`
}

function childPath(index: number): RulePath {
  return [...props.path, index]
}

function childPathKey(index: number): string {
  return `root-${[...props.path, index].join('-')}`
}

function isGroup(condition: Condition): boolean {
  return (condition.op === 'all' || condition.op === 'any') && Array.isArray(condition.children)
}

function isPredicate(condition: Condition): boolean {
  if (condition.op === undefined) return condition.children === undefined && condition.child === undefined
  return condition.op === 'not' && condition.child !== undefined && condition.child.op === undefined
}

function changeMode(nextMode: GroupMode): void {
  emit('update:rule', setGroupMode(props.rule, props.path, nextMode))
}
function closeActions(event: Event): void {
  emit('actions-closed', event)
  if (!event.defaultPrevented) actionsTrigger.value?.$el?.focus()
}

</script>

<template>
  <div
    v-if="mode"
    role="group"
    :aria-label="`${modeLabel}: ${mode === 'all' ? t('manage.policy.rule.allDescription') : t('manage.policy.rule.anyDescription')}`"
    data-rule-group
    :data-test="`rule-group-${pathKey}`"
    class="relative min-w-0 pl-5"
  >
    <span
      data-group-rail
      aria-hidden="true"
      class="absolute top-[30px] bottom-[5px] left-0 w-px bg-border-strong"
    />

    <div
      class="grid min-w-0 grid-cols-[auto_minmax(0,1fr)_auto] items-start gap-2 py-1"
      :data-test="`group-header-${pathKey}`"
    >
        <DropdownMenu>
          <DropdownMenuTrigger as-child>
            <Button
              type="button"
              variant="outline"
              size="sm"
              class="-ml-5 inline-flex h-8 w-auto shrink-0 gap-1 border-border-strong bg-surface px-2.5 text-xs font-bold tracking-wide text-primary shadow-none"
              :aria-label="t('manage.policy.rule.groupModeLabel')"
              :data-test="`group-mode-${pathKey}`"
            >
              {{ modeLabel }}
              <ChevronDown class="size-3.5 text-muted" aria-hidden="true" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start">
            <DropdownMenuItem
              :data-test="`group-mode-all-${pathKey}`"
              @select="changeMode('all')"
            >
              {{ t('manage.policy.rule.operatorAll') }}
            </DropdownMenuItem>
            <DropdownMenuItem
              :data-test="`group-mode-any-${pathKey}`"
              @select="changeMode('any')"
            >
              {{ t('manage.policy.rule.operatorAny') }}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
        <p
          class="min-w-0 max-w-prose pt-1.5 text-sm leading-relaxed text-muted"
          :data-test="`group-description-${pathKey}`"
        >
          {{ mode === 'all' ? t('manage.policy.rule.allDescription') : t('manage.policy.rule.anyDescription') }}
        </p>

      <DropdownMenu v-if="canMoveUp || canMoveDown || canRemove">
        <DropdownMenuTrigger as-child>
          <Button
            ref="actionsTrigger"
            type="button"
            variant="ghost"
            size="icon-sm"
            class="shrink-0 text-muted hover:text-ink"
            :aria-label="t('manage.policy.rule.groupActions', { mode: modeLabel })"
            :title="t('manage.policy.rule.groupActions', { mode: modeLabel })"
            :data-test="`group-actions-${pathKey}`"
          >
            <MoreHorizontal class="size-4" aria-hidden="true" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" @close-auto-focus="closeActions">
          <DropdownMenuItem
            v-if="canMoveUp"
            :data-test="`group-move-up-${pathKey}`"
            @select="emit('move', path, -1)"
          >
            {{ t('manage.policy.rule.moveUp') }}
          </DropdownMenuItem>
          <DropdownMenuItem
            v-if="canMoveDown"
            :data-test="`group-move-down-${pathKey}`"
            @select="emit('move', path, 1)"
          >
            {{ t('manage.policy.rule.moveDown') }}
          </DropdownMenuItem>
          <DropdownMenuItem
            v-if="canRemove"
            class="text-destructive focus:text-destructive"
            :aria-label="t('manage.policy.rule.removeGroupLabel', { mode: modeLabel })"
            :data-test="`group-remove-${pathKey}`"
            @select="emit('remove', path)"
          >
            {{ t('manage.policy.rule.removeCondition') }}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>

    <div v-if="children.length" class="min-w-0">
      <template v-for="(child, index) in children" :key="childPathKey(index)">
        <div
          v-if="index > 0"
          class="flex h-6 items-center gap-2 text-xs font-medium text-muted"
          :data-test="`connector-${childPathKey(index)}`"
        >
          <span class="h-px w-4 bg-border" aria-hidden="true" />
          {{ mode === 'all' ? t('manage.policy.rule.connectorAnd') : t('manage.policy.rule.connectorOr') }}
        </div>

        <RuleGroupEditor
          v-if="isGroup(child)"
          :rule="rule"
          :path="childPath(index)"
          :providers="providers"
          :issues="issues"
          :max-depth="maxDepth"
          :max-nodes="maxNodes"
          :max-children="maxChildren"
          :can-move-up="index > 0"
          :can-move-down="index < children.length - 1"
          can-remove
          @update:rule="emit('update:rule', $event)"
          @add-predicate="emit('add-predicate', $event)"
          @add-group="emit('add-group', $event)"
          @move="(path, delta) => emit('move', path, delta)"
          @remove="emit('remove', $event)"
          @actions-closed="emit('actions-closed', $event)"
        />

        <RulePredicateRow
          v-else-if="isPredicate(child)"
          :rule="rule"
          :path="childPath(index)"
          :providers="providers"
          :issues="issues"
          :max-depth="maxDepth"
          :max-nodes="maxNodes"
          :position="index + 1"
          :sibling-count="children.length"
          :parent-mode="mode"
          :can-move-up="index > 0"
          :can-move-down="index < children.length - 1"
          can-remove
          @update:rule="emit('update:rule', $event)"
          @move="(path, delta) => emit('move', path, delta)"
          @remove="emit('remove', $event)"
          @actions-closed="emit('actions-closed', $event)"
        />

        <p
          v-else
          role="alert"
          class="py-3 text-sm text-rose-700"
          :data-test="`invalid-node-${childPathKey(index)}`"
        >
          {{ issueForJSONPath(childJSONPath(index)) ? t(issueForJSONPath(childJSONPath(index))!.messageKey) : t('manage.policy.rule.validation.invalid_shape') }}
        </p>
      </template>
    </div>

    <p
      v-else-if="nodeIssue"
      role="alert"
      class="py-3 text-sm text-rose-700"
      :data-test="`group-error-${pathKey}`"
    >
      {{ t(nodeIssue.messageKey) }}
    </p>

    <div class="mt-2 flex flex-wrap items-center gap-2 border-t border-border pt-3">
      <TooltipProvider>
        <Tooltip>
          <TooltipTrigger as-child>
            <span class="inline-flex">
              <Button
                type="button"
                variant="ghost"
                size="sm"
                class="shadow-none"
                :disabled="Boolean(predicateDisabledReason)"
                :aria-label="t('manage.policy.rule.addCondition')"
                :aria-describedby="predicateDisabledReason ? `add-condition-reason-${pathKey}` : undefined"
                :data-test="`group-add-condition-${pathKey}`"
                @click="emit('add-predicate', path)"
              >
                <Plus class="size-4" aria-hidden="true" />
                {{ t('manage.policy.rule.addCondition') }}
              </Button>
            </span>
          </TooltipTrigger>
          <TooltipContent v-if="predicateDisabledReason">{{ predicateDisabledReason }}</TooltipContent>
        </Tooltip>

        <Tooltip>
          <TooltipTrigger as-child>
            <span class="inline-flex">
              <Button
                type="button"
                variant="ghost"
                size="sm"
                class="shadow-none"
                :disabled="Boolean(groupDisabledReason)"
                :aria-label="t('manage.policy.rule.addNestedGroup')"
                :aria-describedby="groupDisabledReason ? `add-group-reason-${pathKey}` : undefined"
                :data-test="`group-add-group-${pathKey}`"
                @click="emit('add-group', path)"
              >
                <Plus class="size-4" aria-hidden="true" />
                {{ t('manage.policy.rule.addNestedGroup') }}
              </Button>
            </span>
          </TooltipTrigger>
          <TooltipContent v-if="groupDisabledReason">{{ groupDisabledReason }}</TooltipContent>
        </Tooltip>
      </TooltipProvider>

      <span v-if="predicateDisabledReason" :id="`add-condition-reason-${pathKey}`" class="sr-only">
        {{ predicateDisabledReason }}
      </span>
      <span v-if="groupDisabledReason" :id="`add-group-reason-${pathKey}`" class="sr-only">
        {{ groupDisabledReason }}
      </span>
    </div>
  </div>

  <p
    v-else
    role="alert"
    class="py-3 text-sm text-rose-700"
    :data-test="`invalid-node-${pathKey}`"
  >
    {{ nodeIssue ? t(nodeIssue.messageKey) : t('manage.policy.rule.validation.invalid_shape') }}
  </p>
</template>
