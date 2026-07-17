/**
 * Error locale parity: every code in the manifest must have a locale entry in
 * both en and zh. Every detail key must have a label. Every recovery hint must
 * have a label. This catches drift between the Go registry, the frontend
 * manifest, and the locale files.
 */
import { describe, it, expect } from 'vitest'
import en from './en'
import zh from './zh'
import {
  ALL_CODES,
  ALL_DETAIL_KEYS,
  ALL_RECOVERY_HINTS,
  EXPECTED_REGISTRY_CODE_COUNT,
  REGISTRY_CODES,
} from '@/lib/errorCodes'

function get(obj: unknown, path: string): unknown {
  return path.split('.').reduce<unknown>((acc, key) => {
    if (acc && typeof acc === 'object') {
      return (acc as Record<string, unknown>)[key]
    }
    return undefined
  }, obj)
}

describe('error locale parity — every manifest code has en+zh entries', () => {
  for (const def of ALL_CODES) {
    const enKey = `errors.codes.${def.code}`
    const zhKey = `errors.codes.${def.code}`

    it(`en has ${enKey}`, () => {
      const val = get(en, enKey)
      expect(typeof val, `en missing ${enKey}`).toBe('string')
      expect((val as string).length).toBeGreaterThan(0)
    })

    it(`zh has ${zhKey}`, () => {
      const val = get(zh, zhKey)
      expect(typeof val, `zh missing ${zhKey}`).toBe('string')
      expect((val as string).length).toBeGreaterThan(0)
    })
  }
})
describe('VRChat operator errors match the canonical manifest and public copy', () => {
  const stableErrors = {
    vrchat_operator_credentials_invalid: {
      recovery: '',
      en: 'Those operator credentials were rejected. Check them and try again.',
      zh: '操作员凭据被拒绝。请检查后重试。',
    },
    vrchat_operator_challenge_invalid: {
      recovery: '',
      en: 'That verification challenge expired. Start the operator session again.',
      zh: '验证挑战已过期。请重新启动操作员会话。',
    },
    vrchat_operator_code_invalid: {
      recovery: 'retry',
      en: 'That verification code was rejected. Try another code.',
      zh: '验证码被拒绝。请尝试另一个验证码。',
    },
    upstream_rate_limited: {
      recovery: 'retry',
      en: 'The upstream service is limiting requests. Wait, then try again.',
      zh: '上游服务正在限制请求。请稍后重试。',
    },
    upstream_temporarily_unavailable: {
      recovery: 'retry',
      en: 'The upstream service is temporarily unavailable. Try again soon.',
      zh: '上游服务暂时不可用。请稍后重试。',
    },
  } as const

  for (const [code, expected] of Object.entries(stableErrors)) {
    it(`defines ${code} with exact recovery metadata and localized copy`, () => {
      expect(REGISTRY_CODES.find((definition) => definition.code === code)?.recovery).toBe(expected.recovery)
      expect(get(en, `errors.codes.${code}`)).toBe(expected.en)
      expect(get(zh, `errors.codes.${code}`)).toBe(expected.zh)
    })
  }
})

describe('federation flow errors have English and Chinese public copy', () => {
  const flowErrors = {
    vrchat_identity_invalid: {
      en: 'Enter a valid VRChat user ID or profile URL.',
      zh: '请输入有效的 VRChat 用户 ID 或个人资料网址。',
    },
    vrchat_proof_missing: {
      en: 'Add the issued verification link to your VRChat bio links, then try again.',
      zh: '请将签发的验证链接添加到 VRChat 个人简介链接，然后重试。',
    },
    local_username_required: {
      en: 'Choose a local username to finish signing in.',
      zh: '请选择本地用户名以完成登录。',
    },
    federation_action_invalid: {
      en: 'This verification step changed. Reload the flow and continue from the current step.',
      zh: '验证步骤已更改。请重新加载流程并从当前步骤继续。',
    },
    federation_identity_conflict: {
      en: 'That identity cannot be linked to this account.',
      zh: '该身份无法关联到此账户。',
    },
  } as const

  for (const [code, expected] of Object.entries(flowErrors)) {
    it(`defines ${code} in both locales`, () => {
      expect(get(en, `errors.codes.${code}`)).toBe(expected.en)
      expect(get(zh, `errors.codes.${code}`)).toBe(expected.zh)
    })
  }
})

describe('error locale parity — detail keys have labels in en+zh', () => {
  for (const field of ALL_DETAIL_KEYS) {
    it(`en has errors.details.${field}`, () => {
      const val = get(en, `errors.details.${field}`)
      expect(typeof val, `en missing errors.details.${field}`).toBe('string')
    })

    it(`zh has errors.details.${field}`, () => {
      const val = get(zh, `errors.details.${field}`)
      expect(typeof val, `zh missing errors.details.${field}`).toBe('string')
    })
  }
})

describe('error locale parity — recovery hints have labels in en+zh', () => {
  for (const hint of ALL_RECOVERY_HINTS) {
    it(`en has errors.recovery.${hint}`, () => {
      const val = get(en, `errors.recovery.${hint}`)
      expect(typeof val, `en missing errors.recovery.${hint}`).toBe('string')
    })

    it(`zh has errors.recovery.${hint}`, () => {
      const val = get(zh, `errors.recovery.${hint}`)
      expect(typeof val, `zh missing errors.recovery.${hint}`).toBe('string')
    })
  }
})

describe('error locale parity — meta keys exist in en+zh', () => {
  const metaKeys = [
    'errors.unknown',
    'errors.dismiss',
    'errors.detailsLabel',
    'errors.requestId',
    'errors.copyRequestId',
    'errors.copied',
    'errors.diagnostic',
    'errors.diagnosticLoading',
    'errors.diagnosticError',
    'errors.diagnosticRecord',
    'errors.diagnosticField_requestId',
    'errors.diagnosticField_code',
    'errors.diagnosticField_operation',
    'errors.diagnosticField_method',
    'errors.diagnosticField_route',
    'errors.diagnosticField_retryable',
    'errors.diagnosticField_occurredAt',
    'errors.diagnosticField_expiresAt',
    'errors.diagnosticField_fields',
  ]

  for (const key of metaKeys) {
    it(`en has ${key}`, () => {
      const val = get(en, key)
      expect(typeof val, `en missing ${key}`).toBe('string')
    })

    it(`zh has ${key}`, () => {
      const val = get(zh, key)
      expect(typeof val, `zh missing ${key}`).toBe('string')
    })
  }
})
describe('error locale parity — reason catalogs exist in en+zh', () => {
  const reasonCatalogs: Record<string, string[]> = {
    'errors.reasons.reason': ['too_short', 'too_long', 'invalid_format', 'required', 'out_of_range', 'not_registered'],
    'errors.reasons.upstreamCode': ['access_denied', 'invalid_request', 'unauthorized_client', 'unsupported_response_type', 'server_error', 'temporarily_unavailable'],
  }

  for (const [baseKey, values] of Object.entries(reasonCatalogs)) {
    for (const value of values) {
      const key = `${baseKey}.${value}`
      it(`en has ${key}`, () => {
        const val = get(en, key)
        expect(typeof val, `en missing ${key}`).toBe('string')
      })

      it(`zh has ${key}`, () => {
        const val = get(zh, key)
        expect(typeof val, `zh missing ${key}`).toBe('string')
      })
    }
  }
})

describe('error manifest — registry code count matches Go snapshot', () => {
  it(`REGISTRY_CODES has exactly ${EXPECTED_REGISTRY_CODE_COUNT} entries`, () => {
    expect(REGISTRY_CODES.length).toBe(EXPECTED_REGISTRY_CODE_COUNT)
  })
})
