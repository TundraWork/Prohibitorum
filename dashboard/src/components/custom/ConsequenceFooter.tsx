import { Trans } from "@lingui/react/macro";
import { TriangleAlertIcon } from "lucide-react";
import type { ReactNode } from "react";
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
}: {
  consequence?: ReactNode;
  /** Names the action, such as "Remove scope and save". */
  actionLabel: ReactNode;
  onDiscard: () => void;
  pending?: boolean;
  disabled?: boolean;
}): React.ReactElement {
  return (
    <div className="flex w-full flex-wrap items-center gap-x-3 gap-y-3">
      {consequence !== undefined && (
        <>
          <TriangleAlertIcon
            aria-hidden="true"
            className="size-4 shrink-0 text-warning"
          />
          <p className="min-w-0 flex-1 basis-56 text-foreground text-sm">
            {consequence}
          </p>
        </>
      )}
      <div className="ms-auto flex gap-2">
        <Button
          type="button"
          variant="ghost"
          size="sm"
          disabled={pending}
          onClick={onDiscard}
        >
          <Trans id="consequence-footer.discard">Discard</Trans>
        </Button>
        <Button type="submit" size="sm" pending={pending} disabled={disabled}>
          {actionLabel}
        </Button>
      </div>
    </div>
  );
}
