<script setup lang="ts">
/**
 * ErrorPanel — persistent, code-driven, localized error display.
 *
 * Replaces every inline `<Alert v-if="errorText">{{ errorText }}</Alert>` block.
 * The panel renders the localized message for the error's code (or the
 * unknown fallback), a collapsible details disclosure showing curated detail
 * fields and the copyable request ID, an optional recovery action button,
 * and an admin-only diagnostic lookup action.
 *
 * Key contract guarantees:
 * - role="alert" so assistive tech announces it assertively.
 * - NEVER auto-dismisses. The panel persists until the user clicks dismiss
 *   (emits `dismiss`) or a successful retry clears the error (useApi clears
 *   on success).
 * - The requestId is hidden in the collapsed summary and only shown in the
 *   expanded details disclosure.
 * - Non-admin users never see the diagnostic action.
 */
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { X, ChevronDown, Copy, Stethoscope, RotateCcw, LogIn } from 'lucide-vue-next'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import type { ApiError } from '@/lib/errors'
import { errorTranslationKey, detailLabelKey, recoveryLabelKey, localizedDetailEntries } from '@/lib/errors'
import { codeDefinition } from '@/lib/errorCodes'

const props = withDefaults(defineProps<{
  error: ApiError | null
  isAdmin?: boolean
}>(), {
  isAdmin: false,
})

const emit = defineEmits<{
  (e: 'dismiss'): void
  (e: 'recovery'): void
  (e: 'diagnostic', payload: { requestId: string }): void
}>()

const { t, te } = useI18n()
const detailsOpen = ref(false)
const copied = ref(false)

const hasError = computed(() => props.error !== null)

/**
 * Codes owned by a global handler (redirect/toast). For these the summary
 * message is suppressed (errorText is '' in useApi) to avoid duplicating the
 * global UX. The ErrorPanel still renders — showing the unknown fallback and
 * the details/copy/diagnostic actions — but without the code-specific message.
 */
const GLOBAL_CODES = new Set(['no_session', 'maintenance_mode', 'network_error', 'server_error'])

const message = computed(() => {
  const e = props.error
  if (!e) return ''
  // Globally-handled codes: suppress the code-specific message (the global
  // handler owns the UX). Show the unknown fallback instead so the panel still
  // provides actionable feedback without duplicating the redirect/toast.
  if (GLOBAL_CODES.has(e.code)) return t('errors.unknown')
  const key = errorTranslationKey(e.code)
  if (te(key)) return t(key)
  return t('errors.unknown')
})

const recoveryHint = computed(() => {
  const e = props.error
  if (!e) return ''
  return codeDefinition(e.code)?.recovery ?? ''
})

const showRecovery = computed(() => recoveryHint.value !== '')

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

function onDiagnostic(): void {
  const rid = props.error?.requestId
  if (rid) emit('diagnostic', { requestId: rid })
}
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

    <!-- Recovery action -->
    <div v-if="showRecovery" class="flex items-center gap-2">
      <Button
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
        <!-- Curated detail fields -->
        <div v-for="entry in detailEntries" :key="entry.field" class="flex gap-1">
          <span class="font-medium">{{ t(entry.labelKey) }}:</span>
          <span>{{ Array.isArray(entry.value) ? entry.value.join(', ') : String(entry.value) }}</span>
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
    <!-- Admin diagnostic action — always visible for admins with a requestId -->
    <div v-if="showDiagnostic">
      <Button
        type="button"
        data-test="error-diagnostic"
        variant="ghost"
        size="sm"
        class="text-xs"
        @click="onDiagnostic"
      >
        <Stethoscope class="size-3.5" aria-hidden="true" />
        {{ t('errors.diagnostic') }}
      </Button>
    </div>
  </Alert>
</template>
