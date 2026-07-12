<script setup lang="ts">
/** AdminSigningKeysView (/admin/signing-keys) — list + lifecycle (generate/activate/retire). */
import { onMounted, ref, computed } from 'vue'
import { I18nT, useI18n } from 'vue-i18n'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { withSudo } from '@/lib/sudo'
import { Button } from '@/components/ui/button'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/table'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import StatusBadge from '@/components/custom/StatusBadge.vue'
import ConfirmDialog from '@/components/custom/ConfirmDialog.vue'
import TableSkeleton from '@/components/custom/TableSkeleton.vue'
import EmptyState from '@/components/custom/EmptyState.vue'
import ErrorPanel from '@/components/custom/ErrorPanel.vue'
import { formatDateTime } from '@/lib/time'
import { KeyRound } from 'lucide-vue-next'

interface SigningKey {
  kid: string; algorithm: string; use: string; status: string
  publicJwk: Record<string, unknown>
  notBefore?: string; activatedAt?: string; decommissionedAt?: string; retireAfter?: string
}
type Variant = 'neutral' | 'success' | 'caution' | 'danger' | 'info'

const { t } = useI18n()
const { busy, run, error, clear } = useApi()

const rows = ref<SigningKey[]>([])
const viewJwkKid = ref<string | null>(null)
const confirmGenerate = ref(false)
const confirmActivate = ref('')
const confirmRetire = ref('')

const viewJwkKey = computed(() => rows.value.find((k) => k.kid === viewJwkKid.value) ?? null)
const viewJwkOpen = computed({
  get: () => viewJwkKid.value !== null,
  set: (v: boolean) => { if (!v) viewJwkKid.value = null },
})

const STATUS_VARIANT: Record<string, Variant> = { pending: 'neutral', active: 'success', decommissioning: 'caution', retired: 'neutral' }
function statusVariant(s: string): Variant { return STATUS_VARIANT[s] ?? 'neutral' }
function statusLabel(s: string): string {
  if (s === 'active') return t('admin.signingKeys.statusActive')
  if (s === 'pending') return t('admin.signingKeys.statusPending')
  if (s === 'decommissioning') return t('admin.signingKeys.statusDecommissioning')
  if (s === 'retired') return t('admin.signingKeys.statusRetired')
  return s
}
function jwk(k: SigningKey): string { return JSON.stringify(k.publicJwk, null, 2) }

async function load(): Promise<void> {
  const res = await run(() => api.get<SigningKey[]>('/api/prohibitorum/signing-keys'))
  if (res) rows.value = res
}
async function generate(): Promise<void> {
  const ok = await run(() => withSudo(async () => { await api.post('/api/prohibitorum/signing-keys/generate'); return true as const }))
  confirmGenerate.value = false
  if (ok) await load()
}
async function activate(kid: string): Promise<void> {
  const ok = await run(() => withSudo(async () => { await api.post(`/api/prohibitorum/signing-keys/${kid}/activate`); return true as const }))
  confirmActivate.value = ''
  if (ok) await load()
}
async function retire(kid: string): Promise<void> {
  const ok = await run(() => withSudo(async () => { await api.post(`/api/prohibitorum/signing-keys/${kid}/retire`); return true as const }))
  confirmRetire.value = ''
  if (ok) await load()
}

function closeGenerate(v: boolean): void { if (!v) confirmGenerate.value = false }
function closeActivate(v: boolean): void { if (!v) confirmActivate.value = '' }
function closeRetire(v: boolean): void { if (!v) confirmRetire.value = '' }

onMounted(load)
</script>
<template>
  <div class="flex max-w-4xl flex-col gap-6">
    <div class="flex items-center justify-between gap-4">
      <div class="flex flex-col gap-1">
        <h1 class="text-2xl font-semibold tracking-tight text-ink">{{ t('admin.signingKeys.title') }}</h1>
        <p class="text-xs text-muted">{{ t('admin.signingKeys.lifecycleHelp') }}</p>
      </div>
      <Button type="button" data-test="generate" @click="confirmGenerate = true">{{ t('admin.signingKeys.generate') }}</Button>
    </div>
    <ErrorPanel :error="error" @dismiss="clear" :is-admin="true" />

    <TableSkeleton v-if="busy && !rows.length" :rows="5" :cols="5" />
    <Table v-else-if="rows.length">
      <TableHeader>
        <TableRow>
          <TableHead>{{ t('admin.signingKeys.colKid') }}</TableHead>
          <TableHead>{{ t('admin.signingKeys.colAlg') }}</TableHead>
          <TableHead>{{ t('admin.signingKeys.colStatus') }}</TableHead>
          <TableHead>{{ t('admin.signingKeys.colActivated') }}</TableHead>
          <TableHead></TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <TableRow v-for="k in rows" :key="k.kid">
          <TableCell class="max-w-[18rem] truncate font-mono text-sm" :title="k.kid">{{ k.kid }}</TableCell>
          <TableCell class="text-sm text-muted">{{ k.algorithm }} · {{ k.use }}</TableCell>
          <TableCell><StatusBadge :variant="statusVariant(k.status)">{{ statusLabel(k.status) }}</StatusBadge></TableCell>
          <TableCell class="text-sm text-muted">{{ formatDateTime(k.activatedAt) }}</TableCell>
          <TableCell>
            <div class="flex justify-end gap-2">
              <Button type="button" variant="ghost" size="sm" :data-test="`view-jwk-${k.kid}`" :aria-label="t('admin.signingKeys.viewJwk')" @click="viewJwkKid = k.kid">{{ t('admin.signingKeys.viewJwk') }}</Button>
              <Button v-if="k.status === 'pending'" type="button" variant="outline" size="sm" :disabled="busy" :data-test="`activate-${k.kid}`" @click="confirmActivate = k.kid">{{ t('admin.signingKeys.activate') }}</Button>
              <Button v-if="k.status === 'decommissioning'" type="button" variant="outline" size="sm" :disabled="busy" :data-test="`retire-${k.kid}`" @click="confirmRetire = k.kid">{{ t('admin.signingKeys.retire') }}</Button>
            </div>
          </TableCell>
        </TableRow>
      </TableBody>
    </Table>
    <EmptyState v-else-if="!error" :icon="KeyRound" :title="t('admin.signingKeys.empty')" />

    <Dialog v-model:open="viewJwkOpen">
      <DialogContent class="max-w-2xl">
        <DialogHeader>
          <DialogTitle>{{ t('admin.signingKeys.jwkTitle') }}</DialogTitle>
          <DialogDescription v-if="viewJwkKey" class="text-xs text-muted">
            <I18nT keypath="admin.signingKeys.jwkDescription" tag="span">
              <template #kid>
                <span class="break-all font-mono text-ink" data-test="jwk-description-kid">{{ viewJwkKey.kid }}</span>
              </template>
            </I18nT>
          </DialogDescription>
        </DialogHeader>
        <div v-if="viewJwkKey" class="max-h-[60vh] overflow-auto rounded-md bg-sunken p-3">
          <pre class="whitespace-pre-wrap break-all font-mono text-xs text-ink">{{ jwk(viewJwkKey) }}</pre>
        </div>
      </DialogContent>
    </Dialog>

    <ConfirmDialog :open="confirmGenerate" :title="t('admin.signingKeys.generateTitle')" :confirm-label="t('admin.signingKeys.generateConfirm')" :busy="busy"
      @update:open="closeGenerate" @cancel="confirmGenerate = false" @confirm="generate">
      {{ t('admin.signingKeys.generateBody') }}
    </ConfirmDialog>
    <ConfirmDialog :open="!!confirmActivate" :title="t('admin.signingKeys.activateTitle')" :confirm-label="t('admin.signingKeys.activateConfirm')" :busy="busy"
      @update:open="closeActivate" @cancel="confirmActivate = ''" @confirm="activate(confirmActivate)">
      {{ t('admin.signingKeys.activateBody') }}
    </ConfirmDialog>
    <ConfirmDialog :open="!!confirmRetire" :title="t('admin.signingKeys.retireTitle')" :confirm-label="t('admin.signingKeys.retireConfirm')" :busy="busy"
      @update:open="closeRetire" @cancel="confirmRetire = ''" @confirm="retire(confirmRetire)">
      {{ t('admin.signingKeys.retireBody') }}
    </ConfirmDialog>
  </div>
</template>
