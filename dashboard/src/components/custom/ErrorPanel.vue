<script setup lang="ts">
/**
 * ErrorPanel — persistent, code-driven, localized error display.
 *
 * Replaces every inline `<Alert v-if="errorText">{{ errorText }}</Alert>` block.
 * The panel renders the localized message for the error's code (or the
 * unknown fallback), a collapsible details disclosure showing curated detail
 * fields and the copyable request ID, optional recovery guidance (an action
 * button only when a @recovery listener is attached), and an admin-only
 * diagnostic lookup that fetches and renders the curated diagnostic record
 * from /api/prohibitorum/diagnostics/{requestId}.
 *
 * Key contract guarantees:
 * - role="alert" so assistive tech announces it assertively.
 * - NEVER auto-dismisses. The panel persists until the user clicks dismiss
 *   (emits `dismiss`) or a successful retry clears the error (useApi clears
 *   on success).
 * - The requestId is hidden in the collapsed summary and only shown in the
 *   expanded details disclosure.
 * - Non-admin users never see the diagnostic action.
 * - Admin diagnostic fetch is self-contained: the ErrorPanel fetches the
 *   record, shows loading/error states, and renders it persistently in an
 *   accessible region. A sequence guard prevents stale fetches (dismissed,
 *   unmounted, or changed-error) from writing state.
 * - Recovery guidance text is always shown when a recovery hint exists, but
 *   the action button only renders when the parent wires @recovery.
 */
import { ref, computed, watch, onBeforeUnmount, getCurrentInstance, useId } from 'vue'
import { useI18n } from 'vue-i18n'
import { X, ChevronDown, Copy, Stethoscope, RotateCcw, LogIn } from 'lucide-vue-next'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { api } from '@/lib/api'
import type { ApiError } from '@/lib/errors'
import { errorTranslationKey, recoveryLabelKey, localizedDetailEntries } from '@/lib/errors'
import { codeDefinition, GLOBAL_ERROR_CODES } from '@/lib/errorCodes'

interface DiagnosticRecord {
  requestId: string
  code: string
  operation: string
  method: string
  route: string
  retryable: boolean
  fields?: Record<string, unknown>
  occurredAt: string
  expiresAt: string
}

const props = withDefaults(defineProps<{
  error: ApiError | null
  isAdmin?: boolean
  dismissible?: boolean
}>(), {
  isAdmin: false,
  dismissible: true,
})

const emit = defineEmits<{
  (e: 'dismiss'): void
  (e: 'recovery'): void
}>()

const { t, te } = useI18n()
const detailsOpen = ref(false)
const detailsContentId = `error-details-${useId()}`
const copied = ref(false)

// --- diagnostic fetch state ---
const diagState = ref<'idle' | 'loading' | 'loaded' | 'error'>('idle')
const diagRecord = ref<DiagnosticRecord | null>(null)

// M1: sequence guard — each fetch gets a unique token; only the latest token
// may write state. The error watcher bumps diagSeq when the error changes or
// is cleared; onBeforeUnmount bumps it when the panel is removed. Together
// these cover every path where a stale fetch result must be discarded.
let diagSeq = 0
watch(() => props.error, () => {
  diagSeq++
  diagState.value = 'idle'
  diagRecord.value = null
})
onBeforeUnmount(() => {
  diagSeq++
})

const hasError = computed(() => props.error !== null)

const message = computed(() => {
  const e = props.error
  if (!e) return ''
  if (GLOBAL_ERROR_CODES.has(e.code)) return t('errors.unknown')
  const key = errorTranslationKey(e.code)
  if (te(key)) return t(key)
  return t('errors.unknown')
})

const recoveryHint = computed(() => {
  const e = props.error
  if (!e) return ''
  return codeDefinition(e.code)?.recovery ?? ''
})

const showRecoveryGuidance = computed(() => recoveryHint.value !== '')

// M6: only show the recovery action button when the parent actually wires
// @recovery. This avoids dead buttons that emit into a void.
const hasRecoveryListener = computed(() => {
  const instance = getCurrentInstance()
  return !!instance?.vnode.props?.onRecovery
})

const showRecoveryButton = computed(() => showRecoveryGuidance.value && hasRecoveryListener.value)

const recoveryLabel = computed(() => {
  const hint = recoveryHint.value
  if (!hint) return ''
  return t(recoveryLabelKey(hint))
})

const recoveryIcon = computed(() => {
  switch (recoveryHint.value) {
    case 'reauth': return LogIn
    default: return RotateCcw
  }
})

const detailEntries = computed(() => {
  const e = props.error
  if (!e) return []
  return localizedDetailEntries(e)
})

const showRequestId = computed(() => !!props.error?.requestId)
const showDiagnostic = computed(() => props.isAdmin && showRequestId.value)
watch(showDiagnostic, (visible) => {
  if (visible) return
  diagSeq++
  diagState.value = 'idle'
  diagRecord.value = null
})

async function copyRequestId(): Promise<void> {
  const rid = props.error?.requestId
  if (!rid) return
  try {
    await navigator.clipboard.writeText(rid)
    copied.value = true
    setTimeout(() => { copied.value = false }, 2000)
  } catch {
    // clipboard API unavailable — silently no-op
  }
}

function onDismiss(): void {
  emit('dismiss')
}

function onRecovery(): void {
  emit('recovery')
}

async function fetchDiagnostic(): Promise<void> {
  const rid = props.error?.requestId
  if (!rid) return
  const seq = ++diagSeq
  diagState.value = 'loading'
  diagRecord.value = null
  try {
    const result = await api.get<DiagnosticRecord>(
      `/api/prohibitorum/diagnostics/${encodeURIComponent(rid)}`,
    )
    // M1: discard stale results — the error may have changed or the panel
    // may have been unmounted while the fetch was in-flight.
    if (seq !== diagSeq) return
    diagRecord.value = result
    diagState.value = 'loaded'
  } catch {
    if (seq !== diagSeq) return
    diagState.value = 'error'
  }
}

const diagFields = computed(() => {
  const r = diagRecord.value
  if (!r) return []
  const entries: Array<{ key: string; label: string; value: string }> = [
    { key: 'requestId', label: t('errors.diagnosticField_requestId'), value: r.requestId },
    { key: 'code', label: t('errors.diagnosticField_code'), value: r.code },
    { key: 'operation', label: t('errors.diagnosticField_operation'), value: r.operation },
    { key: 'method', label: t('errors.diagnosticField_method'), value: r.method },
    { key: 'route', label: t('errors.diagnosticField_route'), value: r.route },
    { key: 'retryable', label: t('errors.diagnosticField_retryable'), value: r.retryable ? t('common.yes') : t('common.no') },
    { key: 'occurredAt', label: t('errors.diagnosticField_occurredAt'), value: r.occurredAt },
    { key: 'expiresAt', label: t('errors.diagnosticField_expiresAt'), value: r.expiresAt },
  ]
  if (r.fields && Object.keys(r.fields).length > 0) {
    entries.push({ key: 'fields', label: t('errors.diagnosticField_fields'), value: JSON.stringify(r.fields) })
  }
  return entries
})
</script>

<template>
  <Alert
    v-if="hasError"
    variant="destructive"
    role="alert"
    aria-live="polite"
    class="relative flex flex-col gap-0 border-destructive/25 bg-destructive/[0.05] px-4 py-3.5 pe-12"
  >
    <AlertDescription data-test="error-summary" class="leading-5 text-destructive">
      {{ message }}
    </AlertDescription>

    <button
      v-if="dismissible"
      type="button"
      data-test="error-dismiss"
      class="absolute -end-1 top-0 inline-flex size-11 items-center justify-center rounded text-destructive hover:text-destructive/80 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      :aria-label="t('errors.dismiss')"
      @click="onDismiss"
    >
      <X class="size-4" aria-hidden="true" />
    </button>

    <!-- Recovery guidance: text always shown; button only when @recovery is wired -->
    <div v-if="showRecoveryGuidance" class="mt-2">
      <span v-if="!showRecoveryButton" class="text-xs leading-4 text-muted">
        {{ recoveryLabel }}
      </span>
      <Button
        v-else
        type="button"
        data-test="error-recovery"
        variant="outline"
        size="sm"
        class="min-h-11"
        @click="onRecovery"
      >
        <component :is="recoveryIcon" class="size-3.5" aria-hidden="true" />
        {{ recoveryLabel }}
      </Button>
    </div>

    <div data-test="error-actions" class="mt-3 flex min-h-8 flex-wrap items-center gap-x-2 gap-y-1">
      <button
        type="button"
        data-test="error-details-trigger"
        class="inline-flex h-8 items-center gap-1 rounded px-1 text-xs font-medium text-destructive/85 hover:text-destructive focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        :aria-expanded="detailsOpen"
        :aria-controls="detailsContentId"
        @click="detailsOpen = !detailsOpen"
      >
        <ChevronDown
          class="size-3.5 transition-transform"
          :class="{ 'rotate-180': detailsOpen }"
          aria-hidden="true"
        />
        {{ t('errors.detailsLabel') }}
      </button>
      <Button
        v-if="showDiagnostic"
        type="button"
        data-test="error-diagnostic"
        variant="ghost"
        size="sm"
        class="h-8 px-2 text-xs"
        @click="fetchDiagnostic"
      >
        <Stethoscope class="size-3.5" aria-hidden="true" />
        {{ t('errors.diagnostic') }}
      </Button>
    </div>

    <dl
      v-if="detailsOpen"
      :id="detailsContentId"
      data-test="error-details"
      class="mt-2 grid min-w-0 grid-cols-[max-content_minmax(0,1fr)] gap-x-3 gap-y-2 border-t border-destructive/15 pt-3 text-xs"
    >
      <dt data-test="error-code-label" class="font-medium text-muted">
        {{ t('errors.diagnosticField_code') }}
      </dt>
      <dd data-test="error-code" class="break-all font-mono text-ink">
        {{ error?.code }}
      </dd>

      <template v-for="entry in detailEntries" :key="entry.field">
        <dt class="font-medium text-muted">{{ t(entry.labelKey) }}</dt>
        <dd class="min-w-0 break-words text-ink">
          {{
            entry.reasonKey && te(entry.reasonKey)
              ? t(entry.reasonKey)
              : Array.isArray(entry.value) ? entry.value.join(', ') : String(entry.value)
          }}
        </dd>
      </template>

      <template v-if="showRequestId">
        <dt class="font-medium text-muted">{{ t('errors.requestId') }}</dt>
        <dd data-test="error-request-id-row" class="flex min-w-0 items-center gap-1">
          <code data-test="error-request-id" class="min-w-0 flex-1 break-all font-mono text-ink">
            {{ error?.requestId }}
          </code>
          <button
            type="button"
            data-test="error-copy-request-id"
            class="inline-flex size-8 shrink-0 items-center justify-center rounded text-muted hover:text-ink focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            :aria-label="t('errors.copyRequestId')"
            @click="copyRequestId"
          >
            <Copy class="size-3.5" aria-hidden="true" />
          </button>
          <span v-if="copied" class="text-sage-700">{{ t('errors.copied') }}</span>
        </dd>
      </template>
    </dl>

    <div
      v-if="showDiagnostic && diagState === 'loading'"
      data-test="diagnostic-loading"
      class="mt-3 text-xs text-muted"
      role="status"
    >
      {{ t('errors.diagnosticLoading') }}
    </div>

    <div
      v-if="showDiagnostic && diagState === 'error'"
      data-test="diagnostic-error"
      class="mt-3 text-xs text-destructive"
      role="alert"
    >
      {{ t('errors.diagnosticError') }}
    </div>

    <dl
      v-if="showDiagnostic && diagState === 'loaded' && diagRecord"
      data-test="diagnostic-record"
      role="region"
      :aria-label="t('errors.diagnosticRecord')"
      class="mt-3 grid min-w-0 grid-cols-[max-content_minmax(0,1fr)] gap-x-3 gap-y-2 rounded-md border border-border p-3 text-xs"
    >
      <template v-for="field in diagFields" :key="field.key">
        <dt class="font-medium text-muted">{{ field.label }}</dt>
        <dd class="min-w-0 break-all text-ink">{{ field.value }}</dd>
      </template>
    </dl>
  </Alert>
</template>
