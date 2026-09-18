/**
 * Prod-parity i18n guard, all locales.
 *
 * vue-i18n's DEV compiler only WARNS on malformed messages (e.g. a literal '@',
 * read as a linked reference); the PRODUCTION compiler THROWS at render time,
 * blanking the subtree. This compiles every leaf of every locale through
 * @intlify/message-compiler with a throwing collector — the prod path — so a
 * compile-invalid message fails here, not in a user's browser. Literal '@' must
 * be escaped as {'@'}; '{...}' interpolation and '|' plurals stay valid.
 */
import { describe, it, expect } from 'vitest'
import { baseCompile } from '@intlify/message-compiler'
import en from './en'
import zh from './zh'

function leaves(obj: unknown, path: string[] = []): Array<[string, string]> {
  if (typeof obj === 'string') return [[path.join('.'), obj]]
  if (obj && typeof obj === 'object') {
    return Object.entries(obj as Record<string, unknown>).flatMap(([k, v]) => leaves(v, [...path, k]))
  }
  return []
}

describe.each([['en', en], ['zh', zh]] as const)('%s locale messages compile under the production compiler', (name, msgs) => {
  it(`has no message that the prod vue-i18n compiler rejects (${name})`, () => {
    const broken: string[] = []
    for (const [key, value] of leaves(msgs)) {
      const errors: string[] = []
      baseCompile(value, { onError: (e) => errors.push(`${e.code}:${e.message}`) })
      if (errors.length) broken.push(`${key} = ${JSON.stringify(value)} -> ${errors.join('; ')}`)
    }
    expect(broken, `messages rejected by the production compiler:\n${broken.join('\n')}`).toEqual([])
  })
})
