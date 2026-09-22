import {
  AlertDialog,
  Description,
  Label,
  Tooltip,
  useOverlayState,
} from "@heroui/react";
import { Trans } from "@lingui/react/macro";
import type { ReactNode } from "react";
import { Button } from "@/components/custom/Button";

/**
 * One irreversible action on the console, with its confirmation and its
 * reason for being unavailable.
 *
 * The card's own rule is that an action the account cannot take right now is a
 * disabled button with the reason in a tooltip, never a sentence elsewhere on
 * the page — and that anything irreversible goes through a dialog naming what
 * it removes. Both live here so the user page, the token list and the group
 * page draw the same control rather than three near-copies.
 *
 * `disabledReason` and `disabled` are separate on purpose: a control can be
 * disabled because the action is unavailable (permanent, explained in the
 * tooltip) or because a write is in flight (transient, no explanation worth
 * reading).
 */
export function DangerZone({
  label,
  description,
  confirmLabel,
  title,
  body,
  disabled = false,
  disabledReason,
  isPending = false,
  onConfirm,
}: {
  label: ReactNode;
  description?: ReactNode;
  /** Short text on the trigger's confirmation dialog heading. */
  title: ReactNode;
  /** What exactly the action removes, named plainly. */
  body: ReactNode;
  confirmLabel: ReactNode;
  disabled?: boolean;
  /** Shown in a tooltip when `disabled`; omit when the reason is self-evident. */
  disabledReason?: ReactNode;
  isPending?: boolean;
  onConfirm: () => void;
}) {
  const dialog = useOverlayState();
  const trigger = (
    <Button
      variant="danger-soft"
      isDisabled={disabled || isPending}
      isPending={isPending}
      onPress={() => dialog.setOpen(true)}
    >
      {label}
    </Button>
  );

  return (
    <div className="flex flex-col gap-2">
      {/* A disabled button emits no hover or focus for a tooltip to answer, so
          the tooltip listens on the trigger wrapper instead. */}
      {disabled && disabledReason !== undefined ? (
        <Tooltip delay={0}>
          <Tooltip.Trigger>{trigger}</Tooltip.Trigger>
          <Tooltip.Content>{disabledReason}</Tooltip.Content>
        </Tooltip>
      ) : (
        trigger
      )}
      {description !== undefined && (
        <Description className="text-xs text-muted">{description}</Description>
      )}

      <AlertDialog isOpen={dialog.isOpen} onOpenChange={dialog.setOpen}>
        <AlertDialog.Backdrop>
          <AlertDialog.Container placement="center" size="md">
            <AlertDialog.Dialog>
              <AlertDialog.Header>
                <AlertDialog.Heading>{title}</AlertDialog.Heading>
              </AlertDialog.Header>
              <AlertDialog.Body>{body}</AlertDialog.Body>
              <AlertDialog.Footer>
                <Button
                  variant="secondary"
                  isDisabled={isPending}
                  onPress={() => dialog.setOpen(false)}
                >
                  <Trans id="admin.danger.cancel">Cancel</Trans>
                </Button>
                <Button
                  variant="danger"
                  isPending={isPending}
                  onPress={() => {
                    onConfirm();
                    dialog.setOpen(false);
                  }}
                >
                  {confirmLabel}
                </Button>
              </AlertDialog.Footer>
            </AlertDialog.Dialog>
          </AlertDialog.Container>
        </AlertDialog.Backdrop>
      </AlertDialog>
    </div>
  );
}

/** A heading for a block of dangerous actions; the label is the block's title. */
export function DangerZoneTitle({ children }: { children: ReactNode }) {
  return (
    <Label className="text-sm font-medium text-foreground">{children}</Label>
  );
}
