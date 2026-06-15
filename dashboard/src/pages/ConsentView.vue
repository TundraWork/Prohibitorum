<script setup lang="ts">
/**
 * ConsentView — the OIDC consent screen (/consent?ticket=…&return_to=…).
 *
 * Contract (pkg/server/handle_consent.go — verified):
 *   GET  /api/prohibitorum/consent?ticket=<t>
 *        → { client{clientId,displayName,…}, account{displayName}, scopes[] }
 *        requires a session; no_session → /login, invalid ticket → /error.
 *   POST /api/prohibitorum/consent?return_to=<authorizeURL>
 *        body { ticket, decision: 'approve' | 'deny' }  → { redirect }
 *
 * The `ticket` and `return_to` both arrive as PAGE query params (the OP
 * redirects the browser here). return_to is a POST *query* param, not body.
 *
 * Redirect handling — deliberately NOT guarded by safeReturnTo: the server
 * returns either the absolute issuer authorize URL (approve, validated
 * same-origin-as-issuer server-side) or the cross-origin RP redirect_uri with
 * error=access_denied (deny, built from the registered redirect_uri). Both are
 * server-validated; safeReturnTo only permits same-origin RELATIVE paths and
 * would wrongly reject both. We hand off to the server's value verbatim.
 */
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { api, type ApiError } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { hardRedirect } from '@/lib/navigate'
import CenteredLayout from '@/pages/CenteredLayout.vue'
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

    <div v-else-if="ctx" class="flex flex-col gap-6">
      <div class="flex flex-col items-center gap-2 text-center">
        <img
          v-if="ctx.client.logoUri"
          :src="ctx.client.logoUri"
          :alt="ctx.client.displayName"
          class="size-12 rounded-md object-contain"
        />
        <p class="text-ink">{{ t('consent.requestingAccess', { client: ctx.client.displayName }) }}</p>
        <p class="text-sm text-muted">
          {{ t('consent.yourAccount', { displayName: ctx.account.displayName }) }}
        </p>
      </div>

      <div class="flex flex-col gap-2">
        <p class="text-sm font-medium text-ink">{{ t('consent.scopesHeading') }}</p>
        <ConsentScopeList :scopes="ctx.scopes" />
      </div>

      <Alert v-if="errorText" variant="destructive" role="alert" aria-live="polite">
        <AlertDescription>{{ errorText }}</AlertDescription>
      </Alert>

      <div class="flex gap-3">
        <Button variant="outline" class="flex-1" :disabled="busy" @click="decide('deny')">
          {{ t('consent.deny') }}
        </Button>
        <Button class="flex-1" :disabled="busy" @click="decide('approve')">
          {{ t('consent.approveCount', { count: ctx.scopes.length }) }}
        </Button>
      </div>

      <p v-if="ctx.client.policyUri || ctx.client.tosUri" class="text-center text-xs text-muted">
        <a
          v-if="ctx.client.policyUri"
          :href="ctx.client.policyUri"
          target="_blank"
          rel="noopener noreferrer"
          class="underline-offset-4 hover:underline"
        >{{ t('consent.privacyPolicy') }}</a>
        <span v-if="ctx.client.policyUri && ctx.client.tosUri"> &middot; </span>
        <a
          v-if="ctx.client.tosUri"
          :href="ctx.client.tosUri"
          target="_blank"
          rel="noopener noreferrer"
          class="underline-offset-4 hover:underline"
        >{{ t('consent.termsOfService') }}</a>
      </p>
    </div>
  </CenteredLayout>
</template>
