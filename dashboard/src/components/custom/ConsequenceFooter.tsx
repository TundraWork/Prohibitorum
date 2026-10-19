import { Trans } from "@lingui/react/macro";
import { TriangleAlertIcon } from "lucide-react";
import { type ReactNode, useId } from "react";
import { Button } from "@/components/custom/Button";

/**
 * The footer of a section whose save does more than store a value: a warning
 * icon, what the save will do, and Discard beside a primary button named for
 * the action.
 *
 * It goes in a `Section`'s `footer`, inside the form it submits, so the
 * primary button is the form's submit.
 */
export function ConsequenceFooter({
  consequence,
  actionLabel,
  onDiscard,
  pending = false,
  disabled = false,
  tone = "warning",
}: {
  consequence?: ReactNode;
  /** Names the action, such as "Remove scope and save". */
  actionLabel: ReactNode;
  onDiscard: () => void;
  pending?: boolean;
  disabled?: boolean;
  /**
   * `danger` for a footer whose text is the reason the save is refused rather
   * than what it will do.
   */
  tone?: "warning" | "danger";
}): React.ReactElement {
  const consequenceId = useId();
  return (
    <div
      data-tone={tone}
      className="flex w-full flex-wrap items-center gap-x-3 gap-y-3"
    >
      {consequence != null && (
        <>
          <TriangleAlertIcon
            aria-hidden="true"
            className={
              tone === "danger"
                ? "size-4 shrink-0 text-destructive"
                : "size-4 shrink-0 text-warning"
            }
          />
          <p
            id={consequenceId}
            className="min-w-0 flex-1 basis-56 text-pretty text-foreground text-sm"
          >
            {consequence}
          </p>
        </>
      )}
      <div className="ms-auto flex gap-2">
        <Button
          type="button"
          variant="ghost"
          disabled={pending}
          onClick={onDiscard}
        >
          <Trans id="consequence-footer.discard">Discard</Trans>
        </Button>
        <Button
          type="submit"
          pending={pending}
          disabled={disabled}
          // A button that cannot be pressed says why, where a keyboard finds it.
          aria-describedby={
            disabled && consequence != null ? consequenceId : undefined
          }
        >
          {actionLabel}
        </Button>
      </div>
    </div>
  );
}
