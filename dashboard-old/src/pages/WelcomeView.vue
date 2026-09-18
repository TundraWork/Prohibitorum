<script setup lang="ts">
import { useAction } from '@/composables/useAction'
const action = useAction()
import { useResource } from '@/composables/useResource'
import { confirmationQuery } from '@/queries/ceremonies'
/**
 * WelcomeView — federated identity-confirmation interstitial (/welcome).
 *
 * Shown to a new user arriving via an upstream IdP (auto-provision mode).
 * The backend withholds the full session, mints a short-lived confirmation
 * grant (cookie), and redirects here.  The page:
 *   1. GETs /auth/federation/confirm to display the provisioned identity.
 *   2. Polls while avatarPending is true (background inherit job running).
 *   3. Gates the Continue button until the avatar settles or the cap is hit.
 *   4. Continue  → POST /auth/federation/confirm  → navigate to redirect.
 *   5. Not me    → POST /auth/federation/confirm/decline → navigate to /login.
 *
 * Auth: the grant cookie was set by the server before the redirect; all API
 * calls use credentials: 'include' (enforced by api.*).  A 401 means the
 * grant is missing or expired — redirect to /login.
 */
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Loader2 } from 'lucide-vue-next'
import { api } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Alert, AlertDescription } from '@/components/ui/alert'
import UserAvatar from '@/components/custom/UserAvatar.vue'
import CenteredLayout from '@/pages/CenteredLayout.vue'

const props = withDefaults(defineProps<{ pollMs?: number; capMs?: number }>(), {
  pollMs: 1500,
  capMs: 30_000,
})

interface ConfirmView {
  idpDisplayName: string
  displayName: string
  username: string
  email: string
  avatarUrl?: string
  avatarPending: boolean
}

const { t } = useI18n()
const contextQuery = useResource({ ...confirmationQuery<ConfirmView>(), enabled: false })
const view = computed(() => contextQuery.data.value ?? null)
const busy = ref(false)
const settled = ref(false)
const loading = ref(true)
const confirmError = ref('')
let timer: ReturnType<typeof setTimeout> | undefined
let elapsed = 0
let disposed = false

async function load(): Promise<void> {
  try {
    await contextQuery.refetch({ throwOnError: true })
    if (disposed) return
  } catch {
    if (disposed) return
    window.location.assign('/login')
    return
  } finally {
    // Only clear loading on the first fetch (when loading is still true).
    if (loading.value) loading.value = false
  }
  if (!view.value?.avatarPending || elapsed >= props.capMs) {
    settled.value = true
    return
  }
  elapsed += props.pollMs
  timer = setTimeout(load, props.pollMs)
}

async function confirm(): Promise<void> {
  busy.value = true
  confirmError.value = ''
  try {
    const res = await action.execute(signal => api.post<{ redirect: string; offerLocalSignin?: boolean }>(
      '/api/prohibitorum/auth/federation/confirm',
      {}, { signal }))
    // An invite+provider account has no local credentials yet: the confirm
    // response offers the one-time "add local sign-in" step, which later
    // lands on the same redirect target. Direct navigation keeps the fresh
    // session cookie flowing to the next page.
    if (res.offerLocalSignin) {
      const target = res.redirect || '/'
      window.location.assign(
        `/setup-signin?redirect=${encodeURIComponent(target)}`,
      )
      return
    }
    window.location.assign(res.redirect || '/')
  } catch {
    busy.value = false
    confirmError.value = t('welcome.confirmError')
  }
}

async function notMe(): Promise<void> {
  busy.value = true
  try {
    await action.execute(signal => api.post('/api/prohibitorum/auth/federation/confirm/decline', {}, { signal }))
  } catch {
    // ignore — navigate regardless
  }
  window.location.assign('/login')
}

onMounted(load)
onBeforeUnmount(() => { disposed = true; if (timer) clearTimeout(timer) })
</script>

<template>
  <CenteredLayout>
    <template #title>
      <h1 class="text-xl font-semibold tracking-tight text-ink">{{ t('welcome.title') }}</h1>
    </template>

    <p v-if="loading" class="text-center text-sm text-muted">{{ t('common.loading') }}</p>

    <div v-else-if="view" class="flex flex-col items-center gap-5 text-center">
      <p class="text-sm text-muted">{{ t('welcome.via', { idp: view.idpDisplayName }) }}</p>

      <!-- Avatar with spinner overlay while the background job is running -->
      <div class="relative">
        <UserAvatar
          :display-name="view.displayName"
          :username="view.username"
          :src="view.avatarUrl ?? null"
          class="size-20 text-2xl"
        />
        <span
          v-if="!settled"
          class="absolute inset-0 flex items-center justify-center rounded-full bg-black/40"
          role="status"
          :aria-label="t('welcome.fetchingAvatar')"
        >
          <Loader2 class="size-6 animate-spin text-white" />
        </span>
      </div>

      <!-- Identity details -->
      <div>
        <p class="text-lg font-semibold text-ink">{{ view.displayName }}</p>
        <p class="text-sm text-muted">{{ view.email }}</p>
        <p class="text-xs text-muted">{{ view.username }}</p>
      </div>

      <p class="text-sm text-muted" aria-live="polite">
        {{ t('welcome.description') }}
      </p>

      <!-- Actions -->
      <div class="flex w-full gap-2">
        <Button
          variant="ghost"
          class="flex-1"
          :disabled="busy"
          data-test="welcome-notme"
          @click="notMe"
        >
          {{ t('welcome.notMe') }}
        </Button>
        <Button
          class="flex-1"
          :disabled="busy"
          data-test="welcome-continue"
          @click="confirm"
        >
          {{ t('welcome.continue') }}
        </Button>
      </div>

      <Alert v-if="confirmError" variant="destructive" role="alert" aria-live="polite">
        <AlertDescription>{{ confirmError }}</AlertDescription>
      </Alert>
    </div>
  </CenteredLayout>
</template>
