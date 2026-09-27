import {
  ComboBox,
  Description,
  Input,
  Label,
  ListBox,
  Switch,
} from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import { type ReactNode, useId, useRef } from "react";
import { FormMessages } from "@/components/custom/FormMessages";
import type { RowProblem } from "@/components/custom/RowsField";
import { useFieldContext, useFormContext } from "@/forms/context";
import { withoutServerErrors } from "@/forms/server-errors";
import {
  attributeKeyOf,
  attributeNameFormats,
  attributeSourceLabel,
  attributeSourceOf,
  attributeSourcePrefix,
  attributeSources,
  shortAttributeNameFormat,
} from "@/pages/admin/saml-applications/saml-projection";

/**
 * The attribute map's table. The heading and every row share these tracks, so the
 * columns line up; the last one is the remove button `RowsField` draws.
 *
 * Below `sm` the tracks are dropped, not shrunk: four controls and a switch
 * across a phone's width are unusable at any size, so each takes a line.
 */
const mappingTracks =
  "sm:grid-cols-[minmax(0,1.3fr)_minmax(0,1.3fr)_minmax(0,1.2fr)_minmax(0,1.4fr)_4.5rem]";

/** The column names. `aria-hidden`: the controls carry their own labels. */
export function MappingHeader() {
  return (
    <div
      aria-hidden="true"
      className={`hidden gap-x-2 text-xs font-medium text-muted sm:grid ${mappingTracks}`}
    >
      <span className="truncate">
        <Trans id="admin.saml-apps.mapping.name">Attribute name</Trans>
      </span>
      <span className="truncate">
        <Trans id="admin.saml-apps.mapping.name-format">Name format</Trans>
      </span>
      <span className="truncate">
        <Trans id="admin.saml-apps.mapping.friendly-name">Friendly name</Trans>
      </span>
      <span className="truncate">
        <Trans id="admin.saml-apps.mapping.source">Source</Trans>
      </span>
      <span className="truncate text-center">
        <Trans id="admin.saml-apps.mapping.multi.column">Multi</Trans>
      </span>
    </div>
  );
}

/**
 * One row of the attribute map while it is being edited. The source is a named
 * account fact or an attribute key; the row holds it as one string and composes
 * the wire value on submit.
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
 * The row's controls, drawn inside `RowsField`.
 *
 * The row addresses the field by position, because a mapping has no identity
 * until it is saved: the row is the unit of edit and of error, so `problem`
 * names which of its inputs the submit objected to and only that one is marked.
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
  const { t, i18n } = useLingui();
  const field = useFieldContext<MappingRow[]>();
  const form = useFormContext();
  const submitting = useStore(form.store, (state) => state.isSubmitting);
  const nameId = useId();
  const friendlyId = useId();

  // Text typed into a picker, kept out of the form until it is committed so the
  // caret does not jump mid-word. A choice clears it.
  const typedFormat = useRef<string | null>(null);
  const typedSource = useRef<string | null>(null);

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

  /**
   * The format choices, plus the row's own value when a record carries one the
   * list does not cover.
   *
   * A control whose `selectedKey` is not among its items renders as if nothing
   * were selected, so the stored value is always offered. Its `textValue` is the
   * whole URN — what the input shows and what is saved are the same string.
   */
  const formatItems = [
    ...attributeNameFormats.map((format) => ({
      id: format,
      short: shortAttributeNameFormat(format),
      full: format,
    })),
    ...(attributeNameFormats.includes(
      row.nameFormat as (typeof attributeNameFormats)[number],
    )
      ? []
      : [
          {
            id: row.nameFormat,
            short: shortAttributeNameFormat(row.nameFormat),
            full: row.nameFormat,
          },
        ]),
  ];

  /**
   * The source choices, on the same rule: the named facts, plus the row's own
   * attribute source when it has one. `textValue` is the wire form again, so the
   * input shows `attributes.mail` rather than the key alone — and the label says
   * the same thing in the reader's language.
   */
  const sourceValue = mappingSource(row);
  const sourceItems = [
    ...attributeSources.map((source) => ({
      id: source,
      label: i18n._(attributeSourceLabel(source)),
    })),
    ...(row.sourceKind === "attributes" && row.sourceKey !== ""
      ? [{ id: sourceValue, label: sourceValue }]
      : []),
  ];

  /**
   * Reading a typed source.
   *
   * The wire form is what the input shows, so a reader may type either it or the
   * bare key. A prefix already present is taken as their own spelling of the
   * same thing and is not doubled.
   */
  const commitSource = () => {
    const typed = typedSource.current;
    typedSource.current = null;
    if (typed === null || typed === sourceValue) return;
    const bare = typed.startsWith(attributeSourcePrefix)
      ? typed.slice(attributeSourcePrefix.length)
      : typed;
    const named = (attributeSources as readonly string[]).includes(bare);
    const { sourceKind, sourceKey } = readSourceKind(
      named ? bare : attributeSourceOf(bare),
    );
    update({ sourceKind, sourceKey });
  };

  const commitFormat = () => {
    const typed = typedFormat.current;
    typedFormat.current = null;
    if (typed === null || typed === row.nameFormat) return;
    update({ nameFormat: typed });
  };

  return (
    <div className="flex flex-col gap-1.5">
      <div
        className={`grid grid-cols-1 items-center gap-x-2 gap-y-3 [&>template]:hidden sm:gap-y-0 ${mappingTracks}`}
      >
        <LabelledInput
          id={nameId}
          label={
            <Trans id="admin.saml-apps.mapping.name">Attribute name</Trans>
          }
          placeholder={t({
            id: "admin.saml-apps.mapping.name.placeholder",
            message: "mail",
          })}
          value={row.name}
          disabled={submitting}
          isInvalid={isInvalid("name")}
          onChange={(value) => update({ name: value })}
        />

        <ComboBox
          className="w-full"
          variant="secondary"
          isDisabled={submitting}
          allowsCustomValue
          selectedKey={row.nameFormat}
          aria-label={t({
            id: "admin.saml-apps.mapping.name-format",
            message: "Name format",
          })}
          onInputChange={(value) => {
            typedFormat.current = value;
          }}
          onSelectionChange={(key) => {
            if (typeof key !== "string") return;
            typedFormat.current = null;
            update({ nameFormat: key });
          }}
        >
          <ComboBox.InputGroup>
            <Input
              placeholder={t({
                id: "admin.saml-apps.mapping.name-format.placeholder",
                message: "basic",
              })}
              onBlur={commitFormat}
              onKeyDown={(event) => {
                if (event.key === "Enter") commitFormat();
              }}
            />
            <ComboBox.Trigger />
          </ComboBox.InputGroup>
          <ComboBox.Popover>
            <ListBox items={formatItems}>
              {(item) => (
                <ListBox.Item
                  key={item.id}
                  id={item.id}
                  textValue={item.full}
                  className="py-2"
                >
                  {/* Label over Description: a listbox item lays its children
                      out in a row, so the pair needs its own column. The four
                      URNs share 48 characters of namespace, so the short name
                      is what the reader picks between. */}
                  <div className="flex min-w-0 flex-col">
                    <Label className="truncate">{item.short}</Label>
                    <Description className="truncate text-xs">
                      {item.full}
                    </Description>
                  </div>
                  <ListBox.ItemIndicator />
                </ListBox.Item>
              )}
            </ListBox>
          </ComboBox.Popover>
        </ComboBox>

        <LabelledInput
          id={friendlyId}
          label={
            <Trans id="admin.saml-apps.mapping.friendly-name">
              Friendly name
            </Trans>
          }
          placeholder={t({
            id: "admin.saml-apps.mapping.friendly-name.placeholder",
            message: "Mail address",
          })}
          value={row.friendlyName}
          disabled={submitting}
          onChange={(value) => update({ friendlyName: value })}
        />

        <ComboBox
          className="w-full"
          variant="secondary"
          isDisabled={submitting}
          allowsCustomValue
          selectedKey={sourceValue}
          aria-label={t({
            id: "admin.saml-apps.mapping.source",
            message: "Source",
          })}
          onInputChange={(value) => {
            typedSource.current = value;
          }}
          onSelectionChange={(key) => {
            if (typeof key !== "string") return;
            typedSource.current = null;
            const { sourceKind, sourceKey } = readSourceKind(key);
            update({ sourceKind, sourceKey });
          }}
        >
          <ComboBox.InputGroup>
            <Input
              placeholder={t({
                id: "admin.saml-apps.mapping.source.placeholder",
                message: "attributes.mail",
              })}
              onBlur={commitSource}
              onKeyDown={(event) => {
                if (event.key === "Enter") commitSource();
              }}
            />
            <ComboBox.Trigger />
          </ComboBox.InputGroup>
          <ComboBox.Popover>
            <ListBox items={sourceItems}>
              {(item) => (
                <ListBox.Item
                  key={item.id}
                  id={item.id}
                  textValue={item.id}
                  className="py-2"
                >
                  {/* The label is the same fact in the reader's language; the
                      wire form is what the input holds, so the two are told
                      apart here rather than in the value. */}
                  <span className="flex min-w-0 flex-col">
                    <span className="truncate">{item.label}</span>
                    <span className="truncate text-xs text-muted">
                      {item.id}
                    </span>
                  </span>
                  <ListBox.ItemIndicator />
                </ListBox.Item>
              )}
            </ListBox>
          </ComboBox.Popover>
        </ComboBox>

        {/* Bare, because the heading names the column. */}
        <div className="flex items-center justify-center">
          <Switch
            aria-label={t({
              id: "admin.saml-apps.mapping.multi",
              message: "Send every value",
            })}
            isSelected={row.multi}
            isDisabled={submitting}
            onChange={(selected) => update({ multi: selected })}
          >
            <Switch.Content>
              <Switch.Control>
                <Switch.Thumb />
              </Switch.Control>
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
 * One text cell of a row. The label is for assistive technology only: the column
 * is named once by `MappingHeader`, and the placeholder says the same thing in
 * the empty box.
 */
function LabelledInput({
  id,
  label,
  placeholder,
  value,
  disabled,
  isInvalid = false,
  onChange,
}: {
  id: string;
  label: ReactNode;
  placeholder: string;
  value: string;
  disabled: boolean;
  isInvalid?: boolean;
  onChange: (value: string) => void;
}) {
  return (
    <div className="flex min-w-0 flex-col">
      <label className="sr-only" htmlFor={id}>
        {label}
      </label>
      <Input
        id={id}
        value={value}
        disabled={disabled}
        aria-invalid={isInvalid || undefined}
        autoComplete="off"
        spellCheck={false}
        placeholder={placeholder}
        variant="secondary"
        onChange={(event) => onChange(event.target.value)}
      />
    </div>
  );
}
