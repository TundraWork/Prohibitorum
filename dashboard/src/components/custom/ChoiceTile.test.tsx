import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { ChoiceTile } from "@/components/custom/ChoiceTile";
import { RadioGroup } from "@/components/ui/radio-group";

describe("a list choice tile", () => {
  it("names its radio by the label and describes it by the line under it", async () => {
    const user = userEvent.setup();
    render(
      <RadioGroup aria-label="Level" defaultValue="a">
        <ChoiceTile
          layout="list"
          value="a"
          label="Narrow"
          description="Reaches one thing."
        />
        <ChoiceTile
          layout="list"
          value="b"
          label="Wide"
          description="Reaches everything."
        />
      </RadioGroup>,
    );

    const wide = screen.getByRole("radio", { name: "Wide" });
    expect(wide).toHaveAccessibleDescription("Reaches everything.");
    expect(screen.getByRole("radio", { name: "Narrow" })).toBeChecked();

    // Pressing the description selects the choice as pressing the name does.
    await user.click(screen.getByText("Reaches everything."));
    expect(wide).toBeChecked();
  });

  it("can be disabled", () => {
    render(
      <RadioGroup aria-label="Level">
        <ChoiceTile layout="list" value="a" label="Narrow" disabled />
      </RadioGroup>,
    );
    expect(screen.getByRole("radio", { name: "Narrow" })).toHaveAttribute(
      "aria-disabled",
      "true",
    );
  });
});
