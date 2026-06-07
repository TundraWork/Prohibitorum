<script setup lang="ts">
/**
 * TotpCard — enroll a TOTP authenticator. begin (sudo-gated when re-enrolling)
 * returns secret+otpauth; the backend persists only on verify. First
 * enrollment returns recovery codes.
 */
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { withSudo } from '@/lib/sudo'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Alert, AlertDescription } from '@/components/ui/alert'
import CodeField from '@/components/custom/CodeField.vue'
import TotpQr from '@/components/custom/TotpQr.vue'
import RecoveryCodesDisplay from '@/components/custom/RecoveryCodesDisplay.vue'

const { t, te } = useI18n()
const { busy, error, run } = useApi()
const secret = ref('')
const otpauth = ref('')
const code = ref('')
const recovery = ref<string[]>([])
const enabled = ref(false)

const errorText = computed(() => {
  const e = error.value
  if (!e) return ''
  const key = `errors.${e.code}`
  return te(key) ? t(key) : e.message || t('common.error')
})

async function setup(): Promise<void> {
  const r = await run(() => withSudo(() =>
    api.post<{ secret_base32: string; otpauth_uri: string }>('/api/prohibitorum/me/totp/begin')))
  if (!r) return
  secret.value = r.secret_base32
  otpauth.value = r.otpauth_uri
  recovery.value = []
  enabled.value = false
}

async function verify(): Promise<void> {
  const r = await run(() => withSudo(() =>
    api.post<{ recovery_codes?: string[] } | undefined>('/api/prohibitorum/me/totp/verify', { code: code.value })))
  // 204 (re-enroll) → undefined; first enrollment → { recovery_codes }
  if (error.value) return
  enabled.value = true
  secret.value = ''; otpauth.value = ''; code.value = ''
  if (r && r.recovery_codes) recovery.value = r.recovery_codes
}
</script>

<template>
  <Card>
    <CardHeader><CardTitle>{{ t('security.totp.title') }}</CardTitle></CardHeader>
    <CardContent class="flex flex-col gap-4">
      <p class="text-sm text-muted">{{ t('security.totp.help') }}</p>

      <RecoveryCodesDisplay v-if="recovery.length" :codes="recovery" @confirmed="recovery = []" />

      <template v-else-if="!secret">
        <p v-if="enabled" class="text-sm text-sage" role="status">{{ t('security.totp.enabled') }}</p>
        <Button type="button" class="w-fit" :disabled="busy" @click="setup">{{ t('security.totp.setup') }}</Button>
      </template>

      <template v-else>
        <p class="text-sm text-ink">{{ t('security.totp.scan') }}</p>
        <TotpQr :uri="otpauth" :alt="t('security.totp.scan')" />
        <CodeField :value="secret" :label="t('security.totp.secretLabel')" />
        <form class="flex max-w-xs flex-col gap-2" @submit.prevent="verify">
          <Label for="totp-code">{{ t('security.totp.codeLabel') }}</Label>
          <Input id="totp-code" v-model="code" name="code" inputmode="numeric" autocomplete="one-time-code" required />
          <Button type="submit" :disabled="busy">{{ t('security.totp.verify') }}</Button>
        </form>
      </template>

      <Alert v-if="errorText" variant="destructive" role="alert" aria-live="polite">
        <AlertDescription>{{ errorText }}</AlertDescription>
      </Alert>
    </CardContent>
  </Card>
</template>
