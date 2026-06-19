<script setup lang="ts">
/** AdminAuditView (/admin/audit) — filterable, keyset-paginated audit log. */
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { AUDIT_FACTORS, AUDIT_EVENTS } from '@/lib/audit'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Select, SelectTrigger, SelectContent, SelectItem, SelectValue } from '@/components/ui/select'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/table'
import { formatDateTime } from '@/lib/time'
import TableSkeleton from '@/components/custom/TableSkeleton.vue'
import EmptyState from '@/components/custom/EmptyState.vue'
import { FileX, X } from 'lucide-vue-next'

interface AuditEvent {
  id: number; at: string; accountId?: number; factor: string; event: string
  ip?: string; userAgent?: string; detail?: Record<string, unknown>
}
const LIMIT = 50

type Preset = '15m' | '1h' | '24h' | '7d' | 'custom' | 'all'

const { t } = useI18n()
const { busy, run, errorText } = useApi()

const rows = ref<AuditEvent[]>([])
const hasMore = ref(false)
const expanded = ref<Record<number, boolean>>({})

// Enumerated filters
const factor = ref('')
const event = ref('')
const accountId = ref('')

// Time-range preset + custom inputs
const preset = ref<Preset>('24h')
const since = ref('')
const until = ref('')

// Pagination cursor stack: pageCursors[i] = the `before` param to fetch page i.
// Page 0 uses undefined (no before = newest).
const pageCursors = ref<(number | undefined)[]>([undefined])
const pageIndex = ref(0)

// Derive since/until from preset (at query time)
function presetSince(): string {
  const now = Date.now()
  const deltas: Record<Preset, number | null> = {
    '15m': 15 * 60 * 1000,
    '1h': 60 * 60 * 1000,
    '24h': 24 * 60 * 60 * 1000,
    '7d': 7 * 24 * 60 * 60 * 1000,
    custom: null,
    all: null,
  }
  const delta = deltas[preset.value]
  if (delta === null) return preset.value === 'custom' ? since.value : ''
  return new Date(now - delta).toISOString()
}

function buildQuery(before?: number): string {
  const p = new URLSearchParams()
  if (factor.value) p.set('factor', factor.value)
  if (event.value) p.set('event', event.value)
  if (accountId.value.trim()) p.set('accountId', accountId.value.trim())
  const s = presetSince()
  if (s) p.set('since', s)
  if (preset.value === 'custom' && until.value) p.set('until', new Date(until.value).toISOString())
  if (before !== undefined) p.set('before', String(before))
  p.set('limit', String(LIMIT))
  return `/api/prohibitorum/audit-events?${p.toString()}`
}

async function fetchPage(before?: number): Promise<AuditEvent[] | undefined> {
  return run(() => api.get<AuditEvent[]>(buildQuery(before)))
}

async function reload(): Promise<void> {
  expanded.value = {}
  hasMore.value = false
  pageCursors.value = [undefined]
  pageIndex.value = 0
  const res = await fetchPage()
  if (res) { rows.value = res; hasMore.value = res.length === LIMIT }
}

async function goNext(): Promise<void> {
  const last = rows.value.at(-1)
  if (!last) return
  const nextBefore = last.id
  const newIndex = pageIndex.value + 1
  pageCursors.value = [...pageCursors.value.slice(0, pageIndex.value + 1), nextBefore]
  pageIndex.value = newIndex
  expanded.value = {}
  const res = await fetchPage(nextBefore)
  if (res) { rows.value = res; hasMore.value = res.length === LIMIT }
}

async function goPrev(): Promise<void> {
  if (pageIndex.value <= 0) return
  pageIndex.value--
  expanded.value = {}
  const before = pageCursors.value[pageIndex.value]
  const res = await fetchPage(before)
  if (res) { rows.value = res; hasMore.value = res.length === LIMIT }
}

function applyPreset(p: Preset): void {
  preset.value = p
  void reload()
}

function clearFilters(): void {
  factor.value = ''
  event.value = ''
  accountId.value = ''
  preset.value = '24h'
  since.value = ''
  until.value = ''
  void reload()
}

function toggle(id: number): void { expanded.value = { ...expanded.value, [id]: !expanded.value[id] } }
function detailText(e: AuditEvent): string {
  return e.detail && Object.keys(e.detail).length ? JSON.stringify(e.detail, null, 2) : '—'
}

// Active filter pills
interface ActiveFilter { key: string; label: string; clear: () => void }
const activeFilters = computed<ActiveFilter[]>(() => {
  const pills: ActiveFilter[] = []
  if (factor.value) pills.push({ key: 'factor', label: `Factor: ${factor.value}`, clear: () => { factor.value = ''; void reload() } })
  if (event.value) pills.push({ key: 'event', label: `Event: ${event.value}`, clear: () => { event.value = ''; void reload() } })
  if (accountId.value.trim()) pills.push({ key: 'accountId', label: `Account: ${accountId.value.trim()}`, clear: () => { accountId.value = ''; void reload() } })
  if (preset.value !== 'all' && preset.value !== 'custom') {
    const labels: Record<Preset, string> = { '15m': t('admin.audit.preset15m'), '1h': t('admin.audit.preset1h'), '24h': t('admin.audit.preset24h'), '7d': t('admin.audit.preset7d'), custom: '', all: '' }
    pills.push({ key: 'preset', label: labels[preset.value], clear: () => { preset.value = 'all'; void reload() } })
  }
  if (preset.value === 'custom' && since.value) pills.push({ key: 'since', label: `Since: ${since.value}`, clear: () => { since.value = ''; void reload() } })
  if (preset.value === 'custom' && until.value) pills.push({ key: 'until', label: `Until: ${until.value}`, clear: () => { until.value = ''; void reload() } })
  return pills
})

const presets: { value: Preset; labelKey: string }[] = [
  { value: '15m', labelKey: 'admin.audit.preset15m' },
  { value: '1h', labelKey: 'admin.audit.preset1h' },
  { value: '24h', labelKey: 'admin.audit.preset24h' },
  { value: '7d', labelKey: 'admin.audit.preset7d' },
  { value: 'all', labelKey: 'admin.audit.presetAll' },
  { value: 'custom', labelKey: 'admin.audit.presetCustom' },
]

onMounted(reload)
</script>
<template>
  <div class="flex max-w-5xl flex-col gap-6">
    <h1 class="text-2xl font-semibold tracking-tight text-ink">{{ t('admin.audit.title') }}</h1>
    <Alert v-if="errorText" variant="destructive" role="alert" aria-live="polite"><AlertDescription>{{ errorText }}</AlertDescription></Alert>

    <!-- Time-range preset pills -->
    <div class="flex flex-wrap gap-1.5" role="group" :aria-label="t('admin.audit.filterSince')">
      <button
        v-for="p in presets"
        :key="p.value"
        type="button"
        :data-test="`preset-${p.value}`"
        :aria-pressed="preset === p.value"
        :class="[
          'inline-flex items-center rounded-full px-3 py-1 text-sm font-medium transition-colors outline-none cursor-pointer',
          'focus-visible:ring-ring/50 focus-visible:ring-[3px]',
          preset === p.value
            ? 'bg-primary text-primary-foreground'
            : 'bg-sunken text-muted hover:text-ink hover:bg-sunken/80',
        ]"
        @click="applyPreset(p.value)"
      >{{ t(p.labelKey) }}</button>
    </div>

    <!-- Custom datetime inputs (only shown when preset=custom) -->
    <div v-if="preset === 'custom'" class="grid grid-cols-1 gap-3 sm:grid-cols-2">
      <div class="flex flex-col gap-1.5">
        <Label for="since">{{ t('admin.audit.filterSince') }}</Label>
        <Input id="since" name="since" type="datetime-local" v-model="since" />
      </div>
      <div class="flex flex-col gap-1.5">
        <Label for="until">{{ t('admin.audit.filterUntil') }}</Label>
        <Input id="until" name="until" type="datetime-local" v-model="until" />
      </div>
    </div>

    <!-- Enumerated + text filters -->
    <div class="grid grid-cols-1 gap-3 sm:grid-cols-3">
      <div class="flex flex-col gap-1.5">
        <Label for="factor-select">{{ t('admin.audit.filterFactor') }}</Label>
        <Select :model-value="factor" @update:model-value="(v) => { factor = v === '__any__' ? '' : v }">
          <SelectTrigger id="factor-select" data-test="factor-select" :aria-label="t('admin.audit.filterFactor')">
            <SelectValue :placeholder="t('admin.audit.filterAny')" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="__any__">{{ t('admin.audit.filterAny') }}</SelectItem>
            <SelectItem v-for="f in AUDIT_FACTORS" :key="f" :value="f">{{ f }}</SelectItem>
          </SelectContent>
        </Select>
      </div>
      <div class="flex flex-col gap-1.5">
        <Label for="event-select">{{ t('admin.audit.filterEvent') }}</Label>
        <Select :model-value="event" @update:model-value="(v) => { event = v === '__any__' ? '' : v }">
          <SelectTrigger id="event-select" data-test="event-select" :aria-label="t('admin.audit.filterEvent')">
            <SelectValue :placeholder="t('admin.audit.filterAny')" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="__any__">{{ t('admin.audit.filterAny') }}</SelectItem>
            <SelectItem v-for="ev in AUDIT_EVENTS" :key="ev" :value="ev">{{ ev }}</SelectItem>
          </SelectContent>
        </Select>
      </div>
      <div class="flex flex-col gap-1.5">
        <Label for="accountId">{{ t('admin.audit.filterAccount') }}</Label>
        <Input id="accountId" name="accountId" v-model="accountId" inputmode="numeric" autocomplete="off" />
      </div>
    </div>

    <div class="flex gap-2">
      <Button type="button" :disabled="busy" data-test="apply" @click="reload">{{ t('admin.audit.apply') }}</Button>
      <Button type="button" variant="outline" :disabled="busy" data-test="clear" @click="clearFilters">{{ t('admin.audit.clear') }}</Button>
    </div>

    <!-- Active filter pills -->
    <div v-if="activeFilters.length" class="flex flex-wrap gap-1.5" :aria-label="t('admin.audit.activeFilters')">
      <span
        v-for="f in activeFilters"
        :key="f.key"
        class="inline-flex items-center gap-1 rounded-full bg-sunken px-2.5 py-0.5 text-xs font-medium text-ink"
        :data-test="`filter-pill-${f.key}`"
      >
        {{ f.label }}
        <button
          type="button"
          class="ml-0.5 inline-flex size-6 items-center justify-center rounded-full cursor-pointer text-muted hover:text-ink focus-visible:outline-none focus-visible:ring-ring/50 focus-visible:ring-[3px]"
          :aria-label="t('admin.audit.clearFilter') + ': ' + f.label"
          :data-test="`filter-pill-${f.key}-clear`"
          @click="f.clear()"
        >
          <X class="h-3 w-3" />
        </button>
      </span>
    </div>

    <TableSkeleton v-if="busy && !rows.length" :rows="5" :cols="5" />
    <Table v-else-if="rows.length">
      <TableHeader>
        <TableRow>
          <TableHead>{{ t('admin.audit.colTime') }}</TableHead>
          <TableHead>{{ t('admin.audit.colFactor') }}</TableHead>
          <TableHead>{{ t('admin.audit.colEvent') }}</TableHead>
          <TableHead>{{ t('admin.audit.colAccount') }}</TableHead>
          <TableHead>{{ t('admin.audit.colIp') }}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <template v-for="e in rows" :key="e.id">
          <!-- Row click is a mouse convenience; the real disclosure control is the
               button in the first cell (a <tr> can't carry role=button without losing
               its row semantics). -->
          <TableRow class="cursor-pointer" @click="toggle(e.id)">
            <TableCell class="text-sm text-muted">
              <button
                type="button"
                :data-test="`expand-${e.id}`"
                :aria-expanded="!!expanded[e.id]"
                :aria-label="t('admin.audit.expand')"
                class="flex items-center gap-1 text-left rounded-sm cursor-pointer focus-visible:outline-none focus-visible:ring-ring/50 focus-visible:ring-[3px]"
                @click.stop="toggle(e.id)"
              >{{ formatDateTime(e.at) }}</button>
            </TableCell>
            <TableCell class="text-sm text-ink">{{ e.factor }}</TableCell>
            <TableCell class="text-sm text-ink">{{ e.event }}</TableCell>
            <TableCell class="text-sm text-muted">{{ e.accountId ?? '—' }}</TableCell>
            <TableCell class="text-sm text-muted">{{ e.ip || '—' }}</TableCell>
          </TableRow>
          <TableRow v-if="expanded[e.id]">
            <TableCell colspan="5">
              <span class="text-xs text-muted">{{ t('admin.audit.detail') }}</span>
              <pre class="mt-1 max-h-[40vh] overflow-auto whitespace-pre-wrap break-all rounded-md bg-sunken p-3 font-mono text-xs text-ink">{{ detailText(e) }}</pre>
              <p v-if="e.userAgent" class="mt-2 text-xs text-muted">{{ t('admin.audit.userAgent') }}: {{ e.userAgent }}</p>
            </TableCell>
          </TableRow>
        </template>
      </TableBody>
    </Table>
    <EmptyState v-else-if="!errorText" :icon="FileX" :title="t('admin.audit.empty')" />

    <!-- Prev / Next pagination -->
    <div class="flex items-center gap-3">
      <Button
        v-if="pageIndex > 0"
        type="button"
        variant="outline"
        :disabled="busy"
        data-test="prev-page"
        @click="goPrev"
      >{{ t('admin.audit.prevPage') }}</Button>
      <span v-if="rows.length" class="text-sm text-muted" data-test="page-indicator">{{ t('admin.audit.pageIndicator', { n: pageIndex + 1 }) }}</span>
      <Button
        v-if="hasMore"
        type="button"
        variant="outline"
        :disabled="busy"
        data-test="next-page"
        @click="goNext"
      >{{ t('admin.audit.nextPage') }}</Button>
    </div>
  </div>
</template>
