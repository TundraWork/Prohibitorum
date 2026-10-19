import { Trans, useLingui } from "@lingui/react/macro";
import { PlusIcon, XIcon } from "lucide-react";
import { useId } from "react";
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

/** Codes about the fact itself; every other code is about the value. */
const factProblems = new Set(["invalid_shape", "invalid_fact"]);

/**
 * The rule as an editor: a sentence of conditions inside any/all groups.
 *
 * It is controlled and holds no state of its own, so the group page and the
 * create page both draw it over a tree they own. `issues` are drawn on the row
 * or group each names; the root's own problem is the prompt that an empty rule
 * shows.
 */
export function RuleBuilder({
  value,
  onChange,
  issues = [],
  providers,
  disabled = false,
}: {
  value: RuleGroup;
  onChange: (next: RuleGroup) => void;
  issues?: readonly RuleIssue[];
  providers: readonly RuleProvider[];
  disabled?: boolean;
}) {
  const { i18n } = useLingui();
  const byNode = groupIssues(issues);
  const empty = value.children.length === 0;

  return (
    <div className="grid gap-3">
      <div className="min-w-0 text-sm leading-8">
        <Trans id="admin.group.rule.root">
          Accounts that meet{" "}
          <CombinatorSelect
            value={value.op}
            disabled={disabled}
            onChange={(op) => onChange(setCombinator(value, value.id, op))}
          />{" "}
          of these
        </Trans>
      </div>
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
      />
    </div>
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
  const reasonId = useId();
  const mayNest = canAddGroup(depth);
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
          disabled={disabled}
          aria-label={t({
            id: "admin.group.rule.add",
            message: "Add condition",
          })}
          onClick={() => onChange(addCondition(root, group.id))}
        >
          <PlusIcon aria-hidden="true" />
          <Trans id="admin.group.rule.add-condition">Condition</Trans>
        </Button>
        <Button
          variant="ghost"
          size="xs"
          disabled={disabled || !mayNest}
          aria-label={t({
            id: "admin.group.rule.addGroup",
            message: "Add group",
          })}
          aria-describedby={mayNest ? undefined : reasonId}
          onClick={() => onChange(addGroup(root, group.id))}
        >
          <PlusIcon aria-hidden="true" />
          <Trans id="admin.group.rule.add-group">Group</Trans>
        </Button>
        {!mayNest && (
          <span id={reasonId} className="text-muted-foreground text-xs">
            <Trans id="admin.group.rule.depth-limit">
              Groups nest at most 8 deep.
            </Trans>
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
        "m-0 grid min-w-0 gap-2 rounded-xl border-0 bg-muted/60 p-1.5 ps-2 ring-1 sm:p-2.5 sm:ps-4 ring-border/60",
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
              invalid={codes.length > 0}
              disabled={disabled}
              onChange={(op) => onChange(setCombinator(root, group.id, op))}
            />{" "}
            of these
          </Trans>
        </div>
        <Button
          variant="ghost"
          size="icon-xs"
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
      />
    </fieldset>
  );
}

function Messages({ id, codes }: { id: string; codes: readonly string[] }) {
  const { i18n: catalogue } = useLingui();
  if (codes.length === 0) return null;
  return (
    <p id={id} className="text-destructive-foreground text-xs">
      {codes.map((code) => catalogue._(issueMessage(code))).join(" ")}
    </p>
  );
}

function CombinatorSelect({
  value,
  onChange,
  disabled,
  invalid = false,
}: {
  value: RuleCombinator;
  onChange: (next: RuleCombinator) => void;
  disabled: boolean;
  invalid?: boolean;
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
        aria-invalid={invalid || undefined}
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
  const messageId = useId();
  const invalid = codes.length > 0;
  const aboutFact = codes.some((code) => factProblems.has(code));
  const known = providers.find((entry) => entry.slug === condition.value);
  const isProvider = condition.fact === "connection.provider";
  const unknownProvider =
    isProvider && condition.value !== "" && known === undefined;

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
    <fieldset className="m-0 grid min-w-0 gap-1 border-0 p-0">
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
              aria-invalid={(invalid && !aboutFact) || undefined}
              aria-describedby={invalid && !aboutFact ? messageId : undefined}
              className="min-w-0 flex-1"
            >
              <SelectValue
                // The slug is what tells one unknown provider from another, so
                // it wraps rather than being clipped.
                className={
                  unknownProvider
                    ? "whitespace-normal break-all py-1"
                    : undefined
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
                    <span className="font-mono">
                      <Trans id="admin.group.rule.provider.unknown">
                        Unknown provider ({selected})
                      </Trans>
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
