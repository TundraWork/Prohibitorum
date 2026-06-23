<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { Search, Plus } from 'lucide-vue-next'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { useAuthStore } from '@/stores/auth'
import { useTransientFlag } from '@/composables/useTransientFlag'
import AppTile, { type LaunchpadApp } from '@/components/custom/AppTile.vue'
import AppIcon from '@/components/custom/AppIcon.vue'
import EmptyState from '@/components/custom/EmptyState.vue'
import TableSkeleton from '@/components/custom/TableSkeleton.vue'
import StatusMessage from '@/components/custom/StatusMessage.vue'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from '@/components/ui/dialog'
import ConfirmDialog from '@/components/custom/ConfirmDialog.vue'

// A record from /me/consent. `kind` distinguishes OIDC consents from SAML
// acknowledgements (unified in the consent-experience work); payloads without
// `kind` are treated as OIDC for back-compat.
interface Consent { kind?: 'oidc' | 'saml'; clientId: string; scopes: string[] }

const { t } = useI18n()
const { busy, run, errorText } = useApi()
const auth = useAuthStore()
const router = useRouter()

const apps = ref<LaunchpadApp[]>([])
const consentList = ref<Consent[]>([])
const revokeTarget = ref<LaunchpadApp | null>(null)
const pickerOpen = ref(false)
const search = ref('')
const searchInput = ref<HTMLInputElement | null>(null)
const { flag: copied, trigger: triggerCopied } = useTransientFlag()

const firstName = computed(() => (auth.me?.displayName ?? '').split(' ')[0] || auth.me?.username || '')

const greeting = computed(() => {
  const h = new Date().getHours()
  const part = h < 12 ? 'morning' : h < 18 ? 'afternoon' : 'evening'
  return t(`myApps.greet.${part}`, { name: firstName.value })
})

// "Connected" = the user has consented to (OIDC) or acknowledged (SAML) the app.
// forward-auth has no consent step, so it is always connected once authorized.
const connectedKeys = computed(() => {
  const s = new Set<string>()
  for (const c of consentList.value) s.add(`${c.kind ?? 'oidc'}:${c.clientId}`)
  return s
})
function isConnected(app: LaunchpadApp): boolean {
  return app.kind === 'forward_auth' || connectedKeys.value.has(`${app.kind}:${app.id}`)
}
const connectedApps = computed(() => apps.value.filter(isConnected))
const availableApps = computed(() => apps.value.filter((a) => !isConnected(a)))

const visibleConnected = computed(() => {
  const q = search.value.trim().toLowerCase()
  return q ? connectedApps.value.filter((a) => a.name.toLowerCase().includes(q)) : connectedApps.value
})

// Only OIDC apps carry a per-scope consent record (→ tile access mark + revoke).
const oidcConsentByClient = computed(() => {
  const m = new Map<string, Consent>()
  for (const c of consentList.value) if ((c.kind ?? 'oidc') === 'oidc') m.set(c.clientId, c)
  return m
})
function consentFor(app: LaunchpadApp): Consent | null {
  return app.kind === 'oidc' ? oidcConsentByClient.value.get(app.id) ?? null : null
}

function adminPathFor(app: LaunchpadApp): string {
  switch (app.kind) {
    case 'oidc': return `/admin/oidc-applications/${app.id}`
    case 'forward_auth': return `/admin/forward-auth-apps/${app.id}`
    case 'saml': return `/admin/saml-applications/${app.id}`
  }
}

async function load(): Promise<void> {
  const [a, c] = await Promise.all([
    run(() => api.get<LaunchpadApp[]>('/api/prohibitorum/me/apps')),
    api.get<Consent[]>('/api/prohibitorum/me/consent').catch(() => [] as Consent[]),
  ])
  if (a) apps.value = a
  consentList.value = c
}

async function copyLink(app: LaunchpadApp): Promise<void> {
  // launchUrl may be relative (SAML SSO-init) — resolve to absolute so the copied
  // link works pasted anywhere.
  const url = new URL(app.launchUrl, window.location.origin).href
  try {
    await navigator.clipboard.writeText(url)
    triggerCopied()
  } catch {
    // Clipboard API unavailable (insecure context / denied) — fail quietly.
  }
}

function manage(app: LaunchpadApp): void { void router.push(adminPathFor(app)) }

async function confirmRevoke(): Promise<void> {
  const app = revokeTarget.value
  if (!app) return
  const ok = await run(async () => {
    await api.post('/api/prohibitorum/me/consent/revoke', { clientId: app.id })
    return true as const
  })
  revokeTarget.value = null
  if (ok) await load()
}

// ⌘K / Ctrl-K (or "/" when not already typing) focuses the search box.
function onKeydown(e: KeyboardEvent): void {
  const typing = (el: EventTarget | null): boolean => {
    const n = el as HTMLElement | null
    return !!n && (n.tagName === 'INPUT' || n.tagName === 'TEXTAREA' || n.isContentEditable)
  }
  if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
    e.preventDefault()
    searchInput.value?.focus()
  } else if (e.key === '/' && !typing(e.target)) {
    e.preventDefault()
    searchInput.value?.focus()
  }
}

onMounted(() => {
  void load()
  window.addEventListener('keydown', onKeydown)
})
onUnmounted(() => window.removeEventListener('keydown', onKeydown))
</script>

<template>
  <div class="flex flex-col gap-6">
    <!-- Greeting + frontend search -->
    <div class="flex flex-col gap-4 sm:flex-row sm:items-end sm:justify-between">
      <div class="min-w-0">
        <h1 class="text-3xl font-semibold tracking-tight text-ink">{{ greeting }} <span aria-hidden="true">👋</span></h1>
        <p class="mt-1 text-sm text-muted">{{ t('myApps.tagline') }}</p>
      </div>
      <div class="relative w-full sm:w-72">
        <Search class="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted" aria-hidden="true" />
        <input
          ref="searchInput"
          v-model="search"
          type="text"
          :placeholder="t('myApps.searchPlaceholder')"
          :aria-label="t('myApps.searchPlaceholder')"
          class="h-9 w-full rounded-md border border-input bg-sunken pl-9 pr-12 text-sm text-ink outline-none placeholder:text-muted focus-visible:ring-2 focus-visible:ring-ring"
        />
        <kbd class="pointer-events-none absolute right-2 top-1/2 hidden -translate-y-1/2 rounded border border-line bg-card px-1.5 py-0.5 text-[0.625rem] font-medium text-muted sm:block">⌘K</kbd>
      </div>
    </div>

    <Alert v-if="errorText" variant="destructive" role="alert" aria-live="polite">
      <AlertDescription>{{ errorText }}</AlertDescription>
    </Alert>
    <StatusMessage :show="copied">{{ t('myApps.copied') }}</StatusMessage>

    <TableSkeleton v-if="busy && !apps.length" :rows="2" :cols="3" />

    <template v-else-if="connectedApps.length || availableApps.length">
      <div class="grid gap-4 [grid-template-columns:repeat(auto-fill,minmax(min(100%,11rem),1fr))]">
        <AppTile
          v-for="app in visibleConnected"
          :key="`${app.kind}:${app.id}`"
          :app="app"
          :consent="consentFor(app)"
          :is-admin="auth.isAdmin"
          @revoke="revokeTarget = $event"
          @copy="copyLink"
          @manage="manage"
        />
        <!-- Add-app tile — opens the picker of authorized-but-not-connected apps. -->
        <button
          v-if="availableApps.length && !search"
          type="button"
          class="group flex h-full min-h-[10rem] flex-col items-center justify-center gap-1 rounded-2xl border border-dashed border-border-strong text-muted outline-none transition-colors hover:border-ring hover:text-ink focus-visible:ring-2 focus-visible:ring-ring"
          :data-test="'add-app'"
          @click="pickerOpen = true"
        >
          <Plus class="size-6" aria-hidden="true" />
          <span class="text-sm font-medium">{{ t('myApps.addApp') }}</span>
          <span class="text-xs">{{ t('myApps.addAppHint') }}</span>
        </button>
      </div>

      <p v-if="search && !visibleConnected.length" class="text-sm text-muted">{{ t('myApps.noMatches', { query: search }) }}</p>
      <p v-else-if="!connectedApps.length && availableApps.length" class="text-sm text-muted">{{ t('myApps.connectFirst') }}</p>
    </template>

    <EmptyState v-else-if="!errorText" :title="t('myApps.empty')" :description="t('myApps.emptyHelp')" />

    <!-- Connect-an-app picker: authorized apps the user hasn't connected yet. -->
    <Dialog :open="pickerOpen" @update:open="pickerOpen = $event">
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{{ t('myApps.addAppTitle') }}</DialogTitle>
          <DialogDescription>{{ t('myApps.addAppDesc') }}</DialogDescription>
        </DialogHeader>
        <ul class="flex max-h-[60vh] flex-col gap-1 overflow-y-auto">
          <li v-for="app in availableApps" :key="`${app.kind}:${app.id}`">
            <a
              :href="app.launchUrl"
              target="_blank"
              rel="noopener noreferrer"
              class="flex items-center gap-3 rounded-lg p-2 outline-none hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring"
              :data-test="`connect-${app.kind}-${app.id}`"
              @click="pickerOpen = false"
            >
              <AppIcon :src="app.iconUrl" :name="app.name" size="sm" />
              <span class="min-w-0 flex-1 truncate text-sm font-medium text-ink">{{ app.name }}</span>
              <span class="shrink-0 rounded-full bg-sunken px-2 py-0.5 text-[0.625rem] font-medium uppercase text-muted">{{ t(`myApps.type.${app.kind}`) }}</span>
            </a>
          </li>
        </ul>
      </DialogContent>
    </Dialog>

    <ConfirmDialog
      :open="revokeTarget !== null"
      :title="t('myApps.revokeConfirmTitle')"
      :confirm-label="t('myApps.revoke')"
      :busy="busy"
      @update:open="(v) => { if (!v) revokeTarget = null }"
      @cancel="revokeTarget = null"
      @confirm="confirmRevoke"
    >
      {{ t('myApps.revokeConfirmBody', { name: revokeTarget?.name }) }}
    </ConfirmDialog>
  </div>
</template>
