<script setup lang="ts">
import { onMounted, ref, computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { api, type ApiError } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { hardRedirect } from '@/lib/navigate'
import CenteredLayout from '@/pages/CenteredLayout.vue'
import ConsentCard from '@/components/custom/ConsentCard.vue'
import { Button } from '@/components/ui/button'
import { Alert, AlertDescription } from '@/components/ui/alert'
import CardSkeleton from '@/components/custom/CardSkeleton.vue'

interface SamlConsentContext {
  sp: { id: string; displayName: string; logoUri?: string }
  account: { displayName: string; avatarUrl?: string }
  attributes: string[]
}
interface ConsentResult { redirect: string }

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const { busy, run, errorText } = useApi()

const ticket = String(route.query.ticket ?? '')
const ctx = ref<SamlConsentContext | null>(null)
const loading = ref(true)
const hasAttrs = computed(() => (ctx.value?.attributes.length ?? 0) > 0)

onMounted(async () => {
  try {
    ctx.value = await api.get<SamlConsentContext>(`/api/prohibitorum/saml-consent?ticket=${encodeURIComponent(ticket)}`)
  } catch (e) {
    const code = (e as ApiError | undefined)?.code
    if (code === 'no_session') router.replace({ name: 'login', query: { return_to: route.fullPath } })
    else router.replace({ name: 'error', query: { error: code ?? 'invalid_consent_ticket' } })
  } finally {
    loading.value = false
  }
})

async function decide(decision: 'approve' | 'decline'): Promise<void> {
  const res = await run(() => api.post<ConsentResult>('/api/prohibitorum/saml-consent', { ticket, decision }))
  if (!res) return
  hardRedirect(res.redirect)
}
</script>

<template>
  <CenteredLayout>
    <template #title>
      <h1 class="text-xl font-semibold tracking-tight text-ink">{{ ctx ? t('samlConsent.title', { sp: ctx.sp.displayName }) : t('title.samlConsent') }}</h1>
    </template>

    <CardSkeleton v-if="loading" :lines="3" />

    <ConsentCard
      v-else-if="ctx"
      :logo-uri="ctx.sp.logoUri"
      :display-name="ctx.sp.displayName"
      :account-name="ctx.account.displayName"
      :account-avatar-url="ctx.account.avatarUrl"
    >
      <template #heading>
        <p class="text-ink">{{ t('samlConsent.intro', { sp: ctx.sp.displayName }) }}</p>
      </template>
      <template #body>
        <div class="flex flex-col gap-2">
          <p class="text-sm font-medium text-ink">{{ t('samlConsent.receives', { sp: ctx.sp.displayName }) }}</p>
          <ul v-if="hasAttrs" class="list-disc pl-5 text-sm text-ink">
            <li v-for="a in ctx.attributes" :key="a">{{ a }}</li>
          </ul>
          <p v-else class="text-sm text-ink">{{ t('samlConsent.genericAttributes') }}</p>
          <p class="text-xs text-muted">{{ t('samlConsent.remembered') }}</p>
        </div>
        <Alert v-if="errorText" variant="destructive" role="alert" aria-live="polite">
          <AlertDescription>{{ errorText }}</AlertDescription>
        </Alert>
      </template>
      <template #actions>
        <div class="flex gap-3">
          <Button variant="outline" class="flex-1" :disabled="busy" @click="decide('decline')">{{ t('samlConsent.decline') }}</Button>
          <Button class="flex-1" :disabled="busy" @click="decide('approve')">{{ t('samlConsent.continue') }}</Button>
        </div>
      </template>
    </ConsentCard>
  </CenteredLayout>
</template>
