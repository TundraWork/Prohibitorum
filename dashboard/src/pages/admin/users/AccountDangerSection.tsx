import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Power, Trash2, TriangleAlertIcon } from "lucide-react";
import { useId, useState } from "react";
import type { components } from "@/api/generated/schema";
import {
  deleteAccountMutationOptions,
  setAccountDisabledMutationOptions,
} from "@/api/mutations";
import { withInlineReport } from "@/app/query-client";
import { Button } from "@/components/custom/Button";
import { ConfirmDialog } from "@/components/custom/ConfirmDialog";
import { DangerZoneFrame } from "@/components/custom/DangerZone";
import {
  InlineFailure,
  useInlineFailure,
} from "@/components/custom/InlineFailure";
import { ItemList, ItemListRow } from "@/components/custom/ItemList";

type Account = components["schemas"]["AccountView"];

/**
 * The two actions that stop an account, one row each and both visible at once:
 * keep the account but refuse its sign-ins, or remove it for good.
 *
 * They sit together because a reader who arrives here is deciding between them
 * and should see that disabling is available, and reversible, before reaching
 * for deletion. Both are confirmed. Disabling signs the account out of every
 * session it has open, so its row and dialog say so. An admin cannot be
 * disabled, and nobody can delete their own account, so those buttons are held
 * with the reason beside them; the server enforces the same rule and its refusal, for
 * a case the page could not foresee, is shown once under the rows. Deleting asks
 * for a fresh identity check after the confirmation, then leaves for the list.
 */
export function AccountDangerSection({
  account,
  isSelf = false,
}: {
  account: Account;
  /** Whether this is the account the reader is signed in as. */
  isSelf?: boolean;
}) {
  const { t } = useLingui();
  const reasonId = useId();
  const selfReasonId = useId();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const setDisabled = useMutation(
    withInlineReport(setAccountDisabledMutationOptions(queryClient), true),
  );
  const remove = useMutation(
    withInlineReport(deleteAccountMutationOptions(queryClient), true),
  );
  const failure = useInlineFailure();
  const [confirming, setConfirming] = useState<
    "disable" | "enable" | "delete" | null
  >(null);

  const busy = setDisabled.isPending || remove.isPending;
  const blocked = !account.disabled && account.role === "admin";

  return (
    <DangerZoneFrame
      id="danger"
      icon={<TriangleAlertIcon />}
      title={<Trans id="admin.user.danger.heading">Disable or delete</Trans>}
    >
      <ItemList
        label={t({ id: "admin.user.danger.title", message: "Danger zone" })}
        empty={null}
      >
        <ItemListRow
          icon={<Power size={18} aria-hidden="true" />}
          title={
            account.disabled ? (
              <Trans id="admin.user.state.enable">Enable this account</Trans>
            ) : (
              <Trans id="admin.user.state.disable">Disable this account</Trans>
            )
          }
          details={[
            account.disabled ? (
              <Trans key="note" id="admin.user.state.disabled.note">
                This account cannot sign in. Enabling it restores access with
                the sign-in methods it already had.
              </Trans>
            ) : (
              <Trans key="note" id="admin.user.state.enabled.note.short">
                Disabling stops this account signing in and signs it out of
                every session it has open.
              </Trans>
            ),
          ]}
          stackActions
          actions={
            <Button
              disabled={blocked}
              aria-describedby={blocked ? reasonId : undefined}
              pending={setDisabled.isPending}
              onClick={() => {
                failure.clear();
                setConfirming(account.disabled ? "enable" : "disable");
              }}
              variant="outline"
              size="sm"
            >
              {account.disabled ? (
                <Trans id="admin.user.state.enable.action">Enable</Trans>
              ) : (
                <Trans id="admin.user.state.disable.action">Disable</Trans>
              )}
            </Button>
          }
        >
          {blocked && (
            <p
              id={reasonId}
              className="flex items-center gap-1.5 text-pretty text-warning-foreground text-xs"
            >
              <TriangleAlertIcon
                aria-hidden="true"
                className="size-3.5 shrink-0"
              />
              <Trans id="admin.user.state.admin-blocked">
                An admin cannot be disabled. Make them a user first.
              </Trans>
            </p>
          )}
        </ItemListRow>

        <ItemListRow
          icon={<Trash2 size={18} aria-hidden="true" />}
          title={
            <Trans id="admin.user.delete.title">Delete this account</Trans>
          }
          details={[
            <Trans key="note" id="admin.user.delete.note">
              Removes the account and everything it owns: passkeys, sessions,
              access tokens and connected identities. This cannot be undone.
            </Trans>,
          ]}
          stackActions
          actions={
            <Button
              disabled={isSelf}
              aria-describedby={isSelf ? selfReasonId : undefined}
              pending={remove.isPending}
              onClick={() => {
                failure.clear();
                setConfirming("delete");
              }}
              variant="destructive-outline"
              size="sm"
            >
              <Trans id="admin.user.delete.button">Delete</Trans>
            </Button>
          }
        >
          {isSelf && (
            <p
              id={selfReasonId}
              className="flex items-center gap-1.5 text-pretty text-warning-foreground text-xs"
            >
              <TriangleAlertIcon
                aria-hidden="true"
                className="size-3.5 shrink-0"
              />
              <Trans id="admin.user.delete.self">
                You can't delete your own account.
              </Trans>
            </p>
          )}
        </ItemListRow>
      </ItemList>

      <InlineFailure error={failure.error} />

      <ConfirmDialog
        isOpen={confirming === "disable" || confirming === "enable"}
        onOpenChange={(open) => !open && !busy && setConfirming(null)}
        status="warning"
        title={
          confirming === "enable" ? (
            <Trans id="admin.user.state.enable.title">
              Enable this account?
            </Trans>
          ) : (
            <Trans id="admin.user.state.disable.title">
              Disable this account?
            </Trans>
          )
        }
        body={
          <p>
            {confirming === "enable" ? (
              <Trans id="admin.user.state.enable.body">
                They will be able to sign in again straight away.
              </Trans>
            ) : (
              <Trans id="admin.user.state.disable.body">
                They will be signed out of every session they have open and will
                not be able to sign in until you enable the account again.
              </Trans>
            )}
          </p>
        }
        confirmLabel={
          confirming === "enable" ? (
            <Trans id="admin.user.state.enable.action">Enable</Trans>
          ) : (
            <Trans id="admin.user.state.disable.action">Disable</Trans>
          )
        }
        isPending={setDisabled.isPending}
        onConfirm={() => {
          setDisabled.mutate(
            { id: account.id, disabled: confirming === "disable" },
            {
              onError: failure.report,
              onSettled: () => setConfirming(null),
            },
          );
        }}
      />

      <ConfirmDialog
        isOpen={confirming === "delete"}
        onOpenChange={(open) => !open && !busy && setConfirming(null)}
        status="danger"
        title={
          <Trans id="admin.user.delete.confirm.title">
            Delete this account?
          </Trans>
        }
        subject={account.displayName || account.username}
        body={
          <p>
            <Trans id="admin.user.delete.confirm.body">
              The account, its passkeys, its sessions, its access tokens and its
              connected identities are removed immediately. This cannot be
              undone.
            </Trans>
          </p>
        }
        confirmLabel={
          <Trans id="admin.user.delete.confirm.action">Delete account</Trans>
        }
        isPending={remove.isPending}
        onConfirm={() => {
          remove.mutate(account.id, {
            onSuccess: async () => {
              setConfirming(null);
              await navigate({ to: "/admin/users" });
            },
            onError: (error) => {
              setConfirming(null);
              failure.report(error);
            },
          });
        }}
      />
    </DangerZoneFrame>
  );
}
