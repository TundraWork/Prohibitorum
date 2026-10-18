import { I18nProvider } from "@lingui/react";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  SecretReveal,
  SecretRevealDialog,
} from "@/components/custom/SecretReveal";
import { recoveryCodesCopy } from "@/components/custom/secret-reveal-copy";
import { i18n } from "@/i18n";

const blocker = vi.hoisted(() => ({
  current: undefined as { shouldBlockFn: () => boolean } | undefined,
}));

vi.mock("@tanstack/react-router", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-router")>()),
  useBlocker: (options: { shouldBlockFn: () => boolean }) => {
    blocker.current = options;
  },
}));

const secret = "aaaa-bbbb\ncccc-dddd";

beforeEach(() => {
  i18n.activate("en");
});

afterEach(() => {
  vi.restoreAllMocks();
});

function mount(onContinue = vi.fn(async () => undefined)) {
  const user = userEvent.setup();
  render(
    <I18nProvider i18n={i18n}>
      <SecretReveal
        text={secret}
        filename="codes.txt"
        copy={recoveryCodesCopy}
        onContinue={onContinue}
      />
    </I18nProvider>,
  );
  return { user, onContinue };
}

describe("SecretReveal", () => {
  it("shows the secret once in a read-only field under a warning", () => {
    mount();
    const field = screen.getByRole("textbox", { name: "New recovery codes" });
    expect(field).toHaveValue(secret);
    expect(field).toHaveAttribute("readonly");
    expect(screen.getByText(/shown only once/)).toBeVisible();
  });

  it("copies the secret and says so, or says it could not", async () => {
    const { user } = mount();
    const write = vi
      .spyOn(navigator.clipboard, "writeText")
      .mockResolvedValue(undefined);
    await user.click(screen.getByRole("button", { name: "Copy codes" }));
    expect(write).toHaveBeenCalledWith(secret);
    expect(await screen.findByRole("status")).toHaveTextContent("Copied.");

    write.mockRejectedValue(new Error("denied"));
    await user.click(screen.getByRole("button", { name: "Copy codes" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Could not copy",
    );
  });

  it("downloads the secret as a text file", async () => {
    const { user } = mount();
    let blob: Blob | undefined;
    const create = vi
      .spyOn(URL, "createObjectURL")
      .mockImplementation((object) => {
        blob = object as Blob;
        return "blob:codes";
      });
    vi.spyOn(URL, "revokeObjectURL").mockImplementation(() => undefined);
    let filename: string | undefined;
    const click = vi
      .spyOn(HTMLAnchorElement.prototype, "click")
      .mockImplementation(function (this: HTMLAnchorElement) {
        filename = this.download;
      });
    await user.click(screen.getByRole("button", { name: "Download codes" }));
    expect(create).toHaveBeenCalledTimes(1);
    expect(click).toHaveBeenCalledTimes(1);
    expect(filename).toBe("codes.txt");
    expect(await blob?.text()).toBe(`${secret}\n`);
  });

  it("holds continue until the secret is confirmed saved", async () => {
    const { user, onContinue } = mount();
    const next = screen.getByRole("button", { name: "Continue" });
    expect(next).toBeDisabled();

    await user.click(
      screen.getByRole("checkbox", { name: "I have saved my recovery codes" }),
    );
    expect(next).toBeEnabled();
    await user.click(next);
    expect(onContinue).toHaveBeenCalledTimes(1);
  });
});

function mountDialog(onContinue = vi.fn(async () => undefined)) {
  const user = userEvent.setup();
  render(
    <I18nProvider i18n={i18n}>
      <SecretRevealDialog
        text={secret}
        filename="codes.txt"
        copy={recoveryCodesCopy}
        onContinue={onContinue}
      />
    </I18nProvider>,
  );
  return { user, onContinue };
}

describe("SecretRevealDialog", () => {
  it("is a titled dialog holding the secret", async () => {
    mountDialog();

    const dialog = await screen.findByRole("dialog", {
      name: "Save your new recovery codes",
    });
    expect(
      within(dialog).getByRole("textbox", { name: "New recovery codes" }),
    ).toHaveValue(secret);
  });

  it("stays open on Escape and a press outside until saved, with no close button to press", async () => {
    const { user } = mountDialog();
    const dialog = await screen.findByRole("dialog");

    await user.keyboard("{Escape}");
    await user.click(document.body);

    expect(
      within(dialog).queryByRole("button", { name: "Close" }),
    ).not.toBeInTheDocument();
    expect(screen.getByRole("dialog")).toBeVisible();
    expect(
      within(dialog).getByRole("button", { name: "Continue" }),
    ).toBeDisabled();
  });

  it("holds the way out until the secret is confirmed saved, then continues", async () => {
    const { user, onContinue } = mountDialog();
    const dialog = await screen.findByRole("dialog");

    await user.click(
      within(dialog).getByRole("checkbox", {
        name: "I have saved my recovery codes",
      }),
    );
    await user.click(within(dialog).getByRole("button", { name: "Continue" }));

    expect(onContinue).toHaveBeenCalledTimes(1);
  });

  it("treats the close button as continue once saved", async () => {
    const { user, onContinue } = mountDialog();
    const dialog = await screen.findByRole("dialog");

    await user.click(
      within(dialog).getByRole("checkbox", {
        name: "I have saved my recovery codes",
      }),
    );
    await user.click(within(dialog).getByRole("button", { name: "Close" }));

    await waitFor(() => expect(onContinue).toHaveBeenCalledTimes(1));
  });
});

describe("SecretReveal leaving guard", () => {
  it("asks to confirm before navigating away from an unsaved secret", async () => {
    const { user } = mount();
    const shouldBlock = () => blocker.current?.shouldBlockFn();
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(false);

    expect(shouldBlock()).toBe(true);
    expect(confirm).toHaveBeenCalledTimes(1);

    await user.click(
      screen.getByRole("checkbox", { name: "I have saved my recovery codes" }),
    );
    confirm.mockClear();
    expect(shouldBlock()).toBe(false);
    expect(confirm).not.toHaveBeenCalled();
  });
});
