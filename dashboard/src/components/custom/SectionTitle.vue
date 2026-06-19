<script setup lang="ts">
/**
 * SectionTitle — the section / sub-section heading tier (16px / 600), one step
 * below CardTitle (18px / 600) and one step above the field-label tier
 * (14px / 500). Enforces the DESIGN.md title tier so a heading never collapses
 * into the label tier (the recurring "heading == label" collision the audit
 * caught). Mirrors the inner heading of FormSection.
 *
 * Pass `as` to set the heading level for a correct document outline — e.g.
 * `as="h3"` for a sub-section nested under a CardTitle (which renders an h2).
 */
import type { HTMLAttributes } from 'vue'
import { cn } from '@/lib/utils'

const props = withDefaults(
  defineProps<{ as?: string; class?: HTMLAttributes['class'] }>(),
  { as: 'h3' },
)
</script>

<template>
  <component :is="props.as" :class="cn('text-base font-semibold text-ink', props.class)">
    <slot />
  </component>
</template>
