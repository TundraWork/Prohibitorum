import {
  Fieldset,
  Input,
  Label,
  ListBox,
  Select,
  Separator,
  TextField,
} from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import type { ReactNode } from "react";
import { aliasSourceClaims } from "@/api/federation";
import { FormMessages } from "@/components/custom/FormMessages";
import {
  type RowProblem,
  RowsField,
  rowProblem,
} from "@/components/custom/RowsField";
import { useFieldContext, useFormContext } from "@/forms/context";
import { withoutServerErrors } from "@/forms/server-errors";
import type {
  AliasListProblem,
  AliasRow,
} from "@/pages/admin/oidc-applications/oidc-validation";

/**
 * The wide table's tracks: the name, the claim it is copied from, and the
 * remove button. The two halves of an alias are the same kind of value — a
 * claim name — so they take equal room. The heading and every row share the
 * tracks, so the columns line up.
 */
const aliasTracks = "grid-cols-[minmax(0,1fr)_minmax(0,1fr)_2rem]";

/** The row problem `RowsField` draws, from the alias table's own check. */
export function aliasRowProblem(problem: AliasListProblem) {
  return rowProblem(
    problem.index,
    { line: problem.index + 1, reason: problem.message },
    problem.field,
  );
}

/**
 * The application's claim aliases, as a field of `AliasRow`s: each publishes a
 * granted claim under a second name, for a client that expects a claim the
 * specification does not define.
 *
 * Laid out the way the forward-auth token scopes and the SAML attribute map
 * are. Where the editor has the room it is a two-column table under one set of
 * headings; below 28rem of its own width each alias is a fieldset with its own
 * labels, because two unlabelled boxes say nothing about which is which. The
 * layout not in use is `display: none`, so it is out of the tab order and the
 * accessibility tree, and both read and write the same row.
 *
 * The source list is the server's own: an alias can only read a claim the
 * instance actually resolves, and offering one it would never produce would be
 * an alias that silently publishes nothing.
 */
export function AliasTableField() {
  return (
    <div className="@container/aliases">
      <RowsField<AliasRow>
        label={<Trans id="admin.oidc-apps.aliases">Claim aliases</Trans>}
        description={
          <Trans id="admin.oidc-apps.aliases.hint">
            Publish a granted claim under a second name, for a client that
            expects one the specification does not define.
          </Trans>
        }
        removePlacement="inRow"
        header={<AliasHeader />}
        emptyRow={() => ({ name: "", source: "name" })}
        addLabel={<Trans id="admin.oidc-apps.aliases.add">Add alias</Trans>}
        renderRow={(_row, index, error, problem, remove) => (
          <AliasRowFields
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
 * The wide layout's column names. `aria-hidden`: every control carries its own
 * name, so a reader hearing the row does not also need the heading.
 */
function AliasHeader() {
  return (
    <div
      aria-hidden="true"
      className={`hidden gap-x-2 text-xs font-medium text-muted @md/aliases:grid ${aliasTracks}`}
    >
      <span className="truncate">
        <NameLabel />
      </span>
      <span className="truncate">
        <SourceLabel />
      </span>
    </div>
  );
}

function NameLabel() {
  return <Trans id="admin.oidc-apps.alias.name">Claim name</Trans>;
}

function SourceLabel() {
  return <Trans id="admin.oidc-apps.alias.source">Source claim</Trans>;
}

/**
 * One alias, in both layouts.
 *
 * `RowsField` owns the array and hands each row to this component, so the row
 * addresses the field by position, writes back a copy with its own keys
 * replaced, and marks only the input `problem` names.
 */
function AliasRowFields({
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
  const field = useFieldContext<AliasRow[]>();
  const form = useFormContext();
  const submitting = useStore(form.store, (state) => state.isSubmitting);

  const row = field.state.value[index];
  if (row === undefined) return null;

  const update = (patch: Partial<AliasRow>) => {
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
      <AliasNameInput
        labelled={labelled}
        value={row.name}
        isDisabled={submitting}
        isInvalid={problem?.field === "name"}
        onChange={(name) => update({ name })}
      />
      <AliasSourceSelect
        labelled={labelled}
        value={row.source}
        isDisabled={submitting}
        onChange={(source) => update({ source })}
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
      <div className="hidden flex-col gap-1.5 @md/aliases:flex">
        <div className={`grid items-center gap-x-2 ${aliasTracks}`}>
          {controls(false)}
          <div className="flex justify-end">{remove}</div>
        </div>
        {message}
      </div>

      <div className="flex flex-col gap-3 @md/aliases:hidden">
        {index > 0 && <Separator />}
        {/* The remove button sits on the legend's line, placed over it from a
            wrapper: the legend has to be the fieldset's first child and is
            drawn in its border, so the two cannot share a flex line. */}
        <div className="relative">
          <Fieldset className="gap-3">
            <Fieldset.Legend className="mb-3 pe-10">
              <AliasLegend row={index + 1} />
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

/** The narrow layout's name for an alias, numbered as its remove button is. */
function AliasLegend({ row }: { row: number }) {
  return <Trans id="admin.oidc-apps.aliases.row">Alias {row}</Trans>;
}

/**
 * The name the client will read. On the wide layout the column heading names
 * it and the label is for assistive technology only; on the narrow one it is
 * shown. Monospace, as the source beside it is: both are claim names the
 * client matches exactly.
 */
function AliasNameInput({
  labelled,
  value,
  isDisabled,
  isInvalid,
  onChange,
}: {
  labelled: boolean;
  value: string;
  isDisabled: boolean;
  isInvalid: boolean;
  onChange: (value: string) => void;
}) {
  const { t } = useLingui();
  return (
    <TextField
      className="min-w-0"
      value={value}
      isDisabled={isDisabled}
      isInvalid={isInvalid}
      onChange={onChange}
    >
      <Label className={labelled ? undefined : "sr-only"}>
        <NameLabel />
      </Label>
      <Input
        className="font-mono"
        autoComplete="off"
        spellCheck={false}
        placeholder={t({
          id: "admin.oidc-apps.alias.name.placeholder",
          message: "team",
        })}
        variant="secondary"
      />
    </TextField>
  );
}

/**
 * The claim the alias copies.
 *
 * The trigger shows the claim name as text alone. Its default would copy the
 * whole selected option in, hidden check mark included, which made the trigger
 * a pixel taller than the input beside it; drawing only the name, in the
 * value's own line box, keeps both at the library's own control height.
 */
function AliasSourceSelect({
  labelled,
  value,
  isDisabled,
  onChange,
}: {
  labelled: boolean;
  value: string;
  isDisabled: boolean;
  onChange: (value: string) => void;
}) {
  return (
    <Select
      className="min-w-0"
      variant="secondary"
      isDisabled={isDisabled}
      value={value}
      onChange={(key) => {
        if (typeof key === "string") onChange(key);
      }}
    >
      <Label className={labelled ? undefined : "sr-only"}>
        <SourceLabel />
      </Label>
      <Select.Trigger>
        {/* The face is set on the value itself: a monospace span inside it
            would sit on a second baseline and make the line a pixel taller. */}
        <Select.Value className="font-mono">
          {({ isPlaceholder }) => (isPlaceholder ? null : value)}
        </Select.Value>
        <Select.Indicator />
      </Select.Trigger>
      <Select.Popover>
        <ListBox>
          {aliasSourceClaims.map((claim) => (
            <ListBox.Item key={claim} id={claim} textValue={claim}>
              <span className="font-mono">{claim}</span>
              <ListBox.ItemIndicator />
            </ListBox.Item>
          ))}
        </ListBox>
      </Select.Popover>
    </Select>
  );
}
