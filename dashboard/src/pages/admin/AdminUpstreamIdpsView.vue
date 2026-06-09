<script setup lang="ts">
/** AdminUpstreamIdpsView (/admin/identity-providers) — list upstream IdPs; inline create (sudo). */
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { withSudo } from '@/lib/sudo'
import { Card, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/table'
import StatusBadge from '@/components/custom/StatusBadge.vue'
import RadioCardGroup from '@/components/custom/RadioCardGroup.vue'
import TagInput from '@/components/custom/TagInput.vue'
import ListInput from '@/components/custom/ListInput.vue'
import SettingRow from '@/components/custom/SettingRow.vue'
import FormSection from '@/components/custom/FormSection.vue'

export interface IdentityProvider {
  slug: string; displayName: string; issuerUrl: string; clientId: string
  scopes: string[]; mode: 'auto_provision' | 'invite_only' | 'link_only'; allowedDomains: string[]
  usernameClaim: string; displayNameClaim: string; emailClaim: string
  requireVerifiedEmail: boolean; disabled: boolean; createdAt: string
}

const { t, te } = useI18n()
const router = useRouter()
const { busy, error, run } = useApi()

const rows = ref<IdentityProvider[]>([])
const createOpen = ref(false)
const created = ref(false)

const slug = ref(''); const displayName = ref(''); const issuerUrl = ref(''); const clientId = ref('')
const clientSecret = ref(''); const mode = ref('auto_provision')
const scopes = ref<string[]>(['openid', 'email', 'profile'])
const allowedDomains = ref<string[]>([])
const usernameClaim = ref('preferred_username'); const displayNameClaim = ref('name'); const emailClaim = ref('email')
const requireVerifiedEmail = ref(false)

function validateDomain(s: string): string | null { return /^[a-z0-9.-]+\.[a-z]{2,}$/i.test(s) ? null : t('admin.upstream.domainInvalid') }

const errorText = computed(() => {
  const e = error.value
  if (!e) return ''
  const key = `errors.${e.code}`
  return te(key) ? t(key) : e.message || t('common.error')
})

function modeLabel(m: string): string {
  if (m === 'invite_only') return t('admin.upstream.modeInviteOnly')
  if (m === 'link_only') return t('admin.upstream.modeLinkOnly')
  return t('admin.upstream.modeAutoProvision')
}
function go(s: string): void { router.push(`/admin/identity-providers/${s}`) }

async function load(): Promise<void> {
  const res = await run(() => api.get<IdentityProvider[]>('/api/prohibitorum/identity-providers'))
  if (res) rows.value = res
}

function openCreate(): void {
  slug.value = ''; displayName.value = ''; issuerUrl.value = ''; clientId.value = ''
  clientSecret.value = ''; mode.value = 'auto_provision'
  scopes.value = ['openid', 'email', 'profile']; allowedDomains.value = []
  usernameClaim.value = 'preferred_username'; displayNameClaim.value = 'name'; emailClaim.value = 'email'
  requireVerifiedEmail.value = false; created.value = false; createOpen.value = true
}

async function create(): Promise<void> {
  created.value = false
  const res = await run(() => withSudo(() => api.post<IdentityProvider>('/api/prohibitorum/identity-providers', {
    slug: slug.value, displayName: displayName.value, issuerUrl: issuerUrl.value, clientId: clientId.value,
    clientSecret: clientSecret.value, mode: mode.value, scopes: scopes.value,
    allowedDomains: allowedDomains.value, usernameClaim: usernameClaim.value,
    displayNameClaim: displayNameClaim.value, emailClaim: emailClaim.value,
    requireVerifiedEmail: requireVerifiedEmail.value,
  })))
  if (res) { createOpen.value = false; created.value = true; await load() }
}

onMounted(load)
</script>
<template>
  <div class="flex max-w-4xl flex-col gap-6">
    <div class="flex items-center justify-between gap-4">
      <h1 class="text-2xl font-semibold tracking-tight text-ink">{{ t('admin.upstream.title') }}</h1>
      <Button type="button" data-test="create" @click="openCreate">{{ t('admin.upstream.create') }}</Button>
    </div>
    <p class="text-sm text-muted">{{ t('admin.upstream.poweredNote') }}</p>
    <Alert v-if="errorText" variant="destructive" role="alert" aria-live="polite"><AlertDescription>{{ errorText }}</AlertDescription></Alert>
    <p v-if="created" class="text-sm text-sage" role="status">{{ t('admin.upstream.created') }}</p>

    <Card v-if="createOpen">
      <CardContent class="flex max-w-xl flex-col gap-5 py-4">
        <FormSection :title="t('admin.upstream.sectionConnection')">
          <div class="flex flex-col gap-1.5">
            <Label for="slug">{{ t('admin.upstream.slug') }}</Label>
            <Input id="slug" name="slug" v-model="slug" autocomplete="off" />
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="displayName">{{ t('admin.upstream.displayName') }}</Label>
            <Input id="displayName" name="displayName" v-model="displayName" autocomplete="off" />
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="issuerUrl">{{ t('admin.upstream.issuerUrl') }}</Label>
            <Input id="issuerUrl" name="issuerUrl" v-model="issuerUrl" autocomplete="off" />
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
            <TagInput input-id="scopes" v-model="scopes" :placeholder="t('admin.upstream.scopesHint')" :aria-label="t('admin.upstream.scopes')" />
          </div>
        </FormSection>
        <FormSection :title="t('admin.upstream.sectionProvisioning')">
          <div class="flex flex-col gap-1.5">
            <Label>{{ t('admin.upstream.mode') }}</Label>
            <RadioCardGroup v-model="mode" :aria-label="t('admin.upstream.mode')" :options="[
              {value:'auto_provision',title:t('admin.upstream.modeAutoProvision'),description:t('admin.upstream.modeAutoProvisionDesc')},
              {value:'invite_only',title:t('admin.upstream.modeInviteOnly'),description:t('admin.upstream.modeInviteOnlyDesc')},
              {value:'link_only',title:t('admin.upstream.modeLinkOnly'),description:t('admin.upstream.modeLinkOnlyDesc')}]" />
          </div>
          <div class="flex flex-col gap-1.5">
            <Label>{{ t('admin.upstream.allowedDomains') }}</Label>
            <ListInput v-model="allowedDomains" name="allowedDomains"
              :add-label="t('admin.upstream.addDomain')" :placeholder="t('admin.upstream.domainPlaceholder')" :validate="validateDomain" />
            <p class="text-xs text-muted">{{ t('admin.upstream.domainsHint') }}</p>
          </div>
          <SettingRow :label="t('admin.upstream.requireVerifiedEmail')" :description="t('admin.upstream.requireVerifiedEmailDesc')" for="requireVerifiedEmail">
            <Switch id="requireVerifiedEmail" data-test="requireVerifiedEmail" v-model="requireVerifiedEmail" />
          </SettingRow>
        </FormSection>
        <FormSection :title="t('admin.upstream.sectionClaims')">
          <div class="flex flex-col gap-1.5">
            <Label for="usernameClaim">{{ t('admin.upstream.usernameClaim') }}</Label>
            <Input id="usernameClaim" name="usernameClaim" v-model="usernameClaim" autocomplete="off" />
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="displayNameClaim">{{ t('admin.upstream.displayNameClaim') }}</Label>
            <Input id="displayNameClaim" name="displayNameClaim" v-model="displayNameClaim" autocomplete="off" />
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="emailClaim">{{ t('admin.upstream.emailClaim') }}</Label>
            <Input id="emailClaim" name="emailClaim" v-model="emailClaim" autocomplete="off" />
          </div>
        </FormSection>
        <div class="flex gap-2">
          <Button type="button" :disabled="busy" data-test="create-confirm" @click="create">{{ t('admin.upstream.create') }}</Button>
          <Button type="button" variant="outline" :disabled="busy" data-test="create-cancel" @click="createOpen = false">{{ t('common.cancel') }}</Button>
        </div>
      </CardContent>
    </Card>

    <Table v-if="rows.length">
      <TableHeader>
        <TableRow>
          <TableHead>{{ t('admin.upstream.colName') }}</TableHead>
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
    <p v-else-if="!busy && !errorText && !createOpen" class="text-sm text-muted">{{ t('admin.upstream.empty') }}</p>
  </div>
</template>
