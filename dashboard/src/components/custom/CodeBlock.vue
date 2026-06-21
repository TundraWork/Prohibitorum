<script setup lang="ts">
/** CodeBlock — a multiline monospace block with a copy-to-clipboard button. */
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
    <div class="relative rounded-md border border-border bg-sunken">
      <Button type="button" variant="ghost" size="sm" class="absolute right-1.5 top-1.5"
              :aria-label="t('common.copy')" @click="copy">
        <component :is="copied ? Check : Copy" class="size-4" aria-hidden="true" />
        <span>{{ copied ? t('common.copied') : t('common.copy') }}</span>
      </Button>
      <pre class="overflow-x-auto px-3 py-2 pr-24 font-mono text-xs text-ink"><code>{{ value }}</code></pre>
    </div>
  </div>
</template>
