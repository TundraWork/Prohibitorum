<script setup lang="ts">
/** AdminOidcClientsView (/admin/oidc-applications) — table of OIDC clients; inline create with reveal-once secret. */
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { withSudo } from '@/lib/sudo'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/table'
import { Switch } from '@/components/ui/switch'
import StatusBadge from '@/components/custom/StatusBadge.vue'
import OidcScopePicker from '@/components/custom/OidcScopePicker.vue'
import CodeField from '@/components/custom/CodeField.vue'
import TableSkeleton from '@/components/custom/TableSkeleton.vue'
import ListInput from '@/components/custom/ListInput.vue'
import SettingRow from '@/components/custom/SettingRow.vue'
import FormSection from '@/components/custom/FormSection.vue'
import EmptyState from '@/components/custom/EmptyState.vue'
import { AppWindow } from 'lucide-vue-next'

interface OidcApplication {
  clientId: string
  displayName: string
  redirectUris: string[]
  postLogoutRedirectUris: string[]
  allowedScopes: string[]
  tokenEndpointAuthMethod: string
  requireConsent: boolean
  disabled: boolean
  createdAt: string
}

const { t } = useI18n()
const router = useRouter()
const { busy, run, errorText } = useApi()

const rows = ref<OidcApplication[]>([])
const createOpen = ref(false)
const created = ref(false)
const revealedSecret = ref('')

// Create form state
const clientId = ref('')
const displayName = ref('')
const redirectUris = ref<string[]>([])
const postLogoutUris = ref<string[]>([])
const scopes = ref<string[]>(['openid'])
const isPublic = ref(false)
const requireConsent = ref(false)

function validateUri(s: string): string | null {
  try {
    const u = new URL(s)
    return (u.protocol === 'http:' || u.protocol === 'https:' || u.protocol.length > 0) ? null : t('admin.oidc.uriInvalid')
  } catch {
    return t('admin.oidc.uriInvalid')
  }
}

async function load(): Promise<void> {
  const res = await run(() => api.get<OidcApplication[]>('/api/prohibitorum/oidc-applications'))
  if (res) rows.value = res
}

function go(id: string): void { router.push(`/admin/oidc-applications/${id}`) }

async function create(): Promise<void> {
  created.value = false
  const res = await run(() => withSudo(() => api.post<{ clientId: string; secret?: string }>('/api/prohibitorum/oidc-applications', {
    clientId: clientId.value,
    displayName: displayName.value,
    redirectUris: redirectUris.value,
    postLogoutRedirectUris: postLogoutUris.value,
    scopes: scopes.value,
    public: isPublic.value,
    requireConsent: requireConsent.value,
  })))
  if (res) {
    createOpen.value = false
    revealedSecret.value = res.secret ?? ''
    created.value = true
    await load()
  }
}

function openCreate(): void {
  // Reset form state
  clientId.value = ''
  displayName.value = ''
  redirectUris.value = []
  postLogoutUris.value = []
  scopes.value = ['openid']
  isPublic.value = false
  requireConsent.value = false
  revealedSecret.value = ''
  created.value = false
  createOpen.value = true
}

onMounted(load)
</script>
<template>
  <div class="flex max-w-4xl flex-col gap-6">
    <div class="flex items-center justify-between gap-4">
      <h1 class="text-2xl font-semibold tracking-tight text-ink">{{ t('admin.oidc.title') }}</h1>
      <Button type="button" data-test="create" @click="openCreate">{{ t('admin.oidc.create') }}</Button>
    </div>
    <Alert v-if="errorText" variant="destructive" role="alert" aria-live="polite"><AlertDescription>{{ errorText }}</AlertDescription></Alert>
    <p v-if="created && !revealedSecret" class="text-sm text-sage" role="status">{{ t('admin.oidc.created') }}</p>

    <template v-if="created && revealedSecret">
      <p class="text-sm text-sage" role="status">{{ t('admin.oidc.secretReveal') }}</p>
      <CodeField :value="revealedSecret" />
    </template>

    <Card v-if="createOpen">
      <CardHeader><CardTitle>{{ t('admin.oidc.createTitle') }}</CardTitle></CardHeader>
      <CardContent class="flex flex-col gap-4 py-4">
        <FormSection :title="t('admin.oidc.sectionBasics')">
          <div class="flex flex-col gap-1.5">
            <Label for="clientId">{{ t('admin.oidc.clientId') }}</Label>
            <Input id="clientId" name="clientId" v-model="clientId" autocomplete="off" />
            <p class="text-xs text-muted">{{ t('admin.oidc.clientIdDesc') }}</p>
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="displayName">{{ t('admin.oidc.displayName') }}</Label>
            <Input id="displayName" name="displayName" v-model="displayName" />
          </div>
        </FormSection>
        <FormSection :title="t('admin.oidc.sectionEndpoints')">
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
        </FormSection>
        <FormSection :title="t('admin.oidc.sectionScopes')">
          <OidcScopePicker v-model="scopes" />
          <p class="text-xs text-muted">{{ t('admin.oidc.scopesNote') }}</p>
        </FormSection>
        <FormSection :title="t('admin.oidc.sectionOptions')">
          <SettingRow :label="t('admin.oidc.publicClient')" :description="t('admin.oidc.publicClientDesc')" for="public">
            <Switch id="public" name="public" data-test="public" v-model="isPublic" />
          </SettingRow>
          <SettingRow :label="t('admin.oidc.requireConsent')" :description="t('admin.oidc.requireConsentDesc')" for="requireConsent">
            <Switch id="requireConsent" v-model="requireConsent" />
          </SettingRow>
        </FormSection>
        <div class="flex gap-2">
          <Button type="button" :disabled="busy" data-test="create-confirm" @click="create">{{ t('admin.oidc.create') }}</Button>
          <Button type="button" variant="outline" :disabled="busy" data-test="create-cancel" @click="createOpen = false">{{ t('common.cancel') }}</Button>
        </div>
      </CardContent>
    </Card>

    <TableSkeleton v-if="busy && !rows.length" :rows="5" :cols="3" />
    <Table v-else-if="rows.length">
      <TableHeader>
        <TableRow>
          <TableHead>{{ t('admin.oidc.colClient') }} · {{ t('admin.oidc.clientId') }}</TableHead>
          <TableHead>{{ t('admin.oidc.colType') }}</TableHead>
          <TableHead>{{ t('admin.oidc.colState') }}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <TableRow v-for="c in rows" :key="c.clientId" class="cursor-pointer" tabindex="0"
                  :data-test="`client-row-${c.clientId}`"
                  @click="go(c.clientId)" @keydown.enter="go(c.clientId)" @keydown.space.prevent="go(c.clientId)">
          <TableCell>
            <div class="flex min-w-0 flex-col">
              <span class="truncate font-medium text-ink">{{ c.displayName }}</span>
              <span class="truncate text-muted">{{ c.clientId }}</span>
            </div>
          </TableCell>
          <TableCell>
            <StatusBadge :variant="c.tokenEndpointAuthMethod !== 'none' ? 'caution' : 'neutral'">
              {{ c.tokenEndpointAuthMethod !== 'none' ? t('admin.oidc.confidential') : t('admin.oidc.public') }}
            </StatusBadge>
          </TableCell>
          <TableCell>
            <StatusBadge :variant="c.disabled ? 'danger' : 'success'">
              {{ c.disabled ? t('admin.oidc.disabled') : t('admin.oidc.active') }}
            </StatusBadge>
          </TableCell>
        </TableRow>
      </TableBody>
    </Table>
    <EmptyState v-else-if="!errorText && !createOpen" :icon="AppWindow" :title="t('admin.oidc.empty')" />
  </div>
</template>
