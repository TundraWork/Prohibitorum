<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '../lib/api'
import type { ApiError } from '../lib/api'

const emit = defineEmits<{ success: [] }>()
const { t, te } = useI18n()

type Phase = 'credentials' | 'totp'
const phase = ref<Phase>('credentials')

const username = ref('')
const password = ref('')
const code = ref('')
const partialToken = ref('')

const busy = ref(false)
const error = ref('')

function show(err: unknown) {
  const e = err as Partial<ApiError>
  if (e && e.code && te(`errors.${e.code}`)) error.value = t(`errors.${e.code}`)
  else error.value = e?.message ?? t('login.errorFallback')
}

async function submitCredentials() {
  if (busy.value) return
  error.value = ''
  busy.value = true
  try {
    const res = await api.post<{ partial_session_token: string }>(
      '/api/prohibitorum/auth/password/begin',
      { username: username.value, password: password.value },
    )
    partialToken.value = res.partial_session_token
    phase.value = 'totp'
  } catch (err) {
    show(err)
  } finally {
    busy.value = false
  }
}

async function submitTotp() {
  if (busy.value) return
  error.value = ''
  busy.value = true
  try {
    // 204 No Content -> api returns undefined.
    await api.post('/api/prohibitorum/auth/totp/verify', {
      partial_session_token: partialToken.value,
      code: code.value,
    })
    emit('success')
  } catch (err) {
    show(err)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <form
    v-if="phase === 'credentials'"
    class="flex flex-col gap-3"
    @submit.prevent="submitCredentials"
  >
    <UFormField :label="t('login.username')">
      <UInput v-model="username" autocomplete="username" required block />
    </UFormField>
    <UFormField :label="t('login.passwordLabel')">
      <UInput
        v-model="password"
        type="password"
        autocomplete="current-password"
        required
        block
      />
    </UFormField>
    <p v-if="error" class="text-sm text-error">{{ error }}</p>
    <UButton type="submit" block :loading="busy">{{ t('login.password') }}</UButton>
  </form>

  <form v-else class="flex flex-col gap-3" @submit.prevent="submitTotp">
    <UFormField :label="t('login.totp')">
      <UInput
        v-model="code"
        inputmode="numeric"
        autocomplete="one-time-code"
        required
        block
      />
    </UFormField>
    <p v-if="error" class="text-sm text-error">{{ error }}</p>
    <UButton type="submit" block :loading="busy">{{ t('login.submit') }}</UButton>
  </form>
</template>
