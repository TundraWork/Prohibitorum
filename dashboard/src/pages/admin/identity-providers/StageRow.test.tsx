import { I18nProvider } from "@lingui/react";
import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import type { DiagnosticStageView } from "@/api/raw-admin-paths";
import { i18n } from "@/i18n";
import { EffectiveValue } from "@/pages/admin/identity-providers/EffectiveConfigDialog";
import { StageRow } from "@/pages/admin/identity-providers/StageRow";

function renderRow(stage: DiagnosticStageView) {
  return render(
    <I18nProvider i18n={i18n}>
      <ul>
        <StageRow stage={stage} />
      </ul>
    </I18nProvider>,
  );
}

beforeEach(() => {
  i18n.activate("en");
});
describe("StageRow", () => {
  it("renders the provider's error description under a label", () => {
    const { container } = renderRow({
      name: "callback",
      status: "failed",
      errorCode: "invalid_request",
      errorDescription:
        "AADSTS9002346: Application 'mock-client' is configured for use by Microsoft Account users only.",
    });
    // The label and the raw text share the <p> (the description is a span so
    // it can carry break-words), so match on the paragraph element itself.
    const paragraph = screen.getByText(
      (_, element) =>
        element?.tagName === "P" &&
        (element.textContent ?? "").includes("Provider message:"),
    );
    expect(paragraph).toHaveTextContent("AADSTS9002346");
    expect(screen.getByText(/AADSTS9002346/)).toBeInTheDocument();
    // A failed stage also names its error code, and nothing else is labelled.
    expect(screen.getByText("invalid_request")).toBeInTheDocument();
    expect(container.querySelectorAll("p")).toHaveLength(1);
  });

  it("renders no description line when the stage carries none", () => {
    const { container } = renderRow({
      name: "token_exchange",
      status: "failed",
      errorCode: "invalid_client",
      httpStatus: 401,
    });
    expect(screen.queryByText(/Provider message/)).not.toBeInTheDocument();
    expect(container.querySelectorAll("p")).toHaveLength(0);
  });
});

describe("EffectiveValue", () => {
  it("spells out the JWKS sentinel and an empty scope list instead of not-used", () => {
    const { rerender } = render(
      <I18nProvider i18n={i18n}>
        <EffectiveValue field="jwksEndpoint" value="none" />
      </I18nProvider>,
    );
    expect(
      screen.getByText(/Not used — the ID token is skipped/),
    ).toBeInTheDocument();
    rerender(
      <I18nProvider i18n={i18n}>
        <EffectiveValue field="scopes" value={[]} />
      </I18nProvider>,
    );
    expect(screen.getByText("None sent")).toBeInTheDocument();
    // Every other empty value keeps today's wording, and the sentinel stays
    // raw text only for other fields.
    rerender(
      <I18nProvider i18n={i18n}>
        <EffectiveValue field="userinfoEndpoint" value="" />
      </I18nProvider>,
    );
    expect(screen.getByText("Not used")).not.toHaveTextContent(/skipped/);
  });
});
