import { msg } from "@lingui/core/macro";
import { useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import { type ReactNode, useId } from "react";
import { FormMessages } from "@/components/custom/FormMessages";
import { RowsField, rowProblem } from "@/components/custom/RowsField";
import { Textarea } from "@/components/ui/textarea";
import { useFieldContext, useFormContext } from "@/forms/context";
import type { ListProblem } from "@/forms/lines";
import { withoutServerErrors } from "@/forms/server-errors";

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
export function UriList({
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
  const errorId = useId();

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

  const hasError = error !== undefined && error !== null;

  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex items-start gap-2">
        {/* Monospace: the address has to match the client's own copy
            exactly, and a stray space or a look-alike letter has to show.
            It is a one-line box that grows when the address is longer than
            the box, so on a narrow screen the end of the address (the part
            that tells two apart) is never hidden. Enter submits the form as
            it does in a single-line input, and a line break cannot be typed
            or pasted into the value. */}
        <Textarea
          className="**:data-[slot=textarea]:min-h-0! **:data-[slot=textarea]:resize-none **:data-[slot=textarea]:font-mono **:data-[slot=textarea]:text-[13px] **:data-[slot=textarea]:leading-5 **:data-[slot=textarea]:[overflow-wrap:anywhere] max-sm:**:data-[slot=textarea]:py-[7px]"
          rows={1}
          value={value}
          disabled={submitting}
          aria-label={i18n._({
            ...rowLabelMessage,
            values: { label, row: index + 1 },
          })}
          aria-invalid={isInvalid || undefined}
          aria-describedby={hasError ? errorId : undefined}
          inputMode="url"
          autoComplete="off"
          spellCheck={false}
          placeholder={placeholder}
          onKeyDown={(event) => {
            // Enter confirms an IME candidate while composing; it is not a submit.
            if (event.key !== "Enter" || event.nativeEvent.isComposing) return;
            event.preventDefault();
            event.currentTarget.form?.requestSubmit();
          }}
          onChange={(event) =>
            update(event.target.value.replace(/[\r\n]/g, ""))
          }
        />
        {remove}
      </div>
      {hasError && (
        <p id={errorId} className="text-destructive-foreground text-xs">
          <FormMessages errors={[error]} />
        </p>
      )}
    </div>
  );
}
