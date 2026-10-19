import { Trans, useLingui } from "@lingui/react/macro";
import { FormMessages } from "@/components/custom/FormMessages";
import { Field, FieldError, FieldLabel } from "@/components/ui/field";
import {
  NumberField,
  NumberFieldDecrement,
  NumberFieldGroup,
  NumberFieldIncrement,
  NumberFieldInput,
} from "@/components/ui/number-field";
import {
  maxIntervalSeconds,
  minIntervalSeconds,
} from "@/pages/admin/settings/sign-in-page/sign-in-page-form";

/**
 * The carousel's interval in seconds. A cleared field is handed over as `NaN`,
 * which the form refuses with the range message; `errors` are the form
 * field's.
 */
export function IntervalField({
  value,
  errors,
  disabled,
  onChange,
}: {
  value: number;
  errors: readonly unknown[];
  disabled: boolean;
  onChange: (seconds: number) => void;
}) {
  const { t, i18n } = useLingui();
  const zh = i18n.locale === "zh";
  return (
    <Field disabled={disabled} invalid={errors.length > 0} className="max-w-56">
      <FieldLabel>
        <Trans id="settings.sign-in.interval">Change every</Trans>
      </FieldLabel>
      <NumberField
        value={Number.isNaN(value) ? null : value}
        min={minIntervalSeconds}
        max={maxIntervalSeconds}
        step={5}
        disabled={disabled}
        locale={zh ? "zh-CN" : "en"}
        // Intl's long Chinese unit is "秒钟"; the short one reads "10秒".
        format={{
          style: "unit",
          unit: "second",
          unitDisplay: zh ? "short" : "long",
        }}
        onValueChange={(next) => onChange(next ?? Number.NaN)}
      >
        <NumberFieldGroup>
          <NumberFieldDecrement
            aria-label={t({ id: "number-field.decrease", message: "Decrease" })}
          />
          <NumberFieldInput aria-invalid={errors.length > 0 || undefined} />
          <NumberFieldIncrement
            aria-label={t({ id: "number-field.increase", message: "Increase" })}
          />
        </NumberFieldGroup>
      </NumberField>
      {errors.length > 0 && (
        <FieldError match>
          <FormMessages errors={errors} />
        </FieldError>
      )}
    </Field>
  );
}
