<script setup lang="ts">
// TODO(i18n): English-literal scaffold copy; key + translate after the design-system pass.
import { ref, watch } from 'vue'
import { api } from '../lib/api'
import { startAuthentication } from '@simplewebauthn/browser'
import { sudoState } from '../lib/sudo'

const methods = ref<string[]>([])
const chosen = ref<string>('')
const password = ref('')
const totp = ref('')
const error = ref('')
const busy = ref(false)
const loading = ref(false)

function finish(ok: boolean) {
  const r = sudoState.value.resolve
  sudoState.value = { open: false, resolve: null }
  methods.value = []; chosen.value = ''; password.value = ''; totp.value = ''; error.value = ''
  r?.(ok)
}

watch(() => sudoState.value.open, async (open) => {
  if (!open) return
  error.value = ''; chosen.value = ''
  loading.value = true
  try {
    const r = await api.get<{ methods: string[] }>('/api/prohibitorum/me/sudo/methods')
    methods.value = r.methods ?? []
    if (methods.value.length === 1) chosen.value = methods.value[0]
  } catch (e: any) {
    error.value = e?.message ?? 'Could not load step-up methods'
  } finally {
    loading.value = false
  }
})

async function runWebauthn() {
  const options = await api.post<any>('/api/prohibitorum/me/sudo/begin', { method: 'webauthn' })
  const assertion = await startAuthentication({ optionsJSON: options.publicKey ?? options })
  await api.post('/api/prohibitorum/me/sudo/complete', assertion)
}

async function runPasswordTotp() {
  await api.post('/api/prohibitorum/me/sudo/begin', { method: 'password_totp' })
  await api.post('/api/prohibitorum/me/sudo/complete', { current_password: password.value, totp_code: totp.value })
}

async function submit() {
  if (busy.value || !chosen.value) return
  busy.value = true; error.value = ''
  try {
    if (chosen.value === 'webauthn') await runWebauthn()
    else await runPasswordTotp()
    finish(true)
  } catch (e: any) {
    error.value = e?.message ?? 'Step-up failed'
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <UModal :open="sudoState.open" @update:open="(v: boolean) => { if (!v) finish(false) }">
    <template #content>
      <div class="p-6 space-y-4 w-full max-w-sm">
        <h2 class="text-lg font-semibold">Confirm it's you</h2>
        <p class="text-sm text-muted">This action needs a fresh identity check.</p>
        <p v-if="error" role="alert" aria-live="polite" class="text-error text-sm">{{ error }}</p>

        <div v-if="loading" class="text-sm text-muted">Loading…</div>
        <div v-else-if="methods.length === 0" class="text-sm text-muted">
          No step-up methods are enrolled. Add a passkey or password+2FA first.
        </div>
        <div v-else class="space-y-3">
          <div class="flex gap-2">
            <UButton v-for="m in methods" :key="m" type="button" size="sm"
              :variant="chosen === m ? 'solid' : 'soft'" @click="chosen = m">
              {{ m === 'webauthn' ? 'Passkey' : 'Password + 2FA' }}
            </UButton>
          </div>
          <div v-if="chosen === 'password_totp'" class="space-y-2">
            <UInput v-model="password" type="password" placeholder="Current password" autocomplete="current-password" />
            <UInput v-model="totp" type="text" inputmode="numeric" placeholder="Authenticator code" />
          </div>
        </div>

        <div class="flex justify-end gap-2 pt-2">
          <UButton type="button" color="neutral" variant="ghost" @click="finish(false)">Cancel</UButton>
          <UButton type="button" :disabled="busy || !chosen" :loading="busy" @click="submit">Confirm</UButton>
        </div>
      </div>
    </template>
  </UModal>
</template>
