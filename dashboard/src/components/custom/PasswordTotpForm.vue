<script setup lang="ts">
/**
 * PasswordTotpForm — the password→TOTP fallback login, an explicit two-phase
 * state machine.
 *
 *   phase 'password': {username, password}
 *     → POST /auth/password/begin → { partial_session_token }
 *     → advance to phase 'totp'
 *   phase 'totp': {code}
 *     → POST /auth/totp/verify { partial_session_token, code }
 *     → 204 + session cookie → emit('success')
 *
 * Note on the 204: /auth/totp/verify returns No Content on success, so the
 * api call resolves to undefined either way. The run() callback therefore
 * returns an explicit `true` sentinel so success is distinguishable from the
 * error path (where run() returns undefined).
 *
 * Errors render via errors.<code> (fallback to the raw message) in a
 * role="alert" aria-live="polite" region; busy guards re-entrancy.
 */
import { computed, nextTick, ref, useTemplateRef } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Alert, AlertDescription } from '@/components/ui/alert'

const emit = defineEmits<{ success: [] }>()

const { t, te } = useI18n()
const { busy, error, run } = useApi()

const phase = ref<'password' | 'totp'>('password')
const username = ref('')
const password = ref('')
const code = ref('')
const partialToken = ref('')

// Input is a single-root component → its DOM element is exposed on $el.
const totpInput = useTemplateRef<{ $el?: HTMLElement }>('totpInput')

/** errors.<code> with fallback to the server message, then a generic string. */
const errorText = computed(() => {
  const e = error.value
  if (!e) return ''
  const key = `errors.${e.code}`
  return te(key) ? t(key) : e.message || t('common.error')
})

async function submitPassword(): Promise<void> {
  const res = await run(() =>
    api.post<{ partial_session_token: string }>('/api/prohibitorum/auth/password/begin', {
      username: username.value,
      password: password.value,
    }),
  )
  if (!res) return
  partialToken.value = res.partial_session_token
  phase.value = 'totp'
  await nextTick()
  totpInput.value?.$el?.focus()
}

async function submitTotp(): Promise<void> {
  const ok = await run(async () => {
    // 204 No Content on success → resolve an explicit sentinel.
    await api.post('/api/prohibitorum/auth/totp/verify', {
      partial_session_token: partialToken.value,
      code: code.value,
    })
    return true as const
  })
  if (ok) emit('success')
}
</script>

<template>
  <form
    class="flex flex-col gap-4"
    @submit.prevent="phase === 'password' ? submitPassword() : submitTotp()"
  >
    <!-- Phase 1: username + password -->
    <template v-if="phase === 'password'">
      <div class="flex flex-col gap-1.5">
        <Label for="login-username">{{ t('login.usernameLabel') }}</Label>
        <Input
          id="login-username"
          v-model="username"
          name="username"
          autocomplete="username"
          autocapitalize="none"
          spellcheck="false"
          required
        />
      </div>
      <div class="flex flex-col gap-1.5">
        <Label for="login-password">{{ t('login.passwordLabel') }}</Label>
        <Input
          id="login-password"
          v-model="password"
          name="password"
          type="password"
          autocomplete="current-password"
          required
        />
      </div>
    </template>

    <!-- Phase 2: one-time code -->
    <template v-else>
      <div class="flex flex-col gap-1.5">
        <Label for="login-totp">{{ t('login.totpLabel') }}</Label>
        <Input
          id="login-totp"
          ref="totpInput"
          v-model="code"
          name="code"
          inputmode="numeric"
          autocomplete="one-time-code"
          pattern="[0-9]*"
          maxlength="8"
          required
        />
        <p class="text-sm text-muted">{{ t('login.totpHint') }}</p>
      </div>
    </template>

    <Alert v-if="errorText" variant="destructive" role="alert" aria-live="polite">
      <AlertDescription>{{ errorText }}</AlertDescription>
    </Alert>

    <Button type="submit" variant="outline" class="w-full" :disabled="busy">
      {{ phase === 'password' ? t('login.passwordSubmit') : t('login.totpSubmit') }}
    </Button>
  </form>
</template>
