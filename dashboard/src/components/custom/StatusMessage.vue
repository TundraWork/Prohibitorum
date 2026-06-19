<script setup lang="ts">
/**
 * StatusMessage — a transient success confirmation ("Saved", "Created",
 * "Rotated") announced to screen readers.
 *
 * The container is ALWAYS rendered as a polite live region so it stays in the
 * accessibility tree; only its text content toggles (empty → message). That
 * empty→text change is what assistive tech announces — a plain `v-if`/`v-show`
 * adds the region and its text together, which most screen readers skip. When
 * idle the region is `sr-only` (position:absolute), so it has zero visual AND
 * zero layout footprint: no flex-gap drift, no reserved blank line.
 */
import type { HTMLAttributes } from 'vue'
import { cn } from '@/lib/utils'

const props = withDefaults(
  defineProps<{ show?: boolean; class?: HTMLAttributes['class'] }>(),
  { show: false },
)
</script>

<template>
  <p role="status" :class="show ? cn('text-sm text-sage-700', props.class) : 'sr-only'">
    <template v-if="show"><slot /></template>
  </p>
</template>
