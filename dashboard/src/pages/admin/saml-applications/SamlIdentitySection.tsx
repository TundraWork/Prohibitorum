import { Label, ListBox, Select } from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { isCancellation } from "@/api/errors";
import { readAttributeMap } from "@/api/federation";
import type { components } from "@/api/generated/schema";
import { updateSamlAppMutationOptions } from "@/api/mutations";
import type { SamlAttributeMapping } from "@/api/raw-admin-paths";
import { samlAppUpdateBody } from "@/api/update-bodies";
import { ConsoleCard } from "@/components/custom/ConsoleCard";
import { RowsField, rowProblem } from "@/components/custom/RowsField";
import { Section } from "@/components/custom/Section";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";
import {
  type MappingRow,
  MappingRowFields,
  mappingSource,
  readSourceKind,
} from "@/pages/admin/saml-applications/MappingRowFields";
import {
  attributeMapProblems,
  defaultAttributeNameFormat,
  nameIdFormats,
  shortNameIdFormat,
} from "@/pages/admin/saml-applications/saml-projection";

type SamlApp = components["schemas"]["SAMLApplicationView"];

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
 * ## Why the checks are here and nowhere else
 *
 * The server stores the map as it receives it and validates none of it, so a
 * mapping naming a claim the instance never emits, or two mappings under one
 * name, is accepted and only shows up as a sign-in that comes back missing an
 * attribute. What is checked — names present and unique, sources from the known
 * set, a key whenever the source is an account attribute — is therefore the
 * only thing standing between a typo and that failure.
 *
 * The write goes through the application's own PUT, which is not sudo-gated: the
 * projection changes no credential. `samlAppUpdateBody` carries every field this
 * form does not edit, because the PUT replaces the record and an omitted
 * `attributeMap` would clear it.
 */
export function SamlIdentitySection({ app }: { app: SamlApp }) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const update = useMutation(updateSamlAppMutationOptions(queryClient));

  const form = useAppForm({
    defaultValues: {
      nameIdFormat: app.nameIdFormat,
      mappings: readAttributeMap(app.attributeMap).map(
        (mapping): MappingRow => {
          const { sourceKind, sourceKey } = readSourceKind(mapping.source);
          return {
            name: mapping.name,
            nameFormat: mapping.name_format,
            friendlyName: mapping.friendly_name ?? "",
            sourceKind,
            sourceKey,
            multi: mapping.multi,
          };
        },
      ),
    },
    onSubmit: async ({ value }) => {
      const [first] = attributeMapProblems(toMappings(value.mappings));
      if (first !== undefined) {
        form.setFieldMeta("mappings", (meta) => ({
          ...meta,
          errors: [
            rowProblem(first.index, {
              line: first.index + 1,
              reason: first.message,
            }),
          ],
        }));
        return;
      }
      form.setFieldMeta("mappings", (meta) => ({
        ...meta,
        errors: undefined,
      }));

      try {
        await update.mutateAsync({
          id: app.id,
          body: samlAppUpdateBody(app, {
            nameIdFormat: value.nameIdFormat,
            attributeMap: toMappings(value.mappings),
          }),
        });
      } catch (error) {
        if (isCancellation(error)) return;
        applyServerError(form, error, { codes: {}, locations: {} });
      }
    },
  });

  return (
    <Section
      title={<Trans id="admin.saml-apps.identity">Identity projection</Trans>}
    >
      <ConsoleCard>
        <form.AppForm>
          <form.Form
            label={t({
              id: "admin.saml-apps.identity",
              message: "Identity projection",
            })}
          >
            <form.FormError />

            <form.AppField name="nameIdFormat">
              {(field) => (
                <div className="flex flex-col gap-1.5">
                  <Select
                    className="w-full"
                    variant="secondary"
                    value={field.state.value}
                    onChange={(key) => {
                      if (typeof key === "string") field.handleChange(key);
                    }}
                  >
                    <Label>
                      <Trans id="admin.saml-apps.field.name-id">
                        NameID format
                      </Trans>
                    </Label>
                    <Select.Trigger>
                      <Select.Value />
                      <Select.Indicator />
                    </Select.Trigger>
                    <Select.Popover>
                      <ListBox>
                        {nameIdFormats.map((format) => (
                          <ListBox.Item
                            key={format}
                            id={format}
                            textValue={shortNameIdFormat(format)}
                          >
                            <span className="font-mono text-sm">
                              {shortNameIdFormat(format)}
                            </span>
                            <ListBox.ItemIndicator />
                          </ListBox.Item>
                        ))}
                        {/* The format is free text on the wire and the server's
                            own default is configurable, so a record may carry a
                            value the four do not cover — including the empty
                            string a record created against the instance default
                            keeps. The stored value is offered as it is rather
                            than silently rewritten to one of them: the option's
                            label says what it is, and choosing anything else
                            replaces it. */}
                        {!nameIdFormats.includes(
                          field.state.value as (typeof nameIdFormats)[number],
                        ) && (
                          <ListBox.Item
                            id={field.state.value}
                            textValue={
                              field.state.value === ""
                                ? t({
                                    id: "admin.saml-apps.name-id.default",
                                    message: "Use the instance default",
                                  })
                                : shortNameIdFormat(field.state.value)
                            }
                          >
                            {field.state.value === "" ? (
                              <Trans id="admin.saml-apps.name-id.default">
                                Use the instance default
                              </Trans>
                            ) : (
                              <span className="font-mono text-sm">
                                {shortNameIdFormat(field.state.value)}
                              </span>
                            )}
                            <ListBox.ItemIndicator />
                          </ListBox.Item>
                        )}
                      </ListBox>
                    </Select.Popover>
                  </Select>
                  <p className="text-xs text-muted">
                    <Trans id="admin.saml-apps.identity.name-id.hint">
                      The identifier the assertion's subject carries. Changing
                      it makes every service provider see a new account.
                    </Trans>
                  </p>
                </div>
              )}
            </form.AppField>

            <form.AppField name="mappings">
              {() => (
                <RowsField<MappingRow>
                  label={
                    <Trans id="admin.saml-apps.mapping.title">
                      Attribute map
                    </Trans>
                  }
                  description={
                    <Trans id="admin.saml-apps.mapping.hint">
                      Publish the account's own facts under the names the
                      service provider expects.
                    </Trans>
                  }
                  emptyRow={() => ({
                    name: "",
                    nameFormat: defaultAttributeNameFormat,
                    friendlyName: "",
                    sourceKind: "username",
                    sourceKey: "",
                    multi: false,
                  })}
                  addLabel={
                    <Trans id="admin.saml-apps.mapping.add">
                      Add an attribute
                    </Trans>
                  }
                  renderRow={(_row, index, error) => (
                    <MappingRowFields index={index} error={error} />
                  )}
                />
              )}
            </form.AppField>

            <form.SubmitButton>
              <Trans id="admin.saml-apps.identity.save">Save</Trans>
            </form.SubmitButton>
          </form.Form>
        </form.AppForm>
      </ConsoleCard>
    </Section>
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
