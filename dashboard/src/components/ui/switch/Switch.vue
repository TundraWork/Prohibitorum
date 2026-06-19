<script setup lang="ts">
import type { SwitchRootEmits, SwitchRootProps } from "reka-ui"
import type { HTMLAttributes } from "vue"
import { SwitchRoot, SwitchThumb, useForwardPropsEmits } from "reka-ui"
import { computed } from "vue"
import { cn } from "@/lib/utils"

const props = defineProps<SwitchRootProps & { class?: HTMLAttributes["class"] }>()
const emits = defineEmits<SwitchRootEmits>()

const delegatedProps = computed(() => {
  const { class: _, ...rest } = props
  return rest
})
const forwarded = useForwardPropsEmits(delegatedProps, emits)
</script>

<template>
  <!--
    The Root is 24px tall (WCAG 2.5.8 hit target); the visible 20px track is an inner
    element styled off the Root's data-state via group-* variants.
  -->
  <SwitchRoot
    data-slot="switch"
    v-bind="forwarded"
    :class="cn(
      'group peer inline-flex h-6 w-9 shrink-0 cursor-pointer items-center outline-none',
      'disabled:cursor-not-allowed disabled:opacity-50',
      props.class,
    )"
  >
    <span
      class="pointer-events-none relative inline-flex h-5 w-9 items-center rounded-full border border-transparent shadow-xs transition-colors
        group-focus-visible:ring-ring/50 group-focus-visible:ring-[3px]
        group-data-[state=checked]:bg-primary group-data-[state=unchecked]:bg-border"
    >
      <SwitchThumb
        data-slot="switch-thumb"
        :class="cn(
          'pointer-events-none block size-4 rounded-full bg-bg shadow-sm ring-0 transition-transform',
          'data-[state=checked]:translate-x-4 data-[state=unchecked]:translate-x-0',
        )"
      />
    </span>
  </SwitchRoot>
</template>
