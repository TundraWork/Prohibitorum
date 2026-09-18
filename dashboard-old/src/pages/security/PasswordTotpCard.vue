<script setup lang="ts">
import { usePrivateState } from '@/composables/usePrivateState'
/**
 * PasswordTotpCard — the password + authenticator login factor, one card.
 *
 * A password is only a usable factor together with a confirmed authenticator
 * (see PHB-50): the password alone leaves the account unable to complete
 * /auth/password/begin. So the two are set up through one server transaction
 * and shown as one card, driven by /me/factors' passwordSet + totpEnrolled:
 *
 * - neither / half: one entry runs /me/password-totp/begin + /verify and ends
 *   on the recovery codes. For a half-configured account this is a full
 *   rewrite — password, authenticator and recovery codes all change.
 * - both: two independent operations. Changing the password must not
 *   invalidate the authenticator, and resetting the authenticator must not
 *   require a new password, so each keeps its own endpoint.
 *
 * Sudo placement mirrors the pre-merge PasswordCard/TotpCard: password writes
 * and TOTP `begin` are sudo-gated; TOTP `verify` is not, so the modal cannot
 * interrupt a one-time code entry.
 */
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import StatusMessage from '@/components/custom/StatusMessage.vue'
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
import CodeField from '@/components/custom/CodeField.vue'
import TotpQr from '@/components/custom/TotpQr.vue'
import RecoveryCodesDisplay from '@/components/custom/RecoveryCodesDisplay.vue'
import ErrorPanel from '@/components/custom/ErrorPanel.vue'

const props = defineProps<{ passwordSet?: boolean; totpEnrolled?: boolean }>()
const emit = defineEmits<{ (e: 'changed'): void }>()

const { t } = useI18n()
const { busy, error, run, clear } = useApi('credentials')

type Shape = 'none' | 'both' | 'half'
type Flow = 'setup' | 'change-password' | 'reset-totp'

const flow = ref<Flow | null>(null)
const pw = ref('')
const confirm = ref('')
const localError = ref('')
const secret = ref('')
const otpauth = ref('')
const code = ref('')
const recovery = ref<string[]>([])
const { flag: done, trigger: triggerDone } = useTransientFlag()
const doneText = ref('')

const shape = computed<Shape | null>(() => {
  if (props.passwordSet === undefined || props.totpEnrolled === undefined) return null
  if (props.passwordSet && props.totpEnrolled) return 'both'
  if (!props.passwordSet && !props.totpEnrolled) return 'none'
  return 'half'
})
const shapeText = computed(() => {
  if (shape.value === 'both') return t('security.passwordTotp.bothDesc')
  if (shape.value === 'none') return t('security.passwordTotp.noneDesc')
  return t('security.passwordTotp.halfDesc')
})

function markDone(text: string): void {
  doneText.value = text
  triggerDone()
}

function resetForms(): void {
  pw.value = ''
  confirm.value = ''
  localError.value = ''
  secret.value = ''
  otpauth.value = ''
  code.value = ''
}

function startSetup(): void {
  resetForms()
  clear()
  flow.value = 'setup'
}

function startChangePassword(): void {
  resetForms()
  clear()
  flow.value = 'change-password'
}

function cancel(): void {
  resetForms()
  clear()
  flow.value = null
}

function dismissCodes(): void {
  recovery.value = []
}

function validPassword(): boolean {
  localError.value = ''
  if (pw.value.length < 8) { localError.value = t('security.password.tooShort'); return false }
  if (pw.value !== confirm.value) { localError.value = t('security.password.mismatch'); return false }
  return true
}

async function submitPassword(): Promise<void> {
  if (!validPassword()) return
  if (flow.value === 'change-password') {
    const ok = await run(() => withSudo(async () => {
      await api.post('/api/prohibitorum/me/password/set', { password: pw.value })
      return true as const
    }, t('sudo.reason.setPassword')))
    if (!ok) return
    pw.value = ''
    confirm.value = ''
    flow.value = null
    markDone(t('security.password.saved'))
    emit('changed')
    return
  }
  // Setup: the server writes the password and a confirmed authenticator in one
  // transaction, so abandoning the code step leaves the account untouched.
  const r = await run(() => withSudo(
    () => api.post<{ secret_base32: string; otpauth_uri: string }>(
      '/api/prohibitorum/me/password-totp/begin',
      { password: pw.value },
    ),
    t('sudo.reason.setPasswordTotp'),
  ))
  if (!r) return
  secret.value = r.secret_base32
  otpauth.value = r.otpauth_uri
}

async function beginResetTotp(): Promise<void> {
  resetForms()
  clear()
  const r = await run(() => withSudo(
    () => api.post<{ secret_base32: string; otpauth_uri: string }>('/api/prohibitorum/me/totp/begin'),
    t('sudo.reason.setupTotp'),
  ))
  if (!r) return
  flow.value = 'reset-totp'
  secret.value = r.secret_base32
  otpauth.value = r.otpauth_uri
}

async function verifyTotp(): Promise<void> {
  const path = flow.value === 'setup'
    ? '/api/prohibitorum/me/password-totp/verify'
    : '/api/prohibitorum/me/totp/verify'
  const r = await run(() => api.post<{ recovery_codes?: string[] } | undefined>(path, { code: code.value }))
  if (error.value) {
    if (error.value.code === 'ceremony_expired') {
      resetForms()
      flow.value = null
    }
    return
  }
  secret.value = ''
  otpauth.value = ''
  code.value = ''
  flow.value = null
  if (r?.recovery_codes) {
    recovery.value = r.recovery_codes
  } else {
    markDone(t('security.totp.enabled'))
  }
  emit('changed')
}
usePrivateState(() => { pw.value = ''; confirm.value = ''; secret.value = ''; otpauth.value = ''; code.value = ''; recovery.value = [] })
</script>

<template>
  <Card>
    <CardHeader class="flex flex-row items-center gap-2">
      <CardTitle>{{ t('security.passwordTotp.title') }}</CardTitle>
      <StatusBadge v-if="props.passwordSet === undefined" variant="neutral">—</StatusBadge>
      <StatusBadge v-else :variant="props.passwordSet ? 'success' : 'neutral'">
        {{ props.passwordSet ? t('security.factors.passwordSet') : t('security.factors.passwordUnset') }}
      </StatusBadge>
      <StatusBadge v-if="props.totpEnrolled === undefined" variant="neutral">—</StatusBadge>
      <StatusBadge v-else :variant="props.totpEnrolled ? 'success' : 'neutral'">
        {{ props.totpEnrolled ? t('security.factors.totpActive') : t('security.factors.totpInactive') }}
      </StatusBadge>
    </CardHeader>
    <CardContent class="flex flex-col gap-4">
      <p class="text-sm text-muted">{{ t('security.passwordTotp.help') }}</p>

      <RecoveryCodesDisplay v-if="recovery.length" :codes="recovery" @confirmed="dismissCodes" />

      <template v-else-if="secret">
        <p class="text-sm text-ink">{{ t('security.totp.scan') }}</p>
        <TotpQr :uri="otpauth" :alt="t('security.totp.scan')" />
        <CodeField :value="secret" :label="t('security.totp.secretLabel')" />
        <form class="flex max-w-xs flex-col gap-2" @submit.prevent="verifyTotp">
          <Label for="pwtotp-code">{{ t('security.totp.codeLabel') }}</Label>
          <Input id="pwtotp-code" v-model="code" name="code" inputmode="numeric" autocomplete="one-time-code" required />
          <div class="flex gap-2">
            <Button type="submit" :disabled="busy" data-test="totp-verify">{{ t('security.totp.verify') }}</Button>
            <Button type="button" variant="ghost" :disabled="busy" data-test="totp-cancel" @click="cancel">{{ t('security.totp.cancelSetup') }}</Button>
          </div>
        </form>
      </template>

      <form
        v-else-if="flow === 'setup' || flow === 'change-password'"
        class="flex max-w-sm flex-col gap-4"
        @submit.prevent="submitPassword"
      >
        <div class="flex flex-col gap-1.5">
          <Label for="pwtotp-pw-new">{{ t('security.password.newLabel') }}</Label>
          <Input id="pwtotp-pw-new" v-model="pw" name="new_password" type="password" autocomplete="new-password" required />
        </div>
        <div class="flex flex-col gap-1.5">
          <Label for="pwtotp-pw-confirm">{{ t('security.password.confirmLabel') }}</Label>
          <Input id="pwtotp-pw-confirm" v-model="confirm" name="confirm_password" type="password" autocomplete="new-password" required />
        </div>
        <Alert v-if="localError" variant="destructive" role="alert" aria-live="polite">
          <AlertDescription>{{ localError }}</AlertDescription>
        </Alert>
        <div class="flex flex-wrap gap-2">
          <Button type="submit" :disabled="busy" data-test="password-submit">{{ t('security.password.submit') }}</Button>
          <Button type="button" variant="ghost" :disabled="busy" data-test="password-cancel" @click="cancel">{{ t('security.totp.cancelSetup') }}</Button>
        </div>
      </form>

      <template v-else-if="shape">
        <p class="text-sm text-muted" data-test="shape-desc">{{ shapeText }}</p>
        <p v-if="shape === 'half'" class="text-sm text-muted" data-test="half-note">
          {{ t('security.passwordTotp.halfReplaces') }}
        </p>
        <StatusMessage :show="done">{{ doneText }}</StatusMessage>
        <div class="flex flex-wrap gap-2">
          <template v-if="shape === 'both'">
            <Button type="button" :disabled="busy" data-test="change-password" @click="startChangePassword">
              {{ t('security.passwordTotp.changePassword') }}
            </Button>
            <Button type="button" variant="outline" :disabled="busy" data-test="reset-totp" @click="beginResetTotp">
              {{ t('security.passwordTotp.resetTotp') }}
            </Button>
          </template>
          <Button v-else type="button" :disabled="busy" data-test="setup-both" @click="startSetup">
            {{ t('security.passwordTotp.setup') }}
          </Button>
        </div>
      </template>

      <ErrorPanel :error="error" @dismiss="clear" />
    </CardContent>
  </Card>
</template>
