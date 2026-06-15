/**
 * Prod-parity i18n guard.
 *
 * vue-i18n's DEV/test message compiler only WARNS on malformed messages (e.g. a
 * literal '@', which it reads as a linked-message reference) and renders a
 * fallback — so a bad string passes every component test. The PRODUCTION build's
 * compiler THROWS the same SyntaxError at render time, blanking whichever subtree
 * formatted the message. (This is exactly how `emailPlaceholder: 'name@example.com'`
 * silently blanked the admin "Identity & role" card in the built app.)
 *
 * This test compiles every leaf message through @intlify/message-compiler with a
 * throwing error collector — the same path production takes — so any
 * compile-invalid message fails here instead of in a user's browser. Literal '@'
 * must be escaped as {'@'}; '{...}' interpolation and '|' plurals stay valid.
 */
import { describe, it, expect } from 'vitest'
import { baseCompile } from '@intlify/message-compiler'
import en from './en'

function leaves(obj: unknown, path: string[] = []): Array<[string, string]> {
  if (typeof obj === 'string') return [[path.join('.'), obj]]
  if (obj && typeof obj === 'object') {
    return Object.entries(obj as Record<string, unknown>).flatMap(([k, v]) => leaves(v, [...path, k]))
  }
  return []
}

describe('en locale messages compile under the production compiler', () => {
  it('has no message that the prod vue-i18n compiler rejects', () => {
    const broken: string[] = []
    for (const [key, value] of leaves(en)) {
      const errors: string[] = []
      baseCompile(value, { onError: (e) => errors.push(`${e.code}:${e.message}`) })
      if (errors.length) broken.push(`${key} = ${JSON.stringify(value)} -> ${errors.join('; ')}`)
    }
    expect(broken, `messages rejected by the production compiler:\n${broken.join('\n')}`).toEqual([])
  })
})
