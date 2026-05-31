<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{ url: string }>()
const { t } = useI18n()
const copied = ref(false)

async function copy() {
  try {
    await navigator.clipboard.writeText(props.url)
    copied.value = true
    setTimeout(() => { copied.value = false }, 1500)
  } catch {
    // Clipboard denied (e.g. insecure context): the read-only input still lets
    // the user select + copy manually.
  }
}
</script>

<template>
  <div class="flex items-center gap-2">
    <UInput :model-value="props.url" readonly class="flex-1 font-mono text-xs" />
    <UButton type="button" size="sm" variant="soft" @click="copy">
      {{ copied ? t('common.copied') : t('common.copy') }}
    </UButton>
  </div>
</template>
