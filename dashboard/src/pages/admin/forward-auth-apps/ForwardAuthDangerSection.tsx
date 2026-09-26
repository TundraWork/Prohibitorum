import { Alert } from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Power, Trash2 } from "lucide-react";
import { useState } from "react";
import { describeError } from "@/api/errors";
import type { components } from "@/api/generated/schema";
import {
  deleteForwardAuthAppMutationOptions,
  setAppDisabledMutationOptions,
} from "@/api/mutations";
import { appAccessQueryOptions } from "@/api/queries";
import { Button } from "@/components/custom/Button";
import { ConfirmDialog } from "@/components/custom/ConfirmDialog";
import { ItemList, ItemListRow } from "@/components/custom/ItemList";
import { Section } from "@/components/custom/Section";

type ForwardAuthApp = components["schemas"]["ForwardAuthAppView"];

/**
 * The two actions that change whether the application works at all.
 *
 * One state union rather than one dialog per row: only one of these can be
 * being decided at a time, and a shared `confirming` means opening the second
 * cannot leave the first mounted behind it.
 *
 * Disabling does not revoke what a person already holds — a browser session on
 * the protected host keeps its cookie until it expires, and it is the next
 * verification against a disabled application that refuses it. So the dialog
 * says requests stop being accepted rather than promising everyone is signed
 * out. Deletion names the audience instead of the record, because the record is
 * a row the reader has already seen; what they cannot see from here is who is
 * on the other side of it.
 */
export function ForwardAuthDangerSection({ app }: { app: ForwardAuthApp }) {
  const { t } = useLingui();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const setDisabled = useMutation(
    setAppDisabledMutationOptions(queryClient, "forward_auth"),
  );
  const remove = useMutation(deleteForwardAuthAppMutationOptions(queryClient));

  const [confirming, setConfirming] = useState<
    "disable" | "enable" | "delete" | null
  >(null);

  // The access read already knows which user groups the application is limited
  // to, and the delete dialog is where that list means something. A read that
  // has not landed yet only costs the dialog its count.
  const access = useQuery(appAccessQueryOptions("forward_auth", app.clientId));
  const groups = (access.data?.groups ?? []).map((group) => group.displayName);

  const failure = remove.error ?? setDisabled.error;

  const confirmingDelete = confirming === "delete";
  const confirmingDisable = confirming === "disable";
  const confirmingEnable = confirming === "enable";

  return (
    <Section
      title={<Trans id="admin.forward-auth-apps.danger">Danger zone</Trans>}
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
                Enable the application
              </Trans>
            ) : (
              <Trans id="admin.forward-auth-apps.disable">
                Disable the application
              </Trans>
            )
          }
          details={[
            app.disabled ? (
              <Trans key="note" id="admin.forward-auth-apps.enable.note">
                Requests through its router are accepted again, under the access
                policy it already has.
              </Trans>
            ) : (
              <Trans key="note" id="admin.forward-auth-apps.disable.note">
                Every request through its router is turned away, including ones
                from people who are signed in. The record, its vocabulary and
                its access policy are kept.
              </Trans>
            ),
          ]}
          actions={
            <Button
              size="sm"
              variant="secondary"
              isPending={setDisabled.isPending}
              onPress={() => setConfirming(app.disabled ? "enable" : "disable")}
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
              Delete the application
            </Trans>
          }
          details={[
            <Trans key="note" id="admin.forward-auth-apps.delete.note">
              Removes the application, its scope vocabulary, its icon and its
              access policy. Accounts are not affected.
            </Trans>,
          ]}
          actions={
            <Button
              size="sm"
              variant="danger-soft"
              isPending={remove.isPending}
              onPress={() => setConfirming("delete")}
            >
              <Trans id="admin.forward-auth-apps.delete.action">Delete</Trans>
            </Button>
          }
        />
      </ItemList>

      {failure !== null && (
        <Alert status="danger" role="alert" className="mt-4">
          <Alert.Indicator />
          <Alert.Content>
            <Alert.Title>{t(describeError(failure))}</Alert.Title>
          </Alert.Content>
        </Alert>
      )}

      <ConfirmDialog
        isOpen={confirmingDisable || confirmingEnable}
        onOpenChange={(open) => !open && setConfirming(null)}
        status="warning"
        title={
          confirmingEnable ? (
            <Trans id="admin.forward-auth-apps.enable.confirm.title">
              Enable this application?
            </Trans>
          ) : (
            <Trans id="admin.forward-auth-apps.disable.confirm.title">
              Disable this application?
            </Trans>
          )
        }
        body={
          <p>
            {confirmingEnable ? (
              <Trans id="admin.forward-auth-apps.enable.confirm.body">
                The gateway starts accepting requests through this application
                again, under the access policy it already has.
              </Trans>
            ) : (
              <Trans id="admin.forward-auth-apps.disable.confirm.body">
                The gateway refuses every request through this application,
                including ones from people who are signed in. Nothing is
                deleted, and enabling it again restores access.
              </Trans>
            )}
          </p>
        }
        confirmLabel={
          confirmingEnable ? (
            <Trans id="admin.forward-auth-apps.enable.action">Enable</Trans>
          ) : (
            <Trans id="admin.forward-auth-apps.disable.action">Disable</Trans>
          )
        }
        isPending={setDisabled.isPending}
        onConfirm={() => {
          const disabled = confirmingDisable;
          setDisabled.mutate(
            { appId: app.clientId, disabled },
            { onSettled: () => setConfirming(null) },
          );
        }}
      />

      <ConfirmDialog
        isOpen={confirmingDelete}
        onOpenChange={(open) => !open && setConfirming(null)}
        status="danger"
        title={
          <Trans id="admin.forward-auth-apps.delete.confirm.title">
            Delete this application?
          </Trans>
        }
        body={
          <p>
            {groups.length === 0 ? (
              <Trans id="admin.forward-auth-apps.delete.confirm.open">
                {app.displayName || app.clientId} is removed along with its
                scope vocabulary, its icon and its access policy. Requests
                through its router are turned away immediately. This cannot be
                undone.
              </Trans>
            ) : (
              <Trans id="admin.forward-auth-apps.delete.confirm.groups">
                {app.displayName || app.clientId} is removed along with its
                scope vocabulary, its icon and its access policy — including its
                selection of {groups.length} user groups. Requests through its
                router are turned away immediately. This cannot be undone.
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
              await navigate({ to: "/admin/forward-auth-apps" });
            },
          });
        }}
      />
    </Section>
  );
}
