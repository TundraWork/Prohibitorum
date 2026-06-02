<script setup lang="ts">
const props = defineProps<{ kind: 'planned' | 'stub' | 'beta' }>()
const color: Record<string, string> = { planned: 'neutral', stub: 'warning', beta: 'primary' }
</script>

<template>
  <UBadge :color="color[props.kind]" variant="subtle" size="sm">{{ props.kind }}</UBadge>
</template>
