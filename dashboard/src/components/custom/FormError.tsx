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
    <section
      className="form-error-summary"
      data-form-error-summary
      tabIndex={-1}
      role="alert"
      aria-labelledby={headingId}
    >
      <h3 id={headingId}>
        <Trans id="forms.error.title">Unable to submit</Trans>
      </h3>
      <FormMessages errors={errors.flat(Infinity)} />
    </section>
  );
}
