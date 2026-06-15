<script setup lang="ts">
/**
 * TotpCard — enroll a TOTP authenticator. begin (sudo-gated when re-enrolling)
 * returns secret+otpauth; the backend persists only on verify. First
 * enrollment returns recovery codes.
 *
 * Sudo hoisting: withSudo is called only on `begin` (the "Set up authenticator"
 * button), NOT on `verify`. This prevents the sudo modal from interrupting the
 * user mid-code-entry (TOTP codes expire in 30 s). Once `begin` succeeds the
 * server-side session is already elevated; `verify` runs within that same
 * elevated window without re-prompting. If the elevation expires between begin
 * and verify the server will reject with 401/sudo_required — the error banner
 * shows and the user can restart setup; this edge case is far better than the
 * modal popping while the user is typing a one-time code.
 */
import { ref } from 'vue'
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
import StatusBadge from '@/components/custom/StatusBadge.vue'

const props = defineProps<{ enrolled?: boolean }>()
const emit = defineEmits<{ (e: 'changed'): void }>()

const { t } = useI18n()
const { busy, error, run, errorText } = useApi()
const secret = ref('')
const otpauth = ref('')
const code = ref('')
const recovery = ref<string[]>([])
const enabled = ref(false)

async function setup(): Promise<void> {
  // Sudo elevation happens here at setup start, before the QR is shown, so the
  // sudo modal cannot appear mid-code-entry.
  const r = await run(() => withSudo(() =>
    api.post<{ secret_base32: string; otpauth_uri: string }>('/api/prohibitorum/me/totp/begin'),
    t('sudo.reason.setupTotp')))
  if (!r) return
  secret.value = r.secret_base32
  otpauth.value = r.otpauth_uri
  recovery.value = []
  enabled.value = false
}

async function verify(): Promise<void> {
  // No withSudo here — elevation was acquired at setup. Running verify directly
  // within the elevated session window. If elevation has lapsed the server
  // returns 401 and the error banner prompts the user to restart.
  const r = await run(() =>
    api.post<{ recovery_codes?: string[] } | undefined>('/api/prohibitorum/me/totp/verify', { code: code.value }))
  // 204 (re-enroll) → undefined; first enrollment → { recovery_codes }
  if (error.value) return
  enabled.value = true
  secret.value = ''; otpauth.value = ''; code.value = ''
  if (r && r.recovery_codes) recovery.value = r.recovery_codes
  emit('changed')
}

function cancelSetup(): void {
  secret.value = ''; otpauth.value = ''; code.value = ''
  error.value = null
}
</script>

<template>
  <Card>
    <CardHeader class="flex flex-row items-center gap-2">
      <CardTitle>{{ t('security.totp.title') }}</CardTitle>
      <StatusBadge v-if="props.enrolled === undefined" variant="neutral">—</StatusBadge>
      <StatusBadge v-else :variant="props.enrolled ? 'success' : 'neutral'">
        {{ props.enrolled ? t('security.factors.totpActive') : t('security.factors.totpInactive') }}
      </StatusBadge>
    </CardHeader>
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
          <div class="flex gap-2">
            <Button type="submit" :disabled="busy">{{ t('security.totp.verify') }}</Button>
            <Button type="button" variant="ghost" :disabled="busy" @click="cancelSetup">{{ t('security.totp.cancelSetup') }}</Button>
          </div>
        </form>
      </template>

      <Alert v-if="errorText" variant="destructive" role="alert" aria-live="polite">
        <AlertDescription>{{ errorText }}</AlertDescription>
      </Alert>
    </CardContent>
  </Card>
</template>
