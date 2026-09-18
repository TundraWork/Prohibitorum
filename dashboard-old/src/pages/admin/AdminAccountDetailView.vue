<script setup lang="ts">
import { removeDetail } from '@/queries/invalidation'
import { usePrivateState } from '@/composables/usePrivateState'
/**
 * AdminAccountDetailView (/admin/accounts/:id) — per-account admin actions.
 * Edit identity/role/disabled (PUT round-trips attributes — the backend REPLACES
 * them, so omitting would clear them); force-revoke passkeys (sudo); revoke all
 * sessions; reissue an enrollment link; delete. Attributes are editable via
 * key/value rows (string values); non-string values are preserved read-only.
 * All mutations go through withSudo (no-op unless the server demands sudo —
 * only credential force-revoke does today).
 */
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import StatusMessage from '@/components/custom/StatusMessage.vue'
import { useRoute, useRouter } from 'vue-router'
import { useResource } from '@/composables/useResource'
import { useDraftSync } from '@/composables/useDraftSync'
import { useQueryClient } from '@tanstack/vue-query'
import { detailQuery, accountSectionQuery } from '@/queries/resources'
import { api } from '@/lib/api'
import { type Page } from '@/lib/pagination'
import { useApi } from '@/composables/useApi'
import { useTransientFlag } from '@/composables/useTransientFlag'
import { withSudo } from '@/lib/sudo'
import { relativeTime, formatDateTime } from '@/lib/time'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/table'
import { Separator } from '@/components/ui/separator'
import SegmentedControl from '@/components/custom/SegmentedControl.vue'
import ConfirmDialog from '@/components/custom/ConfirmDialog.vue'
import SectionTitle from '@/components/custom/SectionTitle.vue'
import CodeField from '@/components/custom/CodeField.vue'
import StatusBadge from '@/components/custom/StatusBadge.vue'
import CopyButton from '@/components/custom/CopyButton.vue'
import UserAvatar from '@/components/custom/UserAvatar.vue'
import CardSkeleton from '@/components/custom/CardSkeleton.vue'
import BackLink from '@/components/custom/BackLink.vue'
import UserAgentDisplay from '@/components/custom/UserAgentDisplay.vue'
import EmptyState from '@/components/custom/EmptyState.vue'
import ErrorPanel from '@/components/custom/ErrorPanel.vue'
import IdentityMetadata, { type AccountIdentity } from '@/components/custom/IdentityMetadata.vue'

interface Account {
  id: number; oidcSubject: string; username: string; displayName: string; role: string
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
interface PersonalAccessTokenView {
  id: number; name: string; tokenHint: string
  allApps: boolean; appGrants: Record<string, string[]>
  createdAt: string; expiresAt?: string; lastUsedAt?: string
}

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const { busy: mutationBusy, error: mutationError, run, clear: clearMutation } = useApi('accounts')
// Linked identities load independently so their failure cannot be erased by
// successful account mutations or hide errors from other account sections.

const id = Number(route.params.id)
const queryClient = useQueryClient()
const options = detailQuery<Account>('accounts', id)
const query = useResource(options)
const account = computed({ get: () => query.data.value ?? null, set: (value: Account | null) => { if (value) queryClient.setQueryData(options.queryKey, value) } })
const credentialsQuery = useResource(accountSectionQuery<Page<Credential>>(id, 'credentials'))
const sessionsQuery = useResource(accountSectionQuery<Page<SessionListItem>>(id, 'sessions'))
const tokensQuery = useResource(accountSectionQuery<Page<PersonalAccessTokenView>>(id, 'tokens'))
const identitiesApi = useResource(accountSectionQuery<AccountIdentity[]>(id, 'identities'))
const credentials = computed(() => credentialsQuery.data.value?.items ?? [])
const sessions = computed(() => sessionsQuery.data.value?.items ?? [])
const identities = computed(() => identitiesApi.data.value ?? [])
const identitiesLoaded = identitiesApi.isSuccess
const busy = computed(() => mutationBusy.value || query.busy.value)
const error = computed(() => mutationError.value ?? query.error.value ?? credentialsQuery.error.value ?? sessionsQuery.error.value ?? tokensQuery.error.value)
const notFound = computed(() => query.error.value?.code === 'account_not_found')
function clear(): void { clearMutation(); query.clear(); credentialsQuery.clear(); sessionsQuery.clear(); tokensQuery.clear() }

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

const tokens = computed(() => tokensQuery.data.value?.items ?? [])
const confirmRevokeTokenId = ref<number | null>(null)

const revokeCredId = ref<number | null>(null)
const confirmRevokeSessionId = ref<string | null>(null)
const confirmRevokeAll = ref(false)
const confirmDelete = ref(false)
const revokedCount = ref<number | null>(null)
const reissueUrl = ref('')
const reissueExpires = ref('')

const hasComplexAttrs = computed(() => Object.keys(attrComplex.value).length > 0)

function seedForm(acc: Account): void {
  displayName.value = acc.displayName
  email.value = acc.email ?? ''
  role.value = acc.role === 'admin' ? 'admin' : 'user'
  disabled.value = acc.disabled
  seedAttrs(acc.attributes)
}
const draft = useDraftSync(account, () => [displayName.value, email.value, role.value, disabled.value, buildAttrs()], seedForm)

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
  if (updated) { account.value = updated; draft.accept(updated); triggerSaved() }
}
async function forceRevoke(): Promise<void> {
  const credentialId = revokeCredId.value
  if (credentialId == null) return
  await run(() => withSudo(async () => {
    await api.post('/api/prohibitorum/accounts/credentials/delete', { accountId: id, credentialId })
    return true as const
  }, t('sudo.reason.forceRevokePasskey')))
  revokeCredId.value = null
}
async function revokeSession(sessionId: string): Promise<void> {
  await run(() => withSudo(async () => {
    await api.post(`/api/prohibitorum/accounts/${id}/sessions/revoke`, { sessionId })
    return true as const
  }, t('sudo.reason.revokeSession')))
}
async function revokeToken(tokenId: number): Promise<void> {
  await run(() => withSudo(async () => {
    await api.post('/api/prohibitorum/accounts/tokens/revoke', { id: tokenId })
    return true as const
  }, t('sudo.reason.revokeToken')))
  confirmRevokeTokenId.value = null
}
async function revokeAllSessions(): Promise<void> {
  const res = await run(() => withSudo(() =>
    api.post<{ revoked: number }>('/api/prohibitorum/accounts/revoke-sessions', { id }),
    t('sudo.reason.revokeSession')))
  confirmRevokeAll.value = false
  if (res) { revokedCount.value = res.revoked }
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
    await removeDetail(queryClient, 'accounts', id)
    return true as const
  }, t('sudo.reason.deleteAccount')))
  confirmDelete.value = false
  if (ok) router.push('/admin/accounts')
}
usePrivateState(() => { reissueUrl.value = ''; reissueExpires.value = '' })
</script>
<template>
  <div class="flex max-w-2xl flex-col gap-6">
    <BackLink to="/admin/accounts" :label="t('admin.account.back')" />
    <ErrorPanel v-if="error && !notFound" :error="error" @dismiss="clear" :is-admin="true" />
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
            <Label>{{ t('admin.account.oidcSubject') }}</Label>
            <div class="flex min-w-0 items-center gap-2">
              <p class="min-w-0 select-all font-mono text-sm break-words text-muted" data-test="oidc-subject">
                {{ account.oidcSubject }}
              </p>
              <CopyButton :value="account.oidcSubject" :label="t('admin.account.oidcSubjectCopy')" />
            </div>
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
              :options="[
                {value:'user',label:t('admin.account.roleUser')},
                {value:'admin',label:t('admin.account.roleAdmin')},
              ]" />
            <p class="text-xs text-muted">{{ t('admin.account.roleDesc') }}</p>
          </div>
          <div class="flex flex-col gap-2">
            <Label>{{ t('admin.account.attributes') }}</Label>
            <p v-if="attrRows.length === 0 && !hasComplexAttrs" class="text-sm text-muted">{{ t('admin.account.attributesEmpty') }}</p>
            <div v-for="(row, i) in attrRows" :key="row.uid" class="flex items-center gap-2" :data-test="`attr-row-${i}`">
              <Input :placeholder="t('admin.account.attributesKey')" :aria-label="t('admin.account.attributesKey')" v-model="row.key" class="flex-1" :data-test="`attr-key-${i}`" />
              <Input :placeholder="t('admin.account.attributesValue')" :aria-label="t('admin.account.attributesValue')" v-model="row.value" class="flex-1" :data-test="`attr-value-${i}`" />
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
            <StatusMessage :show="saved">{{ t('admin.account.saved') }}</StatusMessage>
          </div>
        </CardContent>
      </Card>

      <Card data-test="linked-identities">
        <CardHeader><CardTitle>{{ t('identity.linkedIdentities') }}</CardTitle></CardHeader>
        <CardContent class="flex flex-col">
          <ErrorPanel :error="identitiesApi.error.value" @dismiss="identitiesApi.clear" :is-admin="true" />
          <p v-if="identitiesApi.busy.value && !identitiesLoaded" role="status" class="text-sm text-muted">{{ t('common.loading') }}</p>
          <p v-else-if="identitiesLoaded && identities.length === 0" class="text-sm text-muted">{{ t('identity.linkedIdentitiesEmpty') }}</p>
          <div
            v-for="identity in identities"
            :key="identity.id"
            class="flex min-w-0 flex-col gap-2 border-t border-border py-4 first:border-t-0 first:pt-0 last:pb-0"
          >
            <div class="flex min-w-0 flex-wrap items-baseline justify-between gap-2">
              <p class="min-w-0 truncate font-medium text-ink" :title="identity.providerDisplayName">
                {{ identity.providerDisplayName }}
              </p>
              <p class="shrink-0 text-xs text-muted">{{ t('connected.connectedOn', { date: relativeTime(identity.linkedAt) }) }}</p>
            </div>
            <IdentityMetadata :identity="identity" />
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
                <TableCell class="max-w-xs text-sm text-muted"><UserAgentDisplay :ua="s.userAgent" /></TableCell>
                <TableCell>
                  <Button type="button" variant="outline" size="sm" :disabled="busy" :data-test="`session-revoke-${s.id}`" @click="confirmRevokeSessionId = s.id">{{ t('admin.account.sessions.revoke') }}</Button>
                </TableCell>
              </TableRow>
            </TableBody>
          </Table>
          <StatusMessage :show="revokedCount !== null">{{ t('admin.account.sessionsRevoked', { count: revokedCount }) }}</StatusMessage>
          <Button type="button" variant="outline" class="w-fit" :disabled="busy" data-test="revoke-all" @click="confirmRevokeAll = true">{{ t('admin.account.revokeAllSessions') }}</Button>
        </CardContent>
      </Card>

      <Card>
        <CardHeader><CardTitle>{{ t('admin.account.tokens.title') }}</CardTitle></CardHeader>
        <CardContent class="flex flex-col gap-3">
          <EmptyState v-if="tokens.length === 0" :title="t('admin.account.tokens.empty')" />
          <div v-for="tok in tokens" :key="tok.id" class="flex items-center justify-between gap-4" :data-test="`token-row-${tok.id}`">
            <div class="flex min-w-0 flex-col text-sm">
              <span class="truncate font-medium text-ink">{{ tok.name }}</span>
              <span class="font-mono text-xs text-muted">{{ tok.tokenHint }}</span>
              <span class="text-xs text-muted">
                <template v-if="tok.allApps">{{ t('admin.account.tokens.allApps') }}</template>
                <template v-else>{{ t('admin.account.tokens.appCount', { n: Object.keys(tok.appGrants).length }) }}</template>
                <template v-if="tok.expiresAt"> · {{ t('tokens.expires') }} {{ formatDateTime(tok.expiresAt) }}</template>
              </span>
            </div>
            <Button type="button" variant="outline" size="sm" class="shrink-0" :disabled="busy" :data-test="`token-revoke-${tok.id}`" @click="confirmRevokeTokenId = tok.id">{{ t('admin.account.tokens.revoke') }}</Button>
          </div>
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
              <SectionTitle as="h3">{{ t('admin.account.statusLabel') }}</SectionTitle>
              <StatusBadge :variant="disabled ? 'danger' : 'success'" data-test="status-badge">
                {{ disabled ? t('admin.account.statusDisabled') : t('admin.account.statusActive') }}
              </StatusBadge>
            </div>
            <p class="text-xs text-muted">{{ t('admin.account.disabledDesc') }}</p>
            <p v-if="isPersistedAdmin && !disabled" class="text-xs text-amber-700" data-test="disable-admin-hint">{{ t('admin.account.disableAdminHint') }}</p>
            <Button type="button" variant="outline" class="w-fit" :disabled="busy || (isPersistedAdmin && !disabled)" data-test="disable-toggle" @click="toggleDisabled">
              {{ disabled ? t('admin.account.enable') : t('admin.account.disable') }}
            </Button>
          </div>

          <Separator />
          <div class="flex flex-col gap-2">
            <SectionTitle as="h3">{{ t('admin.account.deleteTitle') }}</SectionTitle>
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
    <ConfirmDialog :open="confirmRevokeTokenId !== null" :title="t('admin.account.tokens.revokeConfirmTitle')" :confirm-label="t('admin.account.tokens.revoke')" :busy="busy"
      @update:open="(v) => { if (!v) confirmRevokeTokenId = null }" @cancel="confirmRevokeTokenId = null" @confirm="async () => { if (confirmRevokeTokenId !== null) await revokeToken(confirmRevokeTokenId) }">
      {{ t('admin.account.tokens.revokeConfirmBody') }}
    </ConfirmDialog>
  </div>
</template>
