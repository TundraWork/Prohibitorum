<script setup lang="ts">
/**
 * AdminOidcClientDetailView (/admin/oidc-applications/:clientId) — per-client admin actions.
 * Edit config (PUT with allowedScopes); rotate secret (reveal-once CodeField);
 * delete. All mutations go through withSudo.
 */
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { useTransientFlag } from '@/composables/useTransientFlag'
import { withSudo } from '@/lib/sudo'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Switch } from '@/components/ui/switch'
import OidcScopePicker from '@/components/custom/OidcScopePicker.vue'
import { Separator } from '@/components/ui/separator'
import ConfirmDialog from '@/components/custom/ConfirmDialog.vue'
import CodeField from '@/components/custom/CodeField.vue'
import ListInput from '@/components/custom/ListInput.vue'
import SettingRow from '@/components/custom/SettingRow.vue'
import StatusBadge from '@/components/custom/StatusBadge.vue'
import CardSkeleton from '@/components/custom/CardSkeleton.vue'
import BackLink from '@/components/custom/BackLink.vue'

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
const route = useRoute()
const router = useRouter()
const { busy, error, run, errorText } = useApi()

const clientId = String(route.params.clientId)
const client = ref<OidcApplication | null>(null)
const notFound = ref(false)

const displayName = ref('')
const redirectUris = ref<string[]>([])
const postLogoutUris = ref<string[]>([])
const scopes = ref<string[]>(['openid'])
const requireConsent = ref(false)
const disabled = ref(false)
const { flag: saved, trigger: triggerSaved } = useTransientFlag()

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

async function load(): Promise<void> {
  const c = await run(() => api.get<OidcApplication>(`/api/prohibitorum/oidc-applications/${clientId}`))
  if (!c) { if (error.value?.code === 'client_not_found') notFound.value = true; return }
  client.value = c
  displayName.value = c.displayName
  redirectUris.value = [...c.redirectUris]
  postLogoutUris.value = [...c.postLogoutRedirectUris]
  // openid is mandatory; defend against a payload that somehow lacks it so the
  // (disabled) checkbox can't strand the form in an openid-less state.
  scopes.value = c.allowedScopes.includes('openid') ? [...c.allowedScopes] : ['openid', ...c.allowedScopes]
  requireConsent.value = c.requireConsent
  disabled.value = c.disabled
}

async function save(): Promise<void> {
  rotatedSecret.value = ''
  const updated = await run(() => withSudo(() => api.put<OidcApplication>(`/api/prohibitorum/oidc-applications/${clientId}`, {
    displayName: displayName.value,
    redirectUris: redirectUris.value,
    postLogoutRedirectUris: postLogoutUris.value,
    allowedScopes: scopes.value,
    requireConsent: requireConsent.value,
    disabled: disabled.value,
  }), t('sudo.reason.saveChanges')))
  if (updated) { client.value = updated; triggerSaved() }
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
    return true as const
  }, t('sudo.reason.deleteApp')))
  confirmDelete.value = false
  if (ok) router.push('/admin/oidc-applications')
}

onMounted(load)
</script>
<template>
  <div class="flex max-w-2xl flex-col gap-6">
    <BackLink to="/admin/oidc-applications" :label="t('admin.oidc.back')" />
    <Alert v-if="errorText && !notFound" variant="destructive" role="alert" aria-live="polite"><AlertDescription>{{ errorText }}</AlertDescription></Alert>
    <p v-if="notFound" class="text-sm text-muted" role="status">{{ t('admin.oidc.notFound') }}</p>

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
            <OidcScopePicker v-model="scopes" />
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
          <div class="flex flex-col gap-2">
            <div class="flex items-center gap-2">
              <h4 class="text-sm font-medium text-ink">{{ t('admin.oidc.statusLabel') }}</h4>
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
            <h4 class="text-sm font-medium text-ink">{{ t('admin.oidc.rotateTitle') }}</h4>
            <template v-if="client.tokenEndpointAuthMethod !== 'none'">
              <p class="text-xs text-muted">{{ t('admin.oidc.rotateConfirmBody') }}</p>
              <template v-if="rotatedSecret">
                <p class="text-sm text-sage" role="status">{{ t('admin.oidc.secretReveal') }}</p>
                <CodeField :value="rotatedSecret" />
              </template>
              <Button type="button" variant="outline" class="w-fit" :disabled="busy" data-test="rotate" @click="confirmRotate = true">{{ t('admin.oidc.rotate') }}</Button>
            </template>
            <p v-else class="text-xs text-muted">{{ t('admin.oidc.publicClient') }}</p>
          </div>

          <Separator />
          <div class="flex flex-col gap-2">
            <h4 class="text-sm font-medium text-ink">{{ t('admin.oidc.deleteTitle') }}</h4>
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
