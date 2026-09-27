import { FieldError, Input, Label, TextField } from "@heroui/react";
import { Trans } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import { type ReactNode, useId } from "react";
import { FormMessages } from "@/components/custom/FormMessages";
import { useFieldContext, useFormContext } from "@/forms/context";
import { withoutServerErrors } from "@/forms/server-errors";

/**
 * Which provider claim fills each account field, as a two-column table: the
 * account's field, then the claim name typed beside it.
 *
 * The five rows are fixed — they are the fields an account has — so the table
 * never adds or removes one, and each row is its own form field. It switches
 * on its own width rather than the viewport's, since the card is what has or
 * lacks the room: wide, the field names form one column with the inputs lined
 * up beside them under a column heading; narrow, each name sits over its input
 * and the heading row is dropped, since the stacked name already says which
 * is which. The same shape as the forward-auth header table.
 */
export function ClaimMappingTable({ children }: { children: ReactNode }) {
  const labelId = useId();
  return (
    <div className="@container/claims flex flex-col gap-2">
      <Label id={labelId}>
        <Trans id="admin.federation.claims.mapping">Claim mapping</Trans>
      </Label>
      <div
        aria-hidden="true"
        className="hidden text-xs font-medium text-muted @lg/claims:grid @lg/claims:grid-cols-[10rem_minmax(0,1fr)] @lg/claims:gap-x-6"
      >
        <span>
          <Trans id="admin.federation.claims.column.field">Account field</Trans>
        </span>
        <span>
          <Trans id="admin.federation.claims.column.claim">
            Provider claim
          </Trans>
        </span>
      </div>
      <dl
        aria-labelledby={labelId}
        className="flex flex-col divide-y divide-separator"
      >
        {children}
      </dl>
    </div>
  );
}

/**
 * One account field and the claim it is read from. The claim name is
 * monospace because it is the literal key in the provider's token. The input
 * is named by the account field for assistive technology; on screen the name
 * beside or above it already says so.
 */
export function ClaimMappingRow({
  name,
  hint,
  placeholder,
}: {
  /** The account field, as a plain string so it can also name the input. */
  name: string;
  /** A line under the name, for the row whose value has a consequence. */
  hint?: ReactNode;
  /** The server's default claim for this field. */
  placeholder: string;
}) {
  const field = useFieldContext<string>();
  const form = useFormContext();
  const submitting = useStore(form.store, (state) => state.isSubmitting);
  const errors: unknown[] = field.state.meta.errors.flat(Infinity);
  const invalid = errors.length > 0;

  return (
    <div className="flex flex-col gap-1.5 py-3 first:pt-0 last:pb-0 @lg/claims:grid @lg/claims:grid-cols-[10rem_minmax(0,1fr)] @lg/claims:items-start @lg/claims:gap-x-6">
      {/* A control's height on the wide table, so the name sits on the
          input's centre line rather than at its top edge. */}
      <dt className="flex flex-col justify-center text-sm @lg/claims:min-h-10 md:@lg/claims:min-h-9">
        <span className="font-medium text-foreground">{name}</span>
        {hint !== undefined && (
          <span className="text-xs text-muted">{hint}</span>
        )}
      </dt>
      <dd className="min-w-0">
        <TextField
          className="w-full"
          name={field.name}
          value={field.state.value}
          isDisabled={submitting}
          isInvalid={invalid}
          validationBehavior="aria"
          onBlur={() => {
            if (!form.state.isSubmitting) field.handleBlur();
          }}
          onChange={(value) => {
            if (form.state.isSubmitting) return;
            field.setErrorMap({
              onSubmit: withoutServerErrors(field.state.meta.errorMap.onSubmit),
            });
            field.handleChange(value);
          }}
        >
          <Label className="sr-only">{name}</Label>
          <Input
            className="font-mono"
            autoComplete="off"
            autoCapitalize="off"
            spellCheck={false}
            placeholder={placeholder}
            variant="secondary"
            aria-invalid={invalid || undefined}
          />
          {invalid && (
            <FieldError>
              <FormMessages errors={errors} />
            </FieldError>
          )}
        </TextField>
      </dd>
    </div>
  );
}
