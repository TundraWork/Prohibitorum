import { Plural, Trans, useLingui } from "@lingui/react/macro";
import { issueMessage } from "@/pages/admin/groups/rule-labels";
import type { RuleIssue } from "@/pages/admin/groups/rule-model";

/**
 * The one line that says why a rule cannot be saved. One problem is stated in
 * its own words, so the footer is the reason and not only a pointer. A provider
 * left unchosen is said once for however many rows are marked, because it is
 * the one mistake a reader makes several times; any other set of problems is
 * counted, and each is written on the row or group it marks.
 */
export function RuleIssueSummary({ issues }: { issues: readonly RuleIssue[] }) {
  const { i18n } = useLingui();
  const first = issues[0];
  if (first === undefined) return null;
  const count = issues.length;
  if (issues.every((issue) => issue.code === "missing_provider")) {
    return (
      <Plural
        id="admin.group.rule.summary.missing-provider"
        value={count}
        one="Choose a provider for the highlighted condition."
        other="Choose a provider for each highlighted condition."
      />
    );
  }
  if (count === 1) return <>{i18n._(issueMessage(first.code))}</>;
  return (
    <Trans id="admin.group.rule.summary.several">
      Fix the {count} highlighted problems.
    </Trans>
  );
}
