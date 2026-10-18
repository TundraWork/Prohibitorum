import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { RowMenu } from "@/components/custom/RowMenu";
import { MenuItem } from "@/components/ui/menu";

function mount(onRename = vi.fn(), onRemove = vi.fn()) {
  const user = userEvent.setup();
  render(
    <RowMenu label="More for MacBook Pro">
      <MenuItem onClick={onRename}>Rename</MenuItem>
      <MenuItem onClick={onRemove}>Remove</MenuItem>
    </RowMenu>,
  );
  return { user, onRename, onRemove };
}

describe("RowMenu", () => {
  it("names its trigger by the label and keeps the menu closed", () => {
    mount();

    expect(
      screen.getByRole("button", { name: "More for MacBook Pro" }),
    ).toBeVisible();
    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
  });

  it("opens on click and runs the chosen item", async () => {
    const { user, onRename, onRemove } = mount();

    await user.click(
      screen.getByRole("button", { name: "More for MacBook Pro" }),
    );
    await user.click(await screen.findByRole("menuitem", { name: "Rename" }));

    expect(onRename).toHaveBeenCalledTimes(1);
    expect(onRemove).not.toHaveBeenCalled();
  });

  it("opens from the keyboard on Enter", async () => {
    const { user, onRemove } = mount();

    screen.getByRole("button", { name: "More for MacBook Pro" }).focus();
    await user.keyboard("{Enter}");
    expect(await screen.findByRole("menu")).toBeVisible();
    await user.click(screen.getByRole("menuitem", { name: "Remove" }));

    expect(onRemove).toHaveBeenCalledTimes(1);
  });
});
