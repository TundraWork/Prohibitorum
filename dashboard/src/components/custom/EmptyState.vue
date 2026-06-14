<script setup lang="ts">
import type { Component } from 'vue'

defineProps<{
  icon?: Component
  title: string
  description?: string
}>()
</script>

<template>
  <div role="status" class="flex flex-col items-center gap-2 py-8 text-center">
    <component :is="icon" v-if="icon" class="size-8 text-muted" aria-hidden="true" />
    <p class="text-sm font-medium text-ink">{{ title }}</p>
    <p v-if="description" class="text-xs text-muted">{{ description }}</p>
    <slot />
  </div>
</template>
