import { Description, FieldError, Label, ListBox, Select } from "@heroui/react";
import type { MessageDescriptor } from "@lingui/core";
import { useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import type { ReactNode } from "react";
import { FormMessages } from "@/components/custom/FormMessages";
import { useFieldContext, useFormContext } from "@/forms/context";
import { withoutServerErrors } from "@/forms/server-errors";

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
 * name, then what picking it does, the choices whose cost the reader has to weigh before picking — which
 * account fact an application knows someone by, how a provider treats someone
 * new, how this instance proves itself to it.
 *
 * The trigger shows the name alone. The description is for choosing, and a
 * trigger that repeated it would be two lines tall on every visit. An option
 * that cannot be picked in the form's current state is `disabledKeys`; the
 * caller says why in `description` or in its submit check.
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
  const field = useFieldContext<T>();
  const form = useFormContext();
  const submitting = useStore(form.store, (state) => state.isSubmitting);
  const errors: unknown[] = field.state.meta.errors.flat(Infinity);
  const invalid = errors.length > 0;
  const selected = options.find((option) => option.value === field.state.value);

  return (
    <Select
      className={className}
      variant="secondary"
      name={field.name}
      value={field.state.value}
      disabledKeys={disabledKeys}
      isDisabled={submitting}
      isInvalid={invalid}
      onChange={(key) => {
        const option = options.find((candidate) => candidate.value === key);
        if (option === undefined || form.state.isSubmitting) return;
        field.setErrorMap({
          onSubmit: withoutServerErrors(field.state.meta.errorMap.onSubmit),
        });
        field.handleChange(option.value);
      }}
    >
      <Label className={isLabelHidden ? "sr-only" : undefined}>{label}</Label>
      <Select.Trigger>
        <Select.Value>
          {({ isPlaceholder }) =>
            isPlaceholder || selected === undefined ? null : (
              <span className={selected.literal ? "font-mono" : undefined}>
                {i18n._(selected.label)}
              </span>
            )
          }
        </Select.Value>
        <Select.Indicator />
      </Select.Trigger>
      {description !== undefined && <Description>{description}</Description>}
      {invalid && (
        <FieldError>
          <FormMessages errors={errors} />
        </FieldError>
      )}
      {/* Held to the trigger's width, so each description wraps under its
          name instead of widening the list past the field it opened from. */}
      <Select.Popover className="w-(--trigger-width)">
        <ListBox>
          {options.map((option) => (
            <ListBox.Item
              id={option.value}
              key={option.value}
              textValue={i18n._(option.label)}
            >
              <div className="flex min-w-0 flex-col">
                <Label className={option.literal ? "font-mono" : undefined}>
                  {i18n._(option.label)}
                </Label>
                <Description className="text-pretty">
                  {i18n._(option.description)}
                </Description>
              </div>
              <ListBox.ItemIndicator />
            </ListBox.Item>
          ))}
        </ListBox>
      </Select.Popover>
    </Select>
  );
}
