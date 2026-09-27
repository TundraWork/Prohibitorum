import { I18nProvider } from "@lingui/react";
import { act, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { CopyValue } from "@/components/custom/CopyValue";
import { i18n } from "@/i18n";

const value = "mock-client-1";

function setup() {
  return render(
    <I18nProvider i18n={i18n}>
      <CopyValue
        value={value}
        label="Client ID"
        description="The client is configured with this value."
      />
    </I18nProvider>,
  );
}

/**
 * `userEvent` installs the clipboard jsdom lacks. It runs without delays so
 * its own waits never touch the faked timers.
 */
function user() {
  return userEvent.setup({ delay: null });
}

beforeEach(() => {
  i18n.activate("en");
});

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

describe("CopyValue", () => {
  it("shows the label and names the read-only field with it", () => {
    setup();
    expect(screen.getByText("Client ID")).toBeVisible();
    const input = screen.getByRole("textbox", { name: "Client ID" });
    expect(input).toHaveValue(value);
    expect(input).toHaveAttribute("readonly");
  });

  it("draws the field with the surface variant", () => {
    setup();
    const input = screen.getByRole("textbox", { name: "Client ID" });
    expect(input.closest(".input-group")).toHaveClass("input-group--secondary");
  });

  it("names the copy button after the field", () => {
    setup();
    expect(
      screen.getByRole("button", { name: "Copy Client ID" }),
    ).toBeInTheDocument();
  });

  it("copies the whole value, confirms it, and resets after two seconds", async () => {
    vi.useFakeTimers({
      toFake: ["setTimeout", "clearTimeout"],
      shouldAdvanceTime: true,
    });
    const ue = user();
    const writeText = vi
      .spyOn(navigator.clipboard, "writeText")
      .mockResolvedValue(undefined);
    setup();

    await ue.click(screen.getByRole("button", { name: "Copy Client ID" }));

    expect(writeText).toHaveBeenCalledWith(value);
    expect(
      screen.getByRole("button", { name: "Copied Client ID" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("status")).toHaveTextContent("Copied");

    act(() => {
      vi.advanceTimersByTime(2000);
    });

    expect(
      screen.getByRole("button", { name: "Copy Client ID" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("status")).toBeEmptyDOMElement();
  });

  it("warns when the clipboard refuses, and clears once a copy succeeds", async () => {
    const ue = user();
    const writeText = vi
      .spyOn(navigator.clipboard, "writeText")
      .mockRejectedValueOnce(new DOMException("Denied", "NotAllowedError"));
    setup();

    await ue.click(screen.getByRole("button", { name: "Copy Client ID" }));

    expect(screen.getByRole("alert")).toHaveTextContent(
      "Couldn't copy. Select the value above and copy it yourself.",
    );
    expect(
      screen.getByRole("button", { name: "Copy Client ID" }),
    ).toBeInTheDocument();

    writeText.mockResolvedValueOnce(undefined);
    await ue.click(screen.getByRole("button", { name: "Copy Client ID" }));

    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("selects the value when the field takes focus", () => {
    setup();
    const input = screen.getByRole<HTMLInputElement>("textbox", {
      name: "Client ID",
    });

    act(() => input.focus());

    expect(input.selectionStart).toBe(0);
    expect(input.selectionEnd).toBe(value.length);
  });
});
