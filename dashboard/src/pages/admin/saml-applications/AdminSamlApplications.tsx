import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { FileKey, Pencil, Plus } from "lucide-react";
import { useCursorList } from "@/api/cursor-list";
import type { components } from "@/api/generated/schema";
import { samlAppsListOptions, sessionQueryOptions } from "@/api/queries";
import { quietQuery } from "@/app/query-client";
import { Button } from "@/components/custom/Button";
import { DataTable, type TableColumn } from "@/components/custom/DataTable";
import { EntityCell } from "@/components/custom/EntityCell";
import { APP_DATA_COLUMN, CodeValue } from "@/components/custom/ListCells";
import { PageHeader } from "@/components/custom/PageHeader";
import { LaterPageFailure, SectionFailure } from "@/components/custom/Section";
import { TableEmptyState } from "@/components/custom/TableEmptyState";
import { CertificateText } from "@/pages/admin/saml-applications/CertificateText";
import {
  type CertificateState,
  certificateState,
} from "@/pages/admin/saml-applications/saml-certificate";
import { shortNameIdFormat } from "@/pages/admin/saml-applications/saml-projection";

type SamlApp = components["schemas"]["SAMLApplicationView"];

const openAppMessage = msg({
  id: "admin.saml-apps.open",
  message: "Open {name}",
});

/**
 * The SAML applications this instance is an identity provider for.
 *
 * A reader scanning this list asks three things of a row: what it is, how it
 * identifies people to the service provider, and whether its signing certificate
 * is still good. The first is the identity cell, which dims a disabled
 * application and badges it, and marks a restricted one with a lock; the second
 * is the NameID column; the third is the certificate column.
 *
 * The certificate column is the one fact here that changes on its own, and the
 * one that will eventually break sign-in for the application without anyone
 * touching it. Nobody opens five detail pages to find out which one is about to
 * expire, so the list says it in words, and an expired row says so as well as
 * being coloured.
 *
 * The row's only control opens the application. Every change worth making — the
 * metadata, the attribute map, the access policy, whether it is enabled at all —
 * is a decision that wants the record in front of the reader.
 *
 * A delegated manager sees this page too, with exactly the rows the server gave
 * them and no way to add one: creating an application is an administrator's
 * step. `_protected.admin` admits them; the "New application" button is what
 * distinguishes the two audiences here.
 */
export function AdminSamlApplications() {
  const { t, i18n } = useLingui();
  const navigate = useNavigate();
  const session = useQuery(sessionQueryOptions());
  const list = useCursorList<SamlApp>(quietQuery(samlAppsListOptions()));

  const isAdmin = session.data?.role === "admin";

  const columns: TableColumn<SamlApp>[] = [
    {
      id: "application",
      header: (
        <Trans id="admin.saml-apps.column.application">Application</Trans>
      ),
      cell: (app) => (
        <EntityCell
          iconUrl={app.iconUrl}
          name={app.displayName || app.entityId}
          identifier={<CodeValue value={app.entityId} />}
          link={{
            to: "/admin/saml-applications/$id",
            params: { id: String(app.id) },
          }}
          dimmed={app.disabled}
          restricted={app.accessRestricted}
        />
      ),
    },
    {
      id: "nameIdFormat",
      width: APP_DATA_COLUMN,
      header: <Trans id="admin.saml-apps.column.name-id">NameID format</Trans>,
      // The short name is what an administrator recognises and what a service
      // provider's documentation names; the full URN is the same fact with the
      // specification's namespace in front of it.
      cell: (app) => (
        <span
          data-slot="name-id-format"
          className="font-mono text-muted-foreground text-xs"
        >
          {shortNameIdFormat(app.nameIdFormat)}
        </span>
      ),
    },
    {
      id: "certificate",
      width: APP_DATA_COLUMN,
      header: (
        <Trans id="admin.saml-apps.column.certificate">Certificate</Trans>
      ),
      // The signing certificates are the service provider's own, which the
      // instance uses to check its signed requests; once one lapses, those
      // requests stop verifying.
      cell: (app) => <ListCertificate state={certificateState(app)} />,
    },
    {
      align: "end",
      id: "actions",
      header: "",
      cell: (app) => (
        <Button
          aria-label={i18n._({
            ...openAppMessage,
            values: { name: app.displayName || app.entityId },
          })}
          onClick={() =>
            void navigate({
              to: "/admin/saml-applications/$id",
              params: { id: String(app.id) },
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
        title={<Trans id="admin.saml-apps.page.title">SAML applications</Trans>}
        description={
          <Trans id="admin.saml-apps.page.description">
            Applications that sign people in through this instance with SAML.
          </Trans>
        }
        actions={
          isAdmin ? (
            <Button render={<Link to="/admin/saml-applications/new" />}>
              <Plus aria-hidden="true" />
              <Trans id="admin.saml-apps.create">New application</Trans>
            </Button>
          ) : undefined
        }
      />

      {failed && list.items.length === 0 && (
        <SectionFailure
          error={list.error}
          onRetry={list.refetch}
          title={
            <Trans id="admin.saml-apps.error">
              The SAML applications could not be loaded.
            </Trans>
          }
        />
      )}

      {/* A failed first read has no rows to show, and an empty table under the
          failure would say there are none. */}
      {(!failed || list.items.length > 0) && (
        <DataTable
          label={t({
            id: "admin.saml-apps.table",
            message: "SAML applications",
          })}
          columns={columns}
          rows={list.items}
          rowId={(app) => app.id}
          loading={list.loading}
          hasMore={list.hasMore}
          loadingMore={list.loadingMore}
          onLoadMore={list.loadMore}
          empty={
            <TableEmptyState
              icon={<FileKey size={18} strokeWidth={1.75} aria-hidden="true" />}
              title={
                <Trans id="admin.saml-apps.empty">
                  No SAML applications yet
                </Trans>
              }
              description={
                isAdmin ? (
                  <Trans id="admin.saml-apps.empty.description">
                    Register a service provider to let people sign in to it with
                    SAML.
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
            <Trans id="admin.saml-apps.more_failed">
              The rest of the SAML applications could not be loaded.
            </Trans>
          }
        />
      )}
    </div>
  );
}

/** The certificate column: absent or unreadable states are quiet, dates are not. */
function ListCertificate({ state }: { state: CertificateState }) {
  const hasDate = state.kind === "valid" || state.kind === "expired";
  return (
    <span className={hasDate ? undefined : "text-muted-foreground"}>
      <CertificateText state={state} />
    </span>
  );
}
