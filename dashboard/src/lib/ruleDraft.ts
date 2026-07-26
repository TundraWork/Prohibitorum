import type { Condition, Rule } from './appAccess'

export type RulePathSegment = number | 'child'
export type RulePath = readonly RulePathSegment[]
export type GroupMode = 'all' | 'any'

export interface RuleValidationIssue {
  path: string
  reason: string
  messageKey: string
}

export interface ParsedRuleJSON {
  ok: true
  rule: Rule
  formatted: string
}

export interface InvalidRuleJSON {
  ok: false
  source: string
  path: string
  reason: string
  line?: number
  column?: number
}

export interface RuleOutlineNode {
  kind: 'all' | 'any' | 'predicate'
  text: string
  negated: boolean
  children: RuleOutlineNode[]
  leafCount: number
  groupCount: number
}

function isGroup(condition: Condition): condition is Condition & { op: GroupMode; children: Condition[] } {
  return (condition.op === 'all' || condition.op === 'any') && Array.isArray(condition.children)
}

function isLeaf(condition: Condition): boolean {
  return condition.op === undefined && condition.children === undefined && condition.child === undefined
}

function isNegativeLeaf(condition: Condition): condition is Condition & { op: 'not'; child: Condition } {
  return condition.op === 'not' && condition.child !== undefined && isLeaf(condition.child)
}

function cloneCondition(condition: Condition): Condition {
  if (condition.op === 'all' || condition.op === 'any') {
    return { op: condition.op, children: Array.isArray(condition.children) ? condition.children.map(cloneCondition) : [] }
  }
  if (condition.op === 'not') {
    return condition.child === undefined ? { op: 'not' } : { op: 'not', child: cloneCondition(condition.child) }
  }
  switch (condition.fact) {
    case 'connection.provider':
      return typeof condition.provider === 'string'
        ? { fact: condition.fact, provider: condition.provider }
        : { fact: condition.fact }
    case 'connection.protocol':
      return condition.protocol === undefined
        ? { fact: condition.fact }
        : { fact: condition.fact, protocol: condition.protocol }
    case 'login_method':
      return condition.method === undefined
        ? { fact: condition.fact }
        : { fact: condition.fact, method: condition.method }
    case 'avatar':
      return condition.source === undefined
        ? { fact: condition.fact }
        : { fact: condition.fact, source: condition.source }
    default:
      return {}
  }
}

function replaceAtPath(
  condition: Condition,
  path: RulePath,
  replacement: (node: Condition) => Condition,
): Condition {
  if (path.length === 0) return replacement(cloneCondition(condition))

  const [segment, ...remaining] = path
  if (typeof segment === 'number' && isGroup(condition) && segment >= 0 && segment < condition.children.length) {
    return {
      op: condition.op,
      children: condition.children.map((child, index) =>
        index === segment ? replaceAtPath(child, remaining, replacement) : cloneCondition(child),
      ),
    }
  }
  if (segment === 'child' && condition.op === 'not' && condition.child !== undefined) {
    return { op: 'not', child: replaceAtPath(condition.child, remaining, replacement) }
  }
  return cloneCondition(condition)
}

function withCondition(rule: Rule, condition: Condition): Rule {
  return { version: rule.version, condition }
}

function defaultPredicate(fact: NonNullable<Condition['fact']>): Condition {
  return { fact }
}

function predicateAtPath(rule: Rule, path: RulePath): Condition | undefined {
  const node = conditionAtPath(rule, path)
  if (node === undefined) return undefined
  return isNegativeLeaf(node) ? node.child : isLeaf(node) ? node : undefined
}

export function makeEmptyRule(): Rule {
  return { version: 1, condition: { op: 'all', children: [{}] } }
}

export function cloneRule(rule: Rule): Rule {
  return withCondition(rule, cloneCondition(rule.condition))
}

export function conditionAtPath(rule: Rule, path: RulePath): Condition | undefined {
  let condition: Condition | undefined = rule.condition
  for (const segment of path) {
    if (condition === undefined) return undefined
    if (typeof segment === 'number') {
      condition = isGroup(condition) && segment >= 0 ? condition.children[segment] : undefined
    } else {
      condition = condition.op === 'not' ? condition.child : undefined
    }
  }
  return condition
}

export function addPredicate(rule: Rule, parent: RulePath): Rule {
  const target = conditionAtPath(rule, parent)
  if (target === undefined) return cloneRule(rule)
  if (isGroup(target)) {
    return withCondition(rule, replaceAtPath(rule.condition, parent, (group) => {
      if (!isGroup(group)) return group
      return { op: group.op, children: [...group.children.map(cloneCondition), {}] }
    }))
  }
  if (parent.length === 0 && (isLeaf(target) || isNegativeLeaf(target))) {
    return withCondition(rule, { op: 'all', children: [cloneCondition(target), {}] })
  }
  return cloneRule(rule)
}

export function addNestedGroup(rule: Rule, parent: RulePath): Rule {
  const nested: Condition = { op: 'all', children: [{}] }
  const target = conditionAtPath(rule, parent)
  if (target === undefined) return cloneRule(rule)
  if (isGroup(target)) {
    return withCondition(rule, replaceAtPath(rule.condition, parent, (group) => {
      if (!isGroup(group)) return group
      return { op: group.op, children: [...group.children.map(cloneCondition), nested] }
    }))
  }
  if (parent.length === 0 && (isLeaf(target) || isNegativeLeaf(target))) {
    return withCondition(rule, { op: 'all', children: [cloneCondition(target), nested] })
  }
  return cloneRule(rule)
}

export function setGroupMode(rule: Rule, path: RulePath, mode: GroupMode): Rule {
  const target = conditionAtPath(rule, path)
  if (!target || !isGroup(target)) return cloneRule(rule)
  return withCondition(rule, replaceAtPath(rule.condition, path, (group) => {
    if (!isGroup(group)) return group
    return { op: mode, children: group.children.map(cloneCondition) }
  }))
}

export function setPredicateNegated(rule: Rule, path: RulePath, negated: boolean): Rule {
  const target = conditionAtPath(rule, path)
  if (target === undefined) return cloneRule(rule)
  if (negated && isLeaf(target)) {
    return withCondition(rule, replaceAtPath(rule.condition, path, (leaf) => ({ op: 'not', child: cloneCondition(leaf) })))
  }
  if (!negated && isNegativeLeaf(target)) {
    return withCondition(rule, replaceAtPath(rule.condition, path, (negative) =>
      isNegativeLeaf(negative) ? cloneCondition(negative.child) : negative,
    ))
  }
  return cloneRule(rule)
}

export function removeNode(rule: Rule, path: RulePath): { rule: Rule; removed: Condition; focusPath: RulePath } {
  if (path.length === 0 || typeof path[path.length - 1] !== 'number') {
    return { rule: cloneRule(rule), removed: {}, focusPath: [] }
  }
  const index = path[path.length - 1] as number
  const parent = path.slice(0, -1)
  const group = conditionAtPath(rule, parent)
  if (!group || !isGroup(group) || index < 0 || index >= group.children.length) {
    return { rule: cloneRule(rule), removed: {}, focusPath: parent }
  }

  const removed = cloneCondition(group.children[index])
  const nextChildren = group.children.filter((_, childIndex) => childIndex !== index).map(cloneCondition)
  const focusPath: RulePath = index < nextChildren.length ? [...parent, index] : index > 0 ? [...parent, index - 1] : parent
  return {
    rule: withCondition(rule, replaceAtPath(rule.condition, parent, (node) =>
      isGroup(node) ? { op: node.op, children: nextChildren } : node,
    )),
    removed,
    focusPath,
  }
}

export function restoreNode(rule: Rule, parent: RulePath, index: number, removed: Condition): Rule {
  const target = conditionAtPath(rule, parent)
  if (target === undefined) return cloneRule(rule)
  if (isGroup(target)) {
    return withCondition(rule, replaceAtPath(rule.condition, parent, (group) => {
      if (!isGroup(group)) return group
      const children = group.children.map(cloneCondition)
      children.splice(Math.max(0, Math.min(index, children.length)), 0, cloneCondition(removed))
      return { op: group.op, children }
    }))
  }
  if (parent.length === 0 && (isLeaf(target) || isNegativeLeaf(target))) {
    const children = [cloneCondition(target)]
    children.splice(Math.max(0, Math.min(index, children.length)), 0, cloneCondition(removed))
    return withCondition(rule, { op: 'all', children })
  }
  return cloneRule(rule)
}

export function moveNode(rule: Rule, path: RulePath, delta: -1 | 1): Rule {
  if (path.length === 0 || typeof path[path.length - 1] !== 'number') return cloneRule(rule)
  const index = path[path.length - 1] as number
  const parent = path.slice(0, -1)
  const group = conditionAtPath(rule, parent)
  if (!group || !isGroup(group)) return cloneRule(rule)
  const destination = index + delta
  if (index < 0 || index >= group.children.length || destination < 0 || destination >= group.children.length) {
    return cloneRule(rule)
  }
  return withCondition(rule, replaceAtPath(rule.condition, parent, (node) => {
    if (!isGroup(node)) return node
    const children = node.children.map(cloneCondition)
    ;[children[index], children[destination]] = [children[destination], children[index]]
    return { op: node.op, children }
  }))
}

export function updatePredicateFact(rule: Rule, path: RulePath, fact: NonNullable<Condition['fact']>): Rule {
  if (!predicateAtPath(rule, path)) return cloneRule(rule)
  return withCondition(rule, replaceAtPath(rule.condition, path, (node) => {
    if (isNegativeLeaf(node)) return { op: 'not', child: defaultPredicate(fact) }
    return isLeaf(node) ? defaultPredicate(fact) : node
  }))
}

export function updatePredicateValue(rule: Rule, path: RulePath, value: string): Rule {
  if (!predicateAtPath(rule, path)) return cloneRule(rule)
  return withCondition(rule, replaceAtPath(rule.condition, path, (node) => {
    const update = (leaf: Condition): Condition => {
      switch (leaf.fact) {
        case 'connection.provider':
          return { fact: leaf.fact, provider: value }
        case 'connection.protocol':
          return { fact: leaf.fact, protocol: value as Condition['protocol'] }
        case 'login_method':
          return { fact: leaf.fact, method: value as Condition['method'] }
        case 'avatar':
          return { fact: leaf.fact, source: value as Condition['source'] }
        default:
          return cloneCondition(leaf)
      }
    }
    if (isNegativeLeaf(node)) return { op: 'not', child: update(node.child) }
    return isLeaf(node) ? update(node) : node
  }))
}

const MAX_RULE_DEPTH = 8
const MAX_RULE_NODES = 64
const MAX_RULE_CHILDREN = 32
const CONDITION_KEYS = new Set(['op', 'children', 'child', 'fact', 'provider', 'protocol', 'method', 'source'])
const RULE_KEYS = new Set(['version', 'condition'])

function validationIssue(path: string, reason: string): RuleValidationIssue {
  return { path, reason, messageKey: `manage.policy.rule.validation.${reason}` }
}

function hasUnknownKeys(value: Record<string, unknown>, allowed: ReadonlySet<string>): boolean {
  return Object.keys(value).some((key) => !allowed.has(key))
}

function hasWrongWireTypes(document: Record<string, unknown>): boolean {
  if (typeof document.version !== 'number') return true
  function wrong(node: unknown, allowNull = false): boolean {
    if (node === null) return !allowNull
    if (typeof node !== 'object' || Array.isArray(node)) return true
    const condition = node as Record<string, unknown>
    if (Object.prototype.hasOwnProperty.call(condition, 'op') && typeof condition.op !== 'string') return true
    if (Object.prototype.hasOwnProperty.call(condition, 'fact') && typeof condition.fact !== 'string') return true
    for (const key of ['provider', 'protocol', 'method', 'source'] as const) {
      if (Object.prototype.hasOwnProperty.call(condition, key) && condition[key] !== null && typeof condition[key] !== 'string') return true
    }
    if (Object.prototype.hasOwnProperty.call(condition, 'children')) {
      if (condition.children !== null && !Array.isArray(condition.children)) return true
      if (Array.isArray(condition.children) && condition.children.some((child) => wrong(child))) return true
    }
    if (Object.prototype.hasOwnProperty.call(condition, 'child') && condition.child !== null && wrong(condition.child)) return true
    return false
  }
  return wrong(document.condition, true)
}

export function validateRule(rule: Rule, providers: ReadonlySet<string>): RuleValidationIssue[] {
  const raw = rule as unknown
  if (raw === null || typeof raw !== 'object' || Array.isArray(raw)) {
    return [validationIssue('$', 'invalid_json')]
  }
  const document = raw as Record<string, unknown>
  if (hasUnknownKeys(document, RULE_KEYS)) return [validationIssue('$', 'unknown_field')]
  if (!Object.prototype.hasOwnProperty.call(document, 'version')) return [validationIssue('$', 'missing_version')]
  if (!Object.prototype.hasOwnProperty.call(document, 'condition')) return [validationIssue('$', 'missing_condition')]
  if (hasWrongWireTypes(document)) return [validationIssue('$', 'invalid_json')]
  if (document.version !== 1) return [validationIssue('$', 'unsupported_version')]

  let nodes = 0
  function visit(rawCondition: unknown, path: string, depth: number): RuleValidationIssue | undefined {
    if (depth > MAX_RULE_DEPTH) return validationIssue(path, 'max_depth_exceeded')
    nodes += 1
    if (nodes > MAX_RULE_NODES) return validationIssue(path, 'max_nodes_exceeded')
    if (rawCondition === null || typeof rawCondition !== 'object' || Array.isArray(rawCondition)) {
      return validationIssue(path, 'invalid_shape')
    }
    const condition = rawCondition as Record<string, unknown>
    if (hasUnknownKeys(condition, CONDITION_KEYS)) return validationIssue(path, 'unknown_field')
    if (Object.values(condition).some((value) => value === null)) return validationIssue(path, 'invalid_shape')

    const hasOp = Object.prototype.hasOwnProperty.call(condition, 'op')
    const hasFact = Object.prototype.hasOwnProperty.call(condition, 'fact')
    if (hasOp === hasFact) return validationIssue(path, 'invalid_shape')

    if (hasOp) {
      if (
        Object.prototype.hasOwnProperty.call(condition, 'provider') ||
        Object.prototype.hasOwnProperty.call(condition, 'protocol') ||
        Object.prototype.hasOwnProperty.call(condition, 'method') ||
        Object.prototype.hasOwnProperty.call(condition, 'source') ||
        hasFact
      ) return validationIssue(path, 'invalid_shape')

      if (condition.op === 'all' || condition.op === 'any') {
        if (Object.prototype.hasOwnProperty.call(condition, 'child')) return validationIssue(path, 'invalid_shape')
        if (!Object.prototype.hasOwnProperty.call(condition, 'children')) return validationIssue(path, 'missing_children')
        if (!Array.isArray(condition.children)) return validationIssue(path, 'invalid_shape')
        if (condition.children.length === 0) return validationIssue(path, 'empty_children')
        if (condition.children.length > MAX_RULE_CHILDREN) return validationIssue(path, 'max_children_exceeded')
        for (const [index, child] of condition.children.entries()) {
          const issue = visit(child, `${path}.children[${index}]`, depth + 1)
          if (issue) return issue
        }
        return undefined
      }

      if (condition.op === 'not') {
        if (Object.prototype.hasOwnProperty.call(condition, 'children')) return validationIssue(path, 'invalid_shape')
        if (!Object.prototype.hasOwnProperty.call(condition, 'child')) return validationIssue(path, 'missing_child')
        const child = condition.child
        if (child !== null && typeof child === 'object' && !Array.isArray(child) && Object.prototype.hasOwnProperty.call(child, 'op')) {
          return validationIssue(`${path}.child`, 'not_requires_fact')
        }
        return visit(child, `${path}.child`, depth + 1)
      }
      return validationIssue(path, 'invalid_op')
    }

    if (Object.prototype.hasOwnProperty.call(condition, 'children') || Object.prototype.hasOwnProperty.call(condition, 'child')) {
      return validationIssue(path, 'invalid_shape')
    }
    switch (condition.fact) {
      case 'connection.provider':
        if (Object.prototype.hasOwnProperty.call(condition, 'protocol') || Object.prototype.hasOwnProperty.call(condition, 'method') || Object.prototype.hasOwnProperty.call(condition, 'source')) return validationIssue(path, 'invalid_shape')
        if (typeof condition.provider !== 'string' || condition.provider === '') return validationIssue(path, 'missing_provider')
        if (!providers.has(condition.provider)) return validationIssue(path, 'provider_not_found')
        return undefined
      case 'connection.protocol':
        if (Object.prototype.hasOwnProperty.call(condition, 'provider') || Object.prototype.hasOwnProperty.call(condition, 'method') || Object.prototype.hasOwnProperty.call(condition, 'source')) return validationIssue(path, 'invalid_shape')
        if (condition.protocol !== 'oidc' && condition.protocol !== 'steam' && condition.protocol !== 'vrchat') return validationIssue(path, 'invalid_protocol')
        return undefined
      case 'login_method':
        if (Object.prototype.hasOwnProperty.call(condition, 'provider') || Object.prototype.hasOwnProperty.call(condition, 'protocol') || Object.prototype.hasOwnProperty.call(condition, 'source')) return validationIssue(path, 'invalid_shape')
        if (condition.method !== 'passkey' && condition.method !== 'password_totp' && condition.method !== 'federation') return validationIssue(path, 'invalid_method')
        return undefined
      case 'avatar':
        if (Object.prototype.hasOwnProperty.call(condition, 'provider') || Object.prototype.hasOwnProperty.call(condition, 'protocol') || Object.prototype.hasOwnProperty.call(condition, 'method')) return validationIssue(path, 'invalid_shape')
        if (condition.source !== 'any' && condition.source !== 'user_uploaded') return validationIssue(path, 'invalid_source')
        return undefined
      default:
        return validationIssue(path, 'invalid_fact')
    }
  }

  const issue = visit(document.condition, '$.condition', 1)
  return issue ? [issue] : []
}

function jsonErrorLocation(source: string, error: unknown): Pick<InvalidRuleJSON, 'line' | 'column'> {
  const message = error instanceof Error ? error.message : ''
  const explicit = /line\s+(\d+)\s+column\s+(\d+)/i.exec(message)
  if (explicit) return { line: Number(explicit[1]), column: Number(explicit[2]) }
  const position = /position\s+(\d+)/i.exec(message)
  let offset: number | undefined
  if (position) {
    offset = Number(position[1])
  } else {
    const unexpected = /Unexpected token '([^']+)'/i.exec(message)
    if (unexpected && source.indexOf(unexpected[1]) === source.lastIndexOf(unexpected[1])) {
      offset = source.indexOf(unexpected[1])
    }
  }
  if (offset === undefined || offset < 0) return {}
  const before = source.slice(0, offset)
  const lines = before.split('\n')
  return { line: lines.length, column: (lines.at(-1)?.length ?? 0) + 1 }
}

function trailingJSONReason(source: string): 'trailing_json' | 'invalid_json' {
  let depth = 0
  let inString = false
  let escaped = false
  let started = false
  for (let index = 0; index < source.length; index += 1) {
    const char = source[index]!
    if (!started) {
      if (/\s/.test(char)) continue
      if (char !== '{') return 'invalid_json'
      started = true
      depth = 1
      continue
    }
    if (inString) {
      if (escaped) escaped = false
      else if (char === '\\') escaped = true
      else if (char === '"') inString = false
      continue
    }
    if (char === '"') inString = true
    else if (char === '{' || char === '[') depth += 1
    else if (char === '}' || char === ']') {
      depth -= 1
      if (depth === 0) {
        const suffix = source.slice(index + 1).trim()
        if (suffix === '') return 'invalid_json'
        try {
          JSON.parse(suffix)
          return 'trailing_json'
        } catch {
          return 'invalid_json'
        }
      }
    }
  }
  return 'invalid_json'
}

export function parseRuleJSON(source: string, providers: ReadonlySet<string>): ParsedRuleJSON | InvalidRuleJSON {
  let parsed: unknown
  try {
    parsed = JSON.parse(source)
  } catch (error) {
    return { ok: false, source, path: '$', reason: trailingJSONReason(source), ...jsonErrorLocation(source, error) }
  }
  const issues = validateRule(parsed as Rule, providers)
  if (issues.length > 0) {
    const issue = issues[0]!
    return { ok: false, source, path: issue.path, reason: issue.reason }
  }
  const validated = cloneRule(parsed as Rule)
  return { ok: true, rule: validated, formatted: formatRuleJSON(validated) }
}

export function formatRuleJSON(rule: Rule): string {
  return `${JSON.stringify(cloneRule(rule), null, 2)}\n`
}

function leafExpression(condition: Condition): string {
  switch (condition.fact) {
    case 'connection.provider': return `provider(${JSON.stringify(condition.provider ?? '')})`
    case 'connection.protocol': return `protocol(${JSON.stringify(condition.protocol ?? '')})`
    case 'login_method': return condition.method ?? 'invalid_login_method'
    case 'avatar': return condition.source === 'user_uploaded' ? 'avatar_user_uploaded' : 'avatar_any'
    default: return 'invalid_condition'
  }
}

function conditionExpression(condition: Condition): string {
  if (condition.op === 'all' || condition.op === 'any') {
    const joiner = condition.op === 'all' ? ' && ' : ' || '
    return `(${(condition.children ?? []).map(conditionExpression).join(joiner)})`
  }
  if (condition.op === 'not' && condition.child) return `(!(${leafExpression(condition.child)}))`
  return leafExpression(condition)
}

export function ruleExpression(rule: Rule): string {
  return conditionExpression(rule.condition)
}

function protocolLabel(value: Condition['protocol']): string {
  return value === 'oidc' ? 'OIDC' : value === 'vrchat' ? 'VRChat' : value === 'steam' ? 'Steam' : ''
}

function methodLabel(value: Condition['method']): string {
  switch (value) {
    case 'passkey': return 'Passkey'
    case 'password_totp': return 'Password + TOTP'
    case 'federation': return 'Federation'
    default: return ''
  }
}

function predicateText(condition: Condition, negated: boolean, providerLabels: ReadonlyMap<string, string>): string {
  const operator = negated ? 'is not' : 'is'
  switch (condition.fact) {
    case 'connection.provider': return `Connection provider ${operator} ${providerLabels.get(condition.provider ?? '') ?? condition.provider ?? ''}`
    case 'connection.protocol': return `Connection protocol ${operator} ${protocolLabel(condition.protocol)}`
    case 'login_method': return `Login method ${operator} ${methodLabel(condition.method)}`
    case 'avatar': return `Avatar ${operator} ${condition.source === 'user_uploaded' ? 'User-uploaded' : 'Available'}`
    default: return 'Choose a condition'
  }
}

function outlineCondition(condition: Condition, providerLabels: ReadonlyMap<string, string>): RuleOutlineNode {
  if (condition.op === 'all' || condition.op === 'any') {
    const children = (condition.children ?? []).map((child) => outlineCondition(child, providerLabels))
    return {
      kind: condition.op,
      text: condition.op === 'all' ? 'Every condition is true' : 'At least one condition is true',
      negated: false,
      children,
      leafCount: children.reduce((sum, child) => sum + child.leafCount, 0),
      groupCount: 1 + children.reduce((sum, child) => sum + child.groupCount, 0),
    }
  }
  const negated = isNegativeLeaf(condition)
  const leaf = negated ? condition.child : condition
  return { kind: 'predicate', text: predicateText(leaf, negated, providerLabels), negated, children: [], leafCount: 1, groupCount: 0 }
}

export function ruleOutline(rule: Rule, providerLabels: ReadonlyMap<string, string>): RuleOutlineNode {
  return outlineCondition(rule.condition, providerLabels)
}

export function slugFromDisplayName(value: string): string {
  return value.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '')
}
