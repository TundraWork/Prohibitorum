<script setup lang="ts">
/**
 * SettingRow — a setting field stacked like every other field: the label on top, the
 * control (default slot, e.g. a Switch) below it, then an optional description. Keeps
 * the control directly under its label (no width cap needed) and associates the label
 * to the control via `for`/`id`.
 */
import { Label } from '@/components/ui/label'

const props = defineProps<{
  label: string
  description?: string
  for: string
}>()
</script>

<template>
  <div class="flex flex-col gap-1.5">
    <Label :for="props.for" class="font-normal text-ink">{{ label }}</Label>
    <div class="flex">
      <slot />
    </div>
    <p v-if="description" class="text-xs text-muted">{{ description }}</p>
  </div>
</template>
