<script setup lang="ts">
import { usePrivateState } from '@/composables/usePrivateState'
/**
 * SetupLocalSigninView — the one-time "add a local sign-in method" step
 * (/setup-signin), offered right after /welcome confirmation for accounts
 * minted through an invite + provider. Two paths, both leaving the page on
 * success: a passkey (session-scoped register ceremony) or password +
 * authenticator (a single transaction). Skippable by design: skipping leaves a
 * pure provider account, and local methods can be added later on the Security
 * page.
 *
 * A SudoModal is mounted here because this page renders under CenteredLayout,
 * not DashboardLayout. Without it a `sudo_required` response from withSudo
 * would open the singleton with nobody to resolve it, leaving every button
 * stuck in busy. The modal itself handles accounts with no local credential by
 * redirecting to /login?return_to=<current path>.
 *
 * On skip or completion, hard-redirect to the `redirect` query parameter
 * through safeReturnTo (same-origin only — it feeds window.location.assign);
 * default '/'. A hard navigation is required — the target may be a server
 * route or an OIDC continuation, not a Vue route.
 */
import { computed, ref } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import type { PublicKeyCredentialCreationOptionsJSON } from '@simplewebauthn/browser'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { useWebauthn } from '@/composables/useWebauthn'
import { withSudo } from '@/lib/sudo'
import { hardRedirect } from '@/lib/navigate'
import { safeReturnTo } from '@/lib/returnTo'
import CenteredLayout from '@/pages/CenteredLayout.vue'
import SudoModal from '@/components/custom/SudoModal.vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import CodeField from '@/components/custom/CodeField.vue'
import TotpQr from '@/components/custom/TotpQr.vue'
import RecoveryCodesDisplay from '@/components/custom/RecoveryCodesDisplay.vue'
import ErrorPanel from '@/components/custom/ErrorPanel.vue'

type Method = 'choose' | 'password_totp'
type Phase = 'password' | 'totp' | 'done'

const route = useRoute()
const { t } = useI18n()
const { busy: netBusy, error: netError, run, clear: clearNet } = useApi()
const { busy: waBusy, error: waError, register } = useWebauthn()

const busy = computed(() => netBusy.value || waBusy.value)
const error = computed(() => netError.value ?? waError.value)

const method = ref<Method>('choose')
const phase = ref<Phase>('password')
const localError = ref('')

const pw = ref('')
const confirm = ref('')
const secret = ref('')
const otpauthUri = ref('')
const totpCode = ref('')
const recoveryCodes = ref<string[]>([])

// Untrusted query input that now feeds window.location.assign: safeReturnTo
// rejects //evil.com, the /\evil.com path trick, and cross-origin absolute URLs.
const redirectTarget = computed(() => {
  const raw = route.query.redirect
  const value = Array.isArray(raw) ? raw[0] : raw
  return safeReturnTo(typeof value === 'string' ? value : undefined)
})

function clearError(): void {
  localError.value = ''
  clearNet()
  waError.value = null
}

function startPasswordTotp(): void {
  clearError()
  phase.value = 'password'
  method.value = 'password_totp'
}

function back(): void {
  clearError()
  pw.value = ''
  confirm.value = ''
  method.value = 'choose'
}

function skip(): void {
  hardRedirect(redirectTarget.value)
}

function finish(): void {
  hardRedirect(redirectTarget.value)
}

// Same call order as PasskeysCard.add(): begin (sudo-gated) → register → complete.
async function addPasskey(): Promise<void> {
  clearError()
  const ok = await run(async () => {
    const options = await withSudo(
      () => api.post<PublicKeyCredentialCreationOptionsJSON>('/api/prohibitorum/me/credentials/register/begin'),
      t('sudo.reason.addPasskey'),
    )
    if (!options) return
    const attestation = await register(options)
    if (!attestation) return
    await api.post('/api/prohibitorum/me/credentials/register/complete', attestation)
    return true as const
  })
  if (ok) finish()
}

async function submitPassword(): Promise<void> {
  clearError()
  if (pw.value.length < 8) {
    localError.value = t('security.password.tooShort')
    return
  }
  if (pw.value !== confirm.value) {
    localError.value = t('security.password.mismatch')
    return
  }
  // Sudo elevation happens before the QR is shown, so the modal cannot
  // interrupt the user mid-code-entry (same hoisting as PasswordTotpCard).
  const r = await run(() => withSudo(
    () => api.post<{ secret_base32: string; otpauth_uri: string }>(
      '/api/prohibitorum/me/password-totp/begin',
      { password: pw.value },
    ),
    t('sudo.reason.setPasswordTotp'),
  ))
  if (!r) return
  secret.value = r.secret_base32
  otpauthUri.value = r.otpauth_uri
  phase.value = 'totp'
}

async function verifyTotp(): Promise<void> {
  clearError()
  // No withSudo here — begin already elevated the session (PasswordTotpCard rationale).
  const r = await run(() =>
    api.post<{ recovery_codes?: string[] }>('/api/prohibitorum/me/password-totp/verify', { code: totpCode.value }),
  )
  if (!r) return
  recoveryCodes.value = r.recovery_codes ?? []
  phase.value = 'done'
}

usePrivateState(() => {
  pw.value = ''
  confirm.value = ''
  secret.value = ''
  otpauthUri.value = ''
  totpCode.value = ''
  recoveryCodes.value = []
})
</script>

<template>
  <CenteredLayout>
    <template #title>
      <h1 class="text-xl font-semibold tracking-tight text-ink">{{ t('setupSignin.title') }}</h1>
    </template>
    <div class="flex min-w-0 flex-col gap-4">
      <p class="text-sm leading-5 text-muted">{{ t('setupSignin.intro') }}</p>

      <ErrorPanel :error="error" @dismiss="clearError" />

      <template v-if="method === 'choose'">
        <div class="flex flex-col gap-3">
          <div class="flex flex-col gap-1">
            <Button
              type="button"
              size="lg"
              class="w-full"
              :disabled="busy"
              :aria-busy="busy"
              data-test="choose-passkey"
              @click="addPasskey"
            >
              {{ t('setupSignin.choosePasskey') }}
            </Button>
            <p class="text-center text-xs text-muted">{{ t('setupSignin.choosePasskeyDesc') }}</p>
          </div>
          <div class="flex flex-col gap-1">
            <Button
              type="button"
              size="lg"
              variant="outline"
              class="w-full"
              :disabled="busy"
              data-test="choose-password-totp"
              @click="startPasswordTotp"
            >
              {{ t('setupSignin.choosePasswordTotp') }}
            </Button>
            <p class="text-center text-xs text-muted">{{ t('setupSignin.choosePasswordTotpDesc') }}</p>
          </div>
        </div>

        <div class="flex flex-col gap-2">
          <Button
            type="button"
            variant="ghost"
            class="w-full"
            :disabled="busy"
            data-test="skip"
            @click="skip"
          >
            {{ t('setupSignin.skip') }}
          </Button>
          <p class="text-center text-xs text-muted">{{ t('setupSignin.skipHint') }}</p>
        </div>
      </template>

      <template v-else>
        <form v-if="phase === 'password'" class="flex max-w-sm flex-col gap-4" @submit.prevent="submitPassword">
          <div class="flex flex-col gap-1.5">
            <Label for="setup-pw-new">{{ t('security.password.newLabel') }}</Label>
            <Input id="setup-pw-new" v-model="pw" name="new_password" type="password" autocomplete="new-password" required />
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="setup-pw-confirm">{{ t('security.password.confirmLabel') }}</Label>
            <Input id="setup-pw-confirm" v-model="confirm" name="confirm_password" type="password" autocomplete="new-password" required />
          </div>
          <p v-if="localError" class="text-sm text-destructive" role="alert">{{ localError }}</p>
          <div class="flex flex-wrap gap-2">
            <Button type="submit" :disabled="busy" data-test="password-continue">
              {{ t('setupSignin.passwordContinue') }}
            </Button>
            <Button type="button" variant="ghost" :disabled="busy" data-test="back" @click="back">
              {{ t('setupSignin.back') }}
            </Button>
          </div>
        </form>

        <div v-else-if="phase === 'totp'" class="flex flex-col gap-4">
          <p class="text-sm leading-5 text-muted">{{ t('setupSignin.totpHint') }}</p>
          <TotpQr v-if="otpauthUri" :uri="otpauthUri" :alt="t('setupSignin.totpQrAlt')" />
          <CodeField v-if="secret" :value="secret" :label="t('setupSignin.totpSecretLabel')" />
          <div class="flex max-w-xs flex-col gap-1.5">
            <Label for="setup-totp-code">{{ t('setupSignin.totpCodeLabel') }}</Label>
            <Input
              id="setup-totp-code"
              v-model="totpCode"
              name="totp_code"
              inputmode="numeric"
              autocomplete="one-time-code"
              :disabled="busy"
              @keydown.enter.prevent="verifyTotp"
            />
          </div>
          <div class="flex flex-wrap gap-2">
            <Button type="button" :disabled="busy" :aria-busy="busy" data-test="verify-totp" @click="verifyTotp">
              {{ t('setupSignin.verify') }}
            </Button>
          </div>
        </div>

        <RecoveryCodesDisplay v-else :codes="recoveryCodes" @confirmed="finish" />
      </template>
    </div>

    <SudoModal />
  </CenteredLayout>
</template>
