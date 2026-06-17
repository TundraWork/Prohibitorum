<script setup lang="ts">
/**
 * PasskeyButton — the primary WebAuthn passkey login.
 *
 * Flow:
 *   POST /auth/login/begin        → publicKey request options (+ ceremony cookie)
 *   passkeyGet(options)           → assertion (navigator.credentials.get)
 *   POST /auth/login/complete     → { redirect } (+ session cookie)
 *   → emit('success', redirect)
 *
 * User-cancel (NotAllowedError) is swallowed inside useWebauthn — no error
 * banner for a deliberate dismissal. Network/ceremony errors render via
 * errors.<code> (fallback message). `busy` reflects both the network calls
 * and the in-browser ceremony, so the button is disabled throughout.
 */
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PublicKeyCredentialRequestOptionsJSON } from '@simplewebauthn/browser'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { useWebauthn } from '@/composables/useWebauthn'
import { Button } from '@/components/ui/button'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Fingerprint } from 'lucide-vue-next'

// Raw return_to passthrough — forwarded to the server, which is the
// authoritative validator (validateReturnTo). Not guarded client-side here.
const props = defineProps<{ returnTo?: string }>()
const emit = defineEmits<{ success: [redirect: string] }>()

const { t, te } = useI18n()
const { busy: netBusy, error: netError, run } = useApi()
const { busy: waBusy, error: waError, authenticate } = useWebauthn()

const busy = computed(() => netBusy.value || waBusy.value)
const error = computed(() => netError.value ?? waError.value)

const errorText = computed(() => {
  const e = error.value
  if (!e) return ''
  const key = `errors.${e.code}`
  return te(key) ? t(key) : e.message || t('common.error')
})

async function signIn(): Promise<void> {
  // 1) begin — fetch the WebAuthn request options.
  const options = await run(() =>
    api.post<PublicKeyCredentialRequestOptionsJSON>('/api/prohibitorum/auth/login/begin'),
  )
  if (!options) return // network error (e.g. not_bootstrapped) → shown via netError

  // 2) ceremony — navigator.credentials.get. undefined = user-cancel (silent) or error.
  const assertion = await authenticate(options)
  if (!assertion) return

  // 3) complete — verify the assertion, issue the session; the server returns
  //    the validated redirect to follow.
  const res = await run(() =>
    api.post<{ redirect: string }>(
      `/api/prohibitorum/auth/login/complete?return_to=${encodeURIComponent(props.returnTo ?? '')}`,
      assertion,
    ),
  )
  if (!res) return

  emit('success', res.redirect ?? '/')
}
</script>

<template>
  <div class="flex flex-col gap-2">
    <Button type="button" size="lg" class="w-full" :disabled="busy" @click="signIn">
      <Fingerprint aria-hidden="true" />
      {{ t('login.passkeyButton') }}
    </Button>
    <p class="text-center text-sm text-muted">{{ t('login.passkeyHint') }}</p>

    <Alert v-if="errorText" variant="destructive" role="alert" aria-live="polite">
      <AlertDescription>{{ errorText }}</AlertDescription>
    </Alert>
  </div>
</template>
