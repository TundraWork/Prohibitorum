import { Trans } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import { TriangleAlert } from "lucide-react";
import type { ReactNode } from "react";
import { Button } from "@/components/custom/Button";
import { useFormContext } from "@/forms/context";

/**
 * The form's submit button.
 *
 * `tone="warning"` is for a save that carries a consequence the reader should
 * weigh — a change of the identifier a service keys its users on, or removing
 * a scope tokens were granted. The form confirms that save when it is
 * submitted, and the button says so beforehand: the warning colour, and an icon
 * in front of the label, so the state is not carried by colour alone.
 *
 * `isDisabled` and `variant="secondary"` are for a form that shares its surface
 * with another way to finish the same step: while that other way is in flight
 * this one cannot start, and when the other way is the one to reach for first,
 * this one steps back to the secondary style.
 */
export function SubmitButton({
  children,
  fullWidth = false,
  tone = "default",
  isDisabled = false,
  variant = "primary",
}: {
  children: ReactNode;
  fullWidth?: boolean;
  tone?: "default" | "warning";
  isDisabled?: boolean;
  variant?: "primary" | "secondary";
}) {
  const form = useFormContext();
  const submitting = useStore(form.store, (state) => state.isSubmitting);
  return (
    <div className="flex flex-wrap gap-2">
      <Button
        type="submit"
        fullWidth={fullWidth}
        isPending={submitting}
        isDisabled={isDisabled}
        variant={
          tone === "warning"
            ? "warning"
            : variant === "secondary"
              ? "secondary"
              : undefined
        }
      >
        {({ isPending }) =>
          isPending ? (
            <Trans id="forms.submitting">Submitting…</Trans>
          ) : (
            <>
              {tone === "warning" && (
                <TriangleAlert size={16} aria-hidden="true" />
              )}
              {children}
            </>
          )
        }
      </Button>
    </div>
  );
}
