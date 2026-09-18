<script setup lang="ts">
/**
 * PaginationControls — accessible next/previous navigation for cursor-paginated
 * admin collections.
 *
 * Props:
 *   pageIndex   — 0-based page index (0 = first page)
 *   hasMore     — whether nextCursor is non-empty (a next page exists)
 *   busy        — whether a fetch is in progress
 *   hasItems    — whether the current page has items (controls visibility when
 *                 on the final page with no items)
 *   showIndicator — whether to show the "Page N" indicator (default true)
 *
 * Emits:
 *   next, previous — navigation events
 *
 * Accessibility:
 *   - <nav role="navigation" aria-label="Pagination"> wraps the controls
 *   - Both buttons have descriptive aria-labels
 *   - The page indicator has aria-current="page"
 *   - Disabled states are communicated via the disabled attribute
 *   - After clicking Previous, focus moves to the Next button (or Previous if
 *     still on a non-first page) so keyboard users land on a sensible element
 */
import { ref, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button } from '@/components/ui/button'
import { ChevronLeft, ChevronRight } from 'lucide-vue-next'

const props = withDefaults(defineProps<{
  pageIndex: number
  hasMore: boolean
  busy: boolean
  hasItems?: boolean
  showIndicator?: boolean
}>(), {
  hasItems: true,
  showIndicator: true,
})

const emit = defineEmits<{
  next: []
  previous: []
}>()

const { t } = useI18n()


const prevBtn = ref<InstanceType<typeof Button> | null>(null)
const nextBtn = ref<InstanceType<typeof Button> | null>(null)

const prevDisabled = () => props.busy || props.pageIndex <= 0
const nextDisabled = () => props.busy || !props.hasMore

// Render nothing when there are no items and no more pages (truly empty).
const shouldRender = () => props.hasItems || props.hasMore || props.pageIndex > 0

async function onPrev(): Promise<void> {
  if (prevDisabled()) return
  emit('previous')
  await nextTick()
  // After going back, focus the Next button (or Previous if still mid-list)
  // so keyboard users land on a predictable element.
  if (props.pageIndex > 0) {
    prevBtn.value?.$el?.focus()
  } else {
    nextBtn.value?.$el?.focus()
  }
}

function onNext(): void {
  if (nextDisabled()) return
  emit('next')
}
</script>

<template>
  <nav
    v-if="shouldRender()"
    data-test="pagination-controls"
    role="navigation"
    :aria-label="t('common.pagination.label')"
    class="flex items-center gap-3"
  >
    <Button
      ref="prevBtn"
      type="button"
      variant="outline"
      :disabled="prevDisabled()"
      data-test="prev-page"
      :aria-label="t('common.pagination.previous')"
      @click="onPrev"
    >
      <ChevronLeft class="size-4" />
      {{ t('common.pagination.previous') }}
    </Button>
    <span
      v-if="showIndicator"
      data-test="page-indicator"
      aria-current="page"
      class="text-sm text-muted"
    >{{ t('common.pagination.page', { n: pageIndex + 1 }) }}</span>
    <Button
      ref="nextBtn"
      type="button"
      variant="outline"
      :disabled="nextDisabled()"
      data-test="next-page"
      :aria-label="t('common.pagination.next')"
      @click="onNext"
    >
      {{ t('common.pagination.next') }}
      <ChevronRight class="size-4" />
    </Button>
  </nav>
</template>
