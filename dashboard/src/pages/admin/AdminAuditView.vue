<script setup lang="ts">
/** AdminAuditView (/admin/audit) — filterable, keyset-paginated audit log. */
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/table'
import { formatDateTime } from '@/lib/time'
import TableSkeleton from '@/components/custom/TableSkeleton.vue'

interface AuditEvent {
  id: number; at: string; accountId?: number; factor: string; event: string
  ip?: string; userAgent?: string; detail?: Record<string, unknown>
}
const LIMIT = 50

const { t } = useI18n()
const { busy, run, errorText } = useApi()

const rows = ref<AuditEvent[]>([])
const hasMore = ref(false)
const expanded = ref<Record<number, boolean>>({})

const factor = ref(''); const event = ref(''); const accountId = ref('')
const since = ref(''); const until = ref('')

function buildQuery(before?: number): string {
  const p = new URLSearchParams()
  if (factor.value.trim()) p.set('factor', factor.value.trim())
  if (event.value.trim()) p.set('event', event.value.trim())
  if (accountId.value.trim()) p.set('accountId', accountId.value.trim())
  if (since.value) p.set('since', new Date(since.value).toISOString())
  if (until.value) p.set('until', new Date(until.value).toISOString())
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
  const res = await fetchPage()
  if (res) { rows.value = res; hasMore.value = res.length === LIMIT }
}
async function loadMore(): Promise<void> {
  const last = rows.value.at(-1)
  if (!last) return
  const res = await fetchPage(last.id)
  if (res) { rows.value = [...rows.value, ...res]; hasMore.value = res.length === LIMIT }
}
function clearFilters(): void {
  factor.value = ''; event.value = ''; accountId.value = ''; since.value = ''; until.value = ''
  void reload()
}
function toggle(id: number): void { expanded.value = { ...expanded.value, [id]: !expanded.value[id] } }
function detailText(e: AuditEvent): string {
  return e.detail && Object.keys(e.detail).length ? JSON.stringify(e.detail, null, 2) : '—'
}

onMounted(reload)
</script>
<template>
  <div class="flex max-w-5xl flex-col gap-6">
    <h1 class="text-2xl font-semibold tracking-tight text-ink">{{ t('admin.audit.title') }}</h1>
    <Alert v-if="errorText" variant="destructive" role="alert" aria-live="polite"><AlertDescription>{{ errorText }}</AlertDescription></Alert>

    <div class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-5">
      <div class="flex flex-col gap-1.5">
        <Label for="factor">{{ t('admin.audit.filterFactor') }}</Label>
        <Input id="factor" name="factor" v-model="factor" autocomplete="off" />
      </div>
      <div class="flex flex-col gap-1.5">
        <Label for="event">{{ t('admin.audit.filterEvent') }}</Label>
        <Input id="event" name="event" v-model="event" autocomplete="off" />
      </div>
      <div class="flex flex-col gap-1.5">
        <Label for="accountId">{{ t('admin.audit.filterAccount') }}</Label>
        <Input id="accountId" name="accountId" v-model="accountId" inputmode="numeric" autocomplete="off" />
      </div>
      <div class="flex flex-col gap-1.5">
        <Label for="since">{{ t('admin.audit.filterSince') }}</Label>
        <Input id="since" name="since" type="datetime-local" v-model="since" />
      </div>
      <div class="flex flex-col gap-1.5">
        <Label for="until">{{ t('admin.audit.filterUntil') }}</Label>
        <Input id="until" name="until" type="datetime-local" v-model="until" />
      </div>
    </div>
    <div class="flex gap-2">
      <Button type="button" :disabled="busy" data-test="apply" @click="reload">{{ t('admin.audit.apply') }}</Button>
      <Button type="button" variant="outline" :disabled="busy" data-test="clear" @click="clearFilters">{{ t('admin.audit.clear') }}</Button>
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
          <TableRow class="cursor-pointer" tabindex="0" :data-test="`expand-${e.id}`"
                    role="button" :aria-expanded="!!expanded[e.id]" :aria-label="t('admin.audit.expand')"
                    @click="toggle(e.id)" @keydown.enter="toggle(e.id)" @keydown.space.prevent="toggle(e.id)">
            <TableCell class="text-sm text-muted">{{ formatDateTime(e.at) }}</TableCell>
            <TableCell class="text-sm text-ink">{{ e.factor }}</TableCell>
            <TableCell class="text-sm text-ink">{{ e.event }}</TableCell>
            <TableCell class="text-sm text-muted">{{ e.accountId ?? '—' }}</TableCell>
            <TableCell class="text-sm text-muted">{{ e.ip || '—' }}</TableCell>
          </TableRow>
          <TableRow v-if="expanded[e.id]">
            <TableCell colspan="5">
              <span class="text-xs text-muted">{{ t('admin.audit.detail') }}</span>
              <pre class="mt-1 overflow-x-auto rounded-md bg-sunken p-3 font-mono text-xs text-ink">{{ detailText(e) }}</pre>
              <p v-if="e.userAgent" class="mt-2 text-xs text-muted">{{ t('admin.audit.userAgent') }}: {{ e.userAgent }}</p>
            </TableCell>
          </TableRow>
        </template>
      </TableBody>
    </Table>
    <p v-else-if="!errorText" class="text-sm text-muted">{{ t('admin.audit.empty') }}</p>

    <Button v-if="hasMore" type="button" variant="outline" class="w-fit" :disabled="busy" data-test="load-more" @click="loadMore">{{ t('admin.audit.loadMore') }}</Button>
  </div>
</template>
