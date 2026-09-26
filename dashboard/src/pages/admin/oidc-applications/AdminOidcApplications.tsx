import { Alert } from "@heroui/react";
import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Blocks, Pencil } from "lucide-react";
import { useCursorList } from "@/api/cursor-list";
import { describeError } from "@/api/errors";
import type { components } from "@/api/generated/schema";
import { oidcAppsListOptions, sessionQueryOptions } from "@/api/queries";
import { Button } from "@/components/custom/Button";
import { DataTable, type TableColumn } from "@/components/custom/DataTable";
import { EntityCell } from "@/components/custom/EntityCell";
import { RedirectCell } from "@/components/custom/ListCells";
import { TableEmptyState } from "@/components/custom/TableEmptyState";

type OidcApp = components["schemas"]["OIDCApplicationView"];

const openAppMessage = msg({
  id: "admin.oidc-apps.open",
  message: "Open {name}",
});

/**
 * The OIDC applications this instance is a provider for.
 *
 * A reader scanning this list asks three things of a row: what it is, whether a
 * client can still use it, and where it sends people back to. The first is the
 * identity cell, the third is the redirect column, and the second is the state
 * column at the trailing edge.
 *
 * The redirect column replaced a scopes column. A list of scopes is the same
 * four names on almost every row, and a reader comparing rows learned nothing
 * from it; the registered addresses are the fact that differs, and a client with
 * none is the single most common reason sign-in fails. The full scope set is
 * still on the application's own page, where changing it belongs.
 *
 * The row's only control opens the application. Everything worth changing —
 * redirect URIs, the identity projection, the access policy, the secret, whether
 * it is enabled at all — is a decision that wants the record in front of the
 * reader, and the client secret in particular must not be rotatable by a
 * mis-aimed click in a list.
 *
 * A delegated manager sees this page too, with exactly the rows the server gives
 * them and no way to add one: creating an application is an administrator's
 * step. `_protected.admin` admits them; the "New application" button is what
 * distinguishes the two audiences here.
 */
export function AdminOidcApplications() {
  const { t, i18n } = useLingui();
  const navigate = useNavigate();
  const session = useQuery(sessionQueryOptions());
  const list = useCursorList<OidcApp>(oidcAppsListOptions());

  const isAdmin = session.data?.role === "admin";

  const columns: TableColumn<OidcApp>[] = [
    {
      id: "application",
      header: (
        <Trans id="admin.oidc-apps.column.application">Application</Trans>
      ),
      cell: (app) => (
        <EntityCell
          iconUrl={app.iconUrl}
          name={app.displayName || app.clientId}
          identifier={app.clientId}
          href={`/admin/oidc-applications/${encodeURIComponent(app.clientId)}`}
          dimmed={app.disabled}
          restricted={app.accessRestricted}
        />
      ),
    },
    {
      id: "kind",
      header: <Trans id="admin.oidc-apps.column.kind">Type</Trans>,
      // `none` means the client authenticates with nothing but PKCE, which is
      // the only way the server describes a public client on this view.
      cell: (app) =>
        app.clientAuthMethod === "none" ? (
          <Trans id="admin.oidc-apps.kind.public">Public</Trans>
        ) : (
          <Trans id="admin.oidc-apps.kind.confidential">Confidential</Trans>
        ),
    },
    {
      id: "redirectUris",
      header: <Trans id="admin.oidc-apps.column.redirect">Redirects</Trans>,
      cell: (app) => <RedirectCell uris={app.redirectUris ?? []} />,
    },
    {
      align: "end",
      id: "actions",
      header: "",
      cell: (app) => (
        <Button
          isIconOnly
          size="sm"
          variant="ghost"
          aria-label={i18n._({
            ...openAppMessage,
            values: { name: app.displayName || app.clientId },
          })}
          onPress={() =>
            void navigate({
              to: "/admin/oidc-applications/$clientId",
              params: { clientId: app.clientId },
            })
          }
        >
          <Pencil size={16} aria-hidden="true" />
        </Button>
      ),
      pinned: true,
    },
  ];

  return (
    <div className="flex flex-col gap-4">
      {isAdmin && (
        <div className="flex flex-wrap items-center gap-4">
          <Button
            onPress={() =>
              void navigate({ to: "/admin/oidc-applications/new" })
            }
          >
            <Trans id="admin.oidc-apps.create">New application</Trans>
          </Button>
        </div>
      )}

      {list.error !== null && list.error !== undefined && (
        <Alert status="danger" role="alert">
          <Alert.Indicator />
          <Alert.Content>
            <Alert.Title>{t(describeError(list.error))}</Alert.Title>
          </Alert.Content>
        </Alert>
      )}

      <DataTable
        label={t({ id: "admin.oidc-apps.table", message: "OIDC applications" })}
        columns={columns}
        rows={list.items}
        rowId={(app) => app.clientId}
        loading={list.loading}
        hasMore={list.hasMore}
        loadingMore={list.loadingMore}
        onLoadMore={list.loadMore}
        empty={
          <TableEmptyState
            icon={<Blocks size={18} strokeWidth={1.75} aria-hidden="true" />}
            title={
              <Trans id="admin.oidc-apps.empty">No OIDC applications yet</Trans>
            }
          />
        }
      />
    </div>
  );
}
