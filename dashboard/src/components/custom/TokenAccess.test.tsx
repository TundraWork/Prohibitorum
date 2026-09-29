import { I18nProvider } from "@lingui/react";
import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import {
  TokenAccessBadge,
  TokenAccessSummary,
} from "@/components/custom/TokenAccess";
import { i18n } from "@/i18n";

beforeEach(() => {
  i18n.activate("en");
});

const apps = [
  { clientId: "wiki", displayName: "Wiki" },
  { clientId: "grafana", displayName: "Grafana" },
];

function summary(access: "selected_apps" | "all_apps" | "full" | "sudo") {
  return render(
    <I18nProvider i18n={i18n}>
      <TokenAccessSummary
        token={{ access, apps: access === "selected_apps" ? apps : [] }}
      />
    </I18nProvider>,
  );
}

describe("TokenAccessSummary", () => {
  it("lists the chosen applications for a selected_apps token", () => {
    summary("selected_apps");
    expect(screen.getByText("Wiki & Grafana")).toBeInTheDocument();
  });

  it("says the level for the others", () => {
    for (const [access, text] of [
      ["all_apps", "Every application"],
      ["full", "Full access"],
      ["sudo", "Full access, no identity checks"],
    ] as const) {
      const { unmount } = summary(access);
      expect(screen.getByText(text)).toBeInTheDocument();
      unmount();
    }
  });

  it("says so when a selected_apps token has no applications left", () => {
    render(
      <I18nProvider i18n={i18n}>
        <TokenAccessSummary token={{ access: "selected_apps", apps: [] }} />
      </I18nProvider>,
    );
    expect(screen.getByText("No applications")).toBeInTheDocument();
  });
});

describe("TokenAccessBadge", () => {
  it("marks only the sudo level", () => {
    const { container, rerender } = render(
      <I18nProvider i18n={i18n}>
        <TokenAccessBadge token={{ access: "sudo" }} />
      </I18nProvider>,
    );
    expect(screen.getByText("Skips identity checks")).toBeInTheDocument();
    for (const access of ["selected_apps", "all_apps", "full"] as const) {
      rerender(
        <I18nProvider i18n={i18n}>
          <TokenAccessBadge token={{ access }} />
        </I18nProvider>,
      );
      expect(container).toBeEmptyDOMElement();
    }
  });
});
