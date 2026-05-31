<script setup lang="ts">
import { useI18n } from 'vue-i18n'

defineProps<{ scopes: string[] }>()

const { t, te } = useI18n()

// Localized scope label when a key exists; otherwise the raw scope name
// (so unknown/custom scopes still render).
function scopeLabel(s: string): string {
  return te(`scopes.${s}`) ? t(`scopes.${s}`) : s
}
</script>

<template>
  <ul class="flex flex-col gap-2">
    <li v-for="s in scopes" :key="s" class="flex items-center gap-2">
      <UIcon name="i-lucide-shield-check" class="size-5 text-primary shrink-0" />
      <span class="text-sm">{{ scopeLabel(s) }}</span>
    </li>
  </ul>
</template>
