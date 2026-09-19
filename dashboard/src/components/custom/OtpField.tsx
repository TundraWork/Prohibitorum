import {
  Description,
  FieldError,
  InputOTP,
  Label,
  REGEXP_ONLY_DIGITS,
  TextField,
} from "@heroui/react";
import { useStore } from "@tanstack/react-form";
import { type ComponentProps, Fragment, type ReactNode, useId } from "react";
import { FormMessages } from "@/components/custom/FormMessages";
import { useFieldContext, useFormContext } from "@/forms/context";
import { withoutServerErrors } from "@/forms/server-errors";

/**
 * Numeric one-time-code field. Same label, description, error and disable
 * wiring as `FormField`, with HeroUI's `InputOTP` slots replacing the single
 * text input.
 */
export function OtpField({
  label,
  description,
  digits,
  isDisabled = false,
  variant,
}: {
  label: ReactNode;
  description?: ReactNode;
  digits: number;
  isDisabled?: boolean;
  /** HeroUI input variant. Use `secondary` when the field sits on a surface. */
  variant?: ComponentProps<typeof InputOTP>["variant"];
}) {
  const field = useFieldContext<string>();
  const form = useFormContext();
  const submitting = useStore(form.store, (state) => state.isSubmitting);
  const id = useId();
  const errors: unknown[] = field.state.meta.errors.flat(Infinity);
  const invalid = errors.length > 0;
  const descriptionId = `${id}-description`;
  const errorId = `${id}-error`;
  const disabled = submitting || isDisabled;
  const count = Math.max(1, Math.trunc(digits));
  const splitAt = Math.ceil(count / 2);
  const groups = [
    Array.from({ length: splitAt }, (_, index) => index),
    Array.from({ length: count - splitAt }, (_, index) => splitAt + index),
  ].filter((group) => group.length > 0);

  function handleChange(value: string) {
    if (form.state.isSubmitting || isDisabled) return;
    field.setErrorMap({
      onSubmit: withoutServerErrors(field.state.meta.errorMap.onSubmit),
    });
    field.handleChange(value);
  }

  return (
    <TextField
      name={field.name}
      value={field.state.value}
      isDisabled={disabled}
      isInvalid={invalid}
      validationBehavior="aria"
    >
      <Label htmlFor={id}>{label}</Label>
      <InputOTP
        id={id}
        name={field.name}
        maxLength={count}
        pattern={REGEXP_ONLY_DIGITS}
        value={field.state.value}
        onChange={handleChange}
        onBlur={() => {
          if (!form.state.isSubmitting && !isDisabled) field.handleBlur();
        }}
        isDisabled={disabled}
        isInvalid={invalid}
        inputMode="numeric"
        autoComplete="one-time-code"
        variant={variant}
        textAlign="center"
        aria-invalid={invalid || undefined}
        aria-describedby={
          [description && descriptionId, invalid && errorId]
            .filter(Boolean)
            .join(" ") || undefined
        }
        className="justify-center"
      >
        {groups.map((group) => (
          <Fragment key={group[0]}>
            {group[0] !== 0 && <InputOTP.Separator />}
            <InputOTP.Group className="flex-1">
              {group.map((index) => (
                <InputOTP.Slot key={index} index={index} />
              ))}
            </InputOTP.Group>
          </Fragment>
        ))}
      </InputOTP>
      {description && (
        <Description id={descriptionId}>{description}</Description>
      )}
      {invalid && (
        <FieldError id={errorId}>
          <FormMessages errors={errors} />
        </FieldError>
      )}
    </TextField>
  );
}
