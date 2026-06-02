<script setup lang="ts">
// TODO(i18n): English-literal scaffold copy; key + translate after the design-system pass.
import { ref } from 'vue'
import { api } from '../lib/api'
import { withSudo } from '../lib/sudo'

interface Pairing { pairingId: string; displayCode: string; initiatorUa: string; initiatorIp: string; createdAt: string; expiresAt: string; alreadyBound: boolean }
const code = ref(''); const pending = ref<Pairing | null>(null)
const error = ref(''); const busy = ref(false); const done = ref('')

function show(e: any) { error.value = e?.message ?? 'Something went wrong' }
async function lookup() {
  if (busy.value || !code.value) return; busy.value = true; error.value = ''; done.value = ''; pending.value = null
  try { pending.value = await api.get<Pairing>(`/api/prohibitorum/me/devices/pair/lookup?code=${encodeURIComponent(code.value)}`) } catch (e) { show(e) } finally { busy.value = false }
}
async function approve() {
  if (busy.value) return; busy.value = true; error.value = ''
  try { await withSudo(() => api.post('/api/prohibitorum/me/devices/pair/approve', { code: code.value })); done.value = 'Device approved.'; pending.value = null } catch (e) { show(e) } finally { busy.value = false }
}
async function cancel() {
  if (busy.value) return; busy.value = true; error.value = ''
  try { await api.post('/api/prohibitorum/me/devices/pair/cancel', { code: code.value }); done.value = 'Pairing cancelled.'; pending.value = null } catch (e) { show(e) } finally { busy.value = false }
}
</script>

<template>
  <div class="space-y-4 max-w-2xl">
    <h1 class="text-lg font-semibold">Devices</h1>
    <p class="text-sm text-muted">A new device starts pairing and shows a code. Enter that code here to approve it.</p>
    <p v-if="error" role="alert" aria-live="polite" class="text-error text-sm">{{ error }}</p>
    <p v-if="done" class="text-success text-sm">{{ done }}</p>

    <div class="flex items-center gap-2">
      <UInput data-test="code" v-model="code" type="text" placeholder="Pairing code (e.g. ABCD-1234)" class="w-64" />
      <UButton data-test="lookup" type="button" size="sm" :loading="busy" :disabled="busy" @click="lookup">Look up</UButton>
    </div>

    <UCard v-if="pending">
      <dl class="grid grid-cols-3 gap-y-2 text-sm">
        <dt class="text-muted">Code</dt><dd class="col-span-2 font-mono">{{ pending.displayCode }}</dd>
        <dt class="text-muted">Device</dt><dd class="col-span-2">{{ pending.initiatorUa }}</dd>
        <dt class="text-muted">IP</dt><dd class="col-span-2 font-mono">{{ pending.initiatorIp }}</dd>
        <dt class="text-muted">Expires</dt><dd class="col-span-2">{{ new Date(pending.expiresAt).toLocaleString() }}</dd>
      </dl>
      <template #footer>
        <div class="flex gap-2">
          <UButton data-test="approve" type="button" size="sm" :disabled="busy" @click="approve">Approve</UButton>
          <UButton type="button" size="sm" color="error" variant="soft" :disabled="busy" @click="cancel">Cancel pairing</UButton>
        </div>
      </template>
    </UCard>
  </div>
</template>
