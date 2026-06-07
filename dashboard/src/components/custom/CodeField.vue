<script setup lang="ts">
/** CodeField — a monospace value with a copy-to-clipboard button. */
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Copy, Check } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'

const props = defineProps<{ value: string; label?: string }>()
const { t } = useI18n()
const copied = ref(false)

async function copy(): Promise<void> {
  try {
    await navigator.clipboard.writeText(props.value)
    copied.value = true
    setTimeout(() => { copied.value = false }, 1500)
  } catch {
    /* clipboard blocked — no-op; value is visible for manual copy */
  }
}
</script>

<template>
  <div class="flex flex-col gap-1">
    <span v-if="label" class="text-xs text-muted">{{ label }}</span>
    <div class="flex items-center gap-2 rounded-md border border-border bg-sunken px-3 py-2">
      <code class="min-w-0 flex-1 truncate font-mono text-sm text-ink">{{ value }}</code>
      <Button type="button" variant="ghost" size="sm" class="shrink-0" :aria-label="t('common.copy')" @click="copy">
        <component :is="copied ? Check : Copy" class="size-4" aria-hidden="true" />
        <span>{{ copied ? t('common.copied') : t('common.copy') }}</span>
      </Button>
    </div>
  </div>
</template>
