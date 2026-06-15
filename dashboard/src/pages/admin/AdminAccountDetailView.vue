<script setup lang="ts">
/**
 * AdminAccountDetailView (/admin/accounts/:id) — per-account admin actions.
 * Edit identity/role/disabled (PUT round-trips attributes — the backend REPLACES
 * them, so omitting would clear them); force-revoke passkeys (sudo); revoke all
 * sessions; reissue an enrollment link; delete. Attributes are editable via
 * key/value rows (string values); non-string values are preserved read-only.
 * All mutations go through withSudo (no-op unless the server demands sudo —
 * only credential force-revoke does today).
 */
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { useTransientFlag } from '@/composables/useTransientFlag'
import { withSudo } from '@/lib/sudo'
import { relativeTime, formatDateTime } from '@/lib/time'
import { formatUserAgent } from '@/lib/userAgent'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/table'
import { Separator } from '@/components/ui/separator'
import SegmentedControl from '@/components/custom/SegmentedControl.vue'
import ConfirmDialog from '@/components/custom/ConfirmDialog.vue'
import CodeField from '@/components/custom/CodeField.vue'
import StatusBadge from '@/components/custom/StatusBadge.vue'
import UserAvatar from '@/components/custom/UserAvatar.vue'
import CardSkeleton from '@/components/custom/CardSkeleton.vue'
import BackLink from '@/components/custom/BackLink.vue'

interface Account {
  id: number; username: string; displayName: string; role: string
  email?: string; emailVerified: boolean
  attributes?: Record<string, unknown>; disabled: boolean
  createdAt: string; updatedAt: string; lastSignInAt?: string; avatarUrl?: string
}
interface Credential {
  id: number; credentialIdSuffix: string; nickname?: string; transports: string[]
  backupState: boolean; attestationType: string; createdAt: string; lastUsedAt?: string
}
interface SessionListItem {
  id: string; isCurrent: boolean; issuedAt: string; expiresAt: string
  lastSeenIp: string; userAgent?: string
}

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const { busy, error, run, errorText } = useApi()

const id = Number(route.params.id)
const account = ref<Account | null>(null)
const credentials = ref<Credential[]>([])
const sessions = ref<SessionListItem[]>([])
const notFound = ref(false)

const displayName = ref('')
const email = ref('')
const role = ref<'admin' | 'user'>('user')
const disabled = ref(false)
const { flag: saved, trigger: triggerSaved } = useTransientFlag()

// Attributes editor state
interface AttrRow { uid: number; key: string; value: string }
let attrUid = 0
const attrRows = ref<AttrRow[]>([])
const attrComplex = ref<Record<string, unknown>>({})

function seedAttrs(attrs: Record<string, unknown> | undefined): void {
  const rows: AttrRow[] = []
  const complex: Record<string, unknown> = {}
  for (const [k, v] of Object.entries(attrs ?? {})) {
    if (typeof v === 'string') rows.push({ uid: attrUid++, key: k, value: v })
    else complex[k] = v
  }
  attrRows.value = rows
  attrComplex.value = complex
}

function buildAttrs(): Record<string, unknown> {
  const result: Record<string, unknown> = { ...attrComplex.value }
  for (const row of attrRows.value) {
    if (row.key !== '' && !(row.key in attrComplex.value)) {
      result[row.key] = row.value
    }
  }
  return result
}

function addAttrRow(): void { attrRows.value.push({ uid: attrUid++, key: '', value: '' }) }
function removeAttrRow(i: number): void { attrRows.value.splice(i, 1) }

const revokeCredId = ref<number | null>(null)
const confirmRevokeSessionId = ref<string | null>(null)
const confirmRevokeAll = ref(false)
const confirmDelete = ref(false)
const revokedCount = ref<number | null>(null)
const reissueUrl = ref('')
const reissueExpires = ref('')

const hasComplexAttrs = computed(() => Object.keys(attrComplex.value).length > 0)

async function loadCredentials(): Promise<void> {
  const creds = await run(() => api.get<Credential[]>(`/api/prohibitorum/accounts/${id}/credentials`))
  if (creds) credentials.value = creds
}
async function loadSessions(): Promise<void> {
  const res = await run(() => api.get<SessionListItem[]>(`/api/prohibitorum/accounts/${id}/sessions`))
  if (res) sessions.value = res
}
async function load(): Promise<void> {
  const acc = await run(() => api.get<Account>(`/api/prohibitorum/accounts/${id}`))
  if (!acc) { if (error.value?.code === 'account_not_found') notFound.value = true; return }
  account.value = acc
  displayName.value = acc.displayName
  email.value = acc.email ?? ''
  role.value = acc.role === 'admin' ? 'admin' : 'user'
  disabled.value = acc.disabled
  seedAttrs(acc.attributes)
  await loadCredentials()
  await loadSessions()
}
async function save(): Promise<void> {
  // Send email ONLY when it changed: any explicit email value resets
  // email_verified=false server-side, so omitting it on an unrelated save
  // (e.g. toggling disabled) preserves a federation-verified address.
  const trimmedEmail = email.value.trim()
  const emailChanged = trimmedEmail !== (account.value?.email ?? '')
  const updated = await run(() => withSudo(() => api.put<Account>(`/api/prohibitorum/accounts/${id}`, {
    username: '',
    displayName: displayName.value,
    role: role.value,
    disabled: disabled.value,
    attributes: buildAttrs(),
    ...(emailChanged ? { email: trimmedEmail } : {}),
  }), t('sudo.reason.saveChanges')))
  if (updated) { account.value = updated; displayName.value = updated.displayName; email.value = updated.email ?? ''; seedAttrs(updated.attributes); triggerSaved() }
}
async function forceRevoke(): Promise<void> {
  const credentialId = revokeCredId.value
  if (credentialId == null) return
  const ok = await run(() => withSudo(async () => {
    await api.post('/api/prohibitorum/accounts/credentials/delete', { accountId: id, credentialId })
    return true as const
  }, t('sudo.reason.forceRevokePasskey')))
  revokeCredId.value = null
  if (ok) await loadCredentials()
}
async function revokeSession(sessionId: string): Promise<void> {
  const ok = await run(() => withSudo(async () => {
    await api.post(`/api/prohibitorum/accounts/${id}/sessions/revoke`, { sessionId })
    return true as const
  }, t('sudo.reason.revokeSession')))
  if (ok) await loadSessions()
}
async function revokeAllSessions(): Promise<void> {
  const res = await run(() => withSudo(() =>
    api.post<{ revoked: number }>('/api/prohibitorum/accounts/revoke-sessions', { id }),
    t('sudo.reason.revokeSession')))
  confirmRevokeAll.value = false
  if (res) { revokedCount.value = res.revoked; await loadSessions() }
}
async function reissue(): Promise<void> {
  const res = await run(() => withSudo(() =>
    api.post<{ url: string; expiresAt: string }>('/api/prohibitorum/accounts/reissue-enrollment', { id }),
    t('sudo.reason.reissueEnrollment')))
  if (res) { reissueUrl.value = res.url; reissueExpires.value = res.expiresAt }
}
// Flip the disabled flag on its own (independent of the identity-form Save), via
// the dedicated set-disabled endpoint. The backend rejects disabling an admin —
// the button is gated on the PERSISTED role below so the unsaved form ref can't
// mislead the operator.
async function toggleDisabled(): Promise<void> {
  const next = !disabled.value
  const updated = await run(() => withSudo(() =>
    api.post<Account>('/api/prohibitorum/accounts/set-disabled', { id, disabled: next }),
    t('sudo.reason.disableAccount')))
  if (updated) { account.value = updated; disabled.value = updated.disabled }
}
// Use the SAVED role (account.role), not the unsaved form `role` ref: an admin
// account cannot be disabled, and switching the form to "user" without saving
// must not unlock the button.
const isPersistedAdmin = computed(() => account.value?.role === 'admin')
async function destroy(): Promise<void> {
  const ok = await run(() => withSudo(async () => {
    await api.post('/api/prohibitorum/accounts/delete', { id })
    return true as const
  }, t('sudo.reason.deleteAccount')))
  confirmDelete.value = false
  if (ok) router.push('/admin/accounts')
}
onMounted(load)
</script>
<template>
  <div class="flex max-w-2xl flex-col gap-6">
    <BackLink to="/admin/accounts" :label="t('admin.account.back')" />
    <Alert v-if="errorText && !notFound" variant="destructive" role="alert" aria-live="polite"><AlertDescription>{{ errorText }}</AlertDescription></Alert>
    <p v-if="notFound" class="text-sm text-muted" role="status">{{ t('admin.account.notFound') }}</p>

    <CardSkeleton v-else-if="busy && !account" />

    <template v-else-if="account">
      <div class="flex items-center gap-3">
        <UserAvatar :display-name="account.displayName" :username="account.username" :src="account.avatarUrl" />
        <h1 class="text-2xl font-semibold tracking-tight text-ink">{{ account.displayName }}</h1>
      </div>

      <Card>
        <CardHeader><CardTitle>{{ t('admin.account.identityTitle') }}</CardTitle></CardHeader>
        <CardContent class="flex flex-col gap-4">
          <div class="flex flex-col gap-1.5">
            <Label>{{ t('admin.account.username') }}</Label>
            <p class="font-mono text-sm text-muted">{{ account.username }}</p>
            <p class="text-xs text-muted">{{ t('admin.account.usernameDesc') }}</p>
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="displayName">{{ t('admin.account.displayName') }}</Label>
            <Input id="displayName" name="displayName" v-model="displayName" />
          </div>
          <div class="flex flex-col gap-1.5">
            <div class="flex items-center gap-2">
              <Label for="email">{{ t('admin.account.email') }}</Label>
              <StatusBadge v-if="account.email" :variant="account.emailVerified ? 'success' : 'neutral'">
                {{ account.emailVerified ? t('admin.account.emailVerified') : t('admin.account.emailUnverified') }}
              </StatusBadge>
            </div>
            <Input id="email" name="email" type="email" v-model="email" :placeholder="t('admin.account.emailPlaceholder')" />
            <p class="text-xs text-muted">{{ t('admin.account.emailDesc') }}</p>
          </div>
          <div class="flex flex-col gap-1.5">
            <Label>{{ t('admin.account.role') }}</Label>
            <SegmentedControl v-model="role" :aria-label="t('admin.account.role')"
              :options="[{value:'user',label:t('admin.account.roleUser')},{value:'admin',label:t('admin.account.roleAdmin')}]" />
            <p class="text-xs text-muted">{{ t('admin.account.roleDesc') }}</p>
          </div>
          <div class="flex flex-col gap-2">
            <Label>{{ t('admin.account.attributes') }}</Label>
            <p v-if="attrRows.length === 0 && !hasComplexAttrs" class="text-sm text-muted">{{ t('admin.account.attributesEmpty') }}</p>
            <div v-for="(row, i) in attrRows" :key="row.uid" class="flex items-center gap-2" :data-test="`attr-row-${i}`">
              <Input :placeholder="t('admin.account.attributesKey')" v-model="row.key" class="flex-1" :data-test="`attr-key-${i}`" />
              <Input :placeholder="t('admin.account.attributesValue')" v-model="row.value" class="flex-1" :data-test="`attr-value-${i}`" />
              <Button type="button" variant="outline" size="sm" class="shrink-0" :data-test="`attr-remove-${i}`" @click="removeAttrRow(i)">{{ t('admin.account.attributesRemove') }}</Button>
            </div>
            <div v-if="hasComplexAttrs" class="flex flex-col gap-1">
              <p class="text-xs text-muted">{{ t('admin.account.attributesComplexNote') }}</p>
              <div v-for="(v, k) in attrComplex" :key="k" class="flex min-w-0 gap-2 text-sm text-muted">
                <span class="font-mono">{{ k }}</span><span>=</span><span class="min-w-0 truncate font-mono">{{ JSON.stringify(v) }}</span>
              </div>
            </div>
            <Button type="button" variant="outline" size="sm" class="w-fit" data-test="attr-add" @click="addAttrRow">{{ t('admin.account.attributesAdd') }}</Button>
          </div>
          <div class="flex items-center gap-3">
            <Button type="button" :disabled="busy" data-test="save" @click="save">{{ t('admin.account.save') }}</Button>
            <span v-if="saved" class="text-sm text-sage" role="status">{{ t('admin.account.saved') }}</span>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader><CardTitle>{{ t('admin.account.passkeysTitle') }}</CardTitle></CardHeader>
        <CardContent class="flex flex-col gap-3">
          <p v-if="!credentials.length" class="text-sm text-muted">{{ t('admin.account.passkeysEmpty') }}</p>
          <div v-for="c in credentials" :key="c.id" class="flex items-center justify-between gap-4">
            <div class="flex min-w-0 flex-col text-sm">
              <span class="truncate text-ink">{{ c.nickname || ('····' + c.credentialIdSuffix) }}</span>
              <span class="truncate text-muted">{{ t('admin.account.created') }} {{ relativeTime(c.createdAt) }} · {{ c.lastUsedAt ? t('admin.account.lastUsed') + ' ' + relativeTime(c.lastUsedAt) : t('admin.account.neverUsed') }}</span>
            </div>
            <Button type="button" variant="outline" size="sm" class="shrink-0" :disabled="busy" :data-test="`revoke-cred-${c.id}`" @click="revokeCredId = c.id">{{ t('admin.account.forceRevoke') }}</Button>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader><CardTitle>{{ t('admin.account.sessionsTitle') }}</CardTitle></CardHeader>
        <CardContent class="flex flex-col gap-3">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{{ t('admin.account.sessions.colTime') }}</TableHead>
                <TableHead>{{ t('admin.account.sessions.colIp') }}</TableHead>
                <TableHead>{{ t('admin.account.sessions.colUa') }}</TableHead>
                <TableHead></TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <TableRow v-if="sessions.length === 0">
                <TableCell colspan="4" class="text-sm text-muted">{{ t('admin.account.sessions.empty') }}</TableCell>
              </TableRow>
              <TableRow v-for="s in sessions" :key="s.id" :data-test="`session-row-${s.id}`">
                <TableCell class="text-sm text-ink">{{ formatDateTime(s.issuedAt) }}</TableCell>
                <TableCell class="text-sm text-ink">{{ s.lastSeenIp }}</TableCell>
                <TableCell class="max-w-xs truncate text-sm text-muted" :title="s.userAgent || undefined">{{ formatUserAgent(s.userAgent) }}</TableCell>
                <TableCell>
                  <Button type="button" variant="outline" size="sm" :disabled="busy" :data-test="`session-revoke-${s.id}`" @click="confirmRevokeSessionId = s.id">{{ t('admin.account.sessions.revoke') }}</Button>
                </TableCell>
              </TableRow>
            </TableBody>
          </Table>
          <p v-if="revokedCount !== null" class="text-sm text-sage" role="status">{{ t('admin.account.sessionsRevoked', { count: revokedCount }) }}</p>
          <Button type="button" variant="outline" class="w-fit" :disabled="busy" data-test="revoke-all" @click="confirmRevokeAll = true">{{ t('admin.account.revokeAllSessions') }}</Button>
        </CardContent>
      </Card>

      <Card>
        <CardHeader><CardTitle>{{ t('admin.account.resetTitle') }}</CardTitle></CardHeader>
        <CardContent class="flex flex-col gap-3">
          <p class="text-sm text-muted">{{ t('admin.account.resetHelp') }}</p>
          <CodeField v-if="reissueUrl" :value="reissueUrl" />
          <p v-if="reissueUrl" class="text-xs text-muted">{{ t('admin.account.reissueExpires', { when: formatDateTime(reissueExpires) }) }}</p>
          <Button type="button" variant="outline" class="w-fit" :disabled="busy" data-test="reissue" @click="reissue">{{ t('admin.account.reissue') }}</Button>
        </CardContent>
      </Card>

      <Card class="border-destructive/30 bg-destructive/[0.02]">
        <CardHeader><CardTitle class="text-destructive">{{ t('admin.account.dangerTitle') }}</CardTitle></CardHeader>
        <CardContent class="flex flex-col gap-4">
          <div class="flex flex-col gap-2">
            <div class="flex items-center gap-2">
              <h4 class="text-sm font-medium text-ink">{{ t('admin.account.statusLabel') }}</h4>
              <StatusBadge :variant="disabled ? 'danger' : 'success'" data-test="status-badge">
                {{ disabled ? t('admin.account.statusDisabled') : t('admin.account.statusActive') }}
              </StatusBadge>
            </div>
            <p class="text-xs text-muted">{{ t('admin.account.disabledDesc') }}</p>
            <p v-if="isPersistedAdmin && !disabled" class="text-xs text-amber" data-test="disable-admin-hint">{{ t('admin.account.disableAdminHint') }}</p>
            <Button type="button" variant="outline" class="w-fit" :disabled="busy || (isPersistedAdmin && !disabled)" data-test="disable-toggle" @click="toggleDisabled">
              {{ disabled ? t('admin.account.enable') : t('admin.account.disable') }}
            </Button>
          </div>

          <Separator />
          <div class="flex flex-col gap-2">
            <h4 class="text-sm font-medium text-ink">{{ t('admin.account.deleteTitle') }}</h4>
            <p class="text-xs text-muted">{{ t('admin.account.deleteHelp') }}</p>
            <Button type="button" variant="destructive" class="w-fit" :disabled="busy" data-test="delete" @click="confirmDelete = true">{{ t('admin.account.delete') }}</Button>
          </div>
        </CardContent>
      </Card>
    </template>

    <ConfirmDialog :open="revokeCredId !== null" :title="t('admin.account.forceRevokeConfirmTitle')" :confirm-label="t('admin.account.forceRevoke')" :busy="busy"
      @update:open="(v) => { if (!v) revokeCredId = null }" @cancel="revokeCredId = null" @confirm="forceRevoke">
      {{ t('admin.account.forceRevokeConfirmBody') }}
    </ConfirmDialog>
    <ConfirmDialog :open="confirmRevokeSessionId !== null" :title="t('admin.account.sessions.revokeConfirmTitle')" :confirm-label="t('admin.account.sessions.revoke')" :busy="busy"
      @update:open="(v) => { if (!v) confirmRevokeSessionId = null }" @cancel="confirmRevokeSessionId = null" @confirm="async () => { if (confirmRevokeSessionId) { await revokeSession(confirmRevokeSessionId); confirmRevokeSessionId = null } }">
      {{ t('admin.account.sessions.revokeConfirmBody') }}
    </ConfirmDialog>
    <ConfirmDialog :open="confirmRevokeAll" :title="t('admin.account.revokeAllConfirmTitle')" :confirm-label="t('admin.account.revokeAllSessions')" :busy="busy"
      @update:open="(v) => { if (!v) confirmRevokeAll = false }" @cancel="confirmRevokeAll = false" @confirm="revokeAllSessions">
      {{ t('admin.account.revokeAllConfirmBody') }}
    </ConfirmDialog>
    <ConfirmDialog :open="confirmDelete" :title="t('admin.account.deleteConfirmTitle')" :confirm-label="t('admin.account.delete')" :busy="busy"
      @update:open="(v) => { if (!v) confirmDelete = false }" @cancel="confirmDelete = false" @confirm="destroy">
      {{ t('admin.account.deleteConfirmBody') }}
    </ConfirmDialog>
  </div>
</template>
