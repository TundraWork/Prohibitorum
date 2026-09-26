import { Alert } from "@heroui/react";
import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { FileKey, Pencil } from "lucide-react";
import { useCursorList } from "@/api/cursor-list";
import { describeError } from "@/api/errors";
import type { components } from "@/api/generated/schema";
import { samlAppsListOptions, sessionQueryOptions } from "@/api/queries";
import { Button } from "@/components/custom/Button";
import { DataTable, type TableColumn } from "@/components/custom/DataTable";
import { EntityCell } from "@/components/custom/EntityCell";
import {
  CertificateExpiryCell,
  CodeValue,
} from "@/components/custom/ListCells";
import { TableEmptyState } from "@/components/custom/TableEmptyState";
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
 * is still good. The first is the identity cell, the second is the NameID
 * column, and the third is the certificate column at the trailing edge.
 *
 * The certificate column is the one fact here that changes on its own, and the
 * one that will eventually break sign-in for the application without anyone
 * touching it. It was previously only on the application's own page, which is
 * the wrong place for a deadline: nobody opens five detail pages to find out
 * which one is about to expire, and an expired row now says so in the list.
 *
 * An earlier version of this table carried an "IdP-initiated" column that drew
 * "Allowed" and nothing at all otherwise, which left an empty cell meaning
 * either "not allowed" or "the value did not arrive". The permission is still
 * visible on the application's own page; a column whose empty state is ambiguous
 * does not earn its width.
 *
 * The row's only control opens the application. Every change worth making —
 * the metadata, the attribute map, the access policy, whether it is enabled at
 * all — is a decision that wants the record in front of the reader.
 *
 * A delegated manager sees this page too, with exactly the rows the server gave
 * them and no way to add one: creating an application is an administrator's
 * step. `_protected.admin` admits them, so the "New application" button is what
 * distinguishes the two audiences here.
 */
export function AdminSamlApplications() {
  const { t, i18n } = useLingui();
  const navigate = useNavigate();
  const session = useQuery(sessionQueryOptions());
  const list = useCursorList<SamlApp>(samlAppsListOptions());

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
          identifier={<EntityId value={app.entityId} />}
          href={`/admin/saml-applications/${app.id}`}
          dimmed={app.disabled}
          restricted={app.accessRestricted}
        />
      ),
    },
    {
      id: "nameIdFormat",
      header: <Trans id="admin.saml-apps.column.name-id">NameID format</Trans>,
      // The short name is what an administrator recognises and what a service
      // provider's documentation names; the full URN is the same fact with the
      // specification's namespace in front of it.
      cell: (app) => (
        <span className="font-mono text-xs text-muted">
          {shortNameIdFormat(app.nameIdFormat)}
        </span>
      ),
    },
    {
      id: "certificate",
      header: (
        <Trans id="admin.saml-apps.column.certificate">Certificate</Trans>
      ),
      // The signing key is the one that matters: it is what the SP validates
      // assertions against, and what expires. An encryption key has no bearing
      // on whether sign-in works.
      cell: (app) => <CertificateExpiryCell notAfter={signingKeyExpiry(app)} />,
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
            values: { name: app.displayName || app.entityId },
          })}
          onPress={() =>
            void navigate({
              to: "/admin/saml-applications/$id",
              params: { id: String(app.id) },
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
              void navigate({ to: "/admin/saml-applications/new" })
            }
          >
            <Trans id="admin.saml-apps.create">New application</Trans>
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
        label={t({ id: "admin.saml-apps.table", message: "SAML applications" })}
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
              <Trans id="admin.saml-apps.empty">No SAML applications yet</Trans>
            }
          />
        }
      />
    </div>
  );
}

/**
 * The Entity ID as the row's identifier line.
 *
 * An Entity ID is a URI and is routinely longer than the column it sits in, so
 * the cell clips it — `EntityCell` truncates its identifier — and this keeps the
 * whole value one hover away. Every row carries the tooltip rather than only the
 * long ones, because whether a value fits depends on the viewport, and a rule
 * that guessed would be wrong at one width or the other.
 */
function EntityId({ value }: { value: string }) {
  return <CodeValue value={value} />;
}

/**
 * When this application's signing certificate stops being valid.
 *
 * A SAML SP validates assertions against the IdP's signing key, so that is the
 * key whose expiry breaks sign-in; an encryption key expiring would not. The
 * server returns both uses in `keys`, so the filtering happens here rather than
 * on the row.
 *
 * The earliest expiry wins when a key has been rotated and the old one is still
 * published for verification: it is the first date on which something stops
 * working, which is the deadline the reader is looking for.
 */
function signingKeyExpiry(app: SamlApp): string | undefined {
  let earliest: string | undefined;
  for (const key of app.keys ?? []) {
    if (key.use !== "signing" || key.notAfter === undefined) continue;
    if (earliest === undefined || key.notAfter < earliest) {
      earliest = key.notAfter;
    }
  }
  return earliest;
}
