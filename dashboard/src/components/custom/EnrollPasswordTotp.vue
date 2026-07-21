<script setup lang="ts">
/**
 * EnrollPasswordTotp — the password+TOTP arm of the enrollment ceremony
 * (/enroll/:token). Modeled on AccountRecovery: an unauthenticated,
 * token-scoped begin→QR→confirm→recovery-codes flow, composed from the shared
 * threshold leaves. Offered for every intent except bootstrap (EnrollView's
 * method chooser gates that). The verify response sets the session cookie, so
 * on success we hard-redirect to the app root.
 *
 * Federation-bound invites reject both local methods with
 * enrollment_federation_required; we surface that up to EnrollView (which shows
 * the "continue to your provider" interstitial), mirroring the passkey path.
 */
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api, type ApiError } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { hardRedirect } from '@/lib/navigate'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Alert, AlertDescription } from '@/components/ui/alert'
import TotpQr from '@/components/custom/TotpQr.vue'
import CodeField from '@/components/custom/CodeField.vue'
import RecoveryCodesDisplay from '@/components/custom/RecoveryCodesDisplay.vue'
import ErrorPanel from '@/components/custom/ErrorPanel.vue'

const props = defineProps<{
  token: string
  identity: { username: string; displayName: string } | null
}>()
const emit = defineEmits<{ back: []; federationRequired: [] }>()

const { t } = useI18n()
const { busy, run, error, clear } = useApi()

const phase = ref<'password' | 'totp' | 'done'>('password')
const password = ref('')
const confirm = ref('')
const localError = ref('')
const secret = ref('')
const otpauthUri = ref('')
const totpCode = ref('')
const newCodes = ref<string[]>([])

const basePath = computed(
  () => `/api/prohibitorum/enrollments/${encodeURIComponent(props.token)}/password-totp`,
)

async function submitPassword(): Promise<void> {
  localError.value = ''
  if (password.value.length < 8) {
    localError.value = t('enroll.pwTooShort')
    return
  }
  if (password.value !== confirm.value) {
    localError.value = t('enroll.pwMismatch')
    return
  }
  const body: Record<string, string> = { password: password.value }
  if (props.identity) {
    body.username = props.identity.username
    body.displayName = props.identity.displayName
  }
  const res = await run(() =>
    api.post<{ secret_base32: string; otpauth_uri: string }>(`${basePath.value}/begin`, body),
  )
  if (!res) {
    if ((error.value as ApiError | null)?.code === 'enrollment_federation_required') {
      emit('federationRequired')
    }
    return
  }
  secret.value = res.secret_base32
  otpauthUri.value = res.otpauth_uri
  phase.value = 'totp'
}

async function verifyTotp(): Promise<void> {
  const res = await run(() =>
    api.post<{ recoveryCodes: string[] }>(`${basePath.value}/verify`, { code: totpCode.value }),
  )
  if (!res) return
  newCodes.value = res.recoveryCodes
  phase.value = 'done'
}

function finish(): void {
  hardRedirect('/')
}
</script>

<template>
  <div class="flex flex-col gap-4" data-test="enroll-password-totp">
    <ErrorPanel :error="error" @dismiss="clear" />

    <template v-if="phase === 'password'">
      <div class="flex flex-col gap-1.5">
        <Label for="enroll-password">{{ t('enroll.pwPasswordLabel') }}</Label>
        <Input
          id="enroll-password"
          v-model="password"
          type="password"
          name="new-password"
          autocomplete="new-password"
          @keydown.enter.prevent="submitPassword"
        />
        <p class="text-xs text-muted">{{ t('enroll.pwPasswordDesc') }}</p>
      </div>
      <div class="flex flex-col gap-1.5">
        <Label for="enroll-password-confirm">{{ t('enroll.pwConfirmLabel') }}</Label>
        <Input
          id="enroll-password-confirm"
          v-model="confirm"
          type="password"
          name="confirm-password"
          autocomplete="new-password"
          @keydown.enter.prevent="submitPassword"
        />
      </div>
      <Alert v-if="localError" variant="destructive" role="alert" aria-live="polite">
        <AlertDescription>{{ localError }}</AlertDescription>
      </Alert>
      <div class="flex flex-col gap-2">
        <Button
          type="button"
          size="lg"
          class="w-full"
          :disabled="busy || !password || !confirm"
          :aria-busy="busy"
          data-test="pwtotp-continue"
          @click="submitPassword"
        >
          {{ t('enroll.pwContinue') }}
        </Button>
        <Button type="button" variant="ghost" class="w-full" data-test="pwtotp-back" @click="emit('back')">
          {{ t('enroll.methodBack') }}
        </Button>
      </div>
    </template>

    <template v-else-if="phase === 'totp'">
      <h2 class="text-base font-semibold text-ink">{{ t('enroll.totpTitle') }}</h2>
      <p class="text-sm leading-5 text-muted">{{ t('enroll.totpHint') }}</p>
      <TotpQr v-if="otpauthUri" :uri="otpauthUri" :alt="t('enroll.totpTitle')" />
      <CodeField v-if="secret" :value="secret" :label="t('enroll.totpSecretLabel')" />
      <div class="flex flex-col gap-1.5">
        <Label for="enroll-totp-code">{{ t('enroll.totpCodeLabel') }}</Label>
        <Input
          id="enroll-totp-code"
          v-model="totpCode"
          inputmode="numeric"
          autocomplete="one-time-code"
          pattern="[0-9]*"
          maxlength="8"
          @keydown.enter.prevent="verifyTotp"
        />
      </div>
      <Button
        type="button"
        size="lg"
        class="w-full"
        :disabled="busy || !totpCode"
        :aria-busy="busy"
        data-test="pwtotp-verify"
        @click="verifyTotp"
      >
        {{ t('enroll.totpVerify') }}
      </Button>
    </template>

    <template v-else>
      <RecoveryCodesDisplay :codes="newCodes" @confirmed="finish" />
    </template>
  </div>
</template>
