import { Trans, useLingui } from "@lingui/react/macro";
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
import {
  nameIdFormats,
  shortNameIdFormat,
} from "@/pages/admin/saml-applications/saml-projection";

/**
 * The NameID format, as a field of `nameIdFormat`.
 *
 * The four formats the specification names are offered by their last segment,
 * with the whole URN under it, because two versions of the specification share
 * a segment. The format is free text on the wire and the server's own default
 * is configurable, so a record may carry a value the four do not cover —
 * including the empty string a record created against the instance default
 * keeps. The stored value is offered as it is rather than silently rewritten to
 * one of them, and choosing anything else replaces it.
 *
 * `offerDefault` lists the empty string among the choices, for a form that is
 * still choosing and may leave the format to the instance.
 */
export function NameIdFormatField({
  offerDefault = false,
}: {
  offerDefault?: boolean;
}) {
  const { t } = useLingui();
  const { field, errors, invalid, disabled, onChange } =
    useFieldBinding<string>(false);

  const value = field.state.value;
  const listed: readonly string[] = offerDefault
    ? ["", ...nameIdFormats]
    : nameIdFormats;
  const formats = listed.includes(value) ? listed : [...listed, value];
  const defaultLabel = t({
    id: "admin.saml-apps.name-id.default",
    message: "Use the instance default",
  });
  const label = (format: string) =>
    format === "" ? defaultLabel : shortNameIdFormat(format);

  return (
    <Field
      className="max-w-lg"
      disabled={disabled}
      invalid={invalid}
      name={field.name}
    >
      <FieldLabel>
        <Trans id="admin.saml-apps.field.name-id">NameID format</Trans>
      </FieldLabel>
      <Select
        value={value}
        disabled={disabled}
        onValueChange={(next) => {
          if (typeof next === "string") onChange(next);
        }}
      >
        <SelectTrigger aria-invalid={invalid || undefined}>
          <SelectValue>
            {() => (
              <span className={value === "" ? undefined : "font-mono"}>
                {label(value)}
              </span>
            )}
          </SelectValue>
        </SelectTrigger>
        <SelectPopup alignItemWithTrigger={false}>
          {formats.map((format) => (
            <SelectItem key={format} value={format}>
              <div className="flex min-w-0 flex-col">
                <span className={format === "" ? undefined : "font-mono"}>
                  {label(format)}
                </span>
                {format !== "" && (
                  <span className="font-mono text-muted-foreground text-xs">
                    {format}
                  </span>
                )}
              </div>
            </SelectItem>
          ))}
        </SelectPopup>
      </Select>
      <FieldDescription>
        <Trans id="admin.saml-apps.identity.name-id.hint">
          The identifier the assertion's subject carries.
        </Trans>
      </FieldDescription>
      {invalid && (
        <FieldError match>
          <FormMessages errors={errors} />
        </FieldError>
      )}
    </Field>
  );
}
