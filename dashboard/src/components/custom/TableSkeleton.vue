<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { Skeleton } from '@/components/ui/skeleton'

const props = withDefaults(defineProps<{ rows?: number; cols?: number }>(), { rows: 5, cols: 4 })
const { t } = useI18n()
</script>

<template>
  <div role="status" aria-busy="true" aria-live="polite" class="w-full">
    <div
      v-for="row in props.rows"
      :key="row"
      class="flex items-center gap-3 border-b px-2 py-3"
    >
      <Skeleton v-for="col in props.cols" :key="col" class="h-4 flex-1" />
    </div>
    <span class="sr-only">{{ t('common.loading') }}</span>
  </div>
</template>
