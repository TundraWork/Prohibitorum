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
 *     → 200 { redirect } → emit('success', redirect)
 *
 * Note: /auth/totp/verify returns { redirect } — the server-validated
 * destination to navigate to. The account-recovery sub-flow emits success
 * with no redirect argument; LoginView falls back to goReturnTo() in that case.
 *
 * Errors render via errors.<code> (fallback to the raw message) in a
 * role="alert" aria-live="polite" region; busy guards re-entrancy.
 */
import { nextTick, ref, useTemplateRef } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import AccountRecovery from '@/components/custom/AccountRecovery.vue'
import ErrorPanel from '@/components/custom/ErrorPanel.vue'

// Raw return_to passthrough — forwarded to the server, which is the
// authoritative validator (validateReturnTo). Not guarded client-side here.
const props = defineProps<{ returnTo?: string }>()
const emit = defineEmits<{ success: [redirect?: string] }>()

const { t } = useI18n()
const { busy, run, error, clear } = useApi()

const phase = ref<'password' | 'totp'>('password')
const username = ref('')
const password = ref('')
const code = ref('')
const partialToken = ref('')
const recovering = ref(false)
const recoveryNote = ref('')

// Input is a single-root component → its DOM element is exposed on $el.
const totpInput = useTemplateRef<{ $el?: HTMLElement }>('totpInput')

function onRecoveryRestart(): void {
  recovering.value = false
  phase.value = 'password'
  code.value = ''
  recoveryNote.value = t('login.recoveryRestart')
}

async function submitPassword(): Promise<void> {
  recoveryNote.value = ''
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
  const res = await run(() =>
    api.post<{ redirect: string }>(
      `/api/prohibitorum/auth/totp/verify?return_to=${encodeURIComponent(props.returnTo ?? '')}`,
      { partial_session_token: partialToken.value, code: code.value },
    ),
  )
  if (res) emit('success', res.redirect ?? '/')
}
</script>

<template>
  <form
    class="flex flex-col gap-4"
    @submit.prevent="phase === 'password' ? submitPassword() : submitTotp()"
  >
    <!-- Phase 1: username + password -->
    <template v-if="phase === 'password'">
      <p v-if="recoveryNote" class="text-sm text-muted" role="status">{{ recoveryNote }}</p>
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
      <template v-if="!recovering">
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
        <button type="button" class="cursor-pointer text-left text-sm text-muted underline-offset-4 hover:underline focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring rounded-sm" data-test="lost-authenticator" @click="recovering = true">
          {{ t('login.lostAuthenticator') }}
        </button>
      </template>
      <AccountRecovery v-else :partial-token="partialToken" @success="emit('success')" @restart="onRecoveryRestart" />
    </template>

    <template v-if="!recovering">
      <ErrorPanel :error="error" @dismiss="clear" />

      <Button type="submit" class="w-full" :disabled="busy">
        {{ phase === 'password' ? t('login.passwordSubmit') : t('login.totpSubmit') }}
      </Button>
    </template>
  </form>
</template>
