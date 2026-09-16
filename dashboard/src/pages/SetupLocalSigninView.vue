<script setup lang="ts">
/**
 * SetupLocalSigninView — the one-time "add a local sign-in method" step
 * (/setup-signin), offered right after /welcome confirmation for accounts
 * minted through an invite + provider. Skippable by design: skipping leaves a
 * pure provider account, and password/TOTP can be added later on the Security
 * page. Uses the existing session endpoints (/me/password/set,
 * /me/totp/begin+verify) wrapped in withSudo — the freshly issued session is
 * inside the recent-auth window, so normally no extra verification pops up.
 *
 * On skip or completion, navigate to the `redirect` query parameter (validated
 * server-side upstream when the confirm response produced it); default '/'.
 */
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { withSudo } from '@/lib/sudo'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import CodeField from '@/components/custom/CodeField.vue'
import TotpQr from '@/components/custom/TotpQr.vue'
import RecoveryCodesDisplay from '@/components/custom/RecoveryCodesDisplay.vue'
import ErrorPanel from '@/components/custom/ErrorPanel.vue'

type Method = 'choose' | 'password' | 'totp'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const { busy, error, run, clear } = useApi()

const method = ref<Method>('choose')
const localError = ref('')

// password state
const pw = ref('')
const confirm = ref('')
// totp state
const secret = ref('')
const otpauthUri = ref('')
const totpCode = ref('')
// results
const recoveryCodes = ref<string[]>([])
const passwordSet = ref(false)
const totpSet = ref(false)

const redirectTarget = computed(() => {
  const raw = route.query.redirect
  const value = Array.isArray(raw) ? raw[0] : raw
  return typeof value === 'string' && value.startsWith('/') ? value : '/'
})

function clearLocalError(): void {
  localError.value = ''
  clear()
}

function skip(): void {
  void router.replace(redirectTarget.value)
}

function finish(): void {
  void router.replace(redirectTarget.value)
}

async function submitPassword(): Promise<void> {
  clearLocalError()
  if (pw.value.length < 8) {
    localError.value = t('security.password.tooShort')
    return
  }
  if (pw.value !== confirm.value) {
    localError.value = t('security.password.mismatch')
    return
  }
  const ok = await run(() =>
    withSudo(async () => {
      await api.post('/api/prohibitorum/me/password/set', { password: pw.value })
      return true as const
    }, t('sudo.reason.setPassword')),
  )
  if (ok) {
    passwordSet.value = true
    pw.value = ''
    confirm.value = ''
  }
}

async function beginTotp(): Promise<void> {
  clearLocalError()
  // Sudo elevation happens at begin, before the QR is shown, so the modal
  // cannot interrupt the user mid-code-entry (same hoisting as TotpCard).
  const r = await run(() =>
    withSudo(
      () => api.post<{ secret_base32: string; otpauth_uri: string }>('/api/prohibitorum/me/totp/begin'),
      t('sudo.reason.setupTotp'),
    ),
  )
  if (!r) return
  secret.value = r.secret_base32
  otpauthUri.value = r.otpauth_uri
}

async function verifyTotp(): Promise<void> {
  clearLocalError()
  // No withSudo here — elevation was acquired at begin (TotpCard rationale).
  const r = await run(() =>
    api.post<{ recovery_codes?: string[] }>('/api/prohibitorum/me/totp/verify', { code: totpCode.value }),
  )
  if (!r) return
  totpSet.value = true
  recoveryCodes.value = r.recovery_codes ?? []
}
</script>

<template>
  <div class="mx-auto flex min-h-svh w-full max-w-2xl flex-col justify-center gap-6 px-4 py-10">
    <Card class="w-full max-w-xl">
      <CardHeader>
        <CardTitle class="text-xl font-semibold tracking-tight">{{ t('setupSignin.title') }}</CardTitle>
      </CardHeader>
      <CardContent class="flex flex-col gap-4">
        <p class="text-sm leading-5 text-muted">{{ t('setupSignin.intro') }}</p>

        <template v-if="method === 'choose'">
          <div class="flex flex-col gap-3">
            <Button type="button" size="lg" class="w-full" data-test="choose-password" @click="method = 'password'">
              {{ t('setupSignin.choosePassword') }}
            </Button>
            <Button type="button" size="lg" variant="outline" class="w-full" data-test="choose-totp" @click="beginTotp(); method = 'totp'">
              {{ t('setupSignin.chooseTotp') }}
            </Button>
          </div>

          <div class="flex flex-col gap-2">
            <p v-if="passwordSet" data-test="password-done" class="text-sm text-muted">{{ t('setupSignin.passwordDone') }}</p>
            <p v-if="totpSet" data-test="totp-done" class="text-sm text-muted">{{ t('setupSignin.totpDone') }}</p>
            <Button type="button" variant="ghost" class="w-full" data-test="skip" @click="skip">
              {{ t('setupSignin.skip') }}
            </Button>
            <p class="text-center text-xs text-muted">{{ t('setupSignin.skipHint') }}</p>
          </div>
        </template>

        <template v-else-if="method === 'password'">
          <form class="flex max-w-sm flex-col gap-4" @submit.prevent="submitPassword">
            <div class="flex flex-col gap-1.5">
              <Label for="setup-pw-new">{{ t('security.password.newLabel') }}</Label>
              <Input id="setup-pw-new" v-model="pw" name="new_password" type="password" autocomplete="new-password" required />
            </div>
            <div class="flex flex-col gap-1.5">
              <Label for="setup-pw-confirm">{{ t('security.password.confirmLabel') }}</Label>
              <Input id="setup-pw-confirm" v-model="confirm" name="confirm_password" type="password" autocomplete="new-password" required />
            </div>
            <p v-if="passwordSet" data-test="password-done" class="text-sm text-muted">{{ t('setupSignin.passwordDone') }}</p>
            <p v-if="localError" class="text-sm text-destructive" role="alert">{{ localError }}</p>
            <ErrorPanel :error="error" @dismiss="clearLocalError" />
            <div class="flex gap-2">
              <Button type="submit" :disabled="busy">{{ t('setupSignin.savePassword') }}</Button>
              <Button type="button" variant="ghost" @click="method = 'choose'">{{ t('setupSignin.back') }}</Button>
            </div>
          </form>
        </template>
        <template v-else-if="method === 'totp'">
          <div v-if="!totpSet" class="flex flex-col gap-4">
            <TotpQr :uri="otpauthUri" :alt="t('setupSignin.totpQrAlt')" />
            <CodeField v-if="secret" :value="secret" :label="t('setupSignin.totpSecretLabel')" />
            <p class="text-sm text-muted">{{ t('setupSignin.totpHint') }}</p>
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
            <p v-if="localError" class="text-sm text-destructive" role="alert">{{ localError }}</p>
            <ErrorPanel :error="error" @dismiss="clearLocalError" />
            <div class="flex gap-2">
              <Button type="button" :disabled="busy" @click="verifyTotp">{{ t('setupSignin.verify') }}</Button>
              <Button type="button" variant="ghost" @click="method = 'choose'">{{ t('setupSignin.back') }}</Button>
            </div>
          </div>
          <div v-else class="flex flex-col gap-4">
            <p data-test="totp-verified" class="text-sm text-muted">{{ t('setupSignin.totpVerified') }}</p>
            <RecoveryCodesDisplay v-if="recoveryCodes.length" :codes="recoveryCodes" />
            <Button type="button" class="self-start" data-test="finish" @click="finish">
              {{ t('setupSignin.finish') }}
            </Button>
          </div>
        </template>
      </CardContent>
    </Card>
  </div>
</template>

