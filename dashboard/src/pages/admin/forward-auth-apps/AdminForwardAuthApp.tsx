import { useLingui } from "@lingui/react/macro";
import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { forwardAuthAppQueryOptions, sessionQueryOptions } from "@/api/queries";
import { AppAccessSection } from "@/components/custom/AppAccessSection";
import { AppDetailLayout } from "@/components/custom/AppDetailLayout";
import {
  AppManagerNote,
  AppManagersSection,
} from "@/components/custom/AppManagersSection";
import { useConsoleCrumb } from "@/components/custom/console-crumb";
import { EntityAvatar } from "@/components/custom/EntityAvatar";
import { ObjectHeader } from "@/components/custom/ObjectHeader";
import { ForwardAuthAppearanceSection } from "@/pages/admin/forward-auth-apps/ForwardAuthAppearanceSection";
import { ForwardAuthDangerSection } from "@/pages/admin/forward-auth-apps/ForwardAuthDangerSection";
import { ForwardAuthHeadersSection } from "@/pages/admin/forward-auth-apps/ForwardAuthHeadersSection";
import { ForwardAuthProxySection } from "@/pages/admin/forward-auth-apps/ForwardAuthProxySection";
import { ForwardAuthServiceSection } from "@/pages/admin/forward-auth-apps/ForwardAuthServiceSection";
import { Route } from "@/routes/_protected.admin.forward-auth-apps_.$clientId";

/**
 * One forward-auth application's page: who it is at the top, then a column of
 * sections with an outline beside them.
 *
 * The order matches the other kinds of application. The host it protects
 * comes first, then who may pass and who manages it; then what the service
 * receives — the headers, with `Remote-User` the one choice among them — and
 * the Traefik configuration that puts the gateway in front of it. How the
 * application is shown comes next, and last the changes that stop it.
 *
 * Every section is mounted on arrival, so its reads start at once, and each
 * saves the record it edits through `forwardAuthAppUpdateBody` or its own
 * endpoint. They do not share unsaved state: leaving the page abandons whatever
 * was typed in a form that was never submitted. The record is passed down
 * rather than read again: the route loader already has it.
 */
export function AdminForwardAuthApp() {
  const { t } = useLingui();
  const { clientId } = Route.useParams();
  const { data: app } = useSuspenseQuery(forwardAuthAppQueryOptions(clientId));
  // Read with `useQuery` rather than suspending: the page's shape does not
  // depend on the answer, only on which controls the reader is offered, and a
  // blank section while a role resolves would be worse than a moment without
  // the administrators' one.
  const session = useQuery(sessionQueryOptions());
  const isAdmin = session.data?.role === "admin";

  const name = app.displayName || app.clientId;
  useConsoleCrumb(name);

  return (
    <AppDetailLayout
      header={
        <div className="flex flex-col gap-3">
          <ObjectHeader
            icon={
              <EntityAvatar
                size="xl"
                iconUrl={app.iconUrl}
                name={name}
                className="rounded-2xl"
                fallbackClassName="rounded-2xl"
              />
            }
            title={name}
            statuses={[
              app.disabled ? "disabled" : "enabled",
              app.accessRestricted ? "restricted" : "open",
            ]}
            identifier={{
              value: app.clientId,
              label: t({
                id: "admin.oidc-apps.copy-client-id",
                message: "Copy the client ID",
              }),
            }}
            // A record with no host has nothing to show: a blank fact would
            // read as a value.
            facts={
              app.forwardAuthHost === ""
                ? []
                : [
                    <span
                      key="host"
                      className="break-all font-mono text-[13px] text-foreground"
                    >
                      {app.forwardAuthHost}
                    </span>,
                  ]
            }
          />
          <AppManagerNote isAdmin={isAdmin} />
        </div>
      }
    >
      <ForwardAuthServiceSection app={app} />
      <AppAccessSection
        kind="forward_auth"
        appId={app.clientId}
        isAdmin={isAdmin}
      />
      <AppManagersSection
        kind="forward_auth"
        appId={app.clientId}
        isAdmin={isAdmin}
      />
      <ForwardAuthHeadersSection app={app} />
      <ForwardAuthProxySection app={app} />
      <ForwardAuthAppearanceSection app={app} />
      <ForwardAuthDangerSection app={app} />
    </AppDetailLayout>
  );
}
