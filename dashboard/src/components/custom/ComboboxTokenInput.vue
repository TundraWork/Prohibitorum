<script setup lang="ts">
/**
 * ComboboxTokenInput — a chip/token input with a filterable suggestion dropdown.
 *
 * Enter a list of strings as removable chips: Enter/comma/blur commits the draft, paste splits
 * on newline/comma, Backspace on an empty draft removes the last chip, per-item validation flags
 * invalid chips, and long values truncate with a tooltip. On top of that it offers a combobox
 * dropdown of suggested values, each shown with a short description. Suggestions are a
 * convenience: arbitrary CUSTOM values can still be typed and committed. modelValue is string[].
 *
 * The dropdown is an inline role="listbox" (no portal) layered over the chip input, with the
 * input carrying combobox ARIA (role="combobox", aria-expanded, aria-controls). Up/Down move
 * the active option, Enter selects the active option (else commits the draft as a custom value),
 * Escape closes the list.
 */
import { computed, nextTick, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { X } from 'lucide-vue-next'
import { cn } from '@/lib/utils'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'

interface Suggestion {
  value: string
  description?: string
}

const props = withDefaults(
  defineProps<{
    modelValue: string[]
    suggestions?: Suggestion[]
    validate?: (value: string) => string | null
    placeholder?: string
    splitOn?: RegExp
    inputId?: string
    ariaLabel?: string
  }>(),
  { suggestions: () => [], splitOn: () => /[\n,]+/ },
)
const emit = defineEmits<{ (e: 'update:modelValue', value: string[]): void }>()

const { t } = useI18n()
const draft = ref('')
const inputEl = ref<HTMLInputElement | null>(null)
const open = ref(false)
const activeIndex = ref(-1)

const listboxId = computed(() => (props.inputId ? `${props.inputId}-listbox` : 'combobox-token-listbox'))

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

// Suggestions not already selected, filtered by the (case-insensitive) draft substring.
const filteredSuggestions = computed<Suggestion[]>(() => {
  const q = draft.value.trim().toLowerCase()
  return props.suggestions.filter((s) => {
    if (props.modelValue.includes(s.value)) return false
    if (!q) return true
    return s.value.toLowerCase().includes(q)
  })
})

const showList = computed(() => open.value && filteredSuggestions.value.length > 0)

function activeOptionId(): string | undefined {
  if (!showList.value || activeIndex.value < 0) return undefined
  const s = filteredSuggestions.value[activeIndex.value]
  return s ? `${listboxId.value}-${s.value}` : undefined
}

function addItems(raw: string[]): void {
  const next = [...props.modelValue]
  for (const r of raw) {
    const value = r.trim()
    if (value && !next.includes(value)) next.push(value)
  }
  if (next.length !== props.modelValue.length) emit('update:modelValue', next)
}
function commitDraft(): void {
  if (draft.value.trim()) addItems(draft.value.split(props.splitOn))
  draft.value = ''
  activeIndex.value = -1
}
function selectSuggestion(s: Suggestion): void {
  addItems([s.value])
  draft.value = ''
  activeIndex.value = -1
  // Keep focus in the input so the admin can keep adding scopes.
  inputEl.value?.focus()
}
function removeAt(index: number): void {
  emit('update:modelValue', props.modelValue.filter((_, i) => i !== index))
}

function openList(): void {
  open.value = true
}
function closeList(): void {
  open.value = false
  activeIndex.value = -1
}

function onInput(): void {
  open.value = true
  // Reset the active option when the filter changes so we don't point past the end.
  activeIndex.value = -1
}
function onEnter(e: KeyboardEvent): void {
  e.preventDefault()
  if (showList.value && activeIndex.value >= 0) {
    const s = filteredSuggestions.value[activeIndex.value]
    if (s) {
      selectSuggestion(s)
      return
    }
  }
  commitDraft()
}
function onKeydown(e: KeyboardEvent): void {
  if (e.key === ',') {
    e.preventDefault()
    commitDraft()
  } else if (e.key === 'ArrowDown') {
    e.preventDefault()
    if (!open.value) open.value = true
    if (filteredSuggestions.value.length) {
      activeIndex.value = (activeIndex.value + 1) % filteredSuggestions.value.length
    }
  } else if (e.key === 'ArrowUp') {
    e.preventDefault()
    if (filteredSuggestions.value.length) {
      activeIndex.value =
        activeIndex.value <= 0 ? filteredSuggestions.value.length - 1 : activeIndex.value - 1
    }
  } else if (e.key === 'Escape') {
    if (open.value) {
      e.preventDefault()
      closeList()
    }
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
    activeIndex.value = -1
  }
}
function onBlur(): void {
  // Commit any draft, then close. Delay the close so a mousedown on an option still fires.
  commitDraft()
  nextTick(() => closeList())
}
function focusInput(): void {
  inputEl.value?.focus()
}
</script>

<template>
  <div class="relative flex flex-col gap-1">
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
        role="combobox"
        :aria-expanded="showList"
        :aria-controls="listboxId"
        aria-autocomplete="list"
        :aria-activedescendant="activeOptionId()"
        :aria-label="ariaLabel"
        :placeholder="modelValue.length ? '' : placeholder"
        class="min-w-[6rem] flex-1 bg-transparent text-ink outline-none placeholder:text-muted-foreground"
        @keydown.enter="onEnter"
        @keydown="onKeydown"
        @input="onInput"
        @focus="openList"
        @paste="onPaste"
        @blur="onBlur"
      />
    </div>

    <ul
      v-if="showList"
      :id="listboxId"
      role="listbox"
      data-test="scope-listbox"
      :class="cn(
        'absolute top-full z-50 mt-1 max-h-60 w-full overflow-y-auto rounded-md border bg-popover p-1 text-popover-foreground shadow-md',
      )"
    >
      <li
        v-for="(s, i) in filteredSuggestions"
        :id="`${listboxId}-${s.value}`"
        :key="s.value"
        role="option"
        :data-test="`scope-option-${s.value}`"
        :aria-selected="i === activeIndex"
        :class="cn(
          'flex cursor-pointer flex-col gap-0.5 rounded-sm px-2 py-1.5 text-sm',
          i === activeIndex ? 'bg-accent text-accent-foreground' : 'text-ink',
        )"
        @mousedown.prevent
        @click="selectSuggestion(s)"
        @mousemove="activeIndex = i"
      >
        <span class="font-medium">{{ s.value }}</span>
        <span v-if="s.description" class="text-xs text-muted-foreground">{{ s.description }}</span>
      </li>
    </ul>

    <p v-if="firstError" class="text-xs text-destructive">{{ firstError }}</p>
  </div>
</template>
