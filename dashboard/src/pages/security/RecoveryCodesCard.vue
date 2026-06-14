<script setup lang="ts">
/** RecoveryCodesCard — regenerate recovery codes (sudo-gated; needs confirmed TOTP). */
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { withSudo } from '@/lib/sudo'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Alert, AlertDescription } from '@/components/ui/alert'
import RecoveryCodesDisplay from '@/components/custom/RecoveryCodesDisplay.vue'
import StatusBadge from '@/components/custom/StatusBadge.vue'

const props = defineProps<{ remaining?: number }>()
const emit = defineEmits<{ (e: 'changed'): void }>()

const { t, te } = useI18n()
const { busy, error, run } = useApi()
const codes = ref<string[]>([])

const errorText = computed(() => {
  const e = error.value
  if (!e) return ''
  if (e.code === 'bad_request') return t('security.recovery.needTotp')
  const key = `errors.${e.code}`
  return te(key) ? t(key) : e.message || t('common.error')
})

async function regenerate(): Promise<void> {
  const r = await run(() => withSudo(() =>
    api.post<{ recovery_codes: string[] }>('/api/prohibitorum/me/recovery-codes/regenerate')))
  if (r) { codes.value = r.recovery_codes ?? []; emit('changed') }
}
</script>

<template>
  <Card>
    <CardHeader>
      <CardTitle class="flex items-center gap-2">
        {{ t('security.recovery.title') }}
        <StatusBadge
          v-if="props.remaining !== undefined"
          :variant="props.remaining > 4 ? 'success' : props.remaining > 0 ? 'caution' : 'danger'"
        >
          {{ t('security.factors.recoveryRemaining', { n: props.remaining }) }}
        </StatusBadge>
      </CardTitle>
    </CardHeader>
    <CardContent class="flex flex-col gap-4">
      <RecoveryCodesDisplay v-if="codes.length" :codes="codes" regenerated @confirmed="codes = []" />
      <template v-else>
        <p class="text-sm text-muted">{{ t('security.recovery.help') }}</p>
        <Button type="button" variant="outline" class="w-fit" :disabled="busy" @click="regenerate">
          {{ t('security.recovery.regenerate') }}
        </Button>
      </template>
      <Alert v-if="errorText" variant="destructive" role="alert" aria-live="polite">
        <AlertDescription>{{ errorText }}</AlertDescription>
      </Alert>
    </CardContent>
  </Card>
</template>
