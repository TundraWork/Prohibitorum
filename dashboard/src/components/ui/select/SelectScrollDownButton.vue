<script setup lang="ts">
import type { SelectScrollDownButtonProps } from "reka-ui"
import type { HTMLAttributes } from "vue"
import { SelectScrollDownButton, useForwardProps } from "reka-ui"
import { ChevronDown } from "lucide-vue-next"
import { computed } from "vue"
import { cn } from "@/lib/utils"

const props = defineProps<SelectScrollDownButtonProps & { class?: HTMLAttributes["class"] }>()

const delegatedProps = computed(() => {
  const { class: _, ...rest } = props
  return rest
})
const forwarded = useForwardProps(delegatedProps)
</script>

<template>
  <SelectScrollDownButton
    data-slot="select-scroll-down-button"
    v-bind="forwarded"
    :class="cn('flex cursor-pointer items-center justify-center py-1', props.class)"
  >
    <ChevronDown class="size-4" />
  </SelectScrollDownButton>
</template>
