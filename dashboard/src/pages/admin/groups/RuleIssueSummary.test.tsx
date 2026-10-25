import { I18nProvider } from "@lingui/react";
import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { i18n } from "@/i18n";
import { RuleIssueSummary } from "@/pages/admin/groups/RuleIssueSummary";
import type { RuleIssue } from "@/pages/admin/groups/rule-model";

beforeEach(() => i18n.activate("en"));

function summary(issues: RuleIssue[]) {
  return render(
    <I18nProvider i18n={i18n}>
      <RuleIssueSummary issues={issues} />
    </I18nProvider>,
  );
}

describe("the rule's footer reason", () => {
  it("says nothing without a problem", () => {
    const { container } = summary([]);
    expect(container).toBeEmptyDOMElement();
  });

  it("states a single problem in its own words", () => {
    summary([{ nodeId: "n1", code: "max_depth_exceeded" }]);
    expect(
      screen.getByText(
        "Conditions are nested too deeply. Use at most 8 levels.",
      ),
    ).toBeVisible();
  });

  it("states a problem with the rule as a whole", () => {
    summary([{ nodeId: null, code: "empty_rule" }]);
    expect(
      screen.getByText("A rule needs at least one condition."),
    ).toBeVisible();
  });

  it("counts several different problems", () => {
    summary([
      { nodeId: "n1", code: "max_depth_exceeded" },
      { nodeId: "n2", code: "missing_provider" },
    ]);
    expect(screen.getByText("Fix the 2 highlighted problems.")).toBeVisible();
  });

  it("says an unchosen provider once for any number of rows", () => {
    summary([
      { nodeId: "n1", code: "missing_provider" },
      { nodeId: "n2", code: "missing_provider" },
    ]);
    expect(
      screen.getByText("Choose a provider for each highlighted condition."),
    ).toBeVisible();
  });
});
