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
 * `tone="destructive"` is the same warning for a save that grants more than
 * can be taken back by the reader alone, such as a token that skips the
 * identity check: the destructive colour with the same icon.
 *
 * `disabled` and `variant="outline"` are for a form that shares its surface
 * with another way to finish the same step: while that other way is in flight
 * this one cannot start, and when the other way is the one to reach for first,
 * this one steps back to the outline style.
 */
export function SubmitButton({
  children,
  fullWidth = false,
  tone = "default",
  disabled = false,
  variant = "default",
  size = "default",
}: {
  children: ReactNode;
  fullWidth?: boolean;
  tone?: "default" | "warning" | "destructive";
  disabled?: boolean;
  variant?: "default" | "outline";
  /** `lg` for a page whose form is its main event, as the sign-in steps are. */
  size?: "default" | "lg";
}) {
  const form = useFormContext();
  const submitting = useStore(form.store, (state) => state.isSubmitting);
  return (
    <div className="flex flex-wrap gap-2">
      <Button
        type="submit"
        className={fullWidth ? "w-full" : undefined}
        pending={submitting}
        disabled={disabled}
        variant={
          tone === "destructive"
            ? "destructive"
            : tone === "warning"
              ? "default"
              : variant
        }
        size={size}
      >
        {submitting ? (
          <Trans id="forms.submitting">Submitting…</Trans>
        ) : (
          <>
            {tone !== "default" && (
              <TriangleAlert size={16} aria-hidden="true" />
            )}
            {children}
          </>
        )}
      </Button>
    </div>
  );
}
