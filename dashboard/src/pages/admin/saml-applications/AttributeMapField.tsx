import { Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import { type ReactNode, useRef } from "react";
import { FormMessages } from "@/components/custom/FormMessages";
import { useHostOverlayContainer } from "@/components/custom/host-overlay-container";
import { type RowProblem, RowsField } from "@/components/custom/RowsField";
import { Field, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
  InputGroupText,
} from "@/components/ui/input-group";
import {
  Select,
  SelectItem,
  SelectPopup,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
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
 * The attribute map's tracks on a wide editor: the name, the source, the name
 * format, the friendly name, the multi-value switch and the remove button. The
 * heading and every row share them, so the columns line up.
 */
const mappingTracks =
  "@2xl/mapping:grid-cols-[minmax(0,1fr)_minmax(0,1.1fr)_minmax(0,1fr)_minmax(0,1fr)_3.5rem_2rem]";

/**
 * The attribute map, as a field of `mappings`.
 *
 * Where the editor has room for six controls on a line, a mapping is a row of a
 * table under one set of column names; below that, each mapping stacks its
 * controls, each with its label, because a stack of unlabelled boxes says
 * nothing about which is which. It is one set of controls reshaped by the
 * editor's own width — the outline rail opening or closing changes the room a
 * row has — so there is a single control of each kind per row for a reader and
 * a test to find.
 *
 * No control carries example text: an empty box with a grey `mail` in it reads
 * as a value that is already there.
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
      className={`hidden gap-x-2 font-medium text-muted-foreground text-xs @2xl/mapping:grid ${mappingTracks}`}
    >
      <span className="truncate">
        <NameLabel />
      </span>
      <span className="truncate">
        <SourceLabel />
      </span>
      <span className="truncate">
        <FormatLabel />
      </span>
      <span className="truncate">
        <FriendlyLabel />
      </span>
      <span className="truncate text-center">
        <Trans id="admin.saml-apps.mapping.multi.column">Multi</Trans>
      </span>
    </div>
  );
}

function NameLabel() {
  return <Trans id="admin.saml-apps.mapping.name">Attribute name</Trans>;
}

function SourceLabel() {
  return <Trans id="admin.saml-apps.mapping.source">Source</Trans>;
}

function FormatLabel() {
  return <Trans id="admin.saml-apps.mapping.name-format">Name format</Trans>;
}

function FriendlyLabel() {
  return (
    <Trans id="admin.saml-apps.mapping.friendly-name">Friendly name</Trans>
  );
}

/**
 * One row of the attribute map while it is being edited. The source is a named
 * account fact or an attribute key; the row holds the two parts apart and
 * composes the wire value on submit.
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

/** The sources a row offers, in the order the select lists them. */
const sourceKinds: readonly AttributeSourceKind[] = [
  ...attributeSources,
  "attributes",
];

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
 * One mapping.
 *
 * The row addresses the field by position, because a mapping has no identity
 * until it is saved: the row is the unit of edit and of error, so `problem`
 * names which of its inputs the submit objected to and only that one is marked.
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
  const anchor = useRef<HTMLDivElement>(null);
  const container = useHostOverlayContainer(anchor);

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

  // A format outside the standard three is the record's own, and is offered as
  // it is rather than rewritten to one of them.
  const isStandardFormat = (attributeNameFormats as readonly string[]).includes(
    row.nameFormat,
  );
  const formats = isStandardFormat
    ? attributeNameFormats
    : [...attributeNameFormats, row.nameFormat];

  const hasError = error !== undefined && error !== null;
  const nameInvalid = problem?.field === "name";
  // A missing key is a fault in the source as much as a bad fact is.
  const sourceInvalid = problem?.field === "source";
  const keyInvalid = problem?.field === "key";

  return (
    <div
      ref={anchor}
      className={`grid grid-cols-[minmax(0,1fr)_auto] items-start gap-x-2 gap-y-3 ${mappingTracks} ${
        // Stacked rows need a rule between them: one row's last control would
        // otherwise run into the next row's first label.
        index > 0 ? "@max-2xl/mapping:border-t @max-2xl/mapping:pt-4" : ""
      }`}
    >
      <Field
        className="col-start-1 row-start-1 min-w-0"
        disabled={submitting}
        invalid={nameInvalid}
      >
        <FieldLabel className="@2xl/mapping:sr-only">
          <NameLabel />
        </FieldLabel>
        <Input
          className="font-mono"
          value={row.name}
          aria-invalid={nameInvalid || undefined}
          autoComplete="off"
          spellCheck={false}
          onChange={(event) => update({ name: event.target.value })}
        />
      </Field>

      <div className="col-span-2 flex min-w-0 flex-col gap-2 @2xl/mapping:col-span-1">
        <Field disabled={submitting} invalid={sourceInvalid}>
          <FieldLabel className="@2xl/mapping:sr-only">
            <SourceLabel />
          </FieldLabel>
          <Select
            value={row.sourceKind}
            onValueChange={(next) => {
              if ((sourceKinds as readonly string[]).includes(next as string)) {
                update({ sourceKind: next as AttributeSourceKind });
              }
            }}
          >
            <SelectTrigger aria-invalid={sourceInvalid || undefined}>
              <SelectValue>
                {() => i18n._(attributeSourceLabel(row.sourceKind))}
              </SelectValue>
            </SelectTrigger>
            <SelectPopup
              alignItemWithTrigger={false}
              portalProps={{ container }}
            >
              {sourceKinds.map((kind) => (
                <SelectItem key={kind} value={kind}>
                  <div className="flex min-w-0 flex-col">
                    <span>{i18n._(attributeSourceLabel(kind))}</span>
                    <span className="font-mono text-muted-foreground text-xs">
                      {kind === "attributes"
                        ? `${attributeSourcePrefix}…`
                        : kind}
                    </span>
                  </div>
                </SelectItem>
              ))}
            </SelectPopup>
          </Select>
        </Field>
        {row.sourceKind === "attributes" && (
          <InputGroup>
            <InputGroupAddon>
              <InputGroupText className="font-mono">
                {attributeSourcePrefix}
              </InputGroupText>
            </InputGroupAddon>
            <InputGroupInput
              className="font-mono"
              value={row.sourceKey}
              disabled={submitting}
              aria-label={t({
                id: "admin.saml-apps.mapping.key",
                message: "Attribute key",
              })}
              aria-invalid={keyInvalid || undefined}
              autoComplete="off"
              spellCheck={false}
              onChange={(event) => update({ sourceKey: event.target.value })}
            />
          </InputGroup>
        )}
      </div>

      <Field
        className="col-span-2 min-w-0 @2xl/mapping:col-span-1"
        disabled={submitting}
      >
        <FieldLabel className="@2xl/mapping:sr-only">
          <FormatLabel />
        </FieldLabel>
        <Select
          value={row.nameFormat}
          onValueChange={(next) => {
            if (typeof next === "string") update({ nameFormat: next });
          }}
        >
          <SelectTrigger>
            {/* The whole value wraps rather than truncates: a format outside
                the standard three is what the service provider asked for, and
                the part that is cut off is the part that tells them apart. */}
            <SelectValue className="break-all text-clip">
              {() =>
                isStandardFormat
                  ? shortAttributeNameFormat(row.nameFormat)
                  : row.nameFormat
              }
            </SelectValue>
          </SelectTrigger>
          <SelectPopup alignItemWithTrigger={false} portalProps={{ container }}>
            {formats.map((format) => (
              <SelectItem key={format} value={format}>
                <div className="flex min-w-0 flex-col">
                  <span>{shortAttributeNameFormat(format)}</span>
                  <span className="font-mono text-muted-foreground text-xs">
                    {format}
                  </span>
                </div>
              </SelectItem>
            ))}
          </SelectPopup>
        </Select>
      </Field>

      <Field
        className="col-span-2 min-w-0 @2xl/mapping:col-span-1"
        disabled={submitting}
      >
        <FieldLabel className="@2xl/mapping:sr-only">
          <FriendlyLabel />
        </FieldLabel>
        <Input
          value={row.friendlyName}
          autoComplete="off"
          spellCheck={false}
          onChange={(event) => update({ friendlyName: event.target.value })}
        />
      </Field>

      <Field
        className="col-span-2 @2xl/mapping:col-span-1 @2xl/mapping:h-9 @2xl/mapping:items-center @2xl/mapping:justify-center"
        disabled={submitting}
      >
        <div className="flex items-center gap-2">
          <Switch
            checked={row.multi}
            onCheckedChange={(multi) => update({ multi })}
          />
          <FieldLabel className="@2xl/mapping:sr-only">
            <Trans id="admin.saml-apps.mapping.multi">Send every value</Trans>
          </FieldLabel>
        </div>
      </Field>

      <div className="col-start-2 row-start-1 flex h-9 items-center justify-end self-end @2xl/mapping:col-start-6 @2xl/mapping:self-start sm:h-8">
        {remove}
      </div>

      {hasError && (
        <p className="col-span-full text-destructive-foreground text-xs">
          <FormMessages errors={[error]} />
        </p>
      )}
    </div>
  );
}
