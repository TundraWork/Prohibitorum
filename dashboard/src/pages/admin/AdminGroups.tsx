import { Chip } from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { UsersRound } from "lucide-react";
import { groupsQueryOptions } from "@/api/queries";
import type { AppGroupView } from "@/api/raw-admin-paths";
import { Button } from "@/components/custom/Button";
import { ConsoleCard } from "@/components/custom/ConsoleCard";
import { DataTable, type TableColumn } from "@/components/custom/DataTable";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";
import { TableEmptyState } from "@/components/custom/TableEmptyState";

/**
 * The user-group directory.
 *
 * `GET /groups` answers with the whole directory in one bare array rather than
 * a cursor page, so this list neither pages nor asks for more. A non-admin
 * caller is answered with their own memberships instead, which carry no
 * `applicationCount`; the column reads as absent rather than as zero.
 */
export function AdminGroups() {
  const { t } = useLingui();
  const navigate = useNavigate();
  const groups = useQuery(groupsQueryOptions());

  const openGroup = (id: number) => {
    void navigate({
      to: "/admin/groups/$groupId",
      params: { groupId: String(id) },
    });
  };

  return (
    <ConsoleCard title={<Trans id="admin.groups.title">User groups</Trans>}>
      <p className="text-xs text-muted">
        <Trans id="admin.groups.note">
          A group either keeps the members you add by hand, or works its members
          out from a rule.
        </Trans>
      </p>

      <div className="flex flex-wrap items-center justify-end gap-4">
        <Button onPress={() => void navigate({ to: "/admin/groups/new" })}>
          <Trans id="admin.groups.new">New group</Trans>
        </Button>
      </div>

      {groups.isError && (
        <SurfaceAlert status="danger" role="alert">
          <SurfaceAlert.Indicator />
          <SurfaceAlert.Content>
            <SurfaceAlert.Title>
              <Trans id="admin.groups.error">
                The list could not be loaded. Try again.
              </Trans>
            </SurfaceAlert.Title>
          </SurfaceAlert.Content>
        </SurfaceAlert>
      )}

      <DataTable
        label={t({ id: "admin.groups.table", message: "User groups" })}
        columns={groupColumns(openGroup)}
        rows={groups.data ?? []}
        rowId={(group) => group.id}
        loading={groups.isPending}
        empty={
          <TableEmptyState
            icon={
              <UsersRound size={18} strokeWidth={1.75} aria-hidden="true" />
            }
            title={<Trans id="admin.groups.empty">No user groups yet</Trans>}
          />
        }
      />
    </ConsoleCard>
  );
}

function groupColumns(open: (id: number) => void): TableColumn<AppGroupView>[] {
  return [
    {
      id: "displayName",
      header: <Trans id="admin.groups.column.name">Name</Trans>,
      cell: (group) => (
        <Link
          className="font-medium"
          params={{ groupId: String(group.id) }}
          to="/admin/groups/$groupId"
        >
          {group.displayName}
        </Link>
      ),
    },
    {
      id: "kind",
      header: <Trans id="admin.groups.column.kind">Kind</Trans>,
      cell: (group) => (
        <Chip size="sm" variant="soft">
          {group.kind === "rule" ? (
            <Trans id="admin.groups.kind.rule">Rule</Trans>
          ) : (
            <Trans id="admin.groups.kind.manual">Manual</Trans>
          )}
        </Chip>
      ),
    },
    {
      id: "slug",
      header: <Trans id="admin.groups.column.slug">Slug</Trans>,
      cell: (group) => <span className="font-mono text-xs">{group.slug}</span>,
    },
    {
      id: "description",
      header: <Trans id="admin.groups.column.description">Description</Trans>,
      cell: (group) => group.description || "—",
    },
    {
      align: "end",
      id: "applicationCount",
      header: <Trans id="admin.groups.column.applications">Applications</Trans>,
      cell: (group) => group.applicationCount ?? "—",
    },
    {
      id: "actions",
      header: <Trans id="admin.column.actions">Actions</Trans>,
      cell: (group) => (
        <Button size="sm" variant="secondary" onPress={() => open(group.id)}>
          <Trans id="admin.groups.edit">Edit</Trans>
        </Button>
      ),
      pinned: true,
    },
  ];
}
