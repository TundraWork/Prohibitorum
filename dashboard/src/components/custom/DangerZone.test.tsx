import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { DangerZoneFrame } from "@/components/custom/DangerZone";
import { ItemList, ItemListRow } from "@/components/custom/ItemList";

describe("DangerZoneFrame", () => {
  it("is a destructive-toned frame titled at level 2 whose rows share one panel", () => {
    const { container } = render(
      <DangerZoneFrame title="Danger zone">
        <ItemList label="Danger zone" empty={null}>
          <ItemListRow
            key="a"
            title="Delete it"
            details={["This cannot be undone."]}
            actions={<button type="button">Delete</button>}
          />
        </ItemList>
      </DangerZoneFrame>,
    );

    const frame = container.querySelector("[data-slot='frame']");
    expect(frame).toHaveAttribute("data-tone", "danger");
    expect(
      screen.getByRole("heading", { level: 2, name: "Danger zone" }),
    ).toBeInTheDocument();
    expect(
      container.querySelectorAll("[data-slot='frame-panel']"),
    ).toHaveLength(1);
    expect(screen.getByRole("button", { name: "Delete" })).toBeInTheDocument();
  });
});
