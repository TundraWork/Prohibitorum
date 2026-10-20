import { Trans, useLingui } from "@lingui/react/macro";
import { useEffect, useId, useState } from "react";
import { Textarea } from "@/components/ui/textarea";
import {
  type RuleDocumentProblem,
  type RuleGroup,
  readRuleDocument,
  serializeRule,
} from "@/pages/admin/groups/rule-model";

/**
 * The rule as the document the server stores.
 *
 * The builder's tree is the single source of truth: the text renders from it
 * every time, and writes back only when it parses into a rule document on blur,
 * so a half-typed document never replaces a tree the reader can still see.
 * While the box holds text that was refused, `onProblem` says so, so the section
 * can count it as an unsaved edit that cannot be saved.
 */
export function RuleJsonTab({
  value,
  onApply,
  onProblem,
  disabled = false,
}: {
  value: RuleGroup;
  onApply: (next: RuleGroup) => void;
  onProblem?: (problem: RuleDocumentProblem | null) => void;
  disabled?: boolean;
}) {
  const { t } = useLingui();
  const serialized = JSON.stringify(serializeRule(value), null, 2);
  const [text, setText] = useState(serialized);
  const [problem, setProblem] = useState<RuleDocumentProblem | null>(null);
  const errorId = useId();

  // The text follows the tree, so the tab always shows what is about to be saved.
  // A message about text that has been replaced would describe nothing.
  useEffect(() => {
    setText(serialized);
    setProblem(null);
  }, [serialized]);

  useEffect(() => {
    onProblem?.(problem);
  }, [problem, onProblem]);

  const apply = () => {
    if (text === serialized) return;
    const read = readRuleDocument(text);
    if (!read.ok) {
      setProblem(read.problem);
      return;
    }
    setProblem(null);
    onApply(read.root);
  };

  return (
    <div className="grid gap-2">
      <Textarea
        aria-label={t({
          id: "admin.group.rule.json.label",
          message: "Rule document",
        })}
        aria-invalid={problem !== null || undefined}
        aria-describedby={problem === null ? undefined : errorId}
        className="font-mono text-[13px]"
        rows={14}
        spellCheck={false}
        disabled={disabled}
        value={text}
        onChange={(event) => setText(event.target.value)}
        onBlur={apply}
      />
      <p className="text-muted-foreground text-caption">
        <Trans id="admin.group.rule.json.applies">
          Applied when you leave the box. The builder then shows what the
          document says.
        </Trans>
      </p>
      {problem !== null && (
        <p
          id={errorId}
          role="alert"
          className="text-destructive-foreground text-caption"
        >
          <RuleJsonProblem problem={problem} />
        </p>
      )}
    </div>
  );
}

/** Why a document was refused, in the words both the box and the footer use. */
export function RuleJsonProblem({ problem }: { problem: RuleDocumentProblem }) {
  switch (problem) {
    case "unreadable":
      return (
        <Trans id="admin.group.rule.json.unreadable">
          This is not valid JSON. The builder still holds your rule.
        </Trans>
      );
    case "unknown-condition":
      return (
        <Trans id="admin.group.rule.json.unknown">
          This rule uses a condition this console does not know. The builder
          still holds your rule.
        </Trans>
      );
    case "nested-negation":
      return (
        <Trans id="admin.group.rule.json.nested-negation">
          A negation can only wrap one condition, not another negation. The
          builder still holds your rule.
        </Trans>
      );
    case "not-a-document":
      return (
        <Trans id="admin.group.rule.json.nodocument">
          A rule document needs a version and a condition. The builder still
          holds your rule.
        </Trans>
      );
  }
}
