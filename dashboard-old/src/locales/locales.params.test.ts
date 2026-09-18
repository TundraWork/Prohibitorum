/**
 * Param parity: each leaf's set of `{…}` tokens (interpolation params AND
 * literal escapes like {'@'} / {'{'} / {'}'} ) must be identical between en and
 * zh. A translation that drops `{count}` or an `@`/brace escape renders wrong or
 * blanks in prod — the compile + key-parity guards don't catch a missing param.
 */
import { describe, it, expect } from 'vitest'
import en from './en'
import zh from './zh'

function leaves(o: unknown, p: string[] = []): Array<[string, string]> {
  if (typeof o === 'string') return [[p.join('.'), o]]
  if (o && typeof o === 'object') return Object.entries(o as Record<string, unknown>).flatMap(([k, v]) => leaves(v, [...p, k]))
  return []
}
const toks = (s: string) => (s.match(/\{[^{}]*\}/g) ?? []).sort()

describe('locale interpolation-param parity', () => {
  it('every zh leaf has the same {…} tokens as the en leaf', () => {
    const enMap = Object.fromEntries(leaves(en))
    const mismatches: string[] = []
    for (const [key, zhVal] of leaves(zh)) {
      const a = toks(enMap[key] ?? ''), b = toks(zhVal)
      if (JSON.stringify(a) !== JSON.stringify(b)) mismatches.push(`${key}: en=${JSON.stringify(a)} zh=${JSON.stringify(b)}`)
    }
    expect(mismatches, `param mismatches:\n${mismatches.join('\n')}`).toEqual([])
  })
})
