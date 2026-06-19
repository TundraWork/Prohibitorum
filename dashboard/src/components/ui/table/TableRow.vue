<script setup lang="ts">
import type { HTMLAttributes } from "vue"
import { cn } from "@/lib/utils"
const props = defineProps<{ class?: HTMLAttributes["class"] }>()
</script>
<template>
  <tr data-slot="table-row" :class="cn('hover:bg-sunken/60 border-b transition-colors focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-ring', props.class)"><slot /></tr>
</template>
