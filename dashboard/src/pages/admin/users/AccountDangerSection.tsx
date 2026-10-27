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
  const [leaving, setLeaving] = useState(false);

  const busy = setDisabled.isPending || remove.isPending || leaving;
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
                This account is disabled and cannot sign in. Once enabled, it
                can sign in with its existing sign-in methods.
              </Trans>
            ) : (
              <Trans key="note" id="admin.user.state.enabled.note.short">
                A disabled account cannot sign in, and all its sessions end. It
                can be enabled again at any time.
              </Trans>
            ),
          ]}
          stackActions
          actions={
            <Button
              aria-describedby={blocked ? reasonId : undefined}
              disabled={blocked || busy}
              onClick={() => {
                failure.clear();
                setConfirming(account.disabled ? "enable" : "disable");
              }}
              variant="outline"
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
              className="flex items-center gap-1.5 text-pretty text-warning-foreground text-caption"
            >
              <TriangleAlertIcon
                aria-hidden="true"
                className="size-3.5 shrink-0"
              />
              <Trans id="admin.user.state.admin-blocked">
                Admins cannot be disabled. Change the role to User first.
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
              Deletes the account with its passkeys, sessions, access tokens and
              linked identities. This cannot be undone.
            </Trans>,
          ]}
          stackActions
          actions={
            <Button
              disabled={isSelf || busy}
              aria-describedby={isSelf ? selfReasonId : undefined}
              onClick={() => {
                failure.clear();
                setConfirming("delete");
              }}
              variant="destructive-outline"
            >
              <Trans id="admin.user.delete.button">Delete</Trans>
            </Button>
          }
        >
          {isSelf && (
            <p
              id={selfReasonId}
              className="flex items-center gap-1.5 text-pretty text-warning-foreground text-caption"
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
        subject={account.displayName || account.username}
        body={
          <p>
            {confirming === "enable" ? (
              <Trans id="admin.user.state.enable.body">
                The account can sign in again immediately.
              </Trans>
            ) : (
              <Trans id="admin.user.state.disable.body">
                The account is signed out of all sessions and cannot sign in
                until it is enabled again.
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
              The account and its passkeys, sessions, access tokens and linked
              identities are deleted immediately. This cannot be undone.
            </Trans>
          </p>
        }
        confirmLabel={
          <Trans id="admin.user.delete.confirm.action">Delete account</Trans>
        }
        isPending={remove.isPending || leaving}
        onConfirm={() => {
          remove.mutate(account.id, {
            // The dialog stays pending until the list has replaced this page.
            onSuccess: async () => {
              setLeaving(true);
              try {
                await navigate({ to: "/admin/users" });
              } finally {
                setLeaving(false);
                setConfirming(null);
              }
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
