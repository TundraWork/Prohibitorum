import { I18nProvider } from "@lingui/react";
import { act, fireEvent, render, screen } from "@testing-library/react";
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

function field() {
  return screen.getByRole<HTMLInputElement>("textbox", { name: "Client ID" });
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
    expect(field()).toHaveValue(value);
    expect(field()).toHaveAttribute("readonly");
  });

  it("draws the field with the surface variant and no button", () => {
    setup();
    expect(field().closest(".input-group")).toHaveClass(
      "input-group--secondary",
    );
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });

  it("copies the whole value on a click, confirms it, and resets after two seconds", async () => {
    vi.useFakeTimers({
      toFake: ["setTimeout", "clearTimeout"],
      shouldAdvanceTime: true,
    });
    const ue = user();
    const writeText = vi
      .spyOn(navigator.clipboard, "writeText")
      .mockResolvedValue(undefined);
    setup();

    await ue.click(field());

    expect(writeText).toHaveBeenCalledWith(value);
    expect(field().selectionStart).toBe(0);
    expect(field().selectionEnd).toBe(value.length);
    expect(screen.getByRole("status")).toHaveTextContent("Copied");
    expect(screen.getByRole("tooltip")).toHaveTextContent("Copied");

    act(() => {
      vi.advanceTimersByTime(2000);
    });

    expect(screen.getByRole("status")).toBeEmptyDOMElement();
    expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
  });

  it("warns when the clipboard refuses, and clears once a copy succeeds", async () => {
    const ue = user();
    const writeText = vi
      .spyOn(navigator.clipboard, "writeText")
      .mockRejectedValueOnce(new DOMException("Denied", "NotAllowedError"));
    setup();

    await ue.click(field());

    expect(screen.getByRole("alert")).toHaveTextContent(
      "Couldn't copy. Select the value above and copy it yourself.",
    );
    expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();

    writeText.mockResolvedValueOnce(undefined);
    await ue.click(field());

    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("only selects on keyboard focus, and confirms a copy made by hand", () => {
    const writeText = vi.spyOn(navigator.clipboard, "writeText");
    setup();

    act(() => field().focus());

    expect(field().selectionStart).toBe(0);
    expect(field().selectionEnd).toBe(value.length);
    expect(writeText).not.toHaveBeenCalled();

    fireEvent.copy(field());

    expect(screen.getByRole("status")).toHaveTextContent("Copied");
  });
});
