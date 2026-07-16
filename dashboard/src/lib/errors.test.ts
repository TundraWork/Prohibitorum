import { describe, it, expect } from 'vitest'
import {
  type ApiError,
  isApiError,
  errorTranslationKey,
  detailLabelKey,
  detailReasonKey,
  recoveryLabelKey,
  parseApiError,
  localizedDetailEntries,
} from './errors'
import {
  REGISTRY_CODES,
  CLIENT_CODES,
  ALL_CODES,
  ALL_DETAIL_KEYS,
  ALL_RECOVERY_HINTS,
  EXPECTED_REGISTRY_CODE_COUNT,
  GLOBAL_ERROR_CODES,
} from './errorCodes'

describe('ApiError contract', () => {
  it('ApiError has code, optional details and requestId, and no message', () => {
    const err: ApiError = {
      code: 'account_disabled',
      details: { field: 'status' },
      requestId: 'rid-123',
    }
    expect(err.code).toBe('account_disabled')
    expect(err.details).toEqual({ field: 'status' })
    expect(err.requestId).toBe('rid-123')
    expect((err as Record<string, unknown>).message).toBeUndefined()
  })

  it('isApiError accepts an object with a string code', () => {
    expect(isApiError({ code: 'x' })).toBe(true)
    expect(isApiError({ code: 42 })).toBe(false)
    expect(isApiError(null)).toBe(false)
    expect(isApiError({})).toBe(false)
  })

  it('isApiError does NOT require message', () => {
    expect(isApiError({ code: 'server_error' })).toBe(true)
    // An object with message but no code is NOT an ApiError
    expect(isApiError({ message: 'oops' })).toBe(false)
  })
})

describe('parseApiError', () => {
  it('extracts code, details, and requestId from a server envelope', () => {
    const err = parseApiError(
      { code: 'invalid_role', details: { allowed: ['user', 'admin'] }, requestId: 'rid-1' },
    )
    expect(err).toEqual({
      code: 'invalid_role',
      details: { allowed: ['user', 'admin'] },
      requestId: 'rid-1',
    })
  })

  it('strips the message field — never trusts server prose', () => {
    const err = parseApiError(
      { code: 'bad_request', message: 'raw server text', requestId: 'rid-2' },
    )
    expect((err as Record<string, unknown>).message).toBeUndefined()
    expect(err.code).toBe('bad_request')
  })

  it('falls back to server_error when body has no code', () => {
    const err = parseApiError({ foo: 'bar' }, 'rid-3')
    expect(err.code).toBe('server_error')
    expect(err.requestId).toBe('rid-3')
    expect(err.details).toBeUndefined()
  })

  it('falls back to server_error for null/undefined body', () => {
    expect(parseApiError(null).code).toBe('server_error')
    expect(parseApiError(undefined).code).toBe('server_error')
  })

  it('uses the header requestId when the body lacks one', () => {
    const err = parseApiError({ code: 'rate_limited' }, 'header-rid')
    expect(err.requestId).toBe('header-rid')
  })

  it('preserves details only when they are an object', () => {
    const err = parseApiError({ code: 'x', details: 'not-an-object' })
    expect(err.details).toBeUndefined()
  })
})

describe('translation key helpers', () => {
  it('errorTranslationKey returns errors.codes.<code>', () => {
    expect(errorTranslationKey('account_disabled')).toBe('errors.codes.account_disabled')
  })

  it('detailLabelKey returns errors.details.<field>', () => {
    expect(detailLabelKey('allowed')).toBe('errors.details.allowed')
  })

  it('detailReasonKey returns errors.reasons.<field>.<value>', () => {
    expect(detailReasonKey('reason', 'not_registered')).toBe('errors.reasons.reason.not_registered')
  })

  it('recoveryLabelKey returns errors.recovery.<hint>', () => {
    expect(recoveryLabelKey('retry')).toBe('errors.recovery.retry')
  })
})

describe('localizedDetailEntries', () => {
  it('returns declared detail keys that exist on the error', () => {
    const err: ApiError = {
      code: 'invalid_role',
      details: { allowed: ['user', 'admin'] },
    }
    const entries = localizedDetailEntries(err)
    expect(entries).toHaveLength(1)
    expect(entries[0]).toEqual({
      field: 'allowed',
      labelKey: 'errors.details.allowed',
      value: ['user', 'admin'],
      reasonKey: undefined,
    })
  })

  it('returns multiple entries for codes with multiple detail keys', () => {
    const err: ApiError = {
      code: 'validation_failed',
      details: { location: 'body', reason: 'too_short' },
    }
    const entries = localizedDetailEntries(err)
    expect(entries).toHaveLength(2)
    expect(entries.map((e) => e.field)).toEqual(['location', 'reason'])
  })

  it('drops undeclared detail keys', () => {
    const err: ApiError = {
      code: 'bad_request',
      details: { field: 'x', secret: 'should-not-leak' },
    }
    // bad_request declares no detail keys
    expect(localizedDetailEntries(err)).toEqual([])
  })

  it('returns empty for codes with no details', () => {
    expect(localizedDetailEntries({ code: 'server_error' })).toEqual([])
  })

  it('returns empty when details is missing', () => {
    expect(localizedDetailEntries({ code: 'invalid_role' })).toEqual([])
  })
})

describe('localizedDetailEntries — reason translation (M7)', () => {
  it('includes a reasonKey for string detail values that have a known reason catalog', () => {
    const err: ApiError = {
      code: 'validation_failed',
      details: { location: 'body', reason: 'too_short' },
    }
    const entries = localizedDetailEntries(err)
    const reasonEntry = entries.find((e) => e.field === 'reason')
    expect(reasonEntry).toBeDefined()
    expect(reasonEntry!.reasonKey).toBe('errors.reasons.reason.too_short')
  })

  it('leaves reasonKey undefined when the value has no known reason catalog entry', () => {
    const err: ApiError = {
      code: 'validation_failed',
      details: { location: 'body', reason: 'some_unknown_reason' },
    }
    const entries = localizedDetailEntries(err)
    const reasonEntry = entries.find((e) => e.field === 'reason')
    expect(reasonEntry).toBeDefined()
    expect(reasonEntry!.reasonKey).toBeUndefined()
  })

  it('leaves reasonKey undefined for non-string detail values', () => {
    const err: ApiError = {
      code: 'rate_limited',
      details: { retryAfterSeconds: 30 },
    }
    const entries = localizedDetailEntries(err)
    const entry = entries.find((e) => e.field === 'retryAfterSeconds')
    expect(entry).toBeDefined()
    expect(entry!.reasonKey).toBeUndefined()
  })

  it('includes reasonKey for upstreamCode detail values', () => {
    const err: ApiError = {
      code: 'upstream_error',
      details: { upstreamCode: 'access_denied' },
    }
    const entries = localizedDetailEntries(err)
    const entry = entries.find((e) => e.field === 'upstreamCode')
    expect(entry).toBeDefined()
    expect(entry!.reasonKey).toBe('errors.reasons.upstreamCode.access_denied')
  })
})

describe('shared GLOBAL_ERROR_CODES (M5)', () => {
  it('is exported from errorCodes and contains the global codes', () => {
    expect(GLOBAL_ERROR_CODES.has('no_session')).toBe(true)
    expect(GLOBAL_ERROR_CODES.has('maintenance_mode')).toBe(true)
    expect(GLOBAL_ERROR_CODES.has('network_error')).toBe(true)
    expect(GLOBAL_ERROR_CODES.has('server_error')).toBe(true)
    expect(GLOBAL_ERROR_CODES.has('bad_request')).toBe(false)
  })
})

// --- locale parity: every registry/client code has a locale entry ---

describe('error code manifest integrity', () => {
  it('REGISTRY_CODES count matches the Go registry snapshot', () => {
    expect(REGISTRY_CODES.length).toBe(EXPECTED_REGISTRY_CODE_COUNT)
  })

  it('includes provider_not_ready from the Go registry', () => {
    expect(REGISTRY_CODES.map((definition) => definition.code)).toContain('provider_not_ready')
  })

  it('includes Task 6 VRChat operator recovery metadata', () => {
    expect(REGISTRY_CODES.filter((definition) => [
      'vrchat_operator_credentials_invalid',
      'vrchat_operator_challenge_invalid',
      'vrchat_operator_code_invalid',
      'upstream_rate_limited',
      'upstream_temporarily_unavailable',
    ].includes(definition.code))).toEqual([
      { code: 'upstream_rate_limited', details: [], recovery: 'retry' },
      { code: 'upstream_temporarily_unavailable', details: [], recovery: 'retry' },
      { code: 'vrchat_operator_challenge_invalid', details: [], recovery: '' },
      { code: 'vrchat_operator_code_invalid', details: [], recovery: 'retry' },
      { code: 'vrchat_operator_credentials_invalid', details: [], recovery: '' },
    ])
  })

  it('all codes are unique', () => {
    const codes = ALL_CODES.map((d) => d.code)
    const seen = new Set<string>()
    for (const c of codes) {
      if (seen.has(c)) {
        throw new Error(`duplicate code: ${c}`)
      }
      seen.add(c)
    }
    expect(seen.size).toBe(codes.length)
  })

  it('client codes do not overlap with registry codes', () => {
    const reg = new Set(REGISTRY_CODES.map((d) => d.code))
    for (const c of CLIENT_CODES) {
      expect(reg.has(c.code)).toBe(false)
    }
  })

  it('ALL_DETAIL_KEYS covers every detail key referenced by any code', () => {
    const declared = new Set(ALL_DETAIL_KEYS)
    for (const def of ALL_CODES) {
      for (const dk of def.details) {
        if (!declared.has(dk)) {
          throw new Error(`detail key ${dk} (code ${def.code}) not in ALL_DETAIL_KEYS`)
        }
      }
    }
  })

  it('ALL_RECOVERY_HINTS covers every recovery hint referenced by any code', () => {
    const declared = new Set(ALL_RECOVERY_HINTS)
    for (const def of ALL_CODES) {
      if (def.recovery && !declared.has(def.recovery)) {
        throw new Error(`recovery hint ${def.recovery} (code ${def.code}) not in ALL_RECOVERY_HINTS`)
      }
    }
  })
})
