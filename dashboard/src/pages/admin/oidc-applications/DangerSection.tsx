import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Power, Trash2, TriangleAlertIcon } from "lucide-react";
import { useState } from "react";
import type { components } from "@/api/generated/schema";
import {
  deleteOidcAppMutationOptions,
  setAppDisabledMutationOptions,
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

type OidcApp = components["schemas"]["OIDCApplicationView"];

/**
 * The two changes that stop clients, one row each, both visible at once: stop
 * accepting sign-ins for now, or remove the application for good.
 *
 * They sit together because a reader who arrives here is deciding between them:
 * someone whose client is misbehaving wants to stop it accepting sign-ins, and
 * needs to see that disabling is available — and reversible — before reaching
 * for deletion. Rotating the secret is a credential change and lives with the
 * credentials.
 *
 * Every action is confirmed, including the reversible one. Disabling is undone
 * with a press, but it silently ends sign-in for every client of this
 * application at once, and the dialog's job is to name that rather than ask
 * whether the reader is sure. A dialog stays open while its write is in flight,
 * and a refusal is shown once, under the rows.
 */
export function DangerSection({ app }: { app: OidcApp }) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const setDisabled = useMutation(
    withInlineReport(setAppDisabledMutationOptions(queryClient, "oidc"), true),
  );
  const remove = useMutation(
    withInlineReport(deleteOidcAppMutationOptions(queryClient), true),
  );
  const failure = useInlineFailure();

  const [confirming, setConfirming] = useState<
    "disable" | "enable" | "delete" | null
  >(null);

  // The confirmations name the application under their title.
  const name = app.displayName || app.clientId;
  // A deletion leaves for the list, and its dialog stays pending until the
  // page has gone.
  const [leaving, setLeaving] = useState(false);
  const removing = remove.isPending || leaving;
  const busy = setDisabled.isPending || removing;

  return (
    <DangerZoneFrame
      id="danger"
      icon={<TriangleAlertIcon />}
      title={<Trans id="admin.oidc-apps.danger.title">Disable or delete</Trans>}
    >
      <ItemList
        label={t({
          id: "admin.oidc-apps.danger",
          message: "Danger zone",
        })}
        empty={null}
      >
        <ItemListRow
          icon={<Power size={18} aria-hidden="true" />}
          title={
            app.disabled ? (
              <Trans id="admin.oidc-apps.enable">Enable this application</Trans>
            ) : (
              <Trans id="admin.oidc-apps.disable">
                Disable this application
              </Trans>
            )
          }
          details={[
            app.disabled ? (
              <Trans key="note" id="admin.oidc-apps.enable.note">
                Users can sign in through this application again. Nothing else
                changes.
              </Trans>
            ) : (
              <Trans key="note" id="admin.oidc-apps.disable.note">
                Users can no longer sign in through this application. Its
                record, client secret and access policy are kept.
              </Trans>
            ),
          ]}
          stackActions
          actions={
            <Button
              disabled={setDisabled.isPending}
              onClick={() => {
                failure.clear();
                setConfirming(app.disabled ? "enable" : "disable");
              }}
              variant="outline"
            >
              {app.disabled ? (
                <Trans id="admin.oidc-apps.enable.action">Enable</Trans>
              ) : (
                <Trans id="admin.oidc-apps.disable.action">Disable</Trans>
              )}
            </Button>
          }
        />

        <ItemListRow
          icon={<Trash2 size={18} aria-hidden="true" />}
          title={
            <Trans id="admin.oidc-apps.delete">Delete this application</Trans>
          }
          details={[
            <Trans key="note" id="admin.oidc-apps.delete.note">
              Removes the application, its secret, its icon and its access
              policy. This cannot be undone.
            </Trans>,
          ]}
          stackActions
          actions={
            <Button
              disabled={removing}
              onClick={() => {
                failure.clear();
                setConfirming("delete");
              }}
              variant="destructive-outline"
            >
              <Trans id="admin.oidc-apps.delete.action">Delete</Trans>
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
            <Trans id="admin.oidc-apps.enable.title">
              Enable this application?
            </Trans>
          ) : (
            <Trans id="admin.oidc-apps.disable.title">
              Disable this application?
            </Trans>
          )
        }
        subject={name}
        body={
          confirming === "enable" ? (
            <p>
              <Trans id="admin.oidc-apps.enable.body">
                Users can sign in through this application again immediately.
              </Trans>
            </p>
          ) : (
            <p>
              <Trans id="admin.oidc-apps.disable.body">
                Users can no longer sign in through any client of this
                application, including clients that are currently working. You
                can enable it again at any time.
              </Trans>
            </p>
          )
        }
        confirmLabel={
          confirming === "enable" ? (
            <Trans id="admin.oidc-apps.enable.action">Enable</Trans>
          ) : (
            <Trans id="admin.oidc-apps.disable.action">Disable</Trans>
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
          <Trans id="admin.oidc-apps.delete.title">
            Delete this application?
          </Trans>
        }
        subject={name}
        body={
          <p>
            <Trans id="admin.oidc-apps.delete.body">
              Sign-in through this application stops, and its icon, client
              secret and access policy are deleted. This cannot be undone.
            </Trans>
          </p>
        }
        confirmLabel={
          <Trans id="admin.oidc-apps.delete.confirm.action">
            Delete application
          </Trans>
        }
        isPending={removing}
        onConfirm={() => {
          remove.mutate(app.clientId, {
            onSuccess: async () => {
              setLeaving(true);
              try {
                await navigate({ to: "/admin/oidc-applications" });
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
