import { Chip, Modal } from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { isCancellation } from "@/api/errors";
import type { components } from "@/api/generated/schema";
import { reingestSamlMetadataMutationOptions } from "@/api/mutations";
import { Button } from "@/components/custom/Button";
import { ItemList, ItemListRow } from "@/components/custom/ItemList";
import { Section } from "@/components/custom/Section";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";
import {
  bindingPost,
  bindingRedirect,
  metadataRequired,
} from "@/pages/admin/saml-applications/saml-validation";

type SamlApp = components["schemas"]["SAMLApplicationView"];

/**
 * What the instance learned from the service provider's metadata document.
 *
 * Both lists are read-only on purpose. The ACS endpoints and the signing
 * certificates are not the instance's own choices — they are what the provider
 * published — and the way to change them is to re-read the document, which is
 * what the button under them does. Editing an endpoint by hand would let the
 * console hold a copy that no longer matches the provider, and the failure would
 * surface as a sign-in that posts an assertion nowhere.
 *
 * Re-importing replaces both lists and keeps the Entity ID and the name, which
 * is why it needs no confirmation: nothing about the application's identity
 * changes, and the only loss is a stale endpoint nobody could have used.
 */
export function SamlMetadataSection({ app }: { app: SamlApp }) {
  const { t, i18n } = useLingui();
  const queryClient = useQueryClient();
  const reingest = useMutation(
    reingestSamlMetadataMutationOptions(queryClient),
  );
  const [importing, setImporting] = useState(false);

  const form = useAppForm({
    defaultValues: { metadataXml: "" },
    onSubmit: async ({ value }) => {
      const document = value.metadataXml.trim();
      if (document === "") {
        form.setFieldMeta("metadataXml", (meta) => ({
          ...meta,
          errors: [metadataRequired],
        }));
        return;
      }
      try {
        await reingest.mutateAsync({ id: app.id, metadataXml: document });
        form.reset();
        setImporting(false);
      } catch (error) {
        if (isCancellation(error)) return;
        applyServerError(form, error, { codes: {}, locations: {} });
      }
    },
  });

  return (
    <Section
      title={<Trans id="admin.saml-apps.metadata">Metadata</Trans>}
      action={
        <Button
          variant="secondary"
          onPress={() => {
            form.reset();
            setImporting(true);
          }}
        >
          <Trans id="admin.saml-apps.metadata.reingest">
            Re-import metadata
          </Trans>
        </Button>
      }
    >
      <div className="flex flex-col gap-3">
        <ItemList
          label={t({
            id: "admin.saml-apps.acs.title",
            message: "Assertion Consumer Service endpoints",
          })}
          title={
            <Trans id="admin.saml-apps.acs.title">
              Assertion Consumer Service endpoints
            </Trans>
          }
          empty={
            <p className="px-4 py-6 text-center text-sm text-muted">
              <Trans id="admin.saml-apps.acs.empty">
                This application has no endpoints.
              </Trans>
            </p>
          }
        >
          {(app.acs ?? []).map((endpoint) => (
            <ItemListRow
              key={`${endpoint.binding}:${endpoint.index}`}
              // The index is the endpoint's own identity in the service
              // provider's metadata, so it names the row. Being the default is a
              // fact about it rather than a different endpoint, so it rides
              // beside the name instead of replacing it.
              title={
                <Trans
                  id="admin.saml-apps.acs.endpoint"
                  comment="The endpoint's index in the service provider's own list"
                >
                  Endpoint {endpoint.index}
                </Trans>
              }
              badges={
                endpoint.isDefault ? (
                  <Chip size="sm" variant="soft">
                    <Trans id="admin.saml-apps.acs.default-endpoint">
                      Default
                    </Trans>
                  </Chip>
                ) : undefined
              }
              details={[
                bindingName(endpoint.binding),
                <span key="location" className="break-all font-mono">
                  {endpoint.location}
                </span>,
              ]}
            />
          ))}
        </ItemList>

        <ItemList
          label={t({
            id: "admin.saml-apps.keys.title",
            message: "Signing certificates",
          })}
          title={
            <Trans id="admin.saml-apps.keys.title">Signing certificates</Trans>
          }
          empty={
            <p className="px-4 py-6 text-center text-sm text-muted">
              <Trans id="admin.saml-apps.keys.empty">
                No certificates are published for this application.
              </Trans>
            </p>
          }
        >
          {(app.keys ?? []).map((key, index) => (
            <ItemListRow
              // A certificate has no identifier of its own in this view — the
              // server returns the use and the expiry, not the thumbprint — so
              // the position is what distinguishes two of them.
              // biome-ignore lint/suspicious/noArrayIndexKey: see above
              key={index}
              title={<Trans id="admin.saml-apps.keys.use">Signing</Trans>}
              details={[
                key.notAfter === undefined ? (
                  <Trans id="admin.saml-apps.keys.no-expiry" key="expiry">
                    No expiry
                  </Trans>
                ) : (
                  <Trans
                    key="expiry"
                    id="admin.saml-apps.keys.expires"
                    comment="A date the administrator plans around, so it is a date and not a relative time"
                  >
                    Expires {formatExpiry(key.notAfter, i18n.locale)}
                  </Trans>
                ),
              ]}
            />
          ))}
        </ItemList>
      </div>

      <Modal isOpen={importing} onOpenChange={setImporting}>
        <Modal.Backdrop>
          <Modal.Container placement="center" size="lg" scroll="inside">
            <Modal.Dialog>
              <Modal.Header>
                <Modal.Heading>
                  <Trans id="admin.saml-apps.metadata.dialog.title">
                    Re-import metadata
                  </Trans>
                </Modal.Heading>
              </Modal.Header>
              <Modal.Body>
                <form.AppForm>
                  <form.Form
                    label={t({
                      id: "admin.saml-apps.metadata.dialog.title",
                      message: "Re-import metadata",
                    })}
                    className="flex flex-col gap-4"
                  >
                    <form.FormError />
                    <form.AppField name="metadataXml">
                      {(field) => (
                        <field.TextAreaField
                          label={
                            <Trans id="admin.saml-apps.metadata.dialog.field">
                              Metadata XML
                            </Trans>
                          }
                          description={
                            <Trans id="admin.saml-apps.metadata.dialog.hint">
                              Replaces the endpoints and certificates below. The
                              Entity ID and the name stay as they are.
                            </Trans>
                          }
                          className="font-mono text-xs"
                          rows={10}
                          spellCheck={false}
                          variant="secondary"
                        />
                      )}
                    </form.AppField>
                    <div className="flex flex-wrap justify-end gap-2">
                      <Button
                        variant="tertiary"
                        isDisabled={reingest.isPending}
                        onPress={() => setImporting(false)}
                      >
                        <Trans id="admin.cancel">Cancel</Trans>
                      </Button>
                      <form.SubmitButton>
                        <Trans id="admin.saml-apps.metadata.dialog.submit">
                          Re-import
                        </Trans>
                      </form.SubmitButton>
                    </div>
                  </form.Form>
                </form.AppForm>
              </Modal.Body>
            </Modal.Dialog>
          </Modal.Container>
        </Modal.Backdrop>
      </Modal>
    </Section>
  );
}

/**
 * A binding URN as the word an administrator uses for it.
 *
 * A metadata document carries the whole URN, and that is what the server stores
 * and sends back; the list shows the short name because the URN is the
 * specification's spelling of the same fact. An unrecognised binding is shown
 * whole rather than guessed at — the server does not restrict the field either.
 */
function bindingName(binding: string): string {
  if (binding === bindingPost) return "POST";
  if (binding === bindingRedirect) return "Redirect";
  return binding;
}

/**
 * A certificate's expiry as a date.
 *
 * A date rather than a `RelativeTime`: this is something the administrator plans
 * around — collecting a fresh document before the certificate lapses — and "in 3
 * months" is not when anyone schedules that work.
 */
function formatExpiry(value: string, locale: string): string {
  return new Intl.DateTimeFormat(locale, { dateStyle: "medium" }).format(
    new Date(value),
  );
}
