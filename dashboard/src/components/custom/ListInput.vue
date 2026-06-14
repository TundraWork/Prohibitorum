<script setup lang="ts">
/**
 * ListInput — edit a list of free-form values as one full-width input per row (with an
 * Add button and per-row remove + inline validation). For long/structured values like
 * redirect URLs and domains where the whole value must stay visible and editable —
 * unlike a chip/token input, which suits short tokens. modelValue is string[].
 */
import { nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Plus, X } from 'lucide-vue-next'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'

const props = withDefaults(
  defineProps<{
    modelValue: string[]
    validate?: (value: string) => string | null
    placeholder?: string
    addLabel: string
    name: string
    inputmode?: 'text' | 'url' | 'email'
  }>(),
  { inputmode: 'text' },
)
const emit = defineEmits<{ (e: 'update:modelValue', value: string[]): void }>()
const { t } = useI18n()

interface Row {
  id: number
  value: string
}
let uid = 0
const rows = ref<Row[]>([])
const rowsEl = ref<HTMLElement | null>(null)

function seed(values: string[]): void {
  rows.value = values.map((v) => ({ id: uid++, value: v }))
}
seed(props.modelValue)
// Reseed only when the external model changes to something we did not just emit
// (e.g. a detail view loading its record), avoiding a focus-stealing feedback loop.
watch(
  () => props.modelValue,
  (mv) => {
    const current = rows.value.map((r) => r.value.trim()).filter(Boolean)
    if (JSON.stringify(mv) !== JSON.stringify(current)) seed(mv)
  },
)

function errorFor(value: string): string | null {
  const v = value.trim()
  return v && props.validate ? props.validate(v) : null
}
function emitUpdate(): void {
  emit('update:modelValue', rows.value.map((r) => r.value.trim()).filter(Boolean))
}
function onInput(index: number, value: string | number): void {
  rows.value[index].value = String(value)
  emitUpdate()
}
function addRow(): void {
  rows.value.push({ id: uid++, value: '' })
  nextTick(() => {
    const inputs = rowsEl.value?.querySelectorAll('input')
    inputs?.[inputs.length - 1]?.focus()
  })
}
function removeRow(index: number): void {
  rows.value.splice(index, 1)
  emitUpdate()
}
</script>

<template>
  <div class="flex flex-col gap-2">
    <div v-if="rows.length" ref="rowsEl" class="flex flex-col gap-2">
      <div v-for="(row, i) in rows" :key="row.id" class="flex flex-col gap-1">
        <div class="flex items-center gap-2">
          <Input
            :model-value="row.value"
            :placeholder="placeholder"
            :inputmode="inputmode"
            :aria-invalid="!!errorFor(row.value)"
            :aria-describedby="errorFor(row.value) ? `${name}-err-${row.id}` : undefined"
            :data-test="`${name}-input-${i}`"
            class="flex-1 font-mono text-sm"
            @update:model-value="(v) => onInput(i, v)"
            @keydown.enter.prevent="addRow"
          />
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            class="shrink-0 text-muted hover:text-ink"
            :aria-label="t('common.remove')"
            :data-test="`${name}-remove-${i}`"
            @click="removeRow(i)"
          >
            <X class="size-4" aria-hidden="true" />
          </Button>
        </div>
        <p v-if="errorFor(row.value)" :id="`${name}-err-${row.id}`" class="pl-1 text-xs text-destructive">{{ errorFor(row.value) }}</p>
      </div>
    </div>
    <Button
      type="button"
      variant="outline"
      size="sm"
      class="w-fit"
      :data-test="`${name}-add`"
      @click="addRow"
    >
      <Plus class="size-4" aria-hidden="true" />
      {{ addLabel }}
    </Button>
  </div>
</template>
