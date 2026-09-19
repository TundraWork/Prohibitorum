import { Button, Spinner } from "@heroui/react";
import { Trans } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import type { ReactNode } from "react";
import { useFormContext } from "@/forms/context";

export function SubmitButton({ children }: { children: ReactNode }) {
  const form = useFormContext();
  const submitting = useStore(form.store, (state) => state.isSubmitting);
  return (
    <div className="actions">
      <Button type="submit" isPending={submitting}>
        {({ isPending }) => (
          <>
            {isPending && <Spinner size="sm" color="current" />}
            {isPending ? (
              <Trans id="forms.submitting">Submitting…</Trans>
            ) : (
              children
            )}
          </>
        )}
      </Button>
    </div>
  );
}
