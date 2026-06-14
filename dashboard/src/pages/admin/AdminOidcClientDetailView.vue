<script setup lang="ts">
/**
 * AdminOidcClientDetailView (/admin/oidc-applications/:clientId) — per-client admin actions.
 * Edit config (PUT with allowedScopes); rotate secret (reveal-once CodeField);
 * delete. All mutations go through withSudo.
 */
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { withSudo } from '@/lib/sudo'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Checkbox } from '@/components/ui/checkbox'
import { Switch } from '@/components/ui/switch'
import { Separator } from '@/components/ui/separator'
import ConfirmDialog from '@/components/custom/ConfirmDialog.vue'
import CodeField from '@/components/custom/CodeField.vue'
import ListInput from '@/components/custom/ListInput.vue'
import SettingRow from '@/components/custom/SettingRow.vue'

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
const route = useRoute()
const router = useRouter()
const { busy, error, run } = useApi()

const clientId = String(route.params.clientId)
const client = ref<OidcApplication | null>(null)
const notFound = ref(false)

const displayName = ref('')
const redirectUris = ref<string[]>([])
const postLogoutUris = ref<string[]>([])
const scopes = ref<string[]>(['openid'])
const requireConsent = ref(false)
const disabled = ref(false)
const saved = ref(false)

const confirmRotate = ref(false)
const rotatedSecret = ref('')
const confirmDelete = ref(false)

const errorText = computed(() => {
  const e = error.value
  if (!e) return ''
  const key = `errors.${e.code}`
  return te(key) ? t(key) : e.message || t('common.error')
})

function validateUri(s: string): string | null {
  try {
    const u = new URL(s)
    return (u.protocol === 'http:' || u.protocol === 'https:' || u.protocol.length > 0) ? null : t('admin.oidc.uriInvalid')
  } catch {
    return t('admin.oidc.uriInvalid')
  }
}

function toggleScope(scope: string, checked: boolean): void {
  if (checked && !scopes.value.includes(scope)) {
    scopes.value = [...scopes.value, scope]
  } else if (!checked) {
    scopes.value = scopes.value.filter((s) => s !== scope)
  }
}

async function load(): Promise<void> {
  const c = await run(() => api.get<OidcApplication>(`/api/prohibitorum/oidc-applications/${clientId}`))
  if (!c) { if (error.value?.code === 'client_not_found') notFound.value = true; return }
  client.value = c
  displayName.value = c.displayName
  redirectUris.value = [...c.redirectUris]
  postLogoutUris.value = [...c.postLogoutRedirectUris]
  scopes.value = [...c.allowedScopes]
  requireConsent.value = c.requireConsent
  disabled.value = c.disabled
}

async function save(): Promise<void> {
  saved.value = false
  rotatedSecret.value = ''
  const updated = await run(() => withSudo(() => api.put<OidcApplication>(`/api/prohibitorum/oidc-applications/${clientId}`, {
    displayName: displayName.value,
    redirectUris: redirectUris.value,
    postLogoutRedirectUris: postLogoutUris.value,
    allowedScopes: scopes.value,
    requireConsent: requireConsent.value,
    disabled: disabled.value,
  })))
  if (updated) { client.value = updated; saved.value = true }
}

async function rotateSecret(): Promise<void> {
  saved.value = false
  const res = await run(() => withSudo(() =>
    api.post<{ clientId: string; secret: string }>('/api/prohibitorum/oidc-applications/rotate-secret', { clientId })))
  confirmRotate.value = false
  if (res) rotatedSecret.value = res.secret
}

async function destroy(): Promise<void> {
  saved.value = false
  rotatedSecret.value = ''
  const ok = await run(() => withSudo(async () => {
    await api.post('/api/prohibitorum/oidc-applications/delete', { clientId })
    return true as const
  }))
  confirmDelete.value = false
  if (ok) router.push('/admin/oidc-applications')
}

onMounted(load)
</script>
<template>
  <div class="flex max-w-2xl flex-col gap-6">
    <RouterLink to="/admin/oidc-applications" class="text-sm text-muted underline-offset-4 hover:underline">{{ t('admin.oidc.back') }}</RouterLink>
    <Alert v-if="errorText" variant="destructive" role="alert" aria-live="polite"><AlertDescription>{{ errorText }}</AlertDescription></Alert>
    <p v-if="notFound" class="text-sm text-muted" role="status">{{ t('admin.oidc.notFound') }}</p>

    <template v-else-if="client">
      <h1 class="text-2xl font-semibold tracking-tight text-ink">{{ client.displayName }}</h1>

      <Card>
        <CardHeader><CardTitle>{{ t('admin.oidc.configTitle') }}</CardTitle></CardHeader>
        <CardContent class="flex flex-col gap-4">
          <div class="flex flex-col gap-1.5">
            <Label for="displayName">{{ t('admin.oidc.displayName') }}</Label>
            <Input id="displayName" name="displayName" v-model="displayName" />
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
              <label class="flex items-center gap-2 text-sm text-ink">
                <Checkbox :model-value="scopes.includes('offline_access')" @update:model-value="(c) => toggleScope('offline_access', c === true)" />
                offline_access
              </label>
            </div>
            <p class="text-xs text-muted">{{ t('admin.oidc.scopesNote') }}</p>
          </div>
          <SettingRow :label="t('admin.oidc.requireConsent')" :description="t('admin.oidc.requireConsentDesc')" for="requireConsent">
            <Switch id="requireConsent" v-model="requireConsent" />
          </SettingRow>
          <div class="flex items-center gap-3">
            <Button type="button" :disabled="busy" data-test="save" @click="save">{{ t('admin.oidc.save') }}</Button>
            <span v-if="saved" class="text-sm text-sage" role="status">{{ t('admin.oidc.saved') }}</span>
          </div>
        </CardContent>
      </Card>

      <!-- Danger zone: sensitive operations (disable, rotate secret, delete) grouped together. -->
      <Card class="border-destructive/30 bg-destructive/[0.02]">
        <CardHeader><CardTitle class="text-destructive">{{ t('admin.oidc.dangerTitle') }}</CardTitle></CardHeader>
        <CardContent class="flex flex-col gap-4">
          <SettingRow :label="t('admin.oidc.disabled')" :description="t('admin.oidc.disabledDesc')" for="disabled">
            <Switch id="disabled" v-model="disabled" data-test="disabled" />
          </SettingRow>
          <p class="text-xs text-muted">{{ t('admin.oidc.statusSavedHint') }}</p>

          <Separator />
          <template v-if="client.tokenEndpointAuthMethod !== 'none'">
            <p class="text-sm text-muted">{{ t('admin.oidc.rotateConfirmBody') }}</p>
            <template v-if="rotatedSecret">
              <p class="text-sm text-sage" role="status">{{ t('admin.oidc.secretReveal') }}</p>
              <CodeField :value="rotatedSecret" />
            </template>
            <Button type="button" variant="outline" class="w-fit" :disabled="busy" data-test="rotate" @click="confirmRotate = true">{{ t('admin.oidc.rotate') }}</Button>
          </template>
          <p v-else class="text-sm text-muted">{{ t('admin.oidc.publicClient') }}</p>

          <Separator />
          <p class="text-sm text-muted">{{ t('admin.oidc.deleteHelp') }}</p>
          <Button type="button" variant="destructive" class="w-fit" :disabled="busy" data-test="delete" @click="confirmDelete = true">{{ t('admin.oidc.delete') }}</Button>
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
