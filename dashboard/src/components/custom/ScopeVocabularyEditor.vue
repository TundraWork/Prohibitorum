<script setup lang="ts">
/**
 * ScopeVocabularyEditor — row editor for a forward-auth app's scope vocabulary.
 * Each row: name + description. Persisted shape: ScopeEntry[] — matches the
 * backend forward-auth-app `scopes` field. Mirrors AttributeMapEditor.
 */
import { nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Plus, X } from 'lucide-vue-next'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'

export interface ScopeEntry {
  name: string
  description: string
}

interface Row extends ScopeEntry {
  _id: number
}

const props = defineProps<{ modelValue: ScopeEntry[] }>()
const emit = defineEmits<{ (e: 'update:modelValue', value: ScopeEntry[]): void }>()
const { t } = useI18n()

let uid = 0
const rows = ref<Row[]>([])
const listEl = ref<HTMLElement | null>(null)

function toRows(entries: ScopeEntry[]): Row[] {
  return entries.map((e) => ({ ...e, _id: uid++ }))
}
function seed(entries: ScopeEntry[]): void {
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

function addRow(): void {
  rows.value.push({ _id: uid++, name: '', description: '' })
  nextTick(() => {
    const inputs = listEl.value?.querySelectorAll<HTMLInputElement>('input[data-scope-name]')
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
function onDescriptionInput(i: number, v: string | number): void {
  rows.value[i].description = String(v)
  emitUpdate()
}
</script>

<template>
  <div ref="listEl" class="flex flex-col gap-3" data-test="scope-editor">
    <div v-if="rows.length" class="flex flex-col gap-3">
      <!-- Column headers: grid layout only. Below sm each field is labelled inline. -->
      <div class="hidden grid-cols-[1fr_2fr_2rem] gap-2 text-xs font-medium text-muted sm:grid">
        <span>{{ t('admin.forwardAuth.scopeName') }}</span>
        <span>{{ t('admin.forwardAuth.scopeDescription') }}</span>
        <span />
      </div>

      <!-- Below sm: a labelled card per row. From sm: the 3-column grid. -->
      <div
        v-for="(row, i) in rows"
        :key="row._id"
        class="grid grid-cols-1 gap-2 rounded-md border border-border p-3 sm:grid-cols-[1fr_2fr_2rem] sm:items-start sm:gap-2 sm:rounded-none sm:border-0 sm:p-0"
        :data-test="`scope-row-${i}`"
      >
        <!-- Name -->
        <div class="flex flex-col gap-1">
          <span class="text-xs font-medium text-muted sm:hidden">{{ t('admin.forwardAuth.scopeName') }}</span>
          <Input
            :model-value="row.name"
            :placeholder="t('admin.forwardAuth.scopeName')"
            :aria-label="t('admin.forwardAuth.scopeName')"
            :data-test="`scope-name-${i}`"
            data-scope-name
            @update:model-value="(v) => onNameInput(i, v)"
          />
        </div>

        <!-- Description -->
        <div class="flex flex-col gap-1">
          <span class="text-xs font-medium text-muted sm:hidden">{{ t('admin.forwardAuth.scopeDescription') }}</span>
          <Input
            :model-value="row.description"
            :placeholder="t('admin.forwardAuth.scopeDescription')"
            :aria-label="t('admin.forwardAuth.scopeDescription')"
            :data-test="`scope-desc-${i}`"
            @update:model-value="(v) => onDescriptionInput(i, v)"
          />
        </div>

        <!-- Remove -->
        <div class="flex justify-end sm:block">
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            class="shrink-0 text-muted hover:text-ink sm:mt-0.5"
            :aria-label="t('admin.forwardAuth.removeScope')"
            :data-test="`scope-remove-${i}`"
            @click="removeRow(i)"
          >
            <X class="size-4" aria-hidden="true" />
          </Button>
        </div>
      </div>
    </div>

    <Button
      type="button"
      variant="outline"
      size="sm"
      class="w-fit"
      data-test="scope-add"
      @click="addRow"
    >
      <Plus class="size-4" aria-hidden="true" />
      {{ t('admin.forwardAuth.addScope') }}
    </Button>
  </div>
</template>
