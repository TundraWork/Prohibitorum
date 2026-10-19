import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { KeyRoundIcon } from "lucide-react";
import { type ReactNode, useState } from "react";
import { isCancellation } from "@/api/errors";
import type { components } from "@/api/generated/schema";
import { setIdentityProviderSecretMutationOptions } from "@/api/mutations";
import { withInlineReport } from "@/app/query-client";
import { Button } from "@/components/custom/Button";
import { Section } from "@/components/custom/Section";
import { SettingRow } from "@/components/custom/SettingRow";
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

type Provider = components["schemas"]["IdentityProviderView"];

const secretRequired = msg({
  id: "admin.federation.secret.required",
  message: "Enter the secret.",
});

/**
 * A Steam provider's credentials, as a section of its own, mirroring the
 * OIDC application's: the section is "Credentials" and its one row is the Web
 * API key, so no level repeats the name of the one above it. An OIDC client's
 * secret is set inside its connection section, beside the method that sends it,
 * and VRChat's credential is the operator session, so every other provider
 * draws nothing here.
 */
export function ProviderCredentialsSection({
  provider,
}: {
  provider: Provider;
}) {
  if (provider.protocol !== "steam") return null;
  return (
    <Section
      id="credentials"
      icon={<KeyRoundIcon />}
      title={
        <Trans id="admin.federation.credentials.steam.title">Credentials</Trans>
      }
      description={
        <Trans id="admin.federation.credentials.steam.description">
          The key stays encrypted and is never shown again.
        </Trans>
      }
    >
      <ProviderSecretRow provider={provider} />
    </Section>
  );
}

/**
 * One write-only credential: its name, whether one is stored, and the button
 * that opens the dialog to set or replace it.
 *
 * Setting a secret changes no behaviour a reader could lose, so it asks for no
 * confirmation. The server never returns a stored secret, so replacing means
 * typing the new value rather than editing the old one. The write is held here,
 * not in the dialog, so closing the dialog never drops its outcome.
 */
export function ProviderSecretRow({ provider }: { provider: Provider }) {
  const queryClient = useQueryClient();
  const setSecret = useMutation(
    withInlineReport(
      setIdentityProviderSecretMutationOptions(queryClient),
      true,
    ),
  );
  const [open, setOpen] = useState(false);

  const steam = provider.protocol === "steam";
  const stored = provider.secretConfigured;

  return (
    <>
      <SettingRow
        title={
          steam ? (
            <Trans id="admin.federation.credentials.steam.label">
              Web API key
            </Trans>
          ) : (
            <Trans id="admin.federation.credentials.oidc.label">
              Client secret
            </Trans>
          )
        }
        description={
          steam ? (
            <Trans id="admin.federation.credentials.steam.note">
              How this instance reads the Steam profile of someone who signs in.
            </Trans>
          ) : (
            <Trans id="admin.federation.credentials.oidc.note">
              The secret this instance signs in to the provider with.
            </Trans>
          )
        }
      >
        <div className="flex items-center gap-3">
          <Badge variant={stored ? "success" : "outline"}>
            {stored ? (
              <Trans id="admin.federation.credentials.stored">Stored</Trans>
            ) : (
              <Trans id="admin.federation.credentials.not-set">Not set</Trans>
            )}
          </Badge>
          <Button onClick={() => setOpen(true)} variant="outline">
            {stored ? (
              <Trans id="admin.federation.credentials.replace">Replace</Trans>
            ) : (
              <Trans id="admin.federation.credentials.set">Set</Trans>
            )}
          </Button>
        </div>
      </SettingRow>

      <Dialog
        open={open}
        onOpenChange={(next) => !next && !setSecret.isPending && setOpen(false)}
      >
        <DialogPopup
          className="sm:max-w-md"
          portalProps={{ container: getTopLayerContainer() }}
        >
          <SecretForm
            title={secretDialogTitle(steam, stored)}
            label={
              steam ? (
                <Trans id="admin.federation.credentials.steam.field">Key</Trans>
              ) : (
                <Trans id="admin.federation.new.secret">Client secret</Trans>
              )
            }
            pending={setSecret.isPending}
            onCancel={() => setOpen(false)}
            onSave={async (secret) => {
              await setSecret.mutateAsync({ slug: provider.slug, secret });
              setOpen(false);
            }}
          />
        </DialogPopup>
      </Dialog>
    </>
  );
}

function secretDialogTitle(steam: boolean, stored: boolean): ReactNode {
  if (steam) {
    return stored ? (
      <Trans id="admin.federation.credentials.steam.replace-title">
        Replace the Web API key
      </Trans>
    ) : (
      <Trans id="admin.federation.credentials.steam.set-title">
        Set the Web API key
      </Trans>
    );
  }
  return stored ? (
    <Trans id="admin.federation.credentials.oidc.replace-title">
      Replace the client secret
    </Trans>
  ) : (
    <Trans id="admin.federation.credentials.oidc.set-title">
      Set the client secret
    </Trans>
  );
}

/**
 * The dialog's form. It mounts with the popup, so a closed dialog's typed value
 * is gone and the next one starts empty. A refusal lands in the form and the
 * dialog stays open.
 */
function SecretForm({
  title,
  label,
  pending,
  onCancel,
  onSave,
}: {
  title: ReactNode;
  label: ReactNode;
  pending: boolean;
  onCancel: () => void;
  onSave: (secret: string) => Promise<void>;
}) {
  const { t } = useLingui();
  const form = useAppForm({
    defaultValues: { secret: "" },
    onSubmit: async ({ value }) => {
      try {
        await onSave(value.secret);
      } catch (error) {
        if (isCancellation(error)) return;
        applyServerError(form, error, {
          codes: { bad_request: "secret" },
          locations: {},
        });
      }
    },
  });

  return (
    <form.AppForm>
      <DialogHeader>
        <DialogTitle>{title}</DialogTitle>
      </DialogHeader>
      <form.Form
        label={t({
          id: "admin.federation.credentials.form",
          message: "Secret",
        })}
        className="flex min-h-0 flex-1 flex-col"
      >
        <DialogPanel className="flex flex-col gap-4">
          <form.FormError />
          <form.AppField
            name="secret"
            validators={{
              onSubmit: ({ value }) =>
                value.trim() === "" ? secretRequired : undefined,
            }}
          >
            {(field) => (
              <field.FormField
                label={label}
                description={
                  <Trans id="admin.federation.credentials.hint">
                    Stored encrypted and never shown again.
                  </Trans>
                }
                type="password"
                autoComplete="new-password"
                autoFocus
              />
            )}
          </form.AppField>
        </DialogPanel>
        <DialogFooter>
          <Button variant="ghost" disabled={pending} onClick={onCancel}>
            <Trans id="admin.cancel">Cancel</Trans>
          </Button>
          <form.SubmitButton fullWidth>
            <Trans id="admin.federation.credentials.save">Save</Trans>
          </form.SubmitButton>
        </DialogFooter>
      </form.Form>
    </form.AppForm>
  );
}
