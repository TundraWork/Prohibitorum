import { Input, Label, TextArea } from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import { type ReactNode, useId } from "react";
import { FormMessages } from "@/components/custom/FormMessages";
import { useFieldContext, useFormContext } from "@/forms/context";
import { withoutServerErrors } from "@/forms/server-errors";

/** One entry of a forward-auth application's scope vocabulary. */
export interface ScopeRow {
  name: string;
  description: string;
}

/**
 * The two inputs of one scope row, drawn inside `RowsField`.
 *
 * `RowsField` owns the array and hands each row to a render callback, so the
 * row's inputs address the field by position: this component reads the field
 * from context, writes back a copy with one row's keys replaced, and draws the
 * message the callback was given for that row's name. The row is the unit of
 * edit and the unit of error, which is why it is not two form fields of its
 * own — a scope has no identity until it is saved, so there is nothing to name
 * either one after.
 *
 * The description is a `TextArea` rather than a one-line box because the server
 * allows 256 characters and a description that reads well at that length does
 * not fit on one line.
 */
export function ScopeRows({
  index,
  error,
}: {
  index: number;
  /** The name's complaint for this row, if the submit found one. */
  error: ReactNode;
}) {
  const { t } = useLingui();
  const field = useFieldContext<ScopeRow[]>();
  const form = useFormContext();
  const submitting = useStore(form.store, (state) => state.isSubmitting);
  const nameId = useId();
  const descriptionId = useId();

  const row = field.state.value[index];
  if (row === undefined) return null;

  const update = (patch: Partial<ScopeRow>) => {
    if (form.state.isSubmitting) return;
    // Editing a row clears the server's complaint about the whole field, the
    // way a field's own error clears on the first keystroke.
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
    <div className="flex flex-col gap-1.5">
      <Label className="sr-only" htmlFor={nameId}>
        <Trans id="admin.forward-auth-apps.scope.name">Scope name</Trans>
      </Label>
      <Input
        id={nameId}
        value={row.name}
        disabled={submitting}
        aria-invalid={error !== undefined && error !== null}
        autoComplete="off"
        spellCheck={false}
        placeholder={t({
          id: "admin.forward-auth-apps.scope.name.placeholder",
          message: "read",
        })}
        variant="secondary"
        onChange={(event) => update({ name: event.target.value })}
      />
      <Label className="sr-only" htmlFor={descriptionId}>
        <Trans id="admin.forward-auth-apps.scope.description">
          Description
        </Trans>
      </Label>
      <TextArea
        id={descriptionId}
        value={row.description}
        disabled={submitting}
        rows={2}
        spellCheck={false}
        variant="secondary"
        onChange={(event) => update({ description: event.target.value })}
      />
      {error !== undefined && error !== null && (
        <span className="text-sm text-danger">
          <FormMessages errors={[error]} />
        </span>
      )}
    </div>
  );
}
