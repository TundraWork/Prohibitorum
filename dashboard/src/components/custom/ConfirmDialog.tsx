import { Trans } from "@lingui/react/macro";
import { CircleAlertIcon, InfoIcon, TriangleAlertIcon } from "lucide-react";
import type { ReactNode } from "react";
import { Button } from "@/components/custom/Button";
import { getTopLayerContainer } from "@/components/custom/top-layer";
import {
  AlertDialog,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogPopup,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";

const icons = {
  danger: <CircleAlertIcon className="size-5 text-destructive" />,
  warning: <TriangleAlertIcon className="size-5 text-warning" />,
  accent: <InfoIcon className="size-5 text-info" />,
};

/**
 * The console's confirmation before a consequential action: the status icon
 * beside the heading, the consequence in the body, and cancel on the left of
 * the one action that goes ahead.
 *
 * `status` says what kind of step this is, and the icon and the confirm button
 * follow it: `danger` for what removes something for good, `warning` for a
 * change of state that can be undone, `accent` for a step that is merely worth
 * a second look. The popup carries it as `data-tone` (`accent` reads as
 * `default`). Every confirmation on the console goes through here, so the
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
    <AlertDialog open={isOpen} onOpenChange={onOpenChange}>
      <AlertDialogPopup
        data-tone={status === "accent" ? "default" : status}
        portalProps={{ container: getTopLayerContainer() }}
      >
        <AlertDialogHeader>
          <div className="flex items-center gap-3">
            {icons[status]}
            <AlertDialogTitle>{title}</AlertDialogTitle>
          </div>
          <AlertDialogDescription render={<div />}>
            {body}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <Button
            disabled={isPending}
            onClick={() => onOpenChange(false)}
            variant="ghost"
          >
            {cancelLabel ?? <Trans id="confirm.cancel">Cancel</Trans>}
          </Button>
          <Button
            pending={isPending}
            onClick={onConfirm}
            variant={status === "danger" ? "destructive" : "default"}
          >
            {confirmLabel}
          </Button>
        </AlertDialogFooter>
      </AlertDialogPopup>
    </AlertDialog>
  );
}
