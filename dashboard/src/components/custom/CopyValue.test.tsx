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

  it("describes the value and offers one copy button", () => {
    setup();
    expect(
      screen.getByText("The client is configured with this value."),
    ).toBeVisible();
    expect(screen.getAllByRole("button")).toHaveLength(1);
    expect(screen.getByRole("button", { name: "Copy" })).toBeVisible();
  });

  it("copies the whole value from the button, confirms it, and resets after two seconds", async () => {
    vi.useFakeTimers({
      toFake: ["setTimeout", "clearTimeout"],
      shouldAdvanceTime: true,
    });
    const ue = user();
    const writeText = vi
      .spyOn(navigator.clipboard, "writeText")
      .mockResolvedValue(undefined);
    setup();

    await ue.click(screen.getByRole("button", { name: "Copy" }));

    expect(writeText).toHaveBeenCalledWith(value);
    expect(field().selectionStart).toBe(0);
    expect(field().selectionEnd).toBe(value.length);
    expect(screen.getByRole("button", { name: "Copied" })).toBeVisible();
    expect(screen.getByRole("status")).toHaveTextContent("Copied");

    act(() => {
      vi.advanceTimersByTime(2000);
    });

    expect(screen.getByRole("button", { name: "Copy" })).toBeVisible();
    expect(screen.getByRole("status")).toBeEmptyDOMElement();
  });

  it("warns when the clipboard refuses, and clears once a copy succeeds", async () => {
    const ue = user();
    const writeText = vi
      .spyOn(navigator.clipboard, "writeText")
      .mockRejectedValueOnce(new DOMException("Denied", "NotAllowedError"));
    setup();

    await ue.click(screen.getByRole("button", { name: "Copy" }));

    expect(screen.getByRole("alert")).toHaveTextContent(
      "Couldn't copy. Select the value above and copy it yourself.",
    );
    expect(screen.getByRole("button", { name: "Copy" })).toBeVisible();

    writeText.mockResolvedValueOnce(undefined);
    await ue.click(screen.getByRole("button", { name: "Copy" }));

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
