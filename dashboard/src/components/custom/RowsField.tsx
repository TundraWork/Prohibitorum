import { Description, Label, Tooltip } from "@heroui/react";
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
  /**
   * Why a row cannot be removed once the list is down to `minRows`, shown in
   * a tooltip on the disabled button.
   */
  minRowsReason,
  /**
   * Where the row's remove button sits. A one-line row aligns it to the inputs'
   * own line; a row that draws more than one line of controls passes `top`, so
   * the button reads as removing the whole row rather than the control beside
   * it.
   */
  align = "end",
  /**
   * A column-name row drawn between the field's description and the first row.
   *
   * A list whose rows are a line of unlabelled boxes — the attribute map, the
   * claim aliases — says what each column holds once here rather than on every
   * row. It is the caller's, because only the caller knows the tracks; the list
   * draws it in place and skips it when there is nothing to head.
   */
  header,
  /**
   * Who places the remove button. `beside` draws it after the row, at `align`.
   *
   * `inRow` hands it to `renderRow` as `remove` and draws nothing beside the
   * row, for an editor that lays out its own columns — the attribute map puts it
   * in the last track of its grid on a wide editor and beside a legend on a
   * narrow one. The header is then drawn at the list's full width with no room
   * kept for a button, since the row's own tracks already have a column for it,
   * and `align` is ignored.
   */
  removePlacement = "beside",
  /**
   * For a list whose name is already the heading above it: the label stays for
   * assistive technology and the list's own `aria-labelledby`.
   */
  isLabelHidden = false,
}: {
  label: ReactNode;
  description?: ReactNode;
  renderRow: (
    row: T,
    index: number,
    error: ReactNode,
    problem: RowProblem | undefined,
    /**
     * The row's remove button, bound to it, for the row to place under `inRow`;
     * `null` under `beside`, where the list draws it.
     */
    remove: ReactNode,
  ) => ReactNode;
  /** A fresh row for the add button. */
  emptyRow: () => T;
  addLabel: ReactNode;
  isDisabled?: boolean;
  minRows?: number;
  minRowsReason?: ReactNode;
  align?: "end" | "top";
  header?: ReactNode;
  removePlacement?: "beside" | "inRow";
  isLabelHidden?: boolean;
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

  /** The complaint for `index` as the row's own message, ready to draw. */
  const errorFor = (index: number): ReactNode => {
    const problem = problemFor(index);
    if (problem === undefined) return undefined;
    // The line number is a placeholder in the message, not part of its text.
    return i18n._({
      ...problem.message.reason,
      values: { line: problem.message.line },
    });
  };

  /** The same complaint before it was translated, for the row's own use. */
  const problemFor = (index: number): RowProblem | undefined =>
    rowProblems.find((candidate) => candidate.index === index);

  function changeRows(next: T[]) {
    if (form.state.isSubmitting || isDisabled) return;
    field.setErrorMap({
      onSubmit: withoutServerErrors(field.state.meta.errorMap.onSubmit),
    });
    field.handleChange(next);
  }

  const atMinimum = field.state.value.length <= minRows;

  const removeButton = (index: number) => {
    const button = (
      <Button
        isIconOnly
        size="sm"
        variant="ghost"
        // On a row drawn on one line the button matches the inputs' own
        // height; on a tall row it would otherwise sink to the bottom of
        // the last line, beside whatever control happens to be there.
        className={
          removePlacement === "beside" && align === "top" ? "mt-6" : undefined
        }
        isDisabled={submitting || isDisabled || atMinimum}
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
    );
    if (!atMinimum || minRowsReason === undefined) return button;
    // A disabled button emits no hover or focus for a tooltip to answer, so
    // the tooltip listens on the trigger wrapper instead.
    return (
      <Tooltip delay={0}>
        <Tooltip.Trigger>{button}</Tooltip.Trigger>
        <Tooltip.Content>{minRowsReason}</Tooltip.Content>
      </Tooltip>
    );
  };

  const hasHeader = field.state.value.length > 0 && header !== undefined;

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-col gap-1.5">
        <Label id={id} className={isLabelHidden ? "sr-only" : undefined}>
          {label}
        </Label>
        {description !== undefined && (
          <Description className="text-xs text-muted">
            {description}
          </Description>
        )}
      </div>

      {/* Only over rows: a column name has nothing to name until there is a row
          under it, and an empty list would read as a table with a heading and no
          content rather than as a field waiting for its first value.

          It takes the row's own shape — content, then the width the remove
          button occupies — because the button sits outside the caller's markup:
          a heading laid out across the full width would be a button's width
          wider than every row under it and its columns would point at nothing.
          The width is the icon button's own, so the two agree without either
          having to know the other's size. Under `inRow` the row's tracks hold
          the button, so the heading is the list's own child: a wrapper would
          leave a gap in the column when the heading is hidden. */}
      {hasHeader && removePlacement === "inRow" && header}
      {hasHeader && removePlacement === "beside" && (
        <div
          className={`flex gap-2 ${align === "top" ? "items-start" : "items-end"}`}
        >
          <div className="min-w-0 flex-1">{header}</div>
          <div aria-hidden="true" className="w-6 shrink-0" />
        </div>
      )}

      <ul className="flex flex-col gap-3" aria-labelledby={id}>
        {field.state.value.map((row, index) =>
          // Rows have no stable identity of their own: they are edited in place
          // and reordered only by removal, so the index is the identity.
          removePlacement === "inRow" ? (
            <li key={rowKeys.current[index]}>
              {renderRow(
                row,
                index,
                errorFor(index),
                problemFor(index),
                removeButton(index),
              )}
            </li>
          ) : (
            <li
              key={rowKeys.current[index]}
              className={`flex gap-2 ${align === "top" ? "items-start" : "items-end"}`}
            >
              <div className="min-w-0 flex-1">
                {renderRow(
                  row,
                  index,
                  errorFor(index),
                  problemFor(index),
                  null,
                )}
              </div>
              {removeButton(index)}
            </li>
          ),
        )}
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
  /**
   * Which of the row's inputs the complaint belongs to, when the caller has
   * one that can name it. A row that draws several inputs asks for its own
   * field's problem, so a bad name does not also mark the key beside it.
   */
  field: string;
  message: LineProblem;
}

/** The shape a row-level error takes, for the callers that raise one. */
export function rowProblem(
  index: number,
  message: LineProblem,
  /** The row's input the complaint is about; `"row"` when it is the row itself. */
  field = "row",
): RowProblem {
  return { index, field, message };
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
