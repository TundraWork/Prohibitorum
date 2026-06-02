<script setup lang="ts">
// TODO(i18n): English-literal scaffold copy; key + translate after the design-system pass.
import { ref } from 'vue'
import { api } from '../../lib/api'
import { withSudo } from '../../lib/sudo'

const secret = ref(''); const otpauth = ref(''); const code = ref('')
const recovery = ref<string[]>([]); const busy = ref(false)
const err = ref(''); const done = ref(false); const armedRevoke = ref(false)

async function begin() {
  if (busy.value) return; busy.value = true; err.value = ''; recovery.value = []; done.value = false
  try {
    const r = await withSudo(() => api.post<{ secret_base32: string; otpauth_uri: string }>('/api/prohibitorum/me/totp/begin'))
    secret.value = r.secret_base32; otpauth.value = r.otpauth_uri
  } catch (e: any) { err.value = e?.message ?? 'Could not start TOTP setup' } finally { busy.value = false }
}
async function verify() {
  if (busy.value) return; busy.value = true; err.value = ''
  try {
    const r = await withSudo(() => api.post<{ recovery_codes?: string[] } | undefined>('/api/prohibitorum/me/totp/verify', { code: code.value }))
    done.value = true; secret.value = ''; otpauth.value = ''; code.value = ''
    if (r && r.recovery_codes) recovery.value = r.recovery_codes
  } catch (e: any) { err.value = e?.message ?? 'Invalid code' } finally { busy.value = false }
}
async function revoke() {
  if (busy.value) return; busy.value = true; err.value = ''
  try { await withSudo(() => api.post('/api/prohibitorum/me/auth/revoke-password-totp')); armedRevoke.value = false; done.value = false } catch (e: any) { err.value = e?.message ?? 'Could not revoke'; armedRevoke.value = false } finally { busy.value = false }
}
</script>

<template>
  <UCard>
    <template #header><h2 class="font-medium">Two-factor (TOTP)</h2></template>
    <p v-if="err" role="alert" aria-live="polite" class="text-error text-sm mb-2">{{ err }}</p>

    <div v-if="!secret" class="flex items-center gap-2">
      <UButton data-test="totp-begin" type="button" size="sm" :loading="busy" :disabled="busy" @click="begin">Set up authenticator</UButton>
      <span v-if="done" class="text-success text-sm">2FA configured.</span>
    </div>

    <div v-else class="space-y-3">
      <p class="text-sm text-muted">Add this secret to your authenticator app, then enter a code to confirm.</p>
      <div class="text-sm">Secret: <code class="font-mono">{{ secret }}</code></div>
      <div class="text-xs text-muted break-all font-mono">{{ otpauth }}</div>
      <div class="flex items-center gap-2">
        <UInput data-test="totp-code" v-model="code" type="text" inputmode="numeric" placeholder="6-digit code" class="w-40" />
        <UButton data-test="totp-verify" type="button" size="sm" :loading="busy" :disabled="busy" @click="verify">Verify</UButton>
      </div>
    </div>

    <div v-if="recovery.length" class="mt-4 space-y-1">
      <p class="text-sm font-medium">Recovery codes (save now — shown once):</p>
      <ul class="font-mono text-sm grid grid-cols-2 gap-x-6">
        <li v-for="rc in recovery" :key="rc">{{ rc }}</li>
      </ul>
    </div>

    <template #footer>
      <div class="inline-flex items-center gap-1">
        <template v-if="armedRevoke">
          <UButton type="button" size="xs" color="error" :disabled="busy" @click="revoke">Confirm revoke</UButton>
          <UButton type="button" size="xs" color="neutral" variant="ghost" @click="armedRevoke = false">Cancel</UButton>
        </template>
        <UButton v-else type="button" size="xs" color="error" variant="soft" @click="armedRevoke = true">Revoke password &amp; 2FA</UButton>
      </div>
    </template>
  </UCard>
</template>
