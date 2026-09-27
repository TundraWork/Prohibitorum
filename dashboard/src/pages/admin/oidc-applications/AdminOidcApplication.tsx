import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { oidcAppQueryOptions, sessionQueryOptions } from "@/api/queries";
import { AppAccess, AppManagers } from "@/components/custom/AppAccessPanel";
import {
  AppearanceSection,
  OidcIconSection,
} from "@/pages/admin/oidc-applications/AppearanceSection";
import { ClientConfigSection } from "@/pages/admin/oidc-applications/ClientConfigSection";
import { DangerSection } from "@/pages/admin/oidc-applications/DangerSection";
import { IdentityProjectionSection } from "@/pages/admin/oidc-applications/IdentityProjectionSection";
import { SignInRulesSection } from "@/pages/admin/oidc-applications/SignInRulesSection";
import { Route } from "@/routes/_protected.admin.oidc-applications_.$clientId";

/**
 * One OIDC application's settings, as a column of sections rather than tabs.
 *
 * The order is the order a client is wired up. What the client's own
 * configuration has to match — the Client ID, the addresses, the scopes —
 * comes first, because checking or copying those is what most visits are for.
 * Then the rules a sign-in has to meet, how the application is shown in the
 * console, what a client is told about the account, and last who may use and
 * manage it and the danger zone.
 *
 * Every section is mounted on arrival, so its reads start at once, and each
 * brings its own boundary and says where a failure is (`AGENTS.md`, "Console
 * layout"). The sections do not share unsaved state either: each saves the
 * record it edits through `oidcAppUpdateBody` or its own endpoint, so leaving
 * the page abandons whatever was typed in a form that was never submitted,
 * exactly as on the account's own pages.
 *
 * This file owns the order and the gaps. Each section draws its own heading, so
 * a section's action stays beside the state it reads, and the record is passed
 * down rather than read again: the route loader already has it.
 */
export function AdminOidcApplication() {
  const { clientId } = Route.useParams();
  const { data: app } = useSuspenseQuery(oidcAppQueryOptions(clientId));
  // Read with `useQuery` rather than suspending: the page's shape does not
  // depend on the answer, only on which controls the reader is offered, and a
  // blank section while a role resolves would be worse than a moment without
  // the administrators' one.
  const session = useQuery(sessionQueryOptions());

  return (
    <div className="flex flex-col gap-8">
      <ClientConfigSection app={app} />

      <SignInRulesSection app={app} />

      <AppearanceSection app={app} />

      <OidcIconSection app={app} />

      <IdentityProjectionSection app={app} />

      <AppAccess
        kind="oidc"
        appId={app.clientId}
        isAdmin={session.data?.role === "admin"}
      />

      <AppManagers
        kind="oidc"
        appId={app.clientId}
        isAdmin={session.data?.role === "admin"}
      />

      <DangerSection app={app} />
    </div>
  );
}
