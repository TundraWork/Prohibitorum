<script setup lang="ts">
/** AdminOidcClientsView (/admin/oidc-applications) — table of OIDC clients; inline create with reveal-once secret. */
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
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/table'
import { Textarea } from '@/components/ui/textarea'
import { Checkbox } from '@/components/ui/checkbox'
import { Switch } from '@/components/ui/switch'
import StatusBadge from '@/components/custom/StatusBadge.vue'
import CodeField from '@/components/custom/CodeField.vue'

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

const { t, te } = useI18n()
const router = useRouter()
const { busy, error, run } = useApi()

const rows = ref<OidcApplication[]>([])
const createOpen = ref(false)
const created = ref(false)
const revealedSecret = ref('')

// Create form state
const clientId = ref('')
const displayName = ref('')
const redirectUris = ref('')
const postLogoutUris = ref('')
const scopes = ref<string[]>(['openid'])
const isPublic = ref(false)
const requireConsent = ref(false)

const errorText = computed(() => {
  const e = error.value
  if (!e) return ''
  const key = `errors.${e.code}`
  return te(key) ? t(key) : e.message || t('common.error')
})

async function load(): Promise<void> {
  const res = await run(() => api.get<OidcApplication[]>('/api/prohibitorum/oidc-applications'))
  if (res) rows.value = res
}

function go(id: string): void { router.push(`/admin/oidc-applications/${id}`) }

function lines(s: string): string[] {
  return s.split('\n').map((x) => x.trim()).filter(Boolean)
}

function toggleScope(scope: string, checked: boolean): void {
  if (checked && !scopes.value.includes(scope)) {
    scopes.value = [...scopes.value, scope]
  } else if (!checked) {
    scopes.value = scopes.value.filter((s) => s !== scope)
  }
}

async function create(): Promise<void> {
  created.value = false
  const res = await run(() => withSudo(() => api.post<{ clientId: string; secret?: string }>('/api/prohibitorum/oidc-applications', {
    clientId: clientId.value,
    displayName: displayName.value,
    redirectUris: lines(redirectUris.value),
    postLogoutRedirectUris: lines(postLogoutUris.value),
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
  redirectUris.value = ''
  postLogoutUris.value = ''
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
      <CardContent class="flex flex-col gap-3 py-4">
        <div class="flex flex-col gap-1.5">
          <Label for="clientId">{{ t('admin.oidc.clientId') }}</Label>
          <Input id="clientId" name="clientId" v-model="clientId" autocomplete="off" />
        </div>
        <div class="flex flex-col gap-1.5">
          <Label for="displayName">{{ t('admin.oidc.displayName') }}</Label>
          <Input id="displayName" name="displayName" v-model="displayName" />
        </div>
        <div class="flex flex-col gap-1.5">
          <Label for="redirectUris">{{ t('admin.oidc.redirectUris') }}</Label>
          <Textarea id="redirectUris" name="redirectUris" v-model="redirectUris" :placeholder="t('admin.oidc.urisHint')" />
        </div>
        <div class="flex flex-col gap-1.5">
          <Label for="postLogoutUris">{{ t('admin.oidc.postLogoutUris') }}</Label>
          <Textarea id="postLogoutUris" name="postLogoutRedirectUris" v-model="postLogoutUris" :placeholder="t('admin.oidc.urisHint')" />
        </div>
        <div class="flex flex-col gap-1.5">
          <span class="text-sm font-medium text-ink">{{ t('admin.oidc.scopes') }}</span>
          <div class="flex flex-col gap-1">
            <label class="flex items-center gap-2 text-sm text-ink">
              <Checkbox :model-value="scopes.includes('openid')" disabled />
              openid
            </label>
            <label class="flex items-center gap-2 text-sm text-ink">
              <Checkbox :model-value="scopes.includes('profile')" @update:model-value="(c) => toggleScope('profile', c === true)" />
              profile
            </label>
            <label class="flex items-center gap-2 text-sm text-ink">
              <Checkbox :model-value="scopes.includes('email')" @update:model-value="(c) => toggleScope('email', c === true)" />
              email
            </label>
          </div>
        </div>
        <div class="flex items-center justify-between gap-3">
          <Label for="public" class="font-normal text-ink">{{ t('admin.oidc.publicClient') }}</Label>
          <Switch id="public" name="public" data-test="public" v-model="isPublic" />
        </div>
        <div class="flex items-center justify-between gap-3">
          <Label for="requireConsent" class="font-normal text-ink">{{ t('admin.oidc.requireConsent') }}</Label>
          <Switch id="requireConsent" name="requireConsent" v-model="requireConsent" />
        </div>
        <div class="flex gap-2">
          <Button type="button" :disabled="busy" data-test="create-confirm" @click="create">{{ t('admin.oidc.create') }}</Button>
          <Button type="button" variant="outline" :disabled="busy" data-test="create-cancel" @click="createOpen = false">{{ t('common.cancel') }}</Button>
        </div>
      </CardContent>
    </Card>

    <Table v-if="rows.length">
      <TableHeader>
        <TableRow>
          <TableHead>{{ t('admin.oidc.colClient') }}</TableHead>
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
    <p v-else-if="!busy && !errorText && !createOpen" class="text-sm text-muted">{{ t('admin.oidc.empty') }}</p>
  </div>
</template>
