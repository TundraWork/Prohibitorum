<script setup lang="ts">
/**
 * PairDeviceView (/pair) — the NEW-device side of device pairing (public).
 * begin → show display code → poll status → on approval complete (gets a
 * session cookie) → offer a skippable local-passkey registration → dashboard.
 * The poll timer is cleared on unmount, approval, and expiry.
 */
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { useWebauthn } from '@/composables/useWebauthn'
import type { PublicKeyCredentialCreationOptionsJSON } from '@simplewebauthn/browser'
import CenteredLayout from '@/pages/CenteredLayout.vue'
import CodeField from '@/components/custom/CodeField.vue'
import { Button } from '@/components/ui/button'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Skeleton } from '@/components/ui/skeleton'

const POLL_MS = 2500

interface Begin { pairingId: string; code: string; displayCode: string; expiresAt: string }
interface Status { status: 'pending' | 'approved' | 'expired'; expiresAt?: string }

const { t, te } = useI18n()
const router = useRouter()
const { busy, error, run } = useApi()
const { register, error: waError } = useWebauthn()

type Phase = 'pending' | 'expired' | 'success'
const phase = ref<Phase>('pending')
const displayCode = ref('')
const pairingId = ref('')
const expiresAt = ref('')
const now = ref(Date.now())
let timer: ReturnType<typeof setInterval> | null = null
let polling = false
let mounted = false

const errorText = computed(() => {
  const e = error.value || waError.value
  if (!e) return ''
  const key = `errors.${e.code}`
  return te(key) ? t(key) : e.message || t('common.error')
})
const secondsLeft = computed(() => {
  const ms = Date.parse(expiresAt.value)
  if (Number.isNaN(ms)) return 0
  return Math.max(0, Math.round((ms - now.value) / 1000))
})

function stopTimer(): void { if (timer) { clearInterval(timer); timer = null } }

async function begin(): Promise<void> {
  stopTimer()
  phase.value = 'pending'
  const res = await run(() => api.post<Begin>('/api/prohibitorum/auth/devices/pair/begin'))
  if (!res) return
  pairingId.value = res.pairingId
  displayCode.value = res.displayCode
  expiresAt.value = res.expiresAt
  now.value = Date.now()
  timer = setInterval(poll, POLL_MS)
}

async function poll(): Promise<void> {
  if (polling || phase.value !== 'pending') return
  polling = true
  now.value = Date.now()
  try {
    const s = await api.get<Status>(
      `/api/prohibitorum/auth/devices/pair/status?id=${encodeURIComponent(pairingId.value)}`)
    if (!mounted) return
    if (s.status === 'expired') { stopTimer(); phase.value = 'expired'; return }
    if (s.status === 'approved') {
      stopTimer()
      await complete()
      // complete() failed (still pending) and we're still mounted → resume polling
      // so the user isn't wedged on a dead "waiting" screen.
      if (mounted && phase.value === 'pending') timer = setInterval(poll, POLL_MS)
    }
  } catch {
    // Transient poll failure — keep polling; a terminal state will resolve it.
  } finally {
    polling = false
  }
}

async function complete(): Promise<void> {
  const ok = await run(async () => {
    await api.post('/api/prohibitorum/auth/devices/pair/complete', { pairingId: pairingId.value })
    return true as const
  })
  if (ok) phase.value = 'success'
}

async function addPasskey(): Promise<void> {
  const options = await run(() => api.post<PublicKeyCredentialCreationOptionsJSON>(
    '/api/prohibitorum/me/credentials/register/begin'))
  if (!options) return
  const attestation = await register(options)
  if (!attestation) return
  const ok = await run(async () => {
    await api.post('/api/prohibitorum/me/credentials/register/complete', attestation)
    return true as const
  })
  if (ok) router.push('/')
}

function skip(): void { router.push('/') }

onMounted(() => { mounted = true; begin() })
onUnmounted(() => { mounted = false; stopTimer() })
</script>

<template>
  <CenteredLayout>
    <template #title>
      <h1 class="text-xl font-semibold tracking-tight text-ink">{{ t('pair.title') }}</h1>
    </template>

    <div class="flex flex-col gap-6">
      <Alert v-if="errorText" variant="destructive" role="alert" aria-live="polite">
        <AlertDescription>{{ errorText }}</AlertDescription>
      </Alert>

      <template v-if="phase === 'pending'">
        <p class="text-center text-sm text-muted">{{ t('pair.intro') }}</p>
        <Skeleton v-if="busy && !displayCode" class="h-10 w-40 rounded-md self-center" />
        <CodeField v-else-if="displayCode" :value="displayCode" />
        <p class="text-center text-sm text-muted" role="status">
          {{ t('pair.waiting') }}
          <span v-if="secondsLeft > 0"> · {{ t('pair.expiresIn', { seconds: secondsLeft }) }}</span>
        </p>
      </template>

      <template v-else-if="phase === 'expired'">
        <p class="text-center text-sm text-muted" role="status">{{ t('pair.expired') }}</p>
        <Button type="button" class="w-full" :disabled="busy" data-test="regenerate" @click="begin">
          {{ t('pair.regenerate') }}
        </Button>
      </template>

      <template v-else>
        <p class="text-center text-sm text-sage" role="status">{{ t('pair.success') }}</p>
        <div class="flex flex-col gap-2">
          <Button type="button" class="w-full" :disabled="busy" data-test="add-passkey" @click="addPasskey">
            {{ t('pair.addPasskey') }}
          </Button>
          <p class="text-center text-xs text-muted">{{ t('pair.addPasskeyHelp') }}</p>
          <Button type="button" variant="ghost" class="w-full" data-test="skip" @click="skip">
            {{ t('pair.skip') }}
          </Button>
        </div>
      </template>
    </div>
  </CenteredLayout>
</template>
