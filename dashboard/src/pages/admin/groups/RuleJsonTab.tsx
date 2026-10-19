import { Trans, useLingui } from "@lingui/react/macro";
import { useEffect, useId, useState } from "react";
import { Textarea } from "@/components/ui/textarea";
import {
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
 */
export function RuleJsonTab({
  value,
  onApply,
  disabled = false,
}: {
  value: RuleGroup;
  onApply: (next: RuleGroup) => void;
  disabled?: boolean;
}) {
  const { t } = useLingui();
  const serialized = JSON.stringify(serializeRule(value), null, 2);
  const [text, setText] = useState(serialized);
  const [problem, setProblem] = useState<
    "unreadable" | "not-a-document" | null
  >(null);
  const errorId = useId();

  // The text follows the tree, so the tab always shows what is about to be saved.
  useEffect(() => {
    setText(serialized);
  }, [serialized]);

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
      <p className="text-muted-foreground text-xs">
        <Trans id="admin.group.rule.json.applies">
          Applied when you leave the box. The builder then shows what the
          document says.
        </Trans>
      </p>
      {problem !== null && (
        <p
          id={errorId}
          role="alert"
          className="text-destructive-foreground text-sm"
        >
          {problem === "unreadable" ? (
            <Trans id="admin.group.rule.json.unreadable">
              This is not valid JSON. The form view still holds your rule.
            </Trans>
          ) : (
            <Trans id="admin.group.rule.json.nodocument">
              A rule document needs a version and a condition. The form view
              still holds your rule.
            </Trans>
          )}
        </p>
      )}
    </div>
  );
}
