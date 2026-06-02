<script setup lang="ts">
// TODO(i18n): English-literal scaffold copy; key + translate after the design-system pass.
import { ref } from 'vue'
import { api } from '../../lib/api'
import { withSudo } from '../../lib/sudo'

const password = ref(''); const busy = ref(false); const error = ref(''); const done = ref(false)

async function save() {
  if (busy.value) return
  if (password.value.length < 8) { error.value = 'Password must be at least 8 characters'; return }
  busy.value = true; error.value = ''; done.value = false
  try {
    await withSudo(() => api.post('/api/prohibitorum/me/password/set', { password: password.value }))
    done.value = true; password.value = ''
  } catch (e: any) { error.value = e?.message ?? 'Could not set password' } finally { busy.value = false }
}
</script>

<template>
  <UCard>
    <template #header><h2 class="font-medium">Password</h2></template>
    <div class="flex flex-col gap-2 max-w-sm">
      <UInput v-model="password" type="password" placeholder="New password" autocomplete="new-password" />
      <p v-if="error" role="alert" aria-live="polite" class="text-error text-sm">{{ error }}</p>
      <p v-if="done" class="text-success text-sm">Password updated.</p>
      <UButton data-test="save-password" type="button" class="self-start" :loading="busy" :disabled="busy" @click="save">Set password</UButton>
    </div>
  </UCard>
</template>
