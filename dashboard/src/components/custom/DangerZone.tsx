import { type ReactNode, useState } from "react";
import { Button } from "@/components/custom/Button";
import { ConfirmDialog } from "@/components/custom/ConfirmDialog";
import { Section } from "@/components/custom/Section";
import { Tooltip, TooltipPopup, TooltipTrigger } from "@/components/ui/tooltip";

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
  accessibleLabel,
  description,
  confirmLabel,
  title,
  body,
  disabled = false,
  disabledReason,
  isPending = false,
  size,
  onConfirm,
}: {
  label: ReactNode;
  /**
   * The trigger's accessible name, for a row where the visible label alone
   * does not say which record it acts on ("Revoke" beside each of several
   * tokens). It starts with the visible label, so speech input still finds it.
   */
  accessibleLabel?: string;
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
  /** `sm` inside a list row, beside the row's other small controls. */
  size?: "sm" | "default";
  onConfirm: () => void;
}) {
  const [open, setOpen] = useState(false);
  const trigger = (
    <Button
      aria-label={accessibleLabel}
      disabled={disabled}
      pending={isPending}
      onClick={() => setOpen(true)}
      variant="destructive-outline"
      size={size}
    >
      {label}
    </Button>
  );

  return (
    <div className="flex flex-col gap-2">
      {/* A disabled button emits no hover or focus for a tooltip to answer, so
          the tooltip listens on the trigger wrapper instead. */}
      {disabled && disabledReason !== undefined ? (
        <Tooltip>
          <TooltipTrigger
            // A disabled button cannot take focus, so the wrapper carries it.
            // biome-ignore lint/a11y/noNoninteractiveTabindex: see above
            render={<span className="inline-flex w-fit" tabIndex={0} />}
          >
            {trigger}
          </TooltipTrigger>
          <TooltipPopup>{disabledReason}</TooltipPopup>
        </Tooltip>
      ) : (
        trigger
      )}
      {description !== undefined && (
        <p className="text-muted-foreground text-xs">{description}</p>
      )}

      <ConfirmDialog
        isOpen={open}
        onOpenChange={setOpen}
        status="danger"
        title={title}
        body={body}
        confirmLabel={confirmLabel}
        isPending={isPending}
        onConfirm={() => {
          onConfirm();
          setOpen(false);
        }}
      />
    </div>
  );
}

/**
 * A block of dangerous actions as a destructive-tinted frame: each row is a
 * title, its consequence and its action, on one panel.
 *
 * Rows are an `ItemList` of `ItemListRow`s, so the frame's panel is the list's.
 */
export function DangerZoneFrame({
  id,
  icon,
  title,
  children,
}: {
  /** Anchor for the page outline's link to the block. */
  id?: string;
  icon?: ReactNode;
  title: ReactNode;
  children: ReactNode;
}) {
  return (
    <Section id={id} icon={icon} title={title} tone="danger">
      {children}
    </Section>
  );
}
