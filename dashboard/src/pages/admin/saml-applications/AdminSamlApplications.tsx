import { Alert, Tooltip } from "@heroui/react";
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
 * Three facts distinguish one row from another, and only the first is the
 * record's identity. The name and Entity ID are the identity cell; the state
 * rides on that cell's icon rather than in a column of its own; and the two
 * that remain describe how the application signs people in.
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
          state={app.disabled ? "disabled" : undefined}
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
      id: "idpInitiated",
      header: (
        <Trans id="admin.saml-apps.column.idp-initiated">IdP-initiated</Trans>
      ),
      // Most applications never accept a sign-in the user did not start at the
      // service provider, so the row says nothing unless this one does.
      cell: (app) =>
        app.allowIdpInitiated ? (
          <Trans id="admin.saml-apps.idp-initiated.allowed">Allowed</Trans>
        ) : null,
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
  return (
    <Tooltip delay={0}>
      <Tooltip.Trigger<"span"> render={(props) => <span {...props} />}>
        <span className="font-mono text-xs">{value}</span>
      </Tooltip.Trigger>
      <Tooltip.Content className="break-all font-mono">{value}</Tooltip.Content>
    </Tooltip>
  );
}
