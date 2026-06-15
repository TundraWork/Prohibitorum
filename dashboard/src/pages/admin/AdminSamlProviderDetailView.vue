<script setup lang="ts">
/**
 * AdminSamlProviderDetailView (/admin/saml-applications/:id) — per-SP admin actions.
 * Edit flags (PUT); re-ingest metadata; view ACS endpoints and signing certificates
 * (read-only); delete. All mutations go through withSudo.
 */
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { useTransientFlag } from '@/composables/useTransientFlag'
import { withSudo } from '@/lib/sudo'
import { formatDateTime } from '@/lib/time'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Switch } from '@/components/ui/switch'
import { Separator } from '@/components/ui/separator'
import ConfirmDialog from '@/components/custom/ConfirmDialog.vue'
import SettingRow from '@/components/custom/SettingRow.vue'
import CardSkeleton from '@/components/custom/CardSkeleton.vue'
import StatusBadge from '@/components/custom/StatusBadge.vue'
import BackLink from '@/components/custom/BackLink.vue'

interface AcsEndpoint {
  binding: string
  location: string
  index: number
  isDefault: boolean
}

interface SamlKey {
  use: string
  notAfter?: string
}

interface SamlApplication {
  id: number
  entityId: string
  displayName: string
  nameIdFormat: string
  attributeMap: unknown
  requireSignedAuthnRequest: boolean
  allowIdpInitiated: boolean
  disabled: boolean
  sessionLifetimeSecs?: number
  acs: AcsEndpoint[]
  keys: SamlKey[]
  createdAt: string
}

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const { busy, error, run, errorText } = useApi()

const id = Number(route.params.id)
const sp = ref<SamlApplication | null>(null)
const notFound = ref(false)
const localError = ref('')

const displayName = ref('')
const nameIdFormat = ref('')
const attributeMapText = ref('[]')
const attributeMapError = ref('')
const requireSignedAuthnRequest = ref(false)
const allowIdpInitiated = ref(false)
const disabled = ref(false)
const sessionLifetimeSecs = ref('')
const { flag: saved, trigger: triggerSaved } = useTransientFlag()

const reingestXml = ref('')
const reingestDone = ref(false)

const confirmDelete = ref(false)

function seedForm(data: SamlApplication): void {
  displayName.value = data.displayName
  nameIdFormat.value = data.nameIdFormat
  attributeMapText.value = JSON.stringify(data.attributeMap ?? [], null, 2)
  attributeMapError.value = ''
  requireSignedAuthnRequest.value = data.requireSignedAuthnRequest
  allowIdpInitiated.value = data.allowIdpInitiated
  disabled.value = data.disabled
  sessionLifetimeSecs.value = data.sessionLifetimeSecs != null ? String(data.sessionLifetimeSecs) : ''
}

async function load(): Promise<void> {
  const data = await run(() => api.get<SamlApplication>(`/api/prohibitorum/saml-applications/${id}`))
  if (!data) { if (error.value?.code === 'credential_not_found') notFound.value = true; return }
  sp.value = data
  seedForm(data)
}

async function save(): Promise<void> {
  localError.value = ''
  attributeMapError.value = ''
  reingestDone.value = false
  const secs = sessionLifetimeSecs.value.trim()
  if (secs !== '' && !/^\d+$/.test(secs)) { localError.value = t('admin.saml.sessionLifetimeInvalid'); return }
  let parsedAttributeMap: unknown
  try {
    parsedAttributeMap = JSON.parse(attributeMapText.value)
  } catch {
    attributeMapError.value = t('admin.saml.attributeMapInvalid')
    return
  }
  const updated = await run(() => withSudo(() => api.put<SamlApplication>(`/api/prohibitorum/saml-applications/${id}`, {
    displayName: displayName.value,
    nameIdFormat: nameIdFormat.value,
    attributeMap: parsedAttributeMap,
    requireSignedAuthnRequest: requireSignedAuthnRequest.value,
    allowIdpInitiated: allowIdpInitiated.value,
    ...(secs !== '' ? { sessionLifetimeSecs: Number(secs) } : {}),
  }), t('sudo.reason.saveChanges')))
  if (updated) { sp.value = updated; seedForm(updated); triggerSaved() }
}

async function reingest(): Promise<void> {
  localError.value = ''
  reingestDone.value = false
  const res = await run(() => withSudo(() =>
    api.post<SamlApplication>(`/api/prohibitorum/saml-applications/${id}/reingest-metadata`, { metadataXml: reingestXml.value }),
    t('sudo.reason.saveChanges')))
  if (res) { sp.value = res; seedForm(res); reingestDone.value = true }
}

// Flip the disabled flag on its own (independent of the config Save), via the
// dedicated set-disabled endpoint.
async function toggleDisabled(): Promise<void> {
  localError.value = ''
  reingestDone.value = false
  const next = !disabled.value
  const updated = await run(() => withSudo(() =>
    api.post<SamlApplication>('/api/prohibitorum/saml-applications/set-disabled', { id, disabled: next }),
    t('sudo.reason.disableApp')))
  if (updated) { sp.value = updated; disabled.value = updated.disabled }
}

async function destroy(): Promise<void> {
  localError.value = ''
  reingestDone.value = false
  const ok = await run(() => withSudo(async () => {
    await api.post('/api/prohibitorum/saml-applications/delete', { id })
    return true as const
  }, t('sudo.reason.deleteApp')))
  confirmDelete.value = false
  if (ok) router.push('/admin/saml-applications')
}

function bindingLabel(b: string): string {
  if (b.endsWith('HTTP-POST')) return t('admin.saml.bindingPost')
  if (b.endsWith('HTTP-Redirect')) return t('admin.saml.bindingRedirect')
  return b
}

onMounted(load)
</script>
<template>
  <div class="flex max-w-2xl flex-col gap-6">
    <BackLink to="/admin/saml-applications" :label="t('admin.saml.back')" />
    <Alert v-if="errorText && !notFound" variant="destructive" role="alert" aria-live="polite"><AlertDescription>{{ errorText }}</AlertDescription></Alert>
    <Alert v-if="localError" variant="destructive" role="alert" aria-live="polite"><AlertDescription>{{ localError }}</AlertDescription></Alert>
    <p v-if="notFound" class="text-sm text-muted" role="status">{{ t('admin.saml.notFound') }}</p>

    <CardSkeleton v-else-if="busy && !sp" />

    <template v-else-if="sp">
      <h1 class="text-2xl font-semibold tracking-tight text-ink">{{ sp.displayName }}</h1>

      <!-- Config card -->
      <Card>
        <CardHeader><CardTitle>{{ t('admin.saml.configTitle') }}</CardTitle></CardHeader>
        <CardContent class="flex flex-col gap-4">
          <div class="flex flex-col gap-1.5">
            <Label>{{ t('admin.saml.entityId') }}</Label>
            <p class="font-mono text-sm text-muted">{{ sp.entityId }}</p>
            <p class="text-xs text-muted">{{ t('admin.saml.entityIdDesc') }}</p>
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="displayName">{{ t('admin.saml.displayName') }}</Label>
            <Input id="displayName" name="displayName" v-model="displayName" />
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="nameIdFormat">{{ t('admin.saml.nameIdFormat') }}</Label>
            <Input id="nameIdFormat" name="nameIdFormat" v-model="nameIdFormat" />
            <p class="text-xs text-muted">{{ t('admin.saml.nameIdFormatDesc') }}</p>
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="attributeMap">{{ t('admin.saml.attributeMap') }}</Label>
            <p class="text-xs text-muted">{{ t('admin.saml.attributeMapPurpose') }}</p>
            <Textarea id="attributeMap" name="attributeMap" v-model="attributeMapText" :rows="8" data-test="saml-attributeMap" />
            <p class="text-xs text-muted">{{ t('admin.saml.attributeMapHint') }}</p>
            <p v-if="attributeMapError" class="text-xs text-destructive" data-test="saml-attributeMap-error">{{ attributeMapError }}</p>
          </div>
          <div class="flex flex-col gap-3">
            <SettingRow :label="t('admin.saml.requireSignedAuthn')" :description="t('admin.saml.requireSignedAuthnDesc')" for="requireSignedAuthnRequest">
              <Switch id="requireSignedAuthnRequest" v-model="requireSignedAuthnRequest" />
            </SettingRow>
            <SettingRow :label="t('admin.saml.allowIdpInitiated')" :description="t('admin.saml.allowIdpInitiatedDesc')" for="allowIdpInitiated">
              <Switch id="allowIdpInitiated" v-model="allowIdpInitiated" />
            </SettingRow>
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="sessionLifetimeSecs">{{ t('admin.saml.sessionLifetime') }}</Label>
            <Input id="sessionLifetimeSecs" name="sessionLifetimeSecs" v-model="sessionLifetimeSecs" inputmode="numeric" />
            <p class="text-xs text-muted">{{ t('admin.saml.sessionLifetimeDesc') }}</p>
          </div>
          <div class="flex items-center gap-3">
            <Button type="button" :disabled="busy" data-test="save" @click="save">{{ t('admin.saml.save') }}</Button>
            <span v-if="saved" class="text-sm text-sage" role="status">{{ t('admin.saml.saved') }}</span>
          </div>
        </CardContent>
      </Card>

      <!-- ACS card -->
      <Card>
        <CardHeader><CardTitle>{{ t('admin.saml.acsTitle') }}</CardTitle></CardHeader>
        <CardContent class="flex flex-col gap-3">
          <p v-if="!sp.acs.length" class="text-sm text-muted">—</p>
          <div v-for="(row, i) in sp.acs" :key="i" class="flex min-w-0 flex-col gap-0.5 text-sm">
            <span class="break-all font-mono text-ink">{{ row.location }}</span>
            <span class="text-muted">{{ bindingLabel(row.binding) }} · index {{ row.index }}<template v-if="row.isDefault"> · default</template></span>
          </div>
        </CardContent>
      </Card>

      <!-- Certificates card -->
      <Card>
        <CardHeader><CardTitle>{{ t('admin.saml.keysTitle') }}</CardTitle></CardHeader>
        <CardContent class="flex flex-col gap-3">
          <p v-if="!sp.keys.length" class="text-sm text-muted">—</p>
          <div v-for="(key, i) in sp.keys" :key="i" class="flex flex-col gap-0.5 text-sm">
            <span class="text-ink">{{ key.use }}</span>
            <span class="text-muted">{{ t('admin.saml.certExpires') }} {{ formatDateTime(key.notAfter) }}</span>
          </div>
        </CardContent>
      </Card>

      <!-- Re-ingest metadata card -->
      <Card>
        <CardHeader><CardTitle>{{ t('admin.saml.reingest') }}</CardTitle></CardHeader>
        <CardContent class="flex flex-col gap-3">
          <Label for="reingestXml">{{ t('admin.saml.metadataXml') }}</Label>
          <Textarea id="reingestXml" name="reingestXml" v-model="reingestXml" :placeholder="t('admin.saml.metadataHint')" />
          <div class="flex items-center gap-3">
            <Button type="button" :disabled="busy || !reingestXml" data-test="reingest" @click="reingest">{{ t('admin.saml.reingest') }}</Button>
            <span v-if="reingestDone" class="text-sm text-sage" role="status">{{ t('admin.saml.reingestDone') }}</span>
          </div>
        </CardContent>
      </Card>

      <!-- Danger zone card -->
      <Card class="border-destructive/30 bg-destructive/[0.02]">
        <CardHeader><CardTitle class="text-destructive">{{ t('admin.saml.dangerTitle') }}</CardTitle></CardHeader>
        <CardContent class="flex flex-col gap-4">
          <div class="flex flex-col gap-2">
            <div class="flex items-center gap-2">
              <span class="text-sm font-medium text-ink">{{ t('admin.saml.statusLabel') }}</span>
              <StatusBadge :variant="disabled ? 'danger' : 'success'" data-test="status-badge">
                {{ disabled ? t('admin.saml.disabled') : t('admin.saml.active') }}
              </StatusBadge>
            </div>
            <p class="text-xs text-muted">{{ t('admin.saml.disabledDesc') }}</p>
            <Button type="button" variant="outline" class="w-fit" :disabled="busy" data-test="disable-toggle" @click="toggleDisabled">
              {{ disabled ? t('admin.saml.enable') : t('admin.saml.disable') }}
            </Button>
          </div>

          <Separator />
          <p class="text-sm text-muted">{{ t('admin.saml.deleteHelp') }}</p>
          <Button type="button" variant="destructive" class="w-fit" :disabled="busy" data-test="delete" @click="confirmDelete = true">{{ t('admin.saml.delete') }}</Button>
        </CardContent>
      </Card>
    </template>

    <ConfirmDialog :open="confirmDelete" :title="t('admin.saml.deleteConfirmTitle')" :confirm-label="t('admin.saml.delete')" :busy="busy"
      @update:open="(v) => { if (!v) confirmDelete = false }" @cancel="confirmDelete = false" @confirm="destroy">
      {{ t('admin.saml.deleteConfirmBody') }}
    </ConfirmDialog>
  </div>
</template>
