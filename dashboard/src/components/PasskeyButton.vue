<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { passkeyLogin } from '../lib/webauthn'
import type { ApiError } from '../lib/api'

const emit = defineEmits<{ success: [] }>()
const { t, te } = useI18n()

const busy = ref(false)
const error = ref('')

async function onClick() {
  if (busy.value) return
  error.value = ''
  busy.value = true
  try {
    await passkeyLogin()
    emit('success')
  } catch (err) {
    // Passkey may be unsupported or the ceremony cancelled. Show inline and
    // keep the other login methods usable.
    const e = err as Partial<ApiError>
    if (e && e.code && te(`errors.${e.code}`)) error.value = t(`errors.${e.code}`)
    else error.value = (err instanceof Error ? err.message : e?.message) ?? t('login.errorFallback')
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="flex flex-col gap-2">
    <UButton block size="lg" :loading="busy" @click="onClick">
      {{ t('login.passkey') }}
    </UButton>
    <p v-if="error" class="text-sm text-error">{{ error }}</p>
  </div>
</template>
