import { Trans, useLingui } from "@lingui/react/macro";
import { Link, useNavigate } from "@tanstack/react-router";
import { LogIn, Plus } from "lucide-react";
import { useCursorList } from "@/api/cursor-list";
import { readProviderMode, readProviderProtocol } from "@/api/federation";
import type { components } from "@/api/generated/schema";
import { identityProvidersListOptions } from "@/api/queries";
import { quietQuery } from "@/app/query-client";
import { Button } from "@/components/custom/Button";
import { DataTable, type TableColumn } from "@/components/custom/DataTable";
import { EntityCell } from "@/components/custom/EntityCell";
import { InlineFailure } from "@/components/custom/InlineFailure";
import {
  APP_DATA_COLUMN,
  LinkedAccountsCell,
  NotReadyChip,
} from "@/components/custom/ListCells";
import { PageHeader } from "@/components/custom/PageHeader";
import { TableEmptyState } from "@/components/custom/TableEmptyState";
import { Badge } from "@/components/ui/badge";
import { ProviderActions } from "@/pages/admin/identity-providers/ProviderActions";
import { notReadyReason } from "@/pages/admin/identity-providers/ProviderDangerSection";
import {
  providerModeLabel,
  providerProtocolLabel,
} from "@/pages/admin/identity-providers/provider-options";

type Provider = components["schemas"]["IdentityProviderView"];

/**
 * The upstream identity providers this instance signs people in with.
 *
 * A reader scans this list for three things: which protocol a provider speaks
 * and how it treats someone it has not seen, whether anyone uses it, and whether
 * it is working at all. The first two facts about the configuration share one
 * column — each is a word or two, and a column of two words costs the width of a
 * column of ten.
 *
 * Readiness is the one state the list raises its voice about. A provider that is
 * not ready cannot be enabled, which is what an administrator arriving here
 * usually wants to fix, so the row says so and says what is missing, in text
 * rather than a tooltip. A provider that is merely switched off recedes and is
 * badged instead: it is a settled decision. One that cannot sign anyone in
 * carries a "Sign-in off" badge, because that changes what the provider is for.
 *
 * The account count answers the question asked before touching a provider:
 * whether anyone actually signs in through it. The server counts it in one
 * batched query per page.
 */
export function AdminIdentityProviders() {
  const { t } = useLingui();
  const navigate = useNavigate();
  const list = useCursorList<Provider>(
    quietQuery(identityProvidersListOptions()),
  );

  const columns: TableColumn<Provider>[] = [
    {
      id: "provider",
      header: <Trans id="admin.federation.column.provider">Provider</Trans>,
      cell: (provider) => (
        <EntityCell
          iconUrl={provider.iconUrl}
          name={provider.displayName}
          identifier={provider.slug}
          link={{
            to: "/admin/identity-providers/$slug",
            params: { slug: provider.slug },
          }}
          dimmed={provider.disabled}
          marks={
            provider.allowSignIn ? undefined : (
              <Badge variant="outline" size="sm">
                <Trans id="object.status.sign-in-off">Sign-in off</Trans>
              </Badge>
            )
          }
        />
      ),
    },
    {
      id: "protocol",
      width: APP_DATA_COLUMN,
      header: <Trans id="admin.federation.column.protocol">Protocol</Trans>,
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
      id: "readiness",
      width: APP_DATA_COLUMN,
      header: <Trans id="admin.federation.column.readiness">Readiness</Trans>,
      cell: (provider) =>
        provider.ready ? null : <NotReady provider={provider} />,
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

  const failed = list.error !== null && list.error !== undefined;

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title={
          <Trans id="admin.federation.page.title">Identity providers</Trans>
        }
        description={
          <Trans id="admin.federation.page.description">
            The outside accounts people can sign in with, link to their account
            or accept an invitation through.
          </Trans>
        }
        actions={
          <Button render={<Link to="/admin/identity-providers/new" />}>
            <Plus aria-hidden="true" />
            <Trans id="admin.federation.create">Add a provider</Trans>
          </Button>
        }
      />

      {failed && <InlineFailure error={list.error} />}

      {/* A failed first read has no rows to show, and an empty table under the
          failure would say there are none. */}
      {(!failed || list.items.length > 0) && (
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
              title={
                <Trans id="admin.federation.empty">No providers yet</Trans>
              }
              description={
                <Trans id="admin.federation.empty.description">
                  Add a provider so people can sign in with an account they
                  already have elsewhere.
                </Trans>
              }
            />
          }
        />
      )}
    </div>
  );
}

/**
 * A provider that cannot work yet, and what stands in its way.
 *
 * The reason is the detail page's own sentence, so the two cannot disagree. It
 * wraps inside the column: the table never wraps a cell, but a clipped reason
 * would lose the part that says what to do.
 */
function NotReady({ provider }: { provider: Provider }) {
  return (
    <span className="flex flex-col items-start gap-1">
      <NotReadyChip />
      <span className="whitespace-normal text-muted-foreground text-xs leading-snug">
        {notReadyReason(provider.protocol, !provider.disabled)}
      </span>
    </span>
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
  const { i18n } = useLingui();
  const protocol = readProviderProtocol(provider.protocol);
  const mode = readProviderMode(provider.mode);

  return (
    <span className="flex min-w-0 flex-col">
      <span>
        {protocol === undefined ? (
          <span className="text-muted-foreground">—</span>
        ) : (
          i18n._(providerProtocolLabel(protocol))
        )}
      </span>
      {mode !== undefined && (
        <span className="text-muted-foreground text-xs">
          {i18n._(providerModeLabel(mode))}
        </span>
      )}
    </span>
  );
}
