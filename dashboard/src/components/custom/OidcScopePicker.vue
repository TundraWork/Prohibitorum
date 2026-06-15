<script setup lang="ts">
/**
 * OidcScopePicker — checkbox list for the 4 fixed OIDC scopes.
 * openid is always checked and disabled (required by spec).
 * Toggling profile/email/offline_access adds or removes them from modelValue.
 */
import { useI18n } from 'vue-i18n'
import { Checkbox } from '@/components/ui/checkbox'
import { OIDC_SCOPES } from '@/lib/scopes'

const props = defineProps<{ modelValue: string[] }>()
const emit = defineEmits<{ (e: 'update:modelValue', value: string[]): void }>()

const { t } = useI18n()

function toggle(scope: string, checked: boolean): void {
  if (checked && !props.modelValue.includes(scope)) {
    emit('update:modelValue', [...props.modelValue, scope])
  } else if (!checked) {
    emit('update:modelValue', props.modelValue.filter((s) => s !== scope))
  }
}
</script>

<template>
  <div class="flex flex-col gap-2">
    <label
      v-for="s in OIDC_SCOPES"
      :key="s.value"
      :data-test="`scope-row-${s.value}`"
      class="flex cursor-pointer items-start gap-3"
      :class="s.required ? 'cursor-default' : ''"
    >
      <Checkbox
        :model-value="modelValue.includes(s.value)"
        :disabled="s.required"
        :data-test="`scope-checkbox-${s.value}`"
        class="mt-0.5 shrink-0"
        @update:model-value="(c) => toggle(s.value, c === true)"
      />
      <span class="flex min-w-0 flex-col gap-0.5">
        <span class="font-mono text-sm text-ink">{{ s.value }}</span>
        <span class="text-xs text-muted" :data-test="`scope-desc-${s.value}`">{{ t(s.descKey) }}</span>
      </span>
    </label>
  </div>
</template>
