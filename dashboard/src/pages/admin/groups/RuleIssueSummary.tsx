import { Plural, Trans, useLingui } from "@lingui/react/macro";
import { issueMessage } from "@/pages/admin/groups/rule-labels";
import type { RuleIssue } from "@/pages/admin/groups/rule-model";

/**
 * The one line that says why a rule cannot be saved. A provider left unchosen is
 * said once for however many rows are marked, because it is the one mistake a
 * reader makes several times; any other problem on a row is pointed at, since
 * the row says what it is; a problem with the rule as a whole is stated.
 */
export function RuleIssueSummary({ issues }: { issues: readonly RuleIssue[] }) {
  const { i18n } = useLingui();
  const first = issues[0];
  if (first === undefined) return null;
  if (first.code === "missing_provider") {
    const count = issues.filter(
      (issue) => issue.code === "missing_provider",
    ).length;
    return (
      <Plural
        id="admin.group.rule.summary.missing-provider"
        value={count}
        one="Choose a provider for the highlighted condition."
        other="Choose a provider for each highlighted condition."
      />
    );
  }
  // A problem on a row says what it is on that row; the footer only points at it.
  if (first.nodeId !== null) {
    return (
      <Trans id="admin.group.rule.summary.fix">
        Fix the highlighted part of the rule.
      </Trans>
    );
  }
  return <>{i18n._(issueMessage(first.code))}</>;
}
