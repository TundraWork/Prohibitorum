import { useLingui } from "@lingui/react/macro";
import type { ComponentProps, ReactNode } from "react";
import type { FieldVariant } from "@/components/custom/FormField";
import { FormMessages } from "@/components/custom/FormMessages";
import { useFieldBinding } from "@/components/custom/use-field-binding";
import {
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
} from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import {
  NumberField as CossNumberField,
  NumberFieldDecrement,
  NumberFieldGroup,
  NumberFieldIncrement,
  NumberFieldInput,
} from "@/components/ui/number-field";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";

/**
 * The handful of field shapes the management forms need beyond `FormField`:
 * a multi-line text field, a switch, a number, and a read-only value.
 *
 * They live with the other form field components rather than in the pages that
 * needed them first, so a second page gets the same label and error wiring
 * instead of drawing its own.
 */

/** A `Textarea` wrapped like `FormField`, for values too long to sit on one line. */
export function TextAreaField({
  label,
  description,
  rows = 3,
  isDisabled = false,
  spellCheck = true,
  className,
}: {
  label: ReactNode;
  description?: ReactNode;
  rows?: number;
  isDisabled?: boolean;
  spellCheck?: boolean;
  /** Accepted for compatibility; see `FieldVariant`. */
  variant?: FieldVariant;
  className?: string;
}) {
  const { field, errors, invalid, disabled, onBlur, onChange } =
    useFieldBinding<string>(isDisabled);

  return (
    <Field
      className={className}
      disabled={disabled}
      invalid={invalid}
      name={field.name}
    >
      <FieldLabel>{label}</FieldLabel>
      <Textarea
        rows={rows}
        spellCheck={spellCheck}
        value={field.state.value}
        onChange={(event) => onChange(event.target.value)}
        onBlur={onBlur}
        aria-invalid={invalid || undefined}
      />
      {description !== undefined && (
        <FieldDescription>{description}</FieldDescription>
      )}
      {invalid && (
        <FieldError match>
          <FormMessages errors={errors} />
        </FieldError>
      )}
    </Field>
  );
}

/**
 * A boolean field: the label and its description on the left, the switch on
 * the right. A switch is toggled rather than typed, so the errors it can carry
 * are only the ones the server sends back for it.
 */
export function SwitchField({
  label,
  description,
  isDisabled = false,
}: {
  label: ReactNode;
  description?: ReactNode;
  isDisabled?: boolean;
}) {
  const { field, errors, invalid, disabled, onChange } =
    useFieldBinding<boolean>(isDisabled);

  return (
    <Field disabled={disabled} invalid={invalid} name={field.name}>
      <div className="flex w-full items-center justify-between gap-4">
        <div className="flex min-w-0 flex-col gap-0.5">
          <FieldLabel>{label}</FieldLabel>
          {description !== undefined && (
            <FieldDescription>{description}</FieldDescription>
          )}
        </div>
        <Switch
          checked={field.state.value}
          onCheckedChange={onChange}
          aria-invalid={invalid || undefined}
        />
      </div>
      {invalid && (
        <FieldError match>
          <FormMessages errors={errors} />
        </FieldError>
      )}
    </Field>
  );
}

/** The number a text value holds, or null while it holds none. */
function parseNumber(text: string): number | null {
  if (text.trim() === "") return null;
  const value = Number(text);
  return Number.isFinite(value) ? value : null;
}

/** A single-line field whose value is a number, kept as text in the form. */
export function NumberField({
  label,
  description,
  isDisabled = false,
  className,
}: {
  label: ReactNode;
  description?: ReactNode;
  isDisabled?: boolean;
  /** Accepted for compatibility; see `FieldVariant`. */
  variant?: FieldVariant;
  /** Layout classes for the field; see `FormField`'s `className`. */
  className?: string;
}) {
  const { t } = useLingui();
  const { field, errors, invalid, disabled, onBlur, onChange } =
    useFieldBinding<string>(isDisabled);

  return (
    <Field
      className={className}
      disabled={disabled}
      invalid={invalid}
      name={field.name}
    >
      <FieldLabel>{label}</FieldLabel>
      <CossNumberField
        value={parseNumber(field.state.value)}
        // Durations and lifetimes read oddly as "1,440".
        format={{ useGrouping: false }}
        onValueChange={(value) => onChange(value === null ? "" : String(value))}
      >
        <NumberFieldGroup>
          <NumberFieldDecrement
            aria-label={t({ id: "number-field.decrease", message: "Decrease" })}
          />
          <NumberFieldInput
            onBlur={onBlur}
            aria-invalid={invalid || undefined}
          />
          <NumberFieldIncrement
            aria-label={t({ id: "number-field.increase", message: "Increase" })}
          />
        </NumberFieldGroup>
      </CossNumberField>
      {description !== undefined && (
        <FieldDescription>{description}</FieldDescription>
      )}
      {invalid && (
        <FieldError match>
          <FormMessages errors={errors} />
        </FieldError>
      )}
    </Field>
  );
}

/** A read-only field, for a value the server decides. */
export function ReadOnlyField({
  label,
  value,
  description,
  isMonospace = false,
  size,
}: {
  label: ReactNode;
  value: string;
  description?: ReactNode;
  /** For a literal value the reader may have to type somewhere else. */
  isMonospace?: boolean;
  /** Matches the height of the editable fields beside it. */
  size?: ComponentProps<typeof Input>["size"];
}) {
  return (
    <Field>
      <FieldLabel>{label}</FieldLabel>
      <Input
        readOnly
        size={size}
        value={value}
        className={isMonospace ? "font-mono" : undefined}
      />
      {description !== undefined && (
        <FieldDescription>{description}</FieldDescription>
      )}
    </Field>
  );
}
