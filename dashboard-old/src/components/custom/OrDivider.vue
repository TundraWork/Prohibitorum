<script setup lang="ts">
/** OrDivider — a labeled hairline separating auth-method groups ("or"). */
defineProps<{ label?: string }>()
</script>

<template>
  <div class="flex items-center gap-3" aria-hidden="true">
    <span class="h-px flex-1 bg-border" />
    <span v-if="label" class="text-xs font-medium uppercase tracking-wide text-muted">{{ label }}</span>
    <span class="h-px flex-1 bg-border" />
  </div>
</template>
