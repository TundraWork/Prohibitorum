<script setup lang="ts">
// TODO(i18n): English-literal scaffold copy; key + translate after the design-system pass.
import { ref } from 'vue'
import { api } from '../../lib/api'
import { withSudo } from '../../lib/sudo'

const codes = ref<string[]>([]); const busy = ref(false); const error = ref(''); const armed = ref(false)

async function regen() {
  if (busy.value) return; busy.value = true; error.value = ''
  try {
    const r = await withSudo(() => api.post<{ recovery_codes: string[] }>('/api/prohibitorum/me/recovery-codes/regenerate'))
    codes.value = r.recovery_codes ?? []; armed.value = false
  } catch (e: any) { error.value = e?.message ?? 'Could not regenerate'; armed.value = false } finally { busy.value = false }
}
</script>

<template>
  <UCard>
    <template #header><h2 class="font-medium">Recovery codes</h2></template>
    <p class="text-sm text-muted mb-2">Single-use codes to sign in if you lose your other factors. Regenerating invalidates any previous set.</p>
    <p v-if="error" role="alert" aria-live="polite" class="text-error text-sm mb-2">{{ error }}</p>
    <div class="inline-flex items-center gap-1">
      <template v-if="armed">
        <UButton data-test="regen" type="button" size="xs" color="error" :disabled="busy" @click="regen">Confirm regenerate</UButton>
        <UButton type="button" size="xs" color="neutral" variant="ghost" @click="armed = false">Cancel</UButton>
      </template>
      <UButton v-else data-test="regen" type="button" size="xs" variant="soft" @click="armed = true">Regenerate</UButton>
    </div>
    <div v-if="codes.length" class="mt-3 space-y-1">
      <p class="text-sm font-medium">Save these now — shown once:</p>
      <ul class="font-mono text-sm grid grid-cols-2 gap-x-6"><li v-for="c in codes" :key="c">{{ c }}</li></ul>
    </div>
  </UCard>
</template>
