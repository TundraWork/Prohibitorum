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
        success: 'bg-sage-50 text-sage-700',
        caution: 'bg-amber-50 text-amber-700',
        danger: 'bg-rose-50 text-rose-700',
        info: 'bg-tide-50 text-tide-700',
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
