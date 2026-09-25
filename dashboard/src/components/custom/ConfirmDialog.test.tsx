import { I18nProvider } from "@lingui/react";
import { render, screen, within } from "@testing-library/react";
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
  it("draws the status icon beside the heading and matches the confirm button to it", () => {
    const { unmount } = render(
      <Harness status="danger" onConfirm={() => undefined} />,
    );
    let dialog = screen.getByRole("alertdialog", { name: "Retire this key?" });
    expect(
      dialog.querySelector(
        '[data-slot="alert-dialog-icon"].alert-dialog__icon--danger',
      ),
    ).not.toBeNull();
    expect(
      within(dialog).getByRole("button", { name: "Retire" }).className,
    ).toContain("button--danger");
    unmount();

    render(<Harness status="warning" onConfirm={() => undefined} />);
    dialog = screen.getByRole("alertdialog", { name: "Retire this key?" });
    expect(
      dialog.querySelector(
        '[data-slot="alert-dialog-icon"].alert-dialog__icon--warning',
      ),
    ).not.toBeNull();
    expect(
      within(dialog).getByRole("button", { name: "Retire" }).className,
    ).toContain("button--primary");
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
    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
  });

  it("holds both buttons while the action is in flight", async () => {
    const onConfirm = vi.fn();
    render(<Harness status="warning" isPending onConfirm={onConfirm} />);
    const cancel = screen.getByRole("button", { name: "Cancel" });
    expect(cancel).toBeDisabled();
    const confirm = screen.getByRole("button", { name: /Retire/ });
    expect(confirm).toHaveAttribute("data-pending", "true");
    await userEvent.click(confirm);
    expect(onConfirm).not.toHaveBeenCalled();
  });
});
