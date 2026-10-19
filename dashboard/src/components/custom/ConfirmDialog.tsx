import { Trans } from "@lingui/react/macro";
import { CircleAlertIcon, InfoIcon, TriangleAlertIcon } from "lucide-react";
import type { ReactNode } from "react";
import { Button } from "@/components/custom/Button";
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
  subject,
  body,
  confirmLabel,
  isPending = false,
  onConfirm,
}: {
  isOpen: boolean;
  onOpenChange: (isOpen: boolean) => void;
  status: "danger" | "warning" | "accent";
  title: ReactNode;
  /**
   * What the step acts on, named under the title where every confirmation
   * puts it (an application, an account, a key id), so the title can stay a
   * question about the kind of thing.
   */
  subject?: ReactNode | false;
  /** What happens, named plainly; usually one paragraph. */
  body: ReactNode;
  confirmLabel: ReactNode;
  isPending?: boolean;
  onConfirm: () => void;
}) {
  return (
    <AlertDialog open={isOpen} onOpenChange={onOpenChange}>
      <AlertDialogPopup data-tone={status === "accent" ? "default" : status}>
        {/* The icon rides the first line of the title, and the body lines up
            under the title text rather than under the icon. */}
        <AlertDialogHeader className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 text-left">
          <span aria-hidden="true" className="self-start">
            {icons[status]}
          </span>
          <AlertDialogTitle>{title}</AlertDialogTitle>
          {/* The subject is part of the description, so a screen reader hears
              which one the step acts on. */}
          <AlertDialogDescription
            render={<div />}
            className="col-start-2 flex flex-col gap-2 text-pretty"
          >
            {subject != null && subject !== false && (
              <p
                data-slot="confirm-subject"
                className="break-words font-medium text-foreground text-sm"
              >
                {subject}
              </p>
            )}
            {body}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <Button
            disabled={isPending}
            onClick={() => onOpenChange(false)}
            variant="ghost"
          >
            <Trans id="confirm.cancel">Cancel</Trans>
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
