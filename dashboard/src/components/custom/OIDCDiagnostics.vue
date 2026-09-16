<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api, type ApiError } from '@/lib/api'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import ErrorPanel from '@/components/custom/ErrorPanel.vue'

const props = defineProps<{ slug: string; dirty: boolean }>()
const { t } = useI18n()
interface EffectiveConfig { mode: string; fetchedAt: string; callbackUrl: string; fields: Record<string, { value: unknown; source: string }> }
interface Stage { name: string; status: string; durationMs?: number; endpoint?: string; httpStatus?: number; errorCode?: string; requestId?: string }
interface TestResult { status: string; expiresAt: string; stages: Stage[]; claims?: Record<string, unknown> }
const effective = ref<EffectiveConfig | null>(null)
const stale = ref(false)
const busy = ref(false)
const error = ref<ApiError | null>(null)
const result = ref<TestResult | null>(null)
const testId = ref('')
const base = '/api/prohibitorum/identity-providers/' + encodeURIComponent(props.slug)
let disposed = false
let timer: ReturnType<typeof setTimeout> | undefined
let polls = 0
function text(value: unknown): string { return Array.isArray(value) ? value.join(', ') : value == null || value === '' ? '—' : String(value) }
function diagnosticLabel(group: string, value: string): string { const key = 'admin.upstream.diagnostics.' + group + '.' + value; return t(key) }
async function refresh(): Promise<void> {
  busy.value = true; error.value = null
  try { const value = await api.get<EffectiveConfig>(base + '/effective-config'); if (!disposed) { effective.value = value; stale.value = false } }
  catch (e) { if (!disposed) { stale.value = effective.value !== null; error.value = e as ApiError } }
  finally { if (!disposed) busy.value = false }
}
async function start(): Promise<void> {
  if (props.dirty) return
  busy.value = true; error.value = null
  try {
    const value = await api.post<{ id: string; authorizationUrl: string }>(base + '/tests')
    if (!disposed) window.location.assign(value.authorizationUrl)
  } catch (e) { if (!disposed) error.value = e as ApiError }
  finally { if (!disposed) busy.value = false }
}
function schedule(): void {
  if (disposed || result.value?.status !== 'running' || polls >= 30) return
  timer = setTimeout(() => { polls++; void readResult(false) }, 1000)
}
async function readResult(completeIfReady: boolean): Promise<void> {
  if (!testId.value || disposed) return
  if (timer) clearTimeout(timer)
  const id = testId.value
  const current = () => !disposed && testId.value === id
  busy.value = true
  try {
    const path = base + '/tests/' + encodeURIComponent(id)
    let value = await api.get<TestResult>(path)
    if (!current()) return
    if (completeIfReady && value.status === 'ready') value = await api.post<TestResult>(path + '/complete')
    if (current()) { result.value = value; error.value = null; schedule() }
  } catch (e) {
    if (current()) {
      error.value = e as ApiError
      if (completeIfReady) { timer = setTimeout(() => { void readResult(false) }, 1000) }
    }
  }
  finally { if (current()) busy.value = false }
}
function close(): void {
  if (timer) clearTimeout(timer)
  result.value = null; testId.value = ''; error.value = null; busy.value = false
  const url = new URL(window.location.href); url.searchParams.delete('test'); window.history.replaceState(window.history.state, '', url)
}
onMounted(() => {
  const id = new URLSearchParams(window.location.search).get('test')
  if (id && /^[A-Za-z0-9_-]{43}$/.test(id)) { testId.value = id; void readResult(true) }
})
onBeforeUnmount(() => { disposed = true; if (timer) clearTimeout(timer) })
</script>
<template>
  <Card data-test="oidc-diagnostics">
    <CardHeader><CardTitle>{{ t('admin.upstream.diagnostics.title') }}</CardTitle></CardHeader>
    <CardContent class="flex min-w-0 flex-col gap-4">
      <p class="text-sm text-muted">{{ t('admin.upstream.diagnostics.savedConfig') }}</p>
      <p v-if="dirty" role="status" class="text-sm text-muted">{{ t('admin.upstream.diagnostics.saveFirst') }}</p>
      <ErrorPanel v-if="error" :error="error" :is-admin="true" @dismiss="error = null" />
      <div v-if="error?.details?.stage === 'discovery'" class="text-sm" data-test="discovery-error">
        <p class="text-destructive">{{ diagnosticLabel('stages', 'discovery') }} · {{ diagnosticLabel('errors', 'discovery_failed') }}</p>
        <p class="break-all text-xs text-muted">{{ error.details.endpoint }} · {{ error.details.durationMs }} ms</p>
      </div>
      <div class="flex flex-wrap gap-2">
        <Button type="button" variant="outline" :disabled="busy" data-test="effective-refresh" @click="refresh">{{ t('admin.upstream.diagnostics.refresh') }}</Button>
        <Button type="button" :disabled="busy || dirty || !effective" data-test="oidc-test-start" @click="start">{{ t('admin.upstream.diagnostics.test') }}</Button>
      </div>
      <template v-if="effective">
        <p v-if="stale" role="status" class="text-sm text-destructive">{{ t('admin.upstream.diagnostics.stale') }}</p>
        <p class="text-xs text-muted">{{ t('admin.upstream.diagnostics.fetchedAt') }} {{ effective.fetchedAt }}</p>
        <p v-if="effective.mode === 'manual'" class="text-sm text-muted">{{ t('admin.upstream.diagnostics.noDiscovery') }}</p>
        <dl class="flex min-w-0 flex-col gap-3" data-test="effective-fields">
          <div v-for="(field, key) in effective.fields" :key="key" class="min-w-0">
            <dt class="text-sm font-medium">{{ diagnosticLabel('fields', String(key)) }} <span class="font-normal text-muted">({{ diagnosticLabel('sources', field.source) }})</span></dt>
            <dd class="break-all text-sm text-ink">{{ text(field.value) }}</dd>
          </div>
        </dl>
        <p class="text-sm text-muted">{{ t('admin.upstream.diagnostics.registerCallback') }}</p>
        <code class="break-all text-xs" data-test="test-callback">{{ effective.callbackUrl }}</code>
      </template>
      <template v-if="result">
        <p role="status" class="font-medium" data-test="test-status">{{ diagnosticLabel('statuses', result.status) }}</p>
        <ol class="flex min-w-0 flex-col gap-3">
          <li v-for="(stage, index) in result.stages" :key="index" class="min-w-0 rounded-md border border-line p-3">
            <p class="text-sm font-medium">{{ diagnosticLabel('stages', stage.name) }} · {{ diagnosticLabel('statuses', stage.status) }}</p>
            <p v-if="stage.endpoint" class="break-all text-xs text-muted">{{ stage.endpoint }}</p>
            <p v-if="stage.durationMs !== undefined || stage.httpStatus" class="text-xs text-muted">{{ stage.durationMs ?? 0 }} ms <span v-if="stage.httpStatus"> · HTTP {{ stage.httpStatus }}</span></p>
            <p v-if="stage.errorCode" class="text-sm text-destructive">{{ diagnosticLabel('errors', stage.errorCode) }}</p>
            <p v-if="stage.requestId" class="break-all text-xs text-muted">{{ t('admin.upstream.diagnostics.requestId') }} {{ stage.requestId }}</p>
          </li>
        </ol>
        <dl v-if="result.claims" class="flex min-w-0 flex-col gap-2">
          <div v-for="(value, key) in result.claims" :key="key"><dt class="text-sm font-medium">{{ key }}</dt><dd class="break-all text-sm">{{ text(value) }}</dd></div>
        </dl>
        <p class="text-xs text-muted">{{ t('admin.upstream.diagnostics.expiresAt') }} {{ result.expiresAt }}</p>
        <div class="flex flex-wrap gap-2">
          <Button v-if="result.status === 'running'" type="button" variant="outline" :disabled="busy" @click="readResult(false)">{{ t('admin.upstream.diagnostics.refreshResult') }}</Button>
          <Button type="button" variant="outline" @click="close">{{ t('common.close') }}</Button>
        </div>
      </template>
    </CardContent>
  </Card>
</template>
