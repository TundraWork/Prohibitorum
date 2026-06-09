<script setup lang="ts">
/**
 * RadioCardGroup — all-visible single-select as labelled cards (title + description).
 * For small option sets that need explaining (e.g. provisioning mode). Built on reka
 * RadioGroup with `as-child` so the card itself is the radio control (one tab stop,
 * arrow-key nav, focus ring, data-[state=checked] for free).
 */
import { RadioGroupRoot, RadioGroupItem } from 'reka-ui'
import { cn } from '@/lib/utils'

export interface RadioCardOption {
  value: string
  title: string
  description?: string
}

defineProps<{
  modelValue?: string
  options: RadioCardOption[]
  ariaLabel?: string
}>()
defineEmits<{ (e: 'update:modelValue', value: string): void }>()
</script>

<template>
  <RadioGroupRoot
    :model-value="modelValue"
    :aria-label="ariaLabel"
    class="grid gap-2"
    @update:model-value="$emit('update:modelValue', String($event))"
  >
    <RadioGroupItem
      v-for="o in options"
      :key="o.value"
      :value="o.value"
      as-child
      :data-test="`radio-card-${o.value}`"
    >
      <div
        :class="cn(
          'group flex cursor-pointer items-start gap-3 rounded-md border border-input bg-sunken p-3 text-left transition-colors outline-none',
          'hover:border-ring',
          'focus-visible:border-ring focus-visible:ring-ring/50 focus-visible:ring-[3px]',
          'data-[state=checked]:border-primary data-[state=checked]:bg-bg',
        )"
      >
        <span class="mt-0.5 grid size-4 shrink-0 place-items-center rounded-full border border-input group-data-[state=checked]:border-primary">
          <span class="size-2 rounded-full bg-primary opacity-0 transition-opacity group-data-[state=checked]:opacity-100" />
        </span>
        <span class="flex min-w-0 flex-col gap-0.5">
          <span class="text-sm font-medium text-ink">{{ o.title }}</span>
          <span v-if="o.description" class="text-sm text-muted">{{ o.description }}</span>
        </span>
      </div>
    </RadioGroupItem>
  </RadioGroupRoot>
</template>
