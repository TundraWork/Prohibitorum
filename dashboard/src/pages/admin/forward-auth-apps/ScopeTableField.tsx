import { Fieldset, Input, Label, Separator, TextField } from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import type { ReactNode } from "react";
import { FormMessages } from "@/components/custom/FormMessages";
import {
  type RowProblem,
  RowsField,
  rowProblem,
} from "@/components/custom/RowsField";
import { useFieldContext, useFormContext } from "@/forms/context";
import { withoutServerErrors } from "@/forms/server-errors";
import type { ScopeListProblem } from "@/pages/admin/forward-auth-apps/forward-auth-validation";

/** One entry of a forward-auth application's token scopes. */
export interface ScopeRow {
  name: string;
  description: string;
}

/**
 * The wide table's tracks: the name, the description it explains at twice the
 * room, and the remove button. The heading and every row share them, so the
 * columns line up.
 */
const scopeTracks = "grid-cols-[minmax(0,1fr)_minmax(0,2fr)_2rem]";

/** The row problem `RowsField` draws, from the vocabulary's own check. */
export function scopeRowProblem(problem: ScopeListProblem) {
  return rowProblem(
    problem.index,
    { line: problem.index + 1, reason: problem.message },
    problem.field,
  );
}

/**
 * The scopes a personal access token may be granted for this application, as
 * a field of `ScopeRow`s.
 *
 * A scope is a name and a line saying what it allows, and the reader compares
 * them down the list, so where the editor has the room it is a two-column table
 * under one set of headings. Below that, each scope is a fieldset with its own
 * labels, because two unlabelled boxes say nothing about which is which. The
 * switch is on the editor's own width rather than the viewport's — the create
 * page holds it in the reading measure, the detail page in a wide card — and
 * the layout not in use is `display: none`, so it is out of the tab order and
 * the accessibility tree. Both read and write the same row, so resizing keeps
 * what was typed. The SAML attribute map is drawn the same way.
 */
export function ScopeTableField({
  isLabelHidden = false,
}: {
  /**
   * Under a `Section` of the same name, the field's label would repeat the
   * heading above it; it stays for assistive technology.
   */
  isLabelHidden?: boolean;
}) {
  return (
    <div className="@container/scopes">
      <RowsField<ScopeRow>
        label={
          <Trans id="admin.forward-auth-apps.scopes.label">Token scopes</Trans>
        }
        isLabelHidden={isLabelHidden}
        description={
          <Trans id="admin.forward-auth-apps.scopes.hint">
            Personal access tokens choose from these, and the service receives
            the granted ones in Remote-Scopes.
          </Trans>
        }
        removePlacement="inRow"
        header={<ScopeHeader />}
        emptyRow={() => ({ name: "", description: "" })}
        addLabel={
          <Trans id="admin.forward-auth-apps.scopes.add">Add a scope</Trans>
        }
        renderRow={(_row, index, error, problem, remove) => (
          <ScopeRowFields
            index={index}
            error={error}
            problem={problem}
            remove={remove}
          />
        )}
      />
    </div>
  );
}

/**
 * The wide layout's column names. `aria-hidden`: every input carries its own
 * name, so a reader hearing the row does not also need the heading.
 */
function ScopeHeader() {
  return (
    <div
      aria-hidden="true"
      className={`hidden gap-x-2 text-xs font-medium text-muted @md/scopes:grid ${scopeTracks}`}
    >
      <span className="truncate">
        <Trans id="admin.forward-auth-apps.scopes.name">Name</Trans>
      </span>
      <span className="truncate">
        <Trans id="admin.forward-auth-apps.scopes.description">
          Description
        </Trans>
      </span>
    </div>
  );
}

/**
 * One scope, in both layouts.
 *
 * `RowsField` owns the array and hands each row to this component, so the row
 * addresses the field by position, writes back a copy with its own keys
 * replaced, and marks only the input `problem` names.
 */
function ScopeRowFields({
  index,
  error,
  problem,
  remove,
}: {
  index: number;
  /** The row's complaint, already worded, to draw under the row. */
  error: ReactNode;
  /** The same complaint before it was worded, naming the input at fault. */
  problem: RowProblem | undefined;
  /** The row's remove button, bound to it by `RowsField`. */
  remove: ReactNode;
}) {
  const { t } = useLingui();
  const field = useFieldContext<ScopeRow[]>();
  const form = useFormContext();
  const submitting = useStore(form.store, (state) => state.isSubmitting);

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

  const controls = (labelled: boolean) => (
    <>
      <ScopeInput
        labelled={labelled}
        label={<Trans id="admin.forward-auth-apps.scopes.name">Name</Trans>}
        placeholder={t({
          id: "admin.forward-auth-apps.scope.name.placeholder",
          message: "read",
        })}
        value={row.name}
        isMono
        isDisabled={submitting}
        isInvalid={problem?.field === "name"}
        onChange={(name) => update({ name })}
      />
      <ScopeInput
        labelled={labelled}
        label={
          <Trans id="admin.forward-auth-apps.scopes.description">
            Description
          </Trans>
        }
        placeholder={t({
          id: "admin.forward-auth-apps.scopes.description.placeholder",
          message: "Read the application's data",
        })}
        value={row.description}
        isDisabled={submitting}
        isInvalid={problem?.field === "description"}
        onChange={(description) => update({ description })}
      />
    </>
  );

  const message = error !== undefined && error !== null && (
    <span className="text-sm text-danger">
      <FormMessages errors={[error]} />
    </span>
  );

  return (
    <>
      <div className="hidden flex-col gap-1.5 @md/scopes:flex">
        <div className={`grid items-center gap-x-2 ${scopeTracks}`}>
          {controls(false)}
          <div className="flex justify-end">{remove}</div>
        </div>
        {message}
      </div>

      <div className="flex flex-col gap-3 @md/scopes:hidden">
        {index > 0 && <Separator />}
        {/* The remove button sits on the legend's line, placed over it from a
            wrapper: the legend has to be the fieldset's first child and is
            drawn in its border, so the two cannot share a flex line. */}
        <div className="relative">
          <Fieldset className="gap-3">
            <Fieldset.Legend className="mb-3 pe-10">
              <ScopeLegend row={index + 1} />
            </Fieldset.Legend>
            <Fieldset.Group>{controls(true)}</Fieldset.Group>
            {message}
          </Fieldset>
          <div className="absolute end-0 top-0 flex h-6 items-center">
            {remove}
          </div>
        </div>
      </div>
    </>
  );
}

/** The narrow layout's name for a scope, numbered as its remove button is. */
function ScopeLegend({ row }: { row: number }) {
  return <Trans id="admin.forward-auth-apps.scopes.row">Scope {row}</Trans>;
}

/**
 * A text input of the row. On the wide layout the column heading names it and
 * the label is for assistive technology only; on the narrow one it is shown.
 * The name is monospace because it is an identifier the service matches
 * exactly, and a stray space or a look-alike letter has to be visible.
 */
function ScopeInput({
  labelled,
  label,
  placeholder,
  value,
  isMono = false,
  isDisabled,
  isInvalid,
  onChange,
}: {
  labelled: boolean;
  label: ReactNode;
  placeholder: string;
  value: string;
  isMono?: boolean;
  isDisabled: boolean;
  isInvalid: boolean;
  onChange: (value: string) => void;
}) {
  return (
    <TextField
      className="min-w-0"
      value={value}
      isDisabled={isDisabled}
      isInvalid={isInvalid}
      onChange={onChange}
    >
      <Label className={labelled ? undefined : "sr-only"}>{label}</Label>
      <Input
        className={isMono ? "font-mono" : undefined}
        autoComplete="off"
        spellCheck={false}
        placeholder={placeholder}
        variant="secondary"
      />
    </TextField>
  );
}
