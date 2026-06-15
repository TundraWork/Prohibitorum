<script setup lang="ts">
/**
 * AttributeMapEditor — row editor for SAML attribute mappings.
 * Each row: name (required), name_format, source (required), multi (boolean).
 * Persisted shape: AttributeMapEntry[] — identical to the backend SAML attribute_map.
 */
import { nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Plus, X } from 'lucide-vue-next'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'

export interface AttributeMapEntry {
  name: string
  name_format: string
  source: string
  multi: boolean
}

const DEFAULT_NAME_FORMAT = 'urn:oasis:names:tc:SAML:2.0:attrname-format:basic'

interface Row extends AttributeMapEntry {
  _id: number
}

const props = defineProps<{ modelValue: AttributeMapEntry[] }>()
const emit = defineEmits<{ (e: 'update:modelValue', value: AttributeMapEntry[]): void }>()
const { t } = useI18n()

let uid = 0
const rows = ref<Row[]>([])
const listEl = ref<HTMLElement | null>(null)

function toRows(entries: AttributeMapEntry[]): Row[] {
  return entries.map((e) => ({ ...e, _id: uid++ }))
}
function seed(entries: AttributeMapEntry[]): void {
  rows.value = toRows(entries)
}
seed(props.modelValue)

watch(
  () => props.modelValue,
  (mv) => {
    const cur = rows.value.map(({ _id: _, ...e }) => e)
    if (JSON.stringify(mv) !== JSON.stringify(cur)) seed(mv)
  },
)

function emitUpdate(): void {
  emit('update:modelValue', rows.value.map(({ _id: _, ...e }) => e))
}

function nameError(row: Row): string | null {
  return row.name.trim() ? null : t('admin.saml.attrNameRequired')
}
function sourceError(row: Row): string | null {
  return row.source.trim() ? null : t('admin.saml.attrSourceRequired')
}

function addRow(): void {
  rows.value.push({ _id: uid++, name: '', name_format: DEFAULT_NAME_FORMAT, source: '', multi: false })
  nextTick(() => {
    const inputs = listEl.value?.querySelectorAll<HTMLInputElement>('input[data-attr-name]')
    inputs?.[inputs.length - 1]?.focus()
  })
}

function removeRow(index: number): void {
  rows.value.splice(index, 1)
  emitUpdate()
}

function onNameInput(i: number, v: string | number): void {
  rows.value[i].name = String(v)
  emitUpdate()
}
function onFormatInput(i: number, v: string | number): void {
  rows.value[i].name_format = String(v)
  emitUpdate()
}
function onSourceInput(i: number, v: string | number): void {
  rows.value[i].source = String(v)
  emitUpdate()
}
function onMultiChange(i: number, checked: boolean | 'indeterminate'): void {
  rows.value[i].multi = checked === true
  emitUpdate()
}
</script>

<template>
  <div class="flex flex-col gap-3" data-test="attr-map-editor">
    <div v-if="rows.length" ref="listEl" class="flex flex-col gap-3">
      <!-- Column headers -->
      <div class="grid grid-cols-[1fr_1fr_1fr_3rem_2rem] gap-2 text-xs font-medium text-muted">
        <span>{{ t('admin.saml.attrColName') }}</span>
        <span>{{ t('admin.saml.attrColFormat') }}</span>
        <span>{{ t('admin.saml.attrColSource') }}</span>
        <span class="text-center">{{ t('admin.saml.attrColMulti') }}</span>
        <span />
      </div>

      <div
        v-for="(row, i) in rows"
        :key="row._id"
        class="grid grid-cols-[1fr_1fr_1fr_3rem_2rem] items-start gap-2"
        :data-test="`attr-row-${i}`"
      >
        <!-- Name -->
        <div class="flex flex-col gap-1">
          <Input
            :model-value="row.name"
            :placeholder="t('admin.saml.attrNamePlaceholder')"
            :aria-label="t('admin.saml.attrColName')"
            :aria-invalid="!!nameError(row)"
            :data-test="`attr-name-${i}`"
            data-attr-name
            @update:model-value="(v) => onNameInput(i, v)"
          />
          <p v-if="nameError(row)" class="text-xs text-destructive" :data-test="`attr-name-err-${i}`">{{ nameError(row) }}</p>
        </div>

        <!-- Name format -->
        <Input
          :model-value="row.name_format"
          :placeholder="t('admin.saml.attrFormatPlaceholder')"
          :aria-label="t('admin.saml.attrColFormat')"
          :data-test="`attr-format-${i}`"
          @update:model-value="(v) => onFormatInput(i, v)"
        />

        <!-- Source -->
        <div class="flex flex-col gap-1">
          <Input
            :model-value="row.source"
            :placeholder="t('admin.saml.attrSourcePlaceholder')"
            :aria-label="t('admin.saml.attrColSource')"
            :aria-invalid="!!sourceError(row)"
            :data-test="`attr-source-${i}`"
            @update:model-value="(v) => onSourceInput(i, v)"
          />
          <p v-if="sourceError(row)" class="text-xs text-destructive" :data-test="`attr-source-err-${i}`">{{ sourceError(row) }}</p>
        </div>

        <!-- Multi checkbox -->
        <div class="flex justify-center pt-2">
          <Checkbox
            :id="`attr-multi-${row._id}`"
            :checked="row.multi"
            :aria-label="t('admin.saml.attrColMulti')"
            :data-test="`attr-multi-${i}`"
            @update:checked="(v: boolean | 'indeterminate') => onMultiChange(i, v)"
          />
        </div>

        <!-- Remove -->
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          class="mt-0.5 shrink-0 text-muted hover:text-ink"
          :aria-label="t('common.remove')"
          :data-test="`attr-remove-${i}`"
          @click="removeRow(i)"
        >
          <X class="size-4" aria-hidden="true" />
        </Button>
      </div>
    </div>

    <p v-if="rows.length" class="text-xs text-muted">{{ t('admin.saml.attrSourceHint') }}</p>

    <Button
      type="button"
      variant="outline"
      size="sm"
      class="w-fit"
      data-test="attr-add"
      @click="addRow"
    >
      <Plus class="size-4" aria-hidden="true" />
      {{ t('admin.saml.attrAdd') }}
    </Button>
  </div>
</template>
