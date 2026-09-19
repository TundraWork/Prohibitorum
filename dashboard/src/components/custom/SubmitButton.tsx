import { Button, Spinner } from "@heroui/react";
import { Trans } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import type { ReactNode } from "react";
import { useFormContext } from "@/forms/context";

export function SubmitButton({ children }: { children: ReactNode }) {
  const form = useFormContext();
  const submitting = useStore(form.store, (state) => state.isSubmitting);
  return (
    <div className="actions submit-feedback">
      <Button type="submit" isDisabled={submitting}>
        {children}
      </Button>
      <span role="status">
        {submitting && (
          <>
            <span aria-hidden="true">
              <Spinner size="sm" color="current" />
            </span>
            <Trans id="forms.submitting">Submitting…</Trans>
          </>
        )}
      </span>
    </div>
  );
}
