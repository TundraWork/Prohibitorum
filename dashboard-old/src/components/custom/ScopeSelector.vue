<script setup lang="ts">
/**
 * ScopeSelector — unified scope picker used for both OIDC apps and upstream IdPs.
 *
 * Known scopes are rendered as described checkbox rows (checkbox + name + description).
 * A `required` known scope is always checked, disabled, and always included in the emitted value.
 *
 * When allowCustom is true, scopes in modelValue that are not in the known list appear as
 * removable chips below a "Custom scopes" divider, and an add-control lets the admin type
 * and commit additional custom values (Enter / comma).  When allowCustom is false the custom
 * section is not shown and no custom tokens can be added.
 *
 * Emitted value = (checked known scopes, required ones forced on) union (custom scopes), de-duped,
 * known scopes first then custom.
 */
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { X } from 'lucide-vue-next'
import { Checkbox } from '@/components/ui/checkbox'
import { Button } from '@/components/ui/button'

export interface KnownScope {
  value: string
  description: string
  required?: boolean
}

const props = withDefaults(
  defineProps<{
    modelValue: string[]
    known: KnownScope[]
    allowCustom?: boolean
  }>(),
  { allowCustom: false },
)
const emit = defineEmits<{ (e: 'update:modelValue', value: string[]): void }>()

const { t } = useI18n()

// ---------- known scope helpers ----------

const knownValues = computed(() => new Set(props.known.map((s) => s.value)))

function toggleKnown(scope: string, checked: boolean): void {
  const k = props.known.find((s) => s.value === scope)
  if (k?.required) return // ignore toggles on required scopes
  const custom = props.modelValue.filter((s) => !knownValues.value.has(s))
  let checkedKnown: string[]
  if (checked && !props.modelValue.includes(scope)) {
    checkedKnown = props.known.filter((s) => props.modelValue.includes(s.value) || s.value === scope).map((s) => s.value)
  } else {
    checkedKnown = props.known.filter((s) => props.modelValue.includes(s.value) && s.value !== scope).map((s) => s.value)
  }
  // Always include required scopes.
  for (const s of props.known) {
    if (s.required && !checkedKnown.includes(s.value)) checkedKnown.unshift(s.value)
  }
  // Stable ordering: known order first, then custom.
  const next = [...checkedKnown, ...custom]
  emit('update:modelValue', dedupe(next))
}

// ---------- custom scope helpers ----------

const customScopes = computed(() => props.modelValue.filter((s) => !knownValues.value.has(s)))

const draft = ref('')

function commitDraft(): void {
  const value = draft.value.trim()
  draft.value = ''
  if (!value) return
  addCustom(value)
}

function addCustom(value: string): void {
  if (!value || props.modelValue.includes(value)) return
  // Keep: known (in original known order) + existing custom + new value.
  const knownChecked = props.known.filter((s) => props.modelValue.includes(s.value)).map((s) => s.value)
  const existing = customScopes.value
  emit('update:modelValue', dedupe([...knownChecked, ...existing, value]))
}

function removeCustom(value: string): void {
  emit('update:modelValue', props.modelValue.filter((s) => s !== value))
}

function onDraftKeydown(e: KeyboardEvent): void {
  if (e.key === 'Enter') { e.preventDefault(); commitDraft() }
  else if (e.key === ',') { e.preventDefault(); commitDraft() }
  else if (e.key === 'Backspace' && draft.value === '' && customScopes.value.length > 0) {
    removeCustom(customScopes.value[customScopes.value.length - 1])
  }
}

function onDraftBlur(): void {
  commitDraft()
}

// ---------- utils ----------

function dedupe(arr: string[]): string[] {
  return arr.filter((v, i) => arr.indexOf(v) === i)
}
</script>

<template>
  <div class="flex flex-col gap-2">
    <!-- Known scopes as described checkbox rows -->
    <label
      v-for="s in known"
      :key="s.value"
      :data-test="`scope-row-${s.value}`"
      class="flex items-start gap-3"
      :class="s.required ? 'cursor-default' : 'cursor-pointer'"
    >
      <Checkbox
        :model-value="modelValue.includes(s.value)"
        :disabled="s.required"
        :data-test="`scope-checkbox-${s.value}`"
        class="mt-0.5 shrink-0"
        @update:model-value="(c) => toggleKnown(s.value, c === true)"
      />
      <span class="flex min-w-0 flex-col gap-0.5">
        <span class="font-mono text-sm text-ink">{{ s.value }}</span>
        <span class="text-xs text-muted" :data-test="`scope-desc-${s.value}`">{{ s.description }}</span>
      </span>
    </label>

    <!-- Custom scopes section (only when allowCustom) -->
    <template v-if="allowCustom">
      <div class="mt-1 flex items-center gap-2">
        <div class="h-px flex-1 bg-border" />
        <span class="text-xs text-muted" data-test="custom-scopes-label">{{ t('scopeSelector.customLabel') }}</span>
        <div class="h-px flex-1 bg-border" />
      </div>

      <!-- Chips for existing custom scopes -->
      <div v-if="customScopes.length" class="flex flex-wrap gap-1.5">
        <span
          v-for="s in customScopes"
          :key="s"
          :data-test="`custom-chip-${s}`"
          class="inline-flex items-center gap-1 rounded border border-border bg-bg px-2 py-0.5 text-xs text-ink"
        >
          <span class="font-mono">{{ s }}</span>
          <button
            type="button"
            :data-test="`custom-chip-remove-${s}`"
            class="inline-flex size-6 shrink-0 items-center justify-center rounded-sm opacity-70 transition-opacity hover:opacity-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring cursor-pointer"
            :aria-label="t('common.remove') + ': ' + s"
            @click.stop="removeCustom(s)"
          >
            <X class="size-3" aria-hidden="true" />
          </button>
        </span>
      </div>

      <!-- Add custom scope input -->
      <div class="flex items-center gap-2">
        <input
          v-model="draft"
          data-test="custom-scope-input"
          :aria-label="t('scopeSelector.customPlaceholder')"
          :placeholder="t('scopeSelector.customPlaceholder')"
          class="h-8 min-w-0 flex-1 rounded-md border border-input bg-sunken px-2 text-sm text-ink outline-none placeholder:text-muted-foreground focus:border-ring focus:ring-[3px] focus:ring-ring/50"
          @keydown="onDraftKeydown"
          @blur="onDraftBlur"
        />
        <Button type="button" variant="outline" size="sm" data-test="custom-scope-add" @click="commitDraft">
          {{ t('common.add') }}
        </Button>
      </div>
    </template>
  </div>
</template>
