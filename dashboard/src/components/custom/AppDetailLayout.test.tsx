import { I18nProvider } from "@lingui/react";
import { render, screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { AppDetailLayout } from "@/components/custom/AppDetailLayout";
import { ObjectHeader } from "@/components/custom/ObjectHeader";
import { Section } from "@/components/custom/Section";
import { i18n } from "@/i18n";

beforeEach(() => {
  i18n.activate("en");
});

function Page({ dirty = false }: { dirty?: boolean }) {
  return (
    <I18nProvider i18n={i18n}>
      <AppDetailLayout
        header={<ObjectHeader icon={<span>G</span>} title="Grafana" />}
        facts={<span>id.example.test</span>}
      >
        <Section title="Sign-in" dirty={dirty}>
          sign-in body
        </Section>
        <Section title="Disable or delete" tone="danger">
          danger body
        </Section>
        <Section title="Access">access body</Section>
      </AppDetailLayout>
    </I18nProvider>
  );
}

describe("AppDetailLayout", () => {
  it("draws the header's title as the page heading", () => {
    render(<Page />);
    expect(
      screen.getByRole("heading", { level: 1, name: "Grafana" }),
    ).toBeInTheDocument();
  });

  it("draws the sections in the order given", () => {
    render(<Page />);
    const titles = screen
      .getAllByRole("heading", { level: 2 })
      .map((heading) => heading.textContent);
    expect(titles).toEqual(["Sign-in", "Disable or delete", "Access"]);
  });

  it("lists every section in the rail, the danger one last", () => {
    render(<Page />);
    const rail = screen.getByRole("navigation", { name: "On this page" });
    const links = within(rail)
      .getAllByRole("link")
      .map((link) => link.textContent);
    expect(links).toEqual(["Sign-in", "Access", "Disable or delete"]);
    expect(within(rail).getByRole("link", { name: "Access" })).toHaveAttribute(
      "href",
      "#access",
    );
  });

  it("dots the rail link of a section with unsaved edits", () => {
    const { rerender } = render(<Page />);
    expect(screen.queryByRole("img", { name: "Unsaved changes" })).toBeNull();

    rerender(<Page dirty />);
    const link = screen.getByRole("link", { name: /Sign-in/ });
    expect(
      within(link).getByRole("img", { name: "Unsaved changes" }),
    ).toBeInTheDocument();
  });

  it("reserves room for the unsaved bar only while a section is dirty", () => {
    const { rerender, container } = render(<Page />);
    const columns = () =>
      container.querySelector("[data-slot=detail-columns]") as HTMLElement;
    expect(columns()).not.toHaveAttribute("data-reserving");

    rerender(<Page dirty />);
    expect(columns()).toHaveAttribute("data-reserving", "true");

    rerender(<Page />);
    expect(columns()).not.toHaveAttribute("data-reserving");
  });

  it("also shows the facts outside the rail, for widths without one", () => {
    render(<Page />);
    expect(screen.getAllByText("id.example.test")).toHaveLength(2);
  });

  it("shows the facts under the outline", () => {
    render(<Page />);
    const rail = screen.getByRole("navigation", { name: "On this page" });
    expect(
      within(rail.parentElement as HTMLElement).getByText("id.example.test"),
    ).toBeInTheDocument();
  });
});
