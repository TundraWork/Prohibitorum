import {
  Chip,
  Description,
  Label,
  ListBox,
  Radio,
  RadioGroup,
  Select,
  Tabs,
  TextArea,
  TextField,
  ToggleButton,
  Tooltip,
} from "@heroui/react";
import { msg } from "@lingui/core/macro";
import { Plural, Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate, useRouter } from "@tanstack/react-router";
import { Plus, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
import {
  createGroupMutationOptions,
  rulePreviewMutationOptions,
  updateGroupMutationOptions,
} from "@/api/mutations";
import { groupProvidersQueryOptions } from "@/api/queries";
import type {
  AppAccessCondition,
  AppAccessRule,
  AppGroupView,
  CreateGroupRequest,
  ProviderDescriptorView,
  UpdateGroupRequest,
} from "@/api/raw-admin-paths";
import { withRouterSkipLoading } from "@/app/router";
import { Button } from "@/components/custom/Button";
import { ConsoleCard } from "@/components/custom/ConsoleCard";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";

type GroupKind = "manual" | "rule";

type Fact =
  | "connection.provider"
  | "connection.protocol"
  | "login_method"
  | "avatar";

/**
 * The rule editor's own model, kept apart from the wire shape on purpose.
 *
 * What a reader manipulates is a row — one fact, optionally negated — sitting
 * inside a group. The wire shape differs: a `not` holds exactly one leaf, so a
 * negated row serialises as `{op: "not", child: <leaf>}` and has no `children`
 * of its own, while a group is `{op: "all" | "any", children: [...]}`. The two
 * are close enough to convert either way without loss, and this one gives every
 * node an id, so React keys survive an edit that reorders siblings.
 */
interface RuleNode {
  id: string;
  negated: boolean;
  /** Empty on a row, which holds a single fact. Non-empty on a group. */
  children: RuleNode[];
  op: "all" | "any";
  fact: Fact;
  value: string;
}

/** The fact names `appaccess` accepts, with the values each one takes. */
const protocols = ["oidc", "steam", "vrchat"] as const;
const methods = ["passkey", "password_totp", "federation"] as const;
const sources = ["any", "user_uploaded"] as const;

interface FactValues {
  /** The select's options, or null when the value is a free-form provider slug. */
  options: readonly string[] | null;
  read: (condition: AppAccessCondition) => string;
  write: (value: string) => AppAccessCondition;
}

const factValues: Readonly<Record<Fact, FactValues>> = {
  "connection.provider": {
    options: null,
    read: (condition) => condition.provider ?? "",
    write: (value) => ({ fact: "connection.provider", provider: value }),
  },
  "connection.protocol": {
    options: protocols,
    read: (condition) => condition.protocol ?? "",
    write: (value) => ({ fact: "connection.protocol", protocol: value }),
  },
  login_method: {
    options: methods,
    read: (condition) => condition.method ?? "",
    write: (value) => ({ fact: "login_method", method: value }),
  },
  avatar: {
    options: sources,
    read: (condition) => condition.source ?? "",
    write: (value) => ({ fact: "avatar", source: value }),
  },
};

const facts = Object.keys(factValues) as Fact[];

const draftVersion = 1;
const rootId = "root";

let nextId = 0;
function newNode(): RuleNode {
  nextId += 1;
  return {
    id: `n${nextId}`,
    negated: false,
    children: [],
    op: "all",
    fact: "login_method",
    value: methods[0],
  };
}

/** A new leaf always starts on a value the server accepts. */
function newLeaf(fact: Fact = "login_method"): RuleNode {
  const node = newNode();
  node.fact = fact;
  node.value = factValues[fact].options?.[0] ?? "";
  return node;
}

function newGroup(): RuleNode {
  const node = newNode();
  node.children = [newLeaf()];
  return node;
}

function rootTree(): RuleNode {
  const node = newGroup();
  node.id = rootId;
  return node;
}

function isGroup(node: RuleNode): boolean {
  return node.children.length > 0;
}

/* ---------------------------------------------------- wire and tree -- */

function nodeFor(condition: AppAccessCondition | undefined): RuleNode {
  const node = newNode();
  if (condition === undefined) return node;
  node.negated = condition.op === "not";
  const inner = node.negated ? condition.child : condition;
  if (inner === undefined || inner === null) return node;

  if (inner.op === "all" || inner.op === "any") {
    node.op = inner.op;
    node.children = (inner.children ?? []).map(nodeFor);
    return node;
  }
  if (inner.fact !== undefined && Object.hasOwn(factValues, inner.fact)) {
    node.fact = inner.fact as Fact;
    node.value = factValues[inner.fact as Fact].read(inner);
  }
  return node;
}

function wireForNode(node: RuleNode): AppAccessCondition {
  if (isGroup(node)) {
    const branch: AppAccessCondition = {
      op: node.op,
      children: node.children.map(wireForNode),
    };
    // The server rejects a `not` whose child is a combinator, so a negated form
    // row is the only thing that ever produces a `not` — and only around a leaf.
    return node.negated ? { op: "not", child: branch } : branch;
  }
  const leaf = factValues[node.fact].write(node.value);
  return node.negated ? { op: "not", child: leaf } : leaf;
}

function ruleForTree(root: RuleNode): AppAccessRule {
  return { version: draftVersion, condition: wireForNode(root) };
}

/**
 * Reads a rule document back into the editor.
 *
 * A document whose top-level condition is a single row is wrapped in a group,
 * because the editor's outermost row list is the document's root condition and
 * cannot itself be a lone `not`.
 */
function treeForRule(rule: AppAccessRule | undefined): RuleNode {
  if (rule === undefined) return rootTree();
  const condition = nodeFor(rule.condition);
  if (isGroup(condition)) {
    condition.id = rootId;
    return condition;
  }
  const root = rootTree();
  root.children = [condition];
  return root;
}

/* --------------------------------------------------------- validation -- */

/** The bounds `appaccess` enforces on one document. */
const maxDepth = 8;
const maxNodes = 64;
const maxChildren = 32;

type RuleProblem =
  | "too_deep"
  | "too_many_nodes"
  | "too_many_children"
  | "provider_required";

const problemText: Readonly<Record<RuleProblem, ReturnType<typeof msg>>> = {
  too_deep: msg({
    id: "admin.group.rule.problem.too_deep",
    message: "Conditions are nested too deeply. Use at most 8 levels.",
  }),
  too_many_nodes: msg({
    id: "admin.group.rule.problem.too_many_nodes",
    message: "This rule has too many conditions. Use at most 64 in total.",
  }),
  too_many_children: msg({
    id: "admin.group.rule.problem.too_many_children",
    message: "A group can hold at most 32 conditions.",
  }),
  provider_required: msg({
    id: "admin.group.rule.problem.provider_required",
    message: "Choose a provider for every provider condition.",
  }),
};

function nodeCount(node: RuleNode): number {
  return (
    1 + node.children.reduce((total, child) => total + nodeCount(child), 0)
  );
}

/**
 * Checks a draft against the document's limits before it is sent. The server is
 * the authority; this only spares the reader a round trip for the mistakes the
 * limits make predictable, which is why the preview's own failure stays the
 * message that is shown when the two disagree.
 */
function checkTree(root: RuleNode): RuleProblem | null {
  if (nodeCount(root) > maxNodes) return "too_many_nodes";
  let problem: RuleProblem | null = null;
  const walk = (node: RuleNode, depth: number) => {
    if (problem !== null) return;
    if (depth > maxDepth) problem = "too_deep";
    else if (node.children.length > maxChildren) problem = "too_many_children";
    else if (
      !isGroup(node) &&
      node.fact === "connection.provider" &&
      node.value === ""
    )
      problem = "provider_required";
    for (const child of node.children) walk(child, depth + 1);
  };
  walk(root, 1);
  return problem;
}

/* ------------------------------------------------------- path mapping -- */

/**
 * Maps a server-reported JSON path onto the row that produced it, so
 * `$.condition.children[2]` marks the third condition of the root group instead
 * of being reported as a failure of the whole document.
 *
 * The walk follows the wire shape rather than the editor's: a negated row is a
 * `not` wrapped around the same row, so the `child` step on the way to a
 * rejection like `not_requires_fact` resolves back to the row itself.
 */
function nodeIdAtPath(root: RuleNode, path: string): string | null {
  const steps = path.match(/\.[A-Za-z_]+|\[\d+\]/g);
  if (steps === null || steps.length === 0) return null;

  let node: RuleNode | null = root;
  // The path opens with `$.condition`, which the first step names.
  for (const step of steps.slice(1)) {
    if (node === null) return null;
    if (step === ".child") continue;
    if (step === ".children") continue;
    node = node.children[Number(step.slice(1, -1))] ?? null;
  }
  return node?.id ?? null;
}

/** Wording for a server reason code. An unlisted code is shown as it arrives. */
const reasonText: Readonly<Record<string, ReturnType<typeof msg>>> = {
  invalid_shape: msg({
    id: "admin.group.rule.reason.invalid_shape",
    message: "This condition mixes fields that do not belong together.",
  }),
  invalid_op: msg({
    id: "admin.group.rule.reason.invalid_op",
    message: "That group type is not supported.",
  }),
  invalid_fact: msg({
    id: "admin.group.rule.reason.invalid_fact",
    message: "That condition type is not supported.",
  }),
  max_depth_exceeded: msg({
    id: "admin.group.rule.reason.max_depth_exceeded",
    message: "Conditions are nested too deeply. Use at most 8 levels.",
  }),
  max_nodes_exceeded: msg({
    id: "admin.group.rule.reason.max_nodes_exceeded",
    message: "This rule has too many conditions. Use at most 64 in total.",
  }),
  max_children_exceeded: msg({
    id: "admin.group.rule.reason.max_children_exceeded",
    message: "A group can hold at most 32 conditions.",
  }),
  empty_children: msg({
    id: "admin.group.rule.reason.empty_children",
    message: "A group needs at least one condition.",
  }),
  not_requires_fact: msg({
    id: "admin.group.rule.reason.not_requires_fact",
    message: "A negated row must hold one condition, not another group.",
  }),
  missing_provider: msg({
    id: "admin.group.rule.reason.missing_provider",
    message: "Choose a provider for this condition.",
  }),
  provider_not_found: msg({
    id: "admin.group.rule.reason.provider_not_found",
    message: "That provider is not available on this instance.",
  }),
  invalid_protocol: msg({
    id: "admin.group.rule.reason.invalid_protocol",
    message: "That protocol is not supported.",
  }),
  invalid_method: msg({
    id: "admin.group.rule.reason.invalid_method",
    message: "That sign-in method is not supported.",
  }),
  invalid_source: msg({
    id: "admin.group.rule.reason.invalid_source",
    message: "That picture source is not supported.",
  }),
  unsupported_version: msg({
    id: "admin.group.rule.reason.unsupported_version",
    message: "This rule was written for another version of the server.",
  }),
  invalid_json: msg({
    id: "admin.group.rule.reason.invalid_json",
    message: "The document is not valid JSON.",
  }),
  unknown_field: msg({
    id: "admin.group.rule.reason.unknown_field",
    message: "The document has a field the server does not recognise.",
  }),
  trailing_json: msg({
    id: "admin.group.rule.reason.trailing_json",
    message: "The document has content after the rule.",
  }),
  missing_version: msg({
    id: "admin.group.rule.reason.missing_version",
    message: "The document is missing its version.",
  }),
  missing_condition: msg({
    id: "admin.group.rule.reason.missing_condition",
    message: "The document is missing its condition.",
  }),
};

const ruleRejected = msg({
  id: "admin.group.rule.rejected",
  message: "The server did not accept this rule. Check it and try again.",
});

/* ------------------------------------------------------------ helpers -- */

function replaceNode(node: RuleNode, id: string, next: RuleNode): RuleNode {
  if (node.id === id) return next;
  if (node.children.length === 0) return node;
  return {
    ...node,
    children: node.children.map((child) => replaceNode(child, id, next)),
  };
}

function removeNode(node: RuleNode, id: string): RuleNode {
  if (node.children.length === 0) return node;
  return {
    ...node,
    children: node.children
      .filter((child) => child.id !== id)
      .map((child) => removeNode(child, id)),
  };
}

function appendChild(node: RuleNode, id: string, child: RuleNode): RuleNode {
  if (node.id === id) return { ...node, children: [...node.children, child] };
  if (node.children.length === 0) return node;
  return {
    ...node,
    children: node.children.map((entry) => appendChild(entry, id, child)),
  };
}

/* -------------------------------------------------------------- screen -- */

/**
 * One screen for creating a user group and for changing one.
 *
 * The kind is the exception to a single form shape: it is a decision made once
 * — a manual group collects its decisions one account at a time on the group's
 * own page, a rule group is described by a document — and the server rejects
 * `kind` on update, so an existing group shows it as a fact, not a control.
 */
/**
 * Creates a group, or edits `group` when one is given.
 *
 * The group's own values are copied into the form once, on mount. A later
 * refetch is a refresh of a record the reader may already be editing, so it is
 * not allowed to overwrite what they have typed.
 */
export function AdminGroupForm({ group }: { group?: AppGroupView }) {
  const { t } = useLingui();
  const navigate = useNavigate();
  const router = useRouter();
  const queryClient = useQueryClient();
  const editing = group !== undefined;
  const providers = useQuery(groupProvidersQueryOptions());

  const [kind, setKind] = useState<GroupKind>(
    group?.kind === "rule" ? "rule" : "manual",
  );
  const [tree, setTree] = useState<RuleNode>(() =>
    group === undefined ? rootTree() : treeForRule(group.rule),
  );
  const [ruleError, setRuleError] = useState<{
    message: string;
    nodeId: string | null;
  } | null>(null);
  const [matchedCount, setMatchedCount] = useState<number | null>(null);
  const [defaultValues] = useState(() => ({
    displayName: group?.displayName ?? "",
    slug: group?.slug ?? "",
    description: group?.description ?? "",
    exposedToDownstream: group?.exposedToDownstream ?? true,
  }));

  const create = useMutation(createGroupMutationOptions(queryClient));
  const update = useMutation(updateGroupMutationOptions(queryClient));
  const preview = useMutation(rulePreviewMutationOptions());

  const ruleGroup = kind === "rule";
  const pending = create.isPending || update.isPending;

  const form = useAppForm({
    defaultValues,
    onSubmit: async ({ value }) => {
      setRuleError(null);
      const body = {
        slug: value.slug.trim(),
        displayName: value.displayName.trim(),
        description: value.description.trim(),
        exposedToDownstream: value.exposedToDownstream,
      };
      const draft = ruleForTree(tree);
      try {
        if (group === undefined) {
          const request: CreateGroupRequest = { ...body, kind };
          // A rule on a manual group is rejected outright, so it is left off
          // the request rather than sent empty.
          if (ruleGroup) request.rule = draft;
          const saved = await create.mutateAsync(request);
          // The pressed submit button already shows the wait while the new
          // group's page loads, so the form stays up instead of a spinner.
          await withRouterSkipLoading(router, () =>
            navigate({
              to: "/admin/groups/$groupId",
              params: { groupId: String(saved.id) },
            }),
          );
          return;
        }
        const request: UpdateGroupRequest = body;
        // An omitted rule means "keep the current one", so a manual group
        // never sends the document it does not have.
        if (ruleGroup) request.rule = draft;
        await update.mutateAsync({ groupId: group.id, body: request });
        await navigate({ to: "/admin/groups" });
      } catch (error) {
        // `group_slug_conflict` is about one field, so it lands on that field;
        // a rule the server refused is about the document as a whole.
        applyServerError(form, error, {
          locations: {},
          codes: { group_slug_conflict: "slug" },
        });
      }
    },
  });

  const { mutateAsync: previewRule, reset: clearPreview } = preview;

  useEffect(() => {
    // A group that does not exist yet cannot match anyone, so the draft is only
    // checked once it has been created.
    if (!ruleGroup || !editing) return;
    // Typing in one row usually starts several debounced requests; only the
    // newest may report, or an older rejection would overwrite a newer answer.
    let current = true;
    const timer = setTimeout(async () => {
      try {
        const page = await previewRule(ruleForTree(tree));
        if (!current) return;
        setMatchedCount(page.matchedCount);
        setRuleError(null);
      } catch (error) {
        if (!current) return;
        setMatchedCount(null);
        const failure = error as {
          code?: string;
          details?: { path?: string; reason?: string };
        };
        if (failure.code !== "invalid_group_rule") {
          setRuleError({ message: t(ruleRejected), nodeId: null });
          return;
        }
        const reason = failure.details?.reason;
        setRuleError({
          message:
            reason !== undefined && Object.hasOwn(reasonText, reason)
              ? t(reasonText[reason] as ReturnType<typeof msg>)
              : (reason ?? t(ruleRejected)),
          nodeId:
            failure.details?.path === undefined
              ? null
              : nodeIdAtPath(tree, failure.details.path),
        });
      }
    }, 400);
    return () => {
      current = false;
      clearTimeout(timer);
      clearPreview();
    };
  }, [clearPreview, editing, previewRule, ruleGroup, t, tree]);

  return (
    <ConsoleCard
      contentClassName="flex flex-col gap-4"
      title={
        editing ? (
          <Trans id="admin.group.edit.title">User group</Trans>
        ) : (
          <Trans id="admin.group.create.title">New user group</Trans>
        )
      }
    >
      <form.AppForm>
        <form.Form label={t({ id: "admin.group.form", message: "User group" })}>
          <form.FormError />

          {editing ? (
            <div className="flex flex-col gap-1">
              <Label>
                <Trans id="admin.group.kind.label">How it fills</Trans>
              </Label>
              <Chip className="w-fit" color="accent" size="sm" variant="soft">
                {ruleGroup ? (
                  <Trans id="admin.group.kind.rule">Rule</Trans>
                ) : (
                  <Trans id="admin.group.kind.manual">Manual</Trans>
                )}
              </Chip>
              <Description>
                <Trans id="admin.group.kind.fixed">
                  A group's kind is set when it is created and cannot be changed
                  afterwards.
                </Trans>
              </Description>
            </div>
          ) : (
            <RadioGroup
              name="kind"
              value={kind}
              variant="secondary"
              onChange={(value) => setKind(value as GroupKind)}
            >
              <Label>
                <Trans id="admin.group.kind.label">How it fills</Trans>
              </Label>
              <Description>
                <Trans id="admin.group.kind.note">
                  A rule group keeps itself up to date as accounts change. A
                  manual group holds the accounts you allow or deny by hand.
                </Trans>
              </Description>
              <Radio value="manual">
                <Radio.Content>
                  <Radio.Control>
                    <Radio.Indicator />
                  </Radio.Control>
                  <Trans id="admin.group.kind.manual">Manual</Trans>
                </Radio.Content>
              </Radio>
              <Radio value="rule">
                <Radio.Content>
                  <Radio.Control>
                    <Radio.Indicator />
                  </Radio.Control>
                  <Trans id="admin.group.kind.rule">Rule</Trans>
                </Radio.Content>
              </Radio>
            </RadioGroup>
          )}

          <form.AppField
            name="displayName"
            validators={{
              onChange: ({ value }) =>
                value.trim() === ""
                  ? msg({
                      id: "admin.group.display-name.required",
                      message: "Give the group a name.",
                    })
                  : undefined,
            }}
          >
            {(field) => (
              <field.FormField
                label={<Trans id="admin.group.display-name">Name</Trans>}
                description={
                  <Trans id="admin.group.display-name.hint">
                    What this group is called in the console.
                  </Trans>
                }
                autoComplete="off"
                variant="secondary"
              />
            )}
          </form.AppField>

          <form.AppField
            name="slug"
            validators={{
              onChange: ({ value }) => {
                const slug = value.trim();
                if (slug === "") {
                  return msg({
                    id: "admin.group.slug.required",
                    message: "Give the group an identifier.",
                  });
                }
                // Mirrors `validateAppGroupSlug`: lowercase letters, digits and
                // single hyphens, at most 64 characters. The server answers a
                // malformed slug with a bare `bad_request`, which says nothing
                // useful, so the shape is stated here instead.
                return /^[a-z0-9](-?[a-z0-9])*$/.test(slug) && slug.length <= 64
                  ? undefined
                  : msg({
                      id: "admin.group.slug.format",
                      message:
                        "Use lowercase letters, digits and hyphens, up to 64 characters.",
                    });
              },
            }}
          >
            {(field) => (
              <field.FormField
                label={<Trans id="admin.group.slug">Identifier</Trans>}
                description={
                  <Trans id="admin.group.slug.hint">
                    Lowercase letters, digits and hyphens. Applications name
                    this group by it.
                  </Trans>
                }
                autoComplete="off"
                autoCapitalize="none"
                spellCheck={false}
                variant="secondary"
              />
            )}
          </form.AppField>

          <form.AppField name="description">
            {(field) => (
              <field.TextAreaField
                label={<Trans id="admin.group.description">Description</Trans>}
                description={
                  <Trans id="admin.group.description.hint">
                    Optional. What this group is for, for whoever reads it next.
                  </Trans>
                }
                rows={3}
                variant="secondary"
              />
            )}
          </form.AppField>

          <form.AppField name="exposedToDownstream">
            {(field) => (
              <field.SwitchField
                label={
                  <Trans id="admin.group.exposed">Offer to applications</Trans>
                }
                description={
                  <Trans id="admin.group.exposed.hint">
                    Applications can select this group for the access they
                    grant.
                  </Trans>
                }
              />
            )}
          </form.AppField>

          {ruleGroup ? (
            <RuleEditor
              draft={ruleForTree(tree)}
              error={ruleError}
              matchedCount={matchedCount}
              onChange={setTree}
              pending={pending}
              providers={providers.data ?? []}
              root={tree}
            />
          ) : (
            <Description>
              <Trans id="admin.group.manual.note">
                Which accounts this group holds is decided on the group's own
                page, once it has been created.
              </Trans>
            </Description>
          )}

          <div className="flex flex-wrap gap-2">
            <Button
              isDisabled={pending}
              variant="secondary"
              onPress={() => void navigate({ to: "/admin/groups" })}
            >
              <Trans id="admin.group.cancel">Cancel</Trans>
            </Button>
            <form.SubmitButton>
              {editing ? (
                <Trans id="admin.group.save">Save changes</Trans>
              ) : (
                <Trans id="admin.group.create">Create group</Trans>
              )}
            </form.SubmitButton>
          </div>
        </form.Form>
      </form.AppForm>
    </ConsoleCard>
  );
}

/**
 * The rule document, edited two ways over one draft.
 *
 * The structured view is the single source of truth. The JSON panel renders
 * from the same tree every time it is shown and writes back only when its text
 * parses into a condition, so a half-typed document never replaces an editor
 * the reader can still see. Both panels stay mounted, which is what lets the
 * text survive a trip through the other view.
 */
function RuleEditor({
  root,
  draft,
  providers,
  error,
  matchedCount,
  pending,
  onChange,
}: {
  root: RuleNode;
  draft: AppAccessRule;
  providers: readonly ProviderDescriptorView[];
  error: { message: string; nodeId: string | null } | null;
  matchedCount: number | null;
  pending: boolean;
  onChange: (root: RuleNode) => void;
}) {
  const { t } = useLingui();
  const [view, setView] = useState<"form" | "json">("form");
  const [document, setDocument] = useState(() =>
    JSON.stringify(draft, null, 2),
  );
  const [documentError, setDocumentError] = useState<string | null>(null);

  // The text follows the draft rather than being edited in place, so switching
  // views always shows the document that is about to be saved.
  useEffect(() => {
    setDocument(JSON.stringify(draft, null, 2));
  }, [draft]);

  const applyDocument = () => {
    let parsed: unknown;
    try {
      parsed = JSON.parse(document);
    } catch {
      setDocumentError(
        t({
          id: "admin.group.rule.json.unreadable",
          message:
            "This is not valid JSON. The form view still holds your rule.",
        }),
      );
      return;
    }
    if (
      typeof parsed !== "object" ||
      parsed === null ||
      !("condition" in parsed) ||
      typeof parsed.condition !== "object" ||
      parsed.condition === null
    ) {
      setDocumentError(
        t({
          id: "admin.group.rule.json.nodocument",
          message:
            "A rule document needs a version and a condition. The form view still holds your rule.",
        }),
      );
      return;
    }
    setDocumentError(null);
    const candidate = parsed as AppAccessRule;
    onChange(
      treeForRule({
        version: candidate.version,
        condition: candidate.condition,
      }),
    );
  };

  const problem = checkTree(root);

  return (
    <div className="flex flex-col gap-3 rounded-medium border border-separator p-4">
      <div className="flex flex-col gap-1">
        <Label>
          <Trans id="admin.group.rule.label">Who the rule matches</Trans>
        </Label>
        <Description>
          <Trans id="admin.group.rule.hint">
            An account joins this group while the rule matches it. Rules are
            checked whenever access is asked for, so the group stays current.
          </Trans>
        </Description>
      </div>

      <Tabs
        className="w-full"
        selectedKey={view}
        onSelectionChange={(key) => setView(key as "form" | "json")}
      >
        <Tabs.ListContainer className="w-fit max-w-full">
          <Tabs.List
            aria-label={t({
              id: "admin.group.rule.views",
              message: "Rule editor",
            })}
          >
            <Tabs.Tab className="whitespace-nowrap" id="form">
              <Trans id="admin.group.rule.view.form">Conditions</Trans>
              <Tabs.Indicator />
            </Tabs.Tab>
            <Tabs.Tab className="whitespace-nowrap" id="json">
              <Trans id="admin.group.rule.view.json">JSON</Trans>
              <Tabs.Indicator />
            </Tabs.Tab>
          </Tabs.List>
        </Tabs.ListContainer>

        <Tabs.Panel className="pt-4" id="form">
          <div className="flex flex-col gap-3">
            {root.children.map((child) => (
              <ConditionNode
                key={child.id}
                depth={2}
                errorNodeId={error?.nodeId ?? null}
                node={child}
                pending={pending}
                providers={providers}
                root={root}
                onChange={onChange}
              />
            ))}
            <div className="flex flex-wrap gap-2">
              <Button
                isDisabled={pending}
                size="sm"
                variant="secondary"
                onPress={() => onChange(appendChild(root, root.id, newLeaf()))}
              >
                <Plus size={16} aria-hidden="true" />
                <Trans id="admin.group.rule.add">Add condition</Trans>
              </Button>
              <Button
                isDisabled={pending}
                size="sm"
                variant="secondary"
                onPress={() => onChange(appendChild(root, root.id, newGroup()))}
              >
                <Plus size={16} aria-hidden="true" />
                <Trans id="admin.group.rule.addGroup">Add group</Trans>
              </Button>
            </div>
          </div>
        </Tabs.Panel>

        <Tabs.Panel className="pt-4" id="json">
          <TextField
            isInvalid={documentError !== null}
            value={document}
            onChange={setDocument}
            onBlur={applyDocument}
          >
            <TextArea
              aria-label={t({
                id: "admin.group.rule.json.label",
                message: "Rule document",
              })}
              className="min-h-64 font-mono text-xs"
              rows={16}
              spellCheck={false}
              variant="secondary"
            />
            <Description>
              <Trans id="admin.group.rule.json.hint">
                Edited on blur: leave the box and the form view picks up what
                the document says.
              </Trans>
            </Description>
          </TextField>
          {documentError !== null && (
            <span className="mt-2 block text-sm text-danger" role="alert">
              {documentError}
            </span>
          )}
        </Tabs.Panel>
      </Tabs>

      {problem !== null && (
        <span className="text-sm text-danger" role="alert">
          {t(problemText[problem])}
        </span>
      )}
      {error !== null && (
        <span className="text-sm text-danger" role="alert">
          {error.message}
        </span>
      )}
      {error === null && problem === null && matchedCount !== null && (
        <p className="text-sm text-muted" role="status">
          <Plural
            id="admin.group.rule.matched"
            value={matchedCount}
            one="# account matches this rule"
            other="# accounts match this rule"
          />
        </p>
      )}
    </div>
  );
}

/**
 * One row of the editor: a single condition, or a group of them.
 *
 * A group is drawn as a bordered column so nesting is visible, and a row's
 * "Not" control is offered only on a single condition — a `not` around a
 * combinator is what the server rejects, so the control that would produce one
 * is never drawn.
 */
function ConditionNode({
  node,
  root,
  providers,
  depth,
  errorNodeId,
  pending,
  onChange,
}: {
  node: RuleNode;
  root: RuleNode;
  providers: readonly ProviderDescriptorView[];
  depth: number;
  errorNodeId: string | null;
  pending: boolean;
  onChange: (root: RuleNode) => void;
}) {
  const { t } = useLingui();
  const invalid = errorNodeId === node.id;
  const update = (patch: Partial<RuleNode>) =>
    onChange(replaceNode(root, node.id, { ...node, ...patch }));
  const remove = () => onChange(removeNode(root, node.id));

  if (isGroup(node)) {
    return (
      <div
        className={`flex flex-col gap-3 rounded-medium border p-3 ${
          invalid ? "border-danger" : "border-separator"
        }`}
      >
        <div className="flex flex-wrap items-end gap-2">
          <Select
            className="w-40"
            isDisabled={pending}
            value={node.op}
            variant="secondary"
            onChange={(key) => update({ op: key as "all" | "any" })}
          >
            <Label>
              <Trans id="admin.group.rule.group.op">Match</Trans>
            </Label>
            <Select.Trigger>
              <Select.Value />
              <Select.Indicator />
            </Select.Trigger>
            <Select.Popover>
              <ListBox>
                <ListBox.Item id="all" textValue="all">
                  <Trans id="admin.group.rule.group.all">All of these</Trans>
                  <ListBox.ItemIndicator />
                </ListBox.Item>
                <ListBox.Item id="any" textValue="any">
                  <Trans id="admin.group.rule.group.any">Any of these</Trans>
                  <ListBox.ItemIndicator />
                </ListBox.Item>
              </ListBox>
            </Select.Popover>
          </Select>
          <Button
            isIconOnly
            aria-label={t({
              id: "admin.group.rule.remove",
              message: "Remove this condition",
            })}
            isDisabled={pending}
            size="sm"
            variant="danger-soft"
            onPress={remove}
          >
            <Trash2 size={16} aria-hidden="true" />
          </Button>
        </div>

        <div className="flex flex-col gap-3 border-separator border-s ps-4">
          {node.children.map((child) => (
            <ConditionNode
              key={child.id}
              depth={depth + 1}
              errorNodeId={errorNodeId}
              node={child}
              pending={pending}
              providers={providers}
              root={root}
              onChange={onChange}
            />
          ))}
        </div>

        <div className="flex flex-wrap gap-2">
          <Button
            isDisabled={pending}
            size="sm"
            variant="secondary"
            onPress={() => onChange(appendChild(root, node.id, newLeaf()))}
          >
            <Plus size={16} aria-hidden="true" />
            <Trans id="admin.group.rule.add">Add condition</Trans>
          </Button>
          {/* The document is bounded at 8 levels, so the control that would go
              past the limit is not drawn on the last one. */}
          {depth < maxDepth && (
            <Button
              isDisabled={pending}
              size="sm"
              variant="secondary"
              onPress={() => onChange(appendChild(root, node.id, newGroup()))}
            >
              <Plus size={16} aria-hidden="true" />
              <Trans id="admin.group.rule.addGroup">Add group</Trans>
            </Button>
          )}
        </div>
      </div>
    );
  }

  const fact = factValues[node.fact];
  return (
    <div className="flex flex-wrap items-end gap-2">
      <Select
        className="w-52"
        isDisabled={pending}
        value={node.fact}
        variant="secondary"
        onChange={(key) => {
          const next = key as Fact;
          update({
            fact: next,
            value: factValues[next].options?.[0] ?? "",
          });
        }}
      >
        <Label>
          <Trans id="admin.group.rule.fact">Condition</Trans>
        </Label>
        <Select.Trigger>
          <Select.Value />
          <Select.Indicator />
        </Select.Trigger>
        <Select.Popover>
          <ListBox>
            {facts.map((entry) => (
              <ListBox.Item id={entry} key={entry} textValue={entry}>
                {entry}
                <ListBox.ItemIndicator />
              </ListBox.Item>
            ))}
          </ListBox>
        </Select.Popover>
      </Select>

      {fact.options === null ? (
        <Select
          className="w-56"
          isDisabled={pending}
          placeholder={t({
            id: "admin.group.rule.provider.placeholder",
            message: "Choose a provider",
          })}
          value={node.value === "" ? null : node.value}
          variant="secondary"
          onChange={(key) => update({ value: String(key ?? "") })}
        >
          <Label>
            <Trans id="admin.group.rule.value">Value</Trans>
          </Label>
          <Select.Trigger>
            <Select.Value />
            <Select.Indicator />
          </Select.Trigger>
          <Select.Popover>
            <ListBox>
              {providers.map((provider) => (
                <ListBox.Item
                  id={provider.slug}
                  key={provider.slug}
                  textValue={provider.displayName}
                >
                  {provider.displayName}
                  <ListBox.ItemIndicator />
                </ListBox.Item>
              ))}
            </ListBox>
          </Select.Popover>
        </Select>
      ) : (
        <Select
          className="w-56"
          isDisabled={pending}
          value={node.value}
          variant="secondary"
          onChange={(key) => update({ value: String(key ?? "") })}
        >
          <Label>
            <Trans id="admin.group.rule.value">Value</Trans>
          </Label>
          <Select.Trigger>
            <Select.Value />
            <Select.Indicator />
          </Select.Trigger>
          <Select.Popover>
            <ListBox>
              {fact.options.map((option) => (
                <ListBox.Item id={option} key={option} textValue={option}>
                  {option}
                  <ListBox.ItemIndicator />
                </ListBox.Item>
              ))}
            </ListBox>
          </Select.Popover>
        </Select>
      )}

      <Tooltip delay={0}>
        <Tooltip.Trigger>
          <ToggleButton
            isDisabled={pending}
            isSelected={node.negated}
            size="sm"
            variant="ghost"
            onChange={(selected) => update({ negated: selected })}
          >
            <Trans id="admin.group.rule.not">Not</Trans>
          </ToggleButton>
        </Tooltip.Trigger>
        <Tooltip.Content>
          <Trans id="admin.group.rule.not.hint">
            Match accounts this condition does not describe.
          </Trans>
        </Tooltip.Content>
      </Tooltip>

      <Button
        isIconOnly
        aria-label={t({
          id: "admin.group.rule.remove",
          message: "Remove this condition",
        })}
        isDisabled={pending}
        size="sm"
        variant="danger-soft"
        onPress={remove}
      >
        <Trash2 size={16} aria-hidden="true" />
      </Button>
    </div>
  );
}
