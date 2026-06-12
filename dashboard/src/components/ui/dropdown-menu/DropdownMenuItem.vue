<script setup lang="ts">
import type { DropdownMenuItemEmits, DropdownMenuItemProps } from "reka-ui"
import type { HTMLAttributes } from "vue"
import { reactiveOmit } from "@vueuse/core"
import { DropdownMenuItem, useForwardPropsEmits } from "reka-ui"
import { cn } from "@/lib/utils"

const props = defineProps<DropdownMenuItemProps & { class?: HTMLAttributes["class"], inset?: boolean }>()
const emits = defineEmits<DropdownMenuItemEmits>()

const delegatedProps = reactiveOmit(props, "class", "inset")
const forwarded = useForwardPropsEmits(delegatedProps, emits)
</script>

<template>
  <DropdownMenuItem
    data-slot="dropdown-menu-item"
    :data-inset="inset ? '' : undefined"
    v-bind="forwarded"
    :class="cn(
      'focus:bg-accent focus:text-accent-foreground relative flex cursor-default items-center gap-2 rounded-sm px-2 py-1.5 text-sm outline-hidden select-none data-[disabled]:pointer-events-none data-[disabled]:opacity-50 data-[inset]:pl-8 [&>svg]:size-4 [&>svg]:shrink-0',
      props.class,
    )"
  >
    <slot />
  </DropdownMenuItem>
</template>
