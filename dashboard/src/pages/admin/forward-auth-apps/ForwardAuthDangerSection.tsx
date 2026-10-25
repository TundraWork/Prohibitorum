import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Power, Trash2, TriangleAlertIcon } from "lucide-react";
import { useState } from "react";
import type { components } from "@/api/generated/schema";
import {
  deleteForwardAuthAppMutationOptions,
  setAppDisabledMutationOptions,
} from "@/api/mutations";
import { appAccessQueryOptions } from "@/api/queries";
import { quietQuery, withInlineReport } from "@/app/query-client";
import { Button } from "@/components/custom/Button";
import { ConfirmDialog } from "@/components/custom/ConfirmDialog";
import { DangerZoneFrame } from "@/components/custom/DangerZone";
import {
  InlineFailure,
  useInlineFailure,
} from "@/components/custom/InlineFailure";
import { ItemList, ItemListRow } from "@/components/custom/ItemList";

type ForwardAuthApp = components["schemas"]["ForwardAuthAppView"];

/**
 * The two actions that change whether the application works at all, one row
 * each and both visible at once: stop accepting requests for now, or remove the
 * application for good.
 *
 * They sit together because a reader who arrives here is deciding between them,
 * and needs to see that disabling is available — and reversible — before
 * reaching for deletion. It is the same section, in the same words, as on the
 * other kinds of application.
 *
 * Every action is confirmed, including the reversible one. Disabling does not
 * revoke what a person already holds — a browser session on the protected host
 * keeps its cookie until it expires, and it is the next verification against a
 * disabled application that refuses it — so the dialog says requests stop being
 * accepted rather than promising everyone is signed out. Deletion names the
 * audience instead of the record, because the record is a row the reader has
 * already seen; what they cannot see from here is who is on the other side of
 * it. A dialog stays open while its write is in flight, and a refusal is shown
 * once, under the rows. Deleting asks for a fresh identity check; disabling does
 * not.
 */
export function ForwardAuthDangerSection({ app }: { app: ForwardAuthApp }) {
  const { t } = useLingui();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const setDisabled = useMutation(
    withInlineReport(
      setAppDisabledMutationOptions(queryClient, "forward_auth"),
      true,
    ),
  );
  const remove = useMutation(
    withInlineReport(deleteForwardAuthAppMutationOptions(queryClient), true),
  );
  const failure = useInlineFailure();

  const [confirming, setConfirming] = useState<
    "disable" | "enable" | "delete" | null
  >(null);

  // The access read already knows which user groups the application is limited
  // to, and the delete dialog is where that list means something. A read that
  // has not landed, or has failed, only costs the dialog its count, so it
  // reports nothing of its own.
  const access = useQuery(
    quietQuery(appAccessQueryOptions("forward_auth", app.clientId)),
  );
  const count = access.data?.groups.length ?? 0;
  // Named, so the confirmation carries it as `{name}`, the placeholder both
  // catalogs use.
  const name = app.displayName || app.clientId;
  const busy = setDisabled.isPending || remove.isPending;

  return (
    <DangerZoneFrame
      id="danger"
      icon={<TriangleAlertIcon />}
      title={
        <Trans id="admin.forward-auth-apps.danger.title">
          Disable or delete
        </Trans>
      }
    >
      <ItemList
        label={t({
          id: "admin.forward-auth-apps.danger",
          message: "Danger zone",
        })}
        empty={null}
      >
        <ItemListRow
          icon={<Power size={18} aria-hidden="true" />}
          title={
            app.disabled ? (
              <Trans id="admin.forward-auth-apps.enable">
                Enable this application
              </Trans>
            ) : (
              <Trans id="admin.forward-auth-apps.disable">
                Disable this application
              </Trans>
            )
          }
          details={[
            app.disabled ? (
              <Trans key="note" id="admin.forward-auth-apps.enable.note">
                Requests through its router are accepted again under its
                existing access policy.
              </Trans>
            ) : (
              <Trans key="note" id="admin.forward-auth-apps.disable.note">
                Every request through its router is refused, including requests
                from signed-in users. Its record and access policy are kept.
              </Trans>
            ),
          ]}
          stackActions
          actions={
            <Button
              pending={setDisabled.isPending}
              onClick={() => {
                failure.clear();
                setConfirming(app.disabled ? "enable" : "disable");
              }}
              variant="outline"
            >
              {app.disabled ? (
                <Trans id="admin.forward-auth-apps.enable.action">Enable</Trans>
              ) : (
                <Trans id="admin.forward-auth-apps.disable.action">
                  Disable
                </Trans>
              )}
            </Button>
          }
        />

        <ItemListRow
          icon={<Trash2 size={18} aria-hidden="true" />}
          title={
            <Trans id="admin.forward-auth-apps.delete">
              Delete this application
            </Trans>
          }
          details={[
            <Trans key="note" id="admin.forward-auth-apps.delete.note">
              Removes the application, its icon and its access policy. Accounts
              are not affected.
            </Trans>,
          ]}
          stackActions
          actions={
            <Button
              pending={remove.isPending}
              onClick={() => {
                failure.clear();
                setConfirming("delete");
              }}
              variant="destructive-outline"
            >
              <Trans id="admin.forward-auth-apps.delete.action">Delete</Trans>
            </Button>
          }
        />
      </ItemList>

      <InlineFailure error={failure.error} />

      <ConfirmDialog
        isOpen={confirming === "disable" || confirming === "enable"}
        onOpenChange={(open) => !open && !busy && setConfirming(null)}
        status="warning"
        title={
          confirming === "enable" ? (
            <Trans id="admin.forward-auth-apps.enable.confirm.title">
              Enable this application?
            </Trans>
          ) : (
            <Trans id="admin.forward-auth-apps.disable.confirm.title">
              Disable this application?
            </Trans>
          )
        }
        subject={name}
        body={
          <p>
            {confirming === "enable" ? (
              <Trans id="admin.forward-auth-apps.enable.confirm.body">
                The gateway accepts requests to this application again under its
                existing access policy.
              </Trans>
            ) : (
              <Trans id="admin.forward-auth-apps.disable.confirm.body">
                The gateway refuses every request to this application, including
                requests from signed-in users. Nothing is deleted, and enabling
                it again restores access.
              </Trans>
            )}
          </p>
        }
        confirmLabel={
          confirming === "enable" ? (
            <Trans id="admin.forward-auth-apps.enable.action">Enable</Trans>
          ) : (
            <Trans id="admin.forward-auth-apps.disable.action">Disable</Trans>
          )
        }
        isPending={setDisabled.isPending}
        onConfirm={() => {
          const disabled = confirming === "disable";
          setDisabled.mutate(
            { appId: app.clientId, disabled },
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
          <Trans id="admin.forward-auth-apps.delete.confirm.title">
            Delete this application?
          </Trans>
        }
        subject={name}
        body={
          <p>
            {count === 0 ? (
              <Trans id="admin.forward-auth-apps.delete.confirm.open">
                The application, its icon and its access policy are deleted.
                Requests through its router are refused immediately. This cannot
                be undone.
              </Trans>
            ) : (
              <Trans id="admin.forward-auth-apps.delete.confirm.groups">
                The application, its icon and its access policy are deleted,
                including its selection of {count} user groups. Requests through
                its router are refused immediately. This cannot be undone.
              </Trans>
            )}
          </p>
        }
        confirmLabel={
          <Trans id="admin.forward-auth-apps.delete.confirm.action">
            Delete application
          </Trans>
        }
        isPending={remove.isPending}
        onConfirm={() => {
          remove.mutate(app.clientId, {
            onSuccess: async () => {
              setConfirming(null);
              await navigate({ to: "/admin/forward-auth-apps" });
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
