import { describe, expect, it } from 'vitest'
import type { Condition, Rule } from './appAccess'
import {
  addNestedGroup,
  addPredicate,
  cloneRule,
  conditionAtPath,
  formatRuleJSON,
  makeEmptyRule,
  moveNode,
  parseRuleJSON,
  removeNode,
  restoreNode,
  ruleExpression,
  ruleOutline,
  setGroupMode,
  setPredicateNegated,
  slugFromDisplayName,
  updatePredicateFact,
  updatePredicateValue,
  validateRule,
} from './ruleDraft'

const providerLeaf: Condition = { fact: 'connection.provider', provider: 'downstream' }
const protocolLeaf: Condition = { fact: 'connection.protocol', protocol: 'oidc' }
const passkeyLeaf: Condition = { fact: 'login_method', method: 'passkey' }

function rule(condition: Condition): Rule {
  return { version: 1, condition }
}

function frozenRule(condition: Condition): Rule {
  return structuredClone(rule(condition))
}

describe('immutable rule draft transformations', () => {
  it('changes a group mode without changing its children or source rule', () => {
    const originalChildren = [providerLeaf, protocolLeaf]
    const allRule = frozenRule({ op: 'all', children: originalChildren })
    const before = structuredClone(allRule)

    const updated = setGroupMode(allRule, [], 'any')

    expect(updated).not.toBe(allRule)
    expect(updated.condition).toEqual({ op: 'any', children: originalChildren })
    expect(allRule).toEqual(before)
  })

  it('wraps and unwraps exactly one predicate for is-not polarity', () => {
    const leafRule = frozenRule(providerLeaf)
    const before = structuredClone(leafRule)
    const negativeLeafRule = setPredicateNegated(leafRule, [], true)

    expect(negativeLeafRule).not.toBe(leafRule)
    expect(negativeLeafRule.condition).toEqual({ op: 'not', child: providerLeaf })
    expect(setPredicateNegated(negativeLeafRule, [], false).condition).toEqual(providerLeaf)
    expect(leafRule).toEqual(before)
  })

  it('adds an incomplete predicate to a root leaf by preserving it in ALL', () => {
    const leafRule = frozenRule(providerLeaf)
    const before = structuredClone(leafRule)

    const updated = addPredicate(leafRule, [])

    expect(updated).not.toBe(leafRule)
    expect(updated.condition).toEqual({ op: 'all', children: [providerLeaf, {}] })
    expect(leafRule).toEqual(before)
  })

  it('adds nested groups and predicates only beneath logical groups', () => {
    const base = frozenRule({ op: 'all', children: [providerLeaf] })
    const withNested = addNestedGroup(base, [])
    const withPredicate = addPredicate(withNested, [1])

    expect(withNested.condition).toEqual({
      op: 'all',
      children: [providerLeaf, { op: 'all', children: [{}] }],
    })
    expect(withPredicate.condition).toEqual({
      op: 'all',
      children: [providerLeaf, { op: 'all', children: [{}, {}] }],
    })
    expect(base.condition).toEqual({ op: 'all', children: [providerLeaf] })
  })

  it('moves a child inside its group without mutating the source', () => {
    const firstChild = providerLeaf
    const secondChild = protocolLeaf
    const thirdChild = passkeyLeaf
    const threeChildRule = frozenRule({ op: 'all', children: [firstChild, secondChild, thirdChild] })
    const before = structuredClone(threeChildRule)

    const moved = moveNode(threeChildRule, [2], -1)

    expect(moved).not.toBe(threeChildRule)
    expect(moved.condition.children).toEqual([firstChild, thirdChild, secondChild])
    expect(threeChildRule).toEqual(before)
  })

  it('removes and restores a subtree with the adjacent focus target', () => {
    const source = frozenRule({ op: 'all', children: [providerLeaf, { op: 'any', children: [protocolLeaf] }, passkeyLeaf] })
    const before = structuredClone(source)

    const removed = removeNode(source, [1])
    const restored = restoreNode(removed.rule, [], 1, removed.removed)

    expect(removed.removed).toEqual({ op: 'any', children: [protocolLeaf] })
    expect(removed.rule.condition).toEqual({ op: 'all', children: [providerLeaf, passkeyLeaf] })
    expect(removed.focusPath).toEqual([1])
    expect(restored).toEqual(source)
    expect(source).toEqual(before)
  })

  it('updates a leaf fact and value while preserving its not wrapper', () => {
    const source = frozenRule({ op: 'all', children: [{ op: 'not', child: providerLeaf }] })
    const before = structuredClone(source)

    const factChanged = updatePredicateFact(source, [0], 'login_method')
    const valueChanged = updatePredicateValue(factChanged, [0], 'federation')

    expect(factChanged.condition).toEqual({
      op: 'all',
      children: [{ op: 'not', child: { fact: 'login_method' } }],
    })
    expect(valueChanged.condition).toEqual({
      op: 'all',
      children: [{ op: 'not', child: { fact: 'login_method', method: 'federation' } }],
    })
    expect(conditionAtPath(valueChanged, [0, 'child'])).toEqual({ fact: 'login_method', method: 'federation' })
    expect(source).toEqual(before)
  })

  it('keeps every newly selected fact incomplete while preserving polarity', () => {
    for (const fact of ['connection.provider', 'connection.protocol', 'login_method', 'avatar'] as const) {
      const positive = updatePredicateFact(rule({}), [], fact)
      expect(positive.condition).toEqual({ fact })
      expect(validateRule(positive, new Set(['downstream']))[0]?.reason).toBeDefined()

      const negative = updatePredicateFact(rule({ op: 'not', child: {} }), [], fact)
      expect(negative.condition).toEqual({ op: 'not', child: { fact } })
      expect(validateRule(negative, new Set(['downstream']))[0]?.reason).toBeDefined()
    }
  })

  it('clones only the closed condition vocabulary', () => {
    const source = {
      version: 1,
      condition: { fact: 'avatar', source: 'any', ignored: 'not persisted' },
      unexpected: true,
    } as unknown as Rule

    expect(cloneRule(source)).toEqual({ version: 1, condition: { fact: 'avatar', source: 'any' } })
  })
})

describe('closed rule validation and JSON', () => {
  const providers = new Set(['downstream'])

  it('starts with one intentionally incomplete condition', () => {
    const draft = makeEmptyRule()
    expect(draft).toEqual({ version: 1, condition: { op: 'all', children: [{}] } })
    expect(validateRule(draft, providers)).toEqual([
      expect.objectContaining({ path: '$.condition.children[0]', reason: 'invalid_shape' }),
    ])
  })

  it('accepts leaf negation and rejects group or nested negation', () => {
    expect(validateRule(rule({ op: 'not', child: providerLeaf }), providers)).toEqual([])
    expect(validateRule(rule({ op: 'not', child: { op: 'all', children: [providerLeaf] } }), providers)).toEqual([
      expect.objectContaining({ path: '$.condition.child', reason: 'not_requires_fact' }),
    ])
    expect(validateRule(rule({ op: 'not', child: { op: 'not', child: providerLeaf } }), providers)).toEqual([
      expect.objectContaining({ path: '$.condition.child', reason: 'not_requires_fact' }),
    ])
  })

  it('rejects unknown fields, bad providers, nulls, bounds, and unsupported versions', () => {
    expect(parseRuleJSON('{"version":2,"condition":{"fact":"avatar","source":"any"}}', providers)).toMatchObject({ ok: false, reason: 'unsupported_version', path: '$' })
    expect(parseRuleJSON('{"version":1,"condition":{"fact":"avatar","source":"any","secret":true}}', providers)).toMatchObject({ ok: false, reason: 'unknown_field', path: '$.condition' })
    expect(parseRuleJSON('{"version":1,"condition":{"fact":"connection.provider","provider":"missing"}}', providers)).toMatchObject({ ok: false, reason: 'provider_not_found', path: '$.condition' })
    expect(parseRuleJSON('{"version":1,"condition":null}', providers)).toMatchObject({ ok: false, reason: 'invalid_shape', path: '$.condition' })
    expect(validateRule(rule({ op: 'all', children: [] }), providers)).toEqual([
      expect.objectContaining({ path: '$.condition', reason: 'empty_children' }),
    ])
    expect(validateRule(rule({ op: 'all', children: Array.from({ length: 33 }, () => providerLeaf) }), providers)).toEqual([
      expect.objectContaining({ path: '$.condition', reason: 'max_children_exceeded' }),
    ])
  })

  it('reports malformed JSON location and preserves its source', () => {
    const source = '{\n  "version": 1,\n  "condition": }'
    const parsed = parseRuleJSON(source, providers)
    expect(parsed).toMatchObject({ ok: false, source, path: '$', reason: 'invalid_json' })
    if (parsed.ok) throw new Error('expected invalid JSON')
    expect(parsed.line).toBe(3)
    expect(parsed.column).toBeGreaterThan(1)
  })

  it('round-trips valid JSON canonically without changing the rule', () => {
    const original = rule({ op: 'all', children: [providerLeaf, { op: 'not', child: passkeyLeaf }] })
    const parsed = parseRuleJSON(JSON.stringify(original), providers)
    expect(parsed).toEqual({ ok: true, rule: original, formatted: `${JSON.stringify(original, null, 2)}\n` })
    expect(formatRuleJSON(original)).toBe(`${JSON.stringify(original, null, 2)}\n`)
  })

  it('distinguishes trailing JSON and rejects wrong wire member types as invalid JSON', () => {
    const valid = '{"version":1,"condition":{"fact":"avatar","source":"any"}}'
    expect(parseRuleJSON(`${valid} {}`, providers)).toMatchObject({ ok: false, reason: 'trailing_json', path: '$' })
    expect(parseRuleJSON(`${valid} ]`, providers)).toMatchObject({ ok: false, reason: 'invalid_json', path: '$' })
    expect(parseRuleJSON(`${valid} {`, providers)).toMatchObject({ ok: false, reason: 'invalid_json', path: '$' })

    for (const source of [
      '{"version":1,"condition":{"op":7,"children":[]}}',
      '{"version":1,"condition":{"fact":7}}',
      '{"version":1,"condition":{"fact":"avatar","source":7}}',
      '{"version":1,"condition":{"op":"all","children":{}}}',
      '{"version":1,"condition":[]}',
      '{"version":1,"condition":{"op":"not","child":7}}',
      '{"version":1,"condition":{"op":"all","children":[7]}}',
    ]) {
      expect(parseRuleJSON(source, providers)).toMatchObject({ ok: false, reason: 'invalid_json' })
    }
  })
})


describe('rule meaning and stable identifiers', () => {
  const nested = rule({
    op: 'all',
    children: [
      providerLeaf,
      protocolLeaf,
      { op: 'any', children: [{ fact: 'login_method', method: 'federation' }, passkeyLeaf] },
      { op: 'not', child: { fact: 'login_method', method: 'password_totp' } },
    ],
  })

  it('renders a fully parenthesized compact expression', () => {
    expect(ruleExpression(nested)).toBe('(provider("downstream") && protocol("oidc") && (federation || passkey) && (!(password_totp)))')
  })

  it('renders an outline with display names and is-not wording', () => {
    const outline = ruleOutline(nested, new Map([['downstream', 'Downstream identity']]))
    expect(outline).toMatchObject({ kind: 'all', text: 'Every condition is true', negated: false, leafCount: 5, groupCount: 2 })
    expect(outline.children[0]).toMatchObject({ kind: 'predicate', text: 'Connection provider is Downstream identity', negated: false })
    expect(outline.children[3]).toMatchObject({ kind: 'predicate', text: 'Login method is not Password + TOTP', negated: true })
  })

  it('generates only canonical group slugs', () => {
    expect(slugFromDisplayName('  Trusted Federated Profile  ')).toBe('trusted-federated-profile')
    expect(slugFromDisplayName('Café / 開発 Team')).toBe('caf-team')
    expect(slugFromDisplayName('---')).toBe('')
  })
})
