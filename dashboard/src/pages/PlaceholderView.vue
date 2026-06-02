<script setup lang="ts">
// TODO(i18n): English-literal scaffold copy; key + translate after the design-system pass.
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import StatusBadge from '../components/StatusBadge.vue'

const route = useRoute()
const title = computed(() => (route.meta.title as string) ?? 'Planned')
const summary = computed(() => (route.meta.summary as string) ?? '')
</script>

<template>
  <UCard class="max-w-xl">
    <template #header>
      <div class="flex items-center gap-2">
        <h1 class="text-lg font-semibold">{{ title }}</h1>
        <StatusBadge kind="planned" />
      </div>
    </template>
    <p class="text-sm text-muted">{{ summary }}</p>
    <p class="text-sm text-muted mt-2">Not yet implemented.</p>
  </UCard>
</template>
