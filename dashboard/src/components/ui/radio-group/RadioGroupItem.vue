<script setup lang="ts">
import type { RadioGroupItemProps } from "reka-ui"
import type { HTMLAttributes } from "vue"
import { RadioGroupIndicator, RadioGroupItem, useForwardProps } from "reka-ui"
import { Circle } from "lucide-vue-next"
import { computed } from "vue"
import { cn } from "@/lib/utils"

const props = defineProps<RadioGroupItemProps & { class?: HTMLAttributes["class"] }>()

const delegatedProps = computed(() => {
  const { class: _, ...rest } = props
  return rest
})
const forwarded = useForwardProps(delegatedProps)
</script>

<template>
  <!--
    The Root is a 24px-square hit target (WCAG 2.5.8); the visible 16px circle is an
    inner element styled off the Root's data-state via group-* variants.
  -->
  <RadioGroupItem
    data-slot="radio-group-item"
    v-bind="forwarded"
    :class="cn(
      'group peer inline-flex size-6 shrink-0 items-center justify-center outline-none',
      'cursor-pointer disabled:cursor-not-allowed disabled:opacity-50',
      props.class,
    )"
  >
    <span
      class="flex aspect-square size-4 items-center justify-center rounded-full border border-input bg-bg text-primary shadow-xs transition-shadow
        group-focus-visible:border-ring group-focus-visible:ring-ring/50 group-focus-visible:ring-[3px]
        group-data-[state=checked]:border-primary"
    >
      <RadioGroupIndicator
        data-slot="radio-group-indicator"
        class="relative flex items-center justify-center"
      >
        <Circle class="size-2 fill-primary text-primary" />
      </RadioGroupIndicator>
    </span>
  </RadioGroupItem>
</template>
