import { AlertDialog } from "@heroui/react";
import { Trans } from "@lingui/react/macro";
import type { ReactNode } from "react";
import { Button } from "@/components/custom/Button";

/**
 * The console's confirmation before a consequential action: HeroUI's
 * `AlertDialog` with its status icon beside the heading, the consequence in the
 * body, and cancel on the left of the one action that goes ahead.
 *
 * `status` says what kind of step this is, and the icon and the confirm button
 * follow it: `danger` for what removes something for good, `warning` for a
 * change of state that can be undone, `accent` for a step that is merely worth
 * a second look. Every confirmation on the console goes through here, so the
 * icon, the button order and the pending state are decided once.
 *
 * The dialog does not close itself on confirm. The caller decides — some close
 * at once and let the trigger show progress, others stay open with the confirm
 * button pending until the write settles.
 */
export function ConfirmDialog({
  isOpen,
  onOpenChange,
  status,
  title,
  body,
  confirmLabel,
  cancelLabel,
  isPending = false,
  onConfirm,
}: {
  isOpen: boolean;
  onOpenChange: (isOpen: boolean) => void;
  status: "danger" | "warning" | "accent";
  title: ReactNode;
  /** What happens, named plainly; usually one paragraph. */
  body: ReactNode;
  confirmLabel: ReactNode;
  /** Defaults to "Cancel"; a dialog may name the choice instead ("Keep it"). */
  cancelLabel?: ReactNode;
  isPending?: boolean;
  onConfirm: () => void;
}) {
  return (
    <AlertDialog isOpen={isOpen} onOpenChange={onOpenChange}>
      <AlertDialog.Backdrop>
        <AlertDialog.Container placement="center" size="md">
          <AlertDialog.Dialog>
            <AlertDialog.Header>
              <AlertDialog.Icon status={status} />
              <AlertDialog.Heading>{title}</AlertDialog.Heading>
            </AlertDialog.Header>
            <AlertDialog.Body>{body}</AlertDialog.Body>
            <AlertDialog.Footer>
              <Button
                variant="tertiary"
                isDisabled={isPending}
                onPress={() => onOpenChange(false)}
              >
                {cancelLabel ?? <Trans id="confirm.cancel">Cancel</Trans>}
              </Button>
              <Button
                variant={status === "danger" ? "danger" : "primary"}
                isPending={isPending}
                onPress={onConfirm}
              >
                {confirmLabel}
              </Button>
            </AlertDialog.Footer>
          </AlertDialog.Dialog>
        </AlertDialog.Container>
      </AlertDialog.Backdrop>
    </AlertDialog>
  );
}
