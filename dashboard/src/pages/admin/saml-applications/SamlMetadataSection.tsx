import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { FileKeyIcon, GlobeIcon, RefreshCwIcon } from "lucide-react";
import { useState } from "react";
import { isCancellation } from "@/api/errors";
import type { components } from "@/api/generated/schema";
import { reingestSamlMetadataMutationOptions } from "@/api/mutations";
import { withInlineReport } from "@/app/query-client";
import { Button } from "@/components/custom/Button";
import { ConfirmDialog } from "@/components/custom/ConfirmDialog";
import { ItemList, ItemListRow } from "@/components/custom/ItemList";
import { Section } from "@/components/custom/Section";
import { getTopLayerContainer } from "@/components/custom/top-layer";
import { Badge } from "@/components/ui/badge";
import {
  Dialog,
  DialogFooter,
  DialogHeader,
  DialogPanel,
  DialogPopup,
  DialogTitle,
} from "@/components/ui/dialog";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";
import {
  bindingPost,
  bindingRedirect,
  metadataRequired,
} from "@/pages/admin/saml-applications/saml-validation";

type SamlApp = components["schemas"]["SAMLApplicationView"];
type ReingestMetadata = ReturnType<typeof useReingestMetadata>;

function useReingestMetadata() {
  const queryClient = useQueryClient();
  return useMutation(
    withInlineReport(reingestSamlMetadataMutationOptions(queryClient), true),
  );
}

/**
 * What the instance learned from the service provider's metadata document.
 *
 * Both kinds of row are read-only on purpose. The ACS endpoints and the signing
 * certificates are not the instance's own choices — they are what the provider
 * published — and the way to change them is to re-read the document, which is
 * what the button in the header does. Editing an endpoint by hand would let the
 * console hold a copy that no longer matches the provider, and the failure would
 * surface as a sign-in that posts an assertion nowhere.
 *
 * Re-importing replaces both lists and keeps the Entity ID and the name. The
 * replacement is confirmed before it is sent, with the document still in the
 * dialog behind: a stale endpoint is not worth keeping, but a wrong document
 * would leave every sign-in with nowhere to post to.
 */
export function SamlMetadataSection({ app }: { app: SamlApp }) {
  const { t } = useLingui();
  const reingest = useReingestMetadata();
  const [importing, setImporting] = useState(false);

  const endpoints = app.acs ?? [];
  const certificates = app.keys ?? [];

  return (
    <Section
      id="metadata"
      icon={<FileKeyIcon />}
      title={t({ id: "admin.saml-apps.metadata", message: "Metadata" })}
      description={
        <Trans id="admin.saml-apps.metadata.description">
          What the service provider published: where assertions are posted and
          which certificates sign its requests.
        </Trans>
      }
      action={
        <Button variant="outline" onClick={() => setImporting(true)}>
          <RefreshCwIcon aria-hidden="true" />
          <Trans id="admin.saml-apps.metadata.reingest">
            Re-import metadata
          </Trans>
        </Button>
      }
    >
      <ItemList
        label={t({
          id: "admin.saml-apps.metadata.list",
          message: "Endpoints and certificates",
        })}
        empty={null}
      >
        {endpoints.map((endpoint) => (
          <ItemListRow
            key={`acs:${endpoint.binding}:${endpoint.index}`}
            icon={<GlobeIcon aria-hidden="true" size={18} />}
            // The index is the endpoint's own identity in the service
            // provider's metadata, so it names the row. Being the default is a
            // fact about it rather than a different endpoint, so it rides beside
            // the name instead of replacing it.
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
                <Badge variant="outline">
                  <Trans id="admin.saml-apps.acs.default-endpoint">
                    Default
                  </Trans>
                </Badge>
              ) : undefined
            }
            details={[
              bindingName(endpoint.binding),
              <span key="location" className="font-mono">
                {endpoint.location}
              </span>,
            ]}
          />
        ))}
        {endpoints.length === 0 && (
          <ItemListRow
            key="acs:none"
            icon={<GlobeIcon aria-hidden="true" size={18} />}
            dimmed
            title={
              <Trans id="admin.saml-apps.acs.empty">
                This application has no endpoints.
              </Trans>
            }
          />
        )}
        {certificates.map((key, index) => (
          <ItemListRow
            // A certificate has no identifier of its own in this view — the
            // server returns the use and the expiry, not the thumbprint — so
            // the position is what distinguishes two of them.
            // biome-ignore lint/suspicious/noArrayIndexKey: see above
            key={`key:${index}`}
            icon={<FileKeyIcon aria-hidden="true" size={18} />}
            title={
              key.use === "signing" ? (
                <Trans id="admin.saml-apps.keys.signing">
                  Signing certificate
                </Trans>
              ) : (
                <Trans id="admin.saml-apps.keys.encryption">
                  Encryption certificate
                </Trans>
              )
            }
            details={[
              <CertificateExpiry key="expiry" notAfter={key.notAfter} />,
            ]}
          />
        ))}
        {certificates.length === 0 && (
          <ItemListRow
            key="key:none"
            icon={<FileKeyIcon aria-hidden="true" size={18} />}
            dimmed
            title={
              <Trans id="admin.saml-apps.keys.empty">
                No certificates are published for this application.
              </Trans>
            }
          />
        )}
      </ItemList>

      <ReimportDialog
        open={importing}
        app={app}
        reingest={reingest}
        onClose={() => setImporting(false)}
      />
    </Section>
  );
}

/** A certificate's expiry as a date, in the danger colour once it has passed. */
function CertificateExpiry({ notAfter }: { notAfter: string | undefined }) {
  const { i18n } = useLingui();
  if (notAfter === undefined) {
    return <Trans id="admin.saml-apps.keys.no-expiry">No expiry</Trans>;
  }
  // A date rather than a relative time: this is something the administrator
  // plans around — collecting a fresh document before the certificate lapses —
  // and "in 3 months" is not when anyone schedules that work.
  const date = new Intl.DateTimeFormat(i18n.locale, {
    dateStyle: "medium",
  }).format(new Date(notAfter));
  return new Date(notAfter).getTime() < Date.now() ? (
    <span className="text-destructive-foreground">
      <Trans id="admin.saml-apps.keys.expired">Expired {date}</Trans>
    </span>
  ) : (
    <Trans
      id="admin.saml-apps.keys.expires"
      comment="A date the administrator plans around, so it is a date and not a relative time"
    >
      Expires {date}
    </Trans>
  );
}

/**
 * The dialog that takes a new metadata document.
 *
 * The write belongs to the section; the dialog stays up while it is in flight,
 * so a refusal lands on the form the document is in. Submitting the form does
 * not send: it asks first, and only the confirmation does.
 */
function ReimportDialog({
  open,
  app,
  reingest,
  onClose,
}: {
  open: boolean;
  app: SamlApp;
  reingest: ReingestMetadata;
  onClose: () => void;
}) {
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => !next && !reingest.isPending && onClose()}
    >
      <DialogPopup
        className="sm:max-w-2xl"
        portalProps={{ container: getTopLayerContainer() }}
      >
        <ReimportForm app={app} reingest={reingest} onClose={onClose} />
      </DialogPopup>
    </Dialog>
  );
}

function ReimportForm({
  app,
  reingest,
  onClose,
}: {
  app: SamlApp;
  reingest: ReingestMetadata;
  onClose: () => void;
}) {
  const { t } = useLingui();
  // The document the confirmation is about, checked and ready to send.
  const [pending, setPending] = useState<string | null>(null);

  const form = useAppForm({
    defaultValues: { metadataXml: "" },
    onSubmit: ({ value }) => {
      const document = value.metadataXml.trim();
      if (document === "") {
        form.setFieldMeta("metadataXml", (meta) => ({
          ...meta,
          errorMap: { ...meta.errorMap, onSubmit: [metadataRequired] },
        }));
        return;
      }
      setPending(document);
    },
  });

  async function send(document: string) {
    try {
      await reingest.mutateAsync({ id: app.id, metadataXml: document });
      onClose();
    } catch (error) {
      if (isCancellation(error)) return;
      applyServerError(form, error, { codes: {}, locations: {} });
    }
  }

  return (
    <form.AppForm>
      <DialogHeader>
        <DialogTitle>
          <Trans id="admin.saml-apps.metadata.dialog.title">
            Re-import metadata
          </Trans>
        </DialogTitle>
      </DialogHeader>
      <form.Form
        label={t({
          id: "admin.saml-apps.metadata.dialog.title",
          message: "Re-import metadata",
        })}
        className="flex min-h-0 flex-1 flex-col"
      >
        <DialogPanel className="flex flex-col gap-4">
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
                    Replaces the endpoints and certificates below. The Entity ID
                    and the name stay as they are.
                  </Trans>
                }
                rows={10}
                isMonospace
                spellCheck={false}
              />
            )}
          </form.AppField>
        </DialogPanel>
        <DialogFooter>
          <Button
            variant="ghost"
            disabled={reingest.isPending}
            onClick={onClose}
          >
            <Trans id="admin.cancel">Cancel</Trans>
          </Button>
          <form.SubmitButton fullWidth>
            <Trans id="admin.saml-apps.metadata.dialog.submit">Re-import</Trans>
          </form.SubmitButton>
        </DialogFooter>
      </form.Form>

      <ConfirmDialog
        isOpen={pending !== null}
        onOpenChange={(next) => {
          if (!next && !reingest.isPending) setPending(null);
        }}
        status="warning"
        title={
          <Trans id="admin.saml-apps.metadata.confirm.title">
            Replace the endpoints and certificates?
          </Trans>
        }
        body={
          <p>
            <Trans id="admin.saml-apps.metadata.confirm.body">
              This document replaces every endpoint and certificate the
              application has now. If it is the wrong document, people cannot
              sign in until the right one is imported.
            </Trans>
          </p>
        }
        confirmLabel={
          <Trans id="admin.saml-apps.metadata.confirm.action">
            Replace and re-import
          </Trans>
        }
        isPending={reingest.isPending}
        onConfirm={() => {
          if (pending === null) return;
          // Closed once the write settles either way: a refusal is reported on
          // the form, where the reader can act on it.
          void send(pending).finally(() => setPending(null));
        }}
      />
    </form.AppForm>
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
