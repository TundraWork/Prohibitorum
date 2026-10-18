import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { PageHeader } from "@/components/custom/PageHeader";

describe("PageHeader", () => {
  it("is the page's only h1, with the description under it", () => {
    render(
      <PageHeader
        title="Security"
        description="How you sign in to Northlight."
      />,
    );

    const headings = screen.getAllByRole("heading");
    expect(headings).toHaveLength(1);
    expect(headings[0]).toHaveTextContent("Security");
    expect(screen.getByRole("heading", { level: 1 })).toBe(headings[0]);
    expect(screen.getByText("How you sign in to Northlight.")).toBeVisible();
  });

  it("draws no description element when there is none", () => {
    const { container } = render(<PageHeader title="Security" />);

    expect(
      container.querySelector("[data-slot='page-description']"),
    ).toBeNull();
  });

  it("keeps its actions beside the title", () => {
    render(
      <PageHeader
        title="Devices"
        actions={<button type="button">Sign in a device</button>}
      />,
    );

    const actions = screen
      .getByRole("button", { name: "Sign in a device" })
      .closest("[data-slot='page-actions']");
    expect(actions).not.toBeNull();
  });
});
