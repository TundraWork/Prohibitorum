import { AlertDialog, Button, Spinner } from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { describeError, isCancellation } from "@/api/errors";
import type { components } from "@/api/generated/schema";
import {
  identityLinkUrl,
  unlinkIdentityMutationOptions,
} from "@/api/mutations";
import {
  federationProvidersQueryOptions,
  identitiesQueryOptions,
} from "@/api/queries";
import { runWithSudo } from "@/api/sudo";
import { sudoReason } from "@/api/sudo-reasons";
import { DataTable, type TableColumn } from "@/components/custom/DataTable";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";

type Identity = components["schemas"]["AccountIdentityView"];

/**
 * Upstream identities bound to this account.
 *
 * Linking leaves the SPA: the backend answers the begin request with a redirect
 * to the provider and brings the browser back to a console route afterwards, so
 * the browser is sent there directly rather than through the router. The sudo
 * prompt runs first, because the begin endpoint is sudo-guarded and a redirect
 * cannot ask for verification.
 */
export function IdentitiesPanel() {
  const { t, i18n } = useLingui();
  const queryClient = useQueryClient();
  const identities = useQuery(identitiesQueryOptions());
  const providers = useQuery(federationProvidersQueryOptions());
  const [target, setTarget] = useState<Identity | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [linking, setLinking] = useState<string | null>(null);

  const unlink = useMutation(unlinkIdentityMutationOptions(queryClient));

  const format = (value: string) =>
    new Intl.DateTimeFormat(i18n.locale, {
      dateStyle: "medium",
      timeStyle: "short",
    }).format(new Date(value));

  const columns: readonly TableColumn<Identity>[] = [
    {
      id: "provider",
      header: <Trans id="security.identities.column.provider">Provider</Trans>,
      cell: (identity) => (
        <span className="wrap-anywhere font-medium">
          {identity.providerDisplayName}
        </span>
      ),
    },
    {
      id: "protocol",
      header: <Trans id="security.identities.column.protocol">Protocol</Trans>,
      cell: (identity) => (
        <span className="uppercase">{identity.protocol}</span>
      ),
    },
    {
      id: "subject",
      header: <Trans id="security.identities.column.subject">Identifier</Trans>,
      cell: (identity) => (
        <span className="wrap-anywhere font-mono text-xs">
          {identity.subject}
        </span>
      ),
    },
    {
      id: "email",
      header: <Trans id="security.identities.column.email">Email</Trans>,
      cell: (identity) =>
        identity.email ? (
          <span className="wrap-anywhere">{identity.email}</span>
        ) : (
          <span className="text-muted">—</span>
        ),
    },
    {
      id: "linkedAt",
      header: <Trans id="security.identities.column.linked">Linked</Trans>,
      cell: (identity) => format(identity.linkedAt),
    },
    {
      id: "actions",
      header: <Trans id="security.column.actions">Actions</Trans>,
      cell: (identity) => (
        <Button
          size="sm"
          variant="danger-soft"
          onPress={() => setTarget(identity)}
        >
          <Trans id="security.identities.unlink">Unlink</Trans>
        </Button>
      ),
      align: "end",
    },
  ];

  const available = providers.data ?? [];

  return (
    <>
      <p className="max-w-prose text-sm text-muted">
        <Trans id="security.identities.intro">
          Other services you can sign in through. Each identity you link becomes
          another way into this account.
        </Trans>
      </p>

      {error !== null && (
        <SurfaceAlert status="danger" role="alert">
          <SurfaceAlert.Indicator />
          <SurfaceAlert.Content>
            <SurfaceAlert.Title>{t(describeError(error))}</SurfaceAlert.Title>
          </SurfaceAlert.Content>
        </SurfaceAlert>
      )}

      <DataTable
        label={t({
          id: "security.identities.table",
          message: "Connected identities",
        })}
        columns={columns}
        rows={identities.data ?? []}
        rowId={(identity) => identity.id}
        loading={identities.isPending}
        empty={
          <Trans id="security.identities.empty">
            No other sign-in identities are linked yet.
          </Trans>
        }
      />

      <div className="flex flex-col gap-3">
        <h3 className="text-lg font-semibold">
          <Trans id="security.identities.link.title">
            Link another identity
          </Trans>
        </h3>
        <p className="max-w-prose text-sm text-muted">
          <Trans id="security.identities.link.note">
            You will verify your identity here first, and the provider will then
            ask you to sign in.
          </Trans>
        </p>
        {available.length === 0 ? (
          <p className="text-sm text-muted">
            <Trans id="security.identities.link.none">
              This instance has no other providers configured.
            </Trans>
          </p>
        ) : (
          <div className="flex flex-wrap gap-2">
            {available.map((provider) => (
              <Button
                key={provider.slug}
                variant="secondary"
                isPending={linking === provider.slug}
                isDisabled={linking !== null}
                onPress={() => {
                  setError(null);
                  setLinking(provider.slug);
                  void runWithSudo(async () => {
                    // A full-page assignment: the response is a redirect the
                    // browser has to follow, not a body the client can read.
                    window.location.assign(identityLinkUrl(provider.slug));
                  }, sudoReason.linkIdentity)
                    .catch((failure: unknown) => {
                      if (!isCancellation(failure)) setError(failure);
                    })
                    .finally(() => setLinking(null));
                }}
              >
                {linking === provider.slug && (
                  <Spinner size="sm" color="current" />
                )}
                <Trans id="security.identities.link.action">
                  Link {provider.displayName}
                </Trans>
              </Button>
            ))}
          </div>
        )}
      </div>

      <AlertDialog
        isOpen={target !== null}
        onOpenChange={(open) => !open && setTarget(null)}
      >
        <AlertDialog.Backdrop>
          <AlertDialog.Container placement="center" size="md">
            <AlertDialog.Dialog>
              <AlertDialog.Header>
                <AlertDialog.Heading>
                  <Trans id="security.identities.unlink.title">
                    Unlink this identity?
                  </Trans>
                </AlertDialog.Heading>
              </AlertDialog.Header>
              <AlertDialog.Body>
                <p>
                  <Trans id="security.identities.unlink.body">
                    You will no longer be able to sign in through this provider.
                    Your other sign-in methods are unaffected.
                  </Trans>
                </p>
              </AlertDialog.Body>
              <AlertDialog.Footer>
                <Button
                  variant="secondary"
                  onPress={() => setTarget(null)}
                  isDisabled={unlink.isPending}
                >
                  <Trans id="security.cancel">Cancel</Trans>
                </Button>
                <Button
                  variant="danger"
                  isPending={unlink.isPending}
                  onPress={() => {
                    if (!target) return;
                    unlink.mutate(target.id, {
                      onSuccess: () => setTarget(null),
                      onError: (failure) => {
                        setError(failure);
                        setTarget(null);
                      },
                    });
                  }}
                >
                  <Trans id="security.identities.unlink.action">Unlink</Trans>
                </Button>
              </AlertDialog.Footer>
            </AlertDialog.Dialog>
          </AlertDialog.Container>
        </AlertDialog.Backdrop>
      </AlertDialog>
    </>
  );
}
