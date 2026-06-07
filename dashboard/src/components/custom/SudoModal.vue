<script setup lang="ts">
/**
 * SudoModal — the sudo step-up ceremony. Mounted ONCE in DashboardLayout;
 * watches the lib/sudo singleton. Opening fetches the account's elevation
 * methods; the user re-proves a factor (passkey, or password + TOTP); a 204
 * from /me/sudo/complete resolves the pending withSudo()/ensureSudo() promise.
 */
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PublicKeyCredentialRequestOptionsJSON } from '@simplewebauthn/browser'
import { api, type ApiError } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { useWebauthn } from '@/composables/useWebauthn'
import { sudoState, _resolveSudo } from '@/lib/sudo'
import { ShieldCheck, Fingerprint } from 'lucide-vue-next'
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter,
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Alert, AlertDescription } from '@/components/ui/alert'

const { t, te } = useI18n()
const { busy: netBusy, error: netError, run } = useApi()
const { busy: waBusy, error: waError, authenticate } = useWebauthn()

const open = computed({
  get: () => sudoState.value.open,
  set: (v) => { if (!v) _resolveSudo(false) },
})

const methods = ref<string[] | null>(null)
const showPwForm = ref(false)
const password = ref('')
const code = ref('')

const busy = computed(() => netBusy.value || waBusy.value)
const error = computed<ApiError | null>(() => netError.value ?? waError.value)
const errorText = computed(() => {
  const e = error.value
  if (!e) return ''
  const key = `errors.${e.code}`
  return te(key) ? t(key) : e.message || t('common.error')
})
const hasPasskey = computed(() => methods.value?.includes('webauthn') ?? false)
const hasPwTotp = computed(() => methods.value?.includes('password_totp') ?? false)

watch(() => sudoState.value.open, async (isOpen) => {
  if (!isOpen) return
  methods.value = null
  showPwForm.value = false
  password.value = ''
  code.value = ''
  netError.value = null
  waError.value = null
  try {
    const res = await api.get<{ methods: string[] }>('/api/prohibitorum/me/sudo/methods')
    methods.value = res.methods ?? []
    showPwForm.value = !hasPasskey.value && hasPwTotp.value
  } catch {
    methods.value = []
  }
})

function switchToPassword() { netError.value = null; waError.value = null; showPwForm.value = true }

async function doPasskey(): Promise<void> {
  const options = await run(() =>
    api.post<PublicKeyCredentialRequestOptionsJSON>('/api/prohibitorum/me/sudo/begin', { method: 'webauthn' }),
  )
  if (!options) return
  const assertion = await authenticate(options)
  if (!assertion) return
  const ok = await run(async () => {
    await api.post('/api/prohibitorum/me/sudo/complete', assertion)
    return true as const
  })
  if (ok) _resolveSudo(true)
}

async function doPasswordTotp(): Promise<void> {
  const began = await run(async () => {
    await api.post('/api/prohibitorum/me/sudo/begin', { method: 'password_totp' })
    return true as const
  })
  if (!began) return
  const ok = await run(async () => {
    await api.post('/api/prohibitorum/me/sudo/complete', {
      current_password: password.value,
      totp_code: code.value,
    })
    return true as const
  })
  if (ok) _resolveSudo(true)
}
</script>

<template>
  <Dialog v-model:open="open">
    <DialogContent>
      <DialogHeader>
        <span class="inline-flex size-10 items-center justify-center rounded-full bg-tide/10 text-tide-strong">
          <ShieldCheck class="size-5" aria-hidden="true" />
        </span>
        <DialogTitle>{{ t('sudo.title') }}</DialogTitle>
        <DialogDescription>{{ t('sudo.prompt') }}</DialogDescription>
      </DialogHeader>

      <p v-if="methods === null" class="text-sm text-muted">{{ t('common.loading') }}</p>

      <p v-else-if="methods.length === 0" class="text-sm text-muted">{{ t('sudo.noMethod') }}</p>

      <div v-else class="flex flex-col gap-4">
        <Button v-if="hasPasskey && !showPwForm" size="lg" class="w-full" :disabled="busy" @click="doPasskey">
          <Fingerprint aria-hidden="true" />
          {{ t('sudo.passkeyButton') }}
        </Button>

        <button
          v-if="hasPasskey && hasPwTotp && !showPwForm"
          type="button"
          class="text-sm text-tide-strong underline-offset-4 hover:underline"
          @click="switchToPassword"
        >
          {{ t('sudo.usePassword') }}
        </button>

        <form v-if="showPwForm" class="flex flex-col gap-3" @submit.prevent="doPasswordTotp">
          <div class="flex flex-col gap-1.5">
            <Label for="sudo-password">{{ t('sudo.passwordLabel') }}</Label>
            <Input id="sudo-password" v-model="password" name="current_password" type="password"
                   autocomplete="current-password" required />
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="sudo-code">{{ t('sudo.codeLabel') }}</Label>
            <Input id="sudo-code" v-model="code" name="totp_code" inputmode="numeric"
                   autocomplete="one-time-code" required />
          </div>
          <Button type="submit" class="w-full" :disabled="busy">{{ t('sudo.verify') }}</Button>
        </form>

        <Alert v-if="errorText" variant="destructive" role="alert" aria-live="polite">
          <AlertDescription>{{ errorText }}</AlertDescription>
        </Alert>
      </div>

      <DialogFooter>
        <Button variant="ghost" :disabled="busy" @click="_resolveSudo(false)">{{ t('sudo.cancel') }}</Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>
</template>
