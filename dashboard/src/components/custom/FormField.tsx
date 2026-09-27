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
  placeholder,
  isDisabled = false,
  isMonospace = false,
  autoFocus = false,
  variant,
  className,
}: {
  label: ReactNode;
  description?: ReactNode;
  inputMode?: "text" | "numeric" | "url";
  type?: ComponentProps<typeof Input>["type"];
  autoComplete?: string;
  autoCapitalize?: string;
  spellCheck?: boolean;
  /** An example value, for a field whose format is easier shown than said. */
  placeholder?: string;
  isDisabled?: boolean;
  /**
   * For a literal protocol value — an issuer URL, a client ID — that has to
   * match another system's copy character for character.
   */
  isMonospace?: boolean;
  /**
   * Takes focus on mount, for the field a dialog opens on so the step can be
   * finished without reaching for the pointer.
   */
  autoFocus?: boolean;
  /** HeroUI input variant. Use `secondary` when the field sits on a surface. */
  variant?: ComponentProps<typeof Input>["variant"];
  /**
   * Layout classes for the field. A field in a grid column takes its reading
   * width here rather than stretching to the column, which is wider than a
   * value wants to be read across.
   */
  className?: string;
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
      className={className}
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
        className={isMonospace ? "font-mono" : undefined}
        inputMode={inputMode}
        type={type}
        autoComplete={autoComplete}
        autoCapitalize={autoCapitalize}
        spellCheck={spellCheck}
        placeholder={placeholder}
        autoFocus={autoFocus}
        variant={variant}
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
