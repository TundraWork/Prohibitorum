<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { MoreHorizontal } from 'lucide-vue-next'
import type { Condition, ProviderDescriptor, Rule } from '@/lib/appAccess'
import {
  setPredicateNegated,
  updatePredicateFact,
  updatePredicateValue,
  type RulePath,
  type RuleValidationIssue,
} from '@/lib/ruleDraft'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'

const FACTS = [
  'connection.provider',
  'connection.protocol',
  'login_method',
  'avatar',
] as const satisfies readonly NonNullable<Condition['fact']>[]

const props = withDefaults(defineProps<{
  rule: Rule
  path: RulePath
  providers: ProviderDescriptor[]
  issues: RuleValidationIssue[]
  maxDepth: number
  maxNodes: number
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
const node = computed(() => conditionAtPath(props.rule.condition, props.path))
const predicate = computed(() => node.value?.op === 'not' ? node.value.child : node.value)
const negated = computed(() => node.value?.op === 'not')
const fact = computed(() => predicate.value?.fact)
const value = computed(() => {
  switch (predicate.value?.fact) {
    case 'connection.provider': return predicate.value.provider
    case 'connection.protocol': return predicate.value.protocol
    case 'login_method': return predicate.value.method
    case 'avatar': return predicate.value.source
    default: return undefined
  }
})
const issue = computed(() => props.issues.find((candidate) =>
  candidate.path === jsonPath.value || candidate.path.startsWith(`${jsonPath.value}.`),
))
const errorId = computed(() => `predicate-error-${pathKey.value}`)
const issueDescription = computed(() => issue.value ? errorId.value : undefined)
const nodeCount = computed(() => countNodes(props.rule.condition))
const negativeDisabledReason = computed(() => {
  if (negated.value) return ''
  if (nodeCount.value + 1 > props.maxNodes) {
    return t('manage.policy.rule.limitNodes', { max: props.maxNodes })
  }
  if (props.path.length + 2 > props.maxDepth) {
    return t('manage.policy.rule.limitDepth', { max: props.maxDepth })
  }
  return ''
})
const contentLabel = computed(() => {
  if (!fact.value) return t('manage.policy.rule.incompleteCondition')
  const valueLabel = value.value ? labelForValue(value.value) : t('manage.policy.rule.incompleteValue')
  return `${factLabel(fact.value)} ${negated.value ? t('manage.policy.rule.isNot') : t('manage.policy.rule.is')} ${valueLabel}`
})
const valueOptions = computed<Array<{ value: string; label: string; detail?: string }>>(() => {
  switch (fact.value) {
    case 'connection.provider':
      return props.providers.map((provider) => ({
        value: provider.slug,
        label: provider.displayName,
        detail: provider.slug,
      }))
    case 'connection.protocol':
      return [
        { value: 'oidc', label: t('manage.policy.rule.protocolOidc') },
        { value: 'steam', label: t('manage.policy.rule.protocolSteam') },
        { value: 'vrchat', label: t('manage.policy.rule.protocolVrchat') },
      ]
    case 'login_method':
      return [
        { value: 'passkey', label: t('manage.policy.rule.loginPasskey') },
        { value: 'password_totp', label: t('manage.policy.rule.loginPasswordTotp') },
        { value: 'federation', label: t('manage.policy.rule.loginFederation') },
      ]
    case 'avatar':
      return [
        { value: 'any', label: t('manage.policy.rule.avatarAny') },
        { value: 'user_uploaded', label: t('manage.policy.rule.avatarUserUploaded') },
      ]
    default:
      return []
  }
})

function countNodes(condition: Condition): number {
  if (condition.op === 'all' || condition.op === 'any') {
    return 1 + (condition.children ?? []).reduce((count, child) => count + countNodes(child), 0)
  }
  if (condition.op === 'not') return 1 + (condition.child ? countNodes(condition.child) : 0)
  return 1
}

function conditionAtPath(root: Condition, path: RulePath): Condition | undefined {
  let current: Condition | undefined = root
  for (const segment of path) {
    if (!current) return undefined
    current = segment === 'child' ? current.child : current.children?.[segment]
  }
  return current
}

function factLabel(selectedFact: NonNullable<Condition['fact']>): string {
  const key = {
    'connection.provider': 'factConnectionProvider',
    'connection.protocol': 'factConnectionProtocol',
    login_method: 'factLoginMethod',
    avatar: 'factAvatar',
  }[selectedFact]
  return t(`manage.policy.rule.${key}`)
}

function labelForValue(selectedValue: string): string {
  return valueOptions.value.find((option) => option.value === selectedValue)?.label ?? selectedValue
}

function updateFact(nextFact: unknown): void {
  if (typeof nextFact !== 'string' || !FACTS.includes(nextFact as typeof FACTS[number])) return
  emit('update:rule', updatePredicateFact(props.rule, props.path, nextFact as typeof FACTS[number]))
}

function updatePolarity(polarity: unknown): void {
  if (polarity !== 'positive' && polarity !== 'negative') return
  if (polarity === 'negative' && negativeDisabledReason.value) return
  emit('update:rule', setPredicateNegated(props.rule, props.path, polarity === 'negative'))
}

function updateValue(nextValue: unknown): void {
  if (typeof nextValue !== 'string') return
  emit('update:rule', updatePredicateValue(props.rule, props.path, nextValue))
}
function closeActions(event: Event): void {
  emit('actions-closed', event)
  if (!event.defaultPrevented) actionsTrigger.value?.$el?.focus()
}

</script>

<template>
  <div
    role="group"
    :aria-label="contentLabel"
    :data-test="`predicate-row-${pathKey}`"
    class="min-w-0 py-2"
  >
    <div class="grid min-w-0 grid-cols-1 gap-2 sm:grid-cols-[minmax(9rem,1fr)_7rem_minmax(10rem,1.25fr)_2.25rem] sm:items-start">
      <Select :model-value="fact" @update:model-value="updateFact">
        <SelectTrigger
          data-clause-control="fact"
          :data-test="`predicate-fact-${pathKey}`"
          :aria-label="t('manage.policy.rule.factLabel')"
          :aria-invalid="issue ? 'true' : undefined"
          :aria-describedby="issueDescription"
          class="bg-surface shadow-none"
        >
          <SelectValue :placeholder="t('manage.policy.rule.chooseFact')" />
        </SelectTrigger>
        <SelectContent>
          <SelectItem
            v-for="option in FACTS"
            :key="option"
            :value="option"
            :data-value="option"
            :data-test="`predicate-fact-option-${pathKey}-${option}`"
          >
            {{ factLabel(option) }}
          </SelectItem>
        </SelectContent>
      </Select>

      <Select :model-value="negated ? 'negative' : 'positive'" @update:model-value="updatePolarity">
        <SelectTrigger
          data-clause-control="polarity"
          :data-test="`predicate-polarity-${pathKey}`"
          :aria-label="t('manage.policy.rule.polarityLabel')"
          class="bg-surface shadow-none"
        >
          <SelectValue>{{ negated ? t('manage.policy.rule.isNot') : t('manage.policy.rule.is') }}</SelectValue>
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="positive" :data-test="`predicate-polarity-option-${pathKey}-positive`">
            {{ t('manage.policy.rule.is') }}
          </SelectItem>
          <SelectItem
            value="negative"
            :disabled="Boolean(negativeDisabledReason)"
            :data-test="`predicate-polarity-option-${pathKey}-negative`"
          >
            {{ t('manage.policy.rule.isNot') }}
            <span v-if="negativeDisabledReason" class="sr-only"> — {{ negativeDisabledReason }}</span>
          </SelectItem>
        </SelectContent>
      </Select>

      <Select :model-value="value" :disabled="!fact" @update:model-value="updateValue">
        <SelectTrigger
          data-clause-control="value"
          :data-test="`predicate-value-${pathKey}`"
          :aria-label="t('manage.policy.rule.valueLabel')"
          :aria-invalid="issue && fact ? 'true' : undefined"
          :aria-describedby="issueDescription"
          class="bg-surface shadow-none"
        >
          <SelectValue :placeholder="t('manage.policy.rule.chooseValue')" />
        </SelectTrigger>
        <SelectContent>
          <SelectItem
            v-for="option in valueOptions"
            :key="option.value"
            :value="option.value"
            :data-test="`predicate-value-option-${pathKey}-${option.value}`"
          >
            <span class="flex min-w-0 flex-col">
              <span>{{ option.label }}</span>
              <span v-if="option.detail" class="font-mono text-xs text-muted">{{ option.detail }}</span>
            </span>
          </SelectItem>
        </SelectContent>
      </Select>

      <DropdownMenu v-if="canMoveUp || canMoveDown || canRemove">
        <TooltipProvider>
          <Tooltip>
            <TooltipTrigger as-child>
              <DropdownMenuTrigger as-child>
                <Button
                  ref="actionsTrigger"
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  class="text-muted hover:text-ink"
                  :aria-label="t('manage.policy.rule.conditionActions', { condition: contentLabel })"
                  :data-test="`predicate-actions-${pathKey}`"
                >
                  <MoreHorizontal class="size-4" aria-hidden="true" />
                </Button>
              </DropdownMenuTrigger>
            </TooltipTrigger>
            <TooltipContent>{{ t('manage.policy.rule.conditionActions', { condition: contentLabel }) }}</TooltipContent>
          </Tooltip>
        </TooltipProvider>
        <DropdownMenuContent align="end" @close-auto-focus="closeActions">
          <DropdownMenuItem
            v-if="canMoveUp"
            :aria-label="t('manage.policy.rule.moveConditionUpLabel', { condition: contentLabel })"
            :data-test="`predicate-move-up-${pathKey}`"
            @select="emit('move', path, -1)"
          >
            {{ t('manage.policy.rule.moveUp') }}
          </DropdownMenuItem>
          <DropdownMenuItem
            v-if="canMoveDown"
            :aria-label="t('manage.policy.rule.moveConditionDownLabel', { condition: contentLabel })"
            :data-test="`predicate-move-down-${pathKey}`"
            @select="emit('move', path, 1)"
          >
            {{ t('manage.policy.rule.moveDown') }}
          </DropdownMenuItem>
          <DropdownMenuItem
            v-if="canRemove"
            class="text-destructive focus:text-destructive"
            :aria-label="t('manage.policy.rule.removeConditionLabel', { condition: contentLabel })"
            :data-test="`predicate-remove-${pathKey}`"
            @select="emit('remove', path)"
          >
            {{ t('manage.policy.rule.removeCondition') }}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>

    <p
      v-if="issue"
      :id="errorId"
      role="alert"
      class="mt-1 text-sm text-rose-700"
      :data-test="`predicate-error-${pathKey}`"
    >
      {{ t(issue.messageKey) }}
    </p>
  </div>
</template>
