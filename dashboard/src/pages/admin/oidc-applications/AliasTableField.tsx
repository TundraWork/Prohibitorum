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
import { Field, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectItem,
  SelectPopup,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
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
const aliasTracks = "@md/aliases:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_2rem]";

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
 * headings; below 28rem of its own width each alias stacks its two inputs, each
 * with its label, because two unlabelled boxes say nothing about which is
 * which. It is one set of controls reshaped by container width, so there is a
 * single input of each kind per row for a reader and a test to find.
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
            Publishes a granted claim under a second name, for clients that
            expect a claim name the specification does not define.
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
      className={`hidden gap-x-2 text-muted-foreground text-xs font-medium @md/aliases:grid ${aliasTracks}`}
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
 * One alias.
 *
 * `RowsField` owns the array and hands each row to this component, so the row
 * addresses the field by position, writes back a copy with its own keys
 * replaced, and marks only the input `problem` names. On the narrow layout the
 * remove button sits beside the name and the source takes the line under it.
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
  const { t } = useLingui();
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

  const hasError = error !== undefined && error !== null;

  return (
    <div
      className={`grid grid-cols-[minmax(0,1fr)_auto] items-end gap-x-2 gap-y-3 ${aliasTracks}`}
    >
      <Field
        className="col-start-1 row-start-1 min-w-0"
        disabled={submitting}
        invalid={problem?.field === "name"}
      >
        <FieldLabel className="@md/aliases:sr-only">
          <NameLabel />
        </FieldLabel>
        {/* Monospace, as the source beside it is: both are claim names the
            client matches exactly. */}
        <Input
          className="font-mono"
          value={row.name}
          aria-invalid={problem?.field === "name" || undefined}
          autoComplete="off"
          spellCheck={false}
          placeholder={t({
            id: "admin.oidc-apps.alias.name.placeholder",
            message: "team",
          })}
          onChange={(event) => update({ name: event.target.value })}
        />
      </Field>
      <Field
        className="col-span-2 row-start-2 min-w-0 @md/aliases:col-span-1 @md/aliases:col-start-2 @md/aliases:row-start-1"
        disabled={submitting}
      >
        <FieldLabel className="@md/aliases:sr-only">
          <SourceLabel />
        </FieldLabel>
        <Select
          value={row.source}
          onValueChange={(source) => {
            if (typeof source === "string") update({ source });
          }}
        >
          <SelectTrigger>
            <SelectValue>
              {() => <span className="font-mono">{row.source}</span>}
            </SelectValue>
          </SelectTrigger>
          <SelectPopup alignItemWithTrigger={false}>
            {aliasSourceClaims.map((claim) => (
              <SelectItem key={claim} value={claim}>
                <span className="font-mono">{claim}</span>
              </SelectItem>
            ))}
          </SelectPopup>
        </Select>
      </Field>
      <div className="col-start-2 row-start-1 flex h-9 items-center justify-end @md/aliases:col-start-3">
        {remove}
      </div>
      {hasError && (
        <p className="col-span-full text-destructive-foreground text-sm">
          <FormMessages errors={[error]} />
        </p>
      )}
    </div>
  );
}
