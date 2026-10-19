import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { TableEmptyState } from "@/components/custom/TableEmptyState";

describe("TableEmptyState", () => {
  it("says what is missing and draws no action unless it is given one", () => {
    render(<TableEmptyState icon={<span />} title="Nothing here" />);

    expect(screen.getByText("Nothing here")).toBeVisible();
    expect(screen.queryByRole("button")).toBeNull();
  });

  it("draws the way out of the empty state under the message", () => {
    render(
      <TableEmptyState
        icon={<span />}
        title="No match"
        action={<button type="button">Clear filters</button>}
      />,
    );

    expect(screen.getByRole("button", { name: "Clear filters" })).toBeVisible();
  });
});
