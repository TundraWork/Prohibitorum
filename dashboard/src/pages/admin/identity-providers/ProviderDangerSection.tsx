import { Modal, Tooltip } from "@heroui/react";
import { msg } from "@lingui/core/macro";
import { Plural, Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { KeyRound, Power, Trash2 } from "lucide-react";
import { type ReactNode, useState } from "react";
import { readOidcProviderConfig } from "@/api/federation";
import type { components } from "@/api/generated/schema";
import {
  deleteIdentityProviderMutationOptions,
  setIdentityProviderDisabledMutationOptions,
  setIdentityProviderSecretMutationOptions,
} from "@/api/mutations";
import { Button } from "@/components/custom/Button";
import { ConfirmDialog } from "@/components/custom/ConfirmDialog";
import { ItemList, ItemListRow } from "@/components/custom/ItemList";
import { Section } from "@/components/custom/Section";
import { useAppForm } from "@/forms/use-app-form";

type Provider = components["schemas"]["IdentityProviderView"];

/**
 * Enable or disable, set the secret, delete — one row per action, as the
 * console's other danger sections are.
 *
 * Enabling is offered as a disabled button with the reason in a tooltip when
 * the provider cannot be enabled: the server refuses it with
 * `provider_not_ready`, and a button that simply failed on press would leave
 * the reader guessing what is missing. What is missing depends on the protocol,
 * so the tooltip says it for this one.
 *
 * A public OIDC client sends no secret, so its row is left out rather than
 * shown disabled: there is nothing to set. A write that fails is reported by
 * the console's error toast, as every failed request is, and the rows stay as
 * they were.
 */
export function ProviderDangerSection({ provider }: { provider: Provider }) {
  const { t } = useLingui();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [confirmingDisable, setConfirmingDisable] = useState(false);
  const [confirmingDelete, setConfirmingDelete] = useState(false);
  const [settingSecret, setSettingSecret] = useState(false);

  const setDisabled = useMutation(
    setIdentityProviderDisabledMutationOptions(queryClient),
  );
  const remove = useMutation(
    deleteIdentityProviderMutationOptions(queryClient),
  );
  const setSecret = useMutation(
    setIdentityProviderSecretMutationOptions(queryClient),
  );

  const hasSecret = provider.secretConfigured;
  const isPublicClient =
    provider.protocol === "oidc" &&
    readOidcProviderConfig(provider.config)?.tokenAuthMethod === "none";
  const takesSecret =
    (provider.protocol === "oidc" && !isPublicClient) ||
    provider.protocol === "steam";
  const cannotEnable = provider.disabled && !provider.ready;
  // Named, so the confirmation carries it as `{count}` in both catalogs.
  const count = provider.linkedAccountCount ?? 0;

  const toggle = (
    <Button
      size="sm"
      variant="secondary"
      isDisabled={cannotEnable}
      isPending={setDisabled.isPending}
      onPress={() => {
        if (provider.disabled) {
          setDisabled.mutate({ slug: provider.slug, disabled: false });
        } else {
          setConfirmingDisable(true);
        }
      }}
    >
      {provider.disabled ? (
        <Trans id="admin.federation.danger.enable.action">Enable</Trans>
      ) : (
        <Trans id="admin.federation.danger.disable.action">Disable</Trans>
      )}
    </Button>
  );

  return (
    <Section
      title={<Trans id="admin.federation.danger.title">Danger zone</Trans>}
    >
      <ItemList
        label={t({
          id: "admin.federation.danger.label",
          message: "Danger zone",
        })}
        empty={null}
      >
        <ItemListRow
          icon={<Power size={18} aria-hidden="true" />}
          title={
            provider.disabled ? (
              <Trans id="admin.federation.danger.enable">
                Enable this provider
              </Trans>
            ) : (
              <Trans id="admin.federation.danger.disable">
                Disable this provider
              </Trans>
            )
          }
          details={[
            provider.disabled ? (
              <Trans key="off" id="admin.federation.danger.enable.hint">
                It is not offered at sign-in right now.
              </Trans>
            ) : (
              <Trans key="on" id="admin.federation.danger.disable.hint">
                Nobody can sign in with it while it is disabled.
              </Trans>
            ),
          ]}
          actions={
            cannotEnable ? (
              <Tooltip delay={0}>
                <Tooltip.Trigger>{toggle}</Tooltip.Trigger>
                <Tooltip.Content>
                  {notReadyReason(provider.protocol)}
                </Tooltip.Content>
              </Tooltip>
            ) : (
              toggle
            )
          }
        />

        {takesSecret && (
          <ItemListRow
            icon={<KeyRound size={18} aria-hidden="true" />}
            title={
              hasSecret ? (
                <Trans id="admin.federation.danger.replace-secret">
                  Replace the secret
                </Trans>
              ) : (
                <Trans id="admin.federation.danger.set-secret">
                  Set the secret
                </Trans>
              )
            }
            details={[
              provider.protocol === "oidc" ? (
                <Trans key="oidc" id="admin.federation.danger.secret.oidc">
                  The client secret this instance signs in with.
                </Trans>
              ) : (
                <Trans key="steam" id="admin.federation.danger.secret.steam">
                  The Steam Web API key.
                </Trans>
              ),
            ]}
            actions={
              <Button
                size="sm"
                variant="secondary"
                onPress={() => setSettingSecret(true)}
              >
                {hasSecret ? (
                  <Trans id="admin.federation.danger.secret.replace-action">
                    Replace
                  </Trans>
                ) : (
                  <Trans id="admin.federation.danger.secret.action">Set</Trans>
                )}
              </Button>
            }
          />
        )}

        <ItemListRow
          icon={<Trash2 size={18} aria-hidden="true" />}
          title={
            <Trans id="admin.federation.danger.delete">
              Delete this provider
            </Trans>
          }
          details={[
            <Trans key="delete" id="admin.federation.danger.delete.hint">
              The accounts linked through it stay; only the link is removed.
            </Trans>,
          ]}
          actions={
            <Button
              size="sm"
              variant="danger-soft"
              isPending={remove.isPending}
              onPress={() => setConfirmingDelete(true)}
            >
              <Trans id="admin.federation.danger.delete.action">Delete</Trans>
            </Button>
          }
        />
      </ItemList>

      <ConfirmDialog
        isOpen={confirmingDisable}
        onOpenChange={setConfirmingDisable}
        status="warning"
        title={
          <Trans id="admin.federation.danger.disable.confirm.title">
            Disable this provider?
          </Trans>
        }
        body={
          <p>
            <Trans id="admin.federation.danger.disable.confirm.body">
              It disappears from the sign-in page and from every account that
              could step up with it. Nothing is deleted.
            </Trans>
          </p>
        }
        confirmLabel={
          <Trans id="admin.federation.danger.disable.confirm.action">
            Disable
          </Trans>
        }
        isPending={setDisabled.isPending}
        onConfirm={() => {
          setDisabled.mutate(
            { slug: provider.slug, disabled: true },
            { onSettled: () => setConfirmingDisable(false) },
          );
        }}
      />

      <ConfirmDialog
        isOpen={confirmingDelete}
        onOpenChange={setConfirmingDelete}
        status="danger"
        title={
          <Trans id="admin.federation.danger.delete.confirm.title">
            Delete this provider?
          </Trans>
        }
        body={
          count > 0 ? (
            <p>
              <Trans id="admin.federation.danger.delete.confirm.linked">
                <Plural
                  value={count}
                  one="# account loses"
                  other="# accounts lose"
                />{" "}
                the way they sign in with it. An account that can only sign in
                this way needs a new registration link from you.
              </Trans>
            </p>
          ) : (
            <p>
              <Trans id="admin.federation.danger.delete.confirm.empty">
                The provider and its icon are removed.
              </Trans>
            </p>
          )
        }
        confirmLabel={
          <Trans id="admin.federation.danger.delete.confirm.action">
            Delete
          </Trans>
        }
        isPending={remove.isPending}
        onConfirm={() => {
          remove.mutate(provider.slug, {
            onSuccess: async () => {
              await navigate({ to: "/admin/identity-providers" });
            },
            onSettled: () => setConfirmingDelete(false),
          });
        }}
      />

      <SetSecretDialog
        isOpen={settingSecret}
        onOpenChange={(open) => {
          if (!open) setSecret.reset();
          setSettingSecret(open);
        }}
        title={
          hasSecret ? (
            <Trans id="admin.federation.danger.replace-secret">
              Replace the secret
            </Trans>
          ) : (
            <Trans id="admin.federation.danger.set-secret">
              Set the secret
            </Trans>
          )
        }
        label={
          provider.protocol === "oidc" ? (
            <Trans id="admin.federation.new.secret">Client secret</Trans>
          ) : (
            <Trans id="admin.federation.new.steam-key">Web API key</Trans>
          )
        }
        isPending={setSecret.isPending}
        onSave={(secret) =>
          setSecret.mutateAsync({ slug: provider.slug, secret }).then(
            () => setSettingSecret(false),
            () => undefined,
          )
        }
      />
    </Section>
  );
}

/** What a provider of this protocol still needs before it can be enabled. */
function notReadyReason(protocol: string): ReactNode {
  switch (protocol) {
    case "steam":
      return (
        <Trans id="admin.federation.danger.not-ready.steam">
          Set the Web API key before enabling it.
        </Trans>
      );
    case "vrchat":
      return (
        <Trans id="admin.federation.danger.not-ready.vrchat">
          Sign in the operator account before enabling it.
        </Trans>
      );
    default:
      return (
        <Trans id="admin.federation.danger.not-ready.oidc">
          Needs a valid connection and a client secret before it can be enabled.
        </Trans>
      );
  }
}

const secretRequired = msg({
  id: "admin.federation.danger.secret.required",
  message: "Enter the secret.",
});

/**
 * One secret in a dialog. It is write-only: the server never returns a stored
 * secret, so the field starts empty and replacing means typing the new value
 * rather than editing the old one. The form spans the body and the footer, so
 * the actions sit where HeroUI keeps a dialog's actions.
 */
function SetSecretDialog({
  isOpen,
  onOpenChange,
  title,
  label,
  isPending,
  onSave,
}: {
  isOpen: boolean;
  onOpenChange: (open: boolean) => void;
  title: ReactNode;
  label: ReactNode;
  isPending: boolean;
  onSave: (secret: string) => Promise<void>;
}) {
  const { t } = useLingui();
  const form = useAppForm({
    defaultValues: { secret: "" },
    onSubmit: async ({ value }) => {
      await onSave(value.secret);
      form.reset();
    },
  });

  return (
    <Modal isOpen={isOpen} onOpenChange={onOpenChange}>
      <Modal.Backdrop>
        <Modal.Container placement="center" size="md">
          <Modal.Dialog>
            <Modal.Header>
              <Modal.Heading>{title}</Modal.Heading>
            </Modal.Header>
            <form.AppForm>
              <form.Form
                label={t({
                  id: "admin.federation.danger.secret.form",
                  message: "Secret",
                })}
                className="flex min-h-0 flex-1 flex-col"
              >
                <Modal.Body>
                  <div className="flex flex-col gap-4">
                    <form.FormError />
                    <form.AppField
                      name="secret"
                      validators={{
                        onSubmit: ({ value }) =>
                          value.trim() === "" ? secretRequired : undefined,
                      }}
                    >
                      {(field) => (
                        <field.FormField
                          label={label}
                          description={
                            <Trans id="admin.federation.danger.secret.hint">
                              Stored encrypted and never shown again.
                            </Trans>
                          }
                          type="password"
                          autoComplete="new-password"
                          variant="secondary"
                        />
                      )}
                    </form.AppField>
                  </div>
                </Modal.Body>
                <Modal.Footer className="mt-5">
                  <Button
                    variant="secondary"
                    isDisabled={isPending}
                    onPress={() => onOpenChange(false)}
                  >
                    <Trans id="confirm.cancel">Cancel</Trans>
                  </Button>
                  <form.SubmitButton>
                    <Trans id="admin.federation.danger.secret.save">Save</Trans>
                  </form.SubmitButton>
                </Modal.Footer>
              </form.Form>
            </form.AppForm>
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    </Modal>
  );
}
