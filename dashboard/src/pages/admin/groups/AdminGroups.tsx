import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import {
  ChevronRight,
  HandIcon,
  ListFilterIcon,
  Plus,
  UsersRound,
} from "lucide-react";
import { groupsQueryOptions } from "@/api/queries";
import type { AppGroupView } from "@/api/raw-admin-paths";
import { quietQuery } from "@/app/query-client";
import { Button } from "@/components/custom/Button";
import { DataTable, type TableColumn } from "@/components/custom/DataTable";
import { EntityCell } from "@/components/custom/EntityCell";
import { APP_DATA_COLUMN } from "@/components/custom/ListCells";
import { PageHeader } from "@/components/custom/PageHeader";
import { SectionFailure } from "@/components/custom/Section";
import { TableEmptyState } from "@/components/custom/TableEmptyState";
import { Badge } from "@/components/ui/badge";

const openGroupMessage = msg({
  id: "admin.groups.open",
  message: "Open {name}",
});

/**
 * The user-group directory.
 *
 * `GET /groups` answers with the whole directory in one bare array rather than
 * a cursor page, so this list neither pages nor asks for more. A non-admin
 * caller is answered with their own memberships instead, which carry no
 * `applicationCount`; the column reads as absent rather than as zero.
 *
 * A group sent to applications is the ordinary state and says nothing; one that
 * is not sent carries a mark after its name, because it is the one an
 * administrator is looking for when an application is missing a group claim.
 * The row's only control opens the group.
 */
export function AdminGroups() {
  const { t, i18n } = useLingui();
  const navigate = useNavigate();
  const groups = useQuery(quietQuery(groupsQueryOptions()));

  const columns: TableColumn<AppGroupView>[] = [
    {
      id: "group",
      header: <Trans id="admin.groups.column.group">Group</Trans>,
      cell: (group) => (
        <EntityCell
          name={group.displayName}
          identifier={group.slug}
          fallback={
            group.kind === "rule" ? (
              <ListFilterIcon className="size-4" aria-hidden="true" />
            ) : (
              <HandIcon className="size-4" aria-hidden="true" />
            )
          }
          link={{
            to: "/admin/groups/$groupId",
            params: { groupId: String(group.id) },
          }}
          marks={
            group.exposedToDownstream ? null : (
              <Badge variant="outline">
                <Trans id="object.status.not-sent">
                  Not sent to applications
                </Trans>
              </Badge>
            )
          }
        />
      ),
    },
    {
      id: "kind",
      // One word, so it holds less than a data column: the room goes to the
      // group's name, and the count after it stays in view.
      width: "w-28 min-w-28 max-w-28",
      header: <Trans id="admin.groups.column.kind">Kind</Trans>,
      cell: (group) =>
        group.kind === "rule" ? (
          <Trans id="admin.group.kind.rule">Rule</Trans>
        ) : (
          <Trans id="admin.group.kind.manual">Manual</Trans>
        ),
    },
    {
      id: "description",
      width: APP_DATA_COLUMN,
      header: <Trans id="admin.groups.column.description">Description</Trans>,
      cell: (group) =>
        group.description ? (
          <span
            className="block truncate text-muted-foreground"
            title={group.description}
          >
            {group.description}
          </span>
        ) : (
          <span className="text-muted-foreground">—</span>
        ),
    },
    {
      align: "end",
      id: "applicationCount",
      header: <Trans id="admin.groups.column.applications">Applications</Trans>,
      cell: (group) =>
        group.applicationCount === undefined ? (
          <span className="text-muted-foreground">—</span>
        ) : (
          <span className="tabular-nums">{group.applicationCount}</span>
        ),
    },
    {
      align: "end",
      id: "actions",
      header: "",
      cell: (group) => (
        <Button
          aria-label={i18n._({
            ...openGroupMessage,
            values: { name: group.displayName },
          })}
          onClick={() =>
            void navigate({
              to: "/admin/groups/$groupId",
              params: { groupId: String(group.id) },
            })
          }
          variant="ghost"
          size="icon-sm"
        >
          <ChevronRight
            size={16}
            aria-hidden="true"
            className="text-muted-foreground"
          />
        </Button>
      ),
      pinned: true,
    },
  ];

  const failed = groups.error !== null;

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title={<Trans id="admin.groups.page.title">User groups</Trans>}
        description={
          <Trans id="admin.groups.page.description">
            Groups decide which accounts an application lets in, and are the
            names applications receive for them.
          </Trans>
        }
        actions={
          <Button render={<Link to="/admin/groups/new" />}>
            <Plus aria-hidden="true" />
            <Trans id="admin.groups.new">New group</Trans>
          </Button>
        }
      />

      {failed && (
        <SectionFailure
          error={groups.error}
          onRetry={() => void groups.refetch()}
          title={
            <Trans id="admin.groups.error">
              The user groups could not be loaded.
            </Trans>
          }
        />
      )}

      {/* A failed first read has no rows to show, and an empty table under the
          failure would say there are none. */}
      {(!failed || (groups.data?.length ?? 0) > 0) && (
        <DataTable
          label={t({ id: "admin.groups.table", message: "User groups" })}
          columns={columns}
          rows={groups.data ?? []}
          rowId={(group) => group.id}
          loading={groups.isPending}
          empty={
            <TableEmptyState
              icon={
                <UsersRound size={18} strokeWidth={1.75} aria-hidden="true" />
              }
              title={<Trans id="admin.groups.empty">No user groups yet</Trans>}
              description={
                <Trans id="admin.groups.empty.description">
                  Create one to choose accounts by hand or to match them with a
                  rule.
                </Trans>
              }
            />
          }
        />
      )}
    </div>
  );
}
