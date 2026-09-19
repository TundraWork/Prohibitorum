import { Alert } from "@heroui/react";
import { Trans } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import { useId } from "react";
import { FormMessages } from "@/components/custom/FormMessages";
import { useFormContext } from "@/forms/context";

export function FormError() {
  const form = useFormContext();
  const errors = useStore(form.store, (state) => state.errors);
  const headingId = useId();
  if (!errors.length) return null;
  return (
    <Alert
      status="danger"
      data-form-error-summary
      tabIndex={-1}
      role="alert"
      aria-labelledby={headingId}
    >
      <Alert.Indicator />
      <Alert.Content>
        <Alert.Title id={headingId} render={(props) => <h3 {...props} />}>
          <Trans id="forms.error.title">Unable to submit</Trans>
        </Alert.Title>
        <Alert.Description>
          <FormMessages errors={errors.flat(Infinity)} />
        </Alert.Description>
      </Alert.Content>
    </Alert>
  );
}
