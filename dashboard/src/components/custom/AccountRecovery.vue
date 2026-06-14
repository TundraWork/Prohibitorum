<script setup lang="ts">
/**
 * AccountRecovery — inline recovery for password+TOTP accounts that lost their
 * authenticator. Driven from PasswordTotpForm's TOTP step with the
 * partial_session_token in hand. code → re-enroll TOTP → new recovery codes → success.
 * A failed recovery code spends the partial token, so failure emits 'restart'.
 */
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Alert, AlertDescription } from '@/components/ui/alert'
import TotpQr from '@/components/custom/TotpQr.vue'
import CodeField from '@/components/custom/CodeField.vue'
import RecoveryCodesDisplay from '@/components/custom/RecoveryCodesDisplay.vue'

const props = defineProps<{ partialToken: string }>()
const emit = defineEmits<{ success: []; restart: [] }>()

const { t } = useI18n()
const { busy, run, errorText } = useApi()

const phase = ref<'code' | 'reenroll' | 'done'>('code')
const recoveryCode = ref('')
const recoveryToken = ref('')
const otpauthUri = ref('')
const secret = ref('')
const totpCode = ref('')
const newCodes = ref<string[]>([])

async function verifyCode(): Promise<void> {
  const res = await run(() => api.post<{ recovery_session_token: string }>(
    '/api/prohibitorum/auth/recovery-code/verify',
    { partial_session_token: props.partialToken, code: recoveryCode.value }))
  if (!res) { emit('restart'); return } // partial token is spent on any failure → restart
  recoveryToken.value = res.recovery_session_token
  phase.value = 'reenroll'
  await beginReenroll()
}
async function beginReenroll(): Promise<void> {
  const res = await run(() => api.post<{ secret_base32: string; otpauth_uri: string }>(
    '/api/prohibitorum/auth/recovery/totp/begin',
    { recovery_session_token: recoveryToken.value }))
  if (res) { secret.value = res.secret_base32; otpauthUri.value = res.otpauth_uri }
}
async function verifyReenroll(): Promise<void> {
  const res = await run(() => api.post<{ recovery_codes: string[] }>(
    '/api/prohibitorum/auth/recovery/totp/verify',
    { recovery_session_token: recoveryToken.value, code: totpCode.value }))
  if (res) { newCodes.value = res.recovery_codes; phase.value = 'done' }
}
</script>
<template>
  <div class="flex flex-col gap-4">
    <Alert v-if="errorText" variant="destructive" role="alert" aria-live="polite"><AlertDescription>{{ errorText }}</AlertDescription></Alert>

    <template v-if="phase === 'code'">
      <h2 class="text-base font-semibold text-ink">{{ t('recovery.title') }}</h2>
      <div class="flex flex-col gap-1.5">
        <Label for="recovery-code">{{ t('recovery.codeLabel') }}</Label>
        <Input id="recovery-code" name="recovery-code" v-model="recoveryCode" autocomplete="one-time-code" @keydown.enter.prevent="verifyCode" />
        <p class="text-sm text-muted">{{ t('recovery.codeHint') }}</p>
      </div>
      <Button type="button" class="w-full" :disabled="busy || !recoveryCode" data-test="verify-code" @click="verifyCode">{{ t('recovery.verify') }}</Button>
    </template>

    <template v-else-if="phase === 'reenroll'">
      <h2 class="text-base font-semibold text-ink">{{ t('recovery.reenrollTitle') }}</h2>
      <p class="text-sm text-muted">{{ t('recovery.reenrollHint') }}</p>
      <TotpQr v-if="otpauthUri" :uri="otpauthUri" :alt="t('recovery.reenrollTitle')" />
      <CodeField v-if="secret" :value="secret" :label="t('recovery.secretLabel')" />
      <Button v-if="!otpauthUri && !busy" type="button" variant="outline" class="w-full" data-test="reenroll-retry" @click="beginReenroll">
        {{ t('common.tryAgain') }}
      </Button>
      <div class="flex flex-col gap-1.5">
        <Label for="reenroll-code">{{ t('recovery.codeInputLabel') }}</Label>
        <Input id="reenroll-code" name="reenroll-code" v-model="totpCode" inputmode="numeric" autocomplete="one-time-code" pattern="[0-9]*" maxlength="8" @keydown.enter.prevent="verifyReenroll" />
      </div>
      <Button type="button" class="w-full" :disabled="busy || !totpCode" data-test="confirm-reenroll" @click="verifyReenroll">{{ t('recovery.confirm') }}</Button>
    </template>

    <template v-else>
      <RecoveryCodesDisplay :codes="newCodes" regenerated @confirmed="emit('success')" />
    </template>
  </div>
</template>
