import { Input, Label, TextField } from "@heroui/react";
import { msg } from "@lingui/core/macro";
import { useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import type { ReactNode } from "react";
import { FormMessages } from "@/components/custom/FormMessages";
import { RowsField, rowProblem } from "@/components/custom/RowsField";
import { useFieldContext, useFormContext } from "@/forms/context";
import { withoutServerErrors } from "@/forms/server-errors";
import type { ListProblem } from "@/pages/admin/oidc-applications/oidc-validation";

const rowLabelMessage = msg({
  id: "admin.oidc-apps.uri.row",
  message: "{label} {row}",
});

/** The row problem `RowsField` draws, from an address list's own check. */
export function uriRowProblem(problem: ListProblem) {
  return rowProblem(problem.index, {
    line: problem.index + 1,
    reason: problem.message,
  });
}

/**
 * A list of addresses, one input per address, as a field of strings.
 *
 * Redirect and post-logout addresses are copied into a client's configuration
 * one at a time and have to match it character for character, so each is its
 * own input with its own remove button rather than a line in a text box: the
 * row is what the reader checks, and a mistake is marked on the row it is in.
 * The input and its button share one centre line, and a row's message is drawn
 * under the row so it never pushes the button out of line. The value is kept
 * exactly as typed and saved in the order shown.
 */
export function UriListField({
  label,
  description,
  addLabel,
  placeholder,
  minRows = 0,
  minRowsReason,
}: {
  /** The list's name, also used to name each input for assistive technology. */
  label: string;
  description?: ReactNode;
  addLabel: ReactNode;
  placeholder: string;
  /**
   * Rows are never removed below this count. A list that must hold an address
   * passes 1, so its last row stays and the field is never left empty.
   */
  minRows?: number;
  /** Why the last rows cannot be removed, for the disabled remove button. */
  minRowsReason?: ReactNode;
}) {
  return (
    <RowsField<string>
      label={label}
      description={description}
      removePlacement="inRow"
      minRows={minRows}
      minRowsReason={minRowsReason}
      emptyRow={() => ""}
      addLabel={addLabel}
      renderRow={(_row, index, error, problem, remove) => (
        <UriRow
          index={index}
          label={label}
          placeholder={placeholder}
          error={error}
          isInvalid={problem !== undefined}
          remove={remove}
        />
      )}
    />
  );
}

function UriRow({
  index,
  label,
  placeholder,
  error,
  isInvalid,
  remove,
}: {
  index: number;
  label: string;
  placeholder: string;
  /** The row's complaint, already worded, to draw under the row. */
  error: ReactNode;
  isInvalid: boolean;
  /** The row's remove button, bound to it by `RowsField`. */
  remove: ReactNode;
}) {
  const { i18n } = useLingui();
  const field = useFieldContext<string[]>();
  const form = useFormContext();
  const submitting = useStore(form.store, (state) => state.isSubmitting);

  const value = field.state.value[index];
  if (value === undefined) return null;

  const update = (next: string) => {
    if (form.state.isSubmitting) return;
    // Editing a row clears the server's complaint about the whole field, the
    // way a field's own error clears on the first keystroke.
    field.setErrorMap({
      onSubmit: withoutServerErrors(field.state.meta.errorMap.onSubmit),
    });
    field.handleChange(
      field.state.value.map((candidate, at) =>
        at === index ? next : candidate,
      ),
    );
  };

  return (
    <div className="flex flex-col gap-1.5">
      <div className="grid grid-cols-[minmax(0,1fr)_2rem] items-center gap-x-2">
        <TextField
          className="min-w-0"
          value={value}
          isDisabled={submitting}
          isInvalid={isInvalid}
          onChange={update}
        >
          <Label className="sr-only">
            {i18n._({ ...rowLabelMessage, values: { label, row: index + 1 } })}
          </Label>
          {/* Monospace: the address has to match the client's own copy
              exactly, and a stray space or a look-alike letter has to show. */}
          <Input
            className="font-mono"
            inputMode="url"
            autoComplete="off"
            spellCheck={false}
            placeholder={placeholder}
            variant="secondary"
          />
        </TextField>
        <div className="flex justify-end">{remove}</div>
      </div>
      {error !== undefined && error !== null && (
        <span className="text-sm text-danger">
          <FormMessages errors={[error]} />
        </span>
      )}
    </div>
  );
}
