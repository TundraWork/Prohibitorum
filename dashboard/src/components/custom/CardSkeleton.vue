<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { Skeleton } from '@/components/ui/skeleton'

const props = withDefaults(defineProps<{ lines?: number }>(), { lines: 4 })
const { t } = useI18n()
</script>

<template>
  <div role="status" aria-busy="true" class="flex w-full flex-col gap-3">
    <Skeleton class="h-6 w-1/3" />
    <template v-for="n in props.lines" :key="n">
      <Skeleton :class="n === props.lines ? 'h-4 w-2/3' : 'h-4 w-full'" />
    </template>
    <span class="sr-only">{{ t('common.loading') }}</span>
  </div>
</template>
