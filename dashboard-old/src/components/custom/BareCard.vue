<script setup lang="ts">
import type { HTMLAttributes } from "vue"
import { Card } from "@/components/ui/card"
import { cn } from "@/lib/utils"

const props = defineProps<{
  class?: HTMLAttributes["class"]
}>()
</script>

<template>
  <Card data-untitled="true" :class="cn('py-1', props.class)">
    <slot />
  </Card>
</template>
