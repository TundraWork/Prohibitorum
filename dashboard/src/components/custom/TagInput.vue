<script setup lang="ts">
/**
 * TagInput — enter a list of strings as removable chips. Replaces "one X per line"
 * textareas: Enter/comma/blur commits, paste splits on newline/comma, Backspace on an
 * empty draft removes the last chip, per-item validation flags invalid chips, and long
 * values truncate with a tooltip. modelValue is string[].
 */
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { X } from 'lucide-vue-next'
import { cn } from '@/lib/utils'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'

const props = withDefaults(
  defineProps<{
    modelValue: string[]
    validate?: (value: string) => string | null
    placeholder?: string
    splitOn?: RegExp
    inputId?: string
    ariaLabel?: string
  }>(),
  { splitOn: () => /[\n,]+/ },
)
const emit = defineEmits<{ (e: 'update:modelValue', value: string[]): void }>()

const { t } = useI18n()
const draft = ref('')
const inputEl = ref<HTMLInputElement | null>(null)

function errorFor(value: string): string | null {
  return props.validate ? props.validate(value) : null
}
const firstError = computed(() => {
  for (const item of props.modelValue) {
    const e = errorFor(item)
    if (e) return e
  }
  return ''
})

function addItems(raw: string[]): void {
  const next = [...props.modelValue]
  for (const r of raw) {
    const value = r.trim()
    if (value && !next.includes(value)) next.push(value)
  }
  if (next.length !== props.modelValue.length) emit('update:modelValue', next)
}
function commit(): void {
  if (draft.value.trim()) addItems(draft.value.split(props.splitOn))
  draft.value = ''
}
function removeAt(index: number): void {
  emit('update:modelValue', props.modelValue.filter((_, i) => i !== index))
}
function onKeydown(e: KeyboardEvent): void {
  if (e.key === ',') {
    e.preventDefault()
    commit()
  } else if (e.key === 'Backspace' && draft.value === '' && props.modelValue.length > 0) {
    removeAt(props.modelValue.length - 1)
  }
}
function onPaste(e: ClipboardEvent): void {
  const text = e.clipboardData?.getData('text') ?? ''
  if (props.splitOn.test(text)) {
    e.preventDefault()
    addItems(text.split(props.splitOn))
    draft.value = ''
  }
}
function focusInput(): void {
  inputEl.value?.focus()
}
</script>

<template>
  <div class="flex flex-col gap-1">
    <div
      :class="cn(
        'flex min-h-9 w-full flex-wrap items-center gap-1.5 rounded-md border border-input bg-sunken px-2 py-1.5 text-sm',
        'focus-within:border-ring focus-within:ring-ring/50 focus-within:ring-[3px]',
      )"
      role="group"
      :aria-label="ariaLabel"
      @click="focusInput"
    >
      <span
        v-for="(item, i) in modelValue"
        :key="`${item}-${i}`"
        :data-test="`tag-${item}`"
        :class="cn(
          'inline-flex max-w-full items-center gap-1 rounded px-2 py-0.5 text-xs',
          errorFor(item)
            ? 'border border-destructive/40 bg-destructive/10 text-destructive'
            : 'border border-border bg-bg text-ink',
        )"
      >
        <TooltipProvider :delay-duration="300">
          <Tooltip>
            <TooltipTrigger as-child>
              <span class="max-w-[16rem] truncate">{{ item }}</span>
            </TooltipTrigger>
            <TooltipContent>{{ item }}</TooltipContent>
          </Tooltip>
        </TooltipProvider>
        <button
          type="button"
          class="shrink-0 rounded-sm opacity-70 transition-opacity hover:opacity-100"
          :data-test="`tag-remove-${item}`"
          :aria-label="t('common.remove')"
          @click.stop="removeAt(i)"
        >
          <X class="size-3" aria-hidden="true" />
        </button>
      </span>
      <input
        :id="inputId"
        ref="inputEl"
        v-model="draft"
        :aria-label="ariaLabel"
        :placeholder="modelValue.length ? '' : placeholder"
        class="min-w-[6rem] flex-1 bg-transparent text-ink outline-none placeholder:text-muted-foreground"
        @keydown.enter.prevent="commit"
        @keydown="onKeydown"
        @paste="onPaste"
        @blur="commit"
      />
    </div>
    <p v-if="firstError" class="text-xs text-destructive">{{ firstError }}</p>
  </div>
</template>
