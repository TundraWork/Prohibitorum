import { Trans, useLingui } from "@lingui/react/macro";
import { PlusIcon, XIcon } from "lucide-react";
import { createContext, useContext, useId } from "react";
import { Button } from "@/components/custom/Button";
import { SegmentedControl } from "@/components/custom/SegmentedControl";
import {
  Select,
  SelectItem,
  SelectPopup,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { cn } from "@/lib/utils";
import {
  factLabels,
  issueMessage,
  valueLabel,
} from "@/pages/admin/groups/rule-labels";
import {
  addCondition,
  addGroup,
  canAddGroup,
  fixedValues,
  type RuleCombinator,
  type RuleCondition,
  type RuleFact,
  type RuleGroup,
  type RuleIssue,
  type RuleNode,
  removeNode,
  ruleFacts,
  ruleLimits,
  setCombinator,
  setFact,
  setNegated,
  setValue,
} from "@/pages/admin/groups/rule-model";

/** One provider the rule can name, with the state the picker notes beside it. */
export interface RuleProvider {
  slug: string;
  displayName: string;
  /** Either state keeps the provider usable in a rule, so both are only noted. */
  disabled?: boolean;
  signInOff?: boolean;
}

/**
 * Whether the provider list has arrived. Until it has, a saved provider is only
 * a slug: calling it unknown would be a claim nothing has checked.
 */
export type RuleProvidersState = "loading" | "failed" | "loaded";

const ProvidersStateContext = createContext<RuleProvidersState>("loaded");

/** What the builder needs to draw problems on the rows they belong to. */
type IssuesByNode = ReadonlyMap<string, readonly string[]>;

function groupIssues(issues: readonly RuleIssue[]): IssuesByNode {
  const byNode = new Map<string, string[]>();
  for (const issue of issues) {
    if (issue.nodeId === null) continue;
    const codes = byNode.get(issue.nodeId) ?? [];
    if (!codes.includes(issue.code)) codes.push(issue.code);
    byNode.set(issue.nodeId, codes);
  }
  return byNode;
}

/** Codes about the fact itself. */
const factProblems = new Set(["invalid_shape", "invalid_fact"]);

/**
 * Codes about where a row sits in the rule, not about what it says: the row is
 * marked as a whole, because neither its fact nor its value is wrong. Every
 * other code is about the value.
 */
const rowProblems = new Set([
  "max_depth_exceeded",
  "max_nodes_exceeded",
  "max_children_exceeded",
]);

/**
 * The rule as an editor: a sentence of conditions inside any/all groups.
 *
 * It is controlled and holds no state of its own, so the group page and the
 * create page both draw it over a tree they own. `issues` are drawn on the row
 * or group each names, the root included; the prompt an empty rule shows is the
 * builder's own.
 */
export function RuleBuilder({
  value,
  onChange,
  issues = [],
  providers,
  providersState = "loaded",
  disabled = false,
}: {
  value: RuleGroup;
  onChange: (next: RuleGroup) => void;
  issues?: readonly RuleIssue[];
  providers: readonly RuleProvider[];
  providersState?: RuleProvidersState;
  disabled?: boolean;
}) {
  const { i18n } = useLingui();
  const byNode = groupIssues(issues);
  const empty = value.children.length === 0;
  const rootCodes = byNode.get(value.id) ?? [];
  const rootMessageId = useId();

  return (
    <ProvidersStateContext.Provider value={providersState}>
      <div className="grid gap-3">
        <div
          data-invalid={rootCodes.length > 0 || undefined}
          className={cn(
            "min-w-0 text-sm leading-8",
            rootCodes.length > 0 &&
              "-mx-2 rounded-lg px-2 ring-1 ring-destructive/40",
          )}
        >
          <Trans id="admin.group.rule.root">
            Accounts that meet{" "}
            <CombinatorSelect
              value={value.op}
              describedBy={rootCodes.length > 0 ? rootMessageId : undefined}
              disabled={disabled}
              onChange={(op) => onChange(setCombinator(value, value.id, op))}
            />{" "}
            of these
          </Trans>
        </div>
        <Messages id={rootMessageId} codes={rootCodes} />
        {empty && (
          <p className="text-destructive-foreground text-sm">
            {i18n._(issueMessage("empty_rule"))}
          </p>
        )}
        <Children
          group={value}
          depth={1}
          root={value}
          onChange={onChange}
          byNode={byNode}
          providers={providers}
          disabled={disabled}
          overflowMessageId={
            rootCodes.includes("max_children_exceeded")
              ? rootMessageId
              : undefined
          }
        />
      </div>
    </ProvidersStateContext.Provider>
  );
}

function Children({
  group,
  depth,
  root,
  onChange,
  byNode,
  providers,
  disabled,
  overflowMessageId,
}: {
  group: RuleGroup;
  depth: number;
  root: RuleGroup;
  onChange: (next: RuleGroup) => void;
  byNode: IssuesByNode;
  providers: readonly RuleProvider[];
  disabled: boolean;
  /** Where the group already says it holds too many; the limit is not said twice. */
  overflowMessageId?: string;
}) {
  const { t, i18n } = useLingui();
  const reasonId = useId();
  const full = group.children.length >= ruleLimits.maxChildren;
  const mayNest = canAddGroup(depth);
  const conditionBlocked = disabled || full;
  const groupBlocked = disabled || full || !mayNest;
  // A group over its limit says so in its own header, in the error's colour.
  const limitSaid = full && overflowMessageId !== undefined;
  const reasonShown = (full || !mayNest) && !limitSaid;
  const whyBlocked = reasonShown ? reasonId : overflowMessageId;
  return (
    <>
      {group.children.map((child) => (
        <Node
          key={child.id}
          node={child}
          depth={depth + 1}
          root={root}
          onChange={onChange}
          byNode={byNode}
          providers={providers}
          disabled={disabled}
        />
      ))}
      <div className="flex flex-wrap items-center gap-1">
        <Button
          variant="ghost"
          size="xs"
          disabled={conditionBlocked}
          aria-label={t({
            id: "admin.group.rule.add",
            message: "Add condition",
          })}
          aria-describedby={full ? whyBlocked : undefined}
          onClick={() => onChange(addCondition(root, group.id))}
        >
          <PlusIcon aria-hidden="true" />
          <Trans id="admin.group.rule.add-condition">Condition</Trans>
        </Button>
        <Button
          variant="ghost"
          size="xs"
          disabled={groupBlocked}
          aria-label={t({
            id: "admin.group.rule.addGroup",
            message: "Add group",
          })}
          aria-describedby={full || !mayNest ? whyBlocked : undefined}
          onClick={() => onChange(addGroup(root, group.id))}
        >
          <PlusIcon aria-hidden="true" />
          <Trans id="admin.group.rule.add-group">Group</Trans>
        </Button>
        {reasonShown && (
          <span
            id={reasonId}
            className="text-pretty text-muted-foreground text-xs dark:text-foreground/60"
          >
            {full ? (
              i18n._(issueMessage("max_children_exceeded"))
            ) : (
              <Trans id="admin.group.rule.depth-limit">
                Conditions can be nested at most 8 levels deep. No more groups
                can be added here.
              </Trans>
            )}
          </span>
        )}
      </div>
    </>
  );
}

function Node({
  node,
  depth,
  root,
  onChange,
  byNode,
  providers,
  disabled,
}: {
  node: RuleNode;
  depth: number;
  root: RuleGroup;
  onChange: (next: RuleGroup) => void;
  byNode: IssuesByNode;
  providers: readonly RuleProvider[];
  disabled: boolean;
}) {
  return node.kind === "group" ? (
    <Well
      group={node}
      depth={depth}
      root={root}
      onChange={onChange}
      byNode={byNode}
      providers={providers}
      disabled={disabled}
    />
  ) : (
    <ConditionRow
      condition={node}
      root={root}
      onChange={onChange}
      codes={byNode.get(node.id) ?? []}
      providers={providers}
      disabled={disabled}
    />
  );
}

/** A nested group, drawn as a well so its depth reads at a glance. */
function Well({
  group,
  depth,
  root,
  onChange,
  byNode,
  providers,
  disabled,
}: {
  group: RuleGroup;
  depth: number;
  root: RuleGroup;
  onChange: (next: RuleGroup) => void;
  byNode: IssuesByNode;
  providers: readonly RuleProvider[];
  disabled: boolean;
}) {
  const { t } = useLingui();
  const codes = byNode.get(group.id) ?? [];
  const messageId = useId();
  return (
    <fieldset
      aria-describedby={codes.length > 0 ? messageId : undefined}
      className={cn(
        // One tone at every level: the inset ring and the indent carry the
        // nesting. The well reaches into its parent's end padding, so the
        // conditions of every level end on the same line.
        "m-0 -me-1.5 grid min-w-0 gap-2 rounded-xl border-0 p-1.5 ps-2 ring-1 ring-border/60 sm:-me-2.5 sm:p-2.5 sm:ps-4",
        depth <= 2 && "bg-muted/60",
        codes.length > 0 && "ring-destructive/40",
      )}
    >
      <legend className="sr-only">
        <Trans id="admin.group.rule.well">Group</Trans>
      </legend>
      <div className="flex items-start gap-2 text-muted-foreground text-sm">
        <div className="min-w-0 flex-1 leading-8">
          <Trans id="admin.group.rule.well-sentence">
            Match{" "}
            <CombinatorSelect
              value={group.op}
              describedBy={codes.length > 0 ? messageId : undefined}
              disabled={disabled}
              onChange={(op) => onChange(setCombinator(root, group.id, op))}
            />{" "}
            of these
          </Trans>
        </div>
        <Button
          variant="ghost"
          size="icon-sm"
          className="mt-0.5 shrink-0"
          disabled={disabled}
          aria-label={t({
            id: "admin.group.rule.remove-group",
            message: "Remove this group",
          })}
          onClick={() => onChange(removeNode(root, group.id))}
        >
          <XIcon aria-hidden="true" />
        </Button>
      </div>
      <Messages id={messageId} codes={codes} />
      <Children
        group={group}
        depth={depth}
        root={root}
        onChange={onChange}
        byNode={byNode}
        providers={providers}
        disabled={disabled}
        overflowMessageId={
          codes.includes("max_children_exceeded") ? messageId : undefined
        }
      />
    </fieldset>
  );
}

function Messages({ id, codes }: { id: string; codes: readonly string[] }) {
  const { i18n: catalogue } = useLingui();
  if (codes.length === 0) return null;
  return (
    <p id={id} className="text-destructive-foreground text-caption">
      {codes.map((code) => catalogue._(issueMessage(code))).join(" ")}
    </p>
  );
}

function CombinatorSelect({
  value,
  onChange,
  disabled,
  describedBy,
}: {
  value: RuleCombinator;
  onChange: (next: RuleCombinator) => void;
  disabled: boolean;
  describedBy?: string;
}) {
  const { t } = useLingui();
  const labels: Record<RuleCombinator, string> = {
    all: t({ id: "admin.group.rule.combinator.all", message: "all" }),
    any: t({ id: "admin.group.rule.combinator.any", message: "any" }),
  };
  return (
    <Select
      value={value}
      disabled={disabled}
      onValueChange={(next) => {
        if (next === "all" || next === "any") onChange(next);
      }}
    >
      <SelectTrigger
        size="sm"
        className="mx-1 inline-flex w-auto min-w-0 align-middle"
        aria-describedby={describedBy}
        aria-label={t({ id: "admin.group.rule.group.op", message: "Match" })}
      >
        <SelectValue>{() => labels[value]}</SelectValue>
      </SelectTrigger>
      <SelectPopup alignItemWithTrigger={false}>
        <SelectItem value="all">{labels.all}</SelectItem>
        <SelectItem value="any">{labels.any}</SelectItem>
      </SelectPopup>
    </Select>
  );
}

/** A provider's name with the states the picker notes beside it. */
function useProviderName() {
  const { t } = useLingui();
  return (provider: RuleProvider): string => {
    const name = provider.displayName;
    if (provider.disabled && provider.signInOff) {
      return t({
        id: "admin.group.rule.provider.disabled-sign-in-off",
        message: `${name} · disabled, sign-in off`,
      });
    }
    if (provider.disabled) {
      return t({
        id: "admin.group.rule.provider.disabled",
        message: `${name} · disabled`,
      });
    }
    if (provider.signInOff) {
      return t({
        id: "admin.group.rule.provider.sign-in-off",
        message: `${name} · sign-in off`,
      });
    }
    return name;
  };
}

function ConditionRow({
  condition,
  root,
  onChange,
  codes,
  providers,
  disabled,
}: {
  condition: RuleCondition;
  root: RuleGroup;
  onChange: (next: RuleGroup) => void;
  codes: readonly string[];
  providers: readonly RuleProvider[];
  disabled: boolean;
}) {
  const { t, i18n: catalogue } = useLingui();
  const providerName = useProviderName();
  const providersState = useContext(ProvidersStateContext);
  const messageId = useId();
  const noteId = useId();
  const aboutFact = codes.some((code) => factProblems.has(code));
  const aboutRow = codes.some((code) => rowProblems.has(code));
  const aboutValue = codes.some(
    (code) => !factProblems.has(code) && !rowProblems.has(code),
  );
  const known = providers.find((entry) => entry.slug === condition.value);
  const isProvider = condition.fact === "connection.provider";
  const unnamedProvider =
    isProvider && condition.value !== "" && known === undefined;
  // Only a list that has loaded can say a provider is not in it.
  const unknownProvider = unnamedProvider && providersState === "loaded";
  // The same note stands over a saved slug and over an empty picker: either way
  // the names the reader would choose from are missing.
  const namesFailed =
    isProvider &&
    providersState === "failed" &&
    (unnamedProvider || condition.value === "");

  const factText = catalogue._(factLabels[condition.fact]);
  const valueText = isProvider
    ? known === undefined
      ? condition.value
      : providerName(known)
    : (() => {
        const label = valueLabel(condition.fact, condition.value);
        return label === undefined ? condition.value : catalogue._(label);
      })();
  const summary = condition.negated
    ? t({
        id: "admin.group.rule.summary-negated",
        message: `${factText} is not ${valueText}`,
      })
    : t({
        id: "admin.group.rule.summary",
        message: `${factText} is ${valueText}`,
      });

  const items = isProvider
    ? providers.map((entry) => ({
        value: entry.slug,
        label: providerName(entry),
      }))
    : fixedValues(condition.fact).map((entry) => {
        const label = valueLabel(condition.fact, entry);
        return {
          value: entry,
          label: label === undefined ? entry : catalogue._(label),
        };
      });

  return (
    <fieldset
      data-slot="rule-condition"
      data-invalid={aboutRow || undefined}
      aria-describedby={aboutRow ? messageId : undefined}
      className={cn(
        "m-0 grid min-w-0 gap-1 border-0 p-0",
        // On a narrow screen a condition is two lines, so a rule between
        // conditions says which line belongs to which.
        "max-sm:[[data-slot=rule-condition]+&]:border-t max-sm:[[data-slot=rule-condition]+&]:pt-3",
        aboutRow &&
          "-m-1.5 rounded-lg bg-destructive/4 p-1.5 ring-1 ring-destructive/40",
      )}
    >
      <legend className="sr-only">{summary}</legend>
      <div className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-2 sm:grid-cols-[minmax(9rem,13rem)_auto_minmax(0,1fr)_auto]">
        <Select
          value={condition.fact}
          disabled={disabled}
          onValueChange={(next) => {
            if (ruleFacts.includes(next as RuleFact)) {
              onChange(setFact(root, condition.id, next as RuleFact));
            }
          }}
        >
          <SelectTrigger
            size="sm"
            aria-label={t({
              id: "admin.group.rule.fact",
              message: "Condition",
            })}
            aria-invalid={aboutFact || undefined}
            aria-describedby={aboutFact ? messageId : undefined}
            className="min-w-0 max-sm:col-start-1 max-sm:row-start-1"
          >
            <SelectValue>{() => factText}</SelectValue>
          </SelectTrigger>
          <SelectPopup alignItemWithTrigger={false}>
            {ruleFacts.map((fact) => (
              <SelectItem key={fact} value={fact}>
                {catalogue._(factLabels[fact])}
              </SelectItem>
            ))}
          </SelectPopup>
        </Select>

        <div className="flex min-w-0 items-center gap-2 max-sm:col-span-2 max-sm:row-start-2 sm:contents">
          <SenseToggle
            negated={condition.negated}
            disabled={disabled}
            onChange={(negated) =>
              onChange(setNegated(root, condition.id, negated))
            }
          />
          <Select
            value={condition.value === "" ? null : condition.value}
            disabled={disabled}
            items={items}
            onValueChange={(next) => {
              if (typeof next === "string") {
                onChange(setValue(root, condition.id, next));
              }
            }}
          >
            <SelectTrigger
              size="sm"
              aria-label={t({ id: "admin.group.rule.value", message: "Value" })}
              aria-invalid={aboutValue || undefined}
              aria-describedby={
                [aboutValue ? messageId : null, namesFailed ? noteId : null]
                  .filter((id) => id !== null)
                  .join(" ") || undefined
              }
              className="min-w-0 flex-1"
            >
              <SelectValue
                // The slug is what tells one provider from another, so it wraps
                // rather than being clipped; so does any value on a narrow
                // screen, where a nested row leaves it little width.
                className={
                  unnamedProvider
                    ? "whitespace-normal wrap-anywhere py-1"
                    : "max-sm:whitespace-normal max-sm:wrap-anywhere max-sm:py-1"
                }
              >
                {(selected: string | null) =>
                  selected === null ? (
                    <span className="text-muted-foreground">
                      <Trans id="admin.group.rule.provider.placeholder">
                        Choose a provider
                      </Trans>
                    </span>
                  ) : unknownProvider ? (
                    <Trans id="admin.group.rule.provider.unknown">
                      Unknown provider (
                      <span
                        data-slot="rule-provider-slug"
                        className="font-mono"
                      >
                        {selected}
                      </span>
                      )
                    </Trans>
                  ) : unnamedProvider ? (
                    <span data-slot="rule-provider-slug" className="font-mono">
                      {selected}
                    </span>
                  ) : (
                    valueText
                  )
                }
              </SelectValue>
            </SelectTrigger>
            <SelectPopup alignItemWithTrigger={false}>
              {items.map((entry) => (
                <SelectItem key={entry.value} value={entry.value}>
                  {entry.label}
                </SelectItem>
              ))}
            </SelectPopup>
          </Select>
        </div>

        <Button
          variant="ghost"
          size="icon-sm"
          disabled={disabled}
          className="max-sm:col-start-2 max-sm:row-start-1"
          aria-label={t({
            id: "admin.group.rule.remove-condition",
            message: `Remove the condition: ${summary}`,
          })}
          onClick={() => onChange(removeNode(root, condition.id))}
        >
          <XIcon aria-hidden="true" />
        </Button>
      </div>
      <Messages id={messageId} codes={codes} />
      {namesFailed && (
        <p id={noteId} className="text-muted-foreground text-caption">
          <Trans id="admin.group.rule.provider.names-failed">
            The provider names could not be loaded.
          </Trans>
        </p>
      )}
    </fieldset>
  );
}

/** `is` / `is not`: one is always chosen, and `is not` is tinted as a refusal. */
function SenseToggle({
  negated,
  onChange,
  disabled,
}: {
  negated: boolean;
  onChange: (negated: boolean) => void;
  disabled: boolean;
}) {
  const { t } = useLingui();
  return (
    <SegmentedControl
      label={t({ id: "admin.group.rule.sense", message: "Condition sense" })}
      value={negated ? "not" : "is"}
      disabled={disabled}
      onValueChange={(next) => onChange(next === "not")}
      options={[
        {
          value: "is",
          label: <Trans id="admin.group.rule.sense.is">is</Trans>,
        },
        {
          value: "not",
          label: <Trans id="admin.group.rule.sense.not">is not</Trans>,
          itemClassName: "data-checked:text-destructive-foreground",
        },
      ]}
    />
  );
}
