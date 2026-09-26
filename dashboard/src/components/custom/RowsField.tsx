import { Description, Label } from "@heroui/react";
import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import { Plus, Trash2 } from "lucide-react";
import { type ReactNode, useId, useRef } from "react";
import { Button } from "@/components/custom/Button";
import { FormMessages } from "@/components/custom/FormMessages";
import { useFieldContext, useFormContext } from "@/forms/context";
import { type LineProblem, lineProblemMessage } from "@/forms/lines";
import { withoutServerErrors } from "@/forms/server-errors";

const removeRowMessage = msg({
  id: "form.rows.remove",
  message: "Remove row {row}",
});

/**
 * A form field whose value is a list of rows, each row a small group of inputs.
 *
 * The console has four of these — a forward-auth scope vocabulary, an OIDC claim
 * alias table, a SAML attribute map, and a SAML ACS endpoint list — and they all
 * behave the same way: every row can be removed, the list ends with a control
 * that adds one, and an error can belong to a single input of a single row
 * rather than to the field as a whole. ("The third row's address is not a URL"
 * is actionable; "this field is invalid" is not.)
 *
 * A row is passed as a render callback so each call site draws its own inputs,
 * and the error for `row.field` is handed to that callback rather than to the
 * field as a whole. The field's own value is the array of rows; what makes a row
 * valid is the caller's business, and is checked on submit.
 */
export function RowsField<T>({
  label,
  description,
  /**
   * One row's inputs. `error` is the message to draw under `fieldName` in this
   * row, or undefined when that input is fine.
   */
  renderRow,
  emptyRow,
  addLabel,
  isDisabled = false,
  /** Rows are never removed below this count. */
  minRows = 0,
}: {
  label: ReactNode;
  description?: ReactNode;
  renderRow: (row: T, index: number, error: ReactNode) => ReactNode;
  /** A fresh row for the add button. */
  emptyRow: () => T;
  addLabel: ReactNode;
  isDisabled?: boolean;
  minRows?: number;
}) {
  const field = useFieldContext<T[]>();
  const form = useFormContext();
  const { i18n } = useLingui();
  const submitting = useStore(form.store, (state) => state.isSubmitting);
  const id = useId();

  // One stable key per row. The form owns the row values; these are ours. A key
  // derived from the row's own text would change on every keystroke and remount
  // the input the reader is typing in, and the array index is not stable across
  // a removal. Reusing the key at a position after a removal is deliberate: the
  // row that moved up is the row that moved up.
  const rowKeys = useRef<string[]>([]);
  rowKeys.current = field.state.value.map(
    (_, index) => rowKeys.current[index] ?? crypto.randomUUID(),
  );

  const errors: unknown[] = field.state.meta.errors.flat(Infinity);
  // A row-level error arrives as `{ index, message }`; the rest are field-level
  // and drawn under the list.
  const rowProblems = errors.filter(isRowProblem);
  const fieldErrors = errors.filter((error) => !isRowProblem(error));

  const errorFor = (index: number): ReactNode => {
    const problem = rowProblems.find((candidate) => candidate.index === index);
    if (problem === undefined) return undefined;
    // The line number is a placeholder in the message, not part of its text.
    return i18n._({
      ...problem.message.reason,
      values: { line: problem.message.line },
    });
  };

  function changeRows(next: T[]) {
    if (form.state.isSubmitting || isDisabled) return;
    field.setErrorMap({
      onSubmit: withoutServerErrors(field.state.meta.errorMap.onSubmit),
    });
    field.handleChange(next);
  }

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-col gap-1.5">
        <Label id={id}>{label}</Label>
        {description !== undefined && (
          <Description className="text-xs text-muted">
            {description}
          </Description>
        )}
      </div>

      <ul className="flex flex-col gap-3" aria-labelledby={id}>
        {field.state.value.map((row, index) => (
          // Rows have no stable identity of their own: they are edited in place
          // and reordered only by removal, so the index is the identity.
          <li key={rowKeys.current[index]} className="flex items-end gap-2">
            <div className="min-w-0 flex-1">
              {renderRow(row, index, errorFor(index))}
            </div>
            <Button
              isIconOnly
              size="sm"
              variant="ghost"
              isDisabled={
                submitting || isDisabled || field.state.value.length <= minRows
              }
              aria-label={i18n._({
                ...removeRowMessage,
                values: { row: index + 1 },
              })}
              onPress={() => {
                changeRows(field.state.value.filter((_, at) => at !== index));
              }}
            >
              <Trash2 size={16} aria-hidden="true" />
            </Button>
          </li>
        ))}
      </ul>

      <div>
        <Button
          variant="outline"
          size="sm"
          isDisabled={submitting || isDisabled}
          onPress={() => {
            changeRows([...field.state.value, emptyRow()]);
          }}
        >
          <Plus size={16} aria-hidden="true" />
          {addLabel}
        </Button>
      </div>

      {fieldErrors.length > 0 && (
        <span className="text-sm text-danger">
          <FormMessages errors={fieldErrors} />
        </span>
      )}
    </div>
  );
}

/** A one-per-line box and this list are two views of the same kind of value. */
export function RowsFieldHint() {
  return (
    <Trans id="form.rows.hint">
      One value per line. Blank lines are ignored; a line with spaces at either
      end is refused rather than trimmed.
    </Trans>
  );
}

interface RowProblem {
  index: number;
  message: LineProblem;
}

/** The shape a row-level error takes, for the callers that raise one. */
export function rowProblem(index: number, message: LineProblem): RowProblem {
  return { index, message };
}

function isRowProblem(value: unknown): value is RowProblem {
  return (
    typeof value === "object" &&
    value !== null &&
    "index" in value &&
    "message" in value &&
    typeof (value as RowProblem).index === "number"
  );
}

export type { RowProblem };
export { lineProblemMessage };
