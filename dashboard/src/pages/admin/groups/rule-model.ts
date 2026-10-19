import type { AppAccessCondition, AppAccessRule } from "@/api/raw-admin-paths";

/**
 * The rule builder's model, kept apart from the wire shape on purpose.
 *
 * What a reader edits is a condition (one fact, optionally negated) inside a
 * group. On the wire a `not` is a node of its own holding exactly one leaf, so a
 * negated condition serialises as `{op: "not", child: <leaf>}`. The two convert
 * either way without loss, and every node here carries an id, so React keys and
 * server paths survive an edit that reorders siblings.
 */

export type RuleFact =
  | "login_method"
  | "connection.provider"
  | "connection.protocol"
  | "avatar";

/** In the order the fact select lists them. */
export const ruleFacts: readonly RuleFact[] = [
  "login_method",
  "connection.provider",
  "connection.protocol",
  "avatar",
];

/** The values each fact takes. A provider's are the slugs of the catalogue. */
export const ruleFactValues = {
  login_method: ["passkey", "password_totp", "federation"],
  "connection.protocol": ["oidc", "steam", "vrchat"],
  avatar: ["any", "user_uploaded"],
} as const;

/** The values a fact takes by itself; a provider's come from the catalogue. */
export function fixedValues(fact: RuleFact): readonly string[] {
  return fact === "connection.provider" ? [] : ruleFactValues[fact];
}

/** The field a leaf carries its value in, per fact. */
const valueField = {
  login_method: "method",
  "connection.provider": "provider",
  "connection.protocol": "protocol",
  avatar: "source",
} as const satisfies Record<RuleFact, keyof AppAccessCondition>;

export type RuleCombinator = "all" | "any";

export interface RuleCondition {
  kind: "condition";
  id: string;
  fact: RuleFact;
  /** Empty while a choice is still to be made. */
  value: string;
  negated: boolean;
}

export interface RuleGroup {
  kind: "group";
  id: string;
  op: RuleCombinator;
  children: RuleNode[];
  /**
   * Set only by a document that negates a group, which the server refuses. The
   * builder has no control that does it, so the group is kept to be marked.
   */
  negated?: true;
}

export type RuleNode = RuleCondition | RuleGroup;

/** The bounds the server enforces on one document. */
export const ruleLimits = { maxDepth: 8, maxNodes: 64, maxChildren: 32 };

export const rootGroupId = "root";
const documentVersion = 1;

let nextId = 0;
function newId(): string {
  nextId += 1;
  return `n${nextId}`;
}

/** A passkey condition: it needs no choice, so a new row starts on a real value. */
export function newCondition(
  fact: RuleFact = "login_method",
  value = firstValue(fact),
): RuleCondition {
  return { kind: "condition", id: newId(), fact, value, negated: false };
}

function firstValue(fact: RuleFact): string {
  return fixedValues(fact)[0] ?? "";
}

/** A group is never empty: it starts with one condition. */
export function newGroup(): RuleGroup {
  return { kind: "group", id: newId(), op: "all", children: [newCondition()] };
}

/** A rule with one condition, for a group that has no stored rule yet. */
export function newRule(): RuleGroup {
  return { ...newGroup(), id: rootGroupId };
}

/* ----------------------------------------------------------- the wire -- */

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isFact(value: unknown): value is RuleFact {
  return typeof value === "string" && ruleFacts.includes(value as RuleFact);
}

function leafFor(
  raw: Record<string, unknown>,
  negated: boolean,
): RuleCondition {
  if (!isFact(raw.fact)) return { ...newCondition(), value: "", negated };
  const value = raw[valueField[raw.fact]];
  return {
    kind: "condition",
    id: newId(),
    fact: raw.fact,
    value: typeof value === "string" ? value : "",
    negated,
  };
}

/**
 * Reads one wire node. A document the server would refuse is kept as close as
 * the model allows, so the refusal can be shown on the row it concerns: a node
 * of an unknown shape becomes a condition with no value, and a negated group is
 * a group marked `negated`.
 */
function nodeFor(raw: unknown): RuleNode {
  if (!isRecord(raw)) return { ...newCondition(), value: "" };
  if (raw.op === "all" || raw.op === "any") {
    return {
      kind: "group",
      id: newId(),
      op: raw.op,
      children: Array.isArray(raw.children) ? raw.children.map(nodeFor) : [],
    };
  }
  if (raw.op === "not") {
    const inner = nodeFor(raw.child);
    return inner.kind === "group"
      ? { ...inner, negated: true as const }
      : { ...inner, negated: true };
  }
  return leafFor(raw, false);
}

/**
 * Reads a rule document into the builder. A top-level condition that is not a
 * group is wrapped in one, because the builder's outermost list is the
 * document's root combinator.
 */
export function parseRule(rule: AppAccessRule | undefined): RuleGroup {
  if (rule === undefined) {
    return { kind: "group", id: rootGroupId, op: "all", children: [] };
  }
  const node = nodeFor(rule.condition);
  if (node.kind === "group" && node.negated !== true) {
    return { ...node, id: rootGroupId };
  }
  return { kind: "group", id: rootGroupId, op: "all", children: [node] };
}

function wireFor(node: RuleNode): AppAccessCondition {
  const body: AppAccessCondition =
    node.kind === "group"
      ? { op: node.op, children: node.children.map(wireFor) }
      : { fact: node.fact, [valueField[node.fact]]: node.value };
  return node.negated === true ? { op: "not", child: body } : body;
}

export function serializeRule(root: RuleGroup): AppAccessRule {
  return { version: documentVersion, condition: wireFor(root) };
}

export type RuleDocumentRead =
  | { ok: true; root: RuleGroup }
  | { ok: false; problem: "unreadable" | "not-a-document" };

/** Reads the JSON a reader typed or pasted, saying which way it is unusable. */
export function readRuleDocument(text: string): RuleDocumentRead {
  let parsed: unknown;
  try {
    parsed = JSON.parse(text);
  } catch {
    return { ok: false, problem: "unreadable" };
  }
  if (!isRecord(parsed) || !isRecord(parsed.condition)) {
    return { ok: false, problem: "not-a-document" };
  }
  return {
    ok: true,
    root: parseRule({
      version: documentVersion,
      condition: parsed.condition as AppAccessCondition,
    }),
  };
}

/* ---------------------------------------------------------- mutations -- */

function mapNode(
  node: RuleNode,
  id: string,
  change: (node: RuleNode) => RuleNode,
): RuleNode {
  if (node.id === id) return change(node);
  if (node.kind === "condition") return node;
  return {
    ...node,
    children: node.children.map((child) => mapNode(child, id, change)),
  };
}

function mapRoot(
  root: RuleGroup,
  id: string,
  change: (node: RuleNode) => RuleNode,
): RuleGroup {
  return mapNode(root, id, change) as RuleGroup;
}

function append(root: RuleGroup, groupId: string, child: RuleNode): RuleGroup {
  return mapRoot(root, groupId, (node) =>
    node.kind === "group"
      ? { ...node, children: [...node.children, child] }
      : node,
  );
}

export function addCondition(root: RuleGroup, groupId: string): RuleGroup {
  return append(root, groupId, newCondition());
}

export function addGroup(root: RuleGroup, groupId: string): RuleGroup {
  return append(root, groupId, newGroup());
}

function holds(group: RuleGroup, id: string): boolean {
  return group.children.some(
    (child) => child.id === id || (child.kind === "group" && holds(child, id)),
  );
}

function without(group: RuleGroup, id: string): RuleGroup {
  const children: RuleNode[] = [];
  for (const child of group.children) {
    if (child.id === id) continue;
    if (child.kind === "group" && holds(child, id)) {
      const next = without(child, id);
      // A group is never left empty: it goes with its last condition.
      if (next.children.length > 0) children.push(next);
      continue;
    }
    children.push(child);
  }
  return { ...group, children };
}

/**
 * Removes a node. A nested group that loses its last condition goes with it,
 * all the way up; the root stays, empty, and the rule cannot be saved like that.
 */
export function removeNode(root: RuleGroup, id: string): RuleGroup {
  return without(root, id);
}

export function setCombinator(
  root: RuleGroup,
  id: string,
  op: RuleCombinator,
): RuleGroup {
  return mapRoot(root, id, (node) =>
    node.kind === "group" ? { ...node, op } : node,
  );
}

/** Changing the fact starts the value over: the old one means nothing to it. */
export function setFact(
  root: RuleGroup,
  id: string,
  fact: RuleFact,
): RuleGroup {
  return mapRoot(root, id, (node) =>
    node.kind === "condition"
      ? { ...node, fact, value: firstValue(fact) }
      : node,
  );
}

export function setValue(
  root: RuleGroup,
  id: string,
  value: string,
): RuleGroup {
  return mapRoot(root, id, (node) =>
    node.kind === "condition" ? { ...node, value } : node,
  );
}

export function setNegated(
  root: RuleGroup,
  id: string,
  negated: boolean,
): RuleGroup {
  return mapRoot(root, id, (node) =>
    node.kind === "condition" ? { ...node, negated } : node,
  );
}

/* --------------------------------------------------------- validation -- */

/** A server reason code, or `empty_rule` for the one only the builder knows. */
export type RuleIssueCode = string;

export interface RuleIssue {
  /** The row or group the problem is about; `null` for the rule as a whole. */
  nodeId: string | null;
  code: RuleIssueCode;
}

/**
 * Whether a group at this level (the root is 1) may hold another group. The
 * server counts condition rows and the `not` around a negated one as levels, so
 * a new group is only offered while its first condition still sits at level 8
 * or above.
 */
export function canAddGroup(depth: number): boolean {
  return depth + 2 <= ruleLimits.maxDepth;
}

/**
 * Checks a draft against the limits and vocabulary before it is sent. The
 * server stays the authority; this spares a round trip for what the limits make
 * predictable and marks the row at fault. It counts nodes and levels the way the
 * server does, a negation included, and reports each limit once, at the first
 * node that crosses it.
 *
 * `providers` is the catalogue's slugs; without it a provider is only checked
 * for being chosen.
 */
export function checkRule(
  root: RuleGroup,
  providers?: ReadonlySet<string>,
): RuleIssue[] {
  const issues: RuleIssue[] = [];
  let nodes = 0;
  let depthReported = false;
  let nodesReported = false;

  const count = (nodeId: string, depth: number, weight: number) => {
    // A negation is a node and a level of its own, ahead of what it wraps.
    const deepest = depth + weight - 1;
    if (!depthReported && deepest > ruleLimits.maxDepth) {
      depthReported = true;
      issues.push({ nodeId, code: "max_depth_exceeded" });
    }
    nodes += weight;
    if (!nodesReported && nodes > ruleLimits.maxNodes) {
      nodesReported = true;
      issues.push({ nodeId, code: "max_nodes_exceeded" });
    }
  };

  const walk = (node: RuleNode, depth: number) => {
    if (node.kind === "condition") {
      count(node.id, depth, node.negated ? 2 : 1);
      const code = valueProblem(node, providers);
      if (code !== null) issues.push({ nodeId: node.id, code });
      return;
    }
    count(node.id, depth, node.negated === true ? 2 : 1);
    if (node.negated === true) {
      issues.push({ nodeId: node.id, code: "not_requires_fact" });
    }
    if (node.children.length > ruleLimits.maxChildren) {
      issues.push({ nodeId: node.id, code: "max_children_exceeded" });
    }
    if (node.children.length === 0) {
      issues.push(
        node.id === rootGroupId
          ? { nodeId: null, code: "empty_rule" }
          : { nodeId: node.id, code: "empty_children" },
      );
    }
    const childDepth = depth + (node.negated === true ? 2 : 1);
    for (const child of node.children) walk(child, childDepth);
  };

  walk(root, 1);
  return issues;
}

function valueProblem(
  node: RuleCondition,
  providers: ReadonlySet<string> | undefined,
): RuleIssueCode | null {
  if (node.fact === "connection.provider") {
    if (node.value === "") return "missing_provider";
    return providers !== undefined && !providers.has(node.value)
      ? "provider_not_found"
      : null;
  }
  if (fixedValues(node.fact).includes(node.value)) return null;
  return {
    login_method: "invalid_method",
    "connection.protocol": "invalid_protocol",
    avatar: "invalid_source",
  }[node.fact];
}

/* ------------------------------------------------------- path mapping -- */

/**
 * Maps a server path onto the node that produced it, so
 * `$.condition.children[2]` marks the third row of the root instead of the
 * whole document.
 *
 * The walk follows the wire shape: a negated condition is a `not` around the
 * same condition, so the `child` step of a path through it stays on that node.
 */
export function nodeIdAtPath(root: RuleGroup, path: string): string | null {
  const steps = path.match(/\.[A-Za-z_]+|\[\d+\]/g);
  if (steps === null || steps[0] !== ".condition") return null;

  let node: RuleNode = root;
  let rest = steps.slice(1);
  while (rest.length > 0) {
    const [step, ...after] = rest;
    if (step === ".child") {
      rest = after;
      continue;
    }
    if (step !== ".children" || node.kind !== "group") return null;
    const index = after[0];
    if (index === undefined || !index.startsWith("[")) return null;
    const next: RuleNode | undefined =
      node.children[Number(index.slice(1, -1))];
    if (next === undefined) return null;
    node = next;
    rest = after.slice(1);
  }
  return node.id;
}
