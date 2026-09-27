import { Description, Label, ListBox, Select } from "@heroui/react";
import { useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import type { ReactNode } from "react";
import { readPrincipalSource } from "@/api/federation";
import type { PrincipalSource } from "@/api/raw-admin-paths";
import {
  principalSourceLabel,
  principalSources,
} from "@/components/custom/principal-sources";
import { useFieldContext, useFormContext } from "@/forms/context";
import { withoutServerErrors } from "@/forms/server-errors";

/**
 * Which account fact a downstream application knows someone by, as a form
 * field: the OIDC subject source and the forward-auth `Remote-User`.
 *
 * Every option carries its cost under its name, because the choice is only as
 * good as the reader's picture of what each identifier does over time. The
 * field holds the choice like any other; a save that changes it is what the
 * section confirms, not the selection — a reader can look at each option and
 * change their mind without a dialog in the way.
 *
 * The trigger shows the name alone. The option's description is for choosing,
 * and a trigger that repeated it would be two lines tall on every visit.
 */
export function PrincipalSourceField({
  label,
  isLabelHidden = false,
  description,
  className = "w-full",
}: {
  label: ReactNode;
  /**
   * For a field whose name is already on screen beside it, such as a header
   * table's own column: the label stays for assistive technology.
   */
  isLabelHidden?: boolean;
  description?: ReactNode;
  /**
   * Layout classes for the field. A select in a wide column takes its reading
   * width here rather than stretching across the column.
   */
  className?: string;
}) {
  const { i18n } = useLingui();
  const field = useFieldContext<PrincipalSource>();
  const form = useFormContext();
  const submitting = useStore(form.store, (state) => state.isSubmitting);

  return (
    <Select
      className={className}
      variant="secondary"
      name={field.name}
      value={field.state.value}
      isDisabled={submitting}
      onChange={(key) => {
        const source = readPrincipalSource(key);
        if (source === undefined || form.state.isSubmitting) return;
        field.setErrorMap({
          onSubmit: withoutServerErrors(field.state.meta.errorMap.onSubmit),
        });
        field.handleChange(source);
      }}
    >
      <Label className={isLabelHidden ? "sr-only" : undefined}>{label}</Label>
      <Select.Trigger>
        <Select.Value>
          {({ isPlaceholder }) =>
            isPlaceholder
              ? null
              : i18n._(principalSourceLabel(field.state.value))
          }
        </Select.Value>
        <Select.Indicator />
      </Select.Trigger>
      {description !== undefined && <Description>{description}</Description>}
      {/* Held to the trigger's width, so each description wraps under its
          name instead of widening the list past the field it opened from. */}
      <Select.Popover className="w-(--trigger-width)">
        <ListBox>
          {principalSources.map((source) => (
            <ListBox.Item
              id={source.value}
              key={source.value}
              textValue={i18n._(source.label)}
            >
              <div className="flex min-w-0 flex-col">
                <Label>{i18n._(source.label)}</Label>
                <Description className="text-pretty">
                  {i18n._(source.description)}
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
