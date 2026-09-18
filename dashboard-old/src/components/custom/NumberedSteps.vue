<script setup lang="ts">
/**
 * NumberedSteps — an ordered list rendered with circular index badges, the
 * shared step treatment for the threshold verification flows. A step may carry
 * an `href` to render its text as a link (e.g. "Open the VRChat website…"), so
 * every step list — across the identify and proof stages — stays identical
 * instead of drifting into a parallel idiom.
 */
import { ExternalLink } from 'lucide-vue-next'

interface NumberedStep {
  text: string
  href?: string
  target?: string
  test?: string
}

defineProps<{ steps: NumberedStep[]; label?: string }>()
</script>

<template>
  <ol class="grid gap-3" :aria-label="label">
    <li
      v-for="(step, index) in steps"
      :key="step.text"
      class="grid grid-cols-[1.75rem_minmax(0,1fr)] items-start gap-2 text-sm leading-5 text-ink"
    >
      <span
        aria-hidden="true"
        class="inline-flex size-7 items-center justify-center rounded-full bg-tide/10 text-xs font-semibold text-tide-strong"
      >{{ index + 1 }}</span>
      <a
        v-if="step.href"
        :href="step.href"
        :target="step.target ?? '_blank'"
        rel="noopener noreferrer"
        :data-test="step.test"
        class="inline-flex w-fit items-center gap-1.5 pt-1 font-medium text-tide-strong underline underline-offset-4 outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
      >
        {{ step.text }}
        <ExternalLink class="size-4 shrink-0" aria-hidden="true" />
      </a>
      <span v-else class="pt-1">{{ step.text }}</span>
    </li>
  </ol>
</template>
