import { useStore } from "@tanstack/react-form";
import { useFieldContext, useFormContext } from "@/forms/context";
import { withoutServerErrors } from "@/forms/server-errors";

/**
 * What every form field component needs from its TanStack field: the errors to
 * show, whether the control is disabled, and change and blur handlers that
 * hold still while the form submits and clear a server error once edited.
 */
export function useFieldBinding<TValue>(isDisabled: boolean) {
  const field = useFieldContext<TValue>();
  const form = useFormContext();
  const submitting = useStore(form.store, (state) => state.isSubmitting);
  const errors: unknown[] = field.state.meta.errors.flat(Infinity);

  return {
    field,
    errors,
    invalid: errors.length > 0,
    disabled: submitting || isDisabled,
    onBlur() {
      if (!form.state.isSubmitting && !isDisabled) field.handleBlur();
    },
    onChange(value: TValue) {
      if (form.state.isSubmitting || isDisabled) return;
      field.setErrorMap({
        onSubmit: withoutServerErrors(field.state.meta.errorMap.onSubmit),
      });
      field.handleChange(value);
    },
  };
}
