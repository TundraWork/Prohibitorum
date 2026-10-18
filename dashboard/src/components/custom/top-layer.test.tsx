import { I18nProvider } from "@lingui/react";
import { render, screen, waitFor } from "@testing-library/react";
import { useState } from "react";
import { Dialog, Heading, Modal, ModalOverlay } from "react-aria-components";
import { describe, expect, it } from "vitest";
import { ConfirmDialog } from "@/components/custom/ConfirmDialog";
import { getTopLayerContainer } from "@/components/custom/top-layer";
import { i18n } from "@/i18n";

describe("getTopLayerContainer", () => {
  it("returns one body-level element marked as a react-aria top layer", () => {
    const container = getTopLayerContainer();
    expect(container.parentElement).toBe(document.body);
    expect(container).toHaveAttribute("data-react-aria-top-layer");
    expect(getTopLayerContainer()).toBe(container);
  });

  it("makes a new one when the old one was removed", () => {
    getTopLayerContainer().remove();
    expect(getTopLayerContainer().isConnected).toBe(true);
  });
});

function Stack() {
  const [open, setOpen] = useState(true);
  return (
    <I18nProvider i18n={i18n}>
      <ModalOverlay isOpen>
        <Modal>
          <Dialog aria-label="Host">
            <Heading slot="title">Host dialog</Heading>
            <button type="button">host button</button>
          </Dialog>
        </Modal>
      </ModalOverlay>
      <ConfirmDialog
        isOpen={open}
        onOpenChange={setOpen}
        status="danger"
        title="Retire this key?"
        body={<p>Body</p>}
        confirmLabel="Retire"
        onConfirm={() => undefined}
      />
    </I18nProvider>
  );
}

describe("a coss dialog over a react-aria modal", () => {
  it("is not hidden by the modal and takes focus", async () => {
    i18n.activate("en");
    render(<Stack />);
    const dialog = await screen.findByRole("alertdialog", {
      name: "Retire this key?",
    });
    expect(dialog.closest("[aria-hidden='true'], [inert]")).toBeNull();
    expect(dialog.closest("[data-react-aria-top-layer]")).not.toBeNull();
    await waitFor(() =>
      expect(dialog.contains(document.activeElement)).toBe(true),
    );
  });
});
