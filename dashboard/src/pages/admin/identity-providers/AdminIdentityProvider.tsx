import { Modal } from "@heroui/react";
import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import {
  useMutation,
  useQueryClient,
  useSuspenseQuery,
} from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Power } from "lucide-react";
import { type ReactNode, useState } from "react";
import type { components } from "@/api/generated/schema";
import {
  deleteIdentityProviderMutationOptions,
  setIdentityProviderDisabledMutationOptions,
  setIdentityProviderSecretMutationOptions,
} from "@/api/mutations";
import { identityProviderQueryOptions } from "@/api/queries";
import { Button } from "@/components/custom/Button";
import { ConfirmDialog } from "@/components/custom/ConfirmDialog";
import { ItemList, ItemListRow } from "@/components/custom/ItemList";
import { Section } from "@/components/custom/Section";
import { useAppForm } from "@/forms/use-app-form";
import { ProviderClaimsSection } from "@/pages/admin/identity-providers/ProviderClaimsSection";
import { ProviderConnectionSection } from "@/pages/admin/identity-providers/ProviderConnectionSection";
import { ProviderDiagnosticsSection } from "@/pages/admin/identity-providers/ProviderDiagnosticsSection";
import { ProviderGeneralSection } from "@/pages/admin/identity-providers/ProviderGeneralSection";
import { ProviderOperatorSection } from "@/pages/admin/identity-providers/ProviderOperatorSection";

type Provider = components["schemas"]["IdentityProviderView"];

/**
 * One identity provider, as a stack of sections rather than a set of tabs.
 *
 * Every section is short — a form, an icon, two cards of diagnostics, one
 * action list — and an administrator visiting a provider usually reads several
 * of them together: is it ready, does discovery resolve, which claims map where.
 * Showing them all at once means the answer is on screen rather than one click
 * away, and it is the same shape the security and settings pages use.
 *
 * Only the sections the provider's protocol can fill are drawn. A Steam provider
 * has a name, a key and a danger list; drawing an empty "connection" or
 * "diagnostics" heading over them would say a setting exists that does not.
 *
 * The one piece of state the page owns is the diagnostic run id, because it
 * arrives on the URL and two sections need to agree on it.
 */
export function AdminIdentityProvider({
  slug,
  runId,
  onRunIdChange,
}: {
  slug: string;
  /** The diagnostic run named by `?test=`, read from the route's search. */
  runId: string | undefined;
  onRunIdChange: (runId: string | undefined) => void;
}) {
  const { data: provider } = useSuspenseQuery(
    identityProviderQueryOptions(slug),
  );

  return (
    <div className="flex flex-col gap-8">
      <Section
        title={<Trans id="admin.federation.general.title">Provider</Trans>}
      >
        <ProviderGeneralSection provider={provider} />
      </Section>

      {provider.protocol === "oidc" && (
        <>
          <Section
            title={
              <Trans id="admin.federation.connection.title">Connection</Trans>
            }
          >
            <ProviderConnectionSection provider={provider} />
          </Section>
          <Section
            title={
              <Trans id="admin.federation.claims.title">
                Accounts and claims
              </Trans>
            }
          >
            <ProviderClaimsSection provider={provider} />
          </Section>
          <Section
            title={
              <Trans id="admin.federation.diagnostics.title">Diagnostics</Trans>
            }
          >
            <ProviderDiagnosticsSection
              slug={provider.slug}
              runId={runId}
              onRunIdChange={onRunIdChange}
            />
          </Section>
        </>
      )}

      {provider.supportsOperator && (
        <Section
          title={
            <Trans id="admin.federation.operator.title">Operator session</Trans>
          }
        >
          <ProviderOperatorSection provider={provider} />
        </Section>
      )}

      <Section
        title={<Trans id="admin.federation.danger.title">Danger zone</Trans>}
      >
        <ProviderDangerSection provider={provider} />
      </Section>
    </div>
  );
}

/**
 * Enable, disable, set the secret and delete.
 *
 * One row per action, as the console's other danger sections are. Enabling is
 * offered as a disabled button with the reason in a tooltip when the provider
 * cannot be enabled — the server refuses it with `provider_not_ready`, and a
 * button that simply fails on press would make the reader guess which of the
 * three readiness conditions is unmet.
 */
function ProviderDangerSection({ provider }: { provider: Provider }) {
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

  return (
    <>
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
            <Button
              size="sm"
              variant={provider.disabled ? "secondary" : "danger-soft"}
              isDisabled={provider.disabled && !provider.ready}
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
                <Trans id="admin.federation.danger.disable.action">
                  Disable
                </Trans>
              )}
            </Button>
          }
        />

        {(provider.protocol === "oidc" || provider.protocol === "steam") && (
          <ItemListRow
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
                variant="outline"
                onPress={() => setSettingSecret(true)}
              >
                <Trans id="admin.federation.danger.secret.action">Set</Trans>
              </Button>
            }
          />
        )}

        <ItemListRow
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
          provider.linkedAccountCount !== undefined &&
          provider.linkedAccountCount > 0 ? (
            <p>
              <Trans id="admin.federation.danger.delete.confirm.linked">
                {provider.linkedAccountCount} accounts lose the way they sign in
                with it. An account that can only sign in this way needs a new
                registration link from you.
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
        onOpenChange={setSettingSecret}
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
          setSecret.mutate(
            { slug: provider.slug, secret },
            { onSuccess: () => setSettingSecret(false) },
          )
        }
      />
    </>
  );
}

/**
 * One secret in a dialog. It is write-only: the server never returns a stored
 * secret, so the field starts empty and replacing means typing the new value
 * rather than editing the old one.
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
  onSave: (secret: string) => void;
}) {
  const { t } = useLingui();
  const form = useAppForm({
    defaultValues: { secret: "" },
    onSubmit: ({ value }) => onSave(value.secret),
  });

  return (
    <Modal isOpen={isOpen} onOpenChange={onOpenChange}>
      <Modal.Backdrop>
        <Modal.Container placement="center" size="md">
          <Modal.Dialog>
            <Modal.Header>
              <Modal.Heading>{title}</Modal.Heading>
            </Modal.Header>
            <Modal.Body>
              <form.AppForm>
                <form.Form
                  label={t({
                    id: "admin.federation.danger.secret.form",
                    message: "Secret",
                  })}
                  className="flex flex-col gap-4"
                >
                  <form.FormError />
                  <form.AppField
                    name="secret"
                    validators={{
                      onSubmit: ({ value }) =>
                        value.trim() === ""
                          ? msg({
                              id: "admin.federation.danger.secret.required",
                              message: "Enter the secret.",
                            })
                          : undefined,
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
                  <div className="flex items-center gap-2">
                    <form.SubmitButton>
                      <Trans id="admin.federation.danger.secret.save">
                        Save
                      </Trans>
                    </form.SubmitButton>
                    <Button
                      variant="tertiary"
                      isDisabled={isPending}
                      onPress={() => onOpenChange(false)}
                    >
                      <Trans id="confirm.cancel">Cancel</Trans>
                    </Button>
                  </div>
                </form.Form>
              </form.AppForm>
            </Modal.Body>
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    </Modal>
  );
}
