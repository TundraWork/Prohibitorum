<script setup lang="ts">
import { computed, defineComponent, h, type PropType, type VNode } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Condition, ProviderDescriptor, Rule } from '@/lib/appAccess'

interface MeaningLeaf {
  kind: 'leaf'
  subject: string
  operator: string
  value: string
}

interface MeaningGroup {
  kind: 'group'
  op: 'all' | 'any'
  children: MeaningNode[]
}

type MeaningNode = MeaningLeaf | MeaningGroup

const props = withDefaults(defineProps<{
  rule: Rule
  providers: ProviderDescriptor[]
  compact?: boolean
}>(), {
  compact: false,
})

const { t } = useI18n()
const providerLabels = computed(() => new Map(
  props.providers.map((provider) => [provider.slug, provider.displayName]),
))

function protocolValue(condition: Condition): string {
  switch (condition.protocol) {
    case 'oidc': return t('manage.policy.rule.protocolOidc')
    case 'steam': return t('manage.policy.rule.protocolSteam')
    case 'vrchat': return t('manage.policy.rule.protocolVrchat')
    default: return t('manage.policy.rule.incompleteValue')
  }
}

function loginMethodValue(condition: Condition): string {
  switch (condition.method) {
    case 'passkey': return t('manage.policy.rule.loginPasskey')
    case 'password_totp': return t('manage.policy.rule.loginPasswordTotp')
    case 'federation': return t('manage.policy.rule.loginFederation')
    default: return t('manage.policy.rule.incompleteValue')
  }
}

function avatarValue(condition: Condition): string {
  switch (condition.source) {
    case 'any': return t('manage.policy.rule.avatarAny')
    case 'user_uploaded': return t('manage.policy.rule.avatarUserUploaded')
    default: return t('manage.policy.rule.incompleteValue')
  }
}

function leafText(condition: Condition, negated: boolean): MeaningLeaf {
  const operator = t(negated ? 'manage.policy.rule.isNot' : 'manage.policy.rule.is')
  switch (condition.fact) {
    case 'connection.provider':
      return {
        kind: 'leaf',
        subject: t('manage.policy.rule.factConnectionProvider'),
        operator,
        value: condition.provider
          ? providerLabels.value.get(condition.provider) ?? condition.provider
          : t('manage.policy.rule.incompleteValue'),
      }
    case 'connection.protocol':
      return {
        kind: 'leaf',
        subject: t('manage.policy.rule.factConnectionProtocol'),
        operator,
        value: protocolValue(condition),
      }
    case 'login_method':
      return {
        kind: 'leaf',
        subject: t('manage.policy.rule.factLoginMethod'),
        operator,
        value: loginMethodValue(condition),
      }
    case 'avatar':
      return {
        kind: 'leaf',
        subject: t('manage.policy.rule.factAvatar'),
        operator,
        value: avatarValue(condition),
      }
    default:
      return {
        kind: 'leaf',
        subject: t('manage.policy.rule.incompleteCondition'),
        operator: '',
        value: '',
      }
  }
}

function deriveMeaning(condition: Condition): MeaningNode {
  if (condition.op === 'all' || condition.op === 'any') {
    return {
      kind: 'group',
      op: condition.op,
      children: (condition.children ?? []).map(deriveMeaning),
    }
  }
  if (condition.op === 'not' && condition.child) return leafText(condition.child, true)
  return leafText(condition, false)
}

function leafSentence(node: MeaningLeaf): string {
  return t('manage.policy.rule.meaningLeaf', {
    subject: node.subject,
    operator: node.operator,
    value: node.value,
  })
}

function firstLine(node: MeaningNode, punctuation: boolean): string {
  if (node.kind === 'leaf') return leafSentence(node)
  const value = t(node.op === 'all'
    ? 'manage.policy.rule.meaningAll'
    : 'manage.policy.rule.meaningAny')
  return punctuation
    ? t('manage.policy.rule.meaningOutlineLine', { sentence: value })
    : value
}

function leafCount(node: MeaningNode): number {
  return node.kind === 'leaf'
    ? 1
    : node.children.reduce((sum, child) => sum + leafCount(child), 0)
}

function nestedGroupCount(node: MeaningNode): number {
  return node.kind === 'leaf'
    ? 0
    : node.children.reduce(
      (sum, child) => sum + (child.kind === 'group' ? 1 : 0) + nestedGroupCount(child),
      0,
    )
}

function renderLeaf(node: MeaningLeaf): VNode {
  const sentence = leafSentence(node)
  const valueIndex = node.value ? sentence.lastIndexOf(node.value) : -1
  if (valueIndex < 0) return h('span', sentence)
  return h('span', [
    sentence.slice(0, valueIndex),
    h('strong', { class: 'font-semibold text-ink' }, node.value),
    sentence.slice(valueIndex + node.value.length),
  ])
}

const MeaningLeafText = defineComponent({
  name: 'MeaningLeafText',
  props: {
    node: { type: Object as PropType<MeaningLeaf>, required: true },
  },
  setup(componentProps) {
    return () => renderLeaf(componentProps.node)
  },
})

function renderList(node: MeaningGroup): VNode {
  return h('ul', { class: 'mt-2 list-disc space-y-2 pl-5 marker:text-muted' },
    node.children.map((child) => h('li', { class: 'pl-1' }, [
      child.kind === 'leaf'
        ? renderLeaf(child)
        : h('div', [
            h('span', { class: 'font-medium text-ink' }, firstLine(child, true)),
            renderList(child),
          ]),
    ])),
  )
}

const MeaningList = defineComponent({
  name: 'MeaningList',
  props: {
    node: { type: Object as PropType<MeaningGroup>, required: true },
  },
  setup(componentProps) {
    return () => renderList(componentProps.node)
  },
})

const meaning = computed(() => deriveMeaning(props.rule.condition))
const counts = computed(() => t('manage.policy.rule.meaningCounts', {
  leaves: leafCount(meaning.value),
  leafLabel: t('manage.policy.rule.meaningCondition', leafCount(meaning.value)),
  groups: nestedGroupCount(meaning.value),
  groupLabel: t('manage.policy.rule.meaningNestedGroup', nestedGroupCount(meaning.value)),
}))
</script>

<template>
  <div class="min-w-0 text-sm leading-6 text-ink" data-test="rule-meaning">
    <template v-if="compact">
      <p class="font-medium" data-test="rule-meaning-first-line">
        {{ firstLine(meaning, false) }}
      </p>
      <p class="text-muted" data-test="rule-meaning-counts">
        {{ counts }}
      </p>
    </template>

    <template v-else-if="meaning.kind === 'leaf'">
      <p data-test="rule-meaning-first-line">
        <MeaningLeafText :node="meaning" />
      </p>
    </template>

    <template v-else>
      <p class="font-medium" data-test="rule-meaning-first-line">
        {{ firstLine(meaning, true) }}
      </p>
      <MeaningList :node="meaning" />
    </template>
  </div>
</template>
