import { I18nProvider } from "@lingui/react";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ConfirmDialog } from "@/components/custom/ConfirmDialog";
import { i18n } from "@/i18n";

beforeEach(() => {
  i18n.activate("en");
});

function Harness({
  status,
  isPending = false,
  onConfirm,
}: {
  status: "danger" | "warning" | "accent";
  isPending?: boolean;
  onConfirm: () => void;
}) {
  const [open, setOpen] = useState(true);
  return (
    <I18nProvider i18n={i18n}>
      <ConfirmDialog
        isOpen={open}
        onOpenChange={setOpen}
        status={status}
        title="Retire this key?"
        body={<p>It stops verifying after the grace period.</p>}
        confirmLabel="Retire"
        isPending={isPending}
        onConfirm={onConfirm}
      />
    </I18nProvider>
  );
}

describe("ConfirmDialog", () => {
  it("names what it acts on under the title, and always dismisses with Cancel", () => {
    render(
      <I18nProvider i18n={i18n}>
        <ConfirmDialog
          isOpen
          onOpenChange={() => undefined}
          status="danger"
          title="Retire this key?"
          subject="key-1"
          body={<p>It stops verifying.</p>}
          confirmLabel="Retire"
          onConfirm={() => undefined}
        />
      </I18nProvider>,
    );
    const dialog = screen.getByRole("alertdialog", {
      name: "Retire this key?",
    });
    expect(
      dialog.querySelector('[data-slot="confirm-subject"]'),
    ).toHaveTextContent("key-1");
    expect(
      within(dialog).getByRole("button", { name: "Cancel" }),
    ).toBeVisible();
  });

  it("keeps the last subject while the dialog closes, and drops it when reopened without one", () => {
    const view = (isOpen: boolean, subject: string | false) => (
      <I18nProvider i18n={i18n}>
        <ConfirmDialog
          isOpen={isOpen}
          onOpenChange={() => undefined}
          status="danger"
          title="Retire this key?"
          subject={subject}
          body={<p>It stops verifying.</p>}
          confirmLabel="Retire"
          onConfirm={() => undefined}
        />
      </I18nProvider>
    );
    const { rerender } = render(view(true, "key-1"));
    // The caller clears its target as the dialog starts to close.
    rerender(view(false, false));
    const closing = screen.queryByRole("alertdialog", { hidden: true });
    expect(closing).not.toBeNull();
    if (closing !== null) {
      expect(
        closing.querySelector('[data-slot="confirm-subject"]'),
      ).toHaveTextContent("key-1");
    }
    rerender(view(true, false));
    expect(
      screen
        .getByRole("alertdialog", { name: "Retire this key?" })
        .querySelector('[data-slot="confirm-subject"]'),
    ).toBeNull();
  });

  it("announces the subject as part of the description, and draws none for false", () => {
    const { rerender } = render(
      <I18nProvider i18n={i18n}>
        <ConfirmDialog
          isOpen
          onOpenChange={() => undefined}
          status="danger"
          title="Retire this key?"
          subject="key-1"
          body={<p>It stops verifying.</p>}
          confirmLabel="Retire"
          onConfirm={() => undefined}
        />
      </I18nProvider>,
    );
    expect(
      screen.getByRole("alertdialog", { name: "Retire this key?" }),
    ).toHaveAccessibleDescription("key-1 It stops verifying.");
    rerender(
      <I18nProvider i18n={i18n}>
        <ConfirmDialog
          isOpen
          onOpenChange={() => undefined}
          status="danger"
          title="Retire this key?"
          subject={false}
          body={<p>It stops verifying.</p>}
          confirmLabel="Retire"
          onConfirm={() => undefined}
        />
      </I18nProvider>,
    );
    expect(
      screen
        .getByRole("alertdialog", { name: "Retire this key?" })
        .querySelector('[data-slot="confirm-subject"]'),
    ).toBeNull();
  });

  it("marks the popup with its tone and matches the confirm button to it", () => {
    const { unmount } = render(
      <Harness status="danger" onConfirm={() => undefined} />,
    );
    let dialog = screen.getByRole("alertdialog", { name: "Retire this key?" });
    expect(dialog).toHaveAttribute("data-tone", "danger");
    expect(
      within(dialog).getByRole("button", { name: "Retire" }),
    ).toHaveAttribute("data-variant", "destructive");
    unmount();

    const warning = render(
      <Harness status="warning" onConfirm={() => undefined} />,
    );
    dialog = screen.getByRole("alertdialog", { name: "Retire this key?" });
    expect(dialog).toHaveAttribute("data-tone", "warning");
    expect(
      within(dialog).getByRole("button", { name: "Retire" }),
    ).toHaveAttribute("data-variant", "default");
    warning.unmount();

    render(<Harness status="accent" onConfirm={() => undefined} />);
    dialog = screen.getByRole("alertdialog", { name: "Retire this key?" });
    expect(dialog).toHaveAttribute("data-tone", "default");
  });

  it("cancels on Escape without confirming", async () => {
    const onConfirm = vi.fn();
    render(<Harness status="danger" onConfirm={onConfirm} />);
    await userEvent.setup().keyboard("{Escape}");
    await waitFor(() =>
      expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument(),
    );
    expect(onConfirm).not.toHaveBeenCalled();
  });

  it("confirms through the caller and leaves closing to it; cancel closes without confirming", async () => {
    const onConfirm = vi.fn();
    render(<Harness status="danger" onConfirm={onConfirm} />);
    const user = userEvent.setup();

    await user.click(screen.getByRole("button", { name: "Retire" }));
    expect(onConfirm).toHaveBeenCalledTimes(1);
    expect(screen.getByRole("alertdialog")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Cancel" }));
    expect(onConfirm).toHaveBeenCalledTimes(1);
    await waitFor(() =>
      expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument(),
    );
  });

  it("holds both buttons while the action is in flight", async () => {
    const onConfirm = vi.fn();
    render(<Harness status="warning" isPending onConfirm={onConfirm} />);
    const cancel = screen.getByRole("button", { name: "Cancel" });
    expect(cancel).toBeDisabled();
    const confirm = screen.getByRole("button", { name: /Retire/ });
    expect(confirm).toHaveAttribute("aria-busy", "true");
    await userEvent.click(confirm);
    expect(onConfirm).not.toHaveBeenCalled();
  });
});
