<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { formatUserAgent } from '@/lib/userAgent'
import { ChevronDown } from 'lucide-vue-next'

const props = defineProps<{
  ua?: string
}>()

const { t } = useI18n()
const expanded = ref(false)

const hasRawUa = !!props.ua
</script>

<template>
  <div>
    <div class="flex items-center gap-1">
      <span>{{ formatUserAgent(ua) }}</span>
      <button
        v-if="hasRawUa"
        type="button"
        class="inline-flex shrink-0 cursor-pointer items-center rounded p-0.5 text-muted transition-colors hover:text-ink focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        :aria-expanded="expanded"
        :aria-label="expanded ? t('userAgent.hide') : t('userAgent.toggle')"
        @click="expanded = !expanded"
      >
        <ChevronDown
          class="size-3.5 transition-transform duration-150"
          :class="expanded ? 'rotate-180' : ''"
          aria-hidden="true"
        />
      </button>
    </div>
    <p v-if="expanded && hasRawUa" class="mt-1 break-all font-mono text-xs text-muted">{{ ua }}</p>
  </div>
</template>
