<script setup lang="ts">
/**
 * CopyButton — an icon-only copy button for an already-visible value.
 * Unlike CodeField (a sunken box for one-time codes), this renders no
 * frame: it sits beside a value the reader can already see and select.
 */
import { onBeforeUnmount, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Copy, Check } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'

const props = defineProps<{
  value: string
  /** Accessible name; used verbatim as the button's aria-label and title. */
  label: string
}>()
const { t } = useI18n()
const copyState = ref<'idle' | 'copied' | 'failed'>('idle')
let resetTimer: ReturnType<typeof setTimeout> | undefined

onBeforeUnmount(() => {
  if (resetTimer !== undefined) clearTimeout(resetTimer)
})

async function copy(): Promise<void> {
  // A failure must survive a timer armed by an earlier success, so the
  // timer is always disarmed before any state change.
  if (resetTimer !== undefined) clearTimeout(resetTimer)
  copyState.value = 'idle'
  try {
    await navigator.clipboard.writeText(props.value)
    copyState.value = 'copied'
    resetTimer = setTimeout(() => { copyState.value = 'idle' }, 1500)
  } catch {
    copyState.value = 'failed'
  }
}
</script>

<template>
  <span class="inline-flex items-center gap-2">
    <Button
      type="button"
      variant="ghost"
      size="icon-sm"
      class="shrink-0"
      :aria-label="props.label"
      :title="props.label"
      data-test="copy-button"
      @click="copy"
    >
      <component :is="copyState === 'copied' ? Check : Copy" class="size-4" aria-hidden="true" />
    </Button>
    <span
      role="status"
      :class="copyState === 'failed' ? 'text-sm text-rose-700' : 'sr-only'"
    >
      {{ copyState === 'failed' ? t('common.copyFailed') : copyState === 'copied' ? t('common.copied') : '' }}
    </span>
  </span>
</template>
