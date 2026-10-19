import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { Blocks, Pencil, Plus } from "lucide-react";
import { useCursorList } from "@/api/cursor-list";
import type { components } from "@/api/generated/schema";
import { oidcAppsListOptions, sessionQueryOptions } from "@/api/queries";
import { quietQuery } from "@/app/query-client";
import { Button } from "@/components/custom/Button";
import { DataTable, type TableColumn } from "@/components/custom/DataTable";
import { EntityCell } from "@/components/custom/EntityCell";
import { APP_DATA_COLUMN, RedirectCell } from "@/components/custom/ListCells";
import { PageHeader } from "@/components/custom/PageHeader";
import { LaterPageFailure, SectionFailure } from "@/components/custom/Section";
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
 * client can still use it, and where it sends people back to. The first two are
 * the identity cell, which dims a disabled application and badges it, and marks a
 * restricted one with a lock; the third is the redirect column.
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
  const list = useCursorList<OidcApp>(quietQuery(oidcAppsListOptions()));

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
          link={{
            to: "/admin/oidc-applications/$clientId",
            params: { clientId: app.clientId },
          }}
          dimmed={app.disabled}
          restricted={app.accessRestricted}
        />
      ),
    },
    {
      id: "kind",
      width: APP_DATA_COLUMN,
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
      width: APP_DATA_COLUMN,
      header: <Trans id="admin.oidc-apps.column.redirect">Redirects</Trans>,
      cell: (app) => <RedirectCell uris={app.redirectUris ?? []} />,
    },
    {
      align: "end",
      id: "actions",
      header: "",
      cell: (app) => (
        <Button
          aria-label={i18n._({
            ...openAppMessage,
            values: { name: app.displayName || app.clientId },
          })}
          onClick={() =>
            void navigate({
              to: "/admin/oidc-applications/$clientId",
              params: { clientId: app.clientId },
            })
          }
          variant="ghost"
          size="icon-sm"
        >
          <Pencil size={16} aria-hidden="true" />
        </Button>
      ),
      pinned: true,
    },
  ];

  const failed = list.error !== null && list.error !== undefined;

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title={<Trans id="admin.oidc-apps.page.title">OIDC applications</Trans>}
        description={
          <Trans id="admin.oidc-apps.page.description">
            Applications that sign people in through this instance with OpenID
            Connect.
          </Trans>
        }
        actions={
          isAdmin ? (
            <Button render={<Link to="/admin/oidc-applications/new" />}>
              <Plus aria-hidden="true" />
              <Trans id="admin.oidc-apps.create">New application</Trans>
            </Button>
          ) : undefined
        }
      />

      {failed && list.items.length === 0 && (
        <SectionFailure
          error={list.error}
          onRetry={list.refetch}
          title={
            <Trans id="admin.oidc-apps.error">
              The OIDC applications could not be loaded.
            </Trans>
          }
        />
      )}

      {/* A failed first read has no rows to show, and an empty table under the
          failure would say there are none. */}
      {(!failed || list.items.length > 0) && (
        <DataTable
          label={t({
            id: "admin.oidc-apps.table",
            message: "OIDC applications",
          })}
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
                <Trans id="admin.oidc-apps.empty">
                  No OIDC applications yet
                </Trans>
              }
              description={
                isAdmin ? (
                  <Trans id="admin.oidc-apps.empty.description">
                    Register a client to let people sign in to an application
                    with OpenID Connect.
                  </Trans>
                ) : (
                  <Trans id="admin.apps.empty.managed">
                    Applications you are assigned to manage appear here.
                  </Trans>
                )
              }
            />
          }
        />
      )}
      {failed && list.items.length > 0 && (
        <LaterPageFailure
          error={list.error}
          onRetry={list.loadMore}
          title={
            <Trans id="admin.oidc-apps.more_failed">
              The rest of the OIDC applications could not be loaded.
            </Trans>
          }
        />
      )}
    </div>
  );
}
