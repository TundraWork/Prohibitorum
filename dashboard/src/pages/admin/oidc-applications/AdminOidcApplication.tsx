import { Trans, useLingui } from "@lingui/react/macro";
import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { ExternalLinkIcon } from "lucide-react";
import {
  oidcAppQueryOptions,
  oidcDiscoveryQueryOptions,
  sessionQueryOptions,
} from "@/api/queries";
import { quietQuery } from "@/app/query-client";
import { AppAccessSection } from "@/components/custom/AppAccessSection";
import { AppDetailLayout } from "@/components/custom/AppDetailLayout";
import {
  AppManagerNote,
  AppManagersSection,
} from "@/components/custom/AppManagersSection";
import { Button } from "@/components/custom/Button";
import { CopyableValue } from "@/components/custom/CopyableValue";
import { useConsoleCrumb } from "@/components/custom/console-crumb";
import { EntityAvatar } from "@/components/custom/EntityAvatar";
import { ObjectHeader } from "@/components/custom/ObjectHeader";
import { AppearanceSection } from "@/pages/admin/oidc-applications/AppearanceSection";
import { ClientConfigSection } from "@/pages/admin/oidc-applications/ClientConfigSection";
import { CredentialsSection } from "@/pages/admin/oidc-applications/CredentialsSection";
import { DangerSection } from "@/pages/admin/oidc-applications/DangerSection";
import { IdentityProjectionSection } from "@/pages/admin/oidc-applications/IdentityProjectionSection";
import { Route } from "@/routes/_protected.admin.oidc-applications_.$clientId";

/**
 * One OIDC application's page: who it is at the top, then a column of sections
 * with an outline beside them.
 *
 * The order is the order a client is wired up. What the client's own
 * configuration has to match — the addresses, the scopes — comes first, because
 * checking or copying those is what most visits are for. Then who may use and
 * manage it, what a client is told about the account, how the application is
 * shown, its credentials, and last the changes that stop it.
 *
 * Every section is mounted on arrival, so its reads start at once, and each
 * brings its own boundary and says where a failure is (`AGENTS.md`, "Console
 * layout"). The sections do not share unsaved state either: each saves the
 * record it edits through `oidcAppUpdateBody` or its own endpoint, so leaving
 * the page abandons whatever was typed in a form that was never submitted. The
 * record is passed down rather than read again: the route loader already has it.
 */
export function AdminOidcApplication() {
  const { t } = useLingui();
  const { clientId } = Route.useParams();
  const { data: app } = useSuspenseQuery(oidcAppQueryOptions(clientId));
  // Read with `useQuery` rather than suspending: the page's shape does not
  // depend on the answer, only on which controls the reader is offered, and a
  // blank section while a role resolves would be worse than a moment without
  // the administrators' one.
  const session = useQuery(sessionQueryOptions());
  const isAdmin = session.data?.role === "admin";

  // The issuer is a quiet read: until the discovery document answers, and if it
  // cannot, the fact is left out rather than shown as a guess.
  const discovery = useQuery(quietQuery(oidcDiscoveryQueryOptions()));
  const issuer = discovery.data?.issuer;

  const name = app.displayName || app.clientId;
  useConsoleCrumb(name);
  // `none` is the one client authentication method that means "no secret at
  // all", which is what makes a client public.
  const isPublic = app.clientAuthMethod === "none";

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
            facts={[
              isPublic ? (
                <Trans key="kind" id="admin.oidc-apps.kind.public">
                  Public
                </Trans>
              ) : (
                <Trans key="kind" id="admin.oidc-apps.kind.confidential">
                  Confidential
                </Trans>
              ),
            ]}
            actions={
              app.launchUrl !== undefined && app.launchUrl !== "" ? (
                <Button
                  variant="outline"
                  render={
                    <a
                      href={app.launchUrl}
                      target="_blank"
                      rel="noopener noreferrer"
                    />
                  }
                >
                  <Trans id="admin.oidc-apps.open">Open {name}</Trans>
                  <ExternalLinkIcon aria-hidden="true" />
                </Button>
              ) : undefined
            }
          />
          <AppManagerNote isAdmin={isAdmin} />
        </div>
      }
      facts={issuer === undefined ? undefined : <IssuerFact issuer={issuer} />}
    >
      <ClientConfigSection app={app} />
      <AppAccessSection kind="oidc" appId={app.clientId} isAdmin={isAdmin} />
      <AppManagersSection kind="oidc" appId={app.clientId} isAdmin={isAdmin} />
      <IdentityProjectionSection app={app} />
      <AppearanceSection app={app} />
      <CredentialsSection app={app} />
      <DangerSection app={app} />
    </AppDetailLayout>
  );
}

/**
 * The issuer clients are told to trust, with a copy button.
 *
 * It is the one the discovery document names, because the server's issuer is
 * configured and may differ from the origin this console is served from; the
 * origin is never shown in its place.
 */
function IssuerFact({ issuer }: { issuer: string }) {
  const { t } = useLingui();
  return (
    <div className="flex min-w-0 flex-col gap-1">
      <span className="text-muted-foreground">
        <Trans id="admin.oidc-apps.issuer">Issuer</Trans>
      </span>
      <CopyableValue
        value={issuer}
        label={t({
          id: "admin.oidc-apps.copy-issuer",
          message: "Copy the issuer",
        })}
        wrap
      />
    </div>
  );
}
