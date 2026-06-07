<script setup lang="ts">
/** StatusBadge — small token-styled status pill (State-Has-a-Color, DESIGN.md). */
import type { HTMLAttributes } from 'vue'
import { cva, type VariantProps } from 'class-variance-authority'
import { cn } from '@/lib/utils'

const badge = cva(
  'inline-flex items-center gap-1 whitespace-nowrap rounded-full px-2 py-0.5 text-xs font-medium',
  {
    variants: {
      variant: {
        neutral: 'bg-sunken text-muted',
        success: 'bg-sage/12 text-sage',
        caution: 'bg-amber/15 text-amber',
        danger: 'bg-rose/12 text-rose',
      },
    },
    defaultVariants: { variant: 'neutral' },
  },
)
type Props = { variant?: VariantProps<typeof badge>['variant']; class?: HTMLAttributes['class'] }
const props = defineProps<Props>()
</script>

<template>
  <span :class="cn(badge({ variant: props.variant }), props.class)"><slot /></span>
</template>
