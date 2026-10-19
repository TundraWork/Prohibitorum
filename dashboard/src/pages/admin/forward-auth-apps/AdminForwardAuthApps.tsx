import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { Pencil, Plus, Route } from "lucide-react";
import { useCursorList } from "@/api/cursor-list";
import type { components } from "@/api/generated/schema";
import { forwardAuthAppsListOptions, sessionQueryOptions } from "@/api/queries";
import { quietQuery } from "@/app/query-client";
import { Button } from "@/components/custom/Button";
import { DataTable, type TableColumn } from "@/components/custom/DataTable";
import { EntityCell } from "@/components/custom/EntityCell";
import { InlineFailure } from "@/components/custom/InlineFailure";
import {
  APP_DATA_COLUMN,
  CodeValue,
  PrincipalSourceCell,
} from "@/components/custom/ListCells";
import { PageHeader } from "@/components/custom/PageHeader";
import { TableEmptyState } from "@/components/custom/TableEmptyState";

type ForwardAuthApp = components["schemas"]["ForwardAuthAppView"];

const openAppMessage = msg({
  id: "admin.forward-auth-apps.open",
  message: "Open {name}",
});

/**
 * The forward-auth applications Traefik asks this instance about.
 *
 * A reader scanning this list asks three things of a row: which service it is,
 * which host it protects, and which identity it passes on. The first is the
 * identity cell, which dims a disabled application and badges it, and marks a
 * restricted one with a lock; the others are columns.
 *
 * The row's only control opens the application. Every change worth making — the
 * host, the identity projection, the access policy, whether it is enabled — is
 * a decision that wants the record in front of the reader.
 *
 * A delegated manager sees this page too, with exactly the rows the server gives
 * them and no way to add one: creating an application is an administrator's
 * step. `_protected.admin` admits them; the "New application" button is what
 * distinguishes the two audiences here.
 */
export function AdminForwardAuthApps() {
  const { t, i18n } = useLingui();
  const navigate = useNavigate();
  const session = useQuery(sessionQueryOptions());
  const list = useCursorList<ForwardAuthApp>(
    quietQuery(forwardAuthAppsListOptions()),
  );

  const isAdmin = session.data?.role === "admin";

  const columns: TableColumn<ForwardAuthApp>[] = [
    {
      id: "application",
      header: (
        <Trans id="admin.forward-auth-apps.column.application">
          Application
        </Trans>
      ),
      cell: (app) => (
        <EntityCell
          iconUrl={app.iconUrl}
          name={app.displayName || app.clientId}
          identifier={app.clientId}
          link={{
            to: "/admin/forward-auth-apps/$clientId",
            params: { clientId: app.clientId },
          }}
          dimmed={app.disabled}
          restricted={app.accessRestricted}
        />
      ),
    },
    {
      id: "host",
      width: APP_DATA_COLUMN,
      header: <Trans id="admin.forward-auth-apps.column.host">Hostname</Trans>,
      // Monospace: the value goes into a Traefik rule character by character.
      cell: (app) =>
        app.forwardAuthHost === "" ? (
          <span className="text-muted-foreground">—</span>
        ) : (
          <CodeValue value={app.forwardAuthHost} />
        ),
    },
    {
      id: "remoteUser",
      width: APP_DATA_COLUMN,
      header: (
        <Trans id="admin.forward-auth-apps.column.remote-user">
          Remote user
        </Trans>
      ),
      // What the upstream service receives as the authenticated identity. The
      // console reads the server's string into the vocabulary it renders and
      // shows a dash for one it cannot read, rather than the raw enum.
      cell: (app) => <PrincipalSourceCell value={app.remoteUserSource} />,
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
              to: "/admin/forward-auth-apps/$clientId",
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
        title={
          <Trans id="admin.forward-auth-apps.page.title">
            Forward-auth applications
          </Trans>
        }
        description={
          <Trans id="admin.forward-auth-apps.page.description">
            Services behind your reverse proxy that this instance signs people
            in to.
          </Trans>
        }
        actions={
          isAdmin ? (
            <Button render={<Link to="/admin/forward-auth-apps/new" />}>
              <Plus aria-hidden="true" />
              <Trans id="admin.forward-auth-apps.create">New application</Trans>
            </Button>
          ) : undefined
        }
      />

      {failed && <InlineFailure error={list.error} />}

      {/* A failed first read has no rows to show, and an empty table under the
          failure would say there are none. */}
      {(!failed || list.items.length > 0) && (
        <DataTable
          label={t({
            id: "admin.forward-auth-apps.table",
            message: "Forward-auth applications",
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
              icon={<Route size={18} strokeWidth={1.75} aria-hidden="true" />}
              title={
                <Trans id="admin.forward-auth-apps.empty">
                  No forward-auth applications yet
                </Trans>
              }
              description={
                isAdmin ? (
                  <Trans id="admin.forward-auth-apps.empty.description">
                    Add a service behind your reverse proxy to let people sign
                    in to it.
                  </Trans>
                ) : (
                  <Trans id="admin.apps.empty.managed">
                    Applications you are assigned to manage appear here.
                  </Trans>
                )
              }
              action={
                isAdmin ? (
                  <Button render={<Link to="/admin/forward-auth-apps/new" />}>
                    <Plus aria-hidden="true" />
                    <Trans id="admin.forward-auth-apps.create">
                      New application
                    </Trans>
                  </Button>
                ) : undefined
              }
            />
          }
        />
      )}
    </div>
  );
}
