<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { Search, Plus, LayoutGrid } from 'lucide-vue-next'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { useAuthStore } from '@/stores/auth'
import { useTransientFlag } from '@/composables/useTransientFlag'
import AppTile, { type LaunchpadApp } from '@/components/custom/AppTile.vue'
import AppIcon from '@/components/custom/AppIcon.vue'
import ProtocolBadge from '@/components/custom/ProtocolBadge.vue'
import EmptyState from '@/components/custom/EmptyState.vue'
import { Skeleton } from '@/components/ui/skeleton'
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

// Search-focus shortcut indicator. The handler accepts ⌘K and Ctrl-K on every
// platform; only the displayed hint differs — the ⌘ glyph on Apple platforms,
// "Ctrl K" elsewhere — so Windows/Linux users aren't shown a key they don't have.
const isApplePlatform = /mac|iphone|ipad|ipod/i.test(
  (navigator as Navigator & { userAgentData?: { platform?: string } }).userAgentData?.platform ||
    navigator.platform ||
    navigator.userAgent,
)
const searchShortcut = isApplePlatform ? '⌘K' : 'Ctrl K'

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

// The grant backing a tile's access mark + revoke action. OIDC apps carry a
// per-scope consent record; SAML apps carry a scope-less acknowledgement.
// forward-auth is always-on at the proxy, so it has no revocable grant.
const oidcConsentByClient = computed(() => {
  const m = new Map<string, Consent>()
  for (const c of consentList.value) if ((c.kind ?? 'oidc') === 'oidc') m.set(c.clientId, c)
  return m
})
function consentFor(app: LaunchpadApp): Consent | null {
  if (app.kind === 'oidc') return oidcConsentByClient.value.get(app.id) ?? null
  if (app.kind === 'saml' && connectedKeys.value.has(`saml:${app.id}`)) {
    return { kind: 'saml', clientId: app.id, scopes: [] }
  }
  return null
}

function adminPathFor(app: LaunchpadApp): string {
  switch (app.kind) {
    case 'oidc': return `/admin/oidc-applications/${app.id}`
    case 'forward_auth': return `/admin/forward-auth-apps/${app.id}`
    case 'saml': return `/admin/saml-applications/${app.id}`
  }
}

async function load(): Promise<void> {
  // Both lists are required to render the connected-vs-available split correctly.
  // Fetch them under one run() so a failure of EITHER surfaces as an error rather
  // than silently degrading (a swallowed /me/consent would drop every "connected"
  // mark and push connected apps back into the "Add app" picker).
  const result = await run(async () => {
    const [a, c] = await Promise.all([
      api.get<LaunchpadApp[]>('/api/prohibitorum/me/apps'),
      api.get<Consent[]>('/api/prohibitorum/me/consent'),
    ])
    return { a, c }
  })
  if (result) {
    apps.value = result.a
    consentList.value = result.c
  }
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
    // Send `kind` explicitly (matches AppAccessView) so the backend targets the
    // right consent table — the SP numeric id and an OIDC client_id could collide.
    await api.post('/api/prohibitorum/me/consent/revoke', { kind: app.kind, clientId: app.id })
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
        <kbd class="pointer-events-none absolute right-2 top-1/2 hidden -translate-y-1/2 rounded border border-line bg-card px-1.5 py-0.5 text-[0.625rem] font-medium text-muted sm:block">{{ searchShortcut }}</kbd>
      </div>
    </div>

    <Alert v-if="errorText" variant="destructive" role="alert" aria-live="polite">
      <AlertDescription>{{ errorText }}</AlertDescription>
    </Alert>
    <StatusMessage :show="copied">{{ t('myApps.copied') }}</StatusMessage>

    <!-- Loading: placeholder tiles in the same grid the real apps use, so there's
         no row-to-card jump when data arrives. -->
    <div
      v-if="busy && !apps.length"
      role="status"
      aria-busy="true"
      aria-live="polite"
      class="grid gap-4 [grid-template-columns:repeat(auto-fill,minmax(min(100%,11rem),1fr))]"
    >
      <div v-for="n in 6" :key="n" class="overflow-hidden rounded-2xl border border-line bg-card">
        <Skeleton class="aspect-[4/3] w-full rounded-none" />
        <div class="border-t border-line px-3 py-2.5">
          <Skeleton class="h-4 w-2/3" />
        </div>
      </div>
      <span class="sr-only">{{ t('common.loading') }}</span>
    </div>

    <template v-else-if="connectedApps.length || availableApps.length">
      <ul role="list" class="grid gap-4 [grid-template-columns:repeat(auto-fill,minmax(min(100%,11rem),1fr))]">
        <li v-for="app in visibleConnected" :key="`${app.kind}:${app.id}`">
          <AppTile
            :app="app"
            :consent="consentFor(app)"
            :is-admin="auth.isAdmin"
            @revoke="revokeTarget = $event"
            @copy="copyLink"
            @manage="manage"
          />
        </li>
        <!-- Add-app tile — opens the picker of authorized-but-not-connected apps. -->
        <li v-if="availableApps.length && !search">
          <button
            type="button"
            class="group flex h-full min-h-[10rem] w-full cursor-pointer flex-col items-center justify-center gap-1 rounded-2xl border border-dashed border-border-strong text-muted outline-none transition-colors hover:border-ring hover:text-ink focus-visible:ring-2 focus-visible:ring-ring"
            :data-test="'add-app'"
            @click="pickerOpen = true"
          >
            <Plus class="size-6" aria-hidden="true" />
            <span class="text-sm font-medium">{{ t('myApps.addApp') }}</span>
            <span class="text-xs">{{ t('myApps.addAppHint') }}</span>
          </button>
        </li>
      </ul>

      <p v-if="search && !visibleConnected.length" role="status" class="text-sm text-muted">{{ t('myApps.noMatches', { query: search }) }}</p>
      <p v-else-if="!connectedApps.length && availableApps.length" role="status" class="text-sm text-muted">{{ t('myApps.connectFirst') }}</p>
    </template>

    <EmptyState v-else-if="!errorText" :icon="LayoutGrid" :title="t('myApps.empty')" :description="t('myApps.emptyHelp')" />

    <!-- Connect-an-app picker: authorized apps the user hasn't connected yet. -->
    <Dialog :open="pickerOpen" @update:open="pickerOpen = $event">
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{{ t('myApps.addAppTitle') }}</DialogTitle>
          <DialogDescription>{{ t('myApps.addAppDesc') }}</DialogDescription>
        </DialogHeader>
        <ul class="flex max-h-[60vh] flex-col gap-1 overflow-y-auto">
          <li
            v-for="app in availableApps"
            :key="`${app.kind}:${app.id}`"
            class="relative flex items-center gap-3 rounded-lg p-2 transition-colors hover:bg-accent"
          >
            <!-- Stretched launch overlay: covers the row, owns the focus ring.
                 Kept FIRST so the dialog's open-autofocus lands on the launch
                 link, not the protocol badge (whose tooltip would otherwise pop
                 open unprompted when the dialog appears). -->
            <a
              :href="app.launchUrl"
              target="_blank"
              rel="noopener noreferrer"
              :aria-label="t('myApps.open', { name: app.name })"
              class="absolute inset-0 z-0 rounded-lg outline-none focus-visible:ring-2 focus-visible:ring-ring"
              :data-test="`connect-${app.kind}-${app.id}`"
              @click="pickerOpen = false"
            />
            <AppIcon :src="app.iconUrl" :name="app.name" size="sm" />
            <span class="min-w-0 flex-1 truncate text-sm font-medium text-ink">{{ app.name }}</span>
            <!-- Protocol glyph + hover tooltip, matching the launchpad tiles. Sits
                 above the stretched launch anchor so hovering it shows the hint
                 without launching the app. -->
            <ProtocolBadge
              :kind="app.kind"
              class="relative z-[1] size-7 shrink-0 rounded-full bg-sunken text-muted ring-1 ring-line hover:text-ink"
            />
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
