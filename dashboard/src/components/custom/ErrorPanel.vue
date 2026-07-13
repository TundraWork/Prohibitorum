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
import { ref, computed, watch, onBeforeUnmount, getCurrentInstance } from 'vue'
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
}>(), {
  isAdmin: false,
})

const emit = defineEmits<{
  (e: 'dismiss'): void
  (e: 'recovery'): void
}>()

const { t, te } = useI18n()
const detailsOpen = ref(false)
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
    class="flex flex-col gap-2"
  >
    <div class="flex items-start gap-2">
      <AlertDescription class="flex-1">{{ message }}</AlertDescription>
      <button
        type="button"
        data-test="error-dismiss"
        class="-mr-1 shrink-0 rounded p-0.5 text-destructive hover:text-destructive/80 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        :aria-label="t('errors.dismiss')"
        @click="onDismiss"
      >
        <X class="size-4" aria-hidden="true" />
      </button>
    </div>

    <!-- Recovery guidance: text always shown; button only when @recovery is wired -->
    <div v-if="showRecoveryGuidance" class="flex items-center gap-2">
      <span v-if="!showRecoveryButton" class="text-xs text-muted">{{ recoveryLabel }}</span>
      <Button
        v-else
        type="button"
        data-test="error-recovery"
        variant="outline"
        size="sm"
        @click="onRecovery"
      >
        <component :is="recoveryIcon" class="size-3.5" aria-hidden="true" />
        {{ recoveryLabel }}
      </Button>
    </div>

    <!-- Details disclosure -->
    <div v-if="detailEntries.length > 0 || showRequestId">
      <button
        type="button"
        data-test="error-details-trigger"
        class="flex items-center gap-1 text-xs font-medium text-destructive/80 hover:text-destructive focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring rounded"
        :aria-expanded="detailsOpen"
        aria-controls="error-details-content"
        @click="detailsOpen = !detailsOpen"
      >
        <ChevronDown
          class="size-3.5 transition-transform"
          :class="{ 'rotate-180': detailsOpen }"
          aria-hidden="true"
        />
        {{ t('errors.detailsLabel') }}
      </button>

      <div v-if="detailsOpen" id="error-details-content" class="mt-2 flex flex-col gap-1.5 text-xs text-muted">
        <!-- Curated detail fields — use localized reason when available, raw value otherwise -->
        <div v-for="entry in detailEntries" :key="entry.field" class="flex gap-1">
          <span class="font-medium">{{ t(entry.labelKey) }}:</span>
          <span>{{
            entry.reasonKey && te(entry.reasonKey)
              ? t(entry.reasonKey)
              : Array.isArray(entry.value) ? entry.value.join(', ') : String(entry.value)
          }}</span>
        </div>

        <!-- Request ID + copy -->
        <div v-if="showRequestId" class="flex items-center gap-1">
          <span class="font-medium">{{ t('errors.requestId') }}:</span>
          <code class="font-mono">{{ error?.requestId }}</code>
          <button
            type="button"
            data-test="error-copy-request-id"
            class="rounded p-0.5 text-muted hover:text-ink focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            :aria-label="t('errors.copyRequestId')"
            @click="copyRequestId"
          >
            <Copy class="size-3" aria-hidden="true" />
          </button>
          <span v-if="copied" class="text-sage-700">{{ t('errors.copied') }}</span>
        </div>
      </div>
    </div>

    <!-- Admin diagnostic action — visible for admins with a requestId -->
    <div v-if="showDiagnostic">
      <Button
        type="button"
        data-test="error-diagnostic"
        variant="ghost"
        size="sm"
        class="text-xs"
        @click="fetchDiagnostic"
      >
        <Stethoscope class="size-3.5" aria-hidden="true" />
        {{ t('errors.diagnostic') }}
      </Button>

      <!-- Diagnostic loading state -->
      <div v-if="diagState === 'loading'" data-test="diagnostic-loading" class="mt-2 text-xs text-muted" role="status">
        {{ t('errors.diagnosticLoading') }}
      </div>

      <!-- Diagnostic error state -->
      <div v-if="diagState === 'error'" data-test="diagnostic-error" class="mt-2 text-xs text-destructive" role="alert">
        {{ t('errors.diagnosticError') }}
      </div>

      <!-- Diagnostic record — persistent, accessible -->
      <div
        v-if="diagState === 'loaded' && diagRecord"
        data-test="diagnostic-record"
        role="region"
        :aria-label="t('errors.diagnosticRecord')"
        class="mt-2 flex flex-col gap-1 rounded-md border border-border p-3 text-xs text-muted"
      >
        <p class="font-medium text-ink">{{ t('errors.diagnosticRecord') }}</p>
        <div v-for="field in diagFields" :key="field.key" class="flex gap-1">
          <span class="font-medium">{{ field.label }}:</span>
          <span class="break-all">{{ field.value }}</span>
        </div>
      </div>
    </div>
  </Alert>
</template>
