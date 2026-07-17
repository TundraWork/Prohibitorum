<script setup lang="ts">
/** CodeField — a monospace value with a copy-to-clipboard button. */
import { onBeforeUnmount, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Copy, Check } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'

const props = withDefaults(defineProps<{
  value: string
  label?: string
  copyLabel?: string
  wrap?: boolean
}>(), {
  label: undefined,
  copyLabel: undefined,
  wrap: false,
})
const { t } = useI18n()
const copyState = ref<'idle' | 'copied' | 'failed'>('idle')
let resetTimer: ReturnType<typeof setTimeout> | undefined

onBeforeUnmount(() => {
  if (resetTimer !== undefined) clearTimeout(resetTimer)
})

async function copy(): Promise<void> {
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
  <div class="flex min-w-0 flex-col gap-1">
    <span v-if="label" class="text-xs text-muted">{{ label }}</span>
    <div class="flex min-w-0 flex-col items-stretch gap-2 rounded-md border border-border bg-sunken px-3 py-2 sm:flex-row sm:items-center">
      <code
        class="min-w-0 flex-1 select-all font-mono text-sm text-ink"
        :class="wrap ? 'break-all whitespace-normal' : 'truncate'"
      >{{ value }}</code>
      <Button
        type="button"
        variant="ghost"
        size="sm"
        class="min-h-11 w-full shrink-0 sm:w-auto"
        :aria-label="copyLabel ?? t('common.copy')"
        data-test="copy-code"
        @click="copy"
      >
        <component :is="copyState === 'copied' ? Check : Copy" class="size-4" aria-hidden="true" />
        <span>{{ copyState === 'copied' ? t('common.copied') : t('common.copy') }}</span>
      </Button>
    </div>
    <p
      role="status"
      :class="copyState === 'failed' ? 'text-sm text-rose-700' : 'sr-only'"
    >
      {{ copyState === 'failed' ? t('common.copyFailed') : copyState === 'copied' ? t('common.copied') : '' }}
    </p>
  </div>
</template>
