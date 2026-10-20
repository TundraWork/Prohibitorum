import { useLingui } from "@lingui/react/macro";
import { type ReactNode, useId } from "react";
import { ChoiceTile } from "@/components/custom/ChoiceTile";
import { FormMessages } from "@/components/custom/FormMessages";
import { useFieldBinding } from "@/components/custom/use-field-binding";
import { Field, FieldError } from "@/components/ui/field";
import { RadioGroup } from "@/components/ui/radio-group";
import type { ProviderOption } from "@/pages/admin/identity-providers/provider-options";

/**
 * A closed set of choices as a form field, each a card with what picking it
 * does under its name.
 *
 * A choice that cannot be picked in the form's current state is listed in
 * `unavailable` with the reason, which is drawn under the group rather than in
 * the dimmed card so it keeps its own contrast; the group names it as its
 * description. A literal protocol value such as `client_secret_basic` is drawn
 * in monospace, the way the provider's own console spells it.
 */
export function ProviderChoiceField<T extends string>({
  label,
  options,
  description,
  unavailable,
}: {
  label: ReactNode;
  options: readonly (ProviderOption<T> & { literal?: boolean })[];
  /** What the group as a whole is about, under the choices. */
  description?: ReactNode;
  /**
   * `yieldsToError` hides the reason while the field has an error, for a caller
   * whose error says the same thing.
   */
  unavailable?: {
    values: readonly T[];
    reason: ReactNode;
    yieldsToError?: boolean;
  };
}) {
  const { i18n } = useLingui();
  const { field, errors, invalid, disabled, onChange } =
    useFieldBinding<T>(false);
  const labelId = useId();
  const descriptionId = useId();
  const reasonId = useId();
  const errorId = useId();
  const showReason =
    unavailable !== undefined && !(unavailable.yieldsToError && invalid);

  return (
    <Field
      name={field.name}
      invalid={invalid}
      disabled={disabled}
      className="items-stretch"
    >
      <span id={labelId} className="font-medium text-sm">
        {label}
      </span>
      <RadioGroup
        aria-labelledby={labelId}
        aria-describedby={
          [
            description !== undefined && descriptionId,
            showReason && reasonId,
            invalid && errorId,
          ]
            .filter(Boolean)
            .join(" ") || undefined
        }
        aria-invalid={invalid || undefined}
        name={field.name}
        value={field.state.value}
        disabled={disabled}
        onValueChange={(next) => onChange(next as T)}
      >
        {options.map((option) => (
          <ChoiceTile
            key={option.value}
            layout="list"
            value={option.value}
            disabled={unavailable?.values.includes(option.value)}
            label={
              option.literal === true ? (
                <span className="font-mono text-[13px]">
                  {i18n._(option.label)}
                </span>
              ) : (
                i18n._(option.label)
              )
            }
            description={i18n._(option.description)}
          />
        ))}
      </RadioGroup>
      {description !== undefined && (
        <p id={descriptionId} className="text-muted-foreground text-caption">
          {description}
        </p>
      )}
      {showReason && (
        <p id={reasonId} className="text-muted-foreground text-caption">
          {unavailable.reason}
        </p>
      )}
      {invalid && (
        <FieldError match id={errorId}>
          <FormMessages errors={errors} />
        </FieldError>
      )}
    </Field>
  );
}
