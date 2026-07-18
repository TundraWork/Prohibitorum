/**
 * Key parity: zh must define exactly the same keys as en — no missing keys
 * (which would silently fall back to English) and no extra keys (dead strings).
 * Guards against translation drift as en evolves.
 */
import { describe, it, expect } from 'vitest'
import en from './en'
import zh from './zh'

function keys(obj: unknown, path: string[] = []): string[] {
  if (typeof obj === 'string') return [path.join('.')]
  if (obj && typeof obj === 'object') {
    return Object.entries(obj as Record<string, unknown>).flatMap(([k, v]) => keys(v, [...path, k]))
  }
  return []
}

describe('locale key parity', () => {
  it('zh defines exactly the same keys as en', () => {
    const enKeys = new Set(keys(en))
    const zhKeys = new Set(keys(zh))
    const missing = [...enKeys].filter((k) => !zhKeys.has(k))
    const extra = [...zhKeys].filter((k) => !enKeys.has(k))
    expect({ missing, extra }).toEqual({ missing: [], extra: [] })
  })

  it('keeps the shared identity metadata vocabulary complete in both locales', () => {
    const required = [
      'identity.protocol',
      'identity.protocolOidc',
      'identity.protocolSteam',
      'identity.protocolVrchat',
      'identity.subject',
      'identity.email',
      'identity.steamId',
      'identity.personaName',
      'identity.vrchatUserId',
      'identity.displayName',
      'identity.profileUrl',
      'identity.linkedIdentities',
      'identity.linkedIdentitiesEmpty',
    ]
    const enKeys = new Set(keys(en))
    const zhKeys = new Set(keys(zh))
    expect(required.filter((key) => !enKeys.has(key))).toEqual([])
    expect(required.filter((key) => !zhKeys.has(key))).toEqual([])
  })

  it('keeps public VRChat enrollment and recovery copy complete in both locales', () => {
    const required = [
      'enroll.titleFederatedRegister',
      'enroll.federatedRegisterBody',
      'enroll.titleRecovery',
      'enroll.recoveryBody',
      'federationFlow.accountNoticePrimary',
      'federationFlow.accountNoticeSupporting',
    ]
    const enKeys = keys(en)
    const zhKeys = keys(zh)
    expect(required.filter((key) => !enKeys.includes(key))).toEqual([])
    expect(required.filter((key) => !zhKeys.includes(key))).toEqual([])
  })
})
