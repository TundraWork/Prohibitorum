<script setup lang="ts">
/**
 * SegmentedControl — all-visible single-select as horizontal segments, for small
 * plain option sets (2-4) inside a save-form (role, SAML ACS binding). Built on reka
 * RadioGroup (radio semantics, click-activated) rather than Tabs (which is for
 * view-switching and activates on mousedown).
 */
import { RadioGroupRoot, RadioGroupItem } from 'reka-ui'
import { cn } from '@/lib/utils'

export interface SegmentOption {
  value: string
  label: string
}

withDefaults(
  defineProps<{
    modelValue?: string
    options: SegmentOption[]
    ariaLabel?: string
    size?: 'default' | 'sm'
  }>(),
  { size: 'default' },
)
defineEmits<{ (e: 'update:modelValue', value: string): void }>()
</script>

<template>
  <RadioGroupRoot
    :model-value="modelValue"
    :aria-label="ariaLabel"
    orientation="horizontal"
    class="inline-flex w-full rounded-lg bg-sunken p-0.5"
    @update:model-value="$emit('update:modelValue', String($event))"
  >
    <RadioGroupItem
      v-for="o in options"
      :key="o.value"
      :value="o.value"
      as-child
      :data-test="`segment-${o.value}`"
    >
      <button
        type="button"
        :class="cn(
          'inline-flex flex-1 items-center justify-center rounded-md px-3 py-1 text-sm font-medium whitespace-nowrap text-muted transition-[color,box-shadow] outline-none cursor-pointer',
          'hover:text-ink',
          'focus-visible:ring-ring/50 focus-visible:ring-[3px]',
          'data-[state=checked]:bg-bg data-[state=checked]:text-ink data-[state=checked]:shadow-sm',
          size === 'sm' && 'px-2 py-0.5 text-xs',
        )"
      >
        {{ o.label }}
      </button>
    </RadioGroupItem>
  </RadioGroupRoot>
</template>
