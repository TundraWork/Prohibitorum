import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { KeyRoundIcon } from "lucide-react";
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
import { ItemList, ItemListRow } from "@/components/custom/ItemList";
import { SecretRevealDialog } from "@/components/custom/SecretReveal";
import { Section } from "@/components/custom/Section";
import { rotatedSecretCopy } from "@/pages/admin/oidc-applications/secret-copy";

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
  const { t } = useLingui();
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
      icon={<KeyRoundIcon />}
      title={<Trans id="admin.oidc-apps.credentials">Credentials</Trans>}
    >
      {isPublic ? (
        <p className="text-muted-foreground text-sm">
          <Trans id="admin.oidc-apps.credentials.public">
            Public clients have no client secret. Each sign-in request is
            verified with PKCE.
          </Trans>
        </p>
      ) : (
        <>
          <ItemList
            label={t({
              id: "admin.oidc-apps.credentials",
              message: "Credentials",
            })}
            empty={null}
          >
            <ItemListRow
              icon={<KeyRoundIcon size={18} aria-hidden="true" />}
              title={
                <Trans id="admin.oidc-apps.credentials.secret">
                  Client secret
                </Trans>
              }
              details={[
                <Trans key="note" id="admin.oidc-apps.rotate.note">
                  Issues a new secret and invalidates the old one. Clients that
                  still use the old secret cannot sign users in until they are
                  updated.
                </Trans>,
              ]}
              stackActions
              actions={
                <Button
                  disabled={rotate.isPending}
                  onClick={() => {
                    failure.clear();
                    setConfirming(true);
                  }}
                  variant="outline"
                >
                  <Trans id="admin.oidc-apps.rotate.action">
                    Rotate secret
                  </Trans>
                </Button>
              }
            />
          </ItemList>
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
              The current secret stops working as soon as you confirm. Clients
              still configured with it cannot sign users in until they get the
              new secret. Decide where to store the new secret before you
              continue.
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
          copy={rotatedSecretCopy}
          onContinue={async () => setSecret(null)}
        />
      )}
    </Section>
  );
}
