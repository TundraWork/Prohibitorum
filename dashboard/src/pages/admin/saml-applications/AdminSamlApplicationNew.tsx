import { Label, ListBox, Select, Tabs } from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { isCancellation } from "@/api/errors";
import { createSamlAppMutationOptions } from "@/api/mutations";
import type { CreateSamlAppRequest } from "@/api/raw-admin-paths";
import { ConsoleCard } from "@/components/custom/ConsoleCard";
import { RowsField, rowProblem } from "@/components/custom/RowsField";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";
import { AcsRowFields } from "@/pages/admin/saml-applications/AcsRowFields";
import { nameIdFormats } from "@/pages/admin/saml-applications/saml-projection";
import {
  type AcsRow,
  acsListProblem,
  bindingPost,
  displayNameRequired,
  entityIdRequired,
  maxBodyBytes,
  metadataRequired,
  metadataTooLarge,
  toAcsRequest,
} from "@/pages/admin/saml-applications/saml-validation";

/**
 * The two ways a SAML application can be registered, and what each asks for.
 *
 * Importing metadata is the common case: a service provider publishes its Entity
 * ID, its Assertion Consumer Service endpoints and its signing certificates in
 * one document, and everything an administrator would otherwise type is in it.
 * Manual entry is for a provider that publishes nothing the console can reach —
 * an internal tool, or a document on a host with no route to it — and then the
 * three facts the server cannot infer have to be given.
 *
 * Both are one form, not two: the switches below the fields apply either way and
 * only the fields between the strip and them change. The choice is therefore a
 * strip over one form rather than two pages — and it is the one place on the
 * console where tabs are right, because what they select is a set of inputs
 * inside a single submission rather than a second view of a record.
 */
type Mode = "metadata" | "manual";

/**
 * Registering a SAML service provider with the instance.
 *
 * Only what the server cannot work out is asked for. The attribute map, the
 * session lifetime and the access policy all have a working default and are set
 * on the detail page, where the record as it stands sits beside the field — a new
 * application has no record yet, so there is nothing to compare it against.
 *
 * Creating an application establishes no credential for anyone and grants no
 * access, which is why the server asks for no fresh verification here and this
 * form makes no step-up call.
 */
export function AdminSamlApplicationNew() {
  const { t } = useLingui();
  const navigate = useNavigate();
  const create = useMutation(createSamlAppMutationOptions());
  const [mode, setMode] = useState<Mode>("metadata");

  const form = useAppForm({
    defaultValues: {
      displayName: "",
      metadataXml: "",
      entityId: "",
      // An empty string is the instance's own default format rather than a
      // missing value: the server reads it that way, and the option says so.
      nameIdFormat: "",
      acs: [
        { binding: bindingPost, location: "", index: "0", isDefault: true },
      ] as AcsRow[],
      requireSignedAuthnRequest: false,
      allowIdpInitiated: false,
      accessRestricted: false,
    },
    onSubmit: async ({ value }) => {
      // The column is `NOT NULL` and the name is what every list and heading
      // shows, so an unnamed application would be a row identified only by a
      // long Entity ID. The form asks for one rather than letting the server
      // store an empty string.
      const name = value.displayName.trim();
      let body: CreateSamlAppRequest;

      if (mode === "metadata") {
        const document = value.metadataXml.trim();
        if (document === "") {
          form.setFieldMeta("metadataXml", (meta) => ({
            ...meta,
            errors: [metadataRequired],
          }));
          return;
        }
        body = {
          displayName: name,
          metadataXml: document,
          requireSignedAuthnRequest: value.requireSignedAuthnRequest,
          allowIdpInitiated: value.allowIdpInitiated,
          accessRestricted: value.accessRestricted,
        };
      } else {
        // The rows are checked at submit rather than on each keystroke: a
        // half-typed address is wrong on every character of the way in, and
        // saying so while it is still being typed is noise rather than help.
        const rowsProblem = acsListProblem(value.acs);
        if (rowsProblem !== undefined) {
          form.setFieldMeta("acs", (meta) => ({
            ...meta,
            errors: [
              rowProblem(rowsProblem.row, {
                line: rowsProblem.row + 1,
                reason: rowsProblem.message,
              }),
            ],
          }));
          return;
        }
        form.setFieldMeta("acs", (meta) => ({ ...meta, errors: undefined }));
        body = {
          displayName: name,
          entityId: value.entityId.trim(),
          nameIdFormat: value.nameIdFormat,
          acs: toAcsRequest(value.acs),
          requireSignedAuthnRequest: value.requireSignedAuthnRequest,
          allowIdpInitiated: value.allowIdpInitiated,
          accessRestricted: value.accessRestricted,
        };
      }

      // Measured on the encoded body rather than on the text as typed: the limit
      // is on the bytes the server receives, and non-ASCII in a metadata document
      // costs more there than the character count suggests.
      if (
        new TextEncoder().encode(JSON.stringify(body)).length > maxBodyBytes
      ) {
        if (mode === "metadata") {
          form.setFieldMeta("metadataXml", (meta) => ({
            ...meta,
            errors: [metadataTooLarge],
          }));
        } else {
          // The manual path hides the metadata box, so the budget cannot be
          // blamed on a field the reader cannot see: the whole request is too
          // large, and that is a form-level statement.
          form.setErrorMap({
            onSubmit: { form: metadataTooLarge, fields: {} },
          });
        }
        return;
      }

      try {
        const created = await create.mutateAsync(body);
        await navigate({
          to: "/admin/saml-applications/$id",
          params: { id: String(created.id) },
        });
      } catch (error) {
        if (isCancellation(error)) return;
        applyServerError(form, error, {
          codes: {
            saml_application_already_exists: "entityId",
            // Every other rejection — a document the server cannot parse, an
            // Entity ID it will not take — comes back as a bare bad request with
            // no field, so each path names the input it can act on.
            bad_request: mode === "metadata" ? "metadataXml" : "entityId",
          },
          locations: {},
        });
      }
    },
  });

  return (
    <ConsoleCard>
      <form.AppForm>
        <form.Form
          label={t({
            id: "admin.saml-apps.new.form",
            message: "New SAML application",
          })}
        >
          <form.FormError />

          <Tabs
            className="w-full"
            variant="secondary"
            selectedKey={mode}
            onSelectionChange={(key) => setMode(key as Mode)}
          >
            <Tabs.ListContainer className="w-fit max-w-full">
              <Tabs.List
                aria-label={t({
                  id: "admin.saml-apps.new.mode",
                  message: "How the application is registered",
                })}
              >
                <Tabs.Tab className="whitespace-nowrap" id="metadata">
                  <Trans id="admin.saml-apps.new.mode.metadata">
                    Import metadata
                  </Trans>
                  <Tabs.Indicator />
                </Tabs.Tab>
                <Tabs.Tab className="whitespace-nowrap" id="manual">
                  <Trans id="admin.saml-apps.new.mode.manual">
                    Enter it by hand
                  </Trans>
                  <Tabs.Indicator />
                </Tabs.Tab>
              </Tabs.List>
            </Tabs.ListContainer>
          </Tabs>

          <form.AppField
            name="displayName"
            validators={{
              onSubmit: ({ value }) =>
                value.trim() === "" ? displayNameRequired : undefined,
            }}
          >
            {(field) => (
              <field.FormField
                label={<Trans id="admin.saml-apps.field.name">Name</Trans>}
                variant="secondary"
              />
            )}
          </form.AppField>

          <form.AppField name="metadataXml">
            {(field) => (
              <field.TextAreaField
                label={
                  <Trans id="admin.saml-apps.field.metadata">
                    Metadata XML
                  </Trans>
                }
                description={
                  <Trans id="admin.saml-apps.field.metadata.hint">
                    The document the service provider publishes for its metadata
                    endpoint.
                  </Trans>
                }
                // Both paths keep their own value while the other is shown, and
                // the hidden one is disabled rather than unmounted, so returning
                // to it finds what was left there.
                isDisabled={mode !== "metadata"}
                className="font-mono text-xs"
                rows={10}
                spellCheck={false}
                variant="secondary"
              />
            )}
          </form.AppField>

          <form.AppField
            name="entityId"
            validators={{
              onSubmit: ({ value }) =>
                mode === "manual" && value.trim() === ""
                  ? entityIdRequired
                  : undefined,
            }}
          >
            {(field) => (
              <field.FormField
                label={
                  <Trans id="admin.saml-apps.field.entity-id">Entity ID</Trans>
                }
                description={
                  <Trans id="admin.saml-apps.field.entity-id.hint">
                    The identifier the service provider publishes. It cannot be
                    changed later.
                  </Trans>
                }
                isDisabled={mode !== "manual"}
                autoComplete="off"
                spellCheck={false}
                variant="secondary"
              />
            )}
          </form.AppField>

          <form.AppField name="nameIdFormat">
            {(field) => (
              <div className="flex flex-col gap-1.5">
                <Select
                  className="w-full"
                  variant="secondary"
                  isDisabled={mode !== "manual"}
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
                      <ListBox.Item
                        id=""
                        textValue={t({
                          id: "admin.saml-apps.name-id.default",
                          message: "Use the instance default",
                        })}
                      >
                        <Trans id="admin.saml-apps.name-id.default">
                          Use the instance default
                        </Trans>
                        <ListBox.ItemIndicator />
                      </ListBox.Item>
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
                    </ListBox>
                  </Select.Popover>
                </Select>
                <p className="text-xs text-muted">
                  <Trans id="admin.saml-apps.field.name-id.hint">
                    The format of the identifier the instance sends as the
                    subject of an assertion.
                  </Trans>
                </p>
              </div>
            )}
          </form.AppField>

          <form.AppField name="acs">
            {(field) => (
              <RowsField<AcsRow>
                label={
                  <Trans id="admin.saml-apps.field.acs">
                    Assertion Consumer Service endpoints
                  </Trans>
                }
                description={
                  <Trans id="admin.saml-apps.field.acs.hint">
                    Where the instance sends the assertion. Exactly one endpoint
                    is the default one.
                  </Trans>
                }
                isDisabled={mode !== "manual"}
                emptyRow={() => ({
                  binding: bindingPost,
                  location: "",
                  index: String(field.state.value.length),
                  isDefault: false,
                })}
                addLabel={
                  <Trans id="admin.saml-apps.acs.add">Add endpoint</Trans>
                }
                renderRow={(_row, index, error) => (
                  <AcsRowFields index={index} error={error} />
                )}
              />
            )}
          </form.AppField>

          <form.AppField name="requireSignedAuthnRequest">
            {(field) => (
              <field.SwitchField
                label={
                  <Trans id="admin.saml-apps.field.signed-request">
                    Require a signed AuthnRequest
                  </Trans>
                }
                description={
                  <Trans id="admin.saml-apps.field.signed-request.hint">
                    Refuse a sign-in request the service provider has not
                    signed. Turn this off only if it cannot sign one.
                  </Trans>
                }
              />
            )}
          </form.AppField>

          <form.AppField name="allowIdpInitiated">
            {(field) => (
              <field.SwitchField
                label={
                  <Trans id="admin.saml-apps.field.idp-initiated">
                    Allow IdP-initiated sign-in
                  </Trans>
                }
                description={
                  <Trans id="admin.saml-apps.field.idp-initiated.hint">
                    Let people start at this instance instead of at the service
                    provider.
                  </Trans>
                }
              />
            )}
          </form.AppField>

          <form.AppField name="accessRestricted">
            {(field) => (
              <field.SwitchField
                label={
                  <Trans id="admin.saml-apps.field.restricted">
                    Restrict access
                  </Trans>
                }
                description={
                  <Trans id="admin.saml-apps.field.restricted.hint">
                    Nobody can use the application until you select a user
                    group.
                  </Trans>
                }
              />
            )}
          </form.AppField>

          <form.SubmitButton>
            <Trans id="admin.saml-apps.new.submit">Create application</Trans>
          </form.SubmitButton>
        </form.Form>
      </form.AppForm>
    </ConsoleCard>
  );
}

/**
 * The last segment of a NameID format URN: `persistent` rather than
 * `urn:oasis:names:tc:SAML:2.0:nameid-format:persistent`.
 *
 * The whole URN is what gets stored and sent, so only the reading is shortened.
 */
export function shortNameIdFormat(format: string): string {
  const separator = format.lastIndexOf(":");
  const short = separator === -1 ? format : format.slice(separator + 1);
  return short === "" ? format : short;
}
