<script setup lang="ts">
import { computed, nextTick, ref, useId } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Rule } from '@/lib/appAccess'
import type { InvalidRuleJSON } from '@/lib/ruleDraft'
import { Button } from '@/components/ui/button'

const props = defineProps<{
  source: string
  parsedRule: Rule
  error?: InvalidRuleJSON
  expression: string
}>()

const emit = defineEmits<{
  'update:source': [string]
  'format': []
  'copy-json': []
  'copy-expression': []
  'escape-tab': []
}>()

const { t } = useI18n()
const textarea = ref<HTMLTextAreaElement>()
const gutter = ref<HTMLElement>()
const allowTabExit = ref(false)
const fieldId = useId()
const helpId = `${fieldId}-help`
const errorId = `${fieldId}-error`
const lineNumbers = computed(() =>
  Array.from({ length: props.source.split('\n').length }, (_, index) => index + 1).join('\n'),
)
const describedBy = computed(() => props.error ? `${helpId} ${errorId}` : helpId)
const errorMessage = computed(() => {
  if (!props.error) return ''
  const detail = t(`manage.policy.rule.validation.${props.error.reason}`)
  if (props.error.line !== undefined && props.error.column !== undefined) {
    return t('manage.policy.rule.jsonErrorLocation', {
      line: props.error.line,
      column: props.error.column,
      detail,
    })
  }
  return t('manage.policy.rule.jsonErrorPath', { path: props.error.path, detail })
})
const showExpression = computed(() => !props.error && Boolean(props.parsedRule))

function syncScroll(): void {
  if (textarea.value && gutter.value) gutter.value.scrollTop = textarea.value.scrollTop
}

async function insertIndent(): Promise<void> {
  const control = textarea.value
  if (!control) return
  const start = control.selectionStart
  const end = control.selectionEnd
  emit('update:source', `${props.source.slice(0, start)}  ${props.source.slice(end)}`)
  await nextTick()
  control.setSelectionRange(start + 2, start + 2)
}

function handleKeydown(event: KeyboardEvent): void {
  if (event.key === 'Escape') {
    allowTabExit.value = true
    return
  }
  if (event.key !== 'Tab') {
    allowTabExit.value = false
    return
  }
  if (allowTabExit.value) {
    allowTabExit.value = false
    emit('escape-tab')
    return
  }
  event.preventDefault()
  void insertIndent()
}
</script>

<template>
  <section class="min-w-0 space-y-4" data-test="rule-json-editor">
    <div class="flex flex-wrap items-end justify-between gap-2">
      <label :for="fieldId" class="text-sm font-medium text-ink">
        {{ t('manage.policy.rule.jsonLabel') }}
      </label>
      <div class="flex flex-wrap gap-1">
        <Button
          type="button"
          variant="ghost"
          size="sm"
          class="shadow-none"
          data-test="rule-json-format"
          :disabled="Boolean(error)"
          @click="emit('format')"
        >
          {{ t('manage.policy.rule.formatJson') }}
        </Button>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          class="shadow-none"
          data-test="rule-json-copy"
          @click="emit('copy-json')"
        >
          {{ t('manage.policy.rule.copyJson') }}
        </Button>
      </div>
    </div>

    <div
      class="grid min-h-64 grid-cols-[3rem_minmax(0,1fr)] overflow-hidden rounded-md border border-input bg-surface focus-within:border-ring focus-within:ring-2 focus-within:ring-ring/25"
      :class="error ? 'border-rose-600' : ''"
    >
      <pre
        ref="gutter"
        aria-hidden="true"
        class="pointer-events-none overflow-hidden border-r border-border bg-sunken px-3 py-3 text-right font-mono text-sm leading-6 text-muted select-none"
        data-test="rule-json-gutter"
      >{{ lineNumbers }}</pre>
      <textarea
        :id="fieldId"
        ref="textarea"
        :value="source"
        wrap="off"
        spellcheck="false"
        autocapitalize="off"
        autocomplete="off"
        class="min-h-64 w-full resize-y overflow-auto bg-surface px-3 py-3 font-mono text-sm leading-6 text-ink outline-none"
        :aria-describedby="describedBy"
        :aria-invalid="error ? 'true' : undefined"
        data-test="rule-json-source"
        @input="emit('update:source', ($event.target as HTMLTextAreaElement).value)"
        @keydown="handleKeydown"
        @scroll="syncScroll"
      />
    </div>

    <p :id="helpId" class="text-sm text-muted" data-test="rule-json-help">
      {{ t('manage.policy.rule.jsonHelp') }}
    </p>
    <p
      v-if="error"
      :id="errorId"
      class="text-sm font-medium text-rose-700"
      role="alert"
      data-test="rule-json-error"
    >
      {{ errorMessage }}
    </p>

    <section v-if="showExpression" class="space-y-2" aria-labelledby="rule-expression-label">
      <div class="flex flex-wrap items-center justify-between gap-2">
        <h3 id="rule-expression-label" class="text-sm font-medium text-ink">
          {{ t('manage.policy.rule.equivalentExpression') }}
        </h3>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          class="shadow-none"
          data-test="rule-expression-copy"
          @click="emit('copy-expression')"
        >
          {{ t('manage.policy.rule.copyExpression') }}
        </Button>
      </div>
      <pre
        class="overflow-x-auto rounded-md border border-border bg-sunken px-3 py-3 font-mono text-sm leading-6 text-ink"
        data-test="rule-expression"
      >{{ expression }}</pre>
    </section>
  </section>
</template>
