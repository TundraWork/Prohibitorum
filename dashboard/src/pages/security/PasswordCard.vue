<script setup lang="ts">
/** PasswordCard — set/replace the password (always sudo-gated server-side). */
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { useTransientFlag } from '@/composables/useTransientFlag'
import { withSudo } from '@/lib/sudo'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Alert, AlertDescription } from '@/components/ui/alert'
import StatusBadge from '@/components/custom/StatusBadge.vue'

const props = defineProps<{ set?: boolean }>()
const emit = defineEmits<{ (e: 'changed'): void }>()

const { t, te } = useI18n()
const { busy, error, run } = useApi()
const pw = ref('')
const confirm = ref('')
const localError = ref('')
const { flag: done, trigger: triggerDone } = useTransientFlag()

const errorText = computed(() => {
  if (localError.value) return localError.value
  const e = error.value
  if (!e) return ''
  const key = `errors.${e.code}`
  return te(key) ? t(key) : e.message || t('common.error')
})

async function submit(): Promise<void> {
  localError.value = ''
  if (pw.value.length < 8) { localError.value = t('security.password.tooShort'); return }
  if (pw.value !== confirm.value) { localError.value = t('security.password.mismatch'); return }
  const ok = await run(() => withSudo(async () => {
    await api.post('/api/prohibitorum/me/password/set', { password: pw.value })
    return true as const
  }, t('sudo.reason.setPassword')))
  if (ok) { triggerDone(); pw.value = ''; confirm.value = ''; emit('changed') }
}
</script>

<template>
  <Card>
    <CardHeader class="flex flex-row items-center gap-2">
      <CardTitle>{{ t('security.password.title') }}</CardTitle>
      <StatusBadge v-if="props.set === undefined" variant="neutral">—</StatusBadge>
      <StatusBadge v-else :variant="props.set ? 'success' : 'neutral'">
        {{ props.set ? t('security.factors.passwordSet') : t('security.factors.passwordUnset') }}
      </StatusBadge>
    </CardHeader>
    <CardContent>
      <form class="flex max-w-sm flex-col gap-4" @submit.prevent="submit">
        <p class="text-sm text-muted">{{ t('security.password.help') }}</p>
        <div class="flex flex-col gap-1.5">
          <Label for="pw-new">{{ t('security.password.newLabel') }}</Label>
          <Input id="pw-new" v-model="pw" name="new_password" type="password" autocomplete="new-password" required />
        </div>
        <div class="flex flex-col gap-1.5">
          <Label for="pw-confirm">{{ t('security.password.confirmLabel') }}</Label>
          <Input id="pw-confirm" v-model="confirm" name="confirm_password" type="password" autocomplete="new-password" required />
        </div>
        <Alert v-if="errorText" variant="destructive" role="alert" aria-live="polite">
          <AlertDescription>{{ errorText }}</AlertDescription>
        </Alert>
        <p v-if="done" class="text-sm text-sage" role="status">{{ t('security.password.saved') }}</p>
        <Button type="submit" :disabled="busy">{{ t('security.password.submit') }}</Button>
      </form>
    </CardContent>
  </Card>
</template>
