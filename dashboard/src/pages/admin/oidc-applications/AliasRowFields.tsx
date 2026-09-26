import { Input, Label, ListBox, Select } from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import { type ReactNode, useId } from "react";
import { aliasSourceClaims } from "@/api/federation";
import { FormMessages } from "@/components/custom/FormMessages";
import { useFieldContext, useFormContext } from "@/forms/context";
import { withoutServerErrors } from "@/forms/server-errors";

/** One claim the application publishes under a second name. */
export interface AliasRow {
  name: string;
  source: string;
}

/**
 * The two inputs of one claim-alias row, drawn inside `RowsField`.
 *
 * `RowsField` owns the array and hands each row to a render callback, so the
 * inputs address the field by position: this component reads the field from
 * context, writes back a copy with one row changed, and draws the message the
 * callback was given for that row's output name. The row is the unit of edit and
 * the unit of error, the same as a scope row — an alias has no identity until it
 * is saved, so there is nothing to name either input after.
 *
 * The source list is the server's own: an alias can only read a claim the
 * instance actually resolves, and offering a name it would never produce would
 * be an alias that silently publishes nothing.
 */
export function AliasRowFields({
  index,
  error,
}: {
  index: number;
  /** The output name's complaint for this row, if the submit found one. */
  error: ReactNode;
}) {
  const { t } = useLingui();
  const field = useFieldContext<AliasRow[]>();
  const form = useFormContext();
  const submitting = useStore(form.store, (state) => state.isSubmitting);
  const nameId = useId();

  const row = field.state.value[index];
  if (row === undefined) return null;

  const update = (patch: Partial<AliasRow>) => {
    if (submitting) return;
    // Editing a row clears the complaint about the whole field, the way a
    // field's own error clears on the first keystroke.
    field.setErrorMap({
      onSubmit: withoutServerErrors(field.state.meta.errorMap.onSubmit),
    });
    field.handleChange(
      field.state.value.map((candidate, at) =>
        at === index ? { ...candidate, ...patch } : candidate,
      ),
    );
  };

  return (
    <div className="flex flex-col gap-1.5 sm:flex-row sm:items-start sm:gap-2">
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <Label className="sr-only" htmlFor={nameId}>
          <Trans id="admin.oidc-apps.alias.name">Claim name</Trans>
        </Label>
        <Input
          id={nameId}
          value={row.name}
          disabled={submitting}
          aria-invalid={error !== undefined && error !== null}
          autoComplete="off"
          spellCheck={false}
          placeholder={t({
            id: "admin.oidc-apps.alias.name.placeholder",
            message: "team",
          })}
          variant="secondary"
          onChange={(event) => update({ name: event.target.value })}
        />
      </div>

      {/* No label of its own: the row is two halves of one statement, and a
          second visible label would read as a second field rather than as the
          value the name is written from. */}
      <Select
        className="sm:w-56"
        variant="secondary"
        isDisabled={submitting}
        value={row.source}
        aria-label={t({
          id: "admin.oidc-apps.alias.source",
          message: "Source claim",
        })}
        onChange={(key) => {
          if (typeof key === "string") update({ source: key });
        }}
      >
        <Select.Trigger>
          <Select.Value />
          <Select.Indicator />
        </Select.Trigger>
        <Select.Popover>
          <ListBox>
            {aliasSourceClaims.map((claim) => (
              <ListBox.Item key={claim} id={claim} textValue={claim}>
                <span className="font-mono text-sm">{claim}</span>
                <ListBox.ItemIndicator />
              </ListBox.Item>
            ))}
          </ListBox>
        </Select.Popover>
      </Select>

      {error !== undefined && error !== null && (
        <span className="text-sm text-danger">
          <FormMessages errors={[error]} />
        </span>
      )}
    </div>
  );
}
