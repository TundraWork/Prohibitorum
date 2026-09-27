import {
  ComboBox,
  Description,
  Fieldset,
  Input,
  Label,
  ListBox,
  Separator,
  Switch,
  TextField,
} from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import { type ReactNode, useRef } from "react";
import { FormMessages } from "@/components/custom/FormMessages";
import { type RowProblem, RowsField } from "@/components/custom/RowsField";
import { useFieldContext, useFormContext } from "@/forms/context";
import { withoutServerErrors } from "@/forms/server-errors";
import {
  attributeKeyOf,
  attributeNameFormats,
  attributeSourceLabel,
  attributeSourceOf,
  attributeSourcePrefix,
  attributeSources,
  defaultAttributeNameFormat,
  shortAttributeNameFormat,
} from "@/pages/admin/saml-applications/saml-projection";

/**
 * The attribute map's table on a wide editor. The heading and every row share
 * these tracks, so the columns line up; the last one holds the row's remove
 * button, which the row draws itself. The name format holds a whole URN, so it
 * gets the widest track.
 */
const mappingTracks =
  "grid-cols-[minmax(0,1.2fr)_minmax(0,1.3fr)_minmax(0,1.5fr)_minmax(0,1.2fr)_4rem_2rem]";

/**
 * The attribute map, as a field of `mappings`.
 *
 * It is drawn twice and shown once. Where the editor is wide enough for five
 * controls on a line, a mapping is a row of a table under one set of column
 * names; below that, each mapping is a fieldset with its own labels, because a
 * stack of unlabelled boxes says nothing about which is which. The switch is
 * on the editor's own width rather than the viewport's — the rail opening or
 * closing changes the room a row has — and the layout not in use is
 * `display: none`, so it is out of the tab order and the accessibility tree.
 * Both read and write the same row, so resizing keeps what was typed.
 */
export function AttributeMapField() {
  return (
    <div className="@container/mapping">
      <RowsField<MappingRow>
        label={<Trans id="admin.saml-apps.mapping.title">Attribute map</Trans>}
        description={
          <Trans id="admin.saml-apps.mapping.hint">
            Publish the account's own facts under the names the service provider
            expects.
          </Trans>
        }
        removePlacement="inRow"
        header={<MappingHeader />}
        emptyRow={() => ({
          name: "",
          nameFormat: defaultAttributeNameFormat,
          friendlyName: "",
          sourceKind: "username",
          sourceKey: "",
          multi: false,
        })}
        addLabel={
          <Trans id="admin.saml-apps.mapping.add">Add an attribute</Trans>
        }
        renderRow={(_row, index, error, problem, remove) => (
          <MappingRowFields
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
 * The column names of the wide layout. `aria-hidden`: every control carries its
 * own name, so a reader hearing the row does not also need the heading.
 */
function MappingHeader() {
  return (
    <div
      aria-hidden="true"
      className={`hidden gap-x-2 text-xs font-medium text-muted @2xl/mapping:grid ${mappingTracks}`}
    >
      <span className="truncate">
        <Trans id="admin.saml-apps.mapping.name">Attribute name</Trans>
      </span>
      <span className="truncate">
        <Trans id="admin.saml-apps.mapping.source">Source</Trans>
      </span>
      <span className="truncate">
        <Trans id="admin.saml-apps.mapping.name-format">Name format</Trans>
      </span>
      <span className="truncate">
        <Trans id="admin.saml-apps.mapping.friendly-name">Friendly name</Trans>
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

interface FormatItem {
  id: string;
  short: string;
  full: string;
}

interface SourceItem {
  id: string;
  label: string;
}

/**
 * One mapping, in both layouts.
 *
 * The row addresses the field by position, because a mapping has no identity
 * until it is saved: the row is the unit of edit and of error, so `problem`
 * names which of its inputs the submit objected to and only that one is marked.
 * The row's state and what a commit does are worked out once here; the two
 * layouts only place the same controls differently.
 */
function MappingRowFields({
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
  const { t, i18n } = useLingui();
  const field = useFieldContext<MappingRow[]>();
  const form = useFormContext();
  const submitting = useStore(form.store, (state) => state.isSubmitting);

  const row = field.state.value[index];
  if (row === undefined) return null;

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
  const formatItems: FormatItem[] = [
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
  const sourceItems: SourceItem[] = [
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
  const commitSource = (typed: string) => {
    if (typed === sourceValue) return;
    const bare = typed.startsWith(attributeSourcePrefix)
      ? typed.slice(attributeSourcePrefix.length)
      : typed;
    const named = (attributeSources as readonly string[]).includes(bare);
    update(readSourceKind(named ? bare : attributeSourceOf(bare)));
  };

  const commitFormat = (typed: string) => {
    if (typed === row.nameFormat) return;
    update({ nameFormat: typed });
  };

  const controls = (labelled: boolean) => ({
    name: (
      <MappingTextInput
        labelled={labelled}
        label={<Trans id="admin.saml-apps.mapping.name">Attribute name</Trans>}
        placeholder={t({
          id: "admin.saml-apps.mapping.name.placeholder",
          message: "mail",
        })}
        value={row.name}
        isDisabled={submitting}
        isInvalid={problem?.field === "name"}
        onChange={(name) => update({ name })}
      />
    ),
    source: (
      <SourcePicker
        labelled={labelled}
        items={sourceItems}
        value={sourceValue}
        isDisabled={submitting}
        // A missing key is a fault in the source as much as a bad fact is: the
        // key is typed into the same box.
        isInvalid={problem?.field === "source" || problem?.field === "key"}
        onSelect={(key) => update(readSourceKind(key))}
        onCommit={commitSource}
      />
    ),
    nameFormat: (
      <NameFormatPicker
        labelled={labelled}
        items={formatItems}
        value={row.nameFormat}
        isDisabled={submitting}
        onSelect={(nameFormat) => update({ nameFormat })}
        onCommit={commitFormat}
      />
    ),
    friendlyName: (
      <MappingTextInput
        labelled={labelled}
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
        isDisabled={submitting}
        onChange={(friendlyName) => update({ friendlyName })}
      />
    ),
    multi: (
      <MultiSwitch
        labelled={labelled}
        value={row.multi}
        isDisabled={submitting}
        onChange={(multi) => update({ multi })}
      />
    ),
  });

  const hasError = error !== undefined && error !== null;
  const message = hasError && (
    <span className="text-sm text-danger">
      <FormMessages errors={[error]} />
    </span>
  );

  const wide = controls(false);
  const narrow = controls(true);

  return (
    <>
      <div className="hidden flex-col gap-1.5 @2xl/mapping:flex">
        <div className={`grid items-center gap-x-2 ${mappingTracks}`}>
          {wide.name}
          {wide.source}
          {wide.nameFormat}
          {wide.friendlyName}
          <div className="flex justify-center">{wide.multi}</div>
          <div className="flex justify-end">{remove}</div>
        </div>
        {message}
      </div>

      <div className="flex flex-col gap-3 @2xl/mapping:hidden">
        {index > 0 && <Separator />}
        {/* The remove button sits on the legend's line. It cannot share a flex
            line with the legend, which has to be the fieldset's first child and
            is drawn in the fieldset's border rather than its content — so it is
            placed over that line from a wrapper, whose top is the legend's. For
            the same reason the fieldset's gap does not reach the legend, which
            takes its own margin instead. */}
        <div className="relative">
          <Fieldset className="gap-3">
            <Fieldset.Legend className="mb-3 pe-10">
              <MappingLegend row={index + 1} />
            </Fieldset.Legend>
            <Fieldset.Group>
              {narrow.name}
              {narrow.source}
              {narrow.nameFormat}
              {narrow.friendlyName}
              {narrow.multi}
            </Fieldset.Group>
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

/** The narrow layout's name for a mapping, numbered as its remove button is. */
function MappingLegend({ row }: { row: number }) {
  return <Trans id="admin.saml-apps.mapping.row">Attribute {row}</Trans>;
}

/**
 * A text control of the row. On the wide layout the column heading names it and
 * the label is for assistive technology only; on the narrow one it is shown.
 */
function MappingTextInput({
  labelled,
  label,
  placeholder,
  value,
  isDisabled,
  isInvalid = false,
  onChange,
}: {
  labelled: boolean;
  label: ReactNode;
  placeholder: string;
  value: string;
  isDisabled: boolean;
  isInvalid?: boolean;
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
        autoComplete="off"
        spellCheck={false}
        placeholder={placeholder}
        variant="secondary"
      />
    </TextField>
  );
}

/**
 * The source: one of the named facts, or `attributes.<key>` typed in.
 *
 * Text typed into the box is kept out of the form until it is committed, on
 * blur or Enter, so the caret does not jump mid-word; a choice from the list
 * clears it. Each picker keeps its own, because the row draws two of them and
 * only the one being typed in has anything to commit.
 */
function SourcePicker({
  labelled,
  items,
  value,
  isDisabled,
  isInvalid,
  onSelect,
  onCommit,
}: {
  labelled: boolean;
  items: SourceItem[];
  value: string;
  isDisabled: boolean;
  isInvalid: boolean;
  onSelect: (key: string) => void;
  onCommit: (typed: string) => void;
}) {
  const { t } = useLingui();
  const typed = useRef<string | null>(null);
  const commit = () => {
    const text = typed.current;
    typed.current = null;
    if (text !== null) onCommit(text);
  };

  return (
    <ComboBox
      className="w-full min-w-0"
      variant="secondary"
      isDisabled={isDisabled}
      isInvalid={isInvalid}
      allowsCustomValue
      selectedKey={value}
      onInputChange={(text) => {
        typed.current = text;
      }}
      onSelectionChange={(key) => {
        if (typeof key !== "string") return;
        typed.current = null;
        onSelect(key);
      }}
    >
      <Label className={labelled ? undefined : "sr-only"}>
        <Trans id="admin.saml-apps.mapping.source">Source</Trans>
      </Label>
      <ComboBox.InputGroup>
        <Input
          placeholder={t({
            id: "admin.saml-apps.mapping.source.placeholder",
            message: "attributes.mail",
          })}
          onBlur={commit}
          onKeyDown={(event) => {
            if (event.key === "Enter") commit();
          }}
        />
        <ComboBox.Trigger />
      </ComboBox.InputGroup>
      <ComboBox.Popover>
        <ListBox items={items}>
          {(item) => (
            <ListBox.Item
              key={item.id}
              id={item.id}
              textValue={item.id}
              className="py-2"
            >
              {/* The label is the same fact in the reader's language; the wire
                  form is what the input holds, so the two are told apart here
                  rather than in the value. */}
              <span className="flex min-w-0 flex-col">
                <span className="truncate">{item.label}</span>
                <span className="truncate text-xs text-muted">{item.id}</span>
              </span>
              <ListBox.ItemIndicator />
            </ListBox.Item>
          )}
        </ListBox>
      </ComboBox.Popover>
    </ComboBox>
  );
}

/**
 * The name format: one of the standard URNs, or any other typed in. The box
 * shows and saves the whole URN; the list leads with the short name, because
 * the standard ones share 48 characters of namespace.
 */
function NameFormatPicker({
  labelled,
  items,
  value,
  isDisabled,
  onSelect,
  onCommit,
}: {
  labelled: boolean;
  items: FormatItem[];
  value: string;
  isDisabled: boolean;
  onSelect: (key: string) => void;
  onCommit: (typed: string) => void;
}) {
  const { t } = useLingui();
  const typed = useRef<string | null>(null);
  const commit = () => {
    const text = typed.current;
    typed.current = null;
    if (text !== null) onCommit(text);
  };

  return (
    <ComboBox
      className="w-full min-w-0"
      variant="secondary"
      isDisabled={isDisabled}
      allowsCustomValue
      selectedKey={value}
      onInputChange={(text) => {
        typed.current = text;
      }}
      onSelectionChange={(key) => {
        if (typeof key !== "string") return;
        typed.current = null;
        onSelect(key);
      }}
    >
      <Label className={labelled ? undefined : "sr-only"}>
        <Trans id="admin.saml-apps.mapping.name-format">Name format</Trans>
      </Label>
      <ComboBox.InputGroup>
        <Input
          placeholder={t({
            id: "admin.saml-apps.mapping.name-format.placeholder",
            message: "basic",
          })}
          onBlur={commit}
          onKeyDown={(event) => {
            if (event.key === "Enter") commit();
          }}
        />
        <ComboBox.Trigger />
      </ComboBox.InputGroup>
      <ComboBox.Popover>
        <ListBox items={items}>
          {(item) => (
            <ListBox.Item
              key={item.id}
              id={item.id}
              textValue={item.full}
              className="py-2"
            >
              {/* Label over Description: a listbox item lays its children out
                  in a row, so the pair needs its own column. */}
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
  );
}

/**
 * Whether a multi-valued source is sent as one attribute value per entry. Bare
 * on the wide layout, where the column heading names it; with its words beside
 * it on the narrow one, the way `SwitchField` draws a switch.
 */
function MultiSwitch({
  labelled,
  value,
  isDisabled,
  onChange,
}: {
  labelled: boolean;
  value: boolean;
  isDisabled: boolean;
  onChange: (value: boolean) => void;
}) {
  const { t } = useLingui();
  const label = t({
    id: "admin.saml-apps.mapping.multi",
    message: "Send every value",
  });

  return (
    <Switch
      aria-label={labelled ? undefined : label}
      isSelected={value}
      isDisabled={isDisabled}
      onChange={onChange}
    >
      <Switch.Content>
        <Switch.Control>
          <Switch.Thumb />
        </Switch.Control>
        {labelled && label}
      </Switch.Content>
    </Switch>
  );
}
