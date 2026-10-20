import { Plural, Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Power, Trash2, TriangleAlertIcon } from "lucide-react";
import { useState } from "react";
import type { components } from "@/api/generated/schema";
import {
  deleteIdentityProviderMutationOptions,
  setIdentityProviderDisabledMutationOptions,
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
import { notReadyReason } from "@/pages/admin/identity-providers/provider-readiness";

type Provider = components["schemas"]["IdentityProviderView"];

/**
 * The two changes that stop a provider, one row each, both visible at once:
 * stop offering it for now, or remove it for good.
 *
 * Disabling confirms, because it takes the provider off the sign-in page and
 * away from every account that could step up with it. Enabling runs straight
 * through, but a provider that is not ready cannot be enabled: the server
 * refuses it with `provider_not_ready`, so the button is disabled and the row
 * says in words what this protocol still needs. Deleting asks for a fresh
 * identity check. A dialog stays open while its write is in flight, and a
 * refusal is shown once, under the rows. Setting a secret is not destructive
 * and lives with the credentials.
 */
export function ProviderDangerSection({ provider }: { provider: Provider }) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const setDisabled = useMutation(
    withInlineReport(
      setIdentityProviderDisabledMutationOptions(queryClient),
      true,
    ),
  );
  const remove = useMutation(
    withInlineReport(deleteIdentityProviderMutationOptions(queryClient), true),
  );
  const failure = useInlineFailure();
  const [confirming, setConfirming] = useState<"disable" | "delete" | null>(
    null,
  );

  const busy = setDisabled.isPending || remove.isPending;
  const cannotEnable = provider.disabled && !provider.ready;
  // Named, so the confirmation carries each as `{count}` in both catalogs.
  const count = provider.linkedAccountCount ?? 0;
  // Pending invitations whose verified snapshot dies with the provider.
  const pending = provider.pendingInvitationCount ?? 0;
  const name = provider.displayName || provider.slug;
  const identityScope = `identity:${provider.slug}`;

  return (
    <DangerZoneFrame
      id="danger"
      icon={<TriangleAlertIcon />}
      title={
        <Trans id="admin.federation.danger.title">Disable or delete</Trans>
      }
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
          stackActions
          actions={
            <Button
              disabled={cannotEnable}
              pending={setDisabled.isPending}
              onClick={() => {
                failure.clear();
                if (provider.disabled) {
                  setDisabled.mutate(
                    { slug: provider.slug, disabled: false },
                    { onError: failure.report },
                  );
                } else {
                  setConfirming("disable");
                }
              }}
              variant="outline"
              size="sm"
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
        >
          {cannotEnable && (
            <p className="flex items-center gap-1.5 text-pretty text-warning-foreground text-sm">
              <TriangleAlertIcon
                aria-hidden="true"
                className="size-3.5 shrink-0"
              />
              {notReadyReason(provider)}
            </p>
          )}
        </ItemListRow>

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
          stackActions
          actions={
            <Button
              pending={remove.isPending}
              onClick={() => {
                failure.clear();
                setConfirming("delete");
              }}
              variant="destructive-outline"
              size="sm"
            >
              <Trans id="admin.federation.danger.delete.action">Delete</Trans>
            </Button>
          }
        />
      </ItemList>

      <InlineFailure error={failure.error} />

      <ConfirmDialog
        isOpen={confirming === "disable"}
        onOpenChange={(open) => !open && !busy && setConfirming(null)}
        status="warning"
        title={
          <Trans id="admin.federation.danger.disable.confirm.title">
            Disable this provider?
          </Trans>
        }
        subject={name}
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
          <Trans id="admin.federation.danger.delete.confirm.title">
            Delete this provider?
          </Trans>
        }
        subject={name}
        body={
          <div className="flex flex-col gap-2">
            {count > 0 && (
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
            )}
            {pending > 0 && (
              <p>
                <Trans id="admin.federation.danger.delete.confirm.pending">
                  <Plural
                    value={pending}
                    one="# unfinished invitation whose identity was verified"
                    other="# unfinished invitations whose identities were verified"
                  />{" "}
                  through it is deleted with the provider.
                </Trans>
              </p>
            )}
            {count === 0 && pending === 0 && (
              <p>
                <Trans id="admin.federation.danger.delete.confirm.empty">
                  The provider and its icon are removed.
                </Trans>
              </p>
            )}
            <p>
              <Trans id="admin.federation.danger.delete.confirm.scope">
                Applications allowed{" "}
                <span className="font-mono">{identityScope}</span> lose that
                scope, and what users agreed to for it is cleared, so a provider
                added later with the same identifier starts fresh.
              </Trans>
            </p>
          </div>
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
              setConfirming(null);
              await navigate({ to: "/admin/identity-providers" });
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
