import { Trans } from "@lingui/react/macro";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import type { components } from "@/api/generated/schema";
import { rotateOidcSecretMutationOptions } from "@/api/mutations";
import { withInlineReport } from "@/app/query-client";
import { Button } from "@/components/custom/Button";
import { ConfirmDialog } from "@/components/custom/ConfirmDialog";
import {
  InlineFailure,
  useInlineFailure,
} from "@/components/custom/InlineFailure";
import { SecretRevealDialog } from "@/components/custom/SecretReveal";
import { Section } from "@/components/custom/Section";
import { SettingRow } from "@/components/custom/SettingRow";
import { clientSecretCopy } from "@/pages/admin/oidc-applications/secret-copy";

type OidcApp = components["schemas"]["OIDCApplicationView"];

/**
 * What a client proves itself with. A confidential client has a secret, shown
 * only when it is created or rotated; a public client has none, so the section
 * says that instead of offering a rotation that would mint a credential the
 * client never sends.
 *
 * Rotating needs a fresh identity check, then a confirmation: the current
 * secret stops working the moment it goes ahead. The new secret is revealed by a
 * dialog that cannot be dismissed while the section holds it, so it cannot
 * outlive the value. The write is held here, not in the dialog, so the dialog's
 * closing never drops its outcome.
 */
export function CredentialsSection({ app }: { app: OidcApp }) {
  const queryClient = useQueryClient();
  const rotate = useMutation(
    withInlineReport(rotateOidcSecretMutationOptions(queryClient), true),
  );
  const failure = useInlineFailure();
  const [confirming, setConfirming] = useState(false);
  const [secret, setSecret] = useState<string | null>(null);

  // `none` is the one client authentication method that means "no secret at
  // all", which is what makes a client public.
  const isPublic = app.clientAuthMethod === "none";

  return (
    <Section
      id="credentials"
      title={<Trans id="admin.oidc-apps.credentials">Credentials</Trans>}
    >
      {isPublic ? (
        <p className="text-muted-foreground text-sm">
          <Trans id="admin.oidc-apps.credentials.public">
            A public client has no secret. It proves each sign-in request with
            PKCE instead.
          </Trans>
        </p>
      ) : (
        <>
          <SettingRow
            title={
              <Trans id="admin.oidc-apps.credentials.secret">
                Client secret
              </Trans>
            }
            description={
              <Trans id="admin.oidc-apps.rotate.note">
                Issues a new secret and discards the old one. Clients still
                using the old secret stop signing accounts in until they are
                updated.
              </Trans>
            }
          >
            <Button
              pending={rotate.isPending}
              onClick={() => {
                failure.clear();
                setConfirming(true);
              }}
              variant="outline"
            >
              <Trans id="admin.oidc-apps.rotate.action">Rotate secret</Trans>
            </Button>
          </SettingRow>
          <InlineFailure error={failure.error} />
        </>
      )}

      <ConfirmDialog
        isOpen={confirming}
        onOpenChange={(open) => {
          if (!open && !rotate.isPending) setConfirming(false);
        }}
        status="warning"
        title={
          <Trans id="admin.oidc-apps.rotate.title">
            Rotate the client secret?
          </Trans>
        }
        body={
          <p>
            <Trans id="admin.oidc-apps.rotate.body">
              The current secret stops working the moment this is confirmed. Any
              client still configured with it stops signing accounts in until
              you give it the new one, so have somewhere to put the new secret
              before you go ahead.
            </Trans>
          </p>
        }
        confirmLabel={
          <Trans id="admin.oidc-apps.rotate.confirm">Rotate secret</Trans>
        }
        isPending={rotate.isPending}
        onConfirm={() => {
          // The new secret is revealed by the dialog below, so the confirmation
          // does not close until it has arrived — a failure leaves the reader
          // where the explanation is, once the write has settled.
          rotate.mutate(app.clientId, {
            onSuccess: (result) => {
              setConfirming(false);
              setSecret(result.secret);
            },
            onError: (error) => {
              setConfirming(false);
              failure.report(error);
            },
          });
        }}
      />

      {secret !== null && (
        // Open and non-dismissable while the secret is held here, so it cannot
        // outlive the value: the reveal's own continue control is the only way
        // out, and it stays on the page the secret belongs to.
        <SecretRevealDialog
          text={secret}
          filename={`${app.clientId}-client-secret.txt`}
          copy={clientSecretCopy}
          onContinue={async () => setSecret(null)}
        />
      )}
    </Section>
  );
}
