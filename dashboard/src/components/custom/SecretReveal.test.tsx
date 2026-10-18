import { I18nProvider } from "@lingui/react";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { Dialog, Heading, Modal, ModalOverlay } from "react-aria-components";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { SecretReveal } from "@/components/custom/SecretReveal";
import { recoveryCodesCopy } from "@/components/custom/secret-reveal-copy";
import { i18n } from "@/i18n";

vi.mock("@tanstack/react-router", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-router")>()),
  useBlocker: () => undefined,
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

function InModal() {
  const [open, setOpen] = useState(true);
  return (
    <I18nProvider i18n={i18n}>
      <ModalOverlay
        isOpen={open}
        onOpenChange={setOpen}
        isDismissable={false}
        isKeyboardDismissDisabled
      >
        <Modal>
          <Dialog aria-label="Save codes">
            <Heading slot="title">Save codes</Heading>
            <SecretReveal
              text={secret}
              filename="codes.txt"
              copy={recoveryCodesCopy}
              onContinue={async () => setOpen(false)}
              inDialog
            />
          </Dialog>
        </Modal>
      </ModalOverlay>
    </I18nProvider>
  );
}

describe("SecretReveal in a caller's dialog", () => {
  it("stays open on Escape and on a press outside, and closes through continue", async () => {
    const user = userEvent.setup();
    render(<InModal />);
    const dialog = screen.getByRole("dialog", { name: "Save codes" });
    expect(
      within(dialog).getByRole("textbox", { name: "New recovery codes" }),
    ).toBeVisible();

    await user.keyboard("{Escape}");
    await user.click(document.body);
    expect(screen.getByRole("dialog", { name: "Save codes" })).toBeVisible();

    await user.click(
      within(dialog).getByRole("checkbox", {
        name: "I have saved my recovery codes",
      }),
    );
    await user.click(within(dialog).getByRole("button", { name: "Continue" }));
    await waitFor(() =>
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
    );
  });
});
