<script setup lang="ts">
/**
 * PasskeysCard — manage the account's WebAuthn passkeys. Adding a passkey is
 * sudo-gated (the register/begin ceremony requires a fresh step-up; complete
 * rides the server-side stash from that begin). List/rename/delete are
 * session-only. The backend sends excludeCredentials on begin (no duplicate
 * passkeys) and rejects deleting the last passkey.
 */
import { computed, nextTick, onMounted, ref, useTemplateRef } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PublicKeyCredentialCreationOptionsJSON } from '@simplewebauthn/browser'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { useWebauthn } from '@/composables/useWebauthn'
import { withSudo } from '@/lib/sudo'
import { relativeTime } from '@/lib/time'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import EmptyState from '@/components/custom/EmptyState.vue'
import { Input } from '@/components/ui/input'
import { Alert, AlertDescription } from '@/components/ui/alert'
import StatusBadge from '@/components/custom/StatusBadge.vue'
import ConfirmDialog from '@/components/custom/ConfirmDialog.vue'
import ErrorPanel from '@/components/custom/ErrorPanel.vue'
import { Trash2, Plus } from 'lucide-vue-next'

interface CredentialView {
  id: number
  credentialIdSuffix: string
  nickname?: string
  transports: string[]
  backupState: boolean
  attestationType: string
  createdAt: string
  lastUsedAt?: string
}

const { t, te } = useI18n()
const { busy: netBusy, error: netError, run, clear: clearNet } = useApi()
const { busy: waBusy, error: waError, register } = useWebauthn()

const busy = computed(() => netBusy.value || waBusy.value)
const error = computed(() => netError.value ?? waError.value)
function clear(): void { clearNet(); waError.value = null }
const rows = ref<CredentialView[]>([])
const loaded = ref(false)
const editingId = ref<number | null>(null)
const draftName = ref('')
const confirmId = ref<number | null>(null)
const nameInput = useTemplateRef<{ $el?: HTMLElement }>('nameInput')

const displayName = (c: CredentialView) => c.nickname || `${t('security.passkeys.defaultName')} ····${c.credentialIdSuffix}`

async function load(): Promise<void> {
  const res = await run(() => api.get<CredentialView[]>('/api/prohibitorum/me/credentials'))
  if (res) { rows.value = res; loaded.value = true }
}

async function add(): Promise<void> {
  const options = await run(() => withSudo(() =>
    api.post<PublicKeyCredentialCreationOptionsJSON>('/api/prohibitorum/me/credentials/register/begin'),
    t('sudo.reason.addPasskey')))
  if (!options) return
  const attestation = await register(options)
  if (!attestation) return
  const ok = await run(async () => {
    await api.post('/api/prohibitorum/me/credentials/register/complete', attestation)
    return true as const
  })
  if (ok) await load()
}

async function startRename(c: CredentialView): Promise<void> { editingId.value = c.id; draftName.value = c.nickname ?? ''; await nextTick(); nameInput.value?.$el?.focus() }
async function saveRename(id: number): Promise<void> {
  const ok = await run(async () => {
    await api.post('/api/prohibitorum/me/credentials/rename', { id, nickname: draftName.value || null })
    return true as const
  })
  if (ok) { editingId.value = null; await load() }
}

async function confirmDelete(): Promise<void> {
  const id = confirmId.value
  if (id == null) return
  const ok = await run(() => withSudo(async () => {
    await api.post('/api/prohibitorum/me/credentials/delete', { id })
    return true as const
  }, t('sudo.reason.removePasskey')))
  confirmId.value = null
  if (ok) await load()
}

onMounted(load)
</script>

<template>
  <Card>
    <CardHeader class="flex flex-row items-center justify-between gap-2">
      <div class="flex items-center gap-2">
        <CardTitle>{{ t('security.passkeys.title') }}</CardTitle>
        <StatusBadge v-if="!loaded" variant="neutral">—</StatusBadge>
        <StatusBadge v-else :variant="rows.length > 0 ? 'success' : 'neutral'">
          {{ rows.length > 0 ? t('security.passkeys.configured', { n: rows.length }) : t('security.passkeys.notConfigured') }}
        </StatusBadge>
      </div>
    </CardHeader>
    <CardContent class="flex flex-col gap-3">
      <p class="text-sm text-muted">{{ t('security.passkeys.help') }}</p>

      <ErrorPanel :error="error" @dismiss="clear" />

      <EmptyState v-if="loaded && rows.length === 0 && !error" :title="t('security.passkeys.empty')" />

      <div v-for="c in rows" :key="c.id" class="flex items-center justify-between gap-3 border-b border-border pb-3 last:border-0 last:pb-0">
        <div class="flex min-w-0 flex-col gap-1">
          <div class="flex min-w-0 items-center gap-2">
            <template v-if="editingId === c.id">
              <Input ref="nameInput" v-model="draftName" name="nickname" class="h-8 w-48" :placeholder="displayName(c)" :aria-label="t('security.passkeys.rename')" />
              <Button type="button" size="sm" :disabled="busy" @click="saveRename(c.id)">{{ t('security.passkeys.save') }}</Button>
            </template>
            <template v-else>
              <span class="min-w-0 truncate text-sm text-ink">{{ displayName(c) }}</span>
              <StatusBadge :variant="c.backupState ? 'success' : 'neutral'" class="shrink-0">
                {{ c.backupState ? t('security.passkeys.synced') : t('security.passkeys.deviceBound') }}
              </StatusBadge>
            </template>
          </div>
          <span class="text-xs text-muted">
            {{ t('security.passkeys.created') }}: {{ relativeTime(c.createdAt) }}
            <template v-if="c.lastUsedAt"> · {{ t('security.passkeys.lastUsed') }}: {{ relativeTime(c.lastUsedAt) }}</template>
          </span>
        </div>
        <div class="flex shrink-0 items-center gap-1">
          <Button v-if="editingId !== c.id" type="button" variant="ghost" size="sm" @click="startRename(c)">{{ t('security.passkeys.rename') }}</Button>
          <Button type="button" variant="ghost" size="icon-sm" :aria-label="t('security.passkeys.remove')" @click="confirmId = c.id">
            <Trash2 class="size-4" aria-hidden="true" />
          </Button>
        </div>
      </div>

      <Button type="button" size="sm" class="w-fit" :disabled="busy" @click="add">
        <Plus class="size-4" aria-hidden="true" /><span>{{ t('security.passkeys.add') }}</span>
      </Button>
    </CardContent>
  </Card>

  <ConfirmDialog
    :open="confirmId !== null"
    :title="t('security.passkeys.removeTitle')"
    :confirm-label="t('security.passkeys.remove')"
    :busy="busy"
    @update:open="(v) => { if (!v) confirmId = null }"
    @cancel="confirmId = null"
    @confirm="confirmDelete"
  >
    {{ t('security.passkeys.removeBody') }}
  </ConfirmDialog>
</template>
