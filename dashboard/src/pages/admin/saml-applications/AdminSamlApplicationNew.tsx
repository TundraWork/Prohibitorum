import { Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import {
  AppWindowIcon,
  ClipboardListIcon,
  FileKeyIcon,
  LogInIcon,
  ShieldCheckIcon,
} from "lucide-react";
import { useState } from "react";
import { isCancellation } from "@/api/errors";
import { createSamlAppMutationOptions } from "@/api/mutations";
import type { CreateSamlAppRequest } from "@/api/raw-admin-paths";
import { withInlineReport } from "@/app/query-client";
import { CancelLink } from "@/components/custom/CancelLink";
import { ChoiceTile } from "@/components/custom/ChoiceTile";
import { useConsoleCrumb } from "@/components/custom/console-crumb";
import { PageBody } from "@/components/custom/PageBody";
import { PageHeader } from "@/components/custom/PageHeader";
import { RowsField, rowProblem } from "@/components/custom/RowsField";
import { Section } from "@/components/custom/Section";
import { RadioGroup } from "@/components/ui/radio-group";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";
import { AcsRowFields } from "@/pages/admin/saml-applications/AcsRowFields";
import { NameIdFormatField } from "@/pages/admin/saml-applications/NameIdFormatField";
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
 * Both are one form, not two: the switches apply either way and only the fields
 * of the service provider's section change. The choice is therefore a described
 * choice at the top of the form, like the client type on the OIDC page.
 */
type Mode = "metadata" | "manual";

/**
 * Registering a SAML service provider with the instance.
 *
 * Only what the server cannot work out is asked for. The attribute map, the
 * session lifetime and the access policy's groups all have a working default and
 * are set on the application's page, where the record as it stands sits beside
 * the field — a new application has no record yet, so there is nothing to
 * compare it against.
 *
 * Creating an application establishes no credential for anyone and grants no
 * access, which is why the server asks for no fresh verification here and this
 * form makes no step-up call. A failed create is shown on the form or the field
 * it belongs to, and the mutation marks itself inline so the failure is not also
 * a toast.
 */
export function AdminSamlApplicationNew() {
  const { t } = useLingui();
  useConsoleCrumb(
    t({ id: "admin.saml-apps.new.title", message: "New SAML application" }),
  );
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const create = useMutation(
    withInlineReport(createSamlAppMutationOptions(queryClient), true),
  );
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
            errorMap: { ...meta.errorMap, onSubmit: [metadataRequired] },
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
            errorMap: {
              ...meta.errorMap,
              onSubmit: [
                rowProblem(rowsProblem.row, {
                  line: rowsProblem.row + 1,
                  reason: rowsProblem.message,
                }),
              ],
            },
          }));
          return;
        }
        form.setFieldMeta("acs", (meta) => ({
          ...meta,
          errorMap: { ...meta.errorMap, onSubmit: undefined },
        }));
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
            errorMap: { ...meta.errorMap, onSubmit: [metadataTooLarge] },
          }));
        } else {
          // The manual path has no metadata box, so the budget cannot be blamed
          // on a field the reader cannot see: the whole request is too large,
          // and that is a form-level statement.
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
  const submitting = useStore(form.store, (state) => state.isSubmitting);

  return (
    <PageBody>
      <PageHeader
        title={
          <Trans id="admin.saml-apps.new.title">New SAML application</Trans>
        }
        description={
          <Trans id="admin.saml-apps.new.description">
            Register a service provider that signs people in through this
            instance. The rest of its settings are on its page once it exists.
          </Trans>
        }
      />
      <form.AppForm>
        <form.Form
          label={t({
            id: "admin.saml-apps.new.form",
            message: "New SAML application",
          })}
          className="flex flex-col gap-6"
        >
          <form.FormError />

          <Section
            id="application"
            icon={<AppWindowIcon />}
            title={
              <Trans id="admin.apps.new.application.title">Application</Trans>
            }
            description={
              <Trans id="admin.saml-apps.new.identity.description">
                The name people see for the application.
              </Trans>
            }
          >
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
                />
              )}
            </form.AppField>
          </Section>

          <Section
            id="registration"
            icon={<ClipboardListIcon />}
            title={
              <Trans id="admin.saml-apps.new.registration.title">
                Registration
              </Trans>
            }
            description={
              <Trans id="admin.saml-apps.new.registration.description">
                A service provider usually publishes a metadata document with
                everything the instance needs.
              </Trans>
            }
          >
            {/* The reader's choice, not the form's: the way of registering is
                what the next section reads, so it stays in component state. */}
            <RadioGroup
              aria-label={t({
                id: "admin.saml-apps.new.mode",
                message: "How the application is registered",
              })}
              value={mode}
              disabled={submitting}
              onValueChange={(next) => setMode(next as Mode)}
            >
              <ChoiceTile
                layout="list"
                value="metadata"
                label={
                  <Trans id="admin.saml-apps.new.mode.metadata">
                    Import metadata
                  </Trans>
                }
                description={
                  <Trans id="admin.saml-apps.new.mode.metadata.description">
                    Paste the document the service provider publishes. The
                    Entity ID, the endpoints and the certificates come from it.
                  </Trans>
                }
              />
              <ChoiceTile
                layout="list"
                value="manual"
                label={
                  <Trans id="admin.saml-apps.new.mode.manual">
                    Enter it by hand
                  </Trans>
                }
                description={
                  <Trans id="admin.saml-apps.new.mode.manual.description">
                    For a provider that publishes no document you can reach. You
                    give the Entity ID and the endpoints yourself.
                  </Trans>
                }
              />
            </RadioGroup>
          </Section>

          <Section
            id="service-provider"
            icon={<FileKeyIcon />}
            title={
              <Trans id="admin.saml-apps.new.provider.title">
                Service provider
              </Trans>
            }
            description={
              mode === "metadata" ? (
                <Trans id="admin.saml-apps.new.provider.metadata.description">
                  The document the service provider publishes.
                </Trans>
              ) : (
                <Trans id="admin.saml-apps.new.provider.manual.description">
                  What the instance cannot work out without a document.
                </Trans>
              )
            }
          >
            {/* Each way keeps its own value while the other is shown: the form
                holds the values, so returning to a way finds what was left. */}
            {mode === "metadata" ? (
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
                        The document the service provider publishes for its
                        metadata endpoint.
                      </Trans>
                    }
                    isMonospace
                    rows={10}
                    spellCheck={false}
                  />
                )}
              </form.AppField>
            ) : (
              <div className="flex flex-col gap-6">
                <form.AppField
                  name="entityId"
                  validators={{
                    onSubmit: ({ value }) =>
                      value.trim() === "" ? entityIdRequired : undefined,
                  }}
                >
                  {(field) => (
                    <field.FormField
                      label={
                        <Trans id="admin.saml-apps.field.entity-id">
                          Entity ID
                        </Trans>
                      }
                      description={
                        <Trans id="admin.saml-apps.field.entity-id.hint">
                          The identifier the service provider publishes. It
                          cannot be changed later.
                        </Trans>
                      }
                      autoComplete="off"
                      spellCheck={false}
                      isMonospace
                      compactValue
                    />
                  )}
                </form.AppField>

                <form.AppField name="nameIdFormat">
                  {() => <NameIdFormatField offerDefault />}
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
                          Where the instance sends the assertion. Exactly one
                          endpoint is the default one.
                        </Trans>
                      }
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
              </div>
            )}
          </Section>

          <Section
            id="sign-in"
            icon={<LogInIcon />}
            title={<Trans id="admin.saml-apps.sign-in.title">Sign-in</Trans>}
            description={
              <Trans id="admin.saml-apps.new.sign-in.description">
                What the service provider has to send, and where people may
                start signing in.
              </Trans>
            }
          >
            <div className="flex flex-col gap-5">
              <form.AppField name="requireSignedAuthnRequest">
                {(field) => (
                  <field.SwitchField
                    label={
                      <Trans id="admin.saml-apps.field.signed-request">
                        Require a signed AuthnRequest
                      </Trans>
                    }
                    description={
                      <Trans id="admin.saml-apps.general.signed-request.hint">
                        Refuse a sign-in request the service provider has not
                        signed. Good practice for every provider that can sign
                        one.
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
                        Let people start at this instance instead of at the
                        service provider.
                      </Trans>
                    }
                  />
                )}
              </form.AppField>
            </div>
          </Section>

          <Section
            id="access"
            icon={<ShieldCheckIcon />}
            title={<Trans id="app.access.title">Access</Trans>}
            description={
              <Trans id="admin.oidc-apps.new.access.description">
                Open it to every account, or limit it to user groups. The groups
                are chosen on the application's page.
              </Trans>
            }
          >
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
          </Section>

          <div className="flex flex-wrap items-center gap-2">
            <form.SubmitButton>
              <Trans id="admin.saml-apps.new.submit">Create application</Trans>
            </form.SubmitButton>
            <CancelLink to="/admin/saml-applications" pending={submitting} />
          </div>
        </form.Form>
      </form.AppForm>
    </PageBody>
  );
}
