<script setup lang="ts">
import { computed, useAttrs } from 'vue'
import { useI18n } from 'vue-i18n'
import { Plus, X } from 'lucide-vue-next'
import type { Condition } from '@/lib/appAccess'
import { Button } from '@/components/ui/button'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

defineOptions({
  name: 'RuleConditionEditor',
  inheritAttrs: false,
})

type LeafFact = NonNullable<Condition['fact']>
type NodeKind = LeafFact | NonNullable<Condition['op']>
type AddKind = 'leaf' | 'all' | 'any' | 'not'
type PathSegment = number | 'child'

interface Choice {
  value: string
  labelKey?: string
  label?: string
}

const NODE_KIND_OPTIONS: readonly { value: NodeKind; labelKey: string }[] = [
  { value: 'all', labelKey: 'manage.policy.rule.operatorAll' },
  { value: 'any', labelKey: 'manage.policy.rule.operatorAny' },
  { value: 'not', labelKey: 'manage.policy.rule.operatorNot' },
  { value: 'connection.provider', labelKey: 'manage.policy.rule.factConnectionProvider' },
  { value: 'connection.protocol', labelKey: 'manage.policy.rule.factConnectionProtocol' },
  { value: 'login_method', labelKey: 'manage.policy.rule.factLoginMethod' },
  { value: 'avatar', labelKey: 'manage.policy.rule.factAvatar' },
]

const PROTOCOL_OPTIONS: readonly Choice[] = [
  { value: 'oidc', labelKey: 'manage.policy.rule.protocolOidc' },
  { value: 'steam', labelKey: 'manage.policy.rule.protocolSteam' },
  { value: 'vrchat', labelKey: 'manage.policy.rule.protocolVrchat' },
]

const LOGIN_METHOD_OPTIONS: readonly Choice[] = [
  { value: 'passkey', labelKey: 'manage.policy.rule.loginPasskey' },
  { value: 'password_totp', labelKey: 'manage.policy.rule.loginPasswordTotp' },
  { value: 'federation', labelKey: 'manage.policy.rule.loginFederation' },
]

const AVATAR_OPTIONS: readonly Choice[] = [
  { value: 'any', labelKey: 'manage.policy.rule.avatarAny' },
  { value: 'user_uploaded', labelKey: 'manage.policy.rule.avatarUserUploaded' },
]

const CONDITION_KEYS: readonly (keyof Condition)[] = [
  'op',
  'children',
  'child',
  'fact',
  'provider',
  'protocol',
  'method',
  'source',
]

const props = withDefaults(
  defineProps<{
    modelValue: Condition
    providers: { slug: string }[]
    maxDepth?: number
    maxNodes?: number
    maxChildren?: number
  }>(),
  {
    maxDepth: 8,
    maxNodes: 64,
    maxChildren: 32,
  },
)

const emit = defineEmits<{
  (event: 'update:modelValue', value: Condition): void
}>()

const attrs = useAttrs()
const { t } = useI18n()

function makeLeaf(fact: LeafFact = 'avatar'): Condition {
  switch (fact) {
    case 'connection.provider':
      return { fact, provider: props.providers[0]?.slug ?? '' }
    case 'connection.protocol':
      return { fact, protocol: 'oidc' }
    case 'login_method':
      return { fact, method: 'passkey' }
    case 'avatar':
      return { fact, source: 'any' }
  }
}

function cloneCondition(condition: Condition): Condition {
  if (condition.op === 'all' || condition.op === 'any') {
    return {
      op: condition.op,
      children: (condition.children ?? []).map(cloneCondition),
    }
  }

  if (condition.op === 'not') {
    return {
      op: 'not',
      child: cloneCondition(condition.child ?? makeLeaf()),
    }
  }

  switch (condition.fact) {
    case 'connection.provider':
      return { fact: condition.fact, provider: condition.provider ?? '' }
    case 'connection.protocol':
      return { fact: condition.fact, protocol: condition.protocol ?? 'oidc' }
    case 'login_method':
      return { fact: condition.fact, method: condition.method ?? 'passkey' }
    case 'avatar':
      return { fact: condition.fact, source: condition.source ?? 'any' }
    default:
      return makeLeaf()
  }
}

function countNodes(condition: Condition): number {
  if (condition.op === 'all' || condition.op === 'any') {
    return 1 + (condition.children ?? []).reduce((total, child) => total + countNodes(child), 0)
  }
  if (condition.op === 'not') {
    return 1 + countNodes(condition.child ?? makeLeaf())
  }
  return 1
}

function parsePath(path: string): PathSegment[] {
  return path
    .split('-')
    .slice(1)
    .map((segment) => (segment === 'child' ? segment : Number(segment)))
}

function conditionAtPath(
  root: Condition,
  segments: readonly PathSegment[],
): Condition | undefined {
  let current: Condition | undefined = root

  for (const segment of segments) {
    if (!current) return undefined

    if (segment === 'child') {
      if (current.op !== 'not') return undefined
      current = current.child
      continue
    }

    if (current.op !== 'all' && current.op !== 'any') return undefined
    current = current.children?.[segment]
  }

  return current
}

function overwriteCondition(target: Condition, replacement: Condition): void {
  for (const key of CONDITION_KEYS) delete target[key]
  Object.assign(target, replacement)
}

const nodePath = computed(() => {
  const path = attrs['data-node-path']
  return typeof path === 'string' && path.startsWith('root') ? path : 'root'
})

const pathSegments = computed(() => parsePath(nodePath.value))
const currentNode = computed(
  () => conditionAtPath(props.modelValue, pathSegments.value) ?? makeLeaf(),
)
const currentDepth = computed(() => pathSegments.value.length + 1)
const totalNodes = computed(() => countNodes(props.modelValue))

const isCollection = computed(
  () => currentNode.value.op === 'all' || currentNode.value.op === 'any',
)
const isNot = computed(() => currentNode.value.op === 'not')
const isLeaf = computed(() => !isCollection.value && !isNot.value)
const isEmptyCombinator = computed(
  () => isCollection.value && (currentNode.value.children?.length ?? 0) === 0,
)
const canRemove = computed(() => {
  const last = pathSegments.value.at(-1)
  return pathSegments.value.length > 0 && last !== 'child'
})

const leafFact = computed<LeafFact>(() => {
  switch (currentNode.value.fact) {
    case 'connection.provider':
    case 'connection.protocol':
    case 'login_method':
    case 'avatar':
      return currentNode.value.fact
    default:
      return 'avatar'
  }
})

const nodeKind = computed<NodeKind>(() => {
  switch (currentNode.value.op) {
    case 'all':
    case 'any':
    case 'not':
      return currentNode.value.op
    default:
      return leafFact.value
  }
})

const leafValue = computed(() => {
  switch (leafFact.value) {
    case 'connection.provider':
      return currentNode.value.provider ?? ''
    case 'connection.protocol':
      return currentNode.value.protocol ?? 'oidc'
    case 'login_method':
      return currentNode.value.method ?? 'passkey'
    case 'avatar':
      return currentNode.value.source ?? 'any'
  }
})

const valueOptions = computed<readonly Choice[]>(() => {
  switch (leafFact.value) {
    case 'connection.provider':
      return props.providers.map(({ slug }) => ({ value: slug, label: slug }))
    case 'connection.protocol':
      return PROTOCOL_OPTIONS
    case 'login_method':
      return LOGIN_METHOD_OPTIONS
    case 'avatar':
      return AVATAR_OPTIONS
  }
})

const nodeLabel = computed(() => {
  switch (currentNode.value.op) {
    case 'all':
      return t('manage.policy.rule.operatorAll')
    case 'any':
      return t('manage.policy.rule.operatorAny')
    case 'not':
      return t('manage.policy.rule.operatorNot')
    default:
      return t('manage.policy.rule.conditionGroup')
  }
})

function childPath(index: number): string {
  return `${nodePath.value}-${index}`
}

function notChildPath(): string {
  return `${nodePath.value}-child`
}

function canAdd(kind: AddKind): boolean {
  if (!isCollection.value) return false

  const children = currentNode.value.children?.length ?? 0
  const addedNodes = kind === 'not' ? 2 : 1
  const addedDepth = kind === 'not' ? 2 : 1

  return (
    children < props.maxChildren &&
    totalNodes.value + addedNodes <= props.maxNodes &&
    currentDepth.value + addedDepth <= props.maxDepth
  )
}

function conditionForKind(kind: NodeKind): Condition {
  switch (kind) {
    case 'all':
      return { op: 'all', children: [] }
    case 'any':
      return { op: 'any', children: [] }
    case 'not':
      return { op: 'not', child: makeLeaf() }
    case 'connection.provider':
    case 'connection.protocol':
    case 'login_method':
    case 'avatar':
      return makeLeaf(kind)
  }
}

function isNodeKind(value: unknown): value is NodeKind {
  return (
    typeof value === 'string' &&
    NODE_KIND_OPTIONS.some((option) => option.value === value)
  )
}

function onNodeKindChange(value: unknown): void {
  if (!isNodeKind(value)) return

  const root = cloneCondition(props.modelValue)
  const target = conditionAtPath(root, pathSegments.value)
  if (!target) return

  overwriteCondition(target, conditionForKind(value))
  emit('update:modelValue', root)
}

function onLeafValueChange(value: unknown): void {
  if (typeof value !== 'string') return

  let replacement: Condition
  switch (leafFact.value) {
    case 'connection.provider':
      if (!props.providers.some(({ slug }) => slug === value)) return
      replacement = { fact: 'connection.provider', provider: value }
      break
    case 'connection.protocol':
      if (value !== 'oidc' && value !== 'steam' && value !== 'vrchat') return
      replacement = { fact: 'connection.protocol', protocol: value }
      break
    case 'login_method':
      if (value !== 'passkey' && value !== 'password_totp' && value !== 'federation') return
      replacement = { fact: 'login_method', method: value }
      break
    case 'avatar':
      if (value !== 'any' && value !== 'user_uploaded') return
      replacement = { fact: 'avatar', source: value }
      break
  }

  const root = cloneCondition(props.modelValue)
  const target = conditionAtPath(root, pathSegments.value)
  if (!target) return

  overwriteCondition(target, replacement)
  emit('update:modelValue', root)
}

function addedCondition(kind: AddKind): Condition {
  switch (kind) {
    case 'leaf':
      return makeLeaf()
    case 'all':
      return { op: 'all', children: [] }
    case 'any':
      return { op: 'any', children: [] }
    case 'not':
      return { op: 'not', child: makeLeaf() }
  }
}

function addCondition(kind: AddKind): void {
  if (!canAdd(kind)) return

  const root = cloneCondition(props.modelValue)
  const target = conditionAtPath(root, pathSegments.value)
  if (!target || (target.op !== 'all' && target.op !== 'any')) return

  target.children?.push(addedCondition(kind))
  emit('update:modelValue', root)
}

function removeCurrent(): void {
  if (!canRemove.value) return

  const segments = [...pathSegments.value]
  const index = segments.pop()
  if (typeof index !== 'number') return

  const root = cloneCondition(props.modelValue)
  const parent = conditionAtPath(root, segments)
  if (!parent || (parent.op !== 'all' && parent.op !== 'any')) return

  parent.children?.splice(index, 1)
  emit('update:modelValue', root)
}

function forwardUpdate(value: Condition): void {
  emit('update:modelValue', value)
}
</script>

<template>
  <div
    role="group"
    :aria-label="nodeLabel"
    :aria-invalid="isEmptyCombinator ? 'true' : undefined"
    :aria-describedby="isEmptyCombinator ? `condition-error-${nodePath}` : undefined"
    :data-test="`condition-node-${nodePath}`"
    class="min-w-0 rounded-md border border-border bg-sunken p-2 sm:p-3"
  >
    <div class="flex min-w-0 flex-wrap items-end gap-2">
      <div class="min-w-0 basis-40 flex-1">
        <span class="mb-1 block text-xs font-medium text-muted">
          {{ t('manage.policy.rule.factLabel') }}
        </span>
        <Select :model-value="nodeKind" @update:model-value="onNodeKindChange">
          <SelectTrigger
            :aria-label="t('manage.policy.rule.factLabel')"
            :data-test="`condition-kind-${nodePath}`"
            class="shadow-none"
          >
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem
              v-for="option in NODE_KIND_OPTIONS"
              :key="option.value"
              :value="option.value"
              :data-test="`condition-option-${nodePath}-${option.value}`"
            >
              {{ t(option.labelKey) }}
            </SelectItem>
          </SelectContent>
        </Select>
      </div>

      <div v-if="isLeaf" class="min-w-0 basis-40 flex-1">
        <span class="mb-1 block text-xs font-medium text-muted">
          {{ t('manage.policy.rule.valueLabel') }}
        </span>
        <Select :model-value="leafValue" @update:model-value="onLeafValueChange">
          <SelectTrigger
            :aria-label="t('manage.policy.rule.valueLabel')"
            :data-test="`condition-value-${nodePath}`"
            class="shadow-none"
          >
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem
              v-for="option in valueOptions"
              :key="option.value"
              :value="option.value"
              :data-test="`condition-option-${nodePath}-${option.value}`"
            >
              {{ option.labelKey ? t(option.labelKey) : option.label }}
            </SelectItem>
          </SelectContent>
        </Select>
      </div>

      <Button
        v-if="canRemove"
        type="button"
        variant="ghost"
        size="icon-sm"
        class="shrink-0 self-end text-muted hover:text-destructive"
        :aria-label="t('manage.policy.rule.removeCondition')"
        :data-test="`remove-${nodePath}`"
        @click="removeCurrent"
      >
        <X class="size-4" aria-hidden="true" />
      </Button>
    </div>

    <template v-if="!isLeaf">

      <div v-if="isCollection && currentNode.children?.length" class="mt-2 flex flex-col gap-2">
        <RuleConditionEditor
          v-for="(_, index) in currentNode.children"
          :key="childPath(index)"
          :model-value="modelValue"
          :providers="providers"
          :max-depth="maxDepth"
          :max-nodes="maxNodes"
          :max-children="maxChildren"
          :data-node-path="childPath(index)"
          @update:model-value="forwardUpdate"
        />
      </div>

      <div v-else-if="isNot" class="mt-2">
        <RuleConditionEditor
          :model-value="modelValue"
          :providers="providers"
          :max-depth="maxDepth"
          :max-nodes="maxNodes"
          :max-children="maxChildren"
          :data-node-path="notChildPath()"
          @update:model-value="forwardUpdate"
        />
      </div>

      <p
        v-if="isEmptyCombinator"
        :id="`condition-error-${nodePath}`"
        role="alert"
        :data-test="`condition-error-${nodePath}`"
        class="mt-2 text-xs text-destructive"
      >
        {{ t('manage.policy.rule.emptyCombinator') }}
      </p>

      <div
        v-if="isCollection"
        class="mt-2 flex flex-wrap items-center gap-1 border-t border-border pt-2"
      >
        <span class="me-1 text-xs font-medium text-muted">
          {{ t('manage.policy.rule.addCondition') }}
        </span>
        <Button
          type="button"
          variant="outline"
          size="sm"
          class="shadow-none"
          :disabled="!canAdd('leaf')"
          :aria-label="t('manage.policy.rule.addLeaf')"
          :data-test="`add-leaf-${nodePath}`"
          @click="addCondition('leaf')"
        >
          <Plus class="size-4" aria-hidden="true" />
          {{ t('manage.policy.rule.addLeaf') }}
        </Button>
        <Button
          type="button"
          variant="outline"
          size="sm"
          class="shadow-none"
          :disabled="!canAdd('all')"
          :aria-label="t('manage.policy.rule.addAll')"
          :data-test="`add-all-${nodePath}`"
          @click="addCondition('all')"
        >
          <Plus class="size-4" aria-hidden="true" />
          {{ t('manage.policy.rule.addAll') }}
        </Button>
        <Button
          type="button"
          variant="outline"
          size="sm"
          class="shadow-none"
          :disabled="!canAdd('any')"
          :aria-label="t('manage.policy.rule.addAny')"
          :data-test="`add-any-${nodePath}`"
          @click="addCondition('any')"
        >
          <Plus class="size-4" aria-hidden="true" />
          {{ t('manage.policy.rule.addAny') }}
        </Button>
        <Button
          type="button"
          variant="outline"
          size="sm"
          class="shadow-none"
          :disabled="!canAdd('not')"
          :aria-label="t('manage.policy.rule.addNot')"
          :data-test="`add-not-${nodePath}`"
          @click="addCondition('not')"
        >
          <Plus class="size-4" aria-hidden="true" />
          {{ t('manage.policy.rule.addNot') }}
        </Button>
      </div>
    </template>
  </div>
</template>
