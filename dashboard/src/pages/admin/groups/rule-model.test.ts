import { describe, expect, it } from "vitest";
import type { AppAccessRule } from "@/api/raw-admin-paths";
import {
  addCondition,
  addGroup,
  canAddGroup,
  checkRule,
  nodeIdAtPath,
  parseRule,
  type RuleCondition,
  type RuleGroup,
  type RuleNode,
  readRuleDocument,
  removeNode,
  rootGroupId,
  ruleLimits,
  serializeRule,
  setCombinator,
  setFact,
  setNegated,
  setValue,
} from "@/pages/admin/groups/rule-model";

const passkey = { fact: "login_method", method: "passkey" };
const github = { fact: "connection.provider", provider: "github" };

function document(condition: AppAccessRule["condition"]): AppAccessRule {
  return { version: 1, condition };
}

function ids(node: RuleNode): string[] {
  return node.kind === "group"
    ? [node.id, ...node.children.flatMap(ids)]
    : [node.id];
}

function group(node: RuleNode | undefined): RuleGroup {
  if (node?.kind !== "group") throw new Error("expected a group");
  return node;
}

function condition(node: RuleNode | undefined): RuleCondition {
  if (node?.kind !== "condition") throw new Error("expected a condition");
  return node;
}

/** A rule whose innermost group sits `levels` groups under the root. */
function nested(levels: number): RuleGroup {
  let root = parseRule(document({ op: "all", children: [passkey] }));
  let parent: RuleGroup = root;
  for (let level = 0; level < levels; level += 1) {
    root = addGroup(root, parent.id);
    parent = group(findGroup(root, parent.id).children.at(-1));
  }
  return root;
}

function findGroup(root: RuleGroup, id: string): RuleGroup {
  if (root.id === id) return root;
  for (const child of root.children) {
    if (child.kind !== "group") continue;
    try {
      return findGroup(child, id);
    } catch {
      // Not under this child.
    }
  }
  throw new Error(`no group ${id}`);
}

function deepest(root: RuleGroup): RuleGroup {
  const last = root.children.at(-1);
  return last?.kind === "group" ? deepest(last) : root;
}

describe("parsing and serialising", () => {
  const stored = document({
    op: "all",
    children: [
      passkey,
      { op: "not", child: github },
      {
        op: "any",
        children: [
          { fact: "connection.protocol", protocol: "steam" },
          { fact: "avatar", source: "user_uploaded" },
        ],
      },
    ],
  });

  it("round-trips a nested document without changing it", () => {
    expect(serializeRule(parseRule(stored))).toEqual(stored);
  });

  it("reads a negated leaf as one condition and gives every node its own id", () => {
    const root = parseRule(stored);

    expect(root.id).toBe(rootGroupId);
    const negated = condition(root.children[1]);
    expect(negated).toMatchObject({
      fact: "connection.provider",
      value: "github",
      negated: true,
    });
    const all = ids(root);
    expect(new Set(all).size).toBe(all.length);
  });

  it("wraps a single leaf, or a negated one, in a root all", () => {
    expect(serializeRule(parseRule(document(passkey))).condition).toEqual({
      op: "all",
      children: [passkey],
    });
    expect(
      serializeRule(parseRule(document({ op: "not", child: github })))
        .condition,
    ).toEqual({ op: "all", children: [{ op: "not", child: github }] });
  });

  it("reads no stored rule as a root with no conditions", () => {
    const root = parseRule(undefined);
    expect(root.op).toBe("all");
    expect(root.children).toEqual([]);
  });

  it("keeps a document the server would refuse, so the refusal can be shown on its row", () => {
    const root = parseRule(
      document({
        op: "all",
        children: [
          { fact: "nonsense" },
          { op: "xor", children: [passkey] },
          { op: "not", child: { op: "any", children: [passkey] } },
        ],
      }),
    );

    expect(condition(root.children[0])).toMatchObject({
      fact: "login_method",
      value: "",
    });
    expect(condition(root.children[1])).toMatchObject({ value: "" });
    expect(group(root.children[2])).toMatchObject({ negated: true });
  });
});

describe("reading a pasted document", () => {
  it("reports text that is not JSON", () => {
    expect(readRuleDocument("{")).toEqual({ ok: false, problem: "unreadable" });
  });

  it.each([
    "[]",
    "null",
    "3",
    '{"version":1}',
    '{"version":1,"condition":null}',
    '{"version":1,"condition":3}',
  ])("reports %s as not being a rule document", (text) => {
    expect(readRuleDocument(text)).toEqual({
      ok: false,
      problem: "not-a-document",
    });
  });

  it.each([
    { fact: "nonsense" },
    { method: "x" },
    { op: "xor", children: [passkey] },
    { op: "all", children: [passkey, { fact: "geo", country: "NZ" }] },
    { op: "not", child: { fact: "geo" } },
    { op: "not" },
  ])("refuses a condition it has no row for: %j", (condition) => {
    expect(readRuleDocument(JSON.stringify({ version: 1, condition }))).toEqual(
      { ok: false, problem: "unknown-condition" },
    );
  });

  it("reads a document and wraps a single leaf in a root all", () => {
    const read = readRuleDocument(
      JSON.stringify({ version: 1, condition: passkey }),
    );

    expect(read.ok).toBe(true);
    if (read.ok) {
      expect(serializeRule(read.root).condition).toEqual({
        op: "all",
        children: [passkey],
      });
    }
  });
});

describe("editing the tree", () => {
  const base = () =>
    parseRule(
      document({
        op: "all",
        children: [passkey, { op: "any", children: [github] }],
      }),
    );

  it("adds a condition that starts on a passkey, and a group that holds one", () => {
    const root = base();
    const inner = group(root.children[1]);

    const withCondition = addCondition(root, inner.id);
    const withGroup = addGroup(root, root.id);

    expect(group(withCondition.children[1]).children).toHaveLength(2);
    expect(
      condition(group(withCondition.children[1]).children[1]),
    ).toMatchObject({ fact: "login_method", value: "passkey", negated: false });
    const added = group(withGroup.children[2]);
    expect(added).toMatchObject({ op: "all" });
    expect(added.children).toHaveLength(1);
  });

  it("leaves the tree it was given untouched", () => {
    const root = base();
    const before = JSON.stringify(root);
    addCondition(root, root.id);
    removeNode(root, root.children[0]?.id ?? "");
    expect(JSON.stringify(root)).toBe(before);
  });

  it("removes a condition", () => {
    const root = base();
    const next = removeNode(root, root.children[0]?.id ?? "");
    expect(next.children).toHaveLength(1);
  });

  it("removes a nested group with its last condition instead of turning it into a condition", () => {
    const root = base();
    const inner = group(root.children[1]);

    const next = removeNode(root, inner.children[0]?.id ?? "");

    expect(next.children).toHaveLength(1);
    expect(next.children[0]?.kind).toBe("condition");
  });

  it("removes every emptied group on the way up", () => {
    let root = parseRule(document({ op: "all", children: [passkey] }));
    root = addGroup(root, root.id);
    const outer = group(root.children[1]);
    root = addGroup(root, outer.id);
    const inner = group(group(root.children[1]).children[1]);
    // Take the outer group's first condition away, so the inner group is all it holds.
    root = removeNode(root, group(root.children[1]).children[0]?.id ?? "");

    const next = removeNode(root, inner.children[0]?.id ?? "");

    expect(next.children).toHaveLength(1);
    expect(next.children[0]?.kind).toBe("condition");
  });

  it("keeps the root when its last condition goes, leaving a rule with no conditions", () => {
    const root = parseRule(document({ op: "all", children: [passkey] }));
    const next = removeNode(root, root.children[0]?.id ?? "");
    expect(next.id).toBe(rootGroupId);
    expect(next.children).toEqual([]);
  });

  it("does not drop a group that was already empty when something else is removed", () => {
    const root = parseRule(
      document({
        op: "all",
        children: [passkey, { op: "any", children: [] }],
      }),
    );
    const next = removeNode(root, root.children[0]?.id ?? "");
    expect(next.children).toHaveLength(1);
    expect(next.children[0]?.kind).toBe("group");
  });

  it("changes a combinator, a fact, a value and a negation", () => {
    let root = base();
    const first = root.children[0]?.id ?? "";
    const inner = group(root.children[1]);

    root = setCombinator(root, inner.id, "all");
    root = setNegated(root, first, true);
    root = setFact(root, first, "connection.provider");
    expect(group(root.children[1]).op).toBe("all");
    expect(condition(root.children[0])).toMatchObject({
      fact: "connection.provider",
      value: "",
      negated: true,
    });

    root = setValue(root, first, "github");
    expect(condition(root.children[0]).value).toBe("github");
    root = setFact(root, first, "connection.protocol");
    expect(condition(root.children[0]).value).toBe("oidc");
  });
});

describe("checking a rule against the limits", () => {
  const known = new Set(["github"]);

  it("finds nothing wrong with a sound rule", () => {
    const root = parseRule(
      document({
        op: "all",
        children: [passkey, { op: "not", child: github }],
      }),
    );
    expect(checkRule(root, known)).toEqual([]);
  });

  it("says a rule with no conditions cannot be saved, on no row", () => {
    expect(checkRule(parseRule(undefined))).toEqual([
      { nodeId: null, code: "empty_rule" },
    ]);
  });

  it("marks a provider condition with no provider", () => {
    const root = parseRule(document({ op: "all", children: [github] }));
    const next = setValue(root, root.children[0]?.id ?? "", "");
    expect(checkRule(next)).toEqual([
      { nodeId: root.children[0]?.id, code: "missing_provider" },
    ]);
  });

  it("marks a provider the catalogue does not hold, only once the catalogue is known", () => {
    const root = parseRule(
      document({
        op: "all",
        children: [{ fact: "connection.provider", provider: "gone" }],
      }),
    );
    expect(checkRule(root)).toEqual([]);
    expect(checkRule(root, known)).toEqual([
      { nodeId: root.children[0]?.id, code: "provider_not_found" },
    ]);
  });

  it("marks a value the fact does not take", () => {
    const root = parseRule(
      document({
        op: "all",
        children: [
          { fact: "login_method", method: "sms" },
          { fact: "connection.protocol", protocol: "ldap" },
          { fact: "avatar", source: "gravatar" },
        ],
      }),
    );
    expect(checkRule(root).map((issue) => issue.code)).toEqual([
      "invalid_method",
      "invalid_protocol",
      "invalid_source",
    ]);
  });

  it("marks a group with nothing in it, and one the server cannot read a negation of", () => {
    const root = parseRule(
      document({
        op: "all",
        children: [
          passkey,
          { op: "any", children: [] },
          { op: "not", child: { op: "any", children: [passkey] } },
        ],
      }),
    );
    expect(checkRule(root)).toEqual([
      { nodeId: root.children[1]?.id, code: "empty_children" },
      { nodeId: root.children[2]?.id, code: "not_requires_fact" },
    ]);
  });

  it("allows 32 conditions in a group and marks the group that holds 33", () => {
    const leaves = (count: number) =>
      Array.from({ length: count }, () => passkey);
    const fits = parseRule(
      document({ op: "all", children: leaves(ruleLimits.maxChildren) }),
    );
    const over = parseRule(
      document({ op: "all", children: leaves(ruleLimits.maxChildren + 1) }),
    );
    expect(checkRule(fits)).toEqual([]);
    expect(checkRule(over)).toEqual([
      { nodeId: rootGroupId, code: "max_children_exceeded" },
    ]);
  });

  it("counts a negation as a node of its own, as the server does", () => {
    // The root and 31 negated conditions are 1 + 62 nodes; a group of two adds three more.
    const negated = { op: "not", child: passkey };
    const fits = parseRule(
      document({
        op: "all",
        children: [...Array.from({ length: 31 }, () => negated)],
      }),
    );
    const over = parseRule(
      document({
        op: "all",
        children: [
          ...Array.from({ length: 31 }, () => negated),
          { op: "all", children: [passkey, passkey] },
        ],
      }),
    );
    expect(checkRule(fits)).toEqual([]);
    const issues = checkRule(over);
    expect(issues).toHaveLength(1);
    expect(issues[0]?.code).toBe("max_nodes_exceeded");
    expect(issues[0]?.nodeId).not.toBeNull();
  });

  it("marks the first node that sits below depth 8", () => {
    // The root is level 1, six groups sit below it, and the innermost holds a condition at level 8.
    const fits = nested(6);
    expect(checkRule(fits)).toEqual([]);

    const over = addGroup(fits, deepest(fits).id);

    const added = group(deepest(over));
    expect(checkRule(over)).toEqual([
      { nodeId: added.children[0]?.id, code: "max_depth_exceeded" },
    ]);
  });

  it("counts the not around a condition as a level", () => {
    const root = nested(6);
    const inner = deepest(root);
    const leaf = condition(inner.children[0]);
    expect(checkRule(root)).toEqual([]);

    const negated = setNegated(root, leaf.id, true);

    expect(checkRule(negated)).toEqual([
      { nodeId: leaf.id, code: "max_depth_exceeded" },
    ]);
  });
});

describe("how deep a group may go", () => {
  it("lets a group take another only while that group's first condition stays within 8 levels", () => {
    expect(canAddGroup(1)).toBe(true);
    expect(canAddGroup(6)).toBe(true);
    expect(canAddGroup(7)).toBe(false);
    expect(canAddGroup(8)).toBe(false);
  });

  it("agrees with the limit the check enforces", () => {
    // Walk down adding a group wherever the rule says one may go: never over the limit.
    let root = parseRule(document({ op: "all", children: [passkey] }));
    let depth = 1;
    while (canAddGroup(depth)) {
      root = addGroup(root, deepest(root).id);
      depth += 1;
    }
    expect(depth).toBe(7);
    expect(checkRule(root)).toEqual([]);
  });
});

describe("finding the node a server path names", () => {
  const root = parseRule(
    document({
      op: "all",
      children: [
        passkey,
        { op: "not", child: github },
        { op: "any", children: [passkey, github] },
      ],
    }),
  );
  const [first, second, third] = root.children;
  const inner = group(third);

  it("names the root for the condition itself", () => {
    expect(nodeIdAtPath(root, "$.condition")).toBe(rootGroupId);
  });

  it("follows child indexes", () => {
    expect(nodeIdAtPath(root, "$.condition.children[0]")).toBe(first?.id);
    expect(nodeIdAtPath(root, "$.condition.children[2]")).toBe(third?.id);
    expect(nodeIdAtPath(root, "$.condition.children[2].children[1]")).toBe(
      inner.children[1]?.id,
    );
  });

  it("resolves the child of a not back to the negated condition", () => {
    expect(nodeIdAtPath(root, "$.condition.children[1].child")).toBe(
      second?.id,
    );
    expect(nodeIdAtPath(root, "$.condition.children[1]")).toBe(second?.id);
  });

  it("names nothing for the document as a whole or for a path that is not there", () => {
    expect(nodeIdAtPath(root, "$")).toBeNull();
    expect(nodeIdAtPath(root, "$.condition.children[9]")).toBeNull();
    expect(
      nodeIdAtPath(root, "$.condition.children[0].children[0]"),
    ).toBeNull();
    expect(nodeIdAtPath(root, "nonsense")).toBeNull();
    expect(nodeIdAtPath(root, "")).toBeNull();
  });
});
