import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { SegmentedControl } from "@/components/custom/SegmentedControl";

const options = [
  { value: "is", label: "is" },
  { value: "not", label: "is not" },
] as const;

describe("SegmentedControl", () => {
  it("shows the chosen segment as checked in a named group", () => {
    render(
      <SegmentedControl
        label="Sense"
        value="not"
        options={options}
        onValueChange={vi.fn()}
      />,
    );

    expect(screen.getByRole("radiogroup", { name: "Sense" })).toBeVisible();
    expect(screen.getByRole("radio", { name: "is not" })).toBeChecked();
    expect(screen.getByRole("radio", { name: "is" })).not.toBeChecked();
  });

  it("reports the segment pressed, and keeps the chosen one when it is pressed again", async () => {
    const user = userEvent.setup();
    const onValueChange = vi.fn();
    render(
      <SegmentedControl
        label="Sense"
        value="is"
        options={options}
        onValueChange={onValueChange}
      />,
    );

    await user.click(screen.getByRole("radio", { name: "is" }));
    expect(onValueChange).not.toHaveBeenCalled();
    await user.click(screen.getByRole("radio", { name: "is not" }));
    expect(onValueChange).toHaveBeenCalledWith("not");
  });

  it("cannot be pressed while disabled", async () => {
    const user = userEvent.setup();
    const onValueChange = vi.fn();
    render(
      <SegmentedControl
        label="Sense"
        value="is"
        options={options}
        disabled
        onValueChange={onValueChange}
      />,
    );

    await user.click(screen.getByRole("radio", { name: "is not" }));
    expect(onValueChange).not.toHaveBeenCalled();
  });

  it("takes layout classes for the strip, so it can fill its column", () => {
    render(
      <SegmentedControl
        label="Sense"
        value="is"
        options={options}
        className="w-full"
        onValueChange={vi.fn()}
      />,
    );

    expect(screen.getByRole("radiogroup", { name: "Sense" })).toHaveClass(
      "w-full",
    );
    expect(screen.getByRole("radiogroup", { name: "Sense" })).not.toHaveClass(
      "w-fit",
    );
  });
});
