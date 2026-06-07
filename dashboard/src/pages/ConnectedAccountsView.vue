<script setup lang="ts">
/**
 * ConnectedAccountsView (/connected) — manage federated identities.
 * GET /me/identities lists links; unlink is sudo-gated + confirmed; linking a
 * new provider needs a PROACTIVE sudo step (the begin endpoint is a sudo-gated
 * 302 that withSudo's XHR-retry can't replay), then a hard redirect upstream.
 */
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { withSudo, ensureSudo } from '@/lib/sudo'
import { hardRedirect } from '@/lib/navigate'
import { Link2 } from 'lucide-vue-next'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Alert, AlertDescription } from '@/components/ui/alert'
import ConfirmDialog from '@/components/custom/ConfirmDialog.vue'

interface Identity {
  id: number
  idpSlug: string
  idpDisplayName: string
  upstreamEmail: string | null
  linkedAt: string
}
interface Provider { slug: string; displayName: string }

const { t, te } = useI18n()
const { busy, error, run } = useApi()

const identities = ref<Identity[]>([])
const providers = ref<Provider[]>([])
const providersLoaded = ref(false)
const confirmId = ref<number | null>(null)

const fmt = (d: string) => { const ms = Date.parse(d); return Number.isNaN(ms) ? '' : new Date(ms).toLocaleDateString() }
const errorText = computed(() => {
  const e = error.value
  if (!e) return ''
  const key = `errors.${e.code}`
  return te(key) ? t(key) : e.message || t('common.error')
})
const linkedSlugs = computed(() => new Set(identities.value.map((i) => i.idpSlug)))

async function loadIdentities(): Promise<void> {
  const res = await run(() => api.get<Identity[]>('/api/prohibitorum/me/identities'))
  if (res) identities.value = res
}
async function loadProviders(): Promise<void> {
  try {
    providers.value = await api.get<Provider[]>('/api/prohibitorum/auth/federation')
  } catch {
    providers.value = []
  } finally {
    providersLoaded.value = true
  }
}
async function confirmUnlink(): Promise<void> {
  const id = confirmId.value
  if (id == null) return
  const ok = await run(() => withSudo(async () => {
    await api.post(`/api/prohibitorum/me/identities/${id}/unlink`)
    return true as const
  }))
  confirmId.value = null
  if (ok) await loadIdentities()
}
async function link(slug: string): Promise<void> {
  const elevated = await ensureSudo()
  if (!elevated) return
  hardRedirect(
    `/api/prohibitorum/me/identities/link/${encodeURIComponent(slug)}/begin?return_to=${encodeURIComponent('/connected')}`)
}

onMounted(async () => { await Promise.all([loadIdentities(), loadProviders()]) })
</script>

<template>
  <div class="flex max-w-2xl flex-col gap-6">
    <h1 class="text-2xl font-semibold tracking-tight text-ink">{{ t('connected.title') }}</h1>
    <p class="text-sm text-muted">{{ t('connected.help') }}</p>

    <Alert v-if="errorText" variant="destructive" role="alert" aria-live="polite">
      <AlertDescription>{{ errorText }}</AlertDescription>
    </Alert>

    <Card v-for="ident in identities" :key="ident.id">
      <CardContent class="flex items-center justify-between gap-4 py-4">
        <div class="flex min-w-0 flex-1 flex-col gap-1 text-sm">
          <span class="min-w-0 truncate font-medium text-ink">{{ ident.idpDisplayName }}</span>
          <span v-if="ident.upstreamEmail" class="min-w-0 truncate text-muted">{{ ident.upstreamEmail }}</span>
          <span v-if="fmt(ident.linkedAt)" class="truncate text-muted">{{ t('connected.linked') }}: {{ fmt(ident.linkedAt) }}</span>
        </div>
        <Button variant="outline" size="sm" class="shrink-0" :disabled="busy"
                :data-test="`unlink-${ident.id}`" @click="confirmId = ident.id">
          {{ t('connected.unlink') }}
        </Button>
      </CardContent>
    </Card>

    <p v-if="!busy && identities.length === 0 && !errorText" class="text-sm text-muted">
      {{ t('connected.empty') }}
    </p>

    <Card>
      <CardHeader>
        <CardTitle class="flex items-center gap-2">
          <Link2 class="size-4 shrink-0" aria-hidden="true" />
          {{ t('connected.linkHeading') }}
        </CardTitle>
      </CardHeader>
      <CardContent class="flex flex-col gap-3">
        <p class="text-sm text-muted">{{ t('connected.linkHelp') }}</p>
        <p v-if="providersLoaded && providers.length === 0" class="text-sm text-muted">{{ t('connected.noProviders') }}</p>
        <div v-else-if="providers.length > 0" class="flex flex-col gap-2">
          <Button v-for="p in providers" :key="p.slug" type="button" variant="outline" class="w-full justify-between"
                  :disabled="linkedSlugs.has(p.slug) || busy" :data-test="`link-${p.slug}`" @click="link(p.slug)">
            <span>{{ p.displayName }}</span>
            <span v-if="linkedSlugs.has(p.slug)" class="text-xs text-muted">{{ t('connected.alreadyLinked') }}</span>
          </Button>
        </div>
      </CardContent>
    </Card>

    <ConfirmDialog
      :open="confirmId !== null"
      :title="t('connected.unlinkConfirmTitle')"
      :confirm-label="t('connected.unlink')"
      :busy="busy"
      @update:open="(v) => { if (!v) confirmId = null }"
      @cancel="confirmId = null"
      @confirm="confirmUnlink"
    >
      {{ t('connected.unlinkConfirmBody') }}
    </ConfirmDialog>
  </div>
</template>
