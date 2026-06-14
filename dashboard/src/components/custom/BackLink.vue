<script setup lang="ts">
import { ChevronLeft } from 'lucide-vue-next'

defineProps<{
  to: string
  label: string
}>()
</script>

<template>
  <RouterLink
    :to="to"
    class="inline-flex items-center gap-1 text-sm text-muted underline-offset-4 hover:underline hover:text-ink cursor-pointer rounded-sm focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring"
  >
    <ChevronLeft class="size-4" aria-hidden="true" />
    {{ label }}
  </RouterLink>
</template>
