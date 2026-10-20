import type { MessageDescriptor } from "@lingui/core";
import { useLingui } from "@lingui/react/macro";
import type { ReactNode } from "react";
import { FormMessages } from "@/components/custom/FormMessages";
import { useFieldBinding } from "@/components/custom/use-field-binding";
import {
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
} from "@/components/ui/field";
import {
  Select,
  SelectItem,
  SelectPopup,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { cn } from "@/lib/utils";

/** One choice: its name, and a line saying what picking it does. */
export interface SelectOption<T extends string> {
  value: T;
  label: MessageDescriptor;
  description: MessageDescriptor;
  /**
   * The name is a literal protocol value, such as `client_secret_basic`, and
   * is drawn in monospace the way the provider's own console spells it.
   */
  literal?: boolean;
}

/**
 * A closed set of choices as a form field, each option two lines tall: its
 * name, then what picking it does. These are the choices whose cost the reader
 * has to weigh before picking: which account fact an application knows someone
 * by, how a provider treats someone new, how this instance proves itself to it.
 *
 * The trigger shows the name alone, and the chosen option's explanation sits
 * under the field as its description, so a trigger that repeated it would not
 * be two lines tall on every visit. An option that cannot be picked in the
 * form's current state is `disabledKeys`; the caller says why in `description`
 * or in its submit check.
 */
export function OptionSelectField<T extends string>({
  label,
  options,
  description,
  disabledKeys,
  isLabelHidden = false,
  className = "w-full",
}: {
  label: ReactNode;
  options: readonly SelectOption<T>[];
  description?: ReactNode;
  disabledKeys?: readonly T[];
  /**
   * For a field whose name is already on screen beside it, such as a header
   * table's own column: the label stays for assistive technology.
   */
  isLabelHidden?: boolean;
  /**
   * Layout classes for the field. A select in a wide column takes its reading
   * width here rather than stretching across the column.
   */
  className?: string;
}) {
  const { i18n } = useLingui();
  const { field, errors, invalid, disabled, onChange } =
    useFieldBinding<T>(false);
  const selected = options.find((option) => option.value === field.state.value);

  return (
    <Field
      className={className}
      disabled={disabled}
      invalid={invalid}
      name={field.name}
    >
      <FieldLabel className={isLabelHidden ? "sr-only" : undefined}>
        {label}
      </FieldLabel>
      <Select
        value={field.state.value}
        disabled={disabled}
        onValueChange={(next) => {
          if (options.some((option) => option.value === next)) {
            onChange(next as T);
          }
        }}
      >
        <SelectTrigger aria-invalid={invalid || undefined}>
          <SelectValue>
            {() =>
              selected === undefined ? null : (
                <span className={cn(selected.literal && "font-mono")}>
                  {i18n._(selected.label)}
                </span>
              )
            }
          </SelectValue>
        </SelectTrigger>
        <SelectPopup alignItemWithTrigger={false}>
          {options.map((option) => (
            <SelectItem
              key={option.value}
              value={option.value}
              disabled={disabledKeys?.includes(option.value)}
            >
              <div className="flex min-w-0 flex-col">
                <span className={cn(option.literal && "font-mono")}>
                  {i18n._(option.label)}
                </span>
                <span className="text-pretty text-muted-foreground text-caption">
                  {i18n._(option.description)}
                </span>
              </div>
            </SelectItem>
          ))}
        </SelectPopup>
      </Select>
      {description !== undefined && (
        <FieldDescription>{description}</FieldDescription>
      )}
      {selected !== undefined && (
        <FieldDescription>{i18n._(selected.description)}</FieldDescription>
      )}
      {invalid && (
        <FieldError match>
          <FormMessages errors={errors} />
        </FieldError>
      )}
    </Field>
  );
}
