import { Alert } from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { useNavigate } from "@tanstack/react-router";
import { LogIn } from "lucide-react";
import { useCursorList } from "@/api/cursor-list";
import { describeError } from "@/api/errors";
import { readProviderMode, readProviderProtocol } from "@/api/federation";
import type { components } from "@/api/generated/schema";
import { identityProvidersListOptions } from "@/api/queries";
import { Button } from "@/components/custom/Button";
import { DataTable, type TableColumn } from "@/components/custom/DataTable";
import { EntityCell } from "@/components/custom/EntityCell";
import {
  LinkedAccountsCell,
  NotReadyChip,
} from "@/components/custom/ListCells";
import { TableEmptyState } from "@/components/custom/TableEmptyState";
import { ProviderActions } from "@/pages/admin/identity-providers/ProviderActions";

type Provider = components["schemas"]["IdentityProviderView"];

/**
 * The upstream identity providers this instance signs people in with.
 *
 * A reader scans this list for three things: which protocol a provider speaks,
 * how it treats an unknown person arriving from it, and whether it is working at
 * all. The first two are one column — they are both short descriptions of the
 * same configuration, and splitting them costs a column out of the measure for
 * two words each. The third is the readiness chip at the trailing edge.
 *
 * Readiness is the only state this list announces. A provider that is not ready
 * cannot be enabled, which is the one thing an administrator arriving here
 * usually wants to fix, so it is the fact worth raising; the row's action menu
 * says why. A provider that is merely switched off recedes instead — it is a
 * settled decision, and labelling it would bury the one that needs work.
 *
 * The account count answers the question an operator asks before touching a
 * provider — whether anyone actually signs in through it — which is what decides
 * whether a change is safe. The server counts it in one batched query per page.
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
          dimmed={provider.disabled}
        />
      ),
    },
    {
      id: "protocol",
      header: <Trans id="admin.federation.column.protocol">Protocol</Trans>,
      // Protocol and provisioning mode are two halves of one description: what
      // the provider speaks, and what it does with someone it has not seen. A
      // value the console cannot read is reported as such rather than printed
      // raw — the server narrows both loosely.
      cell: (provider) => <ProtocolMode provider={provider} />,
    },
    {
      align: "end",
      id: "linkedAccounts",
      header: (
        <Trans id="admin.federation.column.linked-accounts">Accounts</Trans>
      ),
      cell: (provider) => (
        <LinkedAccountsCell count={provider.linkedAccountCount} />
      ),
    },
    {
      align: "end",
      id: "ready",
      header: "",
      cell: (provider) => (provider.ready ? null : <NotReadyChip />),
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

/**
 * What a provider speaks, and what it does with a person it has not seen.
 *
 * Both values arrive from the server as loosely-typed strings, so they are
 * narrowed through `api/federation` and anything outside the known vocabulary
 * reads as absent rather than being printed raw. The protocol leads and the
 * provisioning follows in muted text: a reader scanning the column compares
 * protocols, and the provisioning is the qualifier.
 *
 * They sit in one cell rather than two columns because each is one or two words,
 * and a column of two words costs the same width as a column of ten.
 */
function ProtocolMode({ provider }: { provider: Provider }) {
  const protocol = readProviderProtocol(provider.protocol);
  const mode = readProviderMode(provider.mode);
  const modeLabel =
    mode === "auto_provision" ? (
      <Trans id="admin.federation.mode.auto">Creates accounts</Trans>
    ) : mode === "invite_only" ? (
      <Trans id="admin.federation.mode.invite">Invitation only</Trans>
    ) : mode === "link_only" ? (
      <Trans id="admin.federation.mode.link">Links existing accounts</Trans>
    ) : null;

  return (
    <span className="flex min-w-0 flex-col">
      <span>
        {protocol === "oidc" ? (
          <Trans id="admin.federation.protocol.oidc">OIDC</Trans>
        ) : protocol === "steam" ? (
          <Trans id="admin.federation.protocol.steam">Steam</Trans>
        ) : protocol === "vrchat" ? (
          <Trans id="admin.federation.protocol.vrchat">VRChat</Trans>
        ) : (
          <span className="text-muted">—</span>
        )}
      </span>
      {modeLabel !== null && (
        <span className="text-xs text-muted">{modeLabel}</span>
      )}
    </span>
  );
}
