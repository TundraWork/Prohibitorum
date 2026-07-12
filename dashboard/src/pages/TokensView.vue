<script setup lang="ts">
/**
 * TokensView (/tokens) — list/revoke the caller's personal access tokens and
 * create one via a dialog that reveals the plaintext exactly once.
 *
 * GET  /me/tokens                  → PersonalAccessTokenView[]
 * GET  /me/forward-auth-apps       → FAApp[]
 * POST /me/tokens                  → { token: string, pat: PersonalAccessTokenView }  (sudo-gated)
 * POST /me/tokens/revoke {id}      → 204
 */
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Copy, Check, Terminal } from 'lucide-vue-next'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { withSudo } from '@/lib/sudo'
import { relativeTime, formatDateTime } from '@/lib/time'
import { Card, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Checkbox } from '@/components/ui/checkbox'
import { Switch } from '@/components/ui/switch'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Select, SelectTrigger, SelectValue, SelectContent, SelectItem } from '@/components/ui/select'
import StatusBadge from '@/components/custom/StatusBadge.vue'
import ConfirmDialog from '@/components/custom/ConfirmDialog.vue'
import TableSkeleton from '@/components/custom/TableSkeleton.vue'
import EmptyState from '@/components/custom/EmptyState.vue'
import ErrorPanel from '@/components/custom/ErrorPanel.vue'

interface FAApp {
  clientId: string
  displayName: string
  scopes: { name: string; description?: string }[]
}

interface PersonalAccessTokenView {
  id: number
  name: string
  tokenHint: string
  allApps: boolean
  appGrants: Record<string, string[]>
  createdAt: string
  expiresAt?: string
  lastUsedAt?: string
}

const { t } = useI18n()
const { busy, run, error, clear, errorText } = useApi()
// Separate instance so the apps prefetch never collides with the token-list
// busy-guard (opening the dialog mid-prefetch must not no-op the apps fetch).
const appsApi = useApi()

const rows = ref<PersonalAccessTokenView[]>([])
const confirmRevokeId = ref<number | null>(null)

// Forward-auth apps (loaded on mount + on dialog open, for picker + name resolution)
const apps = ref<FAApp[]>([])

// Create dialog state
const createOpen = ref(false)
const newName = ref('')
const newExpiry = ref('0')  // '0' = no expiry, '30', '90' = days
const allApps = ref(false)
const grants = ref<Record<string, string[]>>({})

// Reveal state (after creation)
const revealToken = ref<string | null>(null)
const copied = ref(false)
const copyFailed = ref(false)
const savedConfirmed = ref(false)

function isExpired(pat: PersonalAccessTokenView): boolean {
  return !!pat.expiresAt && new Date(pat.expiresAt) < new Date()
}

/** Resolve a clientId to its display name via the loaded apps list. */
function appName(clientId: string): string {
  return apps.value.find((a) => a.clientId === clientId)?.displayName ?? clientId
}

function toggleApp(cid: string, on: boolean): void {
  if (on) {
    grants.value = { ...grants.value, [cid]: [] }
  } else {
    const g = { ...grants.value }
    delete g[cid]
    grants.value = g
  }
}

function toggleScope(cid: string, name: string, on: boolean): void {
  const cur = grants.value[cid] ?? []
  grants.value = {
    ...grants.value,
    [cid]: on ? [...cur, name] : cur.filter((s) => s !== name),
  }
}

const canSubmit = computed(
  () => newName.value.trim() && (allApps.value || Object.keys(grants.value).length > 0),
)

async function loadApps(): Promise<void> {
  const res = await appsApi.run(() => api.get<FAApp[]>('/api/prohibitorum/me/forward-auth-apps'))
  if (res) apps.value = res
}

async function load(): Promise<void> {
  const res = await run(() => api.get<PersonalAccessTokenView[]>('/api/prohibitorum/me/tokens'))
  if (res) rows.value = res
}

async function revoke(): Promise<void> {
  const id = confirmRevokeId.value
  if (id == null) return
  const ok = await run(async () => {
    await api.post('/api/prohibitorum/me/tokens/revoke', { id })
    return true as const
  })
  confirmRevokeId.value = null
  if (ok) await load()
}

async function create(): Promise<void> {
  const body = {
    name: newName.value.trim(),
    expiresInDays: parseInt(newExpiry.value, 10) || undefined,
    allApps: allApps.value,
    appGrants: allApps.value ? {} : grants.value,
  }

  const res = await run(() =>
    withSudo(
      () => api.post<{ token: string; pat: PersonalAccessTokenView }>('/api/prohibitorum/me/tokens', body),
      t('sudo.reason.createToken'),
    ),
  )
  if (!res) return

  // Reload the list so the new token appears immediately in the background.
  void load()

  // Transition to the reveal state.
  revealToken.value = res.token
  savedConfirmed.value = false
  copied.value = false
  copyFailed.value = false
}

async function copyToken(): Promise<void> {
  if (!revealToken.value) return
  try {
    await navigator.clipboard.writeText(revealToken.value)
    copied.value = true
    setTimeout(() => { copied.value = false }, 1500)
  } catch {
    copyFailed.value = true
  }
}

async function openCreate(): Promise<void> {
  newName.value = ''
  newExpiry.value = '0'
  allApps.value = false
  grants.value = {}
  revealToken.value = null
  savedConfirmed.value = false
  copied.value = false
  copyFailed.value = false
  createOpen.value = true
  // Fetch the apps list so the picker is populated.
  await loadApps()
}

function closeCreate(): void {
  createOpen.value = false
  // Clear the secret on close — never persists after the dialog is dismissed.
  revealToken.value = null
}

onMounted(async () => {
  await load()
  // Load apps list in the background for name resolution in the list view.
  void loadApps()
})
</script>

<template>
  <div class="flex max-w-2xl flex-col gap-6">
    <div class="flex items-center justify-between gap-4">
      <h1 class="text-2xl font-semibold tracking-tight text-ink">{{ t('tokens.title') }}</h1>
      <Button type="button" data-test="new-token" @click="openCreate">{{ t('tokens.create') }}</Button>
    </div>

    <p class="text-sm text-muted">{{ t('tokens.intro') }}</p>

    <ErrorPanel :error="error" @dismiss="clear" />

    <TableSkeleton v-if="busy && !rows.length" :rows="3" :cols="1" />
    <template v-else-if="rows.length">
      <Card v-for="r in rows" :key="r.id">
        <CardContent class="flex items-center justify-between gap-4 py-4">
          <div class="flex min-w-0 flex-1 flex-col gap-1 text-sm">
            <div class="flex min-w-0 items-center gap-2">
              <span class="min-w-0 truncate font-medium text-ink" data-test="token-name">{{ r.name }}</span>
              <StatusBadge v-if="isExpired(r)" variant="danger" class="shrink-0">{{ t('tokens.expired') }}</StatusBadge>
            </div>
            <span class="truncate text-muted">{{ t('tokens.hint') }}: <span class="font-mono">{{ r.tokenHint }}</span></span>
            <span class="truncate text-muted">{{ t('tokens.created') }}: {{ relativeTime(r.createdAt) }}</span>
            <span v-if="r.expiresAt" class="truncate text-muted">{{ t('tokens.expires') }}: {{ formatDateTime(r.expiresAt) }}</span>
            <span v-else class="truncate text-muted">{{ t('tokens.expires') }}: {{ t('tokens.expiryNever') }}</span>
            <span class="truncate text-muted">
              {{ t('tokens.lastUsed') }}:
              <template v-if="r.lastUsedAt">{{ relativeTime(r.lastUsedAt) }}</template>
              <template v-else>{{ t('tokens.neverUsed') }}</template>
            </span>
            <!-- Per-app grants display -->
            <div v-if="r.allApps" class="mt-1">
              <span class="inline-flex items-center rounded border border-border bg-sunken px-1.5 py-0.5 text-xs text-muted">
                {{ t('tokens.allAppsLabel') }}
              </span>
            </div>
            <div v-else-if="Object.keys(r.appGrants).length" class="mt-1 flex flex-col gap-1">
              <div v-for="(scopes, clientId) in r.appGrants" :key="clientId" class="flex flex-wrap items-center gap-1">
                <span class="text-xs font-medium text-ink">{{ appName(String(clientId)) }}</span>
                <template v-if="scopes.length">
                  <span
                    v-for="s in scopes"
                    :key="s"
                    class="inline-flex items-center rounded border border-border bg-sunken px-1.5 py-0.5 font-mono text-xs text-muted"
                  >{{ s }}</span>
                </template>
                <span v-else class="text-xs text-muted">({{ t('tokens.allScopesLabel') }})</span>
              </div>
            </div>
          </div>
          <Button variant="outline" size="sm" class="shrink-0" :disabled="busy" data-test="revoke" @click="confirmRevokeId = r.id">
            {{ t('tokens.revoke') }}
          </Button>
        </CardContent>
      </Card>
    </template>
    <EmptyState v-else-if="!error" :icon="Terminal" :title="t('tokens.empty')" />

    <ConfirmDialog
      :open="confirmRevokeId !== null"
      :title="t('tokens.revokeConfirmTitle')"
      :confirm-label="t('tokens.revoke')"
      :busy="busy"
      @update:open="(v) => { if (!v) confirmRevokeId = null }"
      @cancel="confirmRevokeId = null"
      @confirm="revoke"
    >
      {{ t('tokens.revokeConfirmBody') }}
    </ConfirmDialog>

    <!-- Create / Reveal dialog -->
    <Dialog :open="createOpen" @update:open="(v) => { if (!v) closeCreate() }">
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{{ revealToken ? t('tokens.revealTitle') : t('tokens.createTitle') }}</DialogTitle>
        </DialogHeader>

        <!-- Form state: shown before creation -->
        <template v-if="!revealToken">
          <div class="flex flex-col gap-4">
            <div class="flex flex-col gap-1.5">
              <Label for="token-name">{{ t('tokens.nameLabel') }}</Label>
              <Input
                id="token-name"
                v-model="newName"
                name="name"
                :placeholder="t('tokens.namePlaceholder')"
                autocomplete="off"
              />
            </div>

            <div class="flex flex-col gap-1.5">
              <Label for="token-expiry">{{ t('tokens.expiryLabel') }}</Label>
              <Select v-model="newExpiry">
                <SelectTrigger id="token-expiry" name="expiry" class="w-full" :aria-label="t('tokens.expiryLabel')">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="0">{{ t('tokens.expiryNever') }}</SelectItem>
                  <SelectItem value="30">{{ t('tokens.expiry30') }}</SelectItem>
                  <SelectItem value="90">{{ t('tokens.expiry90') }}</SelectItem>
                </SelectContent>
              </Select>
            </div>

            <!-- App + scope picker -->
            <div class="flex flex-col gap-3">
              <label class="flex cursor-pointer items-center gap-2 text-sm text-ink">
                <Switch v-model="allApps" data-test="all-apps" />
                <span>{{ t('tokens.allAppsLabel') }}</span>
              </label>
              <template v-if="!allApps">
                <p class="text-xs text-muted">{{ t('tokens.appsHelp') }}</p>
                <div v-for="a in apps" :key="a.clientId" class="rounded-md border border-border p-3">
                  <label class="flex cursor-pointer items-center gap-2 text-sm font-medium text-ink">
                    <Checkbox
                      :model-value="a.clientId in grants"
                      data-test="app"
                      @update:model-value="(v) => toggleApp(a.clientId, Boolean(v))"
                    />
                    <span>{{ a.displayName }}</span>
                  </label>
                  <div v-if="a.clientId in grants && a.scopes.length" class="mt-2 flex flex-col gap-1 pl-6">
                    <label
                      v-for="sc in a.scopes"
                      :key="sc.name"
                      class="flex cursor-pointer items-center gap-2 text-sm text-ink"
                    >
                      <Checkbox
                        :model-value="(grants[a.clientId] ?? []).includes(sc.name)"
                        @update:model-value="(v) => toggleScope(a.clientId, sc.name, Boolean(v))"
                      />
                      <span class="font-mono">{{ sc.name }}</span>
                      <span v-if="sc.description" class="text-muted">— {{ sc.description }}</span>
                    </label>
                  </div>
                </div>
                <EmptyState v-if="!apps.length" :icon="Terminal" :title="t('tokens.noApps')" />
              </template>
            </div>

            <ErrorPanel :error="error" @dismiss="clear" />

            <div class="flex gap-2">
              <Button
                type="button"
                :disabled="busy || !canSubmit"
                data-test="token-create-submit"
                @click="create"
              >
                {{ t('tokens.create') }}
              </Button>
              <Button type="button" variant="outline" :disabled="busy" @click="closeCreate">
                {{ t('common.cancel') }}
              </Button>
            </div>
          </div>
        </template>

        <!-- Reveal state: shown once after successful creation -->
        <template v-else>
          <div class="flex flex-col gap-4">
            <p class="text-sm text-muted">{{ t('tokens.revealIntro') }}</p>

            <div class="flex items-center gap-2">
              <code class="min-w-0 flex-1 overflow-x-auto rounded-md border border-border bg-sunken px-3 py-2 font-mono text-sm text-ink">{{ revealToken }}</code>
              <Button type="button" variant="outline" size="sm" class="shrink-0" @click="copyToken">
                <component :is="copied ? Check : Copy" class="size-4" aria-hidden="true" />
                <span>{{ copied ? t('common.copied') : t('tokens.copy') }}</span>
              </Button>
            </div>
            <p v-if="copyFailed" class="text-xs text-destructive" role="alert">{{ t('tokens.copyFailed') }}</p>

            <label class="flex cursor-pointer items-center gap-2 text-sm text-ink">
              <Checkbox v-model="savedConfirmed" data-test="token-saved" />
              <span>{{ t('tokens.savedConfirm') }}</span>
            </label>

            <Button
              type="button"
              class="w-full"
              :disabled="!savedConfirmed"
              data-test="token-done"
              @click="closeCreate"
            >
              {{ t('tokens.done') }}
            </Button>
          </div>
        </template>
      </DialogContent>
    </Dialog>
  </div>
</template>
