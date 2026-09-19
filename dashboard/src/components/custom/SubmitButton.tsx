import { Button, Spinner } from "@heroui/react";
import { Trans } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import type { ReactNode } from "react";
import { useFormContext } from "@/forms/context";

export function SubmitButton({
  children,
  fullWidth = false,
}: {
  children: ReactNode;
  fullWidth?: boolean;
}) {
  const form = useFormContext();
  const submitting = useStore(form.store, (state) => state.isSubmitting);
  return (
    <div className="flex flex-wrap gap-2">
      <Button type="submit" fullWidth={fullWidth} isPending={submitting}>
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
