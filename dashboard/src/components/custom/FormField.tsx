import {
  Description,
  FieldError,
  Input,
  Label,
  TextField,
} from "@heroui/react";
import { useStore } from "@tanstack/react-form";
import { type ComponentProps, type ReactNode, useId } from "react";
import { FormMessages } from "@/components/custom/FormMessages";
import { useFieldContext, useFormContext } from "@/forms/context";
import { withoutServerErrors } from "@/forms/server-errors";

export function FormField({
  label,
  description,
  inputMode,
  type,
  autoComplete,
  autoCapitalize,
  spellCheck,
  isDisabled = false,
}: {
  label: ReactNode;
  description?: ReactNode;
  inputMode?: "text" | "numeric";
  type?: ComponentProps<typeof Input>["type"];
  autoComplete?: string;
  autoCapitalize?: string;
  spellCheck?: boolean;
  isDisabled?: boolean;
}) {
  const field = useFieldContext<string>();
  const form = useFormContext();
  const submitting = useStore(form.store, (state) => state.isSubmitting);
  const id = useId();
  const errors: unknown[] = field.state.meta.errors.flat(Infinity);
  const invalid = errors.length > 0;
  const descriptionId = `${id}-description`;
  const errorId = `${id}-error`;
  return (
    <TextField
      name={field.name}
      value={field.state.value}
      isDisabled={submitting || isDisabled}
      isInvalid={invalid}
      validationBehavior="aria"
      onBlur={() => {
        if (!form.state.isSubmitting && !isDisabled) field.handleBlur();
      }}
      onChange={(value) => {
        if (form.state.isSubmitting || isDisabled) return;
        field.setErrorMap({
          onSubmit: withoutServerErrors(field.state.meta.errorMap.onSubmit),
        });
        field.handleChange(value);
      }}
    >
      <Label htmlFor={id}>{label}</Label>
      <Input
        id={id}
        inputMode={inputMode}
        type={type}
        autoComplete={autoComplete}
        autoCapitalize={autoCapitalize}
        spellCheck={spellCheck}
        aria-invalid={invalid || undefined}
        aria-describedby={
          [description && descriptionId, invalid && errorId]
            .filter(Boolean)
            .join(" ") || undefined
        }
      />
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
