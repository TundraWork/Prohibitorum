import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Fingerprint, Trash2 } from "lucide-react";
import { useState } from "react";
import type { components } from "@/api/generated/schema";
import { deleteAccountCredentialMutationOptions } from "@/api/mutations";
import { accountCredentialsQueryOptions } from "@/api/queries";
import { quietQuery, withInlineReport } from "@/app/query-client";
import { Button } from "@/components/custom/Button";
import { ConfirmDialog } from "@/components/custom/ConfirmDialog";
import { useInlineFailure } from "@/components/custom/InlineFailure";
import { ItemListEmpty, ItemListRow } from "@/components/custom/ItemList";
import { RelativeTime } from "@/components/custom/RelativeTime";
import { AccessSection } from "@/pages/admin/users/AccessSection";
import { CredentialIcon } from "@/pages/security/PasskeysPanel";

type Credential = components["schemas"]["CredentialView"];

/**
 * The passkeys an account signs in with, each removable by an administrator.
 * Removing one stops it working at once and asks for no identity check, so the
 * confirmation is the only step; if it was the account's only way in, the
 * account needs a new registration link.
 */
export function AccountPasskeysSection({ accountId }: { accountId: number }) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const credentials = useQuery(
    quietQuery(accountCredentialsQueryOptions(accountId)),
  );
  const remove = useMutation(
    withInlineReport(deleteAccountCredentialMutationOptions(queryClient), true),
  );
  const failure = useInlineFailure();
  const [target, setTarget] = useState<Credential | null>(null);

  return (
    <>
      <AccessSection
        id="passkeys"
        icon={<Fingerprint />}
        title={<Trans id="admin.user.passkeys.title">Passkeys</Trans>}
        description={
          <Trans id="admin.user.passkeys.description">
            The passkeys they sign in with. Removing one stops it working
            straight away.
          </Trans>
        }
        label={t({ id: "admin.user.passkeys.table", message: "Passkeys" })}
        query={credentials}
        writeError={failure.error}
        empty={
          <ItemListEmpty>
            <Trans id="admin.user.passkeys.empty">No passkeys</Trans>
          </ItemListEmpty>
        }
      >
        {(credentials.data?.items ?? []).map((credential) => {
          const unnamed = t({
            id: "admin.user.passkeys.unnamed",
            message: "Unnamed passkey",
          });
          const name = credential.nickname || unnamed;
          const lastUsed = credential.lastUsedAt;
          return (
            <ItemListRow
              key={credential.id}
              icon={<CredentialIcon transports={credential.transports} />}
              title={
                credential.nickname || (
                  <span className="text-muted-foreground">{unnamed}</span>
                )
              }
              details={[
                <Trans key="added" id="admin.user.passkeys.detail.added">
                  Added <RelativeTime value={credential.createdAt} />
                </Trans>,
                lastUsed === undefined ? (
                  <Trans key="used" id="admin.user.passkeys.never_used">
                    Not used yet
                  </Trans>
                ) : (
                  <Trans key="used" id="admin.user.passkeys.detail.lastUsed">
                    Last used <RelativeTime value={lastUsed} />
                  </Trans>
                ),
                <span key="suffix" className="font-mono">
                  …{credential.credentialIdSuffix}
                </span>,
              ]}
              actions={
                <Button
                  aria-label={t({
                    id: "admin.user.passkeys.revoke.named",
                    message: `Remove ${name}`,
                  })}
                  onClick={() => {
                    failure.clear();
                    setTarget(credential);
                  }}
                  variant="destructive-outline"
                  size="icon-sm"
                >
                  <Trash2 aria-hidden="true" />
                </Button>
              }
            />
          );
        })}
      </AccessSection>

      <ConfirmDialog
        isOpen={target !== null}
        onOpenChange={(open) => !open && !remove.isPending && setTarget(null)}
        status="danger"
        title={
          <Trans id="admin.user.passkeys.revoke.title">
            Remove this passkey?
          </Trans>
        }
        subject={
          target !== null &&
          (target.nickname ||
            t({
              id: "admin.user.passkeys.unnamed",
              message: "Unnamed passkey",
            }))
        }
        body={
          <p>
            <Trans id="admin.user.passkeys.revoke.body">
              This passkey stops working immediately. If it is the only way this
              account signs in, they will need a new registration link.
            </Trans>
          </p>
        }
        confirmLabel={
          <Trans id="admin.user.passkeys.revoke.action">Remove passkey</Trans>
        }
        isPending={remove.isPending}
        onConfirm={() => {
          if (target === null) return;
          remove.mutate(
            { accountId, credentialId: target.id },
            {
              onError: failure.report,
              onSettled: () => setTarget(null),
            },
          );
        }}
      />
    </>
  );
}
