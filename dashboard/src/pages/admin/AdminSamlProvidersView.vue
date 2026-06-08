<script setup lang="ts">
/** AdminSamlProvidersView (/admin/saml-providers) — table of SAML SPs; inline create (metadata XML or manual ACS). */
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
import StatusBadge from '@/components/custom/StatusBadge.vue'

const POST_URN = 'urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST'
const REDIRECT_URN = 'urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect'
let acsRowId = 0

interface SamlProvider {
  id: number
  entityId: string
  displayName: string
  nameIdFormat: string
  requireSignedAuthnRequest: boolean
  wantAssertionsSigned: boolean
  allowIdpInitiated: boolean
  createdAt: string
}

interface AcsRow {
  id: number
  binding: string
  location: string
  index: number
  isDefault: boolean
}

const { t, te } = useI18n()
const router = useRouter()
const { busy, error, run } = useApi()

const rows = ref<SamlProvider[]>([])
const createOpen = ref(false)
const created = ref(false)

// Mode toggle
const mode = ref<'metadata' | 'manual'>('metadata')

// Metadata mode form state
const metadataXml = ref('')
const metadataDisplayName = ref('')

// Manual mode form state
const displayName = ref('')
const entityId = ref('')
const nameIdFormat = ref('')
const acsRows = ref<AcsRow[]>([])

// Shared flags
const requireSignedAuthnRequest = ref(false)
const wantAssertionsSigned = ref(false)
const allowIdpInitiated = ref(false)

const errorText = computed(() => {
  const e = error.value
  if (!e) return ''
  const key = `errors.${e.code}`
  return te(key) ? t(key) : e.message || t('common.error')
})

async function load(): Promise<void> {
  const res = await run(() => api.get<SamlProvider[]>('/api/prohibitorum/saml-providers'))
  if (res) rows.value = res
}

function go(id: number): void { router.push(`/admin/saml-providers/${id}`) }

function openCreate(): void {
  mode.value = 'metadata'
  metadataXml.value = ''
  metadataDisplayName.value = ''
  displayName.value = ''
  entityId.value = ''
  nameIdFormat.value = ''
  acsRows.value = []
  requireSignedAuthnRequest.value = false
  wantAssertionsSigned.value = false
  allowIdpInitiated.value = false
  created.value = false
  createOpen.value = true
}

function addAcsRow(): void {
  acsRows.value.push({
    id: acsRowId++,
    binding: POST_URN,
    location: '',
    index: acsRows.value.length,
    isDefault: acsRows.value.length === 0,
  })
}

function setDefault(i: number): void {
  acsRows.value.forEach((r, j) => { r.isDefault = j === i })
}

function removeAcsRow(i: number): void {
  acsRows.value = acsRows.value.filter((_, idx) => idx !== i)
  acsRows.value.forEach((r, j) => { r.index = j })
  if (acsRows.value.length > 0 && !acsRows.value.some(r => r.isDefault)) {
    acsRows.value[0].isDefault = true
  }
}

async function create(): Promise<void> {
  created.value = false
  let body: Record<string, unknown>
  if (mode.value === 'metadata') {
    body = {
      metadataXml: metadataXml.value,
      displayName: metadataDisplayName.value || undefined,
      requireSignedAuthnRequest: requireSignedAuthnRequest.value,
      wantAssertionsSigned: wantAssertionsSigned.value,
      allowIdpInitiated: allowIdpInitiated.value,
    }
  } else {
    body = {
      displayName: displayName.value,
      entityId: entityId.value,
      nameIdFormat: nameIdFormat.value || undefined,
      requireSignedAuthnRequest: requireSignedAuthnRequest.value,
      wantAssertionsSigned: wantAssertionsSigned.value,
      allowIdpInitiated: allowIdpInitiated.value,
      acs: acsRows.value.map(({ id: _id, ...rest }) => rest),
    }
  }
  const res = await run(() => withSudo(() => api.post('/api/prohibitorum/saml-providers', body)))
  if (res) {
    createOpen.value = false
    created.value = true
    await load()
  }
}

onMounted(load)
</script>
<template>
  <div class="flex max-w-4xl flex-col gap-6">
    <div class="flex items-center justify-between gap-4">
      <h1 class="text-2xl font-semibold tracking-tight text-ink">{{ t('admin.saml.title') }}</h1>
      <Button type="button" data-test="create" @click="openCreate">{{ t('admin.saml.create') }}</Button>
    </div>
    <Alert v-if="errorText" variant="destructive" role="alert" aria-live="polite"><AlertDescription>{{ errorText }}</AlertDescription></Alert>
    <p v-if="created" class="text-sm text-sage" role="status">{{ t('admin.saml.created') }}</p>

    <Card v-if="createOpen">
      <CardContent class="flex flex-col gap-3 py-4">
        <!-- Mode toggle -->
        <div class="flex gap-2">
          <Button type="button" :variant="mode === 'metadata' ? 'default' : 'outline'" data-test="mode-metadata" @click="mode = 'metadata'">{{ t('admin.saml.modeMetadata') }}</Button>
          <Button type="button" :variant="mode === 'manual' ? 'default' : 'outline'" data-test="mode-manual" @click="mode = 'manual'">{{ t('admin.saml.modeManual') }}</Button>
        </div>

        <!-- Metadata mode -->
        <template v-if="mode === 'metadata'">
          <div class="flex flex-col gap-1.5">
            <Label for="metadataXml">{{ t('admin.saml.metadataXml') }}</Label>
            <Textarea id="metadataXml" name="metadataXml" v-model="metadataXml" :placeholder="t('admin.saml.metadataHint')" />
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="metadataDisplayName">{{ t('admin.saml.displayName') }}</Label>
            <Input id="metadataDisplayName" name="metadataDisplayName" v-model="metadataDisplayName" />
          </div>
        </template>

        <!-- Manual mode -->
        <template v-else>
          <div class="flex flex-col gap-1.5">
            <Label for="displayName">{{ t('admin.saml.displayName') }}</Label>
            <Input id="displayName" name="displayName" v-model="displayName" />
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="entityId">{{ t('admin.saml.entityId') }}</Label>
            <Input id="entityId" name="entityId" v-model="entityId" />
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="nameIdFormat">{{ t('admin.saml.nameIdFormat') }}</Label>
            <Input id="nameIdFormat" name="nameIdFormat" v-model="nameIdFormat" />
          </div>

          <!-- ACS rows -->
          <div class="flex flex-col gap-2">
            <span class="text-sm font-medium text-ink">{{ t('admin.saml.acs') }}</span>
            <div v-for="(row, i) in acsRows" :key="row.id" class="flex flex-wrap items-end gap-2 rounded-md border p-2">
              <div class="flex flex-col gap-1">
                <label :for="`acs-binding-${i}`" class="text-xs text-muted">{{ t('admin.saml.acsBinding') }}</label>
                <select :id="`acs-binding-${i}`" v-model="row.binding" class="bg-sunken border-input h-9 rounded-md border px-2 text-sm text-ink">
                  <option :value="POST_URN">{{ t('admin.saml.bindingPost') }}</option>
                  <option :value="REDIRECT_URN">{{ t('admin.saml.bindingRedirect') }}</option>
                </select>
              </div>
              <div class="flex min-w-0 flex-1 flex-col gap-1">
                <label :for="`acs-location-${i}`" class="text-xs text-muted">{{ t('admin.saml.acsLocation') }}</label>
                <Input :id="`acs-location-${i}`" :name="`acs-location-${i}`" v-model="row.location" />
              </div>
              <div class="flex w-16 flex-col gap-1">
                <label :for="`acs-index-${i}`" class="text-xs text-muted">{{ t('admin.saml.acsIndex') }}</label>
                <Input :id="`acs-index-${i}`" :name="`acs-index-${i}`" type="number" v-model.number="row.index" />
              </div>
              <div class="flex flex-col gap-1">
                <span class="text-xs text-muted">{{ t('admin.saml.acsDefault') }}</span>
                <label class="flex items-center gap-1.5 text-sm text-ink">
                  <input type="checkbox" :checked="row.isDefault" @change="setDefault(i)" />
                </label>
              </div>
              <Button type="button" variant="outline" size="sm" :data-test="`acs-remove-${i}`" @click="removeAcsRow(i)">{{ t('admin.saml.acsRemove') }}</Button>
            </div>
            <Button type="button" variant="outline" size="sm" data-test="acs-add" @click="addAcsRow">{{ t('admin.saml.acsAdd') }}</Button>
          </div>
        </template>

        <!-- Shared flags -->
        <label class="flex items-center gap-2 text-sm text-ink">
          <input type="checkbox" v-model="requireSignedAuthnRequest" />
          {{ t('admin.saml.requireSignedAuthn') }}
        </label>
        <label class="flex items-center gap-2 text-sm text-ink">
          <input type="checkbox" v-model="wantAssertionsSigned" />
          {{ t('admin.saml.wantAssertionsSigned') }}
        </label>
        <label class="flex items-center gap-2 text-sm text-ink">
          <input type="checkbox" v-model="allowIdpInitiated" />
          {{ t('admin.saml.allowIdpInitiated') }}
        </label>

        <div class="flex gap-2">
          <Button type="button" :disabled="busy" data-test="create-confirm" @click="create">{{ t('admin.saml.create') }}</Button>
          <Button type="button" variant="outline" :disabled="busy" data-test="create-cancel" @click="createOpen = false">{{ t('common.cancel') }}</Button>
        </div>
      </CardContent>
    </Card>

    <Table v-if="rows.length">
      <TableHeader>
        <TableRow>
          <TableHead>{{ t('admin.saml.colEntity') }}</TableHead>
          <TableHead>{{ t('admin.saml.colName') }}</TableHead>
          <TableHead>{{ t('admin.saml.colIdpInit') }}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <TableRow v-for="sp in rows" :key="sp.id" class="cursor-pointer" tabindex="0"
                  :data-test="`sp-row-${sp.id}`"
                  @click="go(sp.id)" @keydown.enter="go(sp.id)" @keydown.space.prevent="go(sp.id)">
          <TableCell class="font-medium text-ink">{{ sp.entityId }}</TableCell>
          <TableCell class="text-ink">{{ sp.displayName }}</TableCell>
          <TableCell>
            <StatusBadge :variant="sp.allowIdpInitiated ? 'success' : 'neutral'">
              {{ sp.allowIdpInitiated ? t('admin.saml.yes') : t('admin.saml.no') }}
            </StatusBadge>
          </TableCell>
        </TableRow>
      </TableBody>
    </Table>
    <p v-else-if="!busy && !errorText && !createOpen" class="text-sm text-muted">{{ t('admin.saml.empty') }}</p>
  </div>
</template>
