import { Alert } from "@heroui/react";
import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Pencil, Route } from "lucide-react";
import { useCursorList } from "@/api/cursor-list";
import { describeError } from "@/api/errors";
import type { components } from "@/api/generated/schema";
import { forwardAuthAppsListOptions, sessionQueryOptions } from "@/api/queries";
import { Button } from "@/components/custom/Button";
import { DataTable, type TableColumn } from "@/components/custom/DataTable";
import { EntityCell } from "@/components/custom/EntityCell";
import { CodeValue, PrincipalSourceCell } from "@/components/custom/ListCells";
import { TableEmptyState } from "@/components/custom/TableEmptyState";
import { scopeSummary } from "@/pages/admin/forward-auth-apps/scope-summary";

type ForwardAuthApp = components["schemas"]["ForwardAuthAppView"];

const openAppMessage = msg({
  id: "admin.forward-auth-apps.open",
  message: "Open {name}",
});

/**
 * The forward-auth applications Traefik asks this instance about.
 *
 * A row is read for three things: which service it is, which host it protects,
 * and what a token may ask for there. The first two are the identity cell and a
 * column; the state — whether it is enabled, whether access is restricted — gets
 * no column of its own, riding on that cell's icon and a padlock after the name,
 * so only the rows that need attention carry a mark (`AGENTS.md`, "Tables and
 * lists").
 *
 * The scope column names at most three declared scopes and counts the rest: the
 * vocabulary is a list of labels the upstream service interprets, and a row of
 * twenty names would say less about the row than `+17` does.
 *
 * The row's only control opens the application. Every change worth making —
 * the host, the vocabulary, the identity projection, the access policy, whether
 * it is enabled — wants the record in front of the reader.
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
  const list = useCursorList<ForwardAuthApp>(forwardAuthAppsListOptions());

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
          href={`/admin/forward-auth-apps/${encodeURIComponent(app.clientId)}`}
          dimmed={app.disabled}
          restricted={app.accessRestricted}
        />
      ),
    },
    {
      id: "host",
      header: <Trans id="admin.forward-auth-apps.column.host">Hostname</Trans>,
      // Monospace: the value goes into a Traefik rule character by character.
      // The tooltip carries the whole host, since a long one is clipped.
      cell: (app) =>
        app.forwardAuthHost === "" ? (
          <span className="text-muted">—</span>
        ) : (
          <CodeValue value={app.forwardAuthHost} />
        ),
    },
    {
      id: "remoteUser",
      header: (
        <Trans id="admin.forward-auth-apps.column.remote-user">
          Remote user
        </Trans>
      ),
      // What the upstream service receives as the authenticated identity. The
      // console reads the server's string into the exact vocabulary it renders
      // and reports nothing for a value it cannot read (see `api/federation`),
      // rather than printing the raw enum at the reader.
      cell: (app) => <PrincipalSourceCell value={app.remoteUserSource} />,
    },
    {
      id: "scopes",
      header: <Trans id="admin.forward-auth-apps.column.scopes">Scopes</Trans>,
      cell: (app) => (
        <span className="text-muted">{scopeSummary(app.scopes)}</span>
      ),
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
              to: "/admin/forward-auth-apps/$clientId",
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
              void navigate({ to: "/admin/forward-auth-apps/new" })
            }
          >
            <Trans id="admin.forward-auth-apps.create">New application</Trans>
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
          />
        }
      />
    </div>
  );
}
