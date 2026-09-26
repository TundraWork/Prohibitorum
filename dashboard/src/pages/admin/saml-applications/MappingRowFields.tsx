import { Input, Label, ListBox, Select, Switch } from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import { type ReactNode, useId } from "react";
import { FormMessages } from "@/components/custom/FormMessages";
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
 */
export function MappingRowFields({
  index,
  error,
}: {
  index: number;
  /** The input's complaint for this row, if the submit found one. */
  error: ReactNode;
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

  const invalid = error !== undefined && error !== null;

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

  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap items-end gap-3">
        <LabelledInput
          id={nameId}
          label={<Trans id="admin.saml-apps.mapping.name">Name</Trans>}
          value={row.name}
          disabled={submitting}
          isInvalid={invalid}
          onChange={(value) => update({ name: value })}
        />
        <LabelledInput
          id={formatId}
          label={
            <Trans id="admin.saml-apps.mapping.name-format">Name format</Trans>
          }
          value={row.nameFormat}
          disabled={submitting}
          onChange={(value) => update({ nameFormat: value })}
        />
        <LabelledInput
          id={friendlyId}
          label={
            <Trans id="admin.saml-apps.mapping.friendly-name">
              Friendly name
            </Trans>
          }
          value={row.friendlyName}
          disabled={submitting}
          onChange={(value) => update({ friendlyName: value })}
        />
      </div>

      <div className="flex flex-wrap items-end gap-3">
        <Select
          className="w-44"
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

        {row.sourceKind === "attributes" && (
          <LabelledInput
            id={keyId}
            label={
              <Trans id="admin.saml-apps.mapping.key">Attribute key</Trans>
            }
            value={row.sourceKey}
            disabled={submitting}
            isInvalid={invalid}
            onChange={(value) => update({ sourceKey: value })}
          />
        )}

        <div className="pb-2.5">
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

      {invalid && (
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
 * The five inputs differ only in their label and which part of the row they
 * write, so they are drawn by one of these rather than five times over: a row
 * that repeated the label, the id and the change handler five times would have
 * five places to keep the same.
 */
function LabelledInput({
  id,
  label,
  value,
  disabled,
  isInvalid = false,
  onChange,
}: {
  id: string;
  label: ReactNode;
  value: string;
  disabled: boolean;
  isInvalid?: boolean;
  onChange: (value: string) => void;
}) {
  return (
    <div className="flex min-w-32 flex-1 flex-col gap-1">
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
