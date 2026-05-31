<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { api } from '../lib/api'
import type { ApiError } from '../lib/api'
import { safeReturnTo } from '../lib/returnTo'
import ConsentScopeList from '../components/ConsentScopeList.vue'

interface ConsentContext {
  client: {
    clientId: string
    displayName: string
    logoUri?: string
    policyUri?: string
    tosUri?: string
  }
  account: { displayName: string }
  scopes: string[]
}

const { t, te } = useI18n()
const route = useRoute()
const router = useRouter()

// Each entry point re-guards its own return_to (same-origin enforced).
const ticket = String(route.query.ticket ?? '')
const returnTo = safeReturnTo(
  typeof route.query.return_to === 'string' ? route.query.return_to : '',
)

const ctx = ref<ConsentContext | null>(null)
const busy = ref(false)
const error = ref('')

function show(err: unknown) {
  const e = err as Partial<ApiError>
  if (e && e.code && te(`errors.${e.code}`)) error.value = t(`errors.${e.code}`)
  else error.value = e?.message ?? t('consent.errorFallback')
}

onMounted(async () => {
  if (!ticket) {
    router.push({ name: 'error', query: { code: 'invalid_consent_ticket' } })
    return
  }
  try {
    ctx.value = await api.get<ConsentContext>(
      '/api/prohibitorum/consent?ticket=' + encodeURIComponent(ticket),
    )
  } catch (err) {
    const e = err as Partial<ApiError>
    router.push({
      name: 'error',
      query: { code: e?.code ?? 'invalid_consent_ticket' },
    })
  }
})

async function decide(decision: 'approve' | 'deny') {
  if (busy.value) return
  error.value = ''
  busy.value = true
  try {
    const res = await api.post<{ redirect: string }>(
      '/api/prohibitorum/consent?return_to=' + encodeURIComponent(returnTo ?? ''),
      { ticket, decision },
    )
    window.location.assign(res.redirect)
  } catch (err) {
    show(err)
    busy.value = false
  }
}
</script>

<template>
  <UCard v-if="ctx" class="w-full max-w-sm mx-auto mt-16">
    <h1 class="text-xl font-semibold text-center">{{ t('consent.title') }}</h1>

    <div class="mt-6 flex flex-col items-center gap-4">
      <UAvatar
        size="lg"
        :text="(ctx.client.displayName.charAt(0) || '?').toUpperCase()"
      />

      <p class="text-center">
        {{ t('consent.requests', { app: ctx.client.displayName }) }}
      </p>

      <ConsentScopeList class="self-stretch" :scopes="ctx.scopes" />

      <p class="text-sm text-muted text-center">
        {{ t('consent.continueAs', { account: ctx.account.displayName }) }}
      </p>

      <p v-if="error" role="alert" aria-live="polite" class="text-sm text-error">
        {{ error }}
      </p>

      <div class="flex w-full gap-3">
        <UButton
          type="button"
          color="neutral"
          variant="subtle"
          block
          class="flex-1"
          :loading="busy"
          :disabled="busy"
          @click="decide('deny')"
        >
          {{ t('consent.deny') }}
        </UButton>
        <UButton
          type="button"
          block
          class="flex-1"
          :loading="busy"
          :disabled="busy"
          @click="decide('approve')"
        >
          {{ t('consent.approve') }}
        </UButton>
      </div>
    </div>
  </UCard>
  <div v-else class="flex justify-center mt-24">
    <UIcon name="i-lucide-loader-2" class="size-8 animate-spin text-muted" />
  </div>
</template>
