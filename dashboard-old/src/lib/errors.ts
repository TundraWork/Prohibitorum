/**
 * Error types and localization helpers for the code-driven error contract.
 *
 * The API client (api.ts) maps every failure to an `ApiError` with a stable
 * `code`, optional curated `details`, and the server-generated `requestId`.
 * The server NEVER sends a display message — the frontend selects localized
 * copy from the code and details via `errorTranslationKey` and
 * `localizedDetailEntries`.
 *
 * `ApiError` intentionally has no `message` field. Unknown/unparseable
 * failures get `code: 'server_error'` (or `'network_error'`) with no details.
 */

import type { ErrorCodeDef } from './errorCodes'
import { codeDefinition } from './errorCodes'

export interface ApiError {
  code: string
  details?: Record<string, string | number | boolean | string[]>
  requestId?: string
  retryAfterSeconds?: number
}

/** Type guard: does `v` look like an ApiError (has a string `code`)? */
export function isApiError(v: unknown): v is ApiError {
  return (
    typeof v === 'object' &&
    v !== null &&
    typeof (v as Record<string, unknown>).code === 'string'
  )
}

/**
 * The i18n key for a code's primary message: `errors.codes.<code>`.
 * Callers check `te(key)` to decide whether to use the localized message or
 * the unknown-code fallback (`errors.unknown`).
 */
export function errorTranslationKey(code: string): string {
  return `errors.codes.${code}`
}

/**
 * The i18n key for a detail field's label: `errors.details.<field>`.
 */
export function detailLabelKey(field: string): string {
  return `errors.details.${field}`
}

/**
 * The i18n key for a detail field's value/reason: `errors.reasons.<field>.<value>`.
 * Used for enum-style detail values like `reason: 'not_registered'`.
 */
export function detailReasonKey(field: string, value: string): string {
  return `errors.reasons.${field}.${value}`
}

/**
 * The i18n key for a recovery action label: `errors.recovery.<hint>`.
 */
export function recoveryLabelKey(hint: string): string {
  return `errors.recovery.${hint}`
}

/**
 * Extract a safe ApiError from a parsed JSON body. Strips any `message`
 * field — the server may include it in legacy/error responses but the
 * frontend contract is code-driven only.
 *
 * If the body has a string `code`, it becomes the ApiError code. Otherwise
 * falls back to `server_error` with no details.
 */
export function parseApiError(
  data: unknown,
  requestId?: string,
): ApiError {
  if (isApiError(data)) {
    const err: ApiError = { code: data.code }
    if (data.details && typeof data.details === 'object') {
      err.details = data.details as Record<string, string | number | boolean | string[]>
    }
    if (requestId) {
      err.requestId = requestId
    } else if (typeof data.requestId === 'string' && data.requestId) {
      err.requestId = data.requestId
    }
    return err
  }
  const err: ApiError = { code: 'server_error' }
  if (requestId) err.requestId = requestId
  return err
}

/**
 * Known enum reason values per detail field. Only values listed here have a
 * `reasonKey` set — the locale files define `errors.reasons.<field>.<value>`
 * for each entry. Values not in the catalog fall back to the raw curated
 * value (still safe to display, just not localized as an enum).
 */
const KNOWN_REASONS: Record<string, Set<string>> = {
  reason: new Set([
    'too_short', 'too_long', 'invalid_format', 'required', 'out_of_range', 'not_registered',
  ]),
  upstreamCode: new Set([
    'access_denied', 'invalid_request', 'unauthorized_client', 'unsupported_response_type',
    'server_error', 'temporarily_unavailable',
  ]),
}

/**
 * Build a list of `{label, value, reasonKey?}` entries for the details
 * disclosure. Each declared detail key that exists on the error produces an
 * entry whose `label` is the i18n key for that field and `value` is the raw
 * detail value. For string-valued fields whose value is in the KNOWN_REASONS
 * catalog, `reasonKey` is set to the i18n key for
 * `errors.reasons.<field>.<value>` so the component can translate known enum
 * values; it stays undefined when no catalog entry exists (the component
 * falls back to the raw curated value).
 *
 * Only declared detail keys (from the manifest) are included — undeclared
 * keys are silently dropped to prevent leaking unexpected server data.
 */
export function localizedDetailEntries(
  error: ApiError,
): ReadonlyArray<{ field: string; labelKey: string; value: string | number | boolean | string[]; reasonKey?: string }> {
  const def: ErrorCodeDef | undefined = codeDefinition(error.code)
  if (!def || !error.details) return []
  return def.details
    .filter((field) => error.details![field] !== undefined && error.details![field] !== null)
    .map((field) => {
      const rawValue = error.details![field]
      let reasonKey: string | undefined
      if (typeof rawValue === 'string' && KNOWN_REASONS[field]?.has(rawValue)) {
        reasonKey = detailReasonKey(field, rawValue)
      }
      return {
        field,
        labelKey: detailLabelKey(field),
        value: rawValue,
        reasonKey,
      }
    })
}
