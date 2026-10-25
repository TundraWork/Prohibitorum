import { I18nProvider } from "@lingui/react";
import { render as renderUi, screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { i18n } from "@/i18n";
import { AdminHome } from "@/pages/admin/AdminHome";

function render() {
  return renderUi(
    <I18nProvider i18n={i18n}>
      <AdminHome />
    </I18nProvider>,
  );
}

describe("AdminHome", () => {
  beforeEach(() => {
    i18n.activate("en");
  });

  it("names the area with its only h1 and states what it is for", () => {
    render();
    const heading = screen.getByRole("heading", {
      level: 1,
      name: "Administration",
    });
    expect(heading).toBeInTheDocument();
    const header = heading.closest("header") as HTMLElement;
    expect(within(header).getByText(/from the sidebar/i)).toBeVisible();
  });

  it("sits in the page column with a single heading", () => {
    const { container } = render();
    expect(container.querySelector('[data-slot="page-body"]')).not.toBeNull();
    expect(screen.getAllByRole("heading")).toHaveLength(1);
  });
});
