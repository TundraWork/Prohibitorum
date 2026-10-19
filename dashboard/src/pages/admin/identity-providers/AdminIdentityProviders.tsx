import { Trans, useLingui } from "@lingui/react/macro";
import { Link, useNavigate } from "@tanstack/react-router";
import { LogIn, Plus, TriangleAlertIcon } from "lucide-react";
import { useCursorList } from "@/api/cursor-list";
import { readProviderMode, readProviderProtocol } from "@/api/federation";
import type { components } from "@/api/generated/schema";
import { identityProvidersListOptions } from "@/api/queries";
import { quietQuery } from "@/app/query-client";
import { Button } from "@/components/custom/Button";
import { DataTable, type TableColumn } from "@/components/custom/DataTable";
import { EntityCell } from "@/components/custom/EntityCell";
import {
  APP_DATA_COLUMN,
  LinkedAccountsCell,
  NotReadyChip,
} from "@/components/custom/ListCells";
import { PageHeader } from "@/components/custom/PageHeader";
import { LaterPageFailure, SectionFailure } from "@/components/custom/Section";
import { TableEmptyState } from "@/components/custom/TableEmptyState";
import { Badge } from "@/components/ui/badge";
import { ProviderActions } from "@/pages/admin/identity-providers/ProviderActions";
import {
  providerModeLabel,
  providerProtocolLabel,
} from "@/pages/admin/identity-providers/provider-options";
import { notReadyReason } from "@/pages/admin/identity-providers/provider-readiness";

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
 * usually wants to fix, so the name carries a "Not ready" mark, visible at any
 * width, and the row says what is missing in text rather than a tooltip. A provider that is merely switched off recedes and is
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

  const allColumns: TableColumn<Provider>[] = [
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
            <>
              {provider.allowSignIn ? null : (
                <Badge variant="outline">
                  <Trans id="object.status.sign-in-off">Sign-in off</Trans>
                </Badge>
              )}
              {provider.ready ? null : <NotReadyChip />}
            </>
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

  // The readiness column exists to say what is missing, so it is drawn only
  // while some provider on the page is not ready; otherwise it is an empty
  // header over an empty column.
  const anyNotReady = list.items.some((provider) => !provider.ready);
  const columns = allColumns.filter(
    (column) => column.id !== "readiness" || anyNotReady,
  );

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
            <Trans id="admin.federation.create">New provider</Trans>
          </Button>
        }
      />

      {failed && !list.laterFailed && (
        <SectionFailure
          error={list.error}
          onRetry={list.refetch}
          title={
            <Trans id="admin.federation.error">
              The identity providers could not be loaded.
            </Trans>
          }
        />
      )}

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
          hasMore={list.hasMore && !list.laterFailed}
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
      {list.laterFailed && (
        <LaterPageFailure
          error={list.error}
          onRetry={list.loadMore}
          title={
            <Trans id="admin.federation.more_failed">
              The rest of the identity providers could not be loaded.
            </Trans>
          }
        />
      )}
    </div>
  );
}

/**
 * What stands in the way of a provider that is not ready.
 *
 * The state itself is the "Not ready" mark on the name, with the other states
 * of the row, so it is seen at any width; this column only says what to do. It
 * carries the short form of the detail page's reason, so a row stays one line
 * tall and the sentence in full waits on the page.
 */
function NotReady({ provider }: { provider: Provider }) {
  return (
    <span className="flex items-center gap-1.5 text-warning-foreground text-xs">
      <TriangleAlertIcon aria-hidden="true" className="size-3.5 shrink-0" />
      {notReadyReason(provider, true)}
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
