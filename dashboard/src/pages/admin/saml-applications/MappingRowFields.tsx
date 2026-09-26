import { Input, Label, ListBox, Select, Switch } from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import { type ReactNode, useId } from "react";
import { FormMessages } from "@/components/custom/FormMessages";
import type { RowProblem } from "@/components/custom/RowsField";
import { useFieldContext, useFormContext } from "@/forms/context";
import { withoutServerErrors } from "@/forms/server-errors";
import {
  accountAttributeLabel,
  attributeKeyOf,
  attributeSourceLabel,
  attributeSourceOf,
  attributeSources,
} from "@/pages/admin/saml-applications/saml-projection";

/**
 * One row of the attribute map while it is being edited.
 *
 * The wire shape is a mapping — name, format, friendly name, source, multi —
 * but the source is not one choice: it is either one of the named account facts
 * or a key into the account's attribute bag, and the second needs a second input
 * beside it. The row therefore holds the source as the console presents it and
 * composes the wire value on submit, so "which of the two is this row reading"
 * stays out of the stored value.
 */
export interface MappingRow {
  name: string;
  nameFormat: string;
  friendlyName: string;
  /** `attributes` when the row reads an account attribute, else the fact. */
  sourceKind: AttributeSourceKind;
  /** The key, when `sourceKind` is `attributes`; ignored otherwise. */
  sourceKey: string;
  multi: boolean;
}

export type AttributeSourceKind =
  | (typeof attributeSources)[number]
  | "attributes";

/** The source a row composes to: `attributes.<key>` or the named fact. */
export function mappingSource(row: MappingRow): string {
  return row.sourceKind === "attributes"
    ? attributeSourceOf(row.sourceKey)
    : row.sourceKind;
}

/** A stored mapping's source, split back into the row's two parts. */
export function readSourceKind(source: string): {
  sourceKind: AttributeSourceKind;
  sourceKey: string;
} {
  for (const fact of attributeSources) {
    if (source === fact) return { sourceKind: fact, sourceKey: "" };
  }
  return { sourceKind: "attributes", sourceKey: attributeKeyOf(source) };
}

/**
 * The row's inputs, drawn inside `RowsField`.
 *
 * `RowsField` owns the array and hands each row to a render callback, so the
 * row's inputs address the field by position: this component reads the field
 * from context, writes back a copy with one row replaced, and draws the message
 * the callback was given for that row's input. The row is the unit of edit and
 * the unit of error, which is why it is not five `AppField`s — a mapping has no
 * identity until it is saved, and "the second row's name is empty" is something
 * the reader can act on where "this field is invalid" is not.
 *
 * The row also takes that complaint untranslated. It draws several inputs, and
 * the submit knows which of them it was about, so the mark goes on the input at
 * fault rather than on all of them: a name that is empty does not also make the
 * attribute key beside it look wrong.
 */
export function MappingRowFields({
  index,
  error,
  problem,
}: {
  index: number;
  /** The row's complaint, already worded, to draw under the row. */
  error: ReactNode;
  /** The same complaint before it was worded, naming the input at fault. */
  problem: RowProblem | undefined;
}) {
  const { i18n } = useLingui();
  const field = useFieldContext<MappingRow[]>();
  const form = useFormContext();
  const submitting = useStore(form.store, (state) => state.isSubmitting);
  const nameId = useId();
  const formatId = useId();
  const friendlyId = useId();
  const keyId = useId();

  const row = field.state.value[index];
  if (row === undefined) return null;

  const isInvalid = (input: string) => problem?.field === input;

  const update = (patch: Partial<MappingRow>) => {
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

  // Two lines of three columns rather than one line of five inputs and a
  // switch: the row is read as an output side (what the provider sees) over a
  // source side (where the value comes from), and at the console's measure a
  // single line does not fit — the five controls fell to whatever widths the
  // flex row had left, so no two rows lined up and the fourth control wrapped
  // unpredictably. The tracks are fixed, so every row's columns agree, and the
  // `attributes` key's cell stays reserved whether or not it is drawn.
  //
  // Below `sm` the tracks collapse to one column and each input takes the row:
  // three 100-odd-pixel boxes side by side are not usable on a phone, and the
  // stacked order still reads output side then source side.
  return (
    <div className="flex flex-col gap-3">
      <div className="grid grid-cols-1 gap-x-3 gap-y-3 sm:grid-cols-6">
        <LabelledInput
          id={nameId}
          className="sm:col-span-2"
          label={<Trans id="admin.saml-apps.mapping.name">Name</Trans>}
          value={row.name}
          disabled={submitting}
          isInvalid={isInvalid("name")}
          onChange={(value) => update({ name: value })}
        />
        <LabelledInput
          id={formatId}
          className="sm:col-span-2"
          label={
            <Trans id="admin.saml-apps.mapping.name-format">Name format</Trans>
          }
          value={row.nameFormat}
          disabled={submitting}
          onChange={(value) => update({ nameFormat: value })}
        />
        <LabelledInput
          id={friendlyId}
          className="sm:col-span-2"
          label={
            <Trans id="admin.saml-apps.mapping.friendly-name">
              Friendly name
            </Trans>
          }
          value={row.friendlyName}
          disabled={submitting}
          onChange={(value) => update({ friendlyName: value })}
        />

        <Select
          className="sm:col-span-2"
          variant="secondary"
          isDisabled={submitting}
          value={row.sourceKind}
          onChange={(key) => {
            if (typeof key === "string") {
              update({ sourceKind: key as AttributeSourceKind });
            }
          }}
        >
          <Label>
            <Trans id="admin.saml-apps.mapping.source">Source</Trans>
          </Label>
          <Select.Trigger>
            <Select.Value />
            <Select.Indicator />
          </Select.Trigger>
          <Select.Popover>
            <ListBox>
              {attributeSources.map((source) => (
                <ListBox.Item
                  key={source}
                  id={source}
                  textValue={i18n._(attributeSourceLabel(source))}
                >
                  {i18n._(attributeSourceLabel(source))}
                  <ListBox.ItemIndicator />
                </ListBox.Item>
              ))}
              <ListBox.Item
                id="attributes"
                textValue={i18n._(accountAttributeLabel)}
              >
                {i18n._(accountAttributeLabel)}
                <ListBox.ItemIndicator />
              </ListBox.Item>
            </ListBox>
          </Select.Popover>
        </Select>

        {/* The cell is always there: a row reading one of the named facts has
            nothing to put in it, and letting the track collapse would shift
            the switch to the source's trailing edge on those rows only. */}
        <div className="sm:col-span-2">
          {row.sourceKind === "attributes" && (
            <LabelledInput
              id={keyId}
              label={
                <Trans id="admin.saml-apps.mapping.key">Attribute key</Trans>
              }
              value={row.sourceKey}
              disabled={submitting}
              isInvalid={isInvalid("key")}
              onChange={(value) => update({ sourceKey: value })}
            />
          )}
        </div>

        {/* Bottom-aligned to the row's inputs rather than given a label of its
            own: the switch carries its own text, and the input boxes beside it
            end at the same line, so the row reads across. */}
        <div className="flex h-9 items-center self-end sm:col-span-2">
          <Switch
            isSelected={row.multi}
            isDisabled={submitting}
            onChange={(selected) => update({ multi: selected })}
          >
            <Switch.Content>
              <Switch.Control>
                <Switch.Thumb />
              </Switch.Control>
              <Trans id="admin.saml-apps.mapping.multi">Send every value</Trans>
            </Switch.Content>
          </Switch>
        </div>
      </div>

      {error !== undefined && error !== null && (
        <span className="text-sm text-danger">
          <FormMessages errors={[error]} />
        </span>
      )}
    </div>
  );
}

/**
 * One labeled text input of a row.
 *
 * The row's inputs differ only in their label and which part of the row they
 * write, so they are drawn by one of these rather than several times over: a
 * row that repeated the label, the id and the change handler for each of them
 * would have that many places to keep the same.
 *
 * The label is always drawn, including on the row's second line: a reader who
 * has scrolled to a row's source side should not have to look back up to know
 * which box holds the attribute key. The grid's tracks carry the widths, so the
 * col-span comes in from the caller rather than every input being `flex-1`.
 */
function LabelledInput({
  id,
  label,
  value,
  disabled,
  className,
  isInvalid = false,
  onChange,
}: {
  id: string;
  label: ReactNode;
  value: string;
  disabled: boolean;
  /** The grid cell this input occupies. */
  className?: string;
  isInvalid?: boolean;
  onChange: (value: string) => void;
}) {
  return (
    <div className={`flex flex-col gap-1 ${className ?? ""}`}>
      <Label htmlFor={id}>{label}</Label>
      <Input
        id={id}
        value={value}
        disabled={disabled}
        aria-invalid={isInvalid}
        autoComplete="off"
        spellCheck={false}
        variant="secondary"
        onChange={(event) => onChange(event.target.value)}
      />
    </div>
  );
}
