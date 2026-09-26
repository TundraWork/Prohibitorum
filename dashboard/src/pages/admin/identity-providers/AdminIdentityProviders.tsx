import { Alert } from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { useNavigate } from "@tanstack/react-router";
import { LogIn } from "lucide-react";
import { useCursorList } from "@/api/cursor-list";
import { describeError } from "@/api/errors";
import type { components } from "@/api/generated/schema";
import { identityProvidersListOptions } from "@/api/queries";
import { Button } from "@/components/custom/Button";
import { DataTable, type TableColumn } from "@/components/custom/DataTable";
import { EntityCell } from "@/components/custom/EntityCell";
import { TableEmptyState } from "@/components/custom/TableEmptyState";
import { ProviderActions } from "@/pages/admin/identity-providers/ProviderActions";

type Provider = components["schemas"]["IdentityProviderView"];

/**
 * The upstream identity providers this instance signs people in with.
 *
 * A provider is either ready or it is not, and either enabled or not, and those
 * two are the only facts a reader scans for. Rather than two columns mostly
 * reading "yes", the state rides on the row's own icon — see `EntityCell` — so
 * only the rows that need attention carry a mark.
 *
 * A provider that is not ready cannot be enabled, which is the one thing an
 * administrator arriving here usually wants to fix; the row's action menu says
 * why rather than leaving a disabled button unexplained.
 */
export function AdminIdentityProviders() {
  const { t } = useLingui();
  const navigate = useNavigate();
  const list = useCursorList<Provider>(identityProvidersListOptions());

  const columns: TableColumn<Provider>[] = [
    {
      id: "provider",
      header: <Trans id="admin.federation.column.provider">Provider</Trans>,
      cell: (provider) => (
        <EntityCell
          iconUrl={provider.iconUrl}
          name={provider.displayName}
          identifier={provider.slug}
          href={`/admin/identity-providers/${provider.slug}`}
          // Not ready outranks disabled: an operator cannot enable it until it
          // is, so that is the fact worth showing.
          state={
            !provider.ready
              ? "not_ready"
              : provider.disabled
                ? "disabled"
                : undefined
          }
        />
      ),
    },
    {
      id: "protocol",
      header: <Trans id="admin.federation.column.protocol">Protocol</Trans>,
      cell: (provider) =>
        provider.protocol === "oidc" ? (
          <Trans id="admin.federation.protocol.oidc">OIDC</Trans>
        ) : provider.protocol === "steam" ? (
          <Trans id="admin.federation.protocol.steam">Steam</Trans>
        ) : (
          <Trans id="admin.federation.protocol.vrchat">VRChat</Trans>
        ),
    },
    {
      id: "mode",
      header: <Trans id="admin.federation.column.mode">Provisioning</Trans>,
      cell: (provider) =>
        provider.mode === "auto_provision" ? (
          <Trans id="admin.federation.mode.auto">Creates accounts</Trans>
        ) : provider.mode === "invite_only" ? (
          <Trans id="admin.federation.mode.invite">Invitation only</Trans>
        ) : (
          <Trans id="admin.federation.mode.link">Links existing accounts</Trans>
        ),
    },
    {
      id: "actions",
      header: "",
      align: "end",
      pinned: true,
      cell: (provider) => (
        <ProviderActions
          slug={provider.slug}
          displayName={provider.displayName}
          onEdit={() =>
            void navigate({
              to: "/admin/identity-providers/$slug",
              params: { slug: provider.slug },
            })
          }
        />
      ),
    },
  ];

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-4">
        <Button
          onPress={() => void navigate({ to: "/admin/identity-providers/new" })}
        >
          <Trans id="admin.federation.create">Add a provider</Trans>
        </Button>
      </div>

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
          id: "admin.federation.table",
          message: "Identity providers",
        })}
        columns={columns}
        rows={list.items}
        rowId={(provider) => provider.slug}
        loading={list.loading}
        hasMore={list.hasMore}
        loadingMore={list.loadingMore}
        onLoadMore={list.loadMore}
        empty={
          <TableEmptyState
            icon={<LogIn size={18} strokeWidth={1.75} aria-hidden="true" />}
            title={<Trans id="admin.federation.empty">No providers yet</Trans>}
          />
        }
      />
    </div>
  );
}
