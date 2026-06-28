<script setup lang="ts">
/**
 * TokensView (/tokens) — list/revoke the caller's personal access tokens and
 * create one via a dialog that reveals the plaintext exactly once.
 *
 * GET  /me/tokens                 → PersonalAccessTokenView[]
 * POST /me/tokens                 → { token: string, pat: PersonalAccessTokenView }  (sudo-gated)
 * POST /me/tokens/revoke {id}     → 204
 */
import { onMounted, ref } from 'vue'
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
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Select, SelectTrigger, SelectValue, SelectContent, SelectItem } from '@/components/ui/select'
import StatusBadge from '@/components/custom/StatusBadge.vue'
import ConfirmDialog from '@/components/custom/ConfirmDialog.vue'
import TableSkeleton from '@/components/custom/TableSkeleton.vue'
import EmptyState from '@/components/custom/EmptyState.vue'
import ScopeSelector from '@/components/custom/ScopeSelector.vue'

interface PersonalAccessTokenView {
  id: number
  name: string
  tokenHint: string
  upstreamScopes: string[]
  allowedClientIds: string[]
  createdAt: string
  expiresAt?: string
  lastUsedAt?: string
}

const { t } = useI18n()
const { busy, run, errorText } = useApi()

const rows = ref<PersonalAccessTokenView[]>([])
const confirmRevokeId = ref<number | null>(null)

// Create dialog state
const createOpen = ref(false)
const newName = ref('')
const newExpiry = ref('0')  // '0' = no expiry, '30', '90' = days
const newScopes = ref<string[]>([])

// Reveal state (after creation)
const revealToken = ref<string | null>(null)
const copied = ref(false)
const copyFailed = ref(false)
const savedConfirmed = ref(false)

function isExpired(pat: PersonalAccessTokenView): boolean {
  return !!pat.expiresAt && new Date(pat.expiresAt) < new Date()
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
  const body: { name: string; expiresInDays?: number; upstreamScopes?: string[] } = {
    name: newName.value.trim(),
  }
  const days = parseInt(newExpiry.value, 10)
  if (days > 0) body.expiresInDays = days
  if (newScopes.value.length > 0) body.upstreamScopes = [...newScopes.value]

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

function openCreate(): void {
  newName.value = ''
  newExpiry.value = '0'
  newScopes.value = []
  revealToken.value = null
  savedConfirmed.value = false
  copied.value = false
  copyFailed.value = false
  createOpen.value = true
}

function closeCreate(): void {
  createOpen.value = false
  // Clear the secret on close — never persists after the dialog is dismissed.
  revealToken.value = null
}

onMounted(load)
</script>

<template>
  <div class="flex max-w-2xl flex-col gap-6">
    <div class="flex items-center justify-between gap-4">
      <h1 class="text-2xl font-semibold tracking-tight text-ink">{{ t('tokens.title') }}</h1>
      <Button type="button" data-test="new-token" @click="openCreate">{{ t('tokens.create') }}</Button>
    </div>

    <p class="text-sm text-muted">{{ t('tokens.intro') }}</p>

    <Alert v-if="errorText" variant="destructive" role="alert" aria-live="polite">
      <AlertDescription>{{ errorText }}</AlertDescription>
    </Alert>

    <TableSkeleton v-if="busy && !rows.length" :rows="3" :cols="1" />
    <template v-else-if="rows.length">
      <Card v-for="r in rows" :key="r.id">
        <CardContent class="flex items-center justify-between gap-4 py-4">
          <div class="flex min-w-0 flex-1 flex-col gap-1 text-sm">
            <div class="flex min-w-0 items-center gap-2">
              <span class="min-w-0 truncate font-medium text-ink" data-test="token-name">{{ r.name }}</span>
              <StatusBadge v-if="isExpired(r)" variant="caution" class="shrink-0">{{ t('tokens.expired') }}</StatusBadge>
            </div>
            <span class="truncate text-muted">{{ t('tokens.hint') }}: <span class="font-mono">{{ r.tokenHint }}</span></span>
            <span class="truncate text-muted">{{ t('tokens.created') }}: {{ relativeTime(r.createdAt) }}</span>
            <span v-if="r.expiresAt" class="truncate text-muted">{{ t('tokens.expires') }}: {{ formatDateTime(r.expiresAt) }}</span>
            <span class="truncate text-muted">
              {{ t('tokens.lastUsed') }}:
              <template v-if="r.lastUsedAt">{{ relativeTime(r.lastUsedAt) }}</template>
              <template v-else>{{ t('tokens.neverUsed') }}</template>
            </span>
            <div v-if="r.upstreamScopes.length" class="mt-1 flex flex-wrap gap-1">
              <span
                v-for="s in r.upstreamScopes"
                :key="s"
                class="inline-flex items-center rounded border border-border bg-sunken px-1.5 py-0.5 font-mono text-xs text-muted"
              >{{ s }}</span>
            </div>
            <div v-if="r.allowedClientIds.length" class="mt-1 flex flex-wrap gap-1">
              <span
                v-for="c in r.allowedClientIds"
                :key="c"
                class="inline-flex items-center rounded border border-border bg-sunken px-1.5 py-0.5 font-mono text-xs text-muted"
              >{{ c }}</span>
            </div>
          </div>
          <Button variant="outline" size="sm" class="shrink-0" :disabled="busy" data-test="revoke" @click="confirmRevokeId = r.id">
            {{ t('tokens.revoke') }}
          </Button>
        </CardContent>
      </Card>
    </template>
    <EmptyState v-else-if="!errorText" :icon="Terminal" :title="t('tokens.empty')" />

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
          <DialogTitle>{{ t('tokens.createTitle') }}</DialogTitle>
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

            <div class="flex flex-col gap-1.5">
              <Label>{{ t('tokens.scopesLabel') }}</Label>
              <ScopeSelector :known="[]" :allow-custom="true" v-model="newScopes" />
              <p class="text-xs text-muted">{{ t('tokens.scopesHint') }}</p>
            </div>

            <Alert v-if="errorText" variant="destructive" role="alert" aria-live="polite">
              <AlertDescription>{{ errorText }}</AlertDescription>
            </Alert>

            <div class="flex gap-2">
              <Button
                type="button"
                :disabled="busy || !newName.trim()"
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
            <p v-if="copyFailed" class="text-xs text-destructive" role="alert">{{ t('common.error') }}</p>

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
