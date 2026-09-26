import { Modal } from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { KeyRound, Power, Trash2 } from "lucide-react";
import { useState } from "react";
import { describeError } from "@/api/errors";
import type { components } from "@/api/generated/schema";
import {
  deleteOidcAppMutationOptions,
  rotateOidcSecretMutationOptions,
  setAppDisabledMutationOptions,
} from "@/api/mutations";
import { Button } from "@/components/custom/Button";
import { ConfirmDialog } from "@/components/custom/ConfirmDialog";
import { ItemList, ItemListRow } from "@/components/custom/ItemList";
import { SecretReveal } from "@/components/custom/SecretReveal";
import { Section } from "@/components/custom/Section";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";
import { clientSecretCopy } from "@/pages/admin/oidc-applications/secret-copy";

type OidcApp = components["schemas"]["OIDCApplicationView"];

/**
 * The three changes that break clients, one row each, all visible at once.
 *
 * They sit together because a reader who arrives here is deciding between them:
 * someone whose client is misbehaving wants to stop it accepting sign-ins, and
 * needs to see that disabling is available — and reversible — before reaching
 * for rotating the secret or deleting the record. Collapsing them behind a
 * control would hide the cheap option behind the expensive one.
 *
 * Every action is confirmed, including the reversible one. Disabling is undone
 * with a press, but it silently ends sign-in for every client of this
 * application at once, and the dialog's job is to name that rather than ask
 * whether the reader is sure.
 */
export function DangerSection({ app }: { app: OidcApp }) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const setDisabled = useMutation(
    setAppDisabledMutationOptions(queryClient, "oidc"),
  );
  const rotate = useMutation(rotateOidcSecretMutationOptions(queryClient));
  const remove = useMutation(deleteOidcAppMutationOptions(queryClient));

  const [confirming, setConfirming] = useState<
    "disable" | "enable" | "rotate" | "delete" | null
  >(null);
  const [secret, setSecret] = useState<string | null>(null);

  // The list loads first as `undefined`; the danger rows must not offer a
  // rotation the server would take for a public client.
  const isPublic = app.clientAuthMethod === "none";

  const failure = rotate.error ?? remove.error ?? setDisabled.error;

  return (
    <Section title={<Trans id="admin.oidc-apps.danger">Danger zone</Trans>}>
      <ItemList
        label={t({ id: "admin.oidc-apps.danger", message: "Danger zone" })}
        empty={null}
      >
        <ItemListRow
          icon={<Power size={18} aria-hidden="true" />}
          title={
            app.disabled ? (
              <Trans id="admin.oidc-apps.enable">Enable the application</Trans>
            ) : (
              <Trans id="admin.oidc-apps.disable">
                Disable the application
              </Trans>
            )
          }
          details={[
            app.disabled ? (
              <Trans key="note" id="admin.oidc-apps.enable.note">
                Clients can sign accounts in again. Nothing else about the
                application changes.
              </Trans>
            ) : (
              <Trans key="note" id="admin.oidc-apps.disable.note">
                Every client of this application stops being able to sign
                accounts in. The record, its secret and its access policy are
                kept.
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
                <Trans id="admin.oidc-apps.enable.action">Enable</Trans>
              ) : (
                <Trans id="admin.oidc-apps.disable.action">Disable</Trans>
              )}
            </Button>
          }
        />

        {/* Rotating a public client's secret would create a credential the
            client never sends, so the row is left out rather than shown
            disabled: there is nothing here to explain to a reader who has no
            secret. */}
        {!isPublic && (
          <ItemListRow
            icon={<KeyRound size={18} aria-hidden="true" />}
            title={
              <Trans id="admin.oidc-apps.rotate">
                Rotate the client secret
              </Trans>
            }
            details={[
              <Trans key="note" id="admin.oidc-apps.rotate.note">
                Issues a new secret and discards the old one. Clients still
                using the old secret stop signing accounts in until they are
                updated.
              </Trans>,
            ]}
            actions={
              <Button
                size="sm"
                variant="secondary"
                isPending={rotate.isPending}
                onPress={() => setConfirming("rotate")}
              >
                <Trans id="admin.oidc-apps.rotate.action">Rotate secret</Trans>
              </Button>
            }
          />
        )}

        <ItemListRow
          icon={<Trash2 size={18} aria-hidden="true" />}
          title={
            <Trans id="admin.oidc-apps.delete">Delete the application</Trans>
          }
          details={[
            <Trans key="note" id="admin.oidc-apps.delete.note">
              Removes the application, its secret, its icon and its access
              policy. This cannot be undone.
            </Trans>,
          ]}
          actions={
            <Button
              size="sm"
              variant="danger-soft"
              isPending={remove.isPending}
              onPress={() => setConfirming("delete")}
            >
              <Trans id="admin.oidc-apps.delete.action">Delete</Trans>
            </Button>
          }
        />
      </ItemList>

      {failure !== null && failure !== undefined && (
        <SurfaceAlert status="danger" role="alert">
          <SurfaceAlert.Indicator />
          <SurfaceAlert.Content>
            <SurfaceAlert.Title>{t(describeError(failure))}</SurfaceAlert.Title>
          </SurfaceAlert.Content>
        </SurfaceAlert>
      )}

      <ConfirmDialog
        isOpen={confirming === "disable" || confirming === "enable"}
        onOpenChange={(open) => !open && setConfirming(null)}
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
        body={
          confirming === "enable" ? (
            <p>
              <Trans id="admin.oidc-apps.enable.body">
                Clients can sign accounts in again straight away. Accounts that
                signed in while it was disabled were not affected.
              </Trans>
            </p>
          ) : (
            <p>
              <Trans id="admin.oidc-apps.disable.body">
                Every client stops being able to sign accounts in, including the
                ones that are working. You can enable it again at any time.
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
            { onSettled: () => setConfirming(null) },
          );
        }}
      />

      <ConfirmDialog
        isOpen={confirming === "rotate"}
        onOpenChange={(open) => !open && setConfirming(null)}
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
          // where the explanation is.
          rotate.mutate(app.clientId, {
            onSuccess: (result) => {
              setConfirming(null);
              setSecret(result.secret);
            },
            onError: () => setConfirming(null),
          });
        }}
      />

      <ConfirmDialog
        isOpen={confirming === "delete"}
        onOpenChange={(open) => !open && setConfirming(null)}
        status="danger"
        title={
          <Trans id="admin.oidc-apps.delete.title">
            Delete this application?
          </Trans>
        }
        body={
          <p>
            <Trans id="admin.oidc-apps.delete.body">
              {app.displayName || app.clientId} stops being able to sign anyone
              in, and its icon, secret and access policy go with it. This cannot
              be undone.
            </Trans>
          </p>
        }
        confirmLabel={<Trans id="admin.oidc-apps.delete.action">Delete</Trans>}
        isPending={remove.isPending}
        onConfirm={() => {
          remove.mutate(app.clientId, {
            onSuccess: async () => {
              setConfirming(null);
              await navigate({ to: "/admin/oidc-applications" });
            },
            onError: () => setConfirming(null),
          });
        }}
      />

      {secret !== null && (
        // Open and non-dismissable while the secret is held here, so it cannot
        // outlive the value: the reveal's own continue control is the only way
        // out, and it stays on the page the secret belongs to.
        <Modal isOpen onOpenChange={() => {}}>
          <Modal.Backdrop isDismissable={false} isKeyboardDismissDisabled>
            <Modal.Container placement="center" size="lg" scroll="inside">
              <Modal.Dialog>
                <Modal.Header>
                  <Modal.Heading>{t(clientSecretCopy.title)}</Modal.Heading>
                </Modal.Header>
                <SecretReveal
                  text={secret}
                  filename={`${app.clientId}-client-secret.txt`}
                  copy={clientSecretCopy}
                  onContinue={async () => setSecret(null)}
                  inDialog
                />
              </Modal.Dialog>
            </Modal.Container>
          </Modal.Backdrop>
        </Modal>
      )}
    </Section>
  );
}
