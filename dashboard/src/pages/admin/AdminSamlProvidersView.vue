<script setup lang="ts">
/** AdminSamlProvidersView (/admin/saml-applications) — table of SAML SPs; inline create (metadata XML or manual ACS). */
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { useTransientFlag } from '@/composables/useTransientFlag'
import { withSudo } from '@/lib/sudo'
import { Card, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/table'
import { Textarea } from '@/components/ui/textarea'
import { Switch } from '@/components/ui/switch'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import StatusBadge from '@/components/custom/StatusBadge.vue'
import SegmentedControl from '@/components/custom/SegmentedControl.vue'
import TableSkeleton from '@/components/custom/TableSkeleton.vue'
import SettingRow from '@/components/custom/SettingRow.vue'
import EmptyState from '@/components/custom/EmptyState.vue'
import { Building2 } from 'lucide-vue-next'

const POST_URN = 'urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST'
const REDIRECT_URN = 'urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect'
let acsRowId = 0

interface SamlApplication {
  id: number
  entityId: string
  displayName: string
  nameIdFormat: string
  requireSignedAuthnRequest: boolean
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

const { t } = useI18n()
const router = useRouter()
const { busy, run, errorText } = useApi()

const rows = ref<SamlApplication[]>([])
const createOpen = ref(false)
const { flag: created, trigger: triggerCreated } = useTransientFlag()

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
const allowIdpInitiated = ref(false)

async function load(): Promise<void> {
  const res = await run(() => api.get<SamlApplication[]>('/api/prohibitorum/saml-applications'))
  if (res) rows.value = res
}

function go(id: number): void { router.push(`/admin/saml-applications/${id}`) }

function openCreate(): void {
  mode.value = 'metadata'
  metadataXml.value = ''
  metadataDisplayName.value = ''
  displayName.value = ''
  entityId.value = ''
  nameIdFormat.value = ''
  acsRows.value = []
  requireSignedAuthnRequest.value = false
  allowIdpInitiated.value = false
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

// RadioGroup binds to the default row's id (as a string); selecting one marks
// it as the sole default ACS endpoint. The emit type is reka's AcceptableValue,
// so accept it broadly and normalize to the row-id string.
const defaultAcsKey = computed(() => {
  const d = acsRows.value.find(r => r.isDefault)
  return d ? String(d.id) : ''
})
function onDefaultChange(val: unknown): void {
  const key = val == null ? '' : String(val)
  acsRows.value.forEach(r => { r.isDefault = String(r.id) === key })
}

function removeAcsRow(i: number): void {
  acsRows.value = acsRows.value.filter((_, idx) => idx !== i)
  acsRows.value.forEach((r, j) => { r.index = j })
  if (acsRows.value.length > 0 && !acsRows.value.some(r => r.isDefault)) {
    acsRows.value[0].isDefault = true
  }
}

async function create(): Promise<void> {
  let body: Record<string, unknown>
  if (mode.value === 'metadata') {
    body = {
      metadataXml: metadataXml.value,
      displayName: metadataDisplayName.value || undefined,
      requireSignedAuthnRequest: requireSignedAuthnRequest.value,
      allowIdpInitiated: allowIdpInitiated.value,
    }
  } else {
    body = {
      displayName: displayName.value,
      entityId: entityId.value,
      nameIdFormat: nameIdFormat.value || undefined,
      requireSignedAuthnRequest: requireSignedAuthnRequest.value,
      allowIdpInitiated: allowIdpInitiated.value,
      acs: acsRows.value.map(({ id: _id, ...rest }) => rest),
    }
  }
  const res = await run(() => withSudo(() => api.post('/api/prohibitorum/saml-applications', body)))
  if (res) {
    createOpen.value = false
    triggerCreated()
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
      <CardContent class="flex flex-col gap-4 py-4">
        <!-- Mode toggle (segmented control) -->
        <Tabs v-model="mode" class="gap-4">
          <TabsList class="w-full max-w-xs">
            <TabsTrigger value="metadata" data-test="mode-metadata">{{ t('admin.saml.modeMetadata') }}</TabsTrigger>
            <TabsTrigger value="manual" data-test="mode-manual">{{ t('admin.saml.modeManual') }}</TabsTrigger>
          </TabsList>

          <!-- Metadata mode -->
          <TabsContent value="metadata" class="flex flex-col gap-3">
            <div class="flex flex-col gap-1.5">
              <Label for="metadataXml">{{ t('admin.saml.metadataXml') }}</Label>
              <Textarea id="metadataXml" name="metadataXml" v-model="metadataXml" :placeholder="t('admin.saml.metadataHint')" />
            </div>
            <div class="flex flex-col gap-1.5">
              <Label for="metadataDisplayName">{{ t('admin.saml.displayName') }}</Label>
              <Input id="metadataDisplayName" name="metadataDisplayName" v-model="metadataDisplayName" />
            </div>
          </TabsContent>

          <!-- Manual mode -->
          <TabsContent value="manual" class="flex flex-col gap-3">
            <div class="flex flex-col gap-1.5">
              <Label for="displayName">{{ t('admin.saml.displayName') }}</Label>
              <Input id="displayName" name="displayName" v-model="displayName" />
            </div>
            <div class="flex flex-col gap-1.5">
              <Label for="entityId">{{ t('admin.saml.entityId') }}</Label>
              <Input id="entityId" name="entityId" v-model="entityId" />
              <p class="text-xs text-muted">{{ t('admin.saml.entityIdDesc') }}</p>
            </div>
            <div class="flex flex-col gap-1.5">
              <Label for="nameIdFormat">{{ t('admin.saml.nameIdFormat') }}</Label>
              <Input id="nameIdFormat" name="nameIdFormat" v-model="nameIdFormat" />
              <p class="text-xs text-muted">{{ t('admin.saml.nameIdFormatDesc') }}</p>
            </div>

            <!-- ACS rows -->
            <div class="flex flex-col gap-2">
              <span class="text-sm font-medium text-ink">{{ t('admin.saml.acs') }}</span>
              <RadioGroup :model-value="defaultAcsKey" class="gap-2" @update:model-value="onDefaultChange">
                <div v-for="(row, i) in acsRows" :key="row.id" class="flex flex-wrap items-end gap-3 rounded-md border p-2">
                  <div class="flex flex-col gap-1">
                    <Label class="text-xs text-muted">{{ t('admin.saml.acsBinding') }}</Label>
                    <SegmentedControl v-model="row.binding" size="sm" :aria-label="t('admin.saml.acsBinding')"
                      :options="[{value:POST_URN,label:t('admin.saml.bindingPost')},{value:REDIRECT_URN,label:t('admin.saml.bindingRedirect')}]" />
                  </div>
                  <div class="flex min-w-0 flex-1 flex-col gap-1">
                    <Label :for="`acs-location-${i}`" class="text-xs text-muted">{{ t('admin.saml.acsLocation') }}</Label>
                    <Input :id="`acs-location-${i}`" :name="`acs-location-${i}`" v-model="row.location" />
                  </div>
                  <div class="flex w-16 flex-col gap-1">
                    <Label :for="`acs-index-${i}`" class="text-xs text-muted">{{ t('admin.saml.acsIndex') }}</Label>
                    <Input :id="`acs-index-${i}`" :name="`acs-index-${i}`" type="number" v-model.number="row.index" />
                  </div>
                  <div class="flex flex-col items-center gap-1">
                    <Label :for="`acs-default-${i}`" class="text-xs text-muted">{{ t('admin.saml.acsDefault') }}</Label>
                    <RadioGroupItem :id="`acs-default-${i}`" :value="String(row.id)" :data-test="`acs-default-${i}`" class="my-2" />
                  </div>
                  <Button type="button" variant="outline" size="sm" :data-test="`acs-remove-${i}`" @click="removeAcsRow(i)">{{ t('admin.saml.acsRemove') }}</Button>
                </div>
              </RadioGroup>
              <Button type="button" variant="outline" size="sm" data-test="acs-add" @click="addAcsRow">{{ t('admin.saml.acsAdd') }}</Button>
            </div>
          </TabsContent>
        </Tabs>

        <!-- Shared flags -->
        <div class="flex flex-col gap-3 border-t pt-3">
          <span class="text-sm font-medium text-ink">{{ t('admin.saml.securityTitle') }}</span>
          <SettingRow :label="t('admin.saml.requireSignedAuthn')" :description="t('admin.saml.requireSignedAuthnDesc')" for="requireSignedAuthnRequest">
            <Switch id="requireSignedAuthnRequest" v-model="requireSignedAuthnRequest" />
          </SettingRow>
          <SettingRow :label="t('admin.saml.allowIdpInitiated')" :description="t('admin.saml.allowIdpInitiatedDesc')" for="allowIdpInitiated">
            <Switch id="allowIdpInitiated" v-model="allowIdpInitiated" />
          </SettingRow>
        </div>

        <div class="flex gap-2">
          <Button type="button" :disabled="busy" data-test="create-confirm" @click="create">{{ t('admin.saml.create') }}</Button>
          <Button type="button" variant="outline" :disabled="busy" data-test="create-cancel" @click="createOpen = false">{{ t('common.cancel') }}</Button>
        </div>
      </CardContent>
    </Card>

    <TableSkeleton v-if="busy && !rows.length" :rows="5" :cols="3" />
    <Table v-else-if="rows.length">
      <TableHeader>
        <TableRow>
          <TableHead>{{ t('admin.saml.colName') }} · {{ t('admin.saml.colEntity') }}</TableHead>
          <TableHead>{{ t('admin.saml.colIdpInit') }}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <TableRow v-for="sp in rows" :key="sp.id" class="cursor-pointer" tabindex="0"
                  :data-test="`sp-row-${sp.id}`"
                  @click="go(sp.id)" @keydown.enter="go(sp.id)" @keydown.space.prevent="go(sp.id)">
          <TableCell>
            <div class="flex min-w-0 flex-col">
              <span class="truncate font-medium text-ink">{{ sp.displayName }}</span>
              <span class="truncate font-mono text-muted">{{ sp.entityId }}</span>
            </div>
          </TableCell>
          <TableCell>
            <StatusBadge :variant="sp.allowIdpInitiated ? 'success' : 'neutral'">
              {{ sp.allowIdpInitiated ? t('admin.saml.yes') : t('admin.saml.no') }}
            </StatusBadge>
          </TableCell>
        </TableRow>
      </TableBody>
    </Table>
    <EmptyState v-else-if="!errorText && !createOpen" :icon="Building2" :title="t('admin.saml.empty')">
      <Button type="button" variant="outline" @click="openCreate">{{ t('admin.saml.create') }}</Button>
    </EmptyState>
  </div>
</template>
