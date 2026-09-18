<script setup lang="ts">
import type { CheckboxRootEmits, CheckboxRootProps } from "reka-ui"
import type { HTMLAttributes } from "vue"
import { CheckboxIndicator, CheckboxRoot, useForwardPropsEmits } from "reka-ui"
import { Check } from "lucide-vue-next"
import { computed } from "vue"
import { cn } from "@/lib/utils"

const props = defineProps<CheckboxRootProps & { class?: HTMLAttributes["class"] }>()
const emits = defineEmits<CheckboxRootEmits>()

const delegatedProps = computed(() => {
  const { class: _, ...rest } = props
  return rest
})
const forwarded = useForwardPropsEmits(delegatedProps, emits)
</script>

<template>
  <!--
    The Root is a 24px-square hit target (WCAG 2.5.8); the visible 16px box is an
    inner element styled off the Root's data-state via group-* variants. Keeps the
    control visually 16px while the tap/click area clears 24px.
  -->
  <CheckboxRoot
    data-slot="checkbox"
    v-bind="forwarded"
    :class="cn(
      'group peer inline-flex size-6 shrink-0 items-center justify-center outline-none',
      'cursor-pointer disabled:cursor-not-allowed disabled:opacity-50',
      props.class,
    )"
  >
    <span
      class="flex size-4 items-center justify-center rounded-[4px] border border-input bg-bg text-primary-foreground shadow-xs transition-shadow
        group-focus-visible:border-ring group-focus-visible:ring-ring/50 group-focus-visible:ring-[3px]
        group-data-[state=checked]:bg-primary group-data-[state=checked]:border-primary
        group-data-[state=indeterminate]:bg-primary group-data-[state=indeterminate]:border-primary"
    >
      <CheckboxIndicator
        data-slot="checkbox-indicator"
        class="flex items-center justify-center text-current"
      >
        <slot>
          <Check class="size-3.5" />
        </slot>
      </CheckboxIndicator>
    </span>
  </CheckboxRoot>
</template>
