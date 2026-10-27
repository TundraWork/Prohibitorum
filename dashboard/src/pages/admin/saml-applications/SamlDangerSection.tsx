import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Power, Trash2, TriangleAlertIcon } from "lucide-react";
import { useState } from "react";
import type { components } from "@/api/generated/schema";
import {
  deleteSamlAppMutationOptions,
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

type SamlApp = components["schemas"]["SAMLApplicationView"];

/**
 * The two changes that stop the service provider, one row each, both visible at
 * once: stop accepting sign-ins for now, or remove the application for good.
 *
 * They sit together because a reader who arrives here is deciding between them,
 * and needs to see that disabling is available — and reversible — before
 * reaching for deletion. It is the same section, in the same words, as on the
 * other kinds of application.
 *
 * Every action is confirmed, including the reversible one. Disabling is undone
 * with a press, but it turns everyone away at the service provider at once, and
 * the dialog's job is to name that rather than ask whether the reader is sure.
 * A dialog stays open while its write is in flight, and a refusal is shown once,
 * under the rows. Deleting asks for a fresh identity check; disabling does not.
 */
export function SamlDangerSection({ app }: { app: SamlApp }) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const setDisabled = useMutation(
    withInlineReport(setAppDisabledMutationOptions(queryClient, "saml"), true),
  );
  const remove = useMutation(
    withInlineReport(deleteSamlAppMutationOptions(queryClient), true),
  );
  const failure = useInlineFailure();

  const [confirming, setConfirming] = useState<
    "disable" | "enable" | "delete" | null
  >(null);

  // Named, so the confirmation's message carries it as `{name}` rather than a
  // positional placeholder the catalogs do not have.
  const name = app.displayName || app.entityId;
  // A deletion leaves for the list, and its dialog stays pending until the
  // page has gone.
  const [leaving, setLeaving] = useState(false);
  const removing = remove.isPending || leaving;
  const busy = setDisabled.isPending || removing;

  return (
    <DangerZoneFrame
      id="danger"
      icon={<TriangleAlertIcon />}
      title={<Trans id="admin.saml-apps.danger.title">Disable or delete</Trans>}
    >
      <ItemList
        label={t({
          id: "admin.saml-apps.danger",
          message: "Danger zone",
        })}
        empty={null}
      >
        <ItemListRow
          icon={<Power size={18} aria-hidden="true" />}
          title={
            app.disabled ? (
              <Trans id="admin.saml-apps.danger.enable">
                Enable this application
              </Trans>
            ) : (
              <Trans id="admin.saml-apps.danger.disable">
                Disable this application
              </Trans>
            )
          }
          details={[
            app.disabled ? (
              <Trans key="note" id="admin.saml-apps.danger.disabled-note">
                No one can sign in to the service provider until the application
                is enabled again.
              </Trans>
            ) : (
              <Trans key="note" id="admin.saml-apps.danger.enabled-note">
                Disabling stops everyone from signing in to the service
                provider. It can be undone.
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
                <Trans id="admin.saml-apps.danger.enable.action">Enable</Trans>
              ) : (
                <Trans id="admin.saml-apps.danger.disable.action">
                  Disable
                </Trans>
              )}
            </Button>
          }
        />

        <ItemListRow
          icon={<Trash2 size={18} aria-hidden="true" />}
          title={
            <Trans id="admin.saml-apps.danger.delete">
              Delete this application
            </Trans>
          }
          details={[
            <Trans key="note" id="admin.saml-apps.danger.delete-note">
              Removes the application, its endpoints, its certificates, its icon
              and its access policy. This cannot be undone.
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
              <Trans id="admin.saml-apps.danger.delete.action">Delete</Trans>
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
            <Trans id="admin.saml-apps.danger.enable.title">
              Enable this application?
            </Trans>
          ) : (
            <Trans id="admin.saml-apps.danger.disable.title">
              Disable this application?
            </Trans>
          )
        }
        subject={name}
        body={
          confirming === "enable" ? (
            <p>
              <Trans id="admin.saml-apps.danger.enable.body">
                Users can sign in to the service provider again immediately.
                Other settings are unchanged.
              </Trans>
            </p>
          ) : (
            <p>
              <Trans id="admin.saml-apps.danger.disable.body">
                The application stops accepting sign-ins immediately. Its
                configuration, endpoints and certificates are kept, so it can be
                enabled again.
              </Trans>
            </p>
          )
        }
        confirmLabel={
          confirming === "enable" ? (
            <Trans id="admin.saml-apps.danger.enable.confirm">Enable</Trans>
          ) : (
            <Trans id="admin.saml-apps.danger.disable.confirm">Disable</Trans>
          )
        }
        isPending={setDisabled.isPending}
        onConfirm={() => {
          const disabled = confirming === "disable";
          setDisabled.mutate(
            { appId: String(app.id), disabled },
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
          <Trans id="admin.saml-apps.danger.delete.title">
            Delete this application?
          </Trans>
        }
        subject={name}
        body={
          <p>
            <Trans id="admin.saml-apps.danger.delete.body">
              The application and its certificates are deleted, and sign-in
              through it stops working. This cannot be undone.
            </Trans>
          </p>
        }
        confirmLabel={
          <Trans id="admin.saml-apps.danger.delete.confirm">
            Delete application
          </Trans>
        }
        isPending={removing}
        onConfirm={() => {
          remove.mutate(app.id, {
            onSuccess: async () => {
              setLeaving(true);
              try {
                await navigate({ to: "/admin/saml-applications" });
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
