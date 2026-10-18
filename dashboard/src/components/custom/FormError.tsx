import { Trans } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import { CircleAlertIcon } from "lucide-react";
import { useId } from "react";
import { FormMessages } from "@/components/custom/FormMessages";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { useFormContext } from "@/forms/context";

export function FormError() {
  const form = useFormContext();
  const errors = useStore(form.store, (state) => state.errors);
  const headingId = useId();
  if (!errors.length) return null;
  return (
    <Alert
      variant="error"
      data-form-error-summary
      tabIndex={-1}
      aria-labelledby={headingId}
    >
      <CircleAlertIcon aria-hidden="true" />
      <AlertTitle>
        <h3 id={headingId}>
          <Trans id="forms.error.title">Unable to submit</Trans>
        </h3>
      </AlertTitle>
      <AlertDescription>
        <FormMessages errors={errors.flat(Infinity)} />
      </AlertDescription>
    </Alert>
  );
}
