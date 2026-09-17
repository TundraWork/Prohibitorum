<script setup lang="ts">
import { removeDetail, removeManagedApplication } from '@/queries/invalidation'
import { usePrivateState } from '@/composables/usePrivateState'
/**
 * AdminOidcClientDetailView (/admin/oidc-applications/:clientId) — per-client admin actions.
 * Edit config (PUT with allowedScopes); rotate secret (reveal-once CodeField);
 * delete. All mutations go through withSudo.
 */
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import StatusMessage from '@/components/custom/StatusMessage.vue'
import { useRoute, useRouter } from 'vue-router'
import { useResource } from '@/composables/useResource'
import { useDraftSync } from '@/composables/useDraftSync'
import { useQueryClient } from '@tanstack/vue-query'
import { detailQuery } from '@/queries/resources'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { useTransientFlag } from '@/composables/useTransientFlag'
import { withSudo } from '@/lib/sudo'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { Select, SelectTrigger, SelectContent, SelectItem, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import ScopeSelector from '@/components/custom/ScopeSelector.vue'
import { OIDC_SCOPES } from '@/lib/scopes'
import { Separator } from '@/components/ui/separator'
import ConfirmDialog from '@/components/custom/ConfirmDialog.vue'
import SectionTitle from '@/components/custom/SectionTitle.vue'
import CodeField from '@/components/custom/CodeField.vue'
import ListInput from '@/components/custom/ListInput.vue'
import SettingRow from '@/components/custom/SettingRow.vue'
import StatusBadge from '@/components/custom/StatusBadge.vue'
import CardSkeleton from '@/components/custom/CardSkeleton.vue'
import BackLink from '@/components/custom/BackLink.vue'
import AppManagerCard from '@/components/custom/AppManagerCard.vue'
import AppPolicyWorkspace from '@/components/custom/AppPolicyWorkspace.vue'
import EntityIconUpload from '@/components/custom/EntityIconUpload.vue'
import ErrorPanel from '@/components/custom/ErrorPanel.vue'

interface OidcApplication {
  clientId: string
  displayName: string
  redirectUris: string[]
  postLogoutRedirectUris: string[]
  allowedScopes: string[]
  clientAuthMethod: string
  requirePkce: boolean
  requireConsent: boolean
  disabled: boolean
  createdAt: string
  iconUrl?: string | null
  launchUrl?: string | null
  subjectSource: 'sub' | 'username' | 'verified_email'
  claimAliases: Record<string, string>
}

const { t } = useI18n()
const props = withDefaults(defineProps<{ mode?: 'admin' | 'manager'; currentAccountId?: number }>(), { mode: 'admin' })
const route = useRoute()
const router = useRouter()

const oidcScopesDescribed = computed(() => OIDC_SCOPES.map((s) => ({ value: s.value, description: t(s.descKey), required: s.required })))
const { busy: mutationBusy, error: mutationError, run, clear: clearMutation } = useApi('oidc-applications')

const clientId = String(route.params.clientId ?? route.params.id)
const delegated = computed(() => props.mode === 'manager')
const backPath = computed(() => delegated.value ? '/manage/applications' : '/admin/oidc-applications')
const queryClient = useQueryClient()
const options = detailQuery<OidcApplication>('oidc-applications', clientId)
const query = useResource(options)
const client = computed({ get: () => query.data.value ?? null, set: (value: OidcApplication | null) => { if (value) queryClient.setQueryData(options.queryKey, value) } })
const busy = computed(() => mutationBusy.value || query.busy.value)
const error = computed(() => mutationError.value ?? query.error.value)
const notFound = computed(() => query.error.value?.code === 'client_not_found')
function clear(): void { clearMutation(); query.clear() }

const displayName = ref('')
const launchUrl = ref('')
const redirectUris = ref<string[]>([])
const postLogoutUris = ref<string[]>([])
const scopes = ref<string[]>(['openid'])
const requireConsent = ref(false)
const requirePkce = ref(true)
const disabled = ref(false)
const subjectSource = ref<OidcApplication['subjectSource']>('sub')
const claimAliasesText = ref('{}')
const claimAliasesError = ref('')
const { flag: saved, trigger: triggerSaved } = useTransientFlag()
const { flag: projectionSaved, trigger: triggerProjectionSaved } = useTransientFlag()

const confirmRotate = ref(false)
const rotatedSecret = ref('')
const confirmDelete = ref(false)

function validateUri(s: string): string | null {
  try {
    const u = new URL(s)
    return (u.protocol === 'http:' || u.protocol === 'https:' || u.protocol.length > 0) ? null : t('admin.oidc.uriInvalid')
  } catch {
    return t('admin.oidc.uriInvalid')
  }
}

function seedForm(c: OidcApplication): void {
  displayName.value = c.displayName
  launchUrl.value = c.launchUrl ?? ''
  redirectUris.value = [...c.redirectUris]
  postLogoutUris.value = [...c.postLogoutRedirectUris]
  // openid is mandatory; defend against a payload that somehow lacks it so the
  // (disabled) checkbox can't strand the form in an openid-less state.
  scopes.value = c.allowedScopes.includes('openid') ? [...c.allowedScopes] : ['openid', ...c.allowedScopes]
  requireConsent.value = c.requireConsent
  requirePkce.value = c.requirePkce
  disabled.value = c.disabled
}
const draft = useDraftSync(client, () => [displayName.value, launchUrl.value, redirectUris.value, postLogoutUris.value, scopes.value, requireConsent.value, requirePkce.value, disabled.value], seedForm)

function seedProjection(c: OidcApplication): void {
  subjectSource.value = c.subjectSource || 'sub'
  claimAliasesText.value = JSON.stringify(c.claimAliases ?? {}, null, 2)
  claimAliasesError.value = ''
}
const projectionDraft = useDraftSync(client, () => [subjectSource.value, claimAliasesText.value], seedProjection)

async function save(): Promise<void> {
  rotatedSecret.value = ''
  const updated = await run(() => withSudo(() => api.put<OidcApplication>(`/api/prohibitorum/oidc-applications/${clientId}`, {
    displayName: displayName.value,
    launchUrl: launchUrl.value,
    redirectUris: redirectUris.value,
    postLogoutRedirectUris: postLogoutUris.value,
    allowedScopes: scopes.value,
    requireConsent: requireConsent.value,
    requirePkce: requirePkce.value,
    disabled: disabled.value,
  }), t('sudo.reason.saveChanges')))
  if (updated) { client.value = updated; draft.accept(updated); triggerSaved() }
}

function parseClaimAliases(): Record<string, string> | null {
  try {
    const parsed: unknown = JSON.parse(claimAliasesText.value)
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) throw new Error()
    const aliases = parsed as Record<string, unknown>
    const sources = new Set(['name', 'preferred_username', 'email', 'picture'])
    const reserved = new Set(['iss', 'sub', 'aud', 'exp', 'iat', 'auth_time', 'sid', 'amr', 'nonce', 'acr', 'at_hash', 'azp', 'urn:prohibitorum:account_id'])
    if (Object.entries(aliases).some(([name, source]) => !/^[A-Za-z_][A-Za-z0-9_]{0,63}$/.test(name) || reserved.has(name) || typeof source !== 'string' || !sources.has(source))) throw new Error()
    claimAliasesError.value = ''
    return aliases as Record<string, string>
  } catch {
    claimAliasesError.value = t('admin.oidc.claimAliasesInvalid')
    return null
  }
}

async function saveProjection(): Promise<void> {
  const aliases = parseClaimAliases()
  if (!aliases) return
  const updated = await run(() => api.put<OidcApplication>(`/api/prohibitorum/oidc-applications/${clientId}/identity-projection`, {
    subjectSource: subjectSource.value,
    claimAliases: aliases,
  }))
  if (updated) { client.value = updated; projectionDraft.accept(updated); triggerProjectionSaved() }
}

async function rotateSecret(): Promise<void> {
  const res = await run(() => withSudo(() =>
    api.post<{ clientId: string; secret: string }>('/api/prohibitorum/oidc-applications/rotate-secret', { clientId }),
    t('sudo.reason.rotateSecret')))
  confirmRotate.value = false
  if (res) rotatedSecret.value = res.secret
}

// Flip the disabled flag on its own (independent of the config Save), via the
// dedicated set-disabled endpoint.
async function toggleDisabled(): Promise<void> {
  rotatedSecret.value = ''
  const next = !disabled.value
  const updated = await run(() => withSudo(() =>
    api.post<OidcApplication>('/api/prohibitorum/oidc-applications/set-disabled', { clientId, disabled: next }),
    t('sudo.reason.disableApp')))
  if (updated) { client.value = updated; disabled.value = updated.disabled }
}

async function destroy(): Promise<void> {
  rotatedSecret.value = ''
  const ok = await run(() => withSudo(async () => {
    await api.post('/api/prohibitorum/oidc-applications/delete', { clientId })
    await removeDetail(queryClient, 'oidc-applications', clientId)
    return true as const
  }, t('sudo.reason.deleteApp')))
  confirmDelete.value = false
  if (ok) router.push(backPath.value)
}

async function handleSelfRemoval(): Promise<void> {
  await removeManagedApplication(queryClient, 'oidc-applications', clientId, 'oidc', clientId)
  await router.push(backPath.value)
}

usePrivateState(() => { rotatedSecret.value = '' })
</script>
<template>
  <div class="flex max-w-4xl flex-col gap-6">
    <BackLink :to="backPath" :label="delegated ? t('manage.applications.back') : t('admin.oidc.back')" />
    <ErrorPanel v-if="error && !notFound" :error="error" @dismiss="clear" :is-admin="props.mode === 'admin'" />
    <p v-if="notFound" class="text-sm text-muted" role="status">{{ delegated ? t('manage.applications.notFound') : t('admin.oidc.notFound') }}</p>

    <CardSkeleton v-else-if="busy && !client" />

    <template v-else-if="client">
      <h1 class="text-2xl font-semibold tracking-tight text-ink">{{ client.displayName }}</h1>

      <Card>
        <CardHeader><CardTitle>{{ t('admin.oidc.configTitle') }}</CardTitle></CardHeader>
        <CardContent class="flex flex-col gap-4">
          <div class="flex flex-col gap-1.5">
            <Label>{{ t('admin.oidc.clientId') }}</Label>
            <p class="font-mono text-sm text-muted" data-test="oidc-client-id">{{ client.clientId }}</p>
            <p class="text-xs text-muted">{{ t('admin.oidc.clientIdDesc') }}</p>
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="displayName">{{ t('admin.oidc.displayName') }}</Label>
            <Input id="displayName" name="displayName" v-model="displayName" />
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="launchUrl">{{ t('admin.oidc.launchUrl') }}</Label>
            <Input id="launchUrl" name="launchUrl" v-model="launchUrl" inputmode="url" :placeholder="t('admin.oidc.launchUrlPlaceholder')" />
          </div>
          <div class="flex flex-col gap-1.5">
            <Label>{{ t('admin.oidc.redirectUris') }}</Label>
            <ListInput v-model="redirectUris" name="redirectUris" inputmode="url"
              :add-label="t('admin.oidc.addRedirectUri')" :placeholder="t('admin.oidc.redirectUriPlaceholder')" :validate="validateUri" />
            <p class="text-xs text-muted">{{ t('admin.oidc.redirectUrisDesc') }}</p>
          </div>
          <div class="flex flex-col gap-1.5">
            <Label>{{ t('admin.oidc.postLogoutUris') }}</Label>
            <ListInput v-model="postLogoutUris" name="postLogoutUris" inputmode="url"
              :add-label="t('admin.oidc.addPostLogoutUri')" :placeholder="t('admin.oidc.postLogoutPlaceholder')" :validate="validateUri" />
            <p class="text-xs text-muted">{{ t('admin.oidc.postLogoutUrisDesc') }}</p>
          </div>
          <div class="flex flex-col gap-1.5">
            <span class="text-sm font-medium text-ink">{{ t('admin.oidc.scopes') }}</span>
            <ScopeSelector :known="oidcScopesDescribed" :allow-custom="false" v-model="scopes" />
            <p class="text-xs text-muted">{{ t('admin.oidc.scopesNote') }}</p>
          </div>
          <SettingRow :label="t('admin.oidc.requireConsent')" :description="t('admin.oidc.requireConsentDesc')" for="requireConsent">
            <Switch id="requireConsent" v-model="requireConsent" />
          </SettingRow>
          <SettingRow :label="t('admin.oidc.requirePkce')" :description="t('admin.oidc.requirePkceDesc')" for="requirePkce">
            <Switch id="requirePkce" data-test="require-pkce" v-model="requirePkce" :disabled="client?.clientAuthMethod === 'none'" />
          </SettingRow>
          <div class="flex items-center gap-3">
            <Button type="button" :disabled="busy" data-test="save" @click="save">{{ t('admin.oidc.save') }}</Button>
            <StatusMessage :show="saved">{{ t('admin.oidc.saved') }}</StatusMessage>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader><CardTitle>{{ t('admin.oidc.identityProjectionTitle') }}</CardTitle></CardHeader>
        <CardContent class="flex flex-col gap-4">
          <p class="max-w-2xl text-sm text-muted">{{ t('admin.oidc.identityProjectionDesc') }}</p>
          <div class="flex max-w-md flex-col gap-1.5">
            <Label for="subject-source">{{ t('admin.oidc.subjectSource') }}</Label>
            <Select :model-value="subjectSource" @update:model-value="(value) => (subjectSource = value as OidcApplication['subjectSource'])">
              <SelectTrigger id="subject-source" data-test="subject-source">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="sub">{{ t('admin.oidc.principalSub') }}</SelectItem>
                <SelectItem value="username">{{ t('admin.oidc.principalUsername') }}</SelectItem>
                <SelectItem value="verified_email">{{ t('admin.oidc.principalVerifiedEmail') }}</SelectItem>
              </SelectContent>
            </Select>
            <p class="text-xs text-muted">{{ t('admin.oidc.subjectSourceDesc') }}</p>
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="claim-aliases">{{ t('admin.oidc.claimAliases') }}</Label>
            <Textarea id="claim-aliases" v-model="claimAliasesText" class="min-h-32 font-mono" data-test="claim-aliases" :aria-invalid="claimAliasesError ? 'true' : undefined" aria-describedby="claim-aliases-help claim-aliases-error" :spellcheck="false" />
            <p id="claim-aliases-help" class="text-xs text-muted">{{ t('admin.oidc.claimAliasesDesc') }}</p>
            <p v-if="claimAliasesError" id="claim-aliases-error" class="text-sm text-destructive" role="alert">{{ claimAliasesError }}</p>
          </div>
          <p class="text-sm text-amber-700">{{ t('admin.oidc.identityProjectionWarning') }}</p>
          <div class="flex flex-wrap items-center gap-3">
            <Button type="button" :disabled="busy" data-test="save-identity-projection" @click="saveProjection">{{ t('admin.oidc.saveIdentityProjection') }}</Button>
            <StatusMessage :show="projectionSaved">{{ t('admin.oidc.identityProjectionSaved') }}</StatusMessage>
          </div>
        </CardContent>
      </Card>

      <EntityIconUpload
        :base-path="`/api/prohibitorum/oidc-applications/${clientId}`"
        :name="client?.displayName ?? clientId"
        :icon-url="client?.iconUrl"
      />

      <AppManagerCard
        kind="oidc"
        :app-id="clientId"
        :mode="props.mode"
        :current-account-id="props.currentAccountId"
        @self-removed="handleSelfRemoval"
      />
      <AppPolicyWorkspace kind="oidc" :app-id="clientId" :display-name="client.displayName" :mode="props.mode" />

      <!-- Danger zone (kept LAST — destructive actions belong at the bottom). -->
      <Card class="border-destructive/30 bg-destructive/[0.02]">
        <CardHeader><CardTitle class="text-destructive">{{ t('admin.oidc.dangerTitle') }}</CardTitle></CardHeader>
        <CardContent class="flex flex-col gap-4">
          <div class="flex flex-col gap-2">
            <div class="flex items-center gap-2">
              <SectionTitle as="h3">{{ t('admin.oidc.statusLabel') }}</SectionTitle>
              <StatusBadge :variant="disabled ? 'danger' : 'success'" data-test="status-badge">
                {{ disabled ? t('admin.oidc.disabled') : t('admin.oidc.active') }}
              </StatusBadge>
            </div>
            <p class="text-xs text-muted">{{ t('admin.oidc.disabledDesc') }}</p>
            <Button type="button" variant="outline" class="w-fit" :disabled="busy" data-test="disable-toggle" @click="toggleDisabled">
              {{ disabled ? t('admin.oidc.enable') : t('admin.oidc.disable') }}
            </Button>
          </div>

          <Separator />
          <div class="flex flex-col gap-2">
            <SectionTitle as="h3">{{ t('admin.oidc.rotateTitle') }}</SectionTitle>
            <template v-if="client.clientAuthMethod !== 'none'">
              <p class="text-xs text-muted">{{ t('admin.oidc.rotateConfirmBody') }}</p>
              <template v-if="rotatedSecret">
                <p class="text-sm text-sage-700" role="status">{{ t('admin.oidc.secretReveal') }}</p>
                <CodeField :value="rotatedSecret" />
              </template>
              <Button type="button" variant="outline" class="w-fit" :disabled="busy" data-test="rotate" @click="confirmRotate = true">{{ t('admin.oidc.rotate') }}</Button>
            </template>
            <p v-else class="text-xs text-muted">{{ t('admin.oidc.publicClient') }}</p>
          </div>

          <Separator />
          <div class="flex flex-col gap-2">
            <SectionTitle as="h3">{{ t('admin.oidc.deleteTitle') }}</SectionTitle>
            <p class="text-xs text-muted">{{ t('admin.oidc.deleteHelp') }}</p>
            <Button type="button" variant="destructive" class="w-fit" :disabled="busy" data-test="delete" @click="confirmDelete = true">{{ t('admin.oidc.delete') }}</Button>
          </div>
        </CardContent>
      </Card>
    </template>

    <ConfirmDialog :open="confirmRotate" :title="t('admin.oidc.rotateConfirmTitle')" :confirm-label="t('admin.oidc.rotate')" :busy="busy"
      @update:open="(v) => { if (!v) confirmRotate = false }" @cancel="confirmRotate = false" @confirm="rotateSecret">
      {{ t('admin.oidc.rotateConfirmBody') }}
    </ConfirmDialog>
    <ConfirmDialog :open="confirmDelete" :title="t('admin.oidc.deleteConfirmTitle')" :confirm-label="t('admin.oidc.delete')" :busy="busy"
      @update:open="(v) => { if (!v) confirmDelete = false }" @cancel="confirmDelete = false" @confirm="destroy">
      {{ t('admin.oidc.deleteConfirmBody') }}
    </ConfirmDialog>
  </div>
</template>
