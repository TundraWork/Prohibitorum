<script setup lang="ts">
/**
 * ConsentView — the OIDC consent screen (/consent?ticket=…&return_to=…).
 *
 * Contract (pkg/server/handle_consent.go — verified):
 *   GET  /api/prohibitorum/consent?ticket=<t>
 *        → { client{clientId,displayName,…}, account{displayName}, scopes[], alreadyGranted? }
 *        requires a session; no_session → /login, invalid ticket → /error.
 *   POST /api/prohibitorum/consent?return_to=<authorizeURL>
 *        body { ticket, decision: 'approve' | 'deny' }  → { redirect }
 *
 * The `ticket` and `return_to` both arrive as PAGE query params (the OP
 * redirects the browser here). return_to is a POST *query* param, not body.
 *
 * Redirect handling — deliberately NOT guarded by safeReturnTo: the server
 * returns either a server-validated same-origin RELATIVE path (approve —
 * validateReturnTo normalises the issuer authorize URL to path+query) or the
 * cross-origin RP redirect_uri with error=access_denied (deny, built from the
 * registered redirect_uri). Both are server-validated; the deny value is
 * cross-origin, so safeReturnTo would wrongly reject it. We hand off to the
 * server's value verbatim.
 */
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { api, type ApiError } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { hardRedirect } from '@/lib/navigate'
import CenteredLayout from '@/pages/CenteredLayout.vue'
import ConsentCard from '@/components/custom/ConsentCard.vue'
import ConsentScopeList from '@/components/custom/ConsentScopeList.vue'
import { Button } from '@/components/ui/button'
import { Alert, AlertDescription } from '@/components/ui/alert'
import CardSkeleton from '@/components/custom/CardSkeleton.vue'

interface ConsentClient {
  clientId: string
  displayName: string
  logoUri?: string
  policyUri?: string
  tosUri?: string
}
interface ConsentContext {
  client: ConsentClient
  account: { displayName: string }
  scopes: string[]
  alreadyGranted?: string[]
}
interface ConsentResult {
  redirect: string
}

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const { busy, run, errorText } = useApi()

const ticket = String(route.query.ticket ?? '')
const returnTo = String(route.query.return_to ?? '')

const ctx = ref<ConsentContext | null>(null)
const loading = ref(true)

const isIncremental = computed(() => (ctx.value?.alreadyGranted?.length ?? 0) > 0)
const newScopes = computed(() => {
  const had = new Set(ctx.value?.alreadyGranted ?? [])
  return (ctx.value?.scopes ?? []).filter((s) => !had.has(s))
})

onMounted(async () => {
  try {
    ctx.value = await api.get<ConsentContext>(
      `/api/prohibitorum/consent?ticket=${encodeURIComponent(ticket)}`,
    )
  } catch (e) {
    const code = (e as ApiError | undefined)?.code
    if (code === 'no_session') {
      // Not signed in — send them to login and back here afterwards.
      router.replace({ name: 'login', query: { return_to: route.fullPath } })
    } else {
      router.replace({ name: 'error', query: { error: code ?? 'invalid_consent_ticket' } })
    }
  } finally {
    loading.value = false
  }
})

async function decide(decision: 'approve' | 'deny'): Promise<void> {
  const res = await run(() =>
    api.post<ConsentResult>(
      `/api/prohibitorum/consent?return_to=${encodeURIComponent(returnTo)}`,
      { ticket, decision },
    ),
  )
  if (!res) return
  hardRedirect(res.redirect)
}
</script>

<template>
  <CenteredLayout>
    <template #title>
      <h1 class="text-xl font-semibold tracking-tight text-ink">{{ t('consent.title') }}</h1>
    </template>

    <CardSkeleton v-if="loading" :lines="3" />

    <ConsentCard
      v-else-if="ctx"
      :logo-uri="ctx.client.logoUri"
      :display-name="ctx.client.displayName"
      :account-name="ctx.account.displayName"
      :policy-uri="ctx.client.policyUri"
      :tos-uri="ctx.client.tosUri"
    >
      <template #heading>
        <p class="text-ink">
          {{ isIncremental
            ? t('consent.additionalAccessTitle', { client: ctx.client.displayName })
            : t('consent.requestingAccess', { client: ctx.client.displayName }) }}
        </p>
      </template>
      <template #body>
        <div class="flex flex-col gap-2">
          <p class="text-sm font-medium text-ink">{{ t('consent.scopesHeading') }}</p>
          <ConsentScopeList :scopes="ctx.scopes" :new-scopes="isIncremental ? newScopes : []" />
          <p class="text-xs text-muted">{{ t('consent.remembered', { client: ctx.client.displayName }) }}</p>
          <p class="text-xs text-muted">{{ t('consent.manageHint') }}</p>
        </div>
        <Alert v-if="errorText" variant="destructive" role="alert" aria-live="polite">
          <AlertDescription>{{ errorText }}</AlertDescription>
        </Alert>
      </template>
      <template #actions>
        <div class="flex gap-3">
          <Button variant="outline" class="flex-1" :disabled="busy" @click="decide('deny')">{{ t('consent.deny') }}</Button>
          <Button class="flex-1" :disabled="busy" @click="decide('approve')">{{ t('consent.approveCount', { count: isIncremental ? newScopes.length : ctx.scopes.length }) }}</Button>
        </div>
      </template>
    </ConsentCard>
  </CenteredLayout>
</template>
