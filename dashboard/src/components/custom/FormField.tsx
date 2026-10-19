import type { ComponentProps, ReactNode } from "react";
import { FormMessages } from "@/components/custom/FormMessages";
import { useFieldBinding } from "@/components/custom/use-field-binding";
import {
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
} from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";

/**
 * Kept so a field can still be told which surface it sits on. coss inputs read
 * the same on a page and on a card, so it no longer changes anything.
 */
export type FieldVariant = "primary" | "secondary";

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
  compactValue = false,
  autoFocus = false,
  size,
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
   * The console's value size for a monospace field: 16px below `sm`, so iOS
   * does not zoom on focus, and 13px from `sm` up, matching `AddressInput`.
   * Public pages leave it off and keep the input's own sizes.
   */
  compactValue?: boolean;
  /**
   * Takes focus on mount, for the field a dialog opens on so the step can be
   * finished without reaching for the pointer.
   */
  autoFocus?: boolean;
  /** Accepted for compatibility; see `FieldVariant`. */
  variant?: FieldVariant;
  /** The input's height; `lg` is for a page whose controls are the main event. */
  size?: ComponentProps<typeof Input>["size"];
  /**
   * Layout classes for the field. A field in a grid column takes its reading
   * width here rather than stretching to the column, which is wider than a
   * value wants to be read across.
   */
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
      <Input
        size={size}
        className={cn(
          isMonospace && "font-mono",
          compactValue && "text-base sm:text-[13px]",
        )}
        value={field.state.value}
        onValueChange={onChange}
        onBlur={onBlur}
        inputMode={inputMode}
        type={type}
        autoComplete={autoComplete}
        autoCapitalize={autoCapitalize}
        spellCheck={spellCheck}
        placeholder={placeholder}
        autoFocus={autoFocus}
        aria-invalid={invalid || undefined}
      />
      {description && <FieldDescription>{description}</FieldDescription>}
      {invalid && (
        <FieldError match>
          <FormMessages errors={errors} />
        </FieldError>
      )}
    </Field>
  );
}
