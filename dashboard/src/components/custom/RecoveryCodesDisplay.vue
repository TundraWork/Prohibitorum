<script setup lang="ts">
/**
 * RecoveryCodesDisplay — present one-time recovery codes safely: mono grid,
 * copy-all + download, secure-storage guidance, and a save-confirmation gate
 * the parent honours before dismissing.
 */
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Copy, Check, Download } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Checkbox } from '@/components/ui/checkbox'
import SectionTitle from '@/components/custom/SectionTitle.vue'

const props = defineProps<{ codes: string[]; regenerated?: boolean }>()
const emit = defineEmits<{ confirmed: [] }>()
const { t } = useI18n()

const copied = ref(false)
const copying = ref(false)
const copyFailed = ref(false)
const saved = ref(false)

async function copyAll(): Promise<void> {
  copying.value = true
  copyFailed.value = false
  try {
    await navigator.clipboard.writeText(props.codes.join('\n'))
    copied.value = true
    setTimeout(() => { copied.value = false }, 1500)
  } catch {
    copyFailed.value = true
  } finally {
    copying.value = false
  }
}

function download(): void {
  const blob = new Blob([props.codes.join('\n') + '\n'], { type: 'text/plain' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = 'recovery-codes.txt'
  document.body.appendChild(a)
  a.click()
  a.remove()
  URL.revokeObjectURL(url)
}
</script>

<template>
  <div class="flex flex-col gap-4">
    <div class="flex flex-col gap-1">
      <SectionTitle>{{ t('recoveryCodes.heading') }}</SectionTitle>
      <p class="text-sm text-muted">{{ t('recoveryCodes.intro') }}</p>
    </div>

    <Alert v-if="regenerated" role="status">
      <AlertDescription>{{ t('recoveryCodes.regeneratedWarning') }}</AlertDescription>
    </Alert>

    <ul class="grid grid-cols-2 gap-2 rounded-md border border-border bg-sunken p-3">
      <li v-for="c in codes" :key="c" class="whitespace-nowrap font-mono text-sm text-ink">{{ c }}</li>
    </ul>

    <div class="flex flex-wrap gap-2">
      <Button type="button" variant="outline" size="sm" :aria-busy="copying" @click="copyAll">
        <component :is="copied ? Check : Copy" class="size-4" aria-hidden="true" />
        <span>{{ copied ? t('common.copied') : t('recoveryCodes.copyAll') }}</span>
      </Button>
      <Button type="button" variant="outline" size="sm" @click="download">
        <Download class="size-4" aria-hidden="true" />
        <span>{{ t('recoveryCodes.download') }}</span>
      </Button>
    </div>
    <p v-if="copyFailed" class="text-xs text-destructive" role="alert">{{ t('recoveryCodes.copyFailed') }}</p>

    <p class="text-xs text-muted">{{ t('recoveryCodes.storage') }}</p>

    <label class="flex items-center gap-2 text-sm text-ink">
      <Checkbox v-model="saved" data-test="saved" />
      <span>{{ t('recoveryCodes.savedConfirm') }}</span>
    </label>

    <Button type="button" class="w-full" :disabled="!saved" data-test="done" @click="emit('confirmed')">
      {{ t('recoveryCodes.done') }}
    </Button>
  </div>
</template>
