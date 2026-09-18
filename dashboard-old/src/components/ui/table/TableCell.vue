<script setup lang="ts">
import type { HTMLAttributes } from "vue"
import { cn } from "@/lib/utils"
const props = defineProps<{ class?: HTMLAttributes["class"] }>()
</script>
<template>
  <td data-slot="table-cell" :class="cn('p-2 align-middle', props.class)"><slot /></td>
</template>
