import { I18nProvider } from "@lingui/react";
import { render, screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { OutlineRail } from "@/components/custom/OutlineRail";
import { Section } from "@/components/custom/Section";
import { SectionRegistryProvider } from "@/components/custom/SectionRegistry";
import { i18n } from "@/i18n";

beforeEach(() => i18n.activate("en"));

function Page({ dirty = "" }: { dirty?: string }) {
  return (
    <I18nProvider i18n={i18n}>
      <SectionRegistryProvider>
        <OutlineRail facts={<span>fact</span>} />
        <Section title="General" dirty={dirty === "General"}>
          x
        </Section>
        <Section title="Disable or delete" tone="danger">
          x
        </Section>
        <Section title="Connection" dirty={dirty === "Connection"}>
          x
        </Section>
      </SectionRegistryProvider>
    </I18nProvider>
  );
}

const links = () =>
  within(screen.getByRole("navigation", { name: "On this page" })).getAllByRole(
    "link",
  );

describe("OutlineRail", () => {
  it("lists the sections as in-page links, the danger one last", () => {
    render(<Page />);
    expect(links().map((link) => link.textContent)).toEqual([
      "General",
      "Connection",
      "Disable or delete",
    ]);
    expect(links()[0]?.getAttribute("href")).toBe("#general");
    expect(screen.getByText("fact")).toBeTruthy();
  });

  it("marks a dirty section's link as unsaved", () => {
    const { rerender } = render(<Page />);
    expect(screen.queryByLabelText("Unsaved changes")).toBeNull();
    rerender(<Page dirty="Connection" />);
    const dot = screen.getByLabelText("Unsaved changes");
    expect(links()[1]?.contains(dot)).toBe(true);
  });
});
