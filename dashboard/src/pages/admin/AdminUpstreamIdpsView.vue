<script setup lang="ts">
/** AdminUpstreamIdpsView (/admin/identity-providers) — list upstream IdPs; inline create (sudo). */
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import StatusMessage from '@/components/custom/StatusMessage.vue'
import { useRouter } from 'vue-router'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { useCursorPage } from '@/composables/useCursorPage'
import { type Page, buildPagePath } from '@/lib/pagination'
import { useTransientFlag } from '@/composables/useTransientFlag'
import { withSudo } from '@/lib/sudo'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/table'
import StatusBadge from '@/components/custom/StatusBadge.vue'
import RadioCardGroup from '@/components/custom/RadioCardGroup.vue'
import ScopeSelector from '@/components/custom/ScopeSelector.vue'
import ListInput from '@/components/custom/ListInput.vue'
import { UPSTREAM_SCOPE_SUGGESTIONS } from '@/lib/scopes'
import SettingRow from '@/components/custom/SettingRow.vue'
import FormSection from '@/components/custom/FormSection.vue'
import TableSkeleton from '@/components/custom/TableSkeleton.vue'
import EmptyState from '@/components/custom/EmptyState.vue'
import ErrorPanel from '@/components/custom/ErrorPanel.vue'
import PaginationControls from '@/components/custom/PaginationControls.vue'
import { Link2 } from 'lucide-vue-next'

export interface OIDCProviderConfig {
  issuerUrl: string
  clientId: string
  scopes: string[]
  allowedDomains: string[]
  usernameClaim: string
  displayNameClaim: string
  emailClaim: string
  pictureClaim: string
  requireVerifiedEmail: boolean
  allowPrivateNetwork: boolean
}

export interface IdentitySearchField {
  key: string
  operators: string[]
}

export type ProviderProtocol = 'oidc' | 'steam' | 'vrchat'
export type ProviderMode = 'auto_provision' | 'invite_only' | 'link_only'

export interface IdentityProvider {
  slug: string
  displayName: string
  protocol: ProviderProtocol
  mode: ProviderMode
  config: Record<string, unknown>
  disabled: boolean
  secretConfigured: boolean
  secretStatus: 'unconfigured' | 'configured' | 'valid' | 'invalid'
  secretValidatedAt: string | null
  ready: boolean
  supportsOperator: boolean
  searchFields: IdentitySearchField[]
  createdAt: string
  iconUrl?: string | null
}

const { t } = useI18n()
const router = useRouter()
const { busy, run, error, clear } = useApi()

const page = useCursorPage<IdentityProvider>((cursor) =>
  api.get<Page<IdentityProvider>>(buildPagePath('/api/prohibitorum/identity-providers', { cursor })),
)
const rows = page.items
const displayError = computed(() => page.error.value ?? error.value)
function clearError(): void { page.clear(); clear() }
const createOpen = ref(false)
const { flag: created, trigger: triggerCreated } = useTransientFlag()

const slug = ref(''); const displayName = ref(''); const issuerUrl = ref(''); const clientId = ref('')
const clientSecret = ref(''); const mode = ref<ProviderMode>('auto_provision')
const scopes = ref<string[]>(['openid', 'profile', 'email'])
const allowedDomains = ref<string[]>([])
const usernameClaim = ref('preferred_username'); const displayNameClaim = ref('name'); const emailClaim = ref('email'); const pictureClaim = ref('picture')
const requireVerifiedEmail = ref(false)
const protocol = ref<ProviderProtocol>('oidc'); const apiKey = ref('')

function validateDomain(s: string): string | null { return /^[a-z0-9.-]+\.[a-z]{2,}$/i.test(s) ? null : t('admin.upstream.domainInvalid') }

const upstreamScopesKnown = computed(() => UPSTREAM_SCOPE_SUGGESTIONS.map((s) => ({ value: s.value, description: t(s.descKey) })))

function modeLabel(m: string): string {
  if (m === 'invite_only') return t('admin.upstream.modeInviteOnly')
  if (m === 'link_only') return t('admin.upstream.modeLinkOnly')
  return t('admin.upstream.modeAutoProvision')
}
function go(s: string): void { router.push(`/admin/identity-providers/${s}`) }



function openCreate(): void {
  slug.value = ''; displayName.value = ''; issuerUrl.value = ''; clientId.value = ''
  clientSecret.value = ''; mode.value = 'auto_provision'
  scopes.value = ['openid', 'profile', 'email']; allowedDomains.value = []
  usernameClaim.value = 'preferred_username'; displayNameClaim.value = 'name'; emailClaim.value = 'email'; pictureClaim.value = 'picture'
  requireVerifiedEmail.value = false; protocol.value = 'oidc'; apiKey.value = ''; createOpen.value = true
}

type CreateProviderRequest =
  | { slug: string; displayName: string; protocol: 'oidc'; mode: ProviderMode; config: OIDCProviderConfig; secret: string }
  | { slug: string; displayName: string; protocol: 'steam'; mode: ProviderMode; config: Record<string, never>; secret: string }
  | { slug: string; displayName: string; protocol: 'vrchat'; mode: ProviderMode; config: Record<string, never> }

function buildCreateRequest(selected: ProviderProtocol): CreateProviderRequest {
  const common = { slug: slug.value, displayName: displayName.value, mode: mode.value }
  switch (selected) {
    case 'oidc':
      return {
        ...common,
        protocol: selected,
        config: {
          issuerUrl: issuerUrl.value,
          clientId: clientId.value,
          scopes: scopes.value,
          allowedDomains: allowedDomains.value,
          usernameClaim: usernameClaim.value,
          displayNameClaim: displayNameClaim.value,
          emailClaim: emailClaim.value,
          pictureClaim: pictureClaim.value,
          requireVerifiedEmail: requireVerifiedEmail.value,
          allowPrivateNetwork: false,
        },
        secret: clientSecret.value,
      }
    case 'steam':
      return { ...common, protocol: selected, config: {}, secret: apiKey.value }
    case 'vrchat':
      return { ...common, protocol: selected, config: {} }
  }
}

async function create(): Promise<void> {
  const body = buildCreateRequest(protocol.value)
  const res = await run(() => withSudo(() =>
    api.post<IdentityProvider>('/api/prohibitorum/identity-providers', body),
  ))
  if (!res) return
  createOpen.value = false
  if (res.protocol === 'vrchat') {
    await router.push(`/admin/identity-providers/${res.slug}`)
    return
  }
  triggerCreated()
  await page.reload()
}

</script>
<template>
  <div class="flex max-w-4xl flex-col gap-6">
    <div class="flex items-center justify-between gap-4">
      <h1 class="text-2xl font-semibold tracking-tight text-ink">{{ t('admin.upstream.title') }}</h1>
      <Button type="button" data-test="create" @click="openCreate">{{ t('admin.upstream.create') }}</Button>
    </div>
    <p class="text-sm text-muted">{{ t('admin.upstream.poweredNote') }}</p>
    <ErrorPanel :error="displayError" @dismiss="clearError" :is-admin="true" />
    <StatusMessage :show="created">{{ t('admin.upstream.created') }}</StatusMessage>

    <Card v-if="createOpen">
      <CardHeader><CardTitle>{{ t('admin.upstream.createTitle') }}</CardTitle></CardHeader>
      <CardContent class="flex flex-col gap-5 py-4">
        <FormSection :title="t('admin.upstream.sectionConnection')">
          <div class="flex flex-col gap-1.5">
            <Label for="slug">{{ t('admin.upstream.slug') }}</Label>
            <Input id="slug" name="slug" v-model="slug" autocomplete="off" />
            <p class="text-xs text-muted">{{ t('admin.upstream.slugDesc') }}</p>
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="displayName">{{ t('admin.upstream.displayName') }}</Label>
            <Input id="displayName" name="displayName" v-model="displayName" autocomplete="off" />
          </div>
          <div class="flex flex-col gap-1.5">
            <Label>{{ t('admin.upstream.protocol') }}</Label>
            <RadioCardGroup v-model="protocol" :aria-label="t('admin.upstream.protocol')" :options="[
              {value:'oidc',title:t('admin.upstream.protocolOidc'),description:t('admin.upstream.protocolOidcDesc')},
              {value:'steam',title:t('admin.upstream.protocolSteam'),description:t('admin.upstream.protocolSteamDesc')},
              {value:'vrchat',title:t('admin.upstream.protocolVrchat'),description:t('admin.upstream.protocolVrchatDesc')}]" />
          </div>
          <Alert v-if="protocol === 'vrchat'" role="note" data-test="vrchat-create-warning">
            <AlertDescription class="max-w-[75ch]">{{ t('admin.upstream.vrchatCreateWarning') }}</AlertDescription>
          </Alert>
          <template v-if="protocol === 'oidc'">
            <div class="flex flex-col gap-1.5">
              <Label for="issuerUrl">{{ t('admin.upstream.issuerUrl') }}</Label>
              <Input id="issuerUrl" name="issuerUrl" v-model="issuerUrl" autocomplete="off" />
              <p class="text-xs text-muted">{{ t('admin.upstream.issuerUrlDesc') }}</p>
            </div>
            <div class="flex flex-col gap-1.5">
              <Label for="clientId">{{ t('admin.upstream.clientId') }}</Label>
              <Input id="clientId" name="clientId" v-model="clientId" autocomplete="off" />
            </div>
            <div class="flex flex-col gap-1.5">
              <Label for="clientSecret">{{ t('admin.upstream.clientSecret') }}</Label>
              <Input id="clientSecret" name="clientSecret" type="password" v-model="clientSecret" autocomplete="off" />
            </div>
            <div class="flex flex-col gap-1.5">
              <Label for="scopes">{{ t('admin.upstream.scopes') }}</Label>
              <ScopeSelector :known="upstreamScopesKnown" :allow-custom="true" v-model="scopes" />
              <p class="text-xs text-muted">{{ t('admin.upstream.scopesDesc') }}</p>
            </div>
          </template>
          <template v-if="protocol === 'steam'">
            <div class="flex flex-col gap-1.5">
              <Label for="apiKey">{{ t('admin.upstream.steamApiKey') }}</Label>
              <Input id="apiKey" name="apiKey" type="password" v-model="apiKey" autocomplete="off" />
              <p class="text-xs text-muted">{{ t('admin.upstream.steamApiKeyDesc') }}</p>
            </div>
          </template>
        </FormSection>
        <FormSection :title="t('admin.upstream.sectionProvisioning')">
          <div class="flex flex-col gap-1.5">
            <Label>{{ t('admin.upstream.mode') }}</Label>
            <RadioCardGroup v-model="mode" :aria-label="t('admin.upstream.mode')" :options="[
              {value:'auto_provision',title:t('admin.upstream.modeAutoProvision'),description:t('admin.upstream.modeAutoProvisionDesc')},
              {value:'invite_only',title:t('admin.upstream.modeInviteOnly'),description:t('admin.upstream.modeInviteOnlyDesc')},
              {value:'link_only',title:t('admin.upstream.modeLinkOnly'),description:t('admin.upstream.modeLinkOnlyDesc')}]" />
          </div>
          <div v-if="protocol === 'oidc'" class="flex flex-col gap-1.5">
            <Label>{{ t('admin.upstream.allowedDomains') }}</Label>
            <ListInput v-model="allowedDomains" name="allowedDomains"
              :add-label="t('admin.upstream.addDomain')" :placeholder="t('admin.upstream.domainPlaceholder')" :validate="validateDomain" />
            <p class="text-xs text-muted">{{ t('admin.upstream.domainsHint') }}</p>
          </div>
          <SettingRow v-if="protocol === 'oidc'" :label="t('admin.upstream.requireVerifiedEmail')" :description="t('admin.upstream.requireVerifiedEmailDesc')" for="requireVerifiedEmail">
            <Switch id="requireVerifiedEmail" data-test="requireVerifiedEmail" v-model="requireVerifiedEmail" />
          </SettingRow>
        </FormSection>
        <FormSection v-if="protocol === 'oidc'" :title="t('admin.upstream.sectionClaims')">
          <div class="grid grid-cols-[minmax(7rem,auto)_1fr] items-center gap-x-3 gap-y-2">
            <Label class="text-sm" for="usernameClaim">{{ t('admin.upstream.usernameClaim') }}</Label>
            <Input id="usernameClaim" name="usernameClaim" class="h-8" v-model="usernameClaim" placeholder="preferred_username" autocomplete="off" data-test="claim-username" />
            <Label class="text-sm" for="displayNameClaim">{{ t('admin.upstream.displayNameClaim') }}</Label>
            <Input id="displayNameClaim" name="displayNameClaim" class="h-8" v-model="displayNameClaim" placeholder="name" autocomplete="off" data-test="claim-displayName" />
            <Label class="text-sm" for="emailClaim">{{ t('admin.upstream.emailClaim') }}</Label>
            <Input id="emailClaim" name="emailClaim" class="h-8" v-model="emailClaim" placeholder="email" autocomplete="off" data-test="claim-email" />
            <Label class="text-sm" for="pictureClaim">{{ t('admin.upstream.pictureClaim') }}</Label>
            <Input id="pictureClaim" name="pictureClaim" class="h-8" v-model="pictureClaim" placeholder="picture" autocomplete="off" data-test="claim-avatar" />
          </div>
          <p class="text-xs text-muted">{{ t('admin.upstream.claimsHint') }}</p>
        </FormSection>
        <div class="flex gap-2">
          <Button type="button" :disabled="busy" data-test="create-confirm" @click="create">{{ t('admin.upstream.create') }}</Button>
          <Button type="button" variant="outline" :disabled="busy" data-test="create-cancel" @click="createOpen = false">{{ t('common.cancel') }}</Button>
        </div>
      </CardContent>
    </Card>

    <TableSkeleton v-if="page.busy.value && !rows.length" :rows="5" :cols="3" />
    <Table v-else-if="rows.length">
      <TableHeader>
        <TableRow>
          <TableHead>{{ t('admin.upstream.colName') }} · {{ t('admin.upstream.colSlug') }}</TableHead>
          <TableHead>{{ t('admin.upstream.colMode') }}</TableHead>
          <TableHead>{{ t('admin.upstream.colState') }}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <TableRow v-for="i in rows" :key="i.slug" class="cursor-pointer" tabindex="0"
                  :data-test="`idp-row-${i.slug}`"
                  @click="go(i.slug)" @keydown.enter="go(i.slug)" @keydown.space.prevent="go(i.slug)">
          <TableCell>
            <div class="flex min-w-0 flex-col">
              <span class="truncate font-medium text-ink">{{ i.displayName }}</span>
              <span class="truncate text-muted">{{ i.slug }}</span>
            </div>
          </TableCell>
          <TableCell><StatusBadge variant="neutral">{{ modeLabel(i.mode) }}</StatusBadge></TableCell>
          <TableCell>
            <StatusBadge :variant="i.disabled ? 'danger' : 'success'">
              {{ i.disabled ? t('admin.upstream.disabled') : t('admin.upstream.active') }}
            </StatusBadge>
          </TableCell>
        </TableRow>
      </TableBody>
    </Table>
    <EmptyState v-else-if="!displayError && !createOpen" :icon="Link2" :title="t('admin.upstream.empty')" />
    <PaginationControls
      v-if="rows.length || page.pageIndex.value > 0"
      :page-index="page.pageIndex.value"
      :has-more="page.hasMore.value"
      :busy="page.busy.value"
      :has-items="rows.length > 0"
      @next="page.next"
      @previous="page.previous"
    />
  </div>
</template>
