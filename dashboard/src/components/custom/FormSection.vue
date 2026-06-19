<script setup lang="ts">
/**
 * FormSection — a titled group of related fields within a form card. Separates groups
 * with a hairline (no nested cards); the first section omits the top divider.
 */
defineProps<{
  title: string
  description?: string
}>()
</script>

<template>
  <section class="flex flex-col gap-3 border-t border-border pt-4 first:border-t-0 first:pt-0">
    <div class="flex flex-col gap-0.5">
      <h3 class="text-base font-semibold text-ink">{{ title }}</h3>
      <p v-if="description" class="text-sm text-muted">{{ description }}</p>
    </div>
    <slot />
  </section>
</template>
