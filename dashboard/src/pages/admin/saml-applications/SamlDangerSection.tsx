import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { describeError, isCancellation } from "@/api/errors";
import type { components } from "@/api/generated/schema";
import {
  deleteSamlAppMutationOptions,
  setAppDisabledMutationOptions,
} from "@/api/mutations";
import { Button } from "@/components/custom/Button";
import { ConfirmDialog } from "@/components/custom/ConfirmDialog";
import { ItemList, ItemListRow } from "@/components/custom/ItemList";
import { Section } from "@/components/custom/Section";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";

type SamlApp = components["schemas"]["SAMLApplicationView"];

/**
 * The two things that change whether the application works at all.
 *
 * Disabling is a state change and can be undone, so its confirmation is a
 * warning; deleting removes the record, its endpoints and its certificates, and
 * there is nothing to go back to, so that one is danger. Both are rows on one
 * list because they are read together — an administrator arrives here having
 * already decided which of the two they want.
 *
 * Neither is sudo-gated for a manager: the server authorises both by the
 * application's own manager list. A wrong choice here costs a sign-in method,
 * which is what the confirmation is for, not a credential, which is what a fresh
 * verification would be for.
 */
export function SamlDangerSection({ app }: { app: SamlApp }) {
  const { t } = useLingui();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const setDisabled = useMutation(
    setAppDisabledMutationOptions(queryClient, "saml"),
  );
  const remove = useMutation(deleteSamlAppMutationOptions(queryClient));
  const [confirming, setConfirming] = useState<"disable" | "delete" | null>(
    null,
  );

  const name = app.displayName || app.entityId;

  return (
    <>
      <Section title={<Trans id="admin.saml-apps.danger">Danger zone</Trans>}>
        <ItemList
          label={t({
            id: "admin.saml-apps.danger",
            message: "Danger zone",
          })}
          empty={null}
        >
          <ItemListRow
            title={
              app.disabled ? (
                <Trans id="admin.saml-apps.danger.enable">
                  Enable the application
                </Trans>
              ) : (
                <Trans id="admin.saml-apps.danger.disable">
                  Disable the application
                </Trans>
              )
            }
            details={[
              app.disabled ? (
                <Trans key="disable" id="admin.saml-apps.danger.disabled-note">
                  Everyone is turned away at the service provider until it is
                  enabled again.
                </Trans>
              ) : (
                <Trans key="disable" id="admin.saml-apps.danger.enabled-note">
                  Disabling turns everyone away at the service provider. It can
                  be undone.
                </Trans>
              ),
            ]}
            actions={
              <Button
                size="sm"
                variant={app.disabled ? "secondary" : "danger-soft"}
                isPending={setDisabled.isPending}
                onPress={() => {
                  // Enabling is the reversible direction and needs no dialog;
                  // disabling locks people out until someone notices, so it
                  // asks first.
                  if (app.disabled) {
                    setDisabled.mutate({
                      appId: String(app.id),
                      disabled: false,
                    });
                    return;
                  }
                  setConfirming("disable");
                }}
              >
                {app.disabled ? (
                  <Trans id="admin.saml-apps.danger.enable.action">
                    Enable
                  </Trans>
                ) : (
                  <Trans id="admin.saml-apps.danger.disable.action">
                    Disable
                  </Trans>
                )}
              </Button>
            }
          >
            {setDisabled.error !== null && (
              <SurfaceAlert status="danger" role="alert">
                <SurfaceAlert.Indicator />
                <SurfaceAlert.Content>
                  <SurfaceAlert.Title>
                    {t(describeError(setDisabled.error))}
                  </SurfaceAlert.Title>
                </SurfaceAlert.Content>
              </SurfaceAlert>
            )}
          </ItemListRow>

          <ItemListRow
            title={
              <Trans id="admin.saml-apps.danger.delete">
                Delete the application
              </Trans>
            }
            details={[
              <Trans key="delete" id="admin.saml-apps.danger.delete-note">
                The application, its endpoints and its certificates are removed.
                This cannot be undone.
              </Trans>,
            ]}
            actions={
              <Button
                size="sm"
                variant="danger-soft"
                onPress={() => setConfirming("delete")}
              >
                <Trans id="admin.saml-apps.danger.delete.action">Delete</Trans>
              </Button>
            }
          />
        </ItemList>
      </Section>

      <ConfirmDialog
        isOpen={confirming === "disable"}
        onOpenChange={(open) => !open && setConfirming(null)}
        status="warning"
        title={
          <Trans id="admin.saml-apps.danger.disable.title">
            Disable this application?
          </Trans>
        }
        body={
          <p>
            <Trans id="admin.saml-apps.danger.disable.body">
              {name} stops accepting sign-ins immediately. Its configuration,
              endpoints and certificates are kept, so it can be enabled again.
            </Trans>
          </p>
        }
        confirmLabel={
          <Trans id="admin.saml-apps.danger.disable.confirm">Disable</Trans>
        }
        isPending={setDisabled.isPending}
        onConfirm={() => {
          setDisabled.mutate(
            { appId: String(app.id), disabled: true },
            { onSettled: () => setConfirming(null) },
          );
        }}
      />

      <ConfirmDialog
        isOpen={confirming === "delete"}
        onOpenChange={(open) => !open && setConfirming(null)}
        status="danger"
        title={
          <Trans id="admin.saml-apps.danger.delete.title">
            Delete this application?
          </Trans>
        }
        body={
          <p>
            <Trans id="admin.saml-apps.danger.delete.body">
              {name} and its certificates are removed, and anyone who signs in
              through it is turned away. This cannot be undone.
            </Trans>
          </p>
        }
        confirmLabel={
          <Trans id="admin.saml-apps.danger.delete.confirm">Delete</Trans>
        }
        isPending={remove.isPending}
        onConfirm={() => {
          remove.mutate(app.id, {
            // Only the delete leaves the page: the record every section above
            // draws is gone, so staying would show forms over nothing. A
            // refusal keeps the reader here, where the row it is about still is.
            onSuccess: async () => {
              await navigate({ to: "/admin/saml-applications" });
            },
            onError: (error) => {
              if (isCancellation(error)) return;
              setConfirming(null);
            },
          });
        }}
      />

      {/* A failed delete reports itself on the row rather than in a dialog of
          its own: the row is already the thing that failed, and the reason
          belongs beside the action that produced it. */}
      <ConfirmDialog
        isOpen={remove.error !== null && confirming === null}
        onOpenChange={() => remove.reset()}
        status="danger"
        title={
          <Trans id="admin.saml-apps.danger.delete.failed">
            The application could not be deleted
          </Trans>
        }
        body={<p>{t(describeError(remove.error))}</p>}
        confirmLabel={
          <Trans id="admin.saml-apps.danger.delete.dismiss">Close</Trans>
        }
        onConfirm={() => remove.reset()}
      />
    </>
  );
}
