import { I18nProvider } from "@lingui/react";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it } from "vitest";
import { FilterPopover } from "@/components/custom/FilterPopover";
import { i18n } from "@/i18n";

beforeEach(() => {
  i18n.activate("en");
});

function view(count: number) {
  return (
    <I18nProvider i18n={i18n}>
      <FilterPopover count={count}>
        <input aria-label="Value" />
      </FilterPopover>
    </I18nProvider>
  );
}

describe("FilterPopover", () => {
  it("names the button plainly while nothing is set, and with the count once something is", () => {
    const { rerender } = render(view(0));
    expect(screen.getByRole("button", { name: "Filters" })).toBeVisible();

    rerender(view(2));
    expect(
      screen.getByRole("button", { name: "Filters, 2 active" }),
    ).toBeVisible();
  });

  it("opens a dialog holding the controls, and the button closes it again", async () => {
    const user = userEvent.setup();
    render(view(0));
    const button = screen.getByRole("button", { name: "Filters" });

    await user.click(button);
    expect(
      await screen.findByRole("dialog", { name: "Filters" }),
    ).toBeVisible();
    expect(screen.getByRole("textbox", { name: "Value" })).toBeVisible();

    await user.click(button);
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });

  it("closes on Escape and returns focus to the button", async () => {
    const user = userEvent.setup();
    render(view(0));
    const button = screen.getByRole("button", { name: "Filters" });

    await user.click(button);
    await screen.findByRole("dialog", { name: "Filters" });
    await user.keyboard("{Escape}");

    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(button).toHaveFocus();
  });
});
