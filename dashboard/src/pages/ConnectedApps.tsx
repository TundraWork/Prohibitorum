import { AlertDialog, Avatar, Button } from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { AppWindow } from "lucide-react";
import { useState } from "react";
import { revokeConsentMutationOptions } from "@/api/mutations";
import { consentQueryOptions } from "@/api/queries";
import { DataTable, type TableColumn } from "@/components/custom/DataTable";

import { SurfaceAlert } from "@/components/custom/SurfaceAlert";

type ConsentedApp = {
  clientId: string;
  kind: string;
  name: string;
  iconUrl?: string;
  grantedAt: string;
  scopes: string[] | null;
};

/**
 * Applications the account has granted access to. This is revocation only: the
 * launchpad that used to live here needs its own consent rules and is not part
 * of this page.
 */
export function ConnectedApps() {
  const { i18n, t } = useLingui();
  const queryClient = useQueryClient();
  const consent = useQuery(consentQueryOptions());
  const [target, setTarget] = useState<ConsentedApp | null>(null);
  const [confirming, setConfirming] = useState(false);

  const revoke = useMutation(revokeConsentMutationOptions(queryClient));

  const format = (value: string) =>
    new Intl.DateTimeFormat(i18n.locale, {
      dateStyle: "medium",
      timeStyle: "short",
    }).format(new Date(value));

  const columns: readonly TableColumn<ConsentedApp>[] = [
    {
      id: "name",
      header: <Trans id="apps.column.name">Application</Trans>,
      cell: (app) => (
        <div className="flex min-w-0 items-center gap-3">
          <Avatar className="size-8 shrink-0">
            {app.iconUrl && <Avatar.Image src={app.iconUrl} alt="" />}
            <Avatar.Fallback>
              <AppWindow size={16} aria-hidden="true" />
            </Avatar.Fallback>
          </Avatar>
          <span className="wrap-anywhere font-medium">{app.name}</span>
        </div>
      ),
    },
    {
      id: "kind",
      header: <Trans id="apps.column.kind">Kind</Trans>,
      cell: (app) =>
        app.kind === "saml" ? (
          <Trans id="apps.kind.saml">SAML</Trans>
        ) : (
          <Trans id="apps.kind.oidc">OpenID Connect</Trans>
        ),
    },
    {
      id: "grantedAt",
      header: <Trans id="apps.column.granted">Authorized</Trans>,
      cell: (app) => format(app.grantedAt),
      align: "end",
    },
    {
      id: "scopes",
      header: <Trans id="apps.column.scopes">Scopes</Trans>,
      cell: (app) =>
        app.scopes && app.scopes.length > 0 ? (
          <span className="wrap-anywhere">{app.scopes.join(", ")}</span>
        ) : (
          <span className="text-muted">
            <Trans id="apps.scopes.none">Basic profile</Trans>
          </span>
        ),
    },
    {
      id: "actions",
      header: <Trans id="apps.column.actions">Actions</Trans>,
      cell: (app) => (
        <Button
          size="sm"
          variant="danger-soft"
          onPress={() => {
            setTarget(app);
            setConfirming(true);
          }}
        >
          <Trans id="apps.remove">Remove access</Trans>
        </Button>
      ),
      align: "end",
    },
  ];

  return (
    <>
      <p className="text-sm text-muted">
        <Trans id="apps.intro">
          Applications you have approved. Removing access revokes what they can
          read about you; they will ask again the next time you sign in.
        </Trans>
      </p>

      {consent.isError && (
        <SurfaceAlert status="danger" role="alert">
          <SurfaceAlert.Indicator />
          <SurfaceAlert.Content>
            <SurfaceAlert.Title>
              {t({
                id: "apps.load_failed",
                message: "Could not load your connected applications.",
              })}
            </SurfaceAlert.Title>
          </SurfaceAlert.Content>
        </SurfaceAlert>
      )}

      <DataTable
        label={t({
          id: "apps.table.label",
          message: "Connected applications",
        })}
        columns={columns}
        rows={consent.data ?? []}
        rowId={(app) => `${app.kind}:${app.clientId}`}
        loading={consent.isPending}
        empty={
          <Trans id="apps.empty">
            You have not approved any applications yet.
          </Trans>
        }
      />

      <AlertDialog isOpen={confirming} onOpenChange={setConfirming}>
        <AlertDialog.Backdrop>
          <AlertDialog.Container placement="center" size="md">
            <AlertDialog.Dialog>
              <AlertDialog.Header>
                <AlertDialog.Heading>
                  <Trans id="apps.confirm.title">Remove access?</Trans>
                </AlertDialog.Heading>
              </AlertDialog.Header>
              <AlertDialog.Body>
                <p>
                  <Trans id="apps.confirm.body">
                    This application will no longer read your profile. It will
                    need to ask for permission again.
                  </Trans>
                </p>
              </AlertDialog.Body>
              <AlertDialog.Footer>
                <Button
                  variant="secondary"
                  onPress={() => setConfirming(false)}
                  isDisabled={revoke.isPending}
                >
                  <Trans id="apps.confirm.cancel">Keep access</Trans>
                </Button>
                <Button
                  variant="danger"
                  isPending={revoke.isPending}
                  onPress={() => {
                    if (!target) return;
                    // `kind` is echoed exactly as listed: the server uses it to
                    // tell an OIDC client id from a SAML entity id.
                    revoke.mutate(
                      { clientId: target.clientId, kind: target.kind },
                      {
                        onSuccess: () => {
                          setConfirming(false);
                          setTarget(null);
                        },
                        // The global toast reports what went wrong.
                        onError: () => setConfirming(false),
                      },
                    );
                  }}
                >
                  <Trans id="apps.confirm.remove">Remove access</Trans>
                </Button>
              </AlertDialog.Footer>
            </AlertDialog.Dialog>
          </AlertDialog.Container>
        </AlertDialog.Backdrop>
      </AlertDialog>
    </>
  );
}
