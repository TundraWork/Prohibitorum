<script setup lang="ts">
/**
 * FormSection — a titled group of related fields within a form card. Separates groups
 * with a hairline (no nested cards); the first section omits the top divider.
 * Renders an <h3> by default — a sub-section under the card's CardTitle (h2); pass `as`
 * to override the heading level for a correct document outline.
 */
withDefaults(
  defineProps<{
    title: string
    description?: string
    as?: string
  }>(),
  { as: 'h3' },
)
</script>

<template>
  <section class="flex flex-col gap-3 border-t border-border pt-4 first:border-t-0 first:pt-0">
    <div class="flex flex-col gap-0.5">
      <component :is="as" class="text-base font-semibold text-ink">{{ title }}</component>
      <p v-if="description" class="text-sm text-muted">{{ description }}</p>
    </div>
    <slot />
  </section>
</template>
