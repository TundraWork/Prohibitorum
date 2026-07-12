<script setup lang="ts">
/**
 * SudoModal — the sudo step-up ceremony, mounted ONCE in DashboardLayout;
 * watches the lib/sudo singleton. Opening fetches the account's LOCAL elevation
 * methods and mirrors the login screen's local section: passkey primary, an OR
 * divider, then the password+TOTP form inline. A 204 from /me/sudo/complete
 * resolves the pending withSudo()/ensureSudo() promise.
 *
 * Upstream-login-only accounts have no local factor to re-prove in a modal, so
 * when neither local method is available we redirect to the real /login (which
 * re-runs the upstream flow and re-grants the recent-auth window), returning to
 * the current route. Federation is NOT a step-up factor here — it lives only on
 * the login screen. Reachable only on a stale session: a recent login already
 * satisfies the gate without opening the modal.
 */
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import type { PublicKeyCredentialRequestOptionsJSON } from '@simplewebauthn/browser'
import { api, type ApiError } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { useWebauthn } from '@/composables/useWebauthn'
import { sudoState, _resolveSudo } from '@/lib/sudo'
import { hardRedirect } from '@/lib/navigate'
import { ShieldCheck, Fingerprint } from 'lucide-vue-next'
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter,
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import OrDivider from '@/components/custom/OrDivider.vue'
import ErrorPanel from '@/components/custom/ErrorPanel.vue'

const { t } = useI18n()
const { busy: netBusy, error: netError, run } = useApi()
const { busy: waBusy, error: waError, authenticate } = useWebauthn()
const route = useRoute()

const open = computed({
  get: () => sudoState.value.open,
  set: (v) => { if (!v) _resolveSudo(false) },
})

type SudoMethodsResponse = { methods: string[] }

const methods = ref<string[] | null>(null)
const password = ref('')
const code = ref('')

const busy = computed(() => netBusy.value || waBusy.value)
const error = computed<ApiError | null>(() => netError.value ?? waError.value)
const hasPasskey = computed(() => methods.value?.includes('webauthn') ?? false)
const hasPwTotp = computed(() => methods.value?.includes('password_totp') ?? false)

watch(() => sudoState.value.open, async (isOpen) => {
  if (!isOpen) return
  methods.value = null
  password.value = ''
  code.value = ''
  netError.value = null
  waError.value = null
  let available: string[] = []
  try {
    const res = await api.get<SudoMethodsResponse>('/api/prohibitorum/me/sudo/methods')
    available = res.methods ?? []
  } catch {
    available = []
  }
  // Upstream-login-only (no local factor): bounce to the real /login, which
  // re-runs the user's auth and re-grants the recent-auth window, then returns.
  if (!available.includes('webauthn') && !available.includes('password_totp')) {
    hardRedirect(`/login?return_to=${encodeURIComponent(route.fullPath)}`)
    return
  }
  methods.value = available
})

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
    <!-- z-[60] (above the default z-50 dialogs): sudo is a step-up that can be
         summoned ON TOP of a destructive ConfirmDialog (e.g. unlink, delete),
         so it must layer above any other open dialog to stay operable. -->
    <DialogContent class="z-[60]" overlay-class="z-[60]">
      <DialogHeader>
        <span class="inline-flex size-10 items-center justify-center rounded-full bg-tide/10 text-tide-strong">
          <ShieldCheck class="size-5" aria-hidden="true" />
        </span>
        <DialogTitle>{{ t('sudo.title') }}</DialogTitle>
        <DialogDescription>{{ sudoState.reason || t('sudo.body') }}</DialogDescription>
      </DialogHeader>

      <p v-if="methods === null" class="text-sm text-muted">{{ t('common.loading') }}</p>

      <div v-else class="flex flex-col gap-4">
        <Button v-if="hasPasskey" size="lg" class="w-full" :disabled="busy" @click="doPasskey">
          <Fingerprint aria-hidden="true" />
          {{ t('sudo.passkeyButton') }}
        </Button>

        <OrDivider v-if="hasPasskey && hasPwTotp" :label="t('login.orDivider')" />

        <form v-if="hasPwTotp" class="flex flex-col gap-3" @submit.prevent="doPasswordTotp">
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

        <ErrorPanel :error="error" @dismiss="() => { netError = null; waError = null }" />
      </div>

      <DialogFooter>
        <Button variant="ghost" :disabled="busy" @click="_resolveSudo(false)">{{ t('sudo.cancel') }}</Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>
</template>
