<script setup lang="ts">
/**
 * SettingRow — a settings toggle row: label (+ optional description) on the left, the
 * control (default slot, e.g. a Switch) on the right. Keeps the label and control as a
 * tight pair within a constrained-width form, and associates the label to the control
 * via `for`/`id`.
 */
import { Label } from '@/components/ui/label'

const props = defineProps<{
  label: string
  description?: string
  for: string
}>()
</script>

<template>
  <div class="flex items-start justify-between gap-4 py-1">
    <div class="flex max-w-prose flex-col gap-0.5">
      <Label :for="props.for" class="font-normal text-ink">{{ label }}</Label>
      <p v-if="description" class="text-sm text-muted">{{ description }}</p>
    </div>
    <div class="shrink-0 pt-0.5">
      <slot />
    </div>
  </div>
</template>
