import { Modal } from "@heroui/react";
import { I18nProvider } from "@lingui/react";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
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
    const create = vi.fn(() => "blob:codes");
    vi.stubGlobal("URL", Object.assign(URL, { createObjectURL: create }));
    vi.stubGlobal(
      "URL",
      Object.assign(URL, { revokeObjectURL: () => undefined }),
    );
    const click = vi
      .spyOn(HTMLAnchorElement.prototype, "click")
      .mockImplementation(() => undefined);
    await user.click(screen.getByRole("button", { name: "Download codes" }));
    expect(create).toHaveBeenCalledTimes(1);
    expect(click).toHaveBeenCalledTimes(1);
    vi.unstubAllGlobals();
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
      <Modal isOpen={open} onOpenChange={setOpen}>
        <Modal.Backdrop isDismissable={false} isKeyboardDismissDisabled>
          <Modal.Container>
            <Modal.Dialog>
              <Modal.Header>
                <Modal.Heading>Save codes</Modal.Heading>
              </Modal.Header>
              <SecretReveal
                text={secret}
                filename="codes.txt"
                copy={recoveryCodesCopy}
                onContinue={async () => setOpen(false)}
                inDialog
              />
            </Modal.Dialog>
          </Modal.Container>
        </Modal.Backdrop>
      </Modal>
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
