import { Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { FingerprintIcon } from "lucide-react";
import { useRef, useState } from "react";
import { isCancellation } from "@/api/errors";
import { readAttributeMap } from "@/api/federation";
import type { components } from "@/api/generated/schema";
import { updateSamlAppMutationOptions } from "@/api/mutations";
import type {
  SamlAttributeMapping,
  UpdateSamlAppRequest,
} from "@/api/raw-admin-paths";
import { samlAppUpdateBody } from "@/api/update-bodies";
import { withInlineReport } from "@/app/query-client";
import { ConfirmDialog } from "@/components/custom/ConfirmDialog";
import { ConsequenceFooter } from "@/components/custom/ConsequenceFooter";
import { FormMessages } from "@/components/custom/FormMessages";
import { useHostOverlayContainer } from "@/components/custom/host-overlay-container";
import { rowProblem } from "@/components/custom/RowsField";
import { Section } from "@/components/custom/Section";
import { useFieldBinding } from "@/components/custom/use-field-binding";
import {
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
} from "@/components/ui/field";
import {
  Select,
  SelectItem,
  SelectPopup,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";
import {
  AttributeMapField,
  type MappingRow,
  mappingSource,
  readSourceKind,
} from "@/pages/admin/saml-applications/AttributeMapField";
import {
  attributeMapProblems,
  nameIdFormats,
  shortNameIdFormat,
} from "@/pages/admin/saml-applications/saml-projection";

type SamlApp = components["schemas"]["SAMLApplicationView"];

/** The form's values for a stored record, also the baseline after a save. */
function identityValues(app: SamlApp) {
  return {
    nameIdFormat: app.nameIdFormat,
    mappings: readAttributeMap(app.attributeMap).map((mapping): MappingRow => {
      const { sourceKind, sourceKey } = readSourceKind(mapping.source);
      return {
        name: mapping.name,
        nameFormat: mapping.name_format,
        friendlyName: mapping.friendly_name ?? "",
        sourceKind,
        sourceKey,
        multi: mapping.multi,
      };
    }),
  };
}

/**
 * What the instance tells the service provider about the account it just signed
 * in, and how the assertion identifies them.
 *
 * Two things are decided here. The NameID format picks what goes in the
 * assertion's `Subject` — the pair (issuer, subject) is how the service provider
 * recognises the same person on the next sign-in, so a format that changes what
 * it means makes the provider treat the account as a stranger. And the attribute
 * map publishes the account's own facts under the provider's names, because a
 * provider expecting `USERNAME` will not find it in an assertion that only has
 * `username`.
 *
 * The NameID format is confirmed when the form is saved, not when it is picked.
 * The select is free to move — a reader comparing the options is not committing
 * to one — and the footer states the consequence while the choice differs from
 * the saved one, so it is announced before the press. Submitting then opens the
 * dialog with the checked request held beside the form; the write happens from
 * the dialog, and cancelling leaves the reader's choice in the form, unsent.
 *
 * The server stores the attribute map as it receives it and validates none of
 * it, so a mapping naming two attributes alike, or reading a source the
 * instance never emits, is accepted and only shows up as a sign-in that comes
 * back missing an attribute. The checks here — names present and unique,
 * sources from the known set, a key whenever the source is an account attribute
 * — are the only thing standing between a typo and that failure.
 *
 * The write goes through the application's own PUT, which is not sudo-gated: the
 * projection changes no credential. `samlAppUpdateBody` carries every field this
 * form does not edit, because the PUT replaces the record and an omitted
 * `attributeMap` would clear it.
 */
export function SamlIdentitySection({ app }: { app: SamlApp }) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const update = useMutation(
    withInlineReport(updateSamlAppMutationOptions(queryClient), true),
  );
  // The request the dialog is confirming, checked and ready to send.
  const [pendingBody, setPendingBody] = useState<UpdateSamlAppRequest | null>(
    null,
  );

  const form = useAppForm({
    defaultValues: identityValues(app),
    onSubmit: async ({ value }) => {
      const [first] = attributeMapProblems(toMappings(value.mappings));
      if (first !== undefined) {
        form.setFieldMeta("mappings", (meta) => ({
          ...meta,
          errorMap: {
            ...meta.errorMap,
            onSubmit: [
              rowProblem(
                first.index,
                { line: first.index + 1, reason: first.message },
                // Carries which input the submit found fault with, so the row
                // marks that one rather than every box it draws.
                first.field,
              ),
            ],
          },
        }));
        return;
      }

      const body = samlAppUpdateBody(app, {
        nameIdFormat: value.nameIdFormat,
        attributeMap: toMappings(value.mappings),
      });
      if (value.nameIdFormat !== app.nameIdFormat) {
        setPendingBody(body);
        return;
      }
      await save(body);
    },
  });

  async function save(body: UpdateSamlAppRequest) {
    try {
      const saved = await update.mutateAsync({ id: app.id, body });
      // The stored record is the new baseline, so the section reads as saved.
      form.reset(identityValues(saved));
    } catch (error) {
      if (isCancellation(error)) return;
      applyServerError(form, error, { codes: {}, locations: {} });
    }
  }

  const dirty = useStore(form.store, (state) => !state.isDefaultValue);
  const submitting = useStore(form.store, (state) => state.isSubmitting);
  const formatChanged = useStore(
    form.store,
    (state) => state.values.nameIdFormat !== app.nameIdFormat,
  );

  const title = t({
    id: "admin.saml-apps.identity.title",
    message: "Identity",
  });

  return (
    <>
      <form.AppForm>
        <form.Form label={title}>
          <Section
            id="identity"
            icon={<FingerprintIcon />}
            title={title}
            description={
              <Trans id="admin.saml-apps.identity.description">
                What the service provider is told about someone who signs in.
              </Trans>
            }
            dirty={dirty}
            footer={
              dirty ? (
                <ConsequenceFooter
                  consequence={
                    formatChanged ? (
                      <Trans id="admin.saml-apps.name-id.consequence">
                        Changing the NameID format makes every service provider
                        see a new account.
                      </Trans>
                    ) : undefined
                  }
                  actionLabel={
                    formatChanged ? (
                      <Trans id="admin.saml-apps.name-id.action-label">
                        Change NameID and save
                      </Trans>
                    ) : (
                      <Trans id="admin.saml-apps.save">Save</Trans>
                    )
                  }
                  onDiscard={() => form.reset()}
                  pending={submitting || update.isPending}
                />
              ) : undefined
            }
          >
            <div className="flex flex-col gap-6">
              <form.FormError />

              <form.AppField name="nameIdFormat">
                {() => <NameIdFormatField />}
              </form.AppField>

              <form.AppField name="mappings">
                {() => <AttributeMapField />}
              </form.AppField>
            </div>
          </Section>
        </form.Form>
      </form.AppForm>

      <ConfirmDialog
        isOpen={pendingBody !== null}
        onOpenChange={(open) => {
          if (!open && !update.isPending) setPendingBody(null);
        }}
        status="warning"
        title={
          <Trans id="admin.saml-apps.name-id.confirm.title">
            Change the NameID format?
          </Trans>
        }
        body={
          <p>
            <Trans id="admin.saml-apps.name-id.confirm.body">
              Every service provider that has signed this account in before
              stores the identifier it was given. Changing the format makes each
              of them see a new account, so what it keeps under the old one no
              longer applies.
            </Trans>
          </p>
        }
        confirmLabel={
          <Trans id="admin.saml-apps.name-id.confirm.save">
            Change and save
          </Trans>
        }
        isPending={update.isPending}
        onConfirm={() => {
          if (pendingBody === null) return;
          // Closed once the write settles either way: a refusal is reported on
          // the form, where the reader can act on it.
          void save(pendingBody).finally(() => setPendingBody(null));
        }}
      />
    </>
  );
}

/**
 * The NameID format, as a field of `nameIdFormat`.
 *
 * The four formats the specification names are offered by their last segment,
 * with the whole URN under it, because two versions of the specification share
 * a segment. The format is free text on the wire and the server's own default
 * is configurable, so a record may carry a value the four do not cover —
 * including the empty string a record created against the instance default
 * keeps. The stored value is offered as it is rather than silently rewritten to
 * one of them, and choosing anything else replaces it.
 */
function NameIdFormatField() {
  const { t } = useLingui();
  const { field, errors, invalid, disabled, onChange } =
    useFieldBinding<string>(false);
  const anchor = useRef<HTMLDivElement>(null);
  const container = useHostOverlayContainer(anchor);

  const value = field.state.value;
  const isListed = (nameIdFormats as readonly string[]).includes(value);
  const formats = isListed ? nameIdFormats : [...nameIdFormats, value];
  const defaultLabel = t({
    id: "admin.saml-apps.name-id.default",
    message: "Use the instance default",
  });
  const label = (format: string) =>
    format === "" ? defaultLabel : shortNameIdFormat(format);

  return (
    <Field
      ref={anchor}
      className="max-w-lg"
      disabled={disabled}
      invalid={invalid}
      name={field.name}
    >
      <FieldLabel>
        <Trans id="admin.saml-apps.field.name-id">NameID format</Trans>
      </FieldLabel>
      <Select
        value={value}
        disabled={disabled}
        onValueChange={(next) => {
          if (typeof next === "string") onChange(next);
        }}
      >
        <SelectTrigger aria-invalid={invalid || undefined}>
          <SelectValue>
            {() => (
              <span className={value === "" ? undefined : "font-mono"}>
                {label(value)}
              </span>
            )}
          </SelectValue>
        </SelectTrigger>
        <SelectPopup alignItemWithTrigger={false} portalProps={{ container }}>
          {formats.map((format) => (
            <SelectItem key={format} value={format}>
              <div className="flex min-w-0 flex-col">
                <span className={format === "" ? undefined : "font-mono"}>
                  {label(format)}
                </span>
                {format !== "" && (
                  <span className="font-mono text-muted-foreground text-xs">
                    {format}
                  </span>
                )}
              </div>
            </SelectItem>
          ))}
        </SelectPopup>
      </Select>
      <FieldDescription>
        <Trans id="admin.saml-apps.identity.name-id.hint">
          The identifier the assertion's subject carries.
        </Trans>
      </FieldDescription>
      {invalid && (
        <FieldError match>
          <FormMessages errors={errors} />
        </FieldError>
      )}
    </Field>
  );
}

/**
 * The rows as the wire takes them.
 *
 * A blank friendly name is omitted rather than sent empty: the server reports it
 * back only when it is non-empty, so sending `""` would make the round trip
 * disagree with itself and the field would come back looking untouched but new.
 */
function toMappings(rows: readonly MappingRow[]): SamlAttributeMapping[] {
  return rows.map((row) => ({
    name: row.name.trim(),
    name_format: row.nameFormat.trim(),
    ...(row.friendlyName.trim() === ""
      ? {}
      : { friendly_name: row.friendlyName.trim() }),
    source: mappingSource(row),
    multi: row.multi,
  }));
}
