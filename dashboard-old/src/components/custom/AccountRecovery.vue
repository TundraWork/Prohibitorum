<script setup lang="ts">
import { usePrivateState } from '@/composables/usePrivateState'
/**
 * AccountRecovery consumes a recovery code once. The user can complete login
 * with the existing authenticator or generate and submit a replacement in the
 * same request. Any failed submission spends the partial token, so the parent
 * restarts password authentication.
 */
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { generateTotpEnrollment } from '@/lib/totpEnrollment'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Alert, AlertDescription } from '@/components/ui/alert'
import TotpQr from '@/components/custom/TotpQr.vue'
import CodeField from '@/components/custom/CodeField.vue'
import RecoveryCodesDisplay from '@/components/custom/RecoveryCodesDisplay.vue'
import ErrorPanel from '@/components/custom/ErrorPanel.vue'

const props = defineProps<{ partialToken: string; username: string; returnTo?: string }>()
const emit = defineEmits<{ success: [redirect?: string]; restart: [] }>()

const { t } = useI18n()
const { busy, run, error, clear } = useApi()

const recoveryCode = ref('')
const resetAuthenticator = ref(false)
const otpauthUri = ref('')
const secret = ref('')
const totpCode = ref('')
const newCodes = ref<string[]>([])
const successRedirect = ref('/')

async function setResetAuthenticator(value: boolean | 'indeterminate'): Promise<void> {
  resetAuthenticator.value = value === true
  clear()
  secret.value = ''
  otpauthUri.value = ''
  totpCode.value = ''
  if (!resetAuthenticator.value) return

  const enrollment = await run(() => generateTotpEnrollment(props.username))
  if (!enrollment) {
    resetAuthenticator.value = false
    return
  }
  secret.value = enrollment.secretBase32
  otpauthUri.value = enrollment.otpauthUri
}

async function verifyCode(): Promise<void> {
  if (busy.value || !recoveryCode.value ||
      (resetAuthenticator.value && (!secret.value || !totpCode.value))) return
  const body: Record<string, string | boolean> = {
    partial_session_token: props.partialToken,
    code: recoveryCode.value,
    reset_authenticator: resetAuthenticator.value,
  }
  if (resetAuthenticator.value) {
    body.totp_secret_base32 = secret.value
    body.totp_code = totpCode.value
  }
  const path = '/api/prohibitorum/auth/recovery-code/verify?return_to=' + encodeURIComponent(props.returnTo ?? '')
  const res = await run(() => api.post<{ redirect: string; recovery_codes?: string[] }>(path, body))
  if (!res) {
    emit('restart')
    return
  }
  if (resetAuthenticator.value) {
    successRedirect.value = res.redirect ?? '/'
    newCodes.value = res.recovery_codes ?? []
    return
  }
  emit('success', res.redirect ?? '/')
}

usePrivateState(() => {
  recoveryCode.value = ''
  resetAuthenticator.value = false
  otpauthUri.value = ''
  secret.value = ''
  totpCode.value = ''
  newCodes.value = []
  successRedirect.value = '/'
})
</script>

<template>
  <div class="flex flex-col gap-4">
    <ErrorPanel :error="error" @dismiss="clear" />

    <template v-if="!newCodes.length">
      <h2 class="text-base font-semibold text-ink">{{ t('recovery.title') }}</h2>
      <div class="flex flex-col gap-1.5">
        <Label for="recovery-code">{{ t('recovery.codeLabel') }}</Label>
        <Input id="recovery-code" v-model="recoveryCode" name="recovery-code" autocomplete="one-time-code" @keydown.enter.prevent="verifyCode" />
        <p class="text-sm text-muted">{{ t('recovery.codeHint') }}</p>
      </div>
      <Alert role="status">
        <AlertDescription class="text-xs">{{ t('recovery.codeWarning') }}</AlertDescription>
      </Alert>
      <label class="flex cursor-pointer items-start gap-2 text-sm text-ink">
        <Checkbox
          :model-value="resetAuthenticator"
          data-test="reset-authenticator"
          @update:model-value="setResetAuthenticator"
        />
        <span class="pt-0.5">{{ t('recovery.resetAuthenticator') }}</span>
      </label>

      <div v-if="resetAuthenticator && secret" class="flex flex-col gap-4" data-test="replacement-authenticator">
        <h3 class="text-base font-semibold text-ink">{{ t('recovery.reenrollTitle') }}</h3>
        <p class="text-sm text-muted">{{ t('recovery.reenrollHint') }}</p>
        <TotpQr :uri="otpauthUri" :alt="t('recovery.reenrollTitle')" />
        <CodeField :value="secret" :label="t('recovery.secretLabel')" />
        <div class="flex flex-col gap-1.5">
          <Label for="reenroll-code">{{ t('recovery.codeInputLabel') }}</Label>
          <Input
            id="reenroll-code"
            v-model="totpCode"
            name="reenroll-code"
            inputmode="numeric"
            autocomplete="one-time-code"
            pattern="[0-9]*"
            maxlength="8"
            @keydown.enter.prevent="verifyCode"
          />
        </div>
      </div>

      <Button
        type="button"
        class="w-full"
        :disabled="busy || !recoveryCode || (resetAuthenticator && (!secret || !totpCode))"
        :aria-busy="busy"
        data-test="verify-code"
        @click="verifyCode"
      >
        {{ resetAuthenticator ? t('recovery.confirmReset') : t('recovery.verify') }}
      </Button>
    </template>

    <RecoveryCodesDisplay v-else :codes="newCodes" regenerated @confirmed="emit('success', successRedirect)" />
  </div>
</template>
