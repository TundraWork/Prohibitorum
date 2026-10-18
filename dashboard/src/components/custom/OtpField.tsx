import { useLingui } from "@lingui/react/macro";
import type { ReactNode } from "react";
import type { FieldVariant } from "@/components/custom/FormField";
import { FormMessages } from "@/components/custom/FormMessages";
import { useFieldBinding } from "@/components/custom/use-field-binding";
import {
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
} from "@/components/ui/field";
import {
  OTPField,
  OTPFieldInput,
  OTPFieldSeparator,
} from "@/components/ui/otp-field";

/** Digits only: the default for a numeric authenticator code. */
const digitsOnly = "^\\d+$";

/**
 * One-time-code field. Same label, description, error and disable wiring as
 * `FormField`, with one input per character replacing the single text input.
 * Digits by default; pass `pattern`/`inputMode` for letter-bearing codes.
 */
export function OtpField({
  label,
  description,
  digits,
  isDisabled = false,
  pattern = digitsOnly,
  inputMode = "numeric",
  size,
}: {
  label: ReactNode;
  description?: ReactNode;
  digits: number;
  isDisabled?: boolean;
  /** Accepted for compatibility; see `FieldVariant`. */
  variant?: FieldVariant;
  /**
   * Accepted characters, as a regular expression a single character is tested
   * against. Defaults to digits.
   */
  pattern?: string;
  /** Soft-keyboard hint; keep it in step with `pattern`. */
  inputMode?: "numeric" | "text";
  size?: "default" | "lg";
}) {
  const { t } = useLingui();
  const length = Math.max(1, Math.trunc(digits));
  const { field, errors, invalid, disabled, onBlur, onChange } =
    useFieldBinding<string>(isDisabled);
  const count = length;
  const splitAt = Math.ceil(count / 2);
  const accepted = new RegExp(pattern);
  const positions = Array.from({ length: count }, (_, position) => position);
  const slots = (from: number, to: number) =>
    positions.slice(from, to).map((index) => {
      const position = index + 1;
      return (
        <OTPFieldInput
          key={index}
          // The first slot is named by the field label; the rest need a name
          // of their own so each one announces its position.
          aria-label={
            index === 0
              ? undefined
              : t({
                  id: "otp-field.character",
                  message: `Character ${position} of ${length}`,
                })
          }
        />
      );
    });

  return (
    <Field disabled={disabled} invalid={invalid} name={field.name}>
      <FieldLabel>{label}</FieldLabel>
      <OTPField
        length={count}
        size={size}
        value={field.state.value}
        onValueChange={onChange}
        validationType="none"
        normalizeValue={(value) =>
          [...value].filter((character) => accepted.test(character)).join("")
        }
        inputMode={inputMode}
        autoComplete="one-time-code"
        // Moving between the slots is not leaving the field.
        onBlur={(event) => {
          if (!event.currentTarget.contains(event.relatedTarget)) onBlur();
        }}
      >
        {slots(0, splitAt)}
        {count > splitAt && <OTPFieldSeparator aria-hidden="true" />}
        {slots(splitAt, count)}
      </OTPField>
      {description && <FieldDescription>{description}</FieldDescription>}
      {invalid && (
        <FieldError match>
          <FormMessages errors={errors} />
        </FieldError>
      )}
    </Field>
  );
}
